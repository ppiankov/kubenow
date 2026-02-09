package metrics

import "strings"

// workloadNameLabels is the priority-ordered list of labels to check
// for workload name resolution.
var workloadNameLabels = []string{
	"app.kubernetes.io/name",
	"app.kubernetes.io/instance",
	"app",
	"cnpg.io/cluster",
}

// ResolveWorkloadName determines the workload name from pod labels.
// Falls back to the dash-stripping heuristic if no label matches.
func ResolveWorkloadName(podName string, labels map[string]string) string {
	for _, key := range workloadNameLabels {
		if val, ok := labels[key]; ok && val != "" {
			return val
		}
	}
	return extractWorkloadNameHeuristic(podName)
}

// extractWorkloadNameHeuristic strips the last two dash-separated segments.
// e.g., "payment-api-7d8f9c4b6-abc12" -> "payment-api"
func extractWorkloadNameHeuristic(podName string) string {
	parts := strings.Split(podName, "-")
	if len(parts) <= 2 {
		return podName
	}
	return strings.Join(parts[:len(parts)-2], "-")
}
