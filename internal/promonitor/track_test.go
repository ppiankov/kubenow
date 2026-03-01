package promonitor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ppiankov/kubenow/internal/audit"
	"github.com/ppiankov/kubenow/internal/metrics"
)

// --- Resource parser tests ---

func TestParseCPUResource(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"150m", 0.15},
		{"1", 1.0},
		{"1000m", 1.0},
		{"0m", 0},
		{"500m", 0.5},
		{"2", 2.0},
		{"", 0},
		{"bad", 0},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseCPUResource(tt.input)
			if got != tt.want {
				t.Errorf("parseCPUResource(%q) = %f, want %f", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseMemResource(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"200Mi", 200 * 1024 * 1024},
		{"1Gi", 1024 * 1024 * 1024},
		{"0", 0},
		{"", 0},
		{"512Ki", 512 * 1024},
		{"1048576", 1048576},
		{"bad", 0},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseMemResource(tt.input)
			if got != tt.want {
				t.Errorf("parseMemResource(%q) = %f, want %f", tt.input, got, tt.want)
			}
		})
	}
}

// --- Classification tests ---

func makeDecision(appliedAt time.Time, changes []audit.BundleChange) *audit.DecisionJSON {
	return &audit.DecisionJSON{
		Status:    "applied",
		Timestamp: appliedAt.Format(time.RFC3339),
		AppliedAt: appliedAt.Format(time.RFC3339),
		Workload: audit.BundleWorkload{
			Kind:      "Deployment",
			Name:      "test-svc",
			Namespace: "default",
		},
		Recommendation: audit.DecisionRec{
			Safety:     "SAFE",
			Confidence: "HIGH",
		},
		Changes: changes,
	}
}

func defaultChanges() []audit.BundleChange {
	return []audit.BundleChange{
		{Field: "app/cpu_request", Before: "500m", After: "200m"},
		{Field: "app/cpu_limit", Before: "1", After: "400m"},
		{Field: "app/memory_request", Before: "512Mi", After: "256Mi"},
		{Field: "app/memory_limit", Before: "1Gi", After: "512Mi"},
	}
}

func TestClassifyOutcome_Pending(t *testing.T) {
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	appliedAt := now.Add(-12 * time.Hour) // 12h ago, within 24h window

	result := ClassifyOutcome(&TrackInput{
		Decision: makeDecision(appliedAt, defaultChanges()),
		Usage:    &metrics.WorkloadUsage{CPUP95: 0.05, MemoryP95: 50 * 1024 * 1024},
		Now:      now,
	})

	if result.Outcome != OutcomePending {
		t.Errorf("expected PENDING, got %s", result.Outcome)
	}
}

func TestClassifyOutcome_NoData(t *testing.T) {
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	appliedAt := now.Add(-48 * time.Hour)

	result := ClassifyOutcome(&TrackInput{
		Decision: makeDecision(appliedAt, defaultChanges()),
		Usage:    nil,
		Now:      now,
	})

	if result.Outcome != OutcomeNoData {
		t.Errorf("expected NO_DATA, got %s", result.Outcome)
	}
}

func TestClassifyOutcome_Safe(t *testing.T) {
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	appliedAt := now.Add(-48 * time.Hour)

	// CPU: 0.08 / 0.2 = 40%, Memory: 160Mi / 256Mi ≈ 62.5%
	result := ClassifyOutcome(&TrackInput{
		Decision: makeDecision(appliedAt, defaultChanges()),
		Usage: &metrics.WorkloadUsage{
			CPUP95:    0.08,
			MemoryP95: 160 * 1024 * 1024,
			MemoryMax: 200 * 1024 * 1024,
		},
		Now: now,
	})

	if result.Outcome != OutcomeSafe {
		t.Errorf("expected SAFE, got %s (cpu=%.1f%%, mem=%.1f%%)", result.Outcome, result.CPUPeakPct, result.MemPeakPct)
	}
}

func TestClassifyOutcome_Tight(t *testing.T) {
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	appliedAt := now.Add(-48 * time.Hour)

	// CPU: 0.17 / 0.2 = 85%, Memory: 128Mi / 256Mi = 50%
	result := ClassifyOutcome(&TrackInput{
		Decision: makeDecision(appliedAt, defaultChanges()),
		Usage: &metrics.WorkloadUsage{
			CPUP95:    0.17,
			MemoryP95: 128 * 1024 * 1024,
			MemoryMax: 200 * 1024 * 1024,
		},
		Now: now,
	})

	if result.Outcome != OutcomeTight {
		t.Errorf("expected TIGHT, got %s (cpu=%.1f%%, mem=%.1f%%)", result.Outcome, result.CPUPeakPct, result.MemPeakPct)
	}
}

