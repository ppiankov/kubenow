package policy

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ValidPolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")

	yaml := `apiVersion: kubenow/v1alpha1
kind: Policy
global:
  enabled: true
audit:
  backend: filesystem
  path: /var/lib/kubenow/audit
  retention_days: 90
apply:
  enabled: true
  require_latch: true
  max_request_delta_percent: 30
  max_limit_delta_percent: 50
  allow_limit_decrease: false
  min_latch_duration: 1h
  max_latch_age: 7d
  min_safety_rating: SAFE
namespaces:
  deny:
    - kube-system
    - kube-public
identity:
  require_kube_context: true
  record_os_user: true
  record_git_identity: false
rate_limits:
  max_applies_per_hour: 10
  max_applies_per_workload: 3
  rate_window: 24h
`
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0644))

	result := Load(path)
	assert.Empty(t, result.ErrorMsg)
	assert.False(t, result.Absent)
	assert.Equal(t, path, result.Path)
	require.NotNil(t, result.Policy)

	p := result.Policy
	assert.Equal(t, "kubenow/v1alpha1", p.APIVersion)
	assert.Equal(t, "Policy", p.Kind)
	assert.True(t, p.Global.Enabled)
	assert.Equal(t, "filesystem", p.Audit.Backend)
	assert.Equal(t, "/var/lib/kubenow/audit", p.Audit.Path)
	assert.Equal(t, 90, p.Audit.RetentionDays)
	assert.True(t, p.Apply.Enabled)
	assert.True(t, p.Apply.RequireLatch)
	assert.Equal(t, 30, p.Apply.MaxRequestDeltaPct)
	assert.Equal(t, 50, p.Apply.MaxLimitDeltaPct)
	assert.False(t, p.Apply.AllowLimitDecrease)
	assert.Equal(t, "1h", p.Apply.MinLatchDuration)
	assert.Equal(t, "7d", p.Apply.MaxLatchAge)
	assert.Equal(t, "SAFE", p.Apply.MinSafetyRating)
	assert.Equal(t, []string{"kube-system", "kube-public"}, p.Namespaces.Deny)
	assert.True(t, p.Identity.RequireKubeContext)
	assert.True(t, p.Identity.RecordOSUser)
	assert.False(t, p.Identity.RecordGitIdentity)
	assert.Equal(t, 10, p.RateLimits.MaxAppliesPerHour)
	assert.Equal(t, 3, p.RateLimits.MaxAppliesPerWorkload)
	assert.Equal(t, "24h", p.RateLimits.RateWindow)
}

func TestLoad_FileNotFound(t *testing.T) {
	result := Load("/nonexistent/path/policy.yaml")
	assert.True(t, result.Absent)
	assert.Empty(t, result.ErrorMsg)
	assert.Nil(t, result.Policy)
}

func TestLoad_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	require.NoError(t, os.WriteFile(path, []byte("{{invalid yaml"), 0644))

	result := Load(path)
	assert.False(t, result.Absent)
	assert.Contains(t, result.ErrorMsg, "invalid YAML")
	assert.Nil(t, result.Policy)
}

func TestLoad_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "env-policy.yaml")

	yaml := `apiVersion: kubenow/v1alpha1
kind: Policy
global:
  enabled: false
`
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0644))

	t.Setenv(EnvPolicyPath, path)
	result := Load("")
	assert.Equal(t, path, result.Path)
	require.NotNil(t, result.Policy)
	assert.False(t, result.Policy.Global.Enabled)
}

func TestValidate_ValidPolicy(t *testing.T) {
	p := &Policy{
		APIVersion: CurrentAPIVersion,
		Kind:       CurrentKind,
		Global:     GlobalConfig{Enabled: true},
		Audit: AuditConfig{
			Backend:       "filesystem",
			Path:          "/var/lib/kubenow/audit",
			RetentionDays: 90,
		},
		Apply: ApplyConfig{
			Enabled:            true,
			RequireLatch:       true,
			MaxRequestDeltaPct: 30,
			MaxLimitDeltaPct:   50,
			MinLatchDuration:   "1h",
			MaxLatchAge:        "7d",
			MinSafetyRating:    "SAFE",
		},
		RateLimits: RateConfig{
			MaxAppliesPerHour:     10,
			MaxAppliesPerWorkload: 3,
			RateWindow:            "24h",
		},
	}

	result := Validate(p)
	assert.True(t, result.Valid)
	assert.Empty(t, result.Errors)
}

func TestValidate_WrongAPIVersion(t *testing.T) {
	p := &Policy{
		APIVersion: "wrong/v1",
		Kind:       CurrentKind,
	}

	result := Validate(p)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "apiVersion", result.Errors[0].Field)
}

func TestValidate_WrongKind(t *testing.T) {
	p := &Policy{
		APIVersion: CurrentAPIVersion,
		Kind:       "Wrong",
	}

	result := Validate(p)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "kind", result.Errors[0].Field)
}

