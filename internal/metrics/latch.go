package metrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ppiankov/kubenow/internal/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

// LatchConfig holds configuration for spike monitoring
type LatchConfig struct {
	SampleInterval time.Duration // How often to sample (e.g., 1s, 5s)
	Duration       time.Duration // How long to monitor (e.g., 15m, 1h, 24h)
	Namespaces     []string      // Namespaces to monitor (empty = all)
}

// SpikeData contains captured spike information
type SpikeData struct {
	Namespace    string    `json:"namespace"`
	WorkloadName string    `json:"workload_name"`
	PodName      string    `json:"pod_name"`
	MaxCPU       float64   `json:"max_cpu"`        // Maximum CPU seen (cores)
	MaxMemory    float64   `json:"max_memory"`     // Maximum memory seen (bytes)
	SampleCount  int       `json:"sample_count"`   // Number of samples taken
	FirstSeen    time.Time `json:"first_seen"`     // First sample timestamp
	LastSeen     time.Time `json:"last_seen"`      // Last sample timestamp
	SpikeCount   int       `json:"spike_count"`    // Number of times spike detected
	AvgCPU       float64   `json:"avg_cpu"`        // Average CPU across samples
	AvgMemory    float64   `json:"avg_memory"`     // Average memory across samples
	CPUSamples   []float64 `json:"cpu_samples"`    // All CPU samples
	MemSamples   []float64 `json:"memory_samples"` // All memory samples

	// Critical signals during monitoring
	OOMKills        int      `json:"oom_kills"`         // Number of OOMKills detected
	Restarts        int      `json:"restarts"`          // Container restarts during monitoring
	Evictions       int      `json:"evictions"`         // Pod evictions during monitoring
	CriticalEvents  []string `json:"critical_events"`   // List of critical event messages
	ThrottlingDetected bool  `json:"throttling_detected"` // CPU throttling detected
}

// LatchMonitor monitors for sub-scrape-interval spikes
type LatchMonitor struct {
	kubeClient    *kubernetes.Clientset
	metricsClient *metricsclientset.Clientset
	config        LatchConfig
	spikeData     map[string]*SpikeData // key: namespace/workload
	mu            sync.RWMutex
	stopCh        chan struct{}
	doneCh        chan struct{}
}

// NewLatchMonitor creates a new spike monitor
func NewLatchMonitor(kubeClient *kubernetes.Clientset, config LatchConfig) (*LatchMonitor, error) {
	// Build metrics client using same config as kubeClient
	restConfig, err := util.BuildRestConfig("")
	if err != nil {
		return nil, fmt.Errorf("failed to build rest config: %w", err)
	}

	metricsClient, err := metricsclientset.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics client: %w", err)
	}

	// Set defaults
	if config.SampleInterval == 0 {
		config.SampleInterval = 5 * time.Second
	}
	if config.Duration == 0 {
		config.Duration = 15 * time.Minute
	}

	return &LatchMonitor{
		kubeClient:    kubeClient,
		metricsClient: metricsClient,
		config:        config,
		spikeData:     make(map[string]*SpikeData),
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}, nil
}

// Start begins monitoring for spikes
func (m *LatchMonitor) Start(ctx context.Context) error {
	ticker := time.NewTicker(m.config.SampleInterval)
	defer ticker.Stop()

	timeout := time.After(m.config.Duration)

	fmt.Printf("[latch] Starting spike monitoring for %s (sampling every %s)\n",
		m.config.Duration, m.config.SampleInterval)

	sampleCount := 0

	for {
		select {
		case <-ctx.Done():
			close(m.doneCh)
			return ctx.Err()
		case <-m.stopCh:
			close(m.doneCh)
			return nil
		case <-timeout:
			fmt.Printf("[latch] Monitoring complete. Captured %d samples.\n", sampleCount)
			close(m.doneCh)
			return nil
		case <-ticker.C:
			if err := m.sample(ctx); err != nil {
				fmt.Printf("[latch] Sample error: %v\n", err)
				continue
			}
			sampleCount++
		}
	}
}

// Stop stops monitoring
func (m *LatchMonitor) Stop() {
	close(m.stopCh)
	<-m.doneCh
}

