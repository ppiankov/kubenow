package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/kubenow/internal/metrics"
	"github.com/ppiankov/kubenow/internal/promonitor"
)

var trackConfig struct {
	auditPath     string
	prometheusURL string
	format        string
	since         string
}

var trackCmd = &cobra.Command{
	Use:   "track [<kind>/<name>]",
	Short: "Track outcomes of past resource applies",
	Long: `Track and classify outcomes of past resource applies.

Scans audit bundles for applied recommendations, queries Prometheus for
post-apply resource usage, and classifies each apply as:

  SAFE     — peak usage below 80% of new request (headroom confirmed)
  TIGHT    — peak usage 80-95% of new request (working but narrow)
  WRONG    — peak usage above 95% or memory exceeds limit (OOM risk)
  PENDING  — applied less than 24h ago (insufficient observation window)
  NO_DATA  — no Prometheus metrics available for comparison

Without --prometheus-url, all past-24h applies show as NO_DATA.

Examples:
  # Track all applies from the last 7 days
  kubenow pro-monitor track --audit-path /var/kubenow/audit --since 7d

  # Track a specific workload with Prometheus
  kubenow pro-monitor track deployment/payment-api \
    --audit-path /var/kubenow/audit \
    --prometheus-url http://prometheus:9090

  # JSON output for CI integration
  kubenow pro-monitor track --audit-path /var/kubenow/audit --format json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runTrack,
}

func init() {
	proMonitorCmd.AddCommand(trackCmd)
	trackCmd.Flags().StringVar(&trackConfig.auditPath, "audit-path", "", "path to audit bundle directory (required)")
	trackCmd.Flags().StringVar(&trackConfig.prometheusURL, "prometheus-url", "", "Prometheus endpoint for post-apply usage metrics")
	trackCmd.Flags().StringVar(&trackConfig.format, "format", "table", "output format: table or json")
	trackCmd.Flags().StringVar(&trackConfig.since, "since", "", "only show applies within this window (e.g., 7d, 30d, 24h)")
}

func runTrack(_ *cobra.Command, args []string) error {
	if trackConfig.auditPath == "" {
		return fmt.Errorf("--audit-path is required")
	}

	// Parse optional workload filter
	var workloadFilter *promonitor.WorkloadRef
	if len(args) == 1 {
		ref, err := promonitor.ParseWorkloadRef(args[0])
		if err != nil {
			return err
		}
		workloadFilter = ref
	}

	// Parse --since duration (supports "7d" shorthand)
	var since time.Duration
	if trackConfig.since != "" {
		d, err := parseSinceDuration(trackConfig.since)
		if err != nil {
			return fmt.Errorf("invalid --since %q: %w", trackConfig.since, err)
		}
		since = d
	}

	// Optionally connect to Prometheus
	var metricsProvider metrics.MetricsProvider
	if trackConfig.prometheusURL != "" {
		client, err := metrics.NewPrometheusClient(metrics.Config{
			PrometheusURL: trackConfig.prometheusURL,
		})
		if err != nil {
			return fmt.Errorf("failed to connect to Prometheus: %w", err)
		}
		metricsProvider = client
	}

	now := time.Now()
	summary, err := promonitor.RunTrack(context.Background(), &promonitor.TrackConfig{
		AuditPath:      trackConfig.auditPath,
		Metrics:        metricsProvider,
		Since:          since,
		WorkloadFilter: workloadFilter,
		Now:            now,
	})
	if err != nil {
		return err
	}

	switch trackConfig.format {
	case "json":
		output, fmtErr := promonitor.FormatTrackJSON(summary)
		if fmtErr != nil {
			return fmtErr
		}
		fmt.Print(output)
	case "table":
		fmt.Print(promonitor.FormatTrackTable(summary))
	default:
		return fmt.Errorf("unsupported format %q (supported: table, json)", trackConfig.format)
	}

	// Exit non-zero if any WRONG outcomes detected (useful for CI)
	if summary.Wrong > 0 {
		fmt.Fprintf(os.Stderr, "[track] %d WRONG outcome(s) detected\n", summary.Wrong)
	}

	return nil
}

// parseSinceDuration parses a duration string that supports "d" suffix for days.
// Examples: "7d" → 168h, "30d" → 720h, "24h" → 24h, "1h30m" → 1h30m
func parseSinceDuration(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		daysStr := strings.TrimSuffix(s, "d")
		days, err := strconv.Atoi(daysStr)
		if err != nil {
			return 0, fmt.Errorf("invalid days %q: %w", daysStr, err)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}
