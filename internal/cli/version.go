package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

var versionJSON bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show kubenow version information",
	Long:  `Display version information for kubenow including Go version and platform details.`,
	Run: func(_ *cobra.Command, _ []string) {
		info := map[string]string{
			"version":   version,
			"commit":    commit,
			"built":     date,
			"goVersion": runtime.Version(),
			"os":        runtime.GOOS,
			"arch":      runtime.GOARCH,
		}
		if versionJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(info); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		fmt.Printf("kubenow %s (commit: %s, built: %s, go: %s)\n",
			version, commit, date, runtime.Version())
	},
}

func init() {
	versionCmd.Flags().BoolVar(&versionJSON, "json", false, "output version as JSON")
	rootCmd.AddCommand(versionCmd)
}
