package analyzer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBinPacker_Pack_EmptyPods(t *testing.T) {
	template := NodeTemplate{
		InstanceType:   "test",
		CPUCapacity:    4.0,
		MemoryCapacity: 8 * 1024 * 1024 * 1024, // 8Gi
		SystemReserved: SystemReserved{
			CPU:    0.1,
			Memory: 512 * 1024 * 1024, // 512Mi
		},
	}

	packer := NewBinPacker(template)
	result := packer.Pack([]PodRequirement{})

	assert.Equal(t, 0, result.NodeCount)
	assert.True(t, result.Feasible)
	assert.Empty(t, result.Reasons)
}

func TestBinPacker_Pack_SinglePod(t *testing.T) {
	template := NodeTemplate{
		InstanceType:   "test",
		CPUCapacity:    4.0,
		MemoryCapacity: 8 * 1024 * 1024 * 1024, // 8Gi
		SystemReserved: SystemReserved{
			CPU:    0.1,
			Memory: 512 * 1024 * 1024, // 512Mi
		},
	}

	pods := []PodRequirement{
		{
			Name:   "test-pod",
			CPU:    1.0,
			Memory: 2 * 1024 * 1024 * 1024, // 2Gi
		},
	}

	packer := NewBinPacker(template)
	result := packer.Pack(pods)

	assert.Equal(t, 1, result.NodeCount)
	assert.True(t, result.Feasible)
	assert.Empty(t, result.Reasons)
	assert.Greater(t, result.AvgCPUUtil, 0.0)
	assert.Greater(t, result.AvgMemUtil, 0.0)
}

func TestBinPacker_Pack_MultiplePods_OneNode(t *testing.T) {
	template := NodeTemplate{
		InstanceType:   "test",
		CPUCapacity:    4.0,
		MemoryCapacity: 8 * 1024 * 1024 * 1024, // 8Gi
		SystemReserved: SystemReserved{
			CPU:    0.1,
			Memory: 512 * 1024 * 1024, // 512Mi
		},
	}

	pods := []PodRequirement{
		{Name: "pod-1", CPU: 0.5, Memory: 1 * 1024 * 1024 * 1024}, // 1Gi
		{Name: "pod-2", CPU: 0.5, Memory: 1 * 1024 * 1024 * 1024}, // 1Gi
		{Name: "pod-3", CPU: 0.5, Memory: 1 * 1024 * 1024 * 1024}, // 1Gi
	}

	packer := NewBinPacker(template)
	result := packer.Pack(pods)

	// Should fit all on one node
	assert.Equal(t, 1, result.NodeCount)
	assert.True(t, result.Feasible)
	assert.Len(t, result.Nodes[0].Pods, 3)
}

func TestBinPacker_Pack_MultiplePods_MultipleNodes(t *testing.T) {
	template := NodeTemplate{
		InstanceType:   "small",
		CPUCapacity:    2.0,
		MemoryCapacity: 4 * 1024 * 1024 * 1024, // 4Gi
		SystemReserved: SystemReserved{
			CPU:    0.1,
			Memory: 512 * 1024 * 1024, // 512Mi
		},
	}

	pods := []PodRequirement{
		{Name: "pod-1", CPU: 1.5, Memory: 2 * 1024 * 1024 * 1024}, // 2Gi
		{Name: "pod-2", CPU: 1.5, Memory: 2 * 1024 * 1024 * 1024}, // 2Gi
		{Name: "pod-3", CPU: 1.0, Memory: 1 * 1024 * 1024 * 1024}, // 1Gi
	}

	packer := NewBinPacker(template)
	result := packer.Pack(pods)

	// Should require 3 nodes (each pod is large relative to node capacity)
	assert.GreaterOrEqual(t, result.NodeCount, 2)
	assert.True(t, result.Feasible)
}

func TestBinPacker_Pack_PodExceedsNodeCapacity(t *testing.T) {
	template := NodeTemplate{
		InstanceType:   "small",
		CPUCapacity:    2.0,
		MemoryCapacity: 4 * 1024 * 1024 * 1024, // 4Gi
		SystemReserved: SystemReserved{
			CPU:    0.1,
			Memory: 512 * 1024 * 1024, // 512Mi
		},
	}

	pods := []PodRequirement{
		{Name: "huge-pod", CPU: 5.0, Memory: 10 * 1024 * 1024 * 1024}, // 10Gi - exceeds node
		{Name: "small-pod", CPU: 0.5, Memory: 1 * 1024 * 1024 * 1024}, // 1Gi
	}

	packer := NewBinPacker(template)
	result := packer.Pack(pods)

	assert.False(t, result.Feasible)
	assert.NotEmpty(t, result.Reasons)
	assert.Contains(t, result.Reasons[0], "huge-pod")
	assert.Contains(t, result.Reasons[0], "exceeds single node capacity")
}

