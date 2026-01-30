package cli

import (
	"github.com/spf13/cobra"
)

var chaosConfig LLMCommandConfig

var chaosCmd = &cobra.Command{
	Use:   "chaos",
	Short: "Chaos engineering analysis using LLM",
	Long: `Perform chaos engineering analysis using LLM.

This command analyzes your cluster's resilience, identifies potential failure
scenarios, and suggests chaos experiments to validate system reliability.

Examples:
  # Run chaos analysis
  kubenow chaos --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b

  # Focus on specific namespace for chaos testing
  kubenow --namespace production chaos --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b

  # Export chaos report
  kubenow chaos --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b --output chaos-scenarios.md`,
	RunE: func(cmd *cobra.Command, args []string) error {
		chaosConfig.Mode = "chaos"
		if err := RunLLMCommand(cmd, &chaosConfig); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(chaosCmd)
	addLLMFlags(chaosCmd, &chaosConfig)
}
