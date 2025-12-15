package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	// ValidationGuidance provides strict instructions to prevent LLM hallucinations
	ValidationGuidance = "IMPORTANT: The errors and warnings listed above are the COMPLETE and AUTHORITATIVE validation results from KrakenD. Do NOT suggest additional fixes based on assumptions, patterns, or intuition. ONLY fix the errors explicitly listed in this output. If you are unsure about correct KrakenD syntax or configuration, use the search_documentation tool to verify against official documentation before making any suggestions."
)

// ValidationEnvironment represents the available validation methods
type ValidationEnvironment struct {
	HasNativeKrakenD   bool
	HasDocker          bool
	DockerVersion      string
	FlexibleConfig     *FlexibleConfigInfo
}

// FlexibleConfigInfo represents Flexible Configuration detection results
type FlexibleConfigInfo struct {
	Detected       bool     `json:"detected"`
	Type           string   `json:"type"`           // "ce" or "ee"
	BaseTemplate   string   `json:"base_template"`  // e.g., "krakend.tmpl" or "krakend.json"
	BehavioralFile string   `json:"behavioral_file,omitempty"` // "flexible_config.json" (EE only)
	SettingsDir    string   `json:"settings_dir,omitempty"`
	TemplatesDir   string   `json:"templates_dir,omitempty"`
	PartialsDir    string   `json:"partials_dir,omitempty"`
	Explanation    string   `json:"explanation"`
	Implications   []string `json:"implications"`
}

// isFilePath determines if a string is a file path rather than JSON content
// Returns true if it looks like a path, false if it looks like JSON
func isFilePath(s string) bool {
	// Empty string is not a file path
	if s == "" {
		return false
	}

	// JSON content starts with { or [ (ignoring whitespace)
	trimmed := strings.TrimSpace(s)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		return false
	}

	// Check for common path patterns that strongly indicate a file path
	// Even if the file doesn't exist, we want to try reading it and give a clear error

	// Unix absolute path
	if strings.HasPrefix(s, "/") {
		return true
	}

	// Relative path
	if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
		return true
	}

	// Windows absolute path (C:\, D:\, etc.)
	if len(s) >= 3 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
	}

	// File name with .json extension (no newlines, looks like a filename)
	if strings.HasSuffix(s, ".json") && !strings.Contains(s, "\n") {
		return true
	}

	return false
}

// DetectEnvironment checks what validation methods are available
func DetectEnvironment() *ValidationEnvironment {
	env := &ValidationEnvironment{}

	// Check for native krakend binary
	if _, err := exec.LookPath("krakend"); err == nil {
		env.HasNativeKrakenD = true
	}

	// Check for Docker
	if output, err := exec.Command("docker", "--version").CombinedOutput(); err == nil {
		env.HasDocker = true
		env.DockerVersion = strings.TrimSpace(string(output))
	}

	// Check for Flexible Configuration
	env.FlexibleConfig = DetectFlexibleConfiguration()

	return env
}

