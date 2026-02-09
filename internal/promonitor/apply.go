package promonitor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ppiankov/kubenow/internal/audit"
	"github.com/ppiankov/kubenow/internal/policy"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// KubeApplier abstracts Kubernetes mutations for testability.
type KubeApplier interface {
	PatchWorkload(ctx context.Context, ref WorkloadRef, patchJSON []byte, fieldManager string) error
	GetContainerResources(ctx context.Context, ref WorkloadRef) ([]ContainerResources, error)
	GetManagedFields(ctx context.Context, ref WorkloadRef) ([]metav1.ManagedFieldsEntry, error)
	GetWorkloadObject(ctx context.Context, ref WorkloadRef) (map[string]interface{}, error)
}

// ClientsetApplier implements KubeApplier using a real Kubernetes clientset.
type ClientsetApplier struct {
	Client *kubernetes.Clientset
}

const fieldManager = "kubenow"

func (a *ClientsetApplier) PatchWorkload(ctx context.Context, ref WorkloadRef, patchJSON []byte, fm string) error {
	opts := metav1.PatchOptions{FieldManager: fm}
	switch ref.Kind {
	case KindDeployment:
		_, err := a.Client.AppsV1().Deployments(ref.Namespace).Patch(ctx, ref.Name, types.ApplyPatchType, patchJSON, opts)
		return err
	case KindStatefulSet:
		_, err := a.Client.AppsV1().StatefulSets(ref.Namespace).Patch(ctx, ref.Name, types.ApplyPatchType, patchJSON, opts)
		return err
	case KindDaemonSet:
		_, err := a.Client.AppsV1().DaemonSets(ref.Namespace).Patch(ctx, ref.Name, types.ApplyPatchType, patchJSON, opts)
		return err
	case KindPod:
		return fmt.Errorf("apply is not supported for Pod kind (managed by external controller)")
	default:
		return fmt.Errorf("unsupported kind: %s", ref.Kind)
	}
}

func (a *ClientsetApplier) GetContainerResources(ctx context.Context, ref WorkloadRef) ([]ContainerResources, error) {
	return FetchContainerResources(ctx, a.Client, &ref)
}

