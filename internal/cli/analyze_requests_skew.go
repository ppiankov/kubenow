package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/ppiankov/kubenow/internal/analyzer"
	"github.com/ppiankov/kubenow/internal/metrics"
	"github.com/ppiankov/kubenow/internal/util"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
)

var requestsSkewConfig struct {
	prometheusURL       string
	autoDetect          bool
	window              string
	top                 int
	namespaceRegex      string
	minRuntimeDays      int
	output              string
	exportFile          string
	exportFormat        string
	prometheusTimeout   string
	watchForSpikes      bool
	spikeDuration       string
	spikeInterval       string
	showRecommendations bool
	safetyFactor        float64
	silent              bool
	sortBy              string
	// Port-forward options
	k8sService     string
	k8sNamespace   string
	k8sLocalPort   string
	k8sRemotePort  string
}

// spikeWorkload holds spike data with calculated ratios
type spikeWorkload struct {
	key        string
	data       *metrics.SpikeData
	spikeRatio float64
}

var requestsSkewCmd = &cobra.Command{
	Use:   "requests-skew",
	Short: "Find over-provisioned resources",
	Long: `Identify namespaces with over-provisioned resource requests vs actual usage.

This command analyzes resource requests compared to actual Prometheus metrics
to find workloads that are significantly over-provisioned. Results are ranked
by cost impact (skew ratio × absolute resources).

Philosophy:
  - Deterministic: No AI, no prediction, just historical data analysis
  - Evidence-based: Claims based on actual metrics over time window
  - Non-prescriptive: Shows "this would have worked" not "you should do this"

Examples:
  # Basic analysis with default 30-day window
  kubenow analyze requests-skew --prometheus-url http://localhost:9090

  # Focus on production namespace, last 7 days
  kubenow analyze requests-skew --prometheus-url http://localhost:9090 \
    --window 7d --namespace-regex "prod.*"

  # Top 20 results with JSON output
  kubenow analyze requests-skew --prometheus-url http://localhost:9090 \
    --top 20 --output json

  # Export to file
  kubenow analyze requests-skew --prometheus-url http://localhost:9090 \
    --export-file report.json

  # Use native port-forward to in-cluster Prometheus
  kubenow analyze requests-skew --k8s-service prometheus-operated \
    --k8s-namespace monitoring`,
	RunE: runRequestsSkew,
}

func init() {
	analyzeCmd.AddCommand(requestsSkewCmd)

	// Required flags
	requestsSkewCmd.Flags().StringVar(&requestsSkewConfig.prometheusURL, "prometheus-url", "", "Prometheus endpoint (e.g., http://prometheus:9090)")
	requestsSkewCmd.Flags().BoolVar(&requestsSkewConfig.autoDetect, "auto-detect-prometheus", false, "Auto-discover Prometheus in cluster")

	// Optional flags
	requestsSkewCmd.Flags().StringVar(&requestsSkewConfig.window, "window", "30d", "Time window for analysis (e.g., 7d, 24h, 30d)")
	requestsSkewCmd.Flags().IntVar(&requestsSkewConfig.top, "top", 10, "Top N results (0 = all)")
	requestsSkewCmd.Flags().StringVar(&requestsSkewConfig.namespaceRegex, "namespace-regex", ".*", "Namespace filter regex")
	requestsSkewCmd.Flags().IntVar(&requestsSkewConfig.minRuntimeDays, "min-runtime-days", 7, "Ignore workloads younger than N days")
	requestsSkewCmd.Flags().StringVar(&requestsSkewConfig.output, "output", "table", "Output format: table|json")
	requestsSkewCmd.Flags().StringVar(&requestsSkewConfig.exportFile, "export-file", "", "Save to file (optional)")
	requestsSkewCmd.Flags().StringVar(&requestsSkewConfig.exportFormat, "export-format", "json", "Export file format: json|table")
	requestsSkewCmd.Flags().StringVar(&requestsSkewConfig.sortBy, "sort-by", "impact", "Sort results by: impact|skew|cpu|memory|name")
	requestsSkewCmd.Flags().StringVar(&requestsSkewConfig.prometheusTimeout, "prometheus-timeout", "30s", "Query timeout")

	// Spike monitoring flags (experimental)
	requestsSkewCmd.Flags().BoolVar(&requestsSkewConfig.watchForSpikes, "watch-for-spikes", false, "Enable real-time spike monitoring (experimental)")
	requestsSkewCmd.Flags().StringVar(&requestsSkewConfig.spikeDuration, "spike-duration", "15m", "How long to monitor for spikes (e.g., 15m, 1h, 24h)")
	requestsSkewCmd.Flags().StringVar(&requestsSkewConfig.spikeInterval, "spike-interval", "5s", "Sampling interval for spike detection (e.g., 1s, 5s)")
	requestsSkewCmd.Flags().BoolVar(&requestsSkewConfig.showRecommendations, "show-recommendations", false, "Show calculated CPU request recommendations based on spike data")
	requestsSkewCmd.Flags().Float64Var(&requestsSkewConfig.safetyFactor, "safety-factor", 0.0, "Override safety factor for recommendations (default: auto-select based on spike ratio)")

	// CI/CD flags
	requestsSkewCmd.Flags().BoolVar(&requestsSkewConfig.silent, "silent", false, "Suppress progress output (for CI/CD pipelines)")

	// Kubernetes port-forward flags
	requestsSkewCmd.Flags().StringVar(&requestsSkewConfig.k8sService, "k8s-service", "", "Kubernetes service name for port-forward (e.g., 'prometheus-operated')")
	requestsSkewCmd.Flags().StringVar(&requestsSkewConfig.k8sNamespace, "k8s-namespace", "monitoring", "Kubernetes namespace for service")
	requestsSkewCmd.Flags().StringVar(&requestsSkewConfig.k8sLocalPort, "k8s-local-port", "9090", "Local port for port-forward")
	requestsSkewCmd.Flags().StringVar(&requestsSkewConfig.k8sRemotePort, "k8s-remote-port", "9090", "Remote port for port-forward")
}

