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

func testRateLimitConfig(t *testing.T, maxGlobal, maxPerWorkload int) RateLimitConfig {
	t.Helper()
	return RateLimitConfig{
		MaxGlobal:      maxGlobal,
		MaxPerWorkload: maxPerWorkload,
		Window:         time.Hour,
		AuditPath:      t.TempDir(),
	}
}

func TestCheckAndIncrement_FirstApply(t *testing.T) {
	cfg := testRateLimitConfig(t, 10, 5)

	result, err := CheckAndIncrement(cfg, "uid-123", "default/deployment/api", "admin")
	require.NoError(t, err)
	assert.True(t, result.Allowed)

	// Verify files were created
	globalPath := filepath.Join(cfg.AuditPath, ".ratelimit", "cluster.json")
	data, err := os.ReadFile(globalPath)
	require.NoError(t, err)

	var state RateLimitState
	require.NoError(t, json.Unmarshal(data, &state))
	assert.Equal(t, 1, state.Count)
	require.Len(t, state.Entries, 1)
	assert.Equal(t, "default/deployment/api", state.Entries[0].Workload)
}

func TestCheckAndIncrement_AtLimit(t *testing.T) {
	cfg := testRateLimitConfig(t, 3, 0)

	// First 3 should succeed
	for i := 0; i < 3; i++ {
		result, err := CheckAndIncrement(cfg, "uid-123", "default/deployment/api", "admin")
		require.NoError(t, err)
		assert.True(t, result.Allowed, "apply %d should be allowed", i+1)
	}

	// 4th should be denied
	result, err := CheckAndIncrement(cfg, "uid-123", "default/deployment/api", "admin")
	require.NoError(t, err)
	assert.False(t, result.Allowed)
	assert.Contains(t, result.DenialReason, "global rate limit exceeded")
}

func TestCheckAndIncrement_WindowExpired(t *testing.T) {
	cfg := testRateLimitConfig(t, 2, 0)

	// Fill up the global counter
	for i := 0; i < 2; i++ {
		result, err := CheckAndIncrement(cfg, "uid-123", "default/deployment/api", "admin")
		require.NoError(t, err)
		assert.True(t, result.Allowed)
	}

	// Verify at limit
	result, err := CheckAndIncrement(cfg, "uid-123", "default/deployment/api", "admin")
	require.NoError(t, err)
	assert.False(t, result.Allowed)

	// Manually backdate the window start so it expires
	globalPath := filepath.Join(cfg.AuditPath, ".ratelimit", "cluster.json")
	data, err := os.ReadFile(globalPath)
	require.NoError(t, err)

	var state RateLimitState
	require.NoError(t, json.Unmarshal(data, &state))
	state.WindowStart = time.Now().Add(-2 * time.Hour) // expired
	updatedData, err := json.Marshal(state)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(globalPath, updatedData, 0644))

	// Should succeed now (window reset)
	result, err = CheckAndIncrement(cfg, "uid-123", "default/deployment/api", "admin")
	require.NoError(t, err)
	assert.True(t, result.Allowed)
}

func TestCheckAndIncrement_GlobalLimit(t *testing.T) {
	cfg := testRateLimitConfig(t, 1, 10)

	// First apply succeeds
	result, err := CheckAndIncrement(cfg, "uid-123", "default/deployment/api", "admin")
	require.NoError(t, err)
	assert.True(t, result.Allowed)

	// Second apply: different workload but global limit hit
	result, err = CheckAndIncrement(cfg, "uid-456", "default/deployment/web", "admin")
	require.NoError(t, err)
	assert.False(t, result.Allowed)
	assert.Contains(t, result.DenialReason, "global rate limit exceeded")
}

func TestCheckAndIncrement_PerWorkloadLimit(t *testing.T) {
	cfg := testRateLimitConfig(t, 0, 1)

	// First apply to workload A succeeds
	result, err := CheckAndIncrement(cfg, "uid-123", "default/deployment/api", "admin")
	require.NoError(t, err)
	assert.True(t, result.Allowed)

	// Second apply to workload A: per-workload limit hit
	result, err = CheckAndIncrement(cfg, "uid-123", "default/deployment/api", "admin")
	require.NoError(t, err)
	assert.False(t, result.Allowed)
	assert.Contains(t, result.DenialReason, "per-workload rate limit exceeded")

	// Apply to workload B should still succeed
	result, err = CheckAndIncrement(cfg, "uid-456", "default/deployment/web", "admin")
	require.NoError(t, err)
	assert.True(t, result.Allowed)
}

func TestCheckAndIncrement_UnlimitedZero(t *testing.T) {
	cfg := testRateLimitConfig(t, 0, 0)

	// Should always succeed with max=0
	for i := 0; i < 100; i++ {
		result, err := CheckAndIncrement(cfg, "uid-123", "default/deployment/api", "admin")
		require.NoError(t, err)
		assert.True(t, result.Allowed)
	}
}
