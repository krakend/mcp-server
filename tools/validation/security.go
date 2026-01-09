package validation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/krakend/mcp-server/internal/features"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SecurityIssue represents a security vulnerability or concern
type SecurityIssue struct {
	Severity    string   `json:"severity"`              // "critical", "high", "medium", "low", "info"
	Category    string   `json:"category"`              // "authentication", "cors", "headers", "ssl", "exposure", etc.
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Location    string   `json:"location,omitempty"`    // JSON path if applicable
	Remediation string   `json:"remediation"`
	References  []string `json:"references,omitempty"`
}

// AuditSecurityInput defines input for audit_security tool
type AuditSecurityInput struct {
	Config string `json:"config" jsonschema:"KrakenD configuration as JSON string or file path"`
}

// AuditSecurityOutput defines output for audit_security tool
type AuditSecurityOutput struct {
	Valid       bool                   `json:"valid"`
	Method      string                 `json:"method"` // "native", "docker", or "basic"
	Issues      []SecurityIssue        `json:"issues"`
	Summary     string                 `json:"summary"`
	Score       int                    `json:"score,omitempty"` // 0-100 security score
	Environment *ValidationEnvironment `json:"environment,omitempty"`
}

// AuditSecurity performs security audit of KrakenD configuration using three-tier fallback
func AuditSecurity(ctx context.Context, req *mcp.CallToolRequest, input AuditSecurityInput) (*mcp.CallToolResult, AuditSecurityOutput, error) {
	env := DetectEnvironment()

	var result *AuditSecurityOutput
	var err error

	// Check if input.Config is a file path and read it
	configContent := input.Config
	if isFilePath(input.Config) {
		fileContent, err := os.ReadFile(input.Config)
		if err != nil {
			// Provide a clear, specific error message
			var errMsg string
			if os.IsNotExist(err) {
				errMsg = fmt.Sprintf("configuration file not found: %s", input.Config)
			} else if os.IsPermission(err) {
				errMsg = fmt.Sprintf("permission denied reading file: %s", input.Config)
			} else {
				errMsg = fmt.Sprintf("failed to read configuration file '%s': %s", input.Config, err.Error())
			}
			return nil, AuditSecurityOutput{
				Valid:  false,
				Method: "file_read",
				Issues: []SecurityIssue{
					{
						Severity:    "critical",
						Category:    "file_access",
						Title:       "Configuration file could not be read",
						Description: errMsg,
						Remediation: "Ensure the file path is correct and the file exists. Use an absolute path or a path relative to the current working directory.",
					},
				},
				Summary: "Failed to read configuration file for security audit",
			}, nil
		}
		configContent = string(fileContent)
	}

	// Extract target version from config
	targetVersion := ExtractVersionFromConfig(configContent)

	// Version-aware audit with smart fallback

	// Priority 1: Native KrakenD (if version matches or config uses latest)
	if env.HasNativeKrakenD {
		localVersion, verErr := GetLocalKrakenDVersion()
		if verErr == nil {
			if targetVersion == "latest" || localVersion == targetVersion {
				// Version matches or config uses latest - use native
				result, err = auditWithNativeKrakenD(configContent, "")
				if err == nil {
					result.Environment = env
					return nil, *result, nil
				}
			}
			// Version mismatch - skip to Docker
		}
	}

	// Priority 2: Docker with correct version
	if env.HasDocker {
		// Try version-specific image
		result, err = auditWithDockerVersion(configContent, "", targetVersion)
		if err == nil {
			result.Environment = env
			return nil, *result, nil
		}

		// If version-specific failed, try latest
		if targetVersion != "latest" {
			result, err = auditWithDockerVersion(configContent, "", "latest")
			if err == nil {
				result.Environment = env
				return nil, *result, nil
			}
		}
	}

	// Priority 3: Fallback to native even if version mismatch
	if env.HasNativeKrakenD {
		result, err = auditWithNativeKrakenD(configContent, "")
		if err == nil {
			result.Environment = env
			return nil, *result, nil
		}
	}

	// Priority 4: Use basic security checks (last resort)
	result, err = auditWithBasicChecks(configContent)
	if err != nil {
		return nil, AuditSecurityOutput{}, fmt.Errorf("all audit methods failed: %w", err)
	}

	result.Environment = env
	return nil, *result, nil
}

