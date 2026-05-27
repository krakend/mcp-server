package validation

// ValidateConfigDescription is the MCP tool description for validate_config.
// Defined here so it stays co-located with the implementation.
// Note: validate_config is registered via tools.RegisterValidateConfigTool,
// which wraps ValidateConfig with Enterprise session detection.
// audit_security is registered via tools.RegisterAuditTool.
const ValidateConfigDescription = "Complete KrakenD configuration validation with JSON syntax check, version-aware validation (matches $schema field), and linting. Uses smart 4-tier fallback: native krakend check -l (if version matches) → Docker with version-specific image → native with warning → JSON Schema validation. Automatically detects CE vs EE features.\n\nIMPORTANT: The output contains a 'guidance' field with explicit instructions. The errors and warnings returned are AUTHORITATIVE - do NOT suggest additional fixes based on assumptions or patterns. Only fix errors explicitly listed. For unclear syntax, use search_documentation tool to verify against official docs."
