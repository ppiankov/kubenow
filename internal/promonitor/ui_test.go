package promonitor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRenderSafetyRating_AllRatings(t *testing.T) {
	for _, r := range []SafetyRating{SafetyRatingSafe, SafetyRatingCaution, SafetyRatingRisky, SafetyRatingUnsafe} {
		output := renderSafetyRating(r)
		assert.NotEmpty(t, output, "rating %s should render", r)
	}
}

func TestRenderConfidence_AllLevels(t *testing.T) {
	for _, c := range []Confidence{ConfidenceHigh, ConfidenceMedium, ConfidenceLow} {
		output := renderConfidence(c)
		assert.NotEmpty(t, output, "confidence %s should render", c)
	}
}

func TestRenderRecommendation_Unsafe(t *testing.T) {
	rec := &AlignmentRecommendation{
		Safety:     SafetyRatingUnsafe,
		Confidence: ConfidenceLow,
		Warnings:   []string{"safety rating UNSAFE: no recommendation produced"},
		Evidence:   &LatchEvidence{Duration: 15 * time.Minute, SampleCount: 180},
	}
	output := renderRecommendation(rec)
	assert.Contains(t, output, "UNSAFE")
	assert.Contains(t, output, "LOW")
}

func TestRenderRecommendation_WithContainers(t *testing.T) {
	rec := &AlignmentRecommendation{
		Safety:     SafetyRatingSafe,
		Confidence: ConfidenceHigh,
		Containers: []ContainerAlignment{{
			Name:        "app",
			Current:     ResourceValues{CPURequest: 0.1, CPULimit: 0.5, MemoryRequest: 1e8, MemoryLimit: 5e8},
			Recommended: ResourceValues{CPURequest: 0.15, CPULimit: 0.6, MemoryRequest: 1.5e8, MemoryLimit: 5e8},
			Delta:       ResourceDelta{CPURequestPercent: 50, CPULimitPercent: 20, MemoryRequestPercent: 50, MemoryLimitPercent: 0},
		}},
		Evidence: &LatchEvidence{Duration: 15 * time.Minute, SampleCount: 180},
	}
	output := renderRecommendation(rec)
	assert.Contains(t, output, "app")
	assert.Contains(t, output, "SAFE")
}

func TestFmtCPU(t *testing.T) {
	assert.Equal(t, "0m", fmtCPU(0))
	assert.Equal(t, "100m", fmtCPU(0.1))
	assert.Equal(t, "1000m", fmtCPU(1.0))
}

func TestFmtMem(t *testing.T) {
	assert.Equal(t, "0Mi", fmtMem(0))
	assert.Equal(t, "100Mi", fmtMem(100*1024*1024))
}

func TestFmtDelta(t *testing.T) {
	assert.Equal(t, "+50%", fmtDelta(50))
	assert.Equal(t, "-20%", fmtDelta(-20))
	assert.Equal(t, "0%", fmtDelta(0))
}

func TestRenderRecommendation_SkipsZeroMemRows(t *testing.T) {
	rec := &AlignmentRecommendation{
		Safety:     SafetyRatingSafe,
		Confidence: ConfidenceHigh,
		Containers: []ContainerAlignment{{
			Name:        "wiki",
			Current:     ResourceValues{CPURequest: 0.05, CPULimit: 0.5, MemoryRequest: 0, MemoryLimit: 0},
			Recommended: ResourceValues{CPURequest: 0.035, CPULimit: 0.5, MemoryRequest: 0, MemoryLimit: 0},
			Delta:       ResourceDelta{CPURequestPercent: -30, CPULimitPercent: 0, MemoryRequestPercent: 0, MemoryLimitPercent: 0},
		}},
		Evidence: &LatchEvidence{Duration: 15 * time.Minute, SampleCount: 64},
	}
	output := renderRecommendation(rec)
	assert.Contains(t, output, "CPU req")
	assert.Contains(t, output, "CPU lim")
	assert.NotContains(t, output, "MEM req")
	assert.NotContains(t, output, "MEM lim")
}

func TestRenderRecommendation_ShowsMemWhenSet(t *testing.T) {
	rec := &AlignmentRecommendation{
		Safety:     SafetyRatingSafe,
		Confidence: ConfidenceHigh,
		Containers: []ContainerAlignment{{
			Name:        "api",
			Current:     ResourceValues{CPURequest: 0.1, CPULimit: 0.5, MemoryRequest: 128 * 1024 * 1024, MemoryLimit: 512 * 1024 * 1024},
			Recommended: ResourceValues{CPURequest: 0.15, CPULimit: 0.6, MemoryRequest: 192 * 1024 * 1024, MemoryLimit: 512 * 1024 * 1024},
			Delta:       ResourceDelta{CPURequestPercent: 50, CPULimitPercent: 20, MemoryRequestPercent: 50, MemoryLimitPercent: 0},
		}},
		Evidence: &LatchEvidence{Duration: 15 * time.Minute, SampleCount: 180},
	}
	output := renderRecommendation(rec)
	assert.Contains(t, output, "MEM req")
	assert.Contains(t, output, "MEM lim")
}