// auditWithNativeKrakenD audits using native krakend binary
func auditWithNativeKrakenD(configJSON string, tempDir string) (*AuditSecurityOutput, error) {
	env := DetectEnvironment()

	var configFile string

	// If Flexible Configuration is detected, use base template directly
	if env.FlexibleConfig != nil && env.FlexibleConfig.Detected && env.FlexibleConfig.BaseTemplate != "" {
		configFile = env.FlexibleConfig.BaseTemplate
	} else {
		// Create temporary file for standard config
		if tempDir == "" {
			tempDir = os.TempDir()
		}

		tempFile := filepath.Join(tempDir, "krakend-audit-config.json")
		if err := os.WriteFile(tempFile, []byte(configJSON), 0600); err != nil {
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}
		configFile = tempFile
		defer os.Remove(tempFile)
	}

	// Run krakend audit with FC support
	cmd := buildKrakenDCommand(env, "audit", configFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	result := &AuditSecurityOutput{
		Method:      "native",
		Issues:      []SecurityIssue{},
		Environment: env,
	}

	err := cmd.Run()
	output := stdout.String() + stderr.String()

	// krakend audit returns non-zero if issues found
	if err != nil && output == "" {
		return nil, fmt.Errorf("krakend audit command failed: %w", err)
	}

	// Parse krakend audit output
	// Note: krakend audit output format may vary, this is a basic parser
	if strings.Contains(output, "CRITICAL") || strings.Contains(output, "HIGH") {
		result.Valid = false
	} else {
		result.Valid = true
	}

	// Basic parsing of output into issues
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Detect severity keywords
		severity := "info"
		if strings.Contains(strings.ToUpper(line), "CRITICAL") {
			severity = "critical"
		} else if strings.Contains(strings.ToUpper(line), "HIGH") {
			severity = "high"
		} else if strings.Contains(strings.ToUpper(line), "MEDIUM") {
			severity = "medium"
		} else if strings.Contains(strings.ToUpper(line), "LOW") {
			severity = "low"
		}

		if severity != "info" {
			result.Issues = append(result.Issues, SecurityIssue{
				Severity:    severity,
				Category:    "security",
				Title:       line,
				Description: line,
				Remediation: "Review KrakenD security documentation",
			})
		}
	}

	result.Summary = fmt.Sprintf("Security audit completed with %d issue(s) found (native KrakenD)", len(result.Issues))
	return result, nil
}

// auditWithDocker audits using Docker container
func auditWithDocker(configJSON string, tempDir string) (*AuditSecurityOutput, error) {
	env := DetectEnvironment()

	var configFile string
	var cmd *exec.Cmd

	// If Flexible Configuration is detected, mount project directory
	if env.FlexibleConfig != nil && env.FlexibleConfig.Detected && env.FlexibleConfig.BaseTemplate != "" {
		// Get current working directory (project root)
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}

		configFile = env.FlexibleConfig.BaseTemplate
		cmd = buildDockerKrakenDCommand(env, "audit", filepath.Join(cwd, configFile))
	} else {
		// Create temporary file for standard config
		if tempDir == "" {
			tempDir = os.TempDir()
		}

		tempFile := filepath.Join(tempDir, "krakend-audit-config.json")
		if err := os.WriteFile(tempFile, []byte(configJSON), 0600); err != nil {
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}
		defer os.Remove(tempFile)

		// Run docker with krakend audit (standard)
		cmd = exec.Command("docker", "run", "--rm",
			"-v", fmt.Sprintf("%s:/etc/krakend/krakend.json:ro", tempFile),
			"krakend:latest",
			"audit", "-c", "/etc/krakend/krakend.json")
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	result := &AuditSecurityOutput{
		Method:      "docker",
		Issues:      []SecurityIssue{},
		Environment: env,
	}

	err := cmd.Run()
	output := stdout.String() + stderr.String()

	if err != nil && output == "" {
		return nil, fmt.Errorf("docker audit command failed: %w", err)
	}

	// Parse output similar to native
	if strings.Contains(output, "CRITICAL") || strings.Contains(output, "HIGH") {
		result.Valid = false
	} else {
		result.Valid = true
	}

	// Basic parsing
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		severity := "info"
		if strings.Contains(strings.ToUpper(line), "CRITICAL") {
			severity = "critical"
		} else if strings.Contains(strings.ToUpper(line), "HIGH") {
			severity = "high"
		} else if strings.Contains(strings.ToUpper(line), "MEDIUM") {
			severity = "medium"
		} else if strings.Contains(strings.ToUpper(line), "LOW") {
			severity = "low"
		}

		if severity != "info" {
			result.Issues = append(result.Issues, SecurityIssue{
				Severity:    severity,
				Category:    "security",
				Title:       line,
				Description: line,
				Remediation: "Review KrakenD security documentation",
			})
		}
	}

	result.Summary = fmt.Sprintf("Security audit completed with %d issue(s) found (Docker)", len(result.Issues))
	return result, nil
}

