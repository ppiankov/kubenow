package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ppiankov/kubenow/internal/trend"
)

var trendsConfig struct {
	days   int
	output string
}

var trendsCmd = &cobra.Command{
	Use:   "trends",
	Short: "Show historical trend of requests-skew analysis",
	Long: `Display how resource waste has changed over time based on saved snapshots.

Snapshots are saved automatically when running requests-skew with --track-trends.
This command loads historical snapshots and shows per-workload skew deltas.

Examples:
  # Show trends for last 30 days
  kubenow analyze trends

  # Show trends for last 7 days as JSON
  kubenow analyze trends --days 7 --output json`,
	RunE: runTrends,
}

func init() {
	analyzeCmd.AddCommand(trendsCmd)
	trendsCmd.Flags().IntVar(&trendsConfig.days, "days", 30, "Number of days to look back")
	trendsCmd.Flags().StringVar(&trendsConfig.output, "output", "table", "Output format: table|json")
}

func runTrends(_ *cobra.Command, _ []string) error {
	history, err := trend.LoadHistory(trendsConfig.days)
	if err != nil {
		return fmt.Errorf("failed to load trend history: %w", err)
	}

	if len(history) == 0 {
		fmt.Fprintln(os.Stderr, "No trend data found. Run 'analyze requests-skew --track-trends' to start collecting snapshots.")
		return nil
	}

	summary := trend.ComputeTrend(history)
	summary.Days = trendsConfig.days

	if trendsConfig.output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	}

	renderTrendTable(summary)
	return nil
}

func renderTrendTable(s *trend.Summary) {
	fmt.Printf("TREND: requests-skew (last %d days, %d snapshots)\n", s.Days, s.Snapshots)
	fmt.Println("───────────────────────────────────────────────────────────────")
	fmt.Printf("  %-25s %-12s %-12s %s\n", "Workload", "CPU Skew", "Delta", "Direction")
	fmt.Println("───────────────────────────────────────────────────────────────")

	for i := range s.Workloads {
		w := &s.Workloads[i]
		arrow := directionArrow(w.Direction)
		fmt.Printf("  %-25s %-12.2f %-+12.2f %s %s\n",
			w.Namespace+"/"+w.Workload,
			w.CurrentCPU,
			w.DeltaCPU,
			arrow,
			w.Direction,
		)
	}

	fmt.Println("───────────────────────────────────────────────────────────────")
	d := &s.WasteDelta
	fmt.Printf("  Total CPU waste: %.2f -> %.2f cores (%+.2f)\n", d.OldestCPU, d.CurrentCPU, d.DeltaCPU)
	fmt.Printf("  Total Mem waste: %.2f -> %.2f GiB (%+.2f)\n", d.OldestMem, d.CurrentMem, d.DeltaMem)
}

func directionArrow(direction string) string {
	switch direction {
	case "improving":
		return "v"
	case "worsening":
		return "^"
	default:
		return "-"
	}
}
