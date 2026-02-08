package audit

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestResolveOSIdentity(t *testing.T) {
	osUser, machine := resolveOSIdentity()
	assert.NotEmpty(t, osUser, "os_user should not be empty")
	assert.NotEmpty(t, machine, "machine should not be empty")
}

func TestResolveKubeconfig_FromFile(t *testing.T) {
	content := `apiVersion: v1
kind: Config
current-context: test-context
contexts:
- context:
    cluster: test-cluster
    user: test-user
  name: test-context
clusters:
- cluster:
    server: https://localhost:6443
  name: test-cluster
users:
- name: test-user
  user:
    token: fake-token
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(content), 0644))

	ctxName, userName := resolveKubeconfig(kubeconfigPath)
	assert.Equal(t, "test-context", ctxName)
	assert.Equal(t, "test-user", userName)
}

func TestResolveKubeconfig_MissingFile(t *testing.T) {
	ctxName, userName := resolveKubeconfig("/nonexistent/path/kubeconfig")
	// Should return empty strings, not panic
	assert.Empty(t, ctxName)
	assert.Empty(t, userName)
}

func TestResolveIdentity_SSRFallback(t *testing.T) {
	// Create a fake client that returns an error for SSR
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "selfsubjectreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, assert.AnError
	})

	// Write a temp kubeconfig so the fallback works
	content := `apiVersion: v1
kind: Config
current-context: fallback-ctx
contexts:
- context:
    cluster: c
    user: fallback-user
  name: fallback-ctx
clusters:
- cluster:
    server: https://localhost:6443
  name: c
users:
- name: fallback-user
  user:
    token: t
`
	tmpDir := t.TempDir()
	kubeconfigPath := filepath.Join(tmpDir, "kubeconfig")
	require.NoError(t, os.WriteFile(kubeconfigPath, []byte(content), 0644))

	id := ResolveIdentity(context.Background(), client, kubeconfigPath)

	assert.Equal(t, "kubeconfig", id.IdentitySource)
	assert.Equal(t, "parsed", id.IdentityConfidence)
	assert.Equal(t, "fallback-ctx", id.KubeContext)
	assert.Equal(t, "fallback-user", id.KubeUser)
	assert.NotEmpty(t, id.OSUser)
	assert.NotEmpty(t, id.Machine)
}

func TestResolveIdentity_SSRSuccess(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "selfsubjectreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authenticationv1.SelfSubjectReview{
			Status: authenticationv1.SelfSubjectReviewStatus{
				UserInfo: authenticationv1.UserInfo{
					Username: "system:admin",
				},
			},
		}, nil
	})

	id := ResolveIdentity(context.Background(), client, "")

	assert.Equal(t, "ssr", id.IdentitySource)
	assert.Equal(t, "verified", id.IdentityConfidence)
	assert.Equal(t, "system:admin", id.KubeUser)
}

func TestResolveIdentity_BothFail(t *testing.T) {
	client := fake.NewSimpleClientset()
	client.PrependReactor("create", "selfsubjectreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, assert.AnError
	})

	// No kubeconfig file exists at this path
	id := ResolveIdentity(context.Background(), client, "/nonexistent/kubeconfig")

	assert.Equal(t, "unknown", id.IdentitySource)
	assert.Equal(t, "none", id.IdentityConfidence)
	assert.NotEmpty(t, id.OSUser)
	assert.NotEmpty(t, id.Machine)
}
