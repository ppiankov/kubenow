// Wrapper to build the K8s client.

package util

import (
	"fmt"
	"os"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// BuildKubeClient builds a Kubernetes clientset.
//
// Priority:
// 1. explicit kubeconfig flag
// 2. $KUBECONFIG
// 3. in-cluster config
func BuildKubeClient(kubeconfig string) (*kubernetes.Clientset, error) {
	var (
		cfg *rest.Config
		err error
	)

	if kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("build config from kubeconfig=%s: %w", kubeconfig, err)
		}
	} else if env := os.Getenv("KUBECONFIG"); env != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", env)
		if err != nil {
			return nil, fmt.Errorf("build config from $KUBECONFIG=%s: %w", env, err)
		}
	} else {
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("in-cluster config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("new clientset: %w", err)
	}
	return clientset, nil
}
