package tools

import (
	"context"

	"github.com/krakend/mcp-server/tools/validation"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Re-export types from validation subpackage for backward compatibility
type (
	ValidationEnvironment  = validation.ValidationEnvironment
	FlexibleConfigInfo     = validation.FlexibleConfigInfo
	ValidationResult       = validation.ValidationResult
	ValidationError        = validation.ValidationError
	ValidationWarning      = validation.ValidationWarning
	ValidateConfigInput    = validation.ValidateConfigInput
	ValidateConfigOutput   = validation.ValidateConfigOutput
	SecurityIssue          = validation.SecurityIssue
	AuditSecurityInput     = validation.AuditSecurityInput
	AuditSecurityOutput    = validation.AuditSecurityOutput
)

// Re-export constants
const (
	ValidationGuidance = validation.ValidationGuidance
)

// Re-export functions from validation subpackage
var (
	DetectEnvironment             = validation.DetectEnvironment
	DetectFlexibleConfiguration   = validation.DetectFlexibleConfiguration
	ExtractVersionFromConfig      = validation.ExtractVersionFromConfig
	GetLocalKrakenDVersion        = validation.GetLocalKrakenDVersion
	ValidateConfig                = validation.ValidateConfig
	AuditSecurity                 = validation.AuditSecurity
	RegisterValidationTools       = validation.RegisterValidationTools
)

// Deprecated: Use validation.DetectEnvironment directly
func _DetectEnvironment() *ValidationEnvironment {
	return validation.DetectEnvironment()
}

// Deprecated: Use validation.ValidateConfig directly
func _ValidateConfig(ctx context.Context, req *mcp.CallToolRequest, input ValidateConfigInput) (*mcp.CallToolResult, ValidateConfigOutput, error) {
	return validation.ValidateConfig(ctx, req, input)
}

// Deprecated: Use validation.AuditSecurity directly
func _AuditSecurity(ctx context.Context, req *mcp.CallToolRequest, input AuditSecurityInput) (*mcp.CallToolResult, AuditSecurityOutput, error) {
	return validation.AuditSecurity(ctx, req, input)
}
