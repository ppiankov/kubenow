# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Planned
- Auto-detect Prometheus in-cluster
- Cloud provider cost integration (AWS, GCP, Azure)
- Historical trend tracking for analyze commands
- Recommendation patches (kubectl apply-able YAML)

---

## [0.1.8] - 2026-02-06

### Fixed

#### Termination Data Visibility in Latch Mode
Fixed critical issue where container terminations were not being displayed properly:

1. **Termination Data Now Always Shown**
   - Previously: "Completed" terminations with exit code 0 were hidden
   - Now: ALL terminations shown when container has restarts
   - Impact: Users can now see WHY a container restarted, even if it was a "normal" exit
   - Example: "Completed (exit 0)" is now visible alongside restart count

2. **Termination Timestamps Added**
   - Previously: No indication of WHEN termination happened
   - Now: Shows "how long ago" the termination occurred
   - Format: "Container Restarts: 1 (last: 104d ago)"
   - Impact: Users can prioritize recent issues over old ones

3. **Enhanced Termination Display**
   - Added visual indicators:
     - 🔴 for critical issues (OOMKilled, exit 137)
     - ⚠️  for warnings (Error, non-zero exits)
     - ✓  for normal completions
   - Shows human-readable exit code meanings
   - Example: "137 (SIGKILL - usually OOMKilled): 2 times"

4. **Captured Missing Termination Data**
   - Fixed: TerminationReasons and ExitCodes maps were populated but not persisted
   - Added: LastTerminationTime field to SpikeData structure
   - Now captures FinishedAt timestamp from container status

**User Impact**: Operators can now see complete termination history including normal exits, understand when issues occurred, and prioritize recent problems over historical ones.

---

## [0.1.7] - 2026-02-06

### Fixed

#### Critical Feature Completions
Four major incomplete features have been fixed:

1. **Spike Data Now Included in Exports** (CRITICAL FIX)
   - Previously: Spike monitoring data was lost when using `--export-file`
   - Now: All spike data (OOMKills, exit codes, termination reasons) saved to JSON
   - Impact: Latch mode results are now fully exportable for analysis/automation

2. **Namespace Regex Filtering Now Works**
   - Previously: `--namespace-regex` flag existed but did nothing
   - Now: Fully functional regex filtering of namespaces
   - Example: `--namespace-regex "prod.*"` filters to production namespaces
   - Validates regex pattern and returns clear error if invalid

3. **Workload Runtime Calculation Implemented**
   - Previously: Always showed "N/A", `--min-runtime-days` didn't work
   - Now: Calculates actual runtime from creation timestamp
   - Shows runtime in days (e.g., "45d")
   - Properly filters workloads younger than `--min-runtime-days`
   - Impact: Can now exclude recently deployed workloads from analysis

4. **LimitRange Default Detection Added**
   - Previously: Showed LimitRange defaults but never flagged workloads using them
   - Now: Detects when workloads likely use LimitRange defaults
   - Heuristic: Flags workloads with common default values (0.1, 0.5, 1.0 cores)
   - Sets `UsingDefaultRequests: true` in output
   - Adds note to quota context: "Possibly using LimitRange defaults"
   - Impact: Identify workloads that may not have intentionally set requests

### Why This Matters
- **Export completeness**: Spike monitoring data no longer lost
- **Filtering accuracy**: Namespace regex actually works as documented
- **Age filtering**: Can properly exclude new workloads from analysis
- **Default detection**: Identify unintentional resource requests

---

## [0.1.6] - 2026-02-06

### Added

#### Comprehensive Container Termination Tracking
- **ALL termination reasons tracked**, not just OOMKilled:
  - `OOMKilled` - Out of memory (exit code 137)
  - `Error` - Container exited with error
  - `ContainerCannotRun` - Configuration issue
  - `CrashLoopBackOff` - Repeatedly failing to start
  - `ImagePullBackOff` / `ErrImagePull` - Image issues
  - And all other Kubernetes termination reasons
- **Exit code tracking and interpretation**:
  - Exit code 137 (SIGKILL) - Usually OOMKilled or killed by system
  - Exit code 143 (SIGTERM) - Graceful shutdown
  - Exit code 139 (SIGSEGV) - Segmentation fault
  - Exit code 1/2 - Application errors
  - Exit code 126/127 - Command execution issues
  - All exit codes tracked with frequency counts
