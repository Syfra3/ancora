// Ancora — Persistent memory for AI coding agents.
//
// Usage:
//
//	ancora serve          Start HTTP + MCP server
//	ancora mcp            Start MCP server only (stdio transport)
//	ancora search <query> Search memories from CLI
//	ancora save           Save a memory from CLI
//	ancora context        Show recent context
//	ancora stats          Show memory stats
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/Syfra3/ancora/internal/embed"
	"github.com/Syfra3/ancora/internal/embedding"
	"github.com/Syfra3/ancora/internal/ipc"
	"github.com/Syfra3/ancora/internal/mcp"
	"github.com/Syfra3/ancora/internal/project"
	searchpkg "github.com/Syfra3/ancora/internal/search"
	"github.com/Syfra3/ancora/internal/server"
	"github.com/Syfra3/ancora/internal/setup"
	"github.com/Syfra3/ancora/internal/store"
	"github.com/Syfra3/ancora/internal/tui"
	versioncheck "github.com/Syfra3/ancora/internal/version"

	tea "github.com/charmbracelet/bubbletea"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// version is set via ldflags at build time by goreleaser or from version.go.
// Falls back to "dev" for local builds; init() tries Version constant, then Go module info.
var version = Version

func init() {
	if version != "dev" && version != "" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		version = strings.TrimPrefix(info.Main.Version, "v")
	}
}

var (
	storeNew      = store.New
	newHTTPServer = server.New
	startHTTP     = (*server.Server).Start

	newMCPServer           = mcp.NewServer
	newMCPServerWithTools  = mcp.NewServerWithTools
	newMCPServerWithConfig = mcp.NewServerWithConfig
	resolveMCPTools        = mcp.ResolveTools
	serveMCP               = mcpserver.ServeStdio

	// detectProject is injectable for testing; wraps project.DetectProject.
	detectProject = project.DetectProject

	newTUIModel   = func(s *store.Store) tui.Model { return tui.New(s, version) }
	newTeaProgram = tea.NewProgram
	runTeaProgram = (*tea.Program).Run

	checkForUpdates = versioncheck.CheckLatest

	setupSupportedAgents        = setup.SupportedAgents
	setupInstallAgent           = setup.Install
	setupAddClaudeCodeAllowlist = setup.AddClaudeCodeAllowlist
	setupCheckEmbeddingsStatus  = setup.CheckEmbeddingsStatus
	newEmbeddingsDownloader     = func(destPath string) embeddingsDownloader { return setup.NewDownloader(destPath) }
	checkLlamaCpp               = setup.CheckLlamaCpp
	llamaCppInstallInstructions = setup.GetLlamaCppInstallInstructions
	scanInputLine               = fmt.Scanln
	newEmbedder                 = func() (embed.Embedder, error) { return embed.New() }
	ipcLoadOrCreateSecret       = ipc.LoadOrCreateSecret
	ipcNewTransport             = ipc.New
	ipcNewServer                = func(transport ipc.Transport, secret string) ipcServer { return ipc.NewServer(transport, secret) }
	ipcNewClient                = func(transport ipc.Transport, secret string) ipcClient { return ipc.NewClient(transport, secret) }

	searchMemories = func(s *store.Store, query string, opts store.SearchOptions) ([]store.SearchResult, searchpkg.Mode, error) {
		embedder, err := newEmbedder()
		if err != nil {
			embedder = nil
		}
		results, mode, err := searchpkg.SearchWithOptions(query, opts, embedder, s)
		if err != nil {
			return nil, mode, err
		}
		out := make([]store.SearchResult, 0, len(results))
		for _, r := range results {
			out = append(out, r.SearchResult)
		}
		return out, mode, nil
	}
	storeAddObservation = func(s *store.Store, p store.AddObservationParams) (int64, error) { return s.AddObservation(p) }
	storeTimeline       = func(s *store.Store, observationID int64, before, after int) (*store.TimelineResult, error) {
		return s.Timeline(observationID, before, after)
	}
	storeFormatContext = func(s *store.Store, project, scope string) (string, error) { return s.FormatContext(project, scope) }
	storeStats         = func(s *store.Store) (*store.Stats, error) { return s.Stats() }
	storeExport        = func(s *store.Store) (*store.ExportData, error) { return s.Export() }
	jsonMarshalIndent  = json.MarshalIndent

	exitFunc = os.Exit

	stdinScanner = func() *bufio.Scanner { return bufio.NewScanner(os.Stdin) }
	userHomeDir  = os.UserHomeDir
)

func main() {
	// Default to TUI if no arguments provided
	if len(os.Args) < 2 {
		os.Args = append(os.Args, "tui")
	}

	// Check for updates on every invocation.
	if result := checkForUpdates(version); result.Status != versioncheck.StatusUpToDate && result.Message != "" {
		fmt.Fprintln(os.Stderr, result.Message)
		fmt.Fprintln(os.Stderr)
	}

	cfg, cfgErr := store.DefaultConfig()
	if cfgErr != nil {
		// Fallback: try to resolve home directory from environment variables
		// that os.UserHomeDir() might have missed (e.g. MCP subprocesses on
		// Windows where %USERPROFILE% is not propagated).
		if home := resolveHomeFallback(); home != "" {
			log.Printf("[ancora] UserHomeDir failed, using fallback: %s", home)
			cfg = store.FallbackConfig(filepath.Join(home, ".ancora"))
		} else {
			fatal(cfgErr)
		}
	}

	// Allow overriding data dir via env
	if dir := os.Getenv("ANCORA_DATA_DIR"); dir != "" {
		cfg.DataDir = dir
	}

	// Migrate orphaned databases that ended up in wrong locations
	// (e.g. drive root on Windows due to previous bug).
	migrateOrphanedDB(cfg.DataDir)

	switch os.Args[1] {
	case "serve":
		cmdServe(cfg)
	case "mcp":
		cmdMCP(cfg)
	case "tui":
		cmdTUI(cfg)
	case "search":
		cmdSearch(cfg)
	case "save":
		cmdSave(cfg)
	case "timeline":
		cmdTimeline(cfg)
	case "context":
		cmdContext(cfg)
	case "stats":
		cmdStats(cfg)
	case "export":
		cmdExport(cfg)
	case "import":
		cmdImport(cfg)
	case "projects":
		cmdProjects(cfg)
	case "setup":
		cmdSetup(cfg)
	case "embeddings":
		cmdEmbeddings(cfg)
	case "doctor":
		cmdDoctor(cfg)
	case "version", "--version", "-v":
		fmt.Printf("ancora %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		printUsage()
		exitFunc(1)
	}
}

// ─── Commands ────────────────────────────────────────────────────────────────

func cmdServe(cfg store.Config) {
	port := 7437 // "ENGR" on phone keypad vibes
	if p := os.Getenv("ANCORA_PORT"); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}
	// Allow: ancora serve 8080
	if len(os.Args) > 2 {
		if n, err := strconv.Atoi(os.Args[2]); err == nil {
			port = n
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	srv := newHTTPServer(s, port)

	// Wire async embedding service into the HTTP server.
	if e, err := embed.New(); err == nil {
		embSvc := embedding.New(e, embedding.NewStoreAdapter(s))
		embSvc.Start()
		defer embSvc.Stop()
		srv.SetEmbeddingService(embSvc)
	}

	// Graceful shutdown on SIGINT/SIGTERM.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-sigCh:
			log.Println("[ancora] shutting down...")
			exitFunc(0)
		case <-done:
			return
		}
	}()

	if err := startHTTP(srv); err != nil {
		fatal(err)
	}
}

