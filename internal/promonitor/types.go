package promonitor

import (
	"time"

	"github.com/ppiankov/kubenow/internal/metrics"
)

// SafetyRating is a deterministic classification based on OOMKills, restarts,
// throttling, and spike patterns.
type SafetyRating string

const (
	SafetyRatingSafe    SafetyRating = "SAFE"
	SafetyRatingCaution SafetyRating = "CAUTION"
	SafetyRatingRisky   SafetyRating = "RISKY"
	SafetyRatingUnsafe  SafetyRating = "UNSAFE"
)

// SafetyRatingLevel returns a numeric level for comparison (higher = worse).
func SafetyRatingLevel(r SafetyRating) int {
	switch r {
	case SafetyRatingSafe:
		return 0
	case SafetyRatingCaution:
		return 1
	case SafetyRatingRisky:
		return 2
	case SafetyRatingUnsafe:
		return 3
	default:
		return 3
	}
}

// ParseSafetyRating converts a string to SafetyRating, defaulting to CAUTION.
func ParseSafetyRating(s string) SafetyRating {
	switch s {
	case "SAFE":
		return SafetyRatingSafe
	case "CAUTION":
		return SafetyRatingCaution
	case "RISKY":
		return SafetyRatingRisky
	case "UNSAFE":
		return SafetyRatingUnsafe
	default:
		return SafetyRatingCaution
	}
}

// Confidence reflects the quality and breadth of evidence behind a recommendation.
type Confidence string

const (
	ConfidenceHigh   Confidence = "HIGH"
	ConfidenceMedium Confidence = "MEDIUM"
	ConfidenceLow    Confidence = "LOW"
)

// ResourceValues holds CPU/memory requests and limits for a container.
type ResourceValues struct {
	CPURequest    float64 `json:"cpu_request"`    // cores
	CPULimit      float64 `json:"cpu_limit"`      // cores
	MemoryRequest float64 `json:"memory_request"` // bytes
	MemoryLimit   float64 `json:"memory_limit"`   // bytes
}

// ResourceDelta holds the percentage change from current to recommended.
type ResourceDelta struct {
	CPURequestPercent    float64 `json:"cpu_request_percent"`
	CPULimitPercent      float64 `json:"cpu_limit_percent"`
	MemoryRequestPercent float64 `json:"memory_request_percent"`
	MemoryLimitPercent   float64 `json:"memory_limit_percent"`
}

// ContainerAlignment is the recommendation for a single container.
type ContainerAlignment struct {
	Name         string         `json:"name"`
	Current      ResourceValues `json:"current"`
	Recommended  ResourceValues `json:"recommended"`
	Delta        ResourceDelta  `json:"delta"`
	Capped       bool           `json:"capped"`
	CappedFields []string       `json:"capped_fields,omitempty"`
}

// ContainerResources holds the current resource values for a container,
// as read from the workload's pod template spec.
type ContainerResources struct {
	Name          string
	CPURequest    float64 // cores
	CPULimit      float64 // cores
	MemoryRequest float64 // bytes
	MemoryLimit   float64 // bytes
}

// LatchEvidence summarizes the latch data backing a recommendation.
type LatchEvidence struct {
	Duration       time.Duration        `json:"duration"`
	SampleCount    int                  `json:"sample_count"`
	SampleInterval time.Duration        `json:"sample_interval"`
	Gaps           int                  `json:"gaps"`
	Valid          bool                 `json:"valid"`
	CPU            *metrics.Percentiles `json:"cpu_percentiles"`
	Memory         *metrics.Percentiles `json:"memory_percentiles"`
}

// PolicyBounds holds the policy guardrails relevant to recommendation.
// Decoupled from the policy package for testability.
type PolicyBounds struct {
	MaxRequestDeltaPct int
	MaxLimitDeltaPct   int
	AllowLimitDecrease bool
	MinSafetyRating    SafetyRating
}

// PolicyResult summarizes policy evaluation for a recommendation.
type PolicyResult struct {
	PolicyPath      string   `json:"policy_path"`
	ApplyPermitted  bool     `json:"apply_permitted"`
	ExportPermitted bool     `json:"export_permitted"`
	DenialReasons   []string `json:"denial_reasons,omitempty"`
	HPADetected     bool     `json:"hpa_detected"`
	HPAName         string   `json:"hpa_name,omitempty"`
}

// AlignmentRecommendation is the full output of the recommendation engine.
type AlignmentRecommendation struct {
	Workload   WorkloadRef          `json:"workload"`
	Timestamp  time.Time            `json:"timestamp"`
	Confidence Confidence           `json:"confidence"`
	Safety     SafetyRating         `json:"safety"`
	Containers []ContainerAlignment `json:"containers"`
	Evidence   *LatchEvidence       `json:"latch_evidence"`
	Policy     *PolicyResult        `json:"policy_result"`
	Warnings   []string             `json:"warnings,omitempty"`
}

// RecommendInput holds all inputs to the recommendation engine.
type RecommendInput struct {
	Latch      *LatchResult
	Containers []ContainerResources
	Bounds     *PolicyBounds // nil = no policy bounds
	HPA        *HPAInfo
	HasProm    bool // Whether Prometheus historical data is available
}
