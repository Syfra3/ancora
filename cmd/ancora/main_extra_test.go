package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Syfra3/ancora/internal/embed"
	"github.com/Syfra3/ancora/internal/mcp"
	searchpkg "github.com/Syfra3/ancora/internal/search"
	engramsrv "github.com/Syfra3/ancora/internal/server"
	"github.com/Syfra3/ancora/internal/setup"
	"github.com/Syfra3/ancora/internal/store"
	"github.com/Syfra3/ancora/internal/tui"
	versioncheck "github.com/Syfra3/ancora/internal/version"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

type exitCode int

func captureOutputAndRecover(t *testing.T, fn func()) (stdout string, stderr string, recovered any) {
	t.Helper()

	oldOut := os.Stdout
	oldErr := os.Stderr

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stderr pipe: %v", err)
	}

	os.Stdout = outW
	os.Stderr = errW

	func() {
		defer func() {
			recovered = recover()
		}()
		fn()
	}()

	_ = outW.Close()
	_ = errW.Close()
	os.Stdout = oldOut
	os.Stderr = oldErr

	outBytes, err := io.ReadAll(outR)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	errBytes, err := io.ReadAll(errR)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}

	return string(outBytes), string(errBytes), recovered
}

func stubExitWithPanic(t *testing.T) {
	t.Helper()
	old := exitFunc
	exitFunc = func(code int) { panic(exitCode(code)) }
	t.Cleanup(func() { exitFunc = old })
}

func stubRuntimeHooks(t *testing.T) {
	t.Helper()
	oldStoreNew := storeNew
	oldNewHTTPServer := newHTTPServer
	oldStartHTTP := startHTTP
	oldNewMCPServer := newMCPServer
	oldNewMCPServerWithTools := newMCPServerWithTools
	oldServeMCP := serveMCP
	oldNewTUIModel := newTUIModel
	oldNewTeaProgram := newTeaProgram
	oldRunTeaProgram := runTeaProgram
	oldSetupSupportedAgents := setupSupportedAgents
	oldSetupInstallAgent := setupInstallAgent
	oldSetupCheckEmbeddingsStatus := setupCheckEmbeddingsStatus
	oldScanInputLine := scanInputLine
	oldNewEmbedder := newEmbedder
	oldSearchMemories := searchMemories
	oldStoreAddObservation := storeAddObservation
	oldStoreTimeline := storeTimeline
	oldStoreFormatContext := storeFormatContext
	oldStoreStats := storeStats
	oldStoreExport := storeExport
	oldJSONMarshalIndent := jsonMarshalIndent
	oldCheckForUpdates := checkForUpdates

	storeNew = store.New
	newHTTPServer = func(s *store.Store, _ int) *engramsrv.Server { return engramsrv.New(s, 0) }
	startHTTP = func(_ *engramsrv.Server) error { return nil }
	newMCPServer = func(s *store.Store) *mcpserver.MCPServer {
		return mcpserver.NewMCPServer("test", "0", mcpserver.WithRecovery())
	}
	newMCPServerWithTools = func(s *store.Store, allowlist map[string]bool) *mcpserver.MCPServer {
		return mcpserver.NewMCPServer("test", "0", mcpserver.WithRecovery())
	}
	serveMCP = func(_ *mcpserver.MCPServer, _ ...mcpserver.StdioOption) error { return nil }
	newTUIModel = func(_ *store.Store) tui.Model { return tui.New(nil, "") }
	newTeaProgram = func(tea.Model, ...tea.ProgramOption) *tea.Program { return &tea.Program{} }
	runTeaProgram = func(*tea.Program) (tea.Model, error) { return nil, nil }
	setupSupportedAgents = setup.SupportedAgents
	setupInstallAgent = setup.Install
	setupCheckEmbeddingsStatus = setup.CheckEmbeddingsStatus
	scanInputLine = fmt.Scanln
	newEmbedder = func() (embed.Embedder, error) { return embed.New() }
	searchMemories = func(s *store.Store, query string, opts store.SearchOptions) ([]store.SearchResult, searchpkg.Mode, error) {
		results, err := s.Search(query, opts)
		return results, searchpkg.ModeKeyword, err
	}
	storeAddObservation = func(s *store.Store, p store.AddObservationParams) (int64, error) {
		return s.AddObservation(p)
	}
	storeTimeline = func(s *store.Store, observationID int64, before, after int) (*store.TimelineResult, error) {
		return s.Timeline(observationID, before, after)
	}
	storeFormatContext = func(s *store.Store, project, scope string) (string, error) {
		return s.FormatContext(project, scope)
	}
	storeStats = func(s *store.Store) (*store.Stats, error) { return s.Stats() }
	storeExport = func(s *store.Store) (*store.ExportData, error) { return s.Export() }
	jsonMarshalIndent = json.MarshalIndent
	checkForUpdates = func(string) versioncheck.CheckResult {
		return versioncheck.CheckResult{Status: versioncheck.StatusUpToDate}
	}

	t.Cleanup(func() {
		storeNew = oldStoreNew
		newHTTPServer = oldNewHTTPServer
		startHTTP = oldStartHTTP
		newMCPServer = oldNewMCPServer
		newMCPServerWithTools = oldNewMCPServerWithTools
		serveMCP = oldServeMCP
		newTUIModel = oldNewTUIModel
		newTeaProgram = oldNewTeaProgram
		runTeaProgram = oldRunTeaProgram
		setupSupportedAgents = oldSetupSupportedAgents
		setupInstallAgent = oldSetupInstallAgent
		setupCheckEmbeddingsStatus = oldSetupCheckEmbeddingsStatus
		scanInputLine = oldScanInputLine
		newEmbedder = oldNewEmbedder
		searchMemories = oldSearchMemories
		storeAddObservation = oldStoreAddObservation
		storeTimeline = oldStoreTimeline
		storeFormatContext = oldStoreFormatContext
		storeStats = oldStoreStats
		storeExport = oldStoreExport
		jsonMarshalIndent = oldJSONMarshalIndent
		checkForUpdates = oldCheckForUpdates
	})
}

