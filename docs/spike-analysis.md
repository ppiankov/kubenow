# Spike Analysis: Interpreting CPU Burst Data

## TL;DR

When you see a workload with a **21.0x spike ratio**, it means the container's **peak CPU usage is 21 times higher than its average**. This indicates bursty workload behavior that requires proper resource request sizing.

**Formula**: `CPU Request = Max Observed CPU × Safety Factor`

**Example**:
- Max CPU: 0.5 cores
- Safety Factor: 2.0x (for typical API)
- **Recommended CPU Request**: 1.0 cores

---

## What Are Spikes?

### The Problem

Kubernetes resource requests define the **guaranteed** CPU a container gets. If you set requests based on average usage, your workload will be CPU-throttled during bursts.

**Bad Sizing (average-based)**:
```yaml
resources:
  requests:
    cpu: 100m  # Based on average usage
# Result: Container throttled to 100m during bursts
# Actual need during spike: 2100m
# User experience: Slow responses, timeouts
```

**Good Sizing (spike-aware)**:
```yaml
resources:
  requests:
    cpu: 2100m  # Based on max observed + safety margin
# Result: Container has CPU available for bursts
# User experience: Fast, consistent responses
```

### What Latch Mode Detects

The `--watch-for-spikes` flag monitors containers at high frequency (1-5 seconds) to catch short bursts that standard Prometheus scraping (15-30 seconds) misses.

**Spike Ratio** = Max CPU / Average CPU

- **1.0x-2.0x**: Steady workload (minimal bursts)
- **2.0x-5.0x**: Moderate bursts (typical APIs)
- **5.0x-20.0x**: High bursts (RAG, ML inference, batch jobs)
- **20.0x+**: Extreme bursts (millisecond-level spikes)

---

## Safety Factors: How Much Headroom?

Safety factors account for:
1. Measurement uncertainty (sampling between scrapes)
2. Future growth (traffic increases)
3. Noisy neighbors (other pods on same node)

### Recommended Safety Factors

| Workload Type | Spike Pattern | Safety Factor | Rationale |
|--------------|---------------|---------------|-----------|
| **RAG / AI Inference** | Extreme (20x+) | **2.5x-3.0x** | Millisecond bursts, user-facing latency |
| **APIs (REST/GraphQL)** | Moderate (2x-5x) | **1.5x-2.0x** | Request handling spikes |
| **Background Workers** | Low (1x-2x) | **1.2x-1.5x** | Not latency-sensitive |
| **Batch Jobs** | High (5x-20x) | **1.5x-2.0x** | Can tolerate some throttling |
| **Databases** | Low (1x-3x) | **1.3x-1.5x** | Steady load with query spikes |

**Conservative Approach**: When unsure, use 2.0x. Over-provisioning by 100% is safer than under-provisioning.

---

## Step-by-Step: Sizing from Spike Data

### Example Output

```
WORKLOAD          MAX CPU  AVG CPU  SPIKE RATIO
exchange-rates    0.412    0.020    21.0x
config-service    0.623    0.020    31.2x
illumination      0.752    0.040    18.8x
```

### Step 1: Identify Max CPU

From the table: `exchange-rates` has **Max CPU = 0.412 cores**

### Step 2: Choose Safety Factor

Workload type: REST API serving exchange rate data
- User-facing: Yes (low latency required)
- Spike ratio: 21.0x (extreme bursts)
- Recommendation: **Safety Factor = 2.5x**

### Step 3: Calculate CPU Request

```
CPU Request = Max CPU × Safety Factor
            = 0.412 × 2.5
            = 1.03 cores
            ≈ 1000m (round to nearest 100m)
```

### Step 4: Apply to Cluster

**Option A: kubectl patch (live update)**
```bash
kubectl patch deployment exchange-rates -n production --type=json -p='[
  {
    "op": "replace",
    "path": "/spec/template/spec/containers/0/resources/requests/cpu",
    "value": "1000m"
  }
]'
```

