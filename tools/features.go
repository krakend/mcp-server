package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/krakend/mcp-server/internal/features"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	remoteFeatureMatrixURL = "https://www.krakend.io/mcp-feature-matrix.yaml"
	featureMatrixFile      = "features/mcp-feature-matrix.yaml"
	featureCacheTTL        = 7 * 24 * time.Hour
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

// DetectEnterpriseFeatures checks if config uses EE-only features
// This wrapper ensures feature data is loaded before detection
func DetectEnterpriseFeatures(configJSON string) bool {
	// Ensure feature data is loaded
	if editionMatrix == nil || featureCatalog == nil {
		if err := LoadFeatureData(); err != nil {
			return false
		}
	}

	// Use centralized detection from internal/features
	return features.DetectEnterpriseFeatures(configJSON, editionMatrix.EEOnlyFeatures)
}

// LoadFeatureData loads the feature catalog and edition matrix using an offline-first strategy:
//  1. Use local cached file if fresh (<7 days old)
//  2. Re-download if stale; on failure fall back to existing local file
//  3. If no local file, download; on failure use embedded fallback
func LoadFeatureData() error {
	localPath := filepath.Join(dataDir, featureMatrixFile)

	if info, err := os.Stat(localPath); err == nil {
		age := time.Since(info.ModTime())
		if age <= featureCacheTTL {
			return loadFeatureMatrixFromPath(localPath)
		}
		log.Printf("Feature matrix is %v old (>7 days), refreshing...", age.Round(24*time.Hour))
		if err := downloadFeatureMatrix(localPath); err != nil {
			log.Printf("Warning: could not refresh feature matrix: %v — using existing local file", err)
		}
		return loadFeatureMatrixFromPath(localPath)
	}

	// No local file: try download first, fall back to embedded.
	if err := downloadFeatureMatrix(localPath); err != nil {
		log.Printf("Warning: could not download feature matrix: %v — using embedded fallback", err)
		return loadEmbeddedFeatureMatrix()
	}
	return loadFeatureMatrixFromPath(localPath)
}

func downloadFeatureMatrix(localPath string) error {
	data, err := features.HTTPFetcher(remoteFeatureMatrixURL)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return fmt.Errorf("failed to create features directory: %w", err)
	}
	return os.WriteFile(localPath, data, 0o644)
}

func loadFeatureMatrixFromPath(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read feature matrix: %w", err)
	}
	return parseAndStoreFeatureMatrix(data)
}

func loadEmbeddedFeatureMatrix() error {
	data, err := defaultDataProvider.ReadFile("data/features/mcp-feature-matrix.yaml")
	if err != nil {
		return fmt.Errorf("no embedded feature matrix available (run build.sh to embed it): %w", err)
	}
	return parseAndStoreFeatureMatrix(data)
}

func parseAndStoreFeatureMatrix(data []byte) error {
	catalog, matrix, err := features.ParseFeatureMatrix(data)
	if err != nil {
		return fmt.Errorf("failed to parse feature matrix: %w", err)
	}
	featureCatalog = catalog
	editionMatrix = matrix
	return nil
}

// FeatureSummary represents lightweight feature info for listing
type FeatureSummary struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Edition     string `json:"edition"` // "ce", "ee", or "both"
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

	output := ListFeaturesOutput{
		Features: summaries,
		Count:    len(summaries),
	}
	return &mcp.CallToolResult{Meta: map[string]interface{}{"count": output.Count}}, output, nil
}

// CheckEditionCompatibilityInput defines input for check_edition_compatibility tool
type CheckEditionCompatibilityInput struct {
	Config string `json:"config" jsonschema:"KrakenD configuration as JSON string"`
}

// CheckEditionCompatibilityOutput defines output for check_edition_compatibility tool
type CheckEditionCompatibilityOutput struct {
	Edition        string                 `json:"edition"`       // "ce", "ee", or "mixed"
	EEFeatures     []string               `json:"ee_features"`   // List of EE-only features found
	CECompatible   bool                   `json:"ce_compatible"` // True if config works with CE
	RequiresEE     bool                   `json:"requires_ee"`   // True if config requires EE
	FeatureDetails []FeatureCompatibility `json:"feature_details"`
	Message        string                 `json:"message"`
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
