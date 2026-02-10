package promonitor

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ppiankov/kubenow/internal/metrics"
	"github.com/stretchr/testify/assert"
)

func TestNewModel(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 15*time.Minute, ModeExportOnly, "test policy", nil)

	assert.Equal(t, "api", m.workload.Name)
	assert.Equal(t, ModeExportOnly, m.mode)
	assert.Equal(t, "test policy", m.policyMsg)
	assert.False(t, m.latchDone)
	assert.False(t, m.quitting)
}

func TestModel_Init(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 15*time.Minute, ModeObserveOnly, "none", nil)

	cmd := m.Init()
	assert.NotNil(t, cmd)
}

func TestModel_Update_Quit(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 15*time.Minute, ModeObserveOnly, "none", nil)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model := updated.(Model)
	assert.True(t, model.quitting)
	assert.NotNil(t, cmd) // tea.Quit
}

func TestModel_Update_WindowSize(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 15*time.Minute, ModeObserveOnly, "none", nil)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := updated.(Model)
	assert.Equal(t, 120, model.width)
	assert.Equal(t, 40, model.height)
}

func TestModel_View_Quitting(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 15*time.Minute, ModeObserveOnly, "none", nil)
	m.quitting = true

	view := m.View()
	assert.Equal(t, "Pro-monitor stopped.\n", view)
}

func TestModel_View_WithHPA(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	hpa := &HPAInfo{Name: "api-hpa", MinReplica: 2, MaxReplica: 10}
	m := NewModel(ref, nil, 15*time.Minute, ModeApplyReady, "loaded", hpa)
	m.width = 100
	m.height = 40

	view := m.View()
	assert.Contains(t, view, "HPA detected")
	assert.Contains(t, view, "api-hpa")
}

func TestRenderModeTag(t *testing.T) {
	tests := []struct {
		mode Mode
		want string
	}{
		{ModeObserveOnly, "OBSERVE ONLY"},
		{ModeExportOnly, "SUGGEST + EXPORT"},
		{ModeApplyReady, "SUGGEST + EXPORT + APPLY"},
	}

	for _, tt := range tests {
		tag := renderModeTag(tt.mode)
		assert.Contains(t, tag, tt.want)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m30s"},
		{65 * time.Minute, "1h5m"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, formatDuration(tt.d))
	}
}

func TestModel_Update_LatchDone(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 15*time.Minute, ModeObserveOnly, "none", nil)

	updated, cmd := m.Update(LatchDoneMsg{Err: nil})
	model := updated.(Model)
	assert.True(t, model.latchDone)
	assert.True(t, model.computing)
	assert.NotNil(t, cmd) // computeRecommendationCmd
}

func TestModel_Update_RecommendDone(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 15*time.Minute, ModeObserveOnly, "none", nil)
	m.computing = true

	rec := &AlignmentRecommendation{
		Safety:     SafetyRatingSafe,
		Confidence: ConfidenceLow,
	}
	updated, _ := m.Update(recommendDoneMsg{rec: rec})
	model := updated.(Model)
	assert.False(t, model.computing)
	assert.NotNil(t, model.recommendation)
	assert.Equal(t, SafetyRatingSafe, model.recommendation.Safety)
}

func TestModel_View_WithRecommendation(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 15*time.Minute, ModeExportOnly, "test", nil)
	m.latchDone = true
	m.width = 100
	m.height = 40
	m.recommendation = &AlignmentRecommendation{
		Safety:     SafetyRatingCaution,
		Confidence: ConfidenceLow,
		Containers: []ContainerAlignment{
			{
				Name:        "api",
				Current:     ResourceValues{CPURequest: 0.1, CPULimit: 0.5, MemoryRequest: 128e6, MemoryLimit: 512e6},
				Recommended: ResourceValues{CPURequest: 0.13, CPULimit: 0.35, MemoryRequest: 200e6, MemoryLimit: 400e6},
				Delta:       ResourceDelta{CPURequestPercent: 30, CPULimitPercent: -30, MemoryRequestPercent: 56, MemoryLimitPercent: -22},
			},
		},
	}

	view := m.View()
	assert.Contains(t, view, "Recommendation")
	assert.Contains(t, view, "CAUTION")
	assert.Contains(t, view, "Container: api")
	assert.Contains(t, view, "e: export")
}

func TestModel_Update_EscFirstPress_ShowsWarning(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 2*time.Hour, ModeObserveOnly, "none", nil)
	m.latchStart = time.Now()
	// latch is nil but latchDone is false — Esc requires latch != nil
	// Simulate by checking that with nil latch, Esc is a no-op
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model := updated.(Model)
	assert.False(t, model.earlyStopPending)
}

func TestModel_Update_EscDismissedByOtherKey(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 2*time.Hour, ModeObserveOnly, "none", nil)
	m.earlyStopPending = true

	// Any non-esc/q/ctrl+c key should dismiss the warning
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model := updated.(Model)
	assert.False(t, model.earlyStopPending)
}

func TestModel_View_EarlyStopWarning(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 2*time.Hour, ModeObserveOnly, "none", nil)
	m.earlyStopPending = true
	m.latchStart = time.Now()
	m.width = 120
	m.height = 40

	view := m.View()
	assert.Contains(t, view, "stop latching")
}

func TestModel_View_EarlyStopComplete(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 2*time.Hour, ModeObserveOnly, "none", nil)
	m.latchDone = true
	m.earlyStopActual = 93 * time.Minute
	m.sampleCount = 1116
	m.width = 120
	m.height = 40

	view := m.View()
	assert.Contains(t, view, "EARLY STOP")
	assert.Contains(t, view, "planned 2h0m0s")
}

