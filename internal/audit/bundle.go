package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pmezard/go-difflib/difflib"
	"gopkg.in/yaml.v3"
)

// BundleConfig holds all inputs needed to create an audit bundle.
type BundleConfig struct {
	AuditPath        string
	Timestamp        time.Time
	Workload         BundleWorkload
	BeforeObject     map[string]interface{}
	Recommendation   BundleRecommendation
	Latch            BundleLatch
	Identity         *Identity
	ClusterServer    string
	Version          string
	GuardrailsPassed []string
	Changes          []BundleChange
}

// BundleWorkload identifies the target workload.
type BundleWorkload struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid"`
}

// BundleRecommendation summarizes the recommendation for the bundle.
type BundleRecommendation struct {
	Safety     string          `json:"safety"`
	Confidence string          `json:"confidence"`
	Evidence   *BundleEvidence `json:"evidence,omitempty"`
}

// BundleEvidence holds the latch evidence summary.
type BundleEvidence struct {
	CPU            BundlePercentiles `json:"cpu"`
	Memory         BundlePercentiles `json:"memory"`
	Signals        BundleSignals     `json:"signals"`
	SafetyMargin   float64           `json:"safety_margin"`
	Duration       time.Duration     `json:"duration"`
	SampleCount    int               `json:"sample_count"`
	SampleInterval time.Duration     `json:"sample_interval"`
}

// BundlePercentiles holds percentile data for a resource.
type BundlePercentiles struct {
	P50        float64 `json:"p50"`
	P95        float64 `json:"p95"`
	P99        float64 `json:"p99"`
	Max        float64 `json:"max"`
	Avg        float64 `json:"avg"`
	SpikeCount int     `json:"spike_count"`
}

// BundleSignals holds distress signals observed during the latch.
type BundleSignals struct {
	OOMKills           int  `json:"oom_kills"`
	Restarts           int  `json:"restarts"`
	Evictions          int  `json:"evictions"`
	ThrottlingDetected bool `json:"throttling_detected"`
}

// BundleChange records a single field change.
type BundleChange struct {
	Field        string  `json:"field"`
	Before       string  `json:"before"`
	After        string  `json:"after"`
	DeltaPercent float64 `json:"delta_percent"`
}

// BundleLatch holds latch metadata for the decision record.
type BundleLatch struct {
	Duration       time.Duration `json:"duration"`
	SampleCount    int           `json:"sample_count"`
	SampleInterval time.Duration `json:"sample_interval"`
}

// DecisionJSON is the full decision record written to decision.json.
type DecisionJSON struct {
	Version        string         `json:"version"`
	Timestamp      string         `json:"timestamp"`
	Status         string         `json:"status"`
	Workload       BundleWorkload `json:"workload"`
	Cluster        string         `json:"cluster"`
	Identity       *Identity      `json:"identity"`
	Recommendation DecisionRec    `json:"recommendation"`
	Latch          DecisionLatch  `json:"latch"`
	Guardrails     []string       `json:"guardrails_passed"`
	Changes        []BundleChange `json:"changes"`
	AppliedAt      string         `json:"applied_at,omitempty"`
	Error          string         `json:"error,omitempty"`
}

// DecisionRec is the recommendation section of decision.json.
type DecisionRec struct {
	Safety     string          `json:"safety"`
	Confidence string          `json:"confidence"`
	Evidence   *BundleEvidence `json:"evidence,omitempty"`
}

// DecisionLatch is the latch section of decision.json.
type DecisionLatch struct {
	Duration       string `json:"duration"`
	SampleCount    int    `json:"sample_count"`
	SampleInterval string `json:"sample_interval"`
}

// AuditBundle tracks paths for a created audit bundle.
type AuditBundle struct {
	Dir          string
	DecisionPath string
}

// CreateBundle creates the audit bundle directory and writes before.yaml and
// a pending decision.json. If this function fails, the apply MUST be aborted.
func CreateBundle(cfg BundleConfig) (*AuditBundle, error) {
	dirName := bundleDirName(cfg.Timestamp, cfg.Workload)
	bundleDir := filepath.Join(cfg.AuditPath, dirName)

	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		return nil, fmt.Errorf("create bundle dir: %w", err)
	}

	// Write before.yaml (volatile fields stripped)
	beforeObj := deepCopyMap(cfg.BeforeObject)
	stripVolatileFields(beforeObj)

	beforeYAML, err := yaml.Marshal(beforeObj)
	if err != nil {
		return nil, fmt.Errorf("marshal before YAML: %w", err)
	}
	beforePath := filepath.Join(bundleDir, "before.yaml")
	if err := os.WriteFile(beforePath, beforeYAML, 0644); err != nil {
		return nil, fmt.Errorf("write before.yaml: %w", err)
	}

	// Write decision.json (pending)
	decision := buildDecisionJSON(cfg, "pending")
	decisionPath := filepath.Join(bundleDir, "decision.json")
	decisionData, err := json.MarshalIndent(decision, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal decision.json: %w", err)
	}
	if err := os.WriteFile(decisionPath, decisionData, 0644); err != nil {
		return nil, fmt.Errorf("write decision.json: %w", err)
	}

	return &AuditBundle{
		Dir:          bundleDir,
		DecisionPath: decisionPath,
	}, nil
}

