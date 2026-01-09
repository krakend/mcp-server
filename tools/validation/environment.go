package validation

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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

// extractFirstPath extracts the first path string from a nested behavior map
func extractFirstPath(data map[string]interface{}, key string) string {
	if section, ok := data[key].(map[string]interface{}); ok {
		if paths, ok := section["paths"].([]interface{}); ok && len(paths) > 0 {
			if path, ok := paths[0].(string); ok {
				return path
			}
		}
	}
	return ""
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
				fc.SettingsDir = extractFirstPath(behavior, "settings")
				fc.TemplatesDir = extractFirstPath(behavior, "templates")
				fc.PartialsDir = extractFirstPath(behavior, "partials")
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

