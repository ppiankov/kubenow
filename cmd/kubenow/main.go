// CLI entrypoint.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ppiankov/kubenow/internal/llm"
	"github.com/ppiankov/kubenow/internal/prompt"
	"github.com/ppiankov/kubenow/internal/result"
	"github.com/ppiankov/kubenow/internal/snapshot"
	"github.com/ppiankov/kubenow/internal/util"
)

func main() {
	var (
		kubeconfig     = flag.String("kubeconfig", "", "Path to kubeconfig (optional)")
		namespace      = flag.String("namespace", "", "Limit analysis to one namespace")
		mode           = flag.String("mode", "default", "Mode: default|pod|incident|teamlead|compliance|chaos")
		llmEndpoint    = flag.String("llm-endpoint", "", "OpenAI-compatible endpoint, e.g. http://localhost:11434/v1")
		model          = flag.String("model", "", "Model name: mixtral:8x22b, gpt-4.1-mini, etc.")
		apiKey         = flag.String("api-key", "", "LLM API key (optional for local)")
		formatFlag     = flag.String("format", "human", "Output format: human|json")
		maxPods        = flag.Int("max-pods", 20, "Max problematic pods to include")
		logLines       = flag.Int("log-lines", 50, "Max log lines per container")
		timeoutSeconds = flag.Int("timeout-seconds", 60, "LLM call timeout in seconds")
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

	fmt.Println("[kubenow] Collecting cluster snapshot...")
	snap, err := snapshot.BuildSnapshot(context.Background(), clientset, *namespace, *maxPods, *logLines)
	if err != nil {
		fmt.Fprintf(os.Stderr, "snapshot error: %v\n", err)
		os.Exit(1)
	}

	snapJSON, err := json.Marshal(snap)
	if err != nil {
		fmt.Fprintf(os.Stderr, "snapshot marshal error: %v\n", err)
		os.Exit(1)
	}

	// Use hard-coded prompt templates
	finalPrompt, err := prompt.LoadPrompt(*mode, string(snapJSON))
	if err != nil {
		fmt.Fprintf(os.Stderr, "prompt error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("[kubenow] Calling LLM endpoint: %s\n", *llmEndpoint)

	timeout := time.Duration(*timeoutSeconds) * time.Second

	cli := llm.Client{
		Endpoint: *llmEndpoint,
		Model:    *model,
		APIKey:   *apiKey,
		Timeout:  timeout,
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	raw, err := cli.Complete(ctx, finalPrompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "llm error: %v\n", err)
		os.Exit(1)
	}

	// Strict JSON mode: keep old behavior.
	if *formatFlag == "json" {
		jsonStr, jerr := extractJSON(raw)
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "llm JSON parse error: %v\nRaw output:\n%s\n", jerr, raw)
			os.Exit(1)
		}

		var tmp any
		if err := json.Unmarshal([]byte(jsonStr), &tmp); err != nil {
			fmt.Fprintf(os.Stderr, "llm JSON unmarshal error: %v\nRaw JSON:\n%s\n", err, jsonStr)
			os.Exit(1)
		}

		out, err := result.PrettyJSON(tmp)
		if err != nil {
			fmt.Println(jsonStr)
			return
		}
		fmt.Print(out)
		return
	}

	// HUMAN MODE (Option C lite):
	// Try JSON first; if that fails, just show the raw LLM output.

	jsonStr, jerr := extractJSON(raw)
	if jerr != nil {
		// No JSON at all: just show raw model answer.
		fmt.Fprintln(os.Stderr, "[kubenow] No JSON detected in LLM output, showing raw response")
		fmt.Println(raw)
		return
	}

	// We have something that looks like JSON; try to parse according to mode.
	switch *mode {
	case "pod":
		var pr result.PodResult
		if err := json.Unmarshal([]byte(jsonStr), &pr); err != nil {
			fmt.Fprintf(os.Stderr, "[kubenow] Failed to parse pod JSON, showing raw response\nError: %v\n", err)
			fmt.Println(raw)
			return
		}
		result.RenderPodHuman(os.Stdout, &pr)

	case "incident":
		var ir result.IncidentResult
		if err := json.Unmarshal([]byte(jsonStr), &ir); err != nil {
			fmt.Fprintf(os.Stderr, "[kubenow] Failed to parse incident JSON, showing raw response\nError: %v\n", err)
			fmt.Println(raw)
			return
		}
		result.RenderIncidentHuman(os.Stdout, &ir)

	case "teamlead":
		var tr result.TeamleadResult
		if err := json.Unmarshal([]byte(jsonStr), &tr); err != nil {
			fmt.Fprintf(os.Stderr, "[kubenow] Failed to parse teamlead JSON, showing raw response\nError: %v\n", err)
			fmt.Println(raw)
			return
		}
		result.RenderTeamleadHuman(os.Stdout, &tr)

	case "compliance":
		var cr result.ComplianceResult
		if err := json.Unmarshal([]byte(jsonStr), &cr); err != nil {
			fmt.Fprintf(os.Stderr, "[kubenow] Failed to parse compliance JSON, showing raw response\nError: %v\n", err)
			fmt.Println(raw)
			return
		}
		result.RenderComplianceHuman(os.Stdout, &cr)

	case "chaos":
		var ch result.ChaosResult
		if err := json.Unmarshal([]byte(jsonStr), &ch); err != nil {
			fmt.Fprintf(os.Stderr, "[kubenow] Failed to parse chaos JSON, showing raw response\nError: %v\n", err)
			fmt.Println(raw)
			return
		}
		result.RenderChaosHuman(os.Stdout, &ch)

	default: // "default"
		var dr result.DefaultResult
		if err := json.Unmarshal([]byte(jsonStr), &dr); err != nil {
			fmt.Fprintf(os.Stderr, "[kubenow] Failed to parse default JSON, showing raw response\nError: %v\n", err)
			fmt.Println(raw)
			return
		}
		result.RenderDefaultHuman(os.Stdout, &dr)
	}
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
