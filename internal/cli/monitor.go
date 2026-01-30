package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ppiankov/kubenow/internal/monitor"
	"github.com/ppiankov/kubenow/internal/util"
	"github.com/spf13/cobra"
)

var monitorConfig struct {
	namespace      string
	severityFilter string
	quiet          bool
	alertSound     bool
}

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Real-time problem monitoring (like 'top' for cluster issues)",
	Long: `Monitor your Kubernetes cluster in real-time for problems.

This command watches for critical issues like:
  • OOMKills - Out of memory kills
  • CrashLoopBackOff - Repeated crashes
  • ImagePullBackOff - Image pull failures
  • Failed pods - Container failures
  • Node issues - NotReady, DiskPressure, etc.

The screen stays mostly empty when everything is healthy (attention-first design).
A heartbeat indicator shows the monitor is actively running.

When problems occur, they appear immediately with:
  • Severity (FATAL/CRITICAL/WARNING)
  • Affected resource (namespace/pod/container)
  • Time since problem started
  • Problem details

Examples:
  # Monitor all namespaces
  kubenow monitor

  # Monitor specific namespace
  kubenow monitor --namespace production

  # Only show critical and fatal issues
  kubenow monitor --severity critical

  # Quiet mode (hide stats, show only problems)
  kubenow monitor --quiet

Philosophy:
  • Attention-first: Screen is empty when healthy
  • No navigation: Problems auto-appear
  • Disappears when everything works
  • Real-time: Problems show immediately (not batched)`,
	RunE: runMonitor,
}

func init() {
	rootCmd.AddCommand(monitorCmd)

	// Flags
	monitorCmd.Flags().StringVarP(&monitorConfig.namespace, "namespace", "n", "", "Monitor specific namespace (default: all)")
	monitorCmd.Flags().StringVar(&monitorConfig.severityFilter, "severity", "", "Minimum severity to show (fatal|critical|warning)")
	monitorCmd.Flags().BoolVar(&monitorConfig.quiet, "quiet", false, "Quiet mode: only show problems, hide stats")
	monitorCmd.Flags().BoolVar(&monitorConfig.alertSound, "alert", false, "Terminal bell on critical problems")
}

func runMonitor(cmd *cobra.Command, args []string) error {
	// Build Kubernetes client
	if IsVerbose() {
		fmt.Fprintln(os.Stderr, "[kubenow] Building Kubernetes client...")
	}

	kubeClient, err := util.BuildKubeClient(GetKubeconfig())
	if err != nil {
		return fmt.Errorf("failed to build Kubernetes client: %w", err)
	}

	// Parse severity filter
	var severityFilter monitor.Severity
	if monitorConfig.severityFilter != "" {
		switch monitorConfig.severityFilter {
		case "fatal":
			severityFilter = monitor.SeverityFatal
		case "critical":
			severityFilter = monitor.SeverityCritical
		case "warning":
			severityFilter = monitor.SeverityWarning
		default:
			return fmt.Errorf("invalid severity filter: %s (must be fatal, critical, or warning)", monitorConfig.severityFilter)
		}
	}

	// Create watcher
	config := monitor.Config{
		Namespace:      monitorConfig.namespace,
		SeverityFilter: severityFilter,
		Quiet:          monitorConfig.quiet,
		AlertSound:     monitorConfig.alertSound,
	}

	watcher := monitor.NewWatcher(kubeClient, config)

	// Start watching
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := watcher.Start(ctx); err != nil {
		return fmt.Errorf("failed to start watcher: %w", err)
	}

	// Run TUI in a loop (for print mode that returns to monitor)
	for {
		model := monitor.NewModel(watcher)
		p := tea.NewProgram(
			model,
			tea.WithAltScreen(),       // Use alternate screen buffer
			tea.WithMouseCellMotion(), // Enable mouse support
		)

		finalModel, err := p.Run()
		if err != nil {
			return fmt.Errorf("error running monitor: %w", err)
		}

		// Check what action was requested
		if m, ok := finalModel.(monitor.Model); ok {
			if m.ExportRequested() {
				return exportProblems(m)
			}
			if m.PrintRequested() {
				// Print to terminal (copyable), wait for input, then loop back
				printProblemsToTerminal(m)
				fmt.Println("\nPress Enter to return to monitor...")
				fmt.Scanln() // Wait for user
				continue     // Restart monitor loop
			}
		}

		// Normal exit
		break
	}

	return nil
}

