# kubenow Architecture

This document describes the architecture, design decisions, and implementation details of kubenow v2.0.

---

## Table of Contents

- [Overview](#overview)
- [High-Level Architecture](#high-level-architecture)
- [Component Responsibilities](#component-responsibilities)
- [Data Flow](#data-flow)
- [Prometheus Integration](#prometheus-integration)
- [Bin-Packing Algorithm](#bin-packing-algorithm)
- [Exit Code Strategy](#exit-code-strategy)
- [Testing Architecture](#testing-architecture)
- [Future Enhancements](#future-enhancements)

---

## Overview

**kubenow** is a dual-mode Kubernetes cluster analyzer:

1. **LLM-Powered Analysis**: Uses OpenAI-compatible APIs for incident triage, pod debugging, compliance checks, and chaos engineering recommendations
2. **Deterministic Analysis**: Uses Prometheus metrics for evidence-based resource optimization and cost savings

**Design Principles**:
- Deterministic analysis is AI-free, prediction-free, reproducible
- Claims are evidence-based: "This would have worked historically"
- Never prescriptive: Shows data, doesn't dictate actions
- Cobra CLI framework for modern UX
- Standard exit codes for automation-friendly behavior
- Dual output (table/JSON) for humans and machines

---

## High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        kubenow CLI (Cobra)                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────────────┐    ┌────────────────────────────┐   │
│  │   LLM Commands       │    │   Analyze Commands         │   │
│  │   ├─ incident        │    │   ├─ requests-skew         │   │
│  │   ├─ pod             │    │   └─ node-footprint        │   │
│  │   ├─ teamlead        │    │                            │   │
│  │   ├─ compliance      │    │   Uses:                    │   │
│  │   ├─ chaos           │    │   • internal/analyzer      │   │
│  │   └─ default         │    │   • internal/metrics       │   │
│  │                      │    │   • internal/result        │   │
│  │   Uses:              │    │                            │   │
│  │   • internal/snapshot│    │   Deterministic:           │   │
│  │   • internal/llm     │    │   • No AI/ML               │   │
│  │   • internal/prompt  │    │   • Historical data        │   │
│  │   • internal/watch   │    │   • Reproducible           │   │
│  └──────────────────────┘    └────────────────────────────┘   │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
           │                                    │
           │                                    │
           ▼                                    ▼
    ┌──────────────┐                   ┌──────────────────┐
    │ Kubernetes   │                   │  Prometheus      │
    │     API      │                   │      API         │
    │              │                   │                  │
    │ • Pods       │                   │ • PromQL Queries │
    │ • Events     │                   │ • Time Series    │
    │ • Logs       │                   │ • Aggregations   │
    │ • Nodes      │                   │                  │
    └──────────────┘                   └──────────────────┘
           │                                    │
           ▼                                    ▼
    ┌──────────────┐                   ┌──────────────────┐
    │ OpenAI API   │                   │  Metrics Data    │
    │ (or Ollama)  │                   │  (30d window)    │
    └──────────────┘                   └──────────────────┘
```

---

## Component Responsibilities

### `/cmd/kubenow/main.go`
- Entry point
- Calls `cli.Execute()` to start Cobra
- Minimal logic, delegates to CLI layer

### `/internal/cli/`
- **root.go**: Cobra root command, global flags (--kubeconfig, --namespace, --verbose)
- **version.go**: Version command
- **incident.go, pod.go, teamlead.go, compliance.go, chaos.go, default.go**: LLM-powered subcommands
- **llm_common.go**: Shared logic for LLM commands (validation, execution, watch mode)
- **analyze.go**: Parent command for deterministic analysis
- **analyze_requests_skew.go**: CLI wrapper for requests-skew analysis
- **analyze_node_footprint.go**: CLI wrapper for node-footprint analysis

### `/internal/snapshot/`
- Kubernetes API interaction
- Fetches pods, events, logs, nodes
- Filters by namespace, pod patterns, keywords
- Concurrent log fetching

### `/internal/llm/`
- OpenAI-compatible API client
- Chat completion requests
- Streaming support (future)

### `/internal/prompt/`
- LLM prompt templates
- Embedded as Go constants (no external .txt files)
- Mode-specific instructions (incident, pod, teamlead, compliance, chaos)

### `/internal/watch/`
- Watch mode implementation
- Interval-based polling
- Diff detection (new, resolved, ongoing issues)
- Graceful shutdown

### `/internal/export/`
- Output formatting (JSON, Markdown, HTML, Plain Text)
- Auto-detection by file extension
- Metadata embedding (timestamp, version, cluster info)

### `/internal/metrics/`
- **interface.go**: `MetricsProvider` abstraction
- **prometheus.go**: Prometheus client implementation
  - Connection modes: explicit URL, auto-detect, port-forward
  - Health checks, query execution
  - Aggregation helpers (average, percentile)
- **query.go**: PromQL query builders
  - CPU/memory usage by namespace/pod/workload
  - Resource requests/limits
  - Duration formatting (30d, 7d, 1h)
- **mock.go**: Mock metrics provider for testing

### `/internal/analyzer/`
- **requests_skew.go**: Over-provisioning analysis
  - Fetches resource requests from K8s API
  - Queries Prometheus for actual usage
  - Calculates skew ratios (requested / avg_used)
  - Computes impact scores (skew × absolute resources)
  - Ranks by impact descending
- **node_footprint.go**: Node topology simulation
  - Queries Prometheus for workload envelope (p50/p95/p99)
  - Fetches current cluster topology
  - Generates alternative node configurations
  - Runs bin-packing simulation
  - Calculates utilization, headroom, feasibility
- **binpacking.go**: First-Fit Decreasing algorithm
  - Sorts pods by size descending
  - Tries to fit on existing nodes (First-Fit)
  - Creates new nodes as needed
  - Validates resource constraints

### `/internal/models/`
- Data structures for analysis results
- Resource usage models
- Node shape definitions
- Workload envelopes

### `/internal/result/`
- Table rendering (tablewriter)
- JSON formatting
- Output file export

### `/internal/util/`
- **exit.go**: Standard exit codes
  - 0: Success
  - 1: Policy failure (reserved)
  - 2: Invalid input
  - 3: Runtime error
- **helpers.go**: Common utilities

---

## Data Flow

### LLM Analysis Flow

```
User Command
    │
    ▼
┌────────────────────┐
│  CLI Command       │  (incident.go, pod.go, etc.)
│  Parse Flags       │
└────────────────────┘
    │
    ▼
┌────────────────────┐
│  Validate Inputs   │  (llm_common.go)
│  • LLM endpoint    │
│  • Model name      │
│  • Filters         │
└────────────────────┘
    │
    ▼
┌────────────────────┐
│  Snapshot          │  (snapshot package)
│  • Fetch pods      │
│  • Fetch events    │
│  • Fetch logs      │
│  • Apply filters   │
└────────────────────┘
    │
    ▼
┌────────────────────┐
│  Build Prompt      │  (prompt package)
│  • Mode template   │
│  • Snapshot JSON   │
│  • Enhancements    │
└────────────────────┘
    │
    ▼
┌────────────────────┐
│  LLM API Call      │  (llm package)
│  • POST /v1/chat/  │
│    completions     │
│  • Parse response  │
└────────────────────┘
    │
    ▼
┌────────────────────┐
│  Render Output     │  (result package)
│  • Format result   │
│  • Export file?    │
│  • Exit code 0     │
└────────────────────┘
```

### Deterministic Analysis Flow (requests-skew)

```
User Command
    │
    ▼
┌────────────────────────┐
│  CLI Command           │  (analyze_requests_skew.go)
│  Parse Flags           │
│  • --prometheus-url    │
│  • --window 30d        │
│  • --top 10            │
└────────────────────────┘
    │
    ▼
┌────────────────────────┐
│  Create Prometheus     │  (metrics/prometheus.go)
│  Client                │
│  • Connect to URL      │
│  • Health check        │
└────────────────────────┘
    │
    ├────────────────┬────────────────────┐
    ▼                ▼                    ▼
┌─────────────┐  ┌──────────────────┐  ┌────────────────┐
│ K8s API     │  │ Prometheus API   │  │ Prometheus API │
│ Get Pods    │  │ Query CPU Usage  │  │ Query Mem Usage│
│ (requests)  │  │ avg_over_time    │  │ avg_over_time  │
└─────────────┘  └──────────────────┘  └────────────────┘
    │                │                    │
    └────────────────┴────────────────────┘
                     │
                     ▼
            ┌─────────────────┐
            │  Analyzer       │  (analyzer/requests_skew.go)
            │  • Calculate    │
            │    skew ratios  │
            │  • Compute      │
            │    impact       │
            │  • Rank results │
            └─────────────────┘
                     │
                     ▼
            ┌─────────────────┐
            │  Format Output  │  (table or JSON)
            │  • Table        │
            │  • JSON         │
            │  • Export file? │
            └─────────────────┘
                     │
                     ▼
            ┌─────────────────┐
            │  Exit Code 0    │
            └─────────────────┘
```

### Deterministic Analysis Flow (node-footprint)

```
User Command
    │
    ▼
┌────────────────────────┐
│  CLI Command           │  (analyze_node_footprint.go)
│  Parse Flags           │
│  • --prometheus-url    │
│  • --percentile p95    │
│  • --node-types CSV    │
└────────────────────────┘
    │
    ▼
┌────────────────────────┐
│  Create Prometheus     │  (metrics/prometheus.go)
│  Client                │
└────────────────────────┘
    │
    ├─────────────────┬──────────────────┐
    ▼                 ▼                  ▼
┌──────────────┐  ┌────────────────┐  ┌──────────────┐
│ K8s API      │  │ Prometheus API │  │ Prometheus API│
│ Get Nodes    │  │ Query CPU p95  │  │ Query Mem p95│
│ Get Pods     │  │ by pod         │  │ by pod       │
└──────────────┘  └────────────────┘  └──────────────┘
    │                 │                  │
    └─────────────────┴──────────────────┘
                      │
                      ▼
            ┌──────────────────────┐
            │  Analyzer            │  (analyzer/node_footprint.go)
            │  • Extract topology  │
            │  • Workload envelope │
            │  • Generate scenarios│
            └──────────────────────┘
                      │
                      ▼
            ┌──────────────────────┐
            │  Bin-Packing         │  (analyzer/binpacking.go)
            │  • Sort pods (FFD)   │
            │  • Try fit on nodes  │
            │  • Calculate util    │
            │  • Check feasibility │
            └──────────────────────┘
                      │
                      ▼
            ┌──────────────────────┐
            │  Format Output       │
            │  • Scenarios table   │
            │  • JSON with reasons │
            │  • Export file?      │
            └──────────────────────┘
                      │
                      ▼
            ┌──────────────────────┐
            │  Exit Code 0         │
            └──────────────────────┘
```

---

## Prometheus Integration

### Connection Modes

**1. Explicit URL** (most common):
```bash
kubenow analyze requests-skew --prometheus-url http://prometheus:9090
kubenow analyze requests-skew --prometheus-url https://prometheus.example.com
```

**2. Auto-Detect** (in-cluster):
```bash
kubenow analyze requests-skew --auto-detect-prometheus
```
Checks common namespaces/services:
- `kube-system/prometheus`
- `monitoring/prometheus`
- `observability/prometheus`
- `prometheus/prometheus`

**3. Port-Forward** (local testing):
```bash
# Terminal 1
kubectl port-forward -n monitoring svc/prometheus 9090:9090

# Terminal 2
kubenow analyze requests-skew --prometheus-url http://localhost:9090
```

### Query Optimization

**Problem**: Large clusters (100+ namespaces, 1000+ pods) can generate expensive queries.

**Strategies**:

1. **Namespace Filtering**: Use `--namespace-regex` to limit scope
   ```bash
   kubenow analyze requests-skew \
     --prometheus-url http://prometheus:9090 \
     --namespace-regex "prod-.*"
   ```

2. **Time Window Adjustment**: Shorter windows = less data
   ```bash
   # 7 days instead of default 30d
   kubenow analyze requests-skew \
     --prometheus-url http://prometheus:9090 \
     --window 7d
   ```

3. **Step Size**: Use 5m step for 30d window (default), adjust if needed
   ```promql
   avg_over_time(
     rate(container_cpu_usage_seconds_total{...}[5m])
   [30d:5m])
   ```

4. **Aggregation at Query Time**: Use PromQL aggregation instead of fetching raw data
   ```promql
   # Good: Aggregate in PromQL
   avg_over_time(avg by (namespace, pod) (container_cpu_usage_seconds_total)[30d])

   # Bad: Fetch all data points, aggregate in Go
   container_cpu_usage_seconds_total{namespace="prod"}
   ```

5. **Batch Queries**: Query multiple workloads in one request using regex
   ```promql
   container_cpu_usage_seconds_total{pod=~"payment-.*|checkout-.*"}
   ```

### Key Queries

**CPU Usage by Workload** (30-day average):
```promql
avg_over_time(
  rate(
    container_cpu_usage_seconds_total{
      namespace="$NAMESPACE",
      pod=~"$WORKLOAD-.*"
    }[5m]
  )[30d:5m]
)
```

**Memory Usage by Workload** (30-day average):
```promql
avg_over_time(
  container_memory_working_set_bytes{
    namespace="$NAMESPACE",
    pod=~"$WORKLOAD-.*"
  }[30d:5m]
)
```

**Resource Requests** (from kube-state-metrics):
```promql
kube_pod_container_resource_requests{
  resource="cpu",
  namespace="$NAMESPACE",
  pod=~"$WORKLOAD-.*"
}
```

**Percentile Queries** (for node-footprint):
```promql
quantile_over_time(0.95,
  rate(
    container_cpu_usage_seconds_total{...}[5m]
  )[30d:5m]
)
```

---

## Bin-Packing Algorithm

### Overview

**Purpose**: Simulate whether a given workload can fit on a candidate node configuration.

**Algorithm**: First-Fit Decreasing (FFD)

**Why FFD?**
- Simple, deterministic, reproducible
- Well-studied algorithm with predictable behavior
- Fast: O(n log n) sort + O(n × m) placement (n pods, m nodes)
- Good approximation for real-world scheduling

### Algorithm Steps

```
1. SORT pods descending by "size"
   size = max(cpu_normalized, memory_normalized)
   cpu_normalized = cpu_cores / node_cpu_capacity
   memory_normalized = memory_gb / node_memory_capacity

2. INITIALIZE empty node pool

3. FOR EACH pod (largest to smallest):
   a. Try to fit on existing nodes (First-Fit):
      - Check both CPU and memory constraints
      - If fits: assign pod to node, update node remaining capacity

   b. If no fit:
      - Create new node
      - Assign pod to new node
      - Add node to pool

   c. Track remaining capacity per node

4. CALCULATE metrics:
   - Total nodes used
   - Average CPU utilization: sum(used_cpu) / sum(total_cpu)
   - Average memory utilization: sum(used_mem) / sum(total_mem)
   - Headroom: unused capacity percentage

5. CHECK feasibility:
   - Feasible if all pods fit
   - Infeasible if any pod exceeds single node capacity
   - Infeasible if peak workload exceeds total capacity with buffer
```

### Example

**Workload**:
- Pod A: 4 CPU, 8 GB
- Pod B: 2 CPU, 4 GB
- Pod C: 1 CPU, 2 GB
- Pod D: 0.5 CPU, 1 GB

**Node Type**: c5.xlarge (4 CPU, 8 GB allocatable)

**Execution**:

```
1. Sort by size descending:
   size_A = max(4/4, 8/8) = 1.0
   size_B = max(2/4, 4/8) = 0.5
   size_C = max(1/4, 2/8) = 0.25
   size_D = max(0.5/4, 1/8) = 0.125

   Order: A, B, C, D

2. Place Pod A (4 CPU, 8 GB):
   - No nodes exist → Create Node 1
   - Assign A to Node 1
   - Node 1 remaining: 0 CPU, 0 GB

3. Place Pod B (2 CPU, 4 GB):
   - Node 1 full → Create Node 2
   - Assign B to Node 2
   - Node 2 remaining: 2 CPU, 4 GB

4. Place Pod C (1 CPU, 2 GB):
   - Try Node 1: No space
   - Try Node 2: Fits!
   - Assign C to Node 2
   - Node 2 remaining: 1 CPU, 2 GB

5. Place Pod D (0.5 CPU, 1 GB):
   - Try Node 1: No space
   - Try Node 2: Fits!
   - Assign D to Node 2
   - Node 2 remaining: 0.5 CPU, 1 GB

Result:
- Nodes used: 2
- Node 1: 4/4 CPU, 8/8 GB (100% CPU, 100% MEM)
- Node 2: 3.5/4 CPU, 7/8 GB (87.5% CPU, 87.5% MEM)
- Avg CPU util: 93.75%
- Avg MEM util: 93.75%
- Headroom: low
```

### Limitations

**What FFD Doesn't Account For**:
- Pod affinity/anti-affinity rules
- Node taints and tolerations
- Topology spread constraints
- Pod priorities and preemption
- DaemonSets (consume resources on every node)
- System reserved resources (kubelet, OS)
- Network policies or storage constraints

**Why This Is Acceptable**:
- kubenow is for **cost estimation**, not production scheduling
- Claims are "This would have fit historically" not "This will work"
- Users are expected to validate with real testing
- Sufficient for identifying obvious over-provisioning

**Future Enhancements**:
- Support for pod affinity rules
- DaemonSet resource accounting
- System reserved resource modeling
- Multi-zone bin-packing

---

## Exit Code Strategy

kubenow v2.0 uses standardized exit codes for automation-friendly behavior.

| Code | Constant | Meaning | Use Case |
|------|----------|---------|----------|
| 0 | `ExitOK` | Success | Command completed successfully |
| 1 | `ExitPolicyFail` | Policy failure | (Reserved for future compliance mode) |
| 2 | `ExitInvalidInput` | Invalid input | Bad flags, validation errors, regex errors |
| 3 | `ExitRuntimeError` | Runtime error | API failures, network errors, timeouts |

**Examples**:

```bash
# Success
kubenow incident --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b
echo $?  # 0

# Invalid input (missing required flag)
kubenow incident --model mixtral:8x22b
echo $?  # 2

# Runtime error (Prometheus unreachable)
kubenow analyze requests-skew --prometheus-url http://invalid:9090
echo $?  # 3
```

**Automation Example**:

```bash
#!/bin/bash
kubenow analyze requests-skew --prometheus-url http://prometheus:9090 --export-file report.json

EXIT_CODE=$?
if [ $EXIT_CODE -eq 0 ]; then
  echo "Success"
  send_report report.json
elif [ $EXIT_CODE -eq 2 ]; then
  echo "Invalid input, check configuration"
  exit 1
elif [ $EXIT_CODE -eq 3 ]; then
  echo "Runtime error, check Prometheus connectivity"
  exit 1
fi
```

---

## Testing Architecture

### Unit Tests

**Coverage Target**: 70%+ for new code (v2.0 features)

**Patterns**:

1. **Table-Driven Tests** (Go best practice):
   ```go
   func TestBinPacking(t *testing.T) {
       tests := []struct {
           name     string
           pods     []PodRequirement
           nodeType NodeShape
           expected int  // expected node count
       }{
           {"empty", []PodRequirement{}, c5xlarge, 0},
           {"single pod fits", []PodRequirement{{CPU: 2, Mem: 4}}, c5xlarge, 1},
           // ...
       }

       for _, tt := range tests {
           t.Run(tt.name, func(t *testing.T) {
               result := BinPack(tt.pods, tt.nodeType)
               assert.Equal(t, tt.expected, result.NodeCount)
           })
       }
   }
   ```

2. **Mock External Dependencies**:
   ```go
   // metrics/mock.go
   type MockMetrics struct {
       GetNamespaceResourceUsageFunc func(...) (*NamespaceUsage, error)
   }

   func (m *MockMetrics) GetNamespaceResourceUsage(...) (*NamespaceUsage, error) {
       return m.GetNamespaceResourceUsageFunc(...)
   }

   // Test usage
   mock := &MockMetrics{
       GetNamespaceResourceUsageFunc: func(...) (*NamespaceUsage, error) {
           return &NamespaceUsage{AvgCPU: 2.5}, nil
       },
   }
   ```

3. **Fixtures for Complex Data**:
   ```
   test/fixtures/
   ├── prometheus_cpu_usage.json
   ├── prometheus_memory_usage.json
   └── k8s_pods.json
   ```

### Integration Tests

**Purpose**: Test full command execution with mocked external services.

**Structure**:
```
test/integration/
├── analyze_requests_skew_test.go
├── analyze_node_footprint_test.go
├── mock_prometheus_server.go
└── mock_k8s_client.go
```

**Example**:
```go
func TestRequestsSkewIntegration(t *testing.T) {
    // Start mock Prometheus server
    promServer := startMockPrometheusServer(t)
    defer promServer.Close()

    // Start mock K8s API server
    k8sServer := startMockK8sServer(t)
    defer k8sServer.Close()

    // Execute command
    cmd := exec.Command("kubenow", "analyze", "requests-skew",
        "--prometheus-url", promServer.URL,
        "--kubeconfig", k8sServer.KubeconfigPath,
        "--output", "json")

    output, err := cmd.CombinedOutput()
    require.NoError(t, err)

    // Verify output
    var result RequestsSkewResult
    err = json.Unmarshal(output, &result)
    require.NoError(t, err)
    assert.Len(t, result.Results, 10)
}
```

### CI/CD Pipeline

**GitHub Actions Workflow** (`.github/workflows/ci.yml`):

```yaml
jobs:
  test:
    strategy:
      matrix:
        go: ['1.21', '1.22', '1.23']
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ matrix.go }}
      - run: make test
      - run: make test-coverage
      - uses: codecov/codecov-action@v3

  lint:
    steps:
      - uses: actions/checkout@v4
      - uses: golangci/golangci-lint-action@v4

  build:
    strategy:
      matrix:
        os: [linux, darwin, windows]
        arch: [amd64, arm64]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - run: make build GOOS=${{ matrix.os }} GOARCH=${{ matrix.arch }}

  security:
    steps:
      - uses: actions/checkout@v4
      - uses: aquasecurity/trivy-action@master
        with:
          scan-type: 'fs'
          scan-ref: '.'
```

**Release Workflow** (`.github/workflows/release.yml`):
- Triggered on `v*` tags
- Uses GoReleaser for multi-platform builds
- Generates checksums
- Creates GitHub release with binaries attached

---

## Future Enhancements

### Planned Features

**Deterministic Analysis**:
1. **Historical Trend Tracking**
   - Track skew ratios over time
   - Detect worsening over-provisioning
   - Alert on significant changes

2. **Cloud Cost Integration**
   - AWS/GCP/Azure pricing APIs
   - Calculate actual dollar savings
   - Cost-per-namespace breakdown

3. **Recommendation Patches**
   - Generate kubectl apply-able YAML
   - Reduce resource requests automatically
   - Safe rollback on failure

4. **Auto-Detect Prometheus In-Cluster**
   - Currently requires explicit URL
   - Auto-discover common Prometheus installations
   - Support multiple Prometheus instances

5. **Pod Affinity Support in Bin-Packing**
   - Model affinity/anti-affinity rules
   - Topology spread constraints
   - More realistic simulations

**LLM Analysis**:
1. **Streaming Responses**
   - Real-time output for long analyses
   - Better UX for large clusters

2. **Multi-LLM Support**
   - Use different models per mode
   - Fallback chains (try GPT-4, fall back to Mixtral)

3. **Custom Prompt Templates**
   - User-defined prompt files
   - Domain-specific analysis modes

**General**:
1. **Web UI**
   - Dashboard for continuous monitoring
   - Historical report viewer
   - Interactive filtering

2. **API Server Mode**
   - Expose kubenow as HTTP API
   - Integrate with CI/CD pipelines
   - Webhook support for alerts

3. **Multi-Cluster Support**
   - Analyze multiple clusters in parallel
   - Cross-cluster cost comparison
   - Aggregated reports

---

## References

- **Bin-Packing Algorithms**: [Wikipedia - Bin packing problem](https://en.wikipedia.org/wiki/Bin_packing_problem)
- **Prometheus Query Best Practices**: [Prometheus Docs](https://prometheus.io/docs/practices/histograms/)
- **Cobra CLI Framework**: [spf13/cobra](https://github.com/spf13/cobra)
- **Keep a Changelog**: [keepachangelog.com](https://keepachangelog.com/)
- **Conventional Commits**: [conventionalcommits.org](https://www.conventionalcommits.org/)

---

**Architecture complete! Questions or suggestions? Open an issue on GitHub.**