// auditWithDockerVersion audits using Docker with specific KrakenD version
func auditWithDockerVersion(configJSON string, tempDir string, targetVersion string) (*AuditSecurityOutput, error) {
	env := DetectEnvironment()

	// Detect if EE features are used
	// Pass nil to use CommonEEFeatures from internal/features
	isEE := features.DetectEnterpriseFeatures(configJSON, nil)

	// Determine Docker image based on version and edition
	var dockerImage string
	if isEE {
		dockerImage = fmt.Sprintf("krakend/krakend-ee:%s", targetVersion)
		if targetVersion == "latest" {
			dockerImage = "krakend/krakend-ee:latest"
		}
	} else {
		dockerImage = fmt.Sprintf("krakend:%s", targetVersion)
		if targetVersion == "latest" {
			dockerImage = "krakend:latest"
		}
	}

	var configFile string
	var cmd *exec.Cmd

	// If Flexible Configuration is detected, mount project directory
	if env.FlexibleConfig != nil && env.FlexibleConfig.Detected && env.FlexibleConfig.BaseTemplate != "" {
		// Get current working directory (project root)
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}

		configFile = env.FlexibleConfig.BaseTemplate
		cmd = buildDockerKrakenDCommandWithImage(env, "audit", filepath.Join(cwd, configFile), dockerImage)
	} else {
		// Create temporary file for standard config
		if tempDir == "" {
			tempDir = os.TempDir()
		}

		tempFile := filepath.Join(tempDir, "krakend-audit-config.json")
		if err := os.WriteFile(tempFile, []byte(configJSON), 0600); err != nil {
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}
		defer os.Remove(tempFile)

		// Run docker with krakend audit using version-specific image
		cmd = exec.Command("docker", "run", "--rm",
			"-v", fmt.Sprintf("%s:/etc/krakend/krakend.json:ro", tempFile),
			dockerImage,
			"audit", "-c", "/etc/krakend/krakend.json")
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	result := &AuditSecurityOutput{
		Method:      fmt.Sprintf("docker (%s)", dockerImage),
		Issues:      []SecurityIssue{},
		Environment: env,
	}

	err := cmd.Run()
	output := stdout.String() + stderr.String()

	if err != nil && output == "" {
		return nil, fmt.Errorf("docker audit command failed: %w", err)
	}

	// Parse output similar to native
	if strings.Contains(output, "CRITICAL") || strings.Contains(output, "HIGH") {
		result.Valid = false
	} else {
		result.Valid = true
	}

	// Basic parsing
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		severity := "info"
		if strings.Contains(strings.ToUpper(line), "CRITICAL") {
			severity = "critical"
		} else if strings.Contains(strings.ToUpper(line), "HIGH") {
			severity = "high"
		} else if strings.Contains(strings.ToUpper(line), "MEDIUM") {
			severity = "medium"
		} else if strings.Contains(strings.ToUpper(line), "LOW") {
			severity = "low"
		}

		if severity != "info" {
			result.Issues = append(result.Issues, SecurityIssue{
				Severity:    severity,
				Category:    "security",
				Title:       line,
				Description: line,
				Remediation: "Review KrakenD security documentation",
			})
		}
	}

	result.Summary = fmt.Sprintf("Security audit completed with %d issue(s) found (Docker %s)", len(result.Issues), dockerImage)
	return result, nil
}

