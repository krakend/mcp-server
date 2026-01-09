#!/bin/bash
##
## install.sh - Install KrakenD MCP Server (standalone)
##
## Usage:
##   curl -sSL https://raw.githubusercontent.com/krakend/mcp-server/main/scripts/install.sh | bash
##
##   Or download and run:
##   ./install.sh              # Normal mode
##   ./install.sh --verbose    # Verbose mode
##

set -e

# Latest version (update on each release)
VERSION="0.6.2"

# Installation directory (to be determined by determine_install_dir)
INSTALL_DIR=""
DATA_DIR="${HOME}/.krakend-mcp"

# Flag to show PATH instructions if needed
NEEDS_PATH_REMINDER=0

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Verbose mode
VERBOSE=0
if [[ "$1" == "-v" || "$1" == "--verbose" ]]; then
    VERBOSE=1
fi

# Logging functions
log() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_verbose() {
    if [ $VERBOSE -eq 1 ]; then
        echo -e "${BLUE}[DEBUG]${NC} $1"
    fi
}

log_success() {
    echo -e "${GREEN}[✓]${NC} $1"
}

log_error() {
    echo -e "${RED}[✗]${NC} $1" >&2
}

log_warning() {
    echo -e "${YELLOW}[!]${NC} $1"
}

# Detect platform
detect_platform() {
    local os arch

    os=$(uname -s | tr '[:upper:]' '[:lower:]')
    arch=$(uname -m)

    log_verbose "Detected: OS=$os, ARCH=$arch"

    # Normalize OS
    case "$os" in
        darwin)
            OS_NAME="darwin"
            ;;
        linux)
            OS_NAME="linux"
            ;;
        mingw*|msys*|cygwin*|windows*)
            OS_NAME="windows"
            ;;
        *)
            log_error "Unsupported OS: $os"
            log_error "Supported: macOS, Linux, Windows"
            exit 1
            ;;
    esac

    # Normalize architecture
    case "$arch" in
        x86_64|amd64)
            ARCH_NAME="amd64"
            ;;
        arm64|aarch64)
            ARCH_NAME="arm64"
            ;;
        *)
            log_error "Unsupported architecture: $arch"
            log_error "Supported: x86_64 (amd64), arm64 (aarch64)"
            exit 1
            ;;
    esac

    # Binary filename
    BINARY_NAME="krakend-mcp-${OS_NAME}-${ARCH_NAME}"
    if [ "$OS_NAME" = "windows" ]; then
        BINARY_NAME="${BINARY_NAME}.exe"
    fi

    log "Platform: ${OS_NAME}-${ARCH_NAME}"
    log_verbose "Binary name: $BINARY_NAME"
}

# Determine installation directory
determine_install_dir() {
    # Preferred: /usr/local/bin (system-wide, traditional location)
    # Works on: macOS, Linux, WSL
    if [ -d "/usr/local/bin" ] && [ -w "/usr/local/bin" ]; then
        INSTALL_DIR="/usr/local/bin"
        log "Using system installation directory: $INSTALL_DIR"
        return
    fi

    # Fallback: ~/.local/bin (XDG standard, user-local)
    # Works on: macOS, Linux, WSL, Git Bash, MSYS2, Cygwin
    INSTALL_DIR="$HOME/.local/bin"
    log "Using user installation directory: $INSTALL_DIR"

    # Create directory if it doesn't exist
    if [ ! -d "$INSTALL_DIR" ]; then
        log "Creating directory $INSTALL_DIR..."
        mkdir -p "$INSTALL_DIR"
        NEEDS_PATH_REMINDER=1
    fi

    # Check if directory is in PATH
    if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
        NEEDS_PATH_REMINDER=1
    fi
}

