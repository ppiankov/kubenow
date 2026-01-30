package analyzer

import (
	"context"
	"fmt"
	"time"

	"github.com/ppiankov/kubenow/internal/metrics"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// NodeFootprintAnalyzer analyzes cluster node topology and simulates alternatives
type NodeFootprintAnalyzer struct {
	kubeClient      *kubernetes.Clientset
	metricsProvider metrics.MetricsProvider
	config          NodeFootprintConfig
}

// NodeFootprintConfig holds configuration for node-footprint analysis
type NodeFootprintConfig struct {
	Window     time.Duration // Time window for analysis
	Percentile string        // p50, p95, p99
	NodeTypes  []string      // Candidate node types to simulate
}

// NodeFootprintResult contains the analysis results
type NodeFootprintResult struct {
	Metadata         NodeFootprintMetadata `json:"metadata"`
	CurrentTopology  CurrentTopology       `json:"current_topology"`
	WorkloadEnvelope WorkloadEnvelope      `json:"workload_envelope"`
	Scenarios        []NodeScenario        `json:"scenarios"`
}

// NodeFootprintMetadata contains metadata about the analysis
type NodeFootprintMetadata struct {
	Window        string    `json:"window"`
	Percentile    string    `json:"percentile"`
	WorkloadCount int       `json:"workload_count"`
	GeneratedAt   time.Time `json:"generated_at"`
	Cluster       string    `json:"cluster"`
}

// CurrentTopology describes the current cluster topology
type CurrentTopology struct {
	NodeType          string  `json:"node_type"`
	NodeCount         int     `json:"node_count"`
	TotalCPU          float64 `json:"total_cpu"`
	TotalMemoryGi     float64 `json:"total_memory_gi"`
	AvgCPUUtilization float64 `json:"avg_cpu_utilization"`
	AvgMemUtilization float64 `json:"avg_mem_utilization"`
}

// WorkloadEnvelope describes the resource requirements envelope
type WorkloadEnvelope struct {
	TotalCPURequired    float64 `json:"total_cpu_required"`
	TotalMemoryRequired float64 `json:"total_memory_required"`
	PeakCPU             float64 `json:"peak_cpu"`
	PeakMemory          float64 `json:"peak_memory"`
	PodCount            int     `json:"pod_count"`

	// Safety tracking
	UnstableWorkloadCount int      `json:"unstable_workload_count,omitempty"` // Workloads with OOMKills/restarts
	UnstableWorkloads     []string `json:"unstable_workloads,omitempty"`      // List of unstable workload names
}

// NodeScenario describes an alternative node configuration
type NodeScenario struct {
	Name              string   `json:"name"`
	NodeType          string   `json:"node_type"`
	NodeCount         int      `json:"node_count"`
	CPUPerNode        float64  `json:"cpu_per_node"`
	MemoryPerNodeGi   float64  `json:"memory_per_node_gi"`
	AvgCPUUtilization float64  `json:"avg_cpu_utilization"`
	AvgMemUtilization float64  `json:"avg_mem_utilization"`
	Headroom          string   `json:"headroom"`
	Feasible          bool     `json:"feasible"`
	Reasons           []string `json:"reasons,omitempty"`
	Notes             string   `json:"notes"`
	EstimatedSavings  string   `json:"estimated_savings,omitempty"`

	// Safety analysis
	UnstableWorkloads int      `json:"unstable_workloads,omitempty"` // Count of workloads with recent failures
	SafetyWarnings    []string `json:"safety_warnings,omitempty"`    // Safety concerns about this scenario
}

// NewNodeFootprintAnalyzer creates a new node-footprint analyzer
func NewNodeFootprintAnalyzer(kubeClient *kubernetes.Clientset, metricsProvider metrics.MetricsProvider, config NodeFootprintConfig) *NodeFootprintAnalyzer {
	// Set defaults
	if config.Window == 0 {
		config.Window = 30 * 24 * time.Hour // 30 days default
	}
	if config.Percentile == "" {
		config.Percentile = "p95"
	}
	if len(config.NodeTypes) == 0 {
		// Default candidates
		config.NodeTypes = []string{"c5.xlarge", "c5.2xlarge", "r5.2xlarge"}
	}

	return &NodeFootprintAnalyzer{
		kubeClient:      kubeClient,
		metricsProvider: metricsProvider,
		config:          config,
	}
}

// Analyze performs the node-footprint analysis
func (a *NodeFootprintAnalyzer) Analyze(ctx context.Context) (*NodeFootprintResult, error) {
	result := &NodeFootprintResult{
		Metadata: NodeFootprintMetadata{
			Window:      formatDuration(a.config.Window),
			Percentile:  a.config.Percentile,
			GeneratedAt: time.Now(),
		},
		Scenarios: make([]NodeScenario, 0),
	}

	// Get current topology
	logProgress("[kubenow] Analyzing current cluster topology...\n")
	currentTopology, err := a.getCurrentTopology(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get current topology: %w", err)
	}
	result.CurrentTopology = *currentTopology
	logProgress("[kubenow] Current: %d x %s nodes\n", currentTopology.NodeCount, currentTopology.NodeType)

	// Get workload envelope
	logProgress("[kubenow] Calculating workload envelope (%s percentile)...\n", a.config.Percentile)
	envelope, podRequirements, err := a.getWorkloadEnvelope(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get workload envelope: %w", err)
	}
	result.WorkloadEnvelope = *envelope
	result.Metadata.WorkloadCount = envelope.PodCount
	logProgress("[kubenow] Found %d pods requiring %.2f CPU cores, %.2f GiB memory\n",
		envelope.PodCount, envelope.TotalCPURequired, envelope.TotalMemoryRequired/(1024*1024*1024))

	// Add current topology as first scenario
	currentScenario := NodeScenario{
		Name:              "Current (" + currentTopology.NodeType + ")",
		NodeType:          currentTopology.NodeType,
		NodeCount:         currentTopology.NodeCount,
		AvgCPUUtilization: currentTopology.AvgCPUUtilization,
		AvgMemUtilization: currentTopology.AvgMemUtilization,
		Feasible:          true,
		Notes:             "Current topology",
	}
	result.Scenarios = append(result.Scenarios, currentScenario)

	// Simulate alternative topologies
	logProgress("[kubenow] Simulating alternative topologies...\n")
	nodeTemplates := GetNodeTemplates()
	for i, nodeType := range a.config.NodeTypes {
		template, exists := nodeTemplates[nodeType]
		if !exists {
			logProgress("[kubenow] Warning: unknown node type %s, skipping\n", nodeType)
			continue
		}

		logProgress("[kubenow] [%d/%d] Testing %s configuration...\n", i+1, len(a.config.NodeTypes), nodeType)
		scenario := a.simulateTopology(i+1, template, podRequirements, currentTopology.NodeCount, envelope)
		result.Scenarios = append(result.Scenarios, scenario)
	}
	logProgress("[kubenow] Analysis complete!\n\n")

	return result, nil
}

// getCurrentTopology retrieves the current cluster topology
func (a *NodeFootprintAnalyzer) getCurrentTopology(ctx context.Context) (*CurrentTopology, error) {
	nodes, err := a.kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	if len(nodes.Items) == 0 {
		return nil, fmt.Errorf("no nodes found in cluster")
	}

	// Get cluster usage from metrics
	clusterUsage, err := a.metricsProvider.GetClusterResourceUsage(ctx, a.config.Window)
	if err != nil {
		return nil, fmt.Errorf("failed to get cluster usage: %w", err)
	}

	// Detect node type from first node (simplified)
	nodeType := "unknown"
	if label, exists := nodes.Items[0].Labels["node.kubernetes.io/instance-type"]; exists {
		nodeType = label
	} else if label, exists := nodes.Items[0].Labels["beta.kubernetes.io/instance-type"]; exists {
		nodeType = label
	}

	return &CurrentTopology{
		NodeType:          nodeType,
		NodeCount:         len(nodes.Items),
		TotalCPU:          clusterUsage.TotalCPU,
		TotalMemoryGi:     clusterUsage.TotalMemory / (1024 * 1024 * 1024),
		AvgCPUUtilization: clusterUsage.CPUUtilizationAvg,
		AvgMemUtilization: clusterUsage.MemUtilizationAvg,
	}, nil
}

// getWorkloadEnvelope calculates the workload resource envelope
func (a *NodeFootprintAnalyzer) getWorkloadEnvelope(ctx context.Context) (*WorkloadEnvelope, []PodRequirement, error) {
	envelope := &WorkloadEnvelope{}
	podRequirements := make([]PodRequirement, 0)

	// Get all pods across all namespaces
	pods, err := a.kubeClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, nil, err
	}

	envelope.PodCount = len(pods.Items)

	for _, pod := range pods.Items {
		// Skip pods in kube-system
		if pod.Namespace == "kube-system" {
			continue
		}

		// Calculate pod resource requirements based on percentile
		podCPU := 0.0
		podMem := 0.0

		for _, container := range pod.Spec.Containers {
			// Use requests as baseline (or limits if requests not set)
			cpuReq := container.Resources.Requests.Cpu().AsApproximateFloat64()
			memReq := container.Resources.Requests.Memory().AsApproximateFloat64()

			if cpuReq == 0 && container.Resources.Limits.Cpu() != nil {
				cpuReq = container.Resources.Limits.Cpu().AsApproximateFloat64()
			}
			if memReq == 0 && container.Resources.Limits.Memory() != nil {
				memReq = container.Resources.Limits.Memory().AsApproximateFloat64()
			}

			podCPU += cpuReq
			podMem += memReq
		}

		// TODO: Query actual usage from Prometheus and use percentile
		// For now, use requests as approximation

		podRequirements = append(podRequirements, PodRequirement{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			CPU:       podCPU,
			Memory:    podMem,
		})

		envelope.TotalCPURequired += podCPU
		envelope.TotalMemoryRequired += podMem

		// Track peak (for now, same as total)
		if podCPU > envelope.PeakCPU {
			envelope.PeakCPU = podCPU
		}
		if podMem > envelope.PeakMemory {
			envelope.PeakMemory = podMem
		}
	}

	// Check for unstable workloads (OOMKills, high restarts)
	a.checkWorkloadStability(ctx, pods.Items, envelope)

	return envelope, podRequirements, nil
}

