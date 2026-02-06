package monitor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("39")). // Blue
			MarginBottom(1)

	fatalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")). // Bright red
			Bold(true)

	criticalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("208")) // Orange

	warningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")) // Yellow

	healthyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")). // Bright green
			Bold(true)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")) // Dim gray

	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240")).
			Padding(1, 2)

	heartbeatStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")) // Green
)

// Model holds the bubbletea model
type Model struct {
	watcher         *Watcher
	spinner         spinner.Model
	problems        []Problem
	allProblems     []Problem // Unfiltered problems
	events          []RecentEvent
	stats           ClusterStats
	lastUpdate      time.Time
	width           int
	height          int
	quitting        bool
	paused          bool
	scrollOffset    int
	exportRequested bool
	printRequested  bool
	sortMode        int    // 0=severity, 1=recency, 2=count
	searchMode      bool   // True when in search input mode
	searchQuery     string // Current search filter
	filteredCount   int    // Number of filtered out problems
}

// tickMsg is sent on timer tick for heartbeat
type tickMsg time.Time

// updateMsg is sent when watcher has new data
type updateMsg struct{}

// NewModel creates a new bubbletea model
func NewModel(watcher *Watcher) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot

	return Model{
		watcher:    watcher,
		spinner:    s,
		lastUpdate: time.Now(),
		sortMode:   0, // Default: sort by severity
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		tickCmd(),
		waitForUpdate(m.watcher.GetUpdateChannel()),
	)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle search mode input first
		if m.searchMode {
			switch msg.String() {
			case "esc", "ctrl+c":
				m.searchMode = false
				m.searchQuery = ""
				m.filterProblems()
				return m, nil
			case "enter":
				m.searchMode = false
				m.filterProblems()
				return m, nil
			case "backspace":
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
					m.filterProblems()
				}
				return m, nil
			default:
				// Add character to search (ignore special keys)
				if len(msg.String()) == 1 {
					m.searchQuery += msg.String()
					m.filterProblems()
				}
				return m, nil
			}
		}

		// Normal mode key handling
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			// Clear search filter
			m.searchQuery = ""
			m.filterProblems()
			return m, nil
		case "p", " ": // p or space to pause/resume
			m.paused = !m.paused
			return m, nil
		case "/": // Enter search mode
			m.searchMode = true
			m.searchQuery = ""
			return m, nil
		case "1": // Sort by severity
			m.sortMode = 0
			return m, nil
		case "2": // Sort by recency
			m.sortMode = 1
			return m, nil
		case "3": // Sort by count
			m.sortMode = 2
			return m, nil
		case "up", "k": // Scroll up
			if m.scrollOffset > 0 {
				m.scrollOffset--
			}
			return m, nil
		case "down", "j": // Scroll down
			// Calculate how many problems we can show
			problemsPerScreen := m.calculateProblemsPerScreen()
			maxScroll := max(0, len(m.problems)-problemsPerScreen)
			if m.scrollOffset < maxScroll {
				m.scrollOffset++
			}
			return m, nil
		case "home", "g": // Go to top
			m.scrollOffset = 0
			return m, nil
		case "end", "G": // Go to bottom
			problemsPerScreen := m.calculateProblemsPerScreen()
			m.scrollOffset = max(0, len(m.problems)-problemsPerScreen)
			return m, nil
		case "pageup": // Page up
			problemsPerScreen := m.calculateProblemsPerScreen()
			m.scrollOffset = max(0, m.scrollOffset-problemsPerScreen)
			return m, nil
		case "pagedown": // Page down
			problemsPerScreen := m.calculateProblemsPerScreen()
			maxScroll := max(0, len(m.problems)-problemsPerScreen)
			m.scrollOffset = min(maxScroll, m.scrollOffset+problemsPerScreen)
			return m, nil
		case "e": // Export to file
			m.exportRequested = true
			m.quitting = true
			return m, tea.Quit
		case "c", "v": // Print to terminal (copyable)
			m.printRequested = true
			m.quitting = true
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		// Tick for periodic checks (no visual changes = no flicker)
		return m, tickCmd()

	case updateMsg:
		// Update data from watcher (only if not paused)
		if !m.paused {
			m.allProblems, m.events, m.stats = m.watcher.GetState()
			m.filterProblems() // Apply current search filter
			m.lastUpdate = time.Now()
		}
		return m, waitForUpdate(m.watcher.GetUpdateChannel())

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, nil
}

// filterProblems applies the search query to filter problems
func (m *Model) filterProblems() {
	if m.searchQuery == "" {
		m.problems = m.allProblems
		m.filteredCount = 0
		m.scrollOffset = 0 // Reset scroll when clearing filter
		return
	}

	// Filter problems based on search query
	filtered := make([]Problem, 0)
	query := strings.ToLower(m.searchQuery)

	for _, p := range m.allProblems {
		// Search in namespace, pod, container, problem type, severity, message, reason
		if strings.Contains(strings.ToLower(p.Namespace), query) ||
			strings.Contains(strings.ToLower(p.PodName), query) ||
			strings.Contains(strings.ToLower(p.ContainerName), query) ||
			strings.Contains(strings.ToLower(p.Type), query) ||
			strings.Contains(strings.ToLower(string(p.Severity)), query) ||
			strings.Contains(strings.ToLower(p.Message), query) ||
			strings.Contains(strings.ToLower(p.Reason), query) {
			filtered = append(filtered, p)
		}
	}

	m.problems = filtered
	m.filteredCount = len(m.allProblems) - len(filtered)
	m.scrollOffset = 0 // Reset scroll when applying new filter
}

