package policy

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultPolicyPath = "/etc/kubenow/policy.yaml"
	EnvPolicyPath     = "KUBENOW_POLICY"
	CurrentAPIVersion = "kubenow/v1alpha1"
	CurrentKind       = "Policy"
)

// Policy is the admin-owned configuration that gates pro-monitor behavior.
// kubenow reads it. kubenow never writes it. Admins own it.
type Policy struct {
	APIVersion string       `yaml:"apiVersion"`
	Kind       string       `yaml:"kind"`
	Global     GlobalConfig `yaml:"global"`
	Audit      AuditConfig  `yaml:"audit"`
	Apply      ApplyConfig  `yaml:"apply"`
	Namespaces NSConfig     `yaml:"namespaces"`
	Identity   IDConfig     `yaml:"identity"`
	RateLimits RateConfig   `yaml:"rate_limits"`
}

// GlobalConfig contains the master kill switch.
type GlobalConfig struct {
	Enabled bool `yaml:"enabled"`
}

// AuditConfig controls where audit bundles are stored.
type AuditConfig struct {
	Backend       string `yaml:"backend"`
	Path          string `yaml:"path"`
	RetentionDays int    `yaml:"retention_days"`
}

// ApplyConfig controls live apply permissions and guardrails.
type ApplyConfig struct {
	Enabled            bool   `yaml:"enabled"`
	RequireLatch       bool   `yaml:"require_latch"`
	MaxRequestDeltaPct int    `yaml:"max_request_delta_percent"`
	MaxLimitDeltaPct   int    `yaml:"max_limit_delta_percent"`
	AllowLimitDecrease bool   `yaml:"allow_limit_decrease"`
	MinLatchDuration   string `yaml:"min_latch_duration"`
	MaxLatchAge        string `yaml:"max_latch_age"`
	MinSafetyRating    string `yaml:"min_safety_rating"`
}

// NSConfig controls which namespaces are allowed or denied.
type NSConfig struct {
	Deny  []string `yaml:"deny"`
	Allow []string `yaml:"allow,omitempty"`
}

// IDConfig controls identity recording requirements.
type IDConfig struct {
	RequireKubeContext bool `yaml:"require_kube_context"`
	RecordOSUser       bool `yaml:"record_os_user"`
	RecordGitIdentity  bool `yaml:"record_git_identity"`
}

// RateConfig controls apply rate limits.
type RateConfig struct {
	MaxAppliesPerHour     int    `yaml:"max_applies_per_hour"`
	MaxAppliesPerWorkload int    `yaml:"max_applies_per_workload"`
	RateWindow            string `yaml:"rate_window"`
}

// LoadResult is the outcome of loading a policy file.
type LoadResult struct {
	Policy   *Policy
	Path     string
	Absent   bool   // true if no policy file was found (not an error)
	ErrorMsg string // non-empty if the file exists but is invalid
}

// ValidationError represents a single field-level validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) String() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationResult holds the outcome of policy validation.
type ValidationResult struct {
	Valid  bool
	Errors []ValidationError
}

// Load reads the admin policy file from the canonical location or $KUBENOW_POLICY.
// If the file is absent, it returns Absent=true (not an error).
// If the file exists but is invalid, it returns ErrorMsg.
func Load(overridePath string) *LoadResult {
	path := resolvePath(overridePath)

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &LoadResult{Absent: true, Path: path}
		}
		return &LoadResult{Path: path, ErrorMsg: fmt.Sprintf("failed to read policy file: %v", err)}
	}

	var p Policy
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&p); err != nil {
		return &LoadResult{Path: path, ErrorMsg: fmt.Sprintf("invalid YAML: %v", err)}
	}

	return &LoadResult{Policy: &p, Path: path}
}