**Option B: Edit YAML manifest**
```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: exchange-rates
spec:
  template:
    spec:
      containers:
      - name: api
        resources:
          requests:
            cpu: 1000m  # Was: 100m (based on average)
          limits:
            cpu: 2000m  # Optional: 2x request for burst capacity
```

Apply:
```bash
kubectl apply -f deployment.yaml
```

### Step 5: Verify

Run latch mode again after 24 hours:
```bash
./bin/kubenow analyze requests-skew \
  --prometheus-url http://localhost:9090 \
  --watch-for-spikes \
  --spike-duration 5m \
  --namespace production
```

Look for:
- **Spike ratio reduced**: Should be closer to 2.0x-3.0x (since requests now match needs)
- **No throttling**: Check Prometheus metric `container_cpu_cfs_throttled_seconds_total`

---

## Memory Spikes

Memory works differently than CPU:
- **CPU spikes**: Can be throttled (slow down)
- **Memory spikes**: Trigger OOMKills (crash)

If you see memory spikes:
1. Check for memory leaks (increasing trend over time)
2. Set requests to **p99 memory + 20%** (no safety factor needed)
3. Set limits to **2x requests** (allow bursts without OOM)

Example:
```yaml
resources:
  requests:
    memory: 1200Mi  # p99 observed: 1000Mi, +20% = 1200Mi
  limits:
    memory: 2400Mi  # 2x request
```

---

## Historical Validation Philosophy

kubenow follows **evidence-based sizing**:

> "This configuration **would have worked** for the last 30 days based on observed data."

We **never** say:
- "You should do this" (prescriptive)
- "This will work in the future" (predictive)
- "This is the optimal configuration" (subjective)

We **always** say:
- "Based on the last 30 days, this sizing would have accommodated all observed spikes."
- "Your current requests are 5x lower than peak usage."
- "If past behavior repeats, this configuration provides X% headroom."

---

## Common Patterns

### Pattern 1: RAG / LLM Inference

**Symptoms**:
- Spike ratio: 20x-50x
- Duration: Milliseconds to seconds
- Occurs during user queries

**Cause**: Vector search + LLM inference = CPU burst

**Solution**:
```yaml
resources:
  requests:
    cpu: 3000m  # Max observed: 1.2 cores × 2.5 safety factor
  limits:
    cpu: 4000m  # Allow some burst above request
```

**Sampling**: Use `--spike-interval 1s` to catch sub-second spikes

### Pattern 2: API with Caching

**Symptoms**:
- Spike ratio: 5x-10x
- Duration: Seconds
- Occurs on cache misses

**Cause**: Cache miss triggers database query + response serialization

**Solution**:
```yaml
resources:
  requests:
    cpu: 800m  # Max observed: 0.4 cores × 2.0 safety factor
```

### Pattern 3: Background Worker

**Symptoms**:
- Spike ratio: 2x-4x
- Duration: Minutes
- Occurs during job processing

**Cause**: Periodic task execution

**Solution**:
```yaml
resources:
  requests:
    cpu: 600m  # Max observed: 0.5 cores × 1.2 safety factor
```

**Note**: Lower safety factor acceptable since not user-facing

---

## Troubleshooting

### "Spike ratio is 1.0x, but users report slowness"

**Possible causes**:
1. Sampling interval too long (increase frequency with `--spike-interval 1s`)
2. Workload has sub-scrape-interval bursts (use latch mode)
3. Slowness not CPU-related (check memory, disk I/O, network)

**Debug**:
```bash
# Check if CPU throttling is occurring
kubectl exec -n production exchange-rates-abc123 -- \
  cat /sys/fs/cgroup/cpu/cpu.stat | grep throttled_time
```

### "I set CPU requests to max observed, still seeing throttling"

**Possible causes**:
1. No safety factor applied (workload grew since measurement)
2. Node overcommit (total requests exceed node capacity)
3. CPU limits set too low (check limits vs requests)