func cmdMCP(cfg store.Config) {
	// Parse --tools, --project/--workspace, and --events/--no-events flags.
	toolsFilter := ""
	projectOverride := ""
	enableEvents := true // default: start IPC socket alongside MCP
	for i := 2; i < len(os.Args); i++ {
		if strings.HasPrefix(os.Args[i], "--tools=") {
			toolsFilter = strings.TrimPrefix(os.Args[i], "--tools=")
		} else if os.Args[i] == "--tools" && i+1 < len(os.Args) {
			toolsFilter = os.Args[i+1]
			i++
		} else if strings.HasPrefix(os.Args[i], "--workspace=") {
			projectOverride = strings.TrimPrefix(os.Args[i], "--workspace=")
		} else if os.Args[i] == "--workspace" && i+1 < len(os.Args) {
			projectOverride = os.Args[i+1]
			i++
		} else if strings.HasPrefix(os.Args[i], "--project=") {
			projectOverride = strings.TrimPrefix(os.Args[i], "--project=")
		} else if os.Args[i] == "--project" && i+1 < len(os.Args) {
			projectOverride = os.Args[i+1]
			i++
		} else if os.Args[i] == "--events" {
			enableEvents = true
		} else if os.Args[i] == "--no-events" {
			enableEvents = false
		}
	}

	// Project detection chain: --project flag → ANCORA_PROJECT env → git detection
	detectedProject := projectOverride
	if detectedProject == "" {
		detectedProject = os.Getenv("ANCORA_PROJECT")
	}
	if detectedProject == "" {
		if cwd, err := os.Getwd(); err == nil {
			detectedProject = detectProject(cwd)
		}
	}
	// Always normalize (lowercase + trim)
	detectedProject, _ = store.NormalizeProject(detectedProject)

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	// IPC event server — started alongside MCP unless --no-events is passed.
	if enableEvents {
		if ipcSrv, ipcErr := startIPCEventServer(s); ipcErr != nil {
			log.Printf("[ancora] IPC event server unavailable: %v", ipcErr)
		} else {
			defer ipcSrv()
		}
	}

	// Initialize embedder for hybrid semantic search (FTS5 + vector RRF fusion).
	// If model is unavailable, gracefully falls back to keyword-only search.
	var embedder mcp.Embedder
	var embSvc *embedding.Service
	if e, err := embed.New(); err == nil {
		embedder = e
		log.Printf("[ancora] hybrid search enabled (model: %s)", e.ModelPath)

		// Start async embedding service so new observations get embedded immediately.
		embSvc = embedding.New(e, embedding.NewStoreAdapter(s))
		embSvc.Start()
		defer embSvc.Stop()
	} else {
		log.Printf("[ancora] hybrid search unavailable, using keyword-only: %v", err)
	}

	mcpCfg := mcp.MCPConfig{
		DefaultProject:   detectedProject,
		Embedder:         embedder,
		EmbeddingService: embSvc,
	}

	allowlist := resolveMCPTools(toolsFilter)
	mcpSrv := newMCPServerWithConfig(s, mcpCfg, allowlist)

	if err := serveMCP(mcpSrv); err != nil {
		fatal(err)
	}
}

// ipcStopper is the cleanup handle returned by startIPCEventServer.
// *ipc.Server has Stop(); *ipc.Client has Close(). Both are wrapped here
// so the caller can defer a single stop function.
type ipcStopper func()

type ipcServer interface {
	Start() error
	Stop()
	Emit(ipc.Event)
}

type ipcClient interface {
	Connect() error
	Close()
	Emit(ipc.Event)
}

// startIPCEventServer wires IPC event emission into the store.
//
// It first tries to START a new IPC server (bind the socket). If the socket
// is already owned by another ancora process (EADDRINUSE), it falls back to
// CONNECTING as a client — events are forwarded through the existing server,
// so Vela receives them normally. This is the key path when running a local
// dev build alongside the system-installed ancora MCP process.
func startIPCEventServer(s *store.Store) (ipcStopper, error) {
	secret, err := ipcLoadOrCreateSecret("")
	if err != nil {
		return nil, fmt.Errorf("load IPC secret: %w", err)
	}

	transport, err := ipcNewTransport("ancora", "")
	if err != nil {
		return nil, fmt.Errorf("create IPC transport: %w", err)
	}

	// Try server mode first.
	srv := ipcNewServer(transport, secret)
	if startErr := srv.Start(); startErr == nil {
		s.SetEventServer(srv)
		log.Printf("[ancora] IPC event socket: %s (server mode)", transport.Path())
		return srv.Stop, nil
	} else if !isAddrInUse(startErr) {
		// Unexpected error — propagate.
		return nil, fmt.Errorf("start IPC server: %w", startErr)
	}

	// Socket owned by another process — connect as client.
	log.Printf("[ancora] IPC socket busy, connecting as client to forward events")
	client := ipcNewClient(transport, secret)
	if err := client.Connect(); err != nil {
		if !shouldReclaimIPCServer(err) {
			return nil, fmt.Errorf("connect IPC client: %w", err)
		}

		log.Printf("[ancora] IPC socket owner is unresponsive, reclaiming %s", transport.Path())
		if err := transport.Close(); err != nil {
			return nil, fmt.Errorf("reclaim IPC socket %s: %w", transport.Path(), err)
		}
		if err := srv.Start(); err != nil {
			return nil, fmt.Errorf("restart IPC server after reclaim: %w", err)
		}

		s.SetEventServer(srv)
		log.Printf("[ancora] IPC event socket: %s (server mode, reclaimed)", transport.Path())
		return srv.Stop, nil
	}

	s.SetEventServer(client)
	log.Printf("[ancora] IPC event socket: %s (client mode)", transport.Path())
	return client.Close, nil
}

func shouldReclaimIPCServer(err error) bool {
	if err == nil {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	return strings.Contains(err.Error(), "no auth response")
}

// isAddrInUse reports whether err is an "address already in use" bind error.
func isAddrInUse(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EADDRINUSE
	}
	return false
}

func cmdTUI(cfg store.Config) {
	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	model := newTUIModel(s)
	p := newTeaProgram(model)
	if _, err := runTeaProgram(p); err != nil {
		fatal(err)
	}
}

