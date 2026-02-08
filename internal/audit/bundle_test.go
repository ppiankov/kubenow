package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundleDirName(t *testing.T) {
	ts := time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC)
	workload := BundleWorkload{
		Kind:      "Deployment",
		Name:      "api-server",
		Namespace: "production",
	}

	name := bundleDirName(ts, workload)
	assert.Equal(t, "20250615T143000Z__production__deployment__api-server", name)
}

func TestCreateBundle_Success(t *testing.T) {
	auditPath := t.TempDir()
	ts := time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC)

	cfg := BundleConfig{
		AuditPath: auditPath,
		Timestamp: ts,
		Workload: BundleWorkload{
			Kind:      "Deployment",
			Name:      "api",
			Namespace: "default",
			UID:       "uid-123",
		},
		BeforeObject: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":              "api",
				"namespace":         "default",
				"resourceVersion":   "12345",
				"generation":        float64(3),
				"managedFields":     []interface{}{"field1"},
				"uid":               "uid-123",
				"creationTimestamp": "2025-01-01T00:00:00Z",
			},
			"spec": map[string]interface{}{
				"replicas": float64(3),
			},
			"status": map[string]interface{}{
				"availableReplicas": float64(3),
			},
		},
		Recommendation: BundleRecommendation{
			Safety:     "SAFE",
			Confidence: "HIGH",
		},
		Identity: &Identity{
			KubeUser:           "admin",
			OSUser:             "testuser",
			Machine:            "testhost",
			IdentitySource:     "ssr",
			IdentityConfidence: "verified",
		},
		Version: "0.2.0",
		Changes: []BundleChange{
			{Field: "cpu_request", Before: "100m", After: "150m", DeltaPercent: 50},
		},
	}

	bundle, err := CreateBundle(cfg)
	require.NoError(t, err)
	require.NotNil(t, bundle)

	// Verify directory exists
	info, err := os.Stat(bundle.Dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Verify before.yaml exists and volatile fields are stripped
	beforeData, err := os.ReadFile(filepath.Join(bundle.Dir, "before.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(beforeData), "resourceVersion")
	assert.NotContains(t, string(beforeData), "managedFields")
	assert.NotContains(t, string(beforeData), "status")
	assert.Contains(t, string(beforeData), "replicas")

	// Verify decision.json exists with pending status
	decisionData, err := os.ReadFile(bundle.DecisionPath)
	require.NoError(t, err)

	var decision DecisionJSON
	require.NoError(t, json.Unmarshal(decisionData, &decision))
	assert.Equal(t, "pending", decision.Status)
	assert.Equal(t, "SAFE", decision.Recommendation.Safety)
	assert.Equal(t, "admin", decision.Identity.KubeUser)
	assert.Equal(t, "0.2.0", decision.Version)
}

func TestCreateBundle_NonWritablePath(t *testing.T) {
	cfg := BundleConfig{
		AuditPath: "/nonexistent/path/that/cannot/be/created",
		Timestamp: time.Now(),
		Workload: BundleWorkload{
			Kind: "Deployment", Name: "api", Namespace: "default",
		},
	}

	bundle, err := CreateBundle(cfg)
	assert.Error(t, err)
	assert.Nil(t, bundle)
}

func TestFinalizeBundle_Success(t *testing.T) {
	auditPath := t.TempDir()
	ts := time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC)

	cfg := BundleConfig{
		AuditPath: auditPath,
		Timestamp: ts,
		Workload: BundleWorkload{
			Kind: "Deployment", Name: "api", Namespace: "default",
		},
		BeforeObject: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata":   map[string]interface{}{"name": "api", "namespace": "default"},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name": "api",
								"resources": map[string]interface{}{
									"requests": map[string]interface{}{"cpu": "100m", "memory": "128Mi"},
									"limits":   map[string]interface{}{"cpu": "500m", "memory": "512Mi"},
								},
							},
						},
					},
				},
			},
		},
		Recommendation: BundleRecommendation{Safety: "SAFE", Confidence: "HIGH"},
		Version:        "0.2.0",
	}

	bundle, err := CreateBundle(cfg)
	require.NoError(t, err)

	// After object has different resources
	afterObj := map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]interface{}{"name": "api", "namespace": "default"},
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name": "api",
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{"cpu": "150m", "memory": "200Mi"},
								"limits":   map[string]interface{}{"cpu": "600m", "memory": "600Mi"},
							},
						},
					},
				},
			},
		},
	}

	appliedAt := time.Date(2025, 6, 15, 14, 30, 5, 0, time.UTC)
	err = FinalizeBundle(bundle, afterObj, "applied", appliedAt, nil)
	require.NoError(t, err)

	// Verify after.yaml exists
	_, err = os.Stat(filepath.Join(bundle.Dir, "after.yaml"))
	require.NoError(t, err)

	// Verify diff.patch exists and has content
	diffData, err := os.ReadFile(filepath.Join(bundle.Dir, "diff.patch"))
	require.NoError(t, err)
	assert.Contains(t, string(diffData), "---")

	// Verify decision.json updated to "applied"
	decisionData, err := os.ReadFile(bundle.DecisionPath)
	require.NoError(t, err)

	var decision DecisionJSON
	require.NoError(t, json.Unmarshal(decisionData, &decision))
	assert.Equal(t, "applied", decision.Status)
	assert.NotEmpty(t, decision.AppliedAt)
	assert.Empty(t, decision.Error)
}

