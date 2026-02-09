package promonitor

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	// Red border — the visual signal that pro-monitor is active and mutation-capable.
	redBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("196")). // Red
			Padding(1, 2)

	bannerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196")) // Red text

	labelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")) // Dim gray

	valueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("255")) // Bright white

	warnStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208")). // Orange
			Bold(true)

	okStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("46")) // Green

	progressStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")) // Blue

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("255"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)
)

func renderView(m Model) string {
	if m.quitting {
		return "Pro-monitor stopped.\n"
	}

	var b strings.Builder

	// Banner
	b.WriteString(bannerStyle.Render("PRO-MONITOR"))
	b.WriteString(" ")
	b.WriteString(renderModeTag(m.mode))
	b.WriteString("\n\n")

	// Workload info
	b.WriteString(renderWorkloadInfo(m))
	b.WriteString("\n")

	// HPA warning
	if m.hpaInfo != nil {
		b.WriteString(renderHPAWarning(m.hpaInfo))
		b.WriteString("\n")
	}

	// Latch progress
	b.WriteString(renderLatchProgress(m))
	b.WriteString("\n")

	// Policy status
	b.WriteString(renderPolicyStatus(m))
	b.WriteString("\n\n")

	// Recommendation or progress
	if m.recommendation != nil {
		b.WriteString(renderRecommendation(m.recommendation))
	} else if m.computing {
		b.WriteString(m.spinner.View())
		b.WriteString(dimStyle.Render(" Computing recommendation..."))
	} else if m.latchDone {
		b.WriteString(m.spinner.View())
		b.WriteString(dimStyle.Render(" Processing latch data..."))
	} else {
		b.WriteString(m.spinner.View())
		b.WriteString(dimStyle.Render(" Latching..."))
	}

	b.WriteString("\n\n")

	// Export status
	if m.exported {
		b.WriteString(okStyle.Render(fmt.Sprintf("Exported to %s", m.exportPath)))
		b.WriteString("\n")
	} else if m.exportError != nil {
		b.WriteString(warnStyle.Render(fmt.Sprintf("Export failed: %v", m.exportError)))
		b.WriteString("\n")
	}

	// Apply status
	if m.confirming {
		b.WriteString(renderConfirmationPrompt(m))
		b.WriteString("\n")
	} else if m.applying {
		b.WriteString(m.spinner.View())
		b.WriteString(dimStyle.Render(" Applying via Server-Side Apply..."))
		b.WriteString("\n")
	} else if m.applyResult != nil {
		b.WriteString(renderApplyResult(m.applyResult))
		b.WriteString("\n")
	}

	// Key bindings
	var keys []string
	if m.recommendation != nil && !m.exported {
		keys = append(keys, "e: export")
	}
	if m.recommendation != nil && m.mode == ModeApplyReady && m.applyResult == nil && !m.applying && !m.confirming {
		keys = append(keys, "a: apply")
	}
	keys = append(keys, "q: quit")
	b.WriteString(dimStyle.Render(strings.Join(keys, "  ")))

	// Error display
	if m.err != nil {
		b.WriteString("\n\n")
		b.WriteString(warnStyle.Render(fmt.Sprintf("Error: %v", m.err)))
	}

	// Apply red border (local copy to avoid mutating package-level style)
	content := b.String()
	border := redBorderStyle
	if m.width > 0 {
		border = border.Width(m.width - 4) // Account for border + padding
	}
	return border.Render(content)
}

func renderModeTag(mode Mode) string {
	switch mode {
	case ModeObserveOnly:
		return labelStyle.Render("[OBSERVE ONLY]")
	case ModeExportOnly:
		return okStyle.Render("[SUGGEST + EXPORT]")
	case ModeApplyReady:
		return warnStyle.Render("[SUGGEST + EXPORT + APPLY]")
	default:
		return ""
	}
}

func renderWorkloadInfo(m Model) string {
	var b strings.Builder
	b.WriteString(labelStyle.Render("Workload:  "))
	b.WriteString(valueStyle.Render(fmt.Sprintf("%s/%s", strings.ToLower(m.workload.Kind), m.workload.Name)))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("Namespace: "))
	b.WriteString(valueStyle.Render(m.workload.Namespace))
	return b.String()
}

func renderHPAWarning(hpa *HPAInfo) string {
	return warnStyle.Render(fmt.Sprintf(
		"⚠ HPA detected: %s (min=%d, max=%d) — apply blocked unless --acknowledge-hpa",
		hpa.Name, hpa.MinReplica, hpa.MaxReplica,
	))
}

