package cli

import (
	"github.com/spf13/cobra"
)

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Deterministic cluster analysis",
	Long: `Run deterministic cluster analysis commands.

The analyze command group provides data-driven, deterministic analysis of your
Kubernetes cluster without using AI/ML. All analysis is based on historical
metrics from Prometheus and reproducible calculations.

Available analysis types:
  - requests-skew: Identify over-provisioned resource requests
  - node-footprint: Simulate alternative cluster topologies

Examples:
  # Find over-provisioned resources
  kubenow analyze requests-skew --prometheus-url http://prometheus:9090

  # Simulate alternative node configurations
  kubenow analyze node-footprint --prometheus-url http://prometheus:9090`,
}

func init() {
	rootCmd.AddCommand(analyzeCmd)
}
