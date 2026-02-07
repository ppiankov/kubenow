package cli

import (
	"github.com/spf13/cobra"
)

var proMonitorCmd = &cobra.Command{
	Use:   "pro-monitor",
	Short: "Resource alignment monitoring and bounded apply",
	Long: `Pro-monitor mode: observe workloads, generate resource alignment
recommendations, and optionally apply bounded changes to live clusters.

All mutation is gated by an admin-owned policy file. Without a valid policy,
pro-monitor operates in observe-only mode — recommendations can be exported
but never applied.

Policy file locations (checked in order):
  1. --policy flag
  2. $KUBENOW_POLICY environment variable
  3. /etc/kubenow/policy.yaml

Available subcommands:
  - validate-policy: Validate an admin policy file`,
}

var policyPath string

func init() {
	rootCmd.AddCommand(proMonitorCmd)
	proMonitorCmd.PersistentFlags().StringVar(&policyPath, "policy", "", "path to admin policy file")
}
