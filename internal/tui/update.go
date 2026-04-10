package tui

import (
	"fmt"
	"path/filepath"

	"github.com/Syfra3/ancora/internal/classify"
	"github.com/Syfra3/ancora/internal/embed"
	"github.com/Syfra3/ancora/internal/setup"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

// ─── Update ──────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// Global quit — always works
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		// If search input is focused, let it handle most keys
		if m.Screen == ScreenSearch && m.SearchInput.Focused() {
			return m.handleSearchInputKeys(msg)
		}
		return m.handleKeyPress(msg.String())

	// ─── Data loaded messages ────────────────────────────────────────────
	case updateCheckMsg:
		m.UpdateStatus = msg.result.Status
		m.UpdateMsg = msg.result.Message
		return m, nil

	case installationCheckMsg:
		m.IsFullyInstalled = msg.isInstalled
		return m, nil

	case mcpStatusMsg:
		m.MCPRunning = msg.running
		return m, nil

	case statsLoadedMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.Stats = msg.stats
		return m, nil

	case searchResultsMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.SearchResults = msg.results
		m.SearchQuery = msg.query
		m.Screen = ScreenSearchResults
		m.Cursor = 0
		m.Scroll = 0
		return m, nil

	case recentObservationsMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.RecentObservations = msg.observations
		return m, nil

	case observationDetailMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.SelectedObservation = msg.observation
		m.Screen = ScreenObservationDetail
		m.DetailScroll = 0
		return m, nil

	case timelineMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.Timeline = msg.timeline
		m.Screen = ScreenTimeline
		m.Scroll = 0
		return m, nil

	case recentSessionsMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.Sessions = msg.sessions
		return m, nil

	case sessionObservationsMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.SessionObservations = msg.observations
		m.Screen = ScreenSessionDetail
		m.Cursor = 0
		m.SessionDetailScroll = 0
		return m, nil

	case projectsWithStatsMsg:
		if msg.err != nil {
			m.ErrorMsg = msg.err.Error()
			return m, nil
		}
		m.Projects = msg.projects
		return m, nil

	case observationMovedMsg:
		if msg.err != nil {
			m.MoveError = msg.err.Error()
			return m, nil
		}
		m.MoveDone = true
		m.MoveError = ""
		return m, nil

	case moveProjectsLoadedMsg:
		if msg.err != nil {
			m.MoveError = msg.err.Error()
			return m, nil
		}
		m.MoveProjectList = msg.projects
		// Set cursor to current workspace if it exists in the list
		if m.SelectedObservation != nil && m.SelectedObservation.Workspace != nil {
			for i, proj := range msg.projects {
				if proj == *m.SelectedObservation.Workspace {
					m.MoveProjectCursor = i
					break
				}
			}
		}
		return m, nil

	case setupInstallMsg:
		m.SetupInstalling = false
		if msg.err != nil {
			m.SetupDone = true
			m.SetupError = msg.err.Error()
			return m, nil
		}
		m.SetupResult = msg.result
		m.SetupError = ""
		// For claude-code, show allowlist prompt before marking done
		if msg.result != nil && msg.result.Agent == "claude-code" {
			m.SetupAllowlistPrompt = true
			return m, nil
		}
		m.SetupDone = true
		return m, nil

	case spinner.TickMsg:
		// Only forward spinner ticks when we're actually installing
		if m.SetupInstalling {
			var cmd tea.Cmd
			m.SetupSpinner, cmd = m.SetupSpinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case purgeResultMsg:
		if msg.err != nil {
			m.PurgeError = msg.err.Error()
			return m, nil
		}
		m.PurgeResult = msg.result
		m.PurgeError = ""
		return m, nil

	case uninstallResultMsg:
		if msg.err != nil {
			m.UninstallError = msg.err.Error()
			return m, nil
		}
		m.UninstallDone = true
		m.UninstallError = ""
		return m, nil

	case setupEnvCompleteMsg:
		m.SetupEnvRunning = false
		if msg.err != nil {
			m.SetupEnvError = msg.err.Error()
		} else {
			m.SetupEnvModelDone = true
			m.SetupEnvModelProgress = 1.0
			m.SetupEnvBackfillDone = true
			m.SetupEnvPluginDone = true
		}
		return m, nil

	case setupEnvProgressMsg:
		switch msg.step {
		case "model":
			m.SetupEnvModelProgress = msg.progress
			if msg.progress >= 1.0 {
				m.SetupEnvModelDone = true
				// Model done, start plugin install
				return m, installSetupEnvPlugin(m.SetupEnvPlugin)
			}
			// Continue listening for progress
			return m, listenForDownloadProgress(m.SetupEnvDownloader)
		case "backfill":
			if msg.progress >= 1.0 {
				m.SetupEnvBackfillDone = true
			}
		case "plugin":
			if msg.progress >= 1.0 {
				m.SetupEnvPluginDone = true
				m.SetupEnvRunning = false
			}
		}
		return m, nil
	}

	return m, nil
}

