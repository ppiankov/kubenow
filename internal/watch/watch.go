// Package watch compares snapshots and streams change output.
package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/ppiankov/kubenow/internal/llm"
	"github.com/ppiankov/kubenow/internal/prompt"
	"github.com/ppiankov/kubenow/internal/result"
	"github.com/ppiankov/kubenow/internal/snapshot"
)

func stderrf(format string, args ...any) {
	if _, err := fmt.Fprintf(os.Stderr, format, args...); err != nil {
		return
	}
}

func stderrln(args ...any) {
	if _, err := fmt.Fprintln(os.Stderr, args...); err != nil {
		return
	}
}

func printlnOut(args ...any) {
	if _, err := fmt.Println(args...); err != nil {
		return
	}
}

// Config holds watch mode configuration.
type Config struct {
	Interval      time.Duration
	MaxIterations int
	AlertNewOnly  bool
	Namespace     string
	MaxPods       int
	LogLines      int
	MaxConcurrent int
	Filters       snapshot.Filters
	Mode          string
	ProblemHint   string
	Enhancements  prompt.PromptEnhancements
	LLMClient     *llm.Client
}

// IssueIdentity uniquely identifies an issue for diff detection.
type IssueIdentity struct {
	Namespace     string
	PodName       string
	IssueType     string
	ContainerName string
}

// IssueDiff represents the difference between two snapshots.
type IssueDiff struct {
	NewIssues      []IssueIdentity
	ResolvedIssues []IssueIdentity
	OngoingIssues  []IssueIdentity
}

// Run executes the watch loop.
func Run(ctx context.Context, clientset *kubernetes.Clientset, config *Config) error {
	var prevSnapshot *snapshot.Snapshot
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()

	iteration := 0
	for {
		iteration++
		timestamp := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")

		stderrf("\n[%s] Iteration %d", timestamp, iteration)
		if config.MaxIterations > 0 {
			stderrf("/%d", config.MaxIterations)
		}
		stderrln()
		stderrln("----------------------------------------")

		// Build current snapshot
		stderrln("[kubenow] Collecting cluster snapshot...")
		currSnapshot, err := snapshot.BuildSnapshot(ctx, clientset, config.Namespace, config.MaxPods, config.LogLines, config.MaxConcurrent, &config.Filters)
		if err != nil {
			stderrf("snapshot error: %v\n", err)
			// Continue watching even if snapshot fails
		} else {
			// Compare with previous snapshot if it exists
			if prevSnapshot != nil {
				diff := compareSnapshots(prevSnapshot, currSnapshot)

				if config.AlertNewOnly && len(diff.NewIssues) == 0 {
					stderrln("[kubenow] No new issues detected")
					prevSnapshot = currSnapshot
				} else {
					printDiff(diff, config.AlertNewOnly)

					if err := runLLMAnalysis(ctx, config, currSnapshot); err != nil {
						stderrf("%v\n", err)
					}

					prevSnapshot = currSnapshot
				}
			} else {
				if err := runLLMAnalysis(ctx, config, currSnapshot); err != nil {
					stderrf("%v\n", err)
				}

				prevSnapshot = currSnapshot
			}
		}

		// Check if we've reached max iterations
		if config.MaxIterations > 0 && iteration >= config.MaxIterations {
			stderrln("\n[kubenow] Max iterations reached. Exiting watch mode.")
			break
		}

		// Wait for next tick or context cancellation
		stderrf("\nNext check in %s... (Ctrl+C to stop)\n", config.Interval)
		select {
		case <-ticker.C:
			continue
		case <-ctx.Done():
			stderrln("\n[kubenow] Watch mode stopped.")
			return ctx.Err()
		}
	}

	return nil
}

