![kubenow logo](https://raw.githubusercontent.com/ppiankov/kubenow/main/docs/img/logo.png)

# 🧯 kubenow — Kubernetes Cluster Analysis & Cost Optimization

[![CI](https://github.com/ppiankov/kubenow/workflows/CI/badge.svg)](https://github.com/ppiankov/kubenow/actions)
[![Go Report Card](https://goreportcard.com/badge/github.com/ppiankov/kubenow)](https://goreportcard.com/report/github.com/ppiankov/kubenow)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

**Version 0.1.1** — Enhanced spike detection for AI/RAG workloads! "11434 is enough. But what if fewer nodes are enough too?"

---

## What is kubenow?

**kubenow** is a Kubernetes cluster analysis tool that combines:
- **Real-time problem monitoring** like `top` for cluster issues
- **Deterministic, evidence-based analysis** for cost and capacity decisions
- **Optional LLM-assisted analysis** for incident triage and diagnostics

### **Monitor Mode** (Live)
Real-time terminal UI for cluster problems:
- **Attention-first**: Empty screen when healthy, shows only broken things
- **Compact view**: 10-15 problems at once, sortable (severity/recency/count)
- **Interactive**: Pause, scroll, sort, copy to terminal
- **No flicker**: Updates only when problems change
- **Copyable**: Press `c` to dump everything to terminal for easy copying

```bash
kubenow monitor                    # Start monitoring
# Press 1/2/3 to sort, ↑↓ to scroll, c to copy, q to quit
```

**When to use:** Answering "what's broken RIGHT NOW?" in clusters with multiple problems. Complements k9s (exploration), kubectl (investigation), and Grafana (metrics).

See **[MONITOR-MODE.md](MONITOR-MODE.md)** for full documentation and tool comparison.

### **Deterministic Analysis** (Core)
Data-driven cost optimization without AI:
- **requests-skew**: Identify over-provisioned resources
- **node-footprint**: Historical capacity feasibility simulation

### **LLM-Powered Analysis** (Optional)
Feed your cluster snapshot into any OpenAI-compatible LLM for:
- Incident triage (ranked, actionable, command-ready)
- Pod-level debugging
- Cluster health summaries
- Teamlead / ownership recommendations
- Compliance / hygiene reviews
- Chaos engineering suggestions

---

## Works with any OpenAI-compatible API:
- **Ollama** (Mixtral, Llama, Qwen, etc.)
- **OpenAI / Azure OpenAI**
- **DeepSeek / Groq / Together / OpenRouter**
- Your own inference server

If it responds to `/v1/chat/completions`, kubenow will talk to it.

---

# Why kubenow?

## Because when the cluster is on fire, nobody wants to run:
- 12 commands
- across 5 namespaces
- using 4 terminals
- while Slack is screaming

## You want:
```bash
TOP ISSUES:
1. callback/data-converter-worker — ImagePullBackOff — critical
2. payments-api — CrashLoopBackOff — high

ROOT CAUSES:
1. Private registry unreachable from nodes.
2. readinessProbe fails immediately.

FIX COMMANDS:
kubectl -n callback get events
kubectl -n callback set image deploy/data-converter-worker api=repo/worker:stable
```

**Short, ranked, actionable.**

## And when you want to save money:
```bash
=== Requests-Skew Analysis ===
Top over-provisioned workloads:

NAMESPACE    WORKLOAD         REQ CPU  AVG CPU  SKEW     IMPACT
prod         payment-api      4.0      0.5      8.0x     HIGH
prod         checkout-worker  2.0      0.3      6.7x     MED

Summary: 6.5 cores wasted, ~$200/month
```

**Evidence-based, reproducible, non-prescriptive.**

---

# Features

## LLM-Powered Analysis

### Incident Mode
Ranks top problems with:
- 1-2 sentence root causes
- Actionable kubectl commands
- Zero fluff, zero theory

### Pod Mode
Deep dive into broken pods:
- Container states, events, restarts
- Image pulls, OOMs, last logs

### Teamlead Mode
Manager-friendly reports:
- Risk assessment, blast radius
- Ownership hints, escalation guidance

### Compliance Mode
Policy / hygiene checks:
- Missing resource limits
- `:latest` tags, namespace misuse
- Registry hygiene, bad env patterns

### Chaos Mode
Suggests experiments based on real weaknesses:
- Node drain, registry outage simulation
- Disruption tests, restart storms

### Smart Filtering
- Include/Exclude pods by pattern (wildcards)
- Include/Exclude namespaces
- Keyword-based log/event filtering
- Problem hints to guide LLM
- Concurrent log fetching

### Power-User Enhancements
- `--enhance-technical`: Stack traces, config diffs
- `--enhance-priority`: Numerical scores, SLO impact
- `--enhance-remediation`: Step-by-step fixes

### Watch Mode
- Continuous monitoring with intervals
- Diff detection (new/resolved/ongoing)
- Alert-only mode for new issues
- Graceful shutdown (Ctrl+C)

### Export Reports
- Save to JSON, Markdown, HTML, Plain Text
- Auto-format detection by extension
- Metadata included (timestamp, version, cluster, filters)

---

## Deterministic Analysis (NEW in v0.1.0)

### requests-skew: Find Over-Provisioned Resources

**Analyzes resource requests vs actual Prometheus metrics.**

```bash
kubenow analyze requests-skew --prometheus-url http://prometheus:9090
```

**Philosophy:**
- ✅ Deterministic: No AI, no prediction
- ✅ Evidence-based: Historical metrics over time window
- ✅ Non-prescriptive: Shows "this would have worked" not "you should do this"
- ✅ Safety-first: Detects OOMKills, restarts, spikes before recommending reductions

**Important:** kubenow never makes future safety guarantees. All `analyze` commands describe
what *would have worked historically* based on observed data, not what *will*
work under all future conditions.

**Output:**
```
NAMESPACE  WORKLOAD         REQ CPU  P99 CPU  SKEW   SAFETY      IMPACT
prod       payment-api      4.0      3.8      8.0x   ⚠ RISKY     HIGH (42.5)
prod       checkout-worker  2.0      0.5      6.7x   ✓ SAFE      MED (18.2)
prod       batch-job        8.0      7.9      8.0x   ✗ UNSAFE    HIGH (65.0)

Summary:
  Average CPU Skew: 7.5x
  Total Wasted CPU: 10.5 cores

⚠️  Safety Warnings:
═══════════════════

✗ UNSAFE (1 workload) - DO NOT REDUCE RESOURCES:
  • prod/batch-job
    - ⚠️ p99 usage at 99% of request
    - ⚠️ 3 OOMKills in window
    - ⚠️ 15 restarts in window

⚠ RISKY (1 workload) - Review carefully:
  • prod/payment-api (safety margin: 1.5x)
    - ⚠️ p99 CPU usage at 95% of request
    - ⚠️ CPU throttled 12.5% of time
```

**Key Features:**
- **Safety Analysis**: Detects OOMKills, restarts, CPU throttling, spike patterns (p99, p99.9, max)
- **Safety Ratings**: SAFE ✓ | CAUTION ⚠️ | RISKY ⚠️ | UNSAFE ✗ with automatic safety margins
- **Real-Time Spike Monitoring**: Optional high-frequency sampling (1-5s) to catch sub-scrape-interval bursts
- Time window analysis (default 30 days)
- Percentile analysis (p95, p99, p99.9, max)
- Namespace filtering with regex
- Top N results
- Impact scoring (skew × absolute resources)
- JSON and table output
- Export to file

**Spike Monitoring:**

Use `--watch-for-spikes` for real-time spike detection (ideal for AI/RAG workloads):

```bash
# Basic spike monitoring (15 minutes, 5-second sampling)
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --watch-for-spikes

# Show calculated recommendations based on spike data
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --watch-for-spikes \
  --show-recommendations

# For RAG workloads: use 1-second sampling to catch millisecond bursts
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --watch-for-spikes \
  --spike-interval 1s \
  --spike-duration 30m \
  --show-recommendations \
  --safety-factor 2.5
```

**Output with recommendations:**
```
📊 Real-Time Spike Monitoring Results:
════════════════════════════════════

⚠️  Detected 3 workloads with CPU spikes > 2x average:

NAMESPACE/WORKLOAD       AVG CPU  MAX CPU  SPIKE RATIO  RECOMMENDED CPU  SAFETY FACTOR
prod/exchange-rates      0.020    0.412    21.0x        1.03 cores       2.5x
prod/config-service      0.020    0.623    31.2x        1.56 cores       2.5x
prod/illumination        0.040    0.752    18.8x        1.50 cores       2.0x

💡 How to Use These Recommendations:
════════════════════════════════════

Formula: CPU Request = Max Observed CPU × Safety Factor

Safety factor auto-selected based on spike ratio:
  • Spike ≥20x: 2.5x (extreme bursts, e.g., RAG/AI inference)
  • Spike 10-20x: 2.0x (high bursts, e.g., batch jobs)
  • Spike 5-10x: 1.5x (moderate bursts, e.g., APIs)
  • Spike 2-5x: 1.2x (low bursts, e.g., background workers)

See SPIKE-ANALYSIS.md for comprehensive guidance.
```

**For detailed spike analysis guidance**, see **[SPIKE-ANALYSIS.md](SPIKE-ANALYSIS.md)** which covers:
- How to calculate resource requests from spike data
- Safety factor selection by workload type
- Step-by-step sizing examples with kubectl commands
- Common patterns (RAG/AI inference, API caching, etc.)
- Troubleshooting throttling and missing metrics

---

### node-footprint: Historical Capacity Feasibility Simulation

**Bin-packing simulation to test smaller node configurations.**

```bash
kubenow analyze node-footprint --prometheus-url http://prometheus:9090
```

**Philosophy:**
- ✅ Simulation-based: Tests alternatives against historical data
- ✅ Evidence-based: Claims "this would have worked historically"
- ✅ Never prescriptive: Doesn't say "you should do this"
- ✅ Reproducible: Same inputs → same outputs

**Output:**
```
Current Topology:
  Node Type: c5.xlarge
  Node Count: 25
  Avg CPU Utilization: 42%

Workload Envelope (p95):
  Total CPU Required: 42.0 cores
  Pod Count: 87

Scenarios:
SCENARIO                 NODES  AVG CPU%  AVG MEM%  FEASIBILITY  NOTES
Current (c5.xlarge)       25     42%       38%       -            Current topology
Alt 1 (c5.2xlarge)        14     78%       72%       YES          44% fewer nodes
Alt 2 (r5.2xlarge)        12     68%       81%       YES (tight)  Memory-optimized
Alt 3 (c5.4xlarge)         8     85%       76%       NO           Insufficient CPU
```

**Key Features:**
- Prometheus-based workload envelope (p50/p95/p99)
- First-Fit Decreasing bin-packing algorithm
- Feasibility checks with reasons
- Headroom calculation (high/medium/low)
- Custom node types support
- Estimated savings
- JSON and table output

---

# 📦 Installation

## Option 1: Download Binary (Recommended)

**Linux (amd64):**
```bash
curl -LO https://github.com/ppiankov/kubenow/releases/latest/download/kubenow_0.1.1_linux_amd64.tar.gz
tar -xzf kubenow_0.1.1_linux_amd64.tar.gz
sudo mv kubenow /usr/local/bin/
```

**macOS (amd64):**
```bash
curl -LO https://github.com/ppiankov/kubenow/releases/latest/download/kubenow_0.1.1_darwin_amd64.tar.gz
tar -xzf kubenow_0.1.1_darwin_amd64.tar.gz
sudo mv kubenow /usr/local/bin/
```

**macOS (arm64 / Apple Silicon):**
```bash
curl -LO https://github.com/ppiankov/kubenow/releases/latest/download/kubenow_0.1.1_darwin_arm64.tar.gz
tar -xzf kubenow_0.1.1_darwin_arm64.tar.gz
sudo mv kubenow /usr/local/bin/
```

## Option 2: Build from Source

Requires Go ≥ 1.21

```bash
git clone https://github.com/ppiankov/kubenow
cd kubenow
make build
sudo mv bin/kubenow /usr/local/bin/
```

## Verify Installation

```bash
kubenow version
# kubenow version 0.1.1
# Go version: go1.23.0
# OS/Arch: darwin/arm64
```

---

# Usage

## LLM-Powered Analysis

### Basic Incident Triage
```bash
kubenow incident \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

### Pod Debugging
```bash
kubenow pod \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b \
  --namespace production
```

### With OpenAI
```bash
export OPENAI_API_KEY="sk-yourkey"

kubenow teamlead \
  --llm-endpoint https://api.openai.com/v1 \
  --model gpt-4.1-mini
```

### Filter Specific Pods
```bash
kubenow incident \
  --include-pods "payment-*,checkout-*" \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

### Memory Issue Investigation
```bash
kubenow incident \
  --include-keywords "OOM,memory,killed" \
  --hint "memory leak investigation" \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

### Power-User Mode
```bash
kubenow incident \
  --enhance-technical \
  --enhance-priority \
  --enhance-remediation \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

### Watch Mode (Continuous Monitoring)
```bash
kubenow incident \
  --watch-interval 1m \
  --watch-alert-new-only \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

### Export to Markdown
```bash
kubenow incident \
  --output incident-report.md \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

---

## Deterministic Analysis (NEW)

### Find Over-Provisioned Resources
```bash
# Basic analysis (30-day window)
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090

# Focus on production namespaces, 7-day window
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --window 7d \
  --namespace-regex "prod.*"

# Top 20 results as JSON
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --top 20 \
  --output json

# Export to file
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --export-file overprovisioned-report.json

# Real-time spike monitoring (catch sub-second bursts)
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --watch-for-spikes \
  --spike-duration 15m \
  --spike-interval 5s

# CI/CD integration (silent mode, JSON output)
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --silent \
  --output json \
  --export-file analysis-results.json
```

### Simulate Alternative Node Topologies
```bash
# Basic analysis with default node types
kubenow analyze node-footprint \
  --prometheus-url http://prometheus:9090

# Use p99 workload envelope (more conservative)
kubenow analyze node-footprint \
  --prometheus-url http://prometheus:9090 \
  --percentile p99

# Custom node types
kubenow analyze node-footprint \
  --prometheus-url http://prometheus:9090 \
  --node-types "c5.large,c5.xlarge,c5.2xlarge,r5.2xlarge"

# Export results
kubenow analyze node-footprint \
  --prometheus-url http://prometheus:9090 \
  --output json \
  --export-file node-footprint.json
```

### Prometheus Connection Methods

**Port-forward (recommended for local analysis):**
```bash
# Find your Prometheus service
kubectl get svc -A | grep prometheus

# Common patterns:
# - kube-prometheus-stack: prometheus-operated or kube-prometheus-stack-prometheus
# - prometheus-operator: prometheus-operated
# - default: prometheus-server

# Example: kube-prometheus-stack
kubectl port-forward -n kube-prometheus-stack svc/prometheus-operated 9090:9090

# In another terminal (use 127.0.0.1, not localhost)
kubenow analyze requests-skew --prometheus-url http://127.0.0.1:9090
```

**Direct URL (external Prometheus):**
```bash
kubenow analyze requests-skew \
  --prometheus-url https://prometheus.example.com
```

**Important Notes:**
- Use `http://127.0.0.1:9090` (not `http://prometheus:9090`) for port-forward
- Analysis is **read-only** - safe to run on production clusters
- No concurrent load - queries run sequentially
- If you see "0 workloads analyzed", check that Prometheus has `container_cpu_usage_seconds_total` metrics

---

# Troubleshooting

## "0 workloads analyzed" in requests-skew

**Symptoms:** `Analyzed: 0 workloads | Top: 0`

**Causes & Fixes:**

1. **Missing Prometheus metrics**
   ```bash
   # Check if metrics exist
   curl -s "http://127.0.0.1:9090/api/v1/label/__name__/values" | grep container_cpu

   # Should see: container_cpu_usage_seconds_total
   ```

   **Fix:** Install metrics-server or ensure cAdvisor metrics are being scraped.

2. **Time window too old**
   ```bash
   # Try shorter window
   kubenow analyze requests-skew --prometheus-url http://127.0.0.1:9090 --window 7d
   ```

3. **No workloads match filters**
   ```bash
   # Check without filters
   kubenow analyze requests-skew --prometheus-url http://127.0.0.1:9090 \
     --namespace-regex ".*" --min-runtime-days 0
   ```

4. **Prometheus not reachable**
   ```bash
   # Test Prometheus directly
   curl http://127.0.0.1:9090/api/v1/query?query=up
   ```

## Why node-footprint works but requests-skew doesn't

- **node-footprint**: Uses Kubernetes API directly (always works if kubectl works)
- **requests-skew**: Requires historical Prometheus metrics (depends on scrape config)

If node-footprint shows results but requests-skew doesn't, your Prometheus likely isn't scraping container metrics or they're named differently.

---

# CI/CD Integration

## Silent Mode for Pipelines

Use `--silent` flag to suppress progress output (perfect for Jenkins, GitLab CI, GitHub Actions, etc.):

```bash
# Silent JSON output (no stderr progress)
kubenow analyze requests-skew \
  --prometheus-url http://prometheus:9090 \
  --silent \
  --output json \
  --export-file results.json

# Exit codes
# 0 = Success
# 2 = Invalid input
# 3 = Runtime error (Prometheus unreachable, etc.)
```

## Example: GitHub Actions Workflow

```yaml
name: Cost Analysis
on:
  schedule:
    - cron: '0 9 * * MON'  # Every Monday at 9am

jobs:
  analyze-cost:
    runs-on: ubuntu-latest
    steps:
      - name: Download kubenow
        run: |
          curl -LO https://github.com/ppiankov/kubenow/releases/latest/download/kubenow_0.1.1_linux_amd64.tar.gz
          tar -xzf kubenow_0.1.1_linux_amd64.tar.gz
          chmod +x kubenow

      - name: Port-forward Prometheus
        run: |
          kubectl port-forward -n monitoring svc/prometheus 9090:9090 &
          sleep 5

      - name: Run Analysis
        run: |
          ./kubenow analyze requests-skew \
            --prometheus-url http://127.0.0.1:9090 \
            --silent \
            --output json \
            --export-file cost-analysis.json

      - name: Upload Results
        uses: actions/upload-artifact@v3
        with:
          name: cost-analysis
          path: cost-analysis.json
```

## Example: Jenkins Pipeline

```groovy
pipeline {
    agent any
    stages {
        stage('Cost Analysis') {
            steps {
                sh '''
                    kubectl port-forward -n monitoring svc/prometheus 9090:9090 &
                    sleep 5

                    ./kubenow analyze requests-skew \
                        --prometheus-url http://127.0.0.1:9090 \
                        --silent \
                        --output json \
                        --export-file cost-analysis.json

                    # Parse and report
                    jq '.summary.total_wasted_cpu' cost-analysis.json
                '''
            }
        }
    }
}
```

---

# 🧠 Recommended Models

| Mode | Best Local | Best Cloud | Notes |
|------|------------|------------|-------|
| incident | mixtral:8x22b | GPT-4.1 Mini | Concise, obedient |
| pod | llama3:70b | GPT-4.1 | Detail friendly |
| teamlead | mixtral:8x22b | GPT-4.1 Mini | Manager-appropriate |
| compliance | qwen2.5:32b | GPT-4.1 | Policy-focused |
| chaos | mixtral:8x22b | Claude Sonnet | Creative thinking |

**Note:** Deterministic `analyze` commands don't use LLMs.

---

# Architecture

```
┌─────────────────────────────────────────────────────┐
│                   kubenow CLI                       │
├─────────────────────────────────────────────────────┤
│                                                     │
│  LLM Commands           │   Analyze Commands        │
│  ├─ incident            │   ├─ requests-skew        │
│  ├─ pod                 │   └─ node-footprint       │
│  ├─ teamlead            │                           │
│  ├─ compliance          │   Prometheus Metrics      │
│  └─ chaos               │   Bin-Packing Simulation  │
│                         │   Deterministic Analysis  │
│  Kubernetes Snapshot    │                           │
│  LLM Analysis           │                           │
└─────────────────────────────────────────────────────┘
           │                          │
           ▼                          ▼
    ┌─────────────┐          ┌──────────────┐
    │ Kubernetes  │          │ Prometheus   │
    │     API     │          │     API      │
    └─────────────┘          └──────────────┘
```

**See [Architecture Documentation](docs/architecture.md) for details.**

---

# 📚 Documentation

- **[Architecture](docs/architecture.md)** - System design and components
- **[Contributing](CONTRIBUTING.md)** - Development guide
- **[Changelog](CHANGELOG.md)** - Version history
- **[Manifesto](MANIFESTO.md)** - Design philosophy

---

# 🧭 Philosophy

This tool follows the principles of **Attention-First Software**:

> The primary responsibility of software is to disappear once it works correctly.

kubenow is built to reduce cognitive load, not monetize attention:
- Deterministic analysis over prescriptive recommendations
- Evidence-based outputs ("this would have worked") not predictions ("you should do this")
- Silent mode for automation, clear progress for humans
- No upsells, no engagement mechanics, no hype

Read the full manifesto: **[MANIFESTO.md](MANIFESTO.md)**

---

# 🤝 Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for:
- Development setup
- Testing guidelines
- Pull request process
- Code style conventions

---

# 📝 License

MIT License - see [LICENSE](LICENSE) file for details.

---

**"11434 is enough. But fewer nodes might be enough too."**
