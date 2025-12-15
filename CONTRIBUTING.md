# Contributing to KrakenD MCP Server

Thank you for your interest in contributing to the KrakenD MCP Server!

## Development Setup

### Prerequisites

- Go 1.21 or higher
- Git

### Local Development

1. Clone the repository:
```bash
git clone https://github.com/krakend/mcp-server.git
cd mcp-server
```

2. Build the server:
```bash
# Build for your platform (auto-detects OS and architecture)
./scripts/build.sh --platform=darwin-arm64  # macOS Apple Silicon
./scripts/build.sh --platform=darwin-amd64  # macOS Intel
./scripts/build.sh --platform=linux-amd64   # Linux x64
```

3. Test the binary:
```bash
./build/krakend-mcp-darwin-arm64 --version
```

The build script automatically:
- Downloads and indexes KrakenD documentation
- Embeds docs and search index in the binary
- Creates a fully offline-capable binary

## Project Structure

```
mcp-server/
├── main.go              # Server entry point
├── tools/               # MCP tools implementation
│   ├── validation.go    # Config validation tools
│   ├── generation.go    # Config generation tools
│   ├── features.go      # Feature detection tools
│   ├── docsearch.go     # Documentation search tools
│   └── embedded.go      # Embedded data (docs, index)
├── tools/data/          # Static data (embedded at build time)
│   ├── features/        # Feature catalog
│   └── editions/        # CE vs EE compatibility matrix
├── scripts/             # Build and installation scripts
│   ├── build.sh         # Cross-platform build script
│   └── install.sh       # Standalone installation script
└── .github/workflows/   # CI/CD automation
```

## Adding New MCP Tools

To add a new MCP tool:

1. Add your tool implementation in the appropriate `tools/*.go` file
2. Register the tool in `main.go` in the `registerTools()` function
3. Add tests for your tool
4. Update the README.md to document the new tool

Example tool structure:

```go
// RegisterMyTool registers a new MCP tool
func RegisterMyTool(server *mcp.Server) error {
    tool := &mcp.Tool{
        Name:        "my_tool",
        Description: "Description of what this tool does",
        InputSchema: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "param": map[string]string{
                    "type":        "string",
                    "description": "Parameter description",
                },
            },
            "required": []string{"param"},
        },
    }

    handler := func(arguments map[string]interface{}) (*mcp.CallToolResult, error) {
        // Tool implementation
        return &mcp.CallToolResult{
            Content: []interface{}{
                map[string]string{
                    "type": "text",
                    "text": "Tool output",
                },
            },
        }, nil
    }

    return server.AddTool(tool, handler)
}
```

## Code Style

- Follow standard Go conventions
- Run `go fmt` before committing
- Add comments for exported functions and types
- Keep functions focused and testable

## Testing

**Status**: Automated tests are planned for the future (see README.md Development section).

Currently, testing is done manually:
- Build the server with `./scripts/build.sh`
- Test with real KrakenD configurations
- Verify all 10 MCP tools work correctly

When adding new features, test thoroughly with various scenarios before submitting a PR.

## Pull Request Process

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Test your changes thoroughly
5. Commit your changes (`git commit -m 'Add amazing feature'`)
6. Push to your branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

### PR Guidelines

- Provide a clear description of the changes
- Include relevant issue numbers if applicable
- Test your changes manually with various scenarios
- Update documentation if needed
- Keep commits focused and atomic
- Run `go fmt` before committing

## Reporting Issues

When reporting issues, please include:

- KrakenD MCP Server version
- Go version
- Operating system and architecture
- Steps to reproduce the issue
- Expected vs actual behavior
- Any relevant error messages or logs

## Documentation

- Update README.md for user-facing changes
- Add inline code comments for complex logic
- Update the feature catalog in `data/features/` when adding support for new KrakenD features

## License

By contributing to this project, you agree that your contributions will be licensed under the Apache 2.0 License.

## Questions?

Feel free to open an issue for questions or discussion about contributing.

---

Thank you for helping make KrakenD MCP Server better!
