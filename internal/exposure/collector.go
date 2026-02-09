// Package exposure queries Kubernetes APIs to build a structural
// topology of possible traffic paths to a workload.
package exposure

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ppiankov/kubenow/internal/metrics"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

// ExposureCollector queries Kubernetes APIs to build an ExposureMap.
type ExposureCollector struct {
	kubeClient    kubernetes.Interface
	metricsClient metricsclientset.Interface
}

// NewExposureCollector creates a new collector.
func NewExposureCollector(kubeClient kubernetes.Interface, metricsClient metricsclientset.Interface) *ExposureCollector {
	return &ExposureCollector{
		kubeClient:    kubeClient,
		metricsClient: metricsClient,
	}
}

// Collect builds the full ExposureMap for a workload. Each sub-query
// is independent — if one fails, the error is recorded and collection
// continues with the remaining queries.
func (c *ExposureCollector) Collect(ctx context.Context, namespace, workloadName, workloadKind string) (*ExposureMap, error) {
	result := &ExposureMap{
		Namespace:    namespace,
		WorkloadName: workloadName,
		WorkloadKind: workloadKind,
		QueryTime:    time.Now(),
	}

	// Step 1: resolve pod labels from workload spec
	podLabels, err := c.resolveWorkloadLabels(ctx, namespace, workloadName, workloadKind)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve workload labels: %w", err)
	}

	// Step 2: find services matching pod labels
	services, errs := c.findMatchingServices(ctx, namespace, podLabels)
	result.Services = services
	result.Errors = append(result.Errors, errs...)

	// Step 3: find ingresses for discovered services
	serviceNames := make([]string, len(services))
	for i, s := range services {
		serviceNames[i] = s.Name
	}
	ingressMap, errs := c.findIngressesForServices(ctx, namespace, serviceNames)
	result.Errors = append(result.Errors, errs...)
	for i := range result.Services {
		if routes, ok := ingressMap[result.Services[i].Name]; ok {
			result.Services[i].Ingresses = routes
		}
	}

	// Step 4: find network policies
	netpolMap, errs := c.findNetworkPolicies(ctx, namespace, podLabels)
	result.Errors = append(result.Errors, errs...)
	for i := range result.Services {
		if rules, ok := netpolMap[result.Services[i].Name]; ok {
			result.Services[i].NetPols = rules
		}
	}
	// Attach netpols at the service level for display; if no services
	// but policies exist, they still apply to the workload's pods.
	if len(result.Services) == 0 && len(netpolMap) > 0 {
		// Netpols found but no services — still useful info
		if rules, ok := netpolMap[""]; ok && len(rules) > 0 {
			result.Errors = append(result.Errors, fmt.Sprintf("%d NetworkPolicy(ies) apply but no services found", len(rules)))
		}
	}

	// Step 5: namespace neighbors by CPU
	neighbors, errs := c.collectNeighbors(ctx, namespace, workloadName)
	result.Neighbors = neighbors
	result.Errors = append(result.Errors, errs...)

	return result, nil
}