// FinalizeBundle writes after.yaml, generates diff.patch, and updates
// decision.json with the final status.
func FinalizeBundle(bundle *AuditBundle, afterObject map[string]interface{}, status string, appliedAt time.Time, applyErr error) error {
	if bundle == nil {
		return fmt.Errorf("nil bundle")
	}

	// Write after.yaml
	afterObj := deepCopyMap(afterObject)
	stripVolatileFields(afterObj)

	afterYAML, err := yaml.Marshal(afterObj)
	if err != nil {
		return fmt.Errorf("marshal after YAML: %w", err)
	}
	afterPath := filepath.Join(bundle.Dir, "after.yaml")
	if err := os.WriteFile(afterPath, afterYAML, 0644); err != nil {
		return fmt.Errorf("write after.yaml: %w", err)
	}

	// Generate diff.patch
	beforeData, err := os.ReadFile(filepath.Join(bundle.Dir, "before.yaml"))
	if err != nil {
		return fmt.Errorf("read before.yaml for diff: %w", err)
	}

	diff := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(beforeData)),
		B:        difflib.SplitLines(string(afterYAML)),
		FromFile: "before.yaml",
		ToFile:   "after.yaml",
		Context:  3,
	}
	diffText, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return fmt.Errorf("generate diff: %w", err)
	}
	diffPath := filepath.Join(bundle.Dir, "diff.patch")
	if err := os.WriteFile(diffPath, []byte(diffText), 0644); err != nil {
		return fmt.Errorf("write diff.patch: %w", err)
	}

	// Update decision.json
	decisionData, err := os.ReadFile(bundle.DecisionPath)
	if err != nil {
		return fmt.Errorf("read decision.json: %w", err)
	}

	var decision DecisionJSON
	if err := json.Unmarshal(decisionData, &decision); err != nil {
		return fmt.Errorf("unmarshal decision.json: %w", err)
	}

	decision.Status = status
	decision.AppliedAt = appliedAt.UTC().Format(time.RFC3339)
	if applyErr != nil {
		decision.Error = applyErr.Error()
	}

	updatedData, err := json.MarshalIndent(decision, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal updated decision.json: %w", err)
	}
	return os.WriteFile(bundle.DecisionPath, updatedData, 0644)
}

// bundleDirName formats the bundle directory name.
func bundleDirName(ts time.Time, workload BundleWorkload) string {
	return fmt.Sprintf("%s__%s__%s__%s",
		ts.UTC().Format("20060102T150405Z"),
		workload.Namespace,
		strings.ToLower(workload.Kind),
		workload.Name)
}

// buildDecisionJSON constructs the full decision record.
func buildDecisionJSON(cfg BundleConfig, status string) *DecisionJSON {
	return &DecisionJSON{
		Version:   cfg.Version,
		Timestamp: cfg.Timestamp.UTC().Format(time.RFC3339),
		Status:    status,
		Workload:  cfg.Workload,
		Cluster:   cfg.ClusterServer,
		Identity:  cfg.Identity,
		Recommendation: DecisionRec{
			Safety:     cfg.Recommendation.Safety,
			Confidence: cfg.Recommendation.Confidence,
			Evidence:   cfg.Recommendation.Evidence,
		},
		Latch: DecisionLatch{
			Duration:       cfg.Latch.Duration.String(),
			SampleCount:    cfg.Latch.SampleCount,
			SampleInterval: cfg.Latch.SampleInterval.String(),
		},
		Guardrails: cfg.GuardrailsPassed,
		Changes:    cfg.Changes,
	}
}

// stripVolatileFields removes ephemeral fields that change between reads.
func stripVolatileFields(obj map[string]interface{}) {
	if metadata, ok := obj["metadata"].(map[string]interface{}); ok {
		delete(metadata, "resourceVersion")
		delete(metadata, "generation")
		delete(metadata, "managedFields")
		delete(metadata, "uid")
		delete(metadata, "creationTimestamp")
	}
	delete(obj, "status")
}

// deepCopyMap creates a deep copy of a map[string]interface{} via JSON round-trip.
func deepCopyMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	data, err := json.Marshal(src)
	if err != nil {
		return nil
	}
	var dst map[string]interface{}
	if err := json.Unmarshal(data, &dst); err != nil {
		return nil
	}
	return dst
}
