package metrics

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ppiankov/kubenow/internal/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

const podLabelRefreshInterval = 60 * time.Second

// LatchConfig holds configuration for spike monitoring
type LatchConfig struct {
	SampleInterval time.Duration    // How often to sample (e.g., 1s, 5s)
	Duration       time.Duration    // How long to monitor (e.g., 15m, 1h, 24h)
	Namespaces     []string         // Namespaces to monitor (empty = all)
	WorkloadFilter string           // If set, only sample this workload name (pro-monitor mode)
	PodLevel       bool             // If true, match exact pod name instead of extracting workload name
	ProgressFunc   func(msg string) // Optional progress callback. If nil, print to stderr.
}

// SpikeData contains captured spike information
type SpikeData struct {
	Namespace    string    `json:"namespace"`
	WorkloadName string    `json:"workload_name"`
	OperatorType string    `json:"operator_type,omitempty"`
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
	OOMKills            int            `json:"oom_kills"`             // Number of OOMKills detected
	Restarts            int            `json:"restarts"`              // Container restarts during monitoring
	Evictions           int            `json:"evictions"`             // Pod evictions during monitoring
	CriticalEvents      []string       `json:"critical_events"`       // List of critical event messages
	ThrottlingDetected  bool           `json:"throttling_detected"`   // CPU throttling detected
	TerminationReasons  map[string]int `json:"termination_reasons"`   // Reasons for container terminations
	ExitCodes           map[int]int    `json:"exit_codes"`            // Exit codes and their frequencies
	LastTerminationTime *time.Time     `json:"last_termination_time"` // When the last termination happened
}

// LatchMonitor monitors for sub-scrape-interval spikes
type LatchMonitor struct {
	kubeClient    *kubernetes.Clientset
	metricsClient *metricsclientset.Clientset
	config        LatchConfig
	spikeData     map[string]*SpikeData // key: namespace/workload
	podLabels     map[string]map[string]string
	mu            sync.RWMutex
	stopCh        chan struct{}
	doneCh        chan struct{}

	// restartBaseline records restart counts at latch start so that
	// checkAllCriticalSignals only reports restarts that occurred during
	// the latch window, not historical restarts from before monitoring.
	// Key: "namespace/pod/container"
	restartBaseline map[string]int32
}

// NewLatchMonitor creates a new spike monitor
func NewLatchMonitor(kubeClient *kubernetes.Clientset, config LatchConfig, kubeOpts ...util.KubeOpts) (*LatchMonitor, error) {
	// Build metrics client using same config as kubeClient
	var opts util.KubeOpts
	if len(kubeOpts) > 0 {
		opts = kubeOpts[0]
	}
	restConfig, err := util.BuildRestConfigWithOpts(opts)
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
		podLabels:     make(map[string]map[string]string),
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}, nil
}

func (m *LatchMonitor) progress(msg string) {
	if m.config.ProgressFunc != nil {
		m.config.ProgressFunc(msg)
	} else {
		fmt.Fprintln(os.Stderr, msg)
	}
}

// Start begins monitoring for spikes
func (m *LatchMonitor) Start(ctx context.Context) error {
	m.refreshPodLabels(ctx)

	// Snapshot restart counts before monitoring so we only report
	// restarts that happen during the latch window.
	m.recordRestartBaseline(ctx)

	ticker := time.NewTicker(m.config.SampleInterval)
	defer ticker.Stop()

	timeout := time.After(m.config.Duration)

	m.progress(fmt.Sprintf("[latch] Starting spike monitoring for %s (sampling every %s)",
		m.config.Duration, m.config.SampleInterval))

	sampleCount := 0
	expectedSamples := int(m.config.Duration / m.config.SampleInterval)
	lastLabelRefresh := time.Now()

	for {
		select {
		case <-ctx.Done():
			close(m.doneCh)
			return ctx.Err()
		case <-m.stopCh:
			close(m.doneCh)
			return nil
		case <-timeout:
			m.progress(fmt.Sprintf("[latch] Monitoring complete. Captured %d samples.", sampleCount))
			m.progress(fmt.Sprintf("[latch] Checking for critical signals (OOMKills, restarts, evictions)..."))
			m.checkAllCriticalSignals(ctx)
			close(m.doneCh)
			return nil
		case <-ticker.C:
			if time.Since(lastLabelRefresh) >= podLabelRefreshInterval {
				m.refreshPodLabels(ctx)
				lastLabelRefresh = time.Now()
			}
			if err := m.sample(ctx); err != nil {
				m.progress(fmt.Sprintf("[latch] Sample error: %v", err))
				continue
			}
			sampleCount++
			// Progress indicator every 10%
			if expectedSamples > 0 && sampleCount%(expectedSamples/10+1) == 0 {
				progress := float64(sampleCount) / float64(expectedSamples) * 100
				m.progress(fmt.Sprintf("[latch] Progress: %.0f%% (%d/%d samples)", progress, sampleCount, expectedSamples))
			}
		}
	}
}