func runLLMAnalysis(ctx context.Context, config *Config, snapshotData interface{}) error {
	snapJSON, err := json.Marshal(snapshotData)
	if err != nil {
		return fmt.Errorf("snapshot marshal error: %w", err)
	}

	finalPrompt, err := prompt.LoadPrompt(config.Mode, string(snapJSON), config.ProblemHint, config.Enhancements)
	if err != nil {
		return fmt.Errorf("prompt error: %w", err)
	}

	stderrf("[kubenow] Calling LLM endpoint...\n")
	raw, err := config.LLMClient.Complete(ctx, finalPrompt)
	if err != nil {
		return fmt.Errorf("llm error: %w", err)
	}

	if err := renderOutput(raw, config.Mode); err != nil {
		return fmt.Errorf("render error: %w", err)
	}

	return nil
}

// compareSnapshots compares two snapshots and returns the diff.
func compareSnapshots(prev, curr *snapshot.Snapshot) IssueDiff {
	prevIssues := extractIssues(prev)
	currIssues := extractIssues(curr)

	var diff IssueDiff

	// Find new issues (in current but not in previous)
	for _, issue := range currIssues {
		if !containsIssue(prevIssues, issue) {
			diff.NewIssues = append(diff.NewIssues, issue)
		}
	}

	// Find resolved issues (in previous but not in current)
	for _, issue := range prevIssues {
		if !containsIssue(currIssues, issue) {
			diff.ResolvedIssues = append(diff.ResolvedIssues, issue)
		}
	}

	// Find ongoing issues (in both)
	for _, issue := range currIssues {
		if containsIssue(prevIssues, issue) {
			diff.OngoingIssues = append(diff.OngoingIssues, issue)
		}
	}

	return diff
}

// extractIssues extracts issue identities from a snapshot.
func extractIssues(snap *snapshot.Snapshot) []IssueIdentity {
	var issues []IssueIdentity

	for i := range snap.ProblemPods {
		pod := &snap.ProblemPods[i]
		// Determine pod-level issue type from Phase and Reason
		podIssueType := pod.Phase
		if pod.Reason != "" {
			podIssueType = pod.Reason
		}

		// Add container-specific issues
		for _, container := range pod.Containers {
			if container.State != "Running" && container.State != "" {
				issueType := container.State
				if container.StateReason != "" {
					issueType = container.StateReason
				}
				containerIssue := IssueIdentity{
					Namespace:     pod.Namespace,
					PodName:       pod.Name,
					IssueType:     issueType,
					ContainerName: container.Name,
				}
				issues = append(issues, containerIssue)
			}
		}

		// Add pod-level issue
		if podIssueType != "" && podIssueType != "Running" && podIssueType != "Succeeded" {
			issue := IssueIdentity{
				Namespace: pod.Namespace,
				PodName:   pod.Name,
				IssueType: podIssueType,
			}
			issues = append(issues, issue)
		}
	}

	return issues
}

// containsIssue checks if an issue list contains a specific issue.
func containsIssue(issues []IssueIdentity, target IssueIdentity) bool {
	for _, issue := range issues {
		if issue.Namespace == target.Namespace &&
			issue.PodName == target.PodName &&
			issue.IssueType == target.IssueType &&
			issue.ContainerName == target.ContainerName {
			return true
		}
	}
	return false
}

// printDiff prints the diff between snapshots.
func printDiff(diff IssueDiff, newOnly bool) {
	if len(diff.NewIssues) > 0 {
		stderrf("\n\033[1;31mNEW ISSUES DETECTED: %d\033[0m\n", len(diff.NewIssues))
		for _, issue := range diff.NewIssues {
			if issue.ContainerName != "" {
				stderrf("  [NEW] %s/%s (container: %s) - %s\n", issue.Namespace, issue.PodName, issue.ContainerName, issue.IssueType)
			} else {
				stderrf("  [NEW] %s/%s - %s\n", issue.Namespace, issue.PodName, issue.IssueType)
			}
		}
	}

	if len(diff.ResolvedIssues) > 0 {
		stderrf("\n\033[1;32mRESOLVED ISSUES: %d\033[0m\n", len(diff.ResolvedIssues))
		for _, issue := range diff.ResolvedIssues {
			if issue.ContainerName != "" {
				stderrf("  [RESOLVED] %s/%s (container: %s) - %s\n", issue.Namespace, issue.PodName, issue.ContainerName, issue.IssueType)
			} else {
				stderrf("  [RESOLVED] %s/%s - %s\n", issue.Namespace, issue.PodName, issue.IssueType)
			}
		}
	}

	if !newOnly && len(diff.OngoingIssues) > 0 {
		stderrf("\n\033[1;33mONGOING ISSUES: %d\033[0m\n", len(diff.OngoingIssues))
		for _, issue := range diff.OngoingIssues {
			if issue.ContainerName != "" {
				stderrf("  [ONGOING] %s/%s (container: %s) - %s\n", issue.Namespace, issue.PodName, issue.ContainerName, issue.IssueType)
			} else {
				stderrf("  [ONGOING] %s/%s - %s\n", issue.Namespace, issue.PodName, issue.IssueType)
			}
		}
	}

	stderrln()
}

