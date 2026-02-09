package metrics

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// PrometheusClient implements MetricsProvider for Prometheus
type PrometheusClient struct {
	api     v1.API
	config  Config
	builder *QueryBuilder
}

// NewPrometheusClient creates a new Prometheus client
func NewPrometheusClient(config Config) (*PrometheusClient, error) {
	if config.PrometheusURL == "" {
		return nil, fmt.Errorf("prometheus URL is required")
	}

	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	client, err := api.NewClient(api.Config{
		Address: config.PrometheusURL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus client: %w", err)
	}

	return &PrometheusClient{
		api:     v1.NewAPI(client),
		config:  config,
		builder: NewQueryBuilder(),
	}, nil
}

// GetAPI returns the underlying Prometheus API client
func (p *PrometheusClient) GetAPI() v1.API {
	return p.api
}

// QueryRange executes a range query
func (p *PrometheusClient) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (model.Matrix, error) {
	r := v1.Range{
		Start: start,
		End:   end,
		Step:  step,
	}

	result, warnings, err := p.api.QueryRange(ctx, query, r)
	if err != nil {
		return nil, fmt.Errorf("query range failed: %w", err)
	}

	if len(warnings) > 0 {
		// Log warnings but don't fail
		fmt.Printf("Prometheus warnings: %v\n", warnings)
	}

	matrix, ok := result.(model.Matrix)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", result)
	}

	return matrix, nil
}

// QueryInstant executes an instant query
func (p *PrometheusClient) QueryInstant(ctx context.Context, query string, ts time.Time) (model.Vector, error) {
	result, warnings, err := p.api.Query(ctx, query, ts)
	if err != nil {
		return nil, fmt.Errorf("instant query failed: %w", err)
	}

	if len(warnings) > 0 {
		fmt.Printf("Prometheus warnings: %v\n", warnings)
	}

	vector, ok := result.(model.Vector)
	if !ok {
		return nil, fmt.Errorf("unexpected result type: %T", result)
	}

	return vector, nil
}

// GetNamespaceResourceUsage retrieves CPU and memory usage for a namespace
func (p *PrometheusClient) GetNamespaceResourceUsage(ctx context.Context, namespace string, window time.Duration) (*NamespaceUsage, error) {
	end := time.Now()
	start := end.Add(-window)

	usage := &NamespaceUsage{
		Namespace:   namespace,
		WindowStart: start,
		WindowEnd:   end,
	}

	// Query CPU average
	cpuAvgQuery := p.builder.CPUAvgOverTime(namespace, window)
	cpuAvgResult, err := p.QueryInstant(ctx, cpuAvgQuery, end)
	if err == nil && len(cpuAvgResult) > 0 {
		usage.CPUAvg = float64(cpuAvgResult[0].Value)
	}

	// Query CPU p95
	cpuP95Query := p.builder.CPUQuantileOverTime(namespace, 0.95, window)
	cpuP95Result, err := p.QueryInstant(ctx, cpuP95Query, end)
	if err == nil && len(cpuP95Result) > 0 {
		usage.CPUP95 = float64(cpuP95Result[0].Value)
	}

	// Query CPU p99
	cpuP99Query := p.builder.CPUQuantileOverTime(namespace, 0.99, window)
	cpuP99Result, err := p.QueryInstant(ctx, cpuP99Query, end)
	if err == nil && len(cpuP99Result) > 0 {
		usage.CPUP99 = float64(cpuP99Result[0].Value)
	}

	// Query memory average
	memAvgQuery := p.builder.MemoryAvgOverTime(namespace, window)
	memAvgResult, err := p.QueryInstant(ctx, memAvgQuery, end)
	if err == nil && len(memAvgResult) > 0 {
		usage.MemoryAvg = float64(memAvgResult[0].Value)
	}

	// Query memory p95
	memP95Query := p.builder.MemoryQuantileOverTime(namespace, 0.95, window)
	memP95Result, err := p.QueryInstant(ctx, memP95Query, end)
	if err == nil && len(memP95Result) > 0 {
		usage.MemoryP95 = float64(memP95Result[0].Value)
	}

	// Query memory p99
	memP99Query := p.builder.MemoryQuantileOverTime(namespace, 0.99, window)
	memP99Result, err := p.QueryInstant(ctx, memP99Query, end)
	if err == nil && len(memP99Result) > 0 {
		usage.MemoryP99 = float64(memP99Result[0].Value)
	}

	return usage, nil
}

