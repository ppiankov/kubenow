package promonitor

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ppiankov/kubenow/internal/audit"
	"github.com/ppiankov/kubenow/internal/metrics"
)

// Classification thresholds for post-apply outcome assessment.
const (
	pendingWindow      = 24 * time.Hour
	safePeakThreshold  = 80.0 // peak < 80% of request → SAFE
	tightPeakThreshold = 95.0 // peak 80-95% → TIGHT, >95% → WRONG
)

// TrackOutcome represents the classification of a post-apply observation.
type TrackOutcome string

// TrackOutcome classification values.
const (
	OutcomeSafe    TrackOutcome = "SAFE"
	OutcomeTight   TrackOutcome = "TIGHT"
	OutcomeWrong   TrackOutcome = "WRONG"
	OutcomePending TrackOutcome = "PENDING"
	OutcomeNoData  TrackOutcome = "NO_DATA"
)

// TrackInput holds all inputs needed to classify an apply outcome.
type TrackInput struct {
	Decision *audit.DecisionJSON
	Usage    *metrics.WorkloadUsage // nil if no Prometheus
	Now      time.Time
}

// TrackResult is the classification output for a single apply.
type TrackResult struct {
	Workload   WorkloadRef          `json:"workload"`
	AppliedAt  time.Time            `json:"applied_at"`
	Outcome    TrackOutcome         `json:"outcome"`
	Safety     string               `json:"safety"`
	Confidence string               `json:"confidence"`
	CPUPeakPct float64              `json:"cpu_peak_pct"`
	MemPeakPct float64              `json:"mem_peak_pct"`
	AuditDir   string               `json:"audit_dir"`
	Changes    []audit.BundleChange `json:"changes"`
	Reason     string               `json:"reason,omitempty"`
}

// TrackSummary aggregates results across all scanned applies.
type TrackSummary struct {
	Results      []TrackResult `json:"results"`
	TotalApplied int           `json:"total_applied"`
	Safe         int           `json:"safe"`
	Tight        int           `json:"tight"`
	Wrong        int           `json:"wrong"`
	Pending      int           `json:"pending"`
	NoData       int           `json:"no_data"`
	ScannedAt    time.Time     `json:"scanned_at"`
}

// TrackConfig controls the RunTrack orchestration.
type TrackConfig struct {
	AuditPath      string
	Metrics        metrics.MetricsProvider // nil = no Prometheus
	Since          time.Duration
	WorkloadFilter *WorkloadRef // nil = all
	Now            time.Time
}