// auditWithBasicChecks performs basic security checks (fallback)
func auditWithBasicChecks(configJSON string) (*AuditSecurityOutput, error) {
	result := &AuditSecurityOutput{
		Method: "basic",
		Issues: []SecurityIssue{},
		Valid:  true,
	}

	var config map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Check 1: Missing CORS configuration
	extraConfig, hasExtraConfig := config["extra_config"].(map[string]interface{})
	if !hasExtraConfig || extraConfig["security/cors"] == nil {
		result.Issues = append(result.Issues, SecurityIssue{
			Severity:    "medium",
			Category:    "cors",
			Title:       "Missing CORS configuration",
			Description: "No CORS configuration found. This may cause issues with browser-based clients.",
			Location:    "$.extra_config['security/cors']",
			Remediation: "Add CORS configuration with appropriate allow_origins, allow_methods, and allow_headers",
			References:  []string{"https://www.krakend.io/docs/service-settings/cors/"},
		})
	}

	// Check 2: No authentication on endpoints
	endpoints, hasEndpoints := config["endpoints"].([]interface{})
	if hasEndpoints {
		for i, ep := range endpoints {
			endpoint, ok := ep.(map[string]interface{})
			if !ok {
				continue
			}

			epExtraConfig, hasEpExtra := endpoint["extra_config"].(map[string]interface{})
			hasAuth := hasEpExtra && (epExtraConfig["auth/validator"] != nil || epExtraConfig["auth/api-keys"] != nil)

			if !hasAuth {
				method := "GET"
				if m, ok := endpoint["method"].(string); ok {
					method = m
				}

				// Warn especially for non-GET endpoints
				if method != "GET" {
					result.Issues = append(result.Issues, SecurityIssue{
						Severity:    "high",
						Category:    "authentication",
						Title:       fmt.Sprintf("No authentication on %s endpoint", method),
						Description: fmt.Sprintf("Endpoint %d has no authentication configured", i),
						Location:    fmt.Sprintf("$.endpoints[%d]", i),
						Remediation: "Add JWT validation or API key authentication to protect this endpoint",
						References: []string{
							"https://www.krakend.io/docs/authorization/jwt-validation/",
							"https://www.krakend.io/docs/enterprise/authentication/api-keys/",
						},
					})
				}
			}
		}
	}

	// Check 3: No rate limiting
	hasRateLimitGlobal := hasExtraConfig && extraConfig["qos/ratelimit/service"] != nil
	hasRateLimitEndpoint := false

	if hasEndpoints {
		for _, ep := range endpoints {
			endpoint, ok := ep.(map[string]interface{})
			if !ok {
				continue
			}
			epExtraConfig, hasEpExtra := endpoint["extra_config"].(map[string]interface{})
			if hasEpExtra && epExtraConfig["qos/ratelimit/router"] != nil {
				hasRateLimitEndpoint = true
				break
			}
		}
	}

	if !hasRateLimitGlobal && !hasRateLimitEndpoint {
		result.Issues = append(result.Issues, SecurityIssue{
			Severity:    "medium",
			Category:    "rate-limiting",
			Title:       "No rate limiting configured",
			Description: "No rate limiting found at service or endpoint level. This exposes the API to abuse.",
			Remediation: "Add rate limiting at service or endpoint level to prevent abuse",
			References:  []string{"https://www.krakend.io/docs/endpoints/rate-limit/"},
		})
	}

	// Check 4: Debug endpoint enabled
	if debug, ok := config["debug_endpoint"].(bool); ok && debug {
		result.Issues = append(result.Issues, SecurityIssue{
			Severity:    "high",
			Category:    "exposure",
			Title:       "Debug endpoint enabled",
			Description: "The /__debug/ endpoint is enabled. This exposes internal metrics and should not be used in production.",
			Location:    "$.debug_endpoint",
			Remediation: "Set debug_endpoint to false or remove it in production",
			References:  []string{"https://www.krakend.io/docs/service-settings/debug-endpoint/"},
		})
		result.Valid = false
	}

	// Determine validity based on severity
	for _, issue := range result.Issues {
		if issue.Severity == "critical" || issue.Severity == "high" {
			result.Valid = false
			break
		}
	}

	result.Summary = fmt.Sprintf("Basic security audit completed with %d issue(s) found (fallback mode - install KrakenD for comprehensive audit)", len(result.Issues))

	return result, nil
}
