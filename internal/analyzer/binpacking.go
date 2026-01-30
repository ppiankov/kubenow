package analyzer

import (
	"fmt"
	"sort"
)

// Node represents a node in the cluster with resource capacity
type Node struct {
	Name           string
	InstanceType   string
	CPUCapacity    float64 // cores
	MemoryCapacity float64 // bytes
	CPUAllocatable float64 // cores (capacity - system reserved)
	MemAllocatable float64 // bytes
	CPUUsed        float64 // currently allocated
	MemUsed        float64 // currently allocated
	Pods           []PodRequirement
}

// PodRequirement represents a pod's resource requirements
type PodRequirement struct {
	Name      string
	Namespace string
	CPU       float64 // cores
	Memory    float64 // bytes
	Priority  int     // For sorting
}

// BinPackingResult contains the result of a bin-packing simulation
type BinPackingResult struct {
	NodeCount  int
	Nodes      []Node
	Feasible   bool
	Reasons    []string
	AvgCPUUtil float64
	AvgMemUtil float64
	Headroom   string // high, medium, low, very low
}

// BinPacker performs First-Fit Decreasing bin packing
type BinPacker struct {
	nodeTemplate NodeTemplate
}

// NodeTemplate defines the shape of a node type
type NodeTemplate struct {
	InstanceType   string
	CPUCapacity    float64
	MemoryCapacity float64
	SystemReserved SystemReserved
}

// SystemReserved defines system-reserved resources
type SystemReserved struct {
	CPU    float64 // cores
	Memory float64 // bytes
}

// NewBinPacker creates a new bin packer
func NewBinPacker(template NodeTemplate) *BinPacker {
	return &BinPacker{
		nodeTemplate: template,
	}
}

// Pack attempts to pack pods into nodes using First-Fit Decreasing
func (bp *BinPacker) Pack(pods []PodRequirement) *BinPackingResult {
	// Sort pods by resource requirements (descending)
	// Use the larger of CPU or memory as the sort key
	sortedPods := make([]PodRequirement, len(pods))
	copy(sortedPods, pods)

	sort.Slice(sortedPods, func(i, j int) bool {
		// Normalize CPU and memory to comparable scale
		// CPU: 1 core = 1
		// Memory: 1Gi = 1
		sizeI := sortedPods[i].CPU + (sortedPods[i].Memory / (1024 * 1024 * 1024))
		sizeJ := sortedPods[j].CPU + (sortedPods[j].Memory / (1024 * 1024 * 1024))
		return sizeI > sizeJ
	})

	result := &BinPackingResult{
		Nodes:    make([]Node, 0),
		Feasible: true,
		Reasons:  make([]string, 0),
	}

	// Try to fit each pod
	for _, pod := range sortedPods {
		placed := false

		// Try to place on existing nodes (First-Fit)
		for i := range result.Nodes {
			if bp.canFit(&result.Nodes[i], pod) {
				bp.placePod(&result.Nodes[i], pod)
				placed = true
				break
			}
		}

		// If can't fit on existing nodes, create new node
		if !placed {
			// Check if pod can fit on a single node at all
			newNode := bp.createNode(len(result.Nodes))
			if !bp.canFit(&newNode, pod) {
				result.Feasible = false
				result.Reasons = append(result.Reasons,
					fmt.Sprintf("Pod %s/%s (CPU: %.2f, Memory: %.2fGi) exceeds single node capacity (CPU: %.2f, Memory: %.2fGi)",
						pod.Namespace, pod.Name, pod.CPU, pod.Memory/(1024*1024*1024),
						newNode.CPUAllocatable, newNode.MemAllocatable/(1024*1024*1024)))
				continue
			}

			bp.placePod(&newNode, pod)
			result.Nodes = append(result.Nodes, newNode)
		}
	}

	result.NodeCount = len(result.Nodes)

	// Calculate utilization
	if result.Feasible {
		bp.calculateUtilization(result)
	}

	return result
}

// canFit checks if a pod can fit on a node
func (bp *BinPacker) canFit(node *Node, pod PodRequirement) bool {
	remainingCPU := node.CPUAllocatable - node.CPUUsed
	remainingMem := node.MemAllocatable - node.MemUsed

	return pod.CPU <= remainingCPU && pod.Memory <= remainingMem
}

// placePod places a pod on a node
func (bp *BinPacker) placePod(node *Node, pod PodRequirement) {
	node.Pods = append(node.Pods, pod)
	node.CPUUsed += pod.CPU
	node.MemUsed += pod.Memory
}

