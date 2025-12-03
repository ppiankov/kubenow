package watch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ppiankov/kubenow/internal/llm"
	"github.com/ppiankov/kubenow/internal/prompt"
	"github.com/ppiankov/kubenow/internal/result"
	"github.com/ppiankov/kubenow/internal/snapshot"
	"k8s.io/client-go/kubernetes"
)

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
func Run(ctx context.Context, clientset *kubernetes.Clientset, config Config) error {
	var prevSnapshot *snapshot.Snapshot
	ticker := time.NewTicker(config.Interval)
	defer ticker.Stop()

	iteration := 0
	for {
		iteration++
		timestamp := time.Now().UTC().Format("2006-01-02 15:04:05 UTC")

		fmt.Fprintf(os.Stderr, "\n[%s] Iteration %d", timestamp, iteration)
		if config.MaxIterations > 0 {
			fmt.Fprintf(os.Stderr, "/%d", config.MaxIterations)
		}
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "----------------------------------------")

		// Build current snapshot
		fmt.Fprintln(os.Stderr, "[kubenow] Collecting cluster snapshot...")
		currSnapshot, err := snapshot.BuildSnapshot(ctx, clientset, config.Namespace, config.MaxPods, config.LogLines, config.MaxConcurrent, config.Filters)
		if err != nil {
			fmt.Fprintf(os.Stderr, "snapshot error: %v\n", err)
			// Continue watching even if snapshot fails
		} else {
			// Compare with previous snapshot if it exists
			if prevSnapshot != nil {
				diff := compareSnapshots(prevSnapshot, currSnapshot)

				if config.AlertNewOnly && len(diff.NewIssues) == 0 {
					fmt.Fprintln(os.Stderr, "[kubenow] No new issues detected")
					prevSnapshot = currSnapshot
				} else {
					printDiff(diff, config.AlertNewOnly)

					// Call LLM for analysis
					snapJSON, err := json.Marshal(currSnapshot)
					if err != nil {
						fmt.Fprintf(os.Stderr, "snapshot marshal error: %v\n", err)
					} else {
						finalPrompt, err := prompt.LoadPrompt(config.Mode, string(snapJSON), config.ProblemHint, config.Enhancements)
						if err != nil {
							fmt.Fprintf(os.Stderr, "prompt error: %v\n", err)
						} else {
							fmt.Fprintf(os.Stderr, "[kubenow] Calling LLM endpoint...\n")
							raw, err := config.LLMClient.Complete(ctx, finalPrompt)
							if err != nil {
								fmt.Fprintf(os.Stderr, "llm error: %v\n", err)
							} else {
								// Render output based on mode
								if err := renderOutput(raw, config.Mode); err != nil {
									fmt.Fprintf(os.Stderr, "render error: %v\n", err)
								}
							}
						}
					}

					prevSnapshot = currSnapshot
				}
			} else {
				// First iteration - call LLM
				snapJSON, err := json.Marshal(currSnapshot)
				if err != nil {
					fmt.Fprintf(os.Stderr, "snapshot marshal error: %v\n", err)
				} else {
					finalPrompt, err := prompt.LoadPrompt(config.Mode, string(snapJSON), config.ProblemHint, config.Enhancements)
					if err != nil {
						fmt.Fprintf(os.Stderr, "prompt error: %v\n", err)
					} else {
						fmt.Fprintf(os.Stderr, "[kubenow] Calling LLM endpoint...\n")
						raw, err := config.LLMClient.Complete(ctx, finalPrompt)
						if err != nil {
							fmt.Fprintf(os.Stderr, "llm error: %v\n", err)
						} else {
							// Render output based on mode
							if err := renderOutput(raw, config.Mode); err != nil {
								fmt.Fprintf(os.Stderr, "render error: %v\n", err)
							}
						}
					}
				}

				prevSnapshot = currSnapshot
			}
		}

		// Check if we've reached max iterations
		if config.MaxIterations > 0 && iteration >= config.MaxIterations {
			fmt.Fprintln(os.Stderr, "\n[kubenow] Max iterations reached. Exiting watch mode.")
			break
		}

		// Wait for next tick or context cancellation
		fmt.Fprintf(os.Stderr, "\nNext check in %s... (Ctrl+C to stop)\n", config.Interval)
		select {
		case <-ticker.C:
			continue
		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\n[kubenow] Watch mode stopped.")
			return ctx.Err()
		}
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

	for _, pod := range snap.ProblemPods {
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
		fmt.Fprintf(os.Stderr, "\n\033[1;31mNEW ISSUES DETECTED: %d\033[0m\n", len(diff.NewIssues))
		for _, issue := range diff.NewIssues {
			if issue.ContainerName != "" {
				fmt.Fprintf(os.Stderr, "  [NEW] %s/%s (container: %s) - %s\n", issue.Namespace, issue.PodName, issue.ContainerName, issue.IssueType)
			} else {
				fmt.Fprintf(os.Stderr, "  [NEW] %s/%s - %s\n", issue.Namespace, issue.PodName, issue.IssueType)
			}
		}
	}

	if len(diff.ResolvedIssues) > 0 {
		fmt.Fprintf(os.Stderr, "\n\033[1;32mRESOLVED ISSUES: %d\033[0m\n", len(diff.ResolvedIssues))
		for _, issue := range diff.ResolvedIssues {
			if issue.ContainerName != "" {
				fmt.Fprintf(os.Stderr, "  [RESOLVED] %s/%s (container: %s) - %s\n", issue.Namespace, issue.PodName, issue.ContainerName, issue.IssueType)
			} else {
				fmt.Fprintf(os.Stderr, "  [RESOLVED] %s/%s - %s\n", issue.Namespace, issue.PodName, issue.IssueType)
			}
		}
	}

	if !newOnly && len(diff.OngoingIssues) > 0 {
		fmt.Fprintf(os.Stderr, "\n\033[1;33mONGOING ISSUES: %d\033[0m\n", len(diff.OngoingIssues))
		for _, issue := range diff.OngoingIssues {
			if issue.ContainerName != "" {
				fmt.Fprintf(os.Stderr, "  [ONGOING] %s/%s (container: %s) - %s\n", issue.Namespace, issue.PodName, issue.ContainerName, issue.IssueType)
			} else {
				fmt.Fprintf(os.Stderr, "  [ONGOING] %s/%s - %s\n", issue.Namespace, issue.PodName, issue.IssueType)
			}
		}
	}

	fmt.Fprintln(os.Stderr)
}

// renderOutput renders the LLM output to stdout.
func renderOutput(raw, mode string) error {
	// Extract and parse JSON
	jsonStr, jerr := extractJSON(raw)
	if jerr != nil {
		// No JSON: show raw response
		fmt.Fprintln(os.Stderr, "[kubenow] No JSON detected in LLM output, showing raw response")
		fmt.Println(raw)
		return nil
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
		fmt.Fprintf(os.Stderr, "[kubenow] Failed to parse %s JSON, showing raw response\nError: %v\n", mode, parseErr)
		fmt.Println(raw)
		return nil
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

// extractJSON is a helper copied from main.go to avoid circular dependency.
func extractJSON(s string) (string, error) {
	if len(s) == 0 {
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
