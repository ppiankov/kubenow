# kubenow v0.3.0 — Pro-Monitor Hardening & Workload Coverage

## Status: DRAFT
## Authors: ppiankov
## Date: 2026-02-08

---

## Summary

v0.3.0 hardens the pro-monitor foundation shipped in v0.2.0 with three focused improvements: better UX for multi-container pods, support for burst workloads (CronJob/Job), and a migration path for policy schema evolution.

One sentence: **Extend pro-monitor to cover more workload types and prepare the policy layer for long-term evolution.**

---

## What This Is

- UX improvements for multi-container pods in the pro-monitor TUI
- Workload coverage expansion: CronJob and Job resource alignment
- Policy infrastructure hardening: versioning, migration, forward compatibility

## What This Is NOT

- Not a VPA or autoscaler — still one-shot, operator-initiated
- Not a controller or continuous enforcement loop
- Not multi-cluster support
- Not cost estimation or FinOps integration

## Out of Scope

These are explicitly excluded from v0.3.0:

- **No Prometheus-only mode.** Latch remains the required evidence source. Prometheus enhances confidence but does not replace latch.
- **No bulk apply.** One workload at a time. No batch mode.
- **No CRDs.** kubenow remains a client-side tool with standard RBAC.
- **No audit backend changes.** Filesystem remains the only audit backend. S3/git backends are future work.

---

## Feature 1: Multi-Container TUI UX

### Problem

The recommendation engine (v0.2.0) handles multiple containers independently, but the TUI renders all containers in a stacked vertical layout. For pods with many sidecars (5+ containers — common with service meshes like Istio), the view overflows and becomes unusable. Operators cannot focus on the container they care about.

### Design

#### Container Selector

When a pod has more than one container, the TUI shows a tab bar at the top of the recommendation view:

```
┌─── PRO-MONITOR COMPLETE ──── ALIGNMENT READY ─────────────────────┐
│                                                                     │
│  Containers: [payment-api] [istio-proxy] [otel-collector] [vault]  │
│              ^^^^^^^^^^^ active                                     │
│                                                                     │
│  ALIGNMENT — payment-api                                            │
│  CPU Requests:    500m  →  180m   (↓ 64%)                          │
│  Memory Requests: 512Mi →  290Mi  (↓ 43%)                          │
│  ...                                                                │
```

#### Key Bindings

| Key | Action |
|-----|--------|
| `Tab` / `Shift+Tab` | Cycle through containers |
| `1`–`9` | Jump to container by index |
| `/` | Filter containers by name |

#### Summary View

A new `[S]ummary` key shows all containers in a compact table:

```
┌─────────────────┬───────────┬───────────┬──────────┬────────┐
│ Container       │ CPU Req   │ Mem Req   │ Safety   │ Action │
├─────────────────┼───────────┼───────────┼──────────┼────────┤
│ payment-api     │ 500m→180m │ 512Mi→290Mi│ CAUTION │ change │
│ istio-proxy     │ 100m→100m │ 128Mi→128Mi│ SAFE    │ none   │
│ otel-collector  │ 200m→80m  │ 256Mi→150Mi│ SAFE    │ change │
│ vault-agent     │ 50m→50m   │ 64Mi→64Mi │ SAFE    │ none   │
└─────────────────┴───────────┴───────────┴──────────┴────────┘
```

#### Apply Scope

When applying, all container changes are applied atomically (single SSA patch). The confirmation prompt lists all containers with changes:

```
APPLY RESOURCE CHANGES

  Target: production/deployment/payment-api

  payment-api:     CPU 500m→180m  MEM 512Mi→290Mi
  otel-collector:  CPU 200m→80m   MEM 256Mi→150Mi
  (2 containers unchanged: istio-proxy, vault-agent)

  Type "apply" to confirm:  _
```

#### Export Scope

Export includes all containers. No per-container export — the patch is always for the full pod spec.

### Tests

- Tab navigation with 1, 3, 5 containers
- Summary view rendering
- Filter by name
- Apply confirmation lists only changed containers
- Single-container pods show no tab bar

---

## Feature 2: CronJob / Job Support

### Problem

CronJobs and Jobs have fundamentally different resource patterns compared to long-running workloads:

- **Burst then idle**: A Job uses resources intensely for minutes/hours, then terminates. A CronJob repeats this pattern on a schedule.
- **No steady state**: Continuous latch sampling captures the idle period (no pods running), which poisons percentile calculations.
- **Startup dominates**: For short Jobs (< 5m), container startup is a significant fraction of total runtime.

The v0.2.0 latch strategy (continuous sampling over a time window) does not suit these workloads.

