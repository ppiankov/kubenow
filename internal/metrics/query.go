package metrics

import (
	"fmt"
	"strings"
	"time"
)

// Workload type constants used in PromQL query construction
const (
	WorkloadTypeStatefulSet = "StatefulSet"
	WorkloadTypePod         = "Pod"
)

// QueryBuilder constructs PromQL queries for common metrics
type QueryBuilder struct{}

// NewQueryBuilder creates a new query builder
func NewQueryBuilder() *QueryBuilder {
	return &QueryBuilder{}
}

// CPUUsageByNamespace returns a query for CPU usage by namespace
func (qb *QueryBuilder) CPUUsageByNamespace(namespace string) string {
	return `sum(rate(container_cpu_usage_seconds_total{namespace=` + escapeLabel(namespace) + `,container!="",container!="POD"}[5m])) by (namespace)`
}

// CPUUsageByPod returns a query for CPU usage by pod
func (qb *QueryBuilder) CPUUsageByPod(namespace, podPattern string) string {
	return `sum(rate(container_cpu_usage_seconds_total{namespace=` + escapeLabel(namespace) + `,pod=~` + escapeLabel(podPattern) + `,container!="",container!="POD"}[5m])) by (pod)`
}

// MemoryUsageByNamespace returns a query for memory usage by namespace
func (qb *QueryBuilder) MemoryUsageByNamespace(namespace string) string {
	return `sum(container_memory_working_set_bytes{namespace=` + escapeLabel(namespace) + `,container!="",container!="POD"}) by (namespace)`
}

// MemoryUsageByPod returns a query for memory usage by pod
func (qb *QueryBuilder) MemoryUsageByPod(namespace, podPattern string) string {
	return `sum(container_memory_working_set_bytes{namespace=` + escapeLabel(namespace) + `,pod=~` + escapeLabel(podPattern) + `,container!="",container!="POD"}) by (pod)`
}

// CPUAvgOverTime returns a query for average CPU usage over a time window
func (qb *QueryBuilder) CPUAvgOverTime(namespace string, window time.Duration) string {
	return `avg_over_time(sum(rate(container_cpu_usage_seconds_total{namespace=` + escapeLabel(namespace) + `,container!="",container!="POD"}[5m]))[` + formatDuration(window) + `:])`
}

// MemoryAvgOverTime returns a query for average memory usage over a time window
func (qb *QueryBuilder) MemoryAvgOverTime(namespace string, window time.Duration) string {
	return `avg_over_time(sum(container_memory_working_set_bytes{namespace=` + escapeLabel(namespace) + `,container!="",container!="POD"})[` + formatDuration(window) + `:])`
}

// CPUQuantileOverTime returns a query for CPU usage at a specific percentile
func (qb *QueryBuilder) CPUQuantileOverTime(namespace string, percentile float64, window time.Duration) string {
	return fmt.Sprintf(`quantile_over_time(%.2f, sum(rate(container_cpu_usage_seconds_total{namespace=`+escapeLabel(namespace)+`,container!="",container!="POD"}[5m]))[`+formatDuration(window)+`:])`, percentile)
}

// MemoryQuantileOverTime returns a query for memory usage at a specific percentile
func (qb *QueryBuilder) MemoryQuantileOverTime(namespace string, percentile float64, window time.Duration) string {
	return fmt.Sprintf(`quantile_over_time(%.2f, sum(container_memory_working_set_bytes{namespace=`+escapeLabel(namespace)+`,container!="",container!="POD"})[`+formatDuration(window)+`:])`, percentile)
}

// CPURequestsByNamespace returns a query for CPU requests by namespace
func (qb *QueryBuilder) CPURequestsByNamespace(namespace string) string {
	return `sum(kube_pod_container_resource_requests{namespace=` + escapeLabel(namespace) + `,resource="cpu"}) by (namespace)`
}

// MemoryRequestsByNamespace returns a query for memory requests by namespace
func (qb *QueryBuilder) MemoryRequestsByNamespace(namespace string) string {
	return `sum(kube_pod_container_resource_requests{namespace=` + escapeLabel(namespace) + `,resource="memory"}) by (namespace)`
}

// CPURequestsByPod returns a query for CPU requests by pod
func (qb *QueryBuilder) CPURequestsByPod(namespace, podPattern string) string {
	return `sum(kube_pod_container_resource_requests{namespace=` + escapeLabel(namespace) + `,pod=~` + escapeLabel(podPattern) + `,resource="cpu"}) by (pod)`
}