func renderLatchProgress(m Model) string {
	var b strings.Builder

	b.WriteString(labelStyle.Render("Latch:     "))

	if m.latchDone {
		b.WriteString(okStyle.Render("COMPLETE"))
		b.WriteString(valueStyle.Render(fmt.Sprintf("  %d samples in %s",
			m.sampleCount, m.latchDuration.String())))
		return b.String()
	}

	if m.latchStart.IsZero() {
		b.WriteString(dimStyle.Render("starting..."))
		return b.String()
	}

	elapsed := time.Since(m.latchStart)
	pct := float64(elapsed) / float64(m.latchDuration) * 100
	if pct > 100 {
		pct = 100
	}

	// Progress bar
	barWidth := 30
	filled := int(pct / 100 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	b.WriteString(progressStyle.Render(bar))
	b.WriteString(valueStyle.Render(fmt.Sprintf(" %.0f%%  %d samples  %s/%s",
		pct, m.sampleCount, formatDuration(elapsed), m.latchDuration.String())))

	return b.String()
}

func renderPolicyStatus(m Model) string {
	var b strings.Builder
	b.WriteString(labelStyle.Render("Policy:    "))
	b.WriteString(valueStyle.Render(m.policyMsg))
	return b.String()
}

func renderRecommendation(rec *AlignmentRecommendation) string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("--- Recommendation ---"))
	b.WriteString("\n")

	// Safety and confidence
	safetyStr := renderSafetyRating(rec.Safety)
	confStr := renderConfidence(rec.Confidence)
	b.WriteString(labelStyle.Render("Safety: "))
	b.WriteString(safetyStr)
	b.WriteString("    ")
	b.WriteString(labelStyle.Render("Confidence: "))
	b.WriteString(confStr)
	b.WriteString("\n")

	// Warnings
	for _, w := range rec.Warnings {
		b.WriteString(warnStyle.Render(fmt.Sprintf("  ! %s", w)))
		b.WriteString("\n")
	}

	// Container recommendations
	if len(rec.Containers) == 0 {
		b.WriteString("\n")
		if rec.Safety == SafetyRatingUnsafe {
			b.WriteString(warnStyle.Render("  Increase resources manually — current allocation is"))
			b.WriteString("\n")
			b.WriteString(warnStyle.Render("  insufficient for observed workload behavior."))
		} else {
			b.WriteString(dimStyle.Render("  No actionable recommendation produced."))
		}
		b.WriteString("\n")
	}

	for _, c := range rec.Containers {
		b.WriteString("\n")
		b.WriteString(headerStyle.Render(fmt.Sprintf("  Container: %s", c.Name)))
		if c.Capped {
			b.WriteString(warnStyle.Render("  [CAPPED]"))
		}
		b.WriteString("\n")

		b.WriteString(renderResourceLine("CPU req", c.Current.CPURequest, c.Recommended.CPURequest, c.Delta.CPURequestPercent, fmtCPU))
		b.WriteString(renderResourceLine("CPU lim", c.Current.CPULimit, c.Recommended.CPULimit, c.Delta.CPULimitPercent, fmtCPU))
		// Only show MEM rows when at least one side has a value set
		if c.Current.MemoryRequest > 0 || c.Recommended.MemoryRequest > 0 {
			b.WriteString(renderResourceLine("MEM req", c.Current.MemoryRequest, c.Recommended.MemoryRequest, c.Delta.MemoryRequestPercent, fmtMem))
		}
		if c.Current.MemoryLimit > 0 || c.Recommended.MemoryLimit > 0 {
			b.WriteString(renderResourceLine("MEM lim", c.Current.MemoryLimit, c.Recommended.MemoryLimit, c.Delta.MemoryLimitPercent, fmtMem))
		}
	}

	// Evidence
	if rec.Evidence != nil {
		b.WriteString("\n")
		b.WriteString(labelStyle.Render(fmt.Sprintf("  Evidence: %d samples, %d gaps, %s latch",
			rec.Evidence.SampleCount, rec.Evidence.Gaps, rec.Evidence.Duration.String())))
		b.WriteString("\n")
	}

	return b.String()
}

func renderConfirmationPrompt(m Model) string {
	var b strings.Builder

	b.WriteString(warnStyle.Render("--- Apply Confirmation ---"))
	b.WriteString("\n")
	b.WriteString(labelStyle.Render("Target: "))
	b.WriteString(valueStyle.Render(m.workload.FullString()))
	b.WriteString("\n")

	if m.recommendation != nil {
		for _, c := range m.recommendation.Containers {
			b.WriteString(fmt.Sprintf("  %s: cpu %s→%s  mem %s→%s\n",
				c.Name,
				fmtCPU(c.Current.CPURequest), fmtCPU(c.Recommended.CPURequest),
				fmtMem(c.Current.MemoryRequest), fmtMem(c.Recommended.MemoryRequest)))
		}
	}

	b.WriteString(warnStyle.Render("This will trigger a rolling restart."))
	b.WriteString("\n")
	b.WriteString(m.confirmInput.View())
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("esc: cancel"))

	return b.String()
}

