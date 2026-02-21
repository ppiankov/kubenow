package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RateLimitConfig holds rate limiting parameters.
type RateLimitConfig struct {
	MaxGlobal      int           // max applies cluster-wide per window (0 = unlimited)
	MaxPerWorkload int           // max applies per workload per window (0 = unlimited)
	Window         time.Duration // tumbling window duration
	AuditPath      string        // base path for rate limit state files
}

// RateLimitState is the persisted state for a single rate counter.
type RateLimitState struct {
	WindowStart    time.Time        `json:"window_start"`
	WindowDuration time.Duration    `json:"window_duration"`
	Count          int              `json:"count"`
	Entries        []RateLimitEntry `json:"entries"`
}

// RateLimitEntry records a single apply event.
type RateLimitEntry struct {
	At       time.Time `json:"at"`
	Workload string    `json:"workload"`
	User     string    `json:"user"`
}

// RateLimitResult holds the outcome of a rate limit check.
type RateLimitResult struct {
	Allowed      bool   `json:"allowed"`
	DenialReason string `json:"denial_reason,omitempty"`
}

// CheckAndIncrement checks both global and per-workload rate limits.
// If both pass, it increments the counters and persists state.
func CheckAndIncrement(cfg RateLimitConfig, workloadUID, workloadRef, user string) (*RateLimitResult, error) {
	rateLimitDir := filepath.Join(cfg.AuditPath, ".ratelimit")
	if err := os.MkdirAll(rateLimitDir, 0755); err != nil {
		return nil, fmt.Errorf("create ratelimit dir: %w", err)
	}

	entry := RateLimitEntry{
		At:       time.Now(),
		Workload: workloadRef,
		User:     user,
	}

	// Check global limit
	globalPath := filepath.Join(rateLimitDir, "cluster.json")
	if cfg.MaxGlobal > 0 {
		allowed, err := checkRateFile(globalPath, cfg.MaxGlobal, cfg.Window, entry)
		if err != nil {
			return nil, fmt.Errorf("global rate check: %w", err)
		}
		if !allowed {
			return &RateLimitResult{
				Allowed:      false,
				DenialReason: fmt.Sprintf("global rate limit exceeded (%d applies in %s window)", cfg.MaxGlobal, cfg.Window),
			}, nil
		}
	}

	// Check per-workload limit
	if cfg.MaxPerWorkload > 0 && workloadUID != "" {
		wlPath := filepath.Join(rateLimitDir, workloadUID+".json")
		allowed, err := checkRateFile(wlPath, cfg.MaxPerWorkload, cfg.Window, entry)
		if err != nil {
			return nil, fmt.Errorf("per-workload rate check: %w", err)
		}
		if !allowed {
			return &RateLimitResult{
				Allowed:      false,
				DenialReason: fmt.Sprintf("per-workload rate limit exceeded (%d applies in %s window)", cfg.MaxPerWorkload, cfg.Window),
			}, nil
		}
	}

	// If we skipped checks (max=0), still record the entry in global
	if cfg.MaxGlobal == 0 {
		if err := recordEntry(globalPath, cfg.Window, entry); err != nil {
			fmt.Fprintf(os.Stderr, "[kubenow] warning: failed to record global rate entry: %v\n", err)
		}
	}
	if cfg.MaxPerWorkload == 0 && workloadUID != "" {
		wlPath := filepath.Join(rateLimitDir, workloadUID+".json")
		if err := recordEntry(wlPath, cfg.Window, entry); err != nil {
			fmt.Fprintf(os.Stderr, "[kubenow] warning: failed to record workload rate entry: %v\n", err)
		}
	}

	return &RateLimitResult{Allowed: true}, nil
}

// checkRateFile reads, checks, and increments a rate limit state file under flock.
// Returns true if the operation is within limits (and the entry was recorded).
func checkRateFile(path string, maxCount int, window time.Duration, entry RateLimitEntry) (bool, error) {
	fd, err := acquireFlock(path + ".lock")
	if err != nil {
		return false, fmt.Errorf("acquire flock: %w", err)
	}
	defer releaseFlock(fd)

	state, err := readState(path)
	if err != nil {
		return false, err
	}

	now := time.Now()

	// Tumbling window: reset if expired
	if state.WindowStart.IsZero() || now.After(state.WindowStart.Add(window)) {
		state = &RateLimitState{
			WindowStart:    now,
			WindowDuration: window,
		}
	}

	// Check limit
	if state.Count >= maxCount {
		return false, nil
	}

	// Increment
	state.Count++
	state.Entries = append(state.Entries, entry)

	if err := writeState(path, state); err != nil {
		return false, err
	}

	return true, nil
}

// recordEntry adds an entry to a state file without checking limits (for unlimited counters).
func recordEntry(path string, window time.Duration, entry RateLimitEntry) error {
	fd, err := acquireFlock(path + ".lock")
	if err != nil {
		return err
	}
	defer releaseFlock(fd)

	state, err := readState(path)
	if err != nil {
		return err
	}

	now := time.Now()
	if state.WindowStart.IsZero() || now.After(state.WindowStart.Add(window)) {
		state = &RateLimitState{
			WindowStart:    now,
			WindowDuration: window,
		}
	}

	state.Count++
	state.Entries = append(state.Entries, entry)
	return writeState(path, state)
}

func readState(path string) (*RateLimitState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &RateLimitState{}, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}

	var state RateLimitState
	if err := json.Unmarshal(data, &state); err != nil {
		// Corrupted file — start fresh
		return &RateLimitState{}, nil
	}
	return &state, nil
}

func writeState(path string, state *RateLimitState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

// Peek checks the global rate limit without incrementing counters.
// Returns Allowed=true optimistically on any error (missing dir, corrupted state).
func Peek(cfg RateLimitConfig) (*RateLimitResult, error) {
	if cfg.MaxGlobal <= 0 {
		return &RateLimitResult{Allowed: true}, nil
	}

	globalPath := filepath.Join(cfg.AuditPath, ".ratelimit", "cluster.json")
	allowed, err := peekRateFile(globalPath, cfg.MaxGlobal, cfg.Window)
	if err != nil {
		// Optimistic: don't block apply on read errors
		return &RateLimitResult{Allowed: true}, nil
	}
	if !allowed {
		return &RateLimitResult{
			Allowed:      false,
			DenialReason: fmt.Sprintf("global rate limit exceeded (%d applies in %s window)", cfg.MaxGlobal, cfg.Window),
		}, nil
	}
	return &RateLimitResult{Allowed: true}, nil
}

// peekRateFile reads rate limit state without modifying it.
func peekRateFile(path string, maxCount int, window time.Duration) (bool, error) {
	state, err := readState(path)
	if err != nil {
		return false, err
	}

	now := time.Now()
	if state.WindowStart.IsZero() || now.After(state.WindowStart.Add(window)) {
		return true, nil // window expired, would reset
	}

	return state.Count < maxCount, nil
}

// acquireFlock and releaseFlock are in flock_unix.go / flock_windows.go
