package promonitor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ppiankov/kubenow/internal/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLatchFilename(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "payment-api", Namespace: "default"}
	assert.Equal(t, "default__Deployment__payment-api.json", latchFilename(ref))
}

func TestSaveAndLoadLatch_RoundTrip(t *testing.T) {
	// Override home dir to temp
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	ref := WorkloadRef{Kind: "Deployment", Name: "test-api", Namespace: "default"}
	samples := make([]float64, 180)
	for i := range samples {
		samples[i] = float64(i + 1)
	}

	now := time.Now()
	data := &metrics.SpikeData{
		Namespace:    "default",
		WorkloadName: "test-api",
		SampleCount:  180,
		FirstSeen:    now.Add(-15 * time.Minute),
		LastSeen:     now,
		CPUSamples:   samples,
		MemSamples:   samples,
		OOMKills:     2,
		Restarts:     3,
	}

	result := BuildLatchResult(ref, data, 15*time.Minute, 5*time.Second)
	require.True(t, result.Valid)
	require.NotNil(t, result.CPU)
	require.NotNil(t, result.Memory)

	// Save
	err := SaveLatch(result)
	require.NoError(t, err)

	// Verify file exists
	dir := filepath.Join(tmpDir, ".kubenow", "latch")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	// Load
	loaded, err := LoadLatch(ref)
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, ref, loaded.Workload)
	assert.Equal(t, 180, loaded.Data.SampleCount)
	assert.Equal(t, 2, loaded.Data.OOMKills)
	assert.Equal(t, 3, loaded.Data.Restarts)
	assert.True(t, loaded.Valid)
	assert.NotNil(t, loaded.CPU)
	assert.NotNil(t, loaded.Memory)
	assert.InDelta(t, 90.5, loaded.CPU.P50, 0.1)
}

func TestLoadLatch_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	ref := WorkloadRef{Kind: "Deployment", Name: "nonexistent", Namespace: "default"}
	_, err := LoadLatch(ref)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no latch data")
}

func TestBuildLatchResult_NilData(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "test", Namespace: "default"}
	result := BuildLatchResult(ref, nil, 15*time.Minute, 5*time.Second)

	assert.False(t, result.Valid)
	assert.Equal(t, "no samples collected", result.Reason)
	assert.Nil(t, result.CPU)
	assert.Nil(t, result.Memory)
}

func TestBuildLatchResult_EmptySamples(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "test", Namespace: "default"}
	data := &metrics.SpikeData{
		CPUSamples: []float64{},
		MemSamples: []float64{},
	}
	result := BuildLatchResult(ref, data, 15*time.Minute, 5*time.Second)

	assert.False(t, result.Valid)
	assert.Equal(t, "no samples collected", result.Reason)
}

func TestBuildLatchResult_TooManyGaps(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "test", Namespace: "default"}
	now := time.Now()
	data := &metrics.SpikeData{
		SampleCount: 10,
		FirstSeen:   now.Add(-15 * time.Minute),
		LastSeen:    now,
		CPUSamples:  make([]float64, 10),
		MemSamples:  make([]float64, 10),
	}
	// Fill samples with some data
	for i := range data.CPUSamples {
		data.CPUSamples[i] = float64(i)
		data.MemSamples[i] = float64(i * 1000)
	}

	result := BuildLatchResult(ref, data, 15*time.Minute, 5*time.Second)

	// Expected ~181 samples, got 10 → >10% gaps
	assert.False(t, result.Valid)
	assert.Contains(t, result.Reason, "too many gaps")
}

func TestBuildLatchResult_ValidWithPercentiles(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "prod"}
	now := time.Now()

	cpuSamples := make([]float64, 180)
	memSamples := make([]float64, 180)
	for i := range cpuSamples {
		cpuSamples[i] = 0.05 + float64(i)*0.001 // 50m to 230m
		memSamples[i] = 150e6 + float64(i)*1e6  // 150Mi to 330Mi
	}

	data := &metrics.SpikeData{
		SampleCount: 180,
		FirstSeen:   now.Add(-15 * time.Minute),
		LastSeen:    now,
		CPUSamples:  cpuSamples,
		MemSamples:  memSamples,
	}

	result := BuildLatchResult(ref, data, 15*time.Minute, 5*time.Second)

	assert.True(t, result.Valid)
	assert.NotNil(t, result.CPU)
	assert.NotNil(t, result.Memory)
	assert.Greater(t, result.CPU.P95, result.CPU.P50)
	assert.Greater(t, result.CPU.P99, result.CPU.P95)
	assert.Greater(t, result.Memory.P95, result.Memory.P50)
}

func TestLatchResult_PlannedDuration_Serialization(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "prod"}
	result := BuildLatchResult(ref, nil, 93*time.Minute, 5*time.Second)
	result.PlannedDuration = 2 * time.Hour

	assert.Equal(t, 93*time.Minute, result.Duration)
	assert.Equal(t, 2*time.Hour, result.PlannedDuration)

	err := SaveLatch(result)
	require.NoError(t, err)

	loaded, err := LoadLatch(ref)
	require.NoError(t, err)
	assert.Equal(t, 93*time.Minute, loaded.Duration)
	assert.Equal(t, 2*time.Hour, loaded.PlannedDuration)
}

func TestLatchResult_NormalCompletion_NoPlanedDuration(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "prod"}
	result := BuildLatchResult(ref, nil, 15*time.Minute, 5*time.Second)

	// PlannedDuration should be zero for normal completion
	assert.Equal(t, time.Duration(0), result.PlannedDuration)
}