func TestClassifyOutcome_Wrong(t *testing.T) {
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	appliedAt := now.Add(-48 * time.Hour)

	// CPU: 0.192 / 0.2 = 96%, Memory: 128Mi / 256Mi = 50%
	result := ClassifyOutcome(&TrackInput{
		Decision: makeDecision(appliedAt, defaultChanges()),
		Usage: &metrics.WorkloadUsage{
			CPUP95:    0.192,
			MemoryP95: 128 * 1024 * 1024,
			MemoryMax: 200 * 1024 * 1024,
		},
		Now: now,
	})

	if result.Outcome != OutcomeWrong {
		t.Errorf("expected WRONG, got %s (cpu=%.1f%%, mem=%.1f%%)", result.Outcome, result.CPUPeakPct, result.MemPeakPct)
	}
}

func TestClassifyOutcome_WrongOOM(t *testing.T) {
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	appliedAt := now.Add(-48 * time.Hour)

	// Memory max exceeds limit (512Mi)
	result := ClassifyOutcome(&TrackInput{
		Decision: makeDecision(appliedAt, defaultChanges()),
		Usage: &metrics.WorkloadUsage{
			CPUP95:    0.05,
			MemoryP95: 100 * 1024 * 1024,
			MemoryMax: 600 * 1024 * 1024, // exceeds 512Mi limit
		},
		Now: now,
	})

	if result.Outcome != OutcomeWrong {
		t.Errorf("expected WRONG (OOM), got %s", result.Outcome)
	}
	if result.Reason != "memory peak exceeds limit (OOM risk)" {
		t.Errorf("expected OOM reason, got %q", result.Reason)
	}
}

func TestClassifyOutcome_BoundaryValues(t *testing.T) {
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	appliedAt := now.Add(-48 * time.Hour)

	tests := []struct {
		name   string
		cpuP95 float64
		want   TrackOutcome
	}{
		// 200m request → threshold values
		{"79.9% → SAFE", 0.1598, OutcomeSafe},   // 0.1598 / 0.2 = 79.9%
		{"80.0% → TIGHT", 0.16, OutcomeTight},   // 0.16 / 0.2 = 80%
		{"94.9% → TIGHT", 0.1898, OutcomeTight}, // 0.1898 / 0.2 = 94.9%
		{"95.0% → WRONG", 0.19, OutcomeWrong},   // 0.19 / 0.2 = 95%
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyOutcome(&TrackInput{
				Decision: makeDecision(appliedAt, defaultChanges()),
				Usage: &metrics.WorkloadUsage{
					CPUP95:    tt.cpuP95,
					MemoryP95: 50 * 1024 * 1024, // low memory to isolate CPU test
					MemoryMax: 100 * 1024 * 1024,
				},
				Now: now,
			})
			if result.Outcome != tt.want {
				t.Errorf("got %s, want %s (cpu=%.1f%%)", result.Outcome, tt.want, result.CPUPeakPct)
			}
		})
	}
}

func TestClassifyOutcome_MultiContainer(t *testing.T) {
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	appliedAt := now.Add(-48 * time.Hour)

	changes := []audit.BundleChange{
		{Field: "app/cpu_request", Before: "500m", After: "200m"},
		{Field: "app/memory_request", Before: "512Mi", After: "256Mi"},
		{Field: "app/memory_limit", Before: "1Gi", After: "512Mi"},
		{Field: "sidecar/cpu_request", Before: "200m", After: "100m"},
		{Field: "sidecar/memory_request", Before: "256Mi", After: "128Mi"},
		{Field: "sidecar/memory_limit", Before: "512Mi", After: "256Mi"},
	}

	// Total CPU request: 200m + 100m = 300m
	// CPU P95: 0.15 → 0.15 / 0.3 = 50% → SAFE
	result := ClassifyOutcome(&TrackInput{
		Decision: makeDecision(appliedAt, changes),
		Usage: &metrics.WorkloadUsage{
			CPUP95:    0.15,
			MemoryP95: 200 * 1024 * 1024,
			MemoryMax: 300 * 1024 * 1024,
		},
		Now: now,
	})

	if result.Outcome != OutcomeSafe {
		t.Errorf("expected SAFE for multi-container, got %s (cpu=%.1f%%)", result.Outcome, result.CPUPeakPct)
	}
}

