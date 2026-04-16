// Package mcp implements the Model Context Protocol server for Ancora.
//
// This exposes memory tools via MCP stdio transport so ANY agent
// (OpenCode, Claude Code, Cursor, Windsurf, etc.) can use Ancora's
// persistent memory just by adding it as an MCP server.
//
// Tool profiles allow agents to load only the tools they need:
//
//	ancora mcp                    → all 15 tools (default)
//	ancora mcp --tools=agent      → 11 tools agents actually use (per skill files)
//	ancora mcp --tools=admin      → 4 tools for TUI/CLI (delete, stats, timeline, merge)
//	ancora mcp --tools=agent,admin → combine profiles
//	ancora mcp --tools=ancora_save,ancora_search → individual tool names
package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/Syfra3/ancora/internal/embedding"
	projectpkg "github.com/Syfra3/ancora/internal/project"
	"github.com/Syfra3/ancora/internal/search"
	"github.com/Syfra3/ancora/internal/store"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Embedder is the interface for generating float32 query vectors.
// Satisfied by embed.NomicEmbedder and embed.MockEmbedder.
type Embedder interface {
	Embed(text string) ([]float32, error)
}

// MCPConfig holds configuration for the MCP server.
type MCPConfig struct {
	DefaultProject   string             // Auto-detected project name, used when LLM sends empty project
	Embedder         Embedder           // Optional: enables hybrid search. nil = keyword-only.
	EmbeddingService *embedding.Service // Optional: triggers async embedding after save.
}

var suggestTopicKey = store.SuggestTopicKey

var loadMCPStats = func(s *store.Store) (*store.Stats, error) {
	return s.Stats()
}

// ─── Tool Profiles ───────────────────────────────────────────────────────────
//
// "agent" — tools AI agents use during coding sessions:
//   ancora_save, ancora_search, ancora_context, ancora_summarize,
//   ancora_start, ancora_end, ancora_get,
//   ancora_suggest_topic, ancora_capture, ancora_save_prompt
//
// "admin" — tools for manual curation, TUI, and dashboards:
//   ancora_update, ancora_delete, ancora_stats, ancora_timeline, ancora_merge
//
// "all" (default) — every tool registered.

// ProfileAgent contains the tool names that AI agents need.
// Sourced from actual skill files and memory protocol instructions
// across all 4 supported agents (Claude Code, OpenCode, Gemini CLI, Codex).
var ProfileAgent = map[string]bool{
	"save":          true, // proactive save — referenced 17 times across protocols
	"search":        true, // search past memories — referenced 6 times
	"context":       true, // recent context from previous sessions — referenced 10 times
	"summarize":     true, // end-of-session summary — referenced 16 times
	"start":         true, // register session start
	"end":           true, // mark session completed
	"get":           true, // full observation content after search — referenced 4 times
	"suggest_topic": true, // stable topic key for upserts — referenced 3 times
	"capture":       true, // extract learnings from text — referenced in Gemini/Codex protocol
	"save_prompt":   true, // save user prompts
	"update":        true, // update observation by ID — skills say "use ancora_update when you have an exact ID to correct"
}

// ProfileAdmin contains tools for TUI, dashboards, and manual curation
// that are NOT referenced in any agent skill or memory protocol.
var ProfileAdmin = map[string]bool{
	"delete":   true, // only in OpenCode's ANCORA_TOOLS filter, not in any agent instructions
	"stats":    true, // only in OpenCode's ANCORA_TOOLS filter, not in any agent instructions
	"timeline": true, // only in OpenCode's ANCORA_TOOLS filter, not in any agent instructions
	"merge":    true, // destructive curation tool — not for agent use
}

// Profiles maps profile names to their tool sets.
var Profiles = map[string]map[string]bool{
	"agent": ProfileAgent,
	"admin": ProfileAdmin,
}