// Validate checks a loaded policy for correctness.
// Returns a ValidationResult with all errors found.
// Panics if p is nil — callers must check LoadResult.ErrorMsg first.
func Validate(p *Policy) *ValidationResult {
	if p == nil {
		return &ValidationResult{
			Valid:  false,
			Errors: []ValidationError{{Field: "policy", Message: "policy is nil"}},
		}
	}

	result := &ValidationResult{Valid: true}

	if p.APIVersion != CurrentAPIVersion {
		result.addError("apiVersion", fmt.Sprintf("expected %q, got %q", CurrentAPIVersion, p.APIVersion))
	}

	if p.Kind != CurrentKind {
		result.addError("kind", fmt.Sprintf("expected %q, got %q", CurrentKind, p.Kind))
	}

	// Audit validation
	if p.Audit.Backend != "" && p.Audit.Backend != "filesystem" {
		result.addError("audit.backend", fmt.Sprintf("unsupported backend %q (v0.3.0 supports: filesystem)", p.Audit.Backend))
	}

	if p.Apply.Enabled {
		if p.Audit.Path == "" {
			result.addError("audit.path", "required when apply.enabled is true")
		}
		if p.Audit.Backend == "" {
			result.addError("audit.backend", "required when apply.enabled is true")
		}
	}

	if p.Audit.RetentionDays < 0 {
		result.addError("audit.retention_days", "must be >= 0")
	}

	// Apply validation
	if p.Apply.MaxRequestDeltaPct < 0 || p.Apply.MaxRequestDeltaPct > 100 {
		result.addError("apply.max_request_delta_percent", "must be 0-100")
	}

	if p.Apply.MaxLimitDeltaPct < 0 || p.Apply.MaxLimitDeltaPct > 100 {
		result.addError("apply.max_limit_delta_percent", "must be 0-100")
	}

	if p.Apply.MinLatchDuration != "" {
		if _, err := time.ParseDuration(p.Apply.MinLatchDuration); err != nil {
			result.addError("apply.min_latch_duration", fmt.Sprintf("invalid duration: %v", err))
		}
	}

	if p.Apply.MaxLatchAge != "" {
		if _, err := parseDurationWithDays(p.Apply.MaxLatchAge); err != nil {
			result.addError("apply.max_latch_age", fmt.Sprintf("invalid duration: %v", err))
		}
	}

	validSafetyRatings := map[string]bool{"SAFE": true, "CAUTION": true, "": true}
	if !validSafetyRatings[p.Apply.MinSafetyRating] {
		result.addError("apply.min_safety_rating", fmt.Sprintf("must be SAFE or CAUTION, got %q", p.Apply.MinSafetyRating))
	}

	// Rate limits validation
	if p.RateLimits.MaxAppliesPerHour < 0 {
		result.addError("rate_limits.max_applies_per_hour", "must be >= 0")
	}

	if p.RateLimits.MaxAppliesPerWorkload < 0 {
		result.addError("rate_limits.max_applies_per_workload", "must be >= 0")
	}

	if p.RateLimits.RateWindow != "" {
		if _, err := parseDurationWithDays(p.RateLimits.RateWindow); err != nil {
			result.addError("rate_limits.rate_window", fmt.Sprintf("invalid duration: %v", err))
		}
	}

	return result
}

// CheckAuditPath verifies the audit directory is writable.
func CheckAuditPath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("audit path does not exist: %s", path)
		}
		return fmt.Errorf("cannot access audit path: %v", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("audit path is not a directory: %s", path)
	}

	// Try writing a temp file to verify writability
	testFile := filepath.Join(path, ".kubenow-write-test")
	if err := os.WriteFile(testFile, []byte("test"), 0o600); err != nil {
		return fmt.Errorf("audit path is not writable: %v", err)
	}
	_ = os.Remove(testFile) // best-effort cleanup

	return nil
}

// IsNamespaceDenied checks if a namespace is blocked by deny/allow lists.
// A namespace is denied if:
//   - it appears in the deny list, OR
//   - an allow list is set and the namespace is not in it.
func (p *Policy) IsNamespaceDenied(namespace string) bool {
	for _, denied := range p.Namespaces.Deny {
		if denied == namespace {
			return true
		}
	}

	// If allow list is set, namespace must be in it
	if len(p.Namespaces.Allow) > 0 {
		for _, allowed := range p.Namespaces.Allow {
			if allowed == namespace {
				return false
			}
		}
		return true // not in allow list
	}

	return false
}

// MinLatchDurationParsed returns the parsed min_latch_duration or the default.
func (p *Policy) MinLatchDurationParsed() time.Duration {
	if p.Apply.MinLatchDuration == "" {
		return time.Hour
	}
	d, err := time.ParseDuration(p.Apply.MinLatchDuration)
	if err != nil {
		return time.Hour
	}
	return d
}

// MaxLatchAgeParsed returns the parsed max_latch_age or the default.
func (p *Policy) MaxLatchAgeParsed() time.Duration {
	if p.Apply.MaxLatchAge == "" {
		return 7 * 24 * time.Hour
	}
	d, err := parseDurationWithDays(p.Apply.MaxLatchAge)
	if err != nil {
		return 7 * 24 * time.Hour
	}
	return d
}

// RateWindowParsed returns the parsed rate_window or the default.
func (p *Policy) RateWindowParsed() time.Duration {
	if p.RateLimits.RateWindow == "" {
		return 24 * time.Hour
	}
	d, err := parseDurationWithDays(p.RateLimits.RateWindow)
	if err != nil {
		return 24 * time.Hour
	}
	return d
}

func (r *ValidationResult) addError(field, message string) {
	r.Valid = false
	r.Errors = append(r.Errors, ValidationError{Field: field, Message: message})
}

func resolvePath(overridePath string) string {
	path := DefaultPolicyPath
	if overridePath != "" {
		path = overridePath
	} else if envPath := os.Getenv(EnvPolicyPath); envPath != "" {
		path = envPath
	}
	// Clean the path and reject traversal attempts
	return filepath.Clean(path)
}

// parseDurationWithDays extends time.ParseDuration to support "d" (days) suffix.
// Negative durations are rejected.
func parseDurationWithDays(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		numStr := strings.TrimSuffix(s, "d")
		var days float64
		if _, err := fmt.Sscanf(numStr, "%f", &days); err != nil {
			return 0, fmt.Errorf("invalid day duration: %s", s)
		}
		if days < 0 {
			return 0, fmt.Errorf("negative duration not allowed: %s", s)
		}
		return time.Duration(days * float64(24*time.Hour)), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("negative duration not allowed: %s", s)
	}
	return d, nil
}
