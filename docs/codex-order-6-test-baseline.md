# Codex Work Order 6: Add tests for baseline package

## Branch
`codex/test-baseline`

## Depends on
None.

## Problem
`internal/baseline/baseline.go` (247 lines) has zero test coverage. Contains JSON serialization (`SaveBaseline`/`LoadBaseline`) and drift comparison (`CompareToBaseline`) — all untested.

## Task
Create `internal/baseline/baseline_test.go` with tests for all exported functions.

## Files to create
- `internal/baseline/baseline_test.go`

## Tests to add

```go
package baseline

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveAndLoadBaseline_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")

	b := &Baseline{
		// Populate with representative fields from the Baseline struct.
		// Read baseline.go to determine exact field names and types.
	}

	err := SaveBaseline(b, path)
	require.NoError(t, err)

	loaded, err := LoadBaseline(path)
	require.NoError(t, err)
	assert.Equal(t, b, loaded)
}

func TestLoadBaseline_MissingFile(t *testing.T) {
	_, err := LoadBaseline("/nonexistent/path.json")
	assert.Error(t, err)
}

func TestLoadBaseline_CorruptedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	err := os.WriteFile(path, []byte("{invalid"), 0644)
	require.NoError(t, err)

	_, err = LoadBaseline(path)
	assert.Error(t, err)
}

func TestCompareToBaseline_NoDrift(t *testing.T) {
	// Same baseline and current state — no drift expected
	// Read CompareToBaseline signature and populate inputs accordingly
}

func TestCompareToBaseline_Improved(t *testing.T) {
	// Current state is better than baseline — drift should show improvement
}

func TestCompareToBaseline_Degraded(t *testing.T) {
	// Current state is worse than baseline — drift should show degradation
}
```

## Verification
```bash
go test -race -count=1 ./internal/baseline/...
go vet ./internal/baseline/...
```

## Notes
- Read `baseline.go` first to understand the exact struct fields and function signatures. The test stubs above are scaffolds — fill in actual field values.
- Use `t.TempDir()` for all file operations — it auto-cleans.
- Import `os` for `os.WriteFile` in the corrupted JSON test.
- No mocks needed — pure functions + filesystem.