// ResolveTools takes a comma-separated string of profile names and/or
// individual tool names and returns the set of tool names to register.
// An empty input means "all" — every tool is registered.
func ResolveTools(input string) map[string]bool {
	input = strings.TrimSpace(input)
	if input == "" || input == "all" {
		return nil // nil means register everything
	}

	result := make(map[string]bool)
	for _, token := range strings.Split(input, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		if token == "all" {
			return nil
		}
		// Backward compatibility: strip ancora_ prefix if user provides it
		token = strings.TrimPrefix(token, "ancora_")

		if profile, ok := Profiles[token]; ok {
			for tool := range profile {
				result[tool] = true
			}
		} else {
			// Treat as individual tool name
			result[token] = true
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// NewServer creates an MCP server with ALL tools registered (backwards compatible).
func NewServer(s *store.Store) *server.MCPServer {
	return NewServerWithConfig(s, MCPConfig{}, nil)
}

// serverInstructions tells MCP clients when to use Ancora's tools.
// 6 core tools are eager (always in context). The rest are deferred
// and require ToolSearch to load.
const serverInstructions = `Ancora provides persistent memory that survives across sessions and compactions.

CORE TOOLS (always available — use without ToolSearch):
  ancora_save — save decisions, bugs, discoveries, conventions PROACTIVELY (do not wait to be asked)
  ancora_search — find past work, decisions, or context from previous sessions
  ancora_context — get recent session history (call at session start or after compaction)
  ancora_summarize — save end-of-session summary (MANDATORY before saying "done")
  ancora_get — get full untruncated content of a search result by ID
  ancora_save_prompt — save user prompt for context

DEFERRED TOOLS (use ToolSearch when needed):
  ancora_update, ancora_suggest_topic, ancora_start, ancora_end,
  ancora_stats, ancora_delete, ancora_timeline, ancora_capture, ancora_merge

PROACTIVE SAVE RULE: Call ancora_save immediately after ANY decision, bug fix, discovery, or convention — not just when asked.`

// NewServerWithTools creates an MCP server registering only the tools in
// the allowlist. If allowlist is nil, all tools are registered.
func NewServerWithTools(s *store.Store, allowlist map[string]bool) *server.MCPServer {
	return NewServerWithConfig(s, MCPConfig{}, allowlist)
}

// NewServerWithConfig creates an MCP server with full configuration including
// default project detection and optional tool allowlist.
func NewServerWithConfig(s *store.Store, cfg MCPConfig, allowlist map[string]bool) *server.MCPServer {
	srv := server.NewMCPServer(
		"ancora",
		"0.1.0",
		server.WithToolCapabilities(true),
		server.WithInstructions(serverInstructions),
	)

	registerTools(srv, s, cfg, allowlist)
	return srv
}

// shouldRegister returns true if the tool should be registered given the
// allowlist. If allowlist is nil, everything is allowed.
func shouldRegister(name string, allowlist map[string]bool) bool {
	if allowlist == nil {
		return true
	}
	return allowlist[name]
}

func registerTools(srv *server.MCPServer, s *store.Store, cfg MCPConfig, allowlist map[string]bool) {
	// ─── search (profile: agent, core — always in context) ─────────
	if shouldRegister("search", allowlist) {
		srv.AddTool(
			mcp.NewTool("search",
				mcp.WithDescription("Search your persistent memory across all sessions. Use this to find past decisions, bugs fixed, patterns used, files changed, or any context from previous coding sessions."),
				mcp.WithTitleAnnotation("Search Memory"),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithString("query",
					mcp.Required(),
					mcp.Description("Search query — natural language or keywords"),
				),
				mcp.WithString("type",
					mcp.Description("Filter by type: tool_use, file_change, command, file_read, search, manual, decision, architecture, bugfix, pattern"),
				),
				mcp.WithString("workspace",
					mcp.Description("Optional workspace filter; when omitted, searches across all local memory"),
				),
				mcp.WithString("visibility",
					mcp.Description("Filter by visibility: 'work' for work knowledge, 'personal' for private life knowledge. Omit to search across ALL observations regardless of visibility."),
				),
				mcp.WithNumber("limit",
					mcp.Description("Max results (default: 10, max: 20)"),
				),
			),
			handleSearch(s, cfg),
		)
	}

	// ─── save (profile: agent, core — always in context) ───────────
	if shouldRegister("save", allowlist) {
		srv.AddTool(
			mcp.NewTool("save",
				mcp.WithTitleAnnotation("Save Memory"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(false),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithDescription(`Save an important observation to persistent memory. Call this PROACTIVELY after completing significant work — don't wait to be asked.

WHEN to save (call this after each of these):
- Architectural decisions or tradeoffs
- Bug fixes (what was wrong, why, how you fixed it)
- New patterns or conventions established
- Configuration changes or environment setup
- Important discoveries or gotchas
- File structure changes

FORMAT for content — use this structured format:
  **What**: [concise description of what was done]
  **Why**: [the reasoning, user request, or problem that drove it]
  **Where**: [files/paths affected, e.g. src/auth/middleware.ts, internal/store/store.go]
  **Learned**: [any gotchas, edge cases, or decisions made — omit if none]

TITLE should be short and searchable, like: "JWT auth middleware", "FTS5 query sanitization", "Fixed N+1 in user list"

Examples:
  title: "Switched from sessions to JWT"
  type: "decision"
  content: "**What**: Replaced express-session with jsonwebtoken for auth\n**Why**: Session storage doesn't scale across multiple instances\n**Where**: src/middleware/auth.ts, src/routes/login.ts\n**Learned**: Must set httpOnly and secure flags on the cookie, refresh tokens need separate rotation logic"

  title: "Fixed FTS5 syntax error on special chars"
  type: "bugfix"
  content: "**What**: Wrapped each search term in quotes before passing to FTS5 MATCH\n**Why**: Users typing queries like 'fix auth bug' would crash because FTS5 interprets special chars as operators\n**Where**: internal/store/store.go — sanitizeFTS() function\n**Learned**: FTS5 MATCH syntax is NOT the same as LIKE — always sanitize user input"`),
				mcp.WithString("title",
					mcp.Required(),
					mcp.Description("Short, searchable title (e.g. 'JWT auth middleware', 'Fixed N+1 query')"),
				),
				mcp.WithString("content",
					mcp.Required(),
					mcp.Description("Structured content using **What**, **Why**, **Where**, **Learned** format"),
				),
				mcp.WithString("type",
					mcp.Description("Category: decision, architecture, bugfix, pattern, config, discovery, learning (default: manual)"),
				),
				mcp.WithString("session_id",
					mcp.Description("Session ID to associate with (default: manual-save-{workspace})"),
				),
				mcp.WithString("workspace",
					mcp.Description("Workspace/project name"),
				),
				mcp.WithString("visibility",
					mcp.Description("Scope for this observation: 'work' (default) or 'personal'. Use 'work' for any work knowledge — coding, research, writing, business (sync eligibility controlled by project enrollment). Use 'personal' for private life knowledge (finance, health, goals) — NEVER synced to Syfra Cloud."),
				),
				mcp.WithString("organization",
					mcp.Description("Organization name for Syfra Cloud (e.g. 'glim', 'syfra'). Only meaningful when visibility=work."),
				),
				mcp.WithString("topic_key",
					mcp.Description("Optional topic identifier for upserts (e.g. architecture/auth-model). Reuses and updates the latest observation in same workspace+visibility."),
				),
				mcp.WithString("references",
					mcp.Description(`Optional JSON array of explicit references to files, functions, or concepts.
Format: [{"type":"file","target":"internal/store/store.go"},{"type":"function","target":"AddObservation"}]
Types: "file" (file path), "function" (function/struct name), "concept" (named concept), "observation" (altro observation ID as string)`),
				),
			),
			handleSave(s, cfg),
		)
	}

	// ─── update (profile: agent, deferred) ──────────────────────────
	if shouldRegister("update", allowlist) {
		srv.AddTool(
			mcp.NewTool("update",
				mcp.WithDescription("Update an existing observation by ID. Only provided fields are changed."),
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("Update Memory"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(false),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithNumber("id",
					mcp.Required(),
					mcp.Description("Observation ID to update"),
				),
				mcp.WithString("title",
					mcp.Description("New title"),
				),
				mcp.WithString("content",
					mcp.Description("New content"),
				),
				mcp.WithString("type",
					mcp.Description("New type/category"),
				),
				mcp.WithString("workspace",
					mcp.Description("New workspace value"),
				),
				mcp.WithString("visibility",
					mcp.Description("New visibility: 'work' or 'personal'. Changing to 'personal' will prevent this observation from ever being synced to Syfra Cloud."),
				),
				mcp.WithString("organization",
					mcp.Description("New organization value for Syfra Cloud"),
				),
				mcp.WithString("topic_key",
					mcp.Description("New topic key (normalized internally)"),
				),
				mcp.WithString("references",
					mcp.Description(`Optional JSON array of explicit references. Pass an empty array "[]" to clear.
Format: [{"type":"file","target":"internal/store/store.go"},{"type":"function","target":"AddObservation"}]`),
				),
			),
			handleUpdate(s),
		)
	}

	// ─── suggest_topic (profile: agent, deferred) ───────────────
	if shouldRegister("suggest_topic", allowlist) {
		srv.AddTool(
			mcp.NewTool("suggest_topic",
				mcp.WithDescription("Suggest a stable topic_key for memory upserts. Use this before ancora_save when you want evolving topics (like architecture decisions) to update a single observation over time."),
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("Suggest Topic Key"),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithString("type",
					mcp.Description("Observation type/category, e.g. architecture, decision, bugfix"),
				),
				mcp.WithString("title",
					mcp.Description("Observation title (preferred input for stable keys)"),
				),
				mcp.WithString("content",
					mcp.Description("Observation content used as fallback if title is empty"),
				),
			),
			handleSuggestTopicKey(),
		)
	}

	// ─── delete (profile: admin, deferred) ──────────────────────────
	if shouldRegister("delete", allowlist) {
		srv.AddTool(
			mcp.NewTool("delete",
				mcp.WithDescription("Delete an observation by ID. Soft-delete by default; set hard_delete=true for permanent deletion."),
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("Delete Memory"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(true),
				mcp.WithIdempotentHintAnnotation(false),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithNumber("id",
					mcp.Required(),
					mcp.Description("Observation ID to delete"),
				),
				mcp.WithBoolean("hard_delete",
					mcp.Description("If true, permanently deletes the observation"),
				),
			),
			handleDelete(s),
		)
	}

	// ─── save_prompt (profile: agent, eager) ────────────────────────
	if shouldRegister("save_prompt", allowlist) {
		srv.AddTool(
			mcp.NewTool("save_prompt",
				mcp.WithDescription("Save a user prompt to persistent memory. Use this to record what the user asked — their intent, questions, and requests — so future sessions have context about the user's goals."),
				mcp.WithTitleAnnotation("Save User Prompt"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(false),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithString("content",
					mcp.Required(),
					mcp.Description("The user's prompt text"),
				),
				mcp.WithString("session_id",
					mcp.Description("Session ID to associate with (default: manual-save-{project})"),
				),
				mcp.WithString("project",
					mcp.Description("Project name"),
				),
			),
			handleSavePrompt(s, cfg),
		)
	}

	// ─── context (profile: agent, core — always in context) ────────
	if shouldRegister("context", allowlist) {
		srv.AddTool(
			mcp.NewTool("context",
				mcp.WithDescription("Get recent memory context from previous sessions. Shows recent sessions and observations to understand what was done before."),
				mcp.WithTitleAnnotation("Get Memory Context"),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithString("workspace",
					mcp.Description("Filter by workspace (omit for all workspaces)"),
				),
				mcp.WithString("visibility",
					mcp.Description("Filter observations by visibility: 'work' for work knowledge, 'personal' for private life knowledge. Omit to retrieve ALL observations regardless of visibility."),
				),
				mcp.WithNumber("limit",
					mcp.Description("Number of observations to retrieve (default: 20)"),
				),
			),
			handleContext(s, cfg),
		)
	}

	// ─── stats (profile: admin, deferred) ───────────────────────────
	if shouldRegister("stats", allowlist) {
		srv.AddTool(
			mcp.NewTool("stats",
				mcp.WithDescription("Show memory system statistics — total sessions, observations, and projects tracked."),
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("Memory Stats"),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
			),
			handleStats(s),
		)
	}

	// ─── timeline (profile: admin, deferred) ────────────────────────
	if shouldRegister("timeline", allowlist) {
		srv.AddTool(
			mcp.NewTool("timeline",
				mcp.WithDescription("Show chronological context around a specific observation. Use after ancora_search to drill into the timeline of events surrounding a search result. This is the progressive disclosure pattern: search first, then timeline to understand context."),
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("Memory Timeline"),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithNumber("observation_id",
					mcp.Required(),
					mcp.Description("The observation ID to center the timeline on (from ancora_search results)"),
				),
				mcp.WithNumber("before",
					mcp.Description("Number of observations to show before the focus (default: 5)"),
				),
				mcp.WithNumber("after",
					mcp.Description("Number of observations to show after the focus (default: 5)"),
				),
			),
			handleTimeline(s),
		)
	}

	// ─── get (profile: agent, eager) ────────────────────
	if shouldRegister("get", allowlist) {
		srv.AddTool(
			mcp.NewTool("get",
				mcp.WithDescription("Get the full content of a specific observation by ID. Use when you need the complete, untruncated content of an observation found via ancora_search or ancora_timeline."),
				mcp.WithTitleAnnotation("Get Observation"),
				mcp.WithReadOnlyHintAnnotation(true),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithNumber("id",
					mcp.Required(),
					mcp.Description("The observation ID to retrieve"),
				),
			),
			handleGetObservation(s),
		)
	}

	// ─── summarize (profile: agent, core — always in context) ─
	if shouldRegister("summarize", allowlist) {
		srv.AddTool(
			mcp.NewTool("summarize",
				mcp.WithTitleAnnotation("Save Session Summary"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(false),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithDescription(`Save a comprehensive end-of-session summary. Call this when a session is ending or when significant work is complete. This creates a structured summary that future sessions will use to understand what happened.

FORMAT — use this exact structure in the content field:

## Goal
[One sentence: what were we building/working on in this session]

## Instructions
[User preferences, constraints, or context discovered during this session. Things a future agent needs to know about HOW the user wants things done. Skip if nothing notable.]

## Discoveries
- [Technical finding, gotcha, or learning 1]
- [Technical finding 2]
- [Important API behavior, config quirk, etc.]

## Accomplished
- ✅ [Completed task 1 — with key implementation details]
- ✅ [Completed task 2 — mention files changed]
- 🔲 [Identified but not yet done — for next session]

## Relevant Files
- path/to/file.ts — [what it does or what changed]
- path/to/other.go — [role in the architecture]

GUIDELINES:
- Be CONCISE but don't lose important details (file paths, error messages, decisions)
- Focus on WHAT and WHY, not HOW (the code itself is in the repo)
- Include things that would save a future agent time
- The Discoveries section is the most valuable — capture gotchas and non-obvious learnings
- Relevant Files should only include files that were significantly changed or are important for context`),
				mcp.WithString("content",
					mcp.Required(),
					mcp.Description("Full session summary using the Goal/Instructions/Discoveries/Accomplished/Files format"),
				),
				mcp.WithString("session_id",
					mcp.Description("Session ID (default: manual-save-{project})"),
				),
				mcp.WithString("project",
					mcp.Required(),
					mcp.Description("Project name"),
				),
			),
			handleSessionSummary(s, cfg),
		)
	}

	// ─── start (profile: agent, deferred) ───────────────────
	if shouldRegister("start", allowlist) {
		srv.AddTool(
			mcp.NewTool("start",
				mcp.WithDescription("Register the start of a new coding session. Call this at the beginning of a session to track activity."),
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("Start Session"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithString("id",
					mcp.Required(),
					mcp.Description("Unique session identifier"),
				),
				mcp.WithString("project",
					mcp.Required(),
					mcp.Description("Project name"),
				),
				mcp.WithString("directory",
					mcp.Description("Working directory"),
				),
			),
			handleSessionStart(s, cfg),
		)
	}

	// ─── end (profile: agent, deferred) ─────────────────────
	if shouldRegister("end", allowlist) {
		srv.AddTool(
			mcp.NewTool("end",
				mcp.WithDescription("Mark a coding session as completed with an optional summary."),
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("End Session"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithString("id",
					mcp.Required(),
					mcp.Description("Session identifier to close"),
				),
				mcp.WithString("summary",
					mcp.Description("Summary of what was accomplished"),
				),
			),
			handleSessionEnd(s),
		)
	}

	// ─── capture (profile: agent, deferred) ─────────────────
	if shouldRegister("capture", allowlist) {
		srv.AddTool(
			mcp.NewTool("capture",
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("Capture Learnings"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(false),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithDescription(`Extract and save structured learnings from text output. Use this at the end of a task to capture knowledge automatically.

The tool looks for sections like "## Key Learnings:" or "## Aprendizajes Clave:" and extracts numbered or bulleted items. Each item is saved as a separate observation.

Duplicates are automatically detected and skipped — safe to call multiple times with the same content.`),
				mcp.WithString("content",
					mcp.Required(),
					mcp.Description("The text output containing a '## Key Learnings:' section with numbered or bulleted items"),
				),
				mcp.WithString("session_id",
					mcp.Description("Session ID (default: manual-save-{project})"),
				),
				mcp.WithString("project",
					mcp.Description("Project name"),
				),
				mcp.WithString("source",
					mcp.Description("Source identifier (e.g. 'subagent-stop', 'session-end')"),
				),
			),
			handleCapturePassive(s, cfg),
		)
	}

	// ─── merge (profile: admin, deferred) ──────────────────
	if shouldRegister("merge", allowlist) {
		srv.AddTool(
			mcp.NewTool("merge",
				mcp.WithDescription("Merge memories from multiple project name variants into one canonical name. Use when you discover project name drift (e.g. 'MyProject' and 'myproject' should be the same project). DESTRUCTIVE — moves all records from source names to the canonical name."),
				mcp.WithDeferLoading(true),
				mcp.WithTitleAnnotation("Merge Projects"),
				mcp.WithReadOnlyHintAnnotation(false),
				mcp.WithDestructiveHintAnnotation(true),
				mcp.WithIdempotentHintAnnotation(true),
				mcp.WithOpenWorldHintAnnotation(false),
				mcp.WithString("from",
					mcp.Required(),
					mcp.Description("Comma-separated list of project names to merge FROM (e.g. 'MyProject,my-project,MYPROJECT')"),
				),
				mcp.WithString("to",
					mcp.Required(),
					mcp.Description("The canonical project name to merge INTO (e.g. 'myproject')"),
				),
			),
			handleMergeProjects(s),
		)
	}
}

// ─── Tool Handlers ───────────────────────────────────────────────────────────

func handleSearch(s *store.Store, cfg MCPConfig) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		query, _ := req.GetArguments()["query"].(string)
		typ, _ := req.GetArguments()["type"].(string)
		workspace, _ := req.GetArguments()["workspace"].(string)
		visibility, _ := req.GetArguments()["visibility"].(string)
		limit := intArg(req, "limit", 10)

		searchResults, searchMode, err := search.SearchWithOptions(query, store.SearchOptions{
			Type:       typ,
			Workspace:  workspace,
			Visibility: visibility,
			Limit:      limit,
		}, cfg.Embedder, s)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Search error: %s. Try simpler keywords.", err)), nil
		}

		var results []store.SearchResult
		for _, r := range searchResults {
			results = append(results, r.SearchResult)
		}

		if len(results) == 0 {
			return mcp.NewToolResultText(fmt.Sprintf("Ancora\n**Searching**: %s\n\nNo memories found.", query)), nil
		}

		var b strings.Builder
		fmt.Fprintf(&b, "Ancora\n**Searching**: %s\n\nFound %d memories (search_mode: %s):\n\n", query, len(results), searchMode)
		anyTruncated := false
		for i, r := range results {
			workspaceDisplay := ""
			if r.Workspace != nil {
				workspaceDisplay = fmt.Sprintf(" | workspace: %s", *r.Workspace)
			}
			preview := truncate(r.Content, 300)
			if len(r.Content) > 300 {
				anyTruncated = true
				preview += " [preview]"
			}
			fmt.Fprintf(&b, "[%d] #%d (%s) — %s\n    %s\n    %s%s | visibility: %s\n\n",
				i+1, r.ID, r.Type, r.Title,
				preview,
				r.CreatedAt, workspaceDisplay, r.Visibility)
		}
		if anyTruncated {
			fmt.Fprintf(&b, "---\nResults above are previews (300 chars). To read the full content of a specific memory, call ancora_get(id: <ID>).\n")
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func handleSave(s *store.Store, cfg MCPConfig) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		title, _ := req.GetArguments()["title"].(string)
		content, _ := req.GetArguments()["content"].(string)
		typ, _ := req.GetArguments()["type"].(string)
		sessionID, _ := req.GetArguments()["session_id"].(string)
		workspace, _ := req.GetArguments()["workspace"].(string)
		visibility, _ := req.GetArguments()["visibility"].(string)
		organization, _ := req.GetArguments()["organization"].(string)
		topicKey, _ := req.GetArguments()["topic_key"].(string)
		references, _ := req.GetArguments()["references"].(string)

		// Apply default workspace when LLM sends empty
		if workspace == "" {
			workspace = cfg.DefaultProject
		}
		// Normalize workspace name and capture warning
		normalized, normWarning := store.NormalizeProject(workspace)
		workspace = normalized

		if typ == "" {
			typ = "manual"
		}
		if sessionID == "" {
			sessionID = defaultSessionID(workspace)
		}
		suggestedTopicKey := suggestTopicKey(typ, title, content)

		// Check for similar existing workspaces (only when this workspace has no existing observations)
		var similarWarning string
		if workspace != "" {
			existingNames, _ := s.ListProjectNames()
			isNew := true
			for _, e := range existingNames {
				if e == workspace {
					isNew = false
					break
				}
			}
			if isNew && len(existingNames) > 0 {
				matches := projectpkg.FindSimilar(workspace, existingNames, 3)
				if len(matches) > 0 {
					bestMatch := matches[0].Name
					// Cheap count query instead of full ListProjectsWithStats
					obsCount, _ := s.CountObservationsForProject(bestMatch)
					similarWarning = fmt.Sprintf("⚠️ Workspace %q has no memories. Similar workspace found: %q (%d memories). Consider using that name instead.", workspace, bestMatch, obsCount)
				}
			}
		}

		// Ensure the session exists
		s.CreateSession(sessionID, workspace, "")

		truncated := len(content) > s.MaxObservationLength()

		obsID, err := s.AddObservation(store.AddObservationParams{
			SessionID:    sessionID,
			Type:         typ,
			Title:        title,
			Content:      content,
			Workspace:    workspace,
			Visibility:   visibility,
			Organization: organization,
			TopicKey:     topicKey,
			References:   references,
		})
		if err != nil {
			return mcp.NewToolResultError("Failed to save: " + err.Error()), nil
		}

		// Fire-and-forget: generate embedding asynchronously.
		// Non-blocking — returns immediately even if embedder is unavailable.
		if cfg.EmbeddingService != nil {
			cfg.EmbeddingService.EnqueueWithText(obsID, title+". "+content)
		}

		msg := fmt.Sprintf("Ancora\n**Saving**: %s\n\nMemory saved (%s)", title, typ)
		if topicKey == "" && suggestedTopicKey != "" {
			msg += fmt.Sprintf("\nSuggested topic_key: %s", suggestedTopicKey)
		}
		if truncated {
			msg += fmt.Sprintf("\n⚠ WARNING: Content was truncated from %d to %d chars. Consider splitting into smaller observations.", len(content), s.MaxObservationLength())
		}
		if normWarning != "" {
			msg += "\n" + normWarning
		}
		if similarWarning != "" {
			msg += "\n" + similarWarning
		}
		return mcp.NewToolResultText(msg), nil
	}
}

func handleSuggestTopicKey() server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		typ, _ := req.GetArguments()["type"].(string)
		title, _ := req.GetArguments()["title"].(string)
		content, _ := req.GetArguments()["content"].(string)

		if strings.TrimSpace(title) == "" && strings.TrimSpace(content) == "" {
			return mcp.NewToolResultError("provide title or content to suggest a topic_key"), nil
		}

		topicKey := suggestTopicKey(typ, title, content)
		if topicKey == "" {
			return mcp.NewToolResultError("could not suggest topic_key from input"), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Suggested topic_key: %s", topicKey)), nil
	}
}

func handleUpdate(s *store.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := int64(intArg(req, "id", 0))
		if id == 0 {
			return mcp.NewToolResultError("id is required"), nil
		}

		update := store.UpdateObservationParams{}
		if v, ok := req.GetArguments()["title"].(string); ok {
			update.Title = &v
		}
		if v, ok := req.GetArguments()["content"].(string); ok {
			update.Content = &v
		}
		if v, ok := req.GetArguments()["type"].(string); ok {
			update.Type = &v
		}
		if v, ok := req.GetArguments()["workspace"].(string); ok {
			update.Workspace = &v
		}
		if v, ok := req.GetArguments()["visibility"].(string); ok {
			update.Visibility = &v
		}
		if v, ok := req.GetArguments()["organization"].(string); ok {
			update.Organization = &v
		}
		if v, ok := req.GetArguments()["topic_key"].(string); ok {
			update.TopicKey = &v
		}
		if v, ok := req.GetArguments()["references"].(string); ok {
			update.References = &v
		}

		if update.Title == nil && update.Content == nil && update.Type == nil && update.Workspace == nil && update.Visibility == nil && update.Organization == nil && update.TopicKey == nil && update.References == nil {
			return mcp.NewToolResultError("provide at least one field to update"), nil
		}

		var contentLen int
		if update.Content != nil {
			contentLen = len(*update.Content)
		}

		obs, err := s.UpdateObservation(id, update)
		if err != nil {
			return mcp.NewToolResultError("Failed to update memory: " + err.Error()), nil
		}

		msg := fmt.Sprintf("Ancora\n**Updating**: observation #%d\n\nMemory updated: %q (%s, visibility=%s)", id, obs.Title, obs.Type, obs.Visibility)
		if contentLen > s.MaxObservationLength() {
			msg += fmt.Sprintf("\n⚠ WARNING: Content was truncated from %d to %d chars. Consider splitting into smaller observations.", contentLen, s.MaxObservationLength())
		}
		return mcp.NewToolResultText(msg), nil
	}
}

func handleDelete(s *store.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := int64(intArg(req, "id", 0))
		if id == 0 {
			return mcp.NewToolResultError("id is required"), nil
		}

		hardDelete := boolArg(req, "hard_delete", false)
		if err := s.DeleteObservation(id, hardDelete); err != nil {
			return mcp.NewToolResultError("Failed to delete memory: " + err.Error()), nil
		}

		mode := "soft-deleted"
		if hardDelete {
			mode = "permanently deleted"
		}
		return mcp.NewToolResultText(fmt.Sprintf("Memory #%d %s", id, mode)), nil
	}
}

func handleSavePrompt(s *store.Store, cfg MCPConfig) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		content, _ := req.GetArguments()["content"].(string)
		sessionID, _ := req.GetArguments()["session_id"].(string)
		project, _ := req.GetArguments()["project"].(string)

		// Apply default project when LLM sends empty
		if project == "" {
			project = cfg.DefaultProject
		}
		project, _ = store.NormalizeProject(project)

		if sessionID == "" {
			sessionID = defaultSessionID(project)
		}

		// Ensure the session exists
		s.CreateSession(sessionID, project, "")

		_, err := s.AddPrompt(store.AddPromptParams{
			SessionID: sessionID,
			Content:   content,
			Project:   project,
		})
		if err != nil {
			return mcp.NewToolResultError("Failed to save prompt: " + err.Error()), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Prompt saved: %q", truncate(content, 80))), nil
	}
}

func handleContext(s *store.Store, cfg MCPConfig) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		workspace, _ := req.GetArguments()["workspace"].(string)
		visibility, _ := req.GetArguments()["visibility"].(string)

		// Apply default workspace when LLM sends empty
		if workspace == "" {
			workspace = cfg.DefaultProject
		}
		workspace, _ = store.NormalizeProject(workspace)

		context, err := s.FormatContext(workspace, visibility)
		if err != nil {
			return mcp.NewToolResultError("Failed to get context: " + err.Error()), nil
		}

		if context == "" {
			return mcp.NewToolResultText("Ancora\n**Loading context**: recent observations\n\nNo previous session memories found."), nil
		}

		stats, _ := s.Stats()
		var projects string
		if len(stats.Projects) > 0 {
			projects = strings.Join(stats.Projects, ", ")
		} else {
			projects = "none"
		}

		result := fmt.Sprintf("Ancora\n**Loading context**: recent observations\n\n%s\n---\nMemory stats: %d sessions, %d observations across projects: %s",
			context, stats.TotalSessions, stats.TotalObservations, projects)

		return mcp.NewToolResultText(result), nil
	}
}

func handleStats(s *store.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		stats, err := loadMCPStats(s)
		if err != nil {
			return mcp.NewToolResultError("Failed to get stats: " + err.Error()), nil
		}

		var projects string
		if len(stats.Projects) > 0 {
			projects = strings.Join(stats.Projects, ", ")
		} else {
			projects = "none yet"
		}

		result := fmt.Sprintf("Memory System Stats:\n- Sessions: %d\n- Observations: %d\n- Prompts: %d\n- Projects: %s",
			stats.TotalSessions, stats.TotalObservations, stats.TotalPrompts, projects)

		return mcp.NewToolResultText(result), nil
	}
}

func handleTimeline(s *store.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		observationID := int64(intArg(req, "observation_id", 0))
		if observationID == 0 {
			return mcp.NewToolResultError("observation_id is required"), nil
		}
		before := intArg(req, "before", 5)
		after := intArg(req, "after", 5)

		result, err := s.Timeline(observationID, before, after)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Timeline error: %s", err)), nil
		}

		var b strings.Builder

		// Session header
		if result.SessionInfo != nil {
			summary := ""
			if result.SessionInfo.Summary != nil {
				summary = fmt.Sprintf(" — %s", truncate(*result.SessionInfo.Summary, 100))
			}
			fmt.Fprintf(&b, "Session: %s (%s)%s\n", result.SessionInfo.Project, result.SessionInfo.StartedAt, summary)
			fmt.Fprintf(&b, "Total observations in session: %d\n\n", result.TotalInRange)
		}

		// Before entries
		if len(result.Before) > 0 {
			b.WriteString("─── Before ───\n")
			for _, e := range result.Before {
				fmt.Fprintf(&b, "  #%d [%s] %s — %s\n", e.ID, e.Type, e.Title, truncate(e.Content, 150))
			}
			b.WriteString("\n")
		}

		// Focus observation (highlighted)
		fmt.Fprintf(&b, ">>> #%d [%s] %s <<<\n", result.Focus.ID, result.Focus.Type, result.Focus.Title)
		fmt.Fprintf(&b, "    %s\n", truncate(result.Focus.Content, 500))
		fmt.Fprintf(&b, "    %s\n\n", result.Focus.CreatedAt)

		// After entries
		if len(result.After) > 0 {
			b.WriteString("─── After ───\n")
			for _, e := range result.After {
				fmt.Fprintf(&b, "  #%d [%s] %s — %s\n", e.ID, e.Type, e.Title, truncate(e.Content, 150))
			}
		}

		return mcp.NewToolResultText(b.String()), nil
	}
}

