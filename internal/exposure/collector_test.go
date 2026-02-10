package exposure

import (
	"context"
	"testing"
	"time"

	"github.com/ppiankov/kubenow/internal/metrics"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

const testIngressClass = "nginx"

func TestSelectorMatchesLabels(t *testing.T) {
	tests := []struct {
		selector map[string]string
		labels   map[string]string
		name     string
		want     bool
	}{
		{map[string]string{"app": "worker"}, map[string]string{"app": "worker"}, "exact match", true},
		{map[string]string{"app": "worker"}, map[string]string{"app": "worker", "version": "v2"}, "subset match", true},
		{map[string]string{"app": "worker"}, map[string]string{"app": "api"}, "no match", false},
		{map[string]string{}, map[string]string{"app": "worker"}, "empty selector matches all", true},
		{nil, map[string]string{"app": "worker"}, "nil selector matches all", true},
		{map[string]string{"app": "worker"}, map[string]string{}, "empty labels no match", false},
		{map[string]string{"app": "worker"}, nil, "nil labels no match", false},
		{nil, nil, "both nil", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, selectorMatchesLabels(tt.selector, tt.labels))
		})
	}
}

func TestFindMatchingServices(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-svc", Namespace: "billing"},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "worker"},
				Type:     corev1.ServiceTypeClusterIP,
				Ports: []corev1.ServicePort{{
					Name:       "http",
					Port:       8080,
					TargetPort: intstr.FromInt32(8080),
					Protocol:   corev1.ProtocolTCP,
				}},
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "unrelated-svc", Namespace: "billing"},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "api"},
				Type:     corev1.ServiceTypeClusterIP,
			},
		},
	)

	collector := &ExposureCollector{kubeClient: client}
	podLabels := map[string]string{"app": "worker", "version": "v1"}
	services, errs := collector.findMatchingServices(ctx, "billing", podLabels)

	assert.Empty(t, errs)
	require.Len(t, services, 1)
	assert.Equal(t, "worker-svc", services[0].Name)
	assert.Equal(t, "ClusterIP", services[0].Type)
	require.Len(t, services[0].Ports, 1)
	assert.Equal(t, int32(8080), services[0].Ports[0].Port)
}

func TestFindMatchingServices_Headless(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "headless", Namespace: "ns"},
			Spec: corev1.ServiceSpec{
				Selector:  map[string]string{"app": "db"},
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: "None",
			},
		},
	)

	collector := &ExposureCollector{kubeClient: client}
	services, _ := collector.findMatchingServices(ctx, "ns", map[string]string{"app": "db"})

	require.Len(t, services, 1)
	assert.Equal(t, "Headless", services[0].Type)
}

func TestFindMatchingServices_NoMatch(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "ns"},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "other"},
			},
		},
	)

	collector := &ExposureCollector{kubeClient: client}
	services, _ := collector.findMatchingServices(ctx, "ns", map[string]string{"app": "worker"})
	assert.Empty(t, services)
}

func TestFindIngressesForServices(t *testing.T) {
	ctx := context.Background()
	className := testIngressClass
	client := fake.NewSimpleClientset(
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "main-ingress", Namespace: "billing"},
			Spec: networkingv1.IngressSpec{
				IngressClassName: &className,
				TLS: []networkingv1.IngressTLS{{
					Hosts: []string{"payments.example.com"},
				}},
				Rules: []networkingv1.IngressRule{{
					Host: "payments.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{{
								Path: "/webhooks",
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{
										Name: "worker-svc",
									},
								},
							}},
						},
					},
				}},
			},
		},
	)

	collector := &ExposureCollector{kubeClient: client}
	result, errs := collector.findIngressesForServices(ctx, "billing", []string{"worker-svc"})

	assert.Empty(t, errs)
	require.Len(t, result["worker-svc"], 1)
	route := result["worker-svc"][0]
	assert.Equal(t, "main-ingress", route.Name)
	assert.Equal(t, testIngressClass, route.ClassName)
	assert.Equal(t, []string{"payments.example.com"}, route.Hosts)
	assert.Equal(t, []string{"/webhooks"}, route.Paths)
	assert.True(t, route.TLS)
}