// GetPodResourceUsage retrieves CPU and memory usage for pods matching a pattern
func (p *PrometheusClient) GetPodResourceUsage(ctx context.Context, namespace, podPattern string, window time.Duration) ([]PodUsage, error) {
	end := time.Now()
	start := end.Add(-window)

	// Query CPU by pod
	cpuQuery := p.builder.CPUUsageByPod(namespace, podPattern)
	cpuMatrix, err := p.QueryRange(ctx, cpuQuery, start, end, time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed to query CPU by pod: %w", err)
	}

	// Query memory by pod
	memQuery := p.builder.MemoryUsageByPod(namespace, podPattern)
	memMatrix, err := p.QueryRange(ctx, memQuery, start, end, time.Minute)
	if err != nil {
		return nil, fmt.Errorf("failed to query memory by pod: %w", err)
	}

	// Aggregate results by pod
	podMap := make(map[string]*PodUsage)

	for _, stream := range cpuMatrix {
		podName := string(stream.Metric["pod"])
		if podName == "" {
			continue
		}

		if _, exists := podMap[podName]; !exists {
			podMap[podName] = &PodUsage{
				PodName:   podName,
				Namespace: namespace,
			}
		}

		// Calculate average, p95, p99
		podMap[podName].CPUAvg = calculateAverage(stream.Values)
		podMap[podName].CPUP95 = calculatePercentile(stream.Values, 0.95)
		podMap[podName].CPUP99 = calculatePercentile(stream.Values, 0.99)
	}

	for _, stream := range memMatrix {
		podName := string(stream.Metric["pod"])
		if podName == "" {
			continue
		}

		if _, exists := podMap[podName]; !exists {
			podMap[podName] = &PodUsage{
				PodName:   podName,
				Namespace: namespace,
			}
		}

		podMap[podName].MemoryAvg = calculateAverage(stream.Values)
		podMap[podName].MemoryP95 = calculatePercentile(stream.Values, 0.95)
		podMap[podName].MemoryP99 = calculatePercentile(stream.Values, 0.99)
	}

	// Convert map to slice
	result := make([]PodUsage, 0, len(podMap))
	for _, usage := range podMap {
		result = append(result, *usage)
	}

	return result, nil
}

// GetWorkloadResourceUsage retrieves CPU and memory usage for a workload
func (p *PrometheusClient) GetWorkloadResourceUsage(ctx context.Context, namespace, workloadName, workloadType string, window time.Duration) (*WorkloadUsage, error) {
	end := time.Now()
	start := end.Add(-window)

	usage := &WorkloadUsage{
		WorkloadName: workloadName,
		Namespace:    namespace,
		WorkloadType: workloadType,
	}

	// Query workload CPU
	cpuQuery := p.builder.WorkloadCPUUsage(namespace, workloadName, workloadType)
	cpuMatrix, err := p.QueryRange(ctx, cpuQuery, start, end, time.Minute)
	if err == nil && len(cpuMatrix) > 0 {
		usage.CPUAvg = calculateAverage(cpuMatrix[0].Values)
		usage.CPUP95 = calculatePercentile(cpuMatrix[0].Values, 0.95)
		usage.CPUP99 = calculatePercentile(cpuMatrix[0].Values, 0.99)
		usage.CPUMax = calculateMax(cpuMatrix[0].Values)
	}

	// Query workload memory
	memQuery := p.builder.WorkloadMemoryUsage(namespace, workloadName, workloadType)
	memMatrix, err := p.QueryRange(ctx, memQuery, start, end, time.Minute)
	if err == nil && len(memMatrix) > 0 {
		usage.MemoryAvg = calculateAverage(memMatrix[0].Values)
		usage.MemoryP95 = calculatePercentile(memMatrix[0].Values, 0.95)
		usage.MemoryP99 = calculatePercentile(memMatrix[0].Values, 0.99)
		usage.MemoryMax = calculateMax(memMatrix[0].Values)
	}

	// Query resource requests (from kube-state-metrics)
	cpuReqQuery := p.builder.CPURequestsByPod(namespace, workloadName+"-.*")
	cpuReqResult, err := p.QueryInstant(ctx, cpuReqQuery, end)
	if err == nil && len(cpuReqResult) > 0 {
		usage.CPURequested = float64(cpuReqResult[0].Value)
	}

	memReqQuery := p.builder.MemoryRequestsByPod(namespace, workloadName+"-.*")
	memReqResult, err := p.QueryInstant(ctx, memReqQuery, end)
	if err == nil && len(memReqResult) > 0 {
		usage.MemoryRequested = float64(memReqResult[0].Value)
	}

	// Calculate skew
	if usage.CPUAvg > 0 {
		usage.CPUSkew = usage.CPURequested / usage.CPUAvg
	}
	if usage.MemoryAvg > 0 {
		usage.MemorySkew = usage.MemoryRequested / usage.MemoryAvg
	}

	return usage, nil
}

