// Package trend provides historical trend tracking for requests-skew analysis.
package trend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// WorkloadSnapshot captures a single workload's skew at a point in time.
type WorkloadSnapshot struct {
	Namespace string  `json:"namespace"`
	Workload  string  `json:"workload"`
	SkewCPU   float64 `json:"skew_cpu"`
	SkewMem   float64 `json:"skew_memory"`
	WasteCPU  float64 `json:"waste_cpu"`    // requested - p95 used (cores)
	WasteMem  float64 `json:"waste_mem_gi"` // requested - p95 used (GiB)
}

// Snapshot captures the full analysis result at a point in time.
type Snapshot struct {
	Timestamp  time.Time          `json:"timestamp"`
	Window     string             `json:"window"`
	Cluster    string             `json:"cluster,omitempty"`
	Workloads  []WorkloadSnapshot `json:"workloads"`
	TotalWaste TotalWaste         `json:"total_waste"`
}

// TotalWaste captures cluster-wide waste totals.
type TotalWaste struct {
	CPU    float64 `json:"cpu_cores"`
	MemGi  float64 `json:"memory_gi"`
	CostMo float64 `json:"cost_monthly,omitempty"`
}

// trendDir returns the directory for persisted trend files.
func trendDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	dir := filepath.Join(home, ".kubenow", "trends")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create trend directory: %w", err)
	}
	return dir, nil
}

// snapshotFilename generates a filename from timestamp.
func snapshotFilename(t time.Time) string {
	return t.UTC().Format("2006-01-02T150405Z") + ".json"
}

// SaveSnapshot persists an analysis snapshot to disk.
func SaveSnapshot(snap *Snapshot) error {
	dir, err := trendDir()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	path := filepath.Join(dir, snapshotFilename(snap.Timestamp))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}
	return nil
}

// LoadHistory loads all snapshots from the last N days, sorted by timestamp.
func LoadHistory(days int) ([]Snapshot, error) {
	dir, err := trendDir()
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().AddDate(0, 0, -days)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read trend directory: %w", err)
	}

	var snapshots []Snapshot
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var snap Snapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			continue
		}

		if snap.Timestamp.Before(cutoff) {
			continue
		}

		snapshots = append(snapshots, snap)
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Timestamp.Before(snapshots[j].Timestamp)
	})

	return snapshots, nil
}