### Design

#### Run-Based Latch

Instead of sampling continuously over a time window, CronJob/Job latch observes **N completed runs** and computes percentiles across runs:

```bash
# Observe the next 5 runs of a CronJob
kubenow pro-monitor latch cronjob/data-export --runs 5

# Observe a single Job execution
kubenow pro-monitor latch job/migration-batch --runs 1
```

#### How It Works

1. **CronJob**: kubenow watches for new Job creation by the CronJob controller. For each Job:
   - Wait for pod(s) to reach Running state
   - Sample at the configured interval until the pod(s) complete or fail
   - Record per-run metrics: peak CPU, peak memory, duration, exit code
   - After N runs, compute cross-run percentiles

2. **Job**: kubenow watches the specified Job. If it's already running, starts sampling immediately. If it hasn't started, waits for it to start.

#### Workload Ref Syntax

```
cronjob/<name>     # Observe CronJob runs
job/<name>          # Observe a specific Job
```

CronJobs resolve to their child Jobs automatically. kubenow never modifies the CronJob's `spec.jobTemplate` directly — it patches the CronJob object so future runs use the new resources.

#### Percentile Computation

Cross-run percentiles are computed differently from continuous latch:

| Metric | Continuous Latch (v0.2.0) | Run-Based Latch (v0.3.0) |
|--------|---------------------------|--------------------------|
| p95 CPU | 95th percentile of all samples | 95th percentile of per-run peak CPU values |
| p95 Memory | 95th percentile of all samples | 95th percentile of per-run peak memory values |
| max | Single highest sample | Highest peak across all runs |

This means: with 5 runs, the "p95" is effectively the second-highest peak. With 20 runs, it's statistically meaningful. The confidence score reflects this.

#### Confidence for Run-Based Latch

| Runs | Confidence |
|------|-----------|
| 1 | LOW |
| 2–4 | LOW |
| 5–9 | MEDIUM |
| 10+ | HIGH (if all runs succeeded) |

Failed runs are excluded from percentile computation but counted in the report. If >50% of observed runs fail, recommendation is blocked.

#### Safety Margins

Job/CronJob safety margins are more conservative than long-running workloads:

| Safety Rating | Long-Running | Job/CronJob |
|--------------|-------------|-------------|
| SAFE | 1.0x | 1.2x |
| CAUTION | 1.3x | 1.5x |
| RISKY | 1.5x | 2.0x |

Rationale: Jobs have less opportunity for graceful degradation. A memory limit too tight means the Job fails entirely, not just degrades.

#### TUI Adaptations

The latch progress view changes for run-based latch:

```
┌─── PRO-MONITOR ACTIVE ────────────────────────────────────────────┐
│ Run-based latching in progress.                                    │
├────────────────────────────────────────────────────────────────────┤
│                                                                    │
│  Workload: production/cronjob/data-export                         │
│  Runs: 3 / 5 completed   (2 in progress, 0 failed)               │
│                                                                    │
│  Run History:                                                      │
│    #1  ✓  3m22s  CPU peak=800m  MEM peak=1.2Gi  exit=0           │
│    #2  ✓  3m18s  CPU peak=750m  MEM peak=1.1Gi  exit=0           │
│    #3  ✓  3m25s  CPU peak=820m  MEM peak=1.3Gi  exit=0           │
│    #4  ⏳ running (1m12s elapsed)                                  │
│    #5  ⏳ waiting                                                   │
│                                                                    │
│  [q] Quit  [s] Show safety details                                │
└────────────────────────────────────────────────────────────────────┘
```

#### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--runs` | int | `5` | Number of completed runs to observe |
| `--run-timeout` | duration | `1h` | Max time to wait for a single run to complete |
| `--total-timeout` | duration | `48h` | Max total observation time |

#### Audit Bundle Differences

The `decision.json` for Job/CronJob includes:

```json
{
  "latch": {
    "mode": "run-based",
    "runs_observed": 5,
    "runs_succeeded": 5,
    "runs_failed": 0,
    "per_run_peaks": [
      {"run": 1, "cpu_peak": 0.8, "mem_peak_bytes": 1288490188, "duration": "3m22s", "exit_code": 0},
      ...
    ]
  }
}
```

### Tests

- Run-based latch with mock Job completions
- Cross-run percentile computation (known inputs)
- Confidence scoring by run count
- Failed run exclusion from percentiles
- CronJob → Job resolution
- Total timeout abort
- Patch targets CronJob's jobTemplate, not Job

---

## Feature 3: Policy Versioning and Migration

### Problem

