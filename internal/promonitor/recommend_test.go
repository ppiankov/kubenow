package promonitor

import (
	"testing"
	"time"

	"github.com/ppiankov/kubenow/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Safety Rating ---

func TestComputeSafetyRating_Safe(t *testing.T) {
	data := &metrics.SpikeData{}
	assert.Equal(t, SafetyRatingSafe, ComputeSafetyRating(data))
}

func TestComputeSafetyRating_CautionOOM(t *testing.T) {
	data := &metrics.SpikeData{OOMKills: 1}
	assert.Equal(t, SafetyRatingCaution, ComputeSafetyRating(data))
}

func TestComputeSafetyRating_CautionRestart(t *testing.T) {
	data := &metrics.SpikeData{Restarts: 1}
	assert.Equal(t, SafetyRatingCaution, ComputeSafetyRating(data))
}

func TestComputeSafetyRating_CautionThrottling(t *testing.T) {
	data := &metrics.SpikeData{ThrottlingDetected: true}
	assert.Equal(t, SafetyRatingCaution, ComputeSafetyRating(data))
}

func TestComputeSafetyRating_Risky(t *testing.T) {
	data := &metrics.SpikeData{OOMKills: 3}
	assert.Equal(t, SafetyRatingRisky, ComputeSafetyRating(data))
}

func TestComputeSafetyRating_RiskyEvictions(t *testing.T) {
	data := &metrics.SpikeData{Evictions: 1}
	assert.Equal(t, SafetyRatingRisky, ComputeSafetyRating(data))
}

func TestComputeSafetyRating_RiskyRestarts(t *testing.T) {
	data := &metrics.SpikeData{Restarts: 6}
	assert.Equal(t, SafetyRatingRisky, ComputeSafetyRating(data))
}

func TestComputeSafetyRating_Unsafe(t *testing.T) {
	data := &metrics.SpikeData{OOMKills: 5}
	assert.Equal(t, SafetyRatingUnsafe, ComputeSafetyRating(data))
}

func TestComputeSafetyRating_UnsafeRestarts(t *testing.T) {
	data := &metrics.SpikeData{Restarts: 20}
	assert.Equal(t, SafetyRatingUnsafe, ComputeSafetyRating(data))
}

func TestComputeSafetyRating_NilData(t *testing.T) {
	assert.Equal(t, SafetyRatingCaution, ComputeSafetyRating(nil))
}

// --- Safety Margin ---

func TestSafetyMargin_AllRatings(t *testing.T) {
	assert.Equal(t, 1.0, safetyMargin(SafetyRatingSafe))
	assert.Equal(t, 1.3, safetyMargin(SafetyRatingCaution))
	assert.Equal(t, 1.5, safetyMargin(SafetyRatingRisky))
	assert.Equal(t, 0.0, safetyMargin(SafetyRatingUnsafe))
}

// --- Confidence ---

func TestComputeConfidence_High(t *testing.T) {
	c := computeConfidence(24*time.Hour, 5000, true, SafetyRatingSafe)
	assert.Equal(t, ConfidenceHigh, c)
}

func TestComputeConfidence_Medium_WithProm(t *testing.T) {
	c := computeConfidence(2*time.Hour, 500, true, SafetyRatingCaution)
	assert.Equal(t, ConfidenceMedium, c)
}

func TestComputeConfidence_Medium_HighSamples(t *testing.T) {
	c := computeConfidence(2*time.Hour, 1000, false, SafetyRatingSafe)
	assert.Equal(t, ConfidenceMedium, c)
}

func TestComputeConfidence_Low_ShortLatch(t *testing.T) {
	c := computeConfidence(15*time.Minute, 180, false, SafetyRatingSafe)
	assert.Equal(t, ConfidenceLow, c)
}

func TestComputeConfidence_Low_NoPromLowSamples(t *testing.T) {
	c := computeConfidence(3*time.Hour, 500, false, SafetyRatingSafe)
	assert.Equal(t, ConfidenceLow, c)
}

func TestComputeConfidence_High_RequiresAllConditions(t *testing.T) {
	// Missing Prom → not HIGH
	c := computeConfidence(24*time.Hour, 5000, false, SafetyRatingSafe)
	assert.NotEqual(t, ConfidenceHigh, c)

	// Safety not SAFE → not HIGH
	c = computeConfidence(24*time.Hour, 5000, true, SafetyRatingCaution)
	assert.NotEqual(t, ConfidenceHigh, c)
}

// --- Delta Percent ---

func TestDeltaPercent(t *testing.T) {
	tests := []struct {
		name     string
		current  float64
		rec      float64
		expected float64
	}{
		{"increase 50%", 100, 150, 50},
		{"decrease 25%", 200, 150, -25},
		{"no change", 100, 100, 0},
		{"from zero", 0, 100, 100},
		{"both zero", 0, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.InDelta(t, tt.expected, deltaPercent(tt.current, tt.rec), 0.01)
		})
	}
}

