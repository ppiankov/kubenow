# Monitor Mode: Real-Time Problem Detection

## TL;DR

**`kubenow monitor`** is like `top` for Kubernetes problems - a live terminal UI that shows ONLY what's broken RIGHT NOW.

```bash
# Start monitoring (all namespaces)
kubenow monitor

# Monitor specific namespace
kubenow monitor --namespace production

# Only show critical problems
kubenow monitor --severity critical
```

**When healthy**: Black screen with pulsing heartbeat
**When broken**: Problems appear immediately with details

---

## The Problem This Solves

### Current Reality (Dashboards)

```
╔═══════════════════════════════════════════════════════╗
║  [Grafana Dashboard - 47 panels]                      ║
╠═══════════════════════════════════════════════════════╣
║                                                       ║
║  CPU Usage: ████████░░ 82%   Memory: ███████░░ 73%  ║
║  Network: ███░░░░░░░ 28%      Disk: ████████░░ 84%   ║
║  Pod Count: 915               Node Count: 12          ║
║  Request Rate: 2.4k/s         Error Rate: 0.02%      ║
║  ... 40 more metrics ...                              ║
║                                                       ║
║  [Somewhere in here, ONE pod is OOMKilling]          ║
║  [You have to scan through green graphs to find it]  ║
║                                                       ║
╚═══════════════════════════════════════════════════════╝
```

**Problem**: Dashboards show EVERYTHING. You scan green metrics looking for red ones.

### kubenow monitor

```
╔═══════════════════════════════════════════════════════╗
║ kubenow monitor                   [Ctrl+C to exit]    ║
╠═══════════════════════════════════════════════════════╣
║                                                       ║
║  ✓ No active problems ⬤                               ║
║                                                       ║
║  Monitoring: 57 namespaces, 915 pods, 12 nodes       ║
║  Last event: 3m ago (Normal: Pulled image)            ║
║                                                       ║
╚═══════════════════════════════════════════════════════╝
```

**Solution**: Screen is empty when healthy. Problems appear instantly when they occur.

---

## What It Monitors

### Priority 1: FATAL (Red ❌)

Events that cause immediate service disruption:

- **OOMKilled**: Container killed due to out of memory
- **CrashLoopBackOff**: Container crashing repeatedly
- **Error**: Container failed to start
- **Failed**: Job or pod failed

### Priority 2: CRITICAL (Orange ⚠️)

Events that will cause service disruption soon:

- **ImagePullBackOff**: Cannot pull container image
- **ErrImagePull**: Image doesn't exist
- **NodeNotReady**: Node is down or unreachable
- **Evicted**: Pod evicted due to resource pressure
- **FailedScheduling**: Cannot place pod on any node

### Priority 3: WARNING (Yellow ⚠️)

Events indicating potential issues:

- **BackOff**: Waiting to restart after failure
- **Unhealthy**: Liveness or readiness probe failed
- **Probe Failed**: Health check not responding

---

## When Problems Detected

```
╔═══════════════════════════════════════════════════════════════╗
║ kubenow monitor                          [Ctrl+C to exit]     ║
╠═══════════════════════════════════════════════════════════════╣
║                                                               ║
║  🔴 ACTIVE PROBLEMS (3)                                       ║
║  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  ║
║                                                               ║
║  ❌ OOMKilled    talala/payment-worker-7d8f9    NOW          ║
║     └─ Container: worker                                      ║
║     └─ Container killed due to out of memory                  ║
║     └─ Count: 23 occurrences                                  ║
║                                                               ║
║  ⚠️  CrashLoop    prod/checkout-api-abc123       2s ago      ║
║     └─ Container: api                                         ║
║     └─ Container crashing repeatedly (restarts: 15)           ║
║                                                               ║
║  ⚠️  BackOff      staging/redis-master-0         5s ago      ║
║     └─ Waiting to restart after failure                       ║
║                                                               ║
║  📊 RECENT EVENTS (last 5m)                                   ║
║  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  ║
║                                                               ║
║  [15:42] Evicted: prod/batch-job-xyz (DiskPressure)          ║
║  [15:40] Failed: anti-fraud/worker-3 (Liveness probe)        ║
║  [15:38] OOMKilled: talala/media-converter-9k2l              ║
║                                                               ║
║  📈 CLUSTER STATUS                                            ║
║  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━  ║
║                                                               ║
║  Pods:     915 total  |  892 running  |  23 problem          ║
║  Nodes:    12 total   |  12 ready     |  0 NotReady          ║
║  Critical: 3 active problems                                  ║
║                                                               ║
╚═══════════════════════════════════════════════════════════════╝
```

