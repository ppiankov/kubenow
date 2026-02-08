package models

import (
	"fmt"
	"time"
)

// ResourceUsage represents CPU and memory usage metrics
type ResourceUsage struct {
	// CPU metrics (cores)
	CPUAvg float64 `json:"cpu_avg"`
	CPUP50 float64 `json:"cpu_p50"`
	CPUP95 float64 `json:"cpu_p95"`
	CPUP99 float64 `json:"cpu_p99"`
	CPUMax float64 `json:"cpu_max"`

	// Memory metrics (bytes)
	MemoryAvg float64 `json:"memory_avg"`
	MemoryP50 float64 `json:"memory_p50"`
	MemoryP95 float64 `json:"memory_p95"`
	MemoryP99 float64 `json:"memory_p99"`
	MemoryMax float64 `json:"memory_max"`

	// Time window
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
}

// ResourceRequests represents Kubernetes resource requests
type ResourceRequests struct {
	CPU    float64 `json:"cpu"`    // cores
	Memory float64 `json:"memory"` // bytes
}

// ResourceLimits represents Kubernetes resource limits
type ResourceLimits struct {
	CPU    float64 `json:"cpu"`    // cores
	Memory float64 `json:"memory"` // bytes
}

// WorkloadMetrics combines usage, requests, and limits for a workload
type WorkloadMetrics struct {
	Namespace    string           `json:"namespace"`
	WorkloadName string           `json:"workload_name"`
	WorkloadType string           `json:"workload_type"` // Deployment, StatefulSet, etc.
	Usage        ResourceUsage    `json:"usage"`
	Requests     ResourceRequests `json:"requests"`
	Limits       ResourceLimits   `json:"limits"`
	PodCount     int              `json:"pod_count"`
	Runtime      time.Duration    `json:"runtime"` // How long workload has been running
}

// SkewMetrics represents over/under-provisioning metrics
type SkewMetrics struct {
	CPUSkew    float64 `json:"cpu_skew"`    // requested / avg used
	MemorySkew float64 `json:"memory_skew"` // requested / avg used

	// Impact score for prioritization
	ImpactScore float64 `json:"impact_score"`
}

// SafetyRating represents the safety level of a resource recommendation
type SafetyRating string

const (
	SafetyRatingSafe    SafetyRating = "SAFE"    // No issues detected, recommendation safe
	SafetyRatingCaution SafetyRating = "CAUTION" // Minor concerns, proceed carefully
	SafetyRatingRisky   SafetyRating = "RISKY"   // Significant concerns, needs review
	SafetyRatingUnsafe  SafetyRating = "UNSAFE"  // Critical issues, do not reduce resources
	SafetyRatingUnknown SafetyRating = "UNKNOWN" // Insufficient data to determine safety
)

// SafetyAnalysis contains spike detection and stability metrics
type SafetyAnalysis struct {
	// Failure indicators
	OOMKills             int    `json:"oom_kills"`              // OOM kills in time window
	Restarts             int    `json:"restarts"`               // Container restarts in time window
	CrashLoopBackOff     bool   `json:"crash_loop_backoff"`     // Currently in crash loop
	LastTerminatedReason string `json:"last_terminated_reason"` // Last termination reason

	// CPU metrics
	CPUThrottledSeconds float64 `json:"cpu_throttled_seconds"` // Total throttled time
	CPUThrottledPercent float64 `json:"cpu_throttled_percent"` // Percent of time throttled

	// Spike detection
	CPUP999          float64 `json:"cpu_p999"`           // 99.9th percentile CPU
	MemoryP999       float64 `json:"memory_p999"`        // 99.9th percentile memory
	CPUSpikeCount    int     `json:"cpu_spike_count"`    // Number of times usage > 2x avg
	MemorySpikeCount int     `json:"memory_spike_count"` // Number of times usage > 2x avg
	MaxCPUSpike      float64 `json:"max_cpu_spike"`      // Largest CPU spike (ratio)
	MaxMemorySpike   float64 `json:"max_memory_spike"`   // Largest memory spike (ratio)

	// Ultra-spike detection (sub-scrape-interval bursts)
	UltraSpikeLikely    bool     `json:"ultra_spike_likely"`    // Statistical detection of sub-second spikes
	UltraSpikeRatio     float64  `json:"ultra_spike_ratio"`     // max/p99 ratio (>3.0 suggests ultra-spikes)
	WorkloadPatternAI   bool     `json:"workload_pattern_ai"`   // Detected AI/inference workload pattern
	WorkloadPatternTags []string `json:"workload_pattern_tags"` // Tags like "llm", "rag", "inference"

	// Safety assessment
	Rating     SafetyRating `json:"rating"`      // Overall safety rating
	Warnings   []string     `json:"warnings"`    // Human-readable warnings
	Reasons    []string     `json:"reasons"`     // Reasons for rating
	SafeMargin float64      `json:"safe_margin"` // Recommended safety margin (e.g., 1.5x = 50% headroom)
}

