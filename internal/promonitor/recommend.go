package promonitor

import (
	"fmt"
	"math"
	"time"

	"github.com/ppiankov/kubenow/internal/metrics"
)

// Safety margin multipliers per rating.
const (
	marginSafe    = 1.0
	marginCaution = 1.3
	marginRisky   = 1.5
)

// CPU limit fallback multiplier when p999 is unavailable (no Prometheus).
const cpuLimitFallbackMul = 1.5

// Memory limit additional factor (memory is not compressible).
const memLimitFactor = 1.2

// Burst cap: limit increases cannot exceed 2x current.
const burstCapMultiplier = 2.0

// Safety rating thresholds.
const (
	unsafeOOMThreshold      = 5
	unsafeRestartThreshold  = 20
	riskyOOMThreshold       = 3
	riskyRestartThreshold   = 5
	cautionOOMThreshold     = 1
	cautionRestartThreshold = 1
)

// ComputeSafetyRating determines the safety rating from spike data signals.
func ComputeSafetyRating(data *metrics.SpikeData) SafetyRating {
	if data == nil {
		return SafetyRatingCaution
	}

	if data.OOMKills >= unsafeOOMThreshold || data.Restarts >= unsafeRestartThreshold {
		return SafetyRatingUnsafe
	}

	if data.OOMKills >= riskyOOMThreshold || data.Restarts > riskyRestartThreshold || data.Evictions > 0 {
		return SafetyRatingRisky
	}

	if data.OOMKills >= cautionOOMThreshold || data.Restarts >= cautionRestartThreshold || data.ThrottlingDetected {
		return SafetyRatingCaution
	}

	return SafetyRatingSafe
}

// safetyMargin returns the multiplier for a given safety rating.
// Returns 0 for UNSAFE (no recommendation should be produced).
func safetyMargin(rating SafetyRating) float64 {
	switch rating {
	case SafetyRatingSafe:
		return marginSafe
	case SafetyRatingCaution:
		return marginCaution
	case SafetyRatingRisky:
		return marginRisky
	default:
		return 0
	}
}

// computeConfidence determines confidence level from evidence quality.
func computeConfidence(latchDuration time.Duration, sampleCount int, hasProm bool, safety SafetyRating) Confidence {
	if latchDuration >= 24*time.Hour && hasProm && safety == SafetyRatingSafe && sampleCount >= 5000 {
		return ConfidenceHigh
	}

	if latchDuration >= 2*time.Hour && (hasProm || sampleCount >= 1000) {
		return ConfidenceMedium
	}

	return ConfidenceLow
}

// Recommend produces an alignment recommendation from latch evidence
// and current container resources. This is a pure computation with no
// side effects — all inputs are provided, all outputs are returned.
func Recommend(input *RecommendInput) *AlignmentRecommendation {
	result := &AlignmentRecommendation{
		Timestamp: time.Now(),
	}

	if input == nil || input.Latch == nil {
		result.Warnings = append(result.Warnings, "no latch data available")
		return result
	}

	latch := input.Latch
	result.Workload = latch.Workload

	if !latch.Valid {
		result.Warnings = append(result.Warnings, fmt.Sprintf("latch invalid: %s", latch.Reason))
		result.Evidence = buildEvidence(latch)
		return result
	}

	if latch.CPU == nil || latch.Memory == nil {
		result.Warnings = append(result.Warnings, "latch missing percentile data")
		result.Evidence = buildEvidence(latch)
		return result
	}

	// Compute safety rating from observed signals
	safety := ComputeSafetyRating(latch.Data)
	result.Safety = safety

	// UNSAFE: no recommendation produced — evidence is inherently low-confidence
	// when the workload is actively crashing.
	if safety == SafetyRatingUnsafe {
		result.Confidence = ConfidenceLow
		if latch.Data != nil {
			if latch.Data.OOMKills > 0 {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("observed %d OOMKill(s) during latch", latch.Data.OOMKills))
			}
			if latch.Data.Restarts > 0 {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("observed %d container restart(s) during latch", latch.Data.Restarts))
			}
			if latch.Data.Evictions > 0 {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("observed %d pod eviction(s) during latch", latch.Data.Evictions))
			}
		}
		result.Warnings = append(result.Warnings, "safety rating UNSAFE: no recommendation produced")
		result.Evidence = buildEvidence(latch)
		return result
	}

	// Check against policy minimum safety rating
	if input.Bounds != nil && input.Bounds.MinSafetyRating != "" {
		minLevel := SafetyRatingLevel(input.Bounds.MinSafetyRating)
		actualLevel := SafetyRatingLevel(safety)
		if actualLevel > minLevel {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"safety rating %s below policy minimum %s: no recommendation produced",
				safety, input.Bounds.MinSafetyRating))
			result.Evidence = buildEvidence(latch)
			return result
		}
	}

	margin := safetyMargin(safety)

	// Compute confidence
	sampleCount := 0
	if latch.Data != nil {
		sampleCount = latch.Data.SampleCount
	}
	result.Confidence = computeConfidence(latch.Duration, sampleCount, input.HasProm, safety)

	// Build evidence
	result.Evidence = buildEvidence(latch)

	// HPA warning
	if input.HPA != nil {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("HPA %q detected (min=%d, max=%d): apply blocked unless acknowledged",
				input.HPA.Name, input.HPA.MinReplica, input.HPA.MaxReplica))
		result.Policy = &PolicyResult{
			HPADetected:     true,
			HPAName:         input.HPA.Name,
			ExportPermitted: true,
		}
	}

	// Multi-container warning
	if len(input.Containers) > 1 {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("multi-container pod (%d containers): recommendations use aggregate pod metrics",
				len(input.Containers)))
	}

	// Compute recommendation per container
	for _, container := range input.Containers {
		alignment := recommendContainer(container, latch.CPU, latch.Memory, margin, input.Bounds, input.HasProm)
		result.Containers = append(result.Containers, alignment)
	}

	// Set policy result if not already set by HPA
	if result.Policy == nil {
		result.Policy = &PolicyResult{ExportPermitted: true}
	}

	return result
}