// resolveWorkloadLabels gets the matchLabels from the workload's pod selector.
func (c *ExposureCollector) resolveWorkloadLabels(ctx context.Context, namespace, name, kind string) (map[string]string, error) {
	switch kind {
	case "Deployment":
		obj, err := c.kubeClient.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if obj.Spec.Selector != nil {
			return obj.Spec.Selector.MatchLabels, nil
		}
		return nil, nil
	case "StatefulSet":
		obj, err := c.kubeClient.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if obj.Spec.Selector != nil {
			return obj.Spec.Selector.MatchLabels, nil
		}
		return nil, nil
	case "DaemonSet":
		obj, err := c.kubeClient.AppsV1().DaemonSets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if obj.Spec.Selector != nil {
			return obj.Spec.Selector.MatchLabels, nil
		}
		return nil, nil
	case "Pod":
		obj, err := c.kubeClient.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return obj.Labels, nil
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
}

// findMatchingServices lists services whose selector is a subset of podLabels.
func (c *ExposureCollector) findMatchingServices(ctx context.Context, namespace string, podLabels map[string]string) (services []ServiceExposure, errs []string) {
	svcs, err := c.kubeClient.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, []string{fmt.Sprintf("services: %v", err)}
	}

	var result []ServiceExposure
	for i := range svcs.Items {
		svc := &svcs.Items[i]
		if !selectorMatchesLabels(svc.Spec.Selector, podLabels) {
			continue
		}

		svcType := string(svc.Spec.Type)
		if svc.Spec.ClusterIP == "None" {
			svcType = "Headless"
		}

		var ports []PortMapping
		for _, p := range svc.Spec.Ports {
			ports = append(ports, PortMapping{
				Name:       p.Name,
				Port:       p.Port,
				TargetPort: p.TargetPort.String(),
				Protocol:   string(p.Protocol),
			})
		}

		result = append(result, ServiceExposure{
			Name:  svc.Name,
			Type:  svcType,
			Ports: ports,
		})
	}
	return result, nil
}

// findIngressesForServices finds ingresses routing to the given services.
func (c *ExposureCollector) findIngressesForServices(ctx context.Context, namespace string, serviceNames []string) (routes map[string][]IngressRoute, errs []string) {
	if len(serviceNames) == 0 {
		return nil, nil
	}

	ingresses, err := c.kubeClient.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, []string{fmt.Sprintf("ingresses: %v", err)}
	}

	nameSet := make(map[string]bool, len(serviceNames))
	for _, n := range serviceNames {
		nameSet[n] = true
	}

	result := make(map[string][]IngressRoute)
	for i := range ingresses.Items {
		ing := &ingresses.Items[i]
		className := ingressClassName(ing)

		// Check if any TLS hosts are configured
		tlsHosts := make(map[string]bool)
		for _, tls := range ing.Spec.TLS {
			for _, h := range tls.Hosts {
				tlsHosts[h] = true
			}
		}

		for _, rule := range ing.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service == nil {
					continue
				}
				svcName := path.Backend.Service.Name
				if !nameSet[svcName] {
					continue
				}

				host := rule.Host
				if host == "" {
					host = "*"
				}
				pathStr := "/"
				if path.Path != "" {
					pathStr = path.Path
				}

				route := IngressRoute{
					Name:      ing.Name,
					ClassName: className,
					Hosts:     []string{host},
					Paths:     []string{pathStr},
					TLS:       tlsHosts[host],
				}
				result[svcName] = append(result[svcName], route)
			}
		}
	}
	return result, nil
}

// findNetworkPolicies finds policies whose podSelector matches the workload's labels.
// Returns a map keyed by "" (workload-level) since netpols apply to pods, not services.
func (c *ExposureCollector) findNetworkPolicies(ctx context.Context, namespace string, podLabels map[string]string) (policies map[string][]NetPolRule, errs []string) {
	netpols, err := c.kubeClient.NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, []string{fmt.Sprintf("networkpolicies: %v", err)}
	}

	var rules []NetPolRule
	for i := range netpols.Items {
		np := &netpols.Items[i]
		if !selectorMatchesLabels(np.Spec.PodSelector.MatchLabels, podLabels) {
			continue
		}

		rule := NetPolRule{PolicyName: np.Name}
		for _, ingress := range np.Spec.Ingress {
			if len(ingress.From) == 0 {
				// Empty from = allow all
				rule.Sources = append(rule.Sources, NetPolSource{Type: "all"})
				continue
			}
			for _, from := range ingress.From {
				rule.Sources = append(rule.Sources, parseNetPolSource(from)...)
			}
		}
		rules = append(rules, rule)
	}

	// Distribute to all services (netpols apply to pods, not services)
	// Use empty string key for workload-level policies
	result := make(map[string][]NetPolRule)
	result[""] = rules
	return result, nil
}

