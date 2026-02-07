package promonitor

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ppiankov/kubenow/internal/metrics"
)

// Mode describes what operations the policy allows.
type Mode int

const (
	ModeObserveOnly Mode = iota // No policy or global.enabled=false
	ModeExportOnly              // Policy present, apply.enabled=false
	ModeApplyReady              // Policy present, apply.enabled=true
)

// Model is the bubbletea model for the pro-monitor TUI.
type Model struct {
	// Workload info
	workload  WorkloadRef
	hpaInfo   *HPAInfo
	mode      Mode
	policyMsg string // Short policy status line

	// Latch state
	latch         *metrics.LatchMonitor
	latchDuration time.Duration
	latchStart    time.Time
	latchDone     bool
	sampleCount   int

	// UI state
	spinner  spinner.Model
	width    int
	height   int
	quitting bool
	err      error
}

// tickMsg fires every second for progress updates.
type tickMsg time.Time

// latchDoneMsg signals latch completion.
type latchDoneMsg struct{ err error }

// NewModel creates a new pro-monitor TUI model.
func NewModel(ref WorkloadRef, latch *metrics.LatchMonitor, duration time.Duration, mode Mode, policyMsg string, hpa *HPAInfo) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	return Model{
		workload:      ref,
		hpaInfo:       hpa,
		mode:          mode,
		policyMsg:     policyMsg,
		latch:         latch,
		latchDuration: duration,
		spinner:       s,
	}
}

// Init starts the bubbletea program.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		tickCmd(),
	)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			if m.latch != nil && !m.latchDone {
				m.latch.Stop()
			}
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		if m.latch != nil && !m.latchDone {
			// Count samples from latch data
			data := m.latch.GetSpikeData()
			total := 0
			for _, d := range data {
				if d.SampleCount > total {
					total = d.SampleCount
				}
			}
			m.sampleCount = total
		}
		return m, tickCmd()

	case latchDoneMsg:
		m.latchDone = true
		if msg.err != nil {
			m.err = msg.err
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// View renders the TUI — delegated to ui.go.
func (m Model) View() string {
	return renderView(m)
}

// SetLatchStart records when the latch started.
func (m *Model) SetLatchStart(t time.Time) {
	m.latchStart = t
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
