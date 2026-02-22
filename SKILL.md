---
name: kubenow
description: Kubernetes resource analysis and cost optimization — deterministic analysis, policy-gated apply, real-time monitoring
user-invocable: false
metadata: {"requires":{"bins":["kubenow"]}}
---

# kubenow — Kubernetes Resource Analysis & Cost Optimization

You have access to `kubenow`, a tool that combines deterministic cost analysis, policy-gated resource alignment, and real-time monitoring for Kubernetes clusters.

## Install

```bash
brew install ppiankov/tap/kubenow
```

## Commands

| Command | What it does |
|---------|-------------|
| `kubenow monitor` | Real-time TUI for cluster problems |
| `kubenow analyze requests-skew` | Find over-provisioned resources via Prometheus |
| `kubenow analyze node-footprint` | Historical capacity simulation (bin-packing) |
| `kubenow pro-monitor latch` | High-resolution resource sampling (1-5s intervals) |
| `kubenow pro-monitor export` | Export recommendation as patch/manifest/diff/JSON |
| `kubenow pro-monitor apply` | Policy-gated Server-Side Apply (requires policy file) |
| `kubenow pro-monitor validate-policy` | Validate a policy file |
| `kubenow incident` | LLM-assisted incident triage |
| `kubenow pod` | LLM-assisted pod debugging |
| `kubenow version` | Print version |

## Key Flags

### analyze requests-skew

| Flag | Description |
|------|-------------|
| `--prometheus-url` | Prometheus endpoint URL |
| `--auto-detect-prometheus` | Auto-detect in-cluster Prometheus |
| `--k8s-service` | Kubernetes service for port-forward |
| `--k8s-namespace` | Namespace for service (default "monitoring") |
| `--window` | Time window for analysis (default "3d") |
| `--namespace-include` | Namespace include pattern |
| `--namespace-exclude` | Namespace exclude pattern |
| `--output` | Output format: table, json, sarif |
| `--export-file` | Export results to file |
| `--compare-baseline` | Compare against saved baseline |
| `--save-baseline` | Save results as baseline |
| `--obfuscate` | Obfuscate workload/namespace names |
| `--silent` | Suppress progress output |
| `--fail-on` | Exit non-zero on severity: critical, warning |

### pro-monitor latch

| Flag | Description |
|------|-------------|
| `-n, --namespace` | Target namespace |
| `--duration` | Sampling duration (default "10m") |
| `--interval` | Sampling interval (default "5s") |
| `--policy` | Admin policy file for bounds and apply gates |
| `--acknowledge-hpa` | Proceed despite HPA conflict |

### pro-monitor export

| Flag | Description |
|------|-------------|
| `--format` | Export format: patch, manifest, diff, json |
| `-n, --namespace` | Target namespace |

### monitor

| Flag | Description |
|------|-------------|
| `--severity` | Minimum severity filter (critical, warning) |
| `--namespace` | Namespace filter |
| `--no-mesh` | Disable service mesh monitoring |

### LLM modes (incident, pod, teamlead, compliance, chaos)

| Flag | Description |
|------|-------------|
| `--llm-endpoint` | OpenAI-compatible API endpoint |
| `--model` | Model name (e.g., mixtral, gpt-4o) |
| `--include-pods` | Pod name filter pattern |
| `--namespace` | Namespace filter |
| `--output` | Export report to file |

## Agent Usage Pattern

```bash
# Find over-provisioned resources (JSON)
kubenow analyze requests-skew \
  --prometheus-url http://localhost:9090 \
  --output json --export-file results.json

# SARIF for GitHub Security tab
kubenow analyze requests-skew \
  --prometheus-url http://localhost:9090 \
  --output sarif --export-file results.sarif

# CI gate — fail on critical over-provisioning
kubenow analyze requests-skew \
  --prometheus-url http://localhost:9090 \
  --output json --fail-on critical

# Baseline drift detection
kubenow analyze requests-skew \
  --prometheus-url http://localhost:9090 \
  --save-baseline baseline.json
# ... later ...
kubenow analyze requests-skew \
  --prometheus-url http://localhost:9090 \
  --compare-baseline baseline.json

# High-resolution latch for a workload
kubenow pro-monitor latch deployment/payment-api -n production --duration 30m

# Export recommendation as SSA patch
kubenow pro-monitor export deployment/payment-api -n production --format patch

# Real-time cluster problems as JSON (one-shot)
kubenow monitor --output json
```

### JSON Output Structure (requests-skew)

```json
{
  "metadata": {
    "tool": "kubenow",
    "version": "0.3.3",
    "command": "analyze requests-skew",
    "prometheus_url": "http://localhost:9090",
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
          "cpu": {
            "request": "4.0",
            "p99_actual": "0.5",
            "skew": 8.0,
            "safety_rating": "SAFE",
            "recommendation": "500m"
          },
          "memory": {
            "request": "4Gi",
            "p99_actual": "1.2Gi",
            "skew": 3.3,
            "safety_rating": "CAUTION",
            "recommendation": "2Gi"
          }
        }
      ],
      "safety_rating": "CAUTION",
      "impact_score": 42.5
    }
  ],
  "summary": {
    "workloads_analyzed": 12,
    "safe": 8,
    "caution": 2,
    "risky": 1,
    "unsafe": 1
  }
}
```

### Parsing Examples

```bash
# List RISKY/UNSAFE workloads
kubenow analyze requests-skew --prometheus-url "$PROM_URL" --output json \
  --export-file - | jq '.workloads[] | select(.safety_rating == "RISKY" or .safety_rating == "UNSAFE")'

# Get total CPU waste
kubenow analyze requests-skew --prometheus-url "$PROM_URL" --output json \
  --export-file - | jq '[.workloads[].containers[].cpu.skew] | add'

# Export SSA patch for a workload (pipe to kubectl)
kubenow pro-monitor export deployment/payment-api -n production --format patch | \
  kubectl apply --server-side -f -
```

## Safety Ratings

| Rating | Meaning |
|--------|---------|
| SAFE | No safety signals, recommendation is confident |
| CAUTION | Minor restarts or low data confidence |
| RISKY | OOMKills detected, recommendation includes extra margin |
| UNSAFE | Structurally blocked — no recommendation produced |

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `2` | Invalid input (bad flags, missing required args) |
| `3` | Runtime error (cluster connection failed, query timeout) |

## What kubenow Does NOT Do

- Does not auto-scale — presents evidence, never auto-adjusts without explicit consent
- Does not install agents, sidecars, or CRDs — zero cluster footprint
- Does not predict future usage — reports what would have worked historically
- Does not write to cluster by default — only `pro-monitor apply` can mutate, and it requires a policy file + 10 pre-flight checks
- Does not use ML — deterministic analysis with explicit thresholds
