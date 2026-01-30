package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ppiankov/kubenow/internal/export"
	"github.com/ppiankov/kubenow/internal/llm"
	"github.com/ppiankov/kubenow/internal/prompt"
	"github.com/ppiankov/kubenow/internal/result"
	"github.com/ppiankov/kubenow/internal/snapshot"
	"github.com/ppiankov/kubenow/internal/util"
	"github.com/ppiankov/kubenow/internal/watch"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// LLMCommandConfig holds common configuration for LLM commands
type LLMCommandConfig struct {
	// Mode for prompt template selection
	Mode string

	// Required flags
	LLMEndpoint string
	Model       string

	// Optional flags
	APIKey         string
	Format         string
	MaxPods        int
	LogLines       int
	TimeoutSeconds int
	MaxConcurrent  int
	OutputFile     string

	// Filters
	IncludePods       string
	ExcludePods       string
	IncludeNamespaces string
	ExcludeNamespaces string
	IncludeKeywords   string
	ExcludeKeywords   string
	ProblemHint       string

	// Enhancements
	EnhanceTechnical   bool
	EnhancePriority    bool
	EnhanceRemediation bool

	// Watch mode
	WatchInterval     string
	WatchIterations   int
	WatchAlertNewOnly bool
}

// RunLLMCommand executes an LLM analysis command
func RunLLMCommand(cmd *cobra.Command, config *LLMCommandConfig) error {
	// Validate required fields
	if config.LLMEndpoint == "" || config.Model == "" {
		return fmt.Errorf("--llm-endpoint and --model are required")
	}

	if config.Format != "human" && config.Format != "json" {
		return fmt.Errorf("--format must be 'human' or 'json'")
	}

	// Build Kubernetes client
	if IsVerbose() {
		fmt.Fprintln(os.Stderr, "[kubenow] Building Kubernetes client...")
	}

	clientset, err := util.BuildKubeClient(GetKubeconfig())
	if err != nil {
		return fmt.Errorf("failed to build Kubernetes client: %w", err)
	}

	// Extract cluster name
	clusterName := extractClusterName(GetKubeconfig())

	// Setup filters
	filters := snapshot.Filters{
		IncludePods:       config.IncludePods,
		ExcludePods:       config.ExcludePods,
		IncludeNamespaces: config.IncludeNamespaces,
		ExcludeNamespaces: config.ExcludeNamespaces,
		IncludeKeywords:   config.IncludeKeywords,
		ExcludeKeywords:   config.ExcludeKeywords,
	}

	// Setup enhancements
	enhancements := prompt.PromptEnhancements{
		Technical:   config.EnhanceTechnical,
		Priority:    config.EnhancePriority,
		Remediation: config.EnhanceRemediation,
	}

	// Setup LLM client
	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	llmClient := llm.Client{
		Endpoint: config.LLMEndpoint,
		Model:    config.Model,
		APIKey:   config.APIKey,
		Timeout:  timeout,
	}

	// Check if watch mode is enabled
	if config.WatchInterval != "" {
		return runWatchMode(clientset, &llmClient, config, filters, enhancements)
	}

	// Single execution mode
	return runSingleExecution(clientset, &llmClient, config, filters, enhancements, clusterName)
}