// IsHealthy returns true if no critical issues are detected
func (sa *SafetyAnalysis) IsHealthy() bool {
	return sa.OOMKills == 0 && sa.Restarts == 0 && !sa.CrashLoopBackOff
}

// HasSpikes returns true if significant usage spikes are detected
func (sa *SafetyAnalysis) HasSpikes() bool {
	return sa.CPUSpikeCount > 0 || sa.MemorySpikeCount > 0
}

// DetermineRating calculates the safety rating based on collected metrics
func (sa *SafetyAnalysis) DetermineRating(cpuUsageP99, memUsageP99, cpuRequested, memRequested float64) {
	sa.Warnings = []string{}
	sa.Reasons = []string{}
	sa.SafeMargin = 1.0 // Default: no extra margin

	// Check for critical failures
	if sa.OOMKills > 0 {
		sa.Rating = SafetyRatingUnsafe
		sa.Warnings = append(sa.Warnings, fmt.Sprintf("⚠️ %d OOMKills in window", sa.OOMKills))
		sa.Reasons = append(sa.Reasons, "Recent OOM kills indicate memory pressure")
		sa.SafeMargin = 2.0 // Need 2x headroom
		return
	}

	if sa.CrashLoopBackOff {
		sa.Rating = SafetyRatingUnsafe
		sa.Warnings = append(sa.Warnings, "⚠️ Pod in CrashLoopBackOff")
		sa.Reasons = append(sa.Reasons, "Pod is unstable, do not reduce resources")
		sa.SafeMargin = 2.0
		return
	}

	// Check for high restart count
	if sa.Restarts > 5 {
		sa.Rating = SafetyRatingRisky
		sa.Warnings = append(sa.Warnings, fmt.Sprintf("⚠️ %d restarts in window", sa.Restarts))
		sa.Reasons = append(sa.Reasons, "High restart count suggests instability")
		sa.SafeMargin = 1.5
	} else if sa.Restarts > 0 {
		sa.Rating = SafetyRatingCaution
		sa.Warnings = append(sa.Warnings, fmt.Sprintf("⚠️ %d restarts in window", sa.Restarts))
		sa.Reasons = append(sa.Reasons, "Some restarts detected")
		sa.SafeMargin = 1.3
	}

	// Check for CPU throttling
	if sa.CPUThrottledPercent > 10.0 {
		if sa.Rating == SafetyRatingUnknown {
			sa.Rating = SafetyRatingRisky
		}
		sa.Warnings = append(sa.Warnings, fmt.Sprintf("⚠️ CPU throttled %.1f%% of time", sa.CPUThrottledPercent))
		sa.Reasons = append(sa.Reasons, "High CPU throttling detected")
		if sa.SafeMargin < 1.5 {
			sa.SafeMargin = 1.5
		}
	}

	// Check for dangerous spike patterns (p99 close to request limit)
	if cpuRequested > 0 && cpuUsageP99/cpuRequested > 0.9 {
		if sa.Rating == SafetyRatingUnknown {
			sa.Rating = SafetyRatingRisky
		}
		sa.Warnings = append(sa.Warnings, fmt.Sprintf("⚠️ p99 CPU usage at %.0f%% of request", (cpuUsageP99/cpuRequested)*100))
		sa.Reasons = append(sa.Reasons, "p99 usage very close to request limit")
		if sa.SafeMargin < 1.5 {
			sa.SafeMargin = 1.5
		}
	}

	if memRequested > 0 && memUsageP99/memRequested > 0.9 {
		if sa.Rating == SafetyRatingUnknown {
			sa.Rating = SafetyRatingRisky
		}
		sa.Warnings = append(sa.Warnings, fmt.Sprintf("⚠️ p99 memory usage at %.0f%% of request", (memUsageP99/memRequested)*100))
		sa.Reasons = append(sa.Reasons, "p99 memory usage very close to request limit")
		if sa.SafeMargin < 1.5 {
			sa.SafeMargin = 1.5
		}
	}

	// Check for high spike frequency
	if sa.CPUSpikeCount > 100 || sa.MemorySpikeCount > 100 {
		if sa.Rating == SafetyRatingUnknown || sa.Rating == SafetyRatingSafe {
			sa.Rating = SafetyRatingCaution
		}
		sa.Warnings = append(sa.Warnings, "⚠️ Frequent usage spikes detected")
		sa.Reasons = append(sa.Reasons, "Workload has bursty behavior")
		if sa.SafeMargin < 1.3 {
			sa.SafeMargin = 1.3
		}
	}

	// Check for ultra-fast spikes (sub-scrape-interval bursts)
	// These happen faster than Prometheus can capture (e.g., RAG queries, AI inference)
	if sa.UltraSpikeLikely {
		if sa.Rating == SafetyRatingUnknown || sa.Rating == SafetyRatingSafe {
			sa.Rating = SafetyRatingCaution
		}
		sa.Warnings = append(sa.Warnings, fmt.Sprintf("⚠️ Ultra-fast spikes detected (max/p99 ratio: %.1fx)", sa.UltraSpikeRatio))
		sa.Reasons = append(sa.Reasons, "Spikes occur faster than monitoring interval - actual bursts may exceed recorded peaks")
		// Extra safety margin for sub-scrape spikes
		if sa.SafeMargin < 2.5 {
			sa.SafeMargin = 2.5
		}
	}

	// Check for AI/inference workload patterns
	if sa.WorkloadPatternAI {
		if sa.Rating == SafetyRatingUnknown || sa.Rating == SafetyRatingSafe {
			sa.Rating = SafetyRatingCaution
		}
		tags := "unknown"
		if len(sa.WorkloadPatternTags) > 0 {
			tags = fmt.Sprintf("%v", sa.WorkloadPatternTags)
		}
		sa.Warnings = append(sa.Warnings, fmt.Sprintf("⚠️ AI/inference workload pattern detected: %s", tags))
		sa.Reasons = append(sa.Reasons, "AI workloads often have sub-second spikes not visible in metrics")
		if sa.SafeMargin < 2.0 {
			sa.SafeMargin = 2.0
		}
	}

	// If no issues found, mark as safe
	if sa.Rating == SafetyRatingUnknown || sa.Rating == "" {
		sa.Rating = SafetyRatingSafe
		sa.Reasons = append(sa.Reasons, "No stability issues detected")
	}
}

