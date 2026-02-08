package promonitor

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ppiankov/kubenow/internal/audit"
	"github.com/ppiankov/kubenow/internal/metrics"
	"github.com/ppiankov/kubenow/internal/policy"
	"k8s.io/client-go/kubernetes"
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

	// Export state
	exported    bool   // true after successful export
	exportPath  string // path to exported file
	exportError error  // export error if any

	// Apply state
	confirming      bool            // true when confirmation prompt is active
	confirmInput    textinput.Model // textinput for "apply" confirmation
	applying        bool            // true while SSA patch is in flight
	applyResult     *ApplyResult    // set after apply completes
	hpaAcknowledged bool            // set via --acknowledge-hpa
	kubeApplier     KubeApplier     // K8s client for SSA apply
	policy          *PolicyBounds   // policy bounds for apply checks
	latchTimestamp  time.Time       // when latch completed (for freshness check)

	// Audit state
	auditPath      string
	fullPolicy     *policy.Policy
	kubeconfigPath string
	kubeClient     kubernetes.Interface

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

// exportDoneMsg signals that the TUI export completed.
type exportDoneMsg struct {
	path string
	err  error
}

// applyDoneMsg carries the apply result back to the model.
type applyDoneMsg struct {
	result *ApplyResult
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
	// When confirming, route input to the textinput first
	if m.confirming {
		return m.updateConfirming(msg)
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			if m.latch != nil && !m.latchDone {
				m.latch.Stop()
			}
			return m, tea.Quit
		case "e":
			if m.recommendation != nil && !m.exported {
				rec := m.recommendation
				workload := m.workload
				return m, func() tea.Msg {
					path, err := ExportToFile(rec, workload)
					return exportDoneMsg{path: path, err: err}
				}
			}
		case "a":
			if m.recommendation != nil && m.mode == ModeApplyReady && m.applyResult == nil && !m.applying {
				// Run pre-flight checks before showing confirmation
				input := m.buildApplyInput()
				reasons := CheckActionable(input)
				if len(reasons) > 0 {
					m.applyResult = &ApplyResult{DenialReasons: reasons}
					return m, nil
				}
				// Enter confirmation mode
				ti := textinput.New()
				ti.Placeholder = `type "apply" to confirm`
				ti.Focus()
				ti.CharLimit = 10
				m.confirmInput = ti
				m.confirming = true
				return m, ti.Focus()
			}
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
		m.latchTimestamp = time.Now()
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

	case exportDoneMsg:
		if msg.err != nil {
			m.exportError = msg.err
		} else {
			m.exported = true
			m.exportPath = msg.path
		}
		return m, nil

	case applyDoneMsg:
		m.applying = false
		m.applyResult = msg.result
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// updateConfirming handles input while the confirmation prompt is active.
func (m Model) updateConfirming(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			if m.confirmInput.Value() == "apply" {
				m.confirming = false
				m.applying = true
				return m, m.executeApplyCmd()
			}
			// Wrong input — cancel
			m.confirming = false
			return m, nil
		case tea.KeyEsc:
			m.confirming = false
			return m, nil
		case tea.KeyCtrlC:
			m.quitting = true
			return m, tea.Quit
		}
	}

	// Forward to textinput
	var cmd tea.Cmd
	m.confirmInput, cmd = m.confirmInput.Update(msg)
	return m, cmd
}

// buildApplyInput assembles the ApplyInput from model state.
func (m Model) buildApplyInput() *ApplyInput {
	return &ApplyInput{
		Recommendation:  m.recommendation,
		Workload:        m.workload,
		Mode:            m.mode,
		Policy:          m.policy,
		HPAInfo:         m.hpaInfo,
		HPAAcknowledged: m.hpaAcknowledged,
		LatchTimestamp:  m.latchTimestamp,
		LatchDuration:   m.latchDuration,
	}
}

// executeApplyCmd returns a Cmd that runs the SSA apply in a goroutine.
func (m Model) executeApplyCmd() tea.Cmd {
	client := m.kubeApplier
	input := m.buildApplyInput()
	auditPath := m.auditPath
	fullPolicy := m.fullPolicy
	kubeconfigPath := m.kubeconfigPath
	kubeClient := m.kubeClient

	return func() tea.Msg {
		var result *ApplyResult
		if auditPath != "" && fullPolicy != nil {
			cfg := &AuditApplyConfig{
				AuditPath:      auditPath,
				Client:         client,
				KubeClient:     kubeClient,
				KubeconfigPath: kubeconfigPath,
				Input:          input,
				Version:        "0.2.0",
				FullPolicy:     fullPolicy,
				RateLimitCfg: audit.RateLimitConfig{
					MaxGlobal:      fullPolicy.RateLimits.MaxAppliesPerHour,
					MaxPerWorkload: fullPolicy.RateLimits.MaxAppliesPerWorkload,
					Window:         fullPolicy.RateWindowParsed(),
					AuditPath:      auditPath,
				},
			}
			result = ExecuteApplyWithAudit(context.Background(), cfg)
		} else {
			result = ExecuteApply(context.Background(), client, input)
		}
		return applyDoneMsg{result: result}
	}
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

// SetKubeApplier sets the Kubernetes client for SSA apply.
func (m *Model) SetKubeApplier(a KubeApplier) {
	m.kubeApplier = a
}

// SetPolicy sets the policy bounds for apply checks.
func (m *Model) SetPolicy(p *PolicyBounds) {
	m.policy = p
}

// SetHPAAcknowledged sets whether the user acknowledged HPA presence.
func (m *Model) SetHPAAcknowledged(ack bool) {
	m.hpaAcknowledged = ack
}

// SetAuditPath sets the audit bundle output path.
func (m *Model) SetAuditPath(path string) {
	m.auditPath = path
}

// SetFullPolicy sets the full loaded policy for audit config.
func (m *Model) SetFullPolicy(p *policy.Policy) {
	m.fullPolicy = p
}

// SetKubeconfigPath sets the kubeconfig path for identity resolution.
func (m *Model) SetKubeconfigPath(path string) {
	m.kubeconfigPath = path
}

// SetKubeClient sets the Kubernetes client for identity resolution.
func (m *Model) SetKubeClient(client kubernetes.Interface) {
	m.kubeClient = client
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
