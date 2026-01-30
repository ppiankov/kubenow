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
}

// Config holds monitor configuration
type Config struct {
	Namespace      string
	SeverityFilter Severity
	Quiet          bool
	AlertSound     bool
}
