package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ─── Layout Components ───────────────────────────────────────────────────────

// renderAsciiLogo renders the ASCII art with gradient colors
func renderAsciiLogo() string {
	logoText := []string{
		` █████╗ ███╗   ██╗ ██████╗ ██████╗ ██████╗  █████╗ `,
		`██╔══██╗████╗  ██║██╔════╝██╔═══██╗██╔══██╗██╔══██╗`,
		`███████║██╔██╗ ██║██║     ██║   ██║██████╔╝███████║`,
		`██╔══██║██║╚██╗██║██║     ██║   ██║██╔══██╗██╔══██║`,
		`██║  ██║██║ ╚████║╚██████╗╚██████╔╝██║  ██║██║  ██║`,
		`╚═╝  ╚═╝╚═╝  ╚═══╝ ╚═════╝ ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝`,
	}

	// Gradient colors (Syfra palette: Lavender to Mint)
	colors := []lipgloss.Color{
		colorLavender, colorLavender, colorMintMid,
		colorMint, colorMint, colorMint,
	}

	var b strings.Builder
	for i, line := range logoText {
		b.WriteString(lipgloss.NewStyle().Foreground(colors[i]).Bold(true).Render(line) + "\n")
	}

	return b.String()
}

// renderTagline renders the Ancora tagline with branding
func renderTagline(version string) string {
	brandStyle := lipgloss.NewStyle().Foreground(colorLavender).Bold(true)
	taglineStyle := lipgloss.NewStyle().Foreground(colorSubtext)

	return brandStyle.Render("Ancora ") +
		taglineStyle.Render(version+" — Scalable memory for real AI agent orchestration and shared knowledge")
}

// renderStatusLine renders the operational status indicator
func (m Model) renderStatusLine() string {
	// Main status
	statusDot := lipgloss.NewStyle().Foreground(colorMint).Render("●")
	statusText := lipgloss.NewStyle().Foreground(colorSubtext).Render("ready")
	if m.Stats != nil {
		statusText = lipgloss.NewStyle().Foreground(colorMint).Render("operational")
	}

	// MCP status
	mcpDot := lipgloss.NewStyle().Foreground(colorRed).Render("●")
	mcpText := lipgloss.NewStyle().Foreground(colorSubtext).Render("offline")
	if m.MCPRunning {
		mcpDot = lipgloss.NewStyle().Foreground(colorMint).Render("●")
		mcpText = lipgloss.NewStyle().Foreground(colorMint).Render("operational")
	}

	return fmt.Sprintf("Status: %s %s  | MCP Status: %s %s", statusDot, statusText, mcpDot, mcpText)
}

// renderSeparator renders a horizontal separator line
func renderSeparator() string {
	return lipgloss.NewStyle().Foreground(colorOverlay).Render(strings.Repeat("─", 60))
}

// renderHeader creates the standard header (logo + status + section title + separator)
// If sectionTitle is empty, only logo and status are shown (for dashboard menu)
func (m Model) renderHeader(sectionTitle string) string {
	var b strings.Builder

	// Logo always shown on all screens
	b.WriteString(renderAsciiLogo())
	b.WriteString("\n")
	b.WriteString(renderTagline(m.Version))
	b.WriteString("\n\n")

	// Status line
	b.WriteString(m.renderStatusLine())
	b.WriteString("\n")

	// Section title (if provided)
	if sectionTitle != "" {
		b.WriteString(headerStyle.Render(sectionTitle))
		b.WriteString("\n")
	}

	// Separator after section title (or after status if no title)
	b.WriteString(renderSeparator())
	b.WriteString("\n\n")

	return b.String()
}

// renderFooter creates the standard footer with separator and help text
func renderFooter(helpText string) string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(renderSeparator())
	b.WriteString(helpStyle.Render("\n" + helpText))

	return b.String()
}

// ─── View (main router) ─────────────────────────────────────────────────────

func (m Model) View() string {
	var content string

	switch m.Screen {
	case ScreenDashboard:
		content = m.viewDashboard()
	case ScreenSearch:
		content = m.viewSearch()
	case ScreenSearchResults:
		content = m.viewSearchResults()
	case ScreenRecent:
		content = m.viewRecent()
	case ScreenObservationDetail:
		content = m.viewObservationDetail()
	case ScreenTimeline:
		content = m.viewTimeline()
	case ScreenSessions:
		content = m.viewSessions()
	case ScreenSessionDetail:
		content = m.viewSessionDetail()
	case ScreenProjects:
		content = m.viewProjects()
	case ScreenSetup:
		content = m.viewSetup()
	case ScreenSetupEnv:
		content = m.viewSetupEnv()
	case ScreenMoveObservation:
		content = m.viewMoveObservation()
	case ScreenPurge:
		content = m.viewPurge()
	case ScreenUninstall:
		content = m.viewUninstall()
	case ScreenSettings:
		content = m.viewSettings()
	default:
		content = "Unknown screen"
	}

	// Show error if present
	if m.ErrorMsg != "" {
		content += "\n" + errorStyle.Render("Error: "+m.ErrorMsg)
	}

	return appStyle.Render(content)
}

// ─── Dashboard ───────────────────────────────────────────────────────────────