func (a *ClientsetApplier) GetManagedFields(ctx context.Context, ref WorkloadRef) ([]metav1.ManagedFieldsEntry, error) {
	switch ref.Kind {
	case KindDeployment:
		obj, err := a.Client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return obj.ManagedFields, nil
	case KindStatefulSet:
		obj, err := a.Client.AppsV1().StatefulSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return obj.ManagedFields, nil
	case KindDaemonSet:
		obj, err := a.Client.AppsV1().DaemonSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return obj.ManagedFields, nil
	case KindPod:
		obj, err := a.Client.CoreV1().Pods(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		return obj.ManagedFields, nil
	default:
		return nil, fmt.Errorf("unsupported kind: %s", ref.Kind)
	}
}

func (a *ClientsetApplier) GetWorkloadObject(ctx context.Context, ref WorkloadRef) (map[string]interface{}, error) {
	var raw interface{}
	switch ref.Kind {
	case KindDeployment:
		obj, err := a.Client.AppsV1().Deployments(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		raw = obj
	case KindStatefulSet:
		obj, err := a.Client.AppsV1().StatefulSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		raw = obj
	case KindDaemonSet:
		obj, err := a.Client.AppsV1().DaemonSets(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		raw = obj
	case KindPod:
		obj, err := a.Client.CoreV1().Pods(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		raw = obj
	default:
		return nil, fmt.Errorf("unsupported kind: %s", ref.Kind)
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal workload: %w", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal workload: %w", err)
	}
	return result, nil
}

// ApplyInput holds all inputs required for the apply operation.
type ApplyInput struct {
	Recommendation   *AlignmentRecommendation
	Workload         WorkloadRef
	Mode             Mode
	Policy           *PolicyBounds
	HPAInfo          *HPAInfo
	HPAAcknowledged  bool
	LatchTimestamp   time.Time
	LatchDuration    time.Duration
	AuditWritable    bool
	IdentityRecorded bool
	RateLimitOK      bool
}

// ApplyResult holds the outcome of an apply operation.
type ApplyResult struct {
	Applied         bool
	DenialReasons   []string
	ConflictManager string
	GitOpsConflict  bool
	Error           error
	Requested       map[string]string // container→resource summary
	Admitted        map[string]string
	Drifts          []ResourceDrift
}

// ResourceDrift records a difference between requested and admitted values.
type ResourceDrift struct {
	Container string
	Field     string
	Requested string
	Admitted  string
}

// Known GitOps field managers that indicate managed-by-GitOps conflicts.
var gitOpsManagers = []string{
	"argocd",
	"flux",
	"helm-controller",
	"kustomize-controller",
}

// CheckActionable validates all preconditions for apply.
// Returns nil if all checks pass, or a list of denial reasons.
func CheckActionable(input *ApplyInput) []string {
	var reasons []string

	if input.Mode != ModeApplyReady {
		reasons = append(reasons, "mode is not apply-ready (policy must enable apply)")
	}

	if input.Recommendation == nil {
		reasons = append(reasons, "no recommendation available")
	}

	if input.Policy == nil {
		reasons = append(reasons, "no policy loaded")
	}

	if input.Recommendation != nil && input.Policy != nil {
		// Safety rating check
		if input.Policy.MinSafetyRating != "" {
			minLevel := SafetyRatingLevel(input.Policy.MinSafetyRating)
			actualLevel := SafetyRatingLevel(input.Recommendation.Safety)
			if actualLevel > minLevel {
				reasons = append(reasons, fmt.Sprintf(
					"safety rating %s below policy minimum %s",
					input.Recommendation.Safety, input.Policy.MinSafetyRating))
			}
		}
	}

	// Namespace check via recommendation policy result
	if input.Recommendation != nil && input.Recommendation.Policy != nil {
		for _, r := range input.Recommendation.Policy.DenialReasons {
			if strings.Contains(r, "namespace") {
				reasons = append(reasons, r)
			}
		}
	}

	// HPA check
	if input.HPAInfo != nil && !input.HPAAcknowledged {
		reasons = append(reasons, fmt.Sprintf(
			"HPA %q detected — pass --acknowledge-hpa to proceed",
			input.HPAInfo.Name))
	}

	// Latch freshness check
	if !input.LatchTimestamp.IsZero() && input.Policy != nil {
		maxAge := 7 * 24 * time.Hour // default
		if input.Policy.MaxLatchAge > 0 {
			maxAge = input.Policy.MaxLatchAge
		}
		if time.Since(input.LatchTimestamp) > maxAge {
			reasons = append(reasons, "latch data is stale (exceeds max_latch_age)")
		}
	}

	// Latch duration check
	if input.LatchDuration > 0 && input.Policy != nil {
		minDuration := time.Hour // default
		if input.Policy.MinLatchDuration > 0 {
			minDuration = input.Policy.MinLatchDuration
		}
		if input.LatchDuration < minDuration {
			reasons = append(reasons, fmt.Sprintf(
				"latch duration %s below policy minimum %s",
				input.LatchDuration, minDuration))
		}
	}

	// Audit/identity/rate-limit checks
	if input.Policy != nil && !input.AuditWritable {
		reasons = append(reasons, "audit path is not writable")
	}
	if input.Policy != nil && !input.IdentityRecorded {
		reasons = append(reasons, "identity not recorded (required for audit)")
	}
	if input.Policy != nil && !input.RateLimitOK {
		reasons = append(reasons, "rate limit exceeded")
	}

	return reasons
}

// ExecuteApply runs the full apply workflow: check → patch → read-back → drift.
func ExecuteApply(ctx context.Context, client KubeApplier, input *ApplyInput) *ApplyResult {
	result := &ApplyResult{}

	// Pod kind is structurally blocked from apply — pods are managed by
	// external controllers (CNPG, Strimzi, etc.) and must not be patched.
	if input.Workload.Kind == KindPod {
		result.DenialReasons = []string{"apply is not supported for Pod kind (managed by external controller)"}
		return result
	}

	// Pre-flight checks
	reasons := CheckActionable(input)
	if len(reasons) > 0 {
		result.DenialReasons = reasons
		return result
	}

	// Build SSA patch
	patchJSON, err := buildSSAPatchJSON(input.Recommendation)
	if err != nil {
		result.Error = fmt.Errorf("failed to build patch: %w", err)
		return result
	}

	// Apply via SSA (force=false)
	err = client.PatchWorkload(ctx, input.Workload, patchJSON, fieldManager)
	if err != nil {
		// Check if this is a conflict error
		if isConflictError(err) {
			manager := detectConflictManager(ctx, client, input.Workload)
			result.ConflictManager = manager
			result.GitOpsConflict = isGitOpsManager(manager)
			result.Error = fmt.Errorf("ssa conflict: %w", err)
		} else {
			result.Error = err
		}
		return result
	}

	result.Applied = true

	// Read-back verification
	admitted, err := client.GetContainerResources(ctx, input.Workload)
	if err != nil {
		// Apply succeeded but read-back failed — still report success
		result.Error = fmt.Errorf("read-back failed (apply succeeded): %w", err)
		return result
	}

	// Build requested map for display
	result.Requested = buildResourceSummary(input.Recommendation.Containers)
	result.Admitted = buildContainerResourceSummary(admitted)

	// Compare for drift
	result.Drifts = compareResources(input.Recommendation.Containers, admitted)

	return result
}

// buildSSAPatchJSON creates a JSON SSA patch from a recommendation.
func buildSSAPatchJSON(rec *AlignmentRecommendation) ([]byte, error) {
	containers := make([]ssaContainer, len(rec.Containers))
	for i, c := range rec.Containers {
		containers[i] = ssaContainer{
			Name: c.Name,
			Resources: ssaResources{
				Requests: ssaResourceValues{
					CPU:    formatCPUResource(c.Recommended.CPURequest),
					Memory: formatMemResource(c.Recommended.MemoryRequest),
				},
				Limits: ssaResourceValues{
					CPU:    formatCPUResource(c.Recommended.CPULimit),
					Memory: formatMemResource(c.Recommended.MemoryLimit),
				},
			},
		}
	}

	apiVersion := "apps/v1"
	doc := ssaPatchDoc{
		APIVersion: apiVersion,
		Kind:       rec.Workload.Kind,
		Metadata: ssaMetadata{
			Name:      rec.Workload.Name,
			Namespace: rec.Workload.Namespace,
		},
		Spec: ssaSpec{
			Template: ssaTemplate{
				Spec: ssaPodSpec{
					Containers: containers,
				},
			},
		},
	}

	return json.Marshal(doc)
}

// SSA patch document structs with JSON tags (parallel to patchDoc in export.go which uses YAML).
type ssaPatchDoc struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   ssaMetadata `json:"metadata"`
	Spec       ssaSpec     `json:"spec"`
}

type ssaMetadata struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type ssaSpec struct {
	Template ssaTemplate `json:"template"`
}

type ssaTemplate struct {
	Spec ssaPodSpec `json:"spec"`
}

type ssaPodSpec struct {
	Containers []ssaContainer `json:"containers"`
}

type ssaContainer struct {
	Name      string       `json:"name"`
	Resources ssaResources `json:"resources"`
}

type ssaResources struct {
	Requests ssaResourceValues `json:"requests"`
	Limits   ssaResourceValues `json:"limits"`
}

type ssaResourceValues struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

// isConflictError checks if the error is a Kubernetes conflict (HTTP 409).
func isConflictError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "conflict") || strings.Contains(errStr, "Conflict") || strings.Contains(errStr, "409")
}

