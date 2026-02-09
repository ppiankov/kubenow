package metrics

import (
	"context"
	"time"

	"github.com/prometheus/common/model"
)

// MetricsProvider defines the interface for querying metrics
type MetricsProvider interface {
	// QueryRange executes a range query over a time window
	QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (model.Matrix, error)

	// QueryInstant executes an instant query at a specific time
	QueryInstant(ctx context.Context, query string, ts time.Time) (model.Vector, error)

	// GetNamespaceResourceUsage retrieves CPU and memory usage for a namespace over a time window
	GetNamespaceResourceUsage(ctx context.Context, namespace string, window time.Duration) (*NamespaceUsage, error)

	// GetPodResourceUsage retrieves CPU and memory usage for pods matching a pattern
	GetPodResourceUsage(ctx context.Context, namespace, podPattern string, window time.Duration) ([]PodUsage, error)

	// GetWorkloadResourceUsage retrieves CPU and memory usage for a workload (Deployment, StatefulSet, etc.)
	GetWorkloadResourceUsage(ctx context.Context, namespace, workloadName, workloadType string, window time.Duration) (*WorkloadUsage, error)

	// HasNamespaceMetrics checks if Prometheus has any container metrics for a namespace
	HasNamespaceMetrics(ctx context.Context, namespace string) (bool, int, error)

	// GetClusterResourceUsage retrieves total cluster resource usage over a time window
	GetClusterResourceUsage(ctx context.Context, window time.Duration) (*ClusterUsage, error)

	// Health checks if the Prometheus endpoint is reachable
	Health(ctx context.Context) error
}

// NamespaceUsage contains resource usage metrics for a namespace
type NamespaceUsage struct {
	Namespace string

	// CPU metrics (cores)
	CPUAvg float64
	CPUP50 float64
	CPUP95 float64
	CPUP99 float64

	// Memory metrics (bytes)
	MemoryAvg float64
	MemoryP50 float64
	MemoryP95 float64
	MemoryP99 float64

	// Time window
	WindowStart time.Time
	WindowEnd   time.Time
}

// PodUsage contains resource usage metrics for a single pod
type PodUsage struct {
	PodName   string
	Namespace string

	// CPU metrics (cores)
	CPUAvg float64
	CPUP95 float64
	CPUP99 float64

	// Memory metrics (bytes)
	MemoryAvg float64
	MemoryP95 float64
	MemoryP99 float64

	// Runtime
	StartTime time.Time
	Runtime   time.Duration
}

// WorkloadUsage contains resource usage metrics for a workload
type WorkloadUsage struct {
	WorkloadName string
	WorkloadType string // Deployment, StatefulSet, DaemonSet, etc.
	Namespace    string

	// Aggregate metrics across all pods
	CPUAvg    float64
	CPUP95    float64
	CPUP99    float64
	CPUMax    float64
	MemoryAvg float64
	MemoryP95 float64
	MemoryP99 float64
	MemoryMax float64

	// Resource requests (from Kubernetes API)
	CPURequested    float64
	MemoryRequested float64 // bytes

	// Number of pods/replicas
	PodCount int

	// Skew ratios
	CPUSkew    float64 // requested / avg used
	MemorySkew float64 // requested / avg used
}

// ClusterUsage contains cluster-wide resource usage metrics
type ClusterUsage struct {
	// Total cluster capacity
	TotalCPU    float64 // cores
	TotalMemory float64 // bytes

	// Usage metrics
	CPUAvg    float64
	CPUP95    float64
	MemoryAvg float64
	MemoryP95 float64

	// Utilization percentages
	CPUUtilizationAvg float64
	CPUUtilizationP95 float64
	MemUtilizationAvg float64
	MemUtilizationP95 float64

	// Node count
	NodeCount int
}

// ConnectionMode defines how to connect to Prometheus
type ConnectionMode string

const (
	// ConnectionModeExplicit uses an explicit URL
	ConnectionModeExplicit ConnectionMode = "explicit"

	// ConnectionModeAutoDetect attempts to auto-discover Prometheus in the cluster
	ConnectionModeAutoDetect ConnectionMode = "auto-detect"

	// ConnectionModePortForward expects Prometheus on localhost (user port-forwards manually)
	ConnectionModePortForward ConnectionMode = "port-forward"
)

// Config holds configuration for metrics providers
type Config struct {
	// PrometheusURL is the Prometheus endpoint (e.g., http://prometheus:9090)
	PrometheusURL string

	// ConnectionMode determines how to connect to Prometheus
	ConnectionMode ConnectionMode

	// Timeout for queries
	Timeout time.Duration

	// Optional: Kubernetes clientset for auto-detection
	KubeClient interface{}
}