// sample takes a single metrics sample
func (m *LatchMonitor) sample(ctx context.Context) error {
	// Get pod metrics from Metrics API
	var podMetricsList *metricsv1beta1.PodMetricsList
	var err error

	if len(m.config.Namespaces) == 0 {
		// All namespaces
		podMetricsList, err = m.metricsClient.MetricsV1beta1().PodMetricses(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	} else {
		// Specific namespaces
		allMetrics := &metricsv1beta1.PodMetricsList{Items: []metricsv1beta1.PodMetrics{}}
		for _, ns := range m.config.Namespaces {
			metrics, err := m.metricsClient.MetricsV1beta1().PodMetricses(ns).List(ctx, metav1.ListOptions{})
			if err != nil {
				continue
			}
			allMetrics.Items = append(allMetrics.Items, metrics.Items...)
		}
		podMetricsList = allMetrics
	}

	if err != nil {
		return fmt.Errorf("failed to get pod metrics: %w", err)
	}

	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, podMetrics := range podMetricsList.Items {
		// Skip kube-system
		if podMetrics.Namespace == "kube-system" {
			continue
		}

		// Extract workload name from pod name (remove replica suffix)
		workloadName := extractWorkloadName(podMetrics.Name)
		key := fmt.Sprintf("%s/%s", podMetrics.Namespace, workloadName)

		// Check for critical signals (OOMKills, restarts, evictions)
		go m.checkCriticalSignals(ctx, podMetrics.Namespace, podMetrics.Name, key)

		// Calculate total CPU and memory for pod
		var totalCPU, totalMemory float64
		for _, container := range podMetrics.Containers {
			cpuQuantity := container.Usage.Cpu()
			memQuantity := container.Usage.Memory()

			totalCPU += cpuQuantity.AsApproximateFloat64()
			totalMemory += float64(memQuantity.Value())
		}

		// Initialize or update spike data
		data, exists := m.spikeData[key]
		if !exists {
			data = &SpikeData{
				Namespace:    podMetrics.Namespace,
				WorkloadName: workloadName,
				PodName:      podMetrics.Name,
				FirstSeen:    now,
				CPUSamples:   make([]float64, 0),
				MemSamples:   make([]float64, 0),
			}
			m.spikeData[key] = data
		}

		// Update metrics
		data.LastSeen = now
		data.SampleCount++
		data.CPUSamples = append(data.CPUSamples, totalCPU)
		data.MemSamples = append(data.MemSamples, totalMemory)

		// Track max values
		if totalCPU > data.MaxCPU {
			data.MaxCPU = totalCPU
			// Count as spike if > 2x average (if we have enough samples)
			if data.SampleCount > 10 && totalCPU > data.AvgCPU*2.0 {
				data.SpikeCount++
			}
		}
		if totalMemory > data.MaxMemory {
			data.MaxMemory = totalMemory
		}

		// Calculate running averages
		data.AvgCPU = calculateFloatAverage(data.CPUSamples)
		data.AvgMemory = calculateFloatAverage(data.MemSamples)
	}

	return nil
}

// GetSpikeData returns all captured spike data
func (m *LatchMonitor) GetSpikeData() map[string]*SpikeData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to avoid concurrent modification
	result := make(map[string]*SpikeData)
	for k, v := range m.spikeData {
		// Deep copy
		dataCopy := *v
		dataCopy.CPUSamples = append([]float64{}, v.CPUSamples...)
		dataCopy.MemSamples = append([]float64{}, v.MemSamples...)
		result[k] = &dataCopy
	}
	return result
}

// GetWorkloadSpikeData returns spike data for a specific workload
func (m *LatchMonitor) GetWorkloadSpikeData(namespace, workloadName string) *SpikeData {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := fmt.Sprintf("%s/%s", namespace, workloadName)
	if data, exists := m.spikeData[key]; exists {
		// Return copy
		dataCopy := *data
		dataCopy.CPUSamples = append([]float64{}, data.CPUSamples...)
		dataCopy.MemSamples = append([]float64{}, data.MemSamples...)
		return &dataCopy
	}
	return nil
}

// extractWorkloadName extracts workload name from pod name
// e.g., "payment-api-7d8f9c4b6-abc12" -> "payment-api"
func extractWorkloadName(podName string) string {
	// Simple heuristic: remove last two dash-separated segments (replicaset suffix + pod suffix)
	parts := splitByDash(podName)
	if len(parts) <= 2 {
		return podName
	}
	// Return everything except last 2 segments
	return joinByDash(parts[:len(parts)-2])
}

