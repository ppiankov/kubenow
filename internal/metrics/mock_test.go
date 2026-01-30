package metrics

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMockMetrics_GetNamespaceResourceUsage(t *testing.T) {
	mock := NewMockMetrics()
	ctx := context.Background()

	t.Run("DefaultFixture", func(t *testing.T) {
		usage, err := mock.GetNamespaceResourceUsage(ctx, "production", 30*24*time.Hour)
		assert.NoError(t, err)
		assert.NotNil(t, usage)
		assert.Equal(t, "production", usage.Namespace)
		assert.Greater(t, usage.CPUAvg, 0.0)
		assert.Greater(t, usage.MemoryAvg, 0.0)
	})

	t.Run("CustomFixture", func(t *testing.T) {
		customUsage := &NamespaceUsage{
			Namespace: "staging",
			CPUAvg:    10.5,
			CPUP95:    15.0,
			MemoryAvg: 20 * 1024 * 1024 * 1024, // 20Gi
		}
		mock.AddNamespaceUsage("staging", customUsage)

		usage, err := mock.GetNamespaceResourceUsage(ctx, "staging", 7*24*time.Hour)
		assert.NoError(t, err)
		assert.Equal(t, 10.5, usage.CPUAvg)
		assert.Equal(t, 15.0, usage.CPUP95)
	})
}

func TestMockMetrics_GetPodResourceUsage(t *testing.T) {
	mock := NewMockMetrics()
	ctx := context.Background()

	t.Run("DefaultFixture", func(t *testing.T) {
		usages, err := mock.GetPodResourceUsage(ctx, "production", "api-.*", 24*time.Hour)
		assert.NoError(t, err)
		assert.Len(t, usages, 1)
		assert.Equal(t, "test-pod-1", usages[0].PodName)
	})

	t.Run("CustomFixture", func(t *testing.T) {
		customUsages := []PodUsage{
			{
				PodName:   "payment-api-abc123",
				Namespace: "production",
				CPUAvg:    2.5,
				MemoryAvg: 4 * 1024 * 1024 * 1024, // 4Gi
			},
			{
				PodName:   "payment-api-def456",
				Namespace: "production",
				CPUAvg:    3.0,
				MemoryAvg: 5 * 1024 * 1024 * 1024, // 5Gi
			},
		}
		mock.AddPodUsages("production", "payment-api-.*", customUsages)

		usages, err := mock.GetPodResourceUsage(ctx, "production", "payment-api-.*", 24*time.Hour)
		assert.NoError(t, err)
		assert.Len(t, usages, 2)
		assert.Equal(t, "payment-api-abc123", usages[0].PodName)
		assert.Equal(t, 2.5, usages[0].CPUAvg)
	})
}

func TestMockMetrics_GetWorkloadResourceUsage(t *testing.T) {
	mock := NewMockMetrics()
	ctx := context.Background()

	usage, err := mock.GetWorkloadResourceUsage(ctx, "production", "payment-api", 30*24*time.Hour)
	assert.NoError(t, err)
	assert.NotNil(t, usage)
	assert.Equal(t, "payment-api", usage.WorkloadName)
	assert.Equal(t, "Deployment", usage.WorkloadType)
	assert.Equal(t, 4.0, usage.CPUSkew)
	assert.Equal(t, 8.0, usage.MemorySkew)
}

func TestMockMetrics_GetClusterResourceUsage(t *testing.T) {
	mock := NewMockMetrics()
	ctx := context.Background()

	t.Run("DefaultFixture", func(t *testing.T) {
		usage, err := mock.GetClusterResourceUsage(ctx, 7*24*time.Hour)
		assert.NoError(t, err)
		assert.NotNil(t, usage)
		assert.Equal(t, 100.0, usage.TotalCPU)
		assert.Equal(t, 25, usage.NodeCount)
		assert.Greater(t, usage.CPUUtilizationAvg, 0.0)
	})

	t.Run("CustomFixture", func(t *testing.T) {
		customUsage := &ClusterUsage{
			TotalCPU:          200.0,
			TotalMemory:       400 * 1024 * 1024 * 1024, // 400Gi
			CPUAvg:            80.0,
			NodeCount:         50,
			CPUUtilizationAvg: 40.0,
		}
		mock.SetClusterUsage(customUsage)

		usage, err := mock.GetClusterResourceUsage(ctx, 7*24*time.Hour)
		assert.NoError(t, err)
		assert.Equal(t, 200.0, usage.TotalCPU)
		assert.Equal(t, 50, usage.NodeCount)
	})
}

func TestMockMetrics_Health(t *testing.T) {
	mock := NewMockMetrics()
	ctx := context.Background()

	t.Run("Success", func(t *testing.T) {
		err := mock.Health(ctx)
		assert.NoError(t, err)
		assert.Equal(t, 1, mock.HealthCalls)
	})

	t.Run("ErrorInjection", func(t *testing.T) {
		mock.Reset()
		mock.HealthError = fmt.Errorf("connection refused")

		err := mock.Health(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "connection refused")
		assert.Equal(t, 1, mock.HealthCalls)
	})
}

func TestMockMetrics_CallTracking(t *testing.T) {
	mock := NewMockMetrics()
	ctx := context.Background()

	// Make several calls
	mock.QueryRange(ctx, "test", time.Now(), time.Now(), time.Minute)
	mock.QueryRange(ctx, "test", time.Now(), time.Now(), time.Minute)
	mock.QueryInstant(ctx, "test", time.Now())
	mock.Health(ctx)

	assert.Equal(t, 2, mock.QueryRangeCalls)
	assert.Equal(t, 1, mock.QueryInstantCalls)
	assert.Equal(t, 1, mock.HealthCalls)

	// Reset and verify
	mock.Reset()
	assert.Equal(t, 0, mock.QueryRangeCalls)
	assert.Equal(t, 0, mock.QueryInstantCalls)
	assert.Equal(t, 0, mock.HealthCalls)
}
