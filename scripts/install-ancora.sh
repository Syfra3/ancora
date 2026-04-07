#!/usr/bin/env bash
set -e

# Ancora Universal Installer
# Usage: curl -sSL https://raw.githubusercontent.com/Syfra3/ancora/main/scripts/install-ancora.sh | bash

VERSION="${ANCORA_VERSION:-latest}"
INSTALL_DIR="${ANCORA_INSTALL_DIR:-/usr/local/bin}"
REPO="Syfra3/ancora"
BINARY_NAME="ancora"

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

detect_platform() {
    local os=""
    local arch=""
    
    # Detect OS
    case "$(uname -s)" in
        Linux*)     os="linux";;
        Darwin*)    os="darwin";;
        MINGW*|MSYS*|CYGWIN*) os="windows";;
        *)
            log_error "Unsupported operating system: $(uname -s)"
            exit 1
            ;;
    esac
    
    # Detect architecture
    case "$(uname -m)" in
        x86_64|amd64)   arch="amd64";;
        aarch64|arm64)  arch="arm64";;
        *)
            log_error "Unsupported architecture: $(uname -m)"
            exit 1
            ;;
    esac
    
    echo "${os}-${arch}"
}

get_latest_version() {
    log_info "Fetching latest version..."
    local latest
    # Get the latest release (tag format: vX.Y.Z)
    latest=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
    
    if [ -z "$latest" ]; then
        log_error "Failed to fetch latest release"
        exit 1
    fi
    
    echo "$latest"
}

download_and_install() {
    local version="$1"
    local platform="$2"
    local tmpdir
    tmpdir=$(mktemp -d)
    
    # Extract version number from tag (v1.0.0 -> 1.0.0)
    local version_number="${version#v}"
    local filename="${BINARY_NAME}-${version_number}-${platform}.tar.gz"
    local url="https://github.com/${REPO}/releases/download/${version}/${filename}"
    
    log_info "Downloading ${BINARY_NAME} ${version} for ${platform}..."
    log_info "URL: ${url}"
    
    if ! curl -L -o "${tmpdir}/${filename}" "${url}"; then
        log_error "Download failed"
        rm -rf "$tmpdir"
        exit 1
    fi
    
    log_info "Extracting archive..."
    if ! tar -xzf "${tmpdir}/${filename}" -C "$tmpdir"; then
        log_error "Extraction failed"
        rm -rf "$tmpdir"
        exit 1
    fi
    
    # Handle Windows .exe extension
    local binary_file="${BINARY_NAME}"
    if [ "$platform" = "windows-amd64" ]; then
        binary_file="${BINARY_NAME}.exe"
    fi
    
    if [ ! -f "${tmpdir}/${binary_file}" ]; then
        log_error "Binary not found in archive"
        rm -rf "$tmpdir"
        exit 1
    fi
    
    log_info "Installing to ${INSTALL_DIR}..."
    
    # Create install directory if it doesn't exist
    if [ ! -d "$INSTALL_DIR" ]; then
        log_warn "Install directory doesn't exist. Attempting to create: ${INSTALL_DIR}"
        if ! mkdir -p "$INSTALL_DIR" 2>/dev/null; then
            log_error "Failed to create ${INSTALL_DIR}. Try running with sudo or set ANCORA_INSTALL_DIR to a writable location."
            log_info "Example: export ANCORA_INSTALL_DIR=\$HOME/.local/bin"
            rm -rf "$tmpdir"
            exit 1
        fi
    fi
    
    # Try to install
    if ! mv "${tmpdir}/${binary_file}" "${INSTALL_DIR}/${binary_file}" 2>/dev/null; then
        log_error "Permission denied. Try running with sudo or set ANCORA_INSTALL_DIR to a writable location."
        log_info "Example: export ANCORA_INSTALL_DIR=\$HOME/.local/bin"
        rm -rf "$tmpdir"
        exit 1
    fi
    
    chmod +x "${INSTALL_DIR}/${binary_file}"
    
    rm -rf "$tmpdir"
    
    log_success "${BINARY_NAME} ${version} installed successfully!"
}

verify_installation() {
    log_info "Verifying installation..."
    
    if ! command -v "${BINARY_NAME}" &> /dev/null; then
        log_warn "${BINARY_NAME} is installed but not in PATH"
        log_info "Add ${INSTALL_DIR} to your PATH:"
        log_info "  export PATH=\"${INSTALL_DIR}:\$PATH\""
        return
    fi
    
    local installed_version
    installed_version=$("${BINARY_NAME}" --version 2>&1 | head -n1 || echo "unknown")
    
    log_success "Installation verified: ${installed_version}"
}

print_next_steps() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo -e "${GREEN}Ancora installed successfully!${NC}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "Get started:"
    echo "  $ ancora --help"
    echo ""
    echo "Documentation: https://github.com/${REPO}"
    echo ""
}

main() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo -e "${BLUE}Ancora Installer${NC}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    
    # Check dependencies
    for cmd in curl tar; do
        if ! command -v "$cmd" &> /dev/null; then
            log_error "Required command not found: $cmd"
            exit 1
        fi
    done
    
    # Detect platform
    local platform
    platform=$(detect_platform)
    log_info "Detected platform: ${platform}"
    
    # Get version
    if [ "$VERSION" = "latest" ]; then
        VERSION=$(get_latest_version)
    fi
    log_info "Target version: ${VERSION}"
    
    # Download and install
    download_and_install "$VERSION" "$platform"
    
    # Verify
    verify_installation
    
    # Print next steps
    print_next_steps
}

main "$@"
