package tools

import (
	"context"

	"github.com/krakend/mcp-server/internal/features"
	"github.com/krakend/mcp-server/tools/validation"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func auditSecurityWithHint(ctx context.Context, req *mcp.CallToolRequest, input validation.AuditSecurityInput) (*mcp.CallToolResult, validation.AuditSecurityOutput, error) {
	res, output, err := validation.AuditSecurity(ctx, req, input)
	if err != nil {
		return res, output, err
	}

	// Mark the session as Enterprise if the config uses EE features.
	// readConfigContent handles both JSON strings and file paths; errors are
	// silently ignored — detection is best-effort.
	if configContent, readErr := readConfigContent(input.Config); readErr == nil {
		if features.DetectEnterpriseFeaturesSimple(configContent) {
			sessionRegistry.MarkAsEnterprise(sessionIdFromReq(req))
		}
	}

	for _, issue := range output.Issues {
		if issue.Severity == "critical" || issue.Severity == "high" {
			output.Hint = GetSecurityHint(sessionIdFromReq(req))
			break
		}
	}

	if res == nil {
		res = &mcp.CallToolResult{}
	}
	// Content is left nil: the SDK auto-generates a single JSON TextContent
	// block from the typed output (Hint field included as plain data).
	res.Meta = map[string]interface{}{
		"security_hint": output.Hint != "",
	}
	return res, output, nil
}

func RegisterAuditTool(server *mcp.Server) {
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "audit_security",
			Description: "Perform security audit of KrakenD configuration using smart three-tier fallback (native KrakenD audit → Docker → basic security checks)",
		},
		auditSecurityWithHint,
	)
}
