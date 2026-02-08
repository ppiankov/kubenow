package metrics

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputePercentiles_KnownInputs(t *testing.T) {
	// 100 evenly spaced samples: 1, 2, 3, ..., 100
	samples := make([]float64, 100)
	for i := range samples {
		samples[i] = float64(i + 1)
	}

	data := &SpikeData{
		CPUSamples: samples,
		MemSamples: samples,
	}

	cpu, mem := data.ComputePercentiles()
	require.NotNil(t, cpu)
	require.NotNil(t, mem)

	// p50 of 1..100 = 50.5
	assert.InDelta(t, 50.5, cpu.P50, 0.01)
	// p95 of 1..100 ≈ 95.05
	assert.InDelta(t, 95.05, cpu.P95, 0.1)
	// p99 of 1..100 ≈ 99.01
	assert.InDelta(t, 99.01, cpu.P99, 0.1)
	// max = 100
	assert.Equal(t, 100.0, cpu.Max)
	// avg = 50.5
	assert.InDelta(t, 50.5, cpu.Avg, 0.01)
}

func TestComputePercentiles_SingleSample(t *testing.T) {
	data := &SpikeData{
		CPUSamples: []float64{42.0},
		MemSamples: []float64{1024.0},
	}

	cpu, mem := data.ComputePercentiles()
	require.NotNil(t, cpu)
	require.NotNil(t, mem)

	assert.Equal(t, 42.0, cpu.P50)
	assert.Equal(t, 42.0, cpu.P95)
	assert.Equal(t, 42.0, cpu.P99)
	assert.Equal(t, 42.0, cpu.Max)
	assert.Equal(t, 42.0, cpu.Avg)

	assert.Equal(t, 1024.0, mem.Max)
}

func TestComputePercentiles_NoSamples(t *testing.T) {
	data := &SpikeData{
		CPUSamples: []float64{},
		MemSamples: []float64{},
	}

	cpu, mem := data.ComputePercentiles()
	assert.Nil(t, cpu)
	assert.Nil(t, mem)
}

func TestComputePercentiles_TwoSamples(t *testing.T) {
	data := &SpikeData{
		CPUSamples: []float64{10, 20},
		MemSamples: []float64{100, 200},
	}

	cpu, mem := data.ComputePercentiles()
	require.NotNil(t, cpu)
	require.NotNil(t, mem)

	// p50 of [10, 20] = 15 (interpolated)
	assert.InDelta(t, 15.0, cpu.P50, 0.01)
	assert.Equal(t, 20.0, cpu.Max)
	assert.InDelta(t, 15.0, cpu.Avg, 0.01)
}

func TestComputePercentiles_DoesNotMutateSamples(t *testing.T) {
	original := []float64{50, 10, 30, 20, 40}
	data := &SpikeData{
		CPUSamples: original,
		MemSamples: original,
	}

	data.ComputePercentiles()

	// Original order should be preserved
	assert.Equal(t, []float64{50, 10, 30, 20, 40}, data.CPUSamples)
}

func TestGapCount(t *testing.T) {
	tests := []struct {
		name      string
		firstSeen time.Time
		lastSeen  time.Time
		samples   int
		interval  time.Duration
		wantGaps  int
	}{
		{
			name:      "no gaps",
			firstSeen: time.Now().Add(-15 * time.Minute),
			lastSeen:  time.Now(),
			samples:   181, // 15min / 5s + 1
			interval:  5 * time.Second,
			wantGaps:  0,
		},
		{
			name:      "some gaps",
			firstSeen: time.Now().Add(-15 * time.Minute),
			lastSeen:  time.Now(),
			samples:   150,
			interval:  5 * time.Second,
			wantGaps:  31, // 181 - 150
		},
		{
			name:      "zero interval",
			firstSeen: time.Now().Add(-15 * time.Minute),
			lastSeen:  time.Now(),
			samples:   100,
			interval:  0,
			wantGaps:  0,
		},
		{
			name:      "zero samples",
			firstSeen: time.Now().Add(-15 * time.Minute),
			lastSeen:  time.Now(),
			samples:   0,
			interval:  5 * time.Second,
			wantGaps:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := &SpikeData{
				FirstSeen:   tt.firstSeen,
				LastSeen:    tt.lastSeen,
				SampleCount: tt.samples,
			}
			assert.Equal(t, tt.wantGaps, data.GapCount(tt.interval))
		})
	}
}

func TestPercentile_EdgeCases(t *testing.T) {
	assert.Equal(t, 0.0, percentile([]float64{}, 0.5))
	assert.Equal(t, 5.0, percentile([]float64{5}, 0.5))
	assert.Equal(t, 5.0, percentile([]float64{5}, 0.99))
}

func TestSortFloat64s(t *testing.T) {
	input := []float64{5, 3, 1, 4, 2}
	sortFloat64s(input)
	assert.Equal(t, []float64{1, 2, 3, 4, 5}, input)
}
