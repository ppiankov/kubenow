package cli

import (
	"github.com/spf13/cobra"
)

var incidentConfig LLMCommandConfig

var incidentCmd = &cobra.Command{
	Use:   "incident",
	Short: "Analyze incidents using LLM",
	Long: `Analyze incidents using LLM-powered triage.

This command performs incident-focused analysis of your Kubernetes cluster,
identifying and prioritizing issues that require immediate attention.

Examples:
  # Analyze incidents with local LLM
  kubenow incident --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b

  # Analyze specific namespace with enhanced technical details
  kubenow --namespace production incident --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b --enhance-technical

  # Watch mode for continuous incident monitoring
  kubenow incident --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b --watch-interval 1m

  # Export incident report to JSON
  kubenow incident --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b --output incident-report.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		incidentConfig.Mode = "incident"
		if err := RunLLMCommand(cmd, &incidentConfig); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(incidentCmd)
	addLLMFlags(incidentCmd, &incidentConfig)
}