func splitByDash(s string) []string {
	result := []string{}
	current := ""
	for _, ch := range s {
		if ch == '-' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func joinByDash(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += "-" + parts[i]
	}
	return result
}

func calculateFloatAverage(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range samples {
		sum += v
	}
	return sum / float64(len(samples))
}

// checkCriticalSignals monitors for OOMKills, restarts, evictions, and other critical events
func (m *LatchMonitor) checkCriticalSignals(ctx context.Context, namespace, podName, workloadKey string) {
	// Get pod details to check container statuses
	pod, err := m.kubeClient.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return // Pod may have been deleted
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	data, exists := m.spikeData[workloadKey]
	if !exists {
		return
	}

	if data.CriticalEvents == nil {
		data.CriticalEvents = make([]string, 0)
	}

	// Check container statuses for OOMKills and restarts
	for _, containerStatus := range pod.Status.ContainerStatuses {
		// Check for OOMKills
		if containerStatus.LastTerminationState.Terminated != nil {
			if containerStatus.LastTerminationState.Terminated.Reason == "OOMKilled" {
				data.OOMKills++
				event := fmt.Sprintf("[%s] OOMKill detected in container %s",
					time.Now().Format("15:04:05"), containerStatus.Name)
				data.CriticalEvents = append(data.CriticalEvents, event)
			}
		}

		// Track restarts (compare with previous sample)
		if containerStatus.RestartCount > int32(data.Restarts) {
			newRestarts := int(containerStatus.RestartCount) - data.Restarts
			data.Restarts = int(containerStatus.RestartCount)
			event := fmt.Sprintf("[%s] Container %s restarted (%d total restarts)",
				time.Now().Format("15:04:05"), containerStatus.Name, containerStatus.RestartCount)
			data.CriticalEvents = append(data.CriticalEvents, event)

			// If restart is due to OOM, it was already counted above
			if containerStatus.LastTerminationState.Terminated != nil &&
				containerStatus.LastTerminationState.Terminated.Reason != "OOMKilled" {
				// Log the reason for non-OOM restarts
				reason := containerStatus.LastTerminationState.Terminated.Reason
				if reason != "" {
					data.CriticalEvents[len(data.CriticalEvents)-1] += fmt.Sprintf(" - Reason: %s", reason)
				}
			}

			_ = newRestarts // avoid unused variable warning
		}

		// Check if container is being throttled (based on state)
		if containerStatus.State.Waiting != nil {
			if containerStatus.State.Waiting.Reason == "CrashLoopBackOff" {
				event := fmt.Sprintf("[%s] Container %s in CrashLoopBackOff",
					time.Now().Format("15:04:05"), containerStatus.Name)
				// Only add if not already in events
				if len(data.CriticalEvents) == 0 || data.CriticalEvents[len(data.CriticalEvents)-1] != event {
					data.CriticalEvents = append(data.CriticalEvents, event)
				}
			}
		}
	}

	// Check for pod eviction
	if pod.Status.Reason == "Evicted" {
		data.Evictions++
		event := fmt.Sprintf("[%s] Pod evicted - Message: %s",
			time.Now().Format("15:04:05"), pod.Status.Message)
		data.CriticalEvents = append(data.CriticalEvents, event)
	}

	// Get recent events for this pod (last 5 minutes)
	events, err := m.kubeClient.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s", podName),
	})
	if err == nil {
		fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
		for _, event := range events.Items {
			if event.LastTimestamp.Time.Before(fiveMinutesAgo) {
				continue
			}

			// Look for critical event types
			if event.Reason == "OOMKilling" || event.Reason == "FailedScheduling" ||
				event.Reason == "FailedMount" || event.Reason == "BackOff" {
				eventMsg := fmt.Sprintf("[%s] Event: %s - %s",
					event.LastTimestamp.Format("15:04:05"), event.Reason, event.Message)

				// Avoid duplicates
				isDuplicate := false
				for _, existing := range data.CriticalEvents {
					if existing == eventMsg {
						isDuplicate = true
						break
					}
				}
				if !isDuplicate {
					data.CriticalEvents = append(data.CriticalEvents, eventMsg)
				}
			}
		}
	}
}