// ─── Key Press Router ────────────────────────────────────────────────────────

func (m Model) handleKeyPress(key string) (tea.Model, tea.Cmd) {
	// Clear error on any keypress
	m.ErrorMsg = ""

	switch m.Screen {
	case ScreenDashboard:
		return m.handleDashboardKeys(key)
	case ScreenSearch:
		return m.handleSearchKeys(key)
	case ScreenSearchResults:
		return m.handleSearchResultsKeys(key)
	case ScreenRecent:
		return m.handleRecentKeys(key)
	case ScreenObservationDetail:
		return m.handleObservationDetailKeys(key)
	case ScreenTimeline:
		return m.handleTimelineKeys(key)
	case ScreenSessions:
		return m.handleSessionsKeys(key)
	case ScreenSessionDetail:
		return m.handleSessionDetailKeys(key)
	case ScreenProjects:
		return m.handleProjectsKeys(key)
	case ScreenSetup:
		return m.handleSetupKeys(key)
	case ScreenSetupEnv:
		return m.handleSetupEnvKeys(key)
	case ScreenMoveObservation:
		return m.handleMoveObservationKeys(key)
	case ScreenPurge:
		return m.handlePurgeKeys(key)
	case ScreenUninstall:
		return m.handleUninstallKeys(key)
	case ScreenSettings:
		return m.handleSettingsKeys(key)
	}
	return m, nil
}

// ─── Dashboard ───────────────────────────────────────────────────────────────

// MenuItem represents a dashboard menu option with description
type MenuItem struct {
	Label       string
	Description string
	Action      string // identifier for the action (search, recent, sessions, etc.)
}

// getDashboardMenuItems returns menu items based on installation status
func getDashboardMenuItems(isInstalled bool) []MenuItem {
	if !isInstalled {
		// Not installed: only show setup and exit
		return []MenuItem{
			{"Setup environment", "Install embedding model, run backfill, install plugin", "setup"},
			{"Exit", "Close the TUI", "exit"},
		}
	}

	// Fully installed: show all options, rename setup to upgrade/update
	return []MenuItem{
		{"Search memories", "Find observations by content or metadata", "search"},
		{"Recent observations", "Browse latest saved memories", "recent"},
		{"Browse sessions", "View all coding sessions", "sessions"},
		{"Browse projects", "View projects with sync status and scope", "projects"},
		{"Search ranking", "Configure workspace-aware result ranking preset", "settings"},
		{"Update Ancora", "Update to new version and reinstall without losing data", "setup"},
		{"Uninstall Ancora", "Remove Ancora completely from your system", "uninstall"},
		{"Purge database", "DELETE ALL data - observations, sessions, prompts", "purge"},
		{"Exit", "Close the TUI", "exit"},
	}
}