// DetectFlexibleConfiguration detects if project uses Flexible Configuration
func DetectFlexibleConfiguration() *FlexibleConfigInfo {
	fc := &FlexibleConfigInfo{
		Detected: false,
		Implications: []string{},
	}

	// Check for EE Extended Flexible Configuration first (more specific)
	if _, err := os.Stat("flexible_config.json"); err == nil {
		fc.Detected = true
		fc.Type = "ee"
		fc.BehavioralFile = "flexible_config.json"
		fc.Explanation = "Extended Flexible Configuration (Enterprise Edition) detected via flexible_config.json behavioral file."

		// Try to read behavioral file to get paths
		if data, err := os.ReadFile("flexible_config.json"); err == nil {
			var behavior map[string]interface{}
			if json.Unmarshal(data, &behavior) == nil {
				// Extract settings paths
				if settings, ok := behavior["settings"].(map[string]interface{}); ok {
					if paths, ok := settings["paths"].([]interface{}); ok && len(paths) > 0 {
						if firstPath, ok := paths[0].(string); ok {
							fc.SettingsDir = firstPath
						}
					}
				}
				// Extract templates paths
				if templates, ok := behavior["templates"].(map[string]interface{}); ok {
					if paths, ok := templates["paths"].([]interface{}); ok && len(paths) > 0 {
						if firstPath, ok := paths[0].(string); ok {
							fc.TemplatesDir = firstPath
						}
					}
				}
				// Extract partials paths
				if partials, ok := behavior["partials"].(map[string]interface{}); ok {
					if paths, ok := partials["paths"].([]interface{}); ok && len(paths) > 0 {
						if firstPath, ok := paths[0].(string); ok {
							fc.PartialsDir = firstPath
						}
					}
				}
			}
		}

		// Detect base template (typically krakend.json for EE)
		if _, err := os.Stat("krakend.json"); err == nil {
			fc.BaseTemplate = "krakend.json"
		} else if _, err := os.Stat("krakend.tmpl"); err == nil {
			fc.BaseTemplate = "krakend.tmpl"
		}

		fc.Implications = []string{
			"Configuration uses Enterprise Edition Extended Flexible Configuration",
			"Commands run normally without environment variables (behavioral file handles everything)",
			"Configuration file is: " + fc.BaseTemplate,
			"Settings are loaded from paths defined in flexible_config.json",
			"Supports multiple file formats: JSON, YAML, TOML, INI, ENV, properties",
			"Use 'out.json' (if configured) to see compiled configuration for debugging",
		}

		return fc
	}

	// Check for CE Flexible Configuration
	// Look for .tmpl files or typical FC directory structure
	hasTmplFile := false
	var tmplFile string

	// Check for krakend.tmpl specifically
	if _, err := os.Stat("krakend.tmpl"); err == nil {
		hasTmplFile = true
		tmplFile = "krakend.tmpl"
	} else {
		// Check for any .tmpl files in current directory
		files, _ := filepath.Glob("*.tmpl")
		if len(files) > 0 {
			hasTmplFile = true
			tmplFile = files[0]
		}
	}

	// Check for typical FC directory structure
	hasSettingsDir := false
	var settingsDir string
	if info, err := os.Stat("config/settings"); err == nil && info.IsDir() {
		hasSettingsDir = true
		settingsDir = "config/settings"
	} else if info, err := os.Stat("settings"); err == nil && info.IsDir() {
		hasSettingsDir = true
		settingsDir = "settings"
	}

	hasTemplatesDir := false
	var templatesDir string
	if info, err := os.Stat("config/templates"); err == nil && info.IsDir() {
		hasTemplatesDir = true
		templatesDir = "config/templates"
	} else if info, err := os.Stat("templates"); err == nil && info.IsDir() {
		hasTemplatesDir = true
		templatesDir = "templates"
	}

	var partialsDir string
	if info, err := os.Stat("config/partials"); err == nil && info.IsDir() {
		partialsDir = "config/partials"
	} else if info, err := os.Stat("partials"); err == nil && info.IsDir() {
		partialsDir = "partials"
	}

	// If we found .tmpl files or FC directory structure, it's likely CE FC
	if hasTmplFile || (hasSettingsDir && hasTemplatesDir) {
		fc.Detected = true
		fc.Type = "ce"
		fc.BaseTemplate = tmplFile
		fc.SettingsDir = settingsDir
		fc.TemplatesDir = templatesDir
		fc.PartialsDir = partialsDir
		fc.Explanation = "Flexible Configuration (Community Edition) detected via .tmpl files and/or config directory structure."

		fc.Implications = []string{
			"Configuration uses Community Edition Flexible Configuration",
			"Commands require environment variables: FC_ENABLE=1, FC_SETTINGS, FC_TEMPLATES, FC_PARTIALS",
			"Base template file: " + fc.BaseTemplate,
		}

		if fc.SettingsDir != "" {
			fc.Implications = append(fc.Implications, "Settings directory: "+fc.SettingsDir)
		}
		if fc.TemplatesDir != "" {
			fc.Implications = append(fc.Implications, "Templates directory: "+fc.TemplatesDir)
		}
		if fc.PartialsDir != "" {
			fc.Implications = append(fc.Implications, "Partials directory: "+fc.PartialsDir)
		}

		fc.Implications = append(fc.Implications, "Use FC_OUT=out.json to generate compiled configuration for debugging")

		return fc
	}

	// No FC detected
	fc.Explanation = "No Flexible Configuration detected. Using standard krakend.json configuration."
	return fc
}

