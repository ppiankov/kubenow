package promonitor

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/ppiankov/kubenow/internal/audit"
	"github.com/ppiankov/kubenow/internal/exposure"
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
	workload     WorkloadRef
	operatorType string // CRD operator type (e.g. "CNPG", "Strimzi"), empty for standard workloads
	hpaInfo      *HPAInfo
	mode         Mode
	policyMsg    string // Short policy status line

	// Latch state
	latch         *metrics.LatchMonitor
	latchDuration time.Duration
	latchInterval time.Duration
	latchStart    time.Time
	latchDone     bool
	sampleCount   int

	// Early-stop state (Esc during latch)
	earlyStopPending bool          // first Esc pressed, awaiting confirmation
	earlyStopActual  time.Duration // actual elapsed duration if stopped early (zero = no early stop)

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

	// Exposure map state (triggered by 'l' key)
	exposureCollector *exposure.ExposureCollector
	exposureMap       *exposure.ExposureMap
	showExposure      bool
	exposureLoading   bool

	// Traffic map state (triggered by 't' key)
	trafficMap     *exposure.TrafficMap
	showTraffic    bool
	trafficLoading bool

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

// exposureDoneMsg carries the exposure map query result.
type exposureDoneMsg struct {
	m   *exposure.ExposureMap
	err error
}

