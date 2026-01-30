# Metric Discovery: How kubenow Finds Your Metrics

## The Problem You Identified

**You asked the right question:**
> "How do we detect metrics? Do we check ServiceMonitor config or just hope the name is the same?"

**The answer was embarrassing:** We were hoping.

**You observed:**
- `node-footprint` works (915 pods found)
- `requests-skew` fails (0 workloads found)

**Root cause:** Different data sources.

---

## The Solution: Auto-Discovery

### What We Fixed

**Before (naive):**
```go
// Just hoped this existed!
query := `container_cpu_usage_seconds_total{...}`
```

**After (smart):**
```go
// 1. Query Prometheus for ALL available metrics
// 2. Try multiple common patterns
// 3. Pick the best match
// 4. Tell user exactly what we found (or didn't)
```

---

## How It Works Now

### Step 1: Discovery Phase
```
[kubenow] Discovering available Prometheus metrics...
```

Queries Prometheus `/api/v1/label/__name__/values` to get ALL metric names.

### Step 2: Pattern Matching

**CPU metrics tried (in order):**
1. `container_cpu_usage_seconds_total` (cAdvisor standard)
2. `container_cpu_usage` (alternative naming)
3. `kubelet_container_cpu_usage_seconds` (Kubelet)
4. `kube_pod_container_resource_requests` (fallback to requests, not usage)

**Memory metrics tried (in order):**
1. `container_memory_working_set_bytes` (cAdvisor standard)
2. `container_memory_usage_bytes` (alternative)
3. `kubelet_container_memory_working_set_bytes` (Kubelet)
4. `kube_pod_container_resource_requests` (fallback)

### Step 3: Validation

**If metrics found:**
```
[kubenow] Using metrics: CPU=container_cpu_usage_seconds_total, Memory=container_memory_working_set_bytes
```

**If metrics missing:**
```
⚠️  Metric Discovery Failed:
════════════════════════

no CPU usage metrics found in Prometheus (tried: container_cpu_usage_seconds_total, container_cpu_usage, etc.)

Available metrics in Prometheus:
  CPU-related: (none found)
  Memory-related: (none found)

Possible causes:
  • cAdvisor metrics not being scraped
  • ServiceMonitor/PodMonitor not configured
  • Prometheus scrape config missing container metrics

See README troubleshooting section for details.
```

---

## Why This Matters

### Your Production Reality

**You have:**
- 57 namespaces
- 915 pods
- Prometheus running
- ServiceMonitors configured

**But:** Container metrics weren't being scraped (or named differently).

**Before our fix:**
- Silent failure
- Confusing "0 workloads" output
- No diagnostic info

**After our fix:**
- Explicit discovery phase
- Clear error messages
- Lists what metrics ARE available
- Suggests fixes

---

## Diagnostic Output You'll See

### Success Case
```bash
$ ./kubenow analyze requests-skew --prometheus-url http://127.0.0.1:9090

[kubenow] Discovering available Prometheus metrics...
[kubenow] Using metrics: CPU=container_cpu_usage_seconds_total, Memory=container_memory_working_set_bytes
[kubenow] Discovering namespaces...
[kubenow] Found 57 namespaces to analyze
[kubenow] [1/57] Analyzing namespace: production
[kubenow]   → Found 12 workloads with metrics
...
```

### Failure Case (Your Current Situation)
```bash
$ ./kubenow analyze requests-skew --prometheus-url http://127.0.0.1:9090

[kubenow] Discovering available Prometheus metrics...

⚠️  Metric Discovery Failed:
════════════════════════

no CPU usage metrics found in Prometheus

Available metrics in Prometheus:
  CPU-related: (none found)
  Memory-related: (none found)

Possible causes:
  • cAdvisor metrics not being scraped
  • ServiceMonitor/PodMonitor not configured
  • Prometheus scrape config missing container metrics
```

---

## How to Fix Your Setup

### Option 1: Check ServiceMonitor (kube-prometheus-stack)

```bash
# Check if container metrics ServiceMonitor exists
kubectl get servicemonitor -n kube-prometheus-stack

# Look for: kubelet or cadvisor ServiceMonitor
kubectl get servicemonitor -n kube-prometheus-stack kubelet -o yaml
```

