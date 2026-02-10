package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ppiankov/kubenow/internal/exposure"
	"github.com/ppiankov/kubenow/internal/metrics"
	"github.com/ppiankov/kubenow/internal/policy"
	"github.com/ppiankov/kubenow/internal/promonitor"
	"github.com/ppiankov/kubenow/internal/util"
	"github.com/spf13/cobra"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

var latchConfig struct {
	duration       string
	interval       string
	acknowledgeHPA bool
	prometheusURL  string
}

var latchCmd = &cobra.Command{
	Use:   "latch <kind>/<name>",
	Short: "Start high-resolution resource sampling for a workload",
	Long: `Latch onto a single workload for high-resolution resource sampling.

This command connects to the Kubernetes Metrics API and samples the target
workload's resource usage at a configurable interval (default 5s). The latch
runs for a specified duration (default 15m), collecting CPU and memory samples.

The red-framed TUI shows:
  - Workload identity and namespace
  - Latch progress (sample count, elapsed/total time)
  - HPA warning if detected (blocks apply unless acknowledged)
  - Policy status (observe-only, suggest+export, or suggest+export+apply)

After the latch completes, a resource alignment recommendation is computed
and displayed with before/after values, safety rating, and confidence level.

Examples:
  # Latch a deployment for 15 minutes
  kubenow pro-monitor latch deployment/payment-api -n default

  # Latch for 1 hour with 1-second samples
  kubenow pro-monitor latch deployment/api-server -n prod --duration 1h --interval 1s

  # Latch a statefulset
  kubenow pro-monitor latch statefulset/postgres -n databases --duration 30m

  # Latch with Linkerd traffic source measurement
  kubenow pro-monitor latch deployment/payment-api -n prod --prometheus-url http://prometheus:9090`,
	Args: cobra.ExactArgs(1),
	RunE: runLatch,
}

func init() {
	proMonitorCmd.AddCommand(latchCmd)
	latchCmd.Flags().StringVar(&latchConfig.duration, "duration", "15m", "latch duration (e.g., 15m, 1h, 24h)")
	latchCmd.Flags().StringVar(&latchConfig.interval, "interval", "5s", "sample interval (e.g., 1s, 5s)")
	latchCmd.Flags().BoolVar(&latchConfig.acknowledgeHPA, "acknowledge-hpa", false, "acknowledge HPA presence and allow apply despite HPA")
	latchCmd.Flags().StringVar(&latchConfig.prometheusURL, "prometheus-url", "", "Prometheus endpoint for Linkerd traffic metrics (e.g., http://prometheus:9090)")
}

