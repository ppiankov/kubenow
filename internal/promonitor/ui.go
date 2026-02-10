package promonitor

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/ppiankov/kubenow/internal/exposure"
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

	// Early-stop warning
	if m.earlyStopPending {
		b.WriteString(warnStyle.Render("Press Esc again to stop latching and proceed with collected data. Any other key to continue."))
		b.WriteString("\n")
	}

	// Policy status
	b.WriteString(renderPolicyStatus(m))
	b.WriteString("\n\n")

	// Main content area — one of: traffic map, exposure map, recommendation, or progress
	switch {
	case m.showTraffic:
		if m.trafficLoading {
			b.WriteString(m.spinner.View())
			b.WriteString(dimStyle.Render(" Querying Linkerd traffic map..."))
		} else if m.trafficMap != nil {
			b.WriteString(renderTrafficMap(m.trafficMap))
		}
		b.WriteString("\n\n")
	case m.showExposure:
		if m.exposureLoading {
			b.WriteString(m.spinner.View())
			b.WriteString(dimStyle.Render(" Querying exposure map..."))
		} else if m.exposureMap != nil {
			b.WriteString(renderExposureMap(m.exposureMap))
		}
		b.WriteString("\n\n")
	case m.recommendation != nil:
		b.WriteString(renderRecommendation(m.recommendation))
	case m.computing:
		b.WriteString(m.spinner.View())
		b.WriteString(dimStyle.Render(" Computing recommendation..."))
	case m.latchDone:
		b.WriteString(m.spinner.View())
		b.WriteString(dimStyle.Render(" Processing latch data..."))
	default:
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
	overlay := m.showExposure || m.showTraffic
	var keys []string
	if !m.latchDone && m.latch != nil {
		keys = append(keys, "esc: stop early")
	}
	if m.recommendation != nil && m.exposureCollector != nil {
		if m.showExposure {
			keys = append(keys, "l: dismiss")
		} else {
			keys = append(keys, "l: exposure map")
		}
		if m.exposureCollector.HasPrometheus() {
			if m.showTraffic {
				keys = append(keys, "t: dismiss")
			} else {
				keys = append(keys, "t: traffic map")
			}
		}
	}
	if m.recommendation != nil && !m.exported && !overlay {
		keys = append(keys, "e: export")
	}
	if m.recommendation != nil && m.mode == ModeApplyReady && m.applyResult == nil && !m.applying && !m.confirming && !overlay {
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
	workloadStr := fmt.Sprintf("%s/%s", strings.ToLower(m.workload.Kind), m.workload.Name)
	if m.operatorType != "" {
		workloadStr = fmt.Sprintf("%s (%s)", workloadStr, m.operatorType)
	}
	b.WriteString(valueStyle.Render(workloadStr))
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
		if m.earlyStopActual > 0 {
			b.WriteString(warnStyle.Render("EARLY STOP"))
			b.WriteString(valueStyle.Render(fmt.Sprintf("  %d samples in %s (planned %s)",
				m.sampleCount, formatDuration(m.earlyStopActual), m.latchDuration.String())))
		} else {
			b.WriteString(okStyle.Render("COMPLETE"))
			b.WriteString(valueStyle.Render(fmt.Sprintf("  %d samples in %s",
				m.sampleCount, m.latchDuration.String())))
		}
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
		evidenceStr := fmt.Sprintf("  Evidence: %d samples, %d gaps, %s latch",
			rec.Evidence.SampleCount, rec.Evidence.Gaps, rec.Evidence.Duration.String())
		if rec.Evidence.PlannedDuration > 0 {
			evidenceStr += fmt.Sprintf(" (planned %s)", rec.Evidence.PlannedDuration.String())
		}
		b.WriteString(labelStyle.Render(evidenceStr))
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

// renderExposureMap renders the structural topology view showing
// possible traffic paths to the workload.
func renderExposureMap(em *exposure.ExposureMap) string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("--- Exposure Map ---"))
	b.WriteString("\n\n")

	renderExposureServices(&b, em.Services)
	renderExposureNetPols(&b, em.Services)
	renderExposureNeighbors(&b, em.Neighbors)

	// Errors
	if len(em.Errors) > 0 {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("Partial data:"))
		b.WriteString("\n")
		for _, e := range em.Errors {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  %s", e)))
			b.WriteString("\n")
		}
	}

	// Disclaimer
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Structural topology from K8s API — not measured traffic. Press t for live Linkerd data."))

	return b.String()
}