// recommendContainer computes the recommendation for a single container.
func recommendContainer(
	current ContainerResources,
	cpuPerc *metrics.Percentiles,
	memPerc *metrics.Percentiles,
	margin float64,
	bounds *PolicyBounds,
	hasProm bool,
) ContainerAlignment {
	alignment := ContainerAlignment{
		Name: current.Name,
		Current: ResourceValues{
			CPURequest:    current.CPURequest,
			CPULimit:      current.CPULimit,
			MemoryRequest: current.MemoryRequest,
			MemoryLimit:   current.MemoryLimit,
		},
	}

	// Recommended requests: p95 * safety_margin
	recCPURequest := cpuPerc.P95 * margin
	recMemRequest := memPerc.P95 * margin

	// Recommended CPU limit: p999 * margin (Prometheus) or p99 * margin * 1.5 (fallback)
	// Currently latch cannot compute p999, so we always use the fallback formula.
	recCPULimit := cpuPerc.P99 * margin * cpuLimitFallbackMul

	// Recommended memory limit: p99 * margin * 1.2, floored by observed max
	recMemLimit := memPerc.P99 * margin * memLimitFactor
	if recMemLimit < memPerc.Max {
		recMemLimit = memPerc.Max
	}

	// Burst cap: limits cannot exceed 2x current (if current > 0)
	if current.CPULimit > 0 {
		maxCPULimit := current.CPULimit * burstCapMultiplier
		if recCPULimit > maxCPULimit {
			recCPULimit = maxCPULimit
		}
	}
	if current.MemoryLimit > 0 {
		maxMemLimit := current.MemoryLimit * burstCapMultiplier
		if recMemLimit > maxMemLimit {
			recMemLimit = maxMemLimit
		}
	}

	// Floor: limit >= request
	if recCPULimit < recCPURequest {
		recCPULimit = recCPURequest
	}
	if recMemLimit < recMemRequest {
		recMemLimit = recMemRequest
	}

	alignment.Recommended = ResourceValues{
		CPURequest:    recCPURequest,
		CPULimit:      recCPULimit,
		MemoryRequest: recMemRequest,
		MemoryLimit:   recMemLimit,
	}

	// Compute deltas
	alignment.Delta = ResourceDelta{
		CPURequestPercent:    deltaPercent(current.CPURequest, recCPURequest),
		CPULimitPercent:      deltaPercent(current.CPULimit, recCPULimit),
		MemoryRequestPercent: deltaPercent(current.MemoryRequest, recMemRequest),
		MemoryLimitPercent:   deltaPercent(current.MemoryLimit, recMemLimit),
	}

	// Apply admin policy bounds
	if bounds != nil {
		applyPolicyBounds(&alignment, bounds)
	}

	return alignment
}