// detectConflictManager inspects managedFields to find the conflicting field manager.
func detectConflictManager(ctx context.Context, client KubeApplier, ref WorkloadRef) string {
	fields, err := client.GetManagedFields(ctx, ref)
	if err != nil {
		return "unknown"
	}

	for _, f := range fields {
		if f.Manager == fieldManager {
			continue // skip our own manager
		}
		if isGitOpsManager(f.Manager) {
			return f.Manager
		}
	}

	// Return first non-self manager if no GitOps manager found
	for _, f := range fields {
		if f.Manager != fieldManager {
			return f.Manager
		}
	}

	return "unknown"
}

// isGitOpsManager checks if a field manager name matches a known GitOps controller.
func isGitOpsManager(manager string) bool {
	lower := strings.ToLower(manager)
	for _, gm := range gitOpsManagers {
		if strings.Contains(lower, gm) {
			return true
		}
	}
	return false
}

// compareResources compares recommended values against admitted values, returning any drifts.
func compareResources(recommended []ContainerAlignment, admitted []ContainerResources) []ResourceDrift {
	var drifts []ResourceDrift

	admittedMap := make(map[string]ContainerResources, len(admitted))
	for _, a := range admitted {
		admittedMap[a.Name] = a
	}

	for _, rec := range recommended {
		adm, ok := admittedMap[rec.Name]
		if !ok {
			continue
		}

		if formatCPUResource(rec.Recommended.CPURequest) != formatCPUResource(adm.CPURequest) {
			drifts = append(drifts, ResourceDrift{
				Container: rec.Name,
				Field:     "cpu_request",
				Requested: formatCPUResource(rec.Recommended.CPURequest),
				Admitted:  formatCPUResource(adm.CPURequest),
			})
		}
		if formatCPUResource(rec.Recommended.CPULimit) != formatCPUResource(adm.CPULimit) {
			drifts = append(drifts, ResourceDrift{
				Container: rec.Name,
				Field:     "cpu_limit",
				Requested: formatCPUResource(rec.Recommended.CPULimit),
				Admitted:  formatCPUResource(adm.CPULimit),
			})
		}
		if formatMemResource(rec.Recommended.MemoryRequest) != formatMemResource(adm.MemoryRequest) {
			drifts = append(drifts, ResourceDrift{
				Container: rec.Name,
				Field:     "memory_request",
				Requested: formatMemResource(rec.Recommended.MemoryRequest),
				Admitted:  formatMemResource(adm.MemoryRequest),
			})
		}
		if formatMemResource(rec.Recommended.MemoryLimit) != formatMemResource(adm.MemoryLimit) {
			drifts = append(drifts, ResourceDrift{
				Container: rec.Name,
				Field:     "memory_limit",
				Requested: formatMemResource(rec.Recommended.MemoryLimit),
				Admitted:  formatMemResource(adm.MemoryLimit),
			})
		}
	}

	return drifts
}