// createNode creates a new node from the template
func (bp *BinPacker) createNode(index int) Node {
	return Node{
		Name:           fmt.Sprintf("node-%d", index),
		InstanceType:   bp.nodeTemplate.InstanceType,
		CPUCapacity:    bp.nodeTemplate.CPUCapacity,
		MemoryCapacity: bp.nodeTemplate.MemoryCapacity,
		CPUAllocatable: bp.nodeTemplate.CPUCapacity - bp.nodeTemplate.SystemReserved.CPU,
		MemAllocatable: bp.nodeTemplate.MemoryCapacity - bp.nodeTemplate.SystemReserved.Memory,
		CPUUsed:        0,
		MemUsed:        0,
		Pods:           make([]PodRequirement, 0),
	}
}

// calculateUtilization calculates average utilization and headroom
func (bp *BinPacker) calculateUtilization(result *BinPackingResult) {
	if len(result.Nodes) == 0 {
		return
	}

	totalCPUUtil := 0.0
	totalMemUtil := 0.0

	for _, node := range result.Nodes {
		cpuUtil := (node.CPUUsed / node.CPUAllocatable) * 100
		memUtil := (node.MemUsed / node.MemAllocatable) * 100

		totalCPUUtil += cpuUtil
		totalMemUtil += memUtil
	}

	result.AvgCPUUtil = totalCPUUtil / float64(len(result.Nodes))
	result.AvgMemUtil = totalMemUtil / float64(len(result.Nodes))

	// Determine headroom category
	avgUtil := (result.AvgCPUUtil + result.AvgMemUtil) / 2
	if avgUtil < 50 {
		result.Headroom = "high"
	} else if avgUtil < 70 {
		result.Headroom = "medium"
	} else if avgUtil < 85 {
		result.Headroom = "low"
	} else {
		result.Headroom = "very low"
	}
}

// GetNodeTemplates returns common AWS/GCP/Azure node types
func GetNodeTemplates() map[string]NodeTemplate {
	return map[string]NodeTemplate{
		// AWS EC2 instances
		"c5.large": {
			InstanceType:   "c5.large",
			CPUCapacity:    2.0,
			MemoryCapacity: 4 * 1024 * 1024 * 1024, // 4Gi
			SystemReserved: SystemReserved{
				CPU:    0.1,
				Memory: 512 * 1024 * 1024, // 512Mi
			},
		},
		"c5.xlarge": {
			InstanceType:   "c5.xlarge",
			CPUCapacity:    4.0,
			MemoryCapacity: 8 * 1024 * 1024 * 1024, // 8Gi
			SystemReserved: SystemReserved{
				CPU:    0.15,
				Memory: 768 * 1024 * 1024, // 768Mi
			},
		},
		"c5.2xlarge": {
			InstanceType:   "c5.2xlarge",
			CPUCapacity:    8.0,
			MemoryCapacity: 16 * 1024 * 1024 * 1024, // 16Gi
			SystemReserved: SystemReserved{
				CPU:    0.2,
				Memory: 1024 * 1024 * 1024, // 1Gi
			},
		},
		"c5.4xlarge": {
			InstanceType:   "c5.4xlarge",
			CPUCapacity:    16.0,
			MemoryCapacity: 32 * 1024 * 1024 * 1024, // 32Gi
			SystemReserved: SystemReserved{
				CPU:    0.3,
				Memory: 1536 * 1024 * 1024, // 1.5Gi
			},
		},
		"r5.2xlarge": {
			InstanceType:   "r5.2xlarge",
			CPUCapacity:    8.0,
			MemoryCapacity: 64 * 1024 * 1024 * 1024, // 64Gi (memory-optimized)
			SystemReserved: SystemReserved{
				CPU:    0.2,
				Memory: 2048 * 1024 * 1024, // 2Gi
			},
		},
		// Generic types for testing
		"small": {
			InstanceType:   "small",
			CPUCapacity:    2.0,
			MemoryCapacity: 4 * 1024 * 1024 * 1024,
			SystemReserved: SystemReserved{CPU: 0.1, Memory: 512 * 1024 * 1024},
		},
		"medium": {
			InstanceType:   "medium",
			CPUCapacity:    4.0,
			MemoryCapacity: 8 * 1024 * 1024 * 1024,
			SystemReserved: SystemReserved{CPU: 0.15, Memory: 768 * 1024 * 1024},
		},
		"large": {
			InstanceType:   "large",
			CPUCapacity:    8.0,
			MemoryCapacity: 16 * 1024 * 1024 * 1024,
			SystemReserved: SystemReserved{CPU: 0.2, Memory: 1024 * 1024 * 1024},
		},
	}
}