---

## Heartbeat Indicator

The heartbeat shows the monitor is actively running:

```
✓ No active problems ⬤   ← Pulse 1
✓ No active problems ⚫   ← Pulse 2 (1 second later)
✓ No active problems ⬤   ← Pulse 3
```

**Why this matters**: Without a heartbeat, a frozen screen looks identical to a healthy screen. The pulse proves the monitor is alive and scanning.

---

## Usage Examples

### Basic Monitoring

```bash
# Monitor everything (all namespaces)
kubenow monitor

# Press Ctrl+C to exit
```

### Namespace Filtering

```bash
# Monitor only production namespace
kubenow monitor --namespace production

# Monitor multiple namespaces (use regex in future version)
kubenow monitor --namespace "prod.*"
```

### Severity Filtering

```bash
# Only show FATAL problems (OOMKills, crashes)
kubenow monitor --severity fatal

# Show CRITICAL and above (includes ImagePullBackOff)
kubenow monitor --severity critical

# Show all problems including warnings
kubenow monitor --severity warning
```

### Quiet Mode

```bash
# Hide stats, only show problems
kubenow monitor --quiet
```

---

## How It Works (Technical)

### Architecture

```
┌─────────────────────────────────────────────────┐
│  kubenow monitor                                │
├─────────────────────────────────────────────────┤
│                                                 │
│  ┌──────────────┐         ┌─────────────────┐  │
│  │ Event Watcher│────────▶│  Problem Store  │  │
│  └──────────────┘         └─────────────────┘  │
│         │                          │            │
│         │                          ▼            │
│         │                  ┌─────────────────┐  │
│  ┌──────────────┐          │   Terminal UI   │  │
│  │  Pod Watcher │────────▶│  (bubbletea)    │  │
│  └──────────────┘          └─────────────────┘  │
│         │                          │            │
│         │                          ▼            │
│  ┌──────────────┐          ┌─────────────────┐  │
│  │ Stats Updater│────────▶│   Your Screen   │  │
│  └──────────────┘          └─────────────────┘  │
│                                                 │
└─────────────────────────────────────────────────┘
```

### Components

1. **Event Watcher**
   - Watches Kubernetes events API (`kubectl get events --watch`)
   - Filters for problem events (OOMKilled, BackOff, Failed, etc.)
   - Classifies by severity (FATAL/CRITICAL/WARNING)

2. **Pod Watcher**
   - Watches pod status changes
   - Detects CrashLoopBackOff, OOMKilled from pod status
   - Tracks restart counts

3. **Stats Updater**
   - Polls cluster stats every 5 seconds
   - Counts total/running/problem pods
   - Counts ready/notready nodes

4. **Terminal UI**
   - Built with `bubbletea` (modern Go TUI library)
   - Updates in real-time as problems appear
   - Heartbeat pulses every 1 second
   - Keyboard handling (Ctrl+C to exit)

### Data Flow

```
1. K8s Event: OOMKilled
   └─▶ Event Watcher detects it
      └─▶ Classifies as FATAL
         └─▶ Adds to Problem Store
            └─▶ Triggers UI update
               └─▶ Problem appears on screen (< 100ms)

2. Heartbeat Timer (every 1s)
   └─▶ Toggle heartbeat indicator
      └─▶ UI updates
         └─▶ Pulsing dot changes (⬤ ⚫ ⬤)
```

---

## When to Use This

**High-value scenarios:**
- **Cluster with 20+ active problems**: Need to prioritize which to fix first
- **Rapid troubleshooting**: "What's broken RIGHT NOW?"
- **Pattern recognition**: "Which pods restart most? Which errors are recent?"
- **Post-deploy monitoring**: "Did my deploy break anything?"
- **Need to copy pod names**: Press `c` to dump everything to terminal

