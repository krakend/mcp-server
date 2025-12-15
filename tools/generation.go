package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GetFeatureConfigTemplateInput defines input for get_feature_config_template tool
type GetFeatureConfigTemplateInput struct {
	Feature    string                 `json:"feature" jsonschema:"Feature name or namespace"`
	Parameters map[string]interface{} `json:"parameters,omitempty" jsonschema:"Feature-specific parameters (optional)"`
}

// GetFeatureConfigTemplateOutput defines output for get_feature_config_template tool
type GetFeatureConfigTemplateOutput struct {
	Template       map[string]interface{} `json:"template"`
	RequiredFields []string               `json:"required_fields"`
	OptionalFields []string               `json:"optional_fields"`
	Documentation  string                 `json:"documentation"`
	Namespace      string                 `json:"namespace"`
}

// GetFeatureConfigTemplate returns a config template for a specific feature
func GetFeatureConfigTemplate(ctx context.Context, req *mcp.CallToolRequest, input GetFeatureConfigTemplateInput) (*mcp.CallToolResult, GetFeatureConfigTemplateOutput, error) {
	if featureCatalog == nil {
		if err := LoadFeatureData(); err != nil {
			return nil, GetFeatureConfigTemplateOutput{}, fmt.Errorf("failed to load feature data: %w", err)
		}
	}

	// Search for feature
	query := strings.ToLower(input.Feature)
	for _, feature := range featureCatalog.Features {
		nameMatch := strings.Contains(strings.ToLower(feature.Name), query)
		nsMatch := strings.Contains(strings.ToLower(feature.Namespace), query)
		idMatch := strings.Contains(strings.ToLower(feature.ID), query)

		if nameMatch || nsMatch || idMatch {
			// Start with example config as template
			template := make(map[string]interface{})
			for k, v := range feature.ExampleConfig {
				template[k] = v
			}

			// Apply custom parameters if provided
			if input.Parameters != nil && len(input.Parameters) > 0 {
				// Merge parameters into template
				if featureConfig, ok := template[feature.Namespace].(map[string]interface{}); ok {
					for key, value := range input.Parameters {
						featureConfig[key] = value
					}
				}
			}

			documentation := fmt.Sprintf("%s\n\nNamespace: %s\nEdition: %s\nCategory: %s\n\nDocs: %s",
				feature.Description,
				feature.Namespace,
				strings.ToUpper(feature.Edition),
				feature.Category,
				feature.DocsURL,
			)

			return nil, GetFeatureConfigTemplateOutput{
				Template:       template,
				RequiredFields: feature.RequiredFields,
				OptionalFields: feature.OptionalFields,
				Documentation:  documentation,
				Namespace:      feature.Namespace,
			}, nil
		}
	}

	return nil, GetFeatureConfigTemplateOutput{}, fmt.Errorf("feature '%s' not found", input.Feature)
}

// Backend represents a backend service configuration
type Backend struct {
	URL        string                 `json:"url"`
	Method     string                 `json:"method,omitempty"`
	Host       []string               `json:"host,omitempty"`
	ExtraConfig map[string]interface{} `json:"extra_config,omitempty"`
}

// GenerateBackendConfigInput defines input for generate_backend_config tool
type GenerateBackendConfigInput struct {
	URL         string                 `json:"url" jsonschema:"Backend URL pattern"`
	Method      string                 `json:"method,omitempty" jsonschema:"HTTP method (optional, defaults to GET)"`
	Host        []string               `json:"host,omitempty" jsonschema:"Backend host URLs (optional)"`
	URLPattern  string                 `json:"url_pattern,omitempty"`
	ExtraConfig map[string]interface{} `json:"extra_config,omitempty"`
}

// GenerateBackendConfigOutput defines output for generate_backend_config tool
type GenerateBackendConfigOutput struct {
	BackendConfig map[string]interface{} `json:"backend_config"`
	Warnings      []string               `json:"warnings,omitempty"`
}

