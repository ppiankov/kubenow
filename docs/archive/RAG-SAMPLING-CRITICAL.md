# ⚡ CRITICAL: RAG Workload Sampling Requirements

## TL;DR

**For RAG workloads, you MUST use 1s or sub-1s sampling intervals. The default 5s will miss spikes entirely.**

```bash
# CORRECT - RAG workload monitoring
kubenow analyze requests-skew \
  --prometheus-url http://127.0.0.1:9090 \
  --watch-for-spikes \
  --spike-duration 30m \
  --spike-interval 1s    # ← CRITICAL!

# WRONG - Will miss most RAG spikes
kubenow analyze requests-skew \
  --prometheus-url http://127.0.0.1:9090 \
  --watch-for-spikes \
  --spike-duration 30m \
  --spike-interval 5s    # ✗ Too slow for RAG
```

---

## Why This Matters

### RAG Query Timeline

**Typical RAG query execution**:
```
Time:    0ms    100ms   200ms   300ms   400ms   500ms
         |-------|-------|-------|-------|-------|
CPU:     5%      95%     98%     92%     8%      5%
         idle    spike   spike   spike   drop    idle
                 ↑                       ↑
                 query starts            query completes
```

**Total spike duration**: 100-500ms (average ~300ms)

### Sampling Comparison

**With 5s sampling interval**:
```
Sample 1 (0s):    5% CPU   ← Before query
Sample 2 (5s):    5% CPU   ← After query
Result: Completely missed the spike! ❌
```

**With 1s sampling interval**:
```
Sample 1 (0s):    5% CPU   ← Before query
Sample 2 (1s):   95% CPU   ← During query ✓
Sample 3 (2s):    8% CPU   ← After query
Result: Captured the spike! ✅
```

**With 500ms (0.5s) sampling interval**:
```
Sample 1 (0.0s):   5% CPU   ← Before query
Sample 2 (0.5s):  95% CPU   ← Query started ✓
Sample 3 (1.0s):  98% CPU   ← Peak ✓
Sample 4 (1.5s):   8% CPU   ← Dropping ✓
Sample 5 (2.0s):   5% CPU   ← Completed
Result: Captured detailed spike profile! ✅✅
```

---

## Mathematical Analysis

### Spike Capture Probability

Given:
- RAG spike duration: **300ms** (average)
- Sampling interval: **T**

**Probability of capturing at least one sample during spike**:
```
P(capture) ≈ spike_duration / sampling_interval
```

**Results**:
- 5s sampling:  300ms / 5000ms = **6% chance**  ❌
- 1s sampling:  300ms / 1000ms = **30% chance** ⚠️
- 500ms sampling: 300ms / 500ms = **60% chance** ✅
- 100ms sampling: 300ms / 100ms = **100% (3+ samples)** ✅✅

### Sample Count During Spike

For reliable spike detection, you want **2+ samples** during the spike:

```
samples_during_spike = spike_duration / sampling_interval

5s sampling:    300ms / 5000ms = 0.06 samples  ❌ (will miss)
1s sampling:    300ms / 1000ms = 0.3 samples   ⚠️ (might catch 1)
500ms sampling: 300ms / 500ms  = 0.6 samples   ✅ (likely catch 1)
250ms sampling: 300ms / 250ms  = 1.2 samples   ✅ (catch 1-2)
100ms sampling: 300ms / 100ms  = 3 samples     ✅✅ (detailed profile)
```

---

## Recommended Sampling Rates

### By RAG Query Latency

| RAG Query P95 Latency | Recommended Interval | Rationale |
|-----------------------|----------------------|-----------|
| < 100ms (ultra-fast)  | 50-100ms            | Catch 1-2 samples per query |
| 100-300ms (fast)      | 100-250ms           | Catch 2-3 samples per query |
| 300-500ms (typical)   | 250ms-1s            | Catch 1-2 samples per query |
| 500ms-1s (slow)       | 500ms-1s            | Catch 1-2 samples per query |
| > 1s (very slow)      | 1s                  | Multiple samples guaranteed |

### Conservative Recommendation

