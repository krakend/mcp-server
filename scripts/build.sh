#!/bin/bash
##
## build.sh - Build KrakenD MCP Server with embedded documentation
##
## This script:
##   1. Downloads official KrakenD documentation
##   2. Indexes documentation with Bleve
##   3. Embeds docs + index into the binary
##   4. Builds cross-platform binaries
##
## Usage:
##   ./scripts/build.sh              # Build for current platform
##   ./scripts/build.sh --all        # Build for all platforms
##   ./scripts/build.sh --platform darwin-arm64  # Build specific platform
##

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m'

log() {
    echo -e "${BLUE}[INFO]${NC} $1"
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

# Directories
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
DOCS_DIR="$PROJECT_ROOT/tools/data/docs"
SEARCH_DIR="$PROJECT_ROOT/tools/data/search"
BUILD_DIR="$PROJECT_ROOT/build"

# Documentation URL
DOCS_URL="https://www.krakend.io/llms-full.txt"

# Version (read from main.go)
VERSION=$(grep 'version.*=' "$PROJECT_ROOT/main.go" | grep -o '[0-9.]*' | head -1)

echo ""
log "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
log "KrakenD MCP Server Build Script v$VERSION"
log "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""

# Parse arguments
BUILD_ALL=false
PLATFORM=""

for arg in "$@"; do
    case $arg in
        --all)
            BUILD_ALL=true
            ;;
        --platform=*)
            PLATFORM="${arg#*=}"
            ;;
        *)
            log_error "Unknown argument: $arg"
            echo "Usage: $0 [--all] [--platform=PLATFORM]"
            exit 1
            ;;
    esac
done

# Step 1: Prepare documentation
log "Step 1: Preparing documentation for embedding..."

mkdir -p "$DOCS_DIR"
mkdir -p "$SEARCH_DIR"

# Download documentation
log "Downloading documentation from $DOCS_URL..."

if command -v curl >/dev/null 2>&1; then
    curl -L --fail --silent --show-error -o "$DOCS_DIR/llms-full.txt" "$DOCS_URL"
elif command -v wget >/dev/null 2>&1; then
    wget -q -O "$DOCS_DIR/llms-full.txt" "$DOCS_URL"
else
    log_error "Neither curl nor wget found. Please install one of them."
    exit 1
fi

if [ ! -f "$DOCS_DIR/llms-full.txt" ]; then
    log_error "Failed to download documentation"
    exit 1
fi

DOC_SIZE=$(wc -c < "$DOCS_DIR/llms-full.txt" | tr -d ' ')
log_success "Documentation downloaded ($DOC_SIZE bytes)"

# Create cache metadata
cat > "$DOCS_DIR/cache.meta" <<EOF
{
  "downloaded_at": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "source_url": "$DOCS_URL",
  "size_bytes": $DOC_SIZE,
  "embedded": true
}
EOF

# Step 2: Index documentation
log "Step 2: Indexing documentation..."

