package promonitor

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/ppiankov/kubenow/internal/metrics"
)

func testRecommendation() *AlignmentRecommendation {
	return &AlignmentRecommendation{
		Workload:   WorkloadRef{Kind: "Deployment", Name: "payment-api", Namespace: "default"},
		Timestamp:  time.Date(2026, 2, 7, 14, 22, 1, 0, time.UTC),
		Confidence: ConfidenceMedium,
		Safety:     SafetyRatingCaution,
		Containers: []ContainerAlignment{
			{
				Name: "payment-api",
				Current: ResourceValues{
					CPURequest: 0.1, CPULimit: 0.5,
					MemoryRequest: 128 * 1024 * 1024, MemoryLimit: 512 * 1024 * 1024,
				},
				Recommended: ResourceValues{
					CPURequest: 0.18, CPULimit: 1.0,
					MemoryRequest: 290 * 1024 * 1024, MemoryLimit: 1024 * 1024 * 1024,
				},
				Delta: ResourceDelta{
					CPURequestPercent: 80, CPULimitPercent: 100,
					MemoryRequestPercent: 126, MemoryLimitPercent: 100,
				},
			},
		},
		Evidence: &LatchEvidence{
			Duration:       15 * time.Minute,
			SampleCount:    180,
			SampleInterval: 5 * time.Second,
			Gaps:           0,
			Valid:          true,
			CPU:            &metrics.Percentiles{P50: 0.08, P95: 0.12, P99: 0.15, Max: 0.2, Avg: 0.07},
			Memory:         &metrics.Percentiles{P50: 100e6, P95: 170e6, P99: 200e6, Max: 220e6, Avg: 90e6},
		},
		Policy: &PolicyResult{ExportPermitted: true},
	}
}

// --- Patch format ---

func TestExportPatch_ValidYAML(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatPatch, nil)
	require.NoError(t, err)

	// Should have evidence comments
	assert.Contains(t, output, "# kubenow alignment patch")
	assert.Contains(t, output, "# Generated:")
	assert.Contains(t, output, "# Workload: default/deployment/payment-api")
	assert.Contains(t, output, "# Confidence: MEDIUM  Safety: CAUTION")
	assert.Contains(t, output, "# Latch: 15m0s (180 samples)")
	assert.Contains(t, output, "kubectl apply --server-side")

	// Strip comments and parse YAML
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	yamlContent := strings.Join(lines, "\n")

	var doc map[string]interface{}
	err = yaml.Unmarshal([]byte(yamlContent), &doc)
	require.NoError(t, err)

	assert.Equal(t, "apps/v1", doc["apiVersion"])
	assert.Equal(t, "Deployment", doc["kind"])

	metadata := doc["metadata"].(map[string]interface{})
	assert.Equal(t, "payment-api", metadata["name"])
	assert.Equal(t, "default", metadata["namespace"])
}

func TestExportPatch_ResourceValues(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatPatch, nil)
	require.NoError(t, err)

	assert.Contains(t, output, "cpu: 180m")
	assert.Contains(t, output, "memory: 290Mi")
	assert.Contains(t, output, "cpu: \"1\"")  // 1.0 cores → "1" (yaml quotes integers-looking strings)
	assert.Contains(t, output, "memory: 1Gi") // 1024Mi → 1Gi
}

func TestExportPatch_MultiContainer(t *testing.T) {
	rec := testRecommendation()
	rec.Containers = append(rec.Containers, ContainerAlignment{
		Name: "sidecar",
		Recommended: ResourceValues{
			CPURequest: 0.05, CPULimit: 0.2,
			MemoryRequest: 64 * 1024 * 1024, MemoryLimit: 128 * 1024 * 1024,
		},
	})

	output, err := Export(rec, FormatPatch, nil)
	require.NoError(t, err)

	assert.Contains(t, output, "name: payment-api")
	assert.Contains(t, output, "name: sidecar")
	assert.Contains(t, output, "cpu: 50m")     // sidecar CPU
	assert.Contains(t, output, "memory: 64Mi") // sidecar memory
}

func TestExportPatch_HPAComment(t *testing.T) {
	rec := testRecommendation()
	rec.Policy = &PolicyResult{HPADetected: true, HPAName: "api-hpa"}

	output, err := Export(rec, FormatPatch, nil)
	require.NoError(t, err)

	assert.Contains(t, output, "# WARNING: HPA")
	assert.Contains(t, output, "api-hpa")
}

