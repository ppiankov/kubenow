package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeDecision(t *testing.T, dir string, decision DecisionJSON) {
	t.Helper()
	data, err := json.MarshalIndent(decision, "", "  ")
	if err != nil {
		t.Fatalf("marshal decision: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "decision.json"), data, 0o600); err != nil {
		t.Fatalf("write decision.json: %v", err)
	}
}

func TestParseBundleTimestamp(t *testing.T) {
	tests := []struct {
		name    string
		dirName string
		wantErr bool
		wantTS  time.Time
	}{
		{
			name:    "valid timestamp",
			dirName: "20260215T143000Z__default__deployment__payment-api",
			wantTS:  time.Date(2026, 2, 15, 14, 30, 0, 0, time.UTC),
		},
		{
			name:    "empty string",
			dirName: "",
			wantErr: true,
		},
		{
			name:    "no separators",
			dirName: "notadate",
			wantErr: true,
		},
		{
			name:    "partial timestamp",
			dirName: "20260215__ns__kind__name",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, err := parseBundleTimestamp(tt.dirName)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !ts.Equal(tt.wantTS) {
				t.Errorf("got %v, want %v", ts, tt.wantTS)
			}
		})
	}
}

func TestScanBundles_Empty(t *testing.T) {
	dir := t.TempDir()
	bundles, err := ScanBundles(ScanConfig{AuditPath: dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bundles) != 0 {
		t.Errorf("expected 0 bundles, got %d", len(bundles))
	}
}

func TestScanBundles_MixedStatuses(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)

	// Create applied bundle
	appliedDir := filepath.Join(root, "20260220T091500Z__prod__deployment__cart-svc")
	if err := os.MkdirAll(appliedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDecision(t, appliedDir, DecisionJSON{
		Status:   "applied",
		Workload: BundleWorkload{Kind: "Deployment", Name: "cart-svc", Namespace: "prod"},
	})

	// Create denied bundle
	deniedDir := filepath.Join(root, "20260221T100000Z__prod__deployment__denied-svc")
	if err := os.MkdirAll(deniedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDecision(t, deniedDir, DecisionJSON{
		Status:   "denied",
		Workload: BundleWorkload{Kind: "Deployment", Name: "denied-svc", Namespace: "prod"},
	})

	// Scan all
	all, err := ScanBundles(ScanConfig{AuditPath: root, Now: now})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 bundles, got %d", len(all))
	}

	// Scan applied only
	applied, err := ScanBundles(ScanConfig{AuditPath: root, Status: "applied", Now: now})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("expected 1 applied bundle, got %d", len(applied))
	}
	if applied[0].Decision.Status != "applied" {
		t.Errorf("expected applied status, got %q", applied[0].Decision.Status)
	}
}

func TestScanBundles_SinceFilter(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)

	// Old bundle (10 days ago)
	oldDir := filepath.Join(root, "20260218T120000Z__default__deployment__old-svc")
	if err := os.MkdirAll(oldDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDecision(t, oldDir, DecisionJSON{
		Status:   "applied",
		Workload: BundleWorkload{Kind: "Deployment", Name: "old-svc", Namespace: "default"},
	})

	// Recent bundle (1 day ago)
	recentDir := filepath.Join(root, "20260227T120000Z__default__deployment__recent-svc")
	if err := os.MkdirAll(recentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDecision(t, recentDir, DecisionJSON{
		Status:   "applied",
		Workload: BundleWorkload{Kind: "Deployment", Name: "recent-svc", Namespace: "default"},
	})

	// Since 7 days — should only return the recent one
	bundles, err := ScanBundles(ScanConfig{
		AuditPath: root,
		Since:     7 * 24 * time.Hour,
		Now:       now,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bundles) != 1 {
		t.Fatalf("expected 1 bundle within 7d, got %d", len(bundles))
	}
	if bundles[0].Decision.Workload.Name != "recent-svc" {
		t.Errorf("expected recent-svc, got %q", bundles[0].Decision.Workload.Name)
	}
}

func TestScanBundles_SortOrder(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)

	// Create 3 bundles in different order
	dirs := []struct {
		ts   string
		name string
	}{
		{"20260215T143000Z", "first"},
		{"20260225T090000Z", "middle"},
		{"20260227T080000Z", "last"},
	}
	for _, d := range dirs {
		dir := filepath.Join(root, d.ts+"__default__deployment__"+d.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		writeDecision(t, dir, DecisionJSON{
			Status:   "applied",
			Workload: BundleWorkload{Kind: "Deployment", Name: d.name, Namespace: "default"},
		})
	}

	bundles, err := ScanBundles(ScanConfig{AuditPath: root, Now: now})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bundles) != 3 {
		t.Fatalf("expected 3 bundles, got %d", len(bundles))
	}

	// Newest first
	if bundles[0].Decision.Workload.Name != "last" {
		t.Errorf("expected newest first (last), got %q", bundles[0].Decision.Workload.Name)
	}
	if bundles[2].Decision.Workload.Name != "first" {
		t.Errorf("expected oldest last (first), got %q", bundles[2].Decision.Workload.Name)
	}
}

func TestScanBundles_MalformedSkipped(t *testing.T) {
	root := t.TempDir()

	// Valid bundle
	validDir := filepath.Join(root, "20260225T090000Z__default__deployment__valid")
	if err := os.MkdirAll(validDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeDecision(t, validDir, DecisionJSON{
		Status:   "applied",
		Workload: BundleWorkload{Kind: "Deployment", Name: "valid", Namespace: "default"},
	})

	// Malformed JSON bundle
	malformedDir := filepath.Join(root, "20260226T090000Z__default__deployment__bad")
	if err := os.MkdirAll(malformedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(malformedDir, "decision.json"), []byte("{bad json"), 0o600); err != nil {
		t.Fatal(err)
	}

	// No decision.json bundle
	noDecisionDir := filepath.Join(root, "20260226T100000Z__default__deployment__nodec")
	if err := os.MkdirAll(noDecisionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Invalid timestamp dir
	invalidTSDir := filepath.Join(root, "not-a-timestamp")
	if err := os.MkdirAll(invalidTSDir, 0o755); err != nil {
		t.Fatal(err)
	}

	bundles, err := ScanBundles(ScanConfig{AuditPath: root})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bundles) != 1 {
		t.Fatalf("expected 1 valid bundle, got %d", len(bundles))
	}
	if bundles[0].Decision.Workload.Name != "valid" {
		t.Errorf("expected 'valid', got %q", bundles[0].Decision.Workload.Name)
	}
}

func TestScanBundles_NonexistentDir(t *testing.T) {
	_, err := ScanBundles(ScanConfig{AuditPath: "/nonexistent/path"})
	if err == nil {
		t.Error("expected error for nonexistent path, got nil")
	}
}
