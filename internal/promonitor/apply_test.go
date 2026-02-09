package promonitor

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mockKubeApplier implements KubeApplier for testing.
type mockKubeApplier struct {
	patchErr       error
	patchCalled    bool
	patchJSON      []byte
	containers     []ContainerResources
	containersErr  error
	managedFields  []metav1.ManagedFieldsEntry
	managedErr     error
	workloadObject map[string]interface{}
	workloadErr    error
}

func (m *mockKubeApplier) PatchWorkload(_ context.Context, _ WorkloadRef, patchJSON []byte, _ string) error {
	m.patchCalled = true
	m.patchJSON = patchJSON
	return m.patchErr
}

func (m *mockKubeApplier) GetContainerResources(_ context.Context, _ WorkloadRef) ([]ContainerResources, error) {
	return m.containers, m.containersErr
}

func (m *mockKubeApplier) GetManagedFields(_ context.Context, _ WorkloadRef) ([]metav1.ManagedFieldsEntry, error) {
	return m.managedFields, m.managedErr
}

func (m *mockKubeApplier) GetWorkloadObject(_ context.Context, _ WorkloadRef) (map[string]interface{}, error) {
	return m.workloadObject, m.workloadErr
}

func validApplyInput() *ApplyInput {
	return &ApplyInput{
		Recommendation: &AlignmentRecommendation{
			Workload:   WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"},
			Safety:     SafetyRatingSafe,
			Confidence: ConfidenceHigh,
			Containers: []ContainerAlignment{
				{
					Name: "api",
					Current: ResourceValues{
						CPURequest: 0.1, CPULimit: 0.5,
						MemoryRequest: 128 * 1024 * 1024, MemoryLimit: 512 * 1024 * 1024,
					},
					Recommended: ResourceValues{
						CPURequest: 0.15, CPULimit: 0.6,
						MemoryRequest: 200 * 1024 * 1024, MemoryLimit: 600 * 1024 * 1024,
					},
				},
			},
			Policy: &PolicyResult{ApplyPermitted: true, ExportPermitted: true},
		},
		Workload:         WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"},
		Mode:             ModeApplyReady,
		Policy:           &PolicyBounds{MinSafetyRating: SafetyRatingSafe},
		LatchTimestamp:   time.Now(),
		LatchDuration:    2 * time.Hour,
		AuditWritable:    true,
		IdentityRecorded: true,
		RateLimitOK:      true,
	}
}

// --- CheckActionable tests ---

func TestCheckActionable_AllPass(t *testing.T) {
	input := validApplyInput()
	reasons := CheckActionable(input)
	assert.Empty(t, reasons)
}

func TestCheckActionable_WrongMode(t *testing.T) {
	input := validApplyInput()
	input.Mode = ModeExportOnly
	reasons := CheckActionable(input)
	assert.Contains(t, reasons[0], "not apply-ready")
}