// buildResourceSummary creates a container→resource summary from recommendation containers.
func buildResourceSummary(containers []ContainerAlignment) map[string]string {
	m := make(map[string]string, len(containers))
	for _, c := range containers {
		m[c.Name] = fmt.Sprintf("cpu=%s/%s mem=%s/%s",
			formatCPUResource(c.Recommended.CPURequest),
			formatCPUResource(c.Recommended.CPULimit),
			formatMemResource(c.Recommended.MemoryRequest),
			formatMemResource(c.Recommended.MemoryLimit))
	}
	return m
}

// buildContainerResourceSummary creates a summary from admitted container resources.
func buildContainerResourceSummary(containers []ContainerResources) map[string]string {
	m := make(map[string]string, len(containers))
	for _, c := range containers {
		m[c.Name] = fmt.Sprintf("cpu=%s/%s mem=%s/%s",
			formatCPUResource(c.CPURequest),
			formatCPUResource(c.CPULimit),
			formatMemResource(c.MemoryRequest),
			formatMemResource(c.MemoryLimit))
	}
	return m
}

// AuditApplyConfig holds all parameters for an audit-wrapped apply.
type AuditApplyConfig struct {
	AuditPath      string
	Client         KubeApplier
	KubeClient     kubernetes.Interface
	KubeconfigPath string
	Input          *ApplyInput
	Version        string
	FullPolicy     *policy.Policy
	RateLimitCfg   audit.RateLimitConfig
}

