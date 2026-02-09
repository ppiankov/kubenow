package baseline

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ppiankov/kubenow/internal/analyzer"
	"github.com/ppiankov/kubenow/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveAndLoadBaseline_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.json")
	start := time.Now()

	result := &analyzer.RequestsSkewResult{
		Metadata: analyzer.RequestsSkewMetadata{
			Window:         "30d",
			MinRuntimeDays: 7,
			GeneratedAt:    time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
			PrometheusURL:  "http://prometheus.local",
			Cluster:        "test-cluster",
		},
		Results: []analyzer.WorkloadSkewAnalysis{
			{
				Namespace: "default",
				Workload:  "app",
				Type:      "Deployment",
				SkewCPU:   1.2,
				Safety:    &models.SafetyAnalysis{Rating: models.SafetyRatingSafe},
			},
		},
	}

	err := SaveBaseline(result, path, "v1.2.3")
	require.NoError(t, err)

	loaded, err := LoadBaseline(path)
	require.NoError(t, err)
	end := time.Now()

	assert.Equal(t, "v1.2.3", loaded.Version)
	assert.Equal(t, result.Metadata, loaded.Metadata)
	assert.Equal(t, result.Results, loaded.Results)
	assert.False(t, loaded.Timestamp.IsZero())
	assert.True(t, !loaded.Timestamp.Before(start) && !loaded.Timestamp.After(end))
}

func TestLoadBaseline_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	_, err := LoadBaseline(path)
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
	baseline := &Baseline{
		Timestamp: time.Now(),
		Results: []analyzer.WorkloadSkewAnalysis{
			makeSkewAnalysis("default", "app", 1.0, models.SafetyRatingSafe, 0, 0),
		},
	}
	current := &analyzer.RequestsSkewResult{
		Results: []analyzer.WorkloadSkewAnalysis{
			makeSkewAnalysis("default", "app", 1.0, models.SafetyRatingSafe, 0, 0),
		},
	}

	report := CompareToBaseline(baseline, current)
	require.NotNil(t, report)
	assert.Len(t, report.Unchanged, 1)
	assert.Len(t, report.Improved, 0)
	assert.Len(t, report.Degraded, 0)
	assert.Len(t, report.New, 0)
	assert.Len(t, report.Removed, 0)
	assert.Equal(t, 1, report.Summary.TotalBaseline)
	assert.Equal(t, 1, report.Summary.TotalCurrent)
	assert.Equal(t, 1, report.Summary.Unchanged)
}

func TestCompareToBaseline_Improved(t *testing.T) {
	baseline := &Baseline{
		Timestamp: time.Now(),
		Results: []analyzer.WorkloadSkewAnalysis{
			makeSkewAnalysis("default", "app", 1.0, models.SafetyRatingRisky, 0, 0),
		},
	}
	current := &analyzer.RequestsSkewResult{
		Results: []analyzer.WorkloadSkewAnalysis{
			makeSkewAnalysis("default", "app", 1.0, models.SafetyRatingSafe, 0, 0),
		},
	}

	report := CompareToBaseline(baseline, current)
	require.NotNil(t, report)
	assert.Len(t, report.Improved, 1)
	assert.Len(t, report.Degraded, 0)
	assert.Len(t, report.Unchanged, 0)
	assert.Equal(t, 1, report.Summary.Improved)
}

func TestCompareToBaseline_Degraded(t *testing.T) {
	baseline := &Baseline{
		Timestamp: time.Now(),
		Results: []analyzer.WorkloadSkewAnalysis{
			makeSkewAnalysis("default", "worker", 1.0, models.SafetyRatingSafe, 0, 0),
		},
	}
	current := &analyzer.RequestsSkewResult{
		Results: []analyzer.WorkloadSkewAnalysis{
			makeSkewAnalysis("default", "worker", 1.0, models.SafetyRatingSafe, 1, 0),
		},
	}

	report := CompareToBaseline(baseline, current)
	require.NotNil(t, report)
	assert.Len(t, report.Degraded, 1)
	assert.Len(t, report.Improved, 0)
	assert.Len(t, report.Unchanged, 0)
	assert.Equal(t, 1, report.Summary.Degraded)
}

func makeSkewAnalysis(namespace, workload string, skew float64, rating models.SafetyRating, oomKills, restarts int) analyzer.WorkloadSkewAnalysis {
	return analyzer.WorkloadSkewAnalysis{
		Namespace: namespace,
		Workload:  workload,
		Type:      "Deployment",
		SkewCPU:   skew,
		Safety: &models.SafetyAnalysis{
			Rating:   rating,
			OOMKills: oomKills,
			Restarts: restarts,
		},
	}
}