// --- Cap Value ---

func TestCapValue(t *testing.T) {
	// Increase capped at +30%
	assert.InDelta(t, 130.0, capValue(100, 30, true), 0.01)
	// Decrease capped at -30%
	assert.InDelta(t, 70.0, capValue(100, 30, false), 0.01)
}

// --- Full Recommendation ---

func testLatch(cpuP95, cpuP99, cpuMax, memP95, memP99, memMax float64, data *metrics.SpikeData) *LatchResult {
	return &LatchResult{
		Workload:  WorkloadRef{Kind: "Deployment", Name: "test-api", Namespace: "default"},
		Timestamp: time.Now(),
		Duration:  15 * time.Minute,
		Interval:  5 * time.Second,
		Data:      data,
		CPU:       &metrics.Percentiles{P50: cpuP95 * 0.8, P95: cpuP95, P99: cpuP99, Max: cpuMax, Avg: cpuP95 * 0.7},
		Memory:    &metrics.Percentiles{P50: memP95 * 0.8, P95: memP95, P99: memP99, Max: memMax, Avg: memP95 * 0.7},
		Valid:     true,
	}
}

func testContainer(cpuReq, cpuLim, memReq, memLim float64) ContainerResources {
	return ContainerResources{
		Name:          "api",
		CPURequest:    cpuReq,
		CPULimit:      cpuLim,
		MemoryRequest: memReq,
		MemoryLimit:   memLim,
	}
}

func TestRecommend_NilInput(t *testing.T) {
	rec := Recommend(nil)
	require.NotNil(t, rec)
	assert.Contains(t, rec.Warnings[0], "no latch data")
	assert.Empty(t, rec.Containers)
}

func TestRecommend_NilLatch(t *testing.T) {
	rec := Recommend(&RecommendInput{Latch: nil})
	require.NotNil(t, rec)
	assert.Contains(t, rec.Warnings[0], "no latch data")
}

func TestRecommend_InvalidLatch(t *testing.T) {
	latch := &LatchResult{
		Valid:  false,
		Reason: "too many gaps: 50/181 (28%)",
	}
	rec := Recommend(&RecommendInput{Latch: latch})
	assert.Contains(t, rec.Warnings[0], "latch invalid")
	assert.Empty(t, rec.Containers)
	assert.NotNil(t, rec.Evidence)
}

func TestRecommend_MissingPercentiles(t *testing.T) {
	latch := &LatchResult{Valid: true, CPU: nil, Memory: nil}
	rec := Recommend(&RecommendInput{Latch: latch})
	assert.Contains(t, rec.Warnings[0], "missing percentile data")
}

func TestRecommend_Unsafe(t *testing.T) {
	data := &metrics.SpikeData{OOMKills: 10, SampleCount: 180}
	latch := testLatch(0.1, 0.15, 0.2, 200e6, 250e6, 300e6, data)

	rec := Recommend(&RecommendInput{
		Latch:      latch,
		Containers: []ContainerResources{testContainer(0.1, 0.5, 128e6, 512e6)},
	})

	assert.Equal(t, SafetyRatingUnsafe, rec.Safety)
	assert.Contains(t, rec.Warnings[0], "UNSAFE")
	assert.Empty(t, rec.Containers)
	assert.NotNil(t, rec.Evidence)
}

