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

	// Placeholder for recommendation engine
	if m.latchDone {
		b.WriteString(dimStyle.Render("Recommendation engine not yet connected."))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("Latch data captured — export will be available in a future PR."))
	} else {
		b.WriteString(m.spinner.View())
		b.WriteString(dimStyle.Render(" Latching..."))
	}

	b.WriteString("\n\n")

	// Key bindings
	b.WriteString(dimStyle.Render("q: quit"))

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

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
