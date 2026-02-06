package analyzer

import (
	"context"
	"fmt"
	"os"
	"regexp"
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
	Namespace         string        // Specific namespace to analyze (overrides regex)
	NamespaceRegex    string        // Namespace filter regex
	MinRuntimeDays    int           // Minimum runtime in days to consider
	IncludeKubeSystem bool          // Include kube-system namespace
	SortBy            string        // Sort by: impact|skew|cpu|memory|name (default: impact)
}

// RequestsSkewResult contains the analysis results
type RequestsSkewResult struct {
	Metadata                RequestsSkewMetadata     `json:"metadata"`
	Summary                 RequestsSkewSummary      `json:"summary"`
	Results                 []WorkloadSkewAnalysis   `json:"results"`
	WorkloadsWithoutMetrics []WorkloadWithoutMetrics `json:"workloads_without_metrics,omitempty"`
	NamespaceQuotas         []NamespaceQuotaInfo     `json:"namespace_quotas,omitempty"`
	SpikeData               map[string]interface{}   `json:"spike_data,omitempty"` // Real-time spike monitoring data (if enabled)
}

// WorkloadWithoutMetrics represents a workload found in K8s but missing from Prometheus
type WorkloadWithoutMetrics struct {
	Namespace string `json:"namespace"`
	Workload  string `json:"workload"`
	Type      string `json:"type"` // Deployment, StatefulSet, etc.
}

// NamespaceQuotaInfo contains ResourceQuota and LimitRange information for a namespace
type NamespaceQuotaInfo struct {
	Namespace             string                 `json:"namespace"`
	HasResourceQuota      bool                   `json:"has_resource_quota"`
	HasLimitRange         bool                   `json:"has_limit_range"`
	QuotaCPU              QuotaUsage             `json:"quota_cpu,omitempty"`
	QuotaMemory           QuotaUsage             `json:"quota_memory,omitempty"`
	LimitRangeDefaults    *LimitRangeDefaults    `json:"limit_range_defaults,omitempty"`
	PotentialQuotaSavings *PotentialQuotaSavings `json:"potential_quota_savings,omitempty"`
}

// QuotaUsage represents resource quota usage
type QuotaUsage struct {
	Hard string  `json:"hard"`           // e.g., "100" cores or "200Gi"
	Used string  `json:"used"`           // e.g., "75" cores or "150Gi"
	HardValue float64 `json:"hard_value"` // Numeric value for calculations
	UsedValue float64 `json:"used_value"` // Numeric value for calculations
	Utilization float64 `json:"utilization_percent"` // Used/Hard * 100
}

// LimitRangeDefaults contains default resource values from LimitRange
type LimitRangeDefaults struct {
	DefaultCPU        string `json:"default_cpu,omitempty"`         // e.g., "100m"
	DefaultMemory     string `json:"default_memory,omitempty"`      // e.g., "128Mi"
	DefaultRequestCPU string `json:"default_request_cpu,omitempty"` // e.g., "100m"
	DefaultRequestMemory string `json:"default_request_memory,omitempty"` // e.g., "128Mi"
	MinCPU            string `json:"min_cpu,omitempty"`
	MinMemory         string `json:"min_memory,omitempty"`
	MaxCPU            string `json:"max_cpu,omitempty"`
	MaxMemory         string `json:"max_memory,omitempty"`
}

