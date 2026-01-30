package cli

import (
	"github.com/spf13/cobra"
)

var complianceConfig LLMCommandConfig

var complianceCmd = &cobra.Command{
	Use:   "compliance",
	Short: "Compliance analysis using LLM",
	Long: `Perform compliance and security analysis using LLM.

This command analyzes your cluster for compliance issues, security concerns,
and best practice violations.

Examples:
  # Run compliance check
  kubenow compliance --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b

  # Export compliance report to HTML
  kubenow compliance --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b --output compliance.html

  # Detailed compliance analysis
  kubenow compliance --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b --enhance-technical --enhance-priority`,
	RunE: func(cmd *cobra.Command, args []string) error {
		complianceConfig.Mode = "compliance"
		if err := RunLLMCommand(cmd, &complianceConfig); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(complianceCmd)
	addLLMFlags(complianceCmd, &complianceConfig)
}
