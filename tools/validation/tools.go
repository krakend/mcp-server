package validation

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterValidationTools registers all validation tools with the MCP server
func RegisterValidationTools(server *mcp.Server) error {
	// Tool 1: validate_config
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "validate_config",
			Description: "Complete KrakenD configuration validation with JSON syntax check, version-aware validation (matches $schema field), and linting. Uses smart 4-tier fallback: native krakend check -l (if version matches) → Docker with version-specific image → native with warning → JSON Schema validation. Automatically detects CE vs EE features.\n\nIMPORTANT: The output contains a 'guidance' field with explicit instructions. The errors and warnings returned are AUTHORITATIVE - do NOT suggest additional fixes based on assumptions or patterns. Only fix errors explicitly listed. For unclear syntax, use search_documentation tool to verify against official docs.",
		},
		ValidateConfig,
	)

	// Tool 2: audit_security
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "audit_security",
			Description: "Perform security audit of KrakenD configuration using smart three-tier fallback (native KrakenD audit → Docker → basic security checks)",
		},
		AuditSecurity,
	)

	return nil
}