// GenerateBackendConfig generates a backend configuration
func GenerateBackendConfig(ctx context.Context, req *mcp.CallToolRequest, input GenerateBackendConfigInput) (*mcp.CallToolResult, GenerateBackendConfigOutput, error) {
	backend := make(map[string]interface{})
	warnings := []string{}

	// Required: URL
	backend["url_pattern"] = input.URLPattern
	if input.URLPattern == "" {
		backend["url_pattern"] = "/"
		warnings = append(warnings, "url_pattern not specified, using default '/'")
	}

	// Host
	if len(input.Host) > 0 {
		backend["host"] = input.Host
	} else {
		// Try to extract host from URL
		if input.URL != "" {
			backend["host"] = []string{input.URL}
		}
	}

	// Method
	if input.Method != "" {
		backend["method"] = strings.ToUpper(input.Method)
	} else {
		backend["method"] = "GET"
	}

	// Extra config (features)
	if input.ExtraConfig != nil && len(input.ExtraConfig) > 0 {
		backend["extra_config"] = input.ExtraConfig
	}

	// Best practice recommendations
	if input.ExtraConfig == nil || input.ExtraConfig["qos/circuit-breaker"] == nil {
		warnings = append(warnings, "Consider adding a circuit breaker (qos/circuit-breaker) for reliability")
	}

	return nil, GenerateBackendConfigOutput{
		BackendConfig: backend,
		Warnings:      warnings,
	}, nil
}

// GenerateEndpointConfigInput defines input for generate_endpoint_config tool
type GenerateEndpointConfigInput struct {
	Method   string                   `json:"method" jsonschema:"HTTP method (GET, POST, etc)"`
	Path     string                   `json:"path" jsonschema:"Endpoint path"`
	Backends []Backend                `json:"backends" jsonschema:"Backend configurations"`
	ExtraConfig map[string]interface{} `json:"extra_config,omitempty"`
	OutputEncoding string              `json:"output_encoding,omitempty"`
}

// GenerateEndpointConfigOutput defines output for generate_endpoint_config tool
type GenerateEndpointConfigOutput struct {
	EndpointConfig map[string]interface{} `json:"endpoint_config"`
	Warnings       []string               `json:"warnings,omitempty"`
	BestPractices  []string               `json:"best_practices,omitempty"`
}

// GenerateEndpointConfig generates an endpoint configuration
func GenerateEndpointConfig(ctx context.Context, req *mcp.CallToolRequest, input GenerateEndpointConfigInput) (*mcp.CallToolResult, GenerateEndpointConfigOutput, error) {
	endpoint := make(map[string]interface{})
	warnings := []string{}
	bestPractices := []string{}

	// Required fields
	endpoint["endpoint"] = input.Path
	endpoint["method"] = strings.ToUpper(input.Method)

	// Output encoding
	if input.OutputEncoding != "" {
		endpoint["output_encoding"] = input.OutputEncoding
	} else {
		endpoint["output_encoding"] = "json"
	}

	// Backends
	if len(input.Backends) == 0 {
		return nil, GenerateEndpointConfigOutput{}, fmt.Errorf("at least one backend is required")
	}

	backends := make([]map[string]interface{}, 0, len(input.Backends))
	for i, backend := range input.Backends {
		b := make(map[string]interface{})

		// Host
		if len(backend.Host) > 0 {
			b["host"] = backend.Host
		} else if backend.URL != "" {
			b["host"] = []string{backend.URL}
		} else {
			return nil, GenerateEndpointConfigOutput{}, fmt.Errorf("backend %d: either 'host' or 'url' must be specified", i)
		}

		// URL pattern
		b["url_pattern"] = "/"

		// Method
		if backend.Method != "" {
			b["method"] = strings.ToUpper(backend.Method)
		} else {
			b["method"] = "GET"
		}

		// Extra config
		if backend.ExtraConfig != nil && len(backend.ExtraConfig) > 0 {
			b["extra_config"] = backend.ExtraConfig

			// Check for circuit breaker
			if backend.ExtraConfig["qos/circuit-breaker"] == nil {
				bestPractices = append(bestPractices, fmt.Sprintf("Backend %d: Consider adding circuit breaker for reliability", i))
			}
		} else {
			bestPractices = append(bestPractices, fmt.Sprintf("Backend %d: Consider adding circuit breaker (qos/circuit-breaker)", i))
		}

		backends = append(backends, b)
	}

	endpoint["backend"] = backends

	// Extra config (endpoint-level features)
	if input.ExtraConfig != nil && len(input.ExtraConfig) > 0 {
		endpoint["extra_config"] = input.ExtraConfig
	}

	// Best practice checks
	if len(input.Backends) > 1 {
		bestPractices = append(bestPractices, "Multiple backends detected - ensure proper aggregation strategy is configured")
	}

	// Rate limiting recommendation
	if input.ExtraConfig == nil || input.ExtraConfig["qos/ratelimit/router"] == nil {
		bestPractices = append(bestPractices, "Consider adding rate limiting (qos/ratelimit/router) to protect this endpoint")
	}

	// Check for authentication if POST/PUT/DELETE
	method := strings.ToUpper(input.Method)
	if (method == "POST" || method == "PUT" || method == "DELETE") {
		if input.ExtraConfig == nil || input.ExtraConfig["auth/validator"] == nil {
			warnings = append(warnings, fmt.Sprintf("%s endpoint without authentication - ensure this is intentional", method))
		}
	}

	return nil, GenerateEndpointConfigOutput{
		EndpointConfig: endpoint,
		Warnings:       warnings,
		BestPractices:  bestPractices,
	}, nil
}

