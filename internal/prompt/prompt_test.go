package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadPrompt_AllModes(t *testing.T) {
	modes := []string{"default", "pod", "incident", "teamlead", "compliance", "chaos"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			out, err := LoadPrompt(mode, "{}", "", PromptEnhancements{})
			require.NoError(t, err)
			assert.NotEmpty(t, out)
			assert.Contains(t, out, "{}")
		})
	}
}

func TestLoadPrompt_WithEnhancements(t *testing.T) {
	enhancements := PromptEnhancements{
		Technical:   true,
		Priority:    true,
		Remediation: true,
	}
	out, err := LoadPrompt("default", "{}", "database", enhancements)
	require.NoError(t, err)
	assert.Contains(t, out, "ENHANCED OUTPUT REQUIREMENTS:")
	assert.Contains(t, out, "TECHNICAL DEPTH ENHANCEMENT")
	assert.Contains(t, out, "PRIORITY SCORING ENHANCEMENT")
	assert.Contains(t, out, "DETAILED REMEDIATION ENHANCEMENT")
	assert.Contains(t, out, "PROBLEM HINT: The user suspects this may be related to: database")
}

func TestLoadPrompt_UnknownMode(t *testing.T) {
	_, err := LoadPrompt("nonexistent", "{}", "", PromptEnhancements{})
	assert.Error(t, err)
}