// MemoryRequestsByPod returns a query for memory requests by pod
func (qb *QueryBuilder) MemoryRequestsByPod(namespace, podPattern string) string {
	return `sum(kube_pod_container_resource_requests{namespace=` + escapeLabel(namespace) + `,pod=~` + escapeLabel(podPattern) + `,resource="memory"}) by (pod)`
}

// NodeCPUCapacity returns a query for total node CPU capacity
func (qb *QueryBuilder) NodeCPUCapacity() string {
	return `sum(kube_node_status_capacity{resource="cpu"})`
}

// NodeMemoryCapacity returns a query for total node memory capacity
func (qb *QueryBuilder) NodeMemoryCapacity() string {
	return `sum(kube_node_status_capacity{resource="memory"})`
}

// workloadPodPattern returns a regex pattern for matching pods belonging to a workload
func workloadPodPattern(workloadName, workloadType string) string {
	switch workloadType {
	case WorkloadTypeStatefulSet:
		return workloadName + "-[0-9]+"
	case WorkloadTypePod:
		return workloadName
	default:
		// Deployment, DaemonSet, and others use replicaset-hash suffix
		return workloadName + "-.*"
	}
}

// WorkloadCPURequests returns a query for total CPU requests across all pods of a workload
func (qb *QueryBuilder) WorkloadCPURequests(namespace, workloadName, workloadType string) string {
	pattern := workloadPodPattern(workloadName, workloadType)
	return `sum(kube_pod_container_resource_requests{namespace=` + escapeLabel(namespace) + `,pod=~` + escapeLabel(pattern) + `,resource="cpu"})`
}

// WorkloadMemoryRequests returns a query for total memory requests across all pods of a workload
func (qb *QueryBuilder) WorkloadMemoryRequests(namespace, workloadName, workloadType string) string {
	pattern := workloadPodPattern(workloadName, workloadType)
	return `sum(kube_pod_container_resource_requests{namespace=` + escapeLabel(namespace) + `,pod=~` + escapeLabel(pattern) + `,resource="memory"})`
}

// WorkloadCPULimits returns a query for total CPU limits across all pods of a workload
func (qb *QueryBuilder) WorkloadCPULimits(namespace, workloadName, workloadType string) string {
	pattern := workloadPodPattern(workloadName, workloadType)
	return `sum(kube_pod_container_resource_limits{namespace=` + escapeLabel(namespace) + `,pod=~` + escapeLabel(pattern) + `,resource="cpu"})`
}

// WorkloadMemoryLimits returns a query for total memory limits across all pods of a workload
func (qb *QueryBuilder) WorkloadMemoryLimits(namespace, workloadName, workloadType string) string {
	pattern := workloadPodPattern(workloadName, workloadType)
	return `sum(kube_pod_container_resource_limits{namespace=` + escapeLabel(namespace) + `,pod=~` + escapeLabel(pattern) + `,resource="memory"})`
}

// escapeLabel escapes a string for use in a PromQL label equality matcher (=).
// Escapes backslashes, double quotes, and newlines.
func escapeLabel(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}

// escapeRegex escapes a string for safe embedding in a PromQL regex matcher (=~).
// The workload/namespace part is escaped for regex metacharacters; the pattern
// suffix (e.g., "-.*", "-[0-9]+") is appended after escaping.
func escapeRegex(name, patternSuffix string) string {
	// Escape regex metacharacters in the name portion
	metacharReplacer := strings.NewReplacer(
		`\`, `\\`,
		`.`, `\.`,
		`*`, `\*`,
		`+`, `\+`,
		`?`, `\?`,
		`(`, `\(`,
		`)`, `\)`,
		`[`, `\[`,
		`]`, `\]`,
		`{`, `\{`,
		`}`, `\}`,
		`|`, `\|`,
		`^`, `\^`,
		`$`, `\$`,
		`"`, `\"`,
		"\n", `\n`,
	)
	escaped := metacharReplacer.Replace(name)
	return `"` + escaped + patternSuffix + `"`
}

// NodeCount returns a query for the number of nodes
func (qb *QueryBuilder) NodeCount() string {
	return `count(kube_node_info)`
}

