package promonitor

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