func runRequestsSkew(cmd *cobra.Command, args []string) error {
	// Set silent mode for CI/CD
	if requestsSkewConfig.silent {
		analyzer.SilentMode = true
	}

	// Setup kubectl port-forward if k8s-service is specified
	var portForward *util.PortForward
	if requestsSkewConfig.k8sService != "" {
		if IsVerbose() {
			fmt.Fprintf(os.Stderr, "[kubenow] Setting up native port-forward to %s/%s...\n",
				requestsSkewConfig.k8sNamespace, requestsSkewConfig.k8sService)
		}

		var err error
		portForward, err = util.NewPortForward(
			requestsSkewConfig.k8sService,
			requestsSkewConfig.k8sNamespace,
			requestsSkewConfig.k8sLocalPort,
			requestsSkewConfig.k8sRemotePort,
		)
		if err != nil {
			return fmt.Errorf("failed to create port-forward: %w", err)
		}

		if err := portForward.Start(); err != nil {
			return fmt.Errorf("failed to start port-forward: %w", err)
		}

		// Stop port-forward on exit
		defer func() {
			if err := portForward.Stop(); err != nil {
				fmt.Fprintf(os.Stderr, "[kubenow] Warning: failed to stop port-forward: %v\n", err)
			}
		}()

		// Use localhost URL if port-forward is active
		if requestsSkewConfig.prometheusURL == "" {
			requestsSkewConfig.prometheusURL = fmt.Sprintf("http://localhost:%s", requestsSkewConfig.k8sLocalPort)
			if IsVerbose() {
				fmt.Fprintf(os.Stderr, "[kubenow] Using port-forward URL: %s\n", requestsSkewConfig.prometheusURL)
			}
		}
	}

	// Validate flags
	if requestsSkewConfig.prometheusURL == "" && !requestsSkewConfig.autoDetect {
		return fmt.Errorf("either --prometheus-url, --k8s-service, or --auto-detect-prometheus is required")
	}

	if requestsSkewConfig.output != "table" && requestsSkewConfig.output != "json" {
		return fmt.Errorf("--output must be 'table' or 'json'")
	}

	if requestsSkewConfig.exportFormat != "table" && requestsSkewConfig.exportFormat != "json" {
		return fmt.Errorf("--export-format must be 'table' or 'json'")
	}

	// Parse window duration
	window, err := metrics.ParseDuration(requestsSkewConfig.window)
	if err != nil {
		return fmt.Errorf("invalid window: %w", err)
	}

	// Parse timeout
	timeout, err := time.ParseDuration(requestsSkewConfig.prometheusTimeout)
	if err != nil {
		return fmt.Errorf("invalid timeout: %w", err)
	}

	// Build Kubernetes client
	if IsVerbose() {
		fmt.Fprintln(os.Stderr, "[kubenow] Building Kubernetes client...")
	}

	kubeClient, err := util.BuildKubeClient(GetKubeconfig())
	if err != nil {
		return fmt.Errorf("failed to build Kubernetes client: %w", err)
	}

	// Create Prometheus client
	if IsVerbose() {
		fmt.Fprintf(os.Stderr, "[kubenow] Connecting to Prometheus: %s\n", requestsSkewConfig.prometheusURL)
	}

	promConfig := metrics.Config{
		PrometheusURL: requestsSkewConfig.prometheusURL,
		Timeout:       timeout,
	}

	metricsProvider, err := metrics.NewPrometheusClient(promConfig)
	if err != nil {
		return fmt.Errorf("failed to create Prometheus client: %w", err)
	}

	// Health check
	ctx := context.Background()
	if err := metricsProvider.Health(ctx); err != nil {
		return fmt.Errorf("Prometheus health check failed: %w", err)
	}

	// Discover available metrics
	if !requestsSkewConfig.silent {
		fmt.Fprintln(os.Stderr, "[kubenow] Discovering available Prometheus metrics...")
	}

	discovery := metrics.NewMetricDiscovery(metricsProvider.GetAPI())
	availableMetrics, err := discovery.DiscoverMetrics(ctx)
	if err != nil {
		return fmt.Errorf("metric discovery failed: %w", err)
	}

	// Validate that required metrics exist
	if err := availableMetrics.ValidateMetrics(); err != nil {
		fmt.Fprintf(os.Stderr, "\n⚠️  Metric Discovery Failed:\n")
		fmt.Fprintf(os.Stderr, "════════════════════════\n\n")
		fmt.Fprintf(os.Stderr, "%s\n\n", err.Error())
		fmt.Fprintf(os.Stderr, "Available metrics in Prometheus:\n")
		if len(availableMetrics.AllCPU) > 0 {
			fmt.Fprintf(os.Stderr, "  CPU-related: %v\n", availableMetrics.AllCPU)
		} else {
			fmt.Fprintf(os.Stderr, "  CPU-related: (none found)\n")
		}
		if len(availableMetrics.AllMemory) > 0 {
			fmt.Fprintf(os.Stderr, "  Memory-related: %v\n", availableMetrics.AllMemory)
		} else {
			fmt.Fprintf(os.Stderr, "  Memory-related: (none found)\n")
		}
		fmt.Fprintf(os.Stderr, "\nPossible causes:\n")
		fmt.Fprintf(os.Stderr, "  • cAdvisor metrics not being scraped\n")
		fmt.Fprintf(os.Stderr, "  • ServiceMonitor/PodMonitor not configured\n")
		fmt.Fprintf(os.Stderr, "  • Prometheus scrape config missing container metrics\n")
		fmt.Fprintf(os.Stderr, "\nSee README troubleshooting section for details.\n")
		return fmt.Errorf("required metrics not available in Prometheus")
	}

	if !requestsSkewConfig.silent {
		fmt.Fprintf(os.Stderr, "[kubenow] Using metrics: CPU=%s, Memory=%s\n",
			availableMetrics.CPUMetric, availableMetrics.MemoryMetric)
	}

	if IsVerbose() {
		fmt.Fprintln(os.Stderr, "[kubenow] Analyzing resource requests vs usage...")
	}

	// Validate sort-by option
	validSortOptions := map[string]bool{
		"impact": true, "skew": true, "cpu": true, "memory": true, "name": true,
	}
	if !validSortOptions[requestsSkewConfig.sortBy] {
		return fmt.Errorf("invalid --sort-by option: %s (must be: impact|skew|cpu|memory|name)", requestsSkewConfig.sortBy)
	}

	// Create analyzer
	analyzerConfig := analyzer.RequestsSkewConfig{
		Window:         window,
		Top:            requestsSkewConfig.top,
		Namespace:      GetNamespace(), // Use global --namespace flag if provided
		NamespaceRegex: requestsSkewConfig.namespaceRegex,
		MinRuntimeDays: requestsSkewConfig.minRuntimeDays,
		SortBy:         requestsSkewConfig.sortBy,
	}

	skewAnalyzer := analyzer.NewRequestsSkewAnalyzer(kubeClient, metricsProvider, analyzerConfig)

	// Run analysis
	result, err := skewAnalyzer.Analyze(ctx)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	// Run spike monitoring if requested
	var spikeData map[string]*metrics.SpikeData
	if requestsSkewConfig.watchForSpikes {
		spikeData, err = runSpikeMonitoring(ctx, kubeClient)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[kubenow] Warning: Spike monitoring failed: %v\n", err)
			// Continue with analysis results even if spike monitoring fails
		}

		// Attach spike data to result for export
		if spikeData != nil && len(spikeData) > 0 {
			result.SpikeData = make(map[string]interface{})
			for key, data := range spikeData {
				result.SpikeData[key] = data
			}
		}
	}

	// Output results
	if requestsSkewConfig.output == "json" {
		return outputRequestsSkewJSON(result, requestsSkewConfig.exportFile)
	}

	return outputRequestsSkewTable(result, spikeData, requestsSkewConfig.exportFile, requestsSkewConfig.exportFormat)
}

