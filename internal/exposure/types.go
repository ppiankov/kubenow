// Package exposure queries Kubernetes APIs to build a structural
// topology of possible traffic paths to a workload.
package exposure

import "time"

// ExposureMap is the result of querying Kubernetes APIs for all
// possible traffic paths to a workload. It is structural — it
// shows what COULD send traffic, not what IS sending traffic.
type ExposureMap struct {
	Namespace    string
	WorkloadName string
	WorkloadKind string
	Services     []ServiceExposure
	Neighbors    []Neighbor
	QueryTime    time.Time
	Errors       []string // non-fatal errors during collection
}

// ServiceExposure represents a Service whose selector matches
// the workload's pod labels.
type ServiceExposure struct {
	Name      string
	Type      string // ClusterIP, NodePort, LoadBalancer, ExternalName, Headless
	Ports     []PortMapping
	Ingresses []IngressRoute
	NetPols   []NetPolRule
}

// PortMapping represents a single service port.
type PortMapping struct {
	Name       string
	TargetPort string
	Protocol   string
	Port       int32
}

// IngressRoute represents an Ingress rule routing to a service.
type IngressRoute struct {
	Name      string
	ClassName string
	Hosts     []string
	Paths     []string
	TLS       bool
}

// NetPolRule represents a NetworkPolicy ingress rule allowing
// traffic to the workload's pods.
type NetPolRule struct {
	PolicyName string
	Sources    []NetPolSource
}

// NetPolSource is a single allowed source in a NetworkPolicy.
type NetPolSource struct {
	Type      string // "namespace", "pod", "ipBlock", "all"
	Namespace string // for namespace selectors
	PodLabel  string // for pod selectors (stringified)
	CIDR      string // for IP blocks
}

// TrafficEdge represents a measured traffic connection between two workloads,
// as reported by Linkerd proxy metrics from Prometheus.
type TrafficEdge struct {
	Deployment  string  // remote deployment name
	Namespace   string  // remote namespace
	RPS         float64 // requests per second (averaged over query window)
	Total       float64 // total requests in query window
	SuccessRate float64 // 0.0-1.0, -1 if unknown
	LatencyP50  float64 // milliseconds, -1 if unknown
	LatencyP99  float64 // milliseconds, -1 if unknown
}

// TrafficMap holds bidirectional Linkerd traffic data for a workload.
type TrafficMap struct {
	Inbound  []TrafficEdge // who sends traffic TO this workload
	Outbound []TrafficEdge // who this workload sends traffic TO
	TCPIn    int64         // total inbound TCP connections (1h window)
	TCPOut   int64         // total outbound TCP connections (1h window)
	Window   time.Duration // query time window
}

// Neighbor is another workload in the same namespace, ranked by
// current CPU usage from the Metrics API.
type Neighbor struct {
	WorkloadName string
	WorkloadKind string // Deployment, StatefulSet, DaemonSet, or empty
	CPUMillis    int64
	MemoryMi     int64
	PodCount     int
}
