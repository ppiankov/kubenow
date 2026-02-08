# Codex Work Order 4: Fix KUBECONFIG tilde expansion

## Branch
`codex/tilde-expansion`

## Depends on
- None (applies cleanly to main)

## Problem
When `KUBECONFIG` contains a tilde path like `~/.kube/nutson-eks-prod.yaml`, kubenow fails with:
```
stat ~/.kube/nutson-eks-prod.yaml: no such file or directory
```
The Go `os` package and `client-go` do not expand `~` to the user's home directory.
The same bug exists for the `--kubeconfig` CLI flag.

## Root Cause
`BuildRestConfig()` in `internal/util/kube.go` passes the raw path to `clientcmd.BuildConfigFromFlags()` without expanding `~`.

## Task
Add a `expandTilde()` helper to `internal/util/kube.go` that replaces a leading `~/` with the user's home directory. Call it on both the explicit flag path and the `$KUBECONFIG` env value before passing to `clientcmd`.

Add unit tests in `internal/util/kube_test.go`.

## Files to modify
- `internal/util/kube.go` — add `expandTilde()`, call it in `BuildRestConfig()`
- `internal/util/kube_test.go` — new file with tests

## Implementation

### kube.go changes

Add this helper function:

```go
// expandTilde replaces a leading ~ with the user's home directory.
// Returns the path unchanged if it doesn't start with ~/.
func expandTilde(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path // can't expand, return as-is
	}
	return filepath.Join(home, path[2:])
}
```

Add `"path/filepath"` and `"strings"` to the imports.

Modify `BuildRestConfig` to expand tilde on both paths:

```go
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
```

### kube_test.go (new file)

```go
package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandTilde_WithTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	result := expandTilde("~/.kube/config")
	assert.Equal(t, filepath.Join(home, ".kube/config"), result)
}

func TestExpandTilde_WithoutTilde(t *testing.T) {
	result := expandTilde("/etc/kubernetes/config")
	assert.Equal(t, "/etc/kubernetes/config", result)
}

func TestExpandTilde_Empty(t *testing.T) {
	result := expandTilde("")
	assert.Equal(t, "", result)
}

func TestExpandTilde_TildeOnly(t *testing.T) {
	// "~" alone without "/" should not expand
	result := expandTilde("~")
	assert.Equal(t, "~", result)
}

func TestExpandTilde_TildeInMiddle(t *testing.T) {
	// Tilde not at start should not expand
	result := expandTilde("/home/user/~/config")
	assert.Equal(t, "/home/user/~/config", result)
}
```

## Constraints
- Only modify `internal/util/kube.go` and create `internal/util/kube_test.go`
- Do NOT modify any other files
- Use `os.UserHomeDir()` (not `os.Getenv("HOME")`) for cross-platform correctness
- The `expandTilde` function must be unexported (lowercase)
- Run `go test -race -cover ./internal/util/...` — all must pass

## Verification
```bash
go test -race -cover ./internal/util/...
```