// ExtractVersionFromConfig extracts the KrakenD version from $schema field
func ExtractVersionFromConfig(configJSON string) string {
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return "latest"
	}

	schema, ok := config["$schema"].(string)
	if !ok || schema == "" {
		return "latest"
	}

	// Parse: https://www.krakend.io/schema/v2.12/krakend.json → "2.12"
	// Parse: https://www.krakend.io/schema/krakend.json → "latest"
	if strings.Contains(schema, "/v") {
		parts := strings.Split(schema, "/")
		for i, part := range parts {
			if strings.HasPrefix(part, "v") && i+1 < len(parts) {
				return strings.TrimPrefix(part, "v")
			}
		}
	}

	return "latest"
}

// GetLocalKrakenDVersion gets the version of local krakend binary
func GetLocalKrakenDVersion() (string, error) {
	cmd := exec.Command("krakend", "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	// Parse output like "KrakenD Version: 2.7.0" → "2.7"
	outputStr := string(output)
	versionRegex := regexp.MustCompile(`(?i)version[:\s]+v?(\d+\.\d+)`)
	matches := versionRegex.FindStringSubmatch(outputStr)
	if len(matches) > 1 {
		return matches[1], nil
	}

	return "", fmt.Errorf("could not parse version from: %s", outputStr)
}

// detectEnterpriseFeatures checks if config uses EE-only features (internal helper)
// Reuses existing edition detection infrastructure from features.go
func detectEnterpriseFeatures(configJSON string) bool {
	// Ensure feature data is loaded
	if editionMatrix == nil || featureCatalog == nil {
		if err := LoadFeatureData(); err != nil {
			return false
		}
	}

	// Parse config
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return false
	}

	// Find namespaces in config (reuses helper from features.go)
	namespaces := findNamespacesInConfig(config)

	// Check against EE-only features from edition matrix
	for _, ns := range namespaces {
		for _, eeNs := range editionMatrix.EEOnlyFeatures {
			if ns == eeNs {
				return true
			}
		}
	}

	return false
}

// buildKrakenDCommand constructs a KrakenD command with FC support if detected
func buildKrakenDCommand(env *ValidationEnvironment, command string, configFile string) *exec.Cmd {
	fc := env.FlexibleConfig

	// Build base arguments with linting for check command
	args := []string{command, "-c", configFile}
	if command == "check" {
		args = append(args, "-l") // Enable linting for detailed error locations
	}

	// If FC not detected, use normal command
	if fc == nil || !fc.Detected {
		return exec.Command("krakend", args...)
	}

	// EE Extended FC: commands run normally (behavioral file handles everything)
	if fc.Type == "ee" {
		// Use base template if available
		if fc.BaseTemplate != "" {
			args[2] = fc.BaseTemplate // Replace configFile with BaseTemplate
		}
		return exec.Command("krakend", args...)
	}

	// CE FC: requires environment variables
	cmd := exec.Command("krakend", args...)

	// Set FC environment variables
	cmd.Env = os.Environ() // Start with current environment

	cmd.Env = append(cmd.Env, "FC_ENABLE=1")

	if fc.SettingsDir != "" {
		cmd.Env = append(cmd.Env, "FC_SETTINGS="+fc.SettingsDir)
	}
	if fc.TemplatesDir != "" {
		cmd.Env = append(cmd.Env, "FC_TEMPLATES="+fc.TemplatesDir)
	}
	if fc.PartialsDir != "" {
		cmd.Env = append(cmd.Env, "FC_PARTIALS="+fc.PartialsDir)
	}

	// Add FC_OUT for debugging
	cmd.Env = append(cmd.Env, "FC_OUT=out.json")

	return cmd
}

