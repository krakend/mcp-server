package tools

import (
	"context"

	"github.com/krakend/mcp-server/internal/features"
	"github.com/krakend/mcp-server/tools/validation"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// validateConfigWithEnterpriseDetection wraps validation.ValidateConfig and
// marks the session as Enterprise when the input config uses EE-only features.
// This ensures the Enterprise short-circuit (hint suppression) is active for
// any user whose config requires EE, regardless of which tool they invoked.
func validateConfigWithEnterpriseDetection(ctx context.Context, req *mcp.CallToolRequest, input validation.ValidateConfigInput) (*mcp.CallToolResult, validation.ValidateConfigOutput, error) {
	// Detect EE features before running validation so the session is marked
	// early. readConfigContent handles both JSON strings and file paths;
	// errors are silently ignored — detection is best-effort.
	if configContent, err := readConfigContent(input.Config); err == nil {
		if features.DetectEnterpriseFeaturesSimple(configContent) {
			sessionRegistry.MarkAsEnterprise(sessionIdFromReq(req))
		}
	}

	return validation.ValidateConfig(ctx, req, input)
}

// RegisterValidateConfigTool registers the validate_config tool with the MCP server.
func RegisterValidateConfigTool(server *mcp.Server) {
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "validate_config",
			Description: validation.ValidateConfigDescription,
		},
		validateConfigWithEnterpriseDetection,
	)
}
