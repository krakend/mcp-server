package tools

import (
	"os"
	"testing"
)

func TestExtractFirstPath(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		key      string
		expected string
	}{
		{
			name: "valid path",
			data: map[string]interface{}{
				"settings": map[string]interface{}{
					"paths": []interface{}{"settings/"},
				},
			},
			key:      "settings",
			expected: "settings/",
		},
		{
			name: "missing key",
			data: map[string]interface{}{
				"other": map[string]interface{}{},
			},
			key:      "settings",
			expected: "",
		},
		{
			name: "empty paths array",
			data: map[string]interface{}{
				"settings": map[string]interface{}{
					"paths": []interface{}{},
				},
			},
			key:      "settings",
			expected: "",
		},
		{
			name: "non-string path",
			data: map[string]interface{}{
				"settings": map[string]interface{}{
					"paths": []interface{}{123},
				},
			},
			key:      "settings",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractFirstPath(tt.data, tt.key)
			if result != tt.expected {
				t.Errorf("extractFirstPath() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDetectFlexibleConfiguration_NoConfig(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "krakend-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Change to temp directory
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	fc := DetectFlexibleConfiguration()

	if fc.Detected {
		t.Error("Expected Detected=false when no config files present")
	}
}

func TestDetectFlexibleConfiguration_EEConfig(t *testing.T) {
	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "krakend-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Change to temp directory
	origDir, _ := os.Getwd()
	defer os.Chdir(origDir)
	os.Chdir(tmpDir)

	// Create flexible_config.json
	config := `{
		"settings": {
			"paths": ["settings/"]
		},
		"templates": {
			"paths": ["templates/"]
		}
	}`
	os.WriteFile("flexible_config.json", []byte(config), 0644)
	os.WriteFile("krakend.json", []byte("{}"), 0644)

	fc := DetectFlexibleConfiguration()

	if !fc.Detected {
		t.Error("Expected Detected=true when flexible_config.json present")
	}
	if fc.Type != "ee" {
		t.Errorf("Expected Type=ee, got %s", fc.Type)
	}
	if fc.SettingsDir != "settings/" {
		t.Errorf("Expected SettingsDir=settings/, got %s", fc.SettingsDir)
	}
	if fc.TemplatesDir != "templates/" {
		t.Errorf("Expected TemplatesDir=templates/, got %s", fc.TemplatesDir)
	}
	if fc.BaseTemplate != "krakend.json" {
		t.Errorf("Expected BaseTemplate=krakend.json, got %s", fc.BaseTemplate)
	}
}

func TestIsFilePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"absolute path", "/path/to/config.json", true},
		{"relative path", "./config.json", true},
		{"relative path 2", "config.json", true},
		{"json string", `{"version": 3}`, false},
		{"json object", `{`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFilePath(tt.input)
			if result != tt.expected {
				t.Errorf("isFilePath(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExtractVersionFromConfig(t *testing.T) {
	tests := []struct {
		name     string
		config   string
		expected string
	}{
		{
			name:     "version 3",
			config:   `{"version": 3}`,
			expected: "latest",
		},
		{
			name:     "no version",
			config:   `{}`,
			expected: "latest",
		},
		{
			name:     "invalid json",
			config:   `{invalid`,
			expected: "latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractVersionFromConfig(tt.config)
			if result != tt.expected {
				t.Errorf("ExtractVersionFromConfig() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestCreateTempFileRaceCondition(t *testing.T) {
	// Test that os.CreateTemp generates unique filenames
	tmpDir, err := os.MkdirTemp("", "krakend-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create multiple temp files
	files := make([]*os.File, 10)
	names := make(map[string]bool)

	for i := 0; i < 10; i++ {
		f, err := os.CreateTemp(tmpDir, "krakend-*.json")
		if err != nil {
			t.Fatalf("Failed to create temp file: %v", err)
		}
		files[i] = f

		// Check uniqueness
		if names[f.Name()] {
			t.Errorf("Duplicate filename generated: %s", f.Name())
		}
		names[f.Name()] = true
	}

	// Cleanup
	for _, f := range files {
		f.Close()
		os.Remove(f.Name())
	}
}

func TestValidateWithSchema_InvalidJSON(t *testing.T) {
	config := `{invalid json`

	result, err := validateWithSchema(config)

	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
	if result != nil {
		t.Error("Expected nil result for invalid JSON")
	}
}

func TestValidateWithSchema_ValidBasicConfig(t *testing.T) {
	config := `{
		"version": 3,
		"timeout": "3000ms",
		"cache_ttl": "300s",
		"endpoints": []
	}`

	result, err := validateWithSchema(config)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("Expected result, got nil")
	}
	if !result.Valid {
		t.Errorf("Expected valid config, got errors: %v", result.Errors)
	}
}

func TestBuildKrakenDCommand(t *testing.T) {
	env := &ValidationEnvironment{
		HasNativeKrakenD: true,
		FlexibleConfig:   nil,
	}

	cmd := buildKrakenDCommand(env, "check", "/path/to/config.json")

	if cmd == nil {
		t.Fatal("Expected command, got nil")
	}
	if cmd.Path != "krakend" {
		t.Errorf("Expected path=krakend, got %s", cmd.Path)
	}

	args := cmd.Args
	if len(args) < 3 {
		t.Fatalf("Expected at least 3 args, got %d", len(args))
	}
	if args[1] != "check" {
		t.Errorf("Expected args[1]=check, got %s", args[1])
	}
}

func TestBuildKrakenDCommand_WithFlexibleConfig(t *testing.T) {
	env := &ValidationEnvironment{
		HasNativeKrakenD: true,
		FlexibleConfig: &FlexibleConfigInfo{
			Detected:    true,
			Type:        "ce",
			SettingsDir: "settings/",
		},
	}

	cmd := buildKrakenDCommand(env, "check", "/path/to/config.json")

	if cmd == nil {
		t.Fatal("Expected command, got nil")
	}

	// Check that FC_SETTINGS env var is set (for CE type)
	foundSettings := false
	foundEnable := false
	for _, envVar := range cmd.Env {
		if envVar == "FC_SETTINGS=settings/" {
			foundSettings = true
		}
		if envVar == "FC_ENABLE=1" {
			foundEnable = true
		}
	}
	if !foundSettings {
		t.Error("Expected FC_SETTINGS env var to be set")
	}
	if !foundEnable {
		t.Error("Expected FC_ENABLE env var to be set")
	}
}
