# Codex Work Order 9: Label-based workload name resolution

## Branch
`codex/label-resolution`

## Depends on
Pod-level latch (already merged to main). Pull latest main before starting.

## Problem
`extractWorkloadName()` uses a blind heuristic: strip last 2 dash-segments from pod name. This works for Deployment pods (`name-replicaset-hash-pod-hash`) but fails for:
- StatefulSet pods: `name-ordinal` (only 1 suffix)
- CNPG pods: `name-ordinal` (same as StatefulSet)
- DaemonSet pods: `name-hash` (only 1 suffix)

The function exists in two places:
- `internal/metrics/latch.go:extractWorkloadName()` (line ~395)
- `internal/exposure/collector.go:extractWorkloadName()` (line ~395)

Both are package-private with identical logic.

## Task
Replace the heuristic with label-based resolution using standard Kubernetes labels. Fall back to the heuristic only when no label matches.

## Implementation

### Step 1: Create shared resolution function

Create `internal/metrics/workload_name.go`:

```go
package metrics

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// workloadNameLabels is the priority-ordered list of labels to check
// for workload name resolution.
var workloadNameLabels = []string{
	"app.kubernetes.io/name",
	"app.kubernetes.io/instance",
	"app",
	"cnpg.io/cluster",
}

// ResolveWorkloadName determines the workload name from pod labels.
// Falls back to the dash-stripping heuristic if no label matches.
func ResolveWorkloadName(podName string, labels map[string]string) string {
	for _, key := range workloadNameLabels {
		if val, ok := labels[key]; ok && val != "" {
			return val
		}
	}
	return extractWorkloadNameHeuristic(podName)
}

// extractWorkloadNameHeuristic strips the last two dash-separated segments.
// e.g., "payment-api-7d8f9c4b6-abc12" -> "payment-api"
func extractWorkloadNameHeuristic(podName string) string {
	parts := strings.Split(podName, "-")
	if len(parts) <= 2 {
		return podName
	}
	return strings.Join(parts[:len(parts)-2], "-")
}
```

### Step 2: Build pod label cache in LatchMonitor

In `internal/metrics/latch.go`:

Add to `LatchMonitor` struct:
```go
podLabels map[string]map[string]string // podName → labels
```

Add method to refresh the label cache (call once at latch start and periodically):
```go
func (m *LatchMonitor) refreshPodLabels(ctx context.Context) {
	pods, err := m.kubeClient.CoreV1().Pods(m.config.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return // non-fatal, fall back to heuristic
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.podLabels = make(map[string]map[string]string, len(pods.Items))
	for i := range pods.Items {
		pod := &pods.Items[i]
		m.podLabels[pod.Name] = pod.Labels
	}
}
```

Call `refreshPodLabels` in `Start()` before the first sample, and every 60 seconds in the ticker loop.

The `kubeClient` is already available — check how `recordRestartBaseline` accesses it and follow the same pattern.

### Step 3: Use ResolveWorkloadName in sample()

In `sample()`, replace:
```go
workloadName := extractWorkloadName(podMetrics.Name)
```
with:
```go
m.mu.RLock()
labels := m.podLabels[podMetrics.Name]
m.mu.RUnlock()
workloadName := ResolveWorkloadName(podMetrics.Name, labels)
```

When `PodLevel` is true, keep the exact pod name match (don't change that path).

### Step 4: Update exposure collector

In `internal/exposure/collector.go`, replace the local `extractWorkloadName()` with a call to `metrics.ResolveWorkloadName()`. Since the exposure collector doesn't have pod labels available during neighbor aggregation, keep the heuristic fallback (pass nil labels):

```go
wlName := metrics.ResolveWorkloadName(pm.Name, nil)
```

Or, if `collectNeighbors()` can access the pod list, fetch labels there too.

### Step 5: Delete old extractWorkloadName

Remove the local `extractWorkloadName()` from both `latch.go` and `collector.go`. The logic now lives in `workload_name.go`.

### Step 6: Tests

Create `internal/metrics/workload_name_test.go`:

```go
package metrics

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveWorkloadName_AppLabel(t *testing.T) {
	labels := map[string]string{"app": "payment-api"}
	assert.Equal(t, "payment-api", ResolveWorkloadName("payment-api-7d8f9c4b6-abc12", labels))
}

func TestResolveWorkloadName_K8sNameLabel(t *testing.T) {
	labels := map[string]string{"app.kubernetes.io/name": "payment-api", "app": "wrong"}
	// app.kubernetes.io/name has higher priority than app
	assert.Equal(t, "payment-api", ResolveWorkloadName("payment-api-7d8f9c4b6-abc12", labels))
}

func TestResolveWorkloadName_CNPGLabel(t *testing.T) {
	labels := map[string]string{"cnpg.io/cluster": "payments-main-db"}
	assert.Equal(t, "payments-main-db", ResolveWorkloadName("payments-main-db-2", labels))
}

func TestResolveWorkloadName_NoLabels(t *testing.T) {
	// Falls back to heuristic
	assert.Equal(t, "payment-api", ResolveWorkloadName("payment-api-7d8f9c4b6-abc12", nil))
}

func TestResolveWorkloadName_EmptyLabels(t *testing.T) {
	assert.Equal(t, "payment-api", ResolveWorkloadName("payment-api-7d8f9c4b6-abc12", map[string]string{}))
}

func TestExtractWorkloadNameHeuristic_Deployment(t *testing.T) {
	assert.Equal(t, "payment-api", extractWorkloadNameHeuristic("payment-api-7d8f9c4b6-abc12"))
}

func TestExtractWorkloadNameHeuristic_Short(t *testing.T) {
	assert.Equal(t, "api-abc", extractWorkloadNameHeuristic("api-abc"))
}

func TestExtractWorkloadNameHeuristic_SingleSegment(t *testing.T) {
	assert.Equal(t, "api", extractWorkloadNameHeuristic("api"))
}
```

## Verification
```bash
go build ./...
go test -race -count=1 ./internal/metrics/... ./internal/exposure/...
go vet ./...
golangci-lint run
```

## Notes
- The `kubeClient` field may need to be added to `LatchMonitor` if it's not already there. Check `recordRestartBaseline()` — it already accesses pods via `m.kubeClient` or similar.
- Use `sync.RWMutex` for the pod labels map — `RLock` in sample() (hot path), `Lock` only in refresh.
- The refresh interval (60s) is a constant — define it as `const podLabelRefreshInterval = 60 * time.Second`.
- Do NOT change the `PodLevel` exact match path — that must remain as-is.
