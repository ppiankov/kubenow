package analyzer

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/ppiankov/kubenow/internal/metrics"
)

// standardOwnerKinds are controller kinds handled by the normal workload discovery loops.
// Pods owned by these are skipped during CRD discovery.
var standardOwnerKinds = map[string]bool{
	"ReplicaSet":  true,
	"StatefulSet": true,
	"DaemonSet":   true,
	"Job":         true,
	"Node":        true,
}

// crdWorkloadGroup represents a group of CRD-managed pods sharing a workload identity.
type crdWorkloadGroup struct {
	workloadName string
	displayType  string    // operator name for user-facing output (e.g. "CNPG")
	promqlType   string    // workload type for PromQL pattern selection
	creationTime time.Time // oldest pod creation timestamp in the group
	podCount     int
}

// discoverCRDWorkloads lists all pods in a namespace and identifies those managed by
// known CRD operators (CNPG, Strimzi, RabbitMQ, etc.) that are not already discovered
// by the standard Deployment/StatefulSet/DaemonSet loops.
func (a *RequestsSkewAnalyzer) discoverCRDWorkloads(ctx context.Context, namespace string, knownWorkloads map[string]bool) ([]crdWorkloadGroup, error) {
	pods, err := a.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// Group pods by resolved workload name
	type groupState struct {
		displayType  string
		creationTime time.Time
		podCount     int
	}
	groups := make(map[string]*groupState)

	for i := range pods.Items {
		pod := &pods.Items[i]
		// Skip standalone pods (no ownerReferences)
		if len(pod.OwnerReferences) == 0 {
			continue
		}

		// Skip pods owned by standard controllers
		ownerKind := pod.OwnerReferences[0].Kind
		if standardOwnerKinds[ownerKind] {
			continue
		}

		// Resolve workload identity via operator labels
		name, operatorType := metrics.ResolveWorkloadIdentity(pod.Name, pod.Labels)
		if operatorType == "" {
			// Unknown custom controller — skip to prevent false positives
			continue
		}

		// Skip workloads already discovered by standard loops
		if knownWorkloads[name] {
			continue
		}

		// Add to group
		g, exists := groups[name]
		if !exists {
			g = &groupState{
				displayType:  operatorType,
				creationTime: pod.CreationTimestamp.Time,
			}
			groups[name] = g
		}
		g.podCount++
		// Track oldest creation timestamp
		if pod.CreationTimestamp.Time.Before(g.creationTime) {
			g.creationTime = pod.CreationTimestamp.Time
		}
	}

	// Convert map to slice
	result := make([]crdWorkloadGroup, 0, len(groups))
	for name, g := range groups {
		result = append(result, crdWorkloadGroup{
			workloadName: name,
			displayType:  g.displayType,
			promqlType:   "StatefulSet", // CRD operators use ordinal pod naming
			creationTime: g.creationTime,
			podCount:     g.podCount,
		})
	}

	return result, nil
}
