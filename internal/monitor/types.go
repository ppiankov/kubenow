package monitor

import "time"

// Severity levels for problems
type Severity string

const (
	SeverityFatal    Severity = "FATAL"
	SeverityCritical Severity = "CRITICAL"
	SeverityWarning  Severity = "WARNING"
)

// Problem represents an active problem in the cluster
type Problem struct {
	Severity      Severity
	Type          string // OOMKilled, CrashLoop, ImagePull, etc.
	Namespace     string
	PodName       string
	ContainerName string
	Message       string
	Reason        string
	FirstSeen     time.Time
	LastSeen      time.Time
	Count         int
	Details       map[string]string
}

// RecentEvent represents a recent event in the cluster
type RecentEvent struct {
	Timestamp time.Time
	Severity  Severity
	Type      string
	Namespace string
	Resource  string
	Message   string
}

// ConnectionStatus represents the cluster connection state
type ConnectionStatus int

const (
	ConnectionUnknown     ConnectionStatus = iota // Not yet attempted
	ConnectionOK                                  // Successfully connected
	ConnectionUnreachable                         // Cluster unreachable
)

// ClusterStats holds cluster statistics
type ClusterStats struct {
	TotalPods      int
	RunningPods    int
	ProblemPods    int
	TotalNodes     int
	ReadyNodes     int
	NotReadyNodes  int
	EventsLast5Min int
	CriticalCount  int
	Connection     ConnectionStatus
	LastError      string // Last connection error message
}

// Config holds monitor configuration
type Config struct {
	Namespace      string
	SeverityFilter Severity
	Quiet          bool
	AlertSound     bool
}