func TestRecommend_SingleContainer_Safe(t *testing.T) {
	// Safe workload: no signals
	data := &metrics.SpikeData{SampleCount: 180}
	// CPU: p95=0.08, p99=0.12, max=0.15 (cores)
	// Mem: p95=170MB, p99=200MB, max=220MB
	latch := testLatch(0.08, 0.12, 0.15, 170e6, 200e6, 220e6, data)

	container := testContainer(0.1, 0.5, 128e6, 512e6)
	rec := Recommend(&RecommendInput{
		Latch:      latch,
		Containers: []ContainerResources{container},
	})

	require.Len(t, rec.Containers, 1)
	c := rec.Containers[0]
	assert.Equal(t, SafetyRatingSafe, rec.Safety)
	assert.Equal(t, ConfidenceLow, rec.Confidence) // 15m, no prom

	// CPU request = p95 * 1.0 = 0.08
	assert.InDelta(t, 0.08, c.Recommended.CPURequest, 0.001)
	// CPU limit = p99 * 1.0 * 1.5 = 0.18
	assert.InDelta(t, 0.18, c.Recommended.CPULimit, 0.001)
	// Mem request = p95 * 1.0 = 170MB
	assert.InDelta(t, 170e6, c.Recommended.MemoryRequest, 1e5)
	// Mem limit = max(p99*1.0*1.2, max) = max(240MB, 220MB) = 240MB
	assert.InDelta(t, 240e6, c.Recommended.MemoryLimit, 1e5)

	// Limit >= request
	assert.GreaterOrEqual(t, c.Recommended.CPULimit, c.Recommended.CPURequest)
	assert.GreaterOrEqual(t, c.Recommended.MemoryLimit, c.Recommended.MemoryRequest)
}

func TestRecommend_SingleContainer_Caution(t *testing.T) {
	data := &metrics.SpikeData{OOMKills: 1, SampleCount: 180}
	latch := testLatch(0.08, 0.12, 0.15, 170e6, 200e6, 220e6, data)

	container := testContainer(0.1, 0.5, 128e6, 512e6)
	rec := Recommend(&RecommendInput{
		Latch:      latch,
		Containers: []ContainerResources{container},
	})

	require.Len(t, rec.Containers, 1)
	c := rec.Containers[0]
	assert.Equal(t, SafetyRatingCaution, rec.Safety)

	// CPU request = p95 * 1.3 = 0.104
	assert.InDelta(t, 0.104, c.Recommended.CPURequest, 0.001)
	// CPU limit = p99 * 1.3 * 1.5 = 0.234
	assert.InDelta(t, 0.234, c.Recommended.CPULimit, 0.001)
	// Mem request = p95 * 1.3 = 221MB
	assert.InDelta(t, 221e6, c.Recommended.MemoryRequest, 1e5)
}

func TestRecommend_BurstCap(t *testing.T) {
	// Set current limits low so burst cap kicks in
	data := &metrics.SpikeData{SampleCount: 180}
	// High p99 values to trigger burst cap
	latch := testLatch(0.5, 0.8, 1.0, 500e6, 900e6, 900e6, data)

	// Current CPU limit is 0.5, so 2x = 1.0
	// Recommended CPU limit without cap: 0.8 * 1.0 * 1.5 = 1.2 → capped to 1.0
	// Current mem limit is 512MB, so 2x = 1024MB
	// Recommended mem limit: 900e6 * 1.0 * 1.2 = 1080MB → capped to 1024MB
	container := testContainer(0.1, 0.5, 128e6, 512e6)
	rec := Recommend(&RecommendInput{
		Latch:      latch,
		Containers: []ContainerResources{container},
	})

	require.Len(t, rec.Containers, 1)
	c := rec.Containers[0]

	// CPU limit burst-capped at 2x current (1.0)
	assert.InDelta(t, 1.0, c.Recommended.CPULimit, 0.001)
	// Memory limit burst-capped at 2x current (1024MB)
	assert.InDelta(t, 1024e6, c.Recommended.MemoryLimit, 1e5)
}

func TestRecommend_MemoryFloor(t *testing.T) {
	// Memory max is higher than p99 * margin * 1.2
	data := &metrics.SpikeData{SampleCount: 180}
	// Mem p99=100MB, max=200MB. p99*1.0*1.2 = 120MB < 200MB max → floor at 200MB
	latch := testLatch(0.05, 0.08, 0.1, 80e6, 100e6, 200e6, data)

	container := testContainer(0.1, 0.5, 128e6, 512e6)
	rec := Recommend(&RecommendInput{
		Latch:      latch,
		Containers: []ContainerResources{container},
	})

	require.Len(t, rec.Containers, 1)
	// Memory limit floored at max (200MB)
	assert.InDelta(t, 200e6, rec.Containers[0].Recommended.MemoryLimit, 1e5)
}