// PodStartTime returns a query for pod start time
func (qb *QueryBuilder) PodStartTime(namespace, podName string) string {
	return `kube_pod_start_time{namespace=` + escapeLabel(namespace) + `,pod=` + escapeLabel(podName) + `}`
}

// WorkloadCPUUsage returns a query for workload CPU usage (aggregated by deployment/statefulset)
func (qb *QueryBuilder) WorkloadCPUUsage(namespace, workloadName, workloadType string) string {
	ns := escapeLabel(namespace)
	switch workloadType {
	case "Deployment":
		return `sum(rate(container_cpu_usage_seconds_total{namespace=` + ns + `,pod=~` + escapeRegex(workloadName, "-.*") + `,container!="",container!="POD"}[5m]))`
	case "StatefulSet":
		return `sum(rate(container_cpu_usage_seconds_total{namespace=` + ns + `,pod=~` + escapeRegex(workloadName, "-[0-9]+") + `,container!="",container!="POD"}[5m]))`
	case "DaemonSet":
		return `sum(rate(container_cpu_usage_seconds_total{namespace=` + ns + `,pod=~` + escapeRegex(workloadName, "-.*") + `,container!="",container!="POD"}[5m]))`
	case "Pod":
		return `sum(rate(container_cpu_usage_seconds_total{namespace=` + ns + `,pod=` + escapeLabel(workloadName) + `,container!="",container!="POD"}[5m]))`
	default:
		return `sum(rate(container_cpu_usage_seconds_total{namespace=` + ns + `,pod=~` + escapeRegex(workloadName, ".*") + `,container!="",container!="POD"}[5m]))`
	}
}

// WorkloadMemoryUsage returns a query for workload memory usage
func (qb *QueryBuilder) WorkloadMemoryUsage(namespace, workloadName, workloadType string) string {
	ns := escapeLabel(namespace)
	switch workloadType {
	case "Deployment":
		return `sum(container_memory_working_set_bytes{namespace=` + ns + `,pod=~` + escapeRegex(workloadName, "-.*") + `,container!="",container!="POD"})`
	case "StatefulSet":
		return `sum(container_memory_working_set_bytes{namespace=` + ns + `,pod=~` + escapeRegex(workloadName, "-[0-9]+") + `,container!="",container!="POD"})`
	case "DaemonSet":
		return `sum(container_memory_working_set_bytes{namespace=` + ns + `,pod=~` + escapeRegex(workloadName, "-.*") + `,container!="",container!="POD"})`
	case "Pod":
		return `sum(container_memory_working_set_bytes{namespace=` + ns + `,pod=` + escapeLabel(workloadName) + `,container!="",container!="POD"})`
	default:
		return `sum(container_memory_working_set_bytes{namespace=` + ns + `,pod=~` + escapeRegex(workloadName, ".*") + `,container!="",container!="POD"})`
	}
}

// formatDuration converts a Go duration to Prometheus duration format
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

// ParseDuration parses a duration string (supports d, h, m, s)
// maxDurationDays is the upper bound for parsed durations (1 year).
const maxDurationDays = 365

func ParseDuration(s string) (time.Duration, error) {
	// Handle Prometheus-style durations like "30d", "7d", "24h"
	if len(s) < 2 {
		return 0, fmt.Errorf("invalid duration: %s", s)
	}

	unit := s[len(s)-1]
	valueStr := s[:len(s)-1]

	var value int
	if _, err := fmt.Sscanf(valueStr, "%d", &value); err != nil {
		return 0, fmt.Errorf("invalid duration value: %s", s)
	}

	if value < 0 {
		return 0, fmt.Errorf("negative duration not allowed: %s", s)
	}

	var d time.Duration
	switch unit {
	case 'd':
		d = time.Duration(value) * 24 * time.Hour
	case 'h':
		d = time.Duration(value) * time.Hour
	case 'm':
		d = time.Duration(value) * time.Minute
	case 's':
		d = time.Duration(value) * time.Second
	case 'w':
		d = time.Duration(value) * 7 * 24 * time.Hour
	default:
		// Try standard Go duration parsing as fallback
		var err error
		d, err = time.ParseDuration(s)
		if err != nil {
			return 0, err
		}
	}

	maxDuration := time.Duration(maxDurationDays) * 24 * time.Hour
	if d > maxDuration {
		return 0, fmt.Errorf("duration %s exceeds maximum (%dd)", s, maxDurationDays)
	}

	return d, nil
}