func runLatch(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Parse workload reference
	ref, err := promonitor.ParseWorkloadRef(args[0])
	if err != nil {
		return err
	}

	// Set namespace
	ns := GetNamespace()
	if ns == "" {
		ns = "default"
	}
	ref.Namespace = ns

	// Parse durations
	duration, err := time.ParseDuration(latchConfig.duration)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", latchConfig.duration, err)
	}

	interval, err := time.ParseDuration(latchConfig.interval)
	if err != nil {
		return fmt.Errorf("invalid interval %q: %w", latchConfig.interval, err)
	}

	if IsVerbose() {
		fmt.Fprintf(os.Stderr, "[pro-monitor] Target: %s in namespace %s\n", ref.String(), ref.Namespace)
		fmt.Fprintf(os.Stderr, "[pro-monitor] Duration: %s, Interval: %s\n", duration, interval)
	}

	// Build K8s clients
	opts := GetKubeOpts()
	kubeClient, err := util.BuildKubeClientWithOpts(opts)
	if err != nil {
		return fmt.Errorf("failed to build Kubernetes client: %w", err)
	}

	restConfig, err := util.BuildRestConfigWithOpts(opts)
	if err != nil {
		return fmt.Errorf("failed to build REST config: %w", err)
	}

	metricsClient, err := metricsclientset.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to build metrics client: %w", err)
	}

	// Validate workload exists
	if err := promonitor.ValidateWorkload(ctx, kubeClient, ref); err != nil {
		return err
	}

	if IsVerbose() {
		fmt.Fprintf(os.Stderr, "[pro-monitor] Workload validated: %s\n", ref.String())
	}

	// Check metrics-server
	if err := promonitor.CheckMetricsServer(ctx, metricsClient, ref.Namespace); err != nil {
		return fmt.Errorf("metrics-server required for latch: %w", err)
	}

	if IsVerbose() {
		fmt.Fprintf(os.Stderr, "[pro-monitor] Metrics-server available\n")
	}

	// Detect HPA
	hpa := promonitor.DetectHPA(ctx, kubeClient, ref)
	if hpa != nil {
		fmt.Fprintf(os.Stderr, "[pro-monitor] WARNING: HPA %q targets this workload (min=%d, max=%d)\n",
			hpa.Name, hpa.MinReplica, hpa.MaxReplica)
		fmt.Fprintf(os.Stderr, "[pro-monitor] Apply will be blocked unless --acknowledge-hpa is passed.\n")
	}

	// Load policy
	mode, policyMsg, bounds, loadedPolicy := resolveMode(policyPath, ref)

	// Pre-fetch current container resources for recommendation
	containers, err := promonitor.FetchContainerResources(ctx, kubeClient, ref)
	if err != nil {
		// Non-fatal: recommendation will still run but without current values
		if IsVerbose() {
			fmt.Fprintf(os.Stderr, "[pro-monitor] Warning: could not read container resources: %v\n", err)
		}
	}

	// Create latch monitor (filtered to target workload).
	// ProgressFunc is a no-op because the bubbletea TUI renders its own
	// progress bar; writing to stderr would corrupt the alternate screen.
	latchMon, err := metrics.NewLatchMonitor(kubeClient, metrics.LatchConfig{
		SampleInterval: interval,
		Duration:       duration,
		Namespaces:     []string{ref.Namespace},
		WorkloadFilter: ref.Name,
		PodLevel:       ref.Kind == "Pod",
		ProgressFunc:   func(string) {},
	}, opts)
	if err != nil {
		return fmt.Errorf("failed to create latch monitor: %w", err)
	}

	// Create TUI model with recommendation inputs
	model := promonitor.NewModel(*ref, latchMon, duration, mode, policyMsg, hpa)
	model.SetLatchStart(time.Now())
	model.SetInterval(interval)
	model.SetContainers(containers)
	if bounds != nil {
		model.SetPolicyBounds(bounds)
	}

	// Wire apply infrastructure
	if mode == promonitor.ModeApplyReady {
		model.SetKubeApplier(&promonitor.ClientsetApplier{Client: kubeClient})
		// Extend bounds with parsed durations from the full policy
		if bounds != nil && loadedPolicy != nil {
			bounds.MaxLatchAge = loadedPolicy.MaxLatchAgeParsed()
			bounds.MinLatchDuration = loadedPolicy.MinLatchDurationParsed()
		}
		model.SetPolicy(bounds)
	}
	// Wire exposure map (load sources + optional Linkerd traffic)
	exposureCollector := exposure.NewExposureCollector(kubeClient, metricsClient)
	if latchConfig.prometheusURL != "" {
		promClient, err := metrics.NewPrometheusClient(metrics.Config{PrometheusURL: latchConfig.prometheusURL})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[pro-monitor] Warning: could not connect to Prometheus: %v\n", err)
		} else {
			exposureCollector.SetPrometheusAPI(promClient.GetAPI())
			if IsVerbose() {
				fmt.Fprintf(os.Stderr, "[pro-monitor] Linkerd traffic metrics enabled via %s\n", latchConfig.prometheusURL)
			}
		}
	}
	model.SetExposureCollector(exposureCollector)

	// Wire audit infrastructure
	if loadedPolicy != nil && loadedPolicy.Audit.Path != "" {
		model.SetAuditPath(loadedPolicy.Audit.Path)
		model.SetFullPolicy(loadedPolicy)
		model.SetKubeconfigPath(GetKubeconfig())
		model.SetKubeClient(kubeClient)
	}

	model.SetHPAAcknowledged(latchConfig.acknowledgeHPA)

	// Create the TUI program first, then start the latch goroutine
	// so it can signal completion via p.Send
	latchCtx, latchCancel := context.WithCancel(ctx)
	defer latchCancel()

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	go func() {
		err := latchMon.Start(latchCtx)
		p.Send(promonitor.LatchDoneMsg{Err: err})
	}()

	if _, err := p.Run(); err != nil {
		latchCancel()
		return fmt.Errorf("tui error: %w", err)
	}

	latchCancel()
	return nil
}

// resolveMode loads the policy and determines the operating mode.
// Returns the mode, a human-readable status message, optional policy bounds,
// and the loaded policy (nil if absent/invalid).
func resolveMode(policyFile string, ref *promonitor.WorkloadRef) (promonitor.Mode, string, *promonitor.PolicyBounds, *policy.Policy) {
	result := policy.Load(policyFile)

	if result.Absent {
		return promonitor.ModeObserveOnly, fmt.Sprintf("none (%s)", result.Path), nil, nil
	}

	if result.ErrorMsg != "" {
		return promonitor.ModeObserveOnly, fmt.Sprintf("invalid: %s", result.ErrorMsg), nil, nil
	}

	p := result.Policy

	vr := policy.Validate(p)
	if !vr.Valid {
		return promonitor.ModeObserveOnly, fmt.Sprintf("validation failed (%d errors)", len(vr.Errors)), nil, nil
	}

	// Extract policy bounds for recommendation engine
	bounds := &promonitor.PolicyBounds{
		MaxRequestDeltaPct: p.Apply.MaxRequestDeltaPct,
		MaxLimitDeltaPct:   p.Apply.MaxLimitDeltaPct,
		AllowLimitDecrease: p.Apply.AllowLimitDecrease,
		MinSafetyRating:    promonitor.ParseSafetyRating(p.Apply.MinSafetyRating),
	}

	if !p.Global.Enabled {
		return promonitor.ModeObserveOnly, "disabled (global.enabled=false)", bounds, p
	}

	if p.IsNamespaceDenied(ref.Namespace) {
		return promonitor.ModeExportOnly, fmt.Sprintf("namespace %q denied", ref.Namespace), bounds, p
	}

	if !p.Apply.Enabled {
		return promonitor.ModeExportOnly, "suggest+export (apply.enabled=false)", bounds, p
	}

	return promonitor.ModeApplyReady, fmt.Sprintf("loaded from %s", result.Path), bounds, p
}
