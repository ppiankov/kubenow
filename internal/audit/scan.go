package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ScannedBundle pairs a parsed decision record with its directory path.
type ScannedBundle struct {
	Decision  DecisionJSON
	Dir       string
	timestamp time.Time // cached from directory name, used for sorting
}

// ScanConfig controls which audit bundles are returned.
type ScanConfig struct {
	AuditPath string        // root directory containing bundle subdirectories
	Status    string        // filter: "applied", "denied", "" = all
	Since     time.Duration // 0 = all bundles; >0 = only bundles newer than Now-Since
	Now       time.Time     // injected for testability
}

// ScanBundles reads audit bundle directories under AuditPath, parses each
// decision.json, filters by status and age, and returns results newest-first.
// Malformed bundles are skipped with a warning to stderr.
func ScanBundles(cfg ScanConfig) ([]ScannedBundle, error) {
	entries, err := os.ReadDir(cfg.AuditPath)
	if err != nil {
		return nil, fmt.Errorf("read audit directory %q: %w", cfg.AuditPath, err)
	}

	var bundles []ScannedBundle

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirName := entry.Name()
		dirPath := filepath.Join(cfg.AuditPath, dirName)

		// Parse timestamp from directory name
		ts, tsErr := parseBundleTimestamp(dirName)
		if tsErr != nil {
			fmt.Fprintf(os.Stderr, "[scan] skipping %q: %v\n", dirName, tsErr)
			continue
		}

		// Apply age filter
		if cfg.Since > 0 && !cfg.Now.IsZero() {
			cutoff := cfg.Now.Add(-cfg.Since)
			if ts.Before(cutoff) {
				continue
			}
		}

		// Read decision.json
		decisionPath := filepath.Join(dirPath, "decision.json")
		data, readErr := os.ReadFile(decisionPath)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "[scan] skipping %q: %v\n", dirName, readErr)
			continue
		}

		var decision DecisionJSON
		if jsonErr := json.Unmarshal(data, &decision); jsonErr != nil {
			fmt.Fprintf(os.Stderr, "[scan] skipping %q: malformed decision.json: %v\n", dirName, jsonErr)
			continue
		}

		// Apply status filter
		if cfg.Status != "" && decision.Status != cfg.Status {
			continue
		}

		bundles = append(bundles, ScannedBundle{
			Decision:  decision,
			Dir:       dirPath,
			timestamp: ts,
		})
	}

	// Sort newest-first by cached timestamp
	sort.Slice(bundles, func(i, j int) bool {
		return bundles[i].timestamp.After(bundles[j].timestamp)
	})

	return bundles, nil
}

// bundleTimestampLayout is the Go time layout matching the audit directory
// timestamp prefix: 20060102T150405Z (UTC, no separators).
const bundleTimestampLayout = "20060102T150405Z"

// parseBundleTimestamp extracts the UTC timestamp from a bundle directory name.
// Expected format: 20060102T150405Z__namespace__kind__name
func parseBundleTimestamp(dirName string) (time.Time, error) {
	parts := strings.SplitN(dirName, "__", 2)
	if len(parts) < 1 || parts[0] == "" {
		return time.Time{}, fmt.Errorf("no timestamp prefix in %q", dirName)
	}

	ts, err := time.Parse(bundleTimestampLayout, parts[0])
	if err != nil {
		return time.Time{}, fmt.Errorf("parse timestamp %q: %w", parts[0], err)
	}
	return ts, nil
}
