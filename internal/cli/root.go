package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const version = "0.1.5"

var (
	// Global flags
	cfgFile    string
	kubeconfig string
	namespace  string
	verbose    bool
)

// rootCmd represents the base command
var rootCmd = &cobra.Command{
	Use:   "kubenow",
	Short: "Kubernetes cluster analyzer with LLM-powered triage and deterministic cost optimization",
	Long: `kubenow is a powerful Kubernetes cluster analyzer that provides:

• LLM-Powered Analysis: Incident triage, pod diagnosis, team reports, compliance checks
• Deterministic Analysis: Resource optimization, cluster topology simulation

Features:
  - Multi-mode LLM analysis (incident, pod, teamlead, compliance, chaos)
  - requests-skew: Identify over-provisioned resources
  - node-footprint: Simulate alternative cluster topologies
  - Watch mode for continuous monitoring
  - Multi-format export (JSON, Markdown, HTML)`,
	Version: version,
	// Disable default completion command
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
}

// Execute adds all child commands to the root command and sets flags appropriately
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.kubenow.yaml)")
	rootCmd.PersistentFlags().StringVar(&kubeconfig, "kubeconfig", "", "path to kubeconfig file (default is $KUBECONFIG or $HOME/.kube/config)")
	rootCmd.PersistentFlags().StringVarP(&namespace, "namespace", "n", "", "kubernetes namespace to analyze (default is all namespaces)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")

	// Bind flags to viper
	viper.BindPFlag("kubeconfig", rootCmd.PersistentFlags().Lookup("kubeconfig"))
	viper.BindPFlag("namespace", rootCmd.PersistentFlags().Lookup("namespace"))
	viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
}

// initConfig reads in config file and ENV variables if set
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding home directory: %v\n", err)
			os.Exit(1)
		}

		// Search config in home directory with name ".kubenow" (without extension)
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".kubenow")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in
	if err := viper.ReadInConfig(); err == nil && verbose {
		fmt.Fprintf(os.Stderr, "Using config file: %s\n", viper.ConfigFileUsed())
	}
}

// GetKubeconfig returns the kubeconfig path from flags or viper
func GetKubeconfig() string {
	if kubeconfig != "" {
		return kubeconfig
	}
	return viper.GetString("kubeconfig")
}

// GetNamespace returns the namespace from flags or viper
func GetNamespace() string {
	if namespace != "" {
		return namespace
	}
	return viper.GetString("namespace")
}

// IsVerbose returns the verbose flag value
func IsVerbose() bool {
	return verbose || viper.GetBool("verbose")
}