func handleGetObservation(s *store.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := int64(intArg(req, "id", 0))
		if id == 0 {
			return mcp.NewToolResultError("id is required"), nil
		}

		obs, err := s.GetObservation(id)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Observation #%d not found", id)), nil
		}

		workspace := ""
		if obs.Workspace != nil {
			workspace = fmt.Sprintf("\nWorkspace: %s", *obs.Workspace)
		}
		visibility := fmt.Sprintf("\nVisibility: %s", obs.Visibility)
		topic := ""
		if obs.TopicKey != nil {
			topic = fmt.Sprintf("\nTopic: %s", *obs.TopicKey)
		}
		toolName := ""
		if obs.ToolName != nil {
			toolName = fmt.Sprintf("\nTool: %s", *obs.ToolName)
		}
		duplicateMeta := fmt.Sprintf("\nDuplicates: %d", obs.DuplicateCount)
		revisionMeta := fmt.Sprintf("\nRevisions: %d", obs.RevisionCount)
		refsMeta := ""
		if obs.References != nil && *obs.References != "" {
			refsMeta = fmt.Sprintf("\nReferences: %s", *obs.References)
		}

		result := fmt.Sprintf("Ancora\n**Retrieving**: observation #%d\n\n#%d [%s] %s\n%s\nSession: %s%s%s\nCreated: %s",
			id,
			obs.ID, obs.Type, obs.Title,
			obs.Content,
			obs.SessionID, workspace+visibility+topic, toolName+duplicateMeta+revisionMeta+refsMeta,
			obs.CreatedAt,
		)

		return mcp.NewToolResultText(result), nil
	}
}

