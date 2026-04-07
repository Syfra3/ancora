#!/usr/bin/env bash
# Ancora Claude Code Setup Script
# Installs ancora CLI, registers marketplace, installs plugin, and configures MCP

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}==>${NC} $1"
}

log_success() {
    echo -e "${GREEN}✓${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}!${NC} $1"
}

log_error() {
    echo -e "${RED}✗${NC} $1"
}

# Check if running from ancora repository
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

if [ ! -f "${REPO_ROOT}/.claude-plugin/marketplace.json" ]; then
    log_error "This script must be run from the ancora repository"
    exit 1
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo -e "${BLUE}Ancora Claude Code Setup${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Step 1: Check if ancora CLI is installed
log_info "Checking for ancora CLI..."
if ! command -v ancora &> /dev/null; then
    log_warn "ancora CLI not found. Please install it first:"
    log_info "  curl -sSL https://raw.githubusercontent.com/Syfra3/ancora/main/scripts/install-ancora.sh | bash"
    exit 1
fi
log_success "ancora CLI found: $(ancora --version | head -n1)"

# Step 2: Check if claude is installed
log_info "Checking for Claude Code CLI..."
if ! command -v claude &> /dev/null; then
    log_error "Claude Code CLI not found. Please install Claude Code first."
    exit 1
fi
log_success "Claude Code CLI found"

# Step 3: Add ancora marketplace (if not already added)
log_info "Checking ancora marketplace registration..."
if claude plugin marketplace list 2>&1 | grep -q "ancora"; then
    log_success "Ancora marketplace already registered"
else
    log_info "Adding ancora marketplace from: ${REPO_ROOT}"
    if claude plugin marketplace add "${REPO_ROOT}"; then
        log_success "Ancora marketplace registered"
    else
        log_error "Failed to register ancora marketplace"
        exit 1
    fi
fi

# Step 4: Install ancora plugin (if not already installed)
log_info "Checking ancora plugin installation..."
PLUGIN_INSTALLED=$(claude plugin list 2>&1 | grep -c "ancora" || true)
if [ "$PLUGIN_INSTALLED" -gt 0 ]; then
    log_success "Ancora plugin already installed"
else
    log_info "Installing ancora plugin..."
    if claude plugin install ancora; then
        log_success "Ancora plugin installed"
    else
        log_error "Failed to install ancora plugin"
        exit 1
    fi
fi

# Step 5: Add ancora MCP server (if not already configured)
log_info "Checking ancora MCP server configuration..."
if claude mcp list 2>&1 | grep -q "ancora"; then
    log_success "Ancora MCP server already configured"
else
    log_info "Configuring ancora MCP server..."
    if claude mcp add ancora ancora -- mcp --tools=agent; then
        log_success "Ancora MCP server configured"
    else
        log_error "Failed to configure ancora MCP server"
        exit 1
    fi
fi

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo -e "${GREEN}Ancora Claude Code setup complete!${NC}"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "The ancora plugin is now installed and will:"
echo "  - Auto-inject memory protocol on session start"
echo "  - Load memory context automatically"
echo "  - Recover context after compaction"
echo "  - Capture session summaries on stop"
echo ""
echo "Start a new Claude Code session to activate ancora memory."
echo ""