func TestModel_View_EscHintDuringLatch(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 15*time.Minute, ModeObserveOnly, "none", nil)
	m.latchStart = time.Now()
	// latch is nil so "esc: stop early" won't show — test with non-nil would need mock
	m.width = 100
	m.height = 40

	view := m.View()
	// Without a latch monitor, the hint shouldn't appear
	assert.NotContains(t, view, "esc: stop early")
}

func TestModel_Update_ExportKey_NoRecommendation(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 15*time.Minute, ModeObserveOnly, "none", nil)

	// 'e' before recommendation should be a no-op
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	assert.Nil(t, cmd)
}

func TestModel_Update_ExportKey_WithRecommendation(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 15*time.Minute, ModeExportOnly, "test", nil)
	m.recommendation = &AlignmentRecommendation{
		Workload:   ref,
		Safety:     SafetyRatingSafe,
		Confidence: ConfidenceHigh,
	}

	// 'e' with recommendation should return a command
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	assert.NotNil(t, cmd)
}

func TestModel_Update_ExportDone(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 15*time.Minute, ModeExportOnly, "test", nil)

	updated, _ := m.Update(exportDoneMsg{path: "/tmp/test.yaml", err: nil})
	model := updated.(Model)
	assert.True(t, model.exported)
	assert.Equal(t, "/tmp/test.yaml", model.exportPath)
	assert.Nil(t, model.exportError)
}

func TestModel_View_ExportedStatus(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	m := NewModel(ref, nil, 15*time.Minute, ModeExportOnly, "test", nil)
	m.width = 100
	m.height = 40
	m.exported = true
	m.exportPath = "kubenow-patch-deployment-api.yaml"
	m.recommendation = &AlignmentRecommendation{
		Safety:     SafetyRatingSafe,
		Confidence: ConfidenceHigh,
	}

	view := m.View()
	assert.Contains(t, view, "Exported to")
	assert.Contains(t, view, "kubenow-patch-deployment-api.yaml")
	// Export key should not be shown after export
	assert.NotContains(t, view, "e: export")
}

func TestNewAnalyzeModel(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "payment-api", Namespace: "prod"}
	rec := &AlignmentRecommendation{
		Safety:     SafetyRatingSafe,
		Confidence: ConfidenceHigh,
	}
	latch := &LatchResult{
		Workload:  ref,
		Duration:  30 * time.Minute,
		Timestamp: time.Now().Add(-1 * time.Hour),
		Data:      &metrics.SpikeData{SampleCount: 360},
		Valid:     true,
	}

	m := NewAnalyzeModel(ref, ModeExportOnly, "policy loaded", nil, rec, latch)

	assert.True(t, m.latchDone, "analyze model starts post-latch")
	assert.NotNil(t, m.recommendation)
	assert.Equal(t, SafetyRatingSafe, m.recommendation.Safety)
	assert.Equal(t, ModeExportOnly, m.mode)
	assert.Equal(t, "policy loaded", m.policyMsg)
	assert.Equal(t, 30*time.Minute, m.latchDuration)
	assert.Equal(t, 360, m.sampleCount)
	assert.Equal(t, time.Duration(0), m.earlyStopActual, "no early stop for full run")
}

func TestNewAnalyzeModel_EarlyStop(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "payment-api", Namespace: "prod"}
	rec := &AlignmentRecommendation{
		Safety:     SafetyRatingCaution,
		Confidence: ConfidenceLow,
	}
	latch := &LatchResult{
		Workload:        ref,
		Duration:        45 * time.Minute, // actual elapsed
		PlannedDuration: 2 * time.Hour,    // original target
		Timestamp:       time.Now().Add(-2 * time.Hour),
		Data:            &metrics.SpikeData{SampleCount: 540},
		Valid:           true,
	}

	m := NewAnalyzeModel(ref, ModeApplyReady, "apply ready", nil, rec, latch)

	assert.True(t, m.latchDone)
	assert.Equal(t, 2*time.Hour, m.latchDuration, "latchDuration reflects planned duration")
	assert.Equal(t, 45*time.Minute, m.earlyStopActual, "earlyStopActual reflects actual elapsed")
	assert.Equal(t, 540, m.sampleCount)
}

func TestNewAnalyzeModel_NilLatch(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	rec := &AlignmentRecommendation{Safety: SafetyRatingSafe}

	m := NewAnalyzeModel(ref, ModeObserveOnly, "none", nil, rec, nil)

	assert.True(t, m.latchDone)
	assert.NotNil(t, m.recommendation)
	assert.Equal(t, time.Duration(0), m.latchDuration)
	assert.Equal(t, 0, m.sampleCount)
}

func TestNewAnalyzeModel_WithHPA(t *testing.T) {
	ref := WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"}
	hpa := &HPAInfo{Name: "api-hpa", MinReplica: 1, MaxReplica: 5}
	rec := &AlignmentRecommendation{Safety: SafetyRatingSafe}
	latch := &LatchResult{
		Duration: 15 * time.Minute,
		Data:     &metrics.SpikeData{SampleCount: 180},
		Valid:    true,
	}

	m := NewAnalyzeModel(ref, ModeApplyReady, "loaded", hpa, rec, latch)

	assert.True(t, m.latchDone)
	assert.NotNil(t, m.hpaInfo)
	assert.Equal(t, "api-hpa", m.hpaInfo.Name)
}