// ClassifyOutcome is a pure function that determines the post-apply outcome
// for a single workload based on its decision record and current usage.
func ClassifyOutcome(input *TrackInput) *TrackResult {
	d := input.Decision
	result := &TrackResult{
		Workload: WorkloadRef{
			Kind:      d.Workload.Kind,
			Name:      d.Workload.Name,
			Namespace: d.Workload.Namespace,
		},
		Safety:     d.Recommendation.Safety,
		Confidence: d.Recommendation.Confidence,
		Changes:    d.Changes,
	}

	// Parse applied_at timestamp
	appliedAt, err := time.Parse(time.RFC3339, d.AppliedAt)
	if err != nil {
		// Fallback: use decision timestamp; if that also fails, appliedAt stays zero
		if ts, tsErr := time.Parse(time.RFC3339, d.Timestamp); tsErr == nil {
			appliedAt = ts
		}
	}
	result.AppliedAt = appliedAt

	// 1. PENDING if less than 24h since apply
	if input.Now.Sub(appliedAt) < pendingWindow {
		result.Outcome = OutcomePending
		result.Reason = "less than 24h since apply"
		return result
	}

	// 2. NO_DATA if no usage metrics
	if input.Usage == nil {
		result.Outcome = OutcomeNoData
		result.Reason = "no Prometheus metrics available"
		return result
	}

	// 3. Extract new resource values from changes
	newCPURequest := extractNewRequest(d.Changes, "cpu_request")
	newMemRequest := extractNewRequest(d.Changes, "memory_request")
	newMemLimit := extractNewRequest(d.Changes, "memory_limit")

	// 4. Compute peak percentages
	var cpuPeakPct, memPeakPct float64
	if newCPURequest > 0 {
		cpuPeakPct = input.Usage.CPUP95 / newCPURequest * 100
	}
	if newMemRequest > 0 {
		memPeakPct = input.Usage.MemoryP95 / newMemRequest * 100
	}
	result.CPUPeakPct = math.Round(cpuPeakPct*10) / 10
	result.MemPeakPct = math.Round(memPeakPct*10) / 10

	// 5. OOM risk: memory max exceeds limit
	if newMemLimit > 0 && input.Usage.MemoryMax > newMemLimit {
		result.Outcome = OutcomeWrong
		result.Reason = "memory peak exceeds limit (OOM risk)"
		return result
	}

	// 6. Classify by worst peak percentage
	worst := cpuPeakPct
	if memPeakPct > worst {
		worst = memPeakPct
	}

	switch {
	case worst < safePeakThreshold:
		result.Outcome = OutcomeSafe
	case worst < tightPeakThreshold:
		result.Outcome = OutcomeTight
		result.Reason = fmt.Sprintf("peak usage at %.0f%% of request", worst)
	default:
		result.Outcome = OutcomeWrong
		result.Reason = fmt.Sprintf("peak usage at %.0f%% of request", worst)
	}

	return result
}

// extractNewRequest sums the "After" values across all containers for a given
// field suffix (e.g., "cpu_request"). The field pattern is "{container}/{suffix}".
func extractNewRequest(changes []audit.BundleChange, fieldSuffix string) float64 {
	var total float64
	for _, c := range changes {
		if strings.HasSuffix(c.Field, "/"+fieldSuffix) {
			if strings.Contains(fieldSuffix, "memory") {
				total += parseMemResource(c.After)
			} else {
				total += parseCPUResource(c.After)
			}
		}
	}
	return total
}

// parseCPUResource converts a K8s CPU resource string to cores.
// Inverse of formatCPUResource in export.go.
// Examples: "150m" → 0.15, "1" → 1.0, "0m" → 0
func parseCPUResource(s string) float64 {
	if s == "" {
		return 0
	}
	if strings.HasSuffix(s, "m") {
		s = strings.TrimSuffix(s, "m")
		var m float64
		if _, err := fmt.Sscanf(s, "%f", &m); err != nil {
			return 0
		}
		return m / 1000
	}
	var cores float64
	if _, err := fmt.Sscanf(s, "%f", &cores); err != nil {
		return 0
	}
	return cores
}

// parseMemResource converts a K8s memory resource string to bytes.
// Inverse of formatMemResource in export.go.
// Examples: "200Mi" → 209715200, "1Gi" → 1073741824, "0" → 0
func parseMemResource(s string) float64 {
	if s == "" || s == "0" {
		return 0
	}
	if strings.HasSuffix(s, "Gi") {
		s = strings.TrimSuffix(s, "Gi")
		var gi float64
		if _, err := fmt.Sscanf(s, "%f", &gi); err != nil {
			return 0
		}
		return gi * 1024 * 1024 * 1024
	}
	if strings.HasSuffix(s, "Mi") {
		s = strings.TrimSuffix(s, "Mi")
		var mi float64
		if _, err := fmt.Sscanf(s, "%f", &mi); err != nil {
			return 0
		}
		return mi * 1024 * 1024
	}
	if strings.HasSuffix(s, "Ki") {
		s = strings.TrimSuffix(s, "Ki")
		var ki float64
		if _, err := fmt.Sscanf(s, "%f", &ki); err != nil {
			return 0
		}
		return ki * 1024
	}
	// Plain bytes
	var b float64
	if _, err := fmt.Sscanf(s, "%f", &b); err != nil {
		return 0
	}
	return b
}

