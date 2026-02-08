# Codex Work Order 2: Improve UNSAFE UX in pro-monitor TUI

## Branch
`codex/unsafe-ux`

## Problem
When pro-monitor shows Safety: UNSAFE, the UI only says "no recommendation produced".
The operator has no idea WHY it is UNSAFE (how many OOMKills? restarts? evictions?)
and no guidance on what to do next.

## Task
Enhance the recommendation rendering to show UNSAFE detail and actionable guidance.

## Files to modify
- `internal/promonitor/recommend.go` — add detail warnings in UNSAFE block
- `internal/promonitor/ui.go` — show UNSAFE guidance text

## Exact changes required

### 1. Add detail warnings in recommend.go

In the UNSAFE early-return block (around line 119-123), add detail warnings BEFORE
the existing "safety rating UNSAFE" warning:

```go
if safety == SafetyRatingUnsafe {
    result.Confidence = ConfidenceLow
    if latch.Data != nil {
        if latch.Data.OOMKills > 0 {
            result.Warnings = append(result.Warnings,
                fmt.Sprintf("observed %d OOMKill(s) during latch", latch.Data.OOMKills))
        }
        if latch.Data.Restarts > 0 {
            result.Warnings = append(result.Warnings,
                fmt.Sprintf("observed %d container restart(s) during latch", latch.Data.Restarts))
        }
        if latch.Data.Evictions > 0 {
            result.Warnings = append(result.Warnings,
                fmt.Sprintf("observed %d pod eviction(s) during latch", latch.Data.Evictions))
        }
    }
    result.Warnings = append(result.Warnings, "safety rating UNSAFE: no recommendation produced")
    result.Evidence = buildEvidence(latch)
    return result
}
```

### 2. Show guidance text in ui.go

In `renderRecommendation` function, where it checks `if len(rec.Containers) == 0`
(around line 243), replace the generic message with UNSAFE-specific guidance:

```go
if len(rec.Containers) == 0 {
    b.WriteString("\n")
    if rec.Safety == SafetyRatingUnsafe {
        b.WriteString(warnStyle.Render("  Increase resources manually — current allocation is"))
        b.WriteString("\n")
        b.WriteString(warnStyle.Render("  insufficient for observed workload behavior."))
    } else {
        b.WriteString(dimStyle.Render("  No actionable recommendation produced."))
    }
    b.WriteString("\n")
}
```

## Constraints
- Only modify ui.go and the UNSAFE block in recommend.go
- Do NOT change safety rating logic or thresholds
- Do NOT change any other rendering functions
- Do NOT add new dependencies
- Use existing style variables (warnStyle, dimStyle, headerStyle)
- Run `make test && make vet` before finishing
- Do NOT create documentation files
- Do NOT add comments to code you did not change

## Verification
```bash
make test && make vet
```

## Command
```bash
codex exec --full-auto --json --output-last-message /tmp/codex-unsafe-ux.md \
  -C "/path/to/kubenow" \
  "Improve UNSAFE UX in pro-monitor TUI.

Two changes:

1. In internal/promonitor/recommend.go, in the UNSAFE early-return block (around line 119
   where 'if safety == SafetyRatingUnsafe'), add detail warnings BEFORE the existing
   'safety rating UNSAFE' warning. Add warnings for OOMKills count, Restarts count,
   and Evictions count from latch.Data (check for nil and >0). Keep the existing
   'result.Confidence = ConfidenceLow' line that is already there.

2. In internal/promonitor/ui.go, in renderRecommendation function, where it checks
   'if len(rec.Containers) == 0' (around line 243), change the display:
   - If rec.Safety == SafetyRatingUnsafe: show in warnStyle two lines:
     'Increase resources manually — current allocation is'
     'insufficient for observed workload behavior.'
   - Otherwise: keep the existing dimStyle 'No actionable recommendation produced.'

Constraints:
- Only modify ui.go and recommend.go
- Do NOT change safety rating logic or thresholds
- Use existing style variables (warnStyle, dimStyle)
- Run 'make test && make vet' before finishing
- Do NOT create documentation files
- Do NOT add comments to code you did not change"
```