# Build temporary indexer
mkdir -p "$PROJECT_ROOT/cmd/indexer"
cat > "$PROJECT_ROOT/cmd/indexer/main.go" <<'INDEXER_EOF'
package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/blevesearch/bleve/v2"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <docs-file> <index-dir>\n", os.Args[0])
		os.Exit(1)
	}

	docsFile := os.Args[1]
	indexDir := os.Args[2]

	// Remove existing index
	os.RemoveAll(indexDir)

	// Create index
	mapping := bleve.NewIndexMapping()
	index, err := bleve.New(indexDir, mapping)
	if err != nil {
		log.Fatalf("Failed to create index: %v", err)
	}
	defer index.Close()

	// Read and index
	file, err := os.Open(docsFile)
	if err != nil {
		log.Fatalf("Failed to open file: %v", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	var chunk strings.Builder
	chunkID := 0
	lineCount := 0

	for scanner.Scan() {
		chunk.WriteString(scanner.Text())
		chunk.WriteString("\n")
		lineCount++

		if lineCount >= 500 {
			doc := map[string]interface{}{
				"id":      fmt.Sprintf("chunk_%d", chunkID),
				"content": chunk.String(),
			}
			index.Index(doc["id"].(string), doc)
			chunk.Reset()
			lineCount = 0
			chunkID++
		}
	}

	if chunk.Len() > 0 {
		doc := map[string]interface{}{
			"id":      fmt.Sprintf("chunk_%d", chunkID),
			"content": chunk.String(),
		}
		index.Index(doc["id"].(string), doc)
		chunkID++
	}

	log.Printf("Indexed %d chunks", chunkID)
}
INDEXER_EOF

mkdir -p "$PROJECT_ROOT/cmd/indexer"
cd "$PROJECT_ROOT"
go build -o "$PROJECT_ROOT/cmd/indexer/indexer" "$PROJECT_ROOT/cmd/indexer/main.go" >/dev/null 2>&1

"$PROJECT_ROOT/cmd/indexer/indexer" "$DOCS_DIR/llms-full.txt" "$SEARCH_DIR/index"
# Give Bleve time to finish async writes
sleep 2
rm -rf "$PROJECT_ROOT/cmd/indexer"

log_success "Documentation indexed"

# Step 3: Build binary
log "Step 3: Building binary..."

mkdir -p "$BUILD_DIR"

build_for_platform() {
    local os=$1
    local arch=$2
    local output_name="krakend-mcp-${os}-${arch}"

    if [ "$os" = "windows" ]; then
        output_name="${output_name}.exe"
    fi

    log "Building $output_name..."

    GOOS=$os GOARCH=$arch go build \
        -ldflags "-s -w" \
        -o "$BUILD_DIR/$output_name" \
        "$PROJECT_ROOT" 2>&1

    if [ -f "$BUILD_DIR/$output_name" ]; then
        local size=$(du -h "$BUILD_DIR/$output_name" | cut -f1)
        log_success "Built $output_name ($size)"
    else
        log_error "Failed to build $output_name"
        return 1
    fi
}

if [ "$BUILD_ALL" = true ]; then
    log "Building for all platforms..."
    build_for_platform "darwin" "amd64"
    build_for_platform "darwin" "arm64"
    build_for_platform "linux" "amd64"
    build_for_platform "linux" "arm64"
    build_for_platform "windows" "amd64"
elif [ -n "$PLATFORM" ]; then
    IFS='-' read -r os arch <<< "$PLATFORM"
    build_for_platform "$os" "$arch"
else
    # Build for current platform
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$ARCH" in
        x86_64) ARCH="amd64" ;;
        aarch64) ARCH="arm64" ;;
    esac

    case "$OS" in
        darwin|linux)
            build_for_platform "$OS" "$ARCH"
            # Also create a symlink without platform suffix for local use
            cd "$BUILD_DIR"
            ln -sf "krakend-mcp-${OS}-${ARCH}" "krakend-mcp-server"
            log_success "Created symlink: krakend-mcp-server -> krakend-mcp-${OS}-${ARCH}"
            ;;
        mingw*|msys*|cygwin*)
            build_for_platform "windows" "$ARCH"
            ;;
        *)
            log_error "Unsupported OS: $OS"
            exit 1
            ;;
    esac
fi

# Step 4: Generate checksums
log "Step 4: Generating checksums..."

cd "$BUILD_DIR"
if command -v sha256sum >/dev/null 2>&1; then
    sha256sum krakend-mcp-* > checksums.txt 2>/dev/null || true
elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 krakend-mcp-* > checksums.txt 2>/dev/null || true
else
    log_warning "sha256sum not found, skipping checksums"
fi

if [ -f checksums.txt ]; then
    log_success "Checksums generated"
fi

cd "$PROJECT_ROOT"

echo ""
log "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
log_success "Build Complete!"
log "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
log "Binaries:"
ls -lh "$BUILD_DIR"/ | grep krakend-mcp
echo ""
log "Documentation embedded:"
echo "  - KrakenD docs: $DOC_SIZE bytes"
echo "  - Search index: $(du -sh "$SEARCH_DIR/index" | cut -f1)"
echo ""
if [ -f "$BUILD_DIR/checksums.txt" ]; then
    log "Checksums:"
    cat "$BUILD_DIR/checksums.txt"
    echo ""
fi
log "The binary is fully offline-capable!"
log "Users can optionally refresh docs with: refresh_documentation_index tool"