func (m Model) viewDashboard() string {
	var b strings.Builder

	// Header (no section title for dashboard - just logo + status)
	b.WriteString(m.renderHeader(""))

	// Menu with two-column layout
	menuLabelStyle := lipgloss.NewStyle().
		Foreground(colorText).
		Bold(false).
		Width(22)

	menuDescStyle := lipgloss.NewStyle().
		Foreground(colorSubtext).
		Width(70)

	menuItems := getDashboardMenuItems(m.IsFullyInstalled)

	for i, item := range menuItems {
		cursor := "  "
		labelStyle := menuLabelStyle
		if i == m.Cursor {
			cursor = lipgloss.NewStyle().Foreground(colorMint).Render("▸ ")
			labelStyle = menuLabelStyle.Copy().Foreground(colorLavender)
		}

		label := labelStyle.Render(item.Label)
		desc := menuDescStyle.Render(item.Description)
		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, label, desc))
	}

	// Footer - conditional based on installation status
	footerHelp := "↑↓ navigate • Enter select • q exit"
	if m.IsFullyInstalled {
		footerHelp = "↑↓ navigate • Enter select • p projects • s search • q exit"
	}
	b.WriteString(renderFooter(footerHelp))

	return b.String()
}

// ─── Search ──────────────────────────────────────────────────────────────────

func (m Model) viewSearch() string {
	var b strings.Builder

	// Header with section title
	b.WriteString(m.renderHeader("Search Memories"))

	// Search input
	b.WriteString(searchInputStyle.Render(m.SearchInput.View()))
	b.WriteString("\n")

	// Footer
	b.WriteString(renderFooter("Type a query and press enter • esc go back"))

	return b.String()
}

// ─── Search Results ──────────────────────────────────────────────────────────

func (m Model) viewSearchResults() string {
	var b strings.Builder

	resultCount := len(m.SearchResults)
	sectionTitle := fmt.Sprintf("Search: %q — %d result", m.SearchQuery, resultCount)
	if resultCount != 1 {
		sectionTitle += "s"
	}

	// Header with section title
	b.WriteString(m.renderHeader(sectionTitle))

	if resultCount == 0 {
		b.WriteString(noResultsStyle.Render("No memories found. Try a different query."))
		b.WriteString("\n")
		b.WriteString(renderFooter("/ new search • esc back"))
		return b.String()
	}

	visibleItems := (m.Height - 15) / 2 // 2 lines per observation item (adjusted for header/footer)
	if visibleItems < 3 {
		visibleItems = 3
	}

	end := m.Scroll + visibleItems
	if end > resultCount {
		end = resultCount
	}

	for i := m.Scroll; i < end; i++ {
		r := m.SearchResults[i]
		b.WriteString(m.renderObservationListItem(i, r.ID, r.Type, r.Title, r.Content, r.CreatedAt, r.Workspace))
	}

	// Scroll indicator
	if resultCount > visibleItems {
		b.WriteString(fmt.Sprintf("\n%s",
			timestampStyle.Render(fmt.Sprintf("showing %d-%d of %d", m.Scroll+1, end, resultCount))))
	}

	b.WriteString(renderFooter("j/k navigate • enter detail • t timeline • / search • esc back"))

	return b.String()
}

// ─── Recent Observations ─────────────────────────────────────────────────────

func (m Model) viewRecent() string {
	var b strings.Builder

	count := len(m.RecentObservations)
	sectionTitle := fmt.Sprintf("Recent Observations — %d total", count)

	// Header with section title
	b.WriteString(m.renderHeader(sectionTitle))

	if count == 0 {
		b.WriteString(noResultsStyle.Render("No observations yet."))
		b.WriteString("\n")
		b.WriteString(renderFooter("esc back"))
		return b.String()
	}

	visibleItems := (m.Height - 15) / 2 // 2 lines per observation item (adjusted for header/footer)
	if visibleItems < 3 {
		visibleItems = 3
	}

	end := m.Scroll + visibleItems
	if end > count {
		end = count
	}

	for i := m.Scroll; i < end; i++ {
		o := m.RecentObservations[i]
		b.WriteString(m.renderObservationListItem(i, o.ID, o.Type, o.Title, o.Content, o.CreatedAt, o.Workspace))
	}

	if count > visibleItems {
		b.WriteString(fmt.Sprintf("\n%s",
			timestampStyle.Render(fmt.Sprintf("showing %d-%d of %d", m.Scroll+1, end, count))))
	}

	b.WriteString(renderFooter("j/k navigate • enter detail • t timeline • esc back"))

	return b.String()
}

// ─── Observation Detail ──────────────────────────────────────────────────────

