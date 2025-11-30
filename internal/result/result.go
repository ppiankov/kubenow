package result

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ---------- Shared JSON helpers ----------

func PrettyJSON(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ---------- Types matching prompt schemas ----------

// Pod mode
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

// Incident mode
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

// Teamlead mode
type TeamleadResult struct {
	BusinessRisk   []string `json:"business_risk"`
	OwnershipHints []string `json:"ownership_hints"`
	TopActions     []string `json:"top_actions"`
	Escalation     []string `json:"escalation"`
}

// Compliance mode
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

// Chaos mode
type ChaosResult struct {
	Vulnerabilities []string `json:"vulnerabilities"`
	Experiments     []struct {
		Name        string `json:"name"`
		Reason      string `json:"reason"`
		Description string `json:"description"`
	} `json:"experiments"`
	ImpactNotes []string `json:"impact_notes"`
}

// Default mode
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

// ---------- Human renderers ----------

// Pod triage
func RenderPodHuman(w io.Writer, r *PodResult) {
	if len(r.Pods) == 0 {
		fmt.Fprintln(w, "No problematic pods detected.")
		return
	}

	for _, p := range r.Pods {
		fmt.Fprintln(w, "────────────────────────────────────────")
		fmt.Fprintf(w, "Namespace:   %s\n", p.Namespace)
		fmt.Fprintf(w, "Pod:         %s\n", p.Name)
		fmt.Fprintf(w, "Severity:    %s\n", strings.ToUpper(p.Severity))
		fmt.Fprintf(w, "Issue:       %s\n", p.IssueType)
		fmt.Fprintf(w, "Container:   %s\n\n", p.FailingContainer)

		fmt.Fprintf(w, "Summary:\n  %s\n\n", p.Summary)
		fmt.Fprintf(w, "Likely root cause:\n  %s\n\n", p.RootCause)

		if len(p.FixCommands) > 0 {
			fmt.Fprintln(w, "Suggested commands:")
			for _, c := range p.FixCommands {
				fmt.Fprintf(w, "  $ %s\n", c)
			}
			fmt.Fprintln(w)
		}

		if p.Notes != "" {
			fmt.Fprintf(w, "Notes:\n  %s\n", p.Notes)
		}
	}
	fmt.Fprintln(w, "────────────────────────────────────────")
}

// Incident view
func RenderIncidentHuman(w io.Writer, r *IncidentResult) {
	fmt.Fprintln(w, "===== INCIDENT VIEW =====")

	if len(r.TopIssues) == 0 {
		fmt.Fprintln(w, "No significant incident-level issues detected.")
		return
	}

	fmt.Fprintln(w, "Top issues:")
	for _, i := range r.TopIssues {
		fmt.Fprintln(w, "─────────────────────────")
		fmt.Fprintf(w, "Namespace: %s\n", i.Namespace)
		fmt.Fprintf(w, "Name:      %s\n", i.Name)
		fmt.Fprintf(w, "Severity:  %s\n", strings.ToUpper(i.Severity))
		fmt.Fprintf(w, "Type:      %s\n\n", i.IssueType)
		fmt.Fprintf(w, "Summary:   %s\n", i.Summary)
		fmt.Fprintf(w, "Impact:    %s\n", i.Impact)
	}

	if len(r.RootCauses) > 0 {
		fmt.Fprintln(w, "\nRoot causes:")
		for _, rc := range r.RootCauses {
			fmt.Fprintf(w, "  - %s\n", rc)
		}
	}

	if len(r.Actions) > 0 {
		fmt.Fprintln(w, "\nActions:")
		for _, a := range r.Actions {
			fmt.Fprintf(w, "  $ %s\n", a)
		}
	}

	if len(r.Notes) > 0 {
		fmt.Fprintln(w, "\nNotes:")
		for _, n := range r.Notes {
			fmt.Fprintf(w, "  - %s\n", n)
		}
	}
}

// Teamlead view
func RenderTeamleadHuman(w io.Writer, r *TeamleadResult) {
	fmt.Fprintln(w, "===== TEAMLEAD VIEW =====")

	if len(r.BusinessRisk) > 0 {
		fmt.Fprintln(w, "Business risk:")
		for _, s := range r.BusinessRisk {
			fmt.Fprintf(w, "  - %s\n", s)
		}
	}

	if len(r.OwnershipHints) > 0 {
		fmt.Fprintln(w, "\nOwnership hints:")
		for _, s := range r.OwnershipHints {
			fmt.Fprintf(w, "  - %s\n", s)
		}
	}

	if len(r.TopActions) > 0 {
		fmt.Fprintln(w, "\nTop actions:")
		for _, s := range r.TopActions {
			fmt.Fprintf(w, "  - %s\n", s)
		}
	}

	if len(r.Escalation) > 0 {
		fmt.Fprintln(w, "\nEscalation conditions:")
		for _, s := range r.Escalation {
			fmt.Fprintf(w, "  - %s\n", s)
		}
	}
}

// Compliance view
func RenderComplianceHuman(w io.Writer, r *ComplianceResult) {
	if len(r.Issues) == 0 {
		fmt.Fprintln(w, "Compliance: no issues detected.")
		return
	}

	fmt.Fprintln(w, "===== COMPLIANCE ISSUES =====")
	for _, i := range r.Issues {
		fmt.Fprintln(w, "──────────────────────────────")
		fmt.Fprintf(w, "Namespace:    %s\n", i.Namespace)
		fmt.Fprintf(w, "Name:         %s\n", i.Name)
		fmt.Fprintf(w, "Type:         %s\n", i.Type)
		fmt.Fprintf(w, "Severity:     %s\n\n", i.Severity)
		fmt.Fprintf(w, "Issue:        %s\n", i.Description)
		fmt.Fprintf(w, "Recommendation:\n  %s\n", i.Recommendation)
	}
}

// Chaos view
func RenderChaosHuman(w io.Writer, r *ChaosResult) {
	fmt.Fprintln(w, "===== CHAOS EXPERIMENTS =====")

	if len(r.Vulnerabilities) > 0 {
		fmt.Fprintln(w, "Vulnerabilities:")
		for _, v := range r.Vulnerabilities {
			fmt.Fprintf(w, "  - %s\n", v)
		}
	}

	if len(r.Experiments) > 0 {
		fmt.Fprintln(w, "\nSuggested experiments:")
		for _, e := range r.Experiments {
			fmt.Fprintln(w, "────────────────────────")
			fmt.Fprintf(w, "Name:   %s\n", e.Name)
			fmt.Fprintf(w, "Reason: %s\n", e.Reason)
			fmt.Fprintf(w, "What to do:\n  %s\n", e.Description)
		}
	}

	if len(r.ImpactNotes) > 0 {
		fmt.Fprintln(w, "\nImpact notes:")
		for _, n := range r.ImpactNotes {
			fmt.Fprintf(w, "  - %s\n", n)
		}
	}
}

// Default cluster summary
func RenderDefaultHuman(w io.Writer, r *DefaultResult) {
	fmt.Fprintln(w, "===== CLUSTER SUMMARY =====")
	fmt.Fprintf(w, "Problem pods:          %d\n", r.Summary.ProblemPodCount)
	fmt.Fprintf(w, "Node readiness:        %s\n", r.Summary.NodeReadiness)
	fmt.Fprintf(w, "Resource pressure:     %s\n", r.Summary.ResourcePressure)

	if len(r.Summary.NamespacesWithIssues) > 0 {
		fmt.Fprintln(w, "Namespaces with issues:")
		for _, ns := range r.Summary.NamespacesWithIssues {
			fmt.Fprintf(w, "  - %s\n", ns)
		}
	}

	if len(r.Issues) > 0 {
		fmt.Fprintln(w, "\nIssues:")
		for _, i := range r.Issues {
			fmt.Fprintln(w, "────────────────────────")
			fmt.Fprintf(w, "Namespace: %s\n", i.Namespace)
			fmt.Fprintf(w, "Name:      %s\n", i.Name)
			fmt.Fprintf(w, "Type:      %s\n", i.IssueType)
			fmt.Fprintf(w, "Severity:  %s\n", i.Severity)
			fmt.Fprintf(w, "Summary:   %s\n", i.ShortSummary)
		}
	}

	if len(r.Recommendations) > 0 {
		fmt.Fprintln(w, "\nRecommendations:")
		for _, rec := range r.Recommendations {
			fmt.Fprintf(w, "  - %s\n", rec)
		}
	}
}