// buildDockerKrakenDCommand constructs a Docker KrakenD command with FC support
func buildDockerKrakenDCommand(env *ValidationEnvironment, command string, configFile string) *exec.Cmd {
	fc := env.FlexibleConfig

	// Base Docker args
	dockerArgs := []string{
		"run", "--rm",
		"-v", fmt.Sprintf("%s:/etc/krakend", filepath.Dir(configFile)),
	}

	// If FC not detected or EE FC, use simple Docker command
	if fc == nil || !fc.Detected || fc.Type == "ee" {
		// EE or no FC: standard docker command
		image := "krakend:latest"
		if fc != nil && fc.Type == "ee" {
			image = "krakend/krakend-ee:latest"
		}

		dockerArgs = append(dockerArgs, image, command, "-c", "/etc/krakend/"+filepath.Base(configFile))
		if command == "check" {
			dockerArgs = append(dockerArgs, "-l") // Enable linting for detailed error locations
		}
		return exec.Command("docker", dockerArgs...)
	}

	// CE FC: add environment variables
	if fc.SettingsDir != "" {
		dockerArgs = append(dockerArgs, "-e", "FC_ENABLE=1")
		dockerArgs = append(dockerArgs, "-e", "FC_SETTINGS=/etc/krakend/"+fc.SettingsDir)
	}
	if fc.TemplatesDir != "" {
		dockerArgs = append(dockerArgs, "-e", "FC_TEMPLATES=/etc/krakend/"+fc.TemplatesDir)
	}
	if fc.PartialsDir != "" {
		dockerArgs = append(dockerArgs, "-e", "FC_PARTIALS=/etc/krakend/"+fc.PartialsDir)
	}
	dockerArgs = append(dockerArgs, "-e", "FC_OUT=/etc/krakend/out.json")

	// Add image and command
	dockerArgs = append(dockerArgs, "krakend:latest", command, "-c", "/etc/krakend/"+filepath.Base(configFile))
	if command == "check" {
		dockerArgs = append(dockerArgs, "-l") // Enable linting for detailed error locations
	}

	return exec.Command("docker", dockerArgs...)
}

// buildDockerKrakenDCommandWithImage constructs a Docker KrakenD command with custom image
func buildDockerKrakenDCommandWithImage(env *ValidationEnvironment, command string, configFile string, dockerImage string) *exec.Cmd {
	fc := env.FlexibleConfig

	// Base Docker args
	dockerArgs := []string{
		"run", "--rm",
		"-v", fmt.Sprintf("%s:/etc/krakend", filepath.Dir(configFile)),
	}

	// If FC not detected or EE FC, use simple Docker command
	if fc == nil || !fc.Detected || fc.Type == "ee" {
		dockerArgs = append(dockerArgs, dockerImage, command, "-c", "/etc/krakend/"+filepath.Base(configFile))
		if command == "check" {
			dockerArgs = append(dockerArgs, "-l") // Enable linting for detailed error locations
		}
		return exec.Command("docker", dockerArgs...)
	}

	// CE FC: add environment variables
	if fc.SettingsDir != "" {
		dockerArgs = append(dockerArgs, "-e", "FC_ENABLE=1")
		dockerArgs = append(dockerArgs, "-e", "FC_SETTINGS=/etc/krakend/"+fc.SettingsDir)
	}
	if fc.TemplatesDir != "" {
		dockerArgs = append(dockerArgs, "-e", "FC_TEMPLATES=/etc/krakend/"+fc.TemplatesDir)
	}
	if fc.PartialsDir != "" {
		dockerArgs = append(dockerArgs, "-e", "FC_PARTIALS=/etc/krakend/"+fc.PartialsDir)
	}
	dockerArgs = append(dockerArgs, "-e", "FC_OUT=/etc/krakend/out.json")

	// Add custom image and command
	dockerArgs = append(dockerArgs, dockerImage, command, "-c", "/etc/krakend/"+filepath.Base(configFile))
	if command == "check" {
		dockerArgs = append(dockerArgs, "-l") // Enable linting for detailed error locations
	}

	return exec.Command("docker", dockerArgs...)
}

// ValidationResult represents the result of configuration validation
type ValidationResult struct {
	Valid       bool                `json:"valid"`
	Method      string              `json:"method"`       // "native", "docker", or "schema"
	Errors      []ValidationError   `json:"errors"`
	Warnings    []ValidationWarning `json:"warnings"`
	Summary     string              `json:"summary"`
	Guidance    string              `json:"guidance,omitempty"` // Instructions for LLM to prevent hallucinations
	Environment *ValidationEnvironment `json:"environment,omitempty"`
}

// ValidationError represents a validation error with location
type ValidationError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
	Line    int    `json:"line,omitempty"`
	Column  int    `json:"column,omitempty"`
	Code    string `json:"code"`
}

// ValidationWarning represents a validation warning
type ValidationWarning struct {
	Path    string `json:"path"`
	Message string `json:"message"`
	Level   string `json:"level"` // "warning", "info"
}

// ValidateConfigInput defines input for validate_config tool
type ValidateConfigInput struct {
	Config  string `json:"config" jsonschema:"KrakenD configuration as JSON string or file path"`
	TempDir string `json:"temp_dir,omitempty" jsonschema:"Temporary directory for validation (optional)"`
}

// ValidateConfigOutput defines output for validate_config tool
type ValidateConfigOutput struct {
	ValidationResult
}

