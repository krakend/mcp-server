package runtime

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/krakend/mcp-server/internal/features"
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
	BehavioralFile string   `json:"behavioral_file,omitempty"`
	SettingsDir    string   `json:"settings_dir,omitempty"`
	TemplatesDir   string   `json:"templates_dir,omitempty"`
	PartialsDir    string   `json:"partials_dir,omitempty"`
	Explanation    string   `json:"explanation"`
	Implications   []string `json:"implications"`
}

// RuntimeInfo contains complete runtime detection information
type RuntimeInfo struct {
	Environment       *ValidationEnvironment `json:"environment"`
	TargetVersion     string                 `json:"target_version"`      // From $schema or "latest"
	ResolvedFrom      string                 `json:"resolved_from,omitempty"` // URL if resolved from schema
	NativeVersion     string                 `json:"native_version,omitempty"`
	IsEnterprise      bool                   `json:"is_enterprise"`
	VersionMatch      bool                   `json:"version_match"`
	RecommendedImage  string                 `json:"recommended_image,omitempty"`
	ExecutionMode     string                 `json:"execution_mode"` // "native", "docker", "docker_recommended", "unavailable"
	Recommendations   []Recommendation       `json:"recommendations"`
}

// Recommendation represents an execution method recommendation
type Recommendation struct {
	Method          string `json:"method"`           // "native" or "docker"
	Priority        int    `json:"priority"`         // 1 = highest
	Reason          string `json:"reason"`
	Warning         string `json:"warning,omitempty"`
	CommandTemplate string `json:"command_template"`
}

// DetectRuntimeInfo performs complete runtime detection
func DetectRuntimeInfo(configJSON string) (*RuntimeInfo, error) {
	env := DetectEnvironment()

	// Extract target version from config
	targetVersion, resolvedFrom := ExtractVersionFromConfig(configJSON)

	// Get native version if available
	nativeVersion := ""
	if env.HasNativeKrakenD {
		if v, err := GetLocalKrakenDVersion(); err == nil {
			nativeVersion = v
		}
	}

	// Detect if enterprise features are used
	isEnterprise := features.DetectEnterpriseFeaturesSimple(configJSON)

	// Determine version match
	versionMatch := false
	if nativeVersion != "" {
		versionMatch = (targetVersion == "latest" || nativeVersion == targetVersion)
	}

	// Determine recommended image
	recommendedImage := ""
	if env.HasDocker {
		if isEnterprise {
			recommendedImage = fmt.Sprintf("krakend/krakend-ee:%s", targetVersion)
		} else {
			recommendedImage = fmt.Sprintf("krakend:%s", targetVersion)
		}
	}

	// Determine execution mode
	executionMode := "unavailable"
	if env.HasDocker && !versionMatch {
		executionMode = "docker_recommended"
	} else if env.HasNativeKrakenD && versionMatch {
		executionMode = "native"
	} else if env.HasDocker {
		executionMode = "docker"
	} else if env.HasNativeKrakenD {
		executionMode = "native"
	}

	// Build recommendations
	recommendations := buildRecommendations(env, targetVersion, nativeVersion, versionMatch, isEnterprise)

	return &RuntimeInfo{
		Environment:      env,
		TargetVersion:    targetVersion,
		ResolvedFrom:     resolvedFrom,
		NativeVersion:    nativeVersion,
		IsEnterprise:     isEnterprise,
		VersionMatch:     versionMatch,
		RecommendedImage: recommendedImage,
		ExecutionMode:    executionMode,
		Recommendations:  recommendations,
	}, nil
}

// buildRecommendations creates ordered list of execution recommendations
func buildRecommendations(env *ValidationEnvironment, targetVersion, nativeVersion string, versionMatch, isEnterprise bool) []Recommendation {
	var recommendations []Recommendation
	priority := 1

	// Scenario 1: Version matches - prefer native
	if env.HasNativeKrakenD && versionMatch {
		recommendations = append(recommendations, Recommendation{
			Method:          "native",
			Priority:        priority,
			Reason:          fmt.Sprintf("Local KrakenD v%s matches config version %s", nativeVersion, targetVersion),
			CommandTemplate: "krakend [command] -c krakend.json",
		})
		priority++

		if env.HasDocker {
			recommendations = append(recommendations, Recommendation{
				Method:          "docker",
				Priority:        priority,
				Reason:          "Alternative using Docker",
				CommandTemplate: buildDockerTemplate(targetVersion, isEnterprise, env.FlexibleConfig),
			})
		}
		return recommendations
	}

	// Scenario 2: Version mismatch but Docker available - prefer Docker
	if env.HasDocker && !versionMatch && env.HasNativeKrakenD {
		recommendations = append(recommendations, Recommendation{
			Method:          "docker",
			Priority:        priority,
			Reason:          fmt.Sprintf("Exact version match available (v%s)", targetVersion),
			CommandTemplate: buildDockerTemplate(targetVersion, isEnterprise, env.FlexibleConfig),
		})
		priority++

		recommendations = append(recommendations, Recommendation{
			Method:          "native",
			Priority:        priority,
			Reason:          fmt.Sprintf("Fallback option (version mismatch: local v%s, config v%s)", nativeVersion, targetVersion),
			Warning:         "Validation may be inaccurate due to version difference",
			CommandTemplate: "krakend [command] -c krakend.json",
		})
		return recommendations
	}

	// Scenario 3: Only Docker available
	if env.HasDocker && !env.HasNativeKrakenD {
		recommendations = append(recommendations, Recommendation{
			Method:          "docker",
			Priority:        priority,
			Reason:          "Native KrakenD not available",
			CommandTemplate: buildDockerTemplate(targetVersion, isEnterprise, env.FlexibleConfig),
		})
		return recommendations
	}

	// Scenario 4: Only native available
	if env.HasNativeKrakenD && !env.HasDocker {
		warning := ""
		if !versionMatch {
			warning = fmt.Sprintf("Version mismatch: local v%s, config v%s", nativeVersion, targetVersion)
		}
		recommendations = append(recommendations, Recommendation{
			Method:          "native",
			Priority:        priority,
			Reason:          "Docker not available",
			Warning:         warning,
			CommandTemplate: "krakend [command] -c krakend.json",
		})
		return recommendations
	}

	return recommendations
}