**Use 1s for all RAG workloads** as a safe default:
- Captures most RAG spikes (≥300ms duration)
- Low overhead on Metrics API
- Balances accuracy vs system load
- Works for 90% of RAG use cases

**Use sub-1s (500ms, 250ms, 100ms) for**:
- Ultra-fast RAG queries (< 200ms)
- Critical production workloads
- When you need detailed spike profiles
- Performance optimization analysis

---

## Real-World Examples

### Example 1: Vector Search + LLM Generation

**Setup**:
- Vector search: 50ms
- LLM generation: 250ms
- Total query time: 300ms
- CPU spike: 0% → 95% → 0%

**Recommended**: `--spike-interval 500ms` or `1s`

### Example 2: Synchronous RAG API

**Setup**:
- User submits query via API
- Vector DB retrieval: 30ms
- Context building: 20ms
- LLM inference: 150ms
- Response streaming: 50ms
- Total: ~250ms

**Recommended**: `--spike-interval 250ms` or `500ms`

### Example 3: Batch RAG Processing

**Setup**:
- Process 100 documents
- Each document: 400ms RAG query
- Queries run sequentially
- Total spike window: 40 seconds

**Recommended**: `--spike-interval 1s` (spikes are sustained)

---

## Command Reference

### Standard RAG Monitoring

```bash
# Default recommendation (1s sampling, 30min window)
kubenow analyze requests-skew \
  --prometheus-url http://127.0.0.1:9090 \
  --watch-for-spikes \
  --spike-duration 30m \
  --spike-interval 1s
```

### High-Frequency RAG Monitoring

```bash
# For ultra-fast RAG queries (< 200ms)
kubenow analyze requests-skew \
  --prometheus-url http://127.0.0.1:9090 \
  --watch-for-spikes \
  --spike-duration 15m \
  --spike-interval 500ms

# For detailed spike profiling
kubenow analyze requests-skew \
  --prometheus-url http://127.0.0.1:9090 \
  --watch-for-spikes \
  --spike-duration 10m \
  --spike-interval 250ms
```

### Production RAG Fleet Monitoring

```bash
# Focus on specific namespace, extended monitoring
kubenow analyze requests-skew \
  --prometheus-url http://127.0.0.1:9090 \
  --namespace-regex "rag-.*" \
  --watch-for-spikes \
  --spike-duration 1h \
  --spike-interval 1s
```

---

## Warning Signs You're Using Wrong Interval

### Symptoms of 5s Sampling on RAG Workloads

❌ **Analysis shows avg CPU: 5%, max CPU: 8%**
   → But you know queries are CPU-intensive
   → You're missing the spikes!

❌ **Prometheus shows steady 5% CPU usage**
   → But users report slow responses
   → Spikes are between scrapes!

❌ **Node-footprint suggests huge consolidation**
   → But workload feels busy
   → You're underestimating actual load!

### What Good Capture Looks Like

✅ **Analysis shows avg CPU: 12%, max CPU: 95%**
   → Clear spike detection
   → Realistic load profile

✅ **Spike count: 150 spikes in 30 minutes**
   → Matches query rate (~5 queries/min)
   → Confirms spike detection working

✅ **Spike ratio: 8x (avg vs max)**
   → RAG queries clearly visible
   → Can size resources appropriately

---

## Impact on Resource Sizing

### Under-Provisioning Risk

**If you miss spikes due to slow sampling**:

```
Wrong analysis (5s sampling):
  Avg CPU: 5%
  Max CPU: 8%
  Recommendation: "Reduce requests from 2 cores to 0.2 cores"
  Result: ❌ THROTTLING & OOMKills when RAG queries hit

Correct analysis (1s sampling):
  Avg CPU: 15%
  Max CPU: 95%
  P99 CPU: 85%
  Recommendation: "Keep 2 cores (p99 + 20% = 1.0 cores minimum)"
  Result: ✅ Handles spikes without throttling
```

### Cost vs Safety Trade-off

**Using 5s sampling**:
- ❌ Missed spikes → under-provisioned
- ❌ Production issues
- ❌ User-facing latency
- ✅ Lower resource requests (but wrong!)

