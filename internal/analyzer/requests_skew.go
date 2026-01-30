package analyzer

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/ppiankov/kubenow/internal/metrics"
	"github.com/ppiankov/kubenow/internal/models"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// SilentMode controls whether progress output is suppressed
var SilentMode = false

// logProgress prints progress messages unless silent mode is enabled
func logProgress(format string, args ...interface{}) {
	if !SilentMode {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

// RequestsSkewAnalyzer analyzes resource request vs usage skew
type RequestsSkewAnalyzer struct {
	kubeClient      *kubernetes.Clientset
	metricsProvider metrics.MetricsProvider
	config          RequestsSkewConfig
}

// RequestsSkewConfig holds configuration for requests-skew analysis
type RequestsSkewConfig struct {
	Window            time.Duration // Time window for analysis (e.g., 30d)
	Top               int           // Top N results (0 = all)
	NamespaceRegex    string        // Namespace filter regex
	MinRuntimeDays    int           // Minimum runtime in days to consider
	IncludeKubeSystem bool          // Include kube-system namespace
}

// RequestsSkewResult contains the analysis results
type RequestsSkewResult struct {
	Metadata              RequestsSkewMetadata   `json:"metadata"`
	Summary               RequestsSkewSummary    `json:"summary"`
	Results               []WorkloadSkewAnalysis `json:"results"`
	WorkloadsWithoutMetrics []WorkloadWithoutMetrics `json:"workloads_without_metrics,omitempty"`
}

// WorkloadWithoutMetrics represents a workload found in K8s but missing from Prometheus
type WorkloadWithoutMetrics struct {
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
	Type      string `json:"type"` // Deployment, StatefulSet, etc.
}

// RequestsSkewMetadata contains metadata about the analysis
type RequestsSkewMetadata struct {
	Window         string    `json:"window"`
	MinRuntimeDays int       `json:"min_runtime_days"`
	GeneratedAt    time.Time `json:"generated_at"`
	PrometheusURL  string    `json:"prometheus_url"`
	Cluster        string    `json:"cluster"`
}

// RequestsSkewSummary contains summary statistics
type RequestsSkewSummary struct {
	TotalWorkloads      int     `json:"total_workloads"`
	AnalyzedWorkloads   int     `json:"analyzed_workloads"`
	SkippedWorkloads    int     `json:"skipped_workloads"`
	AvgSkewCPU          float64 `json:"avg_skew_cpu"`
	AvgSkewMemory       float64 `json:"avg_skew_memory"`
	TotalWastedCPU      float64 `json:"total_wasted_cpu"`
	TotalWastedMemoryGi float64 `json:"total_wasted_memory_gi"`
}

// WorkloadSkewAnalysis contains skew analysis for a single workload
type WorkloadSkewAnalysis struct {
	Namespace         string  `json:"namespace"`
	Workload          string  `json:"workload"`
	Type              string  `json:"type"` // Deployment, StatefulSet, etc.
	RequestedCPU      float64 `json:"requested_cpu"`
	RequestedMemoryGi float64 `json:"requested_memory_gi"`
	AvgUsedCPU        float64 `json:"avg_used_cpu"`
	AvgUsedMemoryGi   float64 `json:"avg_used_memory_gi"`
	P95UsedCPU        float64 `json:"p95_used_cpu"`
	P95UsedMemoryGi   float64 `json:"p95_used_memory_gi"`
	P99UsedCPU        float64 `json:"p99_used_cpu"`
	P99UsedMemoryGi   float64 `json:"p99_used_memory_gi"`
	P999UsedCPU       float64 `json:"p999_used_cpu"`
	P999UsedMemoryGi  float64 `json:"p999_used_memory_gi"`
	MaxUsedCPU        float64 `json:"max_used_cpu"`
	MaxUsedMemoryGi   float64 `json:"max_used_memory_gi"`
	SkewCPU           float64 `json:"skew_cpu"`
	SkewMemory        float64 `json:"skew_memory"`
	ImpactScore       float64 `json:"impact_score"`
	Runtime           string  `json:"runtime"`
	Note              string  `json:"note"`

	// Safety analysis
	Safety *models.SafetyAnalysis `json:"safety,omitempty"`
}

// NewRequestsSkewAnalyzer creates a new requests-skew analyzer
func NewRequestsSkewAnalyzer(kubeClient *kubernetes.Clientset, metricsProvider metrics.MetricsProvider, config RequestsSkewConfig) *RequestsSkewAnalyzer {
	// Set defaults
	if config.Window == 0 {
		config.Window = 30 * 24 * time.Hour // 30 days default
	}
	if config.Top == 0 {
		config.Top = 10 // Default top 10
	}
	if config.MinRuntimeDays == 0 {
		config.MinRuntimeDays = 7 // Default 7 days
	}

	return &RequestsSkewAnalyzer{
		kubeClient:      kubeClient,
		metricsProvider: metricsProvider,
		config:          config,
	}
}

// Analyze performs the requests-skew analysis
func (a *RequestsSkewAnalyzer) Analyze(ctx context.Context) (*RequestsSkewResult, error) {
	result := &RequestsSkewResult{
		Metadata: RequestsSkewMetadata{
			Window:         formatDuration(a.config.Window),
			MinRuntimeDays: a.config.MinRuntimeDays,
			GeneratedAt:    time.Now(),
		},
		Results:                 make([]WorkloadSkewAnalysis, 0),
		WorkloadsWithoutMetrics: make([]WorkloadWithoutMetrics, 0),
	}

	// Get all namespaces
	logProgress("[kubenow] Discovering namespaces...\n")
	namespaces, err := a.getFilteredNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get namespaces: %w", err)
	}
	logProgress("[kubenow] Found %d namespaces to analyze\n", len(namespaces))

	// Analyze each namespace
	for i, ns := range namespaces {
		logProgress("[kubenow] [%d/%d] Analyzing namespace: %s\n", i+1, len(namespaces), ns)
		workloads, noMetrics, err := a.analyzeNamespace(ctx, ns)
		if err != nil {
			// Log error but continue with other namespaces
			logProgress("[kubenow] Warning: failed to analyze namespace %s: %v\n", ns, err)
			continue
		}
		if len(workloads) > 0 {
			logProgress("[kubenow]   → Found %d workloads with metrics\n", len(workloads))
		}
		if len(noMetrics) > 0 {
			logProgress("[kubenow]   → Found %d workloads WITHOUT metrics\n", len(noMetrics))
		}
		result.Results = append(result.Results, workloads...)
		result.WorkloadsWithoutMetrics = append(result.WorkloadsWithoutMetrics, noMetrics...)
	}

	// Calculate summary statistics
	logProgress("[kubenow] Calculating summary statistics...\n")
	a.calculateSummary(result)

	// Sort by impact score (descending)
	sort.Slice(result.Results, func(i, j int) bool {
		return result.Results[i].ImpactScore > result.Results[j].ImpactScore
	})

	// Apply top N limit
	if a.config.Top > 0 && len(result.Results) > a.config.Top {
		result.Results = result.Results[:a.config.Top]
	}

	return result, nil
}

// getFilteredNamespaces retrieves namespaces matching the filter
func (a *RequestsSkewAnalyzer) getFilteredNamespaces(ctx context.Context) ([]string, error) {
	nsList, err := a.kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	namespaces := make([]string, 0)
	for _, ns := range nsList.Items {
		// Skip kube-system unless explicitly included
		if !a.config.IncludeKubeSystem && ns.Name == "kube-system" {
			continue
		}

		// TODO: Apply namespace regex filter if configured
		namespaces = append(namespaces, ns.Name)
	}

	return namespaces, nil
}

// analyzeNamespace analyzes all workloads in a namespace
func (a *RequestsSkewAnalyzer) analyzeNamespace(ctx context.Context, namespace string) ([]WorkloadSkewAnalysis, []WorkloadWithoutMetrics, error) {
	workloads := make([]WorkloadSkewAnalysis, 0)
	noMetrics := make([]WorkloadWithoutMetrics, 0)

	// Analyze Deployments
	deployments, err := a.kubeClient.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	for _, deploy := range deployments.Items {
		analysis, hasMetrics, err := a.analyzeWorkload(ctx, namespace, deploy.Name, "Deployment")
		if err != nil {
			// Log but continue
			continue
		}
		if !hasMetrics {
			noMetrics = append(noMetrics, WorkloadWithoutMetrics{
				Namespace: namespace,
				Workload:  deploy.Name,
				Type:      "Deployment",
			})
		} else if analysis != nil {
			workloads = append(workloads, *analysis)
		}
	}

	// Analyze StatefulSets
	statefulsets, err := a.kubeClient.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list statefulsets: %w", err)
	}

	for _, sts := range statefulsets.Items {
		analysis, hasMetrics, err := a.analyzeWorkload(ctx, namespace, sts.Name, "StatefulSet")
		if err != nil {
			continue
		}
		if !hasMetrics {
			noMetrics = append(noMetrics, WorkloadWithoutMetrics{
				Namespace: namespace,
				Workload:  sts.Name,
				Type:      "StatefulSet",
			})
		} else if analysis != nil {
			workloads = append(workloads, *analysis)
		}
	}

	// Analyze DaemonSets
	daemonsets, err := a.kubeClient.AppsV1().DaemonSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list daemonsets: %w", err)
	}

	for _, ds := range daemonsets.Items {
		analysis, hasMetrics, err := a.analyzeWorkload(ctx, namespace, ds.Name, "DaemonSet")
		if err != nil {
			continue
		}
		if !hasMetrics {
			noMetrics = append(noMetrics, WorkloadWithoutMetrics{
				Namespace: namespace,
				Workload:  ds.Name,
				Type:      "DaemonSet",
			})
		} else if analysis != nil {
			workloads = append(workloads, *analysis)
		}
	}

	return workloads, noMetrics, nil
}

