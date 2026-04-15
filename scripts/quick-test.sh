#!/bin/bash
# Quick test script for Ancora changes
# Usage: ./scripts/quick-test.sh

set -e

echo "🧪 Quick Ancora Testing"
echo ""

# 1. Build
echo "[1/5] Building..."
make dev > /dev/null 2>&1
echo "✓ Build successful"

# 2. Test binary works
echo "[2/5] Testing binary..."
./bin/ancora --version > /dev/null 2>&1
echo "✓ Binary works"

# 3. Test MCP starts
echo "[3/5] Testing MCP server..."
timeout 2 ./bin/ancora mcp > /tmp/mcp-test.log 2>&1 || true
if grep -q "hybrid search enabled" /tmp/mcp-test.log; then
    echo "✓ MCP server starts"
else
    echo "✗ MCP server failed"
    cat /tmp/mcp-test.log
    exit 1
fi

# 4. Test modified packages
echo "[4/5] Testing TUI..."
go test ./internal/tui -run "TestMCPStatusCheck" > /dev/null 2>&1
echo "✓ TUI tests pass"

echo "[5/5] Testing setup..."
go test ./internal/setup -run "TestInjectOpenCodeMCP" > /dev/null 2>&1
echo "✓ Setup tests pass"

echo ""
echo "✅ All quick tests passed!"
echo ""
echo "For full test suite: make test"
