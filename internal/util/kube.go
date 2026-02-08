// Wrapper to build the K8s client.

package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// expandTilde replaces a leading ~ with the user's home directory.
// Returns the path unchanged if it doesn't start with ~/.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

// BuildRestConfig builds a Kubernetes rest config.
//
// Priority:
// 1. explicit kubeconfig flag
// 2. $KUBECONFIG
// 3. in-cluster config
func BuildRestConfig(kubeconfig string) (*rest.Config, error) {
	var (
		cfg *rest.Config
		err error
	)

	if kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", expandTilde(kubeconfig))
		if err != nil {
			return nil, fmt.Errorf("build config from kubeconfig=%s: %w", kubeconfig, err)
		}
	} else if env := os.Getenv("KUBECONFIG"); env != "" {
		expanded := expandTilde(env)
		cfg, err = clientcmd.BuildConfigFromFlags("", expanded)
		if err != nil {
			return nil, fmt.Errorf("build config from $KUBECONFIG=%s: %w", env, err)
		}
	} else {
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("in-cluster config: %w", err)
		}
	}

	return cfg, nil
}

// BuildKubeClient builds a Kubernetes clientset.
//
// Priority:
// 1. explicit kubeconfig flag
// 2. $KUBECONFIG
// 3. in-cluster config
func BuildKubeClient(kubeconfig string) (*kubernetes.Clientset, error) {
	cfg, err := BuildRestConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("new clientset: %w", err)
	}
	return clientset, nil
}
