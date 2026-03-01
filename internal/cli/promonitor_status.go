package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ppiankov/kubenow/internal/promonitor"
)

var statusCmd = &cobra.Command{
	Use:   "status <kind>/<name>",
	Short: "Show latch status and percentiles for a workload",
	Long: `Show the status of a previously completed latch session.

Reads persisted latch data from ~/.kubenow/latch/ and displays:
  - Latch metadata (timestamp, duration, sample count)
  - CPU and memory percentiles (p50, p95, p99, max, avg)
  - Critical signals (OOMKills, restarts, evictions)
  - Validity status (fresh, no excessive gaps)

Examples:
  # Show status for a deployment
  kubenow pro-monitor status deployment/payment-api -n default

  # Show status for a statefulset
  kubenow pro-monitor status sts/postgres -n databases`,
	Args: cobra.ExactArgs(1),
	RunE: runStatus,
}

func init() {
	proMonitorCmd.AddCommand(statusCmd)
}

func runStatus(_ *cobra.Command, args []string) error {
	ref, err := promonitor.ParseWorkloadRef(args[0])
	if err != nil {
		return err
	}

	ns := GetNamespace()
	if ns == "" {
		ns = "default"
	}
	ref.Namespace = ns

	result, err := promonitor.LoadLatch(*ref)
	if err != nil {
		return err
	}

	printLatchStatus(result)
	return nil
}

func printLatchStatus(r *promonitor.LatchResult) {
	w := r.Workload
	stdoutf("Latch: %s/%s (%s)\n", strings.ToLower(w.Kind), w.Name, w.Namespace)

	ago := time.Since(r.Timestamp)
	stdoutf("  Recorded: %s (%s ago)\n", r.Timestamp.Format(time.RFC3339), formatLatchAge(ago))
	stdoutf("  Duration: %s  Samples: %d  Gaps: %d\n",
		r.Duration.String(), sampleCount(r), r.Gaps)

	if r.CPU != nil {
		stdoutf("  CPU:  avg=%s  p50=%s  p95=%s  p99=%s  max=%s\n",
			formatCPU(r.CPU.Avg), formatCPU(r.CPU.P50), formatCPU(r.CPU.P95),
			formatCPU(r.CPU.P99), formatCPU(r.CPU.Max))
	}

	if r.Memory != nil {
		stdoutf("  MEM:  avg=%s  p50=%s  p95=%s  p99=%s  max=%s\n",
			formatMem(r.Memory.Avg), formatMem(r.Memory.P50), formatMem(r.Memory.P95),
			formatMem(r.Memory.P99), formatMem(r.Memory.Max))
	}

	if r.Data != nil {
		stdoutf("  Signals: %d OOMKills, %d restarts, %d evictions\n",
			r.Data.OOMKills, r.Data.Restarts, r.Data.Evictions)
	}

	if r.Valid {
		stdoutf("  Status: VALID (fresh, no excessive gaps)\n")
	} else {
		stdoutf("  Status: INVALID (%s)\n", r.Reason)
	}
}

func sampleCount(r *promonitor.LatchResult) int {
	if r.Data != nil {
		return r.Data.SampleCount
	}
	return 0
}

// formatCPU formats CPU cores as millicores (e.g., 0.07 → "70m").
func formatCPU(cores float64) string {
	m := cores * 1000
	if m < 1 {
		return "0m"
	}
	return fmt.Sprintf("%.0fm", m)
}

// formatMem formats bytes as Mi (e.g., 178257920 → "170Mi").
func formatMem(bytes float64) string {
	mi := bytes / (1024 * 1024)
	if mi < 1 {
		return "0Mi"
	}
	return fmt.Sprintf("%.0fMi", mi)
}

func formatLatchAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