// parseNetPolSource extracts sources from a NetworkPolicyPeer.
func parseNetPolSource(peer networkingv1.NetworkPolicyPeer) []NetPolSource {
	var sources []NetPolSource

	if peer.NamespaceSelector != nil {
		labels := peer.NamespaceSelector.MatchLabels
		if len(labels) == 0 {
			sources = append(sources, NetPolSource{Type: "namespace", Namespace: "*"})
		} else {
			for k, v := range labels {
				sources = append(sources, NetPolSource{
					Type:      "namespace",
					Namespace: fmt.Sprintf("%s=%s", k, v),
				})
			}
		}
	}

	if peer.PodSelector != nil {
		labels := peer.PodSelector.MatchLabels
		parts := make([]string, 0, len(labels))
		for k, v := range labels {
			parts = append(parts, fmt.Sprintf("%s=%s", k, v))
		}
		sort.Strings(parts)
		sources = append(sources, NetPolSource{
			Type:     "pod",
			PodLabel: strings.Join(parts, ","),
		})
	}

	if peer.IPBlock != nil {
		cidr := peer.IPBlock.CIDR
		if len(peer.IPBlock.Except) > 0 {
			cidr += fmt.Sprintf(" (except %s)", strings.Join(peer.IPBlock.Except, ", "))
		}
		sources = append(sources, NetPolSource{
			Type: "ipBlock",
			CIDR: cidr,
		})
	}

	return sources
}

// collectNeighbors queries PodMetrics for the namespace and groups by workload.
func (c *ExposureCollector) collectNeighbors(ctx context.Context, namespace, excludeWorkload string) (neighbors []Neighbor, errs []string) {
	if c.metricsClient == nil {
		return nil, []string{"metrics client not available"}
	}

	podMetrics, err := c.metricsClient.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, []string{fmt.Sprintf("pod metrics: %v", err)}
	}

	// Build pod name → labels map for operator detection
	podLabelMap := make(map[string]map[string]string)
	pods, err := c.kubeClient.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err == nil {
		for i := range pods.Items {
			podLabelMap[pods.Items[i].Name] = pods.Items[i].Labels
		}
	}

	// Aggregate by workload name
	type workloadStats struct {
		cpuNano      int64
		memBytes     int64
		pods         int
		operatorType string
	}
	agg := make(map[string]*workloadStats)

	for i := range podMetrics.Items {
		pm := &podMetrics.Items[i]
		wlName, opType := metrics.ResolveWorkloadIdentity(pm.Name, podLabelMap[pm.Name])
		if wlName == excludeWorkload {
			continue
		}

		stats, ok := agg[wlName]
		if !ok {
			stats = &workloadStats{operatorType: opType}
			agg[wlName] = stats
		}
		stats.pods++

		for j := range pm.Containers {
			ct := &pm.Containers[j]
			stats.cpuNano += ct.Usage.Cpu().MilliValue()
			stats.memBytes += ct.Usage.Memory().Value()
		}
	}

	// Convert to sorted slice
	neighbors = make([]Neighbor, 0, len(agg))
	for name, stats := range agg {
		neighbors = append(neighbors, Neighbor{
			WorkloadName: name,
			WorkloadKind: stats.operatorType,
			CPUMillis:    stats.cpuNano,
			MemoryMi:     stats.memBytes / (1024 * 1024),
			PodCount:     stats.pods,
		})
	}

	sort.Slice(neighbors, func(i, j int) bool {
		return neighbors[i].CPUMillis > neighbors[j].CPUMillis
	})

	return neighbors, nil
}

// selectorMatchesLabels checks if all selector key-value pairs exist in labels.
// An empty or nil selector matches everything.
func selectorMatchesLabels(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return true
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// ingressClassName extracts the ingress class from spec or annotation.
func ingressClassName(ing *networkingv1.Ingress) string {
	if ing.Spec.IngressClassName != nil && *ing.Spec.IngressClassName != "" {
		return *ing.Spec.IngressClassName
	}
	// Fallback to deprecated annotation
	if class, ok := ing.Annotations["kubernetes.io/ingress.class"]; ok {
		return class
	}
	return ""
}