func renderApplyResult(result *ApplyResult) string {
	var b strings.Builder

	if len(result.DenialReasons) > 0 {
		b.WriteString(warnStyle.Render("Apply denied:"))
		b.WriteString("\n")
		for _, r := range result.DenialReasons {
			b.WriteString(warnStyle.Render(fmt.Sprintf("  - %s", r)))
			b.WriteString("\n")
		}
		return b.String()
	}

	if result.GitOpsConflict {
		b.WriteString(errorStyle.Render("SSA conflict with GitOps controller"))
		b.WriteString("\n")
		b.WriteString(warnStyle.Render(fmt.Sprintf("  Field manager: %s", result.ConflictManager)))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  This workload is managed by a GitOps controller."))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  Update resources via your GitOps pipeline instead."))
		b.WriteString("\n")
		return b.String()
	}

	if result.ConflictManager != "" {
		b.WriteString(errorStyle.Render("SSA conflict"))
		b.WriteString("\n")
		b.WriteString(warnStyle.Render(fmt.Sprintf("  Conflicting field manager: %s", result.ConflictManager)))
		b.WriteString("\n")
		return b.String()
	}

	if result.Error != nil {
		b.WriteString(errorStyle.Render(fmt.Sprintf("Apply failed: %v", result.Error)))
		b.WriteString("\n")
		return b.String()
	}

	if result.Applied {
		b.WriteString(okStyle.Render("Applied successfully via Server-Side Apply"))
		b.WriteString("\n")

		if len(result.Drifts) > 0 {
			b.WriteString(warnStyle.Render("  Drift detected (webhook may have mutated values):"))
			b.WriteString("\n")
			for _, d := range result.Drifts {
				b.WriteString(warnStyle.Render(fmt.Sprintf("    %s.%s: requested=%s admitted=%s",
					d.Container, d.Field, d.Requested, d.Admitted)))
				b.WriteString("\n")
			}
		}
	}

	return b.String()
}

func renderResourceLine(label string, current, recommended, deltaPct float64, formatter func(float64) string) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("    %-7s  ", label))
	b.WriteString(valueStyle.Render(fmt.Sprintf("%-8s", formatter(current))))
	b.WriteString(dimStyle.Render(" → "))
	b.WriteString(valueStyle.Render(fmt.Sprintf("%-8s", formatter(recommended))))
	b.WriteString("  ")
	b.WriteString(renderDelta(deltaPct))
	b.WriteString("\n")
	return b.String()
}

func renderSafetyRating(r SafetyRating) string {
	switch r {
	case SafetyRatingSafe:
		return okStyle.Render(string(r))
	case SafetyRatingCaution:
		return warnStyle.Render(string(r))
	case SafetyRatingRisky:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render(string(r))
	case SafetyRatingUnsafe:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Render(string(r))
	default:
		return string(r)
	}
}

func renderConfidence(c Confidence) string {
	switch c {
	case ConfidenceHigh:
		return okStyle.Render(string(c))
	case ConfidenceMedium:
		return warnStyle.Render(string(c))
	case ConfidenceLow:
		return dimStyle.Render(string(c))
	default:
		return string(c)
	}
}

func renderDelta(pct float64) string {
	s := fmtDelta(pct)
	if pct > 0 {
		return warnStyle.Render(s)
	} else if pct < 0 {
		return okStyle.Render(s)
	}
	return dimStyle.Render(s)
}

// fmtCPU formats CPU cores as millicores (e.g., 0.07 → "70m").
func fmtCPU(cores float64) string {
	m := cores * 1000
	if m < 1 {
		return "0m"
	}
	return fmt.Sprintf("%.0fm", m)
}

// fmtMem formats bytes as Mi (e.g., 178257920 → "170Mi").
func fmtMem(bytes float64) string {
	mi := bytes / (1024 * 1024)
	if mi < 1 {
		return "0Mi"
	}
	return fmt.Sprintf("%.0fMi", mi)
}

// fmtDelta formats a percentage delta with sign.
func fmtDelta(pct float64) string {
	if pct > 0 {
		return fmt.Sprintf("+%.0f%%", pct)
	}
	if pct < 0 {
		return fmt.Sprintf("%.0f%%", pct)
	}
	return "0%"
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
