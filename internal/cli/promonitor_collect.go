package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ppiankov/kubenow/internal/metrics"
	"github.com/ppiankov/kubenow/internal/promonitor"
	"github.com/ppiankov/kubenow/internal/util"
	"github.com/spf13/cobra"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

var collectConfig struct {
	duration string
	interval string
	output   string
}

var collectCmd = &cobra.Command{
	Use:   "collect <kind>/<name>",
	Short: "Headless resource sampling (no TUI)",
	Long: `Collect high-resolution resource samples for a workload without a TUI.

Runs the same latch as 'pro-monitor latch' but prints progress to stderr
instead of launching the interactive TUI. Designed for background collection
(e.g., cron jobs, CI pipelines, overnight runs).

On completion (or SIGINT), saves the latch data to ~/.kubenow/latch/ for
later analysis with 'pro-monitor analyze' or 'pro-monitor export'.

Examples:
  # Collect 8 hours of samples overnight
  kubenow pro-monitor collect deployment/payment-api -n prod --duration 8h

  # Collect with 1-second resolution
  kubenow pro-monitor collect deployment/api-server -n prod --duration 2h --interval 1s

  # Collect and save to a specific path
  kubenow pro-monitor collect statefulset/postgres -n databases --duration 4h --output /tmp/latch.json`,
	Args: cobra.ExactArgs(1),
	RunE: runCollect,
}

func init() {
	proMonitorCmd.AddCommand(collectCmd)
	collectCmd.Flags().StringVar(&collectConfig.duration, "duration", "15m", "collection duration (e.g., 15m, 1h, 8h)")
	collectCmd.Flags().StringVar(&collectConfig.interval, "interval", "5s", "sample interval (e.g., 1s, 5s)")
	collectCmd.Flags().StringVar(&collectConfig.output, "output", "", "override output path (default: ~/.kubenow/latch/)")
}

func runCollect(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	ref, err := promonitor.ParseWorkloadRef(args[0])
	if err != nil {
		return err
	}

	ns := GetNamespace()
	if ns == "" {
		ns = "default"
	}
	ref.Namespace = ns

	duration, err := time.ParseDuration(collectConfig.duration)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", collectConfig.duration, err)
	}

	interval, err := time.ParseDuration(collectConfig.interval)
	if err != nil {
		return fmt.Errorf("invalid interval %q: %w", collectConfig.interval, err)
	}

	fmt.Fprintf(os.Stderr, "[collect] Target: %s in namespace %s\n", ref.String(), ref.Namespace)
	fmt.Fprintf(os.Stderr, "[collect] Duration: %s, Interval: %s\n", duration, interval)

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

	// Check metrics-server
	if err := promonitor.CheckMetricsServer(ctx, metricsClient, ref.Namespace); err != nil {
		return fmt.Errorf("metrics-server required for collect: %w", err)
	}

	// Create latch monitor with stderr progress
	latchMon, err := metrics.NewLatchMonitor(kubeClient, metrics.LatchConfig{
		SampleInterval: interval,
		Duration:       duration,
		Namespaces:     []string{ref.Namespace},
		WorkloadFilter: ref.Name,
		PodLevel:       ref.Kind == "Pod",
		ProgressFunc: func(msg string) {
			fmt.Fprintf(os.Stderr, "%s\n", msg)
		},
	}, opts)
	if err != nil {
		return fmt.Errorf("failed to create latch monitor: %w", err)
	}

	// Handle SIGINT for graceful early stop
	latchCtx, latchCancel := context.WithCancel(ctx)
	defer latchCancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	startTime := time.Now()
	var earlyStop bool

	go func() {
		<-sigCh
		fmt.Fprintf(os.Stderr, "\n[collect] Received interrupt — stopping collection and saving data...\n")
		earlyStop = true
		latchMon.Stop()
	}()

	fmt.Fprintf(os.Stderr, "[collect] Starting collection...\n")
	latchErr := latchMon.Start(latchCtx)
	signal.Stop(sigCh)

	if latchErr != nil && latchErr != context.Canceled {
		return fmt.Errorf("latch error: %w", latchErr)
	}

	// Build and persist latch result
	data := latchMon.GetWorkloadSpikeData(ref.Namespace, ref.Name)
	effectiveDuration := duration
	actualDuration := time.Since(startTime)
	if earlyStop {
		effectiveDuration = actualDuration
	}

	latchResult := promonitor.BuildLatchResult(*ref, data, effectiveDuration, interval)
	if earlyStop {
		latchResult.PlannedDuration = duration
	}

	if err := promonitor.SaveLatch(latchResult); err != nil {
		return fmt.Errorf("failed to save latch data: %w", err)
	}

	// Report
	sampleCount := 0
	if data != nil {
		sampleCount = data.SampleCount
	}
	fmt.Fprintf(os.Stderr, "[collect] Collection complete: %d samples in %s\n", sampleCount, actualDuration.Truncate(time.Second))
	if earlyStop {
		fmt.Fprintf(os.Stderr, "[collect] Early stop: collected %s of planned %s (%.0f%%)\n",
			actualDuration.Truncate(time.Second), duration,
			float64(actualDuration)/float64(duration)*100)
	}
	if !latchResult.Valid {
		fmt.Fprintf(os.Stderr, "[collect] WARNING: latch data is invalid: %s\n", latchResult.Reason)
	}

	path := promonitor.LatchFilePath(*ref)
	fmt.Fprintf(os.Stderr, "[collect] Saved to %s\n", path)

	return nil
}