- **Termination reason statistics**: Shows how many times each reason occurred
- **Enhanced output**:
  - Separate sections for termination reasons and exit codes
  - Human-readable explanations for each exit code
- **Better interpretation guidance**: Explains what each signal means for resource sizing

### Fixed

#### Export File Functionality
- **Fixed `--export-file` flag** - now works with table output
  - Previously: only worked with `--output json`
  - Now: works with default table output
  - Behavior: Shows table on screen, saves JSON to file
  - Best of both worlds: human-readable output + machine-parseable export
  - Example: `kubenow analyze requests-skew --export-file report.json` (table output + JSON export)

### Why This Matters
- **Comprehensive failure detection**: Linux kills containers for many reasons (OOM, CPU limits, cgroups, etc.)
- **Root cause analysis**: Exit codes reveal exact failure mode
- **Better safety decisions**: Understand stability before reducing resources
- **Automation support**: Export always works, regardless of output format

---

## [0.1.5] - 2026-02-06

### Fixed

#### Latch Mode: Performance & API Throttling
- **CRITICAL FIX**: Eliminated API throttling storm in spike monitoring
  - Previously: checked critical signals on EVERY sample (every 5s) = hundreds of API calls/second
  - Now: checks critical signals ONCE at end of monitoring = minimal API impact
  - Batches all API calls (List pods by namespace, not Get individual pods)
  - Only checks workloads that were actually monitored (respects namespace filter)
  - Result: ~99% reduction in Kubernetes API calls during spike monitoring

#### Workloads Without Metrics: Diagnostics
- **Auto-diagnosis**: Samples up to 5 workloads without metrics to identify root cause
  - Checks if pods are running
  - Verifies pod labels (app, app.kubernetes.io/name)
  - Identifies missing ServiceMonitor/PodMonitor configuration
  - Provides actionable troubleshooting guidance in output
  - Example: "Pod running with labels, but no Prometheus metrics - check ServiceMonitor/PodMonitor configuration"

#### Progress Indicators
- **Spike monitoring progress**: Shows progress every 10% during latch mode
  - Example: "[latch] Progress: 30% (54/180 samples)"
  - Helps users understand monitoring is working despite long duration
  - More visibility into sampling progress

### Impact
- **Large cluster support**: Latch mode now usable on clusters with hundreds/thousands of pods
- **No more throttling**: Eliminates "client-side throttling" delays
- **Better diagnostics**: Understand WHY workloads lack metrics, not just that they do

---

## [0.1.4] - 2026-02-06

### Added

#### ResourceQuota & LimitRange Awareness
- **`analyze requests-skew`**: Now detects and displays namespace-level resource constraints
  - **ResourceQuota detection**: Shows current quota usage (CPU/Memory) and utilization percentage
  - **Potential quota savings calculation**: Estimates how much quota could be freed by reducing over-provisioned requests to p95
  - **LimitRange awareness**: Displays default requests/limits set at namespace level
    - Shows default CPU/Memory requests and limits
    - Shows min/max constraints for containers
  - **Quota context in results**: Each workload now includes quota context information
  - **Enhanced output section**: New "Namespace ResourceQuota & LimitRange Analysis" section showing:
    - Current quota utilization (used/hard)
    - Potential savings in cores and GiB
    - LimitRange defaults that may be applied to workloads
    - Impact guidance for quota-constrained namespaces
  - **JSON export**: Quota and LimitRange data included in JSON output for automated processing

#### Configurable Result Sorting
- **`analyze requests-skew`**: New `--sort-by` flag for custom result ordering:
  - `impact` (default) - Sort by impact score (skew × absolute resources) - highest first
  - `skew` - Sort by CPU skew ratio - highest over-provisioning first
  - `cpu` - Sort by wasted CPU cores - most wasted first
  - `memory` - Sort by wasted memory - most wasted first
  - `name` - Sort alphabetically by namespace/workload
  - All sorts are descending (worst-first) except name (ascending)