// runSpikeMonitoring runs the latch monitor to detect sub-scrape-interval spikes
func runSpikeMonitoring(ctx context.Context, kubeClient *kubernetes.Clientset) (map[string]*metrics.SpikeData, error) {
	// Parse duration and interval
	duration, err := time.ParseDuration(requestsSkewConfig.spikeDuration)
	if err != nil {
		return nil, fmt.Errorf("invalid spike-duration: %w", err)
	}

	interval, err := time.ParseDuration(requestsSkewConfig.spikeInterval)
	if err != nil {
		return nil, fmt.Errorf("invalid spike-interval: %w", err)
	}

	fmt.Fprintf(os.Stderr, "\n[kubenow] Starting real-time spike monitoring...\n")
	fmt.Fprintf(os.Stderr, "[kubenow] Duration: %s | Interval: %s\n", duration, interval)
	fmt.Fprintf(os.Stderr, "[kubenow] This will sample Kubernetes Metrics API at high frequency to catch sub-scrape-interval spikes.\n\n")

	// Create latch monitor
	latchConfig := metrics.LatchConfig{
		SampleInterval: interval,
		Duration:       duration,
		Namespaces:     []string{}, // Empty = all namespaces (will skip kube-system internally)
	}

	monitor, err := metrics.NewLatchMonitor(kubeClient, latchConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create latch monitor: %w", err)
	}

	// Run monitoring (blocks until complete)
	if err := monitor.Start(ctx); err != nil {
		return nil, fmt.Errorf("spike monitoring failed: %w", err)
	}

	// Get collected data
	spikeData := monitor.GetSpikeData()

	fmt.Fprintf(os.Stderr, "\n[kubenow] Spike monitoring complete. Captured %d workloads.\n\n", len(spikeData))

	return spikeData, nil
}

func outputRequestsSkewJSON(result *analyzer.RequestsSkewResult, exportFile string) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Export to file if specified
	if exportFile != "" {
		if err := os.WriteFile(exportFile, data, 0644); err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "[kubenow] Report saved to: %s\n", exportFile)
		return nil
	}

	// Print to stdout
	fmt.Println(string(data))
	return nil
}