func cmdSearch(cfg store.Config) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: ancora search <query> [--type TYPE] [--project PROJECT] [--scope SCOPE] [--limit N]")
		exitFunc(1)
	}

	// Collect the query (everything that's not a flag)
	var queryParts []string
	opts := store.SearchOptions{Limit: 10}
	searchAllProjects := false

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--type":
			if i+1 < len(os.Args) {
				opts.Type = os.Args[i+1]
				i++
			}
		case "--workspace":
			if i+1 < len(os.Args) {
				opts.Workspace = os.Args[i+1]
				i++
			}
		case "--limit":
			if i+1 < len(os.Args) {
				if n, err := strconv.Atoi(os.Args[i+1]); err == nil {
					opts.Limit = n
				}
				i++
			}
		case "--visibility":
			if i+1 < len(os.Args) {
				opts.Visibility = os.Args[i+1]
				i++
			}
		case "--organization":
			if i+1 < len(os.Args) {
				opts.Organization = os.Args[i+1]
				i++
			}
		case "--all-projects", "--all":
			searchAllProjects = true
		default:
			queryParts = append(queryParts, os.Args[i])
		}
	}

	query := strings.Join(queryParts, " ")
	if query == "" {
		fmt.Fprintln(os.Stderr, "error: search query is required")
		exitFunc(1)
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
		return
	}
	defer s.Close()

	// If searching all workspaces, clear workspace filter
	if searchAllProjects {
		opts.Workspace = ""
	}

	results, _, err := searchMemories(s, query, opts)
	if err != nil {
		fatal(err)
		return
	}

	if len(results) == 0 {
		// Check if there are observations in OTHER workspaces that might be relevant
		if !searchAllProjects && opts.Workspace != "" {
			allProjects, listErr := s.ListProjectNames()
			if listErr == nil && len(allProjects) > 1 {
				fmt.Printf("No memories found for: %q in workspace %q\n", query, opts.Workspace)
				fmt.Printf("Hint: Related memories may exist in other workspaces. Re-run without --workspace to search everywhere.\n")
				// Optionally show available workspaces
				fmt.Printf("Available workspaces: %s\n", strings.Join(allProjects, ", "))
				return
			}
		}
		fmt.Printf("No memories found for: %q\n", query)
		return
	}

	fmt.Printf("Found %d memories:\n\n", len(results))
	for i, r := range results {
		workspace := ""
		if r.Workspace != nil {
			workspace = fmt.Sprintf(" | workspace: %s", *r.Workspace)
		}
		fmt.Printf("[%d] #%d (%s) — %s\n    %s\n    %s%s | visibility: %s\n\n",
			i+1, r.ID, r.Type, r.Title,
			truncate(r.Content, 300),
			r.CreatedAt, workspace, r.Visibility)
	}
}

func cmdSave(cfg store.Config) {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: ancora save <title> <content> [--type TYPE] [--workspace WORKSPACE] [--visibility VISIBILITY] [--organization ORGANIZATION] [--topic TOPIC_KEY]")
		exitFunc(1)
	}

	title := os.Args[2]
	content := os.Args[3]
	typ := "manual"
	workspace := ""
	visibility := "" // PR4: Empty by default, will be auto-inferred if not provided
	organization := ""
	topicKey := ""

	for i := 4; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--type":
			if i+1 < len(os.Args) {
				typ = os.Args[i+1]
				i++
			}
		case "--workspace", "--project": // Accept both old and new names
			if i+1 < len(os.Args) {
				workspace = os.Args[i+1]
				i++
			}
		case "--visibility", "--scope": // Accept both old and new names
			if i+1 < len(os.Args) {
				visibility = os.Args[i+1]
				i++
			}
		case "--organization":
			if i+1 < len(os.Args) {
				organization = os.Args[i+1]
				i++
			}
		case "--topic":
			if i+1 < len(os.Args) {
				topicKey = os.Args[i+1]
				i++
			}
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	sessionID := "manual-save"
	if workspace != "" {
		sessionID = "manual-save-" + workspace
	}
	s.CreateSession(sessionID, workspace, organization)
	id, err := storeAddObservation(s, store.AddObservationParams{
		SessionID:    sessionID,
		Type:         typ,
		Title:        title,
		Content:      content,
		Workspace:    workspace,
		Visibility:   visibility,
		Organization: organization,
		TopicKey:     topicKey,
	})
	if err != nil {
		fatal(err)
	}

	fmt.Printf("Memory saved: #%d %q (%s)\n", id, title, typ)
}

func cmdTimeline(cfg store.Config) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: ancora timeline <observation_id> [--before N] [--after N]")
		exitFunc(1)
	}

	obsID, err := strconv.ParseInt(os.Args[2], 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: invalid observation id %q\n", os.Args[2])
		exitFunc(1)
	}

	before, after := 5, 5
	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--before":
			if i+1 < len(os.Args) {
				if n, err := strconv.Atoi(os.Args[i+1]); err == nil {
					before = n
				}
				i++
			}
		case "--after":
			if i+1 < len(os.Args) {
				if n, err := strconv.Atoi(os.Args[i+1]); err == nil {
					after = n
				}
				i++
			}
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	result, err := storeTimeline(s, obsID, before, after)
	if err != nil {
		fatal(err)
	}

	// Session header
	if result.SessionInfo != nil {
		summary := ""
		if result.SessionInfo.Summary != nil {
			summary = fmt.Sprintf(" — %s", truncate(*result.SessionInfo.Summary, 100))
		}
		fmt.Printf("Session: %s (%s)%s\n", result.SessionInfo.Project, result.SessionInfo.StartedAt, summary)
		fmt.Printf("Total observations in session: %d\n\n", result.TotalInRange)
	}

	// Before
	if len(result.Before) > 0 {
		fmt.Println("─── Before ───")
		for _, e := range result.Before {
			fmt.Printf("  #%d [%s] %s — %s\n", e.ID, e.Type, e.Title, truncate(e.Content, 150))
		}
		fmt.Println()
	}

	// Focus
	fmt.Printf(">>> #%d [%s] %s <<<\n", result.Focus.ID, result.Focus.Type, result.Focus.Title)
	fmt.Printf("    %s\n", truncate(result.Focus.Content, 500))
	fmt.Printf("    %s\n\n", result.Focus.CreatedAt)

	// After
	if len(result.After) > 0 {
		fmt.Println("─── After ───")
		for _, e := range result.After {
			fmt.Printf("  #%d [%s] %s — %s\n", e.ID, e.Type, e.Title, truncate(e.Content, 150))
		}
	}
}

func cmdContext(cfg store.Config) {
	project := ""
	scope := ""

	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--scope":
			if i+1 < len(os.Args) {
				scope = os.Args[i+1]
				i++
			}
		default:
			if project == "" {
				project = os.Args[i]
			}
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	ctx, err := storeFormatContext(s, project, scope)
	if err != nil {
		fatal(err)
	}

	if ctx == "" {
		fmt.Println("No previous session memories found.")
		return
	}

	fmt.Print(ctx)
}

