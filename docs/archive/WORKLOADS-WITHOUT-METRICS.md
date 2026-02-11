# Workloads Without Metrics - Detection & Latch Mode Recommendation

## The Problem: Silent Gaps in Analysis

When running `kubenow analyze requests-skew`, you might have workloads that exist in Kubernetes but have **NO corresponding Prometheus metrics**. This creates a silent gap in your analysis:

- ✅ Workloads found in K8s API: 50 deployments
- ❌ Workloads with Prometheus metrics: 32 deployments
- ⚠️ **Gap**: 18 deployments with no metrics (previously invisible)

---

## Why This Happens

### Common Causes

1. **Container metrics not being scraped**
   - cAdvisor endpoint missing from ServiceMonitor
   - Prometheus not scraping container metrics at all

2. **Workload too new**
   - Created after the analysis window
   - No historical data accumulated yet

3. **Pods not running**
   - Crash loops, pending state
   - No active pods to generate metrics

4. **ServiceMonitor misconfiguration**
   - Missing `cadvisor` port in endpoints
   - Incorrect scrape path

---

## The Solution: Detection + Latch Mode

### What We Implemented (v0.1.1)

**Automatic Detection**:
- Track workloads found in K8s but missing from Prometheus
- Display clear warnings with workload details
- Explain why metrics might be missing

**Latch Mode Recommendation**:
- Suggest using `--watch-for-spikes` for real-time monitoring
- Sample Kubernetes Metrics API directly (bypasses Prometheus)
- Catch sub-scrape-interval spikes Prometheus misses

---

## Example Output

### When Workloads Without Metrics Detected

```bash
$ kubenow analyze requests-skew --prometheus-url http://127.0.0.1:9090

[kubenow] Discovering namespaces...
[kubenow] Found 57 namespaces to analyze
[kubenow] [1/57] Analyzing namespace: production
[kubenow]   → Found 12 workloads with metrics
[kubenow]   → Found 3 workloads WITHOUT metrics
...

=== Requests-Skew Analysis ===
Window: 30d | Analyzed: 32 workloads | Top: 10

NAMESPACE    WORKLOAD        REQ CPU  P99 CPU  SKEW   SAFETY    IMPACT
production   payment-api     4.0      0.8      5.0x   ✓ SAFE    HIGH (42.5)
...

Summary:
  Average CPU Skew: 4.2x
  Average Memory Skew: 3.8x
  Total Wasted CPU: 18.5 cores
  Total Wasted Memory: 32.4GiB

⚠️  Workloads Without Prometheus Metrics:
══════════════════════════════════════════

Found 18 workload(s) in Kubernetes but NO corresponding Prometheus metrics:

  Namespace: production
    • batch-processor (Deployment)
    • webhook-handler (Deployment)
    • cron-worker (CronJob)

  Namespace: staging
    • test-runner (Deployment)
    • load-generator (StatefulSet)

Possible Causes:
  • Container metrics not being scraped by Prometheus
  • ServiceMonitor/PodMonitor missing cAdvisor endpoint
  • Workload too new (created after analysis window)
  • Pods in crash loops or not running

💡 Recommended Action - Use Latch Mode:
═══════════════════════════════════════════

Since these workloads lack Prometheus metrics, you can use LATCH MODE
to monitor them in real-time via the Kubernetes Metrics API:

  kubenow analyze requests-skew \
    --prometheus-url http://127.0.0.1:9090 \
    --watch-for-spikes \
    --spike-duration 15m \
    --spike-interval 5s

What Latch Mode Does:
  • Samples Kubernetes Metrics API at high frequency (default: 5s)
  • Captures sub-scrape-interval spikes (< 15-30s) that Prometheus misses
  • Useful for bursty workloads (AI inference, RAG, batch jobs)
  • Provides real-time data when historical metrics unavailable

Troubleshooting Missing Metrics:
  1. Check ServiceMonitor configuration:
     kubectl get servicemonitor -n kube-prometheus-stack kubelet -o yaml

  2. Verify cAdvisor endpoint exists:
     Look for: endpoints[].port=cadvisor, path=/metrics/cadvisor

  3. Check Prometheus targets:
     kubectl port-forward -n kube-prometheus-stack svc/prometheus-operated 9090:9090
     # Open: http://localhost:9090/targets

See README troubleshooting section for detailed guidance.
```

---

## How It Works (Technical Implementation)

### Detection Logic

**1. Track workloads during analysis** (`internal/analyzer/requests_skew.go`):

```go
type RequestsSkewResult struct {
    Metadata              RequestsSkewMetadata
    Summary               RequestsSkewSummary
    Results               []WorkloadSkewAnalysis
    WorkloadsWithoutMetrics []WorkloadWithoutMetrics  // NEW
}

type WorkloadWithoutMetrics struct {
    Namespace string
    Workload  string
    Type      string // Deployment, StatefulSet, etc.
}
```

**2. Modified analyzeWorkload to distinguish "no metrics" from "error"**:

