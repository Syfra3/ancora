# Manual Release Creation for v1.0.0

Since API access requires write permissions, create the release manually:

## Step 1: Grant Write Access (if you own Syfra3 org)

1. Go to: https://github.com/orgs/Syfra3/people
2. Find your G33N-2 account
3. Grant **Write** access to the `ancora` repository

## Step 2: Create the Release on GitHub

1. Go to: https://github.com/Syfra3/ancora/releases/new?tag=v1.0.0

2. Fill in:
   - **Tag**: `v1.0.0` (already exists)
   - **Release title**: `v1.0.0`
   - **Description**: (paste from below)

### Release Description:

```markdown
## Ancora v1.0.0 - Initial Release

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

See the [README](https://github.com/Syfra3/ancora) for full documentation.
```

## Step 3: Upload Binaries

Drag and drop these files from `dist/` folder:

- `ancora-1.0.0-darwin-amd64.tar.gz` (5.3M)
- `ancora-1.0.0-darwin-arm64.tar.gz` (4.9M)
- `ancora-1.0.0-linux-amd64.tar.gz` (5.2M)
- `ancora-1.0.0-linux-arm64.tar.gz` (4.7M)

## Step 4: Update Homebrew Formula

After the release is published, run:

```bash
cd ~/Documents/personal/syfra/homebrew-tap
./update-formula.sh 1.0.0
git add Formula/ancora.rb
git commit -m "chore: update ancora formula to v1.0.0 with SHA256 checksums"
git push origin main
```

## SHA256 Checksums (for reference):

```
darwin-amd64: 80f3f8cf649a9d52a458874984ae525955c74e124c1fbc4db8f69fad1ab513bf
darwin-arm64: 4fe55e282aba1ae9d470d3614e865df165df394bd73b827a0dad6b9f7449357f
linux-amd64:  761f91594a2e997e072b0632e0c398908df2ca03b44e09db4b8fad62c306efc8
linux-arm64:  b2095f32d37a2c1d99992e05c859e45095fcc8ef6972c2b52c67a2104ce344cf
```