// ValidateConfig performs complete validation using three-tier fallback
func ValidateConfig(ctx context.Context, req *mcp.CallToolRequest, input ValidateConfigInput) (*mcp.CallToolResult, ValidateConfigOutput, error) {
	env := DetectEnvironment()

	result := ValidationResult{
		Valid:       false,
		Errors:      []ValidationError{},
		Warnings:    []ValidationWarning{},
		Environment: env,
	}

	// Check if input.Config is a file path and read it
	configContent := input.Config
	if isFilePath(input.Config) {
		fileContent, err := os.ReadFile(input.Config)
		if err != nil {
			result.Method = "file_read"
			// Provide a clear, specific error message
			var errMsg string
			if os.IsNotExist(err) {
				errMsg = fmt.Sprintf("Configuration file not found: %s", input.Config)
			} else if os.IsPermission(err) {
				errMsg = fmt.Sprintf("Permission denied reading file: %s", input.Config)
			} else {
				errMsg = fmt.Sprintf("Failed to read configuration file '%s': %s", input.Config, err.Error())
			}

			result.Errors = append(result.Errors, ValidationError{
				Message: errMsg,
				Code:    "FILE_READ_ERROR",
				Path:    input.Config,
			})
			result.Summary = "Configuration file could not be read"
			result.Guidance = "Ensure the file path is correct and the file exists. Use an absolute path or a path relative to the current working directory."
			return nil, ValidateConfigOutput{ValidationResult: result}, nil
		}
		configContent = string(fileContent)
	}

	// First, validate JSON syntax
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(configContent), &config); err != nil {
		result.Method = "syntax"
		result.Errors = append(result.Errors, ValidationError{
			Message: fmt.Sprintf("Invalid JSON: %s", err.Error()),
			Code:    "INVALID_JSON",
		})
		result.Summary = "Configuration has JSON syntax errors"
		return nil, ValidateConfigOutput{ValidationResult: result}, nil
	}

	// Extract target version from config
	targetVersion := ExtractVersionFromConfig(configContent)

	// Version-aware validation with smart fallback

	// Priority 1: Native KrakenD (if version matches or config uses latest)
	if env.HasNativeKrakenD {
		localVersion, err := GetLocalKrakenDVersion()
		if err == nil {
			if targetVersion == "latest" || localVersion == targetVersion {
				// Version matches or config uses latest - use native
				if nativeResult, err := validateWithNativeKrakenD(configContent, input.TempDir); err == nil {
					result = *nativeResult
					return nil, ValidateConfigOutput{ValidationResult: result}, nil
				}
			} else {
				// Version mismatch - add warning and skip to Docker
				result.Warnings = append(result.Warnings, ValidationWarning{
					Message: fmt.Sprintf("Local KrakenD is v%s but config targets v%s. Using Docker for accurate validation.", localVersion, targetVersion),
					Level:   "info",
				})
			}
		}
	}

	// Priority 2: Docker with correct version
	if env.HasDocker {
		// Try version-specific image
		if dockerResult, err := validateWithDockerVersion(configContent, input.TempDir, targetVersion); err == nil {
			result = *dockerResult
			return nil, ValidateConfigOutput{ValidationResult: result}, nil
		}

		// If version-specific failed, try latest
		if targetVersion != "latest" {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Message: fmt.Sprintf("Docker image for v%s not available, trying latest", targetVersion),
				Level:   "info",
			})
			if dockerResult, err := validateWithDockerVersion(configContent, input.TempDir, "latest"); err == nil {
				result = *dockerResult
				return nil, ValidateConfigOutput{ValidationResult: result}, nil
			}
		}
	}

	// Priority 3: Fallback to native even if version mismatch (with warning)
	if env.HasNativeKrakenD {
		if nativeResult, err := validateWithNativeKrakenD(configContent, input.TempDir); err == nil {
			nativeResult.Warnings = append(nativeResult.Warnings, ValidationWarning{
				Message: fmt.Sprintf("Config targets v%s but validating with local version (Docker unavailable)", targetVersion),
				Level:   "warning",
			})
			result = *nativeResult
			return nil, ValidateConfigOutput{ValidationResult: result}, nil
		}
	}

	// Priority 4: Go-based schema validation (last resort)
	schemaResult, err := validateWithSchema(configContent)
	if err != nil {
		result.Method = "schema"
		result.Errors = append(result.Errors, ValidationError{
			Message: fmt.Sprintf("Schema validation failed: %s", err.Error()),
			Code:    "VALIDATION_ERROR",
		})
		result.Summary = "Validation failed (no KrakenD or Docker available, schema validation error)"
		return nil, ValidateConfigOutput{ValidationResult: result}, nil
	}

	result = *schemaResult
	return nil, ValidateConfigOutput{ValidationResult: result}, nil
}