func outputRequestsSkewTable(result *analyzer.RequestsSkewResult, spikeData map[string]*metrics.SpikeData, exportFile string, exportFormat string) error {
	// If export file is specified, save to file in requested format
	if exportFile != "" {
		if exportFormat == "json" {
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON for export: %w", err)
			}
			if err := os.WriteFile(exportFile, data, 0644); err != nil {
				return fmt.Errorf("failed to write export file: %w", err)
			}
			fmt.Fprintf(os.Stderr, "[kubenow] Full results exported to: %s (JSON format)\n", exportFile)
		} else if exportFormat == "table" {
			// Defer the table export until after we render it
			// We'll capture the table output and save it
			defer func() {
				if err := exportTableToFile(result, spikeData, exportFile); err != nil {
					fmt.Fprintf(os.Stderr, "[kubenow] Warning: failed to export table: %v\n", err)
				}
			}()
		}
	}

	// Create table
	table := tablewriter.NewWriter(os.Stdout)
	table.Header([]string{"Namespace", "Workload", "Req CPU", "P99 CPU", "Skew", "Safety", "Impact"})

	for _, w := range result.Results {
		safetyLabel := "?"
		if w.Safety != nil {
			safetyLabel = string(w.Safety.Rating)
			// Add emoji indicators
			switch w.Safety.Rating {
			case "SAFE":
				safetyLabel = "✓ SAFE"
			case "CAUTION":
				safetyLabel = "⚠ CAUTION"
			case "RISKY":
				safetyLabel = "⚠ RISKY"
			case "UNSAFE":
				safetyLabel = "✗ UNSAFE"
			}
		}

		table.Append([]string{
			w.Namespace,
			w.Workload,
			fmt.Sprintf("%.2f", w.RequestedCPU),
			fmt.Sprintf("%.2f", w.P99UsedCPU),
			fmt.Sprintf("%.1fx", w.SkewCPU),
			safetyLabel,
			impactScoreLabel(w.ImpactScore),
		})
	}

	// Print summary
	fmt.Printf("\n=== Requests-Skew Analysis ===\n")
	if result.Summary.AnalyzedWorkloads == 0 && len(result.WorkloadsWithoutMetrics) > 0 {
		fmt.Printf("Window: %s | Analyzed: %d workloads (%d have no Prometheus metrics) | Top: %d\n\n",
			result.Metadata.Window,
			result.Summary.AnalyzedWorkloads,
			len(result.WorkloadsWithoutMetrics),
			len(result.Results))
	} else {
		fmt.Printf("Window: %s | Analyzed: %d workloads | Top: %d\n\n",
			result.Metadata.Window,
			result.Summary.AnalyzedWorkloads,
			len(result.Results))
	}

	// Render table
	table.Render()

	// Print summary stats
	fmt.Printf("\nSummary:\n")
	fmt.Printf("  Average CPU Skew: %.2fx\n", result.Summary.AvgSkewCPU)
	fmt.Printf("  Average Memory Skew: %.2fx\n", result.Summary.AvgSkewMemory)
	fmt.Printf("  Total Wasted CPU: %.2f cores\n", result.Summary.TotalWastedCPU)
	fmt.Printf("  Total Wasted Memory: %.2fGi\n", result.Summary.TotalWastedMemoryGi)

	// Print safety warnings
	printSafetyWarnings(result)

	// Print warnings about workloads without metrics
	printWorkloadsWithoutMetricsWarning(result)

	// Print quota information
	printQuotaInformation(result)

	// Print spike monitoring results if available
	if len(spikeData) > 0 {
		printSpikeMonitoringResults(spikeData)
	}

	return nil
}

func printSafetyWarnings(result *analyzer.RequestsSkewResult) {
	// Collect workloads with safety issues
	var unsafe, risky, caution []string

	for _, w := range result.Results {
		if w.Safety == nil {
			continue
		}

		label := fmt.Sprintf("%s/%s", w.Namespace, w.Workload)
		switch w.Safety.Rating {
		case "UNSAFE":
			unsafe = append(unsafe, label)
		case "RISKY":
			risky = append(risky, label)
		case "CAUTION":
			caution = append(caution, label)
		}
	}

	// Print warnings if any issues found
	if len(unsafe) > 0 || len(risky) > 0 || len(caution) > 0 {
		fmt.Printf("\n⚠️  Safety Warnings:\n")
		fmt.Printf("═══════════════════\n\n")

		if len(unsafe) > 0 {
			fmt.Printf("✗ UNSAFE (%d workloads) - DO NOT REDUCE RESOURCES:\n", len(unsafe))
			for _, w := range unsafe {
				// Find the workload details
				for _, wr := range result.Results {
					if fmt.Sprintf("%s/%s", wr.Namespace, wr.Workload) == w && wr.Safety != nil {
						fmt.Printf("  • %s\n", w)
						for _, reason := range wr.Safety.Warnings {
							fmt.Printf("    - %s\n", reason)
						}
						break
					}
				}
			}
			fmt.Println()
		}

		if len(risky) > 0 {
			fmt.Printf("⚠ RISKY (%d workloads) - Review carefully before reducing:\n", len(risky))
			for _, w := range risky {
				for _, wr := range result.Results {
					if fmt.Sprintf("%s/%s", wr.Namespace, wr.Workload) == w && wr.Safety != nil {
						fmt.Printf("  • %s (safety margin: %.1fx)\n", w, wr.Safety.SafeMargin)
						for _, reason := range wr.Safety.Warnings {
							fmt.Printf("    - %s\n", reason)
						}
						break
					}
				}
			}
			fmt.Println()
		}

		if len(caution) > 0 {
			fmt.Printf("⚠ CAUTION (%d workloads) - Minor concerns detected:\n", len(caution))
			for _, w := range caution {
				fmt.Printf("  • %s\n", w)
			}
			fmt.Println()
		}

		fmt.Printf("💡 Recommendation Philosophy:\n")
		fmt.Printf("   - Evidence-based: These warnings are based on historical metrics over %s\n", result.Metadata.Window)
		fmt.Printf("   - Non-prescriptive: We show what would have happened, not what you should do\n")
		fmt.Printf("   - Safety-first: When in doubt, keep existing resources\n")
		fmt.Println()
	} else {
		fmt.Printf("\n✓ No critical safety issues detected in analyzed workloads\n\n")
	}
}

