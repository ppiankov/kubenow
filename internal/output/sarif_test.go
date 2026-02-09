package output

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ppiankov/kubenow/internal/analyzer"
	"github.com/ppiankov/kubenow/internal/models"
	"github.com/ppiankov/kubenow/internal/monitor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSARIFFromRequestsSkew_ValidJSON(t *testing.T) {
	result := &analyzer.RequestsSkewResult{
		Results: []analyzer.WorkloadSkewAnalysis{
			{
				Namespace:    "default",
				Workload:     "api",
				Type:         "Deployment",
				RequestedCPU: 2.0,
				P99UsedCPU:   1.0,
				SkewCPU:      2.5,
				ImpactScore:  1.2,
				Runtime:      "24h",
				Safety:       &models.SafetyAnalysis{Rating: models.SafetyRatingSafe},
			},
		},
	}

	data, err := GenerateSARIFFromRequestsSkew(result, "1.0.0")
	require.NoError(t, err)

	var sarif SARIF
	err = json.Unmarshal(data, &sarif)
	require.NoError(t, err)
	assert.Equal(t, "2.1.0", sarif.Version)
	assert.NotEmpty(t, sarif.Schema)
	assert.Len(t, sarif.Runs, 1)
	assert.NotEmpty(t, sarif.Runs[0].Results)
}

func TestGenerateSARIFFromMonitor_ValidJSON(t *testing.T) {
	problems := []monitor.Problem{
		{
			Severity:      monitor.SeverityCritical,
			Type:          "OOMKilled",
			Namespace:     "default",
			PodName:       "api-123",
			ContainerName: "api",
			Message:       "killed",
			FirstSeen:     time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
			LastSeen:      time.Date(2024, 1, 2, 4, 4, 5, 0, time.UTC),
			Count:         1,
		},
	}

	data, err := GenerateSARIFFromMonitor(problems, "1.0.0")
	require.NoError(t, err)

	var sarif SARIF
	err = json.Unmarshal(data, &sarif)
	require.NoError(t, err)
	assert.Equal(t, "2.1.0", sarif.Version)
	assert.NotEmpty(t, sarif.Schema)
	assert.Len(t, sarif.Runs, 1)
	assert.Len(t, sarif.Runs[0].Results, 1)
	assert.Equal(t, "pod-oomkilled", sarif.Runs[0].Results[0].RuleID)
}

func TestSARIF_StructureSerialization(t *testing.T) {
	s := SARIF{
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
		Version: "2.1.0",
		Runs:    []Run{},
	}
	data, err := json.Marshal(s)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"version":"2.1.0"`)
}