func (m Model) handleDashboardKeys(key string) (tea.Model, tea.Cmd) {
	menuItems := getDashboardMenuItems(m.IsFullyInstalled)

	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down", "j":
		if m.Cursor < len(menuItems)-1 {
			m.Cursor++
		}
	case "enter", " ":
		return m.handleDashboardSelection()
	case "s", "/":
		// Only allow if installed
		if !m.IsFullyInstalled {
			return m, nil
		}
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenSearch
		m.Cursor = 0
		m.SearchInput.SetValue("")
		m.SearchInput.Focus()
		return m, nil
	case "p":
		// Only allow if installed
		if !m.IsFullyInstalled {
			return m, nil
		}
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenProjects
		m.Cursor = 0
		m.Scroll = 0
		m.ProjectScopeFilter = ""
		return m, loadProjectsWithStats(m.store)
	case "q":
		// Check if the current cursor is on Exit action
		if m.Cursor < len(menuItems) && menuItems[m.Cursor].Action == "exit" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) handleDashboardSelection() (tea.Model, tea.Cmd) {
	menuItems := getDashboardMenuItems(m.IsFullyInstalled)

	if m.Cursor >= len(menuItems) {
		return m, nil
	}

	selectedAction := menuItems[m.Cursor].Action

	switch selectedAction {
	case "search":
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenSearch
		m.Cursor = 0
		m.SearchInput.SetValue("")
		m.SearchInput.Focus()
		return m, nil

	case "recent":
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenRecent
		m.Cursor = 0
		m.Scroll = 0
		return m, loadRecentObservations(m.store)

	case "sessions":
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenSessions
		m.Cursor = 0
		m.Scroll = 0
		return m, loadRecentSessions(m.store)

	case "projects":
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenProjects
		m.Cursor = 0
		m.Scroll = 0
		m.ProjectScopeFilter = ""
		return m, loadProjectsWithStats(m.store)

	case "settings":
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenSettings
		m.Cursor = int(m.ClassifyConfig.Preset) // pre-select current preset
		m.SettingsPresetSaved = false
		return m, nil

	case "setup":
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenSetupEnv
		m.Cursor = 0
		m.SetupEnvStep = 0
		m.SetupEnvPlugin = ""
		m.SetupEnvRunning = false
		m.SetupEnvModelDone = false
		m.SetupEnvModelProgress = 0.0
		m.SetupEnvBackfillDone = false
		m.SetupEnvPluginDone = false
		m.SetupEnvError = ""
		return m, nil

	case "purge":
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenPurge
		m.PurgeConfirm = false
		m.PurgeConfirmCursor = 0
		m.PurgeResult = nil
		m.PurgeError = ""
		return m, nil

	case "uninstall":
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenUninstall
		m.UninstallConfirmCursor = 0
		m.UninstallDone = false
		m.UninstallError = ""
		return m, nil

	case "exit":
		return m, tea.Quit
	}

	return m, nil
}

// ─── Search Input ────────────────────────────────────────────────────────────

func (m Model) handleSearchInputKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		query := m.SearchInput.Value()
		if query != "" {
			m.SearchInput.Blur()
			return m, searchMemories(m.store, query)
		}
		return m, nil
	case "esc":
		m.SearchInput.Blur()
		m.Screen = ScreenDashboard
		m.Cursor = 0
		return m, nil
	}

	// Let the text input component handle everything else
	var cmd tea.Cmd
	m.SearchInput, cmd = m.SearchInput.Update(msg)
	return m, cmd
}

func (m Model) handleSearchKeys(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q":
		m.Screen = ScreenDashboard
		m.Cursor = 0
		return m, nil
	case "i", "/":
		m.SearchInput.Focus()
		return m, nil
	}
	return m, nil
}

// ─── Search Results ──────────────────────────────────────────────────────────