func (m Model) viewObservationDetail() string {
	var b strings.Builder

	if m.SelectedObservation == nil {
		// Header with loading state
		b.WriteString(m.renderHeader("Observation Detail"))
		b.WriteString(noResultsStyle.Render("Loading..."))
		b.WriteString("\n")
		b.WriteString(renderFooter("esc back"))
		return b.String()
	}

	obs := m.SelectedObservation
	sectionTitle := fmt.Sprintf("Observation #%d", obs.ID)

	// Header with section title
	b.WriteString(m.renderHeader(sectionTitle))

	// Metadata rows
	b.WriteString(fmt.Sprintf("%s %s\n",
		detailLabelStyle.Render("Type:"),
		typeBadgeStyle.Render(obs.Type)))

	b.WriteString(fmt.Sprintf("%s %s\n",
		detailLabelStyle.Render("Title:"),
		detailValueStyle.Bold(true).Render(obs.Title)))

	b.WriteString(fmt.Sprintf("%s %s\n",
		detailLabelStyle.Render("Session:"),
		idStyle.Render(obs.SessionID)))

	b.WriteString(fmt.Sprintf("%s %s\n",
		detailLabelStyle.Render("Created:"),
		timestampStyle.Render(localTime(obs.CreatedAt))))

	if obs.ToolName != nil {
		b.WriteString(fmt.Sprintf("%s %s\n",
			detailLabelStyle.Render("Tool:"),
			detailValueStyle.Render(*obs.ToolName)))
	}

	if obs.Workspace != nil {
		b.WriteString(fmt.Sprintf("%s %s\n",
			detailLabelStyle.Render("Workspace:"),
			projectStyle.Render(*obs.Workspace)))

		if obs.Visibility != "" {
			b.WriteString(fmt.Sprintf("%s %s\n",
				detailLabelStyle.Render("Visibility:"),
				typeBadgeStyle.Render(obs.Visibility)))
		}
	}

	// Content section
	b.WriteString("\n")
	b.WriteString(sectionHeadingStyle.Render("Content"))
	b.WriteString("\n\n")

	// Wrap content based on terminal width
	wrapWidth := m.Width - 6 // basic padding
	if wrapWidth < 20 {
		wrapWidth = 20
	}
	wrappedContent := detailContentStyle.Width(wrapWidth).Render(obs.Content)

	// Split wrapped content into lines
	contentLines := strings.Split(wrappedContent, "\n")
	maxLines := m.Height - 23 // Adjusted for header/footer
	if maxLines < 5 {
		maxLines = 5
	}

	// Clamp scroll
	maxScroll := len(contentLines) - maxLines
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.DetailScroll > maxScroll {
		m.DetailScroll = maxScroll
	}

	end := m.DetailScroll + maxLines
	if end > len(contentLines) {
		end = len(contentLines)
	}

	for i := m.DetailScroll; i < end; i++ {
		b.WriteString(contentLines[i])
		b.WriteString("\n")
	}

	if len(contentLines) > maxLines {
		b.WriteString(fmt.Sprintf("\n%s",
			timestampStyle.Render(fmt.Sprintf("line %d-%d of %d", m.DetailScroll+1, end, len(contentLines)))))
	}

	b.WriteString(renderFooter("j/k scroll • t timeline • m move • esc back"))

	return b.String()
}

// ─── Move Observation ────────────────────────────────────────────────────────

func (m Model) viewMoveObservation() string {
	var b strings.Builder

	sectionTitle := fmt.Sprintf("Move Observation #%d", m.MoveObservationID)
	b.WriteString(m.renderHeader(sectionTitle))

	if m.SelectedObservation == nil || len(m.MoveProjectList) == 0 {
		b.WriteString(noResultsStyle.Render("Loading..."))
		b.WriteString("\n")
		b.WriteString(renderFooter("esc back"))
		return b.String()
	}

	obs := m.SelectedObservation

	// Current values section
	b.WriteString(sectionHeadingStyle.Render("Current Values"))
	b.WriteString("\n\n")

	b.WriteString(fmt.Sprintf("  %s %s\n",
		detailLabelStyle.Render("Workspace:"),
		projectStyle.Render(derefString(obs.Workspace))))

	b.WriteString(fmt.Sprintf("  %s %s\n\n",
		detailLabelStyle.Render("Visibility:"),
		typeBadgeStyle.Render(obs.Visibility)))

	// Change to... section
	b.WriteString(sectionHeadingStyle.Render("Change to..."))
	b.WriteString("\n\n")

	// Vertical stack layout - Project section
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorLavender).
		Padding(0, 1).
		Width(50)

	activeBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorMint).
		Padding(0, 1).
		Width(50)

	// Project section
	var projectBox strings.Builder
	projectBoxStyle := boxStyle
	projectTitle := "Project (where this memory belongs)"
	if m.MoveActiveColumn == "project" {
		projectBoxStyle = activeBoxStyle
		projectTitle = "▸ " + projectTitle
	}
	projectBox.WriteString(timestampStyle.Render(projectTitle) + "\n")

	for i, proj := range m.MoveProjectList {
		cursor := "  "
		style := listItemStyle
		if i == m.MoveProjectCursor {
			cursor = "▸ "
			style = listSelectedStyle
		}
		projectBox.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render(proj)))
	}

	b.WriteString(projectBoxStyle.Render(projectBox.String()))
	b.WriteString("\n\n")

	// Scope section
	var scopeBox strings.Builder
	scopeBoxStyle := boxStyle
	scopeTitle := "Scope (visibility & sync)"
	if m.MoveActiveColumn == "scope" {
		scopeBoxStyle = activeBoxStyle
		scopeTitle = "▸ " + scopeTitle
	}
	scopeBox.WriteString(timestampStyle.Render(scopeTitle) + "\n")

	scopeOptions := []string{"project", "personal"}
	scopeLabels := []string{
		"project  - work (can sync to cloud)",
		"personal - private (never syncs)",
	}

	for i := range scopeOptions {
		cursor := "  "
		style := listItemStyle
		label := scopeLabels[i]
		if i == m.MoveScopeCursor {
			cursor = "▸ "
			style = listSelectedStyle
		}
		scopeBox.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render(label)))
	}

	b.WriteString(scopeBoxStyle.Render(scopeBox.String()))
	b.WriteString("\n\n")

	if m.MoveError != "" {
		b.WriteString(errorStyle.Render("Error: " + m.MoveError))
		b.WriteString("\n")
	}

	if m.MoveDone {
		b.WriteString(fmt.Sprintf("%s %s\n",
			lipgloss.NewStyle().Bold(true).Foreground(colorMint).Render("✓"),
			lipgloss.NewStyle().Bold(true).Foreground(colorMint).Render("Observation moved successfully")))
		b.WriteString(renderFooter("esc back to detail"))
		return b.String()
	}

	b.WriteString(renderFooter("↹ tab switch section • ↑↓ select • Enter save • Esc cancel"))

	return b.String()
}