func TestFinalizeBundle_Failed(t *testing.T) {
	auditPath := t.TempDir()
	ts := time.Now()

	cfg := BundleConfig{
		AuditPath: auditPath,
		Timestamp: ts,
		Workload: BundleWorkload{
			Kind: "Deployment", Name: "api", Namespace: "default",
		},
		BeforeObject: map[string]interface{}{
			"apiVersion": "apps/v1",
			"metadata":   map[string]interface{}{"name": "api"},
			"spec":       map[string]interface{}{},
		},
		Version: "0.2.0",
	}

	bundle, err := CreateBundle(cfg)
	require.NoError(t, err)

	afterObj := map[string]interface{}{
		"apiVersion": "apps/v1",
		"metadata":   map[string]interface{}{"name": "api"},
		"spec":       map[string]interface{}{},
	}

	applyErr := assert.AnError
	err = FinalizeBundle(bundle, afterObj, "failed", time.Now(), applyErr)
	require.NoError(t, err)

	decisionData, err := os.ReadFile(bundle.DecisionPath)
	require.NoError(t, err)

	var decision DecisionJSON
	require.NoError(t, json.Unmarshal(decisionData, &decision))
	assert.Equal(t, "failed", decision.Status)
	assert.NotEmpty(t, decision.Error)
}

func TestBuildDecisionJSON_AllFields(t *testing.T) {
	ts := time.Date(2025, 6, 15, 14, 30, 0, 0, time.UTC)

	cfg := BundleConfig{
		AuditPath: "/tmp/audit",
		Timestamp: ts,
		Workload: BundleWorkload{
			Kind: "Deployment", Name: "api", Namespace: "prod", UID: "uid-abc",
		},
		Recommendation: BundleRecommendation{
			Safety:     "SAFE",
			Confidence: "HIGH",
			Evidence: &BundleEvidence{
				CPU:            BundlePercentiles{P50: 0.1, P95: 0.3, P99: 0.5, Max: 0.8, Avg: 0.2, SpikeCount: 2},
				Memory:         BundlePercentiles{P50: 100, P95: 200, P99: 300, Max: 400, Avg: 150},
				Signals:        BundleSignals{OOMKills: 0, Restarts: 1, Evictions: 0, ThrottlingDetected: false},
				SafetyMargin:   1.0,
				Duration:       2 * time.Hour,
				SampleCount:    720,
				SampleInterval: 5 * time.Second,
			},
		},
		Latch: BundleLatch{
			Duration:       2 * time.Hour,
			SampleCount:    720,
			SampleInterval: 5 * time.Second,
		},
		Identity: &Identity{
			KubeContext:        "prod-ctx",
			KubeUser:           "deploy-bot",
			OSUser:             "ci",
			Machine:            "runner-01",
			IdentitySource:     "ssr",
			IdentityConfidence: "verified",
		},
		ClusterServer:    "https://k8s.prod.example.com",
		Version:          "0.2.0",
		GuardrailsPassed: []string{"safety_rating", "namespace_allowed", "delta_bounds"},
		Changes: []BundleChange{
			{Field: "cpu_request", Before: "100m", After: "150m", DeltaPercent: 50},
			{Field: "memory_request", Before: "128Mi", After: "200Mi", DeltaPercent: 56.25},
		},
	}

	decision := buildDecisionJSON(cfg, "pending")

	assert.Equal(t, "0.2.0", decision.Version)
	assert.Equal(t, "pending", decision.Status)
	assert.Equal(t, "Deployment", decision.Workload.Kind)
	assert.Equal(t, "api", decision.Workload.Name)
	assert.Equal(t, "prod", decision.Workload.Namespace)
	assert.Equal(t, "uid-abc", decision.Workload.UID)
	assert.Equal(t, "https://k8s.prod.example.com", decision.Cluster)
	assert.Equal(t, "SAFE", decision.Recommendation.Safety)
	assert.Equal(t, "HIGH", decision.Recommendation.Confidence)
	assert.NotNil(t, decision.Recommendation.Evidence)
	assert.Equal(t, "deploy-bot", decision.Identity.KubeUser)
	assert.Equal(t, "2h0m0s", decision.Latch.Duration)
	assert.Equal(t, 720, decision.Latch.SampleCount)
	assert.Len(t, decision.Guardrails, 3)
	assert.Len(t, decision.Changes, 2)

	// Verify it serializes to valid JSON
	data, err := json.MarshalIndent(decision, "", "  ")
	require.NoError(t, err)
	assert.Contains(t, string(data), "cpu_request")
}