// --- Diff format ---

func TestExportDiff(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatDiff, nil)
	require.NoError(t, err)

	assert.Contains(t, output, "--- deployment/payment-api (current)")
	assert.Contains(t, output, "+++ deployment/payment-api (recommended)")
	assert.Contains(t, output, "Container: payment-api")

	// Changed values have +/- markers
	assert.Contains(t, output, "-     cpu: 100m")
	assert.Contains(t, output, "+     cpu: 180m")
	assert.Contains(t, output, "-     memory: 128Mi")
	assert.Contains(t, output, "+     memory: 290Mi")
}

func TestExportDiff_UnchangedValues(t *testing.T) {
	rec := testRecommendation()
	// Make CPU limit unchanged
	rec.Containers[0].Current.CPULimit = 1.0
	rec.Containers[0].Recommended.CPULimit = 1.0

	output, err := Export(rec, FormatDiff, nil)
	require.NoError(t, err)

	// Unchanged CPU limit should not have +/- markers
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if strings.Contains(line, "limits:") {
			continue
		}
	}
	// The line with cpu: 1 in limits should be without +/-
	// (checking the limits section has unchanged format)
	assert.Contains(t, output, "      cpu: 1\n")
}

// --- JSON format ---

func TestExportJSON_Valid(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatJSON, nil)
	require.NoError(t, err)

	var parsed AlignmentRecommendation
	err = json.Unmarshal([]byte(output), &parsed)
	require.NoError(t, err)

	assert.Equal(t, "payment-api", parsed.Workload.Name)
	assert.Equal(t, ConfidenceMedium, parsed.Confidence)
	assert.Equal(t, SafetyRatingCaution, parsed.Safety)
	assert.Len(t, parsed.Containers, 1)
	assert.Equal(t, "payment-api", parsed.Containers[0].Name)
}

func TestExportJSON_EvidenceIncluded(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatJSON, nil)
	require.NoError(t, err)

	assert.Contains(t, output, "latch_evidence")
	assert.Contains(t, output, "sample_count")
}

// --- Manifest format ---

func TestExportManifest_RequiresJSON(t *testing.T) {
	rec := testRecommendation()
	_, err := Export(rec, FormatManifest, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "requires the current workload object")
}

func TestExportManifest_ValidOutput(t *testing.T) {
	rec := testRecommendation()

	// Create a minimal K8s object JSON
	obj := map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]interface{}{
			"name":              "payment-api",
			"namespace":         "default",
			"resourceVersion":   "12345",
			"generation":        3,
			"uid":               "abc-123",
			"creationTimestamp": "2026-01-01T00:00:00Z",
			"managedFields":     []interface{}{},
		},
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []interface{}{
						map[string]interface{}{
							"name": "payment-api",
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{"cpu": "100m", "memory": "128Mi"},
								"limits":   map[string]interface{}{"cpu": "500m", "memory": "512Mi"},
							},
						},
					},
				},
			},
		},
		"status": map[string]interface{}{"replicas": 3},
	}
	objJSON, _ := json.Marshal(obj)

	output, err := Export(rec, FormatManifest, objJSON)
	require.NoError(t, err)

	// Evidence comments present
	assert.Contains(t, output, "# kubenow alignment patch")

	// Volatile fields stripped
	assert.NotContains(t, output, "resourceVersion")
	assert.NotContains(t, output, "managedFields")
	assert.NotContains(t, output, "uid")
	assert.NotContains(t, output, "creationTimestamp")
	assert.NotContains(t, output, "status")

	// Resources updated
	assert.Contains(t, output, "180m")  // new CPU request
	assert.Contains(t, output, "290Mi") // new memory request
}

// --- Format helpers ---

func TestFormatCPUResource(t *testing.T) {
	tests := []struct {
		cores float64
		want  string
	}{
		{0, "0m"},
		{0.001, "1m"},
		{0.05, "50m"},
		{0.1, "100m"},
		{0.5, "500m"},
		{1.0, "1"},
		{2.0, "2"},
		{2.5, "2500m"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, formatCPUResource(tt.cores))
		})
	}
}