// trafficDoneMsg carries the Linkerd traffic map query result.
type trafficDoneMsg struct {
	m   *exposure.TrafficMap
	err error
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

// NewAnalyzeModel creates a TUI model for analyzing existing latch data.
// Starts in post-latch state with the recommendation already computed.
func NewAnalyzeModel(ref WorkloadRef, mode Mode, policyMsg string, hpa *HPAInfo, rec *AlignmentRecommendation, latchResult *LatchResult) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	m := Model{
		workload:       ref,
		hpaInfo:        hpa,
		mode:           mode,
		policyMsg:      policyMsg,
		latchDone:      true,
		recommendation: rec,
		spinner:        s,
	}
	if latchResult != nil {
		m.latchDuration = latchResult.Duration
		m.latchTimestamp = latchResult.Timestamp
		if latchResult.PlannedDuration > 0 {
			// Was early-stopped: Duration is actual, PlannedDuration is original
			m.earlyStopActual = latchResult.Duration
			m.latchDuration = latchResult.PlannedDuration
		}
		if latchResult.Data != nil {
			m.sampleCount = latchResult.Data.SampleCount
		}
	}
	return m
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
		// Any non-Esc key dismisses the early-stop warning
		if m.earlyStopPending && msg.String() != "esc" && msg.String() != "q" && msg.String() != "ctrl+c" {
			m.earlyStopPending = false
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			if m.latch != nil && !m.latchDone {
				m.latch.Stop()
			}
			return m, tea.Quit
		case "esc":
			if !m.latchDone && m.latch != nil {
				if m.earlyStopPending {
					// Second Esc — stop the latch and proceed with collected data
					m.earlyStopActual = time.Since(m.latchStart)
					m.latch.Stop()
					// latch.Stop() blocks until done; LatchDoneMsg will arrive via goroutine
					m.earlyStopPending = false
					return m, nil
				}
				// First Esc — show warning
				m.earlyStopPending = true
				return m, nil
			}
		case "e":
			if m.recommendation != nil && !m.exported {
				rec := m.recommendation
				workload := m.workload
				return m, func() tea.Msg {
					path, err := ExportToFile(rec, workload)
					return exportDoneMsg{path: path, err: err}
				}
			}
		case "l":
			if m.recommendation != nil {
				if m.showExposure {
					m.showExposure = false
					return m, nil
				}
				m.showTraffic = false // mutually exclusive overlays
				if m.exposureMap != nil {
					m.showExposure = true
					return m, nil
				}
				if !m.exposureLoading && m.exposureCollector != nil {
					m.exposureLoading = true
					m.showExposure = true
					ref := m.workload
					return m, func() tea.Msg {
						ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						result, err := m.exposureCollector.Collect(ctx, ref.Namespace, ref.Name, ref.Kind)
						return exposureDoneMsg{m: result, err: err}
					}
				}
			}
		case "t":
			if m.recommendation != nil && m.exposureCollector != nil && m.exposureCollector.HasPrometheus() {
				if m.showTraffic {
					m.showTraffic = false
					return m, nil
				}
				m.showExposure = false // mutually exclusive overlays
				if m.trafficMap != nil {
					m.showTraffic = true
					return m, nil
				}
				if !m.trafficLoading {
					m.trafficLoading = true
					m.showTraffic = true
					ref := m.workload
					return m, func() tea.Msg {
						ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
						defer cancel()
						result, err := m.exposureCollector.CollectTrafficMap(ctx, ref.Namespace, ref.Name)
						return trafficDoneMsg{m: result, err: err}
					}
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
		// Final sample count update and operator type extraction
		if m.latch != nil {
			data := m.latch.GetSpikeData()
			total := 0
			for _, d := range data {
				if d.SampleCount > total {
					total = d.SampleCount
				}
				if d.OperatorType != "" && m.operatorType == "" {
					m.operatorType = d.OperatorType
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

	case exposureDoneMsg:
		m.exposureLoading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.exposureMap = msg.m
		}
		return m, nil

	case trafficDoneMsg:
		m.trafficLoading = false
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.trafficMap = msg.m
		}
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
	input := &ApplyInput{
		Recommendation:  m.recommendation,
		Workload:        m.workload,
		Mode:            m.mode,
		Policy:          m.policy,
		HPAInfo:         m.hpaInfo,
		HPAAcknowledged: m.hpaAcknowledged,
		LatchTimestamp:  m.latchTimestamp,
		LatchDuration:   m.latchDuration,
	}

	// Resolve audit/identity/rate-limit flags for pre-flight checks.
	// Without this, CheckActionable always denies (flags default to false).
	if m.auditPath != "" && m.fullPolicy != nil {
		input.AuditWritable = os.MkdirAll(m.auditPath, 0755) == nil

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		identity := audit.ResolveIdentity(ctx, m.kubeClient, m.kubeconfigPath)
		input.IdentityRecorded = identity.IdentitySource != "unknown"

		peekResult, _ := audit.Peek(audit.RateLimitConfig{
			MaxGlobal: m.fullPolicy.RateLimits.MaxAppliesPerHour,
			Window:    m.fullPolicy.RateWindowParsed(),
			AuditPath: m.auditPath,
		})
		if peekResult != nil {
			input.RateLimitOK = peekResult.Allowed
		} else {
			input.RateLimitOK = true
		}
	} else {
		// No policy/audit configured — no gates to enforce
		input.AuditWritable = true
		input.IdentityRecorded = true
		input.RateLimitOK = true
	}

	return input
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
				Version:        "0.3.0",
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

// SetExposureCollector sets the collector for the exposure map feature.
func (m *Model) SetExposureCollector(c *exposure.ExposureCollector) {
	m.exposureCollector = c
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
	plannedDuration := m.latchDuration
	actualDuration := m.earlyStopActual // zero if not early-stopped
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
		// If early-stopped, use actual elapsed time as the duration
		effectiveDuration := plannedDuration
		if actualDuration > 0 {
			effectiveDuration = actualDuration
		}
		latchResult := BuildLatchResult(workload, data, effectiveDuration, interval)
		if actualDuration > 0 {
			latchResult.PlannedDuration = plannedDuration
		}
		_ = SaveLatch(latchResult) // best-effort persistence

		// Run recommendation engine
		rec := Recommend(&RecommendInput{
			Latch:      latchResult,
			Containers: containers,
			Bounds:     bounds,
			HPA:        hpa,
		})

		// Add early-stop warning
		if actualDuration > 0 {
			rec.Warnings = append(rec.Warnings, fmt.Sprintf(
				"latch stopped early: %s of planned %s (%.0f%%)",
				formatDuration(actualDuration), plannedDuration.String(),
				float64(actualDuration)/float64(plannedDuration)*100,
			))
		}

		return recommendDoneMsg{rec: rec}
	}
}
