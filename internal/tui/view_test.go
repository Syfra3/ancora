package tui

import (
	"strings"
	"testing"

	"github.com/Syfra3/ancora/internal/setup"
	"github.com/Syfra3/ancora/internal/store"
)

func TestTruncateStr(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "unchanged", in: "short", max: 10, want: "short"},
		{name: "replaces newlines", in: "a\nb", max: 10, want: "a b"},
		{name: "truncated", in: "abcdefghijklmnopqrstuvwxyz", max: 5, want: "abcde..."},
		{name: "spanish accents", in: "Decisión de arquitectura", max: 8, want: "Decisión..."},
		{name: "emoji", in: "🐛🔧🚀✨🎉💡", max: 3, want: "🐛🔧🚀..."},
		{name: "mixed ascii and multibyte", in: "café☕latte", max: 5, want: "café☕..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateStr(tt.in, tt.max)
			if got != tt.want {
				t.Fatalf("truncateStr() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderObservationListItem(t *testing.T) {
	m := New(nil, "")
	m.Cursor = 1
	project := "engram"

	line := m.renderObservationListItem(
		1,
		42,
		"bugfix",
		"Title here",
		"content line 1\ncontent line 2",
		"2026-01-01",
		&project,
	)

	if !strings.Contains(line, "▸") {
		t.Fatal("selected item should include cursor marker")
	}
	if !strings.Contains(line, "Title here") {
		t.Fatal("line should include title")
	}
	if !strings.Contains(line, "content line 1 content line 2") {
		t.Fatal("content preview should be rendered on second line")
	}
	if !strings.Contains(line, "engram") {
		t.Fatal("project label should be rendered when project is set")
	}
}

func TestViewRouterAndErrorRendering(t *testing.T) {
	m := New(nil, "")
	m.Screen = Screen(999)
	m.ErrorMsg = "boom"

	out := m.View()
	if !strings.Contains(out, "Unknown screen") {
		t.Fatal("unknown screen fallback text missing")
	}
	if !strings.Contains(out, "Error: boom") {
		t.Fatal("error message should be appended to view")
	}
}

func TestViewSearchResultsAndScrollIndicator(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenSearchResults
	m.Height = 14
	m.SearchQuery = "needle"
	m.SearchResults = []store.SearchResult{
		{Observation: store.Observation{ID: 1, Type: "bugfix", Title: "one", Content: "a", CreatedAt: "2026-01-01"}},
		{Observation: store.Observation{ID: 2, Type: "bugfix", Title: "two", Content: "b", CreatedAt: "2026-01-01"}},
		{Observation: store.Observation{ID: 3, Type: "bugfix", Title: "three", Content: "c", CreatedAt: "2026-01-01"}},
		{Observation: store.Observation{ID: 4, Type: "bugfix", Title: "four", Content: "d", CreatedAt: "2026-01-01"}},
	}

	out := m.viewSearchResults()
	if !strings.Contains(out, "Search: \"needle\"") {
		t.Fatal("search header missing")
	}
	if !strings.Contains(out, "showing 1-3 of 4") {
		t.Fatal("scroll indicator missing for overflowing list")
	}

	m.SearchResults = nil
	out = m.viewSearchResults()
	if !strings.Contains(out, "No memories found") {
		t.Fatal("empty result state missing")
	}
}

func TestViewSetupBranches(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenSetup

	m.SetupInstalling = true
	m.SetupInstallingName = "opencode"
	out := m.viewSetup()
	if !strings.Contains(out, "Installing opencode plugin") {
		t.Fatal("installing state should render progress line")
	}

	m.SetupInstalling = false
	m.SetupDone = true
	m.SetupResult = &setup.Result{Agent: "opencode", Destination: "/tmp/plugins", Files: 2}
	out = m.viewSetup()
	if !strings.Contains(out, "Installed opencode plugin") {
		t.Fatal("success state should render install result")
	}
	if !strings.Contains(out, "Next Steps") {
		t.Fatal("success state should render post-install instructions")
	}

	m.SetupResult = nil
	m.SetupError = "permission denied"
	out = m.viewSetup()
	if !strings.Contains(out, "permission denied") {
		t.Fatal("should show error message")
	}
}

func TestViewMoveObservation(t *testing.T) {
	t.Run("shows loading when observation is nil", func(t *testing.T) {
		m := New(nil, "")
		m.Screen = ScreenMoveObservation
		m.MoveObservationID = 42

		out := m.viewMoveObservation()
		if !strings.Contains(out, "Loading") {
			t.Fatal("should show loading when observation is nil")
		}
	})

	t.Run("displays current values and scope selector", func(t *testing.T) {
		project := "engram"
		m := New(nil, "")
		m.Screen = ScreenMoveObservation
		m.MoveObservationID = 42
		m.SelectedObservation = &store.Observation{
			ID:         42,
			Workspace:  &project,
			Visibility: "project",
		}
		m.MoveScopeCursor = 0
		m.MoveProjectList = []string{"syfra", "engram", "ancora"}
		m.MoveProjectCursor = 1
		m.MoveActiveColumn = "project"

		out := m.viewMoveObservation()
		if !strings.Contains(out, "Move Observation #42") {
			t.Fatal("should show header")
		}
		if !strings.Contains(out, "Current Values") {
			t.Fatal("should show current values section")
		}
		if !strings.Contains(out, "engram") {
			t.Fatal("should show current project")
		}
		if !strings.Contains(out, "Change to") {
			t.Fatal("should show change to section")
		}
		if !strings.Contains(out, "project") || !strings.Contains(out, "personal") {
			t.Fatal("should show scope options")
		}
		if !strings.Contains(out, "syfra") || !strings.Contains(out, "ancora") {
			t.Fatal("should show project options")
		}
	})

	t.Run("shows first scope option selected", func(t *testing.T) {
		project := "engram"
		m := New(nil, "")
		m.Screen = ScreenMoveObservation
		m.MoveObservationID = 42
		m.SelectedObservation = &store.Observation{ID: 42, Workspace: &project, Visibility: "project"}
		m.MoveScopeCursor = 0
		m.MoveProjectList = []string{"syfra", "engram"}
		m.MoveActiveColumn = "scope"

		out := m.viewMoveObservation()
		if !strings.Contains(out, "project") {
			t.Fatal("should show project option")
		}
	})

	t.Run("shows second scope option selected", func(t *testing.T) {
		project := "engram"
		m := New(nil, "")
		m.Screen = ScreenMoveObservation
		m.MoveObservationID = 42
		m.SelectedObservation = &store.Observation{ID: 42, Workspace: &project, Visibility: "project"}
		m.MoveScopeCursor = 1
		m.MoveProjectList = []string{"syfra", "engram"}
		m.MoveActiveColumn = "scope"

		out := m.viewMoveObservation()
		if !strings.Contains(out, "personal") {
			t.Fatal("should show personal option")
		}
	})

	t.Run("shows error message", func(t *testing.T) {
		project := "engram"
		m := New(nil, "")
		m.Screen = ScreenMoveObservation
		m.MoveObservationID = 42
		m.SelectedObservation = &store.Observation{ID: 42, Workspace: &project, Visibility: "project"}
		m.MoveError = "scope change failed"
		m.MoveProjectList = []string{"syfra", "engram"}
		m.MoveActiveColumn = "project"

		out := m.viewMoveObservation()
		if !strings.Contains(out, "scope change failed") {
			t.Fatal("should show error message")
		}
	})

	t.Run("shows success message when done", func(t *testing.T) {
		project := "engram"
		m := New(nil, "")
		m.Screen = ScreenMoveObservation
		m.MoveObservationID = 42
		m.SelectedObservation = &store.Observation{ID: 42, Workspace: &project, Visibility: "project"}
		m.MoveDone = true
		m.MoveProjectList = []string{"syfra", "engram"}
		m.MoveActiveColumn = "project"

		out := m.viewMoveObservation()
		if !strings.Contains(out, "successfully") {
			t.Fatal("should show success message")
		}
	})
}

func TestViewDashboardSearchAndRecent(t *testing.T) {
	// Test with fully installed Ancora
	m := New(nil, "")
	m.IsFullyInstalled = true
	m.Cursor = 1
	m.Stats = &store.Stats{
		TotalSessions:     3,
		TotalObservations: 7,
		TotalPrompts:      2,
		Projects:          []string{"a", "b", "c", "d", "e", "f"},
	}

	out := m.viewDashboard()
	lowerOut := strings.ToLower(out)
	if !strings.Contains(lowerOut, "ancora") {
		t.Fatalf("dashboard should include header, got: %s", out[:200])
	}
	// Check for GLIM-IA style elements
	if !strings.Contains(out, "Status:") {
		t.Fatal("dashboard should show status indicator")
	}
	if !strings.Contains(out, "Search memories") {
		t.Fatal("dashboard should show menu items when installed")
	}
	if !strings.Contains(out, "Find observations") {
		t.Fatal("dashboard should show menu descriptions when installed")
	}

	// Test with not installed Ancora
	mNotInstalled := New(nil, "")
	mNotInstalled.IsFullyInstalled = false
	mNotInstalled.Cursor = 0

	outNotInstalled := mNotInstalled.viewDashboard()
	if !strings.Contains(outNotInstalled, "Setup environment") {
		t.Fatal("dashboard should show setup option when not installed")
	}
	if strings.Contains(outNotInstalled, "Search memories") {
		t.Fatal("dashboard should NOT show search option when not installed")
	}
	if strings.Contains(outNotInstalled, "Recent observations") {
		t.Fatal("dashboard should NOT show recent observations option when not installed")
	}
	if strings.Contains(outNotInstalled, "Browse sessions") {
		t.Fatal("dashboard should NOT show browse sessions option when not installed")
	}
	if !strings.Contains(outNotInstalled, "Exit") {
		t.Fatal("dashboard should show exit option when not installed")
	}

	m.Stats = nil
	out = m.viewDashboard()
	if !strings.Contains(out, "ready") {
		t.Fatal("dashboard should show status even when stats are nil")
	}

	m.Screen = ScreenSearch
	out = m.viewSearch()
	if !strings.Contains(out, "Search Memories") {
		t.Fatal("search view should render title")
	}

	m.Height = 14
	m.RecentObservations = []store.Observation{
		{ID: 1, Type: "bugfix", Title: "one", Content: "a", CreatedAt: "2026-01-01"},
		{ID: 2, Type: "bugfix", Title: "two", Content: "b", CreatedAt: "2026-01-01"},
		{ID: 3, Type: "bugfix", Title: "three", Content: "c", CreatedAt: "2026-01-01"},
		{ID: 4, Type: "bugfix", Title: "four", Content: "d", CreatedAt: "2026-01-01"},
	}
	out = m.viewRecent()
	if !strings.Contains(out, "Recent Observations") {
		t.Fatal("recent view should render title")
	}
	if !strings.Contains(out, "showing 1-3 of 4") {
		t.Fatal("recent view should render scroll indicator when needed")
	}

	m.RecentObservations = nil
	out = m.viewRecent()
	if !strings.Contains(out, "No observations yet") {
		t.Fatal("recent view should render empty state")
	}

	// Force minimum visible items branch
	m.Height = 8
	m.RecentObservations = []store.Observation{{ID: 1, Type: "bugfix", Title: "one", Content: "a", CreatedAt: "2026-01-01"}}
	out = m.viewRecent()
	if !strings.Contains(out, "Recent Observations") {
		t.Fatal("recent view should still render when height is very small")
	}
}

func TestViewObservationDetailTimelineSessionsAndSessionDetail(t *testing.T) {
	m := New(nil, "")
	m.Height = 22

	out := m.viewObservationDetail()
	if !strings.Contains(out, "Loading") {
		t.Fatal("detail view should render loading state when observation is nil")
	}

	tool := "bash"
	project := "engram"
	m.SelectedObservation = &store.Observation{
		ID:        42,
		Type:      "decision",
		Title:     "Architecture decision",
		SessionID: "session-1",
		CreatedAt: "2026-01-01",
		ToolName:  &tool,
		Workspace: &project,
		Content:   strings.Repeat("line\n", 20),
	}
	m.DetailScroll = 99
	out = m.viewObservationDetail()
	if !strings.Contains(out, "Observation #42") || !strings.Contains(out, "Content") {
		t.Fatal("detail view should render metadata and content section")
	}
	if !strings.Contains(out, "line") {
		t.Fatal("detail view should render content lines")
	}

	out = m.viewTimeline()
	if !strings.Contains(out, "Loading") {
		t.Fatal("timeline should render loading state when nil")
	}

	m.Timeline = &store.TimelineResult{
		Focus:        store.Observation{ID: 42, Type: "decision", Title: "focus", Content: "focus content"},
		Before:       []store.TimelineEntry{{ID: 40, Type: "bugfix", Title: "before title"}},
		After:        []store.TimelineEntry{{ID: 43, Type: "pattern", Title: "after title"}},
		SessionInfo:  &store.Session{ID: "session-1", Project: "engram"},
		TotalInRange: 3,
	}
	out = m.viewTimeline()
	if !strings.Contains(out, "Timeline") || !strings.Contains(out, "Before") || !strings.Contains(out, "After") {
		t.Fatal("timeline should render focus and before/after sections")
	}

	m.Sessions = nil
	out = m.viewSessions()
	if !strings.Contains(out, "No sessions yet") {
		t.Fatal("sessions view should render empty state")
	}

	summary := "session summary"
	m.Height = 14
	m.Sessions = []store.SessionSummary{
		{ID: "s1", Workspace: "engram", StartedAt: "2026-01-01", Summary: &summary, ObservationCount: 2},
		{ID: "s2", Workspace: "engram", StartedAt: "2026-01-02", ObservationCount: 1},
		{ID: "s3", Workspace: "engram", StartedAt: "2026-01-03", ObservationCount: 1},
		{ID: "s4", Workspace: "engram", StartedAt: "2026-01-04", ObservationCount: 1},
		{ID: "s5", Workspace: "engram", StartedAt: "2026-01-05", ObservationCount: 1},
		{ID: "s6", Workspace: "engram", StartedAt: "2026-01-06", ObservationCount: 1},
		{ID: "s7", Workspace: "engram", StartedAt: "2026-01-07", ObservationCount: 1},
	}
	out = m.viewSessions()
	if !strings.Contains(out, "Sessions") || !strings.Contains(out, "showing 1-5 of 7") {
		t.Fatalf("sessions view should render list and scroll indicator, got: %s", out)
	}

	// Force minimum visible items branch
	m.Height = 2
	out = m.viewSessions()
	if !strings.Contains(out, "Sessions") {
		t.Fatal("sessions view should render when height is very small")
	}

	m.SelectedSessionIdx = 99
	out = m.viewSessionDetail()
	if !strings.Contains(out, "Session not found") {
		t.Fatal("session detail should guard invalid index")
	}

	m.SelectedSessionIdx = 0
	m.SessionObservations = nil
	out = m.viewSessionDetail()
	if !strings.Contains(out, "No observations in this session") {
		t.Fatal("session detail should render empty observations state")
	}

	m.Height = 16
	m.SessionObservations = []store.Observation{
		{ID: 1, Type: "bugfix", Title: "one", Content: "a", CreatedAt: "2026-01-01"},
		{ID: 2, Type: "bugfix", Title: "two", Content: "b", CreatedAt: "2026-01-01"},
		{ID: 3, Type: "bugfix", Title: "three", Content: "c", CreatedAt: "2026-01-01"},
		{ID: 4, Type: "bugfix", Title: "four", Content: "d", CreatedAt: "2026-01-01"},
	}
	out = m.viewSessionDetail()
	if !strings.Contains(out, "Observations (4)") {
		t.Fatal("session detail should show observations heading")
	}
}

func TestViewRouterCoversAllScreens(t *testing.T) {
	m := New(nil, "")
	m.IsFullyInstalled = true
	m.Stats = &store.Stats{}
	m.SearchResults = []store.SearchResult{{Observation: store.Observation{ID: 1, Type: "bugfix", Title: "t", Content: "c", CreatedAt: "now"}}}
	m.SearchQuery = "q"
	m.RecentObservations = []store.Observation{{ID: 1, Type: "bugfix", Title: "t", Content: "c", CreatedAt: "now"}}
	m.SelectedObservation = &store.Observation{ID: 1, Type: "bugfix", Title: "t", Content: "c", CreatedAt: "now", SessionID: "s1"}
	m.Timeline = &store.TimelineResult{Focus: store.Observation{ID: 1, Type: "bugfix", Title: "t", Content: "c"}, TotalInRange: 1}
	m.Sessions = []store.SessionSummary{{ID: "test", Project: "engram", StartedAt: "now", ObservationCount: 1}}
	m.SelectedSessionIdx = 0
	m.SessionObservations = []store.Observation{{ID: 1, Type: "bugfix", Title: "t", Content: "c", CreatedAt: "now"}}
	m.SetupAgents = []setup.Agent{{Name: "opencode", Description: "OpenCode", InstallDir: "/tmp"}}
	m.Height = 20

	tests := []struct {
		screen Screen
		want   string
	}{
		{screen: ScreenDashboard, want: "memories"}, // Changed from "Actions" (removed)
		{screen: ScreenSearch, want: "Search Memories"},
		{screen: ScreenSearchResults, want: "Search:"},
		{screen: ScreenRecent, want: "Recent Observations"},
		{screen: ScreenObservationDetail, want: "Observation #"},
		{screen: ScreenTimeline, want: "Timeline"},
		{screen: ScreenSessions, want: "Sessions"},
		{screen: ScreenSessionDetail, want: "Session:"},
		{screen: ScreenSetup, want: "Setup"},
		{screen: ScreenMoveObservation, want: "Move Observation"},
	}

	for _, tt := range tests {
		m.Screen = tt.screen
		out := m.View()
		if !strings.Contains(out, tt.want) {
			t.Fatalf("screen %v output missing %q", tt.screen, tt.want)
		}
	}
}

func TestViewSetupRemainingBranches(t *testing.T) {
	m := New(nil, "")
	m.Screen = ScreenSetup
	m.SetupAgents = []setup.Agent{
		{Name: "claude-code", Description: "Claude Code", InstallDir: "/tmp/claude"},
		{Name: "opencode", Description: "OpenCode", InstallDir: "/tmp/opencode"},
	}

	out := m.viewSetup()
	if !strings.Contains(out, "Select an agent to set up") || !strings.Contains(out, "Install to") {
		t.Fatal("setup selection mode should render options and install paths")
	}

	m.SetupInstalling = true
	m.SetupInstallingName = "claude-code"
	out = m.viewSetup()
	if !strings.Contains(out, "Running claude plugin marketplace add + install") {
		t.Fatal("setup installing should render claude-code specific progress text")
	}

	m.SetupInstalling = false
	m.SetupDone = true
	m.SetupError = ""
	m.SetupResult = &setup.Result{Agent: "claude-code", Destination: "/tmp/claude", Files: 0}
	out = m.viewSetup()
	if !strings.Contains(out, "Verify with: claude plugin list") {
		t.Fatal("setup success for claude-code should render next steps")
	}

	m.SetupResult = nil
	m.SetupError = ""
	out = m.viewSetup()
	if !strings.Contains(out, "enter/esc back to dashboard") {
		t.Fatal("setup done without result/error should still render return help")
	}
}

func TestViewSetupAllowlistPrompt(t *testing.T) {
	t.Run("renders allowlist prompt", func(t *testing.T) {
		m := New(nil, "")
		m.Screen = ScreenSetup
		m.SetupAllowlistPrompt = true
		m.SetupResult = &setup.Result{Agent: "claude-code", Destination: "claude plugin system"}

		out := m.viewSetup()
		if !strings.Contains(out, "Installed claude-code plugin") {
			t.Fatal("prompt should show install success")
		}
		if !strings.Contains(out, "Permissions Allowlist") {
			t.Fatal("prompt should show allowlist heading")
		}
		if !strings.Contains(out, "settings.json") {
			t.Fatal("prompt should mention settings.json")
		}
		if !strings.Contains(out, "[y] Yes") || !strings.Contains(out, "[n] No") {
			t.Fatal("prompt should show y/n options")
		}
	})

	t.Run("renders applied state", func(t *testing.T) {
		m := New(nil, "")
		m.Screen = ScreenSetup
		m.SetupDone = true
		m.SetupResult = &setup.Result{Agent: "claude-code", Destination: "claude plugin system"}
		m.SetupAllowlistApplied = true

		out := m.viewSetup()
		if !strings.Contains(out, "tools added to allowlist") {
			t.Fatal("should show allowlist success")
		}
	})

	t.Run("renders error state", func(t *testing.T) {
		m := New(nil, "")
		m.Screen = ScreenSetup
		m.SetupDone = true
		m.SetupResult = &setup.Result{Agent: "claude-code", Destination: "claude plugin system"}
		m.SetupAllowlistError = "permission denied"

		out := m.viewSetup()
		if !strings.Contains(out, "Allowlist update failed") {
			t.Fatal("should show allowlist error")
		}
		if !strings.Contains(out, "permission denied") {
			t.Fatal("should show error message")
		}
	})
}
