package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetermineRating_Safe(t *testing.T) {
	sa := SafetyAnalysis{}
	sa.DetermineRating(0, 0, 0, 0)
	assert.Equal(t, SafetyRatingSafe, sa.Rating)
	assert.InDelta(t, 1.0, sa.SafeMargin, 0.0001)
}

func TestDetermineRating_CautionRestarts(t *testing.T) {
	sa := SafetyAnalysis{Restarts: 1}
	sa.DetermineRating(0, 0, 0, 0)
	assert.Equal(t, SafetyRatingCaution, sa.Rating)
	assert.InDelta(t, 1.3, sa.SafeMargin, 0.0001)
	assert.NotEmpty(t, sa.Warnings)
}

func TestDetermineRating_RiskyRestarts(t *testing.T) {
	sa := SafetyAnalysis{Restarts: 6}
	sa.DetermineRating(0, 0, 0, 0)
	assert.Equal(t, SafetyRatingRisky, sa.Rating)
	assert.InDelta(t, 1.5, sa.SafeMargin, 0.0001)
	assert.NotEmpty(t, sa.Warnings)
}

func TestDetermineRating_UnsafeOOMKills(t *testing.T) {
	sa := SafetyAnalysis{OOMKills: 2}
	sa.DetermineRating(0, 0, 0, 0)
	assert.Equal(t, SafetyRatingUnsafe, sa.Rating)
	assert.InDelta(t, 2.0, sa.SafeMargin, 0.0001)
	assert.NotEmpty(t, sa.Warnings)
}

func TestIsHealthy(t *testing.T) {
	tests := []struct {
		name string
		sa   SafetyAnalysis
		want bool
	}{
		{"zero issues", SafetyAnalysis{}, true},
		{"has OOMKills", SafetyAnalysis{OOMKills: 1}, false},
		{"has restarts", SafetyAnalysis{Restarts: 1}, false},
		{"crash loop", SafetyAnalysis{CrashLoopBackOff: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.sa.IsHealthy())
		})
	}
}

func TestHasSpikes(t *testing.T) {
	tests := []struct {
		name string
		sa   SafetyAnalysis
		want bool
	}{
		{"no spikes", SafetyAnalysis{}, false},
		{"cpu spike", SafetyAnalysis{CPUSpikeCount: 1}, true},
		{"mem spike", SafetyAnalysis{MemorySpikeCount: 1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.sa.HasSpikes())
		})
	}
}

func TestDetectUltraSpikes(t *testing.T) {
	sa := SafetyAnalysis{}
	sa.DetectUltraSpikes(0.2, 0.5, 1.0, 5.0)
	assert.True(t, sa.UltraSpikeLikely)
	assert.InDelta(t, 5.0, sa.UltraSpikeRatio, 0.0001)

	sa2 := SafetyAnalysis{}
	sa2.DetectUltraSpikes(0.2, 0.9, 1.0, 2.0)
	assert.False(t, sa2.UltraSpikeLikely)
	assert.InDelta(t, 2.0, sa2.UltraSpikeRatio, 0.0001)
}

func TestDetectAIWorkloadPattern(t *testing.T) {
	sa := SafetyAnalysis{}
	sa.DetectAIWorkloadPattern(
		[]string{"run", "llm-server"},
		map[string]string{"app": "embedding-service"},
		map[string]string{"component": "rag-worker"},
	)
	assert.True(t, sa.WorkloadPatternAI)
	assert.Contains(t, sa.WorkloadPatternTags, "llm")
	assert.Contains(t, sa.WorkloadPatternTags, "embedding")
	assert.Contains(t, sa.WorkloadPatternTags, "rag")

	sa2 := SafetyAnalysis{}
	sa2.DetectAIWorkloadPattern([]string{"run", "worker"}, nil, nil)
	assert.False(t, sa2.WorkloadPatternAI)
	assert.Empty(t, sa2.WorkloadPatternTags)
}

func TestFormatMemoryBytes(t *testing.T) {
	tests := []struct {
		name  string
		bytes float64
		want  string
	}{
		{"zero", 0, "0B"},
		{"bytes", 500, "500B"},
		{"kilobytes", 1024, "1.00Ki"},
		{"megabytes", 1024 * 1024, "1.00Mi"},
		{"gigabytes", 1024 * 1024 * 1024, "1.00Gi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FormatMemoryBytes(tt.bytes))
		})
	}
}

func TestParseMemoryString(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"megabytes", "100Mi"},
		{"gigabytes", "2Gi"},
		{"invalid", "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMemoryString(tt.input)
			assert.NoError(t, err)
			assert.Equal(t, float64(0), got)
		})
	}
}