func printWorkloadsWithoutMetricsWarning(result *analyzer.RequestsSkewResult) {
	if len(result.WorkloadsWithoutMetrics) == 0 {
		return // No workloads without metrics, nothing to warn about
	}

	fmt.Printf("\n⚠️  Workloads Without Prometheus Metrics:\n")
	fmt.Printf("══════════════════════════════════════════\n\n")

	fmt.Printf("Found %d workload(s) in Kubernetes but NO corresponding Prometheus metrics:\n", len(result.WorkloadsWithoutMetrics))
	fmt.Printf("(requests-skew requires Prometheus metrics to compare requested vs actual usage)\n\n")

	// Group by namespace for better readability
	byNamespace := make(map[string][]analyzer.WorkloadWithoutMetrics)
	for _, w := range result.WorkloadsWithoutMetrics {
		byNamespace[w.Namespace] = append(byNamespace[w.Namespace], w)
	}

	for ns, workloads := range byNamespace {
		fmt.Printf("  Namespace: %s\n", ns)
		for _, w := range workloads {
			fmt.Printf("    • %s (%s)\n", w.Workload, w.Type)
		}
		fmt.Println()
	}

	fmt.Printf("Possible Causes:\n")
	fmt.Printf("  • Container metrics not being scraped by Prometheus\n")
	fmt.Printf("  • ServiceMonitor/PodMonitor missing cAdvisor endpoint\n")
	fmt.Printf("  • Workload too new (created after analysis window)\n")
	fmt.Printf("  • Pods in crash loops or not running\n")
	fmt.Println()

	fmt.Printf("💡 Recommended Action - Use Latch Mode:\n")
	fmt.Printf("═══════════════════════════════════════════\n\n")
	fmt.Printf("Since these workloads lack Prometheus metrics, you can use LATCH MODE\n")
	fmt.Printf("to monitor them in real-time via the Kubernetes Metrics API:\n\n")

	fmt.Printf("  kubenow analyze requests-skew \\\n")
	fmt.Printf("    --prometheus-url %s \\\n", requestsSkewConfig.prometheusURL)
	fmt.Printf("    --watch-for-spikes \\\n")
	fmt.Printf("    --spike-duration 15m \\\n")
	fmt.Printf("    --spike-interval 5s\n\n")

	fmt.Printf("What Latch Mode Does:\n")
	fmt.Printf("  • Samples Kubernetes Metrics API at high frequency (default: 5s)\n")
	fmt.Printf("  • Captures sub-scrape-interval spikes (< 15-30s) that Prometheus misses\n")
	fmt.Printf("  • Useful for bursty workloads (AI inference, RAG, batch jobs)\n")
	fmt.Printf("  • Provides real-time data when historical metrics unavailable\n\n")

	fmt.Printf("⚡ Special Note for RAG Workloads:\n")
	fmt.Printf("  RAG queries are extremely bursty (millisecond-level spikes).\n")
	fmt.Printf("  For RAG workloads, use 1s or sub-1s sampling:\n\n")
	fmt.Printf("    kubenow analyze requests-skew \\\n")
	fmt.Printf("      --prometheus-url %s \\\n", requestsSkewConfig.prometheusURL)
	fmt.Printf("      --watch-for-spikes \\\n")
	fmt.Printf("      --spike-duration 30m \\\n")
	fmt.Printf("      --spike-interval 1s    # ← Critical for RAG!\n\n")

	fmt.Printf("Troubleshooting Missing Metrics:\n")
	fmt.Printf("  1. Check ServiceMonitor configuration:\n")
	fmt.Printf("     kubectl get servicemonitor -n kube-prometheus-stack kubelet -o yaml\n\n")
	fmt.Printf("  2. Verify cAdvisor endpoint exists:\n")
	fmt.Printf("     Look for: endpoints[].port=cadvisor, path=/metrics/cadvisor\n\n")
	fmt.Printf("  3. Check Prometheus targets:\n")
	fmt.Printf("     kubectl port-forward -n kube-prometheus-stack svc/prometheus-operated 9090:9090\n")
	fmt.Printf("     # Open: http://localhost:9090/targets\n\n")

	fmt.Printf("See README troubleshooting section for detailed guidance.\n\n")
}

func impactScoreLabel(score float64) string {
	if score > 30 {
		return fmt.Sprintf("HIGH (%.1f)", score)
	} else if score > 10 {
		return fmt.Sprintf("MED (%.1f)", score)
	}
	return fmt.Sprintf("LOW (%.1f)", score)
}

