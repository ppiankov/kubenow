package metrics

import (
	"context"
	"time"

	"github.com/prometheus/common/model"
)

// MockMetrics is a mock implementation of MetricsProvider for testing
type MockMetrics struct {
	// Fixture data
	NamespaceUsages map[string]*NamespaceUsage
	PodUsages       map[string][]PodUsage
	WorkloadUsages  map[string]*WorkloadUsage
	ClusterUsage    *ClusterUsage

	// Call tracking
	QueryRangeCalls   int
	QueryInstantCalls int
	HealthCalls       int

	// Error injection
	QueryRangeError   error
	QueryInstantError error
	HealthError       error
}

// NewMockMetrics creates a new mock metrics provider with default fixture data
func NewMockMetrics() *MockMetrics {
	return &MockMetrics{
		NamespaceUsages: make(map[string]*NamespaceUsage),
		PodUsages:       make(map[string][]PodUsage),
		WorkloadUsages:  make(map[string]*WorkloadUsage),
		ClusterUsage:    &ClusterUsage{},
	}
}

// QueryRange implements MetricsProvider
func (m *MockMetrics) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (model.Matrix, error) {
	m.QueryRangeCalls++
	if m.QueryRangeError != nil {
		return nil, m.QueryRangeError
	}

	// Return empty matrix for now
	return model.Matrix{}, nil
}

// QueryInstant implements MetricsProvider
func (m *MockMetrics) QueryInstant(ctx context.Context, query string, ts time.Time) (model.Vector, error) {
	m.QueryInstantCalls++
	if m.QueryInstantError != nil {
		return nil, m.QueryInstantError
	}

	// Return empty vector for now
	return model.Vector{}, nil
}

// GetNamespaceResourceUsage implements MetricsProvider
func (m *MockMetrics) GetNamespaceResourceUsage(ctx context.Context, namespace string, window time.Duration) (*NamespaceUsage, error) {
	if usage, exists := m.NamespaceUsages[namespace]; exists {
		return usage, nil
	}

	// Return default fixture
	return &NamespaceUsage{
		Namespace:   namespace,
		CPUAvg:      1.5,
		CPUP95:      2.0,
		CPUP99:      2.5,
		MemoryAvg:   2 * 1024 * 1024 * 1024, // 2Gi
		MemoryP95:   3 * 1024 * 1024 * 1024, // 3Gi
		MemoryP99:   4 * 1024 * 1024 * 1024, // 4Gi
		WindowStart: time.Now().Add(-window),
		WindowEnd:   time.Now(),
	}, nil
}

// GetPodResourceUsage implements MetricsProvider
func (m *MockMetrics) GetPodResourceUsage(ctx context.Context, namespace, podPattern string, window time.Duration) ([]PodUsage, error) {
	key := namespace + "/" + podPattern
	if usages, exists := m.PodUsages[key]; exists {
		return usages, nil
	}

	// Return default fixture
	return []PodUsage{
		{
			PodName:   "test-pod-1",
			Namespace: namespace,
			CPUAvg:    0.5,
			CPUP95:    0.8,
			CPUP99:    1.0,
			MemoryAvg: 512 * 1024 * 1024,  // 512Mi
			MemoryP95: 768 * 1024 * 1024,  // 768Mi
			MemoryP99: 1024 * 1024 * 1024, // 1Gi
			Runtime:   24 * time.Hour,
		},
	}, nil
}

// GetWorkloadResourceUsage implements MetricsProvider
func (m *MockMetrics) GetWorkloadResourceUsage(ctx context.Context, namespace, workloadName, workloadType string, window time.Duration) (*WorkloadUsage, error) {
	key := namespace + "/" + workloadName
	if usage, exists := m.WorkloadUsages[key]; exists {
		return usage, nil
	}

	// Return default fixture
	return &WorkloadUsage{
		WorkloadName:    workloadName,
		WorkloadType:    "Deployment",
		Namespace:       namespace,
		CPUAvg:          1.0,
		CPUP95:          1.5,
		MemoryAvg:       1 * 1024 * 1024 * 1024, // 1Gi
		MemoryP95:       2 * 1024 * 1024 * 1024, // 2Gi
		CPURequested:    4.0,
		MemoryRequested: 8 * 1024 * 1024 * 1024, // 8Gi
		CPULimit:        8.0,
		MemoryLimit:     16 * 1024 * 1024 * 1024, // 16Gi
		PodCount:        3,
		CPUSkew:         4.0, // 4.0 / 1.0
		MemorySkew:      8.0, // 8Gi / 1Gi
	}, nil
}

// GetClusterResourceUsage implements MetricsProvider
func (m *MockMetrics) GetClusterResourceUsage(ctx context.Context, window time.Duration) (*ClusterUsage, error) {
	if m.ClusterUsage.TotalCPU > 0 {
		return m.ClusterUsage, nil
	}

	// Return default fixture
	return &ClusterUsage{
		TotalCPU:          100.0,
		TotalMemory:       200 * 1024 * 1024 * 1024, // 200Gi
		CPUAvg:            42.0,
		CPUP95:            58.0,
		MemoryAvg:         80 * 1024 * 1024 * 1024,  // 80Gi
		MemoryP95:         120 * 1024 * 1024 * 1024, // 120Gi
		CPUUtilizationAvg: 42.0,
		CPUUtilizationP95: 58.0,
		MemUtilizationAvg: 40.0,
		MemUtilizationP95: 60.0,
		NodeCount:         25,
	}, nil
}

// HasNamespaceMetrics implements MetricsProvider
func (m *MockMetrics) HasNamespaceMetrics(_ context.Context, namespace string) (bool, int, error) {
	// If workload usages exist for this namespace, report true
	for key := range m.WorkloadUsages {
		if len(key) > len(namespace) && key[:len(namespace)+1] == namespace+"/" {
			return true, 1, nil
		}
	}
	return false, 0, nil
}

// Health implements MetricsProvider
func (m *MockMetrics) Health(ctx context.Context) error {
	m.HealthCalls++
	return m.HealthError
}

// AddNamespaceUsage adds fixture data for a namespace
func (m *MockMetrics) AddNamespaceUsage(namespace string, usage *NamespaceUsage) {
	m.NamespaceUsages[namespace] = usage
}

// AddPodUsages adds fixture data for pod usages
func (m *MockMetrics) AddPodUsages(namespace, podPattern string, usages []PodUsage) {
	key := namespace + "/" + podPattern
	m.PodUsages[key] = usages
}

// AddWorkloadUsage adds fixture data for a workload
func (m *MockMetrics) AddWorkloadUsage(namespace, workloadName string, usage *WorkloadUsage) {
	key := namespace + "/" + workloadName
	m.WorkloadUsages[key] = usage
}

// SetClusterUsage sets fixture data for cluster usage
func (m *MockMetrics) SetClusterUsage(usage *ClusterUsage) {
	m.ClusterUsage = usage
}

// Reset resets all call counters and errors
func (m *MockMetrics) Reset() {
	m.QueryRangeCalls = 0
	m.QueryInstantCalls = 0
	m.HealthCalls = 0
	m.QueryRangeError = nil
	m.QueryInstantError = nil
	m.HealthError = nil
}