// GetClusterResourceUsage retrieves cluster-wide resource usage
func (p *PrometheusClient) GetClusterResourceUsage(ctx context.Context, window time.Duration) (*ClusterUsage, error) {
	end := time.Now()

	usage := &ClusterUsage{}

	// Query total CPU capacity
	cpuCapQuery := p.builder.NodeCPUCapacity()
	cpuCapResult, err := p.QueryInstant(ctx, cpuCapQuery, end)
	if err == nil && len(cpuCapResult) > 0 {
		usage.TotalCPU = float64(cpuCapResult[0].Value)
	}

	// Query total memory capacity
	memCapQuery := p.builder.NodeMemoryCapacity()
	memCapResult, err := p.QueryInstant(ctx, memCapQuery, end)
	if err == nil && len(memCapResult) > 0 {
		usage.TotalMemory = float64(memCapResult[0].Value)
	}

	// Query node count
	nodeCountQuery := p.builder.NodeCount()
	nodeCountResult, err := p.QueryInstant(ctx, nodeCountQuery, end)
	if err == nil && len(nodeCountResult) > 0 {
		usage.NodeCount = int(nodeCountResult[0].Value)
	}

	// Query cluster-wide CPU usage (all namespaces)
	clusterCPUQuery := `sum(rate(container_cpu_usage_seconds_total{container!="",container!="POD"}[5m]))`
	cpuMatrix, err := p.QueryRange(ctx, clusterCPUQuery, end.Add(-window), end, time.Minute)
	if err == nil && len(cpuMatrix) > 0 {
		usage.CPUAvg = calculateAverage(cpuMatrix[0].Values)
		usage.CPUP95 = calculatePercentile(cpuMatrix[0].Values, 0.95)
	}

	// Query cluster-wide memory usage
	clusterMemQuery := `sum(container_memory_working_set_bytes{container!="",container!="POD"})`
	memMatrix, err := p.QueryRange(ctx, clusterMemQuery, end.Add(-window), end, time.Minute)
	if err == nil && len(memMatrix) > 0 {
		usage.MemoryAvg = calculateAverage(memMatrix[0].Values)
		usage.MemoryP95 = calculatePercentile(memMatrix[0].Values, 0.95)
	}

	// Calculate utilization percentages
	if usage.TotalCPU > 0 {
		usage.CPUUtilizationAvg = (usage.CPUAvg / usage.TotalCPU) * 100
		usage.CPUUtilizationP95 = (usage.CPUP95 / usage.TotalCPU) * 100
	}
	if usage.TotalMemory > 0 {
		usage.MemUtilizationAvg = (usage.MemoryAvg / usage.TotalMemory) * 100
		usage.MemUtilizationP95 = (usage.MemoryP95 / usage.TotalMemory) * 100
	}

	return usage, nil
}

// Health checks if the Prometheus endpoint is reachable
func (p *PrometheusClient) Health(ctx context.Context) error {
	// Simple health check: try to query runtime info
	_, err := p.api.Runtimeinfo(ctx)
	if err != nil {
		return fmt.Errorf("prometheus health check failed: %w", err)
	}
	return nil
}

// HasNamespaceMetrics checks if Prometheus has any container CPU metrics for a namespace.
// Returns (hasMetrics, seriesCount, error).
func (p *PrometheusClient) HasNamespaceMetrics(ctx context.Context, namespace string) (bool, int, error) {
	query := fmt.Sprintf(`count(container_cpu_usage_seconds_total{namespace="%s",container!="",container!="POD"})`, namespace)
	result, err := p.QueryInstant(ctx, query, time.Now())
	if err != nil {
		return false, 0, err
	}
	if len(result) == 0 {
		return false, 0, nil
	}
	count := int(result[0].Value)
	return count > 0, count, nil
}

