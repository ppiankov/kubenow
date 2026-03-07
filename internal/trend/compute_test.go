package trend

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeTrend_Empty(t *testing.T) {
	result := ComputeTrend(nil)
	assert.Equal(t, 0, result.Snapshots)
	assert.Empty(t, result.Workloads)
}

func TestComputeTrend_SingleSnapshot(t *testing.T) {
	history := []Snapshot{
		{
			Timestamp: time.Now(),
			Workloads: []WorkloadSnapshot{
				{Namespace: "prod", Workload: "nginx", SkewCPU: 2.5, SkewMem: 1.8},
			},
			TotalWaste: TotalWaste{CPU: 0.9, MemGi: 1.5},
		},
	}

	result := ComputeTrend(history)
	assert.Equal(t, 1, result.Snapshots)
	require.Len(t, result.Workloads, 1)
	assert.Equal(t, "nginx", result.Workloads[0].Workload)
	assert.Equal(t, 2.5, result.Workloads[0].CurrentCPU)
	assert.Equal(t, 0.0, result.Workloads[0].DeltaCPU) // no delta with single snapshot
	assert.Equal(t, "stable", result.Workloads[0].Direction)
}

func TestComputeTrend_Improving(t *testing.T) {
	history := []Snapshot{
		{
			Timestamp: time.Now().Add(-7 * 24 * time.Hour),
			Workloads: []WorkloadSnapshot{
				{Namespace: "prod", Workload: "nginx", SkewCPU: 3.0, SkewMem: 2.0},
			},
			TotalWaste: TotalWaste{CPU: 1.5, MemGi: 3.0},
		},
		{
			Timestamp: time.Now(),
			Workloads: []WorkloadSnapshot{
				{Namespace: "prod", Workload: "nginx", SkewCPU: 1.5, SkewMem: 1.2},
			},
			TotalWaste: TotalWaste{CPU: 0.5, MemGi: 1.0},
		},
	}

	result := ComputeTrend(history)
	require.Len(t, result.Workloads, 1)
	assert.Equal(t, "improving", result.Workloads[0].Direction)
	assert.InDelta(t, -1.5, result.Workloads[0].DeltaCPU, 0.001)
	assert.InDelta(t, -0.8, result.Workloads[0].DeltaMem, 0.001)
	assert.InDelta(t, -1.0, result.WasteDelta.DeltaCPU, 0.001)
}

func TestComputeTrend_Worsening(t *testing.T) {
	history := []Snapshot{
		{
			Timestamp: time.Now().Add(-7 * 24 * time.Hour),
			Workloads: []WorkloadSnapshot{
				{Namespace: "prod", Workload: "api", SkewCPU: 1.5, SkewMem: 1.2},
			},
			TotalWaste: TotalWaste{CPU: 0.5},
		},
		{
			Timestamp: time.Now(),
			Workloads: []WorkloadSnapshot{
				{Namespace: "prod", Workload: "api", SkewCPU: 3.0, SkewMem: 2.5},
			},
			TotalWaste: TotalWaste{CPU: 1.5},
		},
	}

	result := ComputeTrend(history)
	require.Len(t, result.Workloads, 1)
	assert.Equal(t, "worsening", result.Workloads[0].Direction)
}

func TestComputeTrend_NewWorkload(t *testing.T) {
	history := []Snapshot{
		{
			Timestamp: time.Now().Add(-7 * 24 * time.Hour),
			Workloads: []WorkloadSnapshot{
				{Namespace: "prod", Workload: "old", SkewCPU: 2.0},
			},
		},
		{
			Timestamp: time.Now(),
			Workloads: []WorkloadSnapshot{
				{Namespace: "prod", Workload: "old", SkewCPU: 2.0},
				{Namespace: "prod", Workload: "new", SkewCPU: 3.0, SkewMem: 2.0},
			},
		},
	}

	result := ComputeTrend(history)
	require.Len(t, result.Workloads, 2)

	// New workload has no delta (wasn't in oldest snapshot)
	newW := result.Workloads[1]
	assert.Equal(t, "new", newW.Workload)
	assert.Equal(t, 0.0, newW.DeltaCPU)
}

func TestClassifyDirection(t *testing.T) {
	tests := []struct {
		cpu, mem float64
		want     string
	}{
		{-0.5, -0.3, "improving"},
		{0.5, 0.3, "worsening"},
		{0.01, -0.01, "stable"},
		{0.0, 0.0, "stable"},
	}

	for _, tt := range tests {
		got := classifyDirection(tt.cpu, tt.mem)
		assert.Equal(t, tt.want, got, "cpu=%f mem=%f", tt.cpu, tt.mem)
	}
}