func handleSessionSummary(s *store.Store, cfg MCPConfig) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		content, _ := req.GetArguments()["content"].(string)
		sessionID, _ := req.GetArguments()["session_id"].(string)
		project, _ := req.GetArguments()["project"].(string)

		// Apply default project when LLM sends empty
		if project == "" {
			project = cfg.DefaultProject
		}
		project, _ = store.NormalizeProject(project)

		if sessionID == "" {
			sessionID = defaultSessionID(project)
		}

		// Ensure the session exists
		s.CreateSession(sessionID, project, "")

		title := fmt.Sprintf("Session summary: %s", project)
		obsID, err := s.AddObservation(store.AddObservationParams{
			SessionID: sessionID,
			Type:      "session_summary",
			Title:     title,
			Content:   content,
			Workspace: project,
		})
		if err != nil {
			return mcp.NewToolResultError("Failed to save session summary: " + err.Error()), nil
		}

		// Fire-and-forget: generate embedding asynchronously.
		if cfg.EmbeddingService != nil {
			cfg.EmbeddingService.EnqueueWithText(obsID, title+". "+content)
		}

		return mcp.NewToolResultText(fmt.Sprintf("Session summary saved for project %q", project)), nil
	}
}

func handleSessionStart(s *store.Store, cfg MCPConfig) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, _ := req.GetArguments()["id"].(string)
		project, _ := req.GetArguments()["project"].(string)
		directory, _ := req.GetArguments()["directory"].(string)

		// Apply default project when LLM sends empty
		if project == "" {
			project = cfg.DefaultProject
		}
		project, _ = store.NormalizeProject(project)

		if err := s.CreateSession(id, project, directory); err != nil {
			return mcp.NewToolResultError("Failed to start session: " + err.Error()), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Session %q started for project %q", id, project)), nil
	}
}

