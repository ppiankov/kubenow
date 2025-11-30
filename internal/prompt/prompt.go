// Loads template file + injects JSON.

package prompt

import (
    "fmt"
    "os"
    "strings"
)

func LoadPrompt(path string, snapshotJSON string) (string, error) {
    raw, err := os.ReadFile(path)
    if err != nil {
        return "", err
    }

    p := string(raw)
    p = strings.ReplaceAll(p, "{{SNAPSHOT}}", snapshotJSON)
    return p, nil
}

func PromptPath(mode string) string {
    switch mode {
    case "default":
        return "prompts/default.txt"
    case "pod":
        return "prompts/pod.txt"
    case "incident":
        return "prompts/incident.txt"
    case "teamlead":
        return "prompts/teamlead.txt"
    case "compliance":
        return "prompts/compliance.txt"
    case "chaos":
        return "prompts/chaos.txt"
    }
    return ""
}

func ValidateMode(mode string) error {
    switch mode {
    case "default", "pod", "incident", "teamlead", "compliance", "chaos":
        return nil
    }
    return fmt.Errorf("invalid mode: %s (valid: default|pod|incident|teamlead|compliance|chaos)", mode)
}