// analyzeWorkload analyzes a single workload
// Returns: (*analysis, hasMetrics, error)
// - analysis is nil if no metrics or error
// - hasMetrics is false if workload exists but has no Prometheus metrics
func (a *RequestsSkewAnalyzer) analyzeWorkload(ctx context.Context, namespace, workloadName, workloadType string) (*WorkloadSkewAnalysis, bool, error) {
	// Get workload metrics
	usage, err := a.metricsProvider.GetWorkloadResourceUsage(ctx, namespace, workloadName, a.config.Window)
	if err != nil {
		return nil, true, fmt.Errorf("failed to get workload usage: %w", err)
	}

	// Check if no usage data (workload exists in K8s but no Prometheus metrics)
	if usage.CPUAvg == 0 && usage.MemoryAvg == 0 {
		return nil, false, nil // No metrics found
	}

	// Skip if below minimum runtime
	// TODO: Check actual runtime from Kubernetes API

	// Calculate skew
	cpuSkew := 0.0
	if usage.CPUAvg > 0 {
		cpuSkew = usage.CPURequested / usage.CPUAvg
	}

	memorySkew := 0.0
	if usage.MemoryAvg > 0 {
		memorySkew = usage.MemoryRequested / usage.MemoryAvg
	}

	// Calculate impact score: skew × absolute resources
	impactScore := (cpuSkew * usage.CPURequested) + (memorySkew * (usage.MemoryRequested / (1024 * 1024 * 1024)))

	// Fetch safety data
	safety := a.fetchSafetyData(ctx, namespace, workloadName, workloadType, usage)

	// Generate recommendation note
	note := generateRecommendation(usage.CPURequested, usage.CPUP95, usage.MemoryRequested, usage.MemoryP95)

	// Override note if safety issues detected
	if safety != nil && safety.Rating != models.SafetyRatingSafe {
		note = fmt.Sprintf("%s (Safety: %s)", note, safety.Rating)
	}

	return &WorkloadSkewAnalysis{
		Namespace:         namespace,
		Workload:          workloadName,
		Type:              workloadType,
		RequestedCPU:      usage.CPURequested,
		RequestedMemoryGi: usage.MemoryRequested / (1024 * 1024 * 1024),
		AvgUsedCPU:        usage.CPUAvg,
		AvgUsedMemoryGi:   usage.MemoryAvg / (1024 * 1024 * 1024),
		P95UsedCPU:        usage.CPUP95,
		P95UsedMemoryGi:   usage.MemoryP95 / (1024 * 1024 * 1024),
		P99UsedCPU:        usage.CPUP99,
		P99UsedMemoryGi:   usage.MemoryP99 / (1024 * 1024 * 1024),
		P999UsedCPU:       0, // Will be filled by fetchSafetyData
		P999UsedMemoryGi:  0, // Will be filled by fetchSafetyData
		MaxUsedCPU:        usage.CPUMax,
		MaxUsedMemoryGi:   usage.MemoryMax / (1024 * 1024 * 1024),
		SkewCPU:           cpuSkew,
		SkewMemory:        memorySkew,
		ImpactScore:       impactScore,
		Runtime:           "N/A", // TODO: Calculate from creation time
		Note:              note,
		Safety:            safety,
	}, true, nil
}