func TestRecommend_LimitGeRequest(t *testing.T) {
	// When CPU p95 is high but p99 is only slightly higher,
	// limit could end up below request. Must be floored.
	data := &metrics.SpikeData{SampleCount: 180}
	// With RISKY (1.5x margin): CPU request = 0.2*1.5 = 0.3
	// CPU limit = 0.15*1.5*1.5 = 0.3375 (OK, above request)
	// Edge case: force limit < request
	latch := testLatch(0.2, 0.1, 0.1, 200e6, 150e6, 150e6, data)
	data.OOMKills = 3 // Make it RISKY for higher margin

	container := testContainer(0.1, 0.5, 128e6, 512e6)
	rec := Recommend(&RecommendInput{
		Latch:      latch,
		Containers: []ContainerResources{container},
	})

	require.Len(t, rec.Containers, 1)
	c := rec.Containers[0]
	// With RISKY 1.5x: request = 0.2*1.5 = 0.3, limit = 0.1*1.5*1.5 = 0.225
	// 0.225 < 0.3 so limit should be floored to request
	assert.GreaterOrEqual(t, c.Recommended.CPULimit, c.Recommended.CPURequest)
	assert.GreaterOrEqual(t, c.Recommended.MemoryLimit, c.Recommended.MemoryRequest)
}

func TestRecommend_PolicyBounds(t *testing.T) {
	data := &metrics.SpikeData{SampleCount: 180}
	// Large difference: current 0.1 CPU, recommended ~0.08 → ~-20%
	// With maxRequestDelta=10%, should be capped
	latch := testLatch(0.2, 0.25, 0.3, 300e6, 400e6, 450e6, data)

	container := testContainer(0.1, 0.5, 128e6, 512e6)
	bounds := &PolicyBounds{
		MaxRequestDeltaPct: 10,
		MaxLimitDeltaPct:   15,
		AllowLimitDecrease: true,
		MinSafetyRating:    SafetyRatingSafe,
	}

	rec := Recommend(&RecommendInput{
		Latch:      latch,
		Containers: []ContainerResources{container},
		Bounds:     bounds,
	})

	require.Len(t, rec.Containers, 1)
	c := rec.Containers[0]
	assert.True(t, c.Capped)
	assert.NotEmpty(t, c.CappedFields)

	// Deltas should be within bounds (InDelta for floating point)
	assert.InDelta(t, float64(bounds.MaxRequestDeltaPct), c.Delta.CPURequestPercent, 0.01)
}

func TestRecommend_AllowLimitDecreaseDisabled(t *testing.T) {
	data := &metrics.SpikeData{SampleCount: 180}
	// Low usage → recommendation will be lower than current
	latch := testLatch(0.02, 0.03, 0.04, 50e6, 70e6, 80e6, data)

	// Current limits are high, recommended will be lower
	container := testContainer(0.5, 1.0, 512e6, 1024e6)
	bounds := &PolicyBounds{
		AllowLimitDecrease: false,
	}

	rec := Recommend(&RecommendInput{
		Latch:      latch,
		Containers: []ContainerResources{container},
		Bounds:     bounds,
	})

	require.Len(t, rec.Containers, 1)
	c := rec.Containers[0]

	// Limits should not decrease below current
	assert.Equal(t, container.CPULimit, c.Recommended.CPULimit)
	assert.Equal(t, container.MemoryLimit, c.Recommended.MemoryLimit)
}

func TestRecommend_PolicyMinSafety(t *testing.T) {
	data := &metrics.SpikeData{OOMKills: 3, SampleCount: 180} // RISKY
	latch := testLatch(0.1, 0.15, 0.2, 200e6, 250e6, 300e6, data)

	bounds := &PolicyBounds{MinSafetyRating: SafetyRatingCaution}
	rec := Recommend(&RecommendInput{
		Latch:      latch,
		Containers: []ContainerResources{testContainer(0.1, 0.5, 128e6, 512e6)},
		Bounds:     bounds,
	})

	// RISKY is below CAUTION minimum → no recommendation
	assert.Contains(t, rec.Warnings[0], "below policy minimum")
	assert.Empty(t, rec.Containers)
}

