package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/krakend/mcp-server/internal/features"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Re-export types from internal/features for backward compatibility
type (
	Feature        = features.Feature
	FeatureCatalog = features.FeatureCatalog
	EditionMatrix  = features.EditionMatrix
)

var (
	featureCatalog *features.FeatureCatalog
	editionMatrix  *features.EditionMatrix
)

// LoadFeatureData loads feature catalog and edition matrix
func LoadFeatureData() error {
	// Load feature catalog
	// Try embedded data first (standalone binary), then filesystem (development)
	catalogData, err := defaultDataProvider.ReadFile("data/features/catalog.json")
	if err != nil {
		// Fallback to filesystem (development mode)
		catalogPath := filepath.Join(dataDir, "features/catalog.json")
		catalogData, err = os.ReadFile(catalogPath)
		if err != nil {
			return fmt.Errorf("failed to read feature catalog (embedded or filesystem): %w", err)
		}
	}

	var catalog FeatureCatalog
	if err := json.Unmarshal(catalogData, &catalog); err != nil {
		return fmt.Errorf("failed to parse feature catalog: %w", err)
	}
	featureCatalog = &catalog

	// Load edition matrix
	// Try embedded data first (standalone binary), then filesystem (development)
	matrixData, err := defaultDataProvider.ReadFile("data/editions/matrix.json")
	if err != nil {
		// Fallback to filesystem (development mode)
		matrixPath := filepath.Join(dataDir, "editions/matrix.json")
		matrixData, err = os.ReadFile(matrixPath)
		if err != nil {
			return fmt.Errorf("failed to read edition matrix (embedded or filesystem): %w", err)
		}
	}

	var matrix EditionMatrix
	if err := json.Unmarshal(matrixData, &matrix); err != nil {
		return fmt.Errorf("failed to parse edition matrix: %w", err)
	}
	editionMatrix = &matrix

	return nil
}

// FeatureSummary represents lightweight feature info for listing
type FeatureSummary struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Edition     string `json:"edition"`     // "ce", "ee", or "both"
	Category    string `json:"category"`
	Description string `json:"description"`
	DocsURL     string `json:"docs_url,omitempty"`
}

// ListFeaturesInput defines input for list_features tool
type ListFeaturesInput struct {
	// No input needed - returns all features
}

// ListFeaturesOutput defines output for list_features tool
type ListFeaturesOutput struct {
	Features []FeatureSummary `json:"features"`
	Count    int              `json:"count"`
}

// ListFeatures returns all KrakenD features with lightweight info
func ListFeatures(ctx context.Context, req *mcp.CallToolRequest, input ListFeaturesInput) (*mcp.CallToolResult, ListFeaturesOutput, error) {
	if featureCatalog == nil {
		if err := LoadFeatureData(); err != nil {
			return nil, ListFeaturesOutput{}, fmt.Errorf("failed to load feature data: %w", err)
		}
	}

	summaries := make([]FeatureSummary, 0, len(featureCatalog.Features))
	for _, feature := range featureCatalog.Features {
		summaries = append(summaries, FeatureSummary{
			Name:        feature.Name,
			Namespace:   feature.Namespace,
			Edition:     feature.Edition,
			Category:    feature.Category,
			Description: feature.Description,
			DocsURL:     feature.DocsURL,
		})
	}

	return nil, ListFeaturesOutput{
		Features: summaries,
		Count:    len(summaries),
	}, nil
}

// CheckEditionCompatibilityInput defines input for check_edition_compatibility tool
type CheckEditionCompatibilityInput struct {
	Config string `json:"config" jsonschema:"KrakenD configuration as JSON string"`
}

// CheckEditionCompatibilityOutput defines output for check_edition_compatibility tool
type CheckEditionCompatibilityOutput struct {
	Edition        string   `json:"edition"`         // "ce", "ee", or "mixed"
	EEFeatures     []string `json:"ee_features"`     // List of EE-only features found
	CECompatible   bool     `json:"ce_compatible"`   // True if config works with CE
	RequiresEE     bool     `json:"requires_ee"`     // True if config requires EE
	FeatureDetails []FeatureCompatibility `json:"feature_details"`
	Message        string   `json:"message"`
}

// FeatureCompatibility represents compatibility info for a feature
type FeatureCompatibility struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Edition   string `json:"edition"`
	Available bool   `json:"available"`
}

// CheckEditionCompatibility detects which edition is required for a config
func CheckEditionCompatibility(ctx context.Context, req *mcp.CallToolRequest, input CheckEditionCompatibilityInput) (*mcp.CallToolResult, CheckEditionCompatibilityOutput, error) {
	if editionMatrix == nil || featureCatalog == nil {
		if err := LoadFeatureData(); err != nil {
			return nil, CheckEditionCompatibilityOutput{}, fmt.Errorf("failed to load feature data: %w", err)
		}
	}

	// Parse config
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(input.Config), &config); err != nil {
		return nil, CheckEditionCompatibilityOutput{}, fmt.Errorf("invalid JSON: %w", err)
	}

	// Find all namespaces used in config
	namespaces := features.FindNamespacesInConfig(config)

	// Initialize as empty slices (not nil) to ensure JSON marshals as [] instead of null
	eeFeatures := []string{}
	featureDetails := []FeatureCompatibility{}
	requiresEE := false

	// Check each namespace against edition matrix
	for _, ns := range namespaces {
		isEEOnly := false
		for _, eeNs := range editionMatrix.EEOnlyFeatures {
			if ns == eeNs {
				isEEOnly = true
				requiresEE = true
				eeFeatures = append(eeFeatures, ns)
				break
			}
		}

		// Find feature details
		for _, feature := range featureCatalog.Features {
			if feature.Namespace == ns {
				featureDetails = append(featureDetails, FeatureCompatibility{
					Namespace: ns,
					Name:      feature.Name,
					Edition:   feature.Edition,
					Available: !isEEOnly,
				})
				break
			}
		}
	}

	edition := "ce"
	message := "Configuration is compatible with Community Edition"
	if requiresEE {
		edition = "ee"
		message = fmt.Sprintf("Configuration requires Enterprise Edition (uses %d EE-only feature(s))", len(eeFeatures))
	}

	return nil, CheckEditionCompatibilityOutput{
		Edition:        edition,
		EEFeatures:     eeFeatures,
		CECompatible:   !requiresEE,
		RequiresEE:     requiresEE,
		FeatureDetails: featureDetails,
		Message:        message,
	}, nil
}

// RegisterFeatureTools registers all feature detection tools
func RegisterFeatureTools(server *mcp.Server) error {
	// Initialize feature data
	if err := LoadFeatureData(); err != nil {
		return fmt.Errorf("failed to load feature data: %w", err)
	}

	// Tool 1: list_features
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "list_features",
			Description: "List all KrakenD features with name, namespace, edition (ce/ee/both), category, and description. Use this to browse available features and check edition requirements.",
		},
		ListFeatures,
	)

	// Tool 2: check_edition_compatibility
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "check_edition_compatibility",
			Description: "Detect which KrakenD edition (CE or EE) is required for a configuration by analyzing which features are used",
		},
		CheckEditionCompatibility,
	)

	return nil
}