// validateWithNativeKrakenD validates using native krakend binary
func validateWithNativeKrakenD(configJSON string, tempDir string) (*ValidationResult, error) {
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

		tempFile := filepath.Join(tempDir, "krakend-temp-config.json")
		if err := os.WriteFile(tempFile, []byte(configJSON), 0600); err != nil {
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}
		configFile = tempFile
		defer os.Remove(tempFile)
	}

	// Run krakend check with FC support
	cmd := buildKrakenDCommand(env, "check", configFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	result := &ValidationResult{
		Method:      "native",
		Errors:      []ValidationError{},
		Warnings:    []ValidationWarning{},
		Guidance:    ValidationGuidance,
		Environment: env,
	}

	// Add FC info to result if detected
	if env.FlexibleConfig != nil && env.FlexibleConfig.Detected {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Message: env.FlexibleConfig.Explanation,
			Level:   "info",
		})
		for _, implication := range env.FlexibleConfig.Implications {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Message: implication,
				Level:   "info",
			})
		}
	}

	err := cmd.Run()
	if err != nil {
		// Parse krakend check output for errors
		output := stderr.String() + stdout.String()
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Message: output,
			Code:    "KRAKEND_CHECK_FAILED",
		})
		result.Summary = "KrakenD validation failed (native)"
		return result, nil
	}

	result.Valid = true
	result.Summary = "Configuration is valid (validated with native KrakenD)"
	return result, nil
}

// validateWithDocker validates using Docker container
func validateWithDocker(configJSON string, tempDir string) (*ValidationResult, error) {
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
		cmd = buildDockerKrakenDCommand(env, "check", filepath.Join(cwd, configFile))
	} else {
		// Create temporary file for standard config
		if tempDir == "" {
			tempDir = os.TempDir()
		}

		tempFile := filepath.Join(tempDir, "krakend-temp-config.json")
		if err := os.WriteFile(tempFile, []byte(configJSON), 0600); err != nil {
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}
		defer os.Remove(tempFile)

		// Run docker with krakend check (standard)
		cmd = exec.Command("docker", "run", "--rm",
			"-v", fmt.Sprintf("%s:/etc/krakend/krakend.json:ro", tempFile),
			"krakend:latest",
			"check", "-c", "/etc/krakend/krakend.json", "-l")
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	result := &ValidationResult{
		Method:      "docker",
		Errors:      []ValidationError{},
		Warnings:    []ValidationWarning{},
		Guidance:    ValidationGuidance,
		Environment: env,
	}

	// Add FC info to result if detected
	if env.FlexibleConfig != nil && env.FlexibleConfig.Detected {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Message: env.FlexibleConfig.Explanation,
			Level:   "info",
		})
		for _, implication := range env.FlexibleConfig.Implications {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Message: implication,
				Level:   "info",
			})
		}
	}

	err := cmd.Run()
	if err != nil {
		// Parse docker/krakend output
		output := stderr.String() + stdout.String()
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Message: output,
			Code:    "KRAKEND_CHECK_FAILED",
		})
		result.Summary = "KrakenD validation failed (Docker)"
		return result, nil
	}

	result.Valid = true
	result.Summary = "Configuration is valid (validated with KrakenD via Docker)"
	return result, nil
}

