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
	OOMKills           int               `json:"oom_kills"`            // Number of OOMKills detected
	Restarts           int               `json:"restarts"`             // Container restarts during monitoring
	Evictions          int               `json:"evictions"`            // Pod evictions during monitoring
	CriticalEvents     []string          `json:"critical_events"`      // List of critical event messages
	ThrottlingDetected bool              `json:"throttling_detected"`  // CPU throttling detected
	TerminationReasons map[string]int    `json:"termination_reasons"`  // Reasons for container terminations
	ExitCodes          map[int]int       `json:"exit_codes"`           // Exit codes and their frequencies
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
	expectedSamples := int(m.config.Duration / m.config.SampleInterval)

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
			fmt.Printf("[latch] Checking for critical signals (OOMKills, restarts, evictions)...\n")
			m.checkAllCriticalSignals(ctx)
			close(m.doneCh)
			return nil
		case <-ticker.C:
			if err := m.sample(ctx); err != nil {
				fmt.Printf("[latch] Sample error: %v\n", err)
				continue
			}
			sampleCount++
			// Progress indicator every 10%
			if expectedSamples > 0 && sampleCount%(expectedSamples/10+1) == 0 {
				progress := float64(sampleCount) / float64(expectedSamples) * 100
				fmt.Printf("[latch] Progress: %.0f%% (%d/%d samples)\n", progress, sampleCount, expectedSamples)
			}
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
				Namespace:          podMetrics.Namespace,
				WorkloadName:       workloadName,
				PodName:            podMetrics.Name,
				FirstSeen:          now,
				CPUSamples:         make([]float64, 0),
				MemSamples:         make([]float64, 0),
				TerminationReasons: make(map[string]int),
				ExitCodes:          make(map[int]int),
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