// ─── Timeline ────────────────────────────────────────────────────────────────

func (m Model) viewTimeline() string {
	var b strings.Builder

	if m.Timeline == nil {
		// Header with loading state
		b.WriteString(m.renderHeader("Timeline"))
		b.WriteString(noResultsStyle.Render("Loading..."))
		b.WriteString("\n")
		b.WriteString(renderFooter("esc back"))
		return b.String()
	}

	tl := m.Timeline
	sectionTitle := fmt.Sprintf("Timeline — Observation #%d (%d total in session)", tl.Focus.ID, tl.TotalInRange)

	// Header with section title
	b.WriteString(m.renderHeader(sectionTitle))

	// Session info
	if tl.SessionInfo != nil {
		b.WriteString(fmt.Sprintf("%s %s  %s %s\n\n",
			detailLabelStyle.Render("Session:"),
			idStyle.Render(tl.SessionInfo.ID),
			detailLabelStyle.Render("Project:"),
			projectStyle.Render(tl.SessionInfo.Project)))
	}

	// Before entries
	if len(tl.Before) > 0 {
		b.WriteString(sectionHeadingStyle.Render("Before"))
		b.WriteString("\n")
		for _, e := range tl.Before {
			b.WriteString(fmt.Sprintf("%s %s %s  %s\n",
				timelineConnectorStyle.Render("│"),
				idStyle.Render(fmt.Sprintf("#%-4d", e.ID)),
				typeBadgeStyle.Render(fmt.Sprintf("[%-12s]", e.Type)),
				timelineItemStyle.Render(truncateStr(e.Title, 60))))
		}
		b.WriteString(fmt.Sprintf("%s\n", timelineConnectorStyle.Render("│")))
	}

	// Focus (highlighted)
	focusContent := fmt.Sprintf("%s %s  %s\n%s",
		idStyle.Render(fmt.Sprintf("#%d", tl.Focus.ID)),
		typeBadgeStyle.Render("["+tl.Focus.Type+"]"),
		lipgloss.NewStyle().Bold(true).Foreground(colorLavender).Render(tl.Focus.Title),
		detailContentStyle.Render(truncateStr(tl.Focus.Content, 120)))
	b.WriteString(timelineFocusStyle.Render(focusContent))
	b.WriteString("\n")

	// After entries
	if len(tl.After) > 0 {
		b.WriteString(fmt.Sprintf("%s\n", timelineConnectorStyle.Render("│")))
		b.WriteString(sectionHeadingStyle.Render("After"))
		b.WriteString("\n")
		for _, e := range tl.After {
			b.WriteString(fmt.Sprintf("%s %s %s  %s\n",
				timelineConnectorStyle.Render("│"),
				idStyle.Render(fmt.Sprintf("#%-4d", e.ID)),
				typeBadgeStyle.Render(fmt.Sprintf("[%-12s]", e.Type)),
				timelineItemStyle.Render(truncateStr(e.Title, 60))))
		}
	}

	b.WriteString(renderFooter("j/k scroll • esc back"))

	return b.String()
}

// ─── Sessions ────────────────────────────────────────────────────────────────

func (m Model) viewSessions() string {
	var b strings.Builder

	count := len(m.Sessions)
	sectionTitle := fmt.Sprintf("Sessions — %d total", count)

	// Header with section title
	b.WriteString(m.renderHeader(sectionTitle))

	if count == 0 {
		b.WriteString(noResultsStyle.Render("No sessions yet."))
		b.WriteString("\n")
		b.WriteString(renderFooter("esc back"))
		return b.String()
	}

	visibleItems := m.Height - 13 // Adjusted for header/footer
	if visibleItems < 5 {
		visibleItems = 5
	}

	end := m.Scroll + visibleItems
	if end > count {
		end = count
	}

	for i := m.Scroll; i < end; i++ {
		s := m.Sessions[i]
		cursor := "  "
		style := listItemStyle
		if i == m.Cursor {
			cursor = "▸ "
			style = listSelectedStyle
		}

		summary := ""
		if s.Summary != nil {
			summary = truncateStr(*s.Summary, 50)
		}

		line := fmt.Sprintf("%s%s  %s  %s obs  %s",
			cursor,
			projectStyle.Render(fmt.Sprintf("%-20s", s.Project)),
			timestampStyle.Render(localTime(s.StartedAt)),
			statNumberStyle.Render(fmt.Sprintf("%d", s.ObservationCount)),
			style.Render(summary))

		b.WriteString(line)
		b.WriteString("\n")
	}

	if count > visibleItems {
		b.WriteString(fmt.Sprintf("\n%s",
			timestampStyle.Render(fmt.Sprintf("showing %d-%d of %d", m.Scroll+1, end, count))))
	}

	b.WriteString(renderFooter("j/k navigate • enter view session • esc back"))

	return b.String()
}

// ─── Session Detail ──────────────────────────────────────────────────────────