// validateWithDockerVersion validates using Docker with specific KrakenD version
func validateWithDockerVersion(configJSON string, tempDir string, targetVersion string) (*ValidationResult, error) {
	env := DetectEnvironment()

	// Detect if EE features are used (reuses existing edition detection)
	isEE := detectEnterpriseFeatures(configJSON)

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
		cmd = buildDockerKrakenDCommandWithImage(env, "check", filepath.Join(cwd, configFile), dockerImage)
	} else {
		// Create temporary file for standard config
		if tempDir == "" {
			tempDir = os.TempDir()
		}

		tempFile := filepath.Join(tempDir, "krakend-temp-config.json")
		if err := os.WriteFile(tempFile, []byte(configJSON), 0600); err != nil {
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}
		defer os.Remove(tempFile)

		// Run docker with krakend check using version-specific image
		cmd = exec.Command("docker", "run", "--rm",
			"-v", fmt.Sprintf("%s:/etc/krakend/krakend.json:ro", tempFile),
			dockerImage,
			"check", "-c", "/etc/krakend/krakend.json", "-l")
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	result := &ValidationResult{
		Method:      fmt.Sprintf("docker (%s)", dockerImage),
		Errors:      []ValidationError{},
		Warnings:    []ValidationWarning{},
		Guidance:    ValidationGuidance,
		Environment: env,
	}

	// Add FC info to result if detected
	if env.FlexibleConfig != nil && env.FlexibleConfig.Detected {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Message: env.FlexibleConfig.Explanation,
			Level:   "info",
		})
		for _, implication := range env.FlexibleConfig.Implications {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Message: implication,
				Level:   "info",
			})
		}
	}

	// Add edition info if EE features detected
	if isEE {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Message: fmt.Sprintf("Enterprise Edition features detected, using %s image", dockerImage),
			Level:   "info",
		})
	}

	err := cmd.Run()
	if err != nil {
		// Parse docker/krakend output
		output := stderr.String() + stdout.String()
		result.Valid = false
		result.Errors = append(result.Errors, ValidationError{
			Message: output,
			Code:    "KRAKEND_CHECK_FAILED",
		})
		result.Summary = fmt.Sprintf("KrakenD validation failed (Docker %s)", dockerImage)
		return result, nil
	}

	result.Valid = true
	result.Summary = fmt.Sprintf("Configuration is valid (validated with %s)", dockerImage)
	return result, nil
}

// validateWithSchema validates using version-specific JSON Schema (fallback)
func validateWithSchema(configJSON string) (*ValidationResult, error) {
	env := DetectEnvironment()

	result := &ValidationResult{
		Method:      "schema",
		Errors:      []ValidationError{},
		Warnings:    []ValidationWarning{},
		Guidance:    ValidationGuidance,
		Environment: env,
	}

	var config map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Extract version for schema URL
	targetVersion := ExtractVersionFromConfig(configJSON)

	// Build schema URL
	schemaURL := "https://www.krakend.io/schema/krakend.json"
	if targetVersion != "latest" {
		schemaURL = fmt.Sprintf("https://www.krakend.io/schema/v%s/krakend.json", targetVersion)
	}

	// Try to download schema
	schemaContent, err := downloadSchema(schemaURL)
	if err != nil {
		// Fallback to basic validation if schema download fails
		result.Warnings = append(result.Warnings, ValidationWarning{
			Message: fmt.Sprintf("Could not download schema from %s, using basic validation", schemaURL),
			Level:   "warning",
		})
		return validateBasicSchema(configJSON)
	}

	// Compile the JSON schema
	compiler := jsonschema.NewCompiler()
	// Note: Compiler auto-detects draft version from $schema field in the schema

	// Parse schema
	var schemaDoc interface{}
	if err := json.Unmarshal(schemaContent, &schemaDoc); err != nil {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Message: fmt.Sprintf("Downloaded schema is invalid: %s, using basic validation", err.Error()),
			Level:   "warning",
		})
		return validateBasicSchema(configJSON)
	}

	// Add schema to compiler
	if err := compiler.AddResource(schemaURL, schemaDoc); err != nil {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Message: fmt.Sprintf("Failed to compile schema: %s, using basic validation", err.Error()),
			Level:   "warning",
		})
		return validateBasicSchema(configJSON)
	}

	// Compile the schema
	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		result.Warnings = append(result.Warnings, ValidationWarning{
			Message: fmt.Sprintf("Schema compilation error: %s, using basic validation", err.Error()),
			Level:   "warning",
		})
		return validateBasicSchema(configJSON)
	}

	// Validate config against schema
	if err := schema.Validate(config); err != nil {
		// Schema validation failed - parse errors
		result.Valid = false

		if validationErr, ok := err.(*jsonschema.ValidationError); ok {
			// Parse validation errors from jsonschema library
			result.Errors = parseSchemaValidationErrors(validationErr)
		} else {
			// Generic error
			result.Errors = append(result.Errors, ValidationError{
				Message: err.Error(),
				Code:    "SCHEMA_VALIDATION_ERROR",
			})
		}

		result.Summary = fmt.Sprintf("Configuration validation failed with %d error(s) (JSON Schema v%s)", len(result.Errors), targetVersion)
	} else {
		// Validation passed
		result.Valid = true
		result.Summary = fmt.Sprintf("Configuration is valid according to JSON Schema v%s (fallback mode - install KrakenD for runtime validation)", targetVersion)
		result.Warnings = append(result.Warnings, ValidationWarning{
			Message: "Using JSON Schema validation. Install KrakenD binary or Docker for comprehensive runtime validation.",
			Level:   "info",
		})
	}

	return result, nil
}