func TestCheckActionable_NilRecommendation(t *testing.T) {
	input := validApplyInput()
	input.Recommendation = nil
	reasons := CheckActionable(input)
	found := false
	for _, r := range reasons {
		if r == "no recommendation available" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestCheckActionable_NilPolicy(t *testing.T) {
	input := validApplyInput()
	input.Policy = nil
	reasons := CheckActionable(input)
	found := false
	for _, r := range reasons {
		if r == "no policy loaded" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestCheckActionable_SafetyBelowMinimum(t *testing.T) {
	input := validApplyInput()
	input.Recommendation.Safety = SafetyRatingRisky
	input.Policy.MinSafetyRating = SafetyRatingSafe
	reasons := CheckActionable(input)
	found := false
	for _, r := range reasons {
		if assert.ObjectsAreEqual("safety rating RISKY below policy minimum SAFE", r) {
			found = true
		}
	}
	assert.True(t, found)
}

func TestCheckActionable_HPANotAcknowledged(t *testing.T) {
	input := validApplyInput()
	input.HPAInfo = &HPAInfo{Name: "api-hpa", MinReplica: 2, MaxReplica: 10}
	input.HPAAcknowledged = false
	reasons := CheckActionable(input)
	found := false
	for _, r := range reasons {
		if r == `HPA "api-hpa" detected — pass --acknowledge-hpa to proceed` {
			found = true
		}
	}
	assert.True(t, found)
}

func TestCheckActionable_HPAAcknowledged(t *testing.T) {
	input := validApplyInput()
	input.HPAInfo = &HPAInfo{Name: "api-hpa", MinReplica: 2, MaxReplica: 10}
	input.HPAAcknowledged = true
	reasons := CheckActionable(input)
	assert.Empty(t, reasons)
}

func TestCheckActionable_StaleLatch(t *testing.T) {
	input := validApplyInput()
	input.LatchTimestamp = time.Now().Add(-8 * 24 * time.Hour) // 8 days ago
	reasons := CheckActionable(input)
	found := false
	for _, r := range reasons {
		if r == "latch data is stale (exceeds max_latch_age)" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestCheckActionable_ShortLatch(t *testing.T) {
	input := validApplyInput()
	input.LatchDuration = 5 * time.Minute
	input.Policy.MinLatchDuration = time.Hour
	reasons := CheckActionable(input)
	found := false
	for _, r := range reasons {
		if r == "latch duration 5m0s below policy minimum 1h0m0s" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestCheckActionable_AuditNotWritable(t *testing.T) {
	input := validApplyInput()
	input.AuditWritable = false
	reasons := CheckActionable(input)
	found := false
	for _, r := range reasons {
		if r == "audit path is not writable" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestCheckActionable_IdentityNotRecorded(t *testing.T) {
	input := validApplyInput()
	input.IdentityRecorded = false
	reasons := CheckActionable(input)
	found := false
	for _, r := range reasons {
		if r == "identity not recorded (required for audit)" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestCheckActionable_RateLimitExceeded(t *testing.T) {
	input := validApplyInput()
	input.RateLimitOK = false
	reasons := CheckActionable(input)
	found := false
	for _, r := range reasons {
		if r == "rate limit exceeded" {
			found = true
		}
	}
	assert.True(t, found)
}

// --- ExecuteApply tests ---

func TestExecuteApply_Success(t *testing.T) {
	mock := &mockKubeApplier{
		containers: []ContainerResources{
			{
				Name:          "api",
				CPURequest:    0.15,
				CPULimit:      0.6,
				MemoryRequest: 200 * 1024 * 1024,
				MemoryLimit:   600 * 1024 * 1024,
			},
		},
	}

	input := validApplyInput()
	result := ExecuteApply(context.Background(), mock, input)

	assert.True(t, result.Applied)
	assert.True(t, mock.patchCalled)
	assert.Nil(t, result.Error)
	assert.Empty(t, result.DenialReasons)
	assert.Empty(t, result.Drifts)
}

func TestExecuteApply_SuccessWithDrift(t *testing.T) {
	// Admitted values differ from requested (webhook mutated them)
	mock := &mockKubeApplier{
		containers: []ContainerResources{
			{
				Name:          "api",
				CPURequest:    0.2, // drifted from 0.15
				CPULimit:      0.6,
				MemoryRequest: 200 * 1024 * 1024,
				MemoryLimit:   600 * 1024 * 1024,
			},
		},
	}

	input := validApplyInput()
	result := ExecuteApply(context.Background(), mock, input)

	assert.True(t, result.Applied)
	assert.Nil(t, result.Error)
	require.Len(t, result.Drifts, 1)
	assert.Equal(t, "api", result.Drifts[0].Container)
	assert.Equal(t, "cpu_request", result.Drifts[0].Field)
	assert.Equal(t, "150m", result.Drifts[0].Requested)
	assert.Equal(t, "200m", result.Drifts[0].Admitted)
}

func TestExecuteApply_ConflictGitOps(t *testing.T) {
	mock := &mockKubeApplier{
		patchErr: fmt.Errorf("conflict: Apply failed with 1 conflict: conflict with \"argocd\""),
		managedFields: []metav1.ManagedFieldsEntry{
			{Manager: "argocd", Operation: metav1.ManagedFieldsOperationApply},
		},
	}

	input := validApplyInput()
	result := ExecuteApply(context.Background(), mock, input)

	assert.False(t, result.Applied)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "ssa conflict")
	assert.Equal(t, "argocd", result.ConflictManager)
	assert.True(t, result.GitOpsConflict)
}

func TestExecuteApply_ConflictUnknownManager(t *testing.T) {
	mock := &mockKubeApplier{
		patchErr: fmt.Errorf("conflict: Apply failed with 1 conflict"),
		managedFields: []metav1.ManagedFieldsEntry{
			{Manager: "some-controller", Operation: metav1.ManagedFieldsOperationApply},
		},
	}

	input := validApplyInput()
	result := ExecuteApply(context.Background(), mock, input)

	assert.False(t, result.Applied)
	assert.Equal(t, "some-controller", result.ConflictManager)
	assert.False(t, result.GitOpsConflict)
}

func TestExecuteApply_WebhookRejection(t *testing.T) {
	mock := &mockKubeApplier{
		patchErr: fmt.Errorf("admission webhook denied the request: resource quota exceeded"),
	}

	input := validApplyInput()
	result := ExecuteApply(context.Background(), mock, input)

	assert.False(t, result.Applied)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "admission webhook")
	assert.Empty(t, result.ConflictManager) // not a conflict
}

func TestExecuteApply_DenialShortCircuits(t *testing.T) {
	mock := &mockKubeApplier{}

	input := validApplyInput()
	input.Mode = ModeExportOnly // will cause denial

	result := ExecuteApply(context.Background(), mock, input)

	assert.False(t, result.Applied)
	assert.NotEmpty(t, result.DenialReasons)
	assert.False(t, mock.patchCalled) // patch should never be called
}

// --- buildSSAPatchJSON tests ---

func TestBuildSSAPatchJSON_SingleContainer(t *testing.T) {
	rec := &AlignmentRecommendation{
		Workload: WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"},
		Containers: []ContainerAlignment{
			{
				Name: "api",
				Recommended: ResourceValues{
					CPURequest: 0.15, CPULimit: 0.6,
					MemoryRequest: 200 * 1024 * 1024, MemoryLimit: 600 * 1024 * 1024,
				},
			},
		},
	}

	data, err := buildSSAPatchJSON(rec)
	require.NoError(t, err)

	var doc map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &doc))

	assert.Equal(t, "apps/v1", doc["apiVersion"])
	assert.Equal(t, "Deployment", doc["kind"])

	metadata := doc["metadata"].(map[string]interface{})
	assert.Equal(t, "api", metadata["name"])
	assert.Equal(t, "default", metadata["namespace"])

	spec := doc["spec"].(map[string]interface{})
	template := spec["template"].(map[string]interface{})
	podSpec := template["spec"].(map[string]interface{})
	containers := podSpec["containers"].([]interface{})
	require.Len(t, containers, 1)

	container := containers[0].(map[string]interface{})
	assert.Equal(t, "api", container["name"])

	resources := container["resources"].(map[string]interface{})
	requests := resources["requests"].(map[string]interface{})
	assert.Equal(t, "150m", requests["cpu"])
	assert.Equal(t, "200Mi", requests["memory"])

	limits := resources["limits"].(map[string]interface{})
	assert.Equal(t, "600m", limits["cpu"])
	assert.Equal(t, "600Mi", limits["memory"])
}

func TestBuildSSAPatchJSON_MultiContainer(t *testing.T) {
	rec := &AlignmentRecommendation{
		Workload: WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"},
		Containers: []ContainerAlignment{
			{
				Name: "api",
				Recommended: ResourceValues{
					CPURequest: 0.15, CPULimit: 0.6,
					MemoryRequest: 200 * 1024 * 1024, MemoryLimit: 600 * 1024 * 1024,
				},
			},
			{
				Name: "sidecar",
				Recommended: ResourceValues{
					CPURequest: 0.05, CPULimit: 0.2,
					MemoryRequest: 64 * 1024 * 1024, MemoryLimit: 128 * 1024 * 1024,
				},
			},
		},
	}

	data, err := buildSSAPatchJSON(rec)
	require.NoError(t, err)

	var doc map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &doc))

	spec := doc["spec"].(map[string]interface{})
	template := spec["template"].(map[string]interface{})
	podSpec := template["spec"].(map[string]interface{})
	containers := podSpec["containers"].([]interface{})
	require.Len(t, containers, 2)

	c1 := containers[0].(map[string]interface{})
	assert.Equal(t, "api", c1["name"])

	c2 := containers[1].(map[string]interface{})
	assert.Equal(t, "sidecar", c2["name"])
	resources := c2["resources"].(map[string]interface{})
	requests := resources["requests"].(map[string]interface{})
	assert.Equal(t, "50m", requests["cpu"])
	assert.Equal(t, "64Mi", requests["memory"])
}

// --- detectConflictManager tests ---

func TestDetectConflictManager_ArgoCD(t *testing.T) {
	mock := &mockKubeApplier{
		managedFields: []metav1.ManagedFieldsEntry{
			{Manager: "kubectl-client-side-apply"},
			{Manager: "argocd"},
		},
	}

	manager := detectConflictManager(context.Background(), mock, WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"})
	assert.Equal(t, "argocd", manager)
}

func TestDetectConflictManager_Helm(t *testing.T) {
	mock := &mockKubeApplier{
		managedFields: []metav1.ManagedFieldsEntry{
			{Manager: "helm-controller"},
		},
	}

	manager := detectConflictManager(context.Background(), mock, WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"})
	assert.Equal(t, "helm-controller", manager)
}

func TestDetectConflictManager_Unknown(t *testing.T) {
	mock := &mockKubeApplier{
		managedFields: []metav1.ManagedFieldsEntry{
			{Manager: "kubenow"},
			{Manager: "custom-operator"},
		},
	}

	manager := detectConflictManager(context.Background(), mock, WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"})
	assert.Equal(t, "custom-operator", manager)
}

func TestDetectConflictManager_ErrorFallback(t *testing.T) {
	mock := &mockKubeApplier{
		managedErr: fmt.Errorf("not found"),
	}

	manager := detectConflictManager(context.Background(), mock, WorkloadRef{Kind: "Deployment", Name: "api", Namespace: "default"})
	assert.Equal(t, "unknown", manager)
}

// --- compareResources tests ---

func TestCompareResources_NoChange(t *testing.T) {
	recommended := []ContainerAlignment{
		{
			Name: "api",
			Recommended: ResourceValues{
				CPURequest: 0.15, CPULimit: 0.6,
				MemoryRequest: 200 * 1024 * 1024, MemoryLimit: 600 * 1024 * 1024,
			},
		},
	}
	admitted := []ContainerResources{
		{
			Name:       "api",
			CPURequest: 0.15, CPULimit: 0.6,
			MemoryRequest: 200 * 1024 * 1024, MemoryLimit: 600 * 1024 * 1024,
		},
	}

	drifts := compareResources(recommended, admitted)
	assert.Empty(t, drifts)
}

func TestCompareResources_CPUDrift(t *testing.T) {
	recommended := []ContainerAlignment{
		{
			Name: "api",
			Recommended: ResourceValues{
				CPURequest: 0.15, CPULimit: 0.6,
				MemoryRequest: 200 * 1024 * 1024, MemoryLimit: 600 * 1024 * 1024,
			},
		},
	}
	admitted := []ContainerResources{
		{
			Name:       "api",
			CPURequest: 0.2, CPULimit: 0.6, // CPU request drifted
			MemoryRequest: 200 * 1024 * 1024, MemoryLimit: 600 * 1024 * 1024,
		},
	}

	drifts := compareResources(recommended, admitted)
	require.Len(t, drifts, 1)
	assert.Equal(t, "cpu_request", drifts[0].Field)
	assert.Equal(t, "150m", drifts[0].Requested)
	assert.Equal(t, "200m", drifts[0].Admitted)
}

// --- isGitOpsManager tests ---

func TestIsGitOpsManager(t *testing.T) {
	assert.True(t, isGitOpsManager("argocd"))
	assert.True(t, isGitOpsManager("ArgoCD"))
	assert.True(t, isGitOpsManager("flux"))
	assert.True(t, isGitOpsManager("helm-controller"))
	assert.True(t, isGitOpsManager("kustomize-controller"))
	assert.False(t, isGitOpsManager("kubectl"))
	assert.False(t, isGitOpsManager("kubenow"))
	assert.False(t, isGitOpsManager("custom-operator"))
}