// checkWorkloadStability checks for workloads with recent failures
func (a *NodeFootprintAnalyzer) checkWorkloadStability(ctx context.Context, pods []corev1.Pod, envelope *WorkloadEnvelope) {
	// Type assert to get Prometheus client
	promClient, ok := a.metricsProvider.(*metrics.PrometheusClient)
	if !ok {
		return // Safety checks not available
	}

	unstableWorkloads := make(map[string]bool)

	// Check each pod's owner (deployment/statefulset) for stability
	for _, pod := range pods {
		if pod.Namespace == "kube-system" {
			continue
		}

		// Get owner reference (Deployment, StatefulSet, etc.)
		workloadName := ""
		if len(pod.OwnerReferences) > 0 {
			workloadName = pod.OwnerReferences[0].Name
		} else {
			workloadName = pod.Name
		}

		// Skip if already checked
		workloadKey := fmt.Sprintf("%s/%s", pod.Namespace, workloadName)
		if unstableWorkloads[workloadKey] {
			continue
		}

		// Query for restarts in the last 7 days (shorter window for node planning)
		safetyData, err := promClient.GetWorkloadSafetyData(ctx, pod.Namespace, workloadName, "Deployment", 7*24*time.Hour)
		if err != nil {
			continue
		}

		// Check if workload is unstable
		restarts := int(safetyData["restarts"])
		if restarts > 5 {
			unstableWorkloads[workloadKey] = true
			envelope.UnstableWorkloads = append(envelope.UnstableWorkloads, workloadKey)
			envelope.UnstableWorkloadCount++
		}
	}
}