func TestRecommend_MultiContainer_Warning(t *testing.T) {
	data := &metrics.SpikeData{SampleCount: 180}
	latch := testLatch(0.1, 0.15, 0.2, 200e6, 250e6, 300e6, data)

	containers := []ContainerResources{
		testContainer(0.1, 0.5, 128e6, 512e6),
		{Name: "sidecar", CPURequest: 0.05, CPULimit: 0.2, MemoryRequest: 64e6, MemoryLimit: 128e6},
	}

	rec := Recommend(&RecommendInput{
		Latch:      latch,
		Containers: containers,
	})

	assert.Len(t, rec.Containers, 2)
	hasWarning := false
	for _, w := range rec.Warnings {
		if w != "" {
			hasWarning = true
		}
	}
	assert.True(t, hasWarning)
}

func TestRecommend_HPA_Warning(t *testing.T) {
	data := &metrics.SpikeData{SampleCount: 180}
	latch := testLatch(0.1, 0.15, 0.2, 200e6, 250e6, 300e6, data)

	rec := Recommend(&RecommendInput{
		Latch:      latch,
		Containers: []ContainerResources{testContainer(0.1, 0.5, 128e6, 512e6)},
		HPA:        &HPAInfo{Name: "api-hpa", MinReplica: 1, MaxReplica: 5},
	})

	assert.NotNil(t, rec.Policy)
	assert.True(t, rec.Policy.HPADetected)
	assert.Equal(t, "api-hpa", rec.Policy.HPAName)

	hasHPAWarning := false
	for _, w := range rec.Warnings {
		if w != "" {
			hasHPAWarning = true
		}
	}
	assert.True(t, hasHPAWarning)
}

func TestRecommend_ZeroCurrentResources(t *testing.T) {
	// Container with no requests/limits set
	data := &metrics.SpikeData{SampleCount: 180}
	latch := testLatch(0.1, 0.15, 0.2, 200e6, 250e6, 300e6, data)

	container := testContainer(0, 0, 0, 0)
	rec := Recommend(&RecommendInput{
		Latch:      latch,
		Containers: []ContainerResources{container},
	})

	require.Len(t, rec.Containers, 1)
	c := rec.Containers[0]

	// Should still produce recommendations
	assert.Greater(t, c.Recommended.CPURequest, 0.0)
	assert.Greater(t, c.Recommended.CPULimit, 0.0)
	assert.Greater(t, c.Recommended.MemoryRequest, 0.0)
	assert.Greater(t, c.Recommended.MemoryLimit, 0.0)

	// Limit >= request
	assert.GreaterOrEqual(t, c.Recommended.CPULimit, c.Recommended.CPURequest)
	assert.GreaterOrEqual(t, c.Recommended.MemoryLimit, c.Recommended.MemoryRequest)
}

func TestRecommend_EvidencePopulated(t *testing.T) {
	data := &metrics.SpikeData{SampleCount: 180}
	latch := testLatch(0.1, 0.15, 0.2, 200e6, 250e6, 300e6, data)
	latch.Gaps = 5

	rec := Recommend(&RecommendInput{
		Latch:      latch,
		Containers: []ContainerResources{testContainer(0.1, 0.5, 128e6, 512e6)},
	})

	require.NotNil(t, rec.Evidence)
	assert.Equal(t, 180, rec.Evidence.SampleCount)
	assert.Equal(t, 5, rec.Evidence.Gaps)
	assert.Equal(t, 15*time.Minute, rec.Evidence.Duration)
	assert.True(t, rec.Evidence.Valid)
	assert.NotNil(t, rec.Evidence.CPU)
	assert.NotNil(t, rec.Evidence.Memory)
}

// --- Safety Rating Levels ---

func TestSafetyRatingLevel(t *testing.T) {
	assert.Equal(t, 0, SafetyRatingLevel(SafetyRatingSafe))
	assert.Equal(t, 1, SafetyRatingLevel(SafetyRatingCaution))
	assert.Equal(t, 2, SafetyRatingLevel(SafetyRatingRisky))
	assert.Equal(t, 3, SafetyRatingLevel(SafetyRatingUnsafe))
}

func TestParseSafetyRating(t *testing.T) {
	assert.Equal(t, SafetyRatingSafe, ParseSafetyRating("SAFE"))
	assert.Equal(t, SafetyRatingCaution, ParseSafetyRating("CAUTION"))
	assert.Equal(t, SafetyRatingRisky, ParseSafetyRating("RISKY"))
	assert.Equal(t, SafetyRatingUnsafe, ParseSafetyRating("UNSAFE"))
	assert.Equal(t, SafetyRatingCaution, ParseSafetyRating("unknown"))
}
