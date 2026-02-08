# Codex Work Order 3: Add UNSAFE confidence and UI tests

## Branch
`codex/unsafe-tests`

## Depends on
- Order 2 (unsafe-ux) must be merged first — tests validate the detail warnings

## Problem
Missing test coverage for:
- Confidence field is set to LOW when safety rating is UNSAFE
- UNSAFE detail warnings include OOMKill/restart/eviction counts
- UI renders UNSAFE guidance correctly

## Task
Add tests covering the UNSAFE path in recommend.go and UI rendering.

## Files to modify
- `internal/promonitor/recommend_test.go` — add tests
- `internal/promonitor/ui_test.go` — new file

## Tests to add

### recommend_test.go

```go
func TestRecommend_UnsafeConfidence(t *testing.T) {
    // When safety is UNSAFE via OOMKills, Confidence must be LOW
    latch := &LatchResult{
        Valid:    true,
        Duration: 15 * time.Minute,
        Interval: 5 * time.Second,
        Data: &metrics.SpikeData{
            OOMKills:    5,
            SampleCount: 180,
            CPUSamples:  make([]float64, 180),
            MemSamples:  make([]float64, 180),
        },
        CPU:    &metrics.Percentiles{P50: 0.1, P95: 0.15, P99: 0.2, Max: 0.25, Avg: 0.12},
        Memory: &metrics.Percentiles{P50: 1e8, P95: 1.5e8, P99: 1.8e8, Max: 2e8, Avg: 1.2e8},
    }
    rec := Recommend(&RecommendInput{
        Latch:      latch,
        Containers: []ContainerResources{{Name: "app", CPURequest: 0.1, CPULimit: 0.5, MemoryRequest: 1e8, MemoryLimit: 5e8}},
    })
    assert.Equal(t, SafetyRatingUnsafe, rec.Safety)
    assert.Equal(t, ConfidenceLow, rec.Confidence)
    assert.Empty(t, rec.Containers)
    assert.NotEmpty(t, rec.Warnings)
}

func TestRecommend_UnsafeRestartsConfidence(t *testing.T) {
    // When safety is UNSAFE via restarts, Confidence must be LOW
    latch := &LatchResult{
        Valid:    true,
        Duration: 15 * time.Minute,
        Interval: 5 * time.Second,
        Data: &metrics.SpikeData{
            Restarts:    20,
            SampleCount: 180,
            CPUSamples:  make([]float64, 180),
            MemSamples:  make([]float64, 180),
        },
        CPU:    &metrics.Percentiles{P50: 0.1, P95: 0.15, P99: 0.2, Max: 0.25, Avg: 0.12},
        Memory: &metrics.Percentiles{P50: 1e8, P95: 1.5e8, P99: 1.8e8, Max: 2e8, Avg: 1.2e8},
    }
    rec := Recommend(&RecommendInput{
        Latch:      latch,
        Containers: []ContainerResources{{Name: "app", CPURequest: 0.1, CPULimit: 0.5, MemoryRequest: 1e8, MemoryLimit: 5e8}},
    })
    assert.Equal(t, SafetyRatingUnsafe, rec.Safety)
    assert.Equal(t, ConfidenceLow, rec.Confidence)
    assert.Empty(t, rec.Containers)
}

func TestRecommend_UnsafeDetailWarnings(t *testing.T) {
    // UNSAFE with OOMKills should include count in warnings
    latch := &LatchResult{
        Valid:    true,
        Duration: 15 * time.Minute,
        Interval: 5 * time.Second,
        Data: &metrics.SpikeData{
            OOMKills:    7,
            Restarts:    25,
            Evictions:   2,
            SampleCount: 180,
            CPUSamples:  make([]float64, 180),
            MemSamples:  make([]float64, 180),
        },
        CPU:    &metrics.Percentiles{P50: 0.1, P95: 0.15, P99: 0.2, Max: 0.25, Avg: 0.12},
        Memory: &metrics.Percentiles{P50: 1e8, P95: 1.5e8, P99: 1.8e8, Max: 2e8, Avg: 1.2e8},
    }
    rec := Recommend(&RecommendInput{
        Latch:      latch,
        Containers: []ContainerResources{{Name: "app", CPURequest: 0.1, CPULimit: 0.5, MemoryRequest: 1e8, MemoryLimit: 5e8}},
    })
    // Should contain OOMKill count, restart count, eviction count, and UNSAFE message
    found := map[string]bool{"oom": false, "restart": false, "eviction": false, "UNSAFE": false}
    for _, w := range rec.Warnings {
        if strings.Contains(w, "OOMKill") { found["oom"] = true }
        if strings.Contains(w, "restart") { found["restart"] = true }
        if strings.Contains(w, "eviction") { found["eviction"] = true }
        if strings.Contains(w, "UNSAFE") { found["UNSAFE"] = true }
    }
    for key, v := range found {
        assert.True(t, v, "missing warning for %s", key)
    }
}
```

### ui_test.go (new file)

```go
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
    assert.Equal(t, "95Mi", fmtMem(100*1024*1024))
}

func TestFmtDelta(t *testing.T) {
    assert.Equal(t, "+50%", fmtDelta(50))
    assert.Equal(t, "-20%", fmtDelta(-20))
    assert.Equal(t, "0%", fmtDelta(0))
}
```

## Constraints
- Only add test files, do NOT modify source code
- Follow existing patterns (testify assert/require, package promonitor)
- Run `go test -race -cover ./internal/promonitor/...` — all must pass
- Do NOT create documentation files

## Verification
```bash
go test -race -cover ./internal/promonitor/...
```

## Command
```bash
codex exec --full-auto --json --output-last-message /tmp/codex-unsafe-tests.md \
  -C "/path/to/kubenow" \
  "Add tests for UNSAFE recommendation path and UI rendering.

Files to modify:
- internal/promonitor/recommend_test.go — add tests
- internal/promonitor/ui_test.go — create new file

Tests to add in recommend_test.go:
1. TestRecommend_UnsafeConfidence: 5 OOMKills, valid latch with percentiles and 180 samples.
   Assert Safety==UNSAFE, Confidence==LOW, Containers empty, Warnings not empty.
2. TestRecommend_UnsafeRestartsConfidence: 20 restarts triggering UNSAFE. Same asserts.
3. TestRecommend_UnsafeDetailWarnings: 7 OOMKills + 25 restarts + 2 evictions.
   Verify warnings contain counts for each signal type plus UNSAFE message.
   NOTE: needs 'strings' import.

Tests to add in ui_test.go (new file, package promonitor):
1. TestRenderSafetyRating_AllRatings: each rating renders non-empty
2. TestRenderConfidence_AllLevels: each confidence renders non-empty
3. TestRenderRecommendation_Unsafe: UNSAFE rec renders with UNSAFE and LOW
4. TestRenderRecommendation_WithContainers: SAFE rec with one container
5. TestFmtCPU: 0, 0.1, 1.0
6. TestFmtMem: 0, 100MiB
7. TestFmtDelta: positive, negative, zero

Use testify assert, package promonitor (not _test). Import time for LatchEvidence.
Run 'go test -race -cover ./internal/promonitor/...' — all must pass.
Do NOT modify source files. Do NOT create documentation files."
```