// runWatchMode executes the LLM command in watch mode
func runWatchMode(clientset *kubernetes.Clientset, llmClient *llm.Client, config *LLMCommandConfig, filters snapshot.Filters, enhancements prompt.PromptEnhancements) error {
	interval, err := time.ParseDuration(config.WatchInterval)
	if err != nil {
		return fmt.Errorf("invalid watch-interval: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	setupSignalHandler(cancel)

	if IsVerbose() {
		fmt.Fprintf(os.Stderr, "[kubenow] Starting watch mode (interval: %s)\n", interval)
		if config.WatchIterations > 0 {
			fmt.Fprintf(os.Stderr, "[kubenow] Max iterations: %d\n", config.WatchIterations)
		}
		if config.WatchAlertNewOnly {
			fmt.Fprintln(os.Stderr, "[kubenow] Alert mode: New issues only")
		}
	}

	watchConfig := watch.Config{
		Interval:      interval,
		MaxIterations: config.WatchIterations,
		AlertNewOnly:  config.WatchAlertNewOnly,
		Namespace:     GetNamespace(),
		MaxPods:       config.MaxPods,
		LogLines:      config.LogLines,
		MaxConcurrent: config.MaxConcurrent,
		Filters:       filters,
		Mode:          config.Mode,
		ProblemHint:   config.ProblemHint,
		Enhancements:  enhancements,
		LLMClient:     llmClient,
	}

	if err := watch.Run(ctx, clientset, watchConfig); err != nil && err != context.Canceled {
		return fmt.Errorf("watch error: %w", err)
	}

	return nil
}

// runSingleExecution executes the LLM command once
func runSingleExecution(clientset *kubernetes.Clientset, llmClient *llm.Client, config *LLMCommandConfig, filters snapshot.Filters, enhancements prompt.PromptEnhancements, clusterName string) error {
	if IsVerbose() {
		fmt.Fprintln(os.Stderr, "[kubenow] Collecting cluster snapshot...")
	}

	snap, err := snapshot.BuildSnapshot(context.Background(), clientset, GetNamespace(), config.MaxPods, config.LogLines, config.MaxConcurrent, filters)
	if err != nil {
		return fmt.Errorf("snapshot error: %w", err)
	}

	snapJSON, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("snapshot marshal error: %w", err)
	}

	// Load prompt with enhancements
	finalPrompt, err := prompt.LoadPrompt(config.Mode, string(snapJSON), config.ProblemHint, enhancements)
	if err != nil {
		return fmt.Errorf("prompt error: %w", err)
	}

	if IsVerbose() {
		fmt.Fprintf(os.Stderr, "[kubenow] Calling LLM endpoint: %s\n", config.LLMEndpoint)
	}

	ctx, cancel := context.WithTimeout(context.Background(), llmClient.Timeout)
	defer cancel()

	raw, err := llmClient.Complete(ctx, finalPrompt)
	if err != nil {
		return fmt.Errorf("LLM error: %w", err)
	}

	// Handle output
	return handleOutput(raw, config.Mode, config.Format, config.OutputFile, clusterName, filters)
}

// handleOutput processes the LLM output and writes to stdout or file
func handleOutput(raw, mode, format, outputFile, clusterName string, filters snapshot.Filters) error {
	// Strict JSON mode: keep old behavior for stdout
	if format == "json" && outputFile == "" {
		jsonStr, jerr := extractJSON(raw)
		if jerr != nil {
			return fmt.Errorf("JSON parse error: %w\nRaw output:\n%s", jerr, raw)
		}

		var tmp any
		if err := json.Unmarshal([]byte(jsonStr), &tmp); err != nil {
			return fmt.Errorf("JSON unmarshal error: %w\nRaw JSON:\n%s", err, jsonStr)
		}

		out, err := result.PrettyJSON(tmp)
		if err != nil {
			fmt.Println(jsonStr)
			return nil
		}
		fmt.Print(out)
		return nil
	}

	// Extract and parse JSON
	jsonStr, jerr := extractJSON(raw)
	if jerr != nil {
		// No JSON at all: just show raw model answer
		if outputFile == "" {
			fmt.Fprintln(os.Stderr, "[kubenow] No JSON detected in LLM output, showing raw response")
			fmt.Println(raw)
			return nil
		}
		return fmt.Errorf("no JSON detected in LLM output for file export")
	}

	// Parse according to mode
	var parsedResult interface{}
	var parseErr error

	switch mode {
	case "pod":
		var pr result.PodResult
		parseErr = json.Unmarshal([]byte(jsonStr), &pr)
		parsedResult = &pr
	case "incident":
		var ir result.IncidentResult
		parseErr = json.Unmarshal([]byte(jsonStr), &ir)
		parsedResult = &ir
	case "teamlead":
		var tr result.TeamleadResult
		parseErr = json.Unmarshal([]byte(jsonStr), &tr)
		parsedResult = &tr
	case "compliance":
		var cr result.ComplianceResult
		parseErr = json.Unmarshal([]byte(jsonStr), &cr)
		parsedResult = &cr
	case "chaos":
		var ch result.ChaosResult
		parseErr = json.Unmarshal([]byte(jsonStr), &ch)
		parsedResult = &ch
	default: // "default"
		var dr result.DefaultResult
		parseErr = json.Unmarshal([]byte(jsonStr), &dr)
		parsedResult = &dr
	}

	if parseErr != nil {
		if outputFile == "" {
			fmt.Fprintf(os.Stderr, "[kubenow] Failed to parse %s JSON, showing raw response\nError: %v\n", mode, parseErr)
			fmt.Println(raw)
			return nil
		}
		return fmt.Errorf("failed to parse %s JSON: %w", mode, parseErr)
	}

	// Handle file output
	if outputFile != "" {
		return exportToFile(parsedResult, mode, outputFile, clusterName, filters)
	}

	// Render to stdout (human format)
	switch mode {
	case "pod":
		result.RenderPodHuman(os.Stdout, parsedResult.(*result.PodResult))
	case "incident":
		result.RenderIncidentHuman(os.Stdout, parsedResult.(*result.IncidentResult))
	case "teamlead":
		result.RenderTeamleadHuman(os.Stdout, parsedResult.(*result.TeamleadResult))
	case "compliance":
		result.RenderComplianceHuman(os.Stdout, parsedResult.(*result.ComplianceResult))
	case "chaos":
		result.RenderChaosHuman(os.Stdout, parsedResult.(*result.ChaosResult))
	default:
		result.RenderDefaultHuman(os.Stdout, parsedResult.(*result.DefaultResult))
	}

	return nil
}

// exportToFile exports the result to a file in the specified format
func exportToFile(parsedResult interface{}, mode, outputPath, clusterName string, filters snapshot.Filters) error {
	format := export.DetectFormat(outputPath)

	exporter := export.Exporter{
		Format: format,
		Metadata: export.ExportMetadata{
			GeneratedAt:    time.Now().UTC(),
			KubenowVersion: version, // from root.go
			ClusterName:    clusterName,
			Mode:           mode,
			Filters:        filters,
		},
	}

	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	if err := exporter.Export(parsedResult, file); err != nil {
		return fmt.Errorf("failed to export: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[kubenow] Report saved to: %s\n", outputPath)
	return nil
}

// extractClusterName extracts the cluster name from kubeconfig
func extractClusterName(kubeconfigPath string) string {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}

	config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
	rawConfig, err := config.RawConfig()
	if err != nil {
		return "unknown"
	}

	if rawConfig.CurrentContext == "" {
		return "unknown"
	}

	ctx, ok := rawConfig.Contexts[rawConfig.CurrentContext]
	if !ok {
		return "unknown"
	}

	return ctx.Cluster
}

// extractJSON extracts a JSON object or array from noisy LLM output
func extractJSON(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return "", fmt.Errorf("empty LLM output")
	}

	// If starts with { or [, assume valid JSON
	if s[0] == '{' || s[0] == '[' {
		return s, nil
	}

	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start == -1 || end == -1 || end <= start {
		return "", fmt.Errorf("no JSON object detected in output")
	}

	return s[start : end+1], nil
}