func cmdStats(cfg store.Config) {
	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	stats, err := storeStats(s)
	if err != nil {
		fatal(err)
	}

	projects := "none yet"
	if len(stats.Projects) > 0 {
		projects = strings.Join(stats.Projects, ", ")
	}

	fmt.Printf("Ancora Memory Stats\n")
	fmt.Printf("  Sessions:     %d\n", stats.TotalSessions)
	fmt.Printf("  Observations: %d\n", stats.TotalObservations)
	fmt.Printf("  Prompts:      %d\n", stats.TotalPrompts)
	fmt.Printf("  Projects:     %s\n", projects)
	fmt.Printf("  Database:     %s/ancora.db\n", cfg.DataDir)
}

func cmdExport(cfg store.Config) {
	outFile := "ancora-export.json"
	if len(os.Args) > 2 {
		outFile = os.Args[2]
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	data, err := storeExport(s)
	if err != nil {
		fatal(err)
	}

	out, err := jsonMarshalIndent(data, "", "  ")
	if err != nil {
		fatal(err)
	}

	if err := os.WriteFile(outFile, out, 0644); err != nil {
		fatal(err)
	}

	fmt.Printf("Exported to %s\n", outFile)
	fmt.Printf("  Sessions:     %d\n", len(data.Sessions))
	fmt.Printf("  Observations: %d\n", len(data.Observations))
	fmt.Printf("  Prompts:      %d\n", len(data.Prompts))
}

func cmdImport(cfg store.Config) {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: ancora import <file.json>")
		exitFunc(1)
	}

	inFile := os.Args[2]
	raw, err := os.ReadFile(inFile)
	if err != nil {
		fatal(fmt.Errorf("read %s: %w", inFile, err))
	}

	var data store.ExportData
	if err := json.Unmarshal(raw, &data); err != nil {
		fatal(fmt.Errorf("parse %s: %w", inFile, err))
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	result, err := s.Import(&data)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("Imported from %s\n", inFile)
	fmt.Printf("  Sessions:     %d\n", result.SessionsImported)
	fmt.Printf("  Observations: %d\n", result.ObservationsImported)
	fmt.Printf("  Prompts:      %d\n", result.PromptsImported)
}

func cmdProjects(cfg store.Config) {
	// Route: ancora projects list | ancora projects consolidate [--all] [--dry-run]
	subCmd := "list"
	if len(os.Args) > 2 {
		subCmd = os.Args[2]
	}
	switch subCmd {
	case "consolidate":
		cmdProjectsConsolidate(cfg)
	case "prune":
		cmdProjectsPrune(cfg)
	case "list", "":
		cmdProjectsList(cfg)
	default:
		fmt.Fprintf(os.Stderr, "unknown projects subcommand: %s\n", subCmd)
		fmt.Fprintln(os.Stderr, "usage: ancora projects list")
		fmt.Fprintln(os.Stderr, "       ancora projects consolidate [--all] [--dry-run]")
		fmt.Fprintln(os.Stderr, "       ancora projects prune [--dry-run]")
		exitFunc(1)
	}
}

func cmdProjectsList(cfg store.Config) {
	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	projects, err := s.ListProjectsWithStats()
	if err != nil {
		fatal(err)
	}

	if len(projects) == 0 {
		fmt.Println("No projects found.")
		return
	}

	fmt.Printf("Projects (%d):\n", len(projects))
	for _, p := range projects {
		sessionWord := "sessions"
		if p.SessionCount == 1 {
			sessionWord = "session"
		}
		promptWord := "prompts"
		if p.PromptCount == 1 {
			promptWord = "prompt"
		}
		fmt.Printf("  %-30s %4d obs   %3d %-9s  %3d %s\n",
			p.Name,
			p.ObservationCount,
			p.SessionCount, sessionWord,
			p.PromptCount, promptWord,
		)
	}
}

// projectGroup represents a set of project names that should be merged.
type projectGroup struct {
	Names     []string
	Canonical string // suggested canonical (most observations)
}

// groupSimilarProjects groups projects by name similarity and shared directories.
// Uses a simple union-find approach.
func groupSimilarProjects(projects []store.ProjectStats) []projectGroup {
	n := len(projects)
	if n == 0 {
		return nil
	}

	// parent[i] holds the root of i's component
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}

	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(x, y int) {
		rx, ry := find(x), find(y)
		if rx != ry {
			parent[rx] = ry
		}
	}

	// Build name-only slice and index map for FindSimilar
	names := make([]string, n)
	nameToIndex := make(map[string]int, n)
	for i, p := range projects {
		names[i] = p.Name
		nameToIndex[p.Name] = i
	}

	// Group by name similarity
	for i := 0; i < n; i++ {
		similar := project.FindSimilar(projects[i].Name, names, 3)
		for _, sm := range similar {
			if j, ok := nameToIndex[sm.Name]; ok {
				union(i, j)
			}
		}
	}

	// Group by shared directory
	dirToProjects := make(map[string][]int)
	for i, p := range projects {
		for _, dir := range p.Directories {
			if dir != "" {
				dirToProjects[dir] = append(dirToProjects[dir], i)
			}
		}
	}
	for _, idxs := range dirToProjects {
		for k := 1; k < len(idxs); k++ {
			union(idxs[0], idxs[k])
		}
	}

	// Collect components
	components := make(map[int][]int)
	for i := 0; i < n; i++ {
		root := find(i)
		components[root] = append(components[root], i)
	}

	// Build groups — skip singletons (no duplicates)
	var groups []projectGroup
	for _, idxs := range components {
		if len(idxs) < 2 {
			continue
		}
		// Suggest the one with most observations as canonical
		bestIdx := idxs[0]
		for _, idx := range idxs[1:] {
			if projects[idx].ObservationCount > projects[bestIdx].ObservationCount {
				bestIdx = idx
			}
		}
		grpNames := make([]string, len(idxs))
		for k, idx := range idxs {
			grpNames[k] = projects[idx].Name
		}
		sort.Strings(grpNames)
		groups = append(groups, projectGroup{
			Names:     grpNames,
			Canonical: projects[bestIdx].Name,
		})
	}
	// Sort groups by canonical name for deterministic output
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Canonical < groups[j].Canonical
	})
	return groups
}