// === Safety Analysis Queries ===

// OOMKillsByWorkload returns a query for OOM kills for a workload over time window
func (qb *QueryBuilder) OOMKillsByWorkload(namespace, workloadName string, window time.Duration) string {
	return `sum(increase(kube_pod_container_status_restarts_total{namespace=` + escapeLabel(namespace) + `,pod=~` + escapeRegex(workloadName, ".*") + `}[` + formatDuration(window) + `])) by (pod)`
}

// RestartsByWorkload returns a query for total container restarts for a workload
func (qb *QueryBuilder) RestartsByWorkload(namespace, workloadName string, window time.Duration) string {
	return `sum(increase(kube_pod_container_status_restarts_total{namespace=` + escapeLabel(namespace) + `,pod=~` + escapeRegex(workloadName, ".*") + `}[` + formatDuration(window) + `]))`
}

// CPUThrottledByWorkload returns a query for CPU throttling time for a workload
func (qb *QueryBuilder) CPUThrottledByWorkload(namespace, workloadName string, window time.Duration) string {
	return `sum(increase(container_cpu_cfs_throttled_seconds_total{namespace=` + escapeLabel(namespace) + `,pod=~` + escapeRegex(workloadName, ".*") + `,container!="",container!="POD"}[` + formatDuration(window) + `]))`
}

// CPUThrottledPercentByWorkload returns CPU throttling as percentage of time window
func (qb *QueryBuilder) CPUThrottledPercentByWorkload(namespace, workloadName string, window time.Duration) string {
	windowSeconds := window.Seconds()
	return fmt.Sprintf(`(sum(increase(container_cpu_cfs_throttled_seconds_total{namespace=`+escapeLabel(namespace)+`,pod=~`+escapeRegex(workloadName, ".*")+`,container!="",container!="POD"}[`+formatDuration(window)+`])) / %f) * 100`, windowSeconds)
}

// MaxCPUUsageByWorkload returns max CPU usage for a workload in time window
func (qb *QueryBuilder) MaxCPUUsageByWorkload(namespace, workloadName, workloadType string, window time.Duration) string {
	baseQuery := qb.WorkloadCPUUsage(namespace, workloadName, workloadType)
	return fmt.Sprintf(`max_over_time((%s)[%s:])`, baseQuery, formatDuration(window))
}

// MaxMemoryUsageByWorkload returns max memory usage for a workload in time window
func (qb *QueryBuilder) MaxMemoryUsageByWorkload(namespace, workloadName, workloadType string, window time.Duration) string {
	baseQuery := qb.WorkloadMemoryUsage(namespace, workloadName, workloadType)
	return fmt.Sprintf(`max_over_time((%s)[%s:])`, baseQuery, formatDuration(window))
}

// CPUP999ByWorkload returns 99.9th percentile CPU usage for a workload
func (qb *QueryBuilder) CPUP999ByWorkload(namespace, workloadName, workloadType string, window time.Duration) string {
	baseQuery := qb.WorkloadCPUUsage(namespace, workloadName, workloadType)
	return fmt.Sprintf(`quantile_over_time(0.999, (%s)[%s:])`, baseQuery, formatDuration(window))
}

// MemoryP999ByWorkload returns 99.9th percentile memory usage for a workload
func (qb *QueryBuilder) MemoryP999ByWorkload(namespace, workloadName, workloadType string, window time.Duration) string {
	baseQuery := qb.WorkloadMemoryUsage(namespace, workloadName, workloadType)
	return fmt.Sprintf(`quantile_over_time(0.999, (%s)[%s:])`, baseQuery, formatDuration(window))
}

// PodStatusByWorkload returns current pod status for a workload
func (qb *QueryBuilder) PodStatusByWorkload(namespace, workloadName string) string {
	return `kube_pod_status_phase{namespace=` + escapeLabel(namespace) + `,pod=~` + escapeRegex(workloadName, ".*") + `}`
}

// LastTerminatedReasonByWorkload returns the last container termination reason
func (qb *QueryBuilder) LastTerminatedReasonByWorkload(namespace, workloadName string) string {
	return `kube_pod_container_status_last_terminated_reason{namespace=` + escapeLabel(namespace) + `,pod=~` + escapeRegex(workloadName, ".*") + `}`
}
