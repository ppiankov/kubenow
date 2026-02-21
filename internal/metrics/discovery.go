package metrics

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

// MetricDiscovery detects available metrics in Prometheus
type MetricDiscovery struct {
	api v1.API
}

// AvailableMetrics contains discovered metric information
type AvailableMetrics struct {
	CPUMetric    string   // Best available CPU metric
	MemoryMetric string   // Best available memory metric
	AllCPU       []string // All CPU metrics found
	AllMemory    []string // All memory metrics found
}

// NewMetricDiscovery creates a new metric discovery client
func NewMetricDiscovery(api v1.API) *MetricDiscovery {
	return &MetricDiscovery{api: api}
}

// DiscoverMetrics auto-detects available container metrics
func (d *MetricDiscovery) DiscoverMetrics(ctx context.Context) (*AvailableMetrics, error) {
	// Get all metric names from Prometheus
	labels, _, err := d.api.LabelValues(ctx, "__name__", nil, time.Now().Add(-1*time.Hour), time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to query metric names: %w", err)
	}

	allMetrics := make([]string, 0, len(labels))
	for _, label := range labels {
		allMetrics = append(allMetrics, string(label))
	}

	// Find CPU metrics
	cpuMetrics := findMetrics(allMetrics, []string{
		"container_cpu_usage_seconds_total",    // cAdvisor standard
		"container_cpu_usage",                  // Alternative naming
		"kubelet_container_cpu_usage_seconds",  // Kubelet metrics
		"kube_pod_container_resource_requests", // kube-state-metrics fallback
	})

	// Find memory metrics
	memoryMetrics := findMetrics(allMetrics, []string{
		"container_memory_working_set_bytes",         // cAdvisor standard
		"container_memory_usage_bytes",               // Alternative
		"kubelet_container_memory_working_set_bytes", // Kubelet metrics
		"kube_pod_container_resource_requests",       // kube-state-metrics fallback
	})

	result := &AvailableMetrics{
		AllCPU:    cpuMetrics,
		AllMemory: memoryMetrics,
	}

	// Pick best available metrics
	if len(cpuMetrics) > 0 {
		result.CPUMetric = cpuMetrics[0]
	}
	if len(memoryMetrics) > 0 {
		result.MemoryMetric = memoryMetrics[0]
	}

	return result, nil
}

// findMetrics finds all metrics matching any of the patterns
func findMetrics(allMetrics []string, patterns []string) []string {
	found := make(map[string]bool)
	result := make([]string, 0)

	// Filter out recording rules (metrics with colons are typically aggregated recording rules)
	rawMetrics := make([]string, 0, len(allMetrics))
	for _, metric := range allMetrics {
		// Skip recording rules (contain colons like cluster:namespace:pod_cpu:active:...)
		if !strings.Contains(metric, ":") {
			rawMetrics = append(rawMetrics, metric)
		}
	}

	// Exact matches first (highest priority)
	for _, pattern := range patterns {
		for _, metric := range rawMetrics {
			if metric == pattern && !found[metric] {
				found[metric] = true
				result = append(result, metric)
			}
		}
	}

	// Pattern matches (contains) - only if no exact matches found
	if len(result) == 0 {
		for _, pattern := range patterns {
			for _, metric := range rawMetrics {
				if strings.Contains(metric, pattern) && !found[metric] {
					found[metric] = true
					result = append(result, metric)
				}
			}
		}
	}

	sort.Strings(result)
	return result
}

// ValidateMetrics checks if required metrics are available
func (m *AvailableMetrics) ValidateMetrics() error {
	if m.CPUMetric == "" {
		return fmt.Errorf("no CPU usage metrics found in Prometheus (tried: container_cpu_usage_seconds_total, container_cpu_usage, etc.)")
	}
	if m.MemoryMetric == "" {
		return fmt.Errorf("no memory usage metrics found in Prometheus (tried: container_memory_working_set_bytes, container_memory_usage_bytes, etc.)")
	}
	return nil
}

// GetCPUQuery builds a CPU query with the best available metric
func (m *AvailableMetrics) GetCPUQuery(namespace, workload, workloadType string) string {
	ns := escapeLabel(namespace)
	pod := escapeRegex(workload, "-.*")
	switch {
	case strings.Contains(m.CPUMetric, "usage_seconds_total"):
		return `rate(` + m.CPUMetric + `{namespace=` + ns + `,pod=~` + pod + `}[5m])`
	case strings.Contains(m.CPUMetric, "resource_requests"):
		return `kube_pod_container_resource_requests{namespace=` + ns + `,pod=~` + pod + `,resource="cpu"}`
	default:
		return m.CPUMetric + `{namespace=` + ns + `,pod=~` + pod + `}`
	}
}

// GetMemoryQuery builds a memory query with the best available metric
func (m *AvailableMetrics) GetMemoryQuery(namespace, workload, workloadType string) string {
	ns := escapeLabel(namespace)
	pod := escapeRegex(workload, "-.*")
	switch {
	case strings.Contains(m.MemoryMetric, "resource_requests"):
		return `kube_pod_container_resource_requests{namespace=` + ns + `,pod=~` + pod + `,resource="memory"}`
	default:
		return m.MemoryMetric + `{namespace=` + ns + `,pod=~` + pod + `}`
	}
}

// DiagnosticInfo returns human-readable diagnostic information
func (m *AvailableMetrics) DiagnosticInfo() string {
	var info strings.Builder

	info.WriteString("Discovered Metrics:\n")
	info.WriteString(fmt.Sprintf("  CPU Metric: %s\n", m.CPUMetric))
	if len(m.AllCPU) > 1 {
		info.WriteString(fmt.Sprintf("  Other CPU metrics available: %v\n", m.AllCPU[1:]))
	}
	info.WriteString(fmt.Sprintf("  Memory Metric: %s\n", m.MemoryMetric))
	if len(m.AllMemory) > 1 {
		info.WriteString(fmt.Sprintf("  Other memory metrics available: %v\n", m.AllMemory[1:]))
	}

	return info.String()
}