func printProblemsToTerminal(m monitor.Model) {
	problems, events, stats := m.GetState()

	fmt.Println("\n═══════════════════════════════════════════════════════════════")
	fmt.Println("kubenow monitor - Current State (COPYABLE)")
	fmt.Println("═══════════════════════════════════════════════════════════════\n")

	if len(problems) == 0 {
		fmt.Println("✓ No active problems\n")
	} else {
		fmt.Printf("🔴 ACTIVE PROBLEMS (%d)\n\n", len(problems))

		for i, problem := range problems {
			fmt.Printf("[%d] %s - %s\n", i+1, problem.Severity, problem.Type)
			fmt.Printf("    Namespace: %s\n", problem.Namespace)
			fmt.Printf("    Pod: %s\n", problem.PodName)
			if problem.ContainerName != "" {
				fmt.Printf("    Container: %s\n", problem.ContainerName)
			}
			fmt.Printf("    Message: %s\n", problem.Message)
			if problem.Count > 1 {
				fmt.Printf("    Count: %d occurrences\n", problem.Count)
			}
			fmt.Println()
			fmt.Println("    Quick commands:")
			fmt.Printf("      kubectl -n %s describe pod %s\n", problem.Namespace, problem.PodName)
			fmt.Printf("      kubectl -n %s logs %s", problem.Namespace, problem.PodName)
			if problem.ContainerName != "" {
				fmt.Printf(" -c %s", problem.ContainerName)
			}
			fmt.Println()
			fmt.Printf("      kubectl -n %s get events --field-selector involvedObject.name=%s\n", problem.Namespace, problem.PodName)
			fmt.Println()
			if i < len(problems)-1 {
				fmt.Println("───────────────────────────────────────────────────────────────")
			}
		}
	}

	// Print recent events (last 10)
	if len(events) > 0 {
		fmt.Println("\n📊 RECENT EVENTS (last 5m)\n")
		count := 0
		for _, event := range events {
			if count >= 10 {
				break
			}
			if time.Since(event.Timestamp) > 5*time.Minute {
				break
			}
			fmt.Printf("  [%s] %s: %s/%s\n",
				event.Timestamp.Format("15:04:05"),
				event.Type,
				event.Namespace,
				event.Resource)
			fmt.Printf("      %s\n", event.Message)
			count++
		}
		fmt.Println()
	}

	// Print cluster stats (only if populated)
	if stats.TotalPods > 0 || stats.TotalNodes > 0 {
		fmt.Println("📈 CLUSTER STATUS\n")
		fmt.Printf("  Pods:  %d total  |  %d running  |  %d problem\n", stats.TotalPods, stats.RunningPods, stats.ProblemPods)
		fmt.Printf("  Nodes: %d total  |  %d ready    |  %d NotReady\n", stats.TotalNodes, stats.ReadyNodes, stats.NotReadyNodes)
		fmt.Println()
	}

	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println("TIP: Scroll up in your terminal to copy pod names and commands")
	fmt.Println("═══════════════════════════════════════════════════════════════")
}

func exportProblems(m monitor.Model) error {
	problems, _, stats := m.GetState()

	if len(problems) == 0 {
		fmt.Println("\n✓ No problems to export")
		return nil
	}

	// Generate filename with timestamp
	filename := fmt.Sprintf("kubenow-problems-%s.txt", time.Now().Format("20060102-150405"))

	// Open file
	f, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create export file: %w", err)
	}
	defer f.Close()

	// Write header
	fmt.Fprintf(f, "kubenow monitor - Problem Export\n")
	fmt.Fprintf(f, "Generated: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(f, "Cluster: %d namespaces, %d pods, %d nodes\n", countNamespaces(problems), stats.TotalPods, stats.TotalNodes)
	fmt.Fprintf(f, "═══════════════════════════════════════════════════════════════\n\n")

	// Write problems
	for i, problem := range problems {
		fmt.Fprintf(f, "[%d/%d] %s - %s\n", i+1, len(problems), problem.Severity, problem.Type)
		fmt.Fprintf(f, "  Namespace: %s\n", problem.Namespace)
		fmt.Fprintf(f, "  Pod: %s\n", problem.PodName)
		if problem.ContainerName != "" {
			fmt.Fprintf(f, "  Container: %s\n", problem.ContainerName)
		}
		fmt.Fprintf(f, "  Message: %s\n", problem.Message)
		fmt.Fprintf(f, "  First seen: %s ago\n", formatDuration(time.Since(problem.FirstSeen)))
		fmt.Fprintf(f, "  Last seen: %s ago\n", formatDuration(time.Since(problem.LastSeen)))
		if problem.Count > 1 {
			fmt.Fprintf(f, "  Occurrences: %d\n", problem.Count)
		}
		fmt.Fprintf(f, "\n")
		fmt.Fprintf(f, "  kubectl commands:\n")
		fmt.Fprintf(f, "    kubectl -n %s describe pod %s\n", problem.Namespace, problem.PodName)
		fmt.Fprintf(f, "    kubectl -n %s logs %s", problem.Namespace, problem.PodName)
		if problem.ContainerName != "" {
			fmt.Fprintf(f, " -c %s", problem.ContainerName)
		}
		fmt.Fprintf(f, "\n")
		fmt.Fprintf(f, "    kubectl -n %s get events --field-selector involvedObject.name=%s\n", problem.Namespace, problem.PodName)
		fmt.Fprintf(f, "\n")
		if i < len(problems)-1 {
			fmt.Fprintf(f, "───────────────────────────────────────────────────────────────\n\n")
		}
	}

	fmt.Printf("\n✓ Exported %d problems to: %s\n", len(problems), filename)
	fmt.Println("  You can now copy pod names and commands from this file.")
	return nil
}

func countNamespaces(problems []monitor.Problem) int {
	namespaces := make(map[string]bool)
	for _, p := range problems {
		namespaces[p.Namespace] = true
	}
	return len(namespaces)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}