**Using 1s sampling**:
- ✅ Captured spikes → correctly provisioned
- ✅ Stable production
- ✅ Predictable performance
- ✅ Right-sized resources

---

## Technical Details

### Kubernetes Metrics API Overhead

**Per-sample cost** (approximate):
- API call: ~5-10ms
- CPU overhead: negligible
- Network: ~1KB per pod
- Metrics API load: minimal

**Sampling 100 pods**:
- 1s interval: 100 calls/sec = 100KB/s
- 500ms interval: 200 calls/sec = 200KB/s
- Very low overhead on cluster

**Recommendation**: Don't worry about overhead; prioritize accurate data.

### Why Not Just Use Smaller Intervals Always?

**Trade-offs**:
- **100ms sampling**: 10x more data, 10x more API calls, overkill for most workloads
- **500ms sampling**: Good balance for fast RAG
- **1s sampling**: Sweet spot for typical RAG (300-500ms queries)
- **5s sampling**: Only suitable for steady-state, non-bursty workloads

**Guidance**: Start with 1s, drop to 500ms if you see gaps in spike detection.

---

## FAQ

### Q: Can I use Prometheus instead of latch mode?

**A**: No, Prometheus scrapes every 15-30s (default). RAG spikes are 100-500ms. Prometheus will miss 95%+ of spikes.

### Q: What if my RAG queries are batched?

**A**: Batched queries create sustained load (multiple spikes close together). Use 1s sampling; you'll catch the overall spike pattern.

### Q: My RAG service handles 1 query per minute. Still need 1s sampling?

**A**: Yes! Each query is still a 300ms spike. 5s sampling has only 6% chance of catching it. Use 1s to ensure you capture the spike.

### Q: Can I use latch mode for ALL workloads?

**A**: Latch mode is targeted, not cluster-wide. Use it specifically for:
- RAG workloads (≤1s sampling)
- Workloads without Prometheus metrics
- Suspected spiky workloads you want to investigate

For steady-state workloads, Prometheus historical data is sufficient.

### Q: How long should I monitor RAG workloads?

**A**: Depends on query rate:
- High traffic (>100 queries/hour): 15-30 minutes is sufficient
- Medium traffic (10-100 queries/hour): 30-60 minutes
- Low traffic (<10 queries/hour): 1-2 hours to capture enough samples

---

## Checklist: RAG Workload Monitoring

Before monitoring a RAG workload:

- [ ] Confirmed workload is RAG (vector search + LLM generation)
- [ ] Checked typical query latency (should be 100-500ms)
- [ ] Chosen sampling interval: ≤1s for RAG
- [ ] Set monitoring duration: 15-60 minutes depending on traffic
- [ ] Prepared to analyze spike patterns (max/avg ratios)
- [ ] Will use results to set resource requests with spike headroom

Sample command:
```bash
kubenow analyze requests-skew \
  --prometheus-url http://127.0.0.1:9090 \
  --namespace-regex "rag-.*" \
  --watch-for-spikes \
  --spike-duration 30m \
  --spike-interval 1s \
  --output json \
  --export-file rag-spikes.json
```

---

## Summary

| Workload Type | Query Latency | Sampling Interval | Why |
|--------------|---------------|-------------------|-----|
| **RAG (typical)** | 300-500ms | **1s** | Catches 1-2 samples per query |
| **RAG (fast)** | 100-300ms | **500ms** | Catches 1-2 samples per query |
| **RAG (ultra-fast)** | < 100ms | **250ms or less** | Catches multiple samples |
| **Batch/steady** | > 5s | 5s default | Sustained load, not spiky |
| **Non-bursty** | N/A | Prometheus is fine | Use historical metrics |

**Golden Rule**: When in doubt, use 1s sampling for RAG. It's the safe default that works for 90% of RAG workloads.

---

## Related Documentation

- `WORKLOADS-WITHOUT-METRICS.md` - When to use latch mode
- `METRIC-DISCOVERY.md` - How metric discovery works
- `README.md` - General usage and troubleshooting
