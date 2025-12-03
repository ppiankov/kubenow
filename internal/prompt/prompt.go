package prompt

import (
	"fmt"
	"strings"
)

// PromptEnhancements controls optional prompt enhancements.
type PromptEnhancements struct {
	Technical    bool // Add technical depth (stack traces, config diffs, deeper analysis)
	Priority     bool // Add priority scoring (numerical scores, SLO impact, blast radius)
	Remediation  bool // Add detailed remediation (step-by-step fixes, rollback, prevention)
}

func LoadPrompt(mode string, snapshotJSON string, problemHint string, enhancements PromptEnhancements) (string, error) {
	var tmpl string

	switch mode {
	case "default":
		tmpl = PromptDefault
	case "pod":
		tmpl = PromptPod
	case "incident":
		tmpl = PromptIncident
	case "teamlead":
		tmpl = PromptTeamlead
	case "compliance":
		tmpl = PromptCompliance
	case "chaos":
		tmpl = PromptChaos
	default:
		return "", fmt.Errorf("invalid mode: %s", mode)
	}

	// Inject enhancements before snapshot if any are enabled
	if enhancements.Technical || enhancements.Priority || enhancements.Remediation {
		tmpl = injectEnhancements(tmpl, enhancements)
	}

	out := strings.ReplaceAll(tmpl, "{{SNAPSHOT_JSON}}", snapshotJSON)
	out = strings.ReplaceAll(out, "{{SNAPSHOT}}", snapshotJSON)

	// Add problem hint if provided
	if problemHint != "" {
		hintSection := fmt.Sprintf("\n\nPROBLEM HINT: The user suspects this may be related to: %s\nPlease prioritize analysis in this direction while still identifying other issues.\n", problemHint)
		out = out + hintSection
	}

	return out, nil
}

// injectEnhancements injects enhancement instructions into the prompt template.
func injectEnhancements(tmpl string, enh PromptEnhancements) string {
	// Find injection point - before BEGIN_SNAPSHOT marker
	marker := "BEGIN_SNAPSHOT"
	idx := strings.Index(tmpl, marker)

	if idx == -1 {
		// Fallback: append at the end if marker not found
		return tmpl + buildEnhancementSection(enh)
	}

	// Inject before BEGIN_SNAPSHOT
	enhancementSection := buildEnhancementSection(enh)
	return tmpl[:idx] + enhancementSection + tmpl[idx:]
}

// buildEnhancementSection builds the enhancement instructions based on flags.
func buildEnhancementSection(enh PromptEnhancements) string {
	var sb strings.Builder

	sb.WriteString("\nENHANCED OUTPUT REQUIREMENTS:\n")
	sb.WriteString("The following enhancements have been requested. Add these OPTIONAL fields to your JSON output:\n\n")

	if enh.Technical {
		sb.WriteString(EnhancementTechnical)
		sb.WriteString("\n")
	}

	if enh.Priority {
		sb.WriteString(EnhancementPriority)
		sb.WriteString("\n")
	}

	if enh.Remediation {
		sb.WriteString(EnhancementRemediation)
		sb.WriteString("\n")
	}

	sb.WriteString("All enhancement fields are OPTIONAL. Only include them when the data supports meaningful content.\n\n")

	return sb.String()
}
