# Codex Work Order 7: Add tests for export and result packages

## Branch
`codex/test-export-result`

## Depends on
None.

## Problem
`internal/export/` (4 files) and `internal/result/` (1 file, 291 lines) have zero test coverage. Export handles format detection and multi-format output. Result handles human-readable rendering for 6 output types.

## Task
Create test files for both packages.

## Files to create
- `internal/export/export_test.go`
- `internal/result/result_test.go`

## Tests for export package

```go
package export

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   Format
	}{
		{"json extension", "output.json", FormatJSON},
		{"markdown extension", "output.md", FormatMarkdown},
		{"markdown full", "output.markdown", FormatMarkdown},
		{"html extension", "output.html", FormatHTML},
		{"text extension", "output.txt", FormatText},
		{"unknown extension", "output.xyz", FormatText},
		{"no extension", "output", FormatText},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectFormat(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
```

Read the actual `DetectFormat()` signature and format constants — adjust the test values to match.

For `Export()` method tests, use `t.TempDir()` to create temp output files:
- Test JSON export produces valid JSON
- Test Markdown export contains header markers (`#`, `##`)
- Test text export produces non-empty output

## Tests for result package

```go
package result

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrettyJSON(t *testing.T) {
	// Create a minimal result struct, call PrettyJSON, verify valid JSON
}

func TestRenderPodHuman(t *testing.T) {
	// Create PodResult with known fields, render, verify output contains key fields
}

func TestRenderIncidentHuman(t *testing.T) {
	// Same pattern for IncidentResult
}

func TestRenderTeamleadHuman(t *testing.T) {
	// Same pattern for TeamleadResult
}

func TestRenderComplianceHuman(t *testing.T) {
	// Same pattern for ComplianceResult
}

func TestRenderChaosHuman(t *testing.T) {
	// Same pattern for ChaosResult
}

func TestRenderDefaultHuman(t *testing.T) {
	// Same pattern for DefaultResult
}
```

## Verification
```bash
go test -race -count=1 ./internal/export/... ./internal/result/...
go vet ./internal/export/... ./internal/result/...
```

## Notes
- Read each file before writing tests — the stubs above are scaffolds.
- For render tests: verify the output `strings.Contains()` key field values you set. Don't assert exact formatting — that's brittle.
- HTML export is a stub (returns nil) — test that it doesn't error.
- Use `testify/assert` — already in go.mod.