// DetectUltraSpikes analyzes statistical patterns to detect ultra-fast spikes
// that occur between Prometheus scrape intervals (typically 15-30s)
func (sa *SafetyAnalysis) DetectUltraSpikes(cpuAvg, cpuP95, cpuP99, cpuMax float64) {
	if cpuMax == 0 || cpuP99 == 0 || cpuP95 == 0 {
		return
	}

	// Calculate ratios
	maxToP99 := cpuMax / cpuP99
	p99ToP95 := cpuP99 / cpuP95

	sa.UltraSpikeRatio = maxToP99

	// Ultra-spike heuristic:
	// - max/p99 > 3.0 suggests very short, very high spikes
	// - p99/p95 > 1.5 suggests spike steepness
	// Combined: likely sub-scrape-interval bursts
	if maxToP99 > 3.0 && p99ToP95 > 1.5 {
		sa.UltraSpikeLikely = true
	} else if maxToP99 > 4.0 {
		// Very high max/p99 ratio alone is suspicious
		sa.UltraSpikeLikely = true
	}
}

// DetectAIWorkloadPattern checks container specs for AI/inference patterns
// These workloads typically have sub-second bursts (RAG, embeddings, inference)
func (sa *SafetyAnalysis) DetectAIWorkloadPattern(containerCommand []string, labels map[string]string, annotations map[string]string) {
	// AI/ML workload indicators
	aiPatterns := []string{
		"llm", "inference", "rag", "embedding", "transformer",
		"pytorch", "tensorflow", "huggingface", "openai",
		"langchain", "llamaindex", "vllm", "triton",
		"model-server", "bert", "gpt", "claude",
	}

	detectedTags := []string{}

	// Check container command/args
	commandStr := fmt.Sprintf("%v", containerCommand)
	commandLower := fmt.Sprintf("%s", commandStr)
	for _, pattern := range aiPatterns {
		if contains(commandLower, pattern) {
			detectedTags = append(detectedTags, pattern)
			sa.WorkloadPatternAI = true
		}
	}

	// Check labels
	for key, value := range labels {
		keyValue := fmt.Sprintf("%s=%s", key, value)
		keyValueLower := fmt.Sprintf("%s", keyValue)
		for _, pattern := range aiPatterns {
			if contains(keyValueLower, pattern) {
				if !containsString(detectedTags, pattern) {
					detectedTags = append(detectedTags, pattern)
				}
				sa.WorkloadPatternAI = true
			}
		}
	}

	// Check annotations
	for key, value := range annotations {
		keyValue := fmt.Sprintf("%s=%s", key, value)
		keyValueLower := fmt.Sprintf("%s", keyValue)
		for _, pattern := range aiPatterns {
			if contains(keyValueLower, pattern) {
				if !containsString(detectedTags, pattern) {
					detectedTags = append(detectedTags, pattern)
				}
				sa.WorkloadPatternAI = true
			}
		}
	}

	sa.WorkloadPatternTags = detectedTags
}

