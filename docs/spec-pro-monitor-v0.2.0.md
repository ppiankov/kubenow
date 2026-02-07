# kubenow v0.2.0 — Pro-Monitor & Resource Alignment

## Status: DRAFT
## Authors: ppiankov
## Date: 2026-02-07

---

## Table of Contents

- [Summary](#summary)
- [Terminology](#terminology)
- [What This Is](#what-this-is)
- [What This Is NOT](#what-this-is-not)
- [Out of Scope](#out-of-scope)
- [Motivation](#motivation)
- [Existing Foundation](#existing-foundation)
- [Design Principles](#design-principles)
- [Architecture Overview](#architecture-overview)
- [Mode Hierarchy](#mode-hierarchy)
- [Admin Policy File](#admin-policy-file)
- [Pro-Monitor Mode](#pro-monitor-mode)
- [Recommendation Engine](#recommendation-engine)
- [Apply Path](#apply-path)
- [Export Path](#export-path)
- [Audit Bundles](#audit-bundles)
- [Revert Philosophy](#revert-philosophy)
- [CLI Interface](#cli-interface)
- [TUI Behavior](#tui-behavior)
- [Data Structures](#data-structures)
- [Failure Modes](#failure-modes)
- [Security Considerations](#security-considerations)
- [Implementation Phases](#implementation-phases)
- [Open Questions](#open-questions)

---

## Summary

v0.2.0 adds **pro-monitor mode**: a high-resolution, per-workload observation mode that produces actionable resource alignment recommendations backed by latch evidence. When an admin policy file is present and guardrails are satisfied, operators can apply bounded request/limit changes to live workloads — or export patches for GitOps workflows.

One sentence: **Turn observation into defensible action without guessing.**

---

## Terminology

| Term | Definition |
|------|-----------|
| **Latch** | A time-bounded, high-frequency sampling session targeting a single workload. Uses the K8s Metrics API (not Prometheus) to capture point-in-time resource usage at 1–30s intervals. Produces sample-based percentiles. |
| **Pro-monitor** | A kubenow mode that combines latch evidence with optional Prometheus historical data to produce resource alignment recommendations. Entered explicitly via CLI. |
| **Alignment** | A recommended change to a workload's resource requests and/or limits, backed by latch evidence and safety analysis. Not a prescription — an informed suggestion. |
| **Audit bundle** | A directory of files (before.yaml, after.yaml, diff.patch, decision.json) created for every apply operation. The receipt that proves what changed, why, and who did it. |
| **Admin policy** | A YAML file written by administrators that defines what kubenow is allowed to do. kubenow reads it, never writes it. Controls apply permissions, guardrail bounds, audit storage, and identity requirements. |
| **Safety rating** | A deterministic classification (SAFE, CAUTION, RISKY, UNSAFE) based on OOMKills, restarts, throttling, spike patterns, and workload type. Determines safety margins and apply eligibility. |
| **Confidence** | A score (HIGH, MEDIUM, LOW) reflecting the quality and breadth of evidence behind a recommendation. Driven by latch duration, Prometheus availability, and sample count. |

---

## What This Is

- An extension of kubenow's existing `monitor` and `requests-skew` capabilities
- A controlled path from "your requests are wrong" to "here's what they should be, with evidence"
- A surgical, per-workload tool that requires explicit opt-in at every level
- Export-first: YAML patches and diffs are always available, live apply is gated

## What This Is NOT

- Not a VPA replacement or autoscaler
- Not a controller that continuously manages resources
- Not "safe by default" — safety comes from evidence and guardrails, not magic
- Not a bulk operation tool — one workload at a time
- Not prescriptive — shows evidence and lets operators decide
- No revert button — if alignment needs a revert, the recommendation was wrong

## Out of Scope

These are explicitly excluded from v0.2.0 and are not planned:

- **No automatic continuous enforcement.** kubenow is one-shot "align now", not a controller loop.
- **No bulk apply.** One workload at a time. No `--all-namespaces` apply. No batch mode.
- **No CRDs required.** kubenow operates with standard K8s RBAC, no custom resources to install.
- **No cluster-side components for pro-monitor.** Latch runs client-side against the Metrics API.
- **No VPA integration.** kubenow does not read, write, or coordinate with VerticalPodAutoscaler objects.
- **No cost estimation.** Resource alignment is about correctness, not dollars. Cost integration is a separate future concern.
- **No multi-cluster.** Pro-monitor targets one cluster (the current kube context) at a time.

---

## Motivation

Requests and limits are always wrong. They are:

- Copied from Helm charts written years ago
- Tuned once and forgotten
- Drifted by workload changes
- Cargo-culted across namespaces

The industry answer is VPA (nobody trusts it), dashboards (nobody checks them), or periodic audits (once, then rot). The problem never goes away. It only drifts.

kubenow already detects this drift via `requests-skew`. What's missing is a path from detection to action that doesn't require operators to manually translate analysis output into kubectl patches while cross-referencing dashboards.

---

## Existing Foundation

v0.2.0 builds on existing kubenow components, not new abstractions.

| Component | Location | What It Provides |
|-----------|----------|-----------------|
| `requests-skew` analyzer | `internal/analyzer/requests_skew.go` | Skew ratios, percentile metrics (p50/p95/p99/p999/max), impact scores, safety ratings, quota context |
| `SafetyAnalysis` model | `internal/models/resource_usage.go` | OOMKill/restart tracking, CPU throttle detection, ultra-spike heuristics, AI workload patterns, safety margins (1.0x–2.5x) |
| `LatchMonitor` | `internal/metrics/latch.go` | High-frequency sampling (1–30s), per-workload spike data, critical signal detection (OOMKills, restarts, evictions, termination reasons, exit codes) |
| Monitor TUI | `internal/monitor/` | Bubbletea real-time UI, problem watching, severity-based display, search/filter/export |
| Baseline/Drift | `internal/baseline/baseline.go` | Snapshot save/load, drift comparison, regression detection |
| Config system | Viper + `~/.kubenow.yaml` | CLI flags, config file, env vars |
| Prometheus client | `internal/metrics/prometheus.go` | PromQL queries, percentile aggregation, health checks |

**Key insight**: The recommendation engine already exists (`requests-skew` + `SafetyAnalysis`). The latch evidence collector already exists (`LatchMonitor`). What's missing is:

1. A TUI mode that fuses them into an actionable workflow
2. An admin policy layer that gates mutation
3. An apply path with bounded changes and audit trails
4. An export path for GitOps

---

## Design Principles

These are non-negotiable. If any are violated, the feature is broken.

1. **No action without evidence.** If latch data is absent or stale, the apply button is disabled. Period.
2. **No mutation without admin policy.** If the admin policy file is missing, pro-monitor works in suggest + export mode. Apply is structurally impossible. Export is always available — disabling safe behavior (downloading diffs) to punish the absence of dangerous behavior (live apply) is backwards.
3. **One workload at a time.** No bulk operations. No `--namespace` apply. Surgical, scoped, intentional.
4. **Loud diffs.** Every recommendation shows before/after/why/confidence. If an operator can't explain why values changed, the tool has failed.
5. **Receipts or no surgery.** If kubenow can't write an audit bundle, it refuses to apply. Export does not require audit bundles.
6. **Minimize the need for rollback; don't trivialize it.** No revert button. Receipts enable manual rollback via `kubectl apply -f before.yaml`.

### The Actionable Invariant

This is the single line that defines whether apply is permitted. If any term is false, apply is blocked. If apply ever proceeds with any term false, it is a bug.

```
ACTIONABLE = latch_fresh
           ∧ latch_duration ≥ policy.min_latch_duration
           ∧ latch_valid (no invalidation from pod churn)
           ∧ safety_rating ≥ policy.min_safety_rating
           ∧ namespace ∉ policy.namespaces.deny
           ∧ delta ≤ policy.max_delta_percent
           ∧ ssa_no_conflict
           ∧ hpa_acknowledged (if hpa_present)
           ∧ audit_writable
           ∧ identity_recorded
           ∧ rate_limit_ok
           ∧ policy.global.enabled
           ∧ policy.apply.enabled
```

This invariant is checked atomically before the confirmation prompt. It is logged to `decision.json` as `guardrails_passed` (all terms) or `denial_reasons` (failed terms). Export is not gated by this invariant — only apply.

---

## Architecture Overview

```
┌──────────────────────────────────────────────────────────────────────┐
│                        kubenow CLI (Cobra)                           │
├──────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │                   Pro-Monitor Mode (TUI)                     │    │
│  │                                                              │    │
│  │  ┌─────────────┐  ┌──────────────┐  ┌──────────────────┐   │    │
│  │  │ Latch       │  │ Requests-Skew│  │  Admin Policy    │   │    │
│  │  │ Monitor     │→ │ Analyzer     │→ │  Engine          │   │    │
│  │  │ (evidence)  │  │ (recommend)  │  │  (gate)          │   │    │
│  │  └─────────────┘  └──────────────┘  └──────────────────┘   │    │
│  │         │                │                    │              │    │
│  │         ▼                ▼                    ▼              │    │
│  │  ┌──────────────────────────────────────────────────────┐   │    │
│  │  │               Alignment Result                        │   │    │
│  │  │  • recommended requests/limits                        │   │    │
│  │  │  • confidence score                                   │   │    │
│  │  │  • safety rating + warnings                           │   │    │
│  │  │  • before/after diff                                  │   │    │
│  │  └──────────────────────────────────────────────────────┘   │    │
│  │         │                                                    │    │
│  │         ├──────────────────┬─────────────────────┐          │    │
│  │         ▼                  ▼                     ▼          │    │
│  │  ┌────────────┐   ┌──────────────┐   ┌──────────────────┐  │    │
│  │  │ Export     │   │ Apply        │   │ Suggest          │  │    │
│  │  │ YAML/Patch │   │ (policy req) │   │ (always)         │  │    │
│  │  │ (always)   │   │              │   │                  │  │    │
│  │  └────────────┘   └──────────────┘   └──────────────────┘  │    │
│  │                          │                                   │    │
│  │                          ▼                                   │    │
│  │                   ┌──────────────┐                           │    │
│  │                   │ Audit Bundle │                           │    │
│  │                   │ before.yaml  │                           │    │
│  │                   │ after.yaml   │                           │    │
│  │                   │ decision.json│                           │    │
│  │                   │ diff.patch   │                           │    │
│  │                   └──────────────┘                           │    │
│  └─────────────────────────────────────────────────────────────┘    │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
         │                      │                    │
         ▼                      ▼                    ▼
  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐
  │ K8s Metrics  │     │ Prometheus   │     │ K8s API      │
  │ API (latch)  │     │ (historical) │     │ (apply)      │
  └──────────────┘     └──────────────┘     └──────────────┘
```

---

## Mode Hierarchy

kubenow has three monitoring levels with increasing capability and risk:

```
┌────────────────┬────────────────────────────────────────────────────────────┐
│ Level          │ Capabilities                                               │
├────────────────┼────────────────────────────────────────────────────────────┤
│ monitor        │ Passive. Watches for problems (OOMKill, CrashLoop, etc.)  │
│                │ No resource analysis. No mutations. Always available.      │
│                │ Zero risk.                                                 │
├────────────────┼────────────────────────────────────────────────────────────┤
│ pro-monitor    │ Active. Runs latch on a specific workload.                │
│                │ Produces alignment recommendations with evidence.          │
│                │ Shows before/after diffs.                                  │
│                │ Export patches/YAML: always available.                     │
│                │ Apply: disabled (no admin policy).                         │
│                │ Read-only risk (Metrics API load).                         │
├────────────────┼────────────────────────────────────────────────────────────┤
│ pro-monitor    │ Same as above, plus:                                      │
│ + admin policy │ Apply bounded changes to live workloads (gated).          │
│                │ Write audit bundles (required for apply).                  │
│                │ Mutation risk (controlled, bounded, audited).              │
└────────────────┴────────────────────────────────────────────────────────────┘
```

Apply capability is purely policy-driven. The `pro-monitor` command is always the same — what the operator can **do** after latch completion depends on whether an admin policy file exists and what it permits.

### Transition Rules

- `monitor` → `pro-monitor`: Operator runs `kubenow pro-monitor latch <workload>`.
- `pro-monitor` → export: Always available after latch completes. No policy required.
- `pro-monitor` → apply: Only after latch completes + admin policy present + all guardrails pass.

---

## Admin Policy File

The admin policy file is the central authority for pro-monitor behavior. kubenow reads it. kubenow never writes it. Admins own it.

### Location & Discovery

1. `/etc/kubenow/policy.yaml` (default)
2. `$KUBENOW_POLICY` environment variable (explicit override)

No auto-discovery. No fallback. If absent, pro-monitor runs in suggest-only mode.

### Schema

```yaml
# /etc/kubenow/policy.yaml
# kubenow admin policy — written by administrators, read-only to operators

apiVersion: kubenow/v1alpha1
kind: Policy

global:
  enabled: true                    # Kill switch. false = all mutation disabled.

audit:
  backend: filesystem              # "filesystem" for v0.2.0. Future: s3, git.
  path: /var/lib/kubenow/audit     # Must be writable. Apply refused if not.
  retention_days: 90               # Informational. kubenow does not auto-delete.

apply:
  enabled: true                    # Master switch for live apply.
  require_latch: true              # Require latch evidence before apply. Always true in v0.2.0.
  max_request_delta_percent: 30    # Max change to requests (up or down). 0 = unlimited.
  max_limit_delta_percent: 30      # Max change to limits. 0 = unlimited.
  allow_limit_decrease: false      # Lowering limits risks OOMKill. Default: refuse.
  min_latch_duration: 1h           # Minimum latch window to trust evidence.
  max_latch_age: 7d                # Latch data older than this = stale. Apply refused.
  min_safety_rating: CAUTION       # Minimum safety rating to allow apply. SAFE | CAUTION.
                                   # RISKY and UNSAFE always blocked.

namespaces:
  deny:                            # Never touch these. Hardcoded + admin-defined.
    - kube-system
    - kube-public
    - kube-node-lease
  # allow is implicit: anything not in deny is allowed.
  # If you want an allowlist model, add:
  # allow:
  #   - prod-*
  #   - staging-*

identity:
  require_kube_context: true       # Log kube context with every apply.
  record_os_user: true             # Log OS username (whoami).
  record_git_identity: false       # Log git user.name/user.email. Optional.

rate_limits:
  max_applies_per_hour: 10         # Cluster-wide apply rate limit.
  max_applies_per_workload: 1      # Per-workload rate limit per window.
  rate_window: 24h                 # Window for per-workload rate limit.
```

### Rate Limiting Implementation

Rate limit state is stored as files in the audit directory, not in memory.

**File layout**:
```
<audit_path>/.ratelimit/
  cluster.json          # Cluster-wide counter (applies per hour)
  <workload-uid>.json   # Per-workload counter (applies per window)
```

**Keying**: Per-workload rate limits are keyed by **workload UID** (from the K8s object metadata), not by name. This prevents bypass via delete + recreate, and correctly handles renamed deployments.

**Concurrency safety**: Rate limit files are updated using `flock(2)` advisory file locks. When multiple operators run kubenow concurrently:
- The first to acquire the lock reads, increments, and writes the counter.
- Others block briefly (100ms timeout), then either acquire or see the updated counter.
- If the lock cannot be acquired within 1s, apply is refused with: "Rate limit check timed out. Another kubenow instance may be applying."

**Counter format** (cluster.json example):
```json
{
  "window_start": "2026-02-07T14:00:00Z",
  "window_duration": "1h",
  "count": 3,
  "entries": [
    {"at": "2026-02-07T14:05:00Z", "workload": "prod/deployment/api", "user": "jane"},
    {"at": "2026-02-07T14:12:00Z", "workload": "prod/deployment/web", "user": "jane"},
    {"at": "2026-02-07T14:30:00Z", "workload": "staging/deployment/api", "user": "bob"}
  ]
}
```

Windows are tumbling (reset when expired), not sliding. This is simpler and sufficient for the rate limits involved.

### Behavior When Policy Is Absent

```
$ kubenow pro-monitor latch deployment/api --duration 2h

[kubenow] Pro-monitor mode active.
[kubenow] No admin policy found at /etc/kubenow/policy.yaml
[kubenow] Mode: suggest + export. Live apply disabled.
[kubenow] To enable apply, an administrator must create the policy file.
[kubenow] Latching deployment/api for 2h (sampling every 5s)...
```

No silent fallback. No degraded mode without telling the operator. Export always works — policy gates mutation, not information.

### Behavior When Policy Is Present

```
$ kubenow pro-monitor latch deployment/api --duration 2h

[kubenow] Pro-monitor mode active.
[kubenow] Admin policy: /etc/kubenow/policy.yaml
[kubenow] Audit backend: filesystem (/var/lib/kubenow/audit)
[kubenow] Apply: enabled (bounded ±30%, limit decrease: denied)
[kubenow] Latching deployment/api for 2h (sampling every 5s)...
```

---

## Pro-Monitor Mode

### Entry Point

Pro-monitor is entered via CLI with an explicit workload target and latch duration:

```bash
kubenow pro-monitor latch <workload-ref> --duration <duration> [flags]
```

Where `<workload-ref>` is: `<kind>/<name>` in the current namespace, or `<namespace>/<kind>/<name>`.

Examples:
```bash
# Latch a deployment for 2 hours
kubenow pro-monitor latch deployment/payment-api --duration 2h

# With explicit namespace
kubenow pro-monitor latch -n production deployment/payment-api --duration 2h

# With Prometheus for historical context
kubenow pro-monitor latch deployment/payment-api --duration 2h \
  --prometheus-url http://prometheus:9090

# Shorter latch for quick check (minimum 15m)
kubenow pro-monitor latch deployment/payment-api --duration 15m
```

### What Happens During Latch

1. **Validate workload exists** — fail fast if not found.
2. **Check admin policy** — determine suggest-only vs full mode.
3. **Start latch monitor** — high-frequency sampling via K8s Metrics API.
4. **Fetch historical data** — if Prometheus is available, get `requests-skew` analysis for the workload.
5. **Display TUI** — red-framed pro-monitor view showing:
   - Live resource usage (CPU/memory) with sparkline
   - Current requests/limits vs observed usage
   - Latch progress (samples collected, time remaining)
   - Critical signals (OOMKills, restarts, throttling) as they happen
6. **On completion** — compute recommendations, show alignment result.

### Latch Constraints

| Parameter | Min | Max | Default |
|-----------|-----|-----|---------|
| Duration | 15m | 168h (7d) | 2h |
| Sample interval | 1s | 30s | 5s |
| Workloads per latch | 1 | 1 | 1 |

One workload at a time. No exceptions.

---

## Recommendation Engine

The recommendation engine combines latch evidence with historical Prometheus data (when available) to produce alignment recommendations.

### Input Sources

| Source | What It Provides | Required | Percentile Method |
|--------|-----------------|----------|-------------------|
| Latch data | Real-time p50/p95/p99/max, spike counts, OOMKills, restarts, throttling | Yes | **Sample-based**: computed from N discrete point-in-time samples via K8s Metrics API. Accuracy depends on sample count and interval. A 2h latch at 5s interval = 1440 samples. |
| Prometheus | 30-day historical p50/p95/p99/p999/max, trend data | No (recommended) | **Time-series-based**: computed via `quantile_over_time()` PromQL over continuous scrape data (typically 15–30s resolution). Higher fidelity for long windows. |
| K8s API | Current requests/limits, workload type, pod count | Yes | N/A |
| Safety analysis | Safety rating, warnings, safety margin | Yes (computed) | N/A |

**Important distinction**: Latch percentiles and Prometheus percentiles are not equivalent. Latch samples are point-in-time snapshots from the K8s Metrics API, which itself aggregates over a short window (typically ~30s). This means:

- Latch cannot capture sub-second bursts (but can detect their statistical signature via the ultra-spike heuristic).
- Latch p99 with 100 samples is statistically weaker than Prometheus p99 over 30 days of continuous scraping.
- When both sources are available, the recommendation engine takes the **max** of each percentile to be conservative.

Confidence scoring reflects this: latch-only = LOW confidence, latch + Prometheus = MEDIUM/HIGH.

### Recommendation Algorithm

CPU and memory are computed independently throughout. The algorithm runs once per container.

```
1. Collect observed metrics (per resource: CPU and memory separately):

   From latch (sample-based):
   - latch_cpu_p95, latch_cpu_p99, latch_cpu_max
   - latch_mem_p95, latch_mem_p99, latch_mem_max

   From Prometheus (time-series-based, if available):
   - prom_cpu_p95, prom_cpu_p99, prom_cpu_p999, prom_cpu_max
   - prom_mem_p95, prom_mem_p99, prom_mem_p999, prom_mem_max

2. Select the envelope (conservative, per resource):
   - effective_cpu_p95  = max(latch_cpu_p95, prom_cpu_p95)
   - effective_cpu_p99  = max(latch_cpu_p99, prom_cpu_p99)
   - effective_cpu_p999 = prom_cpu_p999 (latch cannot compute p999 reliably)
   - effective_cpu_max  = max(latch_cpu_max, prom_cpu_max)
   (same pattern for memory)

3. Compute safety margin (from SafetyAnalysis):
   - SAFE:    1.0x  (no extra headroom)
   - CAUTION: 1.3x  (30% headroom)
   - RISKY:   1.5x  (50% headroom)
   - UNSAFE:  N/A   (no recommendation produced)
   - Override: ultra-spikes detected → 2.5x, AI workload patterns → 2.0x

4. Compute recommended requests (per resource):
   - recommended_cpu_request = effective_cpu_p95 * safety_margin
   - recommended_mem_request = effective_mem_p95 * safety_margin

5. Compute recommended limits (per resource):

   CPU limits use p999 (not max), because CPU max is typically:
   - startup spikes, measurement noise, or single-sample bursts
   - CPU is compressible — throttling is preferable to inflated limits
   - recommended_cpu_limit = effective_cpu_p999 * safety_margin
   - If p999 unavailable (no Prometheus): effective_cpu_p99 * safety_margin * 1.5
   - Burst cap: cpu_limit <= current_cpu_limit * 2 (never more than double)

   Memory limits use p99 with a higher margin, because memory is NOT compressible:
   - recommended_mem_limit = effective_mem_p99 * safety_margin * 1.2
   - Floor: mem_limit >= effective_mem_max (never below observed max)
   - Burst cap: mem_limit <= current_mem_limit * 2

   Both resources:
   - Floor: limit >= request (always)
   - Ceiling: limit increase capped at 2x current (prevents insane inflation)

6. Apply admin policy bounds:
   - If request delta exceeds max_request_delta_percent: cap at boundary, mark as capped
   - If limit delta exceeds max_limit_delta_percent: cap at boundary, mark as capped
   - If limit decrease and allow_limit_decrease=false: keep current limit
   - If safety rating below min_safety_rating: refuse recommendation entirely

7. Compute confidence:
   - HIGH:   latch >= 24h AND Prometheus 30d available AND safety SAFE AND samples >= 5000
   - MEDIUM: latch >= 2h AND (Prometheus available OR samples >= 1000)
   - LOW:    latch < 2h OR (no Prometheus AND samples < 1000)
```

### Why Limits Use p999/p99 Instead of Max

Raw `max` is misleading for limit computation:
- A single CPU spike to 4x normal during container startup does not justify a 4x limit forever.
- Memory max may reflect a one-time allocation pattern that never repeats.
- Using max * safety_margin can produce limits 10x the request, which is politically ugly and wastes quota.

Instead:
- **CPU limits** use p999 (captures "real burst ceiling" without startup noise).
- **Memory limits** use p99 with an additional 1.2x factor, floored by observed max (because OOMKill is irreversible).
- Both are capped at 2x current limit to prevent runaway inflation.

### Recommendation Output

```
┌─────────────────────────────────────────────────────────────────┐
│  ALIGNMENT RECOMMENDATION — deployment/payment-api              │
│  Confidence: MEDIUM   Safety: CAUTION   Latch: 2h (360 samples)│
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  CPU Requests:    500m  →  180m   (↓ 64%)                       │
│  CPU Limits:      1000m →  1000m  (unchanged — limit decrease   │
│                                    denied by policy)             │
│  Memory Requests: 512Mi →  290Mi  (↓ 43%)                       │
│  Memory Limits:   1Gi   →  1Gi    (unchanged)                   │
│                                                                  │
│  Evidence:                                                       │
│    CPU  p95=120m  p99=140m  max=380m  spikes=3  safety=1.3x    │
│    MEM  p95=210Mi p99=240Mi max=400Mi spikes=0  safety=1.3x    │
│                                                                  │
│  Warnings:                                                       │
│    ⚠ 2 restarts in latch window                                 │
│    ⚠ CPU p99 usage at 28% of current request                    │
│                                                                  │
│  ┌────────────┐  ┌────────────┐  ┌──────────────────────────┐  │
│  │ [E]xport   │  │ [A]pply    │  │ [Q]uit (no action)       │  │
│  └────────────┘  └────────────┘  └──────────────────────────┘  │
│                                                                  │
│  Apply requires: admin policy ✓  audit writable ✓  bounded ✓   │
└─────────────────────────────────────────────────────────────────┘
```

---

## Apply Path

Apply patches the owning controller (Deployment, StatefulSet, DaemonSet) — never the Pod directly.

### Preconditions (All Must Pass)

| # | Precondition | Failure Behavior |
|---|-------------|-----------------|
| 1 | Admin policy exists and `global.enabled=true` | Apply button disabled. Message: "No admin policy." |
| 2 | `apply.enabled=true` in policy | Apply button disabled. Message: "Apply disabled by admin." |
| 3 | Latch data exists for this workload | Apply button disabled. Message: "Latch required." |
| 4 | Latch data is fresh (`age < max_latch_age`) | Apply button disabled. Message: "Latch data stale (Xd old)." |
| 5 | Latch duration meets `min_latch_duration` | Apply button disabled. Message: "Latch too short (Xm < Ym)." |
| 6 | Safety rating >= `min_safety_rating` | Apply button disabled. Message: "Safety rating too low (RISKY)." |
| 7 | Workload namespace not in `namespaces.deny` | Apply button disabled. Message: "Namespace denied by policy." |
| 8 | Delta within `max_request_delta_percent` | Values capped at boundary. Warning shown. |
| 9 | Limit decrease respects `allow_limit_decrease` | Limit kept at current value. Warning shown. |
| 10 | Rate limit not exceeded | Apply button disabled. Message: "Rate limit (X/hr)." |
| 11 | Audit path writable | Apply button disabled. Message: "Audit storage not writable." |
| 12 | Identity can be recorded | Apply button disabled. Message: "Cannot record operator identity." |
| 13 | No HPA conflict (or explicit override) | Apply blocked by default. Message: "HPA targets this workload. Use --acknowledge-hpa to proceed." |

If **any** precondition fails, the apply button is disabled with a specific message. Export is always available.

### HPA Guardrail (Non-Negotiable)

Changing resource requests on a workload targeted by a HorizontalPodAutoscaler directly affects:
- HPA scaling decisions (when using CPU/memory resource metrics)
- Cluster autoscaler packing assumptions
- Scheduling density across nodes

kubenow handles this as follows:

**Detection**: Before computing recommendations, kubenow checks if an HPA targets the workload (by `scaleTargetRef`).

**If HPA is present**:
- The TUI shows a persistent warning: `⚠ HPA detected: <hpa-name> (targets CPU at 70%)`
- The recommendation output includes a section explaining how the proposed request change affects HPA behavior (e.g., "Reducing CPU request from 500m to 180m means HPA will scale at 126m actual usage instead of 350m")
- **Apply is blocked by default.** The operator must pass `--acknowledge-hpa` at latch time to enable apply after completion.
- Export is always available (HPA interaction is documented in export comments).

**Rationale**: This is not optional. Silently changing requests under an HPA is a guaranteed incident for anyone using `targetCPUUtilizationPercentage`. Making the operator explicitly acknowledge this is the minimum responsible behavior.

### Apply Sequence

```
1. Operator presses [A]pply in TUI.

2. Confirmation prompt (not dismissable):
   ┌──────────────────────────────────────────────────────────┐
   │  APPLY RESOURCE CHANGES                                   │
   │                                                           │
   │  Target: production/deployment/payment-api                │
   │                                                           │
   │  CPU Requests:    500m  →  180m                           │
   │  Memory Requests: 512Mi →  290Mi                          │
   │                                                           │
   │  This will trigger a rolling restart.                     │
   │                                                           │
   │  Type "apply" to confirm:  _                              │
   └──────────────────────────────────────────────────────────┘

3. Write audit bundle BEFORE applying (fail = abort).

4. Read current manifest from K8s API (the "before" snapshot).

5. Compute patch (server-side apply object for the controller's
   .spec.template.spec.containers[].resources).

6. Apply via Server-Side Apply (SSA):
   - Field manager: "kubenow"
   - Force: false (never force — if another manager owns the field, fail)
   - On conflict: abort, record conflict details in decision.json, show in TUI
   - This means: if Helm/ArgoCD/Flux owns the resources field, kubenow will
     fail with a conflict error rather than silently taking ownership.
     This is correct — the operator should resolve via their GitOps tool.

7. Verify patch was accepted (GET the object, compare resources field).

8. Update audit bundle with "after" snapshot and result.

9. Display result:
   ┌──────────────────────────────────────────────────────────┐
   │  ✓ Applied successfully                                   │
   │                                                           │
   │  Audit bundle: /var/lib/kubenow/audit/                    │
   │    2026-02-07T14-22-01Z__prod__deploy__payment-api/       │
   │                                                           │
   │  Rollback (if needed):                                    │
   │    kubectl apply -f before.yaml                           │
   └──────────────────────────────────────────────────────────┘
```

### What Gets Patched

Only the `.spec.template.spec.containers[*].resources` field of the owning controller:

```yaml
spec:
  template:
    spec:
      containers:
        - name: <container-name>
          resources:
            requests:
              cpu: "<recommended>"
              memory: "<recommended>"
            limits:
              cpu: "<recommended or unchanged>"
              memory: "<recommended or unchanged>"
```

Multiple containers in a pod: each container is analyzed and patched independently. InitContainers are never modified.

---

## Export Path

Export is always available after latch completion, regardless of admin policy.

### Export Formats

```bash
# Kubernetes patch file (default)
kubenow pro-monitor export deployment/payment-api --format patch

# Full manifest with changes applied
kubenow pro-monitor export deployment/payment-api --format manifest

# Diff only (unified format)
kubenow pro-monitor export deployment/payment-api --format diff

# JSON summary (for automation)
kubenow pro-monitor export deployment/payment-api --format json
```

### Patch Output Example

```yaml
# kubenow-alignment-patch.yaml
# Generated: 2026-02-07T14:22:01Z
# Workload: production/deployment/payment-api
# Confidence: MEDIUM  Safety: CAUTION
# Latch: 2h (360 samples, 2026-02-07T12:22:01Z to 2026-02-07T14:22:01Z)
#
# Apply with: kubectl apply -f kubenow-alignment-patch.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payment-api
  namespace: production
spec:
  template:
    spec:
      containers:
        - name: payment-api
          resources:
            requests:
              cpu: "180m"
              memory: "290Mi"
            limits:
              cpu: "1000m"
              memory: "1Gi"
```

---

## Audit Bundles

Every apply creates an audit bundle. If the bundle can't be written, apply is refused.

### Directory Structure

```
<audit_path>/
  <timestamp>__<namespace>__<kind>__<name>/
    before.yaml       # Full controller object before change (volatile fields stripped)
    after.yaml         # Full controller object after change (volatile fields stripped)
    diff.patch         # Unified diff (human-readable)
    decision.json      # Structured decision record
```

**before.yaml / after.yaml contents**: The full controller object (e.g., the entire Deployment spec), not just the resources field. This is required for rollback via `kubectl apply -f before.yaml` to produce a valid object.

Volatile fields are stripped before writing:
- `.metadata.resourceVersion` (changes on every update)
- `.metadata.generation` (server-managed)
- `.metadata.managedFields` (SSA bookkeeping, very large)
- `.status` (server-managed, not part of desired state)
- `.metadata.uid` (server-assigned, preserved in decision.json for attribution)
- `.metadata.creationTimestamp` (preserved in decision.json but stripped from the apply-able YAML to avoid warnings)

The resulting YAML is a clean, `kubectl apply`-able object.

Example:
```
/var/lib/kubenow/audit/
  2026-02-07T14-22-01Z__production__deployment__payment-api/
    before.yaml
    after.yaml
    diff.patch
    decision.json
```

### decision.json Schema

```json
{
  "schema_version": "v1alpha1",
  "timestamp": "2026-02-07T14:22:01Z",
  "kubenow_version": "0.2.0",

  "workload": {
    "kind": "Deployment",
    "name": "payment-api",
    "namespace": "production",
    "uid": "a1b2c3d4-e5f6-7890-abcd-ef1234567890"
  },

  "latch": {
    "id": "latch-2026-02-07T12-22-01Z-payment-api",
    "start": "2026-02-07T12:22:01Z",
    "end": "2026-02-07T14:22:01Z",
    "duration": "2h0m0s",
    "sample_count": 360,
    "sample_interval": "5s"
  },

  "evidence": {
    "cpu": {
      "p50": 0.08,
      "p95": 0.12,
      "p99": 0.14,
      "max": 0.38,
      "spike_count": 3,
      "avg": 0.07
    },
    "memory_bytes": {
      "p50": 188743680,
      "p95": 220200960,
      "p99": 251658240,
      "max": 419430400,
      "spike_count": 0,
      "avg": 178257920
    },
    "critical_signals": {
      "oom_kills": 0,
      "restarts": 2,
      "evictions": 0,
      "throttling_detected": false
    },
    "safety_rating": "CAUTION",
    "safety_margin": 1.3,
    "confidence": "MEDIUM"
  },

  "changes": {
    "cpu_request": { "before": "500m", "after": "180m", "delta_percent": -64 },
    "memory_request": { "before": "512Mi", "after": "290Mi", "delta_percent": -43 },
    "cpu_limit": { "before": "1000m", "after": "1000m", "delta_percent": 0 },
    "memory_limit": { "before": "1Gi", "after": "1Gi", "delta_percent": 0 }
  },

  "guardrails_passed": [
    "admin_policy_present",
    "apply_enabled",
    "latch_data_fresh",
    "latch_duration_sufficient",
    "safety_rating_acceptable",
    "namespace_allowed",
    "delta_within_bounds",
    "limit_decrease_policy_respected",
    "rate_limit_ok",
    "audit_writable",
    "identity_recorded"
  ],

  "identity": {
    "kube_context": "prod-cluster",
    "kube_user": "ops-jane",
    "os_user": "jane",
    "machine": "ops-workstation-3",
    "identity_confidence": "best_effort",
    "identity_source": "kubeconfig"
  },

  "cluster": {
    "id_hash": "sha256:abc123...",
    "server": "https://k8s.prod.internal:6443"
  },

  "result": {
    "status": "applied",
    "applied_at": "2026-02-07T14:22:05Z",
    "verified": true
  }
}
```

### Identity Attribution

Kubernetes does not reliably expose the authenticated human identity from a client-side CLI tool. kubenow uses best-effort attribution with explicit provenance:

| `identity_source` | How `kube_user` Is Obtained | Reliability |
|--------------------|-----------------------------|-------------|
| `kubeconfig` | Parsed from kubeconfig user stanza (`.users[].name`) | Low — often a service account name or exec plugin identifier, not a human name |
| `ssr` | `SelfSubjectReview` API (K8s 1.27+) | High — returns the authenticated identity as seen by the API server |
| `unknown` | Neither method succeeded | N/A — `kube_user` will be empty |

kubenow attempts SSR first. If the API is unavailable or RBAC denies it, falls back to kubeconfig parsing. The `identity_source` field records which method was used so auditors know the reliability of the `kube_user` value.

`os_user` (from `os/user.Current()`) and `kube_context` are always available and always recorded.

### Audit Integrity

- Bundle is created **before** apply (with `result.status: "pending"`).
- Updated **after** apply with final status.
- If apply fails, bundle records `result.status: "failed"` with error details.
- If bundle write fails, apply is aborted.

---

## Revert Philosophy

kubenow does not provide an automatic revert. All applied changes are fully recorded and can be reverted using standard Kubernetes tools.

### Why No Revert Button

- Tools that advertise rollback encourage risk-taking and normalize sloppiness.
- If a change needs a revert, the recommendation was wrong — fix the recommendation engine, not the UX.
- Safety comes from suggestion quality, not rollback speed.

### How Rollback Works

Every audit bundle contains `before.yaml`. Rollback is:

```bash
kubectl apply -f /var/lib/kubenow/audit/<bundle>/before.yaml
```

kubenow prints this command after apply as a convenience. It never executes it.

**Drift warning**: `before.yaml` captures the controller state at apply time. If the controller has been modified by other means since then (Helm upgrade, manual edit, another kubenow apply), applying `before.yaml` will overwrite those changes too. Operators should review the diff between `before.yaml` and the current live state before rolling back:

```bash
kubectl diff -f /var/lib/kubenow/audit/<bundle>/before.yaml
```

kubenow prints this hint alongside the rollback command.

---

## CLI Interface

### New Commands

```bash
# Enter pro-monitor with latch on a specific workload
kubenow pro-monitor latch <workload-ref> [flags]

# Export alignment recommendation (after latch completes)
kubenow pro-monitor export <workload-ref> [flags]

# Show latch status / results for a workload
kubenow pro-monitor status <workload-ref>

# Validate admin policy file
kubenow pro-monitor validate-policy [--policy <path>]
```

### Flags for `pro-monitor latch`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--duration` | duration | `2h` | Latch duration (min 15m, max 168h) |
| `--sample-interval` | duration | `5s` | Sample interval (min 1s, max 30s) |
| `--prometheus-url` | string | | Prometheus URL for historical data |
| `--k8s-service` | string | | K8s service for Prometheus port-forward |
| `--k8s-namespace` | string | | Namespace for Prometheus service |
| `--policy` | string | `/etc/kubenow/policy.yaml` | Admin policy file path |
| `--namespace`, `-n` | string | current context | Workload namespace |
| `--output` | string | `tui` | Output mode: `tui`, `json`, `quiet` |
| `--acknowledge-hpa` | bool | `false` | Required to enable apply when an HPA targets the workload |

### Flags for `pro-monitor export`

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--format` | string | `patch` | Export format: `patch`, `manifest`, `diff`, `json` |
| `--output-file`, `-o` | string | stdout | Write to file |
| `--namespace`, `-n` | string | current context | Workload namespace |

---

## TUI Behavior

### Red Frame

When pro-monitor is active, the TUI displays a persistent red border with a warning banner:

```
┌─── PRO-MONITOR ACTIVE ────────────────────────────────────────────┐
│ High-resolution latching in progress.                              │
│ Results may enable live resource changes.                          │
├────────────────────────────────────────────────────────────────────┤
│                                                                    │
│  Workload: production/deployment/payment-api                       │
│  Latch: 45m / 2h (37.5%)  Samples: 540                           │
│                                                                    │
│  ┌── Live Resource Usage ──────────────────────────────────┐      │
│  │  CPU:  ▁▂▁▃▁▂▅▂▁▁▃▁  avg=70m  p95=120m  max=380m     │      │
│  │  MEM:  ▃▃▃▃▃▃▃▃▃▃▃▃  avg=170Mi p95=210Mi max=400Mi   │      │
│  └─────────────────────────────────────────────────────────┘      │
│                                                                    │
│  Current Configuration:                                            │
│    CPU  request=500m  limit=1000m    (p95 is 24% of request)      │
│    MEM  request=512Mi limit=1Gi      (p95 is 41% of request)      │
│                                                                    │
│  Signals: ⚠ 2 restarts  ✓ 0 OOMKills  ✓ no throttling            │
│                                                                    │
│  [q] Quit  [p] Pause latch  [s] Show safety details               │
│                                                                    │
└─── sampling every 5s ─── policy: /etc/kubenow/policy.yaml ────────┘
```

The red frame:
- Cannot be dismissed
- Cannot be hidden
- Persists for the entire latch duration
- Communicates: "You are observing this workload with the intent to possibly change it"

### Post-Latch View

When the latch completes, the TUI transitions to the recommendation view (shown in the Recommendation Engine section above). The red frame remains but the banner changes to:

```
┌─── PRO-MONITOR COMPLETE ──── ALIGNMENT READY ─────────────────────┐
```

### Key Bindings (Pro-Monitor)

| Key | Action |
|-----|--------|
| `q` | Quit pro-monitor (no action taken) |
| `p` / Space | Pause/resume latch |
| `s` | Toggle safety details panel |
| `e` | Export recommendation (after latch completes) |
| `a` | Apply recommendation (after latch completes, if permitted) |
| `/` | Search (filter containers if multi-container pod) |

---

## Data Structures

### New Types (internal/promonitor/)

```go
// AlignmentRecommendation is the output of the recommendation engine.
type AlignmentRecommendation struct {
    Workload     WorkloadRef          `json:"workload"`
    Timestamp    time.Time            `json:"timestamp"`
    Confidence   Confidence           `json:"confidence"`      // HIGH, MEDIUM, LOW
    Safety       *models.SafetyAnalysis `json:"safety"`

    // Per-container recommendations
    Containers []ContainerAlignment  `json:"containers"`

    // Evidence summary
    LatchEvidence  *LatchEvidence    `json:"latch_evidence"`
    HistoricalData *HistoricalData   `json:"historical_data,omitempty"`

    // Policy evaluation
    PolicyResult   *PolicyResult     `json:"policy_result"`
}

type ContainerAlignment struct {
    Name    string              `json:"name"`
    Current ResourceValues      `json:"current"`
    Recommended ResourceValues  `json:"recommended"`
    Delta   ResourceDelta       `json:"delta"`
    Capped  bool                `json:"capped"`   // True if bounded by policy
}

type ResourceValues struct {
    CPURequest    string `json:"cpu_request"`
    CPULimit      string `json:"cpu_limit"`
    MemoryRequest string `json:"memory_request"`
    MemoryLimit   string `json:"memory_limit"`
}

type ResourceDelta struct {
    CPURequestPercent    float64 `json:"cpu_request_percent"`
    CPULimitPercent      float64 `json:"cpu_limit_percent"`
    MemoryRequestPercent float64 `json:"memory_request_percent"`
    MemoryLimitPercent   float64 `json:"memory_limit_percent"`
}

type WorkloadRef struct {
    Kind      string `json:"kind"`      // Deployment, StatefulSet, DaemonSet
    Name      string `json:"name"`
    Namespace string `json:"namespace"`
    UID       string `json:"uid"`
}

type Confidence string
const (
    ConfidenceHigh   Confidence = "HIGH"
    ConfidenceMedium Confidence = "MEDIUM"
    ConfidenceLow    Confidence = "LOW"
)

type LatchEvidence struct {
    LatchID        string        `json:"latch_id"`
    Start          time.Time     `json:"start"`
    End            time.Time     `json:"end"`
    Duration       time.Duration `json:"duration"`
    SampleCount    int           `json:"sample_count"`
    SampleInterval time.Duration `json:"sample_interval"`
    SpikeData      map[string]*metrics.SpikeData `json:"spike_data"`
}

type PolicyResult struct {
    PolicyPath       string   `json:"policy_path"`
    ApplyPermitted   bool     `json:"apply_permitted"`
    ExportPermitted  bool     `json:"export_permitted"`  // Always true after latch
    DenialReasons    []string `json:"denial_reasons,omitempty"`
    GuardrailsPassed []string `json:"guardrails_passed"`
    HPADetected      bool     `json:"hpa_detected"`
    HPAName          string   `json:"hpa_name,omitempty"`
    HPAAcknowledged  bool     `json:"hpa_acknowledged"`  // --acknowledge-hpa was passed
}

// IdentitySource indicates how the kube_user was determined.
type IdentitySource string
const (
    IdentitySourceSSR        IdentitySource = "ssr"        // SelfSubjectReview API
    IdentitySourceKubeconfig IdentitySource = "kubeconfig"  // Parsed from kubeconfig user stanza
    IdentitySourceUnknown    IdentitySource = "unknown"     // Neither method succeeded
)
```

### Admin Policy Types (internal/policy/)

```go
type Policy struct {
    APIVersion string       `yaml:"apiVersion"`
    Kind       string       `yaml:"kind"`
    Global     GlobalConfig `yaml:"global"`
    Audit      AuditConfig  `yaml:"audit"`
    Apply      ApplyConfig  `yaml:"apply"`
    Namespaces NSConfig     `yaml:"namespaces"`
    Identity   IDConfig     `yaml:"identity"`
    RateLimits RateConfig   `yaml:"rate_limits"`
}

type GlobalConfig struct {
    Enabled bool `yaml:"enabled"`
}

type AuditConfig struct {
    Backend       string `yaml:"backend"`
    Path          string `yaml:"path"`
    RetentionDays int    `yaml:"retention_days"`
}

type ApplyConfig struct {
    Enabled               bool   `yaml:"enabled"`
    RequireLatch          bool   `yaml:"require_latch"`
    MaxRequestDeltaPct    int    `yaml:"max_request_delta_percent"`
    MaxLimitDeltaPct      int    `yaml:"max_limit_delta_percent"`
    AllowLimitDecrease    bool   `yaml:"allow_limit_decrease"`
    MinLatchDuration      string `yaml:"min_latch_duration"`
    MaxLatchAge           string `yaml:"max_latch_age"`
    MinSafetyRating       string `yaml:"min_safety_rating"`
}

type NSConfig struct {
    Deny  []string `yaml:"deny"`
    Allow []string `yaml:"allow,omitempty"`
}

type IDConfig struct {
    RequireKubeContext bool `yaml:"require_kube_context"`
    RecordOSUser      bool `yaml:"record_os_user"`
    RecordGitIdentity bool `yaml:"record_git_identity"`
}

type RateConfig struct {
    MaxAppliesPerHour     int    `yaml:"max_applies_per_hour"`
    MaxAppliesPerWorkload int    `yaml:"max_applies_per_workload"`
    RateWindow            string `yaml:"rate_window"`
}
```

---

## Failure Modes

| Failure | Behavior | Recovery |
|---------|----------|----------|
| K8s API unreachable during latch | Latch pauses, retries 3x, then aborts with partial data. No recommendation. | Re-run latch. |
| Metrics-server unavailable | Latch cannot start (it depends on the K8s Metrics API). Clear error: "metrics-server not available. Install metrics-server or ensure it's running." | Install/fix metrics-server. Latch has no fallback — it cannot sample without it. |
| Metrics-server throttled/slow | Latch degrades gracefully: missed samples are counted and reported. If >20% of samples are missed, confidence is downgraded to LOW and a warning is shown. | Increase metrics-server resources or reduce sample frequency. |
| Prometheus unreachable | Latch continues (latch doesn't need Prometheus). Historical data marked "unavailable". Confidence downgraded. | Recommendation still possible, lower confidence. |
| Audit path not writable | Apply disabled at startup. Clear error message. Export still works. | Admin fixes permissions. |
| Policy file invalid YAML | Pro-monitor refuses to start. Validation error with line number. | Admin fixes policy file. Use `validate-policy` command. |
| Workload deleted during latch | Latch aborts. Partial data discarded. | Re-run latch on new workload. |
| Apply rejected by K8s API | Audit bundle records failure. Error shown in TUI. No retry. | Operator investigates (RBAC, admission webhook, quota). |
| SSA field conflict | Another field manager (Helm, ArgoCD, Flux) owns the resources field. Apply fails, conflict details recorded in decision.json. No force, no retry. | Operator updates resources via their GitOps tool, or manually resolves with `kubectl apply --server-side --force-conflicts`. |
| Apply succeeds but rollout fails | Not kubenow's responsibility. The audit bundle exists for rollback. | `kubectl apply -f before.yaml` |
| Rate limit exceeded | Apply button disabled with countdown. Export still works. | Wait for rate window to reset. |
| Latch data stale | Apply button disabled. Message shows age. | Re-run latch. |

### Real-Cluster Failure Modes

These three always show up in production and must be handled explicitly.

**Pod churn during latch (HPA scale, rollout, eviction)**

During a 2h+ latch, pods will be replaced — HPA scales, deployments roll, nodes drain. The latch samples pods by workload name, not pod UID, so new replicas are automatically picked up. However:

- If **all pods** for the workload disappear simultaneously (e.g., full rollout with `maxUnavailable: 100%`), the latch records a gap. Gaps >60s are counted.
- If total gap time exceeds 10% of latch duration, the latch is **invalidated**: recommendation is blocked, TUI shows "Latch invalidated: pod instability during observation (X gaps, Ys total downtime). Re-latch after workload stabilizes."
- If an HPA event or rollout is detected mid-latch (via K8s events), the TUI shows a live warning: `⚠ Rollout in progress — latch data may reflect transitional behavior`.
- The recommendation engine never uses samples from the first 60s after a pod replacement (startup noise).

**SSA conflict with GitOps controller (ArgoCD, Flux)**

The generic SSA conflict handling (fail fast, record in decision.json) is necessary but not sufficient. ArgoCD and Flux continuously reconcile, meaning the conflict will happen **every time** for GitOps-managed workloads. kubenow must make this obvious:

- On SSA conflict, kubenow inspects `metadata.managedFields` to identify the owning field manager.
- If the manager name matches known GitOps patterns (`argocd`, `flux`, `helm-controller`, `kustomize-controller`), the TUI shows a specific message:

```
Apply blocked: resources field owned by "argocd" (GitOps controller).
kubenow cannot apply directly to GitOps-managed workloads.

Recommended: use [E]xport to generate a patch, then commit it to your Git repository.
ArgoCD/Flux will apply the change through your normal deployment pipeline.
```

- The `decision.json` records `"conflict_manager": "argocd"` and `"recommended_action": "export_to_git"`.
- This is not a failure — it's the correct path for GitOps environments. Export is the primary workflow here.

**Quota / LimitRange / admission mutation changes applied values**

A mutating admission webhook (e.g., LimitRange defaults, Gatekeeper mutation) or ResourceQuota enforcement can silently alter the values kubenow applies. The post-apply read-back comparison handles this, but the UX must be explicit:

- After apply, kubenow GETs the controller and compares the live `.resources` field against what was requested.
- If the admitted values differ from the requested values, the TUI shows:

```
⚠ Admitted values differ from requested:
  CPU request: requested 180m → admitted 200m (LimitRange minimum)
  Memory limit: requested 580Mi → admitted 512Mi (mutated by webhook)

The audit bundle records both requested and admitted values.
```

- The `after.yaml` in the audit bundle reflects the **admitted** (live) values, not the requested values.
- The `decision.json` includes a `"post_apply_drift"` section showing the delta between requested and admitted.
- This is informational, not a failure. The apply succeeded — the cluster just adjusted the values. But the operator must know.

---

## Security Considerations

### RBAC Requirements

Pro-monitor needs additional K8s permissions beyond monitor mode:

```yaml
# Read (always needed for pro-monitor)
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list"]
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "daemonsets"]
  verbs: ["get", "list"]
- apiGroups: ["metrics.k8s.io"]
  resources: ["pods"]
  verbs: ["get", "list"]

# Read HPA (needed for HPA guardrail detection)
- apiGroups: ["autoscaling"]
  resources: ["horizontalpodautoscalers"]
  verbs: ["get", "list"]

# Write (only needed for apply, via SSA)
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets", "daemonsets"]
  verbs: ["patch"]
```

### Server-Side Apply Details

kubenow uses **Server-Side Apply (SSA)** exclusively. No strategic merge patch, no client-side apply.

- **Field manager**: `kubenow` (constant, not configurable)
- **Force**: `false` (always). kubenow never force-acquires field ownership.
- **Conflict behavior**: If another field manager (Helm, ArgoCD, Flux, kubectl) owns `.spec.template.spec.containers[*].resources`, SSA returns a conflict error. kubenow:
  - Records the conflict in `decision.json` (including which manager owns the field)
  - Shows the conflict in the TUI with the owning manager name
  - Does **not** retry or offer to force
  - Suggests: "Resolve via your GitOps tool or use `kubectl apply --server-side --force-conflicts` manually"

This is the correct behavior: if a GitOps tool manages resources, kubenow should not silently take ownership.

### Admission Webhook Compatibility

- Admission webhooks (OPA/Gatekeeper, Kyverno) evaluate the SSA request normally.
- If a webhook rejects the change, kubenow records the rejection (reason, webhook name if available) in `decision.json` and does not retry.
- This is correct behavior: admin policies external to kubenow are respected.

### Secret Handling

- kubenow never reads or modifies Secrets.
- Audit bundles contain the full controller object (Deployment/StatefulSet/DaemonSet) but controllers do not embed Secret values in their spec. Secret references (secretKeyRef, volume mounts) appear as names, not values.
- If a controller spec somehow contains inline sensitive data (e.g., environment variables with hardcoded passwords), that data will appear in the audit bundle. This is the operator's responsibility, not kubenow's. kubenow does not attempt to detect or redact inline secrets in controller specs.

---

## Execution Plan

7 PRs. Each ends with one demo command and one golden output snapshot. No PR depends on all others — PRs 1–4 are sequential, PRs 5–7 can overlap after PR 4.

### PR 1: Policy loader + validator

**Branch**: `feat/promonitor-policy`

**Scope**: `internal/policy/` package. Parse, validate, and load the admin policy file. CLI command `kubenow pro-monitor validate-policy`.

**New files**:
- `internal/policy/policy.go` — types, loader, validator
- `internal/policy/policy_test.go` — table-driven tests (valid, invalid, missing, partial)
- `internal/cli/promonitor.go` — `pro-monitor` parent command
- `internal/cli/promonitor_validate.go` — `validate-policy` subcommand

**What it does**:
- Load from `/etc/kubenow/policy.yaml` or `$KUBENOW_POLICY`
- Validate all fields (types, ranges, required fields, known safety ratings)
- Return structured errors with field paths and line numbers
- When absent: return a "no policy" result (not an error)

**Demo command**:
```bash
kubenow pro-monitor validate-policy --policy examples/policy.yaml
```

**Golden output**:
```
Policy: examples/policy.yaml
  apiVersion: kubenow/v1alpha1 ✓
  global.enabled: true ✓
  audit.backend: filesystem ✓
  audit.path: /var/lib/kubenow/audit ✓ (writable)
  apply.enabled: true ✓
  apply.max_request_delta_percent: 30 ✓
  apply.allow_limit_decrease: false ✓
  namespaces.deny: [kube-system, kube-public, kube-node-lease] ✓
  identity.require_kube_context: true ✓
  rate_limits.max_applies_per_hour: 10 ✓

Policy valid. Apply is enabled with bounded ±30%.
```

**Tests**: Valid policy, missing required fields, invalid types, missing file, unwritable audit path, unknown apiVersion.

---

### PR 2: Pro-monitor command scaffolding + red frame TUI shell

**Branch**: `feat/promonitor-tui-shell`

**Scope**: Cobra wiring for `pro-monitor latch`, red-framed bubbletea TUI that connects to a real cluster but shows only the frame + workload info + latch progress. No recommendations yet.

**New files**:
- `internal/cli/promonitor_latch.go` — `latch` subcommand with all flags
- `internal/promonitor/model.go` — bubbletea model (red frame, progress, workload info)
- `internal/promonitor/ui.go` — view rendering (red border, banner, key bindings)

**What it does**:
- Parse `<kind>/<name>` workload ref, validate it exists via K8s API
- Check for metrics-server availability (fail fast if missing)
- Detect HPA targeting the workload (show warning if present)
- Load policy (determine suggest+export vs suggest+export+apply)
- Display red-framed TUI with: workload info, latch progress bar, policy status
- Latch runs but only shows raw sample count (no recommendations yet)

**Demo command**:
```bash
kubenow pro-monitor latch deployment/payment-api --duration 1m --namespace default
```

**Golden output**: Screenshot of red-framed TUI with workload info, latch progress, and "Recommendation engine not yet connected" placeholder.

**Tests**: Workload ref parsing (valid/invalid), HPA detection mock, metrics-server check mock.

---

### PR 3: Per-workload latch + persistence + status

**Branch**: `feat/promonitor-latch-persist`

**Scope**: Extend `LatchMonitor` to filter by single workload. Add percentile computation (p50/p95/p99 from samples). Persist latch data to disk. Add `pro-monitor status` command.

**Changes to existing code**:
- `internal/metrics/latch.go` — add `WorkloadFilter` field to `LatchConfig`, add `ComputePercentiles()` to `SpikeData`
- New: `internal/promonitor/persistence.go` — write/read latch results as JSON to `~/.kubenow/latch/`
- New: `internal/cli/promonitor_status.go` — show latch results for a workload

**What it does**:
- `LatchMonitor` samples only the target workload's pods (matched by owner reference, not just name heuristic)
- On latch completion, sort samples and compute percentiles (p50=median, p95, p99)
- Write latch result to `~/.kubenow/latch/<namespace>__<kind>__<name>.json` (best-effort persistence)
- `pro-monitor status` reads persisted latch and shows summary

**Demo command**:
```bash
kubenow pro-monitor status deployment/payment-api -n default
```

**Golden output**:
```
Latch: deployment/payment-api (default)
  Recorded: 2026-02-07T14:22:01Z (2h ago)
  Duration: 15m  Samples: 180  Missed: 0  Gaps: 0
  CPU:  avg=70m  p50=65m  p95=120m  p99=140m  max=380m
  MEM:  avg=170Mi p50=160Mi p95=210Mi p99=240Mi max=400Mi
  Signals: 0 OOMKills, 2 restarts, 0 evictions
  Status: VALID (fresh, no gaps)
```

**Tests**: Percentile computation (known inputs → known outputs), persistence round-trip, gap detection, pod churn invalidation.

---

### PR 4: Recommendation engine

**Branch**: `feat/promonitor-recommend`

**Scope**: `internal/promonitor/recommend.go`. Fuse latch evidence + optional Prometheus historical data into `AlignmentRecommendation`. Wire into the TUI.

**New files**:
- `internal/promonitor/recommend.go` — recommendation algorithm (the core of this spec)
- `internal/promonitor/recommend_test.go` — table-driven tests for every algorithm step
- `internal/promonitor/types.go` — `AlignmentRecommendation`, `ContainerAlignment`, etc.

**What it does**:
- Implement the full recommendation algorithm: envelope selection, safety margins, per-resource computation, burst caps, policy bounds, confidence scoring
- Handle multi-container pods (independent recommendation per container)
- Detect and report HPA impact (compute new effective scaling threshold)
- Wire into TUI: after latch completes, show recommendation view with before/after/evidence/warnings
- Suggest + export mode: show `[E]xport` and `[Q]uit` buttons. `[A]pply` shown only if policy permits (greyed out with reason otherwise).

**Demo command**:
```bash
kubenow pro-monitor latch deployment/payment-api --duration 15m -n default \
  --prometheus-url http://localhost:9090
```

**Golden output**: Screenshot of TUI recommendation view showing before/after table, evidence summary, confidence MEDIUM, safety CAUTION, and `[E]xport  [A]pply  [Q]uit` buttons.

**Tests**: Envelope selection (latch-only, Prometheus-only, both), safety margin application, burst cap enforcement, policy bounding, confidence scoring, HPA impact calculation, UNSAFE → no recommendation.

---

### PR 5: Export formats

**Branch**: `feat/promonitor-export`

**Scope**: `kubenow pro-monitor export` command. Generate patch, manifest, diff, JSON from a persisted latch + recommendation.

**New files**:
- `internal/cli/promonitor_export.go` — export subcommand
- `internal/promonitor/export.go` — format generators
- `internal/promonitor/export_test.go`

**What it does**:
- Read persisted latch, compute recommendation, generate output
- `--format patch`: SSA-compatible YAML with evidence comments
- `--format manifest`: full controller with new values, volatile fields stripped
- `--format diff`: unified diff between current and recommended
- `--format json`: machine-readable `AlignmentRecommendation`
- HPA warning included as comment in patch/manifest
- Works without admin policy (export is always available)
- TUI `[E]` key triggers export with format selection

**Demo command**:
```bash
kubenow pro-monitor export deployment/payment-api -n default --format patch
```

**Golden output**:
```yaml
# kubenow alignment patch
# Generated: 2026-02-07T14:22:01Z
# Confidence: MEDIUM  Safety: CAUTION  Latch: 15m (180 samples)
# Apply with: kubectl apply --server-side -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payment-api
  namespace: default
spec:
  template:
    spec:
      containers:
        - name: payment-api
          resources:
            requests:
              cpu: "180m"
              memory: "290Mi"
            limits:
              cpu: "1000m"
              memory: "1Gi"
```

**Tests**: Each format produces valid output, volatile field stripping, multi-container, HPA comment inclusion.

---

### PR 6: SSA apply + conflict handling

**Branch**: `feat/promonitor-apply`

**Scope**: The apply path. SSA with field manager `kubenow`, no force, conflict detection, read-back verification, confirmation UX.

**New files**:
- `internal/promonitor/apply.go` — SSA apply, conflict handling, read-back diff
- `internal/promonitor/apply_test.go`

**What it does**:
- Check the actionable invariant (all terms must be true)
- Show confirmation prompt ("Type 'apply' to confirm")
- Execute SSA with field manager `kubenow`, force=false
- On conflict: detect owning manager from `managedFields`, show GitOps-specific message if ArgoCD/Flux
- On success: GET the object, compare admitted values vs requested, show drift if any
- On failure: record error, show in TUI, no retry
- Wire `[A]` key in TUI to apply flow

**Demo command**: Tested against a kind cluster with a deployment. Apply reduces CPU request.

**Golden output**:
```
✓ Applied successfully

  CPU Requests:    500m → 180m
  Memory Requests: 512Mi → 290Mi
  Post-apply check: admitted values match requested ✓

  Audit bundle: (not yet — PR 7)
  Rollback: kubectl apply -f <bundle>/before.yaml
```

**Tests**: SSA mock (success, conflict with known manager, conflict with unknown manager, rejection by webhook), read-back comparison (match, drift), actionable invariant enforcement (each term individually falsified).

---

### PR 7: Audit bundles + rate limiting + identity

**Branch**: `feat/promonitor-audit`

**Scope**: `internal/audit/` package. Audit bundle writer, rate limiting with flock, identity recording via SSR with kubeconfig fallback.

**New files**:
- `internal/audit/bundle.go` — create directory, write before/after/diff/decision
- `internal/audit/ratelimit.go` — file-based rate limiting with flock
- `internal/audit/identity.go` — SSR attempt, kubeconfig fallback, os/user
- `internal/audit/bundle_test.go`, `ratelimit_test.go`, `identity_test.go`

**What it does**:
- Before apply: create bundle directory, write `before.yaml` (full controller, volatile fields stripped), write `decision.json` with status `pending`
- After apply: write `after.yaml`, generate `diff.patch`, update `decision.json` with final status + `post_apply_drift` if applicable
- If bundle write fails at any point: abort apply
- Rate limiting: flock-based, keyed by workload UID, tumbling windows
- Identity: attempt SelfSubjectReview, fall back to kubeconfig user stanza, always record os_user and kube_context

**Demo command**:
```bash
ls /var/lib/kubenow/audit/2026-02-07T14-22-01Z__default__deployment__payment-api/
```

**Golden output**:
```
before.yaml
after.yaml
diff.patch
decision.json
```

Plus `cat decision.json` showing the full schema from the spec.

**Tests**: Bundle write (success, permission denied → abort), volatile field stripping, rate limit enforcement (within limit, exceeded, concurrent access), identity (SSR success, SSR denied → kubeconfig fallback, both fail → unknown).

---

## Decisions Locked

These were open questions that have been resolved:

1. **Export without policy**: **YES.** Export is always available after latch completion, regardless of admin policy. Policy gates mutation (apply), not information (export). Disabling safe behavior to punish the absence of dangerous behavior is backwards.

2. **HPA interaction**: **Warn always, block apply by default.** If an HPA targets the workload, pro-monitor shows a persistent warning explaining the scaling impact. Apply is blocked unless the operator passes `--acknowledge-hpa`. Export always works. See [HPA Guardrail](#hpa-guardrail-non-negotiable).

3. **Prometheus absence**: **Pro-monitor works with latch-only data at LOW confidence.** Apply at LOW confidence is permitted if the admin policy allows it (no `min_confidence` in policy yet — this may be added). The operator sees the LOW confidence clearly in the TUI and must still type "apply" to confirm.

4. **Limits computation**: **Use p999/p99 with burst cap, not raw max.** See [Why Limits Use p999/p99 Instead of Max](#why-limits-use-p999p99-instead-of-max).

5. **Latch state persistence**: **YES, best-effort to `~/.kubenow/latch/`.** Enables "latch overnight, review in morning" and decouples latch from export/apply. Persistence is best-effort — if the file is lost, re-latch. See PR 3.

---

## Open Questions

1. **Multi-container pods**: The recommendation engine handles multiple containers independently, but the TUI needs a container selector. Options: tab-based navigation, stacked view (all containers visible), or default to primary container with `Tab` to cycle. The stacked view is simpler but may not scale to pods with 5+ sidecars.

2. **CronJobs / Jobs**: These have fundamentally different resource patterns (burst then idle). Should pro-monitor support them? Current design: Deployment, StatefulSet, DaemonSet only. Jobs could be added later with a different latch strategy (observe N runs instead of continuous).

3. **Policy versioning**: When the policy schema changes in future versions, how to handle backward compatibility? `apiVersion` field exists but migration strategy is undefined. Options: strict version matching (refuse unknown versions), forward-compatible parsing (ignore unknown fields), or explicit migration command.

---

*This document describes the design for kubenow v0.2.0. It will be updated as implementation progresses and open questions are resolved.*
