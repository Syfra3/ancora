package tui

import "github.com/charmbracelet/lipgloss"

// ─── Colors (Syfra/Ancora Identity Palette) ──────────────────────────

var (
	// Core Syfra Identity colors
	colorBase     = lipgloss.Color("#242426") // Charcoal Void (from design system)
	colorSurface  = lipgloss.Color("#2a2a2d") // Slightly lighter panel bg
	colorOverlay  = lipgloss.Color("#4a4a4e") // Muted borders
	colorText     = lipgloss.Color("#e0e0e2") // Light text
	colorSubtext  = lipgloss.Color("#8a8a8e") // Dim text
	colorLavender = lipgloss.Color("#C8B6FF") // Lavender Primary (Syfra brand)
	colorMint     = lipgloss.Color("#B4FFDD") // Mint Secondary (Syfra brand)
	colorMintMid  = lipgloss.Color("#CEE7F0") // Transition between Lavender and Mint

	// Accent colors
	colorGreen  = lipgloss.Color("#B4FFDD") // Mint for success
	colorPeach  = lipgloss.Color("#FFD4B8") // Warm accent
	colorRed    = lipgloss.Color("#FF9EB8") // Soft error red
	colorBlue   = lipgloss.Color("#9DB8FF") // Light blue accent
	colorMauve  = lipgloss.Color("#E8C8FF") // Light mauve
	colorYellow = lipgloss.Color("#FFF4B8") // Soft yellow
	colorTeal   = lipgloss.Color("#B4FFDD") // Same as Mint
)

// ─── Layout Styles ───────────────────────────────────────────────────────────

var (
	// App frame
	appStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Padding(1, 2)

	// Header bar (section titles - no border, consistent with dashboard layout)
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorLavender).
			MarginBottom(1)

	// Footer / help bar
	helpStyle = lipgloss.NewStyle().
			Foreground(colorSubtext).
			MarginTop(1)

	// Error message
	errorStyle = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true).
			Padding(0, 1)

	// Update available banner
	updateBannerStyle = lipgloss.NewStyle().
				Foreground(colorYellow).
				Bold(true).
				Padding(0, 1)
)

// ─── Dashboard Styles ────────────────────────────────────────────────────────

var (
	// Stat metric (number + label inline)
	statMetricStyle = lipgloss.NewStyle().
			Foreground(colorMint).
			Bold(true)

	// Stat label (dimmed)
	statLabelDimStyle = lipgloss.NewStyle().
				Foreground(colorSubtext)

	// Legacy stat styles (kept for compatibility)
	statNumberStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorGreen).
			Width(8).
			Align(lipgloss.Right)

	statLabelStyle = lipgloss.NewStyle().
			Foreground(colorText).
			PaddingLeft(2)

	// Stat card container
	statCardStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorLavender).
			Padding(1, 2).
			MarginBottom(1)

	// Menu item (normal)
	menuItemStyle = lipgloss.NewStyle().
			Foreground(colorText).
			PaddingLeft(2)

	// Menu item (selected)
	menuSelectedStyle = lipgloss.NewStyle().
				Foreground(colorLavender).
				Bold(true).
				PaddingLeft(1)

	// Dashboard title
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorMauve).
			MarginBottom(1)
)

// ─── List Styles ─────────────────────────────────────────────────────────────

var (
	// List item (normal)
	listItemStyle = lipgloss.NewStyle().
			Foreground(colorText).
			PaddingLeft(2)

	// List item (selected/cursor)
	listSelectedStyle = lipgloss.NewStyle().
				Foreground(colorLavender).
				Bold(true).
				PaddingLeft(1)

	// Observation type badge
	typeBadgeStyle = lipgloss.NewStyle().
			Foreground(colorPeach).
			Bold(true)

	// Observation ID
	idStyle = lipgloss.NewStyle().
		Foreground(colorBlue)

	// Timestamp
	timestampStyle = lipgloss.NewStyle().
			Foreground(colorSubtext).
			Italic(true)

	// Project name
	projectStyle = lipgloss.NewStyle().
			Foreground(colorYellow)

	// Content preview
	contentPreviewStyle = lipgloss.NewStyle().
				Foreground(colorSubtext).
				PaddingLeft(4)
)

// ─── Detail View Styles ──────────────────────────────────────────────────────

var (
	// Section heading in detail views
	sectionHeadingStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorMauve).
				MarginTop(1).
				MarginBottom(1)

	// Detail content
	detailContentStyle = lipgloss.NewStyle().
				Foreground(colorText).
				PaddingLeft(2)

	// Detail label
	detailLabelStyle = lipgloss.NewStyle().
				Foreground(colorSubtext).
				Width(14).
				Align(lipgloss.Right).
				PaddingRight(1)

	// Detail value
	detailValueStyle = lipgloss.NewStyle().
				Foreground(colorText)
)

// ─── Timeline Styles ─────────────────────────────────────────────────────────

var (
	// Timeline focus observation
	timelineFocusStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(colorLavender).
				Padding(0, 1)

	// Timeline before/after items
	timelineItemStyle = lipgloss.NewStyle().
				Foreground(colorSubtext).
				PaddingLeft(2)

	// Timeline arrow connector
	timelineConnectorStyle = lipgloss.NewStyle().
				Foreground(colorOverlay)
)

// ─── Search Styles ───────────────────────────────────────────────────────────

var (
	searchInputStyle = lipgloss.NewStyle().
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(colorLavender).
				Foreground(colorText).
				Padding(0, 1).
				MarginBottom(1)

	searchHighlightStyle = lipgloss.NewStyle().
				Foreground(colorTeal).
				Bold(true)

	noResultsStyle = lipgloss.NewStyle().
			Foreground(colorSubtext).
			Italic(true).
			PaddingLeft(2).
			MarginTop(1)
)

// ─── Purge Styles ───────────────────────────────────────────────────────────────

var (
	// Warning text
	warningStyle = lipgloss.NewStyle().
			Foreground(colorPeach).
			Bold(true)

	// Success text
	successStyle = lipgloss.NewStyle().
			Foreground(colorGreen).
			Bold(true)

	// Menu label (used in purge confirmation)
	menuLabelStyle = lipgloss.NewStyle().
			Foreground(colorText).
			Width(25)
)