func TestValidate_ApplyRequiresAudit(t *testing.T) {
	p := &Policy{
		APIVersion: CurrentAPIVersion,
		Kind:       CurrentKind,
		Apply: ApplyConfig{
			Enabled:            true,
			MaxRequestDeltaPct: 30,
			MaxLimitDeltaPct:   50,
		},
	}

	result := Validate(p)
	assert.False(t, result.Valid)

	fields := make(map[string]bool)
	for _, e := range result.Errors {
		fields[e.Field] = true
	}
	assert.True(t, fields["audit.path"])
	assert.True(t, fields["audit.backend"])
}

func TestValidate_DeltaPercentBounds(t *testing.T) {
	tests := []struct {
		name      string
		request   int
		limit     int
		wantValid bool
	}{
		{"both zero", 0, 0, true},
		{"both max", 100, 100, true},
		{"request negative", -1, 50, false},
		{"request over 100", 101, 50, false},
		{"limit negative", 50, -1, false},
		{"limit over 100", 50, 101, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Policy{
				APIVersion: CurrentAPIVersion,
				Kind:       CurrentKind,
				Apply: ApplyConfig{
					MaxRequestDeltaPct: tt.request,
					MaxLimitDeltaPct:   tt.limit,
				},
			}

			result := Validate(p)
			if tt.wantValid {
				// No delta errors (may have other errors)
				for _, e := range result.Errors {
					assert.NotContains(t, e.Field, "delta_percent")
				}
			} else {
				assert.False(t, result.Valid)
			}
		})
	}
}

func TestValidate_InvalidDurations(t *testing.T) {
	tests := []struct {
		name  string
		field string
		value string
	}{
		{"bad min_latch_duration", "apply.min_latch_duration", "notaduration"},
		{"bad max_latch_age", "apply.max_latch_age", "xyz"},
		{"bad rate_window", "rate_limits.rate_window", "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Policy{
				APIVersion: CurrentAPIVersion,
				Kind:       CurrentKind,
			}

			switch tt.field {
			case "apply.min_latch_duration":
				p.Apply.MinLatchDuration = tt.value
			case "apply.max_latch_age":
				p.Apply.MaxLatchAge = tt.value
			case "rate_limits.rate_window":
				p.RateLimits.RateWindow = tt.value
			}

			result := Validate(p)
			assert.False(t, result.Valid)
			found := false
			for _, e := range result.Errors {
				if e.Field == tt.field {
					found = true
				}
			}
			assert.True(t, found, "expected error for field %s", tt.field)
		})
	}
}

func TestValidate_InvalidSafetyRating(t *testing.T) {
	p := &Policy{
		APIVersion: CurrentAPIVersion,
		Kind:       CurrentKind,
		Apply: ApplyConfig{
			MinSafetyRating: "INVALID",
		},
	}

	result := Validate(p)
	assert.False(t, result.Valid)
	found := false
	for _, e := range result.Errors {
		if e.Field == "apply.min_safety_rating" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestValidate_ValidSafetyRatings(t *testing.T) {
	for _, rating := range []string{"SAFE", "CAUTION", ""} {
		t.Run(rating, func(t *testing.T) {
			p := &Policy{
				APIVersion: CurrentAPIVersion,
				Kind:       CurrentKind,
				Apply: ApplyConfig{
					MinSafetyRating: rating,
				},
			}

			result := Validate(p)
			for _, e := range result.Errors {
				assert.NotEqual(t, "apply.min_safety_rating", e.Field)
			}
		})
	}
}

func TestValidate_UnsupportedAuditBackend(t *testing.T) {
	p := &Policy{
		APIVersion: CurrentAPIVersion,
		Kind:       CurrentKind,
		Audit: AuditConfig{
			Backend: "s3",
		},
	}

	result := Validate(p)
	assert.False(t, result.Valid)
	found := false
	for _, e := range result.Errors {
		if e.Field == "audit.backend" {
			found = true
			assert.Contains(t, e.Message, "unsupported backend")
		}
	}
	assert.True(t, found)
}

func TestValidate_NegativeRetentionDays(t *testing.T) {
	p := &Policy{
		APIVersion: CurrentAPIVersion,
		Kind:       CurrentKind,
		Audit: AuditConfig{
			RetentionDays: -1,
		},
	}

	result := Validate(p)
	assert.False(t, result.Valid)
	found := false
	for _, e := range result.Errors {
		if e.Field == "audit.retention_days" {
			found = true
		}
	}
	assert.True(t, found)
}

func TestValidate_NegativeRateLimits(t *testing.T) {
	p := &Policy{
		APIVersion: CurrentAPIVersion,
		Kind:       CurrentKind,
		RateLimits: RateConfig{
			MaxAppliesPerHour:     -1,
			MaxAppliesPerWorkload: -1,
		},
	}

	result := Validate(p)
	assert.False(t, result.Valid)

	fields := make(map[string]bool)
	for _, e := range result.Errors {
		fields[e.Field] = true
	}
	assert.True(t, fields["rate_limits.max_applies_per_hour"])
	assert.True(t, fields["rate_limits.max_applies_per_workload"])
}

func TestIsNamespaceDenied(t *testing.T) {
	t.Run("deny list only", func(t *testing.T) {
		p := &Policy{
			Namespaces: NSConfig{
				Deny: []string{"kube-system", "kube-public"},
			},
		}
		assert.True(t, p.IsNamespaceDenied("kube-system"))
		assert.True(t, p.IsNamespaceDenied("kube-public"))
		assert.False(t, p.IsNamespaceDenied("default"))
		assert.False(t, p.IsNamespaceDenied("production"))
	})

	t.Run("empty deny list", func(t *testing.T) {
		p := &Policy{}
		assert.False(t, p.IsNamespaceDenied("default"))
		assert.False(t, p.IsNamespaceDenied("kube-system"))
	})

	t.Run("allow list enforced", func(t *testing.T) {
		p := &Policy{
			Namespaces: NSConfig{
				Allow: []string{"staging", "production"},
			},
		}
		assert.False(t, p.IsNamespaceDenied("staging"))
		assert.False(t, p.IsNamespaceDenied("production"))
		assert.True(t, p.IsNamespaceDenied("default"))
		assert.True(t, p.IsNamespaceDenied("kube-system"))
	})

	t.Run("deny takes precedence over allow", func(t *testing.T) {
		p := &Policy{
			Namespaces: NSConfig{
				Deny:  []string{"kube-system"},
				Allow: []string{"kube-system", "production"},
			},
		}
		assert.True(t, p.IsNamespaceDenied("kube-system"))
		assert.False(t, p.IsNamespaceDenied("production"))
	})
}

func TestMinLatchDurationParsed(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{"empty returns default", "", time.Hour},
		{"valid duration", "30m", 30 * time.Minute},
		{"invalid returns default", "notaduration", time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Policy{Apply: ApplyConfig{MinLatchDuration: tt.value}}
			assert.Equal(t, tt.expected, p.MinLatchDurationParsed())
		})
	}
}