# Download binary
download_binary() {
    local url="https://github.com/krakend/mcp-server/releases/download/v${VERSION}/${BINARY_NAME}"
    local checksums_url="https://github.com/krakend/mcp-server/releases/download/v${VERSION}/checksums.txt"
    local tmp_binary="/tmp/${BINARY_NAME}"
    local tmp_checksums="/tmp/checksums.txt"

    log "Downloading KrakenD MCP Server v${VERSION}..."
    log_verbose "URL: $url"

    if [ $VERBOSE -eq 1 ]; then
        curl -L --fail --progress-bar -o "$tmp_binary" "$url"
    else
        curl -L --fail --silent --show-error -o "$tmp_binary" "$url"
    fi

    if [ ! -f "$tmp_binary" ]; then
        log_error "Download failed"
        exit 1
    fi

    log_success "Binary downloaded"

    # Download checksums
    log "Downloading checksums..."
    curl -L --fail --silent --show-error -o "$tmp_checksums" "$checksums_url" || {
        log_warning "Could not download checksums, skipping verification"
        return
    }

    # Verify checksum
    log "Verifying checksum..."
    log_verbose "Checksums file: $tmp_checksums"

    if command -v sha256sum >/dev/null 2>&1; then
        expected=$(grep "$BINARY_NAME" "$tmp_checksums" | awk '{print $1}')
        actual=$(sha256sum "$tmp_binary" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        expected=$(grep "$BINARY_NAME" "$tmp_checksums" | awk '{print $1}')
        actual=$(shasum -a 256 "$tmp_binary" | awk '{print $1}')
    else
        log_warning "sha256sum not found, skipping checksum verification"
        return
    fi

    if [ "$expected" = "$actual" ]; then
        log_success "Checksum verified"
    else
        log_error "Checksum mismatch!"
        log_error "Expected: $expected"
        log_error "Actual:   $actual"
        rm -f "$tmp_binary"
        exit 1
    fi

    rm -f "$tmp_checksums"
}

# Install binary
install_binary() {
    local tmp_binary="/tmp/${BINARY_NAME}"
    local target="${INSTALL_DIR}/krakend-mcp-server"

    log "Installing to $target..."

    mv "$tmp_binary" "$target"
    chmod +x "$target"

    log_success "Binary installed"
}

# Setup data directory
setup_data_dir() {
    log "Setting up data directory at $DATA_DIR..."

    mkdir -p "$DATA_DIR"

    # Create subdirectories for documentation cache
    mkdir -p "$DATA_DIR/docs"
    mkdir -p "$DATA_DIR/search"

    log_success "Data directory created"
    log "Documentation will be downloaded on first use"
}

# Test installation
test_installation() {
    log "Testing installation..."

    local version_output
    version_output=$("${INSTALL_DIR}/krakend-mcp-server" --version 2>&1)

    if echo "$version_output" | grep -q "version $VERSION"; then
        log_success "Installation successful!"
        log "Version: $VERSION"
    else
        log_error "Installation test failed"
        log_error "Output: $version_output"
        exit 1
    fi
}

# Main
main() {
    echo ""
    log "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    log "KrakenD MCP Server Installer v${VERSION}"
    log "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""

    detect_platform
    determine_install_dir
    download_binary
    install_binary
    setup_data_dir
    test_installation

    echo ""
    log "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    log_success "Installation Complete!"
    log "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    log "Installed: ${INSTALL_DIR}/krakend-mcp-server"
    log "Data directory: ${DATA_DIR}"
    echo ""

    # Show PATH instructions if needed
    if [ $NEEDS_PATH_REMINDER -eq 1 ]; then
        log_warning "⚠️  Add $INSTALL_DIR to your PATH:"
        echo ""
        log "  For bash:"
        echo "    echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.bash_profile"
        echo ""
        log "  For zsh:"
        echo "    echo 'export PATH=\"\$HOME/.local/bin:\$PATH\"' >> ~/.zshrc"
        echo ""
        log "  Then restart your shell or run:"
        echo "    source ~/.bash_profile  (for bash)"
        echo "    source ~/.zshrc         (for zsh)"
        echo ""
    fi

    log "Next steps:"
    if [ $NEEDS_PATH_REMINDER -eq 1 ]; then
        echo "  1. Add $INSTALL_DIR to your PATH (see instructions above)"
        echo "  2. Configure your MCP client (see README)"
        echo "  3. Run: krakend-mcp-server --version"
    else
        echo "  1. Configure your MCP client (see README)"
        echo "  2. Run: krakend-mcp-server --version"
    fi
    echo ""
    log "Documentation: https://github.com/krakend/mcp-server"
    echo ""
}

main "$@"