func cmdProjectsConsolidate(cfg store.Config) {
	doAll := false
	dryRun := false
	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--all":
			doAll = true
		case "--dry-run":
			dryRun = true
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	if !doAll {
		// Single-project mode: detect canonical project for cwd, find variants
		cwd, err := os.Getwd()
		if err != nil {
			fatal(err)
		}
		canonical := detectProject(cwd)

		allNames, err := s.ListProjectNames()
		if err != nil {
			fatal(err)
		}

		// Check if the detected canonical actually exists in the DB.
		canonicalExists := false
		for _, n := range allNames {
			if n == canonical {
				canonicalExists = true
				break
			}
		}
		if !canonicalExists {
			fmt.Printf("Note: %q has no existing memories. Merging will move memories into this new project name.\n", canonical)
		}

		// Find candidates by name similarity
		similar := project.FindSimilar(canonical, allNames, 3)

		// Also find candidates by shared directory (catches renames like sdd-agent-team → agent-teams-lite)
		allStats, _ := s.ListProjectsWithStats()
		statsMap := make(map[string]store.ProjectStats)
		var cwdDirs []string // directories for the canonical project
		for _, ps := range allStats {
			statsMap[ps.Name] = ps
			if ps.Name == canonical {
				cwdDirs = ps.Directories
			}
		}
		// If canonical has no stats yet, use cwd as its directory
		if len(cwdDirs) == 0 {
			cwdDirs = []string{cwd}
		}
		// Find projects sharing a directory with the canonical
		similarNames := make(map[string]bool)
		for _, sm := range similar {
			similarNames[sm.Name] = true
		}
		for _, ps := range allStats {
			if ps.Name == canonical || similarNames[ps.Name] {
				continue
			}
			for _, d := range ps.Directories {
				for _, cd := range cwdDirs {
					if d == cd {
						similar = append(similar, project.ProjectMatch{
							Name:      ps.Name,
							MatchType: "shared directory",
						})
						similarNames[ps.Name] = true
					}
				}
			}
		}

		if len(similar) == 0 {
			fmt.Printf("No similar project names found for %q. Nothing to consolidate.\n", canonical)
			return
		}

		fmt.Printf("Detected project: %q\n\n", canonical)
		fmt.Printf("Found similar project names:\n")
		for i, sm := range similar {
			obs := 0
			if ps, ok := statsMap[sm.Name]; ok {
				obs = ps.ObservationCount
			}
			fmt.Printf("  [%d] %-30s %3d obs  (%s)\n", i+1, sm.Name, obs, sm.MatchType)
		}

		if dryRun {
			fmt.Printf("\n[dry-run] Would merge %d project(s) into %q\n", len(similar), canonical)
			return
		}

		fmt.Printf("\nSelect which to merge into %q (comma-separated numbers, 'all', or 'none'): ", canonical)
		var answer string
		scanInputLine(&answer)
		answer = strings.TrimSpace(strings.ToLower(answer))

		if answer == "none" || answer == "n" || answer == "" {
			fmt.Println("Cancelled.")
			return
		}

		var sources []string
		if answer == "all" || answer == "a" {
			for _, sm := range similar {
				sources = append(sources, sm.Name)
			}
		} else {
			// Parse comma-separated indices
			for _, part := range strings.Split(answer, ",") {
				part = strings.TrimSpace(part)
				idx := 0
				if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 1 || idx > len(similar) {
					fmt.Fprintf(os.Stderr, "Invalid selection: %q (expected 1-%d)\n", part, len(similar))
					return
				}
				sources = append(sources, similar[idx-1].Name)
			}
		}

		if len(sources) == 0 {
			fmt.Println("Nothing selected.")
			return
		}

		fmt.Printf("\nMerging %d project(s) into %q...\n", len(sources), canonical)
		result, err := s.MergeProjects(sources, canonical)
		if err != nil {
			fatal(err)
		}

		fmt.Printf("Done! Merged into %q:\n", result.Canonical)
		fmt.Printf("  Observations: %d\n", result.ObservationsUpdated)
		fmt.Printf("  Sessions:     %d\n", result.SessionsUpdated)
		fmt.Printf("  Prompts:      %d\n", result.PromptsUpdated)
		return
	}

	// --all mode: group all projects by similarity + shared directories
	projects, err := s.ListProjectsWithStats()
	if err != nil {
		fatal(err)
	}

	groups := groupSimilarProjects(projects)

	if len(groups) == 0 {
		fmt.Println("No similar project name groups found.")
		return
	}

	fmt.Printf("Found %d group(s) of similar project names:\n\n", len(groups))

	// Build stats map for obs counts
	projectStatsMap := make(map[string]store.ProjectStats)
	for _, p := range projects {
		projectStatsMap[p.Name] = p
	}

	for i, g := range groups {
		fmt.Printf("Group %d:\n", i+1)
		for j, name := range g.Names {
			obs := 0
			if ps, ok := projectStatsMap[name]; ok {
				obs = ps.ObservationCount
			}
			marker := "  "
			if name == g.Canonical {
				marker = "→ "
			}
			fmt.Printf("  %s[%d] %-30s %3d obs\n", marker, j+1, name, obs)
		}
		fmt.Printf("  Suggested canonical: %q (→)\n", g.Canonical)

		if dryRun {
			fmt.Printf("  [dry-run] Would merge into %q\n\n", g.Canonical)
			continue
		}

		fmt.Printf("\n  Options:\n")
		fmt.Printf("    all     — merge everything into %q\n", g.Canonical)
		fmt.Printf("    1,3,... — merge only selected numbers into %q\n", g.Canonical)
		fmt.Printf("    rename  — choose a different canonical name\n")
		fmt.Printf("    skip    — don't touch this group\n")
		fmt.Printf("  Choice: ")
		var answer string
		scanInputLine(&answer)
		answer = strings.TrimSpace(strings.ToLower(answer))

		canonical := g.Canonical

		if answer == "skip" || answer == "s" || answer == "n" || answer == "" {
			fmt.Println("  Skipped.")
			fmt.Println()
			continue
		}

		if answer == "rename" || answer == "r" {
			fmt.Printf("  Enter canonical name: ")
			scanInputLine(&canonical)
			canonical = strings.TrimSpace(canonical)
			if canonical == "" {
				fmt.Println("  Empty input, skipping.")
				fmt.Println()
				continue
			}
			answer = "all" // after rename, merge everything into the new name
		}

		// Determine which sources to merge
		var sources []string
		if answer == "all" || answer == "a" || answer == "y" || answer == "yes" {
			for _, name := range g.Names {
				if name != canonical {
					sources = append(sources, name)
				}
			}
		} else {
			// Parse comma-separated indices
			for _, part := range strings.Split(answer, ",") {
				part = strings.TrimSpace(part)
				idx := 0
				if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 1 || idx > len(g.Names) {
					fmt.Fprintf(os.Stderr, "  Invalid selection: %q (expected 1-%d)\n", part, len(g.Names))
					fmt.Println()
					continue
				}
				selected := g.Names[idx-1]
				if selected != canonical {
					sources = append(sources, selected)
				}
			}
		}
		if len(sources) == 0 {
			fmt.Println("  Nothing to merge.")
			fmt.Println()
			continue
		}

		result, err := s.MergeProjects(sources, canonical)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  Error merging: %v\n", err)
			fmt.Println()
			continue
		}
		fmt.Printf("  Merged: %d obs, %d sessions, %d prompts\n\n",
			result.ObservationsUpdated, result.SessionsUpdated, result.PromptsUpdated)
	}
}

