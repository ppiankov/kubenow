package cli

import (
	"github.com/spf13/cobra"
)

var defaultConfig LLMCommandConfig

var defaultCmd = &cobra.Command{
	Use:   "default",
	Short: "Default cluster analysis using LLM",
	Long: `Perform general-purpose cluster analysis using LLM.

This command provides a comprehensive overview of your cluster health,
identifying the most critical issues across all areas.

Examples:
  # Run default analysis
  kubenow default --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b

  # With all enhancements
  kubenow default --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b \
    --enhance-technical --enhance-priority --enhance-remediation

  # Export to HTML report
  kubenow default --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b --output report.html`,
	RunE: func(cmd *cobra.Command, args []string) error {
		defaultConfig.Mode = "default"
		if err := RunLLMCommand(cmd, &defaultConfig); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(defaultCmd)
	addLLMFlags(defaultCmd, &defaultConfig)
}
