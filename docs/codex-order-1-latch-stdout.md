# Codex Work Order 1: Fix latch stdout contamination

## Branch
`codex/fix-latch-stdout`

## Problem
`internal/metrics/latch.go` uses `fmt.Printf` to print progress messages directly to stdout.
When the latch runs inside BubbleTea's alternate screen (pro-monitor TUI), these raw stdout
writes bleed through and corrupt the TUI rendering — showing duplicate progress bars and
`[latch]` prefixed lines mixed into the UI frame.

## Task
Replace all `fmt.Printf` calls in `latch.go` with a callback-based progress reporting pattern.

## Files to modify
- `internal/metrics/latch.go` (641 lines) — main changes
- `internal/cli/promonitor_latch.go` — wire callback when creating latch monitor

## Exact changes required

### 1. Add ProgressFunc to LatchConfig (latch.go)

In the `LatchConfig` struct, add:
```go
ProgressFunc func(msg string) // Optional progress callback. If nil, print to stderr.
```

### 2. Add a helper method (latch.go)

Add a method to LatchMonitor:
```go
func (m *LatchMonitor) progress(msg string) {
    if m.config.ProgressFunc != nil {
        m.config.ProgressFunc(msg)
    } else {
        fmt.Fprintln(os.Stderr, msg)
    }
}
```

### 3. Replace all fmt.Printf calls (latch.go)

There are 3+ locations with `fmt.Printf` in the Start() method:

Line ~100: `fmt.Printf("[latch] Starting spike monitoring for %s (sampling every %s)\n", ...)`
Line ~115: `fmt.Printf("[latch] Monitoring complete. Captured %d samples.\n", sampleCount)`
           `fmt.Printf("[latch] Checking for critical signals (OOMKills, restarts, evictions)...\n")`
Line ~129: `fmt.Printf("[latch] Progress: %.0f%% (%d/%d samples)\n", ...)`

Also check `checkAllCriticalSignals` and any other methods for additional `fmt.Printf` calls.

Replace ALL `fmt.Printf(...)` with `m.progress(fmt.Sprintf(...))`.

### 4. Wire callback in CLI (promonitor_latch.go)

In the `runLatch` function, do NOT set a ProgressFunc — let it default to stderr.
The TUI does not need progress messages from latch because the TUI model already reads
sample counts via `latch.GetSpikeData()` on every tick.

## Constraints
- Do NOT change any logic, only the output mechanism
- Do NOT add new dependencies
- Do NOT modify the BubbleTea model or UI code
- Do NOT change function signatures except LatchConfig
- Use `os.Stderr` as fallback, never stdout
- Run `make test && make vet` before finishing
- Do NOT create documentation files
- Do NOT add comments to code you did not change

## Verification
```bash
make test && make vet
grep -n 'fmt.Printf' internal/metrics/latch.go  # Should return 0 results
grep -n 'fmt.Fprintf(os.Stdout' internal/metrics/latch.go  # Should return 0 results
```

## Command
```bash
codex exec --full-auto --json --output-last-message /tmp/codex-latch-stdout.md \
  -C "/path/to/kubenow" \
  "Fix latch stdout contamination in internal/metrics/latch.go.

The latch monitor uses fmt.Printf to print progress messages to stdout. This corrupts
the BubbleTea TUI (alternate screen). Replace all fmt.Printf calls with a callback pattern.

Changes:
1. Add 'ProgressFunc func(msg string)' field to LatchConfig struct in latch.go
2. Add a 'progress(msg string)' method on LatchMonitor that calls ProgressFunc if set,
   otherwise writes to os.Stderr via fmt.Fprintln
3. Replace ALL fmt.Printf calls in latch.go with m.progress(fmt.Sprintf(...))
   - Check Start(), checkAllCriticalSignals(), and all other methods
4. Do NOT set ProgressFunc in internal/cli/promonitor_latch.go — let it default to stderr

Constraints:
- Do NOT change any logic, only output mechanism
- Do NOT add new dependencies
- Do NOT modify BubbleTea model or UI code
- Use os.Stderr as fallback, never stdout
- Run 'make test && make vet' before finishing
- Verify: grep -n 'fmt.Printf' internal/metrics/latch.go should return 0 results"
```
