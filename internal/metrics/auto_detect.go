package metrics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// PrometheusCandidate represents a well-known Prometheus service location.
type PrometheusCandidate struct {
	Namespace string
	Service   string
}

// defaultCandidates lists well-known Prometheus service locations in priority order.
var defaultCandidates = []PrometheusCandidate{
	{Namespace: "monitoring", Service: "prometheus-server"},
	{Namespace: "monitoring", Service: "prometheus-operated"},
	{Namespace: "monitoring", Service: "prometheus"},
	{Namespace: "kube-system", Service: "prometheus"},
	{Namespace: "observability", Service: "prometheus"},
	{Namespace: "prometheus", Service: "prometheus"},
}

// AutoDetect discovers a healthy Prometheus service in the cluster by checking
// well-known namespace/service combinations. Returns the in-cluster URL of the
// first healthy Prometheus found.
func AutoDetect(ctx context.Context, kubeClient kubernetes.Interface) (string, error) {
	var checked []string

	for _, c := range defaultCandidates {
		label := c.Namespace + "/" + c.Service
		checked = append(checked, label)

		svc, err := kubeClient.CoreV1().Services(c.Namespace).Get(ctx, c.Service, metav1.GetOptions{})
		if err != nil {
			continue
		}

		// Find a suitable port (prefer 9090, then "http", then first port)
		port := 0
		for _, p := range svc.Spec.Ports {
			if p.Port == 9090 {
				port = int(p.Port)
				break
			}
		}
		if port == 0 {
			for _, p := range svc.Spec.Ports {
				if strings.EqualFold(p.Name, "http") || strings.EqualFold(p.Name, "web") {
					port = int(p.Port)
					break
				}
			}
		}
		if port == 0 && len(svc.Spec.Ports) > 0 {
			port = int(svc.Spec.Ports[0].Port)
		}
		if port == 0 {
			continue
		}

		url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d", c.Service, c.Namespace, port)

		if healthy := probePrometheus(ctx, url); healthy {
			return url, nil
		}
	}

	return "", fmt.Errorf("no Prometheus found in cluster (checked: %s)", strings.Join(checked, ", "))
}

// probePrometheus checks if a Prometheus endpoint is healthy by calling its runtime info API.
func probePrometheus(ctx context.Context, prometheusURL string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	client, err := api.NewClient(api.Config{Address: prometheusURL})
	if err != nil {
		return false
	}

	promAPI := v1.NewAPI(client)
	_, err = promAPI.Runtimeinfo(probeCtx)
	return err == nil
}
