package features

import (
	"encoding/json"
	"strings"
)

// CommonEEFeatures is a curated list of commonly used EE-only namespaces
// This provides a lightweight detection method without requiring the full catalog
var CommonEEFeatures = []string{
	"auth/api-keys",
	"qos/ratelimit/redis",
	"telemetry/opentelemetry",
	"security/bot-detector",
	"auth/signer",
	"auth/gcp",
	"telemetry/newrelic",
	"telemetry/datadog",
	"plugin/req-resp-modifier",
}

// Feature represents a KrakenD feature
type Feature struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Namespace      string                 `json:"namespace"`
	Edition        string                 `json:"edition"` // "ce", "ee", or "both"
	Category       string                 `json:"category"`
	Description    string                 `json:"description"`
	DocsURL        string                 `json:"docs_url"`
	RequiredFields []string               `json:"required_fields"`
	OptionalFields []string               `json:"optional_fields"`
	ExampleConfig  map[string]interface{} `json:"example_config"`
}

// FeatureCatalog represents the complete feature catalog
type FeatureCatalog struct {
	Features    []Feature `json:"features"`
	Version     string    `json:"version"`
	LastUpdated string    `json:"last_updated"`
}

// EditionMatrix represents CE vs EE feature compatibility
type EditionMatrix struct {
	CEFeatures     []string                          `json:"ce_features"`
	EEOnlyFeatures []string                          `json:"ee_only_features"`
	FeatureDetails map[string]map[string]interface{} `json:"feature_details"`
	Version        string                            `json:"version"`
	LastUpdated    string                            `json:"last_updated"`
	Notes          string                            `json:"notes"`
}

// DetectEnterpriseFeatures checks if a config uses EE-only features
// Returns true if any EE-only feature is detected in the configuration
// If eeOnlyFeatures is nil or empty, uses CommonEEFeatures as fallback
func DetectEnterpriseFeatures(configJSON string, eeOnlyFeatures []string) bool {
	// Use common EE features if no list provided
	if len(eeOnlyFeatures) == 0 {
		eeOnlyFeatures = CommonEEFeatures
	}

	// Parse config
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return false
	}

	// Find namespaces in config
	namespaces := FindNamespacesInConfig(config)

	// Check against EE-only features
	for _, ns := range namespaces {
		for _, eeNs := range eeOnlyFeatures {
			if ns == eeNs {
				return true
			}
		}
	}

	return false
}

// DetectEnterpriseFeaturesSimple is a lightweight version that uses simple string matching
// This is faster but less accurate than the full catalog-based detection
func DetectEnterpriseFeaturesSimple(configJSON string) bool {
	// Simple heuristic: check for common EE namespaces using string matching
	for _, ns := range CommonEEFeatures {
		if strings.Contains(configJSON, ns) {
			return true
		}
	}
	return false
}

// FindNamespacesInConfig extracts all namespaces from a configuration
func FindNamespacesInConfig(data interface{}) []string {
	seen := make(map[string]struct{})
	collectNamespaces(data, seen)

	// Convert map to slice
	namespaces := make([]string, 0, len(seen))
	for ns := range seen {
		namespaces = append(namespaces, ns)
	}
	return namespaces
}

// collectNamespaces recursively collects namespaces into a map for deduplication
func collectNamespaces(data interface{}, seen map[string]struct{}) {
	switch v := data.(type) {
	case map[string]interface{}:
		for key, value := range v {
			// Keys with '/' are likely namespaces
			if strings.Contains(key, "/") {
				seen[key] = struct{}{}
			}
			// Recurse into nested structures
			collectNamespaces(value, seen)
		}
	case []interface{}:
		for _, item := range v {
			collectNamespaces(item, seen)
		}
	}
}