func TestFatal(t *testing.T) {
	stubExitWithPanic(t)
	_, stderr, recovered := captureOutputAndRecover(t, func() {
		fatal(errors.New("boom"))
	})

	code, ok := recovered.(exitCode)
	if !ok || int(code) != 1 {
		t.Fatalf("expected exit code 1 panic, got %v", recovered)
	}
	if !strings.Contains(stderr, "ancora: boom") {
		t.Fatalf("fatal stderr mismatch: %q", stderr)
	}
}

func TestCmdServeParsesPortAndErrors(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)

	tests := []struct {
		name      string
		envPort   string
		argPort   string
		wantPort  int
		startErr  error
		wantFatal bool
	}{
		{name: "default port", wantPort: 7437},
		{name: "env port", envPort: "8123", wantPort: 8123},
		{name: "arg overrides env", envPort: "8123", argPort: "9001", wantPort: 9001},
		{name: "invalid env keeps default", envPort: "nope", wantPort: 7437},
		{name: "invalid arg keeps env", envPort: "8123", argPort: "bad", wantPort: 8123},
		{name: "start failure", wantPort: 7437, startErr: errors.New("listen failed"), wantFatal: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stubExitWithPanic(t)
			if tc.envPort != "" {
				t.Setenv("ANCORA_PORT", tc.envPort)
			} else {
				t.Setenv("ANCORA_PORT", "")
			}

			args := []string{"ancora", "serve"}
			if tc.argPort != "" {
				args = append(args, tc.argPort)
			}
			withArgs(t, args...)

			seenPort := -1
			newHTTPServer = func(s *store.Store, port int) *engramsrv.Server {
				seenPort = port
				return engramsrv.New(s, 0)
			}
			startHTTP = func(_ *engramsrv.Server) error {
				return tc.startErr
			}

			_, stderr, recovered := captureOutputAndRecover(t, func() {
				cmdServe(cfg)
			})

			if seenPort != tc.wantPort {
				t.Fatalf("port=%d want=%d", seenPort, tc.wantPort)
			}
			if tc.wantFatal {
				if _, ok := recovered.(exitCode); !ok {
					t.Fatalf("expected fatal exit, got %v", recovered)
				}
				if !strings.Contains(stderr, "listen failed") {
					t.Fatalf("stderr missing start error: %q", stderr)
				}
			} else if recovered != nil {
				t.Fatalf("expected no panic, got %v", recovered)
			}
		})
	}
}