#### Latch Mode: Critical Signal Detection
- **Spike monitoring now detects critical events** during real-time monitoring:
  - **OOMKill detection**: Tracks Out-of-Memory kills during monitoring period
  - **Container restart tracking**: Counts restarts and captures restart reasons
  - **Pod eviction detection**: Identifies pods evicted due to resource pressure
  - **CrashLoopBackOff detection**: Flags containers stuck in crash loops
  - **Event correlation**: Captures related Kubernetes events (FailedScheduling, BackOff, etc.)
  - **Safety warnings**: Automatically warns against reducing requests for workloads with OOMKills or instability
  - **Enhanced output**: New "Critical Signals" section shows:
    - OOMKills with clear warning that memory requests are too low
    - Restart counts and reasons
    - Recent events timeline (last 5 events shown)
    - Interpretation guidance for each signal type

### Why This Matters
- **Quota-constrained namespaces**: See how much capacity could be freed for new workloads
- **Default detection**: Identify workloads using LimitRange defaults vs explicitly set requests
- **Better decision making**: Understand both actual usage AND namespace constraints when right-sizing
- **Safety-first spike analysis**: Prevents dangerous request reductions by detecting OOMKills and instability during monitoring
- **Root cause correlation**: See if high CPU spikes coincide with OOMKills (classic under-resourced pattern)

---

## [0.1.3] - 2026-02-06

### Fixed
- **`analyze requests-skew`**: Now respects the global `--namespace` flag
  - Previously, the `--namespace` flag was ignored, causing the command to analyze all namespaces
  - Added namespace filtering support to RequestsSkewAnalyzer
  - Fixed issue where `--namespace ns` would still scan all 33 namespaces instead of just one

---

## [0.1.2] - 2026-01-30

**First official release of kubenow!** 🎉

### Added

#### Real-Time Problem Monitor
- **`kubenow monitor`**: Live terminal UI for cluster problem monitoring
  - **Attention-first design**: Screen stays empty when healthy, shows only problems
  - **Heartbeat indicator**: Pulsing dot shows monitor is actively running
  - **Real-time detection**: OOMKills, CrashLoopBackOff, ImagePullBackOff, Failed pods
  - **Severity-based filtering**: FATAL/CRITICAL/WARNING classification
  - **Terminal UI**: Built with bubbletea for smooth, responsive experience
  - **Namespace filtering**: Monitor specific namespaces or entire cluster
  - **Recent events timeline**: Last 5 minutes of cluster events
  - **Cluster statistics**: Real-time pod/node health stats
  - **Keyboard controls**:
    - `c`/`v` to print all problems to terminal (COPYABLE - exits alternate screen, shows plain text, press Enter to return)
    - `Space`/`p` to pause/resume updates (freeze display to read)
    - `a` to navigate problems one-by-one with arrow keys
    - `↑`/`↓` or `j`/`k` to navigate between problems
    - `Home`/`g` to jump to first, `End`/`G` to jump to last
    - `e` to export all problems to timestamped file with kubectl commands
    - `q`/`Ctrl+C`/`Esc` to exit
  - No dashboards, no navigation - problems auto-appear when they happen

#### Spike Analysis Guidance
- **`SPIKE-ANALYSIS.md`**: Comprehensive documentation for interpreting CPU spike data
  - Formula for calculating resource requests from spike data
  - Safety factor guidelines by workload type (RAG: 2.5-3.0x, APIs: 1.5-2.0x, etc.)
  - Step-by-step sizing examples with kubectl commands
  - Historical validation philosophy ("would have worked" vs "should do this")
  - Common patterns (RAG/LLM inference, API caching, background workers)
  - Troubleshooting guide for missing metrics and throttling
  - Best practices for measurement windows and iterative sizing
- **`--show-recommendations` flag**: Add calculated CPU request recommendations to spike monitoring output
  - Auto-selects safety factor based on spike ratio (1.2x-2.5x)
  - Optional `--safety-factor` override for custom multipliers
  - Shows "Recommended CPU" column with safety-factor-adjusted values
  - Includes guidance on applying recommendations with kubectl
  - Default output remains unchanged (raw spike data)

### Improved

