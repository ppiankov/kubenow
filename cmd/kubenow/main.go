// CLI entrypoint.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ppiankov/kubenow/internal/export"
	"github.com/ppiankov/kubenow/internal/llm"
	"github.com/ppiankov/kubenow/internal/prompt"
	"github.com/ppiankov/kubenow/internal/result"
	"github.com/ppiankov/kubenow/internal/snapshot"
	"github.com/ppiankov/kubenow/internal/util"
	"github.com/ppiankov/kubenow/internal/watch"
	"k8s.io/client-go/tools/clientcmd"
)

// Version is the kubenow version, set at build time via -ldflags
var Version = "dev"

func main() {
	var (
		kubeconfig       = flag.String("kubeconfig", "", "Path to kubeconfig (optional)")
		namespace        = flag.String("namespace", "", "Limit analysis to one namespace")
		mode             = flag.String("mode", "default", "Mode: default|pod|incident|teamlead|compliance|chaos")
		llmEndpoint      = flag.String("llm-endpoint", "", "OpenAI-compatible endpoint, e.g. http://localhost:11434/v1")
		model            = flag.String("model", "", "Model name: mixtral:8x22b, gpt-4.1-mini, etc.")
		apiKey           = flag.String("api-key", "", "LLM API key (optional for local)")
		formatFlag       = flag.String("format", "human", "Output format: human|json")
		maxPods          = flag.Int("max-pods", 20, "Max problematic pods to include")
		logLines         = flag.Int("log-lines", 50, "Max log lines per container")
		timeoutSeconds   = flag.Int("timeout-seconds", 60, "LLM call timeout in seconds")
		maxConcurrent    = flag.Int("max-concurrent-fetches", 5, "Max concurrent log fetches (avoid API throttling)")
		includePods      = flag.String("include-pods", "", "Comma-separated pod name patterns to include (supports wildcards)")
		excludePods      = flag.String("exclude-pods", "", "Comma-separated pod name patterns to exclude (supports wildcards)")
		includeNamespaces = flag.String("include-namespaces", "", "Comma-separated namespace patterns to include (supports wildcards)")
		excludeNamespaces = flag.String("exclude-namespaces", "", "Comma-separated namespace patterns to exclude (supports wildcards)")
		includeKeywords  = flag.String("include-keywords", "", "Comma-separated keywords to search in logs/events")
		excludeKeywords  = flag.String("exclude-keywords", "", "Comma-separated keywords to exclude from logs/events")
		problemHint      = flag.String("hint", "", "Problem hint to guide LLM analysis (e.g., 'memory leak', 'network issue', 'crash')")
		enhanceTechnical    = flag.Bool("enhance-technical", false, "Include technical depth (stack traces, config diffs, deeper analysis)")
		enhancePriority     = flag.Bool("enhance-priority", false, "Include priority scoring (numerical scores, SLO impact, blast radius)")
		enhanceRemediation  = flag.Bool("enhance-remediation", false, "Include detailed remediation (step-by-step fixes, rollback, prevention)")
		outputFile          = flag.String("output", "", "Save report to file (format auto-detected: .json, .md, .html, .txt)")
		watchInterval       = flag.String("watch-interval", "", "Enable watch mode with interval (e.g., '30s', '1m', '5m')")
		watchIterations     = flag.Int("watch-iterations", 0, "Max watch iterations (0 = infinite)")
		watchAlertNewOnly   = flag.Bool("watch-alert-new-only", false, "Only show new/changed issues in watch mode")
	)
	flag.Parse()

	// Required fields
	if *llmEndpoint == "" || *model == "" {
		fmt.Fprintln(os.Stderr, "[kubenow] --llm-endpoint and --model are required")
		os.Exit(1)
	}

	if *formatFlag != "human" && *formatFlag != "json" {
		fmt.Fprintln(os.Stderr, "[kubenow] --format must be 'human' or 'json'")
		os.Exit(1)
	}

	fmt.Println("[kubenow] Building Kubernetes client...")
	clientset, err := util.BuildKubeClient(*kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "kube client error: %v\n", err)
		os.Exit(1)
	}

	// Extract cluster name from kubeconfig for metadata
	clusterName := extractClusterName(*kubeconfig)

	// Setup filters
	filters := snapshot.Filters{
		IncludePods:       *includePods,
		ExcludePods:       *excludePods,
		IncludeNamespaces: *includeNamespaces,
		ExcludeNamespaces: *excludeNamespaces,
		IncludeKeywords:   *includeKeywords,
		ExcludeKeywords:   *excludeKeywords,
	}

	// Setup enhancements
	enhancements := prompt.PromptEnhancements{
		Technical:   *enhanceTechnical,
		Priority:    *enhancePriority,
		Remediation: *enhanceRemediation,
	}

	// Setup LLM client
	timeout := time.Duration(*timeoutSeconds) * time.Second
	cli := llm.Client{
		Endpoint: *llmEndpoint,
		Model:    *model,
		APIKey:   *apiKey,
		Timeout:  timeout,
	}

	// Check if watch mode is enabled
	if *watchInterval != "" {
		// Parse watch interval duration
		interval, err := time.ParseDuration(*watchInterval)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[kubenow] Invalid watch-interval: %v\n", err)
			os.Exit(1)
		}

		// Setup signal handling for graceful shutdown
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigChan
			fmt.Fprintln(os.Stderr, "\n[kubenow] Received interrupt signal, stopping watch mode...")
			cancel()
		}()

		// Run watch mode
		fmt.Fprintf(os.Stderr, "[kubenow] Starting watch mode (interval: %s)\n", interval)
		if *watchIterations > 0 {
			fmt.Fprintf(os.Stderr, "[kubenow] Max iterations: %d\n", *watchIterations)
		}
		if *watchAlertNewOnly {
			fmt.Fprintln(os.Stderr, "[kubenow] Alert mode: New issues only")
		}

		watchConfig := watch.Config{
			Interval:      interval,
			MaxIterations: *watchIterations,
			AlertNewOnly:  *watchAlertNewOnly,
			Namespace:     *namespace,
			MaxPods:       *maxPods,
			LogLines:      *logLines,
			MaxConcurrent: *maxConcurrent,
			Filters:       filters,
			Mode:          *mode,
			ProblemHint:   *problemHint,
			Enhancements:  enhancements,
			LLMClient:     &cli,
		}

		if err := watch.Run(ctx, clientset, watchConfig); err != nil && err != context.Canceled {
			fmt.Fprintf(os.Stderr, "[kubenow] Watch error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Single execution mode (existing behavior)
	fmt.Println("[kubenow] Collecting cluster snapshot...")
	snap, err := snapshot.BuildSnapshot(context.Background(), clientset, *namespace, *maxPods, *logLines, *maxConcurrent, filters)
	if err != nil {
		fmt.Fprintf(os.Stderr, "snapshot error: %v\n", err)
		os.Exit(1)
	}

	snapJSON, err := json.Marshal(snap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "snapshot marshal error: %v\n", err)
		os.Exit(1)
	}

	// Load prompt with enhancements
	finalPrompt, err := prompt.LoadPrompt(*mode, string(snapJSON), *problemHint, enhancements)
	if err != nil {
		fmt.Fprintf(os.Stderr, "prompt error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[kubenow] Calling LLM endpoint: %s\n", *llmEndpoint)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	raw, err := cli.Complete(ctx, finalPrompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "llm error: %v\n", err)
		os.Exit(1)
	}

	// Handle output: stdout or file
	if err := handleOutput(raw, *mode, *formatFlag, *outputFile, clusterName, filters); err != nil {
		fmt.Fprintf(os.Stderr, "output error: %v\n", err)
		os.Exit(1)
	}
}

// handleOutput processes the LLM output and writes to stdout or file.
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

// exportToFile exports the result to a file in the specified format.
func exportToFile(parsedResult interface{}, mode, outputPath, clusterName string, filters snapshot.Filters) error {
	// Detect format from file extension
	format := export.DetectFormat(outputPath)

	// Create exporter with metadata
	exporter := export.Exporter{
		Format: format,
		Metadata: export.ExportMetadata{
			GeneratedAt:    time.Now().UTC(),
			KubenowVersion: Version,
			ClusterName:    clusterName,
			Mode:           mode,
			Filters:        filters,
		},
	}

	// Create output file
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	// Export
	if err := exporter.Export(parsedResult, file); err != nil {
		return fmt.Errorf("failed to export: %w", err)
	}

	fmt.Fprintf(os.Stderr, "[kubenow] Report saved to: %s\n", outputPath)
	return nil
}

// extractClusterName extracts the cluster name from kubeconfig.
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

// extractJSON extracts a JSON object or array from noisy LLM output.
func extractJSON(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return "", fmt.Errorf("empty LLM output")
	}

	// If starts with { or [, assume valid JSON.
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