func printSpikeMonitoringResults(spikeData map[string]*metrics.SpikeData) {
	fmt.Printf("\n📊 Real-Time Spike Monitoring Results:\n")
	fmt.Printf("═══════════════════════════════════════\n\n")

	// Find workloads with significant spikes
	var workloadsWithSpikes []spikeWorkload

	for key, data := range spikeData {
		if data.AvgCPU == 0 {
			continue
		}

		spikeRatio := data.MaxCPU / data.AvgCPU
		if spikeRatio > 2.0 { // Spike is >2x average
			workloadsWithSpikes = append(workloadsWithSpikes, spikeWorkload{
				key:        key,
				data:       data,
				spikeRatio: spikeRatio,
			})
		}
	}

	if len(workloadsWithSpikes) == 0 {
		fmt.Printf("✓ No significant spikes detected (all workloads < 2x average)\n\n")
		return
	}

	fmt.Printf("⚠️  Detected %d workloads with CPU spikes > 2x average:\n\n", len(workloadsWithSpikes))

	// Create table for spike data
	table := tablewriter.NewWriter(os.Stdout)

	// Add recommendations column if requested
	if requestsSkewConfig.showRecommendations {
		table.Header([]string{"Namespace/Workload", "Avg CPU", "Max CPU", "Spike Ratio", "Recommended CPU", "Safety Factor"})
	} else {
		table.Header([]string{"Namespace/Workload", "Avg CPU", "Max CPU", "Spike Ratio", "Spike Count", "Samples"})
	}

	for _, sw := range workloadsWithSpikes {
		if requestsSkewConfig.showRecommendations {
			// Calculate safety factor based on spike ratio
			safetyFactor := requestsSkewConfig.safetyFactor
			if safetyFactor == 0.0 {
				// Auto-select based on spike ratio
				if sw.spikeRatio >= 20.0 {
					safetyFactor = 2.5
				} else if sw.spikeRatio >= 10.0 {
					safetyFactor = 2.0
				} else if sw.spikeRatio >= 5.0 {
					safetyFactor = 1.5
				} else {
					safetyFactor = 1.2
				}
			}

			// Calculate recommended CPU
			recommendedCPU := sw.data.MaxCPU * safetyFactor

			table.Append([]string{
				sw.key,
				fmt.Sprintf("%.3f", sw.data.AvgCPU),
				fmt.Sprintf("%.3f", sw.data.MaxCPU),
				fmt.Sprintf("%.1fx", sw.spikeRatio),
				fmt.Sprintf("%.2f cores", recommendedCPU),
				fmt.Sprintf("%.1fx", safetyFactor),
			})
		} else {
			table.Append([]string{
				sw.key,
				fmt.Sprintf("%.3f", sw.data.AvgCPU),
				fmt.Sprintf("%.3f", sw.data.MaxCPU),
				fmt.Sprintf("%.1fx", sw.spikeRatio),
				fmt.Sprintf("%d", sw.data.SpikeCount),
				fmt.Sprintf("%d", sw.data.SampleCount),
			})
		}
	}

	table.Render()

	// Print critical signals detected during monitoring
	printCriticalSignals(workloadsWithSpikes)

	if requestsSkewConfig.showRecommendations {
		fmt.Printf("\n💡 How to Use These Recommendations:\n")
		fmt.Printf("═══════════════════════════════════════\n\n")
		fmt.Printf("Formula: CPU Request = Max Observed CPU × Safety Factor\n\n")
		fmt.Printf("Safety factor auto-selected based on spike ratio:\n")
		fmt.Printf("  • Spike ≥20x: 2.5x (extreme bursts, e.g., RAG/AI inference)\n")
		fmt.Printf("  • Spike 10-20x: 2.0x (high bursts, e.g., batch jobs)\n")
		fmt.Printf("  • Spike 5-10x: 1.5x (moderate bursts, e.g., APIs)\n")
		fmt.Printf("  • Spike 2-5x: 1.2x (low bursts, e.g., background workers)\n\n")
		fmt.Printf("Apply with kubectl:\n")
		fmt.Printf("  kubectl patch deployment <name> -n <namespace> --type=json -p='[\n")
		fmt.Printf("    {\"op\": \"replace\", \"path\": \"/spec/template/spec/containers/0/resources/requests/cpu\", \"value\": \"<recommended>m\"}\n")
		fmt.Printf("  ]'\n\n")
		fmt.Printf("See SPIKE-ANALYSIS.md for comprehensive guidance.\n\n")
	} else {
		fmt.Printf("\nKey Findings:\n")
		fmt.Printf("  • These spikes may not be visible in Prometheus metrics (scrape interval ~15-30s)\n")
		fmt.Printf("  • High spike ratios suggest sub-second bursts (common in RAG, AI inference, etc.)\n")
		fmt.Printf("  • Consider these spikes when sizing resource requests\n\n")
		fmt.Printf("💡 Want calculated recommendations? Use: --show-recommendations\n")
		fmt.Printf("   This adds a 'Recommended CPU' column with safety-factor-adjusted values.\n")
		fmt.Printf("   See SPIKE-ANALYSIS.md for detailed interpretation guidance.\n\n")
	}
}

func printQuotaInformation(result *analyzer.RequestsSkewResult) {
	if len(result.NamespaceQuotas) == 0 {
		return // No quota information to display
	}

	fmt.Printf("\n📊 Namespace ResourceQuota & LimitRange Analysis:\n")
	fmt.Printf("═══════════════════════════════════════════════════\n\n")

	for _, quota := range result.NamespaceQuotas {
		fmt.Printf("Namespace: %s\n", quota.Namespace)

		if quota.HasResourceQuota {
			fmt.Printf("  ResourceQuota:\n")
			if quota.QuotaCPU.Hard != "" {
				fmt.Printf("    CPU:    %s used / %s hard (%.1f%% utilized)\n",
					quota.QuotaCPU.Used, quota.QuotaCPU.Hard, quota.QuotaCPU.Utilization)
			}
			if quota.QuotaMemory.Hard != "" {
				fmt.Printf("    Memory: %s used / %s hard (%.1f%% utilized)\n",
					quota.QuotaMemory.Used, quota.QuotaMemory.Hard, quota.QuotaMemory.Utilization)
			}

			if quota.PotentialQuotaSavings != nil {
				fmt.Printf("  Potential Quota Savings (if requests reduced to p95):\n")
				if quota.PotentialQuotaSavings.CPUSavings > 0 {
					fmt.Printf("    CPU:    %.2f cores (%.1f%% of quota)\n",
						quota.PotentialQuotaSavings.CPUSavings,
						quota.PotentialQuotaSavings.CPUPercent)
				}
				if quota.PotentialQuotaSavings.MemorySavings > 0 {
					fmt.Printf("    Memory: %.2f GiB (%.1f%% of quota)\n",
						quota.PotentialQuotaSavings.MemorySavings,
						quota.PotentialQuotaSavings.MemoryPercent)
				}
			}
		}

		if quota.HasLimitRange && quota.LimitRangeDefaults != nil {
			fmt.Printf("  LimitRange Defaults:\n")
			if quota.LimitRangeDefaults.DefaultRequestCPU != "" {
				fmt.Printf("    Default CPU Request:    %s\n", quota.LimitRangeDefaults.DefaultRequestCPU)
			}
			if quota.LimitRangeDefaults.DefaultRequestMemory != "" {
				fmt.Printf("    Default Memory Request: %s\n", quota.LimitRangeDefaults.DefaultRequestMemory)
			}
			if quota.LimitRangeDefaults.DefaultCPU != "" {
				fmt.Printf("    Default CPU Limit:      %s\n", quota.LimitRangeDefaults.DefaultCPU)
			}
			if quota.LimitRangeDefaults.DefaultMemory != "" {
				fmt.Printf("    Default Memory Limit:   %s\n", quota.LimitRangeDefaults.DefaultMemory)
			}
			if quota.LimitRangeDefaults.MinCPU != "" || quota.LimitRangeDefaults.MaxCPU != "" {
				fmt.Printf("    CPU Range:    %s - %s\n",
					quota.LimitRangeDefaults.MinCPU, quota.LimitRangeDefaults.MaxCPU)
			}
			if quota.LimitRangeDefaults.MinMemory != "" || quota.LimitRangeDefaults.MaxMemory != "" {
				fmt.Printf("    Memory Range: %s - %s\n",
					quota.LimitRangeDefaults.MinMemory, quota.LimitRangeDefaults.MaxMemory)
			}
		}

		fmt.Println()
	}

	fmt.Printf("💡 Quota Impact:\n")
	fmt.Printf("   - Reducing over-provisioned requests frees up quota for new workloads\n")
	fmt.Printf("   - Workloads using LimitRange defaults may not have intentionally set requests\n")
	fmt.Printf("   - Consider both actual usage AND quota constraints when right-sizing\n\n")
}