func (m Model) viewSessionDetail() string {
	var b strings.Builder

	if m.SelectedSessionIdx >= len(m.Sessions) {
		// Header with error state
		b.WriteString(m.renderHeader("Session Detail"))
		b.WriteString(noResultsStyle.Render("Session not found."))
		b.WriteString("\n")
		b.WriteString(renderFooter("esc back"))
		return b.String()
	}

	sess := m.Sessions[m.SelectedSessionIdx]
	sectionTitle := fmt.Sprintf("Session: %s — %s", sess.Project, localTime(sess.StartedAt))

	// Header with section title
	b.WriteString(m.renderHeader(sectionTitle))

	// Session metadata
	if sess.Summary != nil {
		b.WriteString(fmt.Sprintf("%s %s\n\n",
			detailLabelStyle.Render("Summary:"),
			detailValueStyle.Render(*sess.Summary)))
	}

	count := len(m.SessionObservations)
	b.WriteString(sectionHeadingStyle.Render(fmt.Sprintf("Observations (%d)", count)))
	b.WriteString("\n\n")

	if count == 0 {
		b.WriteString(noResultsStyle.Render("No observations in this session."))
		b.WriteString("\n")
		b.WriteString(renderFooter("esc back"))
		return b.String()
	}

	visibleItems := (m.Height - 17) / 2 // 2 lines per observation item (adjusted for header/footer)
	if visibleItems < 3 {
		visibleItems = 3
	}

	end := m.SessionDetailScroll + visibleItems
	if end > count {
		end = count
	}

	for i := m.SessionDetailScroll; i < end; i++ {
		o := m.SessionObservations[i]
		b.WriteString(m.renderObservationListItem(i, o.ID, o.Type, o.Title, o.Content, o.CreatedAt, o.Workspace))
	}

	if count > visibleItems {
		b.WriteString(fmt.Sprintf("\n%s",
			timestampStyle.Render(fmt.Sprintf("showing %d-%d of %d", m.SessionDetailScroll+1, end, count))))
	}

	b.WriteString(renderFooter("j/k navigate • enter detail • t timeline • esc back"))

	return b.String()
}

// ─── Projects ────────────────────────────────────────────────────────────────

func (m Model) viewProjects() string {
	var b strings.Builder

	// Header with section title
	filterDesc := "all scopes"
	if m.ProjectScopeFilter == "project" {
		filterDesc = "project scope only"
	} else if m.ProjectScopeFilter == "personal" {
		filterDesc = "personal scope only"
	}
	b.WriteString(m.renderHeader(fmt.Sprintf("Projects — %s", filterDesc)))

	if len(m.Projects) == 0 {
		b.WriteString(contentPreviewStyle.Render("No projects found"))
		b.WriteString(renderFooter("f filter scope • esc back"))
		return b.String()
	}

	// Filter projects by scope if needed
	filtered := m.Projects
	if m.ProjectScopeFilter != "" {
		filtered = []ProjectWithEnrollment{}
		for _, p := range m.Projects {
			// Check if project has observations in the filtered scope
			count, err := m.store.AllObservations(p.Name, m.ProjectScopeFilter, 1)
			if err == nil && len(count) > 0 {
				filtered = append(filtered, p)
			}
		}
	}

	// Calculate visible window
	visibleItems := m.Height - 12
	if visibleItems < 5 {
		visibleItems = 5
	}
	start := m.Scroll
	end := start + visibleItems
	if end > len(filtered) {
		end = len(filtered)
	}

	// Render project list
	for i := start; i < end; i++ {
		p := filtered[i]
		cursor := " "
		style := lipgloss.NewStyle().Foreground(colorText)
		if i == m.Cursor {
			cursor = "▸"
			style = style.Foreground(colorLavender).Bold(true)
		}

		// Sync status indicator
		syncIndicator := timestampStyle.Render("◯ no sync")
		if p.SyncEnabled {
			syncIndicator = lipgloss.NewStyle().Foreground(colorMint).Render("● synced")
		}

		// Project info line
		projectLine := fmt.Sprintf("%s %s %s | %d obs, %d sessions",
			cursor,
			style.Render(p.Name),
			syncIndicator,
			p.ObservationCount,
			p.SessionCount,
		)
		b.WriteString(projectLine + "\n")

		// Directories line (if any)
		if len(p.Directories) > 0 {
			dirText := fmt.Sprintf("  %s", strings.Join(p.Directories, ", "))
			b.WriteString(contentPreviewStyle.Render(truncateStr(dirText, 80)) + "\n")
		}
	}

	// Pagination info
	if len(filtered) > visibleItems {
		b.WriteString("\n" + timestampStyle.Render(
			fmt.Sprintf("showing %d-%d of %d projects", start+1, end, len(filtered))))
	}

	b.WriteString(renderFooter("j/k navigate • enter view • f filter scope • esc back"))

	return b.String()
}

// ─── Setup ───────────────────────────────────────────────────────────────────

