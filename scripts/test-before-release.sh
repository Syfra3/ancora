#!/bin/bash
# Test script to run before releasing Ancora changes
# Usage: ./scripts/test-before-release.sh

set -e

echo "═══════════════════════════════════════════════════"
echo "  Ancora Pre-Release Testing"
echo "═══════════════════════════════════════════════════"
echo ""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Step counter
STEP=1

run_step() {
    echo ""
    echo "${GREEN}[$STEP]${NC} $1"
    ((STEP++))
}

fail() {
    echo "${RED}✗ FAILED:${NC} $1"
    exit 1
}

success() {
    echo "${GREEN}✓ PASSED:${NC} $1"
}

# 1. Run unit tests
run_step "Running unit tests"
if ! make test > /tmp/ancora-test.log 2>&1; then
    cat /tmp/ancora-test.log
    fail "Unit tests failed"
fi
success "All unit tests passed"

# 2. Build dev binary
run_step "Building development binary"
if ! make dev > /tmp/ancora-build.log 2>&1; then
    cat /tmp/ancora-build.log
    fail "Build failed"
fi
success "Binary built successfully"

# 3. Test binary works
run_step "Testing binary execution"
if ! ./bin/ancora --version > /dev/null 2>&1; then
    fail "Binary doesn't execute"
fi
success "Binary executes correctly"

# 4. Test MCP server starts
run_step "Testing MCP server startup"
if ! timeout 2 ./bin/ancora mcp > /tmp/ancora-mcp.log 2>&1; then
    # timeout exit code 124 is expected (we killed it)
    if [ $? -ne 124 ]; then
        cat /tmp/ancora-mcp.log
        fail "MCP server failed to start"
    fi
fi
if ! grep -q "hybrid search enabled" /tmp/ancora-mcp.log; then
    cat /tmp/ancora-mcp.log
    fail "MCP server didn't initialize properly"
fi
success "MCP server starts successfully"

# 5. Test TUI-specific tests
run_step "Testing TUI components"
if ! go test -v ./internal/tui -run "TestMCPStatusCheck" > /tmp/ancora-tui.log 2>&1; then
    cat /tmp/ancora-tui.log
    fail "TUI tests failed"
fi
success "TUI tests passed"

# 6. Test setup injection (modified code)
run_step "Testing setup MCP injection"
if ! go test -v ./internal/setup -run "TestInjectOpenCodeMCP" > /tmp/ancora-setup.log 2>&1; then
    cat /tmp/ancora-setup.log
    fail "Setup injection tests failed"
fi
success "Setup injection tests passed"

# 7. Check for common issues
run_step "Checking for common issues"

# Check if binary size is reasonable (< 50MB)
BINARY_SIZE=$(stat -f%z ./bin/ancora 2>/dev/null || stat -c%s ./bin/ancora 2>/dev/null)
if [ "$BINARY_SIZE" -gt 52428800 ]; then
    echo "${YELLOW}⚠ WARNING:${NC} Binary size is large: $(numfmt --to=iec $BINARY_SIZE)"
fi

# Check if coverage is reasonable (> 60%)
if command -v go &> /dev/null; then
    COVERAGE=$(go test -cover ./... 2>/dev/null | grep -oP 'coverage: \K[0-9.]+' | head -1 || echo "0")
    if (( $(echo "$COVERAGE < 60" | bc -l) )); then
        echo "${YELLOW}⚠ WARNING:${NC} Test coverage is low: ${COVERAGE}%"
    fi
fi

success "No critical issues detected"

# Summary
echo ""
echo "═══════════════════════════════════════════════════"
echo "${GREEN}✓ All pre-release tests passed!${NC}"
echo "═══════════════════════════════════════════════════"
echo ""
echo "Next steps:"
echo "  1. Review CHANGELOG.md"
echo "  2. Update version in cmd/ancora/version.go if needed"
echo "  3. Commit changes: git commit -am 'fix: description'"
echo "  4. Tag release: git tag v1.x.x"
echo "  5. Push: git push origin main --tags"
echo ""