// renderExposureServices renders the services and their ingress routes.
func renderExposureServices(b *strings.Builder, services []exposure.ServiceExposure) {
	b.WriteString(headerStyle.Render("Exposed via:"))
	b.WriteString("\n")

	if len(services) == 0 {
		b.WriteString(dimStyle.Render("  (no services target this workload)"))
		b.WriteString("\n")
		return
	}

	for _, svc := range services {
		ports := formatServicePorts(svc.Ports)
		b.WriteString(valueStyle.Render(fmt.Sprintf("  svc/%s", svc.Name)))
		b.WriteString(dimStyle.Render(fmt.Sprintf(" (%s, %s)", svc.Type, ports)))
		b.WriteString("\n")

		if len(svc.Ingresses) == 0 {
			b.WriteString(dimStyle.Render("    ← no ingress"))
			b.WriteString("\n")
			continue
		}
		for i := range svc.Ingresses {
			b.WriteString(okStyle.Render(fmt.Sprintf("    ← ingress: %s", formatIngressRoute(&svc.Ingresses[i]))))
			b.WriteString("\n")
		}
	}
}

// renderExposureNetPols renders network policies (apply to pods, not per-service).
func renderExposureNetPols(b *strings.Builder, services []exposure.ServiceExposure) {
	if services == nil {
		return
	}
	hasNetPols := false
	for _, svc := range services {
		if len(svc.NetPols) > 0 {
			hasNetPols = true
			break
		}
	}
	if !hasNetPols {
		b.WriteString(dimStyle.Render("    ← netpol: default (all allowed)"))
		b.WriteString("\n")
		return
	}
	for _, svc := range services {
		for _, np := range svc.NetPols {
			sources := formatNetPolSources(np.Sources)
			b.WriteString(warnStyle.Render(fmt.Sprintf("    ← netpol %s: allows from %s", np.PolicyName, sources)))
			b.WriteString("\n")
		}
	}
}

// renderTrafficMap renders the dedicated Linkerd traffic map screen.
func renderTrafficMap(tm *exposure.TrafficMap) string {
	var b strings.Builder

	b.WriteString(headerStyle.Render(fmt.Sprintf("--- Traffic Map (Linkerd, %s window) ---", tm.Window)))
	b.WriteString("\n\n")

	// Inbound section
	b.WriteString(headerStyle.Render("Inbound (who sends traffic here):"))
	b.WriteString("\n")
	if len(tm.Inbound) == 0 {
		b.WriteString(dimStyle.Render("  (no inbound traffic detected)"))
		b.WriteString("\n")
	} else {
		for _, e := range tm.Inbound {
			renderTrafficEdge(&b, e)
		}
	}

	// Outbound section
	b.WriteString("\n")
	b.WriteString(headerStyle.Render("Outbound (where this workload sends):"))
	b.WriteString("\n")
	if len(tm.Outbound) == 0 {
		b.WriteString(dimStyle.Render("  (no outbound traffic detected)"))
		b.WriteString("\n")
	} else {
		for _, e := range tm.Outbound {
			renderTrafficEdge(&b, e)
		}
	}

	// TCP summary
	if tm.TCPIn > 0 || tm.TCPOut > 0 {
		b.WriteString("\n")
		b.WriteString(labelStyle.Render(fmt.Sprintf("TCP: %d inbound / %d outbound connections (%s)",
			tm.TCPIn, tm.TCPOut, tm.Window)))
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Data from Linkerd proxy metrics via Prometheus (window is independent of latch duration)"))

	return b.String()
}

// renderTrafficEdge renders a single traffic edge line with RPS, success rate, and latency.
func renderTrafficEdge(b *strings.Builder, e exposure.TrafficEdge) {
	name := e.Deployment
	if e.Namespace != "" {
		name = fmt.Sprintf("%s (%s)", e.Deployment, e.Namespace)
	}
	fmt.Fprintf(b, "  %-36s ", name)
	b.WriteString(valueStyle.Render(fmt.Sprintf("%7.1f rps", e.RPS)))

	// Success rate
	if e.SuccessRate >= 0 {
		pct := e.SuccessRate * 100
		style := okStyle
		if pct < 99.0 {
			style = warnStyle
		}
		if pct < 95.0 {
			style = errorStyle
		}
		b.WriteString(style.Render(fmt.Sprintf("  %5.1f%% ok", pct)))
	} else {
		b.WriteString(dimStyle.Render("        —"))
	}

	// Latency
	if e.LatencyP50 >= 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  p50: %s", fmtLatency(e.LatencyP50))))
	}
	if e.LatencyP99 >= 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  p99: %s", fmtLatency(e.LatencyP99))))
	}

	b.WriteString("\n")
}

