package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/krakend/mcp-server/internal/runtime"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// DetectRuntimeInput defines input for detect_runtime_environment tool
type DetectRuntimeInput struct {
	Config string `json:"config" jsonschema:"KrakenD configuration (JSON string or file path)"`
}

// DetectRuntimeOutput defines output for detect_runtime_environment tool
type DetectRuntimeOutput struct {
	*runtime.RuntimeInfo
}

// DetectRuntimeEnvironment detects the optimal runtime environment for KrakenD
func DetectRuntimeEnvironment(ctx context.Context, req *mcp.CallToolRequest, input DetectRuntimeInput) (*mcp.CallToolResult, DetectRuntimeOutput, error) {
	// Read config content (file or JSON string)
	configContent, err := readConfigContent(input.Config)
	if err != nil {
		return nil, DetectRuntimeOutput{}, fmt.Errorf("failed to read config: %w", err)
	}

	// Detect runtime info
	runtimeInfo, err := runtime.DetectRuntimeInfo(configContent)
	if err != nil {
		return nil, DetectRuntimeOutput{}, fmt.Errorf("failed to detect runtime: %w", err)
	}

	return nil, DetectRuntimeOutput{RuntimeInfo: runtimeInfo}, nil
}

// RegisterRuntimeTools registers runtime-related tools with the MCP server
func RegisterRuntimeTools(server *mcp.Server) {
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "detect_runtime_environment",
			Description: "Detects the optimal runtime environment for KrakenD (native binary vs Docker), checks version compatibility, and provides execution recommendations. Useful for determining how to run KrakenD commands (check, audit, run, etc.) based on available tools and configuration requirements.",
		},
		DetectRuntimeEnvironment,
	)
}

// readConfigContent reads configuration from file path or returns JSON string directly
func readConfigContent(config string) (string, error) {
	// Check if it's a file path
	trimmed := strings.TrimSpace(config)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		// It's JSON content
		return config, nil
	}

	// Try to read as file
	content, err := os.ReadFile(config)
	if err != nil {
		return "", fmt.Errorf("failed to read config file '%s': %w", config, err)
	}

	return string(content), nil
}
