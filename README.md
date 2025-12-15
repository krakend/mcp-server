# KrakenD MCP Server

**Universal MCP server for KrakenD API Gateway configuration validation, security auditing, and intelligent configuration assistance.**

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](https://www.apache.org/licenses/LICENSE-2.0)
[![Go Version](https://img.shields.io/badge/Go-1.21+-blue.svg)](https://golang.org)
[![MCP Protocol](https://img.shields.io/badge/MCP-1.0-green.svg)](https://modelcontextprotocol.io)

## Overview

KrakenD MCP Server is a [Model Context Protocol](https://modelcontextprotocol.io) server that provides intelligent assistance for [KrakenD API Gateway](https://www.krakend.io) configuration files. It works with **any MCP-compatible AI assistant** including Claude Code, VS Code, Cursor, Cline, and Zed.

### Features

- ✅ **Configuration Validation** - Version-aware validation with specific error messages
- 🔒 **Security Auditing** - Comprehensive security analysis with actionable recommendations
- 🎯 **Feature Discovery** - Browse 100+ KrakenD features with CE/EE compatibility
- 🏗️ **Config Generation** - Generate endpoints, backends, and complete configurations
- 📖 **Documentation Search** - Full-text search through official KrakenD documentation
- 🔍 **Edition Detection** - Automatic CE vs EE feature detection
- ⚡ **Flexible Configuration** - Automatic detection and support for both CE and EE FC variants

## Installation

### Quick Start (Recommended)

**Automatic installation** with platform detection:

```bash
curl -sSL https://raw.githubusercontent.com/krakend/mcp-server/main/scripts/install.sh | bash
```

This script will:
- ✅ Auto-detect your platform (macOS, Linux, Windows)
- ✅ Download the correct binary
- ✅ Verify checksums for security
- ✅ Install to `/usr/local/bin/`
- ✅ Create data directory at `~/.krakend-mcp/`

**Manual installation** - Download pre-compiled binaries from [GitHub Releases](https://github.com/krakend/mcp-server/releases):

```bash
# macOS Apple Silicon
curl -L -o krakend-mcp-server https://github.com/krakend/mcp-server/releases/download/v0.6.1/krakend-mcp-darwin-arm64
chmod +x krakend-mcp-server
sudo mv krakend-mcp-server /usr/local/bin/

# macOS Intel
curl -L -o krakend-mcp-server https://github.com/krakend/mcp-server/releases/download/v0.6.1/krakend-mcp-darwin-amd64
chmod +x krakend-mcp-server
sudo mv krakend-mcp-server /usr/local/bin/

# Linux x64
curl -L -o krakend-mcp-server https://github.com/krakend/mcp-server/releases/download/v0.6.1/krakend-mcp-linux-amd64
chmod +x krakend-mcp-server
sudo mv krakend-mcp-server /usr/local/bin/

# Manually create data directory
mkdir -p ~/.krakend-mcp/{docs,search}
```

### Configuration by Client

#### Claude Code (Recommended - Full Experience)

**🌟 Best Experience**: Use the [KrakenD AI Assistant plugin](https://github.com/krakend/claude-code-plugin) for:
- ✅ Automatic binary management
- ✅ 4 proactive Skills (auto-activate)
- ✅ 1 Architecture Agent
- ✅ Zero configuration

Install plugin:
```bash
# In Claude Code
/plugin marketplace add krakend/claude-code-plugin
/plugin install krakend-ai-assistant
```

The plugin automatically downloads and configures the MCP server.

---

#### Claude Code (MCP Only - Manual Setup)

If you only want the MCP tools without Skills/Agent:

1. Download the binary (see Quick Start above)

2. Create or edit `~/.claude/mcp_settings.json`:
```json
{
  "mcpServers": {
    "krakend": {
      "type": "stdio",
      "command": "/usr/local/bin/krakend-mcp-server",
      "args": [],
      "description": "KrakenD configuration validation and assistance"
    }
  }
}
```

3. Restart Claude Code

**Tools available**: All 10 MCP tools (validate, audit, generate, search docs, etc.)

---

#### Cursor

1. Download the binary (see Quick Start above)

2. Open Settings → Features → MCP Servers

3. Add server configuration:
```json
{
  "mcpServers": {
    "krakend": {
      "command": "/usr/local/bin/krakend-mcp-server",
      "args": []
    }
  }
}
```

4. Restart Cursor

**Usage**: Use `@krakend` in chat to access KrakenD tools

---

#### VS Code (Native MCP Support)

**⭐ Recommended for VS Code users** - Uses built-in MCP support (GitHub Copilot Chat)

1. Download the binary (see Quick Start above)

2. Open Command Palette (`Cmd+P` on macOS, `Ctrl+P` on Windows/Linux)

3. Type **"Add MCP"** and select the command

4. Enter the path to the binary:
   ```
   /usr/local/bin/krakend-mcp-server
   ```
   Or if using local build:
   ```
   /Users/yourusername/path/to/mcp-server/build/krakend-mcp-darwin-arm64
   ```

5. Server is immediately available in Copilot Chat

**Usage**:
- Open GitHub Copilot Chat
- The MCP tools are available in the chat context automatically
- Ask questions like "validate this KrakenD config" or "search KrakenD docs for rate limiting"
- The assistant will use the appropriate MCP tools automatically

---

#### Cline (VS Code Extension)

1. Download the binary (see Quick Start above)

2. Install Cline extension from VS Code Marketplace

3. Open Cline Settings → MCP Servers

4. Add server:
```json
{
  "krakend": {
    "command": "/usr/local/bin/krakend-mcp-server"
  }
}
```

5. Restart VS Code

**Usage**: Cline automatically detects and uses MCP tools

---

#### Zed Editor

1. Download the binary (see Quick Start above)

2. Edit Zed settings (`Cmd+,` → MCP):
```json
{
  "mcp_servers": {
    "krakend": {
      "command": "/usr/local/bin/krakend-mcp-server"
    }
  }
}
```

3. Restart Zed

**Usage**: Access via Zed's AI assistant

---

#### Standalone / CLI / Scripts

Use the server directly for CI/CD, scripts, or standalone tools:

```bash
# Validate config
echo '{"endpoint": "/api/users", ...}' | krakend-mcp-server validate

# Security audit
krakend-mcp-server audit --config krakend.json

# Search docs
krakend-mcp-server search "rate limiting"

# Check version
krakend-mcp-server --version
```

---

### Configuration Comparison

| Client | Setup Difficulty | Features | Best For |
|--------|-----------------|----------|----------|
| **Claude Code + Plugin** | ⭐ Easy (automatic) | Full (MCP + Skills + Agent) | Complete KrakenD assistance |
| **VS Code (Native)** | ⭐⭐ Easy (Cmd+P → Add MCP) | MCP tools only | VS Code + Copilot users |
| **Claude Code (MCP only)** | ⭐⭐ Medium (manual config) | MCP tools only | Simple validation/generation |
| **Cursor** | ⭐⭐ Medium | MCP tools only | Cursor users |
| **Cline** | ⭐⭐ Medium | MCP tools only | Cline users |
| **Zed** | ⭐⭐ Medium | MCP tools only | Zed users |
| **Standalone CLI** | ⭐⭐⭐ Advanced | Direct tool access | CI/CD, scripts |

## Documentation System

KrakenD MCP Server includes an intelligent documentation search system powered by [Bleve](https://github.com/blevesearch/bleve), a full-text search engine written in Go.

### How It Works

**Embedded Documentation (Offline-First)**
- Official KrakenD documentation is **embedded directly in the binary** during build
- Pre-built search index included (~5.7MB)
- **Works completely offline** - no internet required
- Instant availability on first run

**Local Documentation Updates**
- Use `refresh_documentation_index` tool to download latest documentation
- Updated docs stored locally at `~/.krakend-mcp/docs/` and `~/.krakend-mcp/search/`
- **Priority**: Local (if exists) > Embedded (always available)
- Manual refresh recommended every 7 days for latest features

**Search Capabilities**
- Full-text search with relevance ranking
- Context-aware results (shows surrounding text)
- Supports complex queries (phrases, boolean operators)
- Fast response times (milliseconds)
- Works completely offline with embedded docs

### Data Directory Structure

```
~/.krakend-mcp/
├── docs/              # Downloaded documentation files
│   ├── index.json     # Documentation metadata
│   └── content/       # Markdown content files
└── search/            # Bleve search index
    └── *.bleve        # Index files
```

### Storage Requirements

**Binary Size**:
- **MCP Server Binary**: ~21 MB (includes embedded documentation and index)
- No additional downloads required for basic functionality

**Optional Local Storage** (if using `refresh_documentation_index`):
- **Updated Documentation**: ~2 MB
- **Updated Search Index**: ~6 MB
- **Total Local**: ~8 MB additional (stored in `~/.krakend-mcp/`)

### Manual Management

```bash
# Check if documentation is cached
ls ~/.krakend-mcp/docs/

# Clear cache (will re-download on next use)
rm -rf ~/.krakend-mcp/docs/ ~/.krakend-mcp/search/

# Manually refresh (via MCP tool)
# Use refresh_documentation_index tool from your MCP client
```

### Privacy & Offline Use

- Documentation is downloaded from **official KrakenD sources only**
- No telemetry or tracking
- After initial download, search works **completely offline**
- No external requests during search operations

## MCP Tools

The server exposes 10 specialized tools:

### Validation & Security

| Tool | Description |
|------|-------------|
| `validate_config` | Version-aware configuration validation with detailed error messages |
| `audit_security` | Security audit with fallback (native → Docker → basic checks) |
| `check_edition_compatibility` | Detect which KrakenD edition (CE or EE) a config requires |

### Feature Discovery

| Tool | Description |
|------|-------------|
| `list_features` | Browse all KrakenD features with name, namespace, edition, and category |
| `get_feature_config_template` | Get configuration templates with required/optional fields |

### Configuration Generation

| Tool | Description |
|------|-------------|
| `generate_basic_config` | Generate complete KrakenD configuration from scratch |
| `generate_endpoint_config` | Generate endpoint configuration with best practices |
| `generate_backend_config` | Generate backend service configuration |

### Documentation

| Tool | Description |
|------|-------------|
| `search_documentation` | Full-text search through KrakenD documentation (powered by Bleve) |
| `refresh_documentation_index` | Update documentation cache (auto-runs if cache > 7 days old) |

## Usage Examples

### Validate a KrakenD Configuration

```bash
# Via MCP client
validate_config --config krakend.json

# Returns:
# ✅ Configuration is valid for KrakenD v2.7
# Edition required: Community Edition (CE)
```

### Security Audit

```bash
audit_security --config krakend.json

# Returns:
# 🔍 Security Audit Report
# ⚠️ High: Missing authentication on /api/admin endpoints
# ⚠️ Medium: CORS not configured
# ✅ Rate limiting configured correctly
```

### Feature Discovery

```bash
# List all authentication features
list_features --category authentication

# Returns:
# - JWT Validation (CE) - Validate JWT tokens
# - API Key Auth (EE) - API key authentication
# - OAuth Client (CE) - OAuth 2.0 client flow
```

### Generate Configuration

```bash
# Generate a new endpoint
generate_endpoint_config \
  --method GET \
  --path /api/users \
  --backend_url https://backend.example.com/users

# Returns complete endpoint config with best practices
```

## Flexible Configuration Support

KrakenD MCP Server automatically detects and handles both CE and EE variants of Flexible Configuration:

- **CE**: `.tmpl` files with env vars (`FC_ENABLE=1`)
- **EE**: `flexible_config.json` behavioral file

No manual configuration needed - detection is automatic!

## Supported Platforms

Pre-compiled binaries available for:

- macOS (Intel & Apple Silicon)
- Linux (x64 & ARM64)
- Windows (x64)

## Development

### Prerequisites

- Go 1.21+
- KrakenD binary (optional, for native validation)
- Docker (optional, for fallback validation)

### Build from Source

**Quick Build** (with embedded docs):
```bash
git clone https://github.com/krakend/mcp-server.git
cd mcp-server
./scripts/build.sh
./build/krakend-mcp-server --version
```

**Build Options**:
```bash
./scripts/build.sh              # Build for current platform
./scripts/build.sh --all        # Build for all platforms (darwin, linux, windows)
./scripts/build.sh --platform=linux-amd64  # Build specific platform
```

**Development Build** (without embedded docs):
```bash
go build -o krakend-mcp-server
./krakend-mcp-server --version
```

The build script:
1. Downloads official KrakenD documentation
2. Indexes documentation with Bleve
3. Embeds docs + index into binary
4. Compiles cross-platform binaries

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

## Version Compatibility

| KrakenD Version | MCP Server Version | Status |
|----------------|-------------------|--------|
| 2.0 - 2.7      | 0.6.1+            | ✅ Full support |
| 1.x            | -                 | ⚠️ Limited support |

## Related Projects

- **[KrakenD AI Assistant](https://github.com/krakend/claude-code-plugin)** - Claude Code plugin with proactive Skills and Architecture Agent (uses this MCP server)
- **[KrakenD](https://github.com/krakend/krakend-ce)** - Ultra-performant open-source API Gateway

## Architecture

```
┌─────────────────────┐
│  AI Assistant       │
│  (Claude, Cursor,   │
│   Cline, etc.)      │
└──────────┬──────────┘
           │ MCP Protocol (stdio)
           ↓
┌─────────────────────┐
│ KrakenD MCP Server  │
│                     │
│  ├─ Validation      │
│  ├─ Security Audit  │
│  ├─ Features        │
│  ├─ Generation      │
│  └─ Docs Search     │
└──────────┬──────────┘
           │
           ↓
┌─────────────────────┐
│ KrakenD Knowledge   │
│                     │
│  ├─ Feature Catalog │
│  ├─ Edition Matrix  │
│  ├─ Documentation   │
│  └─ Best Practices  │
└─────────────────────┘
```

## Releases

New versions are automatically built and published to [GitHub Releases](https://github.com/krakend/mcp-server/releases) when a new tag is pushed.

Binary naming convention: `krakend-mcp-{OS}-{ARCH}`

Examples:
- `krakend-mcp-darwin-arm64` (macOS Apple Silicon)
- `krakend-mcp-linux-amd64` (Linux x64)
- `krakend-mcp-windows-amd64.exe` (Windows x64)

## Development

### Status

This project is in active development. Current status:

- ✅ Core MCP server implementation
- ✅ All 10 tools functional and tested manually
- ✅ Cross-platform builds (macOS, Linux, Windows)
- ✅ Embedded documentation with offline search
- ⏳ **Automated testing suite** (pending)

### Testing

Automated tests are planned for the future. Priority areas:

1. **Integration tests** for each MCP tool with real-world configs
2. **Unit tests** for edition detection and feature parsing logic
3. **E2E tests** for the complete MCP protocol flow
4. **Golden file tests** for complex validation outputs

Contributions welcome! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Building from Source

See the "Building from Source" section above for detailed build instructions.

## Support

- **Issues**: [GitHub Issues](https://github.com/krakend/mcp-server/issues)
- **Documentation**: [krakend.io/docs](https://www.krakend.io/docs/)
- **Community**: [KrakenD Community](https://github.com/krakend/krakend-ce/discussions)

## License

Apache 2.0 License - see [LICENSE](LICENSE) file for details.

## Security

For security concerns, see [SECURITY.md](SECURITY.md).

---

**Made with ❤️ by [KrakenD](https://www.krakend.io)**

*Part of the KrakenD ecosystem - The fastest API Gateway with auto-generated documentation*