func (m Model) viewSetup() string {
	var b strings.Builder

	// Header with section title
	b.WriteString(m.renderHeader("Setup — Install Agent Plugin"))

	// Show spinner while installing
	if m.SetupInstalling {
		b.WriteString(fmt.Sprintf("%s Installing %s plugin...\n",
			m.SetupSpinner.View(),
			lipgloss.NewStyle().Bold(true).Foreground(colorLavender).Render(m.SetupInstallingName)))
		b.WriteString("\n")

		switch m.SetupInstallingName {
		case "opencode":
			b.WriteString(timestampStyle.Render("Copying plugin file to plugins directory"))
		case "claude-code":
			b.WriteString(timestampStyle.Render("Running claude plugin marketplace add + install"))
		}

		b.WriteString("\n")
		b.WriteString(renderFooter("Installing..."))
		return b.String()
	}

	// Show allowlist prompt after successful claude-code install
	if m.SetupAllowlistPrompt && m.SetupResult != nil {
		successMsg := fmt.Sprintf("Installed %s plugin", m.SetupResult.Agent)
		b.WriteString(fmt.Sprintf("%s %s\n\n",
			lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render("✓"),
			lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render(successMsg)))

		b.WriteString(sectionHeadingStyle.Render("Permissions Allowlist"))
		b.WriteString("\n\n")
		b.WriteString(detailContentStyle.Render("Add ancora tools to ~/.claude/settings.json allowlist?"))
		b.WriteString("\n")
		b.WriteString(timestampStyle.Render("This prevents Claude Code from asking permission on every tool call."))
		b.WriteString("\n")
		b.WriteString(renderFooter("[y] Yes  [n] No"))
		return b.String()
	}

	// Show result after install
	if m.SetupDone {
		if m.SetupError != "" {
			b.WriteString(errorStyle.Render("✗ Installation failed: " + m.SetupError))
			b.WriteString("\n")
		} else if m.SetupResult != nil {
			successMsg := fmt.Sprintf("Installed %s plugin", m.SetupResult.Agent)
			if m.SetupResult.Files > 0 {
				successMsg += fmt.Sprintf(" (%d files)", m.SetupResult.Files)
			}
			b.WriteString(fmt.Sprintf("%s %s\n",
				lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render("✓"),
				lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render(successMsg)))
			b.WriteString(fmt.Sprintf("%s %s\n\n",
				detailLabelStyle.Render("Location:"),
				projectStyle.Render(m.SetupResult.Destination)))

			// Post-install instructions
			switch m.SetupResult.Agent {
			case "opencode":
				b.WriteString(sectionHeadingStyle.Render("Next Steps"))
				b.WriteString("\n")
				b.WriteString(detailContentStyle.Render("1. Restart OpenCode"))
				b.WriteString("\n")
				b.WriteString(detailContentStyle.Render("2. Plugin is auto-loaded from ~/.config/opencode/plugins/"))
				b.WriteString("\n")
				b.WriteString(detailContentStyle.Render("3. Make sure 'ancora' is in your MCP config (opencode.json)"))
				b.WriteString("\n")
			case "claude-code":
				b.WriteString(sectionHeadingStyle.Render("Next Steps"))
				b.WriteString("\n")
				if m.SetupAllowlistApplied {
					b.WriteString(fmt.Sprintf("%s %s\n",
						lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render("✓"),
						detailContentStyle.Render("Ancora tools added to allowlist")))
				} else if m.SetupAllowlistError != "" {
					b.WriteString(fmt.Sprintf("%s %s\n",
						lipgloss.NewStyle().Bold(true).Foreground(colorRed).Render("✗"),
						detailContentStyle.Render("Allowlist update failed: "+m.SetupAllowlistError)))
					b.WriteString(detailContentStyle.Render("Add manually to permissions.allow in ~/.claude/settings.json"))
					b.WriteString("\n")
				}
				b.WriteString(detailContentStyle.Render("1. Restart Claude Code — the plugin is active immediately"))
				b.WriteString("\n")
				b.WriteString(detailContentStyle.Render("2. Verify with: claude plugin list"))
				b.WriteString("\n")
			}
		}

		b.WriteString(renderFooter("enter/esc back to dashboard"))
		return b.String()
	}

	// Agent selection (content instruction)
	b.WriteString(detailContentStyle.Render("Select an agent to set up"))
	b.WriteString("\n\n")

	for i, agent := range m.SetupAgents {
		cursor := "  "
		style := menuItemStyle
		if i == m.Cursor {
			cursor = lipgloss.NewStyle().Foreground(colorMint).Render("▸ ")
			style = menuSelectedStyle
		}

		b.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render(agent.Description)))
		b.WriteString(fmt.Sprintf("    %s %s\n\n",
			detailLabelStyle.Render("Install to:"),
			timestampStyle.Render(agent.InstallDir)))
	}

	b.WriteString(renderFooter("j/k navigate • enter install • esc back"))

	return b.String()
}