// renderOutput renders the LLM output to stdout.
func renderOutput(raw, mode string) error {
	// Extract and parse JSON
	jsonStr, jerr := extractJSON(raw)
	if jerr != nil {
		// No JSON: show raw response
		stderrln("[kubenow] No JSON detected in LLM output, showing raw response")
		printlnOut(raw)
		return nil
	}

	switch mode {
	case "pod":
		var pr result.PodResult
		if err := json.Unmarshal([]byte(jsonStr), &pr); err != nil {
			stderrf("[kubenow] Failed to parse %s JSON, showing raw response\nError: %v\n", mode, err)
			printlnOut(raw)
			return nil
		}
		return result.RenderPodHuman(os.Stdout, &pr)
	case "incident":
		var ir result.IncidentResult
		if err := json.Unmarshal([]byte(jsonStr), &ir); err != nil {
			stderrf("[kubenow] Failed to parse %s JSON, showing raw response\nError: %v\n", mode, err)
			printlnOut(raw)
			return nil
		}
		return result.RenderIncidentHuman(os.Stdout, &ir)
	case "teamlead":
		var tr result.TeamleadResult
		if err := json.Unmarshal([]byte(jsonStr), &tr); err != nil {
			stderrf("[kubenow] Failed to parse %s JSON, showing raw response\nError: %v\n", mode, err)
			printlnOut(raw)
			return nil
		}
		return result.RenderTeamleadHuman(os.Stdout, &tr)
	case "compliance":
		var cr result.ComplianceResult
		if err := json.Unmarshal([]byte(jsonStr), &cr); err != nil {
			stderrf("[kubenow] Failed to parse %s JSON, showing raw response\nError: %v\n", mode, err)
			printlnOut(raw)
			return nil
		}
		return result.RenderComplianceHuman(os.Stdout, &cr)
	case "chaos":
		var ch result.ChaosResult
		if err := json.Unmarshal([]byte(jsonStr), &ch); err != nil {
			stderrf("[kubenow] Failed to parse %s JSON, showing raw response\nError: %v\n", mode, err)
			printlnOut(raw)
			return nil
		}
		return result.RenderChaosHuman(os.Stdout, &ch)
	default:
		var dr result.DefaultResult
		if err := json.Unmarshal([]byte(jsonStr), &dr); err != nil {
			stderrf("[kubenow] Failed to parse %s JSON, showing raw response\nError: %v\n", mode, err)
			printlnOut(raw)
			return nil
		}
		return result.RenderDefaultHuman(os.Stdout, &dr)
	}
}

// extractJSON is a helper copied from main.go to avoid circular dependency.
func extractJSON(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("empty LLM output")
	}

	if s[0] == '{' || s[0] == '[' {
		return s, nil
	}

	start := 0
	end := len(s) - 1
	for i, c := range s {
		if c == '{' {
			start = i
			break
		}
	}
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '}' {
			end = i
			break
		}
	}

	if start >= end {
		return "", fmt.Errorf("no JSON object detected in output")
	}

	return s[start : end+1], nil
}
