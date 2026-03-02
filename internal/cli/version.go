package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show kubenow version information",
	Long:  `Display version information for kubenow including Go version and platform details.`,
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Printf("kubenow %s (commit: %s, built: %s, go: %s)\n",
			version, commit, date, runtime.Version())
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