// buildDockerTemplate builds a Docker command template
func buildDockerTemplate(version string, isEnterprise bool, fc *FlexibleConfigInfo) string {
	image := fmt.Sprintf("krakend:%s", version)
	if isEnterprise {
		image = fmt.Sprintf("krakend/krakend-ee:%s", version)
	}

	// Simple template for now - FC handling can be added later
	return fmt.Sprintf("docker run --rm -v $(pwd):/etc/krakend %s [command] -c /etc/krakend/krakend.json", image)
}

// ExtractVersionFromConfig extracts the KrakenD version from $schema field
func ExtractVersionFromConfig(configJSON string) (version string, resolvedFrom string) {
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return "latest", ""
	}

	schema, ok := config["$schema"].(string)
	if !ok || schema == "" {
		return "latest", ""
	}

	// Parse: https://www.krakend.io/schema/v2.12/krakend.json → "2.12"
	if strings.Contains(schema, "/v") {
		parts := strings.Split(schema, "/")
		for i, part := range parts {
			if strings.HasPrefix(part, "v") && i+1 < len(parts) {
				return strings.TrimPrefix(part, "v"), ""
			}
		}
	}

	// If schema is /schema/krakend.json, resolve the $ref
	if strings.HasSuffix(schema, "/schema/krakend.json") {
		if resolvedVersion := resolveLatestSchemaVersion(schema); resolvedVersion != "" {
			return resolvedVersion, schema
		}
	}

	return "latest", ""
}

// resolveLatestSchemaVersion fetches the schema and resolves the version from $ref
func resolveLatestSchemaVersion(schemaURL string) string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(schemaURL)
	if err != nil {
		return "" // Fallback to "latest"
	}
	defer resp.Body.Close()

	var schema map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&schema); err != nil {
		return ""
	}

	// Parse allOf[0].$ref: "v2.12/krakend.json" → "2.12"
	if allOf, ok := schema["allOf"].([]interface{}); ok && len(allOf) > 0 {
		if ref, ok := allOf[0].(map[string]interface{}); ok {
			if refURL, ok := ref["$ref"].(string); ok {
				if strings.HasPrefix(refURL, "v") {
					parts := strings.Split(refURL, "/")
					if len(parts) > 0 {
						return strings.TrimPrefix(parts[0], "v")
					}
				}
			}
		}
	}

	return ""
}

// DetectEnvironment detects available validation methods
func DetectEnvironment() *ValidationEnvironment {
	env := &ValidationEnvironment{}

	// Check for native KrakenD
	if _, err := exec.LookPath("krakend"); err == nil {
		env.HasNativeKrakenD = true
	}

	// Check for Docker
	if output, err := exec.Command("docker", "--version").CombinedOutput(); err == nil {
		env.HasDocker = true
		env.DockerVersion = strings.TrimSpace(string(output))
	}

	// Check for Flexible Configuration (imported from tools package if needed)
	// For now, leaving this as nil - will be populated by tools/validation.go

	return env
}

// GetLocalKrakenDVersion gets the version of local krakend binary
func GetLocalKrakenDVersion() (string, error) {
	cmd := exec.Command("krakend", "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get KrakenD version: %w", err)
	}

	outputStr := string(output)

	// Parse version from output
	// Example: "Version: 2.7.0" or "KrakenD Version: 2.7.0"
	re := regexp.MustCompile(`[Vv]ersion:?\s+(\d+\.\d+(?:\.\d+)?)`)
	matches := re.FindStringSubmatch(outputStr)
	if len(matches) > 1 {
		return matches[1], nil
	}

	return "", fmt.Errorf("could not parse version from: %s", outputStr)
}