func TestBinPacker_Pack_FirstFitDecreasing(t *testing.T) {
	template := NodeTemplate{
		InstanceType:   "test",
		CPUCapacity:    4.0,
		MemoryCapacity: 8 * 1024 * 1024 * 1024, // 8Gi
		SystemReserved: SystemReserved{
			CPU:    0.1,
			Memory: 512 * 1024 * 1024, // 512Mi
		},
	}

	// Pods in random order (not sorted)
	pods := []PodRequirement{
		{Name: "small-1", CPU: 0.2, Memory: 500 * 1024 * 1024},       // 500Mi
		{Name: "large-1", CPU: 2.0, Memory: 4 * 1024 * 1024 * 1024},  // 4Gi
		{Name: "medium-1", CPU: 1.0, Memory: 2 * 1024 * 1024 * 1024}, // 2Gi
		{Name: "small-2", CPU: 0.2, Memory: 500 * 1024 * 1024},       // 500Mi
	}

	packer := NewBinPacker(template)
	result := packer.Pack(pods)

	assert.True(t, result.Feasible)
	// Should use 2 nodes: large+medium on one, smalls can fit on another or with large
	assert.LessOrEqual(t, result.NodeCount, 2)
}

func TestBinPacker_CalculateUtilization(t *testing.T) {
	template := NodeTemplate{
		InstanceType:   "test",
		CPUCapacity:    4.0,
		MemoryCapacity: 8 * 1024 * 1024 * 1024, // 8Gi
		SystemReserved: SystemReserved{
			CPU:    0.1,
			Memory: 512 * 1024 * 1024, // 512Mi
		},
	}

	pods := []PodRequirement{
		{Name: "pod-1", CPU: 2.0, Memory: 4 * 1024 * 1024 * 1024}, // Uses ~50% of allocatable
	}

	packer := NewBinPacker(template)
	result := packer.Pack(pods)

	assert.True(t, result.Feasible)
	assert.Greater(t, result.AvgCPUUtil, 40.0) // Should be around 50%
	assert.Less(t, result.AvgCPUUtil, 60.0)
	assert.Greater(t, result.AvgMemUtil, 40.0)
	assert.Less(t, result.AvgMemUtil, 60.0)
}

func TestBinPacker_Headroom_Categories(t *testing.T) {
	template := NodeTemplate{
		InstanceType:   "test",
		CPUCapacity:    4.0,
		MemoryCapacity: 8 * 1024 * 1024 * 1024,
		SystemReserved: SystemReserved{CPU: 0.1, Memory: 512 * 1024 * 1024},
	}

	tests := []struct {
		name             string
		pods             []PodRequirement
		expectedHeadroom string
	}{
		{
			name: "high headroom",
			pods: []PodRequirement{
				{Name: "tiny", CPU: 0.5, Memory: 1 * 1024 * 1024 * 1024}, // ~13% CPU, ~13% mem
			},
			expectedHeadroom: "high", // < 50% avg utilization
		},
		{
			name: "medium headroom",
			pods: []PodRequirement{
				{Name: "medium", CPU: 2.0, Memory: 4 * 1024 * 1024 * 1024}, // ~51% CPU, ~53% mem
			},
			expectedHeadroom: "medium", // 50-70% avg utilization
		},
		{
			name: "low headroom",
			pods: []PodRequirement{
				{Name: "large", CPU: 3.0, Memory: 6 * 1024 * 1024 * 1024}, // ~77% CPU, ~80% mem
			},
			expectedHeadroom: "low", // 70-85% avg utilization
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packer := NewBinPacker(template)
			result := packer.Pack(tt.pods)

			assert.True(t, result.Feasible)
			assert.Equal(t, tt.expectedHeadroom, result.Headroom)
		})
	}
}

func TestGetNodeTemplates(t *testing.T) {
	templates := GetNodeTemplates()

	// Verify common node types exist
	assert.NotNil(t, templates["c5.xlarge"])
	assert.NotNil(t, templates["c5.2xlarge"])
	assert.NotNil(t, templates["r5.2xlarge"])
	assert.NotNil(t, templates["small"])
	assert.NotNil(t, templates["medium"])
	assert.NotNil(t, templates["large"])

	// Verify c5.xlarge template
	c5xl := templates["c5.xlarge"]
	assert.Equal(t, "c5.xlarge", c5xl.InstanceType)
	assert.Equal(t, 4.0, c5xl.CPUCapacity)
	assert.Equal(t, 8*1024*1024*1024, int(c5xl.MemoryCapacity)) // 8Gi

	// Verify system reserved resources are set
	assert.Greater(t, c5xl.SystemReserved.CPU, 0.0)
	assert.Greater(t, c5xl.SystemReserved.Memory, 0.0)
}