// fmtLatency formats milliseconds as a human-readable latency string.
func fmtLatency(ms float64) string {
	if ms < 1 {
		return fmt.Sprintf("%.1fms", ms)
	}
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.1fs", ms/1000)
}

const maxNeighbors = 10

// renderExposureNeighbors renders namespace neighbors ranked by CPU.
func renderExposureNeighbors(b *strings.Builder, neighbors []exposure.Neighbor) {
	if len(neighbors) == 0 {
		return
	}
	b.WriteString("\n")
	b.WriteString(headerStyle.Render("Namespace neighbors (by CPU):"))
	b.WriteString("\n")

	for i, n := range neighbors {
		if i >= maxNeighbors {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ... and %d more", len(neighbors)-maxNeighbors)))
			b.WriteString("\n")
			break
		}
		name := n.WorkloadName
		if n.WorkloadKind != "" {
			name = fmt.Sprintf("%s (%s)", name, n.WorkloadKind)
		}
		if n.PodCount > 1 {
			name = fmt.Sprintf("%s (%d pods)", n.WorkloadName, n.PodCount)
			if n.WorkloadKind != "" {
				name = fmt.Sprintf("%s (%s, %d pods)", n.WorkloadName, n.WorkloadKind, n.PodCount)
			}
		}
		fmt.Fprintf(b, "  %-40s ", name)
		b.WriteString(valueStyle.Render(fmt.Sprintf("%dm", n.CPUMillis)))
		b.WriteString("\n")
	}
}

func formatIngressRoute(ing *exposure.IngressRoute) string {
	hosts := strings.Join(ing.Hosts, ", ")
	tls := ""
	if ing.TLS {
		tls = " [TLS]"
	}
	cls := ""
	if ing.ClassName != "" {
		cls = fmt.Sprintf(" (%s)", ing.ClassName)
	}
	return fmt.Sprintf("%s%s%s", hosts, tls, cls)
}

func formatServicePorts(ports []exposure.PortMapping) string {
	if len(ports) == 0 {
		return "no ports"
	}
	parts := make([]string, len(ports))
	for i, p := range ports {
		if p.Name != "" {
			parts[i] = fmt.Sprintf("%s/%d", p.Name, p.Port)
		} else {
			parts[i] = fmt.Sprintf("%d", p.Port)
		}
	}
	return strings.Join(parts, ", ")
}

func formatNetPolSources(sources []exposure.NetPolSource) string {
	if len(sources) == 0 {
		return "none"
	}
	parts := make([]string, len(sources))
	for i, s := range sources {
		switch s.Type {
		case "namespace":
			parts[i] = fmt.Sprintf("ns/%s", s.Namespace)
		case "pod":
			parts[i] = fmt.Sprintf("pods(%s)", s.PodLabel)
		case "ipBlock":
			parts[i] = s.CIDR
		case "all":
			parts[i] = "all"
		default:
			parts[i] = s.Type
		}
	}
	return strings.Join(parts, ", ")
}
