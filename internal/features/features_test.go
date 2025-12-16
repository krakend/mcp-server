package features_test

import (
	"testing"

	"github.com/krakend/mcp-server/internal/features"
)

func TestDetectEnterpriseFeaturesSimple(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   bool
	}{
		{
			name: "CE config without EE features",
			config: `{
				"endpoints": [{"endpoint": "/test", "backend": [{"url_pattern": "/"}]}]
			}`,
			want: false,
		},
		{
			name: "EE config with api-keys",
			config: `{
				"extra_config": {
					"auth/api-keys": {"keys": []}
				}
			}`,
			want: true,
		},
		{
			name: "EE config with redis rate limit",
			config: `{
				"extra_config": {
					"qos/ratelimit/redis": {"host": "localhost"}
				}
			}`,
			want: true,
		},
		{
			name: "EE config with opentelemetry",
			config: `{
				"extra_config": {
					"telemetry/opentelemetry": {"service_name": "test"}
				}
			}`,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := features.DetectEnterpriseFeaturesSimple(tt.config)
			if got != tt.want {
				t.Errorf("DetectEnterpriseFeaturesSimple() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDetectEnterpriseFeatures(t *testing.T) {
	tests := []struct {
		name            string
		config          string
		eeOnlyFeatures  []string
		want            bool
	}{
		{
			name: "CE config",
			config: `{
				"endpoints": [{"endpoint": "/test", "backend": [{"url_pattern": "/"}]}]
			}`,
			eeOnlyFeatures: []string{"auth/api-keys"},
			want:           false,
		},
		{
			name: "EE config with provided list",
			config: `{
				"extra_config": {
					"auth/api-keys": {"keys": []}
				}
			}`,
			eeOnlyFeatures: []string{"auth/api-keys"},
			want:           true,
		},
		{
			name: "Uses CommonEEFeatures when list is empty",
			config: `{
				"extra_config": {
					"auth/api-keys": {"keys": []}
				}
			}`,
			eeOnlyFeatures: nil,
			want:           true,
		},
		{
			name: "Invalid JSON returns false",
			config: `{invalid}`,
			eeOnlyFeatures: []string{"auth/api-keys"},
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := features.DetectEnterpriseFeatures(tt.config, tt.eeOnlyFeatures)
			if got != tt.want {
				t.Errorf("DetectEnterpriseFeatures() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFindNamespacesInConfig(t *testing.T) {
	tests := []struct {
		name string
		data interface{}
		want []string
	}{
		{
			name: "Empty config",
			data: map[string]interface{}{},
			want: []string{},
		},
		{
			name: "Single namespace",
			data: map[string]interface{}{
				"extra_config": map[string]interface{}{
					"auth/api-keys": map[string]interface{}{},
				},
			},
			want: []string{"auth/api-keys"},
		},
		{
			name: "Multiple namespaces",
			data: map[string]interface{}{
				"extra_config": map[string]interface{}{
					"auth/api-keys": map[string]interface{}{},
					"qos/ratelimit/redis": map[string]interface{}{},
				},
			},
			want: []string{"auth/api-keys", "qos/ratelimit/redis"},
		},
		{
			name: "Nested namespaces",
			data: map[string]interface{}{
				"endpoints": []interface{}{
					map[string]interface{}{
						"extra_config": map[string]interface{}{
							"backend/http/client": map[string]interface{}{},
						},
					},
				},
			},
			want: []string{"backend/http/client"},
		},
		{
			name: "Deduplication",
			data: map[string]interface{}{
				"extra_config": map[string]interface{}{
					"auth/api-keys": map[string]interface{}{},
				},
				"endpoints": []interface{}{
					map[string]interface{}{
						"extra_config": map[string]interface{}{
							"auth/api-keys": map[string]interface{}{},
						},
					},
				},
			},
			want: []string{"auth/api-keys"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := features.FindNamespacesInConfig(tt.data)

			// Check length
			if len(got) != len(tt.want) {
				t.Errorf("FindNamespacesInConfig() returned %d namespaces, want %d", len(got), len(tt.want))
			}

			// Check that all expected namespaces are present
			gotMap := make(map[string]bool)
			for _, ns := range got {
				gotMap[ns] = true
			}

			for _, wantNs := range tt.want {
				if !gotMap[wantNs] {
					t.Errorf("FindNamespacesInConfig() missing namespace %q", wantNs)
				}
			}
		})
	}
}

func TestCommonEEFeatures(t *testing.T) {
	// Ensure CommonEEFeatures is not empty
	if len(features.CommonEEFeatures) == 0 {
		t.Error("CommonEEFeatures should not be empty")
	}

	// Check that common EE features are present
	expectedFeatures := []string{
		"auth/api-keys",
		"qos/ratelimit/redis",
		"telemetry/opentelemetry",
	}

	featureMap := make(map[string]bool)
	for _, f := range features.CommonEEFeatures {
		featureMap[f] = true
	}

	for _, expected := range expectedFeatures {
		if !featureMap[expected] {
			t.Errorf("CommonEEFeatures missing expected feature: %s", expected)
		}
	}
}