// PotentialQuotaSavings shows how much quota could be freed by reducing requests
type PotentialQuotaSavings struct {
	CPUSavings    float64 `json:"cpu_savings"`     // Cores that could be freed
	MemorySavings float64 `json:"memory_savings_gi"` // GiB that could be freed
	CPUPercent    float64 `json:"cpu_percent"`     // % of quota
	MemoryPercent float64 `json:"memory_percent"`  // % of quota
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

	// Quota/LimitRange context
	UsingDefaultRequests bool   `json:"using_default_requests,omitempty"` // True if using LimitRange defaults
	QuotaContext         string `json:"quota_context,omitempty"`          // E.g., "Namespace has quota: 50% utilized"
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

	// Fetch quota/limitrange info for namespaces
	logProgress("[kubenow] Fetching ResourceQuotas and LimitRanges...\n")
	quotaMap := make(map[string]*NamespaceQuotaInfo)
	for _, ns := range namespaces {
		quotaInfo, err := a.getNamespaceQuotaInfo(ctx, ns)
		if err != nil {
			logProgress("[kubenow] Warning: failed to get quota info for namespace %s: %v\n", ns, err)
			continue
		}
		if quotaInfo != nil {
			quotaMap[ns] = quotaInfo
			result.NamespaceQuotas = append(result.NamespaceQuotas, *quotaInfo)
		}
	}

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

		// Add quota context to workloads
		if quotaInfo, exists := quotaMap[ns]; exists {
			for i := range workloads {
				a.enrichWorkloadWithQuotaContext(&workloads[i], quotaInfo)
			}
		}

		result.Results = append(result.Results, workloads...)
		result.WorkloadsWithoutMetrics = append(result.WorkloadsWithoutMetrics, noMetrics...)
	}

	// Calculate potential quota savings
	logProgress("[kubenow] Calculating potential quota savings...\n")
	a.calculateQuotaSavings(result)

	// Diagnose why workloads don't have metrics (sample up to 5)
	if len(result.WorkloadsWithoutMetrics) > 0 {
		logProgress("[kubenow] Diagnosing why workloads lack metrics (sampling up to 5)...\n")
		a.diagnoseWorkloadsWithoutMetrics(ctx, result)
	}

	// Calculate summary statistics
	logProgress("[kubenow] Calculating summary statistics...\n")
	a.calculateSummary(result)

	// Sort results based on configured option
	a.sortResults(result)

	// Apply top N limit
	if a.config.Top > 0 && len(result.Results) > a.config.Top {
		result.Results = result.Results[:a.config.Top]
	}

	return result, nil
}

// getFilteredNamespaces retrieves namespaces matching the filter
func (a *RequestsSkewAnalyzer) getFilteredNamespaces(ctx context.Context) ([]string, error) {
	// If a specific namespace is provided, use only that one
	if a.config.Namespace != "" {
		// Verify the namespace exists
		_, err := a.kubeClient.CoreV1().Namespaces().Get(ctx, a.config.Namespace, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("namespace %s not found: %w", a.config.Namespace, err)
		}
		return []string{a.config.Namespace}, nil
	}

	nsList, err := a.kubeClient.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	namespaces := make([]string, 0)

	// Compile regex if provided
	var namespaceRegex *regexp.Regexp
	if a.config.NamespaceRegex != "" && a.config.NamespaceRegex != ".*" {
		namespaceRegex, err = regexp.Compile(a.config.NamespaceRegex)
		if err != nil {
			return nil, fmt.Errorf("invalid namespace regex: %w", err)
		}
	}

	for _, ns := range nsList.Items {
		// Skip kube-system unless explicitly included
		if !a.config.IncludeKubeSystem && ns.Name == "kube-system" {
			continue
		}

		// Apply namespace regex filter if configured
		if namespaceRegex != nil && !namespaceRegex.MatchString(ns.Name) {
			continue
		}

		namespaces = append(namespaces, ns.Name)
	}

	return namespaces, nil
}

