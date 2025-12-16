package tools

import (
	"testing"
)

func TestFindNamespacesInConfig_EmptyConfig(t *testing.T) {
	config := map[string]interface{}{}

	namespaces := findNamespacesInConfig(config)

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

	namespaces := findNamespacesInConfig(config)

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

	namespaces := findNamespacesInConfig(config)

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

	namespaces := findNamespacesInConfig(config)

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

	namespaces := findNamespacesInConfig(config)

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

	namespaces := findNamespacesInConfig(config)

	// Should only find 3 unique namespaces despite 300 occurrences
	if len(namespaces) != 3 {
		t.Errorf("Expected 3 unique namespaces, got %d", len(namespaces))
	}
}

func TestDetectEnterpriseFeatures_NoEEFeatures(t *testing.T) {
	config := `{
		"version": 3,
		"endpoints": []
	}`

	result := detectEnterpriseFeatures(config)

	if result {
		t.Error("Expected false for config without EE features")
	}
}

func TestDetectEnterpriseFeatures_WithEENamespace(t *testing.T) {
	config := `{
		"version": 3,
		"extra_config": {
			"auth/api-keys": {
				"keys": []
			}
		}
	}`

	result := detectEnterpriseFeatures(config)

	if !result {
		t.Error("Expected true for config with EE namespace (auth/api-keys)")
	}
}
