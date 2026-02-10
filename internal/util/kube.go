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

// KubeOpts holds optional overrides for building Kubernetes clients.
type KubeOpts struct {
	Kubeconfig string // explicit path to kubeconfig file
	Context    string // explicit context override (empty = current-context)
}

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

// buildConfigFromOpts builds a rest.Config using clientcmd loading rules
// that respect both kubeconfig path and context overrides.
func buildConfigFromOpts(kubeconfigPath, contextOverride string) (*rest.Config, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		rules.ExplicitPath = expandTilde(kubeconfigPath)
	}

	overrides := &clientcmd.ConfigOverrides{}
	if contextOverride != "" {
		overrides.CurrentContext = contextOverride
	}

	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// BuildRestConfig builds a Kubernetes rest config.
//
// Priority:
// 1. explicit kubeconfig flag
// 2. $KUBECONFIG
// 3. in-cluster config
//
// Deprecated: use BuildRestConfigWithOpts for context support.
func BuildRestConfig(kubeconfig string) (*rest.Config, error) {
	return BuildRestConfigWithOpts(KubeOpts{Kubeconfig: kubeconfig})
}

// BuildRestConfigWithOpts builds a Kubernetes rest config with context support.
//
// Priority:
// 1. explicit kubeconfig path + context override
// 2. $KUBECONFIG + context override
// 3. default ~/.kube/config + context override
// 4. in-cluster config (context override ignored)
func BuildRestConfigWithOpts(opts KubeOpts) (*rest.Config, error) {
	// If context is specified, always use clientcmd loader (not in-cluster)
	if opts.Context != "" {
		return buildConfigFromOpts(opts.Kubeconfig, opts.Context)
	}

	if opts.Kubeconfig != "" {
		return buildConfigFromOpts(opts.Kubeconfig, "")
	}

	if env := os.Getenv("KUBECONFIG"); env != "" {
		return buildConfigFromOpts(env, "")
	}

	// Try in-cluster first, fall back to default kubeconfig
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}

	// Fall back to default kubeconfig location
	return buildConfigFromOpts("", "")
}

// BuildKubeClient builds a Kubernetes clientset.
//
// Deprecated: use BuildKubeClientWithOpts for context support.
func BuildKubeClient(kubeconfig string) (*kubernetes.Clientset, error) {
	return BuildKubeClientWithOpts(KubeOpts{Kubeconfig: kubeconfig})
}

// BuildKubeClientWithOpts builds a Kubernetes clientset with context support.
func BuildKubeClientWithOpts(opts KubeOpts) (*kubernetes.Clientset, error) {
	cfg, err := BuildRestConfigWithOpts(opts)
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("new clientset: %w", err)
	}
	return clientset, nil
}
