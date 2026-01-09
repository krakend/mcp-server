package validation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/krakend/mcp-server/internal/features"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	// ValidationGuidance provides strict instructions to prevent LLM hallucinations
	ValidationGuidance = "IMPORTANT: The errors and warnings listed above are the COMPLETE and AUTHORITATIVE validation results from KrakenD. Do NOT suggest additional fixes based on assumptions, patterns, or intuition. ONLY fix the errors explicitly listed in this output. If you are unsure about correct KrakenD syntax or configuration, use the search_documentation tool to verify against official documentation before making any suggestions."
)

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
	Valid       bool                   `json:"valid"`
	Method      string                 `json:"method"`       // "native", "docker", or "schema"
	Errors      []ValidationError      `json:"errors"`
	Warnings    []ValidationWarning    `json:"warnings"`
	Summary     string                 `json:"summary"`
	Guidance    string                 `json:"guidance,omitempty"` // Instructions for LLM to prevent hallucinations
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

		tmpFile, err := os.CreateTemp(tempDir, "krakend-*.json")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp file: %w", err)
		}
		tempFilePath := tmpFile.Name()
		defer os.Remove(tempFilePath)

		if _, err := tmpFile.Write([]byte(configJSON)); err != nil {
			tmpFile.Close()
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}
		if err := tmpFile.Close(); err != nil {
			return nil, fmt.Errorf("failed to close temp file: %w", err)
		}

		configFile = tempFilePath
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
		result.Valid = false

		// Distinguish between different error types for better user guidance
		if errors.Is(err, exec.ErrNotFound) {
			result.Errors = append(result.Errors, ValidationError{
				Message: "KrakenD binary not found in PATH. Please install KrakenD or ensure it's in your PATH.",
				Code:    "KRAKEND_NOT_FOUND",
			})
			result.Summary = "KrakenD binary not found"
			return result, fmt.Errorf("krakend binary not found: %w", err)
		}

		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			result.Errors = append(result.Errors, ValidationError{
				Message: fmt.Sprintf("Permission denied or path error: %s", pathErr.Error()),
				Code:    "PATH_ERROR",
			})
			result.Summary = "Cannot execute KrakenD"
			return result, fmt.Errorf("krakend execution error: %w", err)
		}

		// Config validation error (krakend ran but found issues)
		output := stderr.String() + stdout.String()
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

// validateWithDockerVersion validates using Docker with specific KrakenD version
func validateWithDockerVersion(configJSON string, tempDir string, targetVersion string) (*ValidationResult, error) {
	env := DetectEnvironment()

	// Detect if EE features are used (reuses existing edition detection)
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
		cmd = buildDockerKrakenDCommandWithImage(env, "check", filepath.Join(cwd, configFile), dockerImage)
	} else {
		// Create temporary file for standard config
		if tempDir == "" {
			tempDir = os.TempDir()
		}

		tmpFile, err := os.CreateTemp(tempDir, "krakend-*.json")
		if err != nil {
			return nil, fmt.Errorf("failed to create temp file: %w", err)
		}
		tempFilePath := tmpFile.Name()
		defer os.Remove(tempFilePath)

		if _, err := tmpFile.Write([]byte(configJSON)); err != nil {
			tmpFile.Close()
			return nil, fmt.Errorf("failed to write temp file: %w", err)
		}
		if err := tmpFile.Close(); err != nil {
			return nil, fmt.Errorf("failed to close temp file: %w", err)
		}

		// Run docker with krakend check using version-specific image
		cmd = exec.Command("docker", "run", "--rm",
			"-v", fmt.Sprintf("%s:/etc/krakend/krakend.json:ro", tempFilePath),
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