func printCriticalSignals(workloads []spikeWorkload) {
	// Collect workloads with critical signals
	var workloadsWithIssues []spikeWorkload
	for _, sw := range workloads {
		if sw.data.OOMKills > 0 || sw.data.Restarts > 0 || sw.data.Evictions > 0 || len(sw.data.CriticalEvents) > 0 {
			workloadsWithIssues = append(workloadsWithIssues, sw)
		}
	}

	if len(workloadsWithIssues) == 0 {
		fmt.Printf("\n✓ No critical signals detected during monitoring (no OOMKills, restarts, or evictions)\n")
		return
	}

	fmt.Printf("\n⚠️  Critical Signals Detected During Monitoring:\n")
	fmt.Printf("═══════════════════════════════════════════════════\n\n")

	for _, sw := range workloadsWithIssues {
		fmt.Printf("Workload: %s\n", sw.key)

		if sw.data.OOMKills > 0 {
			fmt.Printf("  🔴 OOMKills: %d - MEMORY REQUESTS TOO LOW!\n", sw.data.OOMKills)
		}
		if sw.data.Restarts > 0 {
			fmt.Printf("  ⚠️  Container Restarts: %d", sw.data.Restarts)
			if sw.data.LastTerminationTime != nil {
				ago := time.Since(*sw.data.LastTerminationTime)
				fmt.Printf(" (last: %s ago)", formatDuration(ago))
			}
			fmt.Println()
		}
		if sw.data.Evictions > 0 {
			fmt.Printf("  ⚠️  Pod Evictions: %d\n", sw.data.Evictions)
		}

		// Show termination reasons summary (show ALL if there were restarts)
		if len(sw.data.TerminationReasons) > 0 {
			fmt.Printf("  Termination Reasons:\n")
			for reason, count := range sw.data.TerminationReasons {
				// Mark normal completions vs problematic terminations
				icon := "⚠️ " // Default to warning for unknown reasons
				if reason == "OOMKilled" {
					icon = "🔴"
				} else if reason == "Error" || reason == "CrashLoopBackOff" || reason == "Unknown" || reason == "ContainerCannotRun" {
					icon = "⚠️ "
				} else if reason == "Completed" {
					icon = "✓ "
				}
				fmt.Printf("    %s %s: %d times\n", icon, reason, count)
			}
		}

		// Show exit codes summary (show ALL if there were restarts)
		if len(sw.data.ExitCodes) > 0 {
			fmt.Printf("  Exit Codes:\n")
			for code, count := range sw.data.ExitCodes {
				meaning := getExitCodeMeaning(code)
				// Mark normal exits vs problematic ones
				icon := "  "
				if code == 137 {
					icon = "🔴"
				} else if code != 0 {
					icon = "⚠️ "
				} else {
					icon = "✓ "
				}
				fmt.Printf("    %s %d (%s): %d times\n", icon, code, meaning, count)
			}
		}

		if len(sw.data.CriticalEvents) > 0 {
			fmt.Printf("  Recent Events:\n")
			// Show only last 5 events to avoid clutter
			maxEvents := 5
			startIdx := 0
			if len(sw.data.CriticalEvents) > maxEvents {
				startIdx = len(sw.data.CriticalEvents) - maxEvents
				fmt.Printf("    (showing last %d of %d events)\n", maxEvents, len(sw.data.CriticalEvents))
			}
			for _, event := range sw.data.CriticalEvents[startIdx:] {
				fmt.Printf("    • %s\n", event)
			}
		}
		fmt.Println()
	}

	fmt.Printf("💡 Critical Signal Interpretation:\n")
	fmt.Printf("   • OOMKills (exit code 137): Memory requests TOO LOW - increase immediately\n")
	fmt.Printf("   • Exit code 143 (SIGTERM): Graceful shutdown - usually normal\n")
	fmt.Printf("   • Exit code 139 (SIGSEGV): Segmentation fault - application bug\n")
	fmt.Printf("   • Exit code 1/2: Application error - check logs\n")
	fmt.Printf("   • Restarts: May indicate instability or resource pressure\n")
	fmt.Printf("   • Evictions: Node resource pressure, may need more cluster capacity\n")
	fmt.Printf("   • CrashLoopBackOff: Container repeatedly failing to start\n")
	fmt.Printf("   • High spike ratio + OOMKills: Classic sign of bursty workload needing higher limits\n\n")
	fmt.Printf("⚠️  WARNING: Do NOT reduce requests for workloads with:\n")
	fmt.Printf("   • OOMKills or exit code 137 (killed by system)\n")
	fmt.Printf("   • Frequent restarts or CrashLoopBackOff\n")
	fmt.Printf("   • Multiple different exit codes (indicates instability)\n")
	fmt.Printf("   These signals indicate the workload is already under-resourced or unstable.\n\n")
}

// getExitCodeMeaning returns human-readable explanation for common exit codes
func getExitCodeMeaning(exitCode int) string {
	switch exitCode {
	case 0:
		return "Success"
	case 1:
		return "General error"
	case 2:
		return "Misuse of shell command"
	case 126:
		return "Command cannot execute"
	case 127:
		return "Command not found"
	case 128:
		return "Invalid exit argument"
	case 130:
		return "SIGINT (Ctrl+C)"
	case 137:
		return "SIGKILL (usually OOMKilled)"
	case 139:
		return "SIGSEGV (segmentation fault)"
	case 143:
		return "SIGTERM (graceful termination)"
	case 255:
		return "Exit status out of range"
	default:
		// Check if it's a signal (128 + signal number)
		if exitCode > 128 && exitCode < 256 {
			signal := exitCode - 128
			return fmt.Sprintf("Killed by signal %d", signal)
		}
		return "Unknown error"
	}
}

