package promonitor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ppiankov/kubenow/internal/metrics"
)

// LatchResult is the persisted output of a completed latch session.
type LatchResult struct {
	Workload  WorkloadRef          `json:"workload"`
	Timestamp time.Time            `json:"timestamp"`
	Duration  time.Duration        `json:"duration"`
	Interval  time.Duration        `json:"interval"`
	Data      *metrics.SpikeData   `json:"data"`
	CPU       *metrics.Percentiles `json:"cpu_percentiles"`
	Memory    *metrics.Percentiles `json:"memory_percentiles"`
	Gaps      int                  `json:"gaps"`
	Valid     bool                 `json:"valid"`
	Reason    string               `json:"reason,omitempty"` // Why invalid, if applicable
}

// latchDir returns the directory for persisted latch files.
func latchDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".kubenow", "latch")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("cannot create latch directory: %w", err)
	}
	return dir, nil
}

// latchFilename returns the canonical filename for a workload's latch data.
func latchFilename(ref WorkloadRef) string {
	return fmt.Sprintf("%s__%s__%s.json", ref.Namespace, ref.Kind, ref.Name)
}

// SaveLatch persists a latch result to disk. Best-effort — errors are returned
// but should not block the user flow.
func SaveLatch(result *LatchResult) error {
	dir, err := latchDir()
	if err != nil {
		return err
	}

	path := filepath.Join(dir, latchFilename(result.Workload))
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal latch result: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write latch file: %w", err)
	}
	return nil
}

// LoadLatch reads a persisted latch result from disk.
func LoadLatch(ref WorkloadRef) (*LatchResult, error) {
	dir, err := latchDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, latchFilename(ref))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no latch data for %s in namespace %s", ref.String(), ref.Namespace)
		}
		return nil, fmt.Errorf("failed to read latch file: %w", err)
	}

	var result LatchResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse latch file: %w", err)
	}
	return &result, nil
}

// BuildLatchResult creates a LatchResult from completed latch data.
func BuildLatchResult(ref WorkloadRef, data *metrics.SpikeData, duration, interval time.Duration) *LatchResult {
	result := &LatchResult{
		Workload:  ref,
		Timestamp: time.Now(),
		Duration:  duration,
		Interval:  interval,
		Data:      data,
		Valid:     true,
	}

	if data == nil || len(data.CPUSamples) == 0 {
		result.Valid = false
		result.Reason = "no samples collected"
		return result
	}

	// Compute percentiles
	cpu, mem := data.ComputePercentiles()
	result.CPU = cpu
	result.Memory = mem

	// Detect gaps
	result.Gaps = data.GapCount(interval)

	// Validity checks
	maxGapPct := 0.10 // >10% gaps = invalid
	expected := int(duration/interval) + 1
	if expected > 0 && float64(result.Gaps)/float64(expected) > maxGapPct {
		result.Valid = false
		result.Reason = fmt.Sprintf("too many gaps: %d/%d (%.0f%%)", result.Gaps, expected, float64(result.Gaps)/float64(expected)*100)
	}

	return result
}
