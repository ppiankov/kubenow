# Action Plan: CNPG Workload Support

## Context
CloudNativePG (CNPG) manages PostgreSQL clusters via a CRD (`postgresql.cnpg.io/v1 Cluster`).
Pods are owned directly by the Cluster resource — no Deployment or StatefulSet in between.
Pod names follow the pattern `<cluster-name>-<ordinal>` (e.g., `payments-main-db-1`, `payments-main-db-2`).

kubenow currently cannot:
1. **Latch** onto CNPG workloads — `normalizeKind()` rejects anything other than deployment/statefulset/daemonset
2. **Correctly extract workload names** from CNPG pod names — `extractWorkloadName()` strips last 2 dash-segments, turning `payments-main-db-2` into `payments` (should be `payments-main-db`)
3. **Display workload kind** in the spike table — shows `talala/payments-main` without indicating it's a CNPG Cluster

## Issue B: Workload name extraction

### Problem
`extractWorkloadName()` uses a blind heuristic: strip last 2 dash-segments.
This works for Deployment pods (`name-replicaset-hash-pod-hash`) but fails for:
- StatefulSet pods: `name-ordinal` (only 1 suffix to strip)
- CNPG pods: `name-ordinal` (same pattern as StatefulSet)
- DaemonSet pods: `name-hash` (only 1 suffix to strip)

### Fix: Label-based resolution (preferred)
Instead of guessing from pod names, read pod labels to determine the owning workload:

```
Pod labels → workload name:
  app.kubernetes.io/name          (standard)
  app.kubernetes.io/instance      (standard)
  app                              (common)
  cnpg.io/cluster                 (CNPG-specific)
```

Fallback to the current heuristic only when no label matches.

### Implementation
1. During `sample()` in `latch.go`, the Metrics API only returns pod name + container metrics (no labels)
2. Need a one-time pod list at latch start to build a `podName → workloadName` lookup map
3. On each sample, resolve workload name from the map instead of `extractWorkloadName()`
4. Refresh the map periodically (pods may restart with new names)

### Files
- `internal/metrics/latch.go` — add pod label resolution, cache map
- `internal/metrics/latch_test.go` — test the resolution logic

### Complexity: Medium
Requires changing the sampling hot path. Must not add latency — the label map is built async.

---

## Issue C: CNPG latch support

### Problem
Three functions block non-apps/v1 workloads:
1. `normalizeKind()` — rejects anything not deployment/statefulset/daemonset
2. `ValidateWorkload()` — only calls `AppsV1()` getters
3. `FetchContainerResources()` — only reads pod template from apps/v1 objects

CNPG Cluster has no pod template spec — container resources are defined in the CRD spec.

### Approach: Pod-level latch mode

Add a new workload kind `pod` that bypasses workload-level resolution entirely:

```bash
kubenow pro-monitor latch pod/payments-main-db-2 -n talala
```

This is the minimal viable approach:
- `normalizeKind()` accepts `pod` as a kind
- `ValidateWorkload()` checks the pod exists via `CoreV1().Pods().Get()`
- `FetchContainerResources()` reads resources from the pod spec directly
- Latch `WorkloadFilter` matches the exact pod name (no `extractWorkloadName`)

### Why pod-level first
1. Works for ANY CRD-managed workload (CNPG, Strimzi Kafka, MySQL Operator, etc.)
2. No dependency on CRD-specific APIs or dynamic client
3. User already knows the pod name from `kubenow monitor` output
4. Implements the RootOps principle: present evidence, let users decide

### Future: CRD-aware discovery (v0.4.0+)
Full CRD support would require:
- Dynamic client to resolve `ownerReferences` chain
- CRD-specific resource extraction (each operator defines resources differently)
- This is a larger effort and should be a separate milestone

### Implementation
1. `normalizeKind()` — add `"pod", "pods", "po"` → `"Pod"`
2. `ValidateWorkload()` — add `case "Pod":` that calls `CoreV1().Pods().Get()`
3. `FetchContainerResources()` — add `case "Pod":` that reads from `pod.Spec.Containers`
4. `latch.go` sample matching — when kind is Pod, match exact pod name instead of `extractWorkloadName`

### Files
- `internal/promonitor/workload.go` — extend normalizeKind, ValidateWorkload, FetchContainerResources
- `internal/metrics/latch.go` — add pod-level matching in sample()
- `internal/promonitor/workload_test.go` — new tests for pod kind
- `internal/metrics/latch_test.go` — test pod-level filtering

### Complexity: Medium-High
Touches the workload resolution chain end-to-end. Needs careful testing.

---

## Execution Order

| Phase | Task | Owner | Branch |
|-------|------|-------|--------|
| 1 | Tilde expansion (order 4) | Codex | `codex/tilde-expansion` |
| 2 | Pod-level latch support (issue C) | Local | `feat/pod-latch` |
| 3 | Label-based workload name resolution (issue B) | Codex candidate after pod-latch merges | `codex/label-resolution` |

Issue C (pod-level latch) should come before issue B (label resolution) because:
- Pod-level latch is immediately useful for CNPG and all CRD workloads
- Label resolution improves the spike table display but isn't blocking
- Pod-level latch changes the `WorkloadFilter` logic that label resolution also touches

## Not in scope
- Full CRD-aware discovery via dynamic client (v0.4.0+)
- CNPG-specific resource parsing from Cluster CRD spec
- Auto-detection of CNPG workloads in `kubenow monitor`