func (m Model) handleSearchResultsKeys(key string) (tea.Model, tea.Cmd) {
	visibleItems := (m.Height - 10) / 2 // 2 lines per observation item
	if visibleItems < 3 {
		visibleItems = 3
	}

	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
			// Scroll up if cursor goes above visible area
			if m.Cursor < m.Scroll {
				m.Scroll = m.Cursor
			}
		}
	case "down", "j":
		if m.Cursor < len(m.SearchResults)-1 {
			m.Cursor++
			// Scroll down if cursor goes below visible area
			if m.Cursor >= m.Scroll+visibleItems {
				m.Scroll = m.Cursor - visibleItems + 1
			}
		}
	case "enter":
		if len(m.SearchResults) > 0 && m.Cursor < len(m.SearchResults) {
			obsID := m.SearchResults[m.Cursor].ID
			m.PrevScreen = ScreenSearchResults
			return m, loadObservationDetail(m.store, obsID)
		}
	case "t":
		// Timeline for selected result
		if len(m.SearchResults) > 0 && m.Cursor < len(m.SearchResults) {
			obsID := m.SearchResults[m.Cursor].ID
			m.PrevScreen = ScreenSearchResults
			return m, loadTimeline(m.store, obsID)
		}
	case "/", "s":
		m.PrevScreen = ScreenSearchResults
		m.Screen = ScreenSearch
		m.SearchInput.Focus()
		return m, nil
	case "esc", "q":
		m.PrevScreen = ScreenDashboard
		m.Screen = ScreenSearch
		m.Cursor = 0
		m.Scroll = 0
		m.SearchInput.Focus()
		return m, nil
	}
	return m, nil
}

// ─── Recent Observations ─────────────────────────────────────────────────────

func (m Model) handleRecentKeys(key string) (tea.Model, tea.Cmd) {
	visibleItems := (m.Height - 8) / 2 // 2 lines per observation item
	if visibleItems < 3 {
		visibleItems = 3
	}

	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
			if m.Cursor < m.Scroll {
				m.Scroll = m.Cursor
			}
		}
	case "down", "j":
		if m.Cursor < len(m.RecentObservations)-1 {
			m.Cursor++
			if m.Cursor >= m.Scroll+visibleItems {
				m.Scroll = m.Cursor - visibleItems + 1
			}
		}
	case "enter":
		if len(m.RecentObservations) > 0 && m.Cursor < len(m.RecentObservations) {
			obsID := m.RecentObservations[m.Cursor].ID
			m.PrevScreen = ScreenRecent
			return m, loadObservationDetail(m.store, obsID)
		}
	case "t":
		if len(m.RecentObservations) > 0 && m.Cursor < len(m.RecentObservations) {
			obsID := m.RecentObservations[m.Cursor].ID
			m.PrevScreen = ScreenRecent
			return m, loadTimeline(m.store, obsID)
		}
	case "esc", "q":
		m.Screen = ScreenDashboard
		m.Cursor = 0
		m.Scroll = 0
		return m, loadStats(m.store)
	}
	return m, nil
}

// ─── Observation Detail ──────────────────────────────────────────────────────

func (m Model) handleObservationDetailKeys(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.DetailScroll > 0 {
			m.DetailScroll--
		}
	case "down", "j":
		m.DetailScroll++
	case "t":
		// View timeline for this observation
		if m.SelectedObservation != nil {
			return m, loadTimeline(m.store, m.SelectedObservation.ID)
		}
	case "m":
		// Move observation - open move screen
		if m.SelectedObservation != nil {
			// Save the observation detail's PrevScreen so we can restore it when coming back
			m.MoveRestorePrevScreen = m.PrevScreen
			m.PrevScreen = ScreenObservationDetail
			m.Screen = ScreenMoveObservation
			m.MoveObservationID = m.SelectedObservation.ID
			m.MoveDone = false
			m.MoveError = ""
			m.MoveActiveColumn = "project"
			// Set visibility cursor based on current visibility
			if m.SelectedObservation.Visibility == "personal" {
				m.MoveScopeCursor = 1
			} else {
				m.MoveScopeCursor = 0
			}
			return m, loadMoveProjects(m.store)
		}
	case "esc", "q":
		m.Screen = m.PrevScreen
		m.Cursor = 0
		m.DetailScroll = 0
		return m, m.refreshScreen(m.PrevScreen)
	}
	return m, nil
}