```go
// Returns: (*analysis, hasMetrics, error)
func (a *RequestsSkewAnalyzer) analyzeWorkload(...) (*WorkloadSkewAnalysis, bool, error) {
    usage, err := a.metricsProvider.GetWorkloadResourceUsage(...)
    if err != nil {
        return nil, true, err  // Error fetching metrics
    }

    // No usage data = workload exists but no metrics
    if usage.CPUAvg == 0 && usage.MemoryAvg == 0 {
        return nil, false, nil  // hasMetrics = false
    }

    // ... normal analysis
    return analysis, true, nil
}
```

**3. Collect workloads without metrics**:

```go
func (a *RequestsSkewAnalyzer) analyzeNamespace(...) ([]WorkloadSkewAnalysis, []WorkloadWithoutMetrics, error) {
    workloads := make([]WorkloadSkewAnalysis, 0)
    noMetrics := make([]WorkloadWithoutMetrics, 0)

    for _, deploy := range deployments.Items {
        analysis, hasMetrics, err := a.analyzeWorkload(...)

        if !hasMetrics {
            noMetrics = append(noMetrics, WorkloadWithoutMetrics{
                Namespace: namespace,
                Workload:  deploy.Name,
                Type:      "Deployment",
            })
        } else if analysis != nil {
            workloads = append(workloads, *analysis)
        }
    }

    return workloads, noMetrics, nil
}
```

**4. Display warnings** (`internal/cli/analyze_requests_skew.go`):

```go
func printWorkloadsWithoutMetricsWarning(result *analyzer.RequestsSkewResult) {
    if len(result.WorkloadsWithoutMetrics) == 0 {
        return // Nothing to warn about
    }

    // Print warning with:
    // - List of workloads missing metrics
    // - Possible causes
    // - Latch mode recommendation
    // - Troubleshooting steps
}
```

---

## When to Use Latch Mode

### Perfect Use Cases

1. **Workloads with no Prometheus metrics**
   - ServiceMonitor not configured
   - Metrics not being scraped
   - Want baseline data NOW without waiting for Prometheus

2. **⚡ RAG workloads (CRITICAL - Use ≤1s sampling)**
   - RAG queries: 100-500ms CPU spikes
   - Vector search + LLM generation bursts
   - Real-time inference APIs
   - **Must use `--spike-interval 1s` or less**
   - 5s default sampling will miss most spikes

3. **Other bursty AI/ML workloads**
   - Synchronous AI inference (non-RAG)
   - Batch processing with sub-minute bursts
   - Model serving with request spikes

4. **Sub-scrape-interval spikes**
   - Prometheus scrapes every 15-30s
   - Your spikes are < 10s
   - Latch mode samples at 1-5s intervals

5. **Real-time troubleshooting**
   - Debugging resource issues NOW
   - Can't wait for 24h of Prometheus data
   - Need high-frequency sampling

### When NOT to Use Latch Mode

1. **Historical analysis** - Use `requests-skew` with Prometheus
2. **Long-running steady workloads** - Prometheus is sufficient
3. **Production-wide scans** - Latch mode is targeted, not cluster-wide

---

## Latch Mode Command Reference

### Basic Usage

```bash
# Monitor for 15 minutes at 5-second intervals
kubenow analyze requests-skew \
  --prometheus-url http://127.0.0.1:9090 \
  --watch-for-spikes \
  --spike-duration 15m \
  --spike-interval 5s
```

### Common Patterns

```bash
# Quick check (5 minutes, fast sampling)
kubenow analyze requests-skew \
  --prometheus-url http://127.0.0.1:9090 \
  --watch-for-spikes \
  --spike-duration 5m \
  --spike-interval 1s

# Extended monitoring (1 hour, moderate sampling)
kubenow analyze requests-skew \
  --prometheus-url http://127.0.0.1:9090 \
  --watch-for-spikes \
  --spike-duration 1h \
  --spike-interval 10s

# Focus on specific namespace
kubenow analyze requests-skew \
  --prometheus-url http://127.0.0.1:9090 \
  --namespace-regex "production" \
  --watch-for-spikes \
  --spike-duration 30m
```

### ⚡ Special Case: RAG Workloads

**CRITICAL**: RAG (Retrieval-Augmented Generation) queries are **extremely** bursty:
- Query arrives → Spike to 100% CPU for 50-500ms → Drop back to idle
- Default 5s sampling **will miss these spikes entirely**

**For RAG workloads, you MUST use 1s or sub-1s sampling**:

```bash
# RAG workload monitoring (1s sampling - REQUIRED)
kubenow analyze requests-skew \
  --prometheus-url http://127.0.0.1:9090 \
  --watch-for-spikes \
  --spike-duration 30m \
  --spike-interval 1s    # ← Critical! Do not use 5s for RAG

# For extremely fast RAG queries (sub-second sampling)
kubenow analyze requests-skew \
  --prometheus-url http://127.0.0.1:9090 \
  --watch-for-spikes \
  --spike-duration 15m \
  --spike-interval 500ms  # 0.5s sampling for ultra-fast queries
```

