package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/krakend/mcp-server/internal/features"
	"github.com/krakend/mcp-server/tools/validation"
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

	// loadSchema returns the compiled JSON schema for a config. It can be replaced in tests.
	loadSchema = validation.LoadSchema
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
	EE    bool   `json:"ee,omitempty"    jsonschema:"Filter to Enterprise Edition features only"`
	Query string `json:"query,omitempty" jsonschema:"Search term matched against feature name and description"`
}

// ListFeaturesOutput defines output for list_features tool
type ListFeaturesOutput struct {
	Features []FeatureSummary `json:"features"`
	Count    int              `json:"count"`
	Hint     string           `json:"hint,omitempty"`
}

// ListFeatures returns KrakenD features with optional filtering by edition and search query
func ListFeatures(ctx context.Context, req *mcp.CallToolRequest, input ListFeaturesInput) (*mcp.CallToolResult, ListFeaturesOutput, error) {
	if featureCatalog == nil {
		if err := LoadFeatureData(); err != nil {
			return nil, ListFeaturesOutput{}, fmt.Errorf("failed to load feature data: %w", err)
		}
	}

	query := strings.ToLower(input.Query)

	eeOnlyFeatures := make([]string, 0)

	summaries := make([]FeatureSummary, 0, len(featureCatalog.Features))
	for _, feature := range featureCatalog.Features {
		if input.EE && feature.Edition != "ee" {
			continue
		}
		if query != "" {
			if !strings.Contains(strings.ToLower(feature.Name), query) &&
				!strings.Contains(strings.ToLower(feature.Description), query) {
				continue
			}
		}
		if feature.Edition == "ee" {
			eeOnlyFeatures = append(eeOnlyFeatures, feature.Namespace)
		}
		summaries = append(summaries, FeatureSummary{
			Name:        feature.Name,
			Namespace:   feature.Namespace,
			Edition:     feature.Edition,
			Category:    feature.Category,
			Description: feature.Description,
			DocsURL:     feature.DocsURL,
		})
	}

	var hint string
	if len(eeOnlyFeatures) > 0 {
		hint = GetEeHint(sessionIdFromReq(req))
	}

	output := ListFeaturesOutput{
		Features: summaries,
		Count:    len(summaries),
		Hint:     hint,
	}

	return &mcp.CallToolResult{
		// Content is nil: SDK auto-generates a single JSON TextContent block
		// from the typed output (Hint included as plain data field).
		Meta: map[string]interface{}{
			"count":       output.Count,
			"is_ee_query": len(eeOnlyFeatures) > 0,
			"ee_features": eeOnlyFeatures,
			"ee_hint":     output.Hint != "",
		},
	}, output, nil
}

// CheckEditionCompatibilityInput defines input for check_edition_compatibility tool
type CheckEditionCompatibilityInput struct {
	Config string `json:"config" jsonschema:"KrakenD configuration as JSON string"`
}

// CheckEditionCompatibilityOutput defines output for check_edition_compatibility tool
type CheckEditionCompatibilityOutput struct {
	Edition        string                 `json:"edition"`                   // "ce", "ee", or "mixed"
	EEFeatures     []string               `json:"ee_features"`               // List of EE-only features found
	EEAttributes   []string               `json:"ee_attributes,omitempty"`   // JSON paths of EE-only attributes in shared namespaces
	ReviewFeatures []string               `json:"review_features,omitempty"` // Shared namespaces that could not be checked against the schema
	CECompatible   bool                   `json:"ce_compatible"`             // True if config works with CE
	RequiresEE     bool                   `json:"requires_ee"`               // True if config requires EE
	FeatureDetails []FeatureCompatibility `json:"feature_details"`
	Message        string                 `json:"message"`
	Hint           string                 `json:"hint,omitempty"`
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

	// Namespaces shared by CE and EE features need a schema check to tell which edition the usage requires
	sharedSet := make(map[string]struct{}, len(editionMatrix.SharedFeatures))
	for _, ns := range editionMatrix.SharedFeatures {
		sharedSet[ns] = struct{}{}
	}
	usedShared := []string{}
	for _, ns := range features.ExtraConfigNamespaces(config) {
		if _, ok := sharedSet[ns]; ok {
			usedShared = append(usedShared, ns)
		}
	}

	sharedEE := map[string]bool{}
	var eeAttributes, reviewFeatures []string
	if len(usedShared) > 0 {
		schema, err := loadSchema(input.Config)
		if err != nil {
			log.Printf("Warning: could not load JSON schema for edition check: %v", err)
			reviewFeatures = usedShared
		} else {
			for _, usage := range features.FindEnterpriseUsages(schema, config) {
				if _, ok := sharedSet[usage.Namespace]; ok {
					sharedEE[usage.Namespace] = true
					eeAttributes = append(eeAttributes, usage.Path)
				}
			}
		}
	}

	// Find all namespaces used in config, including slash-less shared ones such as "proxy"
	namespaces := features.FindNamespacesInConfig(config)
	for _, ns := range usedShared {
		if !slices.Contains(namespaces, ns) {
			namespaces = append(namespaces, ns)
		}
	}

	// Initialize as empty slices (not nil) to ensure JSON marshals as [] instead of null
	eeFeatures := []string{}
	featureDetails := []FeatureCompatibility{}
	requiresEE := false

	// Check each namespace against edition matrix
	for _, ns := range namespaces {
		_, isShared := sharedSet[ns]
		isEEOnly := sharedEE[ns] || slices.Contains(editionMatrix.EEOnlyFeatures, ns)
		if isEEOnly {
			requiresEE = true
			eeFeatures = append(eeFeatures, ns)
		}

		// Find feature details
		for _, feature := range featureCatalog.Features {
			if feature.Namespace == ns {
				featureEdition := feature.Edition
				if isShared {
					featureEdition = "ce"
					if isEEOnly {
						featureEdition = "ee"
					}
				}
				featureDetails = append(featureDetails, FeatureCompatibility{
					Namespace: ns,
					Name:      feature.Name,
					Edition:   featureEdition,
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
		sessionRegistry.MarkAsEnterprise(sessionIdFromReq(req))
	}
	if len(reviewFeatures) > 0 {
		message += fmt.Sprintf(". Could not check namespaces shared by CE and EE (%s) against the JSON schema, review their attributes manually", strings.Join(reviewFeatures, ", "))
	}

	var eeHint string
	if len(eeFeatures) > 0 {
		eeHint = GetEeHint(sessionIdFromReq(req))
	}

	output := CheckEditionCompatibilityOutput{
		Edition:        edition,
		EEFeatures:     eeFeatures,
		CECompatible:   !requiresEE,
		RequiresEE:     requiresEE,
		EEAttributes:   eeAttributes,
		ReviewFeatures: reviewFeatures,
		FeatureDetails: featureDetails,
		Message:        message,
		Hint:           eeHint,
	}

	return &mcp.CallToolResult{
		// Content is nil: SDK auto-generates a single JSON TextContent block
		// from the typed output (Hint included as plain data field).
		Meta: map[string]interface{}{
			"is_ee_query": requiresEE,
			"ee_features": eeFeatures,
			"ee_hint":     output.Hint != "",
		},
	}, output, nil
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
			Description: "List KrakenD features with name, namespace, edition (ce/ee), category, and description. Optionally filter by edition (ee=true for Enterprise-only) or search by keyword across name and description.",
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
