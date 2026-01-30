package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/ppiankov/kubenow/internal/analyzer"
	"github.com/ppiankov/kubenow/internal/metrics"
	"github.com/ppiankov/kubenow/internal/util"
	"github.com/spf13/cobra"
)

var nodeFootprintConfig struct {
	prometheusURL     string
	autoDetect        bool
	window            string
	percentile        string
	nodeTypes         string
	output            string
	exportFile        string
	prometheusTimeout string
	silent            bool
}

var nodeFootprintCmd = &cobra.Command{
	Use:   "node-footprint",
	Short: "Simulate alternative cluster topologies",
	Long: `Simulate alternative node configurations based on historical workload data.

This command analyzes your cluster's workload envelope and runs bin-packing
simulations to identify potential node topology optimizations. It shows what
smaller or different node configurations would have been sufficient based on
historical usage.

Philosophy:
  - Simulation-based: Tests alternative topologies against historical data
  - Evidence-based: Claims "this would have worked historically"
  - Never prescriptive: Doesn't say "you should do this"
  - Reproducible: Same inputs → same outputs

Examples:
  # Basic analysis with default candidates
  kubenow analyze node-footprint --prometheus-url http://localhost:9090

  # Use p99 envelope instead of p95
  kubenow analyze node-footprint --prometheus-url http://localhost:9090 \
    --percentile p99

  # Specify custom node types to simulate
  kubenow analyze node-footprint --prometheus-url http://localhost:9090 \
    --node-types "c5.large,c5.xlarge,c5.2xlarge"

  # Export results to JSON
  kubenow analyze node-footprint --prometheus-url http://localhost:9090 \
    --output json --export-file footprint.json`,
	RunE: runNodeFootprint,
}

func init() {
	analyzeCmd.AddCommand(nodeFootprintCmd)

	// Required flags
	nodeFootprintCmd.Flags().StringVar(&nodeFootprintConfig.prometheusURL, "prometheus-url", "", "Prometheus endpoint (e.g., http://prometheus:9090)")
	nodeFootprintCmd.Flags().BoolVar(&nodeFootprintConfig.autoDetect, "auto-detect-prometheus", false, "Auto-discover Prometheus in cluster")

	// Optional flags
	nodeFootprintCmd.Flags().StringVar(&nodeFootprintConfig.window, "window", "30d", "Time window for analysis (e.g., 7d, 24h, 30d)")
	nodeFootprintCmd.Flags().StringVar(&nodeFootprintConfig.percentile, "percentile", "p95", "Usage percentile: p50, p95, p99")
	nodeFootprintCmd.Flags().StringVar(&nodeFootprintConfig.nodeTypes, "node-types", "", "Comma-separated node types to simulate (e.g., 'c5.xlarge,c5.2xlarge')")
	nodeFootprintCmd.Flags().StringVar(&nodeFootprintConfig.output, "output", "table", "Output format: table|json")
	nodeFootprintCmd.Flags().StringVar(&nodeFootprintConfig.exportFile, "export-file", "", "Save to file (optional)")
	nodeFootprintCmd.Flags().StringVar(&nodeFootprintConfig.prometheusTimeout, "prometheus-timeout", "30s", "Query timeout")

	// CI/CD flags
	nodeFootprintCmd.Flags().BoolVar(&nodeFootprintConfig.silent, "silent", false, "Suppress progress output (for CI/CD pipelines)")
}

