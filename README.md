![kubenow logo](https://raw.githubusercontent.com/ppiankov/kubenow/main/docs/img/logo.png)

# kubenow — Kubernetes Resource Analysis & Cost Optimization

[![CI](https://github.com/ppiankov/kubenow/workflows/CI/badge.svg)](https://github.com/ppiankov/kubenow/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/ppiankov/kubenow)](https://goreportcard.com/report/github.com/ppiankov/kubenow)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**Version 0.2.5** — Deterministic resource analysis, policy-gated apply, and real-time monitoring for Kubernetes clusters.

---

## Quickstart

```bash
# Install
go install github.com/ppiankov/kubenow/cmd/kubenow@latest
# Or download from releases: https://github.com/ppiankov/kubenow/releases/latest

# Monitor cluster problems (real-time TUI)
kubenow monitor

# Analyze over-provisioned resources
kubenow analyze requests-skew --prometheus-url http://localhost:9090

# High-resolution resource sampling for a workload
kubenow pro-monitor latch deployment/payment-api -n production

# Export recommendation as SSA patch
kubenow pro-monitor export deployment/payment-api -n production --format patch
```

### Exit Codes
- `0` — Success
- `2` — Invalid input (bad flags, missing required args)
- `3` — Runtime error (cluster connection failed, query timeout)

---

## What is kubenow?

A Kubernetes cluster analysis tool that combines:
- **Deterministic cost analysis** — evidence-based resource optimization using Prometheus metrics
- **Pro-Monitor** — policy-gated resource alignment with bounded Server-Side Apply
- **Real-time monitoring** — attention-first TUI for cluster problems
- **LLM-assisted analysis** — optional incident triage via any OpenAI-compatible API

## What kubenow is NOT

- **Not an auto-scaler** — presents evidence, never auto-adjusts resources without explicit consent
- **Not a service mesh** — queries existing APIs, installs nothing into the cluster
- **Not an APM** — no agents, no sidecars, no instrumentation
- **Not a predictor** — reports what *would have worked historically*, never what *will* work
- **Not a replacement for monitoring** — complements Prometheus, Grafana, and alerting

---

## Deterministic Analysis

### requests-skew: Find Over-Provisioned Resources

Compares resource requests against actual Prometheus metrics over a configurable time window.

```bash
kubenow analyze requests-skew --prometheus-url http://prometheus:9090

# With namespace filtering and 7-day window
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --window 7d \
  --namespace-include "prod-*"

# SARIF output for CI integration
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --output sarif --export-file results.sarif

# Compare against saved baseline
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --compare-baseline baseline.json
```

Output:
```
=== Requests-Skew Analysis (Prometheus metrics only) ===

NAMESPACE  WORKLOAD         REQ CPU  P99 CPU  SKEW   SAFETY      IMPACT
prod       payment-api      4.0      3.8      8.0x   RISKY       HIGH (42.5)
prod       checkout-worker  2.0      0.5      6.7x   SAFE        MED (18.2)

Namespace Prometheus Status:
  production     368 series
  staging        142 series
  ads-fraud      no data — use pro-monitor latch for these workloads
```

Key features:
- Safety analysis: OOMKills, restarts, CPU throttling, spike patterns
- Safety ratings: SAFE, CAUTION, RISKY, UNSAFE with automatic margins
- Per-namespace Prometheus diagnostics with latch suggestions
- Obfuscation mode (`--obfuscate`) for sharing without exposing names
- Baseline comparison for tracking drift over time
- Output formats: table, JSON, SARIF

### node-footprint: Historical Capacity Simulation

Bin-packing simulation to test alternative node configurations against historical data.

```bash
kubenow analyze node-footprint --prometheus-url http://prometheus:9090
```

Tests alternative topologies using First-Fit Decreasing algorithm with feasibility checks and headroom calculation.

---

## Pro-Monitor

Policy-gated resource alignment: latch, recommend, export, apply.

### Latch: High-Resolution Resource Sampling

Samples workload resource usage at 1-5 second intervals via the Kubernetes Metrics API, capturing sub-scrape-interval spikes that Prometheus misses.

```bash
# Sample deployment for 30 minutes at 5-second intervals
kubenow pro-monitor latch deployment/payment-api -n production --duration 30m

# Sample a CRD-managed pod directly
kubenow pro-monitor latch pod/payments-main-db-2 -n production
```

The TUI shows real-time progress, and after completion computes a resource alignment recommendation with safety rating and confidence level.

CRD-managed workloads (CNPG, Strimzi, RabbitMQ, Redis, Elasticsearch) are automatically detected from pod labels and displayed with their operator type:

```
Workload:  pod/payments-main-db (CNPG)
Namespace: production
```

### Recommendation

After latch completes, kubenow computes per-container resource recommendations:
- **Safety ratings**: SAFE (no signals), CAUTION (minor restarts), RISKY (OOMKills), UNSAFE (blocked)
- **Confidence levels**: HIGH (24h+ latch + Prometheus), MEDIUM (2h+ latch), LOW
- **Policy bounds**: admin-defined max delta percentages, minimum safety rating
- **Evidence**: sample count, gaps, percentiles (p50/p95/p99/max)

### Export

Export recommendations in multiple formats:

```bash
# SSA-compatible YAML patch (pipe to kubectl apply)
kubenow pro-monitor export deployment/payment-api -n production --format patch

# Full manifest with recommended values
kubenow pro-monitor export deployment/payment-api --format manifest

# Unified diff for review
kubenow pro-monitor export deployment/payment-api --format diff

# Machine-readable JSON
kubenow pro-monitor export deployment/payment-api --format json
```

### Apply: Bounded Server-Side Apply

Policy-gated mutation via Kubernetes Server-Side Apply. Requires an admin policy file.

Pre-flight checks before any mutation:
- Policy loaded and apply enabled
- Safety rating meets policy minimum
- Namespace allowed
- HPA not detected (unless acknowledged)
- Latch data fresh (within policy MaxLatchAge)
- Audit path writable
- Rate limit not exceeded

GitOps conflict detection: inspects `managedFields` for ArgoCD, Flux, Helm, and Kustomize field managers. Reports conflict rather than overwriting.

### Exposure Map

Press `l` during latch to view structural traffic topology:
- Services matching the workload's pod selector
- Ingress routes and TLS configuration
- Network policies (allowed sources)
- Namespace neighbors ranked by CPU usage

Shows **possible** traffic paths from Kubernetes API state, not measured traffic.

### Policy Engine

Admin-controlled guardrails via a policy file:

```bash
kubenow pro-monitor latch deployment/payment-api --policy /etc/kubenow/policy.yaml

# Validate policy without running
kubenow pro-monitor validate-policy --policy policy.yaml --check-paths
```

Three operating modes:
- **Observe Only** — no policy or disabled: view metrics, no recommendations
- **Export Only** — policy present, apply disabled: recommendations with bounds, export only
- **Apply Ready** — policy present, apply enabled: full latch-recommend-export-apply pipeline

### Audit Trail

Every apply operation creates a tamper-evident audit bundle:
- `before.yaml` / `after.yaml` — workload state snapshots
- `diff.patch` — unified diff of changes
- `decision.json` — full decision record (identity, evidence, guardrails, result)

Rate limiting: configurable global and per-workload apply limits per time window.

---

## Real-time Monitor

Terminal UI for cluster problems, designed like `top` for Kubernetes issues.

```bash
kubenow monitor
# Press 1/2/3 to sort, arrow keys to scroll, c to copy, q to quit
```

- Attention-first: empty screen when healthy, shows only broken things
- Watches for: OOMKills, CrashLoopBackOff, ImagePullBackOff, failed pods, node issues
- Service mesh health: linkerd/istio control plane failures and certificate expiry
- Sortable by severity, recency, or count
- Press `c` to dump everything to terminal for copying

Use `--severity critical` to filter for critical issues only.

---

## LLM Analysis (Optional)

Feed cluster snapshots into any OpenAI-compatible API for incident triage, pod debugging, compliance checks, and chaos suggestions.

```bash
# Incident triage
kubenow incident --llm-endpoint http://localhost:11434/v1 --model mixtral

# Pod debugging with filters
kubenow pod --llm-endpoint http://localhost:11434/v1 --model mixtral \
  --include-pods "payment-*" --namespace production

# Export report
kubenow incident --llm-endpoint https://api.openai.com/v1 --model gpt-4o \
  --output incident-report.md
```

Works with Ollama, OpenAI, Azure OpenAI, DeepSeek, Groq, Together, OpenRouter, or any `/v1/chat/completions` endpoint.

Available modes: `incident`, `pod`, `teamlead`, `compliance`, `chaos`

---

## Architecture

```
                         kubenow CLI
  ┌──────────────┬──────────────┬──────────────┬──────────┐
  │   monitor    │   analyze    │  pro-monitor  │   LLM    │
  │              │              │               │  modes   │
  │  Real-time   │ requests-skew│ latch/export  │ incident │
  │  problem     │ node-footprint apply/status  │ pod      │
  │  detection   │              │               │ teamlead │
  │              │              │  Policy Engine│ compliance│
  │              │              │  Audit Trail  │ chaos    │
  │              │              │  Exposure Map │          │
  └──────┬───────┴──────┬───────┴───────┬───────┴────┬─────┘
         │              │               │            │
         ▼              ▼               ▼            ▼
   ┌───────────┐  ┌──────────┐  ┌─────────────┐  ┌─────┐
   │Kubernetes │  │Prometheus│  │Kubernetes   │  │ LLM │
   │  Watch    │  │  API     │  │Metrics API  │  │ API │
   │  API      │  │          │  │+ SSA Apply  │  │     │
   └───────────┘  └──────────┘  └─────────────┘  └─────┘
```

See **[docs/architecture.md](docs/architecture.md)** for details.

---

## Installation

### From Source

Requires Go >= 1.25

```bash
git clone https://github.com/ppiankov/kubenow
cd kubenow
make build
sudo mv bin/kubenow /usr/local/bin/
```

### Binary Downloads

Download from [GitHub Releases](https://github.com/ppiankov/kubenow/releases/latest).

Available for Linux (amd64, arm64), macOS (amd64, arm64), and Windows (amd64).

```bash
kubenow version
# kubenow version 0.2.5
```

---

## Prometheus Connection

```bash
# Port-forward (recommended for local analysis)
kubectl port-forward -n monitoring svc/prometheus-operated 9090:9090
kubenow analyze requests-skew --prometheus-url http://127.0.0.1:9090

# Auto-detect in-cluster Prometheus
kubenow analyze requests-skew --auto-detect-prometheus

# Via Kubernetes service
kubenow analyze requests-skew --k8s-service prometheus-operated --k8s-namespace monitoring
```

Use `http://127.0.0.1:9090` (not `http://prometheus:9090`) for port-forward. Analysis is read-only.

---

## Troubleshooting

### "0 workloads analyzed" in requests-skew

1. **Missing metrics**: Check `container_cpu_usage_seconds_total` exists in Prometheus
2. **Namespace has no Prometheus data**: Use `pro-monitor latch` for workloads in unscraped namespaces
3. **Time window too old**: Try `--window 7d`
4. **Prometheus unreachable**: Test with `curl http://127.0.0.1:9090/api/v1/query?query=up`

### Pro-Monitor issues

1. **"No policy file found"**: Pass `--policy path/to/policy.yaml` or set env var
2. **"Audit path not writable"**: Ensure the audit directory exists and is writable
3. **"Latch data stale"**: Re-run latch — data expires after policy MaxLatchAge (default 7 days)
4. **"Apply denied: HPA detected"**: Pass `--acknowledge-hpa` if HPA conflict is acceptable

---

## CI/CD Integration

```bash
# Silent JSON output for pipelines
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --silent --output json --export-file results.json

# SARIF for GitHub Security tab
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --output sarif --export-file results.sarif

# Fail pipeline on critical issues
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --fail-on critical
```

---

## Known Limitations

- Prometheus metrics required for `requests-skew` (Metrics API alone is insufficient for historical data)
- Pro-Monitor apply limited to Deployment, StatefulSet, DaemonSet (Pod apply blocked — managed by controllers)
- CRD operator detection relies on well-known pod labels; custom operators need `app.kubernetes.io/managed-by`
- LLM analysis quality depends on the model used

## Roadmap

See [CHANGELOG.md](CHANGELOG.md) for version history. Planned:
- Auto-detect Prometheus in-cluster
- Cloud provider cost integration (AWS, GCP, Azure)
- Historical trend tracking

---

## Philosophy

This tool follows the principles of **Attention-First Software**:

> The primary responsibility of software is to disappear once it works correctly.

- Deterministic analysis over prescriptive recommendations
- Evidence-based outputs ("this would have worked") not predictions ("you should do this")
- Actions are reversible; irreversible ones require explicit consent and structural safeguards
- Tools present evidence and let users decide — mirrors, not oracles

Read the full manifesto: **[MANIFESTO.md](MANIFESTO.md)**

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, testing guidelines, and code style.

---

## License

MIT License — see [LICENSE](LICENSE) for details.

---

## Documentation

- [Architecture](docs/architecture.md)
- [Pro-Monitor Spec](docs/spec-pro-monitor-v0.2.0.md)
- [Spike Analysis Guide](SPIKE-ANALYSIS.md)
- [Changelog](CHANGELOG.md)
- [Contributing](CONTRIBUTING.md)
- [Manifesto](MANIFESTO.md)