// fetchSafetyData retrieves safety-related metrics for a workload
func (a *RequestsSkewAnalyzer) fetchSafetyData(ctx context.Context, namespace, workloadName, workloadType string, usage *metrics.WorkloadUsage) *models.SafetyAnalysis {
	// Type assert to get Prometheus client for safety data
	promClient, ok := a.metricsProvider.(*metrics.PrometheusClient)
	if !ok {
		// Safety data not available for non-Prometheus providers
		return &models.SafetyAnalysis{
			Rating:  models.SafetyRatingUnknown,
			Reasons: []string{"Safety data unavailable (non-Prometheus provider)"},
		}
	}

	// Fetch safety data from Prometheus
	safetyData, err := promClient.GetWorkloadSafetyData(ctx, namespace, workloadName, workloadType, a.config.Window)
	if err != nil {
		return &models.SafetyAnalysis{
			Rating:  models.SafetyRatingUnknown,
			Reasons: []string{fmt.Sprintf("Failed to fetch safety data: %v", err)},
		}
	}

	// Build safety analysis
	safety := &models.SafetyAnalysis{
		Restarts:            int(safetyData["restarts"]),
		CPUThrottledSeconds: safetyData["cpu_throttled_seconds"],
		CPUThrottledPercent: safetyData["cpu_throttled_percent"],
		CPUP999:             safetyData["cpu_p999"],
		MemoryP999:          safetyData["memory_p999"],
		MaxCPUSpike:         0, // TODO: Calculate from time series data
		MaxMemorySpike:      0, // TODO: Calculate from time series data
		CPUSpikeCount:       0, // TODO: Calculate from time series data
		MemorySpikeCount:    0, // TODO: Calculate from time series data
		Rating:              models.SafetyRatingUnknown,
	}

	// Detect ultra-fast spikes (statistical analysis)
	safety.DetectUltraSpikes(usage.CPUAvg, usage.CPUP95, usage.CPUP99, usage.CPUMax)

	// Detect AI workload patterns from pod specs
	a.detectWorkloadPattern(ctx, namespace, workloadName, safety)

	// Determine safety rating
	safety.DetermineRating(usage.CPUP99, usage.MemoryP99, usage.CPURequested, usage.MemoryRequested)

	return safety
}