func runNodeFootprint(cmd *cobra.Command, args []string) error {
	// Set silent mode for CI/CD
	if nodeFootprintConfig.silent {
		analyzer.SilentMode = true
	}

	// Validate flags
	if nodeFootprintConfig.prometheusURL == "" && !nodeFootprintConfig.autoDetect {
		return fmt.Errorf("either --prometheus-url or --auto-detect-prometheus is required")
	}

	if nodeFootprintConfig.output != "table" && nodeFootprintConfig.output != "json" {
		return fmt.Errorf("--output must be 'table' or 'json'")
	}

	validPercentiles := map[string]bool{"p50": true, "p95": true, "p99": true}
	if !validPercentiles[nodeFootprintConfig.percentile] {
		return fmt.Errorf("--percentile must be one of: p50, p95, p99")
	}

	// Parse window duration
	window, err := metrics.ParseDuration(nodeFootprintConfig.window)
	if err != nil {
		return fmt.Errorf("invalid window: %w", err)
	}

	// Parse timeout
	timeout, err := time.ParseDuration(nodeFootprintConfig.prometheusTimeout)
	if err != nil {
		return fmt.Errorf("invalid timeout: %w", err)
	}

	// Parse node types
	var nodeTypes []string
	if nodeFootprintConfig.nodeTypes != "" {
		nodeTypes = strings.Split(nodeFootprintConfig.nodeTypes, ",")
		// Trim whitespace
		for i := range nodeTypes {
			nodeTypes[i] = strings.TrimSpace(nodeTypes[i])
		}
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
		fmt.Fprintf(os.Stderr, "[kubenow] Connecting to Prometheus: %s\n", nodeFootprintConfig.prometheusURL)
	}

	promConfig := metrics.Config{
		PrometheusURL: nodeFootprintConfig.prometheusURL,
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

	if IsVerbose() {
		fmt.Fprintln(os.Stderr, "[kubenow] Analyzing cluster node footprint...")
	}

	// Create analyzer
	analyzerConfig := analyzer.NodeFootprintConfig{
		Window:     window,
		Percentile: nodeFootprintConfig.percentile,
		NodeTypes:  nodeTypes,
	}

	footprintAnalyzer := analyzer.NewNodeFootprintAnalyzer(kubeClient, metricsProvider, analyzerConfig)

	// Run analysis
	result, err := footprintAnalyzer.Analyze(ctx)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	// Output results
	if nodeFootprintConfig.output == "json" {
		return outputNodeFootprintJSON(result, nodeFootprintConfig.exportFile)
	}

	return outputNodeFootprintTable(result, nodeFootprintConfig.exportFile)
}

func outputNodeFootprintJSON(result *analyzer.NodeFootprintResult, exportFile string) error {
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

func outputNodeFootprintTable(result *analyzer.NodeFootprintResult, exportFile string) error {
	// Create table
	table := tablewriter.NewWriter(os.Stdout)
	table.Header([]string{"Scenario", "Nodes", "Avg CPU%", "Avg Mem%", "Headroom", "Feasibility", "Notes"})

	for _, scenario := range result.Scenarios {
		feasibility := "YES"
		if !scenario.Feasible {
			feasibility = "NO"
		} else if scenario.Headroom == "very low" {
			feasibility = "YES (tight)"
		}

		notes := scenario.Notes
		if scenario.EstimatedSavings != "" {
			notes = fmt.Sprintf("%s\n%s", scenario.EstimatedSavings, notes)
		}

		table.Append([]string{
			scenario.Name,
			fmt.Sprintf("%d", scenario.NodeCount),
			fmt.Sprintf("%.0f%%", scenario.AvgCPUUtilization),
			fmt.Sprintf("%.0f%%", scenario.AvgMemUtilization),
			scenario.Headroom,
			feasibility,
			notes,
		})
	}

	// Print summary
	fmt.Printf("\n=== Node Footprint Analysis ===\n")
	fmt.Printf("Window: %s | Percentile: %s | Workloads: %d\n\n",
		result.Metadata.Window,
		result.Metadata.Percentile,
		result.Metadata.WorkloadCount)

	fmt.Printf("Current Topology:\n")
	fmt.Printf("  Node Type: %s\n", result.CurrentTopology.NodeType)
	fmt.Printf("  Node Count: %d\n", result.CurrentTopology.NodeCount)
	fmt.Printf("  Total CPU: %.0f cores\n", result.CurrentTopology.TotalCPU)
	fmt.Printf("  Total Memory: %.0fGi\n", result.CurrentTopology.TotalMemoryGi)
	fmt.Printf("  Avg CPU Utilization: %.0f%%\n", result.CurrentTopology.AvgCPUUtilization)
	fmt.Printf("  Avg Memory Utilization: %.0f%%\n\n", result.CurrentTopology.AvgMemUtilization)

	fmt.Printf("Workload Envelope (%s):\n", result.Metadata.Percentile)
	fmt.Printf("  Total CPU Required: %.2f cores\n", result.WorkloadEnvelope.TotalCPURequired)
	fmt.Printf("  Total Memory Required: %.2fGi\n", result.WorkloadEnvelope.TotalMemoryRequired/(1024*1024*1024))
	fmt.Printf("  Pod Count: %d\n\n", result.WorkloadEnvelope.PodCount)

	fmt.Println("Scenarios:")
	// Render table
	table.Render()

	// Print safety warnings if any scenarios have unstable workloads
	hasUnstableWorkloads := false
	unstableCount := 0

	for _, scenario := range result.Scenarios {
		if scenario.UnstableWorkloads > 0 {
			hasUnstableWorkloads = true
			unstableCount = scenario.UnstableWorkloads
			break
		}
	}

	if hasUnstableWorkloads && result.WorkloadEnvelope.UnstableWorkloadCount > 0 {
		fmt.Printf("\n⚠️  Safety Warnings:\n")
		fmt.Printf("═══════════════════\n\n")
		fmt.Printf("⚠ CAUTION: %d workload(s) have recent failures (restarts > 5 in last 7 days)\n", unstableCount)

		if len(result.WorkloadEnvelope.UnstableWorkloads) > 0 {
			fmt.Printf("\nUnstable workloads:\n")
			for _, w := range result.WorkloadEnvelope.UnstableWorkloads {
				fmt.Printf("  • %s\n", w)
			}
		}

		fmt.Printf("\nRecommendations:\n")
		fmt.Printf("  • Stabilize these workloads before reducing node count\n")
		fmt.Printf("  • Investigate root causes (OOMKills, crashes, config issues)\n")
		fmt.Printf("  • Topology changes may increase risk for already-unstable workloads\n")
	}

	// Print philosophy reminder
	fmt.Printf("\nNote: These scenarios show what would have been sufficient based on historical %s usage.\n", result.Metadata.Percentile)
	fmt.Println("Always validate with your specific requirements and add safety margins for production.")

	return nil
}