func (m Model) viewSetupEnv() string {
	var b strings.Builder

	b.WriteString(m.renderHeader(""))

	// Step 0: Select plugin
	if m.SetupEnvStep == 0 {
		menuLabelStyle := lipgloss.NewStyle().
			Foreground(colorText).
			Bold(false).
			Width(30)

		menuDescStyle := lipgloss.NewStyle().
			Foreground(colorSubtext).
			Width(60)

		setupEnvMenuItems := []MenuItem{
			{"OpenCode", "Install MCP plugin for OpenCode agent", "opencode"},
			{"Claude Code", "Install MCP plugin for Claude Code agent", "claude-code"},
		}

		for i, item := range setupEnvMenuItems {
			cursor := "  "
			labelStyle := menuLabelStyle
			if i == m.Cursor {
				cursor = lipgloss.NewStyle().Foreground(colorMint).Render("▸ ")
				labelStyle = menuLabelStyle.Copy().Foreground(colorLavender)
			}

			label := labelStyle.Render(item.Label)
			desc := menuDescStyle.Render(item.Description)
			b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, label, desc))
		}

		b.WriteString("\n")
		b.WriteString(detailContentStyle.Render("This will:"))
		b.WriteString("\n")
		b.WriteString(timestampStyle.Render("  1. Download embedding model (~180 MB)"))
		b.WriteString("\n")
		b.WriteString(timestampStyle.Render("  2. Generate embeddings for existing memories"))
		b.WriteString("\n")
		b.WriteString(timestampStyle.Render("  3. Install selected agent plugin"))
		b.WriteString("\n\n")
		b.WriteString(renderFooter("↑↓ navigate • Enter select • esc back"))

		return b.String()
	}

	// Step 1: Running setup with progress
	if m.SetupEnvStep == 1 {
		b.WriteString(sectionHeadingStyle.Render(fmt.Sprintf("Setting up Ancora with %s", m.SetupEnvPlugin)))
		b.WriteString("\n\n")

		// Task 1: Download model
		if m.SetupEnvModelDone {
			b.WriteString(fmt.Sprintf("%s Download embedding model\n",
				lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render("✓")))
		} else if m.SetupEnvRunning && m.SetupEnvModelProgress > 0 {
			progressBar := renderProgressBar(m.SetupEnvModelProgress)
			b.WriteString(fmt.Sprintf("%s Downloading embedding model... %s\n",
				m.SetupSpinner.View(),
				progressBar))
		} else if m.SetupEnvRunning {
			b.WriteString(fmt.Sprintf("%s Download embedding model\n",
				m.SetupSpinner.View()))
		} else {
			b.WriteString(fmt.Sprintf("%s Download embedding model\n",
				timestampStyle.Render("○")))
		}

		// Task 2: Backfill
		if m.SetupEnvBackfillDone {
			b.WriteString(fmt.Sprintf("%s Generate embeddings\n",
				lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render("✓")))
		} else if m.SetupEnvRunning && m.SetupEnvModelDone {
			b.WriteString(fmt.Sprintf("%s Generating embeddings...\n",
				m.SetupSpinner.View()))
		} else {
			b.WriteString(fmt.Sprintf("%s Generate embeddings\n",
				timestampStyle.Render("○")))
		}

		// Task 3: Install plugin
		if m.SetupEnvPluginDone {
			b.WriteString(fmt.Sprintf("%s Install %s plugin\n",
				lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render("✓"),
				m.SetupEnvPlugin))
		} else if m.SetupEnvRunning && m.SetupEnvBackfillDone {
			b.WriteString(fmt.Sprintf("%s Installing %s plugin...\n",
				m.SetupSpinner.View(),
				m.SetupEnvPlugin))
		} else {
			b.WriteString(fmt.Sprintf("%s Install %s plugin\n",
				timestampStyle.Render("○"),
				m.SetupEnvPlugin))
		}

		b.WriteString("\n")

		if m.SetupEnvError != "" {
			b.WriteString(errorStyle.Render("✗ Setup failed: " + m.SetupEnvError))
			b.WriteString("\n")
			b.WriteString(renderFooter("enter/esc back to dashboard"))
		} else if !m.SetupEnvRunning && m.SetupEnvPluginDone {
			b.WriteString(fmt.Sprintf("%s Setup complete!\n",
				lipgloss.NewStyle().Bold(true).Foreground(colorGreen).Render("✓")))
			b.WriteString("\n")
			b.WriteString(renderFooter("enter/esc back to dashboard"))
		} else {
			b.WriteString(renderFooter("Setting up..."))
		}

		return b.String()
	}

	return b.String()
}

func renderProgressBar(progress float64) string {
	width := 30
	filled := int(progress * float64(width))
	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	percentage := int(progress * 100)
	return fmt.Sprintf("[%s] %d%%", bar, percentage)
}

// ─── Shared Renderers ────────────────────────────────────────────────────────

func (m Model) renderObservationListItem(index int, id int64, obsType, title, content, createdAt string, project *string) string {
	cursor := "  "
	style := listItemStyle
	if index == m.Cursor {
		cursor = "▸ "
		style = listSelectedStyle
	}

	proj := ""
	if project != nil {
		proj = "  " + projectStyle.Render(*project)
	}

	line := fmt.Sprintf("%s%s %s %s%s  %s\n",
		cursor,
		idStyle.Render(fmt.Sprintf("#%-5d", id)),
		typeBadgeStyle.Render(fmt.Sprintf("[%-12s]", obsType)),
		style.Render(truncateStr(title, 50)),
		proj,
		timestampStyle.Render(localTime(createdAt)))

	// Content preview on second line
	preview := truncateStr(content, 80)
	if preview != "" {
		line += contentPreviewStyle.Render(preview) + "\n"
	}

	return line
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// localTime converts a UTC timestamp string from SQLite to local time for display.
func localTime(utc string) string {
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		time.RFC3339,
		time.RFC3339Nano,
	} {
		if t, err := time.Parse(layout, utc); err == nil {
			return t.UTC().Local().Format("2006-01-02 15:04:05")
		}
	}
	return utc // unparseable — return as-is
}