// ─── Timeline ────────────────────────────────────────────────────────────────

func (m Model) handleTimelineKeys(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.Scroll > 0 {
			m.Scroll--
		}
	case "down", "j":
		m.Scroll++
	case "esc", "q":
		m.Screen = m.PrevScreen
		m.Cursor = 0
		m.Scroll = 0
		return m, m.refreshScreen(m.PrevScreen)
	}
	return m, nil
}

// ─── Sessions ────────────────────────────────────────────────────────────────

func (m Model) handleSessionsKeys(key string) (tea.Model, tea.Cmd) {
	visibleItems := m.Height - 8
	if visibleItems < 5 {
		visibleItems = 5
	}

	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
			if m.Cursor < m.Scroll {
				m.Scroll = m.Cursor
			}
		}
	case "down", "j":
		if m.Cursor < len(m.Sessions)-1 {
			m.Cursor++
			if m.Cursor >= m.Scroll+visibleItems {
				m.Scroll = m.Cursor - visibleItems + 1
			}
		}
	case "enter":
		if len(m.Sessions) > 0 && m.Cursor < len(m.Sessions) {
			m.SelectedSessionIdx = m.Cursor
			m.PrevScreen = ScreenSessions
			sessionID := m.Sessions[m.Cursor].ID
			return m, loadSessionObservations(m.store, sessionID)
		}
	case "esc", "q":
		m.Screen = ScreenDashboard
		m.Cursor = 0
		m.Scroll = 0
		return m, loadStats(m.store)
	}
	return m, nil
}

// ─── Session Detail ──────────────────────────────────────────────────────────

func (m Model) handleSessionDetailKeys(key string) (tea.Model, tea.Cmd) {
	visibleItems := (m.Height - 12) / 2 // 2 lines per observation item
	if visibleItems < 3 {
		visibleItems = 3
	}

	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
			if m.Cursor < m.SessionDetailScroll {
				m.SessionDetailScroll = m.Cursor
			}
		}
	case "down", "j":
		if m.Cursor < len(m.SessionObservations)-1 {
			m.Cursor++
			if m.Cursor >= m.SessionDetailScroll+visibleItems {
				m.SessionDetailScroll = m.Cursor - visibleItems + 1
			}
		}
	case "enter":
		if len(m.SessionObservations) > 0 && m.Cursor < len(m.SessionObservations) {
			obsID := m.SessionObservations[m.Cursor].ID
			m.PrevScreen = ScreenSessionDetail
			return m, loadObservationDetail(m.store, obsID)
		}
	case "t":
		if len(m.SessionObservations) > 0 && m.Cursor < len(m.SessionObservations) {
			obsID := m.SessionObservations[m.Cursor].ID
			m.PrevScreen = ScreenSessionDetail
			return m, loadTimeline(m.store, obsID)
		}
	case "esc", "q":
		m.Screen = ScreenSessions
		m.Cursor = m.SelectedSessionIdx
		m.SessionDetailScroll = 0
		return m, loadRecentSessions(m.store)
	}
	return m, nil
}

// ─── Projects ────────────────────────────────────────────────────────────────