func handleSessionEnd(s *store.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id, _ := req.GetArguments()["id"].(string)
		summary, _ := req.GetArguments()["summary"].(string)

		if err := s.EndSession(id, summary); err != nil {
			return mcp.NewToolResultError("Failed to end session: " + err.Error()), nil
		}

		return mcp.NewToolResultText(fmt.Sprintf("Session %q completed", id)), nil
	}
}

func handleCapturePassive(s *store.Store, cfg MCPConfig) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		content, _ := req.GetArguments()["content"].(string)
		sessionID, _ := req.GetArguments()["session_id"].(string)
		project, _ := req.GetArguments()["project"].(string)
		source, _ := req.GetArguments()["source"].(string)

		// Apply default project when LLM sends empty
		if project == "" {
			project = cfg.DefaultProject
		}
		project, _ = store.NormalizeProject(project)

		if content == "" {
			return mcp.NewToolResultError("content is required — include text with a '## Key Learnings:' section"), nil
		}

		if sessionID == "" {
			sessionID = defaultSessionID(project)
			_ = s.CreateSession(sessionID, project, "")
		}

		if source == "" {
			source = "mcp-passive"
		}

		result, err := s.PassiveCapture(store.PassiveCaptureParams{
			SessionID: sessionID,
			Content:   content,
			Project:   project,
			Source:    source,
		})
		if err != nil {
			return mcp.NewToolResultError("Passive capture failed: " + err.Error()), nil
		}

		// Fire-and-forget: enqueue embeddings for all newly saved observations.
		if cfg.EmbeddingService != nil {
			for _, id := range result.SavedIDs {
				cfg.EmbeddingService.EnqueueWithText(id, content)
			}
		}

		return mcp.NewToolResultText(fmt.Sprintf(
			"Passive capture complete: extracted=%d saved=%d duplicates=%d",
			result.Extracted, result.Saved, result.Duplicates,
		)), nil
	}
}