func TestFormatMemResource(t *testing.T) {
	tests := []struct {
		bytes float64
		want  string
	}{
		{0, "0"},
		{1024, "1024"},                       // less than 1Mi → raw bytes
		{64 * 1024 * 1024, "64Mi"},           // 64Mi
		{128 * 1024 * 1024, "128Mi"},         // 128Mi
		{512 * 1024 * 1024, "512Mi"},         // 512Mi
		{1024 * 1024 * 1024, "1Gi"},          // 1Gi
		{2 * 1024 * 1024 * 1024, "2Gi"},      // 2Gi
		{1.5 * 1024 * 1024 * 1024, "1536Mi"}, // 1.5Gi → Mi
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, formatMemResource(tt.bytes))
		})
	}
}

// --- Error cases ---

func TestExport_NilRecommendation(t *testing.T) {
	_, err := Export(nil, FormatPatch, nil)
	assert.Error(t, err)
}

func TestExport_UnsupportedFormat(t *testing.T) {
	rec := testRecommendation()
	_, err := Export(rec, "csv", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestExport_EmptyContainers(t *testing.T) {
	rec := testRecommendation()
	rec.Containers = nil

	// Patch with no containers should still produce valid YAML
	output, err := Export(rec, FormatPatch, nil)
	require.NoError(t, err)
	assert.Contains(t, output, "# kubenow alignment patch")
}

// --- Volatile field stripping ---

// --- Kustomize format ---

func TestExportKustomize_ValidYAML(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatKustomize, nil)
	require.NoError(t, err)

	// Split on --- separator
	parts := strings.SplitN(output, "---\n", 2)
	require.Len(t, parts, 2, "expected two YAML documents separated by ---")

	// First doc (kustomization.yaml) should parse
	var kustomization map[string]interface{}
	// Strip comment lines before parsing
	var kLines []string
	for _, line := range strings.Split(parts[0], "\n") {
		if !strings.HasPrefix(line, "#") {
			kLines = append(kLines, line)
		}
	}
	err = yaml.Unmarshal([]byte(strings.Join(kLines, "\n")), &kustomization)
	require.NoError(t, err)

	// Second doc (patch) should parse
	var patch map[string]interface{}
	var pLines []string
	for _, line := range strings.Split(parts[1], "\n") {
		if !strings.HasPrefix(line, "#") {
			pLines = append(pLines, line)
		}
	}
	err = yaml.Unmarshal([]byte(strings.Join(pLines, "\n")), &patch)
	require.NoError(t, err)
}

func TestExportKustomize_HasKustomizationHeader(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatKustomize, nil)
	require.NoError(t, err)

	parts := strings.SplitN(output, "---\n", 2)
	require.Len(t, parts, 2)

	assert.Contains(t, parts[0], "apiVersion: kustomize.config.k8s.io/v1beta1")
	assert.Contains(t, parts[0], "kind: Kustomization")
}

func TestExportKustomize_PatchesReference(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatKustomize, nil)
	require.NoError(t, err)

	// Patch filename should follow convention
	expectedFilename := "deployment-payment-api-resources.yaml"
	assert.Contains(t, output, expectedFilename)
}

func TestExportKustomize_PatchTarget(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatKustomize, nil)
	require.NoError(t, err)

	parts := strings.SplitN(output, "---\n", 2)
	require.Len(t, parts, 2)

	// Parse kustomization doc (strip comments)
	var kLines []string
	for _, line := range strings.Split(parts[0], "\n") {
		if !strings.HasPrefix(line, "#") {
			kLines = append(kLines, line)
		}
	}
	var kustomization struct {
		Patches []struct {
			Path   string `yaml:"path"`
			Target struct {
				Kind      string `yaml:"kind"`
				Name      string `yaml:"name"`
				Namespace string `yaml:"namespace"`
			} `yaml:"target"`
		} `yaml:"patches"`
	}
	err = yaml.Unmarshal([]byte(strings.Join(kLines, "\n")), &kustomization)
	require.NoError(t, err)

	require.Len(t, kustomization.Patches, 1)
	assert.Equal(t, "Deployment", kustomization.Patches[0].Target.Kind)
	assert.Equal(t, "payment-api", kustomization.Patches[0].Target.Name)
	assert.Equal(t, "default", kustomization.Patches[0].Target.Namespace)
}