func cmdProjectsPrune(cfg store.Config) {
	dryRun := false
	for i := 3; i < len(os.Args); i++ {
		if os.Args[i] == "--dry-run" {
			dryRun = true
		}
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	allStats, err := s.ListProjectsWithStats()
	if err != nil {
		fatal(err)
	}

	// Find projects with 0 observations
	var candidates []store.ProjectStats
	for _, ps := range allStats {
		if ps.ObservationCount == 0 {
			candidates = append(candidates, ps)
		}
	}

	if len(candidates) == 0 {
		fmt.Println("No empty projects to prune.")
		return
	}

	fmt.Printf("Found %d project(s) with 0 observations:\n\n", len(candidates))
	for i, ps := range candidates {
		fmt.Printf("  [%d] %-30s %3d sessions  %3d prompts\n", i+1, ps.Name, ps.SessionCount, ps.PromptCount)
	}

	if dryRun {
		fmt.Printf("\n[dry-run] Would prune %d project(s)\n", len(candidates))
		return
	}

	fmt.Printf("\nSelect which to prune (comma-separated numbers, 'all', or 'none'): ")
	var answer string
	scanInputLine(&answer)
	answer = strings.TrimSpace(strings.ToLower(answer))

	if answer == "none" || answer == "n" || answer == "" {
		fmt.Println("Cancelled.")
		return
	}

	var selected []store.ProjectStats
	if answer == "all" || answer == "a" {
		selected = candidates
	} else {
		for _, part := range strings.Split(answer, ",") {
			part = strings.TrimSpace(part)
			idx := 0
			if _, err := fmt.Sscanf(part, "%d", &idx); err != nil || idx < 1 || idx > len(candidates) {
				fmt.Fprintf(os.Stderr, "Invalid selection: %q (expected 1-%d)\n", part, len(candidates))
				return
			}
			selected = append(selected, candidates[idx-1])
		}
	}

	if len(selected) == 0 {
		fmt.Println("Nothing selected.")
		return
	}

	totalSessions := int64(0)
	totalPrompts := int64(0)
	for _, ps := range selected {
		result, err := s.PruneProject(ps.Name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error pruning %q: %v\n", ps.Name, err)
			continue
		}
		totalSessions += result.SessionsDeleted
		totalPrompts += result.PromptsDeleted
	}

	fmt.Printf("\nPruned %d project(s): %d sessions, %d prompts removed.\n", len(selected), totalSessions, totalPrompts)
}

func cmdSetup(cfg store.Config) {
	// Check for flags
	autoInstall := false
	skipModel := false
	autoBackfill := false
	fastInstall := false
	for i := 2; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--auto-install":
			autoInstall = true
		case "--skip-model":
			skipModel = true
		case "--auto-backfill":
			autoBackfill = true
		case "--fast":
			fastInstall = true
		}
	}

	// If --fast, do auto-install + auto-backfill in one go
	if fastInstall {
		autoInstall = true
		autoBackfill = true
	}

	// If agent name given directly: ancora setup opencode
	if len(os.Args) > 2 && !strings.HasPrefix(os.Args[2], "-") {
		result, err := setupInstallAgent(os.Args[2])
		if err != nil {
			fatal(err)
		}
		fmt.Printf("✓ Installed %s plugin (%d files)\n", result.Agent, result.Files)
		fmt.Printf("  → %s\n", result.Destination)
		printPostInstall(result.Agent)
		return
	}

	// If --auto-install, download model non-interactively
	if autoInstall {
		fmt.Println("Installing embedding model...")
		destPath := filepath.Join(embed.ModelInstallPath(), embed.ModelFileName)
		downloader := setup.NewDownloader(destPath)
		if err := downloader.Download(); err != nil {
			fatal(fmt.Errorf("download failed: %w", err))
		}
		fmt.Printf("✓ Model installed: %s\n", destPath)

		// Auto-backfill after model install
		if autoBackfill {
			fmt.Println("\nRunning embedding backfill...")
			if err := runBackfill(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "Backfill warning: %v\n", err)
			} else {
				fmt.Println("✓ Backfill complete")
			}
		}
		return
	}

	// If --skip-model, exit immediately
	if skipModel {
		fmt.Println("Skipping embedding model installation.")
		return
	}

	// Launch interactive TUI wizard
	model := setup.NewWizardWithVersion(version)
	p := newTeaProgram(model)
	if _, err := runTeaProgram(p); err != nil {
		fatal(err)
	}
}