The v0.2.0 policy uses `apiVersion: kubenow/v1alpha1`. As the policy schema evolves (new fields, changed defaults, removed fields), there is no strategy for:

- How kubenow handles a policy with an unknown apiVersion
- How operators migrate from v1alpha1 to v1beta1 (or v1)
- Whether unknown fields are silently ignored or rejected

### Design

#### Version Handling Strategy

kubenow adopts **strict version matching with forward-compatible parsing**:

1. **Known version**: Parse fully, validate all fields, apply defaults.
2. **Unknown version (newer)**: Parse known fields only, warn about unknown fields, refuse apply (suggest upgrading kubenow). Export still works.
3. **Unknown version (unrecognized format)**: Refuse to load. Error with suggestion.

```
$ kubenow pro-monitor validate-policy --policy policy.yaml

Policy: policy.yaml
  apiVersion: kubenow/v1beta1
  ⚠ Unknown policy version. kubenow 0.3.0 supports: v1alpha1, v1alpha2.
  ⚠ Parsed with forward compatibility (unknown fields ignored).
  ⚠ Apply is disabled until kubenow is upgraded. Export works normally.
```

#### Migration Command

```bash
# Show what would change
kubenow policy migrate --policy policy.yaml --to v1alpha2 --dry-run

# Migrate in place
kubenow policy migrate --policy policy.yaml --to v1alpha2
```

The migrate command:
- Reads the source policy
- Applies field renames, default changes, and structural transformations
- Preserves comments where possible (using a YAML round-trip library)
- Writes the migrated policy
- Shows a diff of changes

#### v1alpha2 Schema Changes (Planned)

| Change | v1alpha1 | v1alpha2 | Migration |
|--------|----------|----------|-----------|
| Add `min_confidence` | N/A | `apply.min_confidence: MEDIUM` | New field with default |
| Add `job_support` | N/A | `apply.job_support: { enabled: true, min_runs: 5 }` | New section with defaults |
| Rename `rate_window` | `rate_limits.rate_window` | `rate_limits.per_workload_window` | Rename |
| Add `policy_version` metadata | N/A | `metadata.migrated_from: v1alpha1` | Auto-set during migration |

#### Validation Enhancements

The `validate-policy` command gains version-aware validation:

```bash
# Validate against current version
kubenow pro-monitor validate-policy --policy policy.yaml

# Validate against a specific version
kubenow pro-monitor validate-policy --policy policy.yaml --schema v1alpha2

# Show all supported versions
kubenow pro-monitor validate-policy --list-versions
```

### Tests

- Load v1alpha1 policy with v1alpha1 parser (success)
- Load v1alpha2 policy with v1alpha1 parser (forward compat, warning)
- Load garbage apiVersion (reject)
- Migrate v1alpha1 → v1alpha2 (field renames, new defaults)
- Migrate preserves unknown fields (user extensions)
- Round-trip: migrate + validate = clean

---

## Execution Plan

3 PRs. Each is independently shippable.

### PR 1: Multi-container TUI UX

**Scope**: Tab-based container selector, summary view, filtered apply confirmation.

**Changes**:
- `internal/promonitor/ui.go` — container tab bar, summary table view
- `internal/promonitor/model.go` — active container state, tab/filter key handling
- `internal/promonitor/apply.go` — multi-container confirmation prompt

### PR 2: CronJob / Job support

**Scope**: Run-based latch mode, cross-run percentiles, CronJob/Job workload refs.

**Changes**:
- `internal/promonitor/workload.go` — add CronJob/Job ref parsing and resolution
- `internal/promonitor/latch_run.go` — new: run-based latch implementation
- `internal/promonitor/recommend.go` — adjusted safety margins for burst workloads
- `internal/promonitor/ui.go` — run history view
- `internal/audit/bundle.go` — run-based latch fields in decision.json
- `internal/cli/promonitor_latch.go` — `--runs`, `--run-timeout`, `--total-timeout` flags

### PR 3: Policy versioning and migration

**Scope**: Version registry, forward-compatible parsing, migration command.

**Changes**:
- `internal/policy/version.go` — new: version registry, compatibility checks
- `internal/policy/migrate.go` — new: migration engine
- `internal/policy/policy.go` — version-aware loading, unknown field handling
- `internal/cli/promonitor_migrate.go` — new: `kubenow policy migrate` command
- `internal/cli/promonitor_validate.go` — version flags, `--list-versions`

---

## Verification

```bash
go build ./...
go vet ./...
go test -race -count=1 ./...
```

---

*This document describes the design for kubenow v0.3.0. Status: DRAFT.*
