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

// operatorLabels maps pod label keys to operator type names.
// When a pod carries one of these labels, ResolveWorkloadIdentity
// returns the corresponding operator type string.
var operatorLabels = map[string]string{
	"cnpg.io/cluster":               "CNPG",
	"strimzi.io/cluster":            "Strimzi",
	"rabbitmq.com/cluster-operator": "RabbitMQ",
	"redis.redis.opstreelabs.in":    "Redis",
	"elasticsearch.k8s.elastic.co":  "Elasticsearch",
}

// managedByOperators maps known managed-by values to operator type names.
var managedByOperators = map[string]string{
	"cloudnative-pg":            "CNPG",
	"strimzi-cluster-operator":  "Strimzi",
	"rabbitmq-cluster-operator": "RabbitMQ",
}

// ResolveWorkloadIdentity determines both the workload name and the
// CRD operator type from pod labels. Returns empty operatorType for
// standard Deployment/StatefulSet/DaemonSet workloads.
func ResolveWorkloadIdentity(podName string, labels map[string]string) (name, operatorType string) {
	name = ResolveWorkloadName(podName, labels)
	operatorType = detectOperatorType(labels)
	return name, operatorType
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

// detectOperatorType checks pod labels for known CRD operator markers.
func detectOperatorType(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	// Check operator-specific labels first (most precise)
	for key, op := range operatorLabels {
		if _, ok := labels[key]; ok {
			return op
		}
	}
	// Fallback: app.kubernetes.io/managed-by
	if managedBy, ok := labels["app.kubernetes.io/managed-by"]; ok {
		if op, ok := managedByOperators[managedBy]; ok {
			return op
		}
	}
	return ""
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