func TestExportKustomize_PatchContent(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatKustomize, nil)
	require.NoError(t, err)

	parts := strings.SplitN(output, "---\n", 2)
	require.Len(t, parts, 2)

	// Patch section should contain recommended values
	assert.Contains(t, parts[1], "cpu: 180m")
	assert.Contains(t, parts[1], "memory: 290Mi")
	assert.Contains(t, parts[1], "name: payment-api")
}

func TestExportKustomize_EvidenceComments(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatKustomize, nil)
	require.NoError(t, err)

	assert.Contains(t, output, "# kubenow alignment patch")
	assert.Contains(t, output, "# Workload: default/deployment/payment-api")
}

func TestSplitKustomizeOutput(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatKustomize, nil)
	require.NoError(t, err)

	kustomization, patch, filename := SplitKustomizeOutput(output, rec.Workload)

	assert.Contains(t, kustomization, "kustomize.config.k8s.io")
	assert.Contains(t, patch, "cpu: 180m")
	assert.Equal(t, "deployment-payment-api-resources.yaml", filename)
}

// --- Helm format ---

func TestExportHelm_SingleContainer(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatHelm, nil)
	require.NoError(t, err)

	// Single container uses flat resources: block
	assert.Contains(t, output, "resources:")
	assert.NotContains(t, output, "containers:")
}

func TestExportHelm_MultiContainer(t *testing.T) {
	rec := testRecommendation()
	rec.Containers = append(rec.Containers, ContainerAlignment{
		Name: "sidecar",
		Recommended: ResourceValues{
			CPURequest: 0.05, CPULimit: 0.2,
			MemoryRequest: 64 * 1024 * 1024, MemoryLimit: 128 * 1024 * 1024,
		},
	})

	output, err := Export(rec, FormatHelm, nil)
	require.NoError(t, err)

	// Multi-container uses containers: keyed by name
	assert.Contains(t, output, "containers:")
	assert.Contains(t, output, "payment-api:")
	assert.Contains(t, output, "sidecar:")
}

func TestExportHelm_ValidYAML(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatHelm, nil)
	require.NoError(t, err)

	// Strip comments and parse
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	var parsed map[string]interface{}
	err = yaml.Unmarshal([]byte(strings.Join(lines, "\n")), &parsed)
	require.NoError(t, err)
	assert.Contains(t, parsed, "resources")
}

func TestExportHelm_ContainsRecommendedValues(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatHelm, nil)
	require.NoError(t, err)

	assert.Contains(t, output, "cpu: 180m")
	assert.Contains(t, output, "memory: 290Mi")
	assert.Contains(t, output, "memory: 1Gi")
}

func TestExportHelm_Comments(t *testing.T) {
	rec := testRecommendation()
	output, err := Export(rec, FormatHelm, nil)
	require.NoError(t, err)

	assert.Contains(t, output, "# kubenow helm values override")
	assert.Contains(t, output, "# Workload: default/deployment/payment-api")
	assert.Contains(t, output, "chart's values.yaml")
}

// --- Volatile field stripping ---

func TestStripVolatileFields(t *testing.T) {
	obj := map[string]interface{}{
		"metadata": map[string]interface{}{
			"name":              "test",
			"namespace":         "default",
			"resourceVersion":   "12345",
			"generation":        3,
			"uid":               "abc",
			"creationTimestamp": "2026-01-01T00:00:00Z",
			"managedFields":     []interface{}{},
			"labels":            map[string]interface{}{"app": "test"},
		},
		"spec":   map[string]interface{}{"replicas": 3},
		"status": map[string]interface{}{"ready": true},
	}

	stripVolatileFields(obj)

	metadata := obj["metadata"].(map[string]interface{})
	assert.Equal(t, "test", metadata["name"])
	assert.Equal(t, "default", metadata["namespace"])
	assert.Equal(t, map[string]interface{}{"app": "test"}, metadata["labels"])

	// Volatile fields removed
	assert.NotContains(t, metadata, "resourceVersion")
	assert.NotContains(t, metadata, "generation")
	assert.NotContains(t, metadata, "uid")
	assert.NotContains(t, metadata, "creationTimestamp")
	assert.NotContains(t, metadata, "managedFields")

	// Status removed
	assert.NotContains(t, obj, "status")

	// Spec preserved
	assert.Contains(t, obj, "spec")
}