// Stop stops monitoring
func (m *LatchMonitor) Stop() {
	close(m.stopCh)
	<-m.doneCh
}

// recordRestartBaseline snapshots current restart counts for all pods
// in the monitored namespaces. Called once at the start of Start().
func (m *LatchMonitor) recordRestartBaseline(ctx context.Context) {
	m.restartBaseline = make(map[string]int32)

	namespaces := m.config.Namespaces
	if len(namespaces) == 0 {
		return // all-namespace mode; baseline will be empty, delta falls back to total
	}

	for _, ns := range namespaces {
		pods, err := m.kubeClient.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}
		for i := range pods.Items {
			pod := &pods.Items[i]
			for j := range pod.Status.ContainerStatuses {
				cs := &pod.Status.ContainerStatuses[j]
				key := fmt.Sprintf("%s/%s/%s", pod.Namespace, pod.Name, cs.Name)
				m.restartBaseline[key] = cs.RestartCount
			}
		}
	}
}

func (m *LatchMonitor) refreshPodLabels(ctx context.Context) {
	namespaces := m.config.Namespaces
	if len(namespaces) == 0 {
		pods, err := m.kubeClient.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
		if err != nil {
			return
		}
		labels := make(map[string]map[string]string, len(pods.Items))
		for i := range pods.Items {
			pod := &pods.Items[i]
			labels[pod.Name] = pod.Labels
		}
		m.mu.Lock()
		m.podLabels = labels
		m.mu.Unlock()
		return
	}

	labels := make(map[string]map[string]string)
	for _, ns := range namespaces {
		pods, err := m.kubeClient.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return
		}
		for i := range pods.Items {
			pod := &pods.Items[i]
			labels[pod.Name] = pod.Labels
		}
	}
	m.mu.Lock()
	m.podLabels = labels
	m.mu.Unlock()
}