// View renders the UI
func (m Model) View() string {
	if m.quitting {
		return "Monitoring stopped.\n"
	}

	var b strings.Builder

	// Compact header
	sortName := []string{"Severity", "Recency", "Count"}[m.sortMode]
	var status string
	if m.paused {
		status = "[PAUSED]"
	} else {
		status = "Live"
	}

	headerLine := fmt.Sprintf("kubenow monitor [%s] | Sort: %s (1/2/3) | /=Search C=Copy Space=Pause ↑↓=Scroll Q=Quit",
		status, sortName)
	b.WriteString(titleStyle.Render(headerLine))
	b.WriteString("\n")

	// Search bar (if active)
	if m.searchMode {
		searchStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
		dimHelpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		b.WriteString(searchStyle.Render(fmt.Sprintf("Search: %s_", m.searchQuery)))
		b.WriteString(dimHelpStyle.Render("  (enter: apply  esc: cancel)"))
		b.WriteString("\n")
	} else if m.searchQuery != "" {
		filterStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
		dimHelpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		b.WriteString(filterStyle.Render(fmt.Sprintf("Filter: %s", m.searchQuery)))
		if m.filteredCount > 0 {
			b.WriteString(dimHelpStyle.Render(fmt.Sprintf(" (%d hidden)", m.filteredCount)))
		}
		b.WriteString(dimHelpStyle.Render("  (esc: clear)"))
		b.WriteString("\n")
	}

	// Active problems section
	if len(m.problems) == 0 {
		// Healthy state
		b.WriteString(m.renderHealthyState())
	} else {
		// Problems detected
		b.WriteString(m.renderProblems())
		events := m.renderRecentEvents()
		if events != "" {
			b.WriteString(events)
		}
	}

	// Cluster stats (always show)
	b.WriteString(m.renderStats())

	return borderStyle.Render(b.String())
}

// renderHealthyState renders the healthy state
func (m Model) renderHealthyState() string {
	var b strings.Builder

	title := healthyStyle.Render("✓ No active problems")
	b.WriteString(title)
	b.WriteString("\n")

	// Compact stats
	b.WriteString(dimStyle.Render(fmt.Sprintf("Cluster: %d pods (%d running), %d nodes | ",
		m.stats.TotalPods, m.stats.RunningPods, m.stats.TotalNodes)))

	// Last event
	if len(m.events) > 0 {
		lastEvent := m.events[0]
		b.WriteString(dimStyle.Render(fmt.Sprintf("Last event: %s ago", formatDuration(time.Since(lastEvent.Timestamp)))))
	} else {
		b.WriteString(dimStyle.Render("No recent events"))
	}

	return b.String()
}

// renderProblems renders active problems
func (m Model) renderProblems() string {
	var b strings.Builder

	// Sort problems based on sort mode
	sorted := make([]Problem, len(m.problems))
	copy(sorted, m.problems)

	switch m.sortMode {
	case 0: // Severity
		sort.Slice(sorted, func(i, j int) bool {
			return severityWeight(sorted[i].Severity) > severityWeight(sorted[j].Severity)
		})
	case 1: // Recency
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].LastSeen.After(sorted[j].LastSeen)
		})
	case 2: // Count
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Count > sorted[j].Count
		})
	}

	// Calculate visible window
	problemsPerScreen := m.calculateProblemsPerScreen()
	startIdx := m.scrollOffset
	endIdx := min(len(sorted), startIdx+problemsPerScreen)

	// Compact header
	b.WriteString(fatalStyle.Render(fmt.Sprintf("🔴 %d PROBLEMS", len(sorted))))
	if len(sorted) > problemsPerScreen {
		b.WriteString(dimStyle.Render(fmt.Sprintf(" (showing %d-%d)", startIdx+1, endIdx)))
	}
	b.WriteString("\n")

	// Show problems
	for i := startIdx; i < endIdx; i++ {
		b.WriteString(m.renderProblemCompact(sorted[i]))
	}

	// Scroll hints
	if startIdx > 0 || endIdx < len(sorted) {
		b.WriteString("\n")
		if startIdx > 0 {
			b.WriteString(dimStyle.Render(fmt.Sprintf("↑ %d more above | ", startIdx)))
		}
		if endIdx < len(sorted) {
			b.WriteString(dimStyle.Render(fmt.Sprintf("↓ %d more below", len(sorted)-endIdx)))
		}
	}

	return b.String()
}

// calculateProblemsPerScreen estimates how many problems fit on screen
func (m Model) calculateProblemsPerScreen() int {
	if m.height < 20 {
		return 3
	}
	// Each problem takes ~3 lines, leave space for header, events, stats
	availableLines := m.height - 12 // Header(2) + events(5) + stats(5)
	return max(3, availableLines/3)
}