**Questions this tool answers instantly:**
- ✅ "Is anything broken?" → Empty screen = no, scrolling list = yes
- ✅ "Which problems should I fix first?" → Press `3` to sort by restart count
- ✅ "What broke in the last 5 minutes?" → Press `2` to sort by recency
- ✅ "Let me copy these pod names to investigate" → Press `c` to print all

**Lower value scenarios:**
- Healthy clusters (empty screen is correct, but not exciting)
- Deep investigation of single pod (you'll still need `kubectl describe`, logs)
- Historical analysis (this is real-time only)
- Cluster management tasks (use k9s or Lens)

---

## Comparison with Other Tools

### vs. `kubectl get events --watch`

**kubectl**:
```
0s  Normal   Pulled  pod/web-api-xyz   Successfully pulled image
0s  Warning  BackOff pod/redis-master  Back-off restarting
0s  Normal   Created pod/worker-abc    Created container
0s  Warning  Failed  pod/job-123       Error: ImagePullBackOff
... (keeps scrolling, no grouping, no severity)
```

**kubenow monitor**:
- Groups related events (same pod crashing = 1 problem)
- Shows severity (FATAL vs WARNING)
- De-duplicates (shows count instead of repeating)
- Ages out resolved problems
- Heartbeat when healthy

### vs. k9s

**k9s**: General-purpose cluster explorer
- Navigate through resources (pods, services, deployments, etc.)
- Drill down with keyboard shortcuts
- Good for exploration and management

**kubenow monitor**: Problem-focused monitoring
- Zero navigation (problems auto-appear)
- Shows ONLY broken things
- Good for "is my cluster on fire?"

**Philosophy difference**:
- k9s: "Here's your entire cluster, explore it"
- kubenow monitor: "Here's what's broken. Nothing? Good."

### vs. Grafana/Prometheus Alerts

**Grafana Alerts**: Notification-based
- Sends alerts to Slack, email, PagerDuty
- Requires threshold configuration
- Can be noisy (alert fatigue)

**kubenow monitor**: Observation-based
- You watch it actively (like `top`)
- Shows problems immediately (no thresholds)
- No notifications (attention-first)

**Use together**:
- Grafana: Automated alerting (wake you up at 3am)
- kubenow monitor: Active troubleshooting (what's broken right now?)

---

## Quick Reference: Which Tool When?

| Question | Use This |
|----------|----------|
| "Is anything broken RIGHT NOW?" | **kubenow monitor** |
| "What should I fix first?" | **kubenow monitor** (sort by count/recency) |
| "Let me explore this namespace" | **k9s** |
| "What happened to this specific pod?" | **kubectl describe/logs** |
| "Show me resource usage trends" | **Grafana/Prometheus** |
| "I need to manage/edit resources" | **Lens** or **k9s** |
| "Copy 20 pod names to investigate" | **kubenow monitor** (press `c`) |

**Philosophy difference:**
- **k9s/Lens**: "Here's your entire cluster, navigate it"
- **Grafana**: "Here are all your metrics, find the problems"
- **kubenow monitor**: "Here's what's broken. Nothing? Good."

---

## Real-World Use Cases

### Use Case 1: Deployment Troubleshooting

**Scenario**: You just deployed a new version and want to watch for issues.

```bash
# Terminal 1: Deploy
kubectl apply -f deployment.yaml

# Terminal 2: Monitor
kubenow monitor --namespace production

# Watch for:
# - ImagePullBackOff (wrong image tag?)
# - CrashLoopBackOff (startup failure?)
# - OOMKilled (memory limits too low?)
```

### Use Case 2: Cluster Health Check

**Scenario**: Morning standup - "Is our cluster healthy?"

```bash
# Quick check
kubenow monitor

# If screen is empty: "All green!"
# If problems appear: "We have 3 OOMKills in production"
```

### Use Case 3: Post-Incident Monitoring

**Scenario**: You fixed an incident, want to ensure it's resolved.

```bash
# Watch for recurring issues
kubenow monitor --namespace affected-namespace

# Leave running for 15 minutes
# If problem reappears: Not fully fixed
# If stays healthy: Confirmed fixed
```

### Use Case 4: Resource Pressure Detection

**Scenario**: Cluster is slow, suspect resource issues.

```bash
# Monitor for evictions and pressure events
kubenow monitor

# Look for:
# - Evicted: DiskPressure
# - Evicted: MemoryPressure
# - FailedScheduling: Insufficient CPU
```

---

## Keyboard Shortcuts

| Key              | Action                                      |
|------------------|---------------------------------------------|
| `c` / `v`        | **Print to terminal (COPYABLE)** ← Use this! |
| `Space` / `p`    | Pause/resume updates                        |
| `1`              | Sort by severity (default)                  |
| `2`              | Sort by recency (most recent first)         |
| `3`              | Sort by count (most restarts first)         |
| `↑` / `k`        | Scroll up                                   |
| `↓` / `j`        | Scroll down                                 |
| `PageUp`         | Scroll up one page                          |
| `PageDown`       | Scroll down one page                        |
| `Home` / `g`     | Jump to top                                 |
| `End` / `G`      | Jump to bottom                              |
| `e`              | Export problems to file (then exit)         |
| `q`              | Exit monitor                                |
| `Ctrl+C`         | Exit monitor                                |
| `Esc`            | Exit monitor                                |

**How to copy pod names and commands (EASIEST WAY):**

1. Press **`c`** (or `v`) while monitor is running
2. Monitor exits to terminal and prints ALL problems in plain text:
   ```
   [1] FATAL - OOMKilled
       Namespace: production
       Pod: payment-api-7d8f9
       Container: worker

       Quick commands:
         kubectl -n production describe pod payment-api-7d8f9
         kubectl -n production logs payment-api-7d8f9 -c worker

   [2] CRITICAL - ImagePullBackOff
       Namespace: staging
       Pod: data-processor-abc123
       ...
   ```
3. **Scroll up in your terminal** to see all problems
4. **Copy** pod names, kubectl commands, or any text you need
5. Press **Enter** to return to live monitor

**Why this works:**
- Exits alternate screen buffer temporarily
- Text appears in your normal terminal (supports selection/copy)
- All 54+ problems printed at once
- Can scroll back through terminal history
- Press Enter to return to live monitoring

**Alternative - Export to file:**
- Press `e` to save everything to `kubenow-problems-TIMESTAMP.txt`
- Good for sharing with teammates or keeping records

**Compact Layout:**
- Shows 10-15 problems at once (uses full screen efficiently)
- Each problem is one line: `❌ OOMKilled  production/payment-api-7d8f9 [worker]  5m ago (×3)`
- No flicker - updates only when problems change
- Scroll with `↑`/`↓` to see more

**Sorting:**
- Press `1` - Sort by severity (FATAL → CRITICAL → WARNING)
- Press `2` - Sort by recency (most recent problems first)
- Press `3` - Sort by restart count (most reboots first)
- Current sort mode shown in header: `Sort: Recency (1/2/3)`

**Example Screen:**
```
kubenow monitor [Live] | Sort: Count (1/2/3) | C=Copy Space=Pause ↑↓=Scroll Q=Quit
🔴 54 PROBLEMS (showing 1-15)
❌ OOMKilled             production/payment-api-7d8f9 [worker]  5m ago (×23)
❌ CrashLoopBackOff      staging/checkout-api-abc123           2m ago (×15)
⚠️  ImagePullBackOff     prod/batch-processor-xyz              1m ago (×8)
⚠️  BackOff              staging/redis-master-0                30s ago (×5)
...
↓ 39 more below

📊 Recent Events: 15:42 Pod evicted | 15:40 Probe failed | 15:38 OOM
📈 Cluster: 915 pods (892 running, 23 problem) | 12 nodes (12 ready)
```

---

## Configuration Options

### Namespace Filtering

```bash
# Monitor specific namespace
kubenow monitor --namespace production

# Monitor all namespaces (default)
kubenow monitor
```

### Severity Filtering

```bash
# Only FATAL (OOMKills, crashes)
kubenow monitor --severity fatal

# CRITICAL and above (includes ImagePullBackOff)
kubenow monitor --severity critical

# All problems including warnings (default)
kubenow monitor --severity warning
```

### Display Options

```bash
# Quiet mode (hide stats)
kubenow monitor --quiet

# Normal mode (show stats) - default
kubenow monitor
```

---

## Philosophy: Attention-First Design

### Principle 1: Disappear When Working

**Bad (traditional dashboards)**:
```
Always showing 50 metrics, 20 graphs, 10 alerts
User scans everything looking for problems
Constant cognitive load
```

**Good (kubenow monitor)**:
```
Empty screen when healthy
Problems appear only when they happen
Zero cognitive load when things work
```

### Principle 2: Minimize Decisions

**Bad**:
```
"Which dashboard should I check?"
"Which metric is relevant?"
"Is this value normal?"
```

**Good**:
```
"Is the screen empty?" → Yes = healthy, No = broken
No decisions needed
```

### Principle 3: Real-Time, Not Batched

**Bad**:
```
Dashboard refreshes every 15 seconds
Alert fires 1 minute after problem
You find out 2 minutes late
```

**Good**:
```
Problem appears < 1 second after it happens
You see it immediately
No delay
```

---

## Roadmap

### Current (v0.1.1+)
- ✅ Real-time event monitoring
- ✅ Pod status monitoring
- ✅ Heartbeat indicator
- ✅ Severity classification
- ✅ Namespace filtering
- ✅ Terminal UI with bubbletea

### Planned (v0.2.0)
- [ ] Keyboard shortcuts for actions:
  - Press `L` on problem → Run LLM analysis
  - Press `D` on problem → kubectl describe
  - Press `S` on problem → Show metrics
- [ ] Sound alerts (terminal bell on FATAL)
- [ ] Problem grouping (multiple related events → 1 problem)
- [ ] Auto-resolve (remove problems that resolve themselves)

### Future (v0.3.0+)
- [ ] Export mode (log to file, no TUI)
- [ ] Network issue detection (DNS failures, timeouts)
- [ ] Historical playback (replay events from past hour)
- [ ] Multi-cluster monitoring (switch between contexts)

---

## FAQ

### Q: Does this replace Grafana/Prometheus?

**A**: No. They serve different purposes:
- **Grafana**: Long-term metrics, historical analysis, automated alerts
- **kubenow monitor**: Real-time problem observation, active troubleshooting

Use both:
- Grafana for metrics and automated alerting
- kubenow monitor for "what's broken right now?"

### Q: Does this work with any Kubernetes cluster?

**A**: Yes. It uses standard Kubernetes APIs:
- `k8s.io/api/core/v1` for events and pods
- No custom CRDs required
- Works with any K8s 1.19+

### Q: How much overhead does it add?

**A**: Minimal:
- Uses Kubernetes watch APIs (efficient streaming)
- No polling (events pushed to us)
- Stats update every 5 seconds (light query)
- Typical resource usage: <10MB RAM, <0.1% CPU

### Q: Can I run this in production?

**A**: Yes. It's read-only and safe:
- No writes to cluster
- No modifications
- Only reads events and pod status
- Same permissions as `kubectl get events --watch`

### Q: How is this different from `watch kubectl get pods`?

**A**:
- **kubectl watch**: Shows ALL pods, requires scanning for problems
- **kubenow monitor**: Shows ONLY problems, zero scanning needed

### Q: What if my cluster is healthy for hours?

**A**: The screen stays mostly empty with a pulsing heartbeat. This is intentional (attention-first design). You can minimize the terminal and the pulsing will catch your eye if problems appear.

---

## Summary

**kubenow monitor** embodies the manifesto:

> "The primary responsibility of software is to disappear once it works correctly."

- Empty screen when healthy → Software disappears
- Heartbeat proves it's running → Minimal attention needed
- Problems auto-appear → No decisions required
- Real-time updates → No constant checking

**When everything works: you see nothing.**
**When something breaks: you see exactly what and where.**

That's attention-first software.