func (m Model) handleProjectsKeys(key string) (tea.Model, tea.Cmd) {
	visibleItems := m.Height - 12
	if visibleItems < 5 {
		visibleItems = 5
	}

	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
			if m.Cursor < m.Scroll {
				m.Scroll = m.Cursor
			}
		}
	case "down", "j":
		if m.Cursor < len(m.Projects)-1 {
			m.Cursor++
			if m.Cursor >= m.Scroll+visibleItems {
				m.Scroll = m.Cursor - visibleItems + 1
			}
		}
	case "f":
		// Cycle through scope filters: "" -> "project" -> "personal" -> ""
		switch m.ProjectScopeFilter {
		case "":
			m.ProjectScopeFilter = "project"
		case "project":
			m.ProjectScopeFilter = "personal"
		case "personal":
			m.ProjectScopeFilter = ""
		}
		m.Cursor = 0
		m.Scroll = 0
	case "enter":
		if len(m.Projects) > 0 && m.Cursor < len(m.Projects) {
			// View observations for selected project
			selectedProject := m.Projects[m.Cursor].Name
			m.PrevScreen = ScreenProjects
			m.Screen = ScreenRecent
			m.Cursor = 0
			m.Scroll = 0
			// Load observations for this project with current scope filter
			return m, func() tea.Msg {
				obs, err := m.store.AllObservations(selectedProject, m.ProjectScopeFilter, 50)
				return recentObservationsMsg{observations: obs, err: err}
			}
		}
	case "esc", "q":
		m.Screen = ScreenDashboard
		m.Cursor = 0
		m.Scroll = 0
		m.ProjectScopeFilter = ""
		return m, loadStats(m.store)
	}
	return m, nil
}

// ─── Setup ───────────────────────────────────────────────────────────────────

func (m Model) handleSetupKeys(key string) (tea.Model, tea.Cmd) {
	// While installing, block all keys
	if m.SetupInstalling {
		return m, nil
	}

	// Allowlist prompt: y/n
	if m.SetupAllowlistPrompt {
		switch key {
		case "y", "Y":
			m.SetupAllowlistPrompt = false
			m.SetupDone = true
			if err := addClaudeCodeAllowlistFn(); err != nil {
				m.SetupAllowlistError = err.Error()
			} else {
				m.SetupAllowlistApplied = true
			}
			return m, nil
		case "n", "N", "esc":
			m.SetupAllowlistPrompt = false
			m.SetupDone = true
			return m, nil
		}
		return m, nil
	}

	// After install completed, any key goes back
	if m.SetupDone {
		switch key {
		case "esc", "q", "enter":
			m.Screen = ScreenDashboard
			m.Cursor = 0
			m.SetupDone = false
			m.SetupResult = nil
			m.SetupError = ""
			m.SetupAllowlistApplied = false
			m.SetupAllowlistError = ""
			return m, loadStats(m.store)
		}
		return m, nil
	}

	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down", "j":
		if m.Cursor < len(m.SetupAgents)-1 {
			m.Cursor++
		}
	case "enter":
		if len(m.SetupAgents) > 0 && m.Cursor < len(m.SetupAgents) {
			agent := m.SetupAgents[m.Cursor]
			m.SetupInstalling = true
			m.SetupInstallingName = agent.Name
			return m, tea.Batch(m.SetupSpinner.Tick, installAgent(agent.Name))
		}
	case "esc", "q":
		m.Screen = ScreenDashboard
		m.Cursor = 0
		return m, loadStats(m.store)
	}
	return m, nil
}

// ─── Setup Environment ────────────────────────────────────────────────────────

func (m Model) handleSetupEnvKeys(key string) (tea.Model, tea.Cmd) {
	// If running, block all keys except viewing
	if m.SetupEnvRunning {
		return m, nil
	}

	// Step 0: Select plugin
	if m.SetupEnvStep == 0 {
		switch key {
		case "up", "k":
			if m.Cursor > 0 {
				m.Cursor--
			}
		case "down", "j":
			if m.Cursor < 1 {
				m.Cursor++
			}
		case "enter":
			if m.Cursor == 0 {
				m.SetupEnvPlugin = "opencode"
			} else {
				m.SetupEnvPlugin = "claude-code"
			}
			m.SetupEnvStep = 1
			m.SetupEnvRunning = true

			// Create downloader
			destPath := filepath.Join(embed.ModelInstallPath(), embed.ModelFileName)
			m.SetupEnvDownloader = setup.NewDownloader(destPath)

			return m, tea.Batch(
				m.SetupSpinner.Tick,
				startSetupEnvDownload(m.SetupEnvDownloader),
				listenForDownloadProgress(m.SetupEnvDownloader),
			)
		case "esc", "q":
			m.Screen = ScreenDashboard
			m.Cursor = 0
			return m, loadStats(m.store)
		}
		return m, nil
	}

	// Step 1: Showing progress/done
	if m.SetupEnvStep == 1 {
		if !m.SetupEnvRunning {
			switch key {
			case "enter", "esc", "q":
				m.Screen = ScreenDashboard
				m.Cursor = 0
				return m, loadStats(m.store)
			}
		}
		return m, nil
	}

	return m, nil
}

