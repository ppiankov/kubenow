# Codex Work Order 8: Add tests for output and prompt packages

## Branch
`codex/test-output-prompt`

## Depends on
None.

## Problem
`internal/output/sarif.go` (356 lines) and `internal/prompt/` (2 files, 332 lines total) have zero test coverage. SARIF generation is a structured output format with strict schema. Prompt loading handles mode-specific template injection.

## Task
Create test files for both packages.

## Files to create
- `internal/output/sarif_test.go`
- `internal/prompt/prompt_test.go`

## Tests for output package

```go
package output

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSARIFFromRequestsSkew_ValidJSON(t *testing.T) {
	// Read GenerateSARIFFromRequestsSkew signature to understand input types.
	// Create minimal input data, generate SARIF, verify:
	// 1. Output is valid JSON
	// 2. Has "$schema" field
	// 3. Has "version" field set to "2.1.0"
	// 4. Has at least one "run" in "runs" array
}

func TestGenerateSARIFFromMonitor_ValidJSON(t *testing.T) {
	// Same pattern for monitor data
}

func TestSARIF_StructureSerialization(t *testing.T) {
	s := SARIF{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs:    []Run{},
	}
	data, err := json.Marshal(s)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"version":"2.1.0"`)
}
```

## Tests for prompt package

```go
package prompt

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadPrompt_AllModes(t *testing.T) {
	// Read LoadPrompt signature. Test each mode produces non-empty output.
	modes := []string{"default", "pod", "incident", "teamlead", "compliance", "chaos"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			prompt := LoadPrompt(mode, nil)
			assert.NotEmpty(t, prompt)
		})
	}
}

func TestLoadPrompt_WithEnhancements(t *testing.T) {
	// Read PromptEnhancements struct fields.
	// Create enhancements, verify they appear in the output.
	enhancements := &PromptEnhancements{
		// Fill based on actual struct
	}
	prompt := LoadPrompt("default", enhancements)
	assert.NotEmpty(t, prompt)
	// Verify enhancement content appears in output
}

func TestLoadPrompt_UnknownMode(t *testing.T) {
	// Unknown mode should fall back to default (or return error)
	prompt := LoadPrompt("nonexistent", nil)
	assert.NotEmpty(t, prompt)
}
```

## Verification
```bash
go test -race -count=1 ./internal/output/... ./internal/prompt/...
go vet ./internal/output/... ./internal/prompt/...
```

## Notes
- Read both packages first — the test stubs above are scaffolds, adjust to actual signatures.
- SARIF tests should verify JSON structure, not exact content strings.
- Prompt tests should verify non-empty output and presence of enhancements, not exact template text.