// renderProblemCompact renders a problem in compact format
func (m Model) renderProblemCompact(p Problem) string {
	var b strings.Builder

	// Severity indicator (text for consistent width)
	indicator := "[!]"
	style := warningStyle
	switch p.Severity {
	case SeverityFatal:
		indicator = "[X]"
		style = fatalStyle
	case SeverityCritical:
		indicator = "[!]"
		style = criticalStyle
	}

	// Build line parts
	typePart := fmt.Sprintf("%-20s", p.Type)
	timeAgo := formatDuration(time.Since(p.LastSeen))

	// Build base line without styling to ensure consistent width
	baseLine := fmt.Sprintf("%s %s  %s/%s", indicator, typePart, p.Namespace, p.PodName)

	// Add optional parts
	containerPart := ""
	if p.ContainerName != "" {
		containerPart = fmt.Sprintf(" [%s]", p.ContainerName)
	}

	countPart := ""
	if p.Count > 1 {
		countPart = fmt.Sprintf(" (×%d)", p.Count)
	}

	// Combine with consistent spacing
	fullLine := fmt.Sprintf("%s%s  %s%s", baseLine, containerPart, timeAgo, countPart)

	// Apply styling only to the type part by replacing it
	styledLine := strings.Replace(fullLine, typePart, style.Render(typePart), 1)

	b.WriteString(styledLine)
	b.WriteString("\n")

	return b.String()
}

// renderProblem renders a single problem
func (m Model) renderProblem(p Problem) string {
	var b strings.Builder

	// Severity indicator and type
	indicator := "⚠️"
	style := warningStyle
	switch p.Severity {
	case SeverityFatal:
		indicator = "❌"
		style = fatalStyle
	case SeverityCritical:
		indicator = "⚠️"
		style = criticalStyle
	}

	// Time ago
	timeAgo := formatDuration(time.Since(p.LastSeen))
	if time.Since(p.LastSeen) < 10*time.Second {
		timeAgo = style.Render("NOW")
	}

	// Main line
	mainLine := fmt.Sprintf("%s  %s    %s/%s    %s",
		indicator,
		style.Render(p.Type),
		p.Namespace,
		p.PodName,
		timeAgo,
	)
	b.WriteString(mainLine)
	b.WriteString("\n")

	// Details
	if p.ContainerName != "" {
		b.WriteString(fmt.Sprintf("     └─ Container: %s\n", dimStyle.Render(p.ContainerName)))
	}
	if p.Message != "" {
		b.WriteString(fmt.Sprintf("     └─ %s\n", dimStyle.Render(truncate(p.Message, 70))))
	}
	if p.Count > 1 {
		b.WriteString(fmt.Sprintf("     └─ Count: %s\n", dimStyle.Render(fmt.Sprintf("%d occurrences", p.Count))))
	}

	return b.String()
}

// renderRecentEvents renders recent events (compact)
func (m Model) renderRecentEvents() string {
	var b strings.Builder

	if len(m.events) == 0 {
		return ""
	}

	b.WriteString("\n")
	b.WriteString(dimStyle.Render("📊 Recent Events: "))

	// Show just the 3 most recent
	count := 0
	for i, event := range m.events {
		if count >= 3 {
			break
		}
		if time.Since(event.Timestamp) > 5*time.Minute {
			break
		}

		if i > 0 {
			b.WriteString(dimStyle.Render(" | "))
		}

		timestamp := event.Timestamp.Format("15:04")
		b.WriteString(dimStyle.Render(fmt.Sprintf("%s %s", timestamp, truncate(event.Message, 30))))
		count++
	}

	return b.String()
}

// renderStats renders cluster statistics (compact)
func (m Model) renderStats() string {
	return dimStyle.Render(fmt.Sprintf("\n📈 Cluster: %d pods (%d running, %d problem) | %d nodes (%d ready)",
		m.stats.TotalPods, m.stats.RunningPods, m.stats.ProblemPods,
		m.stats.TotalNodes, m.stats.ReadyNodes))
}

// countNamespaces counts unique namespaces from problems and events
func (m Model) countNamespaces() int {
	namespaces := make(map[string]bool)
	for _, p := range m.problems {
		namespaces[p.Namespace] = true
	}
	for _, e := range m.events {
		namespaces[e.Namespace] = true
	}
	if len(namespaces) == 0 {
		return 1 // Default
	}
	return len(namespaces)
}

// Helper functions

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func waitForUpdate(ch <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-ch
		return updateMsg{}
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func severityWeight(s Severity) int {
	switch s {
	case SeverityFatal:
		return 3
	case SeverityCritical:
		return 2
	case SeverityWarning:
		return 1
	default:
		return 0
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ExportRequested returns whether export was requested
func (m Model) ExportRequested() bool {
	return m.exportRequested
}

// PrintRequested returns whether print mode was requested
func (m Model) PrintRequested() bool {
	return m.printRequested
}

// GetState returns the current monitoring state (for export)
func (m Model) GetState() ([]Problem, []RecentEvent, ClusterStats) {
	return m.problems, m.events, m.stats
}