// restartDelta returns the number of restarts that occurred since the
// baseline was recorded. If no baseline exists for this container,
// falls back to the full restart count (conservative).
func (m *LatchMonitor) restartDelta(namespace, podName, containerName string, current int32) int32 {
	key := fmt.Sprintf("%s/%s/%s", namespace, podName, containerName)
	baseline, ok := m.restartBaseline[key]
	if !ok {
		return current
	}
	delta := current - baseline
	if delta < 0 {
		return current // pod was recreated; use full count
	}
	return delta
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

	for _, podMetrics := range podMetricsList.Items {
		// Skip kube-system
		if podMetrics.Namespace == "kube-system" {
			continue
		}

		var labels map[string]string
		if !m.config.PodLevel {
			m.mu.RLock()
			labels = m.podLabels[podMetrics.Name]
			m.mu.RUnlock()
		}
		workloadName := podMetrics.Name
		var operatorType string
		if !m.config.PodLevel {
			workloadName, operatorType = ResolveWorkloadIdentity(podMetrics.Name, labels)
		}

		// Skip if workload filter is set and doesn't match
		if m.config.WorkloadFilter != "" && workloadName != m.config.WorkloadFilter {
			continue
		}

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
		m.mu.Lock()
		data, exists := m.spikeData[key]
		if !exists {
			data = &SpikeData{
				Namespace:          podMetrics.Namespace,
				WorkloadName:       workloadName,
				OperatorType:       operatorType,
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
		// Cap sample buffer at 17280 (24h at 5s intervals) to bound memory
		const maxSamples = 17280
		if len(data.CPUSamples) >= maxSamples {
			data.CPUSamples = data.CPUSamples[1:]
			data.MemSamples = data.MemSamples[1:]
		}
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
		m.mu.Unlock()
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
		slash := findFirstSlash(key)
		if slash > 0 {
			namespace := key[:slash]
			namespacesMap[namespace] = true
		}
	}

	// Batch-fetch all pods from monitored namespaces
	for namespace := range namespacesMap {
		pods, err := m.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			m.progress(fmt.Sprintf("[latch] Warning: failed to list pods in namespace %s: %v", namespace, err))
			continue
		}

		// Check each pod for critical signals
		for _, pod := range pods.Items {
			workloadName := pod.Name
			if !m.config.PodLevel {
				workloadName, _ = ResolveWorkloadIdentity(pod.Name, pod.Labels)
			}
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
					finishedAt := terminated.FinishedAt.Time

					// Track when the last termination happened
					if data.LastTerminationTime == nil || finishedAt.After(*data.LastTerminationTime) {
						data.LastTerminationTime = &finishedAt
					}

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

				// Track restarts that occurred during the latch window only
				delta := m.restartDelta(pod.Namespace, pod.Name, containerStatus.Name, containerStatus.RestartCount)
				if delta > 0 {
					data.Restarts += int(delta)
					if delta > 5 {
						event := fmt.Sprintf("High restart count: container %s had %d restarts during latch",
							containerStatus.Name, delta)
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
			labels := m.podLabels[podName]
			workloadName := podName
			if !m.config.PodLevel {
				workloadName, _ = ResolveWorkloadIdentity(podName, labels)
			}
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

// Percentiles holds computed percentile values for a sample set.
type Percentiles struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
	Avg float64 `json:"avg"`
}

// ComputePercentiles computes p50, p95, p99, max, and avg from the CPU and memory samples.
// Returns nil if there are no samples.
func (d *SpikeData) ComputePercentiles() (cpu *Percentiles, mem *Percentiles) {
	if len(d.CPUSamples) == 0 {
		return nil, nil
	}

	cpu = computePercentiles(d.CPUSamples)
	mem = computePercentiles(d.MemSamples)
	return cpu, mem
}

// GapCount returns the number of expected samples that were missed.
// A gap is defined as expectedSamples - actualSamples.
func (d *SpikeData) GapCount(interval time.Duration) int {
	if interval <= 0 || d.SampleCount == 0 {
		return 0
	}
	duration := d.LastSeen.Sub(d.FirstSeen)
	if duration <= 0 {
		return 0
	}
	expected := int(duration/interval) + 1
	gaps := expected - d.SampleCount
	if gaps < 0 {
		return 0
	}
	return gaps
}

func computePercentiles(samples []float64) *Percentiles {
	n := len(samples)
	if n == 0 {
		return &Percentiles{}
	}

	// Sort a copy to avoid mutating the original
	sorted := make([]float64, n)
	copy(sorted, samples)
	sortFloat64s(sorted)

	return &Percentiles{
		P50: percentile(sorted, 0.50),
		P95: percentile(sorted, 0.95),
		P99: percentile(sorted, 0.99),
		Max: sorted[n-1],
		Avg: calculateFloatAverage(samples),
	}
}

func percentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	idx := p * float64(n-1)
	lower := int(idx)
	upper := lower + 1
	if upper >= n {
		return sorted[n-1]
	}
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

func sortFloat64s(a []float64) {
	// Simple insertion sort — fast for typical latch sample sizes (<10k)
	for i := 1; i < len(a); i++ {
		key := a[i]
		j := i - 1
		for j >= 0 && a[j] > key {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = key
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
