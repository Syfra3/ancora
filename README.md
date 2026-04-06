<div align="center">
  <img src="assets/ancora-logo.png" alt="Ancora Logo" width="200"/>
  
  # Ancora
  
  **Scalable memory for real AI agent orchestration and shared knowledge**
  
  > Persistent memory for AI agents. Local-first, open source.
</div>

[![GitHub stars](https://img.shields.io/github/stars/Syfra3/ancora?style=social)](https://github.com/Syfra3/ancora)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Syfra3/ancora)](https://go.dev/)

**Ancora** is a persistent memory system for AI coding agents like Claude Code and OpenCode. It enables agents to remember context, decisions, and patterns across sessions using hybrid search and local embeddings.

## 100% Free & Open Source

- Full local memory storage (SQLite + FTS5)
- 15 MCP tools for memory management
- Hybrid keyword + semantic search
- Local embeddings (nomic-embed-text)
- CLI, TUI, HTTP API
- Works offline, zero telemetry
- MIT licensed

## Cloud Sync (Coming Soon)

Want your memories synced across all your devices?

**[Join the Waitlist for Syfra Cloud](https://syfra.co/waitlist)**

**What you'll get:**
- Multi-device sync (Mac, Linux, Windows)
- End-to-end encryption
- Partner/team sharing
- Web dashboard
- Advanced analytics

---

## Status

**Production Ready** - All features implemented and tested (621 tests passing).

## Quick Start

### Installation

#### Recommended: Homebrew (macOS/Linux)

```bash
# Add the Syfra tap (one time)
brew tap Syfra3/tap

# Install Ancora
brew install ancora

# Verify installation
ancora --version
```

#### Alternative: Bash Install Script (all platforms)

**Quick install:**
```bash
curl -sSL https://raw.githubusercontent.com/Syfra3/ancora/main/scripts/install-ancora.sh | bash
```

**Custom install directory:**
```bash
# Install to specific location (e.g., ~/.local/bin)
export ANCORA_INSTALL_DIR=$HOME/.local/bin
curl -sSL https://raw.githubusercontent.com/Syfra3/ancora/main/scripts/install-ancora.sh | bash

# Make sure the directory is in your PATH
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc  # or ~/.zshrc
source ~/.bashrc
```

The install script will:
- Detect your OS and architecture automatically
- Download the appropriate binary from GitHub Releases
- Verify the checksum
- Install to `/usr/local/bin` (or `$ANCORA_INSTALL_DIR`)
- Make the binary executable

#### From Source

```bash
# Via go install (requires Go 1.25+)
go install github.com/Syfra3/ancora/cmd/ancora@latest

# Or build locally
git clone https://github.com/Syfra3/ancora.git
cd syfra/ancora
make build
```

#### Manual Download

Download pre-built binaries from [GitHub Releases](https://github.com/Syfra3/ancora/releases):

**Platforms:**
- **Linux:** `amd64`, `arm64`
- **macOS:** `amd64` (Intel), `arm64` (Apple Silicon)  
- **Windows:** `amd64`

**Example (Linux amd64):**
```bash
VERSION=1.0.0  # Check releases for latest version
curl -LO "https://github.com/Syfra3/ancora/releases/download/ancora-v${VERSION}/ancora-${VERSION}-linux-amd64.tar.gz"
tar xzf "ancora-${VERSION}-linux-amd64.tar.gz"
sudo mv ancora /usr/local/bin/
chmod +x /usr/local/bin/ancora
```

### Run as MCP Server

```bash
# Recommended: agent profile (11 core tools)
syfra mcp --tools=agent

# Admin profile (4 management tools)
syfra mcp --tools=admin

# All tools (15 total)
syfra mcp
```

Configure in your AI agent's MCP settings:
```json
{
  "mcp": {
    "ancora": {
      "type": "stdio",
      "command": "syfra",
      "args": ["mcp", "--tools=agent"]
    }
  }
}
```

### Setup Agent Integration

```bash
syfra setup              # Interactive wizard
syfra setup claude-code  # Auto-install for Claude Code
syfra setup opencode     # Auto-install for OpenCode
```

### Basic CLI Usage

```bash
# Search memories
syfra search "authentication bug" --workspace=myapp

# Save memory (visibility auto-inferred)
syfra save "Fixed N+1 query" "Optimized user list query" --workspace=myapp --type=bugfix

# Save personal memory
syfra save "2026 Goals" "my goals for the year" --visibility=personal

# View statistics
syfra stats

# Interactive terminal UI
syfra tui

# HTTP server (port 7437)
syfra serve

# Export/import backups
syfra export backup.json
syfra import backup.json
```

## Features

- **15 MCP Tools** - Full memory management suite (ancora_save, ancora_search, ancora_context, etc.)
- **Hybrid Search** - FTS5 keyword + vector semantic search with RRF fusion
- **Local Embeddings** - nomic-embed-text-v1.5 (768-dim, optional, ~270MB)
- **HTTP API** - Engram-compatible REST endpoints on port 7437
- **SQLite Storage** - FTS5 full-text search + vector embeddings in single file
- **Zero CGO** - Pure Go using modernc.org/sqlite, single binary, no dependencies
- **Agent Integrations** - Native plugins for Claude Code and OpenCode
- **Interactive TUI** - Browse, search, timeline view with Bubbletea
- **Project Detection** - Auto-detect from git remote/root directory
- **Smart Deduplication** - 15-minute window with topic-based upserts
- **Export/Import** - JSON backup and restore
- **Cross-Platform** - Linux, macOS, Windows support

## Architecture

```
cmd/syfra/           # CLI entrypoint with subcommands
internal/
  store/             # SQLite data layer (FTS5, vector, CRUD, deduplication)
  mcp/               # MCP server + 15 tool handlers with profiles
  search/            # Hybrid search engine (keyword + semantic RRF fusion)
  embed/             # Embedding pipeline (nomic-embed-text via llama.cpp)
  server/            # HTTP API with Engram-compatible endpoints
  tui/               # Bubbletea interactive UI (dashboard, search, timeline)
  project/           # Project name detection and consolidation
  setup/             # Agent integration installers
  version/           # Version detection and update checks
```

## MCP Tools

### Agent Profile (11 tools - recommended)

Core tools for AI agents to save and retrieve memories:

- **ancora_save** - Save decisions, bugs, discoveries proactively
- **ancora_search** - Hybrid keyword + semantic search (limit: 20)
- **ancora_context** - Get recent session context
- **ancora_summarize** - Save structured end-of-session summaries
- **ancora_get** - Fetch full untruncated content by ID
- **ancora_update** - Update existing observation
- **ancora_suggest_topic** - Generate stable keys for topic upserts
- **ancora_start** - Register session start
- **ancora_end** - Mark session complete
- **ancora_save_prompt** - Save user prompts for context
- **ancora_capture** - Extract learnings from formatted text

### Admin Profile (4 tools)

Management tools for TUI/CLI/dashboards:

- **ancora_delete** - Soft or hard delete observations
- **ancora_stats** - Memory system statistics
- **ancora_timeline** - Chronological context around observation
- **ancora_merge** - Consolidate project name variants

## Usage Examples

### Memory Workflow

```bash
# Agent saves a decision proactively (visibility auto-inferred as 'work')
syfra save "Migrated to bcrypt from SHA256" \
  "**What**: Replaced password hashing
**Why**: Security audit flagged weak hashing
**Where**: internal/auth/hash.go
**Learned**: bcrypt cost=12 is optimal for our load" \
  --type=decision --workspace=webapp --organization=mycompany

# Save personal memory (auto-inferred as 'personal')
syfra save "Health Goals 2026" "my health goals: lose 10 lbs, run 5k" --workspace=life

# Later: search for past decisions
syfra search "password hashing" --type=decision --workspace=webapp

# View timeline context around observation #127
syfra timeline 127 --before=5 --after=5
```

### Project Management

```bash
# List all projects
syfra projects list

# Consolidate duplicate project names (interactive)
syfra projects consolidate

# Preview all similar groups without applying
syfra projects consolidate --all --dry-run

# Prune empty projects
syfra projects prune
```

### HTTP API

```bash
# Start server on default port 7437
syfra serve

# Or custom port
syfra serve 8080

# Available endpoints:
# GET  /health
# POST /sessions, GET /sessions/recent
# POST /observations, GET /observations/recent
# GET  /search?q=query&workspace=&type=&visibility=&organization=&limit=
# GET  /timeline?observation_id=&before=&after=
# GET  /context?workspace=&visibility=
# POST /observations/passive
# GET  /export, POST /import
# GET  /stats
# POST /projects/migrate
```

## Configuration

### Environment Variables

```bash
ANCORA_DATA_DIR=/path/to/data      # Override data directory (default: ~/.ancora)
ANCORA_PORT=8080                   # Override HTTP server port (default: 7437)
ANCORA_PROJECT=myproject           # Override auto-detected workspace for MCP (backward compat)
ANCORA_WORKSPACE=myworkspace       # Override auto-detected workspace for MCP
ANCORA_ORGANIZATION=mycompany      # Set organization for work observations
ANCORA_EMBED_MODEL=/path/model     # Path to GGUF embedding model
```

### Data Directory

```
~/.ancora/
  ancora.db          # SQLite database (FTS5 + vector storage)
  models/            # Optional: embedding models
    nomic-embed-text-v1.5.Q4_K_M.gguf
```

### Database Schema

**Tables:**
- `sessions` - Coding session tracking
- `observations` - Memory records (decisions, bugs, discoveries)
- `observations_fts` - FTS5 virtual table for keyword search
- `observations_vec` - Vector embeddings (768-dim float32)
- `prompts` - User prompt history
- `sync_state`, `sync_mutations`, `enrolled_projects` - Future cloud sync

**Observation Types:**
`tool_use`, `file_change`, `command`, `file_read`, `search`, `manual`, `decision`, `architecture`, `bugfix`, `pattern`, `config`, `discovery`, `learning`, `preference`, `session_summary`

**Visibility Levels:**
- `work` - Professional/work knowledge (default, auto-inferred). Can sync to Syfra Cloud if organization enrolled.
- `personal` - Private life knowledge (health, finances, goals). NEVER synced automatically. Auto-inferred from triggers like "my health", "my finances", "personal", "private", etc.

## Testing

```bash
# Run all tests
cd ancora
go test ./...

# Test coverage: 621 tests across 10 packages
# Status: ALL PASSING
```

Tested packages:
- cmd/syfra (CLI commands)
- internal/store (CRUD, FTS5, vector, deduplication)
- internal/mcp (tool handlers, profiles)
- internal/search (hybrid RRF fusion)
- internal/embed (embedding pipeline)
- internal/server (HTTP API)
- internal/tui (Bubbletea UI)
- internal/project (detection, similarity)
- internal/setup (agent installation)
- internal/version (update checks)

## Agent Integrations

### Claude Code

```bash
syfra setup claude-code
```

Installs:
- Plugin via marketplace: `claude plugin install ancora`
- MCP config: `~/.claude/mcp/ancora.json`
- Skills: `ancora-memory` (ALWAYS ACTIVE protocol)
- Hooks: session tracking, compaction recovery, user prompt capture

### OpenCode

```bash
syfra setup opencode
```

Installs:
- Plugin: `~/.config/opencode/plugins/ancora.ts`
- MCP registration in `opencode.json`
- Session tracking and compaction recovery hooks

## Search Implementation

### Keyword Search (FTS5)
- BM25-like ranking via SQLite FTS5
- Boolean operators supported
- Special character sanitization

### Semantic Search
- 768-dim float32 embeddings (nomic-embed-text)
- Cosine similarity scoring
- Optional: falls back to keyword-only if unavailable

### Hybrid Fusion (RRF)
```
score(doc) = Σ [1/(60 + rank_keyword)] + [1/(60 + rank_semantic)]
```
- k=60 (standard RRF constant)
- Results sorted by combined score
- Graceful degradation to single mode if needed

## Workspace Detection

**Priority:**
1. `--workspace=name` flag (or `--project` for backward compatibility)
2. `ANCORA_WORKSPACE` or `ANCORA_PROJECT` env var
3. Git remote origin URL (extracts repo name)
4. Git root directory basename
5. Current directory basename
6. Fallback: "unknown"

**Normalization:** Always lowercase, trimmed

## Visibility Auto-Detection

When `visibility` is omitted, Ancora automatically infers it from the title and content:

**Personal triggers** (classified as `visibility=personal`):
- "my goals", "my health", "my weight", "my finances", "my salary", "my budget"
- "personal", "private", "family", "home", "vacation", "medical"
- "body measurement", "blood pressure", "doctor visit", "bank account", etc.

**Default**: If no personal triggers found, defaults to `work`

**Override**: Explicitly set `--visibility=work` or `--visibility=personal` to override auto-detection

## Deduplication

**Strategy:**
- 15-minute window for exact duplicates
- SHA256 hash of (title + content + type)
- Increments `duplicate_count`, updates `last_seen_at`
- Topic key upserts: same `topic_key` updates existing observation

## Storage Limits

- Max observation content: 50,000 chars (truncated with warning)
- Max context results: 20 observations
- Max search results: 20 observations
- Deduplication window: 15 minutes

## Dependencies

```go
require (
    github.com/charmbracelet/bubbles v1.0.0       // TUI widgets
    github.com/charmbracelet/bubbletea v1.3.10    // TUI framework
    github.com/charmbracelet/lipgloss v1.1.0      // TUI styling
    github.com/mark3labs/mcp-go v0.44.0           // MCP server
    modernc.org/sqlite v1.45.0                    // Pure Go SQLite (zero CGO)
)
```

Go version: 1.25.0

## Distribution

Ancora is distributed as pre-built binaries for maximum portability:

- **GitHub Releases** - Automatic multi-platform builds on every tag
- **Homebrew Tap** - `brew install Syfra3/tap/ancora`
- **Install Script** - One-command installation for all platforms
- **No source code needed** - Single binary with zero dependencies

### Creating a Release

```bash
# 1. Tag the release
git tag -a v1.0.0 -m "Release v1.0.0"

# 2. Run release automation
cd ancora
make release

# This will:
# - Run tests and linting
# - Push tag to GitHub
# - Trigger GitHub Actions to build binaries
# - Create GitHub release with all artifacts
# - Update Homebrew formula
```

## Roadmap

- [ ] Sync engine abstraction (local and proprietary cloud backends)
- [ ] Web dashboard
- [ ] Multi-model embedding support
- [ ] Advanced analytics and insights
- [ ] Team collaboration features

## Support

For questions or issues, contact: support@syfra.co

## License

MIT License - See [LICENSE](./LICENSE) file for details.

**Ancora Core** is free and open source software. Premium features (cloud sync, enterprise support) are available separately.
