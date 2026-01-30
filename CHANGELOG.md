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

## [Unreleased]

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

---

## [0.1.1] - 2026-01-30

**First official release of kubenow!** 🎉

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

- [Unreleased]: https://github.com/ppiankov/kubenow/compare/v0.1.1...HEAD
- [0.1.1]: https://github.com/ppiankov/kubenow/releases/tag/v0.1.1