**Why this matters**:
- RAG queries typically complete in 100-500ms
- 5s sampling might only catch 1 sample during a spike (or miss it entirely)
- 1s sampling ensures you catch multiple samples per query
- Sub-1s sampling provides detailed spike profiles

**Workload types that need ≤1s sampling**:
- RAG systems (vector search + LLM generation)
- Real-time AI inference APIs
- Synchronous embedding generation
- Live search augmentation
- Any request/response AI workload with <1s SLA

---

## Troubleshooting Missing Metrics

### Step 1: Check ServiceMonitor

```bash
kubectl get servicemonitor -n kube-prometheus-stack
```

Look for `kubelet` ServiceMonitor:

```bash
kubectl get servicemonitor kubelet -n kube-prometheus-stack -o yaml
```

**Should have cAdvisor endpoint**:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: kubelet
spec:
  endpoints:
  - port: https-metrics    # Kubelet metrics
    scheme: https
  - port: cadvisor          # Container metrics ← THIS ONE!
    path: /metrics/cadvisor
    scheme: https
    tlsConfig:
      insecureSkipVerify: true
```

### Step 2: Check Prometheus Targets

```bash
# Port-forward to Prometheus
kubectl port-forward -n kube-prometheus-stack svc/prometheus-operated 9090:9090

# Open in browser: http://localhost:9090/targets
# Look for: kubelet targets with /metrics/cadvisor endpoint
```

**Expected**:
- Target: `kubelet/kubelet/0 (node-name:10250/metrics/cadvisor)`
- State: UP
- Labels: `job="kubelet"`, `metrics_path="/metrics/cadvisor"`

### Step 3: Verify Metrics Exist

```bash
# Query Prometheus for container CPU metrics
curl -s "http://127.0.0.1:9090/api/v1/label/__name__/values" | \
  jq -r '.data[]' | grep container_cpu

# Should see:
# container_cpu_usage_seconds_total
# container_cpu_system_seconds_total
# ...
```

### Step 4: Fix Missing cAdvisor Endpoint

If cAdvisor endpoint is missing, add it:

```bash
kubectl edit servicemonitor kubelet -n kube-prometheus-stack
```

Add under `spec.endpoints`:

```yaml
- port: cadvisor
  path: /metrics/cadvisor
  scheme: https
  tlsConfig:
    insecureSkipVerify: true
```

Wait 1-2 minutes for Prometheus to reload config and start scraping.

---

## JSON Output

When using `--output json`, workloads without metrics are included:

```json
{
  "metadata": {
    "window": "30d",
    "min_runtime_days": 7,
    "generated_at": "2026-01-30T15:30:00Z"
  },
  "summary": {
    "total_workloads": 50,
    "analyzed_workloads": 32,
    "skipped_workloads": 18
  },
  "results": [ ... ],
  "workloads_without_metrics": [
    {
      "namespace": "production",
      "workload": "batch-processor",
      "type": "Deployment"
    },
    {
      "namespace": "production",
      "workload": "webhook-handler",
      "type": "Deployment"
    }
  ]
}
```

Parse with `jq`:

```bash
kubenow analyze requests-skew \
  --prometheus-url http://127.0.0.1:9090 \
  --output json | \
  jq '.workloads_without_metrics | length'

# Output: 18
```

---

## Benefits

### Before This Feature

```
[kubenow] Analyzed: 32 workloads
```

❌ Silent about 18 missing workloads
❌ No guidance on what to do
❌ Users confused why workload count doesn't match

### After This Feature

```
[kubenow] Found 32 workloads with metrics
[kubenow] Found 18 workloads WITHOUT metrics

⚠️  Workloads Without Prometheus Metrics:
...
💡 Recommended Action - Use Latch Mode:
  kubenow analyze requests-skew --watch-for-spikes ...
```

✅ Explicit about missing workloads
✅ Clear recommendation (latch mode)
✅ Troubleshooting guidance
✅ Actionable next steps

---

## Philosophy

This feature aligns with kubenow's core principles:

1. **Transparency**: Never hide missing data
2. **Actionable**: Provide clear next steps (latch mode)
3. **Non-prescriptive**: Explain options, don't force decisions
4. **Evidence-based**: Show what's missing, why, and how to fix

---

## Next Steps

If you see workloads without metrics:

1. **Quick fix**: Use latch mode to get real-time data NOW
2. **Long-term fix**: Fix ServiceMonitor to enable Prometheus scraping
3. **Investigate**: Check if pods are actually running
4. **Monitor**: Re-run analysis after fixes to verify metrics appear

For detailed Prometheus troubleshooting, see:
- `METRIC-DISCOVERY.md` - How metric discovery works
- `README.md` - Troubleshooting section
- `LATCH-MODE.md` - Deep dive on real-time spike monitoring
