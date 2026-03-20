package tools

import (
	"context"
	"testing"

	"github.com/krakend/mcp-server/internal/features"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const minimalFeatureYAML = `
sections:
  - name: "Auth"
    features:
      - name: "API Keys"
        description: "EE API key auth"
        url: "/docs/api-keys"
        ee: true
        namespaces:
          - "auth/api-keys"
      - name: "CORS"
        description: "CE CORS support"
        url: "/docs/cors"
        ee: false
        namespaces:
          - "security/cors"
`

func setMockFeatureFetcher(t *testing.T, yamlContent string) {
	t.Helper()
	orig := features.HTTPFetcher
	origDataDir := dataDir
	features.HTTPFetcher = func(_ string) ([]byte, error) { return []byte(yamlContent), nil }
	dataDir = t.TempDir()
	t.Cleanup(func() {
		features.HTTPFetcher = orig
		dataDir = origDataDir
		featureCatalog = nil
		editionMatrix = nil
	})
}

func TestFindNamespacesInConfig_EmptyConfig(t *testing.T) {
	config := map[string]interface{}{}

	namespaces := features.FindNamespacesInConfig(config)

	if len(namespaces) != 0 {
		t.Errorf("Expected 0 namespaces, got %d", len(namespaces))
	}
}

func TestFindNamespacesInConfig_WithNamespaces(t *testing.T) {
	config := map[string]interface{}{
		"extra_config": map[string]interface{}{
			"github.com/devopsfaith/krakend-ratelimit/juju/router": map[string]interface{}{
				"maxRate": 100,
			},
			"github.com/devopsfaith/krakend-cors": map[string]interface{}{
				"allow_origins": []string{"*"},
			},
		},
	}

	namespaces := features.FindNamespacesInConfig(config)

	if len(namespaces) != 2 {
		t.Errorf("Expected 2 namespaces, got %d", len(namespaces))
	}

	// Check that both namespaces are found
	found := make(map[string]bool)
	for _, ns := range namespaces {
		found[ns] = true
	}

	if !found["github.com/devopsfaith/krakend-ratelimit/juju/router"] {
		t.Error("Expected to find ratelimit namespace")
	}
	if !found["github.com/devopsfaith/krakend-cors"] {
		t.Error("Expected to find cors namespace")
	}
}

func TestFindNamespacesInConfig_Deduplication(t *testing.T) {
	// Config with duplicate namespaces
	config := map[string]interface{}{
		"extra_config": map[string]interface{}{
			"github.com/devopsfaith/krakend-cors": map[string]interface{}{
				"allow_origins": []string{"*"},
			},
		},
		"endpoints": []interface{}{
			map[string]interface{}{
				"extra_config": map[string]interface{}{
					"github.com/devopsfaith/krakend-cors": map[string]interface{}{
						"allow_origins": []string{"https://example.com"},
					},
				},
			},
			map[string]interface{}{
				"extra_config": map[string]interface{}{
					"github.com/devopsfaith/krakend-cors": map[string]interface{}{
						"allow_origins": []string{"https://example2.com"},
					},
				},
			},
		},
	}

	namespaces := features.FindNamespacesInConfig(config)

	// Should only find 1 unique namespace despite 3 occurrences
	if len(namespaces) != 1 {
		t.Errorf("Expected 1 unique namespace, got %d: %v", len(namespaces), namespaces)
	}
}

func TestFindNamespacesInConfig_NestedStructures(t *testing.T) {
	config := map[string]interface{}{
		"endpoints": []interface{}{
			map[string]interface{}{
				"endpoint": "/api/v1",
				"backend": []interface{}{
					map[string]interface{}{
						"extra_config": map[string]interface{}{
							"github.com/devopsfaith/krakend-httpcache": map[string]interface{}{
								"shared": true,
							},
						},
					},
				},
			},
		},
	}

	namespaces := features.FindNamespacesInConfig(config)

	if len(namespaces) != 1 {
		t.Errorf("Expected 1 namespace, got %d", len(namespaces))
	}
	if len(namespaces) > 0 && namespaces[0] != "github.com/devopsfaith/krakend-httpcache" {
		t.Errorf("Expected httpcache namespace, got %s", namespaces[0])
	}
}

func TestFindNamespacesInConfig_NoSlashKeys(t *testing.T) {
	config := map[string]interface{}{
		"version": 3,
		"timeout": "3000ms",
		"endpoints": []interface{}{},
	}

	namespaces := features.FindNamespacesInConfig(config)

	if len(namespaces) != 0 {
		t.Errorf("Expected 0 namespaces (no keys with '/'), got %d", len(namespaces))
	}
}

func TestCollectNamespaces_MapPerformance(t *testing.T) {
	// Create a large config with many duplicate namespaces
	endpoints := make([]interface{}, 100)
	for i := 0; i < 100; i++ {
		endpoints[i] = map[string]interface{}{
			"extra_config": map[string]interface{}{
				"github.com/devopsfaith/krakend-ratelimit/juju/router": map[string]interface{}{},
				"github.com/devopsfaith/krakend-cors":                  map[string]interface{}{},
				"github.com/devopsfaith/krakend-httpcache":             map[string]interface{}{},
			},
		}
	}

	config := map[string]interface{}{
		"endpoints": endpoints,
	}

	namespaces := features.FindNamespacesInConfig(config)

	// Should only find 3 unique namespaces despite 300 occurrences
	if len(namespaces) != 3 {
		t.Errorf("Expected 3 unique namespaces, got %d", len(namespaces))
	}
}

func TestDetectEnterpriseFeatures_NoEEFeatures(t *testing.T) {
	setMockFeatureFetcher(t, minimalFeatureYAML)
	config := `{
		"version": 3,
		"endpoints": []
	}`

	result := DetectEnterpriseFeatures(config)

	if result {
		t.Error("Expected false for config without EE features")
	}
}

func TestDetectEnterpriseFeatures_WithEENamespace(t *testing.T) {
	setMockFeatureFetcher(t, minimalFeatureYAML)
	config := `{
		"version": 3,
		"extra_config": {
			"auth/api-keys": {
				"keys": []
			}
		}
	}`

	result := DetectEnterpriseFeatures(config)

	if !result {
		t.Error("Expected true for config with EE namespace (auth/api-keys)")
	}
}

// listFeaturesYAML has a richer fixture for filter tests: two CE and two EE features
// across different categories with distinct names and descriptions.
const listFeaturesYAML = `
sections:
  - name: "Traffic Management"
    features:
      - name: "Endpoint Rate Limiting"
        description: "Limit requests per endpoint to prevent overload"
        url: "/docs/rate-limit"
        ee: false
        namespaces:
          - "qos/ratelimit/router"
      - name: "Redis Rate Limiting"
        description: "Distributed rate limiting using Redis"
        url: "/docs/redis-rate-limit"
        ee: true
        namespaces:
          - "qos/ratelimit/redis"
  - name: "Authentication"
    features:
      - name: "JWT Validation"
        description: "Validate JSON Web Tokens on every request"
        url: "/docs/jwt"
        ee: false
        namespaces:
          - "auth/validator"
      - name: "API Key Authentication"
        description: "API key-based authentication for enterprise"
        url: "/docs/api-keys"
        ee: true
        namespaces:
          - "auth/api-keys"
`

func callListFeatures(t *testing.T, input ListFeaturesInput) ListFeaturesOutput {
	t.Helper()
	_, output, err := ListFeatures(context.Background(), &mcp.CallToolRequest{}, input)
	if err != nil {
		t.Fatalf("ListFeatures returned unexpected error: %v", err)
	}
	return output
}

func TestListFeatures_NoFilters(t *testing.T) {
	setMockFeatureFetcher(t, listFeaturesYAML)

	output := callListFeatures(t, ListFeaturesInput{})

	if output.Count != 4 {
		t.Errorf("expected 4 features, got %d", output.Count)
	}
	if len(output.Features) != output.Count {
		t.Errorf("Count field (%d) does not match slice length (%d)", output.Count, len(output.Features))
	}
}

func TestListFeatures_EEFilter(t *testing.T) {
	setMockFeatureFetcher(t, listFeaturesYAML)

	output := callListFeatures(t, ListFeaturesInput{EE: true})

	if output.Count != 2 {
		t.Errorf("expected 2 EE features, got %d", output.Count)
	}
	for _, f := range output.Features {
		if f.Edition != "ee" {
			t.Errorf("expected all features to be 'ee', got %q for %q", f.Edition, f.Name)
		}
	}
}

func TestListFeatures_QueryFilter_MatchesName(t *testing.T) {
	setMockFeatureFetcher(t, listFeaturesYAML)

	output := callListFeatures(t, ListFeaturesInput{Query: "JWT"})

	if output.Count != 1 {
		t.Errorf("expected 1 result for query 'JWT', got %d", output.Count)
	}
	if output.Features[0].Namespace != "auth/validator" {
		t.Errorf("unexpected feature returned: %q", output.Features[0].Name)
	}
}

func TestListFeatures_QueryFilter_MatchesDescription(t *testing.T) {
	setMockFeatureFetcher(t, listFeaturesYAML)

	output := callListFeatures(t, ListFeaturesInput{Query: "redis"})

	if output.Count != 1 {
		t.Errorf("expected 1 result for query 'redis', got %d", output.Count)
	}
	if output.Features[0].Namespace != "qos/ratelimit/redis" {
		t.Errorf("unexpected feature returned: %q", output.Features[0].Name)
	}
}

func TestListFeatures_QueryFilter_CaseInsensitive(t *testing.T) {
	setMockFeatureFetcher(t, listFeaturesYAML)

	lower := callListFeatures(t, ListFeaturesInput{Query: "rate limiting"})
	upper := callListFeatures(t, ListFeaturesInput{Query: "RATE LIMITING"})
	mixed := callListFeatures(t, ListFeaturesInput{Query: "Rate Limiting"})

	if lower.Count != upper.Count || lower.Count != mixed.Count {
		t.Errorf("case-insensitive mismatch: lower=%d upper=%d mixed=%d", lower.Count, upper.Count, mixed.Count)
	}
	if lower.Count != 2 {
		t.Errorf("expected 2 results for 'rate limiting', got %d", lower.Count)
	}
}

func TestListFeatures_QueryAndEEFilter_Combined(t *testing.T) {
	setMockFeatureFetcher(t, listFeaturesYAML)

	// "rate" matches both rate-limit features; ee=true should narrow to the EE one
	output := callListFeatures(t, ListFeaturesInput{EE: true, Query: "rate"})

	if output.Count != 1 {
		t.Errorf("expected 1 result for ee+rate, got %d", output.Count)
	}
	if output.Features[0].Namespace != "qos/ratelimit/redis" {
		t.Errorf("unexpected feature: %q", output.Features[0].Name)
	}
}

func TestListFeatures_QueryFilter_NoMatch(t *testing.T) {
	setMockFeatureFetcher(t, listFeaturesYAML)

	output := callListFeatures(t, ListFeaturesInput{Query: "nonexistent"})

	if output.Count != 0 {
		t.Errorf("expected 0 results for unmatched query, got %d", output.Count)
	}
	if output.Features == nil {
		t.Error("Features slice should not be nil")
	}
}