// applyPolicyBounds caps recommendation deltas per policy guardrails.
func applyPolicyBounds(a *ContainerAlignment, b *PolicyBounds) {
	if b.MaxRequestDeltaPct > 0 {
		maxPct := float64(b.MaxRequestDeltaPct)

		if a.Current.CPURequest > 0 && math.Abs(a.Delta.CPURequestPercent) > maxPct {
			a.Recommended.CPURequest = capValue(a.Current.CPURequest, maxPct, a.Delta.CPURequestPercent > 0)
			a.Delta.CPURequestPercent = deltaPercent(a.Current.CPURequest, a.Recommended.CPURequest)
			a.Capped = true
			a.CappedFields = append(a.CappedFields, "cpu_request")
		}

		if a.Current.MemoryRequest > 0 && math.Abs(a.Delta.MemoryRequestPercent) > maxPct {
			a.Recommended.MemoryRequest = capValue(a.Current.MemoryRequest, maxPct, a.Delta.MemoryRequestPercent > 0)
			a.Delta.MemoryRequestPercent = deltaPercent(a.Current.MemoryRequest, a.Recommended.MemoryRequest)
			a.Capped = true
			a.CappedFields = append(a.CappedFields, "memory_request")
		}
	}

	if b.MaxLimitDeltaPct > 0 {
		maxPct := float64(b.MaxLimitDeltaPct)

		if a.Current.CPULimit > 0 && math.Abs(a.Delta.CPULimitPercent) > maxPct {
			a.Recommended.CPULimit = capValue(a.Current.CPULimit, maxPct, a.Delta.CPULimitPercent > 0)
			a.Delta.CPULimitPercent = deltaPercent(a.Current.CPULimit, a.Recommended.CPULimit)
			a.Capped = true
			a.CappedFields = append(a.CappedFields, "cpu_limit")
		}

		if a.Current.MemoryLimit > 0 && math.Abs(a.Delta.MemoryLimitPercent) > maxPct {
			a.Recommended.MemoryLimit = capValue(a.Current.MemoryLimit, maxPct, a.Delta.MemoryLimitPercent > 0)
			a.Delta.MemoryLimitPercent = deltaPercent(a.Current.MemoryLimit, a.Recommended.MemoryLimit)
			a.Capped = true
			a.CappedFields = append(a.CappedFields, "memory_limit")
		}
	}

	// Prevent limit decrease if policy disallows it
	if !b.AllowLimitDecrease {
		if a.Recommended.CPULimit < a.Current.CPULimit && a.Current.CPULimit > 0 {
			a.Recommended.CPULimit = a.Current.CPULimit
			a.Delta.CPULimitPercent = 0
		}
		if a.Recommended.MemoryLimit < a.Current.MemoryLimit && a.Current.MemoryLimit > 0 {
			a.Recommended.MemoryLimit = a.Current.MemoryLimit
			a.Delta.MemoryLimitPercent = 0
		}
	}

	// Re-enforce limit >= request after policy bounds
	if a.Recommended.CPULimit < a.Recommended.CPURequest {
		a.Recommended.CPULimit = a.Recommended.CPURequest
		a.Delta.CPULimitPercent = deltaPercent(a.Current.CPULimit, a.Recommended.CPULimit)
	}
	if a.Recommended.MemoryLimit < a.Recommended.MemoryRequest {
		a.Recommended.MemoryLimit = a.Recommended.MemoryRequest
		a.Delta.MemoryLimitPercent = deltaPercent(a.Current.MemoryLimit, a.Recommended.MemoryLimit)
	}
}

// deltaPercent computes the percentage change from current to recommended.
// Returns 0 if both are zero. Returns 100 if going from zero to non-zero.
func deltaPercent(current, recommended float64) float64 {
	if current == 0 {
		if recommended == 0 {
			return 0
		}
		return 100
	}
	return (recommended - current) / current * 100
}

// capValue limits a value change to maxPct percent from the current value.
func capValue(current, maxPct float64, increase bool) float64 {
	if increase {
		return current * (1 + maxPct/100)
	}
	return current * (1 - maxPct/100)
}

// buildEvidence constructs a LatchEvidence from a LatchResult.
func buildEvidence(latch *LatchResult) *LatchEvidence {
	sc := 0
	if latch.Data != nil {
		sc = latch.Data.SampleCount
	}
	return &LatchEvidence{
		Duration:       latch.Duration,
		SampleCount:    sc,
		SampleInterval: latch.Interval,
		Gaps:           latch.Gaps,
		Valid:          latch.Valid,
		CPU:            latch.CPU,
		Memory:         latch.Memory,
	}
}