**Fix**:
```bash
# Increase safety factor
CPU Request = Max × 2.0  # Instead of 1.0

# Or remove CPU limits entirely (let it burst)
kubectl patch deployment exchange-rates --type=json -p='[
  {"op": "remove", "path": "/spec/template/spec/containers/0/resources/limits/cpu"}
]'
```

### "Workload shows 'WITHOUT metrics' in latch mode"

**Cause**: Prometheus not scraping this workload, or ServiceMonitor missing

**Fix**:
1. Verify Prometheus can reach pod:
   ```bash
   kubectl get servicemonitor -n production
   ```
2. Check Prometheus targets:
   ```
   # Port-forward to Prometheus
   kubectl port-forward -n monitoring svc/prometheus 9090:9090
   # Open http://localhost:9090/targets
   ```
3. If missing, create ServiceMonitor or add pod annotations

---

## Best Practices

### 1. Measure First, Size Later

Don't guess resource requests. Run latch mode for at least 24 hours (7 days ideal) to capture:
- Daily traffic patterns (peak hours)
- Weekly patterns (Monday spikes)
- Anomalies (traffic surges)

```bash
# Run for 7 days, check daily
./bin/kubenow analyze requests-skew \
  --prometheus-url http://localhost:9090 \
  --watch-for-spikes \
  --spike-duration 24h
```

### 2. Start Conservative, Iterate

Initial sizing:
```yaml
requests:
  cpu: Max × 2.5  # Conservative
```

After observing no throttling for 7 days:
```yaml
requests:
  cpu: Max × 2.0  # Slightly reduce
```

**Never** reduce below `Max × 1.5`

### 3. Separate User-Facing from Background

User-facing services: Higher safety factors (2.0x-3.0x)
```yaml
# API pods
resources:
  requests:
    cpu: 2000m
```

Background workers: Lower safety factors (1.2x-1.5x)
```yaml
# Worker pods
resources:
  requests:
    cpu: 600m
```

### 4. Document Your Decisions

Add annotations to deployments:
```yaml
metadata:
  annotations:
    kubenow.dev/sizing-date: "2026-01-30"
    kubenow.dev/max-observed-cpu: "0.412"
    kubenow.dev/safety-factor: "2.5"
    kubenow.dev/spike-ratio: "21.0x"
```

---

## Using `--show-recommendations` Flag

kubenow can calculate recommendations for you:

```bash
./bin/kubenow analyze requests-skew \
  --prometheus-url http://localhost:9090 \
  --watch-for-spikes \
  --show-recommendations
```

**Output**:
```
WORKLOAD          MAX CPU  AVG CPU  SPIKE RATIO  RECOMMENDED CPU
exchange-rates    0.412    0.020    21.0x        1.0 cores (2.5x)
config-service    0.623    0.020    31.2x        1.6 cores (2.5x)
illumination      0.752    0.040    18.8x        1.5 cores (2.0x)
```

**Default safety factors**:
- Spike ratio > 20x: 2.5x
- Spike ratio 10x-20x: 2.0x
- Spike ratio 5x-10x: 1.5x
- Spike ratio < 5x: 1.2x

You can override:
```bash
--safety-factor 3.0  # Use 3.0x for all workloads
```

---

## Summary

1. **Spike ratio** shows how bursty your workload is (max/avg CPU)
2. **Safety factor** adds headroom for uncertainty and growth (1.5x-3.0x)
3. **CPU Request** = Max Observed CPU × Safety Factor
4. **Validation**: Run latch mode for 24h-7d before applying changes
5. **Philosophy**: "Would have worked historically" (evidence-based, not predictive)
6. **User-facing workloads**: Use higher safety factors (2.0x-3.0x)
7. **Background workloads**: Use lower safety factors (1.2x-1.5x)
8. **Iterate**: Start conservative, reduce gradually over time

**Golden rule**: It's better to over-provision by 100% than under-provision by 10%.
