package promonitor

import (
	"context"
	"fmt"
	"strings"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Canonical kind constants used across the promonitor package.
const (
	KindDeployment  = "Deployment"
	KindStatefulSet = "StatefulSet"
	KindDaemonSet   = "DaemonSet"
	KindPod         = "Pod"
)

// WorkloadRef is a parsed kind/name reference.
type WorkloadRef struct {
	Kind      string // Deployment, StatefulSet, DaemonSet, Pod
	Name      string
	Namespace string
}

// String returns the canonical kind/name form.
func (w WorkloadRef) String() string {
	return fmt.Sprintf("%s/%s", strings.ToLower(w.Kind), w.Name)
}

// FullString returns namespace/kind/name.
func (w WorkloadRef) FullString() string {
	return fmt.Sprintf("%s/%s/%s", w.Namespace, strings.ToLower(w.Kind), w.Name)
}

// ParseWorkloadRef parses a "kind/name" string into a WorkloadRef.
// Accepted kinds: deployment, statefulset, daemonset, pod (case-insensitive).
func ParseWorkloadRef(ref string) (*WorkloadRef, error) {
	parts := strings.SplitN(ref, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid workload ref %q: expected <kind>/<name> (e.g., deployment/payment-api)", ref)
	}

	kind, err := normalizeKind(parts[0])
	if err != nil {
		return nil, err
	}

	return &WorkloadRef{
		Kind: kind,
		Name: parts[1],
	}, nil
}

// normalizeKind maps user input to canonical Kubernetes kind.
func normalizeKind(input string) (string, error) {
	switch strings.ToLower(input) {
	case "deployment", "deploy", "deployments":
		return KindDeployment, nil
	case "statefulset", "sts", "statefulsets":
		return KindStatefulSet, nil
	case "daemonset", "ds", "daemonsets":
		return KindDaemonSet, nil
	case "pod", "pods", "po":
		return KindPod, nil
	default:
		return "", fmt.Errorf("unsupported workload kind %q: must be deployment, statefulset, daemonset, or pod", input)
	}
}

// ValidateWorkload checks that the workload exists in the cluster.
func ValidateWorkload(ctx context.Context, client *kubernetes.Clientset, ref *WorkloadRef) error {
	switch ref.Kind {
	case KindDeployment:
		_, err := client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("deployment %q not found in namespace %q: %w", ref.Name, ref.Namespace, err)
		}
	case KindStatefulSet:
		_, err := client.AppsV1().StatefulSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("statefulset %q not found in namespace %q: %w", ref.Name, ref.Namespace, err)
		}
	case KindDaemonSet:
		_, err := client.AppsV1().DaemonSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("daemonset %q not found in namespace %q: %w", ref.Name, ref.Namespace, err)
		}
	case KindPod:
		_, err := client.CoreV1().Pods(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("pod %q not found in namespace %q: %w", ref.Name, ref.Namespace, err)
		}
	default:
		return fmt.Errorf("unsupported kind: %s", ref.Kind)
	}
	return nil
}

// CheckMetricsServer verifies that the metrics-server is available.
func CheckMetricsServer(ctx context.Context, metricsClient *metricsclientset.Clientset, namespace string) error {
	_, err := metricsClient.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{Limit: 1})
	if err != nil {
		return fmt.Errorf("metrics-server not available: %w", err)
	}
	return nil
}

// HPAInfo holds information about an HPA targeting a workload.
type HPAInfo struct {
	Name       string
	MinReplica int32
	MaxReplica int32
}

// DetectHPA checks if any HPA targets the given workload.
// Returns nil if no HPA is found or if the HPA API is unavailable.
func DetectHPA(ctx context.Context, client *kubernetes.Clientset, ref *WorkloadRef) *HPAInfo {
	hpas, err := client.AutoscalingV2().HorizontalPodAutoscalers(ref.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil // HPA API may not be available; not a fatal error
	}

	for _, hpa := range hpas.Items {
		if matchesHPATarget(hpa, ref) {
			info := &HPAInfo{
				Name:       hpa.Name,
				MaxReplica: hpa.Spec.MaxReplicas,
			}
			if hpa.Spec.MinReplicas != nil {
				info.MinReplica = *hpa.Spec.MinReplicas
			}
			return info
		}
	}
	return nil
}

func matchesHPATarget(hpa autoscalingv2.HorizontalPodAutoscaler, ref *WorkloadRef) bool {
	target := hpa.Spec.ScaleTargetRef
	return target.Name == ref.Name && target.Kind == ref.Kind
}

// FetchContainerResources reads the current resource values from the
// workload's pod template spec.
func FetchContainerResources(ctx context.Context, client *kubernetes.Clientset, ref *WorkloadRef) ([]ContainerResources, error) {
	switch ref.Kind {
	case KindDeployment:
		obj, err := client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("cannot read deployment: %w", err)
		}
		return extractContainerResources(obj.Spec.Template.Spec.Containers), nil
	case KindStatefulSet:
		obj, err := client.AppsV1().StatefulSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("cannot read statefulset: %w", err)
		}
		return extractContainerResources(obj.Spec.Template.Spec.Containers), nil
	case KindDaemonSet:
		obj, err := client.AppsV1().DaemonSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("cannot read daemonset: %w", err)
		}
		return extractContainerResources(obj.Spec.Template.Spec.Containers), nil
	case KindPod:
		obj, err := client.CoreV1().Pods(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("cannot read pod: %w", err)
		}
		return extractContainerResources(obj.Spec.Containers), nil
	default:
		return nil, fmt.Errorf("unsupported kind: %s", ref.Kind)
	}
}

func extractContainerResources(containers []corev1.Container) []ContainerResources {
	result := make([]ContainerResources, len(containers))
	for i, c := range containers {
		result[i] = ContainerResources{
			Name:          c.Name,
			CPURequest:    c.Resources.Requests.Cpu().AsApproximateFloat64(),
			CPULimit:      c.Resources.Limits.Cpu().AsApproximateFloat64(),
			MemoryRequest: float64(c.Resources.Requests.Memory().Value()),
			MemoryLimit:   float64(c.Resources.Limits.Memory().Value()),
		}
	}
	return result
}