// setupSignalHandler sets up signal handling for graceful shutdown
func setupSignalHandler(cancel context.CancelFunc) {
	// Signal handling is already done in watch.Run, but we can add here if needed
	// For now, just a placeholder
}

// addLLMFlags adds common LLM flags to a command
func addLLMFlags(cmd *cobra.Command, config *LLMCommandConfig) {
	// Required flags
	cmd.Flags().StringVar(&config.LLMEndpoint, "llm-endpoint", "", "OpenAI-compatible endpoint (e.g., http://localhost:11434/v1)")
	cmd.Flags().StringVar(&config.Model, "model", "", "Model name (e.g., mixtral:8x22b, gpt-4.1-mini)")
	cmd.MarkFlagRequired("llm-endpoint")
	cmd.MarkFlagRequired("model")

	// Optional flags
	cmd.Flags().StringVar(&config.APIKey, "api-key", "", "LLM API key (optional for local models)")
	cmd.Flags().StringVar(&config.Format, "format", "human", "Output format: human|json")
	cmd.Flags().IntVar(&config.MaxPods, "max-pods", 20, "Max problematic pods to include")
	cmd.Flags().IntVar(&config.LogLines, "log-lines", 50, "Max log lines per container")
	cmd.Flags().IntVar(&config.TimeoutSeconds, "timeout-seconds", 60, "LLM call timeout in seconds")
	cmd.Flags().IntVar(&config.MaxConcurrent, "max-concurrent-fetches", 5, "Max concurrent log fetches")
	cmd.Flags().StringVar(&config.OutputFile, "output", "", "Save report to file (format auto-detected: .json, .md, .html, .txt)")

	// Filters
	cmd.Flags().StringVar(&config.IncludePods, "include-pods", "", "Comma-separated pod name patterns to include (supports wildcards)")
	cmd.Flags().StringVar(&config.ExcludePods, "exclude-pods", "", "Comma-separated pod name patterns to exclude (supports wildcards)")
	cmd.Flags().StringVar(&config.IncludeNamespaces, "include-namespaces", "", "Comma-separated namespace patterns to include (supports wildcards)")
	cmd.Flags().StringVar(&config.ExcludeNamespaces, "exclude-namespaces", "", "Comma-separated namespace patterns to exclude (supports wildcards)")
	cmd.Flags().StringVar(&config.IncludeKeywords, "include-keywords", "", "Comma-separated keywords to search in logs/events")
	cmd.Flags().StringVar(&config.ExcludeKeywords, "exclude-keywords", "", "Comma-separated keywords to exclude from logs/events")
	cmd.Flags().StringVar(&config.ProblemHint, "hint", "", "Problem hint to guide LLM analysis (e.g., 'memory leak', 'network issue')")

	// Enhancements
	cmd.Flags().BoolVar(&config.EnhanceTechnical, "enhance-technical", false, "Include technical depth (stack traces, config diffs)")
	cmd.Flags().BoolVar(&config.EnhancePriority, "enhance-priority", false, "Include priority scoring (numerical scores, SLO impact)")
	cmd.Flags().BoolVar(&config.EnhanceRemediation, "enhance-remediation", false, "Include detailed remediation (step-by-step fixes)")

	// Watch mode
	cmd.Flags().StringVar(&config.WatchInterval, "watch-interval", "", "Enable watch mode with interval (e.g., '30s', '1m', '5m')")
	cmd.Flags().IntVar(&config.WatchIterations, "watch-iterations", 0, "Max watch iterations (0 = infinite)")
	cmd.Flags().BoolVar(&config.WatchAlertNewOnly, "watch-alert-new-only", false, "Only show new/changed issues in watch mode")
}
