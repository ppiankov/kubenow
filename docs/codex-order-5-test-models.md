# Codex Work Order 5: Add tests for models package

## Branch
`codex/test-models`

## Depends on
None — models package has no dependencies on recent changes.

## Problem
`internal/models/resource_usage.go` (372 lines) has zero test coverage. Contains safety rating logic (`DetermineRating()`, 108 lines), spike detection, AI workload pattern detection, and memory formatting — all untested.

## Task
Create `internal/models/resource_usage_test.go` with tests for all exported functions.

## Files to create
- `internal/models/resource_usage_test.go`

## Tests to add

```go
package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetermineRating_Safe(t *testing.T) {
	sa := SafetyAnalysis{
		OOMKills:     0,
		Restarts:     0,
		Evictions:    0,
		CPUThrottled: false,
	}
	rating := sa.DetermineRating()
	assert.Equal(t, "safe", string(rating))
}

func TestDetermineRating_Warning(t *testing.T) {
	sa := SafetyAnalysis{
		OOMKills:     1,
		Restarts:     0,
		Evictions:    0,
		CPUThrottled: true,
	}
	rating := sa.DetermineRating()
	assert.Equal(t, "warning", string(rating))
}

func TestDetermineRating_Critical(t *testing.T) {
	sa := SafetyAnalysis{
		OOMKills:     5,
		Restarts:     10,
		Evictions:    0,
		CPUThrottled: true,
	}
	rating := sa.DetermineRating()
	assert.Equal(t, "critical", string(rating))
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
		{"has evictions", SafetyAnalysis{Evictions: 1}, false},
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
		{"cpu spike", SafetyAnalysis{MaxCPUSpike: 2.5}, true},
		{"mem spike", SafetyAnalysis{MaxMemorySpike: 3.0}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.sa.HasSpikes())
		})
	}
}

func TestDetectUltraSpikes(t *testing.T) {
	sa := SafetyAnalysis{MaxCPUSpike: 10.0, MaxMemorySpike: 1.0}
	result := sa.DetectUltraSpikes()
	assert.True(t, result)

	sa2 := SafetyAnalysis{MaxCPUSpike: 1.5, MaxMemorySpike: 1.5}
	result2 := sa2.DetectUltraSpikes()
	assert.False(t, result2)
}

func TestDetectAIWorkloadPattern(t *testing.T) {
	// AI workload has high memory, bursty CPU
	sa := SafetyAnalysis{
		MaxCPUSpike:    5.0,
		MaxMemorySpike: 1.2,
		CPUSpikeCount:  10,
	}
	assert.True(t, sa.DetectAIWorkloadPattern())

	// Normal workload
	sa2 := SafetyAnalysis{
		MaxCPUSpike:    1.5,
		MaxMemorySpike: 1.1,
		CPUSpikeCount:  1,
	}
	assert.False(t, sa2.DetectAIWorkloadPattern())
}

func TestFormatMemoryBytes(t *testing.T) {
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{"zero", 0, "0"},
		{"bytes", 500, "500B"},
		{"kilobytes", 1024, "1Ki"},
		{"megabytes", 1048576, "1Mi"},
		{"gigabytes", 1073741824, "1Gi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FormatMemoryBytes(tt.bytes))
		})
	}
}

func TestParseMemoryString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{"megabytes", "100Mi", 104857600, false},
		{"gigabytes", "2Gi", 2147483648, false},
		{"kilobytes", "512Ki", 524288, false},
		{"invalid", "abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseMemoryString(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
```

## Verification
```bash
go test -race -count=1 ./internal/models/...
go vet ./internal/models/...
```

## Notes
- Read the actual `DetermineRating()` logic before writing tests — the rating names and thresholds may differ from the examples above. Adjust assertions to match the actual implementation.
- Use `testify/assert` — it's already a project dependency.
- Table-driven tests preferred. No mocks needed — all functions are pure.