// checkAllCriticalSignals checks for OOMKills, restarts, evictions ONCE at the end of monitoring
// This batches API calls and only checks workloads that were actually monitored
func (m *LatchMonitor) checkAllCriticalSignals(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get unique namespaces from monitored workloads
	namespacesMap := make(map[string]bool)
	for key := range m.spikeData {
		// Extract namespace from key (format: "namespace/workload")
		parts := splitByDash(key)
		if len(parts) > 0 {
			// Actually the key uses "/" not "-", let me fix this
			namespace := key[:findFirstSlash(key)]
			if namespace != "" {
				namespacesMap[namespace] = true
			}
		}
	}

	// Batch-fetch all pods from monitored namespaces
	for namespace := range namespacesMap {
		pods, err := m.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			fmt.Printf("[latch] Warning: failed to list pods in namespace %s: %v\n", namespace, err)
			continue
		}

		// Check each pod for critical signals
		for _, pod := range pods.Items {
			workloadName := extractWorkloadName(pod.Name)
			key := fmt.Sprintf("%s/%s", pod.Namespace, workloadName)

			data, exists := m.spikeData[key]
			if !exists {
				continue // Skip pods we didn't monitor
			}

			if data.CriticalEvents == nil {
				data.CriticalEvents = make([]string, 0)
			}

			// Check container statuses for ALL termination reasons and exit codes
			for _, containerStatus := range pod.Status.ContainerStatuses {
				// Track ALL termination reasons (not just OOMKilled)
				if containerStatus.LastTerminationState.Terminated != nil {
					terminated := containerStatus.LastTerminationState.Terminated
					reason := terminated.Reason
					exitCode := int(terminated.ExitCode)

					// Count termination reason
					if data.TerminationReasons == nil {
						data.TerminationReasons = make(map[string]int)
					}
					data.TerminationReasons[reason]++

					// Count exit code
					if data.ExitCodes == nil {
						data.ExitCodes = make(map[int]int)
					}
					data.ExitCodes[exitCode]++

					// Special handling for known critical reasons
					switch reason {
					case "OOMKilled":
						data.OOMKills++
						event := fmt.Sprintf("OOMKilled: container %s (exit code %d)", containerStatus.Name, exitCode)
						data.CriticalEvents = append(data.CriticalEvents, event)
					case "Error":
						event := fmt.Sprintf("Error: container %s exited with code %d - %s",
							containerStatus.Name, exitCode, getExitCodeMeaning(exitCode))
						data.CriticalEvents = append(data.CriticalEvents, event)
					case "ContainerCannotRun":
						event := fmt.Sprintf("ContainerCannotRun: %s - %s",
							containerStatus.Name, terminated.Message)
						data.CriticalEvents = append(data.CriticalEvents, event)
					default:
						if reason != "Completed" && reason != "" {
							event := fmt.Sprintf("Terminated: container %s - reason: %s (exit code %d)",
								containerStatus.Name, reason, exitCode)
							data.CriticalEvents = append(data.CriticalEvents, event)
						}
					}
				}

				// Track restarts
				if containerStatus.RestartCount > 0 {
					data.Restarts = int(containerStatus.RestartCount)
					if containerStatus.RestartCount > 5 {
						event := fmt.Sprintf("High restart count: container %s has %d restarts",
							containerStatus.Name, containerStatus.RestartCount)
						data.CriticalEvents = append(data.CriticalEvents, event)
					}
				}

				// Check current state for issues
				if containerStatus.State.Waiting != nil {
					reason := containerStatus.State.Waiting.Reason
					switch reason {
					case "CrashLoopBackOff":
						event := fmt.Sprintf("CrashLoopBackOff: container %s", containerStatus.Name)
						data.CriticalEvents = append(data.CriticalEvents, event)
					case "ImagePullBackOff", "ErrImagePull":
						event := fmt.Sprintf("Image issue: container %s - %s", containerStatus.Name, reason)
						data.CriticalEvents = append(data.CriticalEvents, event)
					case "CreateContainerConfigError", "CreateContainerError":
						event := fmt.Sprintf("Container creation issue: %s - %s", containerStatus.Name, reason)
						data.CriticalEvents = append(data.CriticalEvents, event)
					}
				}

				// Check for CPU throttling in current state
				if containerStatus.State.Running != nil {
					// Note: actual throttling detection would require cgroup metrics
					// This is a placeholder for future enhancement
				}
			}

			// Check for pod eviction
			if pod.Status.Reason == "Evicted" {
				data.Evictions++
				event := fmt.Sprintf("Pod evicted - %s", pod.Status.Message)
				data.CriticalEvents = append(data.CriticalEvents, event)
			}
		}

		// Batch-fetch recent events for this namespace (last 30 minutes)
		events, err := m.kubeClient.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}

		thirtyMinutesAgo := time.Now().Add(-30 * time.Minute)
		for _, event := range events.Items {
			if event.LastTimestamp.Time.Before(thirtyMinutesAgo) {
				continue
			}

			// Extract workload name from pod name
			podName := event.InvolvedObject.Name
			workloadName := extractWorkloadName(podName)
			key := fmt.Sprintf("%s/%s", namespace, workloadName)

			data, exists := m.spikeData[key]
			if !exists {
				continue
			}

			// Look for critical event types
			if event.Reason == "OOMKilling" || event.Reason == "FailedScheduling" ||
				event.Reason == "FailedMount" || event.Reason == "BackOff" {
				eventMsg := fmt.Sprintf("Event: %s - %s", event.Reason, truncateString(event.Message, 100))

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

// Helper functions
func findFirstSlash(s string) int {
	for i, ch := range s {
		if ch == '/' {
			return i
		}
	}
	return -1
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// getExitCodeMeaning returns human-readable explanation for common exit codes
func getExitCodeMeaning(exitCode int) string {
	switch exitCode {
	case 0:
		return "Success"
	case 1:
		return "General error"
	case 2:
		return "Misuse of shell command"
	case 126:
		return "Command cannot execute"
	case 127:
		return "Command not found"
	case 128:
		return "Invalid exit argument"
	case 130:
		return "SIGINT (Ctrl+C)"
	case 137:
		return "SIGKILL (usually OOMKilled or killed by system)"
	case 139:
		return "SIGSEGV (segmentation fault)"
	case 143:
		return "SIGTERM (graceful termination)"
	case 255:
		return "Exit status out of range"
	default:
		// Check if it's a signal (128 + signal number)
		if exitCode > 128 && exitCode < 256 {
			signal := exitCode - 128
			return fmt.Sprintf("Killed by signal %d", signal)
		}
		return "Unknown error"
	}
}