func TestFindIngressesForServices_NoIngress(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()

	collector := &ExposureCollector{kubeClient: client}
	result, errs := collector.findIngressesForServices(ctx, "billing", []string{"worker-svc"})

	assert.Empty(t, errs)
	assert.Empty(t, result["worker-svc"])
}

func TestFindNetworkPolicies(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		&networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "allow-billing", Namespace: "billing"},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "worker"},
				},
				Ingress: []networkingv1.NetworkPolicyIngressRule{{
					From: []networkingv1.NetworkPolicyPeer{{
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"kubernetes.io/metadata.name": "api-gateway"},
						},
					}},
				}},
			},
		},
		&networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "unrelated", Namespace: "billing"},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "api"},
				},
			},
		},
	)

	collector := &ExposureCollector{kubeClient: client}
	result, errs := collector.findNetworkPolicies(ctx, "billing", map[string]string{"app": "worker"})

	assert.Empty(t, errs)
	rules := result[""]
	require.Len(t, rules, 1)
	assert.Equal(t, "allow-billing", rules[0].PolicyName)
	require.Len(t, rules[0].Sources, 1)
	assert.Equal(t, "namespace", rules[0].Sources[0].Type)
	assert.Contains(t, rules[0].Sources[0].Namespace, "api-gateway")
}

func TestFindNetworkPolicies_AllowAll(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		&networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{Name: "allow-all", Namespace: "ns"},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{},
				Ingress:     []networkingv1.NetworkPolicyIngressRule{{
					// Empty From = allow all
				}},
			},
		},
	)

	collector := &ExposureCollector{kubeClient: client}
	result, _ := collector.findNetworkPolicies(ctx, "ns", map[string]string{"app": "worker"})

	rules := result[""]
	require.Len(t, rules, 1)
	require.Len(t, rules[0].Sources, 1)
	assert.Equal(t, "all", rules[0].Sources[0].Type)
}

func TestResolveWorkloadNameFallback(t *testing.T) {
	tests := []struct {
		podName  string
		expected string
	}{
		{"payment-api-7d8f9c4b6-abc12", "payment-api"},
		{"worker-payment-event-6f9b8d-xz4kp", "worker-payment-event"},
		{"simple-abc12", "simple-abc12"},
		{"standalone", "standalone"},
	}
	for _, tt := range tests {
		t.Run(tt.podName, func(t *testing.T) {
			assert.Equal(t, tt.expected, metrics.ResolveWorkloadName(tt.podName, nil))
		})
	}
}

func TestCollect_EndToEnd(t *testing.T) {
	ctx := context.Background()
	className := testIngressClass
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "billing"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "worker"},
				},
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-svc", Namespace: "billing"},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "worker"},
				Type:     corev1.ServiceTypeClusterIP,
				Ports:    []corev1.ServicePort{{Port: 8080, TargetPort: intstr.FromInt32(8080)}},
			},
		},
		&networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: "ing", Namespace: "billing"},
			Spec: networkingv1.IngressSpec{
				IngressClassName: &className,
				Rules: []networkingv1.IngressRule{{
					Host: "billing.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{{
								Path: "/",
								Backend: networkingv1.IngressBackend{
									Service: &networkingv1.IngressServiceBackend{Name: "worker-svc"},
								},
							}},
						},
					},
				}},
			},
		},
	)

	collector := NewExposureCollector(client, nil)
	result, err := collector.Collect(ctx, "billing", "worker", "Deployment")

	require.NoError(t, err)
	require.Len(t, result.Services, 1)
	assert.Equal(t, "worker-svc", result.Services[0].Name)
	require.Len(t, result.Services[0].Ingresses, 1)
	assert.Equal(t, "billing.example.com", result.Services[0].Ingresses[0].Hosts[0])
	// Neighbors empty because no metrics client
	assert.Empty(t, result.Neighbors)
}

func TestIngressClassName_Annotation(t *testing.T) {
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"kubernetes.io/ingress.class": "traefik"},
		},
	}
	assert.Equal(t, "traefik", ingressClassName(ing))
}

