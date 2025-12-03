![kubenow logo](https://raw.githubusercontent.com/ppiankov/kubenow/main/docs/img/logo.png)

# 🧯 kubenow — Kubernetes Incident Triage on Demand

“11434 is enough.”

## kubenow is a single Go binary that takes a live Kubernetes cluster snapshot and feeds it into an LLM (local or cloud) to generate:
	•	🔥 incident triage (ranked, actionable, command-ready)
	•	🛠 pod-level debugging
	•	📊 cluster health summaries
	•	👩‍💼 teamlead / ownership recommendations
	•	🧹 compliance / hygiene reviews
	•	🧨 chaos engineering experiment suggestions

## It works with any OpenAI-compatible API, including:
	•	🦙 Ollama (Mixtral, Llama, Qwen, etc.)
	•	☁️ OpenAI / Azure OpenAI
	•	🔧 DeepSeek / Groq / Together / OpenRouter
	•	or your own weird homemade inference server

If your laptop can run it and respond to /v1/chat/completions,
kubenow will talk to it.

# ✨ Why kubenow?

## Because when the cluster is on fire, nobody wants to run:
	•	12 commands
	•	across 5 namespaces
	•	using 4 terminals
	•	while Slack is screaming

You want:
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
kubectl -n prod edit deploy/payments-api
```

Short, ranked, actionable.

And yes — kubenow can also run teamlead mode, which gently hints at which team probably caused the outage.

# 🧩 Features

## 🎯 Smart Filtering & Targeting
	•	Include/Exclude pods by name pattern (wildcards supported)
	•	Include/Exclude namespaces by pattern
	•	Keyword-based log/event filtering
	•	Problem hints to guide LLM analysis
	•	Concurrent log fetching for faster snapshots
	•	Fine-grained control over what gets analyzed

## ⚡ Optional Power-User Enhancements
	•	**--enhance-technical**: Stack traces, memory dumps, config diffs, deeper analysis
	•	**--enhance-priority**: Numerical priority scores, SLO impact, blast radius estimates
	•	**--enhance-remediation**: Step-by-step fixes, rollback procedures, prevention tips
	•	Mix and match enhancements as needed
	•	Simple by default, powerful when you need it

## 👁️ Watch Mode (NEW)
	•	**Continuous monitoring** with configurable intervals
	•	**Diff detection**: Highlights new, resolved, and ongoing issues
	•	Alert-only mode: Show only new/changed issues
	•	Graceful shutdown with Ctrl+C
	•	Perfect for active incident response and continuous compliance

## 💾 Export Reports (NEW)
	•	**Save reports to files** for sharing with teams
	•	**Auto-format detection**: JSON, Markdown, HTML, Plain Text
	•	**Metadata included**: Timestamp, version, cluster name, filters
	•	Great for post-mortems, audit trails, and team collaboration

## 🔥 Incident Mode (--mode incident)
	•	Ranks the top problems in the cluster
	•	Gives 1–2 sentence root causes
	•	Provides actionable kubectl / YAML patches
	•	Zero fluff, zero theory

## 🧪 Pod Mode (--mode pod)

Deep dive into broken pods:
	•	container states
	•	events
	•	restarts
	•	image pulls
	•	OOMs
	•	last logs

## 📊 Default Mode

High-level cluster summary with readable health insights.

## 👩‍💼 Teamlead Mode (--mode teamlead)

Manager-friendly report:
	•	risk
	•	blast radius
	•	ownership hints
	•	escalation guidance

## 📏 Compliance Mode (--mode compliance)

Finds policy / hygiene issues:
	•	missing resource limits
	•	:latest tags
	•	namespace misuse
	•	registry hygiene
	•	bad env patterns

## 🧨 Chaos Mode

Suggests targeted chaos experiments based on real weaknesses:
	•	node drain
	•	registry outage simulation
	•	disruption tests
	•	restart storms

⸻

# 📦 Installation

Build from source

Requires Go ≥ 1.25.4

```bash
git clone https://github.com/ppiankov/kubenow
cd kubenow
go build ./cmd/kubenow
```

(Optional) Move to PATH

```bash
sudo mv kubenow /usr/local/bin/
```

Helps DevOps engineers identify pods with incorrectly configured
resource limits/requests, reducing cluster waste and improving stability.

# 🚀 Usage

You only need:
	•	a kubeconfig
	•	an LLM endpoint
	•	a model name

Example (local Ollama)
```bash
./kubenow \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b \
  --mode incident
```
Example (OpenAI)

```bash
export KUBENOW_API_KEY="sk-yourkey"

./kubenow \
  --llm-endpoint https://api.openai.com/v1 \
  --model gpt-4.1-mini \
  --mode teamlead
```

Example (one specific namespace)

```bash
./kubenow \
  --namespace prod \
  --mode pod \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

Example (filter specific pods with wildcard)

```bash
./kubenow \
  --include-pods "payment-*,checkout-*" \
  --mode incident \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

Example (exclude system namespaces)

```bash
./kubenow \
  --exclude-namespaces "kube-system,kube-public,kube-node-lease" \
  --mode default \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

Example (search for memory-related issues with hint)

```bash
./kubenow \
  --include-keywords "OOM,memory,killed" \
  --hint "memory leak investigation" \
  --mode incident \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

Example (exclude verbose logs)

```bash
./kubenow \
  --exclude-keywords "debug,trace,verbose" \
  --mode pod \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

Example (focus on specific namespace pattern)

```bash
./kubenow \
  --include-namespaces "prod-*" \
  --exclude-pods "*test*,*debug*" \
  --mode compliance \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

Example (large cluster with controlled concurrency)

```bash
./kubenow \
  --max-pods 100 \
  --max-concurrent-fetches 3 \
  --mode incident \
  --llm-endpoint https://api.openai.com/v1 \
  --model gpt-4.1-mini
```

Example (power-user mode with all enhancements)

```bash
./kubenow \
  --mode incident \
  --enhance-technical \
  --enhance-priority \
  --enhance-remediation \
  --include-pods "payment-*,checkout-*" \
  --hint "possible memory leak" \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

Example (watch mode: continuous monitoring)

```bash
./kubenow \
  --watch-interval 1m \
  --watch-alert-new-only \
  --mode incident \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

Example (export to Markdown for GitHub issue)

```bash
./kubenow \
  --mode incident \
  --output incident-report.md \
  --include-namespaces "prod-*" \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

Example (export to JSON for API/dashboard)

```bash
./kubenow \
  --mode compliance \
  --output compliance-audit.json \
  --llm-endpoint https://api.openai.com/v1 \
  --model gpt-4.1-mini
```


# 🧠 Recommended Models
| Mode | Best Local | Best Cloud |notes |
|-------|-----|-----------|-----------|
| incident | mixtral:8x22b | GPT-4.1 Mini | concise, obedient|
| pod | llama3:70b (if patient) | GPT-4.1 | detail friendly |
| teamlead | mixtral:8x22b | GPT-4.1 Mini | leadership tone |
| compliance | mixtral or Qwen |GPT-4.1 Mini | structured |
| chaos | mixtral |GPT-4.1 Mini | creative but grounded|

Quote of the project:
“11434 is enough.”

# 🔧 Command-Line Flags

## Core Flags
```bash
--kubeconfig <path>           Path to kubeconfig (optional)
--namespace <ns>              Only analyze this namespace
--mode <type>                 default|pod|incident|teamlead|compliance|chaos
--llm-endpoint <url>          OpenAI-compatible URL
--model <name>                Model name (mixtral:8x22b, gpt-4.1-mini, etc.)
--api-key <key>               LLM API key (optional if local)
--max-pods <num>              Max problem pods to include (default: 20)
--log-lines <num>             Logs per container (default: 50)
--format <type>               Output format: human|json (default: human)
--timeout-seconds <num>       LLM call timeout in seconds (default: 60)
--max-concurrent-fetches <num> Max concurrent log fetches (default: 5, prevents API throttling)
```

## Filtering Flags
```bash
--include-pods <patterns>       Comma-separated pod name patterns (supports wildcards: *, ?)
--exclude-pods <patterns>       Comma-separated pod name patterns to exclude
--include-namespaces <patterns> Comma-separated namespace patterns (supports wildcards)
--exclude-namespaces <patterns> Comma-separated namespace patterns to exclude
--include-keywords <keywords>   Only show logs/events containing these keywords
--exclude-keywords <keywords>   Filter out logs/events containing these keywords
--hint <description>            Problem hint to guide LLM (e.g., 'memory leak', 'OOM')
```

## Enhancement Flags (Optional Power-User Features)
```bash
--enhance-technical             Add technical depth (stack traces, config diffs, deeper analysis)
--enhance-priority              Add priority scoring (numerical scores, SLO impact, blast radius)
--enhance-remediation           Add detailed remediation (step-by-step fixes, rollback, prevention)
```

**Default behavior**: kubenow provides concise, actionable output. Enhancement flags are **optional** and add deeper analysis when needed.

## Watch Mode Flags
```bash
--watch-interval <duration>     Enable watch mode with interval (e.g., '30s', '1m', '5m')
--watch-iterations <num>        Max watch iterations (0 = infinite, default: 0)
--watch-alert-new-only          Only show new/changed issues in watch mode
```

## Export Flags
```bash
--output <filepath>             Save report to file (format auto-detected: .json, .md, .html, .txt)
```

# 🎯 Advanced Filtering & Hints

## Pod & Namespace Filtering

Use wildcard patterns to focus your analysis:

**Include patterns**: Only analyze pods/namespaces matching these patterns
- `--include-pods "payment-*,api-*"` - Only payment and api pods
- `--include-namespaces "prod-*"` - Only production namespaces

**Exclude patterns**: Skip specific pods/namespaces
- `--exclude-pods "*test*,*debug*"` - Ignore test/debug pods
- `--exclude-namespaces "kube-system,monitoring"` - Skip system namespaces

**Wildcard support**:
- `*` matches any characters
- `?` matches a single character
- Multiple patterns separated by commas

## Keyword Filtering

Filter logs and events by content:

**Include keywords**: Only show logs/events containing specific keywords
- `--include-keywords "error,fatal,exception"` - Focus on errors
- `--include-keywords "OOM,memory"` - Memory-related issues

**Exclude keywords**: Filter out noise
- `--exclude-keywords "debug,trace,verbose"` - Remove debug messages
- `--exclude-keywords "health check,readiness"` - Ignore health checks

**Case-insensitive**: Keywords are matched case-insensitively

## Problem Hints

Guide the LLM analysis with context:

```bash
--hint "memory leak in payment service"
--hint "network connectivity issues"
--hint "database connection pool exhaustion"
--hint "image pull failures"
```

The hint helps the LLM:
- Prioritize relevant findings
- Provide more targeted recommendations
- Connect related symptoms
- Suggest specific debugging steps

**Best practices:**
- Be specific but concise
- Mention the suspected root cause
- Include relevant component names
- Works with all modes (incident, pod, teamlead, etc.)

## Concurrency Control

Prevent API throttling and rate limits with controlled parallelism:

```bash
--max-concurrent-fetches <num>
```

**Recommended values:**

| Cluster Size | Recommended | Description |
|-------------|-------------|-------------|
| Small (< 50 pods) | `5-10` | Safe default, minimal throttling risk |
| Medium (50-200 pods) | `3-5` | Conservative for production clusters |
| Large (200+ pods) | `2-3` | Strict limit to avoid overwhelming API |
| EKS/GKE/AKS | `3-5` | Cloud providers may have stricter limits |
| Local (minikube/kind) | `10-20` | Less restrictive for local development |

**Symptoms of too high concurrency:**
- `Waited for X.XXs due to client-side throttling` messages
- Slow snapshot collection despite parallelism
- Kubernetes API server errors (429 Too Many Requests)

**Example with concurrency control:**
```bash
./kubenow \
  --max-pods 100 \
  --max-concurrent-fetches 3 \
  --mode incident \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

This fetches logs from up to 100 pods, but only 3 at a time, preventing API throttling.

## Enhancement Flags: Deep Dive

kubenow defaults to **simple, concise output** for fast incident response. For power users who need deeper analysis, three optional enhancement flags are available:

### --enhance-technical

Adds deep technical analysis to the output:
- **Stack traces**: Extracted and highlighted from logs
- **Memory dumps**: Parsed memory statistics, heap dumps, OOM killer details
- **Config diffs**: Recent configuration changes that might have caused issues
- **Deeper analysis**: Network errors, filesystem issues, syscalls, signals

**When to use**: Debugging complex issues, performance problems, or when you need low-level details.

**Output additions**: Adds a `technicalDetails` object with fields `stackTrace`, `memoryDump`, `configDiff`, `deeperAnalysis`

**Example:**
```bash
./kubenow --mode incident --enhance-technical \
  --hint "memory leak" \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

### --enhance-priority

Adds quantitative priority and impact scoring:
- **Priority score**: Numeric 1-10 scale (10 = most critical)
- **SLO impact**: Estimated SLO/SLA violations (e.g., "3/5 services below SLO")
- **Blast radius**: Scope of impact (e.g., "high - affects 40% of users")
- **Urgency**: Classification as immediate|high|medium|low

**When to use**: Triaging multiple incidents, reporting to management, or prioritizing fixes.

**Output additions**: Adds `priorityScore`, `sloImpact`, `blastRadius`, `urgency` fields to issue objects

**Example:**
```bash
./kubenow --mode incident --enhance-priority \
  --max-pods 50 \
  --llm-endpoint https://api.openai.com/v1 \
  --model gpt-4.1-mini
```

### --enhance-remediation

Adds comprehensive fix procedures:
- **Remediation steps**: Numbered, specific kubectl commands with verification checks
- **Rollback procedure**: Exact command to roll back (including revision numbers)
- **Prevention tips**: Actionable recommendations to prevent recurrence
- **Verification checks**: Commands to confirm the fix worked

**When to use**: Training junior engineers, creating runbooks, or when fixes need careful documentation.

**Output additions**: Adds `remediationSteps`, `rollbackProcedure`, `preventionTips` arrays/fields

**Example:**
```bash
./kubenow --mode pod --enhance-remediation \
  --include-pods "payment-*" \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

### Combining Enhancements

Mix and match enhancement flags for maximum detail:

```bash
# Power user mode: ALL enhancements
./kubenow --mode incident \
  --enhance-technical \
  --enhance-priority \
  --enhance-remediation \
  --max-pods 100 \
  --max-concurrent-fetches 3 \
  --llm-endpoint https://api.openai.com/v1 \
  --model gpt-4.1-mini
```

```bash
# Technical + remediation (great for debugging)
./kubenow --mode pod \
  --enhance-technical \
  --enhance-remediation \
  --include-keywords "OOM,crash" \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

### Performance Considerations

**Token usage**: Enhancements increase LLM token consumption
- Base prompt: ~500-800 tokens
- With all enhancements: ~900-1200 tokens
- Impact: Slightly slower responses, higher API costs (still reasonable)

**Model recommendations for enhancements**:
| Enhancement | Min Model | Recommended |
|-------------|-----------|-------------|
| --enhance-technical | 8B+ | mixtral:8x22b, llama3:70b |
| --enhance-priority | 7B+ | mixtral:8x22b, qwen:14b |
| --enhance-remediation | 8B+ | mixtral:8x22b, gpt-4.1-mini |
| All combined | 70B+ / GPT-4 class | mixtral:8x22b (local), GPT-4.1 (cloud) |

**Note**: Smaller models (< 7B) may struggle with complex conditional JSON generation for enhanced output.

## 👁️ Watch Mode: Continuous Monitoring

Watch mode enables **continuous cluster monitoring** with diff detection. kubenow will poll the cluster at regular intervals, detect new, resolved, and ongoing issues, and alert you to changes.

### Basic Watch Mode

```bash
./kubenow \
  --watch-interval 30s \
  --mode incident \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

This checks the cluster every 30 seconds and shows all issues detected.

### Watch Mode Flags

**--watch-interval <duration>**: Enable watch mode with the specified interval
- Examples: `30s`, `1m`, `5m`, `10m`
- Recommended: `30s` for incidents, `1m-5m` for monitoring
- Watch mode is **disabled by default** (keeps kubenow simple by default)

**--watch-iterations <num>**: Limit the number of iterations (default: 0 = infinite)
- `0`: Run forever until Ctrl+C
- `10`: Run exactly 10 iterations then stop
- Useful for scheduled jobs or time-boxed investigations

**--watch-alert-new-only**: Only alert on new or changed issues
- Filters out ongoing issues from previous iterations
- Reduces noise during continuous monitoring
- Great for Slack/webhook integrations (Phase 3)

### Watch Mode Examples

**Continuous incident monitoring (Ctrl+C to stop)**
```bash
./kubenow \
  --watch-interval 1m \
  --mode incident \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

**Monitor for 10 iterations (10 minutes with 1m interval)**
```bash
./kubenow \
  --watch-interval 1m \
  --watch-iterations 10 \
  --mode default \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

**Alert only on new issues (great for dashboards)**
```bash
./kubenow \
  --watch-interval 30s \
  --watch-alert-new-only \
  --mode incident \
  --llm-endpoint https://api.openai.com/v1 \
  --model gpt-4.1-mini
```

**Watch specific namespace with filtering**
```bash
./kubenow \
  --watch-interval 1m \
  --namespace prod \
  --include-pods "payment-*,checkout-*" \
  --watch-alert-new-only \
  --mode pod \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

### How Watch Mode Works

1. **Iteration 1**: Collects initial snapshot, calls LLM, shows all issues
2. **Iteration 2+**: Compares with previous snapshot and detects:
   - **NEW** issues (not in previous snapshot) - highlighted in red
   - **RESOLVED** issues (in previous but not current) - highlighted in green
   - **ONGOING** issues (in both snapshots) - shown unless `--watch-alert-new-only`

3. **Output**: Shows diff first, then full LLM analysis (unless only showing new issues)
4. **Graceful shutdown**: Ctrl+C stops watch mode cleanly

### Watch Mode Output Example

```
[2025-12-03 15:45:23 UTC] Iteration 2/10
----------------------------------------
[kubenow] Collecting cluster snapshot...

NEW ISSUES DETECTED: 2
  [NEW] prod/payment-api - CrashLoopBackOff
  [NEW] prod/checkout-worker (container: init) - ImagePullBackOff

RESOLVED ISSUES: 1
  [RESOLVED] staging/test-pod - Pending

ONGOING ISSUES: 3
  [ONGOING] prod/auth-service - OOMKilled
  [ONGOING] prod/redis-cache - Evicted
  [ONGOING] prod/db-backup - Failed

[kubenow] Calling LLM endpoint...

TOP ISSUES:
1. prod/payment-api - CrashLoopBackOff - critical
   Root Cause: Database connection pool exhausted
   Fix: kubectl -n prod scale deploy/payment-api --replicas=0 && kubectl -n prod scale deploy/payment-api --replicas=3

[... full LLM analysis ...]

Next check in 1m... (Ctrl+C to stop)
```

### Watch Mode Best Practices

**Interval selection:**
- `10s-30s`: Active incident response (high API load)
- `1m-2m`: Continuous monitoring (balanced)
- `5m-10m`: Background health checks (low overhead)

**Performance considerations:**
- Watch mode increases API load (one snapshot per interval)
- Use `--max-concurrent-fetches` to prevent throttling
- Combine with namespace/pod filters to reduce snapshot size
- Consider LLM costs for cloud providers (one API call per iteration)

**Graceful shutdown:**
- Press Ctrl+C to stop watch mode gracefully
- kubenow will finish the current iteration before exiting
- No partial outputs or corrupted state

**Use cases:**
- Active incident monitoring during deployments
- Continuous compliance checks
- Post-incident surveillance (watch for recurrence)
- Team dashboards (pipe to files or webhooks)

## 💾 Export Reports: Save & Share

Export mode allows you to **save kubenow reports to files** for sharing with teams, creating post-mortems, or building audit trails.

### Basic Export

```bash
./kubenow \
  --mode incident \
  --output report.json \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

This saves the incident report to `report.json` instead of printing to stdout.

### Supported Export Formats

Export format is **auto-detected from file extension**:

| Extension | Format | Description |
|-----------|--------|-------------|
| `.json` | JSON | Structured data with metadata wrapper |
| `.md` | Markdown | GitHub-flavored markdown for wikis/issues |
| `.html` | HTML | Self-contained HTML (inline CSS, no external deps) |
| `.txt` | Plain text | Human-readable format (same as stdout) |

### Export Examples

**JSON export (for APIs, scripts, dashboards)**
```bash
./kubenow \
  --mode incident \
  --output incident-2025-12-03.json \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

Output includes metadata:
```json
{
  "metadata": {
    "generatedAt": "2025-12-03T15:45:23Z",
    "kubenowVersion": "v1.0.0",
    "clusterName": "production-eks",
    "mode": "incident",
    "filters": {
      "includePods": "payment-*",
      "includeNamespaces": "prod"
    }
  },
  "result": {
    "topIssues": [...],
    "rootCauses": [...],
    "actions": [...]
  }
}
```

**Markdown export (for GitHub issues, Confluence, wikis)**
```bash
./kubenow \
  --mode pod \
  --output pod-debug.md \
  --include-pods "payment-*" \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

Creates a beautiful GitHub-flavored markdown file:
```markdown
# kubenow Report: pod

**Generated:** 2025-12-03 15:45:23 UTC
**Cluster:** production-eks
**Mode:** pod

---

## Problem Pods

### 1. prod/payment-api-7f8d9c - CRITICAL

**Type:** CrashLoopBackOff
**Failing Container:** api
**Summary:** Container repeatedly crashing...

**Fix Commands:**
` ``bash
kubectl -n prod logs payment-api-7f8d9c
kubectl -n prod describe pod payment-api-7f8d9c
` ``
```

**HTML export (for email, browser viewing, offline access)**
```bash
./kubenow \
  --mode incident \
  --output report.html \
  --llm-endpoint https://api.openai.com/v1 \
  --model gpt-4.1-mini
```

Creates a self-contained HTML file:
- Inline CSS (no external dependencies)
- Color-coded severity
- Print-friendly
- Works offline
- Professional styling

**Plain text export (for logs, terminals, simple storage)**
```bash
./kubenow \
  --mode compliance \
  --output compliance-audit.txt \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

### Export with Filters

**Export filtered incident report**
```bash
./kubenow \
  --mode incident \
  --include-namespaces "prod-*" \
  --exclude-pods "*test*" \
  --output prod-incident-report.md \
  --llm-endpoint http://localhost:11434/v1 \
  --model mixtral:8x22b
```

The export file will include the filter metadata, so readers know what was included/excluded.

### Export Metadata

All export formats (except plain text) include metadata:

- **generatedAt**: ISO 8601 timestamp
- **kubenowVersion**: Tool version (for reproducibility)
- **clusterName**: Extracted from kubeconfig context
- **mode**: Analysis mode used
- **filters**: All filter flags applied (pods, namespaces, keywords)

This metadata helps with:
- Audit trails ("when was this report generated?")
- Reproducibility ("what version of kubenow?")
- Context ("which cluster and namespace?")
- Filtering transparency ("what was included/excluded?")

### Export Best Practices

**Naming conventions:**
```bash
# Timestamped reports
--output "incident-$(date +%Y-%m-%d-%H%M).json"

# Cluster-specific reports
--output "prod-eks-incident.md"

# Mode-specific reports
--output "compliance-audit-$(date +%Y%m%d).json"
```

**Use cases:**
- **JSON**: APIs, dashboards, scripts, data processing
- **Markdown**: GitHub issues, Confluence, team wikis, documentation
- **HTML**: Email attachments, browser viewing, presentations
- **Plain text**: Logs, terminals, grep-able archives

**Sharing with teams:**
- Markdown for GitHub issues (native rendering)
- HTML for email attachments (works everywhere)
- JSON for integrations (Slack bots, ticketing systems)

### Combining Watch Mode + Export

Export is **not yet supported with watch mode** in Phase 1 MVP. This will be added in Phase 2:

**Phase 2 (future):**
```bash
# Timestamped exports per iteration
./kubenow --watch-interval 1m --output report.json
# Saves: report-2025-12-03T15:45:23Z.json, report-2025-12-03T15:46:23Z.json, ...

# Or overwrite same file (latest snapshot)
./kubenow --watch-interval 1m --output report.json --output-overwrite
# Always writes to: report.json
```

For now, use single-execution mode with export, or run watch mode with stdout.

# 🧱 Architecture

```bash
cmd/kubenow/
internal/
  snapshot/   ← collects K8s data with concurrent log fetching & filtering
  prompt/     ← loads prompt templates by mode with optional hints & enhancements
  llm/        ← calls OpenAI-compatible APIs
  util/       ← kube client builder
  result/     ← output rendering and formatting
  export/     ← file export in multiple formats (JSON, Markdown, HTML, text)
  watch/      ← continuous monitoring with diff detection
```

## Snapshot Process

1. **Node Discovery**: Collects all node conditions
2. **Pod Filtering**: Applies include/exclude patterns for pods and namespaces
3. **Problem Detection**: Identifies pods with issues (CrashLoop, ImagePull, OOM, etc.)
4. **Event Collection**: Gathers pod events with keyword filtering
5. **Concurrent Log Fetch**: Fetches logs from all problem pods in parallel (performance boost!)
6. **Keyword Filtering**: Applies include/exclude keywords to logs and events

Snapshot contains:
	•	node conditions
	•	filtered problem pods with:
	•	reason
	•	restart count
	•	container states
	•	resource requests/limits
	•	image names
	•	filtered logs (concurrent fetch)
	•	filtered pod events
	•	issueType (ImagePullError | CrashLoop | OOMKilled | PendingScheduling | etc.)

## Performance Improvements

**Controlled Concurrent Log Fetching**: kubenow fetches logs from problem pods in parallel using a semaphore-based worker pool. This:
- Significantly reduces snapshot collection time
- Prevents Kubernetes API throttling with configurable concurrency limits
- Defaults to 5 concurrent fetches (safe for most clusters)
- Adjustable via `--max-concurrent-fetches` for different cluster sizes

**Smart Filtering**: Filter before analysis to reduce:
- LLM token usage (lower costs)
- Processing time
- Noise in results
- Context window pressure

**Filter Early, Analyze Fast**: Use namespace and pod filters to focus on specific areas, making kubenow even faster for targeted investigations.

**API-Friendly**: Semaphore pattern ensures kubenow never overwhelms your Kubernetes API server, even with `--max-pods 100`.

# 📄 License
MIT

# 🐉 Disclaimer

## This tool can:
	•	shame your engineers
	•	uncover your terrible cluster hygiene
	•	predict who broke production
	•	and suggest chaos tests strong enough to get you fired

Use responsibly.

## ✨ Keywords
	•	kubernetes incident response LLM
	•	kubernetes triage cli
	•	ollama kubernetes assistant
	•	k8s troubleshooting
	•	kubectl alternative
	•	k8s observability
	•	chaos engineering

---