#### requests-skew Output Clarity
- **Summary line enhancement**: When 0 workloads are analyzed, now explicitly shows count of workloads without metrics
  - Old: "Analyzed: 0 workloads"
  - New: "Analyzed: 0 workloads (359 have no Prometheus metrics)"
- **Workloads without metrics section**: Added clarifying note explaining why these workloads can't be analyzed
  - Makes it clear that requests-skew requires Prometheus metrics to compare requested vs actual usage
  - Reduces confusion when many workloads lack metrics (e.g., missing ServiceMonitors)

#### Documentation
- **README**: Restructured with monitor mode as first feature
- **README**: Added tool comparison guide (monitor vs k9s vs Grafana vs kubectl)

### Changed

#### Dependencies
- **k8s.io**: Updated to v0.35.0 (requires Go 1.25.0)
- **Go version**: Updated to go 1.25.0 in go.mod
- **CI**: Added GOTOOLCHAIN=auto to support Go 1.25 auto-download
- **CI**: Temporarily disabled golangci-lint (compatibility issues with Go 1.25)

### Fixed

- **CI**: Multiple fixes for Go version compatibility issues
- **CI**: Fixed test coverage command to handle packages without tests gracefully
- **Monitor UI**: Fixed fmt.Println redundant newlines
- **golangci-lint config**: Fixed deprecation warnings

---

## [0.1.1] - 2026-01-30

Kubernetes cluster analysis tool combining deterministic cost optimization with optional LLM-assisted diagnostics.

### Added

#### LLM-Powered Analysis
- **Incident triage mode**: Ranked, actionable issue analysis
- **Pod debugging mode**: Deep dive into pod issues
- **Teamlead mode**: Manager-friendly reports with risk assessment
- **Compliance mode**: Policy and hygiene checks
- **Chaos mode**: Chaos engineering suggestions based on cluster weaknesses
- **Default mode**: General cluster analysis
- **Watch mode**: Continuous monitoring with configurable intervals
- **Export functionality**: JSON, Markdown, HTML, Plain Text formats
- **Smart filtering**: Include/exclude pods, namespaces, keywords
- **Enhancement flags**: Technical depth, priority scoring, remediation steps

#### Deterministic Analysis Commands
- **`analyze requests-skew`**: Identify over-provisioned resource requests vs actual usage
  - Prometheus metrics integration
  - Time window analysis (configurable, default 30d)
  - Skew ratio calculation (requested / avg used)
  - Impact scoring for prioritization
  - Namespace filtering with regex
  - Table and JSON output formats
  - Export to file support
  - **Safety Analysis**:
    - OOMKill detection
    - Container restart tracking
    - CPU throttling measurement
    - Spike detection (p95/p99/p99.9/max)
    - Safety ratings (SAFE/CAUTION/RISKY/UNSAFE)
    - Automatic safety margins for risky workloads
  - **Ultra-Spike Detection System**:
    - **Statistical detection**: Identifies sub-scrape-interval spikes using max/p99 ratios
    - **AI workload pattern detection**: Scans container specs for AI/ML indicators
    - **Real-time latch mode**: High-frequency sampling (1-5s) to catch actual spikes between Prometheus scrapes
    - Ideal for RAG queries, AI inference, and other millisecond-level bursts
    - CLI flags: `--watch-for-spikes`, `--spike-duration`, `--spike-interval`
  - **Workloads Without Metrics Detection**:
    - Tracks workloads found in K8s API but missing from Prometheus
    - Displays clear warnings with affected workloads grouped by namespace
    - Recommends latch mode for real-time monitoring
    - **RAG-specific guidance**: Warns that RAG workloads require ≤1s sampling intervals
    - Provides troubleshooting steps for ServiceMonitor configuration
    - Included in JSON output for automated processing

- **`analyze node-footprint`**: Historical capacity feasibility simulation
  - Bin-packing simulation (First-Fit Decreasing algorithm)
  - Workload envelope calculation (p50/p95/p99)
  - Feasibility checks with detailed reasons
  - Headroom calculation (high/medium/low/very low)
  - Custom node type support
  - Multi-cloud node templates (AWS c5/r5 series)
  - Estimated savings calculations
  - **Safety warnings**: Checks for unstable workloads before topology changes

