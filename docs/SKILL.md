---
name: kubenow
description: Kubernetes resource analysis and cost optimization — deterministic analysis, policy-gated apply, real-time monitoring
user-invocable: false
metadata: {"requires":{"bins":["kubenow"]}}
---

# kubenow — Kubernetes Resource Analysis & Cost Optimization

Deterministic cost analysis, policy-gated resource alignment, and real-time monitoring for Kubernetes clusters.

## Install

```bash
brew install ppiankov/tap/kubenow
```

## Commands

### kubenow analyze

Find over-provisioned resources via Prometheus. Subcommands: `requests-skew`, `node-footprint`.

**Flags:**
- `--prometheus-url` — Prometheus endpoint URL
- `--format json` — output format: table, json, sarif
- `--window` — time window for analysis (default: 3d)
- `--namespace-include` — namespace include pattern
- `--namespace-exclude` — namespace exclude pattern
- `--export-file` — export results to file
- `--compare-baseline` — compare against saved baseline
- `--save-baseline` — save results as baseline
- `--fail-on` — exit non-zero on severity: critical, warning
- `--obfuscate` — obfuscate workload/namespace names
- `--cost-cpu` — cost per CPU core per hour in dollars
- `--cost-memory` — cost per GiB memory per hour in dollars
- `--instance-type` — node instance type for pricing lookup (e.g., m5.xlarge, n2-standard-4)

**JSON output:**
```json
{
  "metadata": {
    "tool": "kubenow",
    "command": "analyze requests-skew",
    "window": "3d"
  },
  "workloads": [
    {
      "namespace": "production",
      "name": "payment-api",
      "kind": "Deployment",
      "containers": [
        {
          "name": "payment-api",
          "cpu": {"request": "4.0", "p99_actual": "0.5", "skew": 8.0, "recommendation": "500m"},
          "memory": {"request": "4Gi", "p99_actual": "1.2Gi", "skew": 3.3, "recommendation": "2Gi"}
        }
      ],
      "safety_rating": "CAUTION",
      "estimated_monthly_waste": 42.50
    }
  ],
  "summary": {"workloads_analyzed": 12, "safe": 8, "caution": 2, "risky": 1, "unsafe": 1, "total_monthly_waste": 180.00}
}
```

**Exit codes:**
- 0: success
- 2: invalid input
- 3: runtime error (cluster connection failed)

### kubenow pro-monitor export

Export resource recommendations as structured patches for GitOps workflows.

**Flags:**
- `--format` — output format: patch, manifest, diff, json, kustomize, helm
- `-o, --output` — write to file instead of stdout

**Formats:**
- `patch` — SSA-compatible YAML patch (pipe to `kubectl apply`)
- `manifest` — full manifest with recommended values
- `diff` — unified diff for review
- `json` — machine-readable JSON
- `kustomize` — kustomization.yaml + strategic merge patch
- `helm` — values.yaml fragment with resource overrides

### kubenow pro-monitor track

Validate whether past recommendations were accurate by comparing post-apply metrics against new resource requests.

**Flags:**
- `--audit-path` — path to audit bundle directory (required)
- `--prometheus-url` — Prometheus endpoint for post-apply usage metrics
- `--format` — output format: table, json
- `--since` — only show applies within this window (e.g., 7d, 30d, 24h)

**JSON output:**
```json
{
  "recommendations": [
    {
      "workload": "nginx/web",
      "resource": "cpu",
      "old_request": "500m",
      "new_request": "250m",
      "applied_at": "2026-02-15T10:00:00Z",
      "peak_usage": "220m",
      "utilization_pct": 88,
      "outcome": "SAFE"
    }
  ],
  "summary": {"total": 47, "safe": 44, "tight": 2, "wrong": 1, "accuracy_pct": 93.6}
}
```

**Outcome classifications:**
- SAFE — headroom > margin
- TIGHT — headroom < 10%
- WRONG — OOMKill, throttling, or crash detected post-apply
- PENDING — insufficient post-apply data

### kubenow monitor

Real-time TUI for cluster problems.

**Flags:**
- `--format json` — output format
- `--severity` — minimum severity filter
- `--namespace` — namespace filter
- `--no-mesh` — disable service mesh monitoring

### kubenow version

Print version information.

**Flags:**
- `--json` — output version as JSON

**JSON output:**
```json
{
  "version": "0.4.0",
  "commit": "e9b8f18",
  "built": "2026-03-02T11:50:11Z",
  "goVersion": "go1.25.7",
  "os": "darwin",
  "arch": "arm64"
}
```

### kubenow init

Not implemented. No config file required. Kubenow reads flags, environment variables, and an optional policy file.

## What this does NOT do

- Does not auto-scale — presents evidence, never auto-adjusts without explicit consent
- Does not install agents, sidecars, or CRDs — zero cluster footprint
- Does not predict future usage — reports what would have worked historically
- Does not use ML — deterministic analysis with explicit thresholds
- Does not auto-revert — tracks outcomes, presents evidence, user decides

## Parsing examples

```bash
# List RISKY/UNSAFE workloads
kubenow analyze requests-skew --prometheus-url "$PROM_URL" --format json --export-file - | jq '.workloads[] | select(.safety_rating == "RISKY" or .safety_rating == "UNSAFE")'

# Get total CPU waste
kubenow analyze requests-skew --prometheus-url "$PROM_URL" --format json --export-file - | jq '[.workloads[].containers[].cpu.skew] | add'

# Get monthly cost waste estimate
kubenow analyze requests-skew --prometheus-url "$PROM_URL" --cost-cpu 0.048 --cost-memory 0.006 --format json --export-file - | jq '.summary.total_monthly_waste'

# Export recommendations as kustomize patches
kubenow pro-monitor export deployment/payment-api -n production --format kustomize -o patches/

# Track recommendation accuracy over 30 days
kubenow pro-monitor track --audit-path /var/lib/kubenow/audit --prometheus-url "$PROM_URL" --format json --since 30d | jq '.summary.accuracy_pct'

# CI gate
kubenow analyze requests-skew --prometheus-url "$PROM_URL" --format json --fail-on critical

# Version check in CI
kubenow version --json | jq -r '.version'
```