func startSetupEnvDownload(downloader *setup.Downloader) tea.Cmd {
	return func() tea.Msg {
		if err := downloader.Download(); err != nil {
			return setupEnvCompleteMsg{err: fmt.Errorf("model download failed: %w", err)}
		}
		return nil
	}
}

func listenForDownloadProgress(downloader *setup.Downloader) tea.Cmd {
	return func() tea.Msg {
		progress, ok := <-downloader.Progress
		if !ok {
			return nil
		}

		if progress.Error != nil {
			return setupEnvCompleteMsg{err: progress.Error}
		}

		var progressPercent float64
		if progress.TotalBytes > 0 {
			progressPercent = float64(progress.BytesDownloaded) / float64(progress.TotalBytes)
		}

		return setupEnvProgressMsg{
			step:     "model",
			progress: progressPercent,
		}
	}
}

func installSetupEnvPlugin(plugin string) tea.Cmd {
	return func() tea.Msg {
		_, err := setup.Install(plugin)
		if err != nil {
			return setupEnvCompleteMsg{err: fmt.Errorf("plugin install failed: %w", err)}
		}

		return setupEnvProgressMsg{
			step:     "plugin",
			progress: 1.0,
		}
	}
}

type setupEnvCompleteMsg struct{ err error }
type setupEnvProgressMsg struct {
	step     string // "model", "backfill", "plugin"
	progress float64
}

// ─── Move Observation ────────────────────────────────────────────────────────