// getNamespaceQuotaInfo fetches ResourceQuota and LimitRange information for a namespace
func (a *RequestsSkewAnalyzer) getNamespaceQuotaInfo(ctx context.Context, namespace string) (*NamespaceQuotaInfo, error) {
	info := &NamespaceQuotaInfo{
		Namespace: namespace,
	}

	// Fetch ResourceQuotas
	quotas, err := a.kubeClient.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list resource quotas: %w", err)
	}

	if len(quotas.Items) > 0 {
		info.HasResourceQuota = true
		// Use the first quota (most namespaces have only one)
		quota := quotas.Items[0]

		// Extract CPU quota
		if hardCPU, ok := quota.Status.Hard["requests.cpu"]; ok {
			if usedCPU, ok := quota.Status.Used["requests.cpu"]; ok {
				info.QuotaCPU = QuotaUsage{
					Hard:      hardCPU.String(),
					Used:      usedCPU.String(),
					HardValue: float64(hardCPU.MilliValue()) / 1000.0,
					UsedValue: float64(usedCPU.MilliValue()) / 1000.0,
				}
				if info.QuotaCPU.HardValue > 0 {
					info.QuotaCPU.Utilization = (info.QuotaCPU.UsedValue / info.QuotaCPU.HardValue) * 100
				}
			}
		}

		// Extract Memory quota
		if hardMem, ok := quota.Status.Hard["requests.memory"]; ok {
			if usedMem, ok := quota.Status.Used["requests.memory"]; ok {
				info.QuotaMemory = QuotaUsage{
					Hard:      hardMem.String(),
					Used:      usedMem.String(),
					HardValue: float64(hardMem.Value()) / (1024 * 1024 * 1024), // Convert to GiB
					UsedValue: float64(usedMem.Value()) / (1024 * 1024 * 1024), // Convert to GiB
				}
				if info.QuotaMemory.HardValue > 0 {
					info.QuotaMemory.Utilization = (info.QuotaMemory.UsedValue / info.QuotaMemory.HardValue) * 100
				}
			}
		}
	}

	// Fetch LimitRanges
	limitRanges, err := a.kubeClient.CoreV1().LimitRanges(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list limit ranges: %w", err)
	}

	if len(limitRanges.Items) > 0 {
		info.HasLimitRange = true
		info.LimitRangeDefaults = &LimitRangeDefaults{}

		// Extract defaults from first LimitRange (most namespaces have only one)
		lr := limitRanges.Items[0]
		for _, limit := range lr.Spec.Limits {
			if limit.Type == "Container" {
				if defaultCPU, ok := limit.Default["cpu"]; ok {
					info.LimitRangeDefaults.DefaultCPU = defaultCPU.String()
				}
				if defaultMem, ok := limit.Default["memory"]; ok {
					info.LimitRangeDefaults.DefaultMemory = defaultMem.String()
				}
				if defaultReqCPU, ok := limit.DefaultRequest["cpu"]; ok {
					info.LimitRangeDefaults.DefaultRequestCPU = defaultReqCPU.String()
				}
				if defaultReqMem, ok := limit.DefaultRequest["memory"]; ok {
					info.LimitRangeDefaults.DefaultRequestMemory = defaultReqMem.String()
				}
				if minCPU, ok := limit.Min["cpu"]; ok {
					info.LimitRangeDefaults.MinCPU = minCPU.String()
				}
				if minMem, ok := limit.Min["memory"]; ok {
					info.LimitRangeDefaults.MinMemory = minMem.String()
				}
				if maxCPU, ok := limit.Max["cpu"]; ok {
					info.LimitRangeDefaults.MaxCPU = maxCPU.String()
				}
				if maxMem, ok := limit.Max["memory"]; ok {
					info.LimitRangeDefaults.MaxMemory = maxMem.String()
				}
			}
		}
	}

	// Only return info if there's quota or limitrange data
	if !info.HasResourceQuota && !info.HasLimitRange {
		return nil, nil
	}

	return info, nil
}

// enrichWorkloadWithQuotaContext adds quota/limitrange context to a workload
func (a *RequestsSkewAnalyzer) enrichWorkloadWithQuotaContext(workload *WorkloadSkewAnalysis, quotaInfo *NamespaceQuotaInfo) {
	if quotaInfo.HasResourceQuota {
		workload.QuotaContext = fmt.Sprintf("Quota: CPU %.0f%%, Memory %.0f%%",
			quotaInfo.QuotaCPU.Utilization, quotaInfo.QuotaMemory.Utilization)
	}

	// Check if workload might be using LimitRange defaults
	if quotaInfo.HasLimitRange && quotaInfo.LimitRangeDefaults != nil {
		// Simple heuristic: if workload CPU request matches default CPU request, likely using defaults
		defaultReqCPU := quotaInfo.LimitRangeDefaults.DefaultRequestCPU
		if defaultReqCPU != "" {
			// Parse default CPU (e.g., "100m" = 0.1 cores)
			// Simple check: if workload CPU is very close to common defaults
			if workload.RequestedCPU == 0.1 || workload.RequestedCPU == 0.5 || workload.RequestedCPU == 1.0 {
				workload.UsingDefaultRequests = true
				workload.QuotaContext += " | Possibly using LimitRange defaults"
			}
		}
	}
}

