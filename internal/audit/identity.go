package audit

import (
	"context"
	"os"
	"os/user"

	authenticationv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// Identity records who initiated an apply operation.
type Identity struct {
	KubeContext        string `json:"kube_context"`
	KubeUser           string `json:"kube_user"`
	OSUser             string `json:"os_user"`
	Machine            string `json:"machine"`
	IdentitySource     string `json:"identity_source"`
	IdentityConfidence string `json:"identity_confidence"`
}

// ResolveIdentity determines the acting user via SSR (preferred), kubeconfig
// fallback, and OS identity. The returned Identity always has OS fields set.
func ResolveIdentity(ctx context.Context, client kubernetes.Interface, kubeconfigPath string) *Identity {
	id := &Identity{}

	// Always record OS identity
	osUser, machine := resolveOSIdentity()
	id.OSUser = osUser
	id.Machine = machine

	// Try SSR first (K8s 1.27+ GA)
	if client != nil {
		kubeUser, err := resolveSSR(ctx, client)
		if err == nil {
			id.KubeUser = kubeUser
			id.IdentitySource = "ssr"
			id.IdentityConfidence = "verified"
			// Still try kubeconfig for context name
			ctxName, _ := resolveKubeconfig(kubeconfigPath)
			id.KubeContext = ctxName
			return id
		}
	}

	// Fallback to kubeconfig parsing
	ctxName, kubeUser := resolveKubeconfig(kubeconfigPath)
	if ctxName != "" || kubeUser != "" {
		id.KubeContext = ctxName
		id.KubeUser = kubeUser
		id.IdentitySource = "kubeconfig"
		id.IdentityConfidence = "parsed"
		return id
	}

	// Both failed
	id.IdentitySource = "unknown"
	id.IdentityConfidence = "none"
	return id
}

// resolveSSR uses SelfSubjectReview to get the authenticated username.
func resolveSSR(ctx context.Context, client kubernetes.Interface) (string, error) {
	ssr := &authenticationv1.SelfSubjectReview{}
	result, err := client.AuthenticationV1().SelfSubjectReviews().Create(ctx, ssr, metav1.CreateOptions{})
	if err != nil {
		return "", err
	}
	return result.Status.UserInfo.Username, nil
}

// resolveKubeconfig extracts the current context name and user from kubeconfig.
// Returns empty strings on error.
func resolveKubeconfig(kubeconfigPath string) (contextName, userName string) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		rules.ExplicitPath = kubeconfigPath
	}
	overrides := &clientcmd.ConfigOverrides{}
	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)

	rawConfig, err := config.RawConfig()
	if err != nil {
		return "", ""
	}

	contextName = rawConfig.CurrentContext
	if ctx, ok := rawConfig.Contexts[contextName]; ok {
		userName = ctx.AuthInfo
	}
	return contextName, userName
}

// resolveOSIdentity returns the current OS username and hostname.
func resolveOSIdentity() (string, string) {
	var osUser string
	if u, err := user.Current(); err == nil {
		osUser = u.Username
	}
	machine, _ := os.Hostname()
	return osUser, machine
}