// ExecuteApplyWithAudit is the orchestrator that wraps ExecuteApply with
// identity resolution, rate limiting, and audit bundle creation.
func ExecuteApplyWithAudit(ctx context.Context, cfg *AuditApplyConfig) *ApplyResult {
	result := &ApplyResult{}

	// 1. Resolve identity
	identity := audit.ResolveIdentity(ctx, cfg.KubeClient, cfg.KubeconfigPath)

	// 2. Fetch before-object
	beforeObj, err := cfg.Client.GetWorkloadObject(ctx, cfg.Input.Workload)
	if err != nil {
		result.Error = fmt.Errorf("fetch before-object: %w", err)
		return result
	}

	// 3. Extract workload UID
	workloadUID := extractUID(beforeObj)

	// 4. Check rate limit
	rateLimitResult, err := audit.CheckAndIncrement(cfg.RateLimitCfg,
		workloadUID, cfg.Input.Workload.FullString(), identity.KubeUser)
	if err != nil {
		result.Error = fmt.Errorf("rate limit check: %w", err)
		return result
	}

	// 5. Set flags on input
	cfg.Input.AuditWritable = true // we got this far, path is writable
	cfg.Input.IdentityRecorded = identity.IdentitySource != "unknown"
	cfg.Input.RateLimitOK = rateLimitResult.Allowed

	// 6. Run CheckActionable — if denied, return denial result
	reasons := CheckActionable(cfg.Input)
	if len(reasons) > 0 {
		result.DenialReasons = reasons
		return result
	}

	// 7. Map promonitor types → audit BundleConfig
	ts := time.Now()
	bundleCfg := audit.BundleConfig{
		AuditPath: cfg.AuditPath,
		Timestamp: ts,
		Workload: audit.BundleWorkload{
			Kind:      cfg.Input.Workload.Kind,
			Name:      cfg.Input.Workload.Name,
			Namespace: cfg.Input.Workload.Namespace,
			UID:       workloadUID,
		},
		BeforeObject: beforeObj,
		Recommendation: audit.BundleRecommendation{
			Safety:     string(cfg.Input.Recommendation.Safety),
			Confidence: string(cfg.Input.Recommendation.Confidence),
		},
		Identity: identity,
		Version:  cfg.Version,
		Changes:  mapChanges(cfg.Input.Recommendation.Containers),
	}

	if cfg.Input.Recommendation.Evidence != nil {
		bundleCfg.Latch = audit.BundleLatch{
			Duration:       cfg.Input.Recommendation.Evidence.Duration,
			SampleCount:    cfg.Input.Recommendation.Evidence.SampleCount,
			SampleInterval: cfg.Input.Recommendation.Evidence.SampleInterval,
		}
	}

	// 8. CreateBundle — if fails, abort
	bundle, err := audit.CreateBundle(bundleCfg)
	if err != nil {
		result.Error = fmt.Errorf("create audit bundle: %w", err)
		return result
	}

	// 9. ExecuteApply
	applyResult := ExecuteApply(ctx, cfg.Client, cfg.Input)

	// 10. Fetch after-object (best-effort)
	afterObj, afterErr := cfg.Client.GetWorkloadObject(ctx, cfg.Input.Workload)
	if afterErr != nil {
		afterObj = beforeObj // fallback to before if fetch fails
	}

	// 11. FinalizeBundle
	status := "applied"
	if applyResult.Error != nil {
		status = "failed"
	}
	if !applyResult.Applied && len(applyResult.DenialReasons) > 0 {
		status = "denied"
	}
	_ = audit.FinalizeBundle(bundle, afterObj, status, ts, applyResult.Error) // best-effort

	return applyResult
}

// extractUID pulls the UID from a workload object's metadata.
func extractUID(obj map[string]interface{}) string {
	metadata, ok := obj["metadata"].(map[string]interface{})
	if !ok {
		return ""
	}
	uid, _ := metadata["uid"].(string)
	return uid
}

// mapChanges converts container alignment data to audit bundle changes.
func mapChanges(containers []ContainerAlignment) []audit.BundleChange {
	var changes []audit.BundleChange
	for _, c := range containers {
		changes = append(changes, audit.BundleChange{
			Field:        fmt.Sprintf("%s/cpu_request", c.Name),
			Before:       formatCPUResource(c.Current.CPURequest),
			After:        formatCPUResource(c.Recommended.CPURequest),
			DeltaPercent: c.Delta.CPURequestPercent,
		})
		changes = append(changes, audit.BundleChange{
			Field:        fmt.Sprintf("%s/cpu_limit", c.Name),
			Before:       formatCPUResource(c.Current.CPULimit),
			After:        formatCPUResource(c.Recommended.CPULimit),
			DeltaPercent: c.Delta.CPULimitPercent,
		})
		changes = append(changes, audit.BundleChange{
			Field:        fmt.Sprintf("%s/memory_request", c.Name),
			Before:       formatMemResource(c.Current.MemoryRequest),
			After:        formatMemResource(c.Recommended.MemoryRequest),
			DeltaPercent: c.Delta.MemoryRequestPercent,
		})
		changes = append(changes, audit.BundleChange{
			Field:        fmt.Sprintf("%s/memory_limit", c.Name),
			Before:       formatMemResource(c.Current.MemoryLimit),
			After:        formatMemResource(c.Recommended.MemoryLimit),
			DeltaPercent: c.Delta.MemoryLimitPercent,
		})
	}
	return changes
}