func TestClassifyOutcome_ZeroRequest(t *testing.T) {
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	appliedAt := now.Add(-48 * time.Hour)

	changes := []audit.BundleChange{
		{Field: "app/cpu_request", Before: "0m", After: "0m"},
		{Field: "app/memory_request", Before: "0", After: "0"},
		{Field: "app/memory_limit", Before: "0", After: "0"},
	}

	result := ClassifyOutcome(&TrackInput{
		Decision: makeDecision(appliedAt, changes),
		Usage:    &metrics.WorkloadUsage{CPUP95: 0.1, MemoryP95: 100},
		Now:      now,
	})

	// With zero requests, percentages are 0, which is < 80% → SAFE
	if result.Outcome != OutcomeSafe {
		t.Errorf("expected SAFE for zero requests, got %s", result.Outcome)
	}
}

// --- Formatter tests ---

func TestFormatTrackTable(t *testing.T) {
	summary := &TrackSummary{
		Results: []TrackResult{
			{
				Workload:   WorkloadRef{Kind: "Deployment", Name: "test-svc", Namespace: "default"},
				AppliedAt:  time.Date(2026, 2, 15, 14, 30, 0, 0, time.UTC),
				Outcome:    OutcomeSafe,
				CPUPeakPct: 42.0,
				MemPeakPct: 61.0,
			},
			{
				Workload:  WorkloadRef{Kind: "Deployment", Name: "pending-svc", Namespace: "default"},
				AppliedAt: time.Date(2026, 2, 28, 8, 0, 0, 0, time.UTC),
				Outcome:   OutcomePending,
			},
		},
		TotalApplied: 2,
		Safe:         1,
		Pending:      1,
	}

	output := FormatTrackTable(summary)
	if output == "" {
		t.Fatal("expected non-empty table output")
	}
	if !contains(output, "RECOMMENDATION HISTORY") {
		t.Error("missing header")
	}
	if !contains(output, "deployment/test-svc") {
		t.Error("missing workload name")
	}
	if !contains(output, "SAFE") {
		t.Error("missing SAFE outcome")
	}
	if !contains(output, "PENDING") {
		t.Error("missing PENDING outcome")
	}
	if !contains(output, "Score: 2 applied") {
		t.Error("missing score line")
	}
}

