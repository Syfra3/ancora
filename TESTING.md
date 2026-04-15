# Testing Ancora Changes Before Release

## Quick Testing Workflow

### 1. Run Unit Tests

```bash
# Run all tests
make test

# Run specific package tests
go test -v ./internal/tui
go test -v ./internal/setup
go test -v ./internal/mcp

# Run with coverage
make test-coverage
# Opens coverage.html in browser
```

### 2. Build & Test Locally

```bash
# Build without installing (for quick iteration)
make dev
# Binary at: bin/ancora

# Test the binary directly
./bin/ancora --version
./bin/ancora mcp --help

# Test MCP server starts
./bin/ancora mcp &
# Should see: [ancora] hybrid search enabled...
pkill -f "ancora mcp"
```

### 3. Install to GOPATH (Isolated Test)

```bash
# Install to ~/go/bin (doesn't affect Homebrew)
make install

# Verify it's using the new binary
~/go/bin/ancora --version

# Test MCP config injection
~/go/bin/ancora setup

# Check the path it wrote
cat ~/.config/opencode/opencode.json | grep -A 3 "ancora"
```

### 4. Install to Homebrew Location (Production Test)

**IMPORTANT**: This replaces your active Homebrew installation!

```bash
# Build production binary
make build

# Backup current Homebrew binary
sudo cp /home/linuxbrew/.linuxbrew/bin/ancora /home/linuxbrew/.linuxbrew/bin/ancora.backup

# Copy new binary (requires sudo on Homebrew)
sudo cp bin/ancora /home/linuxbrew/.linuxbrew/bin/ancora

# Test it works
ancora --version
ancora mcp --help

# Test TUI with MCP status
ancora

# Restore backup if needed
sudo mv /home/linuxbrew/.linuxbrew/bin/ancora.backup /home/linuxbrew/.linuxbrew/bin/ancora
```

### 5. Test OpenCode Integration

```bash
# Start MCP server manually
ancora mcp &

# Check it's running
pgrep -f "ancora mcp"

# Restart OpenCode to pick up config changes
# Then test MCP tools are available:
# - Try using ancora_search in OpenCode
# - Check for errors in OpenCode logs
```

### 6. Test All Agent Integrations

```bash
# Test setup for all agents
ancora setup

# Check injected configs:
cat ~/.config/opencode/opencode.json | jq '.mcp.ancora'
cat ~/.gemini/settings.json | jq '.mcpServers.ancora'
cat ~/.codex/config.toml | grep -A 3 "mcp_servers.ancora"
cat ~/.claude/mcp/ancora.json
```

## Regression Testing Checklist

Before every release, verify:

- [ ] `make test` passes (all unit tests)
- [ ] `make lint` passes (no linting errors)
- [ ] MCP server starts without errors
- [ ] TUI shows correct status (both server & MCP)
- [ ] `ancora setup` updates configs with correct path
- [ ] OpenCode can connect to MCP server
- [ ] Gemini CLI can connect to MCP server (if installed)
- [ ] Codex can connect to MCP server (if installed)
- [ ] All ancora_* tools work in OpenCode
- [ ] Version output is correct

## Test Scenarios for This Release

### Scenario 1: Fresh Install
```bash
# Simulate user installing for first time
rm -rf ~/.config/opencode/opencode.json
ancora setup
# Verify: config has correct path
```

### Scenario 2: Stale Path Update
```bash
# Simulate user with old wrong path
# Manually edit config to wrong path
ancora setup
# Verify: path gets updated to correct one
```

### Scenario 3: TUI MCP Status
```bash
# Test with MCP running
ancora mcp &
ancora  # Should show "MCP Status: ● operational"

# Test with MCP stopped
pkill -f "ancora mcp"
ancora  # Should show "MCP Status: ● offline"
```

## CI/CD Testing (Optional)

If you have GitHub Actions set up:

```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.22'
      - run: make test
      - run: make build
```

## Release Checklist

Before tagging a release:

1. ✅ All tests pass
2. ✅ Manual testing complete
3. ✅ CHANGELOG.md updated
4. ✅ Version bumped in version.go
5. ✅ Git commit + tag
6. ✅ Test installation from release binary

```bash
# Create release
git commit -am "fix: update MCP config path on setup, add TUI status"
git tag v1.1.1
git push origin main --tags

# Let goreleaser handle the rest
# Or manually: make cross && upload to GitHub releases
```