// RunTrack orchestrates the full tracking workflow: scan audit bundles,
// query Prometheus for post-apply usage, classify each apply.
func RunTrack(ctx context.Context, cfg *TrackConfig) (*TrackSummary, error) {
	bundles, err := audit.ScanBundles(audit.ScanConfig{
		AuditPath: cfg.AuditPath,
		Status:    "applied",
		Since:     cfg.Since,
		Now:       cfg.Now,
	})
	if err != nil {
		return nil, fmt.Errorf("scan audit bundles: %w", err)
	}

	summary := &TrackSummary{
		ScannedAt: cfg.Now,
	}

	for i := range bundles {
		b := &bundles[i]
		// Apply workload filter
		if cfg.WorkloadFilter != nil {
			if !strings.EqualFold(b.Decision.Workload.Kind, cfg.WorkloadFilter.Kind) ||
				b.Decision.Workload.Name != cfg.WorkloadFilter.Name {
				continue
			}
		}

		// Query usage from Prometheus if available
		var usage *metrics.WorkloadUsage
		if cfg.Metrics != nil {
			u, queryErr := cfg.Metrics.GetWorkloadResourceUsage(
				ctx,
				b.Decision.Workload.Namespace,
				b.Decision.Workload.Name,
				b.Decision.Workload.Kind,
				pendingWindow,
			)
			if queryErr == nil {
				usage = u
			}
		}

		decision := b.Decision
		result := ClassifyOutcome(&TrackInput{
			Decision: &decision,
			Usage:    usage,
			Now:      cfg.Now,
		})
		result.AuditDir = b.Dir

		summary.Results = append(summary.Results, *result)
		summary.TotalApplied++

		switch result.Outcome {
		case OutcomeSafe:
			summary.Safe++
		case OutcomeTight:
			summary.Tight++
		case OutcomeWrong:
			summary.Wrong++
		case OutcomePending:
			summary.Pending++
		case OutcomeNoData:
			summary.NoData++
		}
	}

	return summary, nil
}

// FormatTrackTable renders the summary as a human-readable table.
func FormatTrackTable(summary *TrackSummary) string {
	var b strings.Builder

	header := "RECOMMENDATION HISTORY"
	sep := strings.Repeat("\u2500", 70)

	b.WriteString(fmt.Sprintf("\n  %s\n", header))
	b.WriteString(fmt.Sprintf("  %s\n", sep))
	b.WriteString(fmt.Sprintf("  %-26s %-19s %-9s %5s %5s\n", "WORKLOAD", "APPLIED", "OUTCOME", "CPU%", "MEM%"))

	for i := range summary.Results {
		r := &summary.Results[i]
		workload := fmt.Sprintf("%s/%s", strings.ToLower(r.Workload.Kind), r.Workload.Name)

		applied := r.AppliedAt.Format("2006-01-02 15:04")

		cpuStr := "-"
		memStr := "-"
		if r.Outcome != OutcomePending && r.Outcome != OutcomeNoData {
			cpuStr = fmt.Sprintf("%.0f%%", r.CPUPeakPct)
			memStr = fmt.Sprintf("%.0f%%", r.MemPeakPct)
		}

		b.WriteString(fmt.Sprintf("  %-26s %-19s %-9s %5s %5s\n",
			workload, applied, string(r.Outcome), cpuStr, memStr))
	}

	b.WriteString(fmt.Sprintf("  %s\n", sep))
	b.WriteString(fmt.Sprintf("  Score: %d applied | %d SAFE | %d TIGHT | %d WRONG | %d PENDING | %d NO_DATA\n",
		summary.TotalApplied, summary.Safe, summary.Tight, summary.Wrong, summary.Pending, summary.NoData))

	return b.String()
}

// FormatTrackJSON renders the summary as indented JSON.
func FormatTrackJSON(summary *TrackSummary) (string, error) {
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal track JSON: %w", err)
	}
	return string(data) + "\n", nil
}
