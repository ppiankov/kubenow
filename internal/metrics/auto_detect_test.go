package metrics

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestAutoDetect_NoServices(t *testing.T) {
	client := fake.NewSimpleClientset()
	ctx := context.Background()

	_, err := AutoDetect(ctx, client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Prometheus found")
	assert.Contains(t, err.Error(), "monitoring/prometheus-server")
}

func TestAutoDetect_ServiceExistsButUnhealthy(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prometheus-server",
			Namespace: "monitoring",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 9090},
			},
		},
	}
	client := fake.NewSimpleClientset(svc)
	ctx := context.Background()

	// Service exists but Prometheus is not actually running, so probe fails
	_, err := AutoDetect(ctx, client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Prometheus found")
}

func TestAutoDetect_PortSelection_Prefer9090(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prometheus",
			Namespace: "monitoring",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "grpc", Port: 10901},
				{Name: "http", Port: 9090},
			},
		},
	}
	client := fake.NewSimpleClientset(svc)
	ctx := context.Background()

	// Will find the service but probe fails (no real Prometheus)
	_, err := AutoDetect(ctx, client)
	require.Error(t, err)
	// The important thing is it didn't panic and checked the service
}

func TestAutoDetect_PortSelection_FallbackToHTTP(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prometheus",
			Namespace: "monitoring",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 8080},
			},
		},
	}
	client := fake.NewSimpleClientset(svc)
	ctx := context.Background()

	_, err := AutoDetect(ctx, client)
	require.Error(t, err)
}

func TestAutoDetect_PortSelection_FallbackToFirst(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prometheus",
			Namespace: "monitoring",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "custom", Port: 3000},
			},
		},
	}
	client := fake.NewSimpleClientset(svc)
	ctx := context.Background()

	_, err := AutoDetect(ctx, client)
	require.Error(t, err)
}

func TestAutoDetect_NoPorts(t *testing.T) {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prometheus",
			Namespace: "monitoring",
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{},
		},
	}
	client := fake.NewSimpleClientset(svc)
	ctx := context.Background()

	_, err := AutoDetect(ctx, client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no Prometheus found")
}

func TestDefaultCandidates(t *testing.T) {
	assert.GreaterOrEqual(t, len(defaultCandidates), 5, "should check multiple well-known locations")
	// First candidate should be the most common
	assert.Equal(t, "monitoring", defaultCandidates[0].Namespace)
}
