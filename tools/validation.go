package tools

import (
	"github.com/krakend/mcp-server/tools/validation"
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

