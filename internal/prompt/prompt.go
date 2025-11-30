package prompt

import (
	"fmt"
	"strings"
)

func LoadPrompt(mode string, snapshotJSON string) (string, error) {
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

	out := strings.ReplaceAll(tmpl, "{{SNAPSHOT_JSON}}", snapshotJSON)
	out = strings.ReplaceAll(out, "{{SNAPSHOT}}", snapshotJSON)

	return out, nil
}
