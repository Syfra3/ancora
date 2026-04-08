// Package tui implements the Bubbletea terminal UI for Ancora.
//
// Following the Gentleman Bubbletea patterns:
// - Screen constants as iota
// - Single Model struct holds ALL state
// - Update() with type switch
// - Per-screen key handlers returning (tea.Model, tea.Cmd)
// - Vim keys (j/k) for navigation
// - PrevScreen for back navigation
package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Syfra3/ancora/internal/embed"
	"github.com/Syfra3/ancora/internal/setup"
	"github.com/Syfra3/ancora/internal/store"
	"github.com/Syfra3/ancora/internal/version"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── Screens ─────────────────────────────────────────────────────────────────

type Screen int

const (
	ScreenDashboard Screen = iota
	ScreenSearch
	ScreenSearchResults
	ScreenRecent
	ScreenObservationDetail
	ScreenTimeline
	ScreenSessions
	ScreenSessionDetail
	ScreenProjects
	ScreenSetup
	ScreenSetupEnv
	ScreenMoveObservation
	ScreenPurge
	ScreenUninstall
)

// ─── Custom Messages ─────────────────────────────────────────────────────────

type updateCheckMsg struct {
	result version.CheckResult
}

type statsLoadedMsg struct {
	stats *store.Stats
	err   error
}

type searchResultsMsg struct {
	results []store.SearchResult
	query   string
	err     error
}

type recentObservationsMsg struct {
	observations []store.Observation
	err          error
}

type observationDetailMsg struct {
	observation *store.Observation
	err         error
}

type timelineMsg struct {
	timeline *store.TimelineResult
	err      error
}

type recentSessionsMsg struct {
	sessions []store.SessionSummary
	err      error
}

type sessionObservationsMsg struct {
	observations []store.Observation
	err          error
}

type setupInstallMsg struct {
	result *setup.Result
	err    error
}

type projectsWithStatsMsg struct {
	projects []ProjectWithEnrollment
	err      error
}

type observationMovedMsg struct {
	err error
}

type moveProjectsLoadedMsg struct {
	projects []string
	err      error
}

type purgeResultMsg struct {
	result *store.PurgeResult
	err    error
}

type installationCheckMsg struct {
	isInstalled bool
}

type uninstallResultMsg struct {
	err error
}

// ProjectWithEnrollment extends store.ProjectStats with sync enrollment status
type ProjectWithEnrollment struct {
	store.ProjectStats
	SyncEnabled bool
}

// ─── Model ───────────────────────────────────────────────────────────────────

type Model struct {
	store      *store.Store
	Version    string
	Screen     Screen
	PrevScreen Screen
	Width      int
	Height     int
	Cursor     int
	Scroll     int

	// Update notification
	UpdateStatus version.CheckStatus
	UpdateMsg    string

	// Error display
	ErrorMsg string

	// Dashboard
	Stats            *store.Stats
	IsFullyInstalled bool // true if database + embeddings are available
	MCPRunning       bool // true if MCP server process is detected

	// Search
	SearchInput   textinput.Model
	SearchQuery   string
	SearchResults []store.SearchResult

	// Recent observations
	RecentObservations []store.Observation

	// Observation detail
	SelectedObservation *store.Observation
	DetailScroll        int

	// Timeline
	Timeline *store.TimelineResult

	// Sessions
	Sessions            []store.SessionSummary
	SelectedSessionIdx  int
	SessionObservations []store.Observation
	SessionDetailScroll int

	// Projects
	Projects           []ProjectWithEnrollment
	ProjectScopeFilter string // "" = all, "project", "personal"

	// Move observation (edit project/scope)
	MoveObservationID     int64
	MoveProjectList       []string // available projects to choose from
	MoveProjectCursor     int
	MoveScopeCursor       int    // 0 = project, 1 = personal
	MoveActiveColumn      string // "project" or "scope" - which column is focused
	MoveDone              bool
	MoveError             string
	MoveRestorePrevScreen Screen // saves observation detail's PrevScreen to restore after move

	// Setup
	SetupAgents           []setup.Agent
	SetupResult           *setup.Result
	SetupError            string
	SetupDone             bool
	SetupInstalling       bool
	SetupInstallingName   string // agent name being installed (for display)
	SetupAllowlistPrompt  bool   // true = showing y/n prompt for allowlist
	SetupAllowlistApplied bool   // true = allowlist was added successfully
	SetupAllowlistError   string // error message if allowlist injection failed
	SetupSpinner          spinner.Model

	// Setup Environment (multi-step wizard)
	SetupEnvStep          int    // 0=select plugin, 1=running setup
	SetupEnvPlugin        string // "opencode" or "claude-code"
	SetupEnvRunning       bool
	SetupEnvModelDone     bool
	SetupEnvModelProgress float64 // 0.0 to 1.0
	SetupEnvBackfillDone  bool
	SetupEnvPluginDone    bool
	SetupEnvError         string
	SetupEnvDownloader    *setup.Downloader // downloader instance for progress tracking

	// Purge
	PurgeConfirm       bool // true = user has confirmed purge
	PurgeResult        *store.PurgeResult
	PurgeError         string
	PurgeConfirmCursor int // 0 = "No, go back", 1 = "Yes, delete everything"

	// Uninstall
	UninstallConfirmCursor int  // 0 = "No, go back", 1 = "Yes, uninstall"
	UninstallDone          bool // true = uninstall completed
	UninstallError         string
}

