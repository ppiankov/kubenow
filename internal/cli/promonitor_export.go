package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ppiankov/kubenow/internal/promonitor"
	"github.com/ppiankov/kubenow/internal/util"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var exportConfig struct {
	format string
	output string
}

var exportCmd = &cobra.Command{
	Use:   "export <kind>/<name>",
	Short: "Export resource alignment recommendation",
	Long: `Export a resource alignment recommendation from a completed latch session.

Reads persisted latch data from ~/.kubenow/latch/, connects to the cluster
for current resource values, computes the recommendation, and outputs it
in the requested format.

Formats:
  patch    - SSA-compatible YAML patch with evidence comments (default)
  manifest - Full controller YAML with recommended values, volatile fields stripped
  diff     - Unified diff showing current vs recommended values
  json     - Machine-readable AlignmentRecommendation JSON

Export is always available regardless of admin policy.

Examples:
  # Export as SSA patch (pipe to kubectl)
  kubenow pro-monitor export deployment/payment-api -n default --format patch

  # Export full manifest
  kubenow pro-monitor export deployment/payment-api -n default --format manifest

  # Export diff for review
  kubenow pro-monitor export deployment/payment-api --format diff

  # Export JSON for automation
  kubenow pro-monitor export deployment/payment-api --format json -o rec.json`,
	Args: cobra.ExactArgs(1),
	RunE: runExport,
}

func init() {
	proMonitorCmd.AddCommand(exportCmd)
	exportCmd.Flags().StringVar(&exportConfig.format, "format", "patch", "output format (patch, manifest, diff, json)")
	exportCmd.Flags().StringVarP(&exportConfig.output, "output", "o", "", "write to file instead of stdout")
}

func runExport(cmd *cobra.Command, args []string) error {
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
		return fmt.Errorf("no latch data found: %w\nRun 'kubenow pro-monitor latch %s' first", err, args[0])
	}

	// Connect to cluster for current resources
	kubeClient, err := util.BuildKubeClient(GetKubeconfig())
	if err != nil {
		return fmt.Errorf("failed to build Kubernetes client: %w", err)
	}

	containers, err := promonitor.FetchContainerResources(ctx, kubeClient, ref)
	if err != nil {
		return fmt.Errorf("failed to read current resources: %w", err)
	}

	// Compute recommendation
	rec := promonitor.Recommend(&promonitor.RecommendInput{
		Latch:      latch,
		Containers: containers,
	})

	if len(rec.Containers) == 0 {
		fmt.Fprintf(os.Stderr, "No actionable recommendation produced.\n")
		for _, w := range rec.Warnings {
			fmt.Fprintf(os.Stderr, "  %s\n", w)
		}
		return nil
	}

	format := promonitor.ExportFormat(exportConfig.format)

	// For manifest format, fetch the full workload object
	var currentJSON []byte
	if format == promonitor.FormatManifest {
		currentJSON, err = fetchWorkloadJSON(ctx, kubeClient, ref)
		if err != nil {
			return fmt.Errorf("failed to fetch workload for manifest: %w", err)
		}
	}

	output, err := promonitor.Export(rec, format, currentJSON)
	if err != nil {
		return err
	}

	// Write output
	if exportConfig.output != "" {
		if err := os.WriteFile(exportConfig.output, []byte(output), 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Written to %s\n", exportConfig.output)
	} else {
		fmt.Print(output)
	}

	return nil
}

// fetchWorkloadJSON retrieves the full workload object as JSON bytes.
func fetchWorkloadJSON(ctx context.Context, client *kubernetes.Clientset, ref *promonitor.WorkloadRef) ([]byte, error) {
	switch ref.Kind {
	case "Deployment":
		obj, err := client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return json.Marshal(obj)
	case "StatefulSet":
		obj, err := client.AppsV1().StatefulSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return json.Marshal(obj)
	case "DaemonSet":
		obj, err := client.AppsV1().DaemonSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return json.Marshal(obj)
	default:
		return nil, fmt.Errorf("unsupported kind: %s", ref.Kind)
	}
}