// GenerateBasicConfigInput defines input for a helper to generate a complete basic config
type GenerateBasicConfigInput struct {
	Port      int                    `json:"port,omitempty" jsonschema:"Server port (optional, defaults to 8080)"`
	Endpoints []GenerateEndpointConfigInput `json:"endpoints" jsonschema:"Endpoint configurations"`
	Timeout   string                 `json:"timeout,omitempty" jsonschema:"Global timeout (optional)"`
	CacheTTL  string                 `json:"cache_ttl,omitempty"`
}

// GenerateBasicConfigOutput defines output for basic config generation
type GenerateBasicConfigOutput struct {
	Config        map[string]interface{} `json:"config"`
	Warnings      []string               `json:"warnings,omitempty"`
	BestPractices []string               `json:"best_practices,omitempty"`
}

// GenerateBasicConfig generates a complete basic KrakenD configuration
func GenerateBasicConfig(ctx context.Context, req *mcp.CallToolRequest, input GenerateBasicConfigInput) (*mcp.CallToolResult, GenerateBasicConfigOutput, error) {
	config := make(map[string]interface{})
	allWarnings := []string{}
	allBestPractices := []string{}

	// Version
	config["version"] = 3
	config["$schema"] = "https://www.krakend.io/schema/krakend.json"

	// Port
	port := input.Port
	if port == 0 {
		port = 8080
	}
	config["port"] = port

	// Timeout
	timeout := input.Timeout
	if timeout == "" {
		timeout = "3000ms"
		allBestPractices = append(allBestPractices, "Using default timeout of 3s - adjust based on your backend response times")
	}
	config["timeout"] = timeout

	// Cache TTL
	if input.CacheTTL != "" {
		config["cache_ttl"] = input.CacheTTL
	}

	// Generate endpoints
	if len(input.Endpoints) == 0 {
		return nil, GenerateBasicConfigOutput{}, fmt.Errorf("at least one endpoint is required")
	}

	endpoints := make([]map[string]interface{}, 0, len(input.Endpoints))
	for i, endpointInput := range input.Endpoints {
		result, output, err := GenerateEndpointConfig(ctx, req, endpointInput)
		if err != nil {
			return nil, GenerateBasicConfigOutput{}, fmt.Errorf("endpoint %d error: %w", i, err)
		}
		_ = result // Unused in this context

		endpoints = append(endpoints, output.EndpointConfig)

		// Collect warnings and best practices
		for _, w := range output.Warnings {
			allWarnings = append(allWarnings, fmt.Sprintf("Endpoint '%s': %s", endpointInput.Path, w))
		}
		for _, bp := range output.BestPractices {
			allBestPractices = append(allBestPractices, fmt.Sprintf("Endpoint '%s': %s", endpointInput.Path, bp))
		}
	}

	config["endpoints"] = endpoints

	// Add recommended global settings
	allBestPractices = append(allBestPractices, "Consider adding CORS configuration (security/cors) in extra_config if serving web clients")
	allBestPractices = append(allBestPractices, "Consider adding telemetry/logging configuration for observability")

	return nil, GenerateBasicConfigOutput{
		Config:        config,
		Warnings:      allWarnings,
		BestPractices: allBestPractices,
	}, nil
}

// RegisterGenerationTools registers all configuration generation tools
func RegisterGenerationTools(server *mcp.Server) error {
	// Ensure feature data is loaded
	if featureCatalog == nil {
		if err := LoadFeatureData(); err != nil {
			return fmt.Errorf("failed to load feature data: %w", err)
		}
	}

	// Tool 11: get_feature_config_template
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "get_feature_config_template",
			Description: "Get configuration template for a specific KrakenD feature with required/optional fields",
		},
		GetFeatureConfigTemplate,
	)

	// Tool 12: generate_endpoint_config
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "generate_endpoint_config",
			Description: "Generate a complete endpoint configuration with backends and best practices",
		},
		GenerateEndpointConfig,
	)

	// Tool 13: generate_backend_config
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "generate_backend_config",
			Description: "Generate a backend service configuration",
		},
		GenerateBackendConfig,
	)

	// Bonus tool: generate_basic_config (helper for complete configs)
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "generate_basic_config",
			Description: "Generate a complete basic KrakenD configuration with multiple endpoints",
		},
		GenerateBasicConfig,
	)

	return nil
}