func truncateStr(s string, max int) string {
	// Remove newlines for single-line display
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

func limitProjects(projects []string, max int) []string {
	if len(projects) <= max {
		return projects
	}
	return projects[:max]
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ─── Purge Database ───────────────────────────────────────────────────────────

func (m Model) viewPurge() string {
	var b strings.Builder

	// Header with section title
	b.WriteString(m.renderHeader("⚠️  PURGE DATABASE"))

	// Show stats before purge
	if m.Stats != nil {
		b.WriteString(warningStyle.Render("This will PERMANENTLY DELETE ALL DATA:\n"))
		b.WriteString(fmt.Sprintf("  • %d observations\n", m.Stats.TotalObservations))
		b.WriteString(fmt.Sprintf("  • %d sessions\n", m.Stats.TotalSessions))
		b.WriteString(fmt.Sprintf("  • %d prompts\n\n", m.Stats.TotalPrompts))
	}

	// Show confirmation prompt or results
	if m.PurgeResult != nil {
		// Purge completed - show results
		b.WriteString(successStyle.Render("✅ Database purged successfully!\n\n"))
		b.WriteString(fmt.Sprintf("Deleted:\n"))
		b.WriteString(fmt.Sprintf("  • %d observations\n", m.PurgeResult.ObservationsDeleted))
		b.WriteString(fmt.Sprintf("  • %d sessions\n", m.PurgeResult.SessionsDeleted))
		b.WriteString(fmt.Sprintf("  • %d prompts\n\n", m.PurgeResult.PromptsDeleted))
		b.WriteString(lipgloss.NewStyle().Foreground(colorSubtext).Render("Press any key to return to dashboard"))
	} else if m.PurgeError != "" {
		// Purge failed
		b.WriteString(errorStyle.Render("Error: " + m.PurgeError + "\n\n"))
		b.WriteString(renderFooter("esc go back"))
	} else {
		// Show confirmation prompt
		b.WriteString(warningStyle.Render("Are you sure? This cannot be undone!\n\n"))

		// Options
		options := []string{"No, go back", "Yes, delete everything"}
		for i, opt := range options {
			cursor := "  "
			style := menuLabelStyle
			if i == m.PurgeConfirmCursor {
				cursor = lipgloss.NewStyle().Foreground(colorMint).Render("▸ ")
				style = menuLabelStyle.Copy().Foreground(colorLavender)
			}
			b.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render(opt)))
		}

		b.WriteString(renderFooter("j/k navigate • enter confirm • esc cancel"))
	}

	return b.String()
}

// ─── Settings ────────────────────────────────────────────────────────────────

func (m Model) viewSettings() string {
	var b strings.Builder

	b.WriteString(m.renderHeader("⚙  SEARCH RANKING PRESET"))
	b.WriteString("Controls how search results are ranked based on workspace proximity.\n\n")

	labelStyle := lipgloss.NewStyle().Foreground(colorText).Bold(false).Width(12)
	descStyle := lipgloss.NewStyle().Foreground(colorSubtext).Width(60)

	presetDesc := map[string]string{
		"Balanced": "Current workspace first, others slightly demoted  (default)",
		"Strict":   "Heavy penalty for cross-workspace and cross-org results",
		"Flat":     "Pure relevance ranking — no workspace penalty",
	}

	for i, preset := range settingsPresets {
		cursor := "  "
		ls := labelStyle
		if i == m.Cursor {
			cursor = lipgloss.NewStyle().Foreground(colorMint).Render("▸ ")
			ls = labelStyle.Copy().Foreground(colorLavender)
		}

		selected := "  "
		if preset == m.ClassifyConfig.Preset {
			selected = lipgloss.NewStyle().Foreground(colorMint).Render("✓ ")
		}

		label := ls.Render(preset.String())
		desc := descStyle.Render(presetDesc[preset.String()])
		b.WriteString(fmt.Sprintf("%s%s%s %s\n", cursor, selected, label, desc))
	}

	b.WriteString("\n")
	if m.SettingsPresetSaved {
		b.WriteString(successStyle.Render("✅ Preset saved.\n"))
	}

	b.WriteString(renderFooter("j/k navigate • enter select • esc back"))
	return b.String()
}

// ─── Uninstall ───────────────────────────────────────────────────────────────

func (m Model) viewUninstall() string {
	var b strings.Builder

	// Header with section title
	b.WriteString(m.renderHeader("⚠️  UNINSTALL ANCORA"))

	// Show uninstall warning
	b.WriteString(warningStyle.Render("This will completely remove Ancora from your system:\n"))
	b.WriteString("  • Delete embedding model and all cached data\n")
	b.WriteString("  • Remove configuration files\n")
	b.WriteString("  • Database will be preserved (run Purge first to delete)\n\n")

	// Show confirmation prompt or results
	if m.UninstallDone {
		// Uninstall completed
		b.WriteString(successStyle.Render("✅ Ancora uninstalled successfully!\n\n"))
		b.WriteString(lipgloss.NewStyle().Foreground(colorSubtext).Render("You can safely delete the database manually if needed.\n"))
		b.WriteString(lipgloss.NewStyle().Foreground(colorSubtext).Render("Press any key to exit"))
	} else if m.UninstallError != "" {
		// Uninstall failed
		b.WriteString(errorStyle.Render("Error: " + m.UninstallError + "\n\n"))
		b.WriteString(renderFooter("esc go back"))
	} else {
		// Show confirmation prompt
		b.WriteString(warningStyle.Render("Are you sure? This cannot be undone!\n\n"))

		// Options
		options := []string{"No, go back", "Yes, uninstall Ancora"}
		for i, opt := range options {
			cursor := "  "
			style := menuLabelStyle
			if i == m.UninstallConfirmCursor {
				cursor = lipgloss.NewStyle().Foreground(colorMint).Render("▸ ")
				style = menuLabelStyle.Copy().Foreground(colorLavender)
			}
			b.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render(opt)))
		}

		b.WriteString(renderFooter("j/k navigate • enter confirm • esc cancel"))
	}

	return b.String()
}