// exportTableToFile renders the table output and saves it to a file
func exportTableToFile(result *analyzer.RequestsSkewResult, spikeData map[string]*metrics.SpikeData, exportFile string) error {
	// Create a bytes buffer to capture table output
	var buf bytes.Buffer

	// Create table writing to buffer
	table := tablewriter.NewWriter(&buf)
	table.Header([]string{"Namespace", "Workload", "Req CPU", "P99 CPU", "Skew", "Safety", "Impact"})

	for _, w := range result.Results {
		safetyLabel := "?"
		if w.Safety != nil {
			safetyLabel = string(w.Safety.Rating)
			// Add emoji indicators
			switch w.Safety.Rating {
			case "SAFE":
				safetyLabel = "✓ SAFE"
			case "CAUTION":
				safetyLabel = "⚠ CAUTION"
			case "RISKY":
				safetyLabel = "⚠ RISKY"
			case "UNSAFE":
				safetyLabel = "✗ UNSAFE"
			}
		}

		table.Append([]string{
			w.Namespace,
			w.Workload,
			fmt.Sprintf("%.2f", w.RequestedCPU),
			fmt.Sprintf("%.2f", w.P99UsedCPU),
			fmt.Sprintf("%.1fx", w.SkewCPU),
			safetyLabel,
			fmt.Sprintf("%.2f cores", w.ImpactScore),
		})
	}

	table.Render()

	// Add spike data if available
	if len(spikeData) > 0 {
		buf.WriteString("\n=== Spike Monitoring Results ===\n\n")

		// Collect and sort spike workloads
		var spikes []spikeWorkload
		for key, data := range spikeData {
			ratio := 0.0
			if data.AvgCPU > 0 {
				ratio = data.MaxCPU / data.AvgCPU
			}
			spikes = append(spikes, spikeWorkload{
				key:        key,
				data:       data,
				spikeRatio: ratio,
			})
		}

		// Sort by spike ratio descending
		sort.Slice(spikes, func(i, j int) bool {
			return spikes[i].spikeRatio > spikes[j].spikeRatio
		})

		// Write spike data
		for _, sw := range spikes {
			buf.WriteString(fmt.Sprintf("Workload: %s\n", sw.key))
			buf.WriteString(fmt.Sprintf("  Max CPU: %.4f cores (spike)\n", sw.data.MaxCPU))
			buf.WriteString(fmt.Sprintf("  Avg CPU: %.4f cores (baseline)\n", sw.data.AvgCPU))
			buf.WriteString(fmt.Sprintf("  Spike Ratio: %.2fx\n", sw.spikeRatio))
			buf.WriteString(fmt.Sprintf("  Samples: %d over %s\n", sw.data.SampleCount,
				sw.data.LastSeen.Sub(sw.data.FirstSeen).Round(time.Second)))

			if sw.data.OOMKills > 0 {
				buf.WriteString(fmt.Sprintf("  🔴 OOMKills: %d - MEMORY REQUESTS TOO LOW!\n", sw.data.OOMKills))
			}
			if sw.data.Restarts > 0 {
				buf.WriteString(fmt.Sprintf("  ⚠️  Container Restarts: %d", sw.data.Restarts))
				if sw.data.LastTerminationTime != nil {
					ago := time.Since(*sw.data.LastTerminationTime)
					buf.WriteString(fmt.Sprintf(" (last: %s ago)", formatDuration(ago)))
				}
				buf.WriteString("\n")
			}
			if sw.data.Evictions > 0 {
				buf.WriteString(fmt.Sprintf("  ⚠️  Pod Evictions: %d\n", sw.data.Evictions))
			}

			// Show termination reasons
			if len(sw.data.TerminationReasons) > 0 {
				buf.WriteString("  Termination Reasons:\n")
				for reason, count := range sw.data.TerminationReasons {
					icon := "⚠️ "
					if reason == "OOMKilled" {
						icon = "🔴"
					} else if reason == "Error" || reason == "CrashLoopBackOff" || reason == "Unknown" || reason == "ContainerCannotRun" {
						icon = "⚠️ "
					} else if reason == "Completed" {
						icon = "✓ "
					}
					buf.WriteString(fmt.Sprintf("    %s %s: %d times\n", icon, reason, count))
				}
			}

			// Show exit codes
			if len(sw.data.ExitCodes) > 0 {
				buf.WriteString("  Exit Codes:\n")
				for code, count := range sw.data.ExitCodes {
					meaning := getExitCodeMeaning(code)
					icon := "⚠️ "
					if code == 137 {
						icon = "🔴"
					} else if code != 0 {
						icon = "⚠️ "
					} else {
						icon = "✓ "
					}
					buf.WriteString(fmt.Sprintf("    %s %d (%s): %d times\n", icon, code, meaning, count))
				}
			}

			if len(sw.data.CriticalEvents) > 0 {
				buf.WriteString("  Recent Events:\n")
				maxEvents := 5
				startIdx := 0
				if len(sw.data.CriticalEvents) > maxEvents {
					startIdx = len(sw.data.CriticalEvents) - maxEvents
					buf.WriteString(fmt.Sprintf("    (showing last %d of %d events)\n", maxEvents, len(sw.data.CriticalEvents)))
				}
				for _, event := range sw.data.CriticalEvents[startIdx:] {
					buf.WriteString(fmt.Sprintf("    • %s\n", event))
				}
			}
			buf.WriteString("\n")
		}
	}

	// Write to file
	if err := os.WriteFile(exportFile, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write export file: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[kubenow] Table results exported to: %s\n", exportFile)
	return nil
}