#### Infrastructure
- **Cobra CLI Framework**: Modern command-line interface with subcommands
  - Dedicated subcommands: `incident`, `pod`, `teamlead`, `compliance`, `chaos`, `default`, `analyze`
  - Improved help text and usage documentation
  - Better flag organization and validation
- **Prometheus Integration**: Full metrics provider with query builders
  - Support for explicit URL and port-forward workflows
  - Optimized PromQL queries for CPU and memory metrics
  - Mock metrics provider for testing
  - **Metric Auto-Discovery**: Queries Prometheus for available metrics, tries multiple patterns
  - **Workload Tracking**: Identifies workloads with missing Prometheus metrics
- **Kubernetes Metrics API**: Real-time spike monitoring
  - High-frequency sampling via Metrics API
  - Thread-safe data collection
  - Spike detection and reporting
- **Bin-Packing Engine**: Deterministic workload placement simulation
  - First-Fit Decreasing algorithm
  - Resource constraint validation
  - Utilization and headroom calculation
- **Testing Infrastructure**:
  - Comprehensive unit tests (70%+ coverage)
  - GitHub Actions CI/CD pipeline
  - Multi-platform builds (Linux, macOS, Windows for amd64/arm64)
  - golangci-lint integration
  - Security scanning with Trivy
- **Build Automation**:
  - Makefile with standard targets (build, test, lint, clean)
  - GoReleaser configuration for automated releases
  - Multi-platform build support
  - Version stamping from git tags

#### CLI Features
- **Standard Exit Codes**:
  - 0: Success
  - 1: Policy failure (reserved for future use)
  - 2: Invalid input or validation errors
  - 3: Runtime errors (API failures, network issues)
- **Global Flags**: `--kubeconfig`, `--namespace`, `--verbose`, `--config`
- **Progress Indicators**: Real-time feedback during analysis (namespace discovery, workload processing)
- **Silent Mode**: `--silent` flag for CI/CD pipelines (suppresses progress output)
- **Kubernetes Integration**: Full cluster snapshot collection (pods, events, logs, nodes)
- **OpenAI-Compatible API Support**: Works with Ollama, OpenAI, DeepSeek, Groq, etc.

#### Documentation
- Comprehensive README with examples and installation instructions
- CONTRIBUTING.md with development guidelines
- Architecture documentation
- Keep a Changelog format for version history
- **MANIFESTO.md**: Design philosophy (Attention-First Software principles)

### Project Structure
- `cmd/kubenow/` - Main entry point
- `internal/cli/` - CLI commands (Cobra)
- `internal/analyzer/` - Analysis logic (requests-skew, node-footprint, bin-packing)
- `internal/metrics/` - Prometheus integration and latch monitoring
- `internal/models/` - Data structures
- `internal/llm/` - LLM client
- `internal/snapshot/` - Kubernetes snapshot collection
- `internal/export/` - Export formats
- `internal/watch/` - Watch mode
- `internal/prompt/` - LLM prompt templates
- `internal/util/` - Shared utilities

### Dependencies
- k8s.io/client-go v0.35.0
- k8s.io/metrics v0.35.0
- github.com/prometheus/client_golang
- github.com/spf13/cobra
- github.com/olekukonko/tablewriter

---

## Links

- [Unreleased]: https://github.com/ppiankov/kubenow/compare/v0.1.8...HEAD
- [0.1.8]: https://github.com/ppiankov/kubenow/compare/v0.1.7...v0.1.8
- [0.1.7]: https://github.com/ppiankov/kubenow/compare/v0.1.6...v0.1.7
- [0.1.6]: https://github.com/ppiankov/kubenow/compare/v0.1.5...v0.1.6
- [0.1.5]: https://github.com/ppiankov/kubenow/compare/v0.1.4...v0.1.5
- [0.1.4]: https://github.com/ppiankov/kubenow/compare/v0.1.3...v0.1.4
- [0.1.3]: https://github.com/ppiankov/kubenow/compare/v0.1.2...v0.1.3
- [0.1.2]: https://github.com/ppiankov/kubenow/compare/v0.1.1...v0.1.2
- [0.1.1]: https://github.com/ppiankov/kubenow/releases/tag/v0.1.1