func TestCmdMCPAndTUIBranches(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	serveMCP = func(_ *mcpserver.MCPServer, _ ...mcpserver.StdioOption) error { return errors.New("mcp failed") }
	_, mcpErr, recovered := captureOutputAndRecover(t, func() { cmdMCP(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(mcpErr, "mcp failed") {
		t.Fatalf("expected mcp fatal, got panic=%v stderr=%q", recovered, mcpErr)
	}

	serveMCP = func(_ *mcpserver.MCPServer, _ ...mcpserver.StdioOption) error { return nil }
	_, _, recovered = captureOutputAndRecover(t, func() { cmdMCP(cfg) })
	if recovered != nil {
		t.Fatalf("unexpected panic on successful mcp: %v", recovered)
	}

	runTeaProgram = func(*tea.Program) (tea.Model, error) { return nil, errors.New("tui failed") }
	_, tuiErr, recovered := captureOutputAndRecover(t, func() { cmdTUI(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(tuiErr, "tui failed") {
		t.Fatalf("expected tui fatal, got panic=%v stderr=%q", recovered, tuiErr)
	}

	runTeaProgram = func(*tea.Program) (tea.Model, error) { return nil, nil }
	_, _, recovered = captureOutputAndRecover(t, func() { cmdTUI(cfg) })
	if recovered != nil {
		t.Fatalf("unexpected panic on successful tui: %v", recovered)
	}
}

func TestCmdSetupDirectAndInteractive(t *testing.T) {
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	setupInstallAgent = func(agent string) (*setup.Result, error) {
		if agent == "broken" {
			return nil, errors.New("install failed")
		}
		return &setup.Result{Agent: agent, Destination: "/tmp/dest", Files: 2}, nil
	}

	withArgs(t, "ancora", "setup", "codex")
	cfg := testConfig(t)
	out, errOut, recovered := captureOutputAndRecover(t, func() { cmdSetup(cfg) })
	if recovered != nil || errOut != "" {
		t.Fatalf("direct setup should succeed, panic=%v stderr=%q", recovered, errOut)
	}
	if !strings.Contains(out, "Installed codex plugin") {
		t.Fatalf("unexpected direct setup output: %q", out)
	}

	withArgs(t, "ancora", "setup", "broken")
	_, errOut, recovered = captureOutputAndRecover(t, func() { cmdSetup(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(errOut, "install failed") {
		t.Fatalf("expected direct setup fatal, panic=%v stderr=%q", recovered, errOut)
	}

	setupSupportedAgents = func() []setup.Agent {
		return []setup.Agent{{Name: "opencode", Description: "OpenCode", InstallDir: "/tmp/opencode"}}
	}
	scanInputLine = func(a ...any) (int, error) {
		p := a[0].(*string)
		*p = "1"
		return 1, nil
	}

	withArgs(t, "ancora", "setup")
	out, errOut, recovered = captureOutputAndRecover(t, func() { cmdSetup(cfg) })
	if recovered != nil || errOut != "" {
		t.Fatalf("interactive setup should succeed, panic=%v stderr=%q", recovered, errOut)
	}
	if !strings.Contains(out, "Installing opencode plugin") {
		t.Fatalf("unexpected interactive setup output: %q", out)
	}

	scanInputLine = func(a ...any) (int, error) {
		p := a[0].(*string)
		*p = "99"
		return 1, nil
	}
	withArgs(t, "ancora", "setup")
	_, errOut, recovered = captureOutputAndRecover(t, func() { cmdSetup(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(errOut, "Invalid choice") {
		t.Fatalf("expected invalid choice exit, panic=%v stderr=%q", recovered, errOut)
	}
}

func TestCmdExportDefaultAndCmdImportErrors(t *testing.T) {
	workDir := t.TempDir()
	withCwd(t, workDir)

	cfg := testConfig(t)
	stubExitWithPanic(t)

	mustSeedObservation(t, cfg, "s-exp-default", "proj", "note", "title", "content", "project")

	withArgs(t, "ancora", "export")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdExport(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("export default should succeed, panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "Exported to ancora-export.json") {
		t.Fatalf("unexpected default export output: %q", stdout)
	}
	if _, err := os.Stat(filepath.Join(workDir, "ancora-export.json")); err != nil {
		t.Fatalf("expected default export file: %v", err)
	}

	badPath := filepath.Join(workDir, "missing", "out.json")
	withArgs(t, "ancora", "export", badPath)
	_, stderr, recovered = captureOutputAndRecover(t, func() { cmdExport(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(stderr, "no such file or directory") {
		t.Fatalf("expected export write fatal, panic=%v stderr=%q", recovered, stderr)
	}

	withArgs(t, "ancora", "import")
	_, stderr, recovered = captureOutputAndRecover(t, func() { cmdImport(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(stderr, "usage: ancora import") {
		t.Fatalf("expected import usage exit, panic=%v stderr=%q", recovered, stderr)
	}

	withArgs(t, "ancora", "import", filepath.Join(workDir, "nope.json"))
	_, stderr, recovered = captureOutputAndRecover(t, func() { cmdImport(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(stderr, "read") {
		t.Fatalf("expected import read fatal, panic=%v stderr=%q", recovered, stderr)
	}

	invalidJSON := filepath.Join(workDir, "invalid.json")
	if err := os.WriteFile(invalidJSON, []byte("{invalid"), 0644); err != nil {
		t.Fatalf("write invalid json: %v", err)
	}
	withArgs(t, "ancora", "import", invalidJSON)
	_, stderr, recovered = captureOutputAndRecover(t, func() { cmdImport(cfg) })
	if _, ok := recovered.(exitCode); !ok || !strings.Contains(stderr, "parse") {
		t.Fatalf("expected import parse fatal, panic=%v stderr=%q", recovered, stderr)
	}
}

func TestMainDispatchServeMCPAndTUI(t *testing.T) {
	stubRuntimeHooks(t)

	t.Setenv("ANCORA_DATA_DIR", t.TempDir())
	withArgs(t, "ancora", "serve", "8088")
	_, stderr, recovered := captureOutputAndRecover(t, func() { main() })
	if recovered != nil || stderr != "" {
		t.Fatalf("serve dispatch failed: panic=%v stderr=%q", recovered, stderr)
	}

	withArgs(t, "ancora", "mcp")
	_, stderr, recovered = captureOutputAndRecover(t, func() { main() })
	if recovered != nil || stderr != "" {
		t.Fatalf("mcp dispatch failed: panic=%v stderr=%q", recovered, stderr)
	}

	withArgs(t, "engram", "tui")
	_, stderr, recovered = captureOutputAndRecover(t, func() { main() })
	if recovered != nil || stderr != "" {
		t.Fatalf("tui dispatch failed: panic=%v stderr=%q", recovered, stderr)
	}
}

func TestStoreInitFailurePaths(t *testing.T) {
	stubRuntimeHooks(t)
	stubExitWithPanic(t)
	cfg := testConfig(t)
	importFile := filepath.Join(t.TempDir(), "import.json")
	if err := os.WriteFile(importFile, []byte(`{"version":"0.1.0","exported_at":"2026-01-01T00:00:00Z","sessions":[],"observations":[],"prompts":[]}`), 0644); err != nil {
		t.Fatalf("write import file: %v", err)
	}

	storeNew = func(store.Config) (*store.Store, error) {
		return nil, errors.New("store init failed")
	}

	cmds := []func(store.Config){
		cmdServe,
		cmdMCP,
		cmdTUI,
		cmdSearch,
		cmdSave,
		cmdTimeline,
		cmdContext,
		cmdStats,
		cmdExport,
		cmdImport,
	}

	argsByCmd := [][]string{
		{"ancora", "serve"},
		{"ancora", "mcp"},
		{"ancora", "tui"},
		{"ancora", "search", "q"},
		{"ancora", "save", "t", "c"},
		{"ancora", "timeline", "1"},
		{"ancora", "context"},
		{"ancora", "stats"},
		{"ancora", "export"},
		{"ancora", "import", importFile},
	}

	for i, fn := range cmds {
		withArgs(t, argsByCmd[i]...)
		_, stderr, recovered := captureOutputAndRecover(t, func() { fn(cfg) })
		if _, ok := recovered.(exitCode); !ok {
			t.Fatalf("command %d: expected exit panic, got %v", i, recovered)
		}
		if !strings.Contains(stderr, "store init failed") {
			t.Fatalf("command %d: expected store failure stderr, got %q", i, stderr)
		}
	}
}

func TestUsageAndValidationExits(t *testing.T) {
	cfg := testConfig(t)
	stubExitWithPanic(t)

	tests := []struct {
		name       string
		args       []string
		run        func(store.Config)
		errSubstr  string
		stderrOnly bool
	}{
		{name: "search usage", args: []string{"ancora", "search"}, run: cmdSearch, errSubstr: "usage: ancora search"},
		{name: "search missing query", args: []string{"ancora", "search", "--limit", "3"}, run: cmdSearch, errSubstr: "search query is required"},
		{name: "save usage", args: []string{"ancora", "save", "title"}, run: cmdSave, errSubstr: "usage: ancora save"},
		{name: "timeline usage", args: []string{"ancora", "timeline"}, run: cmdTimeline, errSubstr: "usage: ancora timeline"},
		{name: "timeline invalid id", args: []string{"ancora", "timeline", "abc"}, run: cmdTimeline, errSubstr: "invalid observation id"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withArgs(t, tc.args...)
			_, stderr, recovered := captureOutputAndRecover(t, func() { tc.run(cfg) })
			if _, ok := recovered.(exitCode); !ok {
				t.Fatalf("expected exit panic, got %v", recovered)
			}
			if !strings.Contains(stderr, tc.errSubstr) {
				t.Fatalf("stderr missing %q: %q", tc.errSubstr, stderr)
			}
		})
	}
}

func TestMainDispatchRemainingCommands(t *testing.T) {
	stubRuntimeHooks(t)
	stubExitWithPanic(t)
	withCwd(t, t.TempDir())

	dataDir := t.TempDir()
	t.Setenv("ANCORA_DATA_DIR", dataDir)

	seedCfg, scErr := store.DefaultConfig()
	if scErr != nil {
		t.Fatalf("DefaultConfig: %v", scErr)
	}
	seedCfg.DataDir = dataDir
	focusID := mustSeedObservation(t, seedCfg, "s-main", "main-proj", "note", "focus", "focus content", "project")

	importFile := filepath.Join(t.TempDir(), "import.json")
	if err := os.WriteFile(importFile, []byte(`{"version":"0.1.0","exported_at":"2026-01-01T00:00:00Z","sessions":[],"observations":[],"prompts":[]}`), 0644); err != nil {
		t.Fatalf("write import file: %v", err)
	}

	setupInstallAgent = func(agent string) (*setup.Result, error) {
		return &setup.Result{Agent: agent, Destination: "/tmp/dest", Files: 1}, nil
	}

	tests := []struct {
		name string
		args []string
	}{
		{name: "search", args: []string{"ancora", "search", "focus"}},
		{name: "save", args: []string{"ancora", "save", "t", "c"}},
		{name: "timeline", args: []string{"ancora", "timeline", fmt.Sprintf("%d", focusID)}},
		{name: "context", args: []string{"ancora", "context", "main-proj"}},
		{name: "stats", args: []string{"ancora", "stats"}},
		{name: "export", args: []string{"ancora", "export", filepath.Join(t.TempDir(), "exp.json")}},
		{name: "import", args: []string{"ancora", "import", importFile}},
		{name: "setup", args: []string{"ancora", "setup", "codex"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withArgs(t, tc.args...)
			_, stderr, recovered := captureOutputAndRecover(t, func() { main() })
			if recovered != nil {
				t.Fatalf("main panic for %s: %v stderr=%q", tc.name, recovered, stderr)
			}
		})
	}
}

func TestCmdImportStoreImportFailure(t *testing.T) {
	stubExitWithPanic(t)
	cfg := testConfig(t)

	badImport := filepath.Join(t.TempDir(), "bad-import.json")
	badJSON := `{
		"version":"0.1.0",
		"exported_at":"2026-01-01T00:00:00Z",
		"sessions":[],
		"observations":[{"id":1,"session_id":"missing-session","type":"note","title":"x","content":"y","scope":"project","revision_count":1,"duplicate_count":1,"created_at":"2026-01-01 00:00:00","updated_at":"2026-01-01 00:00:00"}],
		"prompts":[]
	}`
	if err := os.WriteFile(badImport, []byte(badJSON), 0644); err != nil {
		t.Fatalf("write bad import: %v", err)
	}

	withArgs(t, "ancora", "import", badImport)
	_, stderr, recovered := captureOutputAndRecover(t, func() { cmdImport(cfg) })
	if _, ok := recovered.(exitCode); !ok {
		t.Fatalf("expected fatal exit, got %v", recovered)
	}
	if !strings.Contains(stderr, "import observation") {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
}

func TestCmdSearchAndSaveDanglingFlags(t *testing.T) {
	cfg := testConfig(t)

	withArgs(t, "ancora", "save", "dangling-title", "dangling-content", "--type")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdSave(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("save with dangling flag failed, panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "Memory saved:") {
		t.Fatalf("unexpected save output: %q", stdout)
	}

	withArgs(t, "ancora", "search", "dangling-content", "--limit", "not-a-number", "--workspace")
	stdout, stderr, recovered = captureOutputAndRecover(t, func() { cmdSearch(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("search with dangling flags failed, panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "Found") {
		t.Fatalf("unexpected search output: %q", stdout)
	}
}

func TestCmdSetupHyphenArgFallsBackToInteractive(t *testing.T) {
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	setupSupportedAgents = func() []setup.Agent {
		return []setup.Agent{{Name: "codex", Description: "Codex", InstallDir: "/tmp/codex"}}
	}
	setupInstallAgent = func(agent string) (*setup.Result, error) {
		return &setup.Result{Agent: agent, Destination: "/tmp/codex", Files: 1}, nil
	}
	scanInputLine = func(a ...any) (int, error) {
		p := a[0].(*string)
		*p = "1"
		return 1, nil
	}

	withArgs(t, "ancora", "setup", "--not-an-agent")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdSetup(testConfig(t)) })
	if recovered != nil || stderr != "" {
		t.Fatalf("setup interactive fallback failed: panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "Which agent do you want to set up?") || !strings.Contains(stdout, "Installing codex plugin") {
		t.Fatalf("unexpected setup output: %q", stdout)
	}
}

func TestCmdTimelineNoBeforeAfterSections(t *testing.T) {
	cfg := testConfig(t)
	focusID := mustSeedObservation(t, cfg, "solo-session", "solo", "note", "focus", "only content", "project")

	withArgs(t, "ancora", "timeline", fmt.Sprintf("%d", focusID), "--before", "0", "--after", "0")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdTimeline(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("timeline failed: panic=%v stderr=%q", recovered, stderr)
	}
	if strings.Contains(stdout, "─── Before ───") || strings.Contains(stdout, "─── After ───") {
		t.Fatalf("unexpected before/after sections in output: %q", stdout)
	}
}

func TestCmdStatsNoProjectsYet(t *testing.T) {
	cfg := testConfig(t)
	withArgs(t, "ancora", "stats")
	stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdStats(cfg) })
	if recovered != nil || stderr != "" {
		t.Fatalf("stats failed: panic=%v stderr=%q", recovered, stderr)
	}
	if !strings.Contains(stdout, "Projects:     none yet") {
		t.Fatalf("expected empty projects output, got: %q", stdout)
	}
}

func TestCommandErrorSeamsAndUncoveredBranches(t *testing.T) {
	stubRuntimeHooks(t)
	stubExitWithPanic(t)
	cfg := testConfig(t)

	assertFatal := func(t *testing.T, stderr string, recovered any, want string) {
		t.Helper()
		if _, ok := recovered.(exitCode); !ok {
			t.Fatalf("expected fatal exit, got %v", recovered)
		}
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr missing %q: %q", want, stderr)
		}
	}

	t.Run("search seam error", func(t *testing.T) {
		withArgs(t, "ancora", "search", "needle")
		searchMemories = func(*store.Store, string, store.SearchOptions) ([]store.SearchResult, searchpkg.Mode, error) {
			return nil, searchpkg.ModeKeyword, errors.New("forced search error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdSearch(cfg) })
		assertFatal(t, stderr, recovered, "forced search error")
	})

	t.Run("save seam error", func(t *testing.T) {
		withArgs(t, "ancora", "save", "title", "content")
		storeAddObservation = func(*store.Store, store.AddObservationParams) (int64, error) {
			return 0, errors.New("forced save error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdSave(cfg) })
		assertFatal(t, stderr, recovered, "forced save error")
	})

	t.Run("timeline seam error", func(t *testing.T) {
		withArgs(t, "ancora", "timeline", "1")
		storeTimeline = func(*store.Store, int64, int, int) (*store.TimelineResult, error) {
			return nil, errors.New("forced timeline error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdTimeline(cfg) })
		assertFatal(t, stderr, recovered, "forced timeline error")
	})

	t.Run("timeline prints session summary", func(t *testing.T) {
		summary := "this session has a non-empty summary"
		withArgs(t, "ancora", "timeline", "1")
		storeTimeline = func(*store.Store, int64, int, int) (*store.TimelineResult, error) {
			return &store.TimelineResult{
				Focus:        store.Observation{ID: 1, Type: "note", Title: "focus", Content: "content", CreatedAt: "2026-01-01"},
				SessionInfo:  &store.Session{ID: "test", Project: "proj", StartedAt: "2026-01-01", Summary: &summary},
				TotalInRange: 1,
			}, nil
		}
		stdout, stderr, recovered := captureOutputAndRecover(t, func() { cmdTimeline(cfg) })
		if recovered != nil || stderr != "" {
			t.Fatalf("expected successful timeline render, panic=%v stderr=%q", recovered, stderr)
		}
		if !strings.Contains(stdout, "Session: proj") || !strings.Contains(stdout, "non-empty summary") {
			t.Fatalf("expected summary in timeline output, got: %q", stdout)
		}
	})

	t.Run("context seam error", func(t *testing.T) {
		withArgs(t, "ancora", "context")
		storeFormatContext = func(*store.Store, string, string) (string, error) {
			return "", errors.New("forced context error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdContext(cfg) })
		assertFatal(t, stderr, recovered, "forced context error")
	})

	t.Run("stats seam error", func(t *testing.T) {
		withArgs(t, "ancora", "stats")
		storeStats = func(*store.Store) (*store.Stats, error) {
			return nil, errors.New("forced stats error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdStats(cfg) })
		assertFatal(t, stderr, recovered, "forced stats error")
	})

	t.Run("export seam error", func(t *testing.T) {
		withArgs(t, "ancora", "export")
		storeExport = func(*store.Store) (*store.ExportData, error) {
			return nil, errors.New("forced export error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdExport(cfg) })
		assertFatal(t, stderr, recovered, "forced export error")
	})

	t.Run("export marshal seam error", func(t *testing.T) {
		withArgs(t, "ancora", "export")
		storeExport = func(s *store.Store) (*store.ExportData, error) { return s.Export() }
		jsonMarshalIndent = func(any, string, string) ([]byte, error) {
			return nil, errors.New("forced marshal error")
		}
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdExport(cfg) })
		assertFatal(t, stderr, recovered, "forced marshal error")
	})

	t.Run("setup interactive install error", func(t *testing.T) {
		setupSupportedAgents = func() []setup.Agent {
			return []setup.Agent{{Name: "codex", Description: "Codex", InstallDir: "/tmp/codex"}}
		}
		scanInputLine = func(a ...any) (int, error) {
			p := a[0].(*string)
			*p = "1"
			return 1, nil
		}
		setupInstallAgent = func(string) (*setup.Result, error) {
			return nil, errors.New("forced setup error")
		}

		withArgs(t, "ancora", "setup")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdSetup(testConfig(t)) })
		assertFatal(t, stderr, recovered, "forced setup error")
	})
}

func TestCmdMCP(t *testing.T) {
	cfg := testConfig(t)
	stubRuntimeHooks(t)
	stubExitWithPanic(t)

	assertFatal := func(t *testing.T, stderr string, recovered any, want string) {
		t.Helper()
		code, ok := recovered.(exitCode)
		if !ok || int(code) != 1 {
			t.Fatalf("expected exit code 1 panic, got %v", recovered)
		}
		if !strings.Contains(stderr, want) {
			t.Fatalf("expected stderr to contain %q, got %q", want, stderr)
		}
	}

	t.Run("no tools filter uses newMCPServerWithConfig with nil allowlist", func(t *testing.T) {
		called := false
		newMCPServerWithConfig = func(s *store.Store, mcpCfg mcp.MCPConfig, allowlist map[string]bool) *mcpserver.MCPServer {
			called = true
			if allowlist != nil {
				t.Errorf("expected nil allowlist for no tools filter, got %v", allowlist)
			}
			return mcpserver.NewMCPServer("test", "0")
		}
		withArgs(t, "ancora", "mcp")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdMCP(cfg) })
		if recovered != nil || stderr != "" {
			t.Fatalf("expected clean run, got panic=%v stderr=%q", recovered, stderr)
		}
		if !called {
			t.Fatal("expected newMCPServerWithConfig to be called")
		}
	})

	t.Run("--tools flag uses newMCPServerWithConfig with non-nil allowlist", func(t *testing.T) {
		var gotAllowlist map[string]bool
		newMCPServerWithConfig = func(s *store.Store, mcpCfg mcp.MCPConfig, allowlist map[string]bool) *mcpserver.MCPServer {
			gotAllowlist = allowlist
			return mcpserver.NewMCPServer("test", "0")
		}
		withArgs(t, "ancora", "mcp", "--tools=agent")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdMCP(cfg) })
		if recovered != nil || stderr != "" {
			t.Fatalf("expected clean run, got panic=%v stderr=%q", recovered, stderr)
		}
		if gotAllowlist == nil {
			t.Fatal("expected newMCPServerWithConfig to be called with non-nil allowlist")
		}
	})

	t.Run("--tools as separate arg uses newMCPServerWithConfig with non-nil allowlist", func(t *testing.T) {
		var gotAllowlist map[string]bool
		newMCPServerWithConfig = func(s *store.Store, mcpCfg mcp.MCPConfig, allowlist map[string]bool) *mcpserver.MCPServer {
			gotAllowlist = allowlist
			return mcpserver.NewMCPServer("test", "0")
		}
		withArgs(t, "ancora", "mcp", "--tools", "agent")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdMCP(cfg) })
		if recovered != nil || stderr != "" {
			t.Fatalf("expected clean run, got panic=%v stderr=%q", recovered, stderr)
		}
		if gotAllowlist == nil {
			t.Fatal("expected newMCPServerWithConfig to be called with non-nil allowlist")
		}
	})

	t.Run("storeNew failure calls fatal", func(t *testing.T) {
		storeNew = func(cfg store.Config) (*store.Store, error) {
			return nil, errors.New("db open failed")
		}
		withArgs(t, "ancora", "mcp")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdMCP(cfg) })
		assertFatal(t, stderr, recovered, "db open failed")
	})

	t.Run("serveMCP failure calls fatal", func(t *testing.T) {
		storeNew = store.New
		serveMCP = func(_ *mcpserver.MCPServer, _ ...mcpserver.StdioOption) error {
			return errors.New("stdio failed")
		}
		withArgs(t, "ancora", "mcp")
		_, stderr, recovered := captureOutputAndRecover(t, func() { cmdMCP(cfg) })
		assertFatal(t, stderr, recovered, "stdio failed")
	})
}
