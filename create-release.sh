#!/bin/bash
set -e

VERSION="1.0.0"
TAG="v${VERSION}"
REPO="Syfra3/ancora"

# Create release notes
NOTES="## Ancora v1.0.0 - Initial Release

Ancora is a persistent memory system for AI coding agents, providing hybrid search capabilities and seamless integration with Claude Desktop and Claude Code.

### Features

- **Hybrid Search**: FTS5 keyword search + semantic vector embeddings using nomic-embed-text-v1.5
- **MCP Server Integration**: Native support for Claude Desktop and Claude Code via MCP protocol
- **TUI Interface**: Browse observations, sessions, and projects with intuitive keyboard navigation
- **Project & Personal Scopes**: Separate work knowledge from personal data
- **Sync Enrollment**: Track which projects are enrolled in Syfra Cloud sync
- **Doctor Command**: System diagnostics for embedding model, database, and MCP configuration
- **Setup Wizard**: Interactive TUI for installing embedding models and plugins
- **Unified Visual Style**: Syfra brand colors (Lavender to Mint gradient) across all TUI screens

### Installation

\`\`\`bash
# Via Homebrew (macOS/Linux)
brew tap Syfra3/tap
brew install ancora

# Or download binaries from this release
\`\`\`

### Platform Support

- Linux: amd64, arm64
- macOS: Intel (amd64), Apple Silicon (arm64)

### Getting Started

\`\`\`bash
# Run setup wizard
ancora setup

# Start the TUI
ancora

# Start MCP server for Claude Code (automatic)
# For Claude Desktop, add to config
ancora mcp
\`\`\`

See the [README](https://github.com/Syfra3/ancora) for full documentation."

echo "Creating release ${TAG}..."
gh release create "$TAG" \
  --repo "$REPO" \
  --title "v${VERSION}" \
  --notes "$NOTES" \
  dist/ancora-${VERSION}-*.tar.gz

echo "✓ Release created: https://github.com/${REPO}/releases/tag/${TAG}"