func TestIngressClassName_Spec(t *testing.T) {
	class := testIngressClass
	ing := &networkingv1.Ingress{
		Spec: networkingv1.IngressSpec{IngressClassName: &class},
	}
	assert.Equal(t, testIngressClass, ingressClassName(ing))
}

// mockPromAPI implements a minimal v1.API for testing traffic source queries.
type mockPromAPI struct {
	v1.API
	queryResult model.Value
	queryErr    error
}

func (m *mockPromAPI) Query(_ context.Context, _ string, _ time.Time, _ ...v1.Option) (model.Value, v1.Warnings, error) {
	return m.queryResult, nil, m.queryErr
}

func TestCollectTrafficSources_WithResults(t *testing.T) {
	ctx := context.Background()
	mock := &mockPromAPI{
		queryResult: model.Vector{
			{
				Metric: model.Metric{"deployment": "payment-api", "namespace": "billing"},
				Value:  512280,
			},
			{
				Metric: model.Metric{"deployment": "gateway", "namespace": "api-gateway"},
				Value:  137160,
			},
			{
				Metric: model.Metric{"deployment": "batch-worker", "namespace": "jobs"},
				Value:  8640,
			},
		},
	}

	collector := &ExposureCollector{promAPI: mock}
	sources, err := collector.collectTrafficSources(ctx, "billing", "worker")

	require.NoError(t, err)
	require.Len(t, sources, 3)

	// Sorted by total descending
	assert.Equal(t, "payment-api", sources[0].Deployment)
	assert.Equal(t, "billing", sources[0].Namespace)
	assert.InDelta(t, 512280, sources[0].Total, 0.1)
	assert.InDelta(t, 512280.0/3600.0, sources[0].RPS, 0.1)

	assert.Equal(t, "gateway", sources[1].Deployment)
	assert.Equal(t, "batch-worker", sources[2].Deployment)
}

func TestCollectTrafficSources_Empty(t *testing.T) {
	ctx := context.Background()
	mock := &mockPromAPI{
		queryResult: model.Vector{},
	}

	collector := &ExposureCollector{promAPI: mock}
	sources, err := collector.collectTrafficSources(ctx, "ns", "worker")

	require.NoError(t, err)
	assert.Empty(t, sources)
}

func TestCollectTrafficSources_ZeroValueFiltered(t *testing.T) {
	ctx := context.Background()
	mock := &mockPromAPI{
		queryResult: model.Vector{
			{
				Metric: model.Metric{"deployment": "active", "namespace": "ns"},
				Value:  1000,
			},
			{
				Metric: model.Metric{"deployment": "idle", "namespace": "ns"},
				Value:  0,
			},
		},
	}

	collector := &ExposureCollector{promAPI: mock}
	sources, err := collector.collectTrafficSources(ctx, "ns", "worker")

	require.NoError(t, err)
	require.Len(t, sources, 1)
	assert.Equal(t, "active", sources[0].Deployment)
}

func TestCollect_NoPrometheus_NilTrafficSources(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "ns"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "worker"},
				},
			},
		},
	)

	collector := NewExposureCollector(client, nil) // no Prometheus
	result, err := collector.Collect(ctx, "ns", "worker", "Deployment")

	require.NoError(t, err)
	assert.Nil(t, result.TrafficSources)
}

func TestCollect_WithPrometheus_TrafficSourcesPopulated(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(
		&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: "worker", Namespace: "ns"},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "worker"},
				},
			},
		},
	)

	mock := &mockPromAPI{
		queryResult: model.Vector{
			{
				Metric: model.Metric{"deployment": "frontend", "namespace": "ns"},
				Value:  5000,
			},
		},
	}

	collector := NewExposureCollector(client, nil)
	collector.SetPrometheusAPI(mock)
	result, err := collector.Collect(ctx, "ns", "worker", "Deployment")

	require.NoError(t, err)
	require.Len(t, result.TrafficSources, 1)
	assert.Equal(t, "frontend", result.TrafficSources[0].Deployment)
}