func TestFormatTrackJSON(t *testing.T) {
	summary := &TrackSummary{
		Results:      []TrackResult{},
		TotalApplied: 0,
		ScannedAt:    time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC),
	}

	output, err := FormatTrackJSON(summary)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output == "" {
		t.Fatal("expected non-empty JSON output")
	}

	// Verify it's valid JSON
	var parsed TrackSummary
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func contains(s, substr string) bool {
	return s != "" && substr != "" && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- Integration test ---

func TestRunTrack_Integration(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)

	// Create applied bundle (2 days ago)
	appliedAt := now.Add(-48 * time.Hour)
	bundleDir := filepath.Join(root, "20260226T120000Z__default__deployment__payment-api")
	if err := os.MkdirAll(bundleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	decision := &audit.DecisionJSON{
		Status:    "applied",
		Timestamp: appliedAt.Format(time.RFC3339),
		AppliedAt: appliedAt.Format(time.RFC3339),
		Workload: audit.BundleWorkload{
			Kind:      "Deployment",
			Name:      "payment-api",
			Namespace: "default",
		},
		Recommendation: audit.DecisionRec{Safety: "SAFE", Confidence: "HIGH"},
		Changes: []audit.BundleChange{
			{Field: "app/cpu_request", Before: "1", After: "500m"},
			{Field: "app/memory_request", Before: "1Gi", After: "512Mi"},
			{Field: "app/memory_limit", Before: "2Gi", After: "1Gi"},
		},
	}
	data, _ := json.MarshalIndent(decision, "", "  ")
	if err := os.WriteFile(filepath.Join(bundleDir, "decision.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	// Create pending bundle (12 hours ago)
	pendingAt := now.Add(-12 * time.Hour)
	pendingDir := filepath.Join(root, "20260228T000000Z__default__deployment__cart-svc")
	if err := os.MkdirAll(pendingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pendingDecision := &audit.DecisionJSON{
		Status:    "applied",
		Timestamp: pendingAt.Format(time.RFC3339),
		AppliedAt: pendingAt.Format(time.RFC3339),
		Workload: audit.BundleWorkload{
			Kind:      "Deployment",
			Name:      "cart-svc",
			Namespace: "default",
		},
		Recommendation: audit.DecisionRec{Safety: "SAFE", Confidence: "HIGH"},
		Changes: []audit.BundleChange{
			{Field: "app/cpu_request", Before: "500m", After: "200m"},
			{Field: "app/memory_request", Before: "512Mi", After: "256Mi"},
			{Field: "app/memory_limit", Before: "1Gi", After: "512Mi"},
		},
	}
	pendingData, _ := json.MarshalIndent(pendingDecision, "", "  ")
	if err := os.WriteFile(filepath.Join(pendingDir, "decision.json"), pendingData, 0o600); err != nil {
		t.Fatal(err)
	}

	// Mock metrics: payment-api at 42% CPU, 61% memory
	mock := metrics.NewMockMetrics()
	mock.AddWorkloadUsage("default", "payment-api", &metrics.WorkloadUsage{
		CPUP95:    0.21,              // 0.21 / 0.5 = 42%
		MemoryP95: 325 * 1024 * 1024, // 325Mi / 512Mi ≈ 63.5%
		MemoryMax: 400 * 1024 * 1024, // 400Mi < 1Gi limit
	})

	summary, err := RunTrack(context.Background(), &TrackConfig{
		AuditPath: root,
		Metrics:   mock,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("RunTrack error: %v", err)
	}

	if summary.TotalApplied != 2 {
		t.Errorf("expected 2 applied, got %d", summary.TotalApplied)
	}
	if summary.Safe != 1 {
		t.Errorf("expected 1 SAFE, got %d", summary.Safe)
	}
	if summary.Pending != 1 {
		t.Errorf("expected 1 PENDING, got %d", summary.Pending)
	}
}

func TestRunTrack_WorkloadFilter(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	appliedAt := now.Add(-48 * time.Hour)

	// Create two bundles
	for _, name := range []string{"svc-a", "svc-b"} {
		dir := filepath.Join(root, "20260226T120000Z__default__deployment__"+name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		d := &audit.DecisionJSON{
			Status:    "applied",
			AppliedAt: appliedAt.Format(time.RFC3339),
			Timestamp: appliedAt.Format(time.RFC3339),
			Workload:  audit.BundleWorkload{Kind: "Deployment", Name: name, Namespace: "default"},
			Changes: []audit.BundleChange{
				{Field: "app/cpu_request", Before: "500m", After: "200m"},
				{Field: "app/memory_request", Before: "512Mi", After: "256Mi"},
				{Field: "app/memory_limit", Before: "1Gi", After: "512Mi"},
			},
		}
		data, _ := json.MarshalIndent(d, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "decision.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	summary, err := RunTrack(context.Background(), &TrackConfig{
		AuditPath:      root,
		WorkloadFilter: &WorkloadRef{Kind: "Deployment", Name: "svc-a"},
		Now:            now,
	})
	if err != nil {
		t.Fatalf("RunTrack error: %v", err)
	}

	if summary.TotalApplied != 1 {
		t.Errorf("expected 1 filtered result, got %d", summary.TotalApplied)
	}
}

func TestRunTrack_NoMetrics(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	appliedAt := now.Add(-48 * time.Hour)

	dir := filepath.Join(root, "20260226T120000Z__default__deployment__no-metrics-svc")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	d := &audit.DecisionJSON{
		Status:    "applied",
		AppliedAt: appliedAt.Format(time.RFC3339),
		Timestamp: appliedAt.Format(time.RFC3339),
		Workload:  audit.BundleWorkload{Kind: "Deployment", Name: "no-metrics-svc", Namespace: "default"},
		Changes: []audit.BundleChange{
			{Field: "app/cpu_request", Before: "500m", After: "200m"},
			{Field: "app/memory_request", Before: "512Mi", After: "256Mi"},
			{Field: "app/memory_limit", Before: "1Gi", After: "512Mi"},
		},
	}
	data, _ := json.MarshalIndent(d, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "decision.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	summary, err := RunTrack(context.Background(), &TrackConfig{
		AuditPath: root,
		Metrics:   nil, // no Prometheus
		Now:       now,
	})
	if err != nil {
		t.Fatalf("RunTrack error: %v", err)
	}

	if summary.NoData != 1 {
		t.Errorf("expected 1 NO_DATA, got %d", summary.NoData)
	}
}
