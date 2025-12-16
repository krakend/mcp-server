package runtime_test

import (
	"strings"
	"testing"

	"github.com/krakend/mcp-server/internal/runtime"
)

func TestExtractVersionFromConfig(t *testing.T) {
	tests := []struct {
		name         string
		config       string
		wantVersion  string
		wantResolved string
	}{
		{
			name: "version in schema",
			config: `{
				"$schema": "https://www.krakend.io/schema/v2.12/krakend.json",
				"version": 3
			}`,
			wantVersion:  "2.12",
			wantResolved: "",
		},
		{
			name: "latest schema - resolves to specific version",
			config: `{
				"$schema": "https://www.krakend.io/schema/krakend.json",
				"version": 3
			}`,
			wantVersion:  "", // Will be resolved from internet, we don't test exact version
			wantResolved: "https://www.krakend.io/schema/krakend.json",
		},
		{
			name: "no schema",
			config: `{
				"version": 3,
				"endpoints": []
			}`,
			wantVersion:  "latest",
			wantResolved: "",
		},
		{
			name:         "invalid json",
			config:       `{invalid}`,
			wantVersion:  "latest",
			wantResolved: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVersion, gotResolved := runtime.ExtractVersionFromConfig(tt.config)

			// If wantVersion is empty, just check it resolved to something other than "latest"
			if tt.wantVersion == "" {
				if gotVersion == "latest" {
					t.Errorf("ExtractVersionFromConfig() version = latest, expected it to be resolved")
				}
				t.Logf("Resolved version: %s", gotVersion)
			} else {
				if gotVersion != tt.wantVersion {
					t.Errorf("ExtractVersionFromConfig() version = %v, want %v", gotVersion, tt.wantVersion)
				}
			}

			if gotResolved != tt.wantResolved {
				t.Errorf("ExtractVersionFromConfig() resolved = %v, want %v", gotResolved, tt.wantResolved)
			}
		})
	}
}

func TestDetectRuntimeInfo(t *testing.T) {
	config := `{
		"$schema": "https://www.krakend.io/schema/v2.12/krakend.json",
		"version": 3,
		"endpoints": []
	}`

	info, err := runtime.DetectRuntimeInfo(config)
	if err != nil {
		t.Fatalf("DetectRuntimeInfo() error = %v", err)
	}

	if info == nil {
		t.Fatal("DetectRuntimeInfo() returned nil")
	}

	if info.TargetVersion != "2.12" {
		t.Errorf("TargetVersion = %v, want 2.12", info.TargetVersion)
	}

	if info.ExecutionMode == "" {
		t.Error("ExecutionMode is empty")
	}

	if len(info.Recommendations) == 0 {
		t.Error("No recommendations provided")
	}

	// Check that recommendations have required fields
	for i, rec := range info.Recommendations {
		if rec.Method == "" {
			t.Errorf("Recommendation %d has empty method", i)
		}
		if rec.Priority == 0 {
			t.Errorf("Recommendation %d has zero priority", i)
		}
		if rec.Reason == "" {
			t.Errorf("Recommendation %d has empty reason", i)
		}
		if rec.CommandTemplate == "" {
			t.Errorf("Recommendation %d has empty command template", i)
		}
		if !strings.Contains(rec.CommandTemplate, "[command]") {
			t.Errorf("Recommendation %d command template doesn't contain [command] placeholder", i)
		}
	}
}

func TestDetectEnterprise(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   bool
	}{
		{
			name: "CE config",
			config: `{
				"endpoints": [{
					"endpoint": "/test",
					"backend": [{"url_pattern": "/"}]
				}]
			}`,
			want: false,
		},
		{
			name: "EE config with api-keys",
			config: `{
				"extra_config": {
					"auth/api-keys": {}
				}
			}`,
			want: true,
		},
		{
			name: "EE config with redis rate limit",
			config: `{
				"extra_config": {
					"qos/ratelimit/redis": {}
				}
			}`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := runtime.DetectRuntimeInfo(tt.config)
			if err != nil {
				t.Fatalf("DetectRuntimeInfo() error = %v", err)
			}
			if info.IsEnterprise != tt.want {
				t.Errorf("IsEnterprise = %v, want %v", info.IsEnterprise, tt.want)
			}
		})
	}
}

func TestDetectEnvironment(t *testing.T) {
	env := runtime.DetectEnvironment()
	if env == nil {
		t.Fatal("DetectEnvironment() returned nil")
	}

	// Just check that it returns a valid structure
	// Actual values depend on the environment
	t.Logf("Native KrakenD: %v", env.HasNativeKrakenD)
	t.Logf("Docker: %v (version: %s)", env.HasDocker, env.DockerVersion)
}