func TestMaxLatchAgeParsed(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{"empty returns default", "", 7 * 24 * time.Hour},
		{"valid days", "14d", 14 * 24 * time.Hour},
		{"valid hours", "48h", 48 * time.Hour},
		{"invalid returns default", "bad", 7 * 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Policy{Apply: ApplyConfig{MaxLatchAge: tt.value}}
			assert.Equal(t, tt.expected, p.MaxLatchAgeParsed())
		})
	}
}

func TestRateWindowParsed(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{"empty returns default", "", 24 * time.Hour},
		{"valid hours", "12h", 12 * time.Hour},
		{"valid days", "3d", 3 * 24 * time.Hour},
		{"invalid returns default", "bad", 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &Policy{RateLimits: RateConfig{RateWindow: tt.value}}
			assert.Equal(t, tt.expected, p.RateWindowParsed())
		})
	}
}

func TestValidate_NilPolicy(t *testing.T) {
	result := Validate(nil)
	assert.False(t, result.Valid)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "policy", result.Errors[0].Field)
}

func TestLoad_UnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")

	yaml := `apiVersion: kubenow/v1alpha1
kind: Policy
global:
  enabled: true
unknown_field: should_fail
`
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0644))

	result := Load(path)
	assert.Contains(t, result.ErrorMsg, "invalid YAML")
	assert.Nil(t, result.Policy)
}

func TestParseDurationWithDays(t *testing.T) {
	tests := []struct {
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{"1d", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"0.5d", 12 * time.Hour, false},
		{"1h", time.Hour, false},
		{"30m", 30 * time.Minute, false},
		{"bad", 0, true},
		{"xd", 0, true},
		{"-7d", 0, true},
		{"-1h", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseDurationWithDays(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, got)
			}
		})
	}
}

func TestCheckAuditPath(t *testing.T) {
	t.Run("valid writable directory", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, CheckAuditPath(dir))
	})

	t.Run("nonexistent path", func(t *testing.T) {
		err := CheckAuditPath("/nonexistent/audit/path")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("path is a file not directory", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "file.txt")
		require.NoError(t, os.WriteFile(f, []byte("x"), 0644))

		err := CheckAuditPath(f)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a directory")
	})
}

func TestValidationError_String(t *testing.T) {
	e := ValidationError{Field: "audit.path", Message: "required"}
	assert.Equal(t, "audit.path: required", e.String())
}

func TestResolvePath(t *testing.T) {
	t.Run("override takes precedence", func(t *testing.T) {
		assert.Equal(t, "/custom/path", resolvePath("/custom/path"))
	})

	t.Run("env var when no override", func(t *testing.T) {
		t.Setenv(EnvPolicyPath, "/env/path")
		assert.Equal(t, "/env/path", resolvePath(""))
	})

	t.Run("default when nothing set", func(t *testing.T) {
		t.Setenv(EnvPolicyPath, "")
		assert.Equal(t, DefaultPolicyPath, resolvePath(""))
	})
}
