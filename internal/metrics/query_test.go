package metrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestQueryBuilder_CPUUsageByNamespace(t *testing.T) {
	qb := NewQueryBuilder()
	query := qb.CPUUsageByNamespace("production")

	assert.Contains(t, query, "namespace=\"production\"")
	assert.Contains(t, query, "container_cpu_usage_seconds_total")
	assert.Contains(t, query, "rate")
}

func TestQueryBuilder_MemoryUsageByPod(t *testing.T) {
	qb := NewQueryBuilder()
	query := qb.MemoryUsageByPod("production", "api-.*")

	assert.Contains(t, query, "namespace=\"production\"")
	assert.Contains(t, query, "pod=~\"api-.*\"")
	assert.Contains(t, query, "container_memory_working_set_bytes")
}

func TestQueryBuilder_WorkloadCPUUsage(t *testing.T) {
	qb := NewQueryBuilder()

	tests := []struct {
		name         string
		workloadType string
		expectedPod  string
	}{
		{
			name:         "Deployment",
			workloadType: "Deployment",
			expectedPod:  "payment-api-.*",
		},
		{
			name:         "StatefulSet",
			workloadType: "StatefulSet",
			expectedPod:  "payment-api-[0-9]+",
		},
		{
			name:         "DaemonSet",
			workloadType: "DaemonSet",
			expectedPod:  "payment-api-.*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			query := qb.WorkloadCPUUsage("production", "payment-api", tt.workloadType)
			assert.Contains(t, query, "namespace=\"production\"")
			assert.Contains(t, query, tt.expectedPod)
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{2 * time.Hour, "2h"},
		{7 * 24 * time.Hour, "7d"},
		{30 * 24 * time.Hour, "30d"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatDuration(tt.duration)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		hasError bool
	}{
		{"30s", 30 * time.Second, false},
		{"5m", 5 * time.Minute, false},
		{"2h", 2 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"1w", 7 * 24 * time.Hour, false},
		{"30d", 30 * 24 * time.Hour, false},
		{"invalid", 0, true},
		{"", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseDuration(tt.input)

			if tt.hasError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestQueryBuilder_CPUQuantileOverTime(t *testing.T) {
	qb := NewQueryBuilder()
	query := qb.CPUQuantileOverTime("production", 0.95, 30*24*time.Hour)

	assert.Contains(t, query, "quantile_over_time")
	assert.Contains(t, query, "0.95")
	assert.Contains(t, query, "namespace=\"production\"")
	assert.Contains(t, query, "[30d:]")
}

func TestQueryBuilder_NodeMetrics(t *testing.T) {
	qb := NewQueryBuilder()

	t.Run("NodeCPUCapacity", func(t *testing.T) {
		query := qb.NodeCPUCapacity()
		assert.Contains(t, query, "kube_node_status_capacity")
		assert.Contains(t, query, "resource=\"cpu\"")
	})

	t.Run("NodeMemoryCapacity", func(t *testing.T) {
		query := qb.NodeMemoryCapacity()
		assert.Contains(t, query, "kube_node_status_capacity")
		assert.Contains(t, query, "resource=\"memory\"")
	})

	t.Run("NodeCount", func(t *testing.T) {
		query := qb.NodeCount()
		assert.Contains(t, query, "kube_node_info")
		assert.Contains(t, query, "count")
	})
}
