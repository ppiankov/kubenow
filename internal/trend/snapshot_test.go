package trend

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveAndLoadSnapshot(t *testing.T) {
	// Use temp dir
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	t.Setenv("HOME", tmpDir)
	defer func() { _ = os.Setenv("HOME", origHome) }()

	snap := &Snapshot{
		Timestamp: time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC),
		Window:    "30d",
		Cluster:   "test-cluster",
		Workloads: []WorkloadSnapshot{
			{Namespace: "prod", Workload: "nginx", SkewCPU: 2.5, SkewMem: 1.8, WasteCPU: 0.3, WasteMem: 0.5},
			{Namespace: "prod", Workload: "api", SkewCPU: 3.0, SkewMem: 2.0, WasteCPU: 0.6, WasteMem: 1.0},
		},
		TotalWaste: TotalWaste{CPU: 0.9, MemGi: 1.5, CostMo: 120},
	}

	err := SaveSnapshot(snap)
	require.NoError(t, err)

	// Verify file exists
	dir := filepath.Join(tmpDir, ".kubenow", "trends")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)

	// Load and verify
	history, err := LoadHistory(30)
	require.NoError(t, err)
	require.Len(t, history, 1)
	assert.Equal(t, "30d", history[0].Window)
	assert.Len(t, history[0].Workloads, 2)
	assert.Equal(t, "nginx", history[0].Workloads[0].Workload)
	assert.InDelta(t, 0.9, history[0].TotalWaste.CPU, 0.001)
}

func TestLoadHistory_FiltersByDays(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Save an old snapshot (40 days ago) and a recent one
	old := &Snapshot{
		Timestamp: time.Now().AddDate(0, 0, -40),
		Window:    "30d",
		Workloads: []WorkloadSnapshot{{Namespace: "ns", Workload: "old", SkewCPU: 1.0}},
	}
	recent := &Snapshot{
		Timestamp: time.Now().Add(-1 * time.Hour),
		Window:    "30d",
		Workloads: []WorkloadSnapshot{{Namespace: "ns", Workload: "recent", SkewCPU: 2.0}},
	}

	require.NoError(t, SaveSnapshot(old))
	require.NoError(t, SaveSnapshot(recent))

	history, err := LoadHistory(30)
	require.NoError(t, err)
	assert.Len(t, history, 1)
	assert.Equal(t, "recent", history[0].Workloads[0].Workload)
}

func TestLoadHistory_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	history, err := LoadHistory(30)
	require.NoError(t, err)
	assert.Empty(t, history)
}

func TestLoadHistory_SortedByTimestamp(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	t2 := time.Now().Add(-1 * time.Hour)
	t1 := time.Now().Add(-2 * time.Hour)

	// Save in reverse order
	require.NoError(t, SaveSnapshot(&Snapshot{Timestamp: t2, Window: "30d", Workloads: []WorkloadSnapshot{{Namespace: "ns", Workload: "second"}}}))
	require.NoError(t, SaveSnapshot(&Snapshot{Timestamp: t1, Window: "30d", Workloads: []WorkloadSnapshot{{Namespace: "ns", Workload: "first"}}}))

	history, err := LoadHistory(30)
	require.NoError(t, err)
	require.Len(t, history, 2)
	assert.Equal(t, "first", history[0].Workloads[0].Workload)
	assert.Equal(t, "second", history[1].Workloads[0].Workload)
}

func TestSnapshotFilename(t *testing.T) {
	ts := time.Date(2026, 3, 7, 14, 30, 45, 0, time.UTC)
	name := snapshotFilename(ts)
	assert.Equal(t, "2026-03-07T143045Z.json", name)
}
