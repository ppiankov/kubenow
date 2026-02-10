package cli

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ppiankov/kubenow/internal/exposure"
	"github.com/ppiankov/kubenow/internal/metrics"
	"github.com/ppiankov/kubenow/internal/promonitor"
	"github.com/ppiankov/kubenow/internal/util"
	"github.com/spf13/cobra"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"
)

var pmAnalyzeConfig struct {
	prometheusURL  string
	acknowledgeHPA bool
}

var pmAnalyzeCmd = &cobra.Command{
	Use:   "analyze <kind>/<name>",
	Short: "Analyze previously collected latch data",
	Long: `Analyze a workload using previously collected latch data from
'pro-monitor collect' or a prior 'pro-monitor latch' session.

Loads persisted latch data from ~/.kubenow/latch/, connects to the cluster
for current resource values, computes the recommendation, and launches the
interactive TUI for export or apply.

This is the counterpart to 'pro-monitor collect' — collect data headlessly
(e.g., overnight via cron), then analyze interactively later.

Policy gates apply normally:
  - MaxLatchAge gates apply on stale data
  - MinLatchDuration gates apply on short collections

Examples:
  # Analyze previously collected data
  kubenow pro-monitor analyze deployment/payment-api -n prod --policy ./policy.yaml

  # Analyze with Linkerd traffic source measurement
  kubenow pro-monitor analyze deployment/payment-api -n prod --prometheus-url http://prometheus:9090`,
	Args: cobra.ExactArgs(1),
	RunE: runAnalyze,
}

func init() {
	proMonitorCmd.AddCommand(pmAnalyzeCmd)
	pmAnalyzeCmd.Flags().StringVar(&pmAnalyzeConfig.prometheusURL, "prometheus-url", "", "Prometheus endpoint for Linkerd traffic metrics")
	pmAnalyzeCmd.Flags().BoolVar(&pmAnalyzeConfig.acknowledgeHPA, "acknowledge-hpa", false, "acknowledge HPA presence and allow apply despite HPA")
}

func runAnalyze(cmd *cobra.Command, args []string) error {
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

	// Load persisted latch data
	latch, err := promonitor.LoadLatch(*ref)
	if err != nil {
		return fmt.Errorf("no latch data found: %w\nRun 'kubenow pro-monitor collect %s -n %s' first", err, args[0], ns)
	}

	fmt.Fprintf(os.Stderr, "[analyze] Loaded latch data: %d samples, duration %s\n",
		latch.Data.SampleCount, latch.Duration.Truncate(1e9))
	if latch.PlannedDuration > 0 {
		fmt.Fprintf(os.Stderr, "[analyze] Early-stopped: %s of planned %s\n", latch.Duration, latch.PlannedDuration)
	}
	if !latch.Valid {
		fmt.Fprintf(os.Stderr, "[analyze] WARNING: latch data is invalid: %s\n", latch.Reason)
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

	// Fetch current container resources
	containers, err := promonitor.FetchContainerResources(ctx, kubeClient, ref)
	if err != nil {
		if IsVerbose() {
			fmt.Fprintf(os.Stderr, "[analyze] Warning: could not read container resources: %v\n", err)
		}
	}

	// Detect HPA
	hpa := promonitor.DetectHPA(ctx, kubeClient, ref)

	// Load policy
	mode, policyMsg, bounds, loadedPolicy := resolveMode(policyPath, ref)

	// Compute recommendation
	rec := promonitor.Recommend(&promonitor.RecommendInput{
		Latch:      latch,
		Containers: containers,
		Bounds:     bounds,
		HPA:        hpa,
	})

	if latch.PlannedDuration > 0 {
		rec.Warnings = append(rec.Warnings, fmt.Sprintf(
			"latch was stopped early: %s of planned %s",
			latch.Duration, latch.PlannedDuration))
	}

	// Create analyze-mode TUI model (starts post-latch)
	model := promonitor.NewAnalyzeModel(*ref, mode, policyMsg, hpa, rec, latch)
	model.SetContainers(containers)
	if bounds != nil {
		model.SetPolicyBounds(bounds)
	}

	// Wire apply infrastructure
	if mode == promonitor.ModeApplyReady {
		model.SetKubeApplier(&promonitor.ClientsetApplier{Client: kubeClient})
		if bounds != nil && loadedPolicy != nil {
			bounds.MaxLatchAge = loadedPolicy.MaxLatchAgeParsed()
			bounds.MinLatchDuration = loadedPolicy.MinLatchDurationParsed()
		}
		model.SetPolicy(bounds)
	}

	// Wire exposure map (+ optional Linkerd traffic)
	exposureCollector := exposure.NewExposureCollector(kubeClient, metricsClient)
	if pmAnalyzeConfig.prometheusURL != "" {
		promClient, err := metrics.NewPrometheusClient(metrics.Config{PrometheusURL: pmAnalyzeConfig.prometheusURL})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[analyze] Warning: could not connect to Prometheus: %v\n", err)
		} else {
			exposureCollector.SetPrometheusAPI(promClient.GetAPI())
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

	model.SetHPAAcknowledged(pmAnalyzeConfig.acknowledgeHPA)

	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui error: %w", err)
	}

	return nil
}