// calculateQuotaSavings calculates potential quota savings from reducing requests
func (a *RequestsSkewAnalyzer) calculateQuotaSavings(result *RequestsSkewResult) {
	// Group workloads by namespace
	workloadsByNs := make(map[string][]WorkloadSkewAnalysis)
	for _, w := range result.Results {
		workloadsByNs[w.Namespace] = append(workloadsByNs[w.Namespace], w)
	}

	// Calculate savings for each namespace with quotas
	for i := range result.NamespaceQuotas {
		quota := &result.NamespaceQuotas[i]
		if !quota.HasResourceQuota {
			continue
		}

		workloads := workloadsByNs[quota.Namespace]
		if len(workloads) == 0 {
			continue
		}

		var cpuSavings, memorySavings float64
		for _, w := range workloads {
			// Potential savings = requested - p95 (conservative estimate)
			if w.RequestedCPU > w.P95UsedCPU {
				cpuSavings += (w.RequestedCPU - w.P95UsedCPU)
			}
			if w.RequestedMemoryGi > w.P95UsedMemoryGi {
				memorySavings += (w.RequestedMemoryGi - w.P95UsedMemoryGi)
			}
		}

		quota.PotentialQuotaSavings = &PotentialQuotaSavings{
			CPUSavings:    cpuSavings,
			MemorySavings: memorySavings,
		}

		// Calculate percentage of quota
		if quota.QuotaCPU.HardValue > 0 {
			quota.PotentialQuotaSavings.CPUPercent = (cpuSavings / quota.QuotaCPU.HardValue) * 100
		}
		if quota.QuotaMemory.HardValue > 0 {
			quota.PotentialQuotaSavings.MemoryPercent = (memorySavings / quota.QuotaMemory.HardValue) * 100
		}
	}
}