// parseSchemaValidationErrors converts jsonschema validation errors to our format
func parseSchemaValidationErrors(validationErr *jsonschema.ValidationError) []ValidationError {
	var errors []ValidationError

	// Build JSON path from InstanceLocation
	path := "$"
	if len(validationErr.InstanceLocation) > 0 {
		path = "$." + strings.Join(validationErr.InstanceLocation, ".")
	}

	// Process main error using Error() method
	errorMsg := validationErr.Error()
	if errorMsg != "" {
		errors = append(errors, ValidationError{
			Path:    path,
			Message: errorMsg,
			Code:    "SCHEMA_VALIDATION_ERROR",
		})
	}

	// Process nested errors recursively
	for _, cause := range validationErr.Causes {
		errors = append(errors, parseSchemaValidationErrors(cause)...)
	}

	return errors
}

// downloadSchema downloads JSON schema with timeout
func downloadSchema(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("schema download failed: HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

// validateBasicSchema performs basic schema validation
func validateBasicSchema(configJSON string) (*ValidationResult, error) {
	result := &ValidationResult{
		Method:   "schema",
		Errors:   []ValidationError{},
		Warnings: []ValidationWarning{},
	}

	var config map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Check for required top-level fields
	requiredFields := []string{"version", "endpoints"}
	for _, field := range requiredFields {
		if _, ok := config[field]; !ok {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Path:    fmt.Sprintf("$.%s", field),
				Message: fmt.Sprintf("Missing required field: %s", field),
				Code:    "MISSING_REQUIRED_FIELD",
			})
		}
	}

	// Check version field type
	if version, ok := config["version"]; ok {
		if _, isInt := version.(float64); !isInt {
			result.Warnings = append(result.Warnings, ValidationWarning{
				Path:    "$.version",
				Message: "Version should be a number",
				Level:   "warning",
			})
		}
	}

	// Check endpoints is an array
	if endpoints, ok := config["endpoints"]; ok {
		if _, isArray := endpoints.([]interface{}); !isArray {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationError{
				Path:    "$.endpoints",
				Message: "Endpoints must be an array",
				Code:    "INVALID_TYPE",
			})
		}
	}

	if len(result.Errors) == 0 {
		result.Valid = true
		result.Summary = "Basic schema validation passed (fallback mode - install KrakenD for complete validation)"
		result.Warnings = append(result.Warnings, ValidationWarning{
			Message: "Using fallback validation. Install KrakenD binary or Docker for comprehensive validation.",
			Level:   "info",
		})
	} else {
		result.Summary = fmt.Sprintf("Schema validation failed with %d error(s)", len(result.Errors))
	}

	result.Guidance = ValidationGuidance

	return result, nil
}

// SecurityIssue represents a security vulnerability or concern
type SecurityIssue struct {
	Severity    string `json:"severity"`    // "critical", "high", "medium", "low", "info"
	Category    string `json:"category"`    // "authentication", "cors", "headers", "ssl", "exposure", etc.
	Title       string `json:"title"`
	Description string `json:"description"`
	Location    string `json:"location,omitempty"`    // JSON path if applicable
	Remediation string `json:"remediation"`
	References  []string `json:"references,omitempty"`
}

// AuditSecurityInput defines input for audit_security tool
type AuditSecurityInput struct {
	Config string `json:"config" jsonschema:"KrakenD configuration as JSON string or file path"`
}

// AuditSecurityOutput defines output for audit_security tool
type AuditSecurityOutput struct {
	Valid  bool            `json:"valid"`
	Method string          `json:"method"` // "native", "docker", or "basic"
	Issues []SecurityIssue `json:"issues"`
	Summary string         `json:"summary"`
	Score   int            `json:"score,omitempty"` // 0-100 security score
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
	isEE := detectEnterpriseFeatures(configJSON)

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
						References:  []string{
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
