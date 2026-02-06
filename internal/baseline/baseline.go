package baseline

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/ppiankov/kubenow/internal/analyzer"
)

// Baseline represents a saved snapshot of analysis results
type Baseline struct {
	Timestamp time.Time                       `json:"timestamp"`
	Version   string                          `json:"version"`
	Metadata  analyzer.RequestsSkewMetadata   `json:"metadata"`
	Results   []analyzer.WorkloadSkewAnalysis `json:"results"`
}

// DriftReport shows changes between baseline and current
type DriftReport struct {
	BaselineTime time.Time       `json:"baseline_time"`
	CurrentTime  time.Time       `json:"current_time"`
	New          []WorkloadDrift `json:"new"`           // New workloads
	Removed      []WorkloadDrift `json:"removed"`       // Removed workloads
	Improved     []WorkloadDrift `json:"improved"`      // Better now
	Degraded     []WorkloadDrift `json:"degraded"`      // Worse now
	Unchanged    []WorkloadDrift `json:"unchanged"`     // No significant change
	Summary      DriftSummary    `json:"summary"`
}

type WorkloadDrift struct {
	Namespace      string  `json:"namespace"`
	Workload       string  `json:"workload"`
	Type           string  `json:"type"`
	BaselineSkew   float64 `json:"baseline_skew,omitempty"`
	CurrentSkew    float64 `json:"current_skew,omitempty"`
	SkewChange     float64 `json:"skew_change,omitempty"`
	BaselineSafety string  `json:"baseline_safety,omitempty"`
	CurrentSafety  string  `json:"current_safety,omitempty"`
}

type DriftSummary struct {
	TotalBaseline int `json:"total_baseline"`
	TotalCurrent  int `json:"total_current"`
	New           int `json:"new"`
	Removed       int `json:"removed"`
	Improved      int `json:"improved"`
	Degraded      int `json:"degraded"`
	Unchanged     int `json:"unchanged"`
}

// SaveBaseline saves analysis results as a baseline
func SaveBaseline(result *analyzer.RequestsSkewResult, filepath string, version string) error {
	baseline := Baseline{
		Timestamp: time.Now(),
		Version:   version,
		Metadata:  result.Metadata,
		Results:   result.Results,
	}

	data, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal baseline: %w", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write baseline file: %w", err)
	}

	return nil
}

// LoadBaseline loads a saved baseline
func LoadBaseline(filepath string) (*Baseline, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read baseline file: %w", err)
	}

	var baseline Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return nil, fmt.Errorf("failed to parse baseline: %w", err)
	}

	return &baseline, nil
}

// CompareToBaseline compares current results to a baseline
func CompareToBaseline(baseline *Baseline, current *analyzer.RequestsSkewResult) *DriftReport {
	report := &DriftReport{
		BaselineTime: baseline.Timestamp,
		CurrentTime:  time.Now(),
		New:          make([]WorkloadDrift, 0),
		Removed:      make([]WorkloadDrift, 0),
		Improved:     make([]WorkloadDrift, 0),
		Degraded:     make([]WorkloadDrift, 0),
		Unchanged:    make([]WorkloadDrift, 0),
	}

	// Create maps for lookup
	baselineMap := make(map[string]analyzer.WorkloadSkewAnalysis)
	for _, w := range baseline.Results {
		key := fmt.Sprintf("%s/%s", w.Namespace, w.Workload)
		baselineMap[key] = w
	}

	currentMap := make(map[string]analyzer.WorkloadSkewAnalysis)
	for _, w := range current.Results {
		key := fmt.Sprintf("%s/%s", w.Namespace, w.Workload)
		currentMap[key] = w
	}

	// Find new and changed workloads
	for key, curr := range currentMap {
		if base, exists := baselineMap[key]; exists {
			// Workload exists in both - check for changes
			drift := WorkloadDrift{
				Namespace:      curr.Namespace,
				Workload:       curr.Workload,
				Type:           curr.Type,
				BaselineSkew:   base.SkewCPU,
				CurrentSkew:    curr.SkewCPU,
				SkewChange:     curr.SkewCPU - base.SkewCPU,
			}

			if base.Safety != nil {
				drift.BaselineSafety = string(base.Safety.Rating)
			}
			if curr.Safety != nil {
				drift.CurrentSafety = string(curr.Safety.Rating)
			}

			// Categorize change
			if isImproved(base, curr) {
				report.Improved = append(report.Improved, drift)
			} else if isDegraded(base, curr) {
				report.Degraded = append(report.Degraded, drift)
			} else {
				report.Unchanged = append(report.Unchanged, drift)
			}
		} else {
			// New workload
			drift := WorkloadDrift{
				Namespace:   curr.Namespace,
				Workload:    curr.Workload,
				Type:        curr.Type,
				CurrentSkew: curr.SkewCPU,
			}
			if curr.Safety != nil {
				drift.CurrentSafety = string(curr.Safety.Rating)
			}
			report.New = append(report.New, drift)
		}
	}

	// Find removed workloads
	for key, base := range baselineMap {
		if _, exists := currentMap[key]; !exists {
			drift := WorkloadDrift{
				Namespace:    base.Namespace,
				Workload:     base.Workload,
				Type:         base.Type,
				BaselineSkew: base.SkewCPU,
			}
			if base.Safety != nil {
				drift.BaselineSafety = string(base.Safety.Rating)
			}
			report.Removed = append(report.Removed, drift)
		}
	}

	// Calculate summary
	report.Summary = DriftSummary{
		TotalBaseline: len(baseline.Results),
		TotalCurrent:  len(current.Results),
		New:           len(report.New),
		Removed:       len(report.Removed),
		Improved:      len(report.Improved),
		Degraded:      len(report.Degraded),
		Unchanged:     len(report.Unchanged),
	}

	return report
}

// isImproved checks if workload has improved
func isImproved(base, curr analyzer.WorkloadSkewAnalysis) bool {
	// Lower skew is better
	if curr.SkewCPU < base.SkewCPU-0.5 {
		return true
	}

	// Better safety rating
	if base.Safety != nil && curr.Safety != nil {
		baseScore := safetyScore(string(base.Safety.Rating))
		currScore := safetyScore(string(curr.Safety.Rating))
		if currScore > baseScore {
			return true
		}
	}

	return false
}

// isDegraded checks if workload has degraded
func isDegraded(base, curr analyzer.WorkloadSkewAnalysis) bool {
	// Higher skew is worse
	if curr.SkewCPU > base.SkewCPU+0.5 {
		return true
	}

	// Worse safety rating
	if base.Safety != nil && curr.Safety != nil {
		baseScore := safetyScore(string(base.Safety.Rating))
		currScore := safetyScore(string(curr.Safety.Rating))
		if currScore < baseScore {
			return true
		}
	}

	// New OOMKills or crashes
	if base.Safety != nil && curr.Safety != nil {
		if curr.Safety.OOMKills > base.Safety.OOMKills ||
		   curr.Safety.Restarts > base.Safety.Restarts {
			return true
		}
	}

	return false
}

// safetyScore converts rating to numeric score for comparison
func safetyScore(rating string) int {
	switch rating {
	case "SAFE":
		return 4
	case "CAUTION":
		return 3
	case "RISKY":
		return 2
	case "UNSAFE":
		return 1
	default:
		return 0
	}
}
