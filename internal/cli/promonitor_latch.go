package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ppiankov/kubenow/internal/metrics"
	"github.com/ppiankov/kubenow/internal/policy"
	"github.com/ppiankov/kubenow/internal/promonitor"
	"github.com/ppiankov/kubenow/internal/util"
	"github.com/spf13/cobra"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

var latchConfig struct {
	duration string
	interval string
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

After the latch completes, recommendation data will be available for
export or apply (in future PRs).

Examples:
  # Latch a deployment for 15 minutes
  kubenow pro-monitor latch deployment/payment-api -n default

  # Latch for 1 hour with 1-second samples
  kubenow pro-monitor latch deployment/api-server -n prod --duration 1h --interval 1s

  # Latch a statefulset
  kubenow pro-monitor latch statefulset/postgres -n databases --duration 30m`,
	Args: cobra.ExactArgs(1),
	RunE: runLatch,
}

func init() {
	proMonitorCmd.AddCommand(latchCmd)
	latchCmd.Flags().StringVar(&latchConfig.duration, "duration", "15m", "latch duration (e.g., 15m, 1h, 24h)")
	latchCmd.Flags().StringVar(&latchConfig.interval, "interval", "5s", "sample interval (e.g., 1s, 5s)")
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
	kubeClient, err := util.BuildKubeClient(GetKubeconfig())
	if err != nil {
		return fmt.Errorf("failed to build Kubernetes client: %w", err)
	}

	restConfig, err := util.BuildRestConfig(GetKubeconfig())
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
	mode, policyMsg := resolveMode(policyPath, ref)

	// Create latch monitor (filtered to target workload)
	latchMon, err := metrics.NewLatchMonitor(kubeClient, metrics.LatchConfig{
		SampleInterval: interval,
		Duration:       duration,
		Namespaces:     []string{ref.Namespace},
		WorkloadFilter: ref.Name,
	})
	if err != nil {
		return fmt.Errorf("failed to create latch monitor: %w", err)
	}

	// Create and run TUI
	model := promonitor.NewModel(*ref, latchMon, duration, mode, policyMsg, hpa)

	// Start latch in background
	latchCtx, latchCancel := context.WithCancel(ctx)
	defer latchCancel()

	latchStart := time.Now()
	model.SetLatchStart(latchStart)

	go func() {
		_ = latchMon.Start(latchCtx)
	}()

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		latchCancel()
		return fmt.Errorf("TUI error: %w", err)
	}

	latchCancel()
	return nil
}

// resolveMode loads the policy and determines the operating mode.
func resolveMode(policyFile string, ref *promonitor.WorkloadRef) (promonitor.Mode, string) {
	result := policy.Load(policyFile)

	if result.Absent {
		return promonitor.ModeObserveOnly, fmt.Sprintf("none (%s)", result.Path)
	}

	if result.ErrorMsg != "" {
		return promonitor.ModeObserveOnly, fmt.Sprintf("invalid: %s", result.ErrorMsg)
	}

	p := result.Policy

	vr := policy.Validate(p)
	if !vr.Valid {
		return promonitor.ModeObserveOnly, fmt.Sprintf("validation failed (%d errors)", len(vr.Errors))
	}

	if !p.Global.Enabled {
		return promonitor.ModeObserveOnly, "disabled (global.enabled=false)"
	}

	if p.IsNamespaceDenied(ref.Namespace) {
		return promonitor.ModeExportOnly, fmt.Sprintf("namespace %q denied", ref.Namespace)
	}

	if !p.Apply.Enabled {
		return promonitor.ModeExportOnly, "suggest+export (apply.enabled=false)"
	}

	return promonitor.ModeApplyReady, fmt.Sprintf("loaded from %s", result.Path)
}