// calculateAverage computes the average of a series of values
func calculateAverage(values []model.SamplePair) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += float64(v.Value)
	}
	return sum / float64(len(values))
}

// calculatePercentile computes the specified percentile of a series of values
func calculatePercentile(values []model.SamplePair, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}

	// Extract float values
	floats := make([]float64, len(values))
	for i, v := range values {
		floats[i] = float64(v.Value)
	}

	// Sort
	sort.Float64s(floats)

	// Calculate percentile index
	index := int(float64(len(floats)) * percentile)
	if index >= len(floats) {
		index = len(floats) - 1
	}

	return floats[index]
}

// calculateMax returns the maximum value from a sample pair slice
func calculateMax(values []model.SamplePair) float64 {
	if len(values) == 0 {
		return 0
	}

	max := float64(values[0].Value)
	for _, v := range values {
		if float64(v.Value) > max {
			max = float64(v.Value)
		}
	}

	return max
}

// GetWorkloadSafetyData retrieves safety-related metrics for a workload
func (p *PrometheusClient) GetWorkloadSafetyData(ctx context.Context, namespace, workloadName, workloadType string, window time.Duration) (map[string]float64, error) {
	end := time.Now()

	results := make(map[string]float64)

	// Query for restarts
	restartsQuery := p.builder.RestartsByWorkload(namespace, workloadName, window)
	restartsVec, err := p.QueryInstant(ctx, restartsQuery, end)
	if err == nil && len(restartsVec) > 0 {
		results["restarts"] = float64(restartsVec[0].Value)
	} else {
		results["restarts"] = 0
	}

	// Query for CPU throttling percentage
	throttleQuery := p.builder.CPUThrottledPercentByWorkload(namespace, workloadName, window)
	throttleVec, err := p.QueryInstant(ctx, throttleQuery, end)
	if err == nil && len(throttleVec) > 0 {
		results["cpu_throttled_percent"] = float64(throttleVec[0].Value)
	} else {
		results["cpu_throttled_percent"] = 0
	}

	// Query for CPU throttling seconds
	throttleSecondsQuery := p.builder.CPUThrottledByWorkload(namespace, workloadName, window)
	throttleSecondsVec, err := p.QueryInstant(ctx, throttleSecondsQuery, end)
	if err == nil && len(throttleSecondsVec) > 0 {
		results["cpu_throttled_seconds"] = float64(throttleSecondsVec[0].Value)
	} else {
		results["cpu_throttled_seconds"] = 0
	}

	// Query for p99.9 CPU
	p999CPUQuery := p.builder.CPUP999ByWorkload(namespace, workloadName, workloadType, window)
	p999CPUVec, err := p.QueryInstant(ctx, p999CPUQuery, end)
	if err == nil && len(p999CPUVec) > 0 {
		results["cpu_p999"] = float64(p999CPUVec[0].Value)
	} else {
		results["cpu_p999"] = 0
	}

	// Query for p99.9 memory
	p999MemQuery := p.builder.MemoryP999ByWorkload(namespace, workloadName, workloadType, window)
	p999MemVec, err := p.QueryInstant(ctx, p999MemQuery, end)
	if err == nil && len(p999MemVec) > 0 {
		results["memory_p999"] = float64(p999MemVec[0].Value)
	} else {
		results["memory_p999"] = 0
	}

	// Query for max CPU
	maxCPUQuery := p.builder.MaxCPUUsageByWorkload(namespace, workloadName, workloadType, window)
	maxCPUVec, err := p.QueryInstant(ctx, maxCPUQuery, end)
	if err == nil && len(maxCPUVec) > 0 {
		results["cpu_max"] = float64(maxCPUVec[0].Value)
	} else {
		results["cpu_max"] = 0
	}

	// Query for max memory
	maxMemQuery := p.builder.MaxMemoryUsageByWorkload(namespace, workloadName, workloadType, window)
	maxMemVec, err := p.QueryInstant(ctx, maxMemQuery, end)
	if err == nil && len(maxMemVec) > 0 {
		results["memory_max"] = float64(maxMemVec[0].Value)
	} else {
		results["memory_max"] = 0
	}

	return results, nil
}