func (m Model) handleMoveObservationKeys(key string) (tea.Model, tea.Cmd) {
	// If move is done, only allow esc to go back
	if m.MoveDone {
		if key == "esc" || key == "q" || key == "enter" {
			m.Screen = ScreenObservationDetail
			m.MoveDone = false
			m.MoveError = ""
			// Restore the original PrevScreen
			m.PrevScreen = m.MoveRestorePrevScreen
			return m, loadObservationDetail(m.store, m.MoveObservationID)
		}
		return m, nil
	}

	switch key {
	case "tab":
		// Switch between project and scope columns
		if m.MoveActiveColumn == "project" {
			m.MoveActiveColumn = "scope"
		} else {
			m.MoveActiveColumn = "project"
		}
	case "up", "k":
		if m.MoveActiveColumn == "project" {
			if m.MoveProjectCursor > 0 {
				m.MoveProjectCursor--
			}
		} else {
			if m.MoveScopeCursor > 0 {
				m.MoveScopeCursor--
			}
		}
	case "down", "j":
		if m.MoveActiveColumn == "project" {
			if m.MoveProjectCursor < len(m.MoveProjectList)-1 {
				m.MoveProjectCursor++
			}
		} else {
			if m.MoveScopeCursor < 1 {
				m.MoveScopeCursor++
			}
		}
	case "enter":
		// Save changes - get selected project and scope
		var newProject *string
		var newScope *string

		selectedProject := m.MoveProjectList[m.MoveProjectCursor]
		newProject = &selectedProject

		if m.MoveScopeCursor == 0 {
			scope := "project"
			newScope = &scope
		} else {
			scope := "personal"
			newScope = &scope
		}

		return m, moveObservation(m.store, m.MoveObservationID, newProject, newScope)
	case "esc", "q":
		m.Screen = ScreenObservationDetail
		m.MoveDone = false
		m.MoveError = ""
		// Restore the original PrevScreen that observation detail had before we opened move screen
		m.PrevScreen = m.MoveRestorePrevScreen
		return m, loadObservationDetail(m.store, m.MoveObservationID)
	}
	return m, nil
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// refreshScreen returns the appropriate data-loading Cmd for a given screen.
// Used when navigating back so lists show fresh data from the DB.
func (m Model) refreshScreen(screen Screen) tea.Cmd {
	switch screen {
	case ScreenDashboard:
		return loadStats(m.store)
	case ScreenRecent:
		return loadRecentObservations(m.store)
	case ScreenSessions:
		return loadRecentSessions(m.store)
	default:
		return nil
	}
}

// ─── Purge Database ─────────────────────────────────────────────────────────────

func (m Model) handlePurgeKeys(key string) (tea.Model, tea.Cmd) {
	// If purge is done (success), any key returns to dashboard
	if m.PurgeResult != nil {
		if key == "esc" || key == "q" || key == "enter" {
			m.Screen = ScreenDashboard
			m.Cursor = 0
			m.PurgeConfirm = false
			m.PurgeConfirmCursor = 0
			m.PurgeResult = nil
			m.PurgeError = ""
			return m, loadStats(m.store)
		}
		return m, nil
	}

	switch key {
	case "up", "k":
		if m.PurgeConfirmCursor > 0 {
			m.PurgeConfirmCursor--
		}
	case "down", "j":
		if m.PurgeConfirmCursor < 1 {
			m.PurgeConfirmCursor++
		}
	case "enter":
		if m.PurgeConfirmCursor == 1 {
			// User confirmed - execute purge
			return m, purgeDatabase(m.store)
		} else {
			// User chose "No" - go back
			m.Screen = ScreenDashboard
			m.Cursor = 0
			return m, nil
		}
	case "esc", "q":
		m.Screen = ScreenDashboard
		m.Cursor = 0
		return m, nil
	}
	return m, nil
}

// ─── Uninstall ──────────────────────────────────────────────────────────────

func (m Model) handleUninstallKeys(key string) (tea.Model, tea.Cmd) {
	// If uninstall is done (success), any key exits the application
	if m.UninstallDone {
		if key == "esc" || key == "q" || key == "enter" {
			return m, tea.Quit
		}
		return m, nil
	}

	switch key {
	case "up", "k":
		if m.UninstallConfirmCursor > 0 {
			m.UninstallConfirmCursor--
		}
	case "down", "j":
		if m.UninstallConfirmCursor < 1 {
			m.UninstallConfirmCursor++
		}
	case "enter":
		if m.UninstallConfirmCursor == 1 {
			// User confirmed - execute uninstall
			return m, uninstallAncora()
		} else {
			// User chose "No" - go back
			m.Screen = ScreenDashboard
			m.Cursor = 0
			return m, nil
		}
	case "esc", "q":
		m.Screen = ScreenDashboard
		m.Cursor = 0
		return m, nil
	}
	return m, nil
}

// ─── Settings (Search Ranking Preset) ────────────────────────────────────────

// settingsPresets is the ordered list of presets shown in the settings screen.
var settingsPresets = []classify.TierPreset{
	classify.PresetBalanced,
	classify.PresetStrict,
	classify.PresetFlat,
}

func (m Model) handleSettingsKeys(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		if m.Cursor > 0 {
			m.Cursor--
		}
	case "down", "j":
		if m.Cursor < len(settingsPresets)-1 {
			m.Cursor++
		}
	case "enter", " ":
		preset := settingsPresets[m.Cursor]
		m.ClassifyConfig.Preset = preset
		m.SettingsPresetSaved = true
		_ = classify.SaveClassifyConfig(m.ClassifyConfigPath, m.ClassifyConfig)
		return m, nil
	case "esc", "q":
		m.Screen = ScreenDashboard
		m.Cursor = 0
		m.SettingsPresetSaved = false
		return m, nil
	}
	return m, nil
}