**Should have endpoints for cAdvisor:**
```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: kubelet
spec:
  endpoints:
  - port: https-metrics  # Kubelet metrics
  - port: cadvisor       # Container metrics (THIS ONE!)
    path: /metrics/cadvisor
```

### Option 2: Query Prometheus Directly

```bash
# Check what metrics exist
curl -s "http://127.0.0.1:9090/api/v1/label/__name__/values" | \
  jq -r '.data[]' | grep -i container | grep -i cpu

# Should see:
# container_cpu_usage_seconds_total
# container_cpu_system_seconds_total
# etc.
```

### Option 3: Check Prometheus Targets

```bash
# Open Prometheus UI
kubectl port-forward -n kube-prometheus-stack svc/prometheus-operated 9090:9090

# Go to: http://localhost:9090/targets
# Look for: kubelet targets with /metrics/cadvisor endpoint
```

---

## Future Enhancement: Hybrid Mode

**Your suggestion is excellent:**
> "Maybe we should implement a combined approach where node-footprint output can be fed to requests-skew"

**Possible architecture:**
```
┌─────────────────────────────────────────────┐
│          Unified Analyzer (Future)          │
├─────────────────────────────────────────────┤
│                                             │
│  1. Get Requests from K8s API (always works)│
│  2. Get Usage from Prometheus (if available)│
│  3. Fall back to requests-only if no metrics│
│  4. Calculate skew if both available        │
│                                             │
└─────────────────────────────────────────────┘
```

**Would allow:**
- Always show workload list (from K8s)
- Show skew IF metrics available
- Graceful degradation

---

## Implementation Details

### Files Added

**`internal/metrics/discovery.go`** (NEW)
- `MetricDiscovery` struct
- `DiscoverMetrics()` - auto-detect available metrics
- `ValidateMetrics()` - check if required metrics exist
- `GetCPUQuery()` - build query with discovered metric names
- `GetMemoryQuery()` - build query with discovered metric names
- `DiagnosticInfo()` - human-readable output

### Files Modified

**`internal/metrics/prometheus.go`**
- Added `GetAPI()` method to expose underlying Prometheus API

**`internal/cli/analyze_requests_skew.go`**
- Added metric discovery phase before analysis
- Added detailed error messages when metrics missing
- Shows what metrics were found/used

---

## Testing Your Fix

### 1. Check Current State
```bash
# What metrics exist?
curl -s "http://127.0.0.1:9090/api/v1/label/__name__/values" | \
  jq -r '.data[]' | grep container | head -20
```

### 2. Run with Discovery
```bash
# Will now show what it found (or didn't)
./kubenow analyze requests-skew --prometheus-url http://127.0.0.1:9090
```

### 3. Fix ServiceMonitor
```bash
# If cAdvisor endpoint missing, add it
kubectl edit servicemonitor kubelet -n kube-prometheus-stack

# Add:
spec:
  endpoints:
  - port: cadvisor
    path: /metrics/cadvisor
    scheme: https
    tlsConfig:
      insecureSkipVerify: true
```

---

## Why node-footprint Works But requests-skew Doesn't

### node-footprint
```
Uses: Kubernetes API → kubectl get pods
Always works: Yes (if kubectl works)
Metrics needed: Only for cluster-level stats and safety checks
Can fail gracefully: Yes
```

### requests-skew (before fix)
```
Uses: Prometheus metrics only
Always works: No (depends on scrape config)
Metrics needed: container_cpu_usage_seconds_total (mandatory)
Can fail gracefully: No (silent failure)
```

### requests-skew (after fix)
```
Uses: Prometheus metrics only
Always works: No (still depends on scrape config)
Metrics needed: Auto-discovered from available metrics
Can fail gracefully: YES (clear error with diagnostics)
```

---

## Your Observation Was Critical

**This is exactly the kind of production feedback that makes tools actually work.**

You identified:
1. Architectural assumption (hardcoded metric names)
2. Inconsistent behavior (node vs requests)
3. Better approach (discovery or hybrid)

**Thank you for the reality check!** 🙏

Now kubenow will:
- ✅ Tell you exactly what metrics it found
- ✅ Explain why it failed
- ✅ Suggest how to fix your setup
- ✅ Not silently fail with "0 workloads"