// New creates a new TUI model connected to the given store.
func New(s *store.Store, version string) Model {
	ti := textinput.New()
	ti.Placeholder = "Search memories..."
	ti.CharLimit = 256
	ti.Width = 60

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorLavender)

	return Model{
		store:            s,
		Version:          version,
		Screen:           ScreenDashboard,
		SearchInput:      ti,
		MoveActiveColumn: "project",
		MoveScopeCursor:  0,
		SetupSpinner:     sp,
	}
}

// Init loads initial data (stats for the dashboard).
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		loadStats(m.store),
		checkForUpdate(m.Version),
		checkInstallation,
		checkMCPStatus,
		tea.EnterAltScreen,
	)
}

// checkInstallation verifies if Ancora is fully installed
func checkInstallation() tea.Msg {
	modelPath := filepath.Join(embed.ModelInstallPath(), embed.ModelFileName)
	_, err := os.Stat(modelPath)
	return installationCheckMsg{
		isInstalled: err == nil,
	}
}

// checkMCPStatus checks if the MCP server process is running
func checkMCPStatus() tea.Msg {
	return mcpStatusMsg{
		running: isMCPRunning(),
	}
}

type mcpStatusMsg struct {
	running bool
}

// isMCPRunning checks if the ancora MCP server is running using pgrep
func isMCPRunning() bool {
	cmd := exec.Command("pgrep", "-f", "ancora mcp")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) != ""
}

// ─── Commands (data loading) ─────────────────────────────────────────────────

func checkForUpdate(v string) tea.Cmd {
	return func() tea.Msg {
		return updateCheckMsg{result: version.CheckLatest(v)}
	}
}

func loadStats(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		stats, err := s.Stats()
		return statsLoadedMsg{stats: stats, err: err}
	}
}

func searchMemories(s *store.Store, query string) tea.Cmd {
	return func() tea.Msg {
		results, err := s.Search(query, store.SearchOptions{Limit: 50})
		return searchResultsMsg{results: results, query: query, err: err}
	}
}

func loadRecentObservations(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		obs, err := s.AllObservations("", "", 50)
		return recentObservationsMsg{observations: obs, err: err}
	}
}

func loadObservationDetail(s *store.Store, id int64) tea.Cmd {
	return func() tea.Msg {
		obs, err := s.GetObservation(id)
		return observationDetailMsg{observation: obs, err: err}
	}
}

func loadTimeline(s *store.Store, obsID int64) tea.Cmd {
	return func() tea.Msg {
		tl, err := s.Timeline(obsID, 10, 10)
		return timelineMsg{timeline: tl, err: err}
	}
}

func loadRecentSessions(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		sessions, err := s.AllSessions("", 50)
		return recentSessionsMsg{sessions: sessions, err: err}
	}
}

func loadSessionObservations(s *store.Store, sessionID string) tea.Cmd {
	return func() tea.Msg {
		obs, err := s.SessionObservations(sessionID, 200)
		return sessionObservationsMsg{observations: obs, err: err}
	}
}

func loadProjectsWithStats(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		stats, err := s.ListProjectsWithStats()
		if err != nil {
			return projectsWithStatsMsg{err: err}
		}

		enrolled, err := s.ListEnrolledProjects()
		if err != nil {
			return projectsWithStatsMsg{err: err}
		}

		enrolledMap := make(map[string]bool)
		for _, ep := range enrolled {
			enrolledMap[ep.Project] = true
		}

		var projects []ProjectWithEnrollment
		for _, ps := range stats {
			projects = append(projects, ProjectWithEnrollment{
				ProjectStats: ps,
				SyncEnabled:  enrolledMap[ps.Name],
			})
		}

		return projectsWithStatsMsg{projects: projects, err: nil}
	}
}

func installAgent(agentName string) tea.Cmd {
	return func() tea.Msg {
		result, err := installAgentFn(agentName)
		return setupInstallMsg{result: result, err: err}
	}
}

func loadMoveProjects(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		stats, err := s.ListProjectsWithStats()
		if err != nil {
			return moveProjectsLoadedMsg{err: err}
		}
		var names []string
		for _, ps := range stats {
			names = append(names, ps.Name)
		}
		return moveProjectsLoadedMsg{projects: names, err: nil}
	}
}

func moveObservation(s *store.Store, id int64, workspace *string, visibility *string) tea.Cmd {
	return func() tea.Msg {
		_, err := s.UpdateObservation(id, store.UpdateObservationParams{
			Workspace:  workspace,
			Visibility: visibility,
		})
		return observationMovedMsg{err: err}
	}
}

func purgeDatabase(s *store.Store) tea.Cmd {
	return func() tea.Msg {
		result, err := s.PurgeAll()
		return purgeResultMsg{result: result, err: err}
	}
}

func uninstallAncora() tea.Cmd {
	return func() tea.Msg {
		// Remove embedding model
		modelPath := filepath.Join(embed.ModelInstallPath(), embed.ModelFileName)
		if err := os.Remove(modelPath); err != nil && !os.IsNotExist(err) {
			return uninstallResultMsg{err: err}
		}

		// Remove model directory if empty
		modelDir := embed.ModelInstallPath()
		if err := os.Remove(modelDir); err != nil && !os.IsNotExist(err) {
			// Directory not empty or other error - ignore
		}

		return uninstallResultMsg{err: nil}
	}
}

var installAgentFn = setup.Install
var addClaudeCodeAllowlistFn = setup.AddClaudeCodeAllowlist
