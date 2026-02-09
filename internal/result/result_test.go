package result

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrettyJSON(t *testing.T) {
	input := map[string]string{"status": "ok"}
	out, err := PrettyJSON(input)
	require.NoError(t, err)

	var decoded map[string]string
	err = json.Unmarshal([]byte(out), &decoded)
	require.NoError(t, err)
	assert.Equal(t, input, decoded)
}

func TestRenderPodHuman(t *testing.T) {
	var buf bytes.Buffer
	r := &PodResult{
		Pods: []struct {
			Namespace        string   `json:"namespace"`
			Name             string   `json:"name"`
			Severity         string   `json:"severity"`
			IssueType        string   `json:"issue_type"`
			FailingContainer string   `json:"failing_container"`
			Summary          string   `json:"summary"`
			RootCause        string   `json:"root_cause"`
			FixCommands      []string `json:"fix_commands"`
			Notes            string   `json:"notes"`
		}{
			{
				Namespace:        "default",
				Name:             "api-123",
				Severity:         "high",
				IssueType:        "CrashLoopBackOff",
				FailingContainer: "api",
				Summary:          "pod crash",
				RootCause:        "OOM",
				FixCommands:      []string{"kubectl logs pod"},
				Notes:            "check memory",
			},
		},
	}
	RenderPodHuman(&buf, r)
	out := buf.String()
	assert.Contains(t, out, "default")
	assert.Contains(t, out, "api-123")
	assert.Contains(t, out, "CrashLoopBackOff")
	assert.Contains(t, out, "OOM")
}

func TestRenderIncidentHuman(t *testing.T) {
	var buf bytes.Buffer
	r := &IncidentResult{
		TopIssues: []struct {
			Namespace string `json:"namespace"`
			Name      string `json:"name"`
			Severity  string `json:"severity"`
			IssueType string `json:"issue_type"`
			Summary   string `json:"summary"`
			Impact    string `json:"impact"`
		}{
			{
				Namespace: "default",
				Name:      "api",
				Severity:  "critical",
				IssueType: "OOM",
				Summary:   "memory pressure",
				Impact:    "high latency",
			},
		},
		RootCauses: []string{"leak"},
		Actions:    []string{"scale up"},
		Notes:      []string{"watch for restarts"},
	}
	RenderIncidentHuman(&buf, r)
	out := buf.String()
	assert.Contains(t, out, "INCIDENT VIEW")
	assert.Contains(t, out, "api")
	assert.Contains(t, out, "OOM")
	assert.Contains(t, out, "leak")
}

func TestRenderTeamleadHuman(t *testing.T) {
	var buf bytes.Buffer
	r := &TeamleadResult{
		BusinessRisk:   []string{"revenue impact"},
		OwnershipHints: []string{"team-a"},
		TopActions:     []string{"scale deployment"},
		Escalation:     []string{"page on-call"},
	}
	RenderTeamleadHuman(&buf, r)
	out := buf.String()
	assert.Contains(t, out, "TEAMLEAD VIEW")
	assert.Contains(t, out, "revenue impact")
	assert.Contains(t, out, "team-a")
}

func TestRenderComplianceHuman(t *testing.T) {
	var buf bytes.Buffer
	r := &ComplianceResult{
		Issues: []struct {
			Namespace      string `json:"namespace"`
			Name           string `json:"name"`
			Type           string `json:"type"`
			Severity       string `json:"severity"`
			Description    string `json:"description"`
			Recommendation string `json:"recommendation"`
		}{
			{
				Namespace:      "default",
				Name:           "api",
				Type:           "policy",
				Severity:       "high",
				Description:    "missing label",
				Recommendation: "add label",
			},
		},
	}
	RenderComplianceHuman(&buf, r)
	out := buf.String()
	assert.Contains(t, out, "COMPLIANCE ISSUES")
	assert.Contains(t, out, "missing label")
	assert.Contains(t, out, "add label")
}

func TestRenderChaosHuman(t *testing.T) {
	var buf bytes.Buffer
	r := &ChaosResult{
		Vulnerabilities: []string{"single replica"},
		Experiments: []struct {
			Name        string `json:"name"`
			Reason      string `json:"reason"`
			Description string `json:"description"`
		}{
			{
				Name:        "kill pod",
				Reason:      "test failover",
				Description: "terminate a pod",
			},
		},
		ImpactNotes: []string{"expect brief blip"},
	}
	RenderChaosHuman(&buf, r)
	out := buf.String()
	assert.Contains(t, out, "CHAOS EXPERIMENTS")
	assert.Contains(t, out, "kill pod")
	assert.Contains(t, out, "expect brief blip")
}

func TestRenderDefaultHuman(t *testing.T) {
	var buf bytes.Buffer
	r := &DefaultResult{}
	r.Summary.ProblemPodCount = 3
	r.Summary.NodeReadiness = "ready"
	r.Summary.ResourcePressure = "low"
	r.Summary.NamespacesWithIssues = []string{"default"}
	r.Issues = []struct {
		Namespace    string `json:"namespace"`
		Name         string `json:"name"`
		IssueType    string `json:"issue_type"`
		Severity     string `json:"severity"`
		ShortSummary string `json:"short_summary"`
	}{
		{
			Namespace:    "default",
			Name:         "api",
			IssueType:    "OOM",
			Severity:     "high",
			ShortSummary: "memory issue",
		},
	}
	r.Recommendations = []string{"increase memory"}

	RenderDefaultHuman(&buf, r)
	out := buf.String()
	assert.Contains(t, out, "CLUSTER SUMMARY")
	assert.Contains(t, out, "Problem pods")
	assert.Contains(t, out, "api")
	assert.Contains(t, out, "increase memory")
}