// Helper function to check if string contains substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func containsString(slice []string, str string) bool {
	for _, s := range slice {
		if s == str {
			return true
		}
	}
	return false
}

// NodeMetrics represents node-level metrics
type NodeMetrics struct {
	NodeName     string  `json:"node_name"`
	InstanceType string  `json:"instance_type"`
	CPUCapacity  float64 `json:"cpu_capacity"` // cores
	MemCapacity  float64 `json:"mem_capacity"` // bytes
	CPUUsageAvg  float64 `json:"cpu_usage_avg"`
	MemUsageAvg  float64 `json:"mem_usage_avg"`
}

// FormatMemoryBytes converts bytes to human-readable format (Gi, Mi, Ki)
func FormatMemoryBytes(bytes float64) string {
	const (
		Ki = 1024
		Mi = 1024 * Ki
		Gi = 1024 * Mi
	)

	if bytes >= Gi {
		return fmt.Sprintf("%.2fGi", bytes/Gi)
	} else if bytes >= Mi {
		return fmt.Sprintf("%.2fMi", bytes/Mi)
	} else if bytes >= Ki {
		return fmt.Sprintf("%.2fKi", bytes/Ki)
	}
	return fmt.Sprintf("%.0fB", bytes)
}

// ParseMemoryString converts human-readable memory to bytes
func ParseMemoryString(s string) (float64, error) {
	// Implementation would parse strings like "8Gi", "512Mi", etc.
	// For now, placeholder
	return 0, nil
}