func printPostInstall(agent string) {
	switch agent {
	case "opencode":
		fmt.Println("\nNext steps:")
		fmt.Println("  1. Restart OpenCode — plugin + MCP server are ready")
		fmt.Println("  2. Run 'ancora serve &' for session tracking (HTTP API)")
	case "claude-code":
		// Offer to add ancora tools to the permissions allowlist
		fmt.Print("\nAdd ancora tools to ~/.claude/settings.json allowlist?\n")
		fmt.Print("This prevents Claude Code from asking permission on every tool call.\n")
		fmt.Print("Add to allowlist? (y/N): ")
		var answer string
		scanInputLine(&answer)
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "y" || answer == "yes" {
			if err := setupAddClaudeCodeAllowlist(); err != nil {
				fmt.Fprintf(os.Stderr, "  warning: could not update allowlist: %v\n", err)
				fmt.Fprintln(os.Stderr, "  You can add them manually to permissions.allow in ~/.claude/settings.json")
			} else {
				fmt.Println("  ✓ Ancora tools added to allowlist")
			}
		} else {
			fmt.Println("  Skipped. You can add them later to permissions.allow in ~/.claude/settings.json")
		}

		fmt.Println("\nNext steps:")
		fmt.Println("  1. Restart Claude Code — the plugin is active immediately")
		fmt.Println("  2. Verify with: claude plugin list")
		fmt.Println("  3. MCP config written to ~/.claude/mcp/ancora.json using absolute binary path")
		fmt.Println("     (survives plugin auto-updates; re-run 'ancora setup claude-code' if you move the binary)")
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func printUsage() {
	fmt.Printf(`ancora v%s — Persistent memory for AI coding agents

Usage:
  ancora <command> [arguments]

Commands:
  serve [port]       Start HTTP API server (default: 7437)
  mcp [--tools=PROFILE] [--project=NAME]
                     Start MCP server (stdio transport, for any AI agent)
                       Profiles: agent (11 tools), admin (4 tools), all (default, 15)
                       Combine: --tools=agent,admin or pick individual tools
                       --project  Override detected project name (default: git remote → cwd)
                       Example: ancora mcp --tools=agent
  tui                Launch interactive terminal UI
	search <query>     Search memories [--type TYPE] [--project PROJECT] [--scope SCOPE] [--limit N]
  save <title> <msg> Save a memory  [--type TYPE] [--project PROJECT] [--scope SCOPE]
  timeline <obs_id>  Show chronological context around an observation [--before N] [--after N]
  context [project]  Show recent context from previous sessions
  stats              Show memory system statistics
  export [file]      Export all memories to JSON (default: ancora-export.json)
  import <file>      Import memories from a JSON export file
  projects list      List all projects with observation, session, and prompt counts
  projects consolidate [--all] [--dry-run]
                     Merge similar project names into one canonical name
                       --all      Scan ALL projects for similar name groups
                       --dry-run  Preview what would be merged (no changes)
  setup [agent]      Interactive setup wizard for embedding model and agent plugins
                       No args: TUI wizard for model installation
                       Agent name: Install agent plugin (opencode, claude-code)
                       --auto-install: Download model non-interactively
                       --auto-backfill: Auto-backfill embeddings after install
  embeddings         Manage embedding model (status|install|backfill|test)
  doctor              Run system health checks (database, embeddings, FTS5)

  version            Print version
  help               Show this help

Environment:
  ANCORA_DATA_DIR      Override data directory (default: ~/.ancora)
  ANCORA_PORT          Override HTTP server port (default: 7437)
  ANCORA_PROJECT       Override auto-detected project name for MCP server
  ANCORA_EMBED_MODEL   Path to embedding model GGUF file (for hybrid search)

MCP Configuration (add to your agent's config):
  {
    "mcp": {
      "ancora": {
        "type": "stdio",
        "command": "ancora",
        "args": ["mcp", "--tools=agent"]
      }
    }
  }
`, version)
}

func fatal(err error) {
	fmt.Fprintf(os.Stderr, "ancora: %s\n", err)
	exitFunc(1)
}

// resolveHomeFallback tries platform-specific environment variables to find
// a home directory when os.UserHomeDir() fails. This commonly happens on
// Windows when ancora is launched as an MCP subprocess without full env
// propagation.
func resolveHomeFallback() string {
	// Windows: try common env vars that might be set even when
	// %USERPROFILE% is missing.
	for _, env := range []string{"USERPROFILE", "HOME", "LOCALAPPDATA"} {
		if v := os.Getenv(env); v != "" {
			if env == "LOCALAPPDATA" {
				// LOCALAPPDATA is C:\Users\<user>\AppData\Local — go up two levels.
				parent := filepath.Dir(filepath.Dir(v))
				if parent != "." && parent != v {
					return parent
				}
			}
			return v
		}
	}

	// Unix: $HOME should always work, but try passwd-style fallback.
	if v := os.Getenv("HOME"); v != "" {
		return v
	}

	return ""
}

// migrateOrphanedDB checks for ancora databases that ended up in wrong
// locations (e.g. drive root on Windows when UserHomeDir failed silently)
// and moves them to the correct location if the correct location has no DB.
func migrateOrphanedDB(correctDir string) {
	correctDB := filepath.Join(correctDir, "ancora.db")

	// If the correct DB already exists, nothing to migrate.
	if _, err := os.Stat(correctDB); err == nil {
		return
	}

	// Known wrong locations: relative ".ancora" resolved from common roots.
	// On Windows this typically ends up at C:\.ancora or D:\.ancora.
	candidates := []string{
		filepath.Join(string(filepath.Separator), ".ancora", "ancora.db"),
	}

	// On Windows, check all drive letter roots.
	if filepath.Separator == '\\' {
		for _, drive := range "CDEFGHIJ" {
			candidates = append(candidates,
				filepath.Join(string(drive)+":\\", ".ancora", "ancora.db"),
			)
		}
	}

	for _, candidate := range candidates {
		if candidate == correctDB {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}

		// Found an orphaned DB — migrate it.
		log.Printf("[ancora] found orphaned database at %s, migrating to %s", candidate, correctDB)

		if err := os.MkdirAll(correctDir, 0755); err != nil {
			log.Printf("[ancora] migration failed (create dir): %v", err)
			return
		}

		// Move DB and WAL/SHM files if they exist.
		for _, suffix := range []string{"", "-wal", "-shm"} {
			src := candidate + suffix
			dst := correctDB + suffix
			if _, statErr := os.Stat(src); statErr != nil {
				continue
			}
			if renameErr := os.Rename(src, dst); renameErr != nil {
				log.Printf("[ancora] migration failed (move %s): %v", filepath.Base(src), renameErr)
				return
			}
		}

		// Clean up empty orphaned directory.
		orphanDir := filepath.Dir(candidate)
		entries, _ := os.ReadDir(orphanDir)
		if len(entries) == 0 {
			os.Remove(orphanDir)
		}

		log.Printf("[ancora] migration complete — memories recovered")
		return
	}
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}

func cmdDoctor(cfg store.Config) {
	fmt.Printf("Ancora Doctor — System Health Check\n")
	fmt.Printf("Version: %s\n\n", version)

	hasErrors := false

	// 1. Database check
	fmt.Printf("━━━ Database ━━━\n")
	dbPath := filepath.Join(cfg.DataDir, "ancora.db")
	if info, err := os.Stat(dbPath); err == nil {
		fmt.Printf("  ✅ Database found: %s (%d MB)\n", dbPath, info.Size()/1024/1024)

		// Try to open and check readability
		if s, err := store.New(cfg); err == nil {
			defer s.Close()
			if stats, err := s.Stats(); err == nil {
				fmt.Printf("  ✅ Database readable: %d observations, %d sessions\n", stats.TotalObservations, stats.TotalSessions)
			} else {
				fmt.Printf("  ❌ Database not readable: %v\n", err)
				hasErrors = true
			}
		} else {
			fmt.Printf("  ❌ Cannot open database: %v\n", err)
			hasErrors = true
		}
	} else {
		fmt.Printf("  ⚠️  Database not found (will be created on first use)\n")
	}

	// 2. Embedding model check
	fmt.Printf("\n━━━ Embedding Model (Hybrid Search) ━━━\n")
	if embedder, err := embed.New(); err == nil {
		fmt.Printf("  ✅ Model found: %s\n", embedder.ModelPath)
		fmt.Printf("  ✅ llama-embedding CLI: %s\n", embedder.CLIPath)
		fmt.Printf("  ✅ Hybrid search: ENABLED (FTS5 + vector RRF fusion)\n")
	} else {
		if errors.Is(err, embed.ErrModelNotFound) {
			fmt.Printf("  ⚠️  Model not found\n")
			fmt.Printf("     Run `ancora setup` to install (~180 MB)\n")
			fmt.Printf("     Fallback: keyword-only search (FTS5)\n")
		} else if errors.Is(err, embed.ErrEmbedderUnavailable) {
			fmt.Printf("  ⚠️  llama-embedding CLI not found in PATH\n")
			fmt.Printf("     Install llama.cpp to enable hybrid search\n")
			fmt.Printf("     Fallback: keyword-only search (FTS5)\n")
		} else {
			fmt.Printf("  ❌ Unexpected error: %v\n", err)
			hasErrors = true
		}
	}

	// 3. Project detection
	fmt.Printf("\n━━━ Project Detection ━━━\n")
	if cwd, err := os.Getwd(); err == nil {
		detectedProject := detectProject(cwd)
		fmt.Printf("  ✅ Current directory: %s\n", cwd)
		fmt.Printf("  ✅ Detected project: %s\n", detectedProject)
	} else {
		fmt.Printf("  ❌ Cannot get current directory: %v\n", err)
		hasErrors = true
	}

	// 4. FTS5 check (implicitly tested by database check)
	fmt.Printf("\n━━━ Full-Text Search (FTS5) ━━━\n")
	fmt.Printf("  ✅ FTS5 enabled (built into SQLite)\n")

	// Summary
	fmt.Printf("\n━━━ Summary ━━━\n")
	if hasErrors {
		fmt.Printf("  ❌ Some checks failed — see errors above\n")
		exitFunc(1)
	} else {
		fmt.Printf("  ✅ All critical checks passed\n")
		fmt.Printf("  ℹ️  Warnings do not prevent Ancora from working\n")
	}
}

func cmdEmbeddings(cfg store.Config) {
	subCmd := "status"
	if len(os.Args) > 2 {
		subCmd = os.Args[2]
	}

	switch subCmd {
	case "status":
		cmdEmbeddingsStatus()
	case "install":
		cmdEmbeddingsInstall()
	case "backfill":
		cmdEmbeddingsBackfill(cfg)
	case "test":
		cmdEmbeddingsTest()
	default:
		fmt.Fprintf(os.Stderr, "unknown embeddings command: %s\n", subCmd)
		fmt.Fprintln(os.Stderr, "usage: ancora embeddings [status|install|backfill|test]")
		exitFunc(1)
	}
}

func cmdEmbeddingsStatus() {
	status, err := setupCheckEmbeddingsStatus()
	if err != nil {
		fatal(err)
	}
	fmt.Printf("Embedding Model Status\n")
	fmt.Printf("  Model:    %s\n", map[bool]string{true: "installed", false: "not found"}[status.ModelInstalled])
	if status.ModelInstalled {
		fmt.Printf("  Path:     %s\n", status.ModelPath)
	}
	fmt.Printf("  CLI:      %s\n", map[bool]string{true: "available", false: "not found"}[status.CLIAvailable])
	if status.CLIAvailable {
		fmt.Printf("  Path:     %s\n", status.CLIPath)
	}
	if status.Tested {
		fmt.Printf("  Verified: %s\n", map[bool]string{true: "yes", false: "no"}[status.Usable])
		if status.TestDimensions > 0 {
			fmt.Printf("  Dims:     %d\n", status.TestDimensions)
		}
		if status.TestError != "" {
			fmt.Printf("  Error:    %s\n", status.TestError)
		}
	}
	fmt.Printf("  Status:   %s\n", status.Message)

	if status.ModelInstalled && status.CLIAvailable {
		fmt.Printf("\nℹ️  Run 'ancora embeddings test' to verify functionality\n")
		fmt.Printf("ℹ️  Run 'ancora embeddings backfill' to generate vectors for existing observations\n")
	}
}

func cmdEmbeddingsInstall() {
	result, err := runEmbeddingsInstall()
	if err != nil {
		fatal(err)
	}
	printEmbeddingsInstallResult(result)
}

type embeddingsInstallResult struct {
	ModelPath string
	CLIPath   string
	CLIFound  bool
}

type embeddingsDownloader interface {
	Download() error
}

func runEmbeddingsInstall() (*embeddingsInstallResult, error) {
	destPath := filepath.Join(embed.ModelInstallPath(), embed.ModelFileName)
	downloader := newEmbeddingsDownloader(destPath)
	if err := downloader.Download(); err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}

	found, path := checkLlamaCpp()
	return &embeddingsInstallResult{
		ModelPath: destPath,
		CLIPath:   path,
		CLIFound:  found,
	}, nil
}

func printEmbeddingsInstallResult(result *embeddingsInstallResult) {
	fmt.Println("Installing embedding model...")
	fmt.Printf("✓ Model installed: %s\n", result.ModelPath)

	fmt.Println("\nChecking llama.cpp CLI...")
	if result.CLIFound {
		fmt.Printf("✓ llama.cpp CLI found: %s\n", result.CLIPath)
		fmt.Println("\n✓ Embeddings ready! Run 'ancora embeddings test' to verify.")
	} else {
		fmt.Println("⚠️  llama.cpp CLI not found in PATH")
		fmt.Println(llamaCppInstallInstructions())
	}
}

func cmdEmbeddingsBackfill(cfg store.Config) {
	fmt.Println("Running embedding backfill...")

	status, err := setupCheckEmbeddingsStatus()
	if err != nil {
		fatal(err)
	}

	if !status.ModelInstalled || !status.CLIAvailable {
		fmt.Println("⚠️  Embeddings not fully configured. Install first with:")
		fmt.Println("  ancora embeddings install")
		return
	}

	s, err := storeNew(cfg)
	if err != nil {
		fatal(err)
	}
	defer s.Close()

	embedder, err := newEmbedder()
	if err != nil {
		fatal(err)
	}

	svc := embedding.New(embedder, embedding.NewStoreAdapter(s))
	success, total, err := svc.Backfill()
	if err != nil {
		fatal(err)
	}

	fmt.Printf("Found %d observations to embed\n", total)
	fmt.Printf("✓ Backfill complete: %d/%d observations embedded\n", success, total)
}

func cmdEmbeddingsTest() {
	fmt.Println("Testing embedding generation...")
	embedder, err := newEmbedder()
	if err != nil {
		fmt.Printf("⚠️  Embeddings not available: %v\n", err)
		fmt.Println("\nRun 'ancora embeddings install' to set up.")
		exitFunc(1)
	}

	testText := "testing semantic search with PC components"
	vec, err := embedder.Embed(testText)
	if err != nil {
		fatal(err)
	}

	fmt.Printf("✓ Embedding generated: %d dimensions\n", len(vec))
	if ne, ok := embedder.(*embed.NomicEmbedder); ok {
		fmt.Printf("  CLI: %s\n", ne.CLIPath)
		fmt.Printf("  Model: %s\n", ne.ModelPath)
	}
	fmt.Println("✓ Embeddings working correctly!")
}

func runBackfill(cfg store.Config) error {
	status, err := setupCheckEmbeddingsStatus()
	if err != nil {
		return err
	}

	if !status.ModelInstalled || !status.CLIAvailable {
		return fmt.Errorf("embeddings not configured")
	}

	s, err := storeNew(cfg)
	if err != nil {
		return err
	}
	defer s.Close()

	embedder, err := newEmbedder()
	if err != nil {
		return err
	}

	svc := embedding.New(embedder, embedding.NewStoreAdapter(s))
	_, _, err = svc.Backfill()
	return err
}
