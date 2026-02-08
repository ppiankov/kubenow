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
	latchInterval time.Duration
	latchStart    time.Time
	latchDone     bool
	sampleCount   int

	// Recommendation inputs (set before TUI starts)
	containers   []ContainerResources
	policyBounds *PolicyBounds

	// Recommendation output (set after latch completes)
	recommendation *AlignmentRecommendation
	computing      bool // true while recommendation is being computed

	// UI state
	spinner  spinner.Model
	width    int
	height   int
	quitting bool
	err      error
}

// tickMsg fires every second for progress updates.
type tickMsg time.Time

// LatchDoneMsg signals latch completion. Exported so the CLI can send it via p.Send.
type LatchDoneMsg struct{ Err error }

// recommendDoneMsg carries the computed recommendation back to the model.
type recommendDoneMsg struct {
	rec *AlignmentRecommendation
}

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

	case LatchDoneMsg:
		m.latchDone = true
		if msg.Err != nil {
			m.err = msg.Err
			return m, nil
		}
		// Final sample count update
		if m.latch != nil {
			data := m.latch.GetSpikeData()
			total := 0
			for _, d := range data {
				if d.SampleCount > total {
					total = d.SampleCount
				}
			}
			m.sampleCount = total
		}
		m.computing = true
		return m, m.computeRecommendationCmd()

	case recommendDoneMsg:
		m.computing = false
		m.recommendation = msg.rec
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

// SetContainers sets the current container resources for recommendation.
func (m *Model) SetContainers(c []ContainerResources) {
	m.containers = c
}

// SetPolicyBounds sets the policy guardrails for recommendation.
func (m *Model) SetPolicyBounds(b *PolicyBounds) {
	m.policyBounds = b
}

// SetInterval sets the sample interval for latch result computation.
func (m *Model) SetInterval(d time.Duration) {
	m.latchInterval = d
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// computeRecommendationCmd returns a Cmd that builds the latch result,
// persists it, and runs the recommendation algorithm.
func (m Model) computeRecommendationCmd() tea.Cmd {
	latch := m.latch
	workload := m.workload
	duration := m.latchDuration
	interval := m.latchInterval
	containers := m.containers
	bounds := m.policyBounds
	hpa := m.hpaInfo

	return func() tea.Msg {
		// Get latch data for the target workload
		var data *metrics.SpikeData
		if latch != nil {
			data = latch.GetWorkloadSpikeData(workload.Namespace, workload.Name)
		}

		// Build and persist latch result
		latchResult := BuildLatchResult(workload, data, duration, interval)
		_ = SaveLatch(latchResult) // best-effort persistence

		// Run recommendation engine
		rec := Recommend(&RecommendInput{
			Latch:      latchResult,
			Containers: containers,
			Bounds:     bounds,
			HPA:        hpa,
		})

		return recommendDoneMsg{rec: rec}
	}
}
