// Package result defines prompt result schemas and human renderers.
package result

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ---------- Shared JSON helpers ----------

// PrettyJSON marshals v as indented JSON.
func PrettyJSON(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ---------- Types matching prompt schemas ----------

// PodResult represents the prompt result for pod mode.
type PodResult struct {
	Pods []struct {
		Namespace        string   `json:"namespace"`
		Name             string   `json:"name"`
		Severity         string   `json:"severity"`
		IssueType        string   `json:"issue_type"`
		FailingContainer string   `json:"failing_container"`
		Summary          string   `json:"summary"`
		RootCause        string   `json:"root_cause"`
		FixCommands      []string `json:"fix_commands"`
		Notes            string   `json:"notes"`
	} `json:"pods"`
}

// IncidentResult represents the prompt result for incident mode.
type IncidentResult struct {
	TopIssues []struct {
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
		Severity  string `json:"severity"`
		IssueType string `json:"issue_type"`
		Summary   string `json:"summary"`
		Impact    string `json:"impact"`
	} `json:"top_issues"`
	RootCauses []string `json:"root_causes"`
	Actions    []string `json:"actions"`
	Notes      []string `json:"notes"`
}

// TeamleadResult represents the prompt result for teamlead mode.
type TeamleadResult struct {
	BusinessRisk   []string `json:"business_risk"`
	OwnershipHints []string `json:"ownership_hints"`
	TopActions     []string `json:"top_actions"`
	Escalation     []string `json:"escalation"`
}

// ComplianceResult represents the prompt result for compliance mode.
type ComplianceResult struct {
	Issues []struct {
		Namespace      string `json:"namespace"`
		Name           string `json:"name"`
		Type           string `json:"type"`
		Severity       string `json:"severity"`
		Description    string `json:"description"`
		Recommendation string `json:"recommendation"`
	} `json:"issues"`
}

// ChaosResult represents the prompt result for chaos mode.
type ChaosResult struct {
	Vulnerabilities []string `json:"vulnerabilities"`
	Experiments     []struct {
		Name        string `json:"name"`
		Reason      string `json:"reason"`
		Description string `json:"description"`
	} `json:"experiments"`
	ImpactNotes []string `json:"impact_notes"`
}

// DefaultResult represents the prompt result for default mode.
type DefaultResult struct {
	Summary struct {
		ProblemPodCount      int      `json:"problem_pod_count"`
		NamespacesWithIssues []string `json:"namespaces_with_issues"`
		NodeReadiness        string   `json:"node_readiness"`
		ResourcePressure     string   `json:"resource_pressure"`
	} `json:"summary"`
	Issues []struct {
		Namespace    string `json:"namespace"`
		Name         string `json:"name"`
		IssueType    string `json:"issue_type"`
		Severity     string `json:"severity"`
		ShortSummary string `json:"short_summary"`
	} `json:"issues"`
	Recommendations []string `json:"recommendations"`
}

type errWriter struct {
	w   io.Writer
	err error
}

func (ew *errWriter) fprintf(format string, args ...any) {
	if ew.err != nil {
		return
	}
	_, ew.err = fmt.Fprintf(ew.w, format, args...)
}

func (ew *errWriter) fprintln(args ...any) {
	if ew.err != nil {
		return
	}
	_, ew.err = fmt.Fprintln(ew.w, args...)
}

// ---------- Human renderers ----------

// RenderPodHuman renders pod-mode results in a human-readable format.
func RenderPodHuman(w io.Writer, r *PodResult) error {
	ew := errWriter{w: w}

	if len(r.Pods) == 0 {
		ew.fprintln("No problematic pods detected.")
		return ew.err
	}

	for i := range r.Pods {
		p := &r.Pods[i]
		ew.fprintln("────────────────────────────────────────")
		ew.fprintf("Namespace:   %s\n", p.Namespace)
		ew.fprintf("Pod:         %s\n", p.Name)
		ew.fprintf("Severity:    %s\n", strings.ToUpper(p.Severity))
		ew.fprintf("Issue:       %s\n", p.IssueType)
		ew.fprintf("Container:   %s\n\n", p.FailingContainer)

		ew.fprintf("Summary:\n  %s\n\n", p.Summary)
		ew.fprintf("Likely root cause:\n  %s\n\n", p.RootCause)

		if len(p.FixCommands) > 0 {
			ew.fprintln("Suggested commands:")
			for _, c := range p.FixCommands {
				ew.fprintf("  $ %s\n", c)
			}
			ew.fprintln()
		}

		if p.Notes != "" {
			ew.fprintf("Notes:\n  %s\n", p.Notes)
		}
	}
	ew.fprintln("────────────────────────────────────────")

	return ew.err
}

// RenderIncidentHuman renders incident-mode results in a human-readable format.
func RenderIncidentHuman(w io.Writer, r *IncidentResult) error {
	ew := errWriter{w: w}

	ew.fprintln("===== INCIDENT VIEW =====")

	if len(r.TopIssues) == 0 {
		ew.fprintln("No significant incident-level issues detected.")
		return ew.err
	}

	ew.fprintln("Top issues:")
	for _, i := range r.TopIssues {
		ew.fprintln("─────────────────────────")
		ew.fprintf("Namespace: %s\n", i.Namespace)
		ew.fprintf("Name:      %s\n", i.Name)
		ew.fprintf("Severity:  %s\n", strings.ToUpper(i.Severity))
		ew.fprintf("Type:      %s\n\n", i.IssueType)
		ew.fprintf("Summary:   %s\n", i.Summary)
		ew.fprintf("Impact:    %s\n", i.Impact)
	}

	if len(r.RootCauses) > 0 {
		ew.fprintln("\nRoot causes:")
		for _, rc := range r.RootCauses {
			ew.fprintf("  - %s\n", rc)
		}
	}

	if len(r.Actions) > 0 {
		ew.fprintln("\nActions:")
		for _, a := range r.Actions {
			ew.fprintf("  $ %s\n", a)
		}
	}

	if len(r.Notes) > 0 {
		ew.fprintln("\nNotes:")
		for _, n := range r.Notes {
			ew.fprintf("  - %s\n", n)
		}
	}

	return ew.err
}

// RenderTeamleadHuman renders teamlead-mode results in a human-readable format.
func RenderTeamleadHuman(w io.Writer, r *TeamleadResult) error {
	ew := errWriter{w: w}

	ew.fprintln("===== TEAMLEAD VIEW =====")

	if len(r.BusinessRisk) > 0 {
		ew.fprintln("Business risk:")
		for _, s := range r.BusinessRisk {
			ew.fprintf("  - %s\n", s)
		}
	}

	if len(r.OwnershipHints) > 0 {
		ew.fprintln("\nOwnership hints:")
		for _, s := range r.OwnershipHints {
			ew.fprintf("  - %s\n", s)
		}
	}

	if len(r.TopActions) > 0 {
		ew.fprintln("\nTop actions:")
		for _, s := range r.TopActions {
			ew.fprintf("  - %s\n", s)
		}
	}

	if len(r.Escalation) > 0 {
		ew.fprintln("\nEscalation conditions:")
		for _, s := range r.Escalation {
			ew.fprintf("  - %s\n", s)
		}
	}

	return ew.err
}

// RenderComplianceHuman renders compliance-mode results in a human-readable format.
func RenderComplianceHuman(w io.Writer, r *ComplianceResult) error {
	ew := errWriter{w: w}

	if len(r.Issues) == 0 {
		ew.fprintln("Compliance: no issues detected.")
		return ew.err
	}

	ew.fprintln("===== COMPLIANCE ISSUES =====")
	for _, i := range r.Issues {
		ew.fprintln("──────────────────────────────")
		ew.fprintf("Namespace:    %s\n", i.Namespace)
		ew.fprintf("Name:         %s\n", i.Name)
		ew.fprintf("Type:         %s\n", i.Type)
		ew.fprintf("Severity:     %s\n\n", i.Severity)
		ew.fprintf("Issue:        %s\n", i.Description)
		ew.fprintf("Recommendation:\n  %s\n", i.Recommendation)
	}

	return ew.err
}

// RenderChaosHuman renders chaos-mode results in a human-readable format.
func RenderChaosHuman(w io.Writer, r *ChaosResult) error {
	ew := errWriter{w: w}

	ew.fprintln("===== CHAOS EXPERIMENTS =====")

	if len(r.Vulnerabilities) > 0 {
		ew.fprintln("Vulnerabilities:")
		for _, v := range r.Vulnerabilities {
			ew.fprintf("  - %s\n", v)
		}
	}

	if len(r.Experiments) > 0 {
		ew.fprintln("\nSuggested experiments:")
		for _, e := range r.Experiments {
			ew.fprintln("────────────────────────")
			ew.fprintf("Name:   %s\n", e.Name)
			ew.fprintf("Reason: %s\n", e.Reason)
			ew.fprintf("What to do:\n  %s\n", e.Description)
		}
	}

	if len(r.ImpactNotes) > 0 {
		ew.fprintln("\nImpact notes:")
		for _, n := range r.ImpactNotes {
			ew.fprintf("  - %s\n", n)
		}
	}

	return ew.err
}

// RenderDefaultHuman renders default-mode results in a human-readable format.
func RenderDefaultHuman(w io.Writer, r *DefaultResult) error {
	ew := errWriter{w: w}

	ew.fprintln("===== CLUSTER SUMMARY =====")
	ew.fprintf("Problem pods:          %d\n", r.Summary.ProblemPodCount)
	ew.fprintf("Node readiness:        %s\n", r.Summary.NodeReadiness)
	ew.fprintf("Resource pressure:     %s\n", r.Summary.ResourcePressure)

	if len(r.Summary.NamespacesWithIssues) > 0 {
		ew.fprintln("Namespaces with issues:")
		for _, ns := range r.Summary.NamespacesWithIssues {
			ew.fprintf("  - %s\n", ns)
		}
	}

	if len(r.Issues) > 0 {
		ew.fprintln("\nIssues:")
		for _, i := range r.Issues {
			ew.fprintln("────────────────────────")
			ew.fprintf("Namespace: %s\n", i.Namespace)
			ew.fprintf("Name:      %s\n", i.Name)
			ew.fprintf("Type:      %s\n", i.IssueType)
			ew.fprintf("Severity:  %s\n", i.Severity)
			ew.fprintf("Summary:   %s\n", i.ShortSummary)
		}
	}

	if len(r.Recommendations) > 0 {
		ew.fprintln("\nRecommendations:")
		for _, rec := range r.Recommendations {
			ew.fprintf("  - %s\n", rec)
		}
	}

	return ew.err
}