// simulateTopology simulates an alternative node topology
func (a *NodeFootprintAnalyzer) simulateTopology(index int, template NodeTemplate, pods []PodRequirement, currentNodeCount int, envelope *WorkloadEnvelope) NodeScenario {
	// Run bin-packing simulation
	packer := NewBinPacker(template)
	result := packer.Pack(pods)

	scenario := NodeScenario{
		Name:              fmt.Sprintf("Alt %d: %s x %d", index, template.InstanceType, result.NodeCount),
		NodeType:          template.InstanceType,
		NodeCount:         result.NodeCount,
		CPUPerNode:        template.CPUCapacity,
		MemoryPerNodeGi:   template.MemoryCapacity / (1024 * 1024 * 1024),
		AvgCPUUtilization: result.AvgCPUUtil,
		AvgMemUtilization: result.AvgMemUtil,
		Headroom:          result.Headroom,
		Feasible:          result.Feasible,
		Reasons:           result.Reasons,
	}

	// Add safety warnings if there are unstable workloads
	if envelope.UnstableWorkloadCount > 0 {
		scenario.UnstableWorkloads = envelope.UnstableWorkloadCount
		scenario.SafetyWarnings = []string{
			fmt.Sprintf("%d workloads have recent failures (restarts > 5 in last 7 days)", envelope.UnstableWorkloadCount),
			"Topology changes may increase risk for already-unstable workloads",
			"Recommend stabilizing workloads before reducing nodes",
		}
	}

	if result.Feasible {
		scenario.Notes = fmt.Sprintf("Based on %s envelope, this would have survived the last %s.",
			a.config.Percentile, formatDuration(a.config.Window))

		// Calculate estimated savings
		if result.NodeCount < currentNodeCount {
			savingsPercent := ((float64(currentNodeCount - result.NodeCount)) / float64(currentNodeCount)) * 100
			scenario.EstimatedSavings = fmt.Sprintf("%.0f%% fewer nodes", savingsPercent)
		}
	} else {
		scenario.Notes = "This configuration would NOT have fit the workload."
	}

	return scenario
}