// detectWorkloadPattern checks pod specs for AI/inference workload indicators
func (a *RequestsSkewAnalyzer) detectWorkloadPattern(ctx context.Context, namespace, workloadName string, safety *models.SafetyAnalysis) {
	// Get pods for this workload
	podList, err := a.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=%s", workloadName),
		Limit:         1, // Just need one pod to check pattern
	})

	if err != nil || len(podList.Items) == 0 {
		// Try alternative label selectors
		podList, err = a.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("app.kubernetes.io/name=%s", workloadName),
			Limit:         1,
		})
	}

	if err != nil || len(podList.Items) == 0 {
		return // Can't detect pattern without pod spec
	}

	pod := podList.Items[0]

	// Extract container commands
	var commands []string
	for _, container := range pod.Spec.Containers {
		commands = append(commands, container.Command...)
		commands = append(commands, container.Args...)
		commands = append(commands, container.Image) // Check image name too
	}

	// Detect AI patterns
	safety.DetectAIWorkloadPattern(commands, pod.Labels, pod.Annotations)
}

// calculateSummary calculates summary statistics
func (a *RequestsSkewAnalyzer) calculateSummary(result *RequestsSkewResult) {
	result.Summary.TotalWorkloads = len(result.Results)
	result.Summary.AnalyzedWorkloads = len(result.Results)

	if len(result.Results) == 0 {
		return
	}

	totalCPUSkew := 0.0
	totalMemSkew := 0.0
	totalWastedCPU := 0.0
	totalWastedMem := 0.0

	for _, w := range result.Results {
		totalCPUSkew += w.SkewCPU
		totalMemSkew += w.SkewMemory

		// Wasted = requested - p95 (with safety margin)
		if w.RequestedCPU > w.P95UsedCPU {
			totalWastedCPU += (w.RequestedCPU - w.P95UsedCPU)
		}
		if w.RequestedMemoryGi > w.P95UsedMemoryGi {
			totalWastedMem += (w.RequestedMemoryGi - w.P95UsedMemoryGi)
		}
	}

	result.Summary.AvgSkewCPU = totalCPUSkew / float64(len(result.Results))
	result.Summary.AvgSkewMemory = totalMemSkew / float64(len(result.Results))
	result.Summary.TotalWastedCPU = totalWastedCPU
	result.Summary.TotalWastedMemoryGi = totalWastedMem
}

// generateRecommendation generates a recommendation note
func generateRecommendation(cpuReq, cpuP95, memReq, memP95 float64) string {
	// Add 50% headroom to p95 for safety
	recommendedCPU := cpuP95 * 1.5
	recommendedMem := memP95 * 1.5

	if cpuReq > recommendedCPU*2 || memReq > recommendedMem*2 {
		return fmt.Sprintf("Consider reducing CPU request to %.2f cores (p95 + 50%% headroom) and memory to %.2fGi",
			recommendedCPU, recommendedMem/(1024*1024*1024))
	}

	return "Resource requests appear reasonable"
}

// formatDuration formats a duration for display
func formatDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