// diagnoseWorkloadsWithoutMetrics samples workloads to understand why they lack Prometheus metrics
func (a *RequestsSkewAnalyzer) diagnoseWorkloadsWithoutMetrics(ctx context.Context, result *RequestsSkewResult) {
	sampleSize := 5
	if len(result.WorkloadsWithoutMetrics) < sampleSize {
		sampleSize = len(result.WorkloadsWithoutMetrics)
	}

	// Sample up to 5 workloads
	for i := 0; i < sampleSize; i++ {
		w := result.WorkloadsWithoutMetrics[i]

		// Get pods for this workload
		labelSelector := fmt.Sprintf("app=%s", w.Workload)
		pods, err := a.kubeClient.CoreV1().Pods(w.Namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labelSelector,
			Limit:         1,
		})

		if err != nil || len(pods.Items) == 0 {
			// Try alternative label selector
			labelSelector = fmt.Sprintf("app.kubernetes.io/name=%s", w.Workload)
			pods, err = a.kubeClient.CoreV1().Pods(w.Namespace).List(ctx, metav1.ListOptions{
				LabelSelector: labelSelector,
				Limit:         1,
			})
		}

		var diagnosis string
		if err != nil {
			diagnosis = fmt.Sprintf("Unable to query pods: %v", err)
		} else if len(pods.Items) == 0 {
			diagnosis = "No pods found - workload may have no replicas or incorrect label selector"
		} else {
			pod := pods.Items[0]
			if pod.Status.Phase != "Running" {
				diagnosis = fmt.Sprintf("Pod not running (phase: %s)", pod.Status.Phase)
			} else if len(pod.Spec.Containers) == 0 {
				diagnosis = "Pod has no containers"
			} else {
				// Check if pod has the right labels for Prometheus scraping
				hasAppLabel := false
				for key := range pod.Labels {
					if key == "app" || key == "app.kubernetes.io/name" {
						hasAppLabel = true
						break
					}
				}
				if !hasAppLabel {
					diagnosis = "Pod missing standard app labels (app or app.kubernetes.io/name)"
				} else {
					diagnosis = "Pod running with labels, but no Prometheus metrics - check ServiceMonitor/PodMonitor configuration"
				}
			}
		}

		// Store diagnosis in the workload metadata (we'll need to extend the struct)
		result.WorkloadsWithoutMetrics[i].Type = fmt.Sprintf("%s (%s)", w.Type, diagnosis)
	}
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
		analysis, hasMetrics, err := a.analyzeWorkload(ctx, namespace, deploy.Name, "Deployment", deploy.CreationTimestamp.Time)
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
		analysis, hasMetrics, err := a.analyzeWorkload(ctx, namespace, sts.Name, "StatefulSet", sts.CreationTimestamp.Time)
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
		analysis, hasMetrics, err := a.analyzeWorkload(ctx, namespace, ds.Name, "DaemonSet", ds.CreationTimestamp.Time)
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
func (a *RequestsSkewAnalyzer) analyzeWorkload(ctx context.Context, namespace, workloadName, workloadType string, creationTime time.Time) (*WorkloadSkewAnalysis, bool, error) {
	// Get workload metrics
	usage, err := a.metricsProvider.GetWorkloadResourceUsage(ctx, namespace, workloadName, a.config.Window)
	if err != nil {
		return nil, true, fmt.Errorf("failed to get workload usage: %w", err)
	}

	// Check if no usage data (workload exists in K8s but no Prometheus metrics)
	if usage.CPUAvg == 0 && usage.MemoryAvg == 0 {
		return nil, false, nil // No metrics found
	}

	// Calculate runtime
	runtimeDays := int(time.Since(creationTime).Hours() / 24)

	// Skip if below minimum runtime
	if runtimeDays < a.config.MinRuntimeDays {
		return nil, false, nil // Workload too young
	}

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
		Runtime:           fmt.Sprintf("%dd", runtimeDays),
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

// sortResults sorts workload results based on configured sort option
func (a *RequestsSkewAnalyzer) sortResults(result *RequestsSkewResult) {
	sortBy := a.config.SortBy
	if sortBy == "" {
		sortBy = "impact" // Default
	}

	switch sortBy {
	case "impact":
		// Sort by impact score (descending - highest impact first)
		sort.Slice(result.Results, func(i, j int) bool {
			return result.Results[i].ImpactScore > result.Results[j].ImpactScore
		})
	case "skew":
		// Sort by CPU skew ratio (descending - highest skew first)
		sort.Slice(result.Results, func(i, j int) bool {
			return result.Results[i].SkewCPU > result.Results[j].SkewCPU
		})
	case "cpu":
		// Sort by wasted CPU (descending - most wasted first)
		sort.Slice(result.Results, func(i, j int) bool {
			wastedI := result.Results[i].RequestedCPU - result.Results[i].P95UsedCPU
			wastedJ := result.Results[j].RequestedCPU - result.Results[j].P95UsedCPU
			return wastedI > wastedJ
		})
	case "memory":
		// Sort by wasted memory (descending - most wasted first)
		sort.Slice(result.Results, func(i, j int) bool {
			wastedI := result.Results[i].RequestedMemoryGi - result.Results[i].P95UsedMemoryGi
			wastedJ := result.Results[j].RequestedMemoryGi - result.Results[j].P95UsedMemoryGi
			return wastedI > wastedJ
		})
	case "name":
		// Sort alphabetically by namespace/workload (ascending)
		sort.Slice(result.Results, func(i, j int) bool {
			nameI := fmt.Sprintf("%s/%s", result.Results[i].Namespace, result.Results[i].Workload)
			nameJ := fmt.Sprintf("%s/%s", result.Results[j].Namespace, result.Results[j].Workload)
			return nameI < nameJ
		})
	}
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