func handleMergeProjects(s *store.Store) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		fromStr, _ := req.GetArguments()["from"].(string)
		to, _ := req.GetArguments()["to"].(string)

		if fromStr == "" || to == "" {
			return mcp.NewToolResultError("both 'from' and 'to' are required"), nil
		}

		var sources []string
		for _, src := range strings.Split(fromStr, ",") {
			src = strings.TrimSpace(src)
			if src != "" {
				sources = append(sources, src)
			}
		}

		if len(sources) == 0 {
			return mcp.NewToolResultError("at least one source project name is required in 'from'"), nil
		}

		result, err := s.MergeProjects(sources, to)
		if err != nil {
			return mcp.NewToolResultError("Merge failed: " + err.Error()), nil
		}

		msg := fmt.Sprintf("Merged %d source(s) into %q:\n", len(result.SourcesMerged), result.Canonical)
		msg += fmt.Sprintf("  Observations moved: %d\n", result.ObservationsUpdated)
		msg += fmt.Sprintf("  Sessions moved:     %d\n", result.SessionsUpdated)
		msg += fmt.Sprintf("  Prompts moved:      %d\n", result.PromptsUpdated)

		return mcp.NewToolResultText(msg), nil
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// defaultSessionID returns a project-scoped default session ID.
// If project is non-empty: "manual-save-{project}"
// If project is empty: "manual-save"
func defaultSessionID(project string) string {
	if project == "" {
		return "manual-save"
	}
	return "manual-save-" + project
}

func intArg(req mcp.CallToolRequest, key string, defaultVal int) int {
	v, ok := req.GetArguments()[key].(float64)
	if !ok {
		return defaultVal
	}
	return int(v)
}

func boolArg(req mcp.CallToolRequest, key string, defaultVal bool) bool {
	v, ok := req.GetArguments()[key].(bool)
	if !ok {
		return defaultVal
	}
	return v
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
