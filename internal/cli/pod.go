package cli

import (
	"github.com/spf13/cobra"
)

var podConfig LLMCommandConfig

var podCmd = &cobra.Command{
	Use:   "pod",
	Short: "Analyze pod issues using LLM",
	Long: `Analyze pod-specific issues using LLM-powered diagnosis.

This command performs detailed pod-level analysis, identifying container failures,
resource issues, configuration problems, and startup/crash loops.

Examples:
  # Analyze pod issues
  kubenow pod --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b

  # Focus on specific pods with pattern
  kubenow pod --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b --include-pods "payment-*"

  # Enhanced remediation guidance
  kubenow pod --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b --enhance-remediation`,
	RunE: func(cmd *cobra.Command, args []string) error {
		podConfig.Mode = "pod"
		if err := RunLLMCommand(cmd, &podConfig); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(podCmd)
	addLLMFlags(podCmd, &podConfig)
}
