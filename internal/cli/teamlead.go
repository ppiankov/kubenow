package cli

import (
	"github.com/spf13/cobra"
)

var teamleadConfig LLMCommandConfig

var teamleadCmd = &cobra.Command{
	Use:   "teamlead",
	Short: "Generate team lead report using LLM",
	Long: `Generate comprehensive team lead reports using LLM.

This command creates executive summaries and team-focused reports suitable
for standups, sprint reviews, and management communication.

Examples:
  # Generate team lead report
  kubenow teamlead --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b

  # Export to Markdown for sharing
  kubenow teamlead --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b --output report.md

  # Focus on production namespace
  kubenow --namespace production teamlead --llm-endpoint http://localhost:11434/v1 --model mixtral:8x22b`,
	RunE: func(cmd *cobra.Command, args []string) error {
		teamleadConfig.Mode = "teamlead"
		if err := RunLLMCommand(cmd, &teamleadConfig); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(teamleadCmd)
	addLLMFlags(teamleadCmd, &teamleadConfig)
}
