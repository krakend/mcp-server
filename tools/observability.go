package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/krakend/mcp-server/internal/observability"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	observabilityCatalogFile = "data/observability/catalog.json"
	observabilityRecipesFile = "data/observability/recipes.json"
)

var (
	observabilityOnce sync.Once
	observabilityData *observability.Data
	observabilityErr  error
)

// loadObservabilityData parses the embedded catalog and recipes once.
func loadObservabilityData() (*observability.Data, error) {
	observabilityOnce.Do(func() {
		cat, err := defaultDataProvider.ReadFile(observabilityCatalogFile)
		if err != nil {
			observabilityErr = fmt.Errorf("observability catalog not embedded: %w", err)
			return
		}
		rec, err := defaultDataProvider.ReadFile(observabilityRecipesFile)
		if err != nil {
			observabilityErr = fmt.Errorf("observability recipes not embedded: %w", err)
			return
		}
		observabilityData, observabilityErr = observability.Load(cat, rec)
	})
	return observabilityData, observabilityErr
}

// parseObservabilityConfig reads a rendered KrakenD config (JSON string or file path).
func parseObservabilityConfig(config string) (map[string]any, error) {
	content, err := readConfigContent(config)
	if err != nil {
		return nil, err
	}
	if strings.Contains(content, "{{") {
		return nil, fmt.Errorf("this looks like a Flexible Configuration template: render it first " +
			"(e.g. FC_ENABLE=1 FC_OUT=/tmp/krakend-rendered.json ... krakend check -c krakend.tmpl) and pass the rendered file")
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(content), &cfg); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	return cfg, nil
}

// AnalyzeConfigObservabilityInput defines input for analyze_config_observability.
type AnalyzeConfigObservabilityInput struct {
	Config string `json:"config" jsonschema:"Rendered KrakenD configuration as JSON string or file path (render Flexible Configuration templates first)"`
}

// AnalyzeConfigObservabilityOutput wraps the analysis.
type AnalyzeConfigObservabilityOutput struct {
	observability.Analysis
	KrakenDVersion string `json:"catalog_krakend_version"`
	Guidance       string `json:"guidance"`
}

const analyzeGuidance = "label_values are PREDICTIONS from the config; Prometheus decides. Confirm with list_prometheus_label_values before relying on them. " +
	"http_route@global is the endpoint label on http_server_*/krakend_proxy_* metrics; http_route@backend is the backend url_pattern on krakend_backend_*/http_client_* metrics. " +
	"http_route@agent is the async agent name on krakend_proxy_* metrics (agents have no http_server_* series, no http_request_method on proxy series and no krakend_endpoint_route on backend series). " +
	"If telemetry_enabled is false, stop: no dashboard; offer the telemetry gap instead (see warnings). " +
	"An empty Prometheus is not a reason to stop: build from the config (label_values, semantic_convention). " +
	"A disabled group means its recipes will return no data until the matching gap is fixed. " +
	"Gaps are for the user: present the ones relevant to the dashboard in your reply (what it adds, the option, the snippet) and offer to apply them; edit the config only after they agree, then validate it."

// AnalyzeConfigObservability predicts the metrics a configuration produces.
func AnalyzeConfigObservability(_ context.Context, _ *mcp.CallToolRequest, input AnalyzeConfigObservabilityInput) (*mcp.CallToolResult, AnalyzeConfigObservabilityOutput, error) {
	data, err := loadObservabilityData()
	if err != nil {
		return nil, AnalyzeConfigObservabilityOutput{}, err
	}
	cfg, err := parseObservabilityConfig(input.Config)
	if err != nil {
		return nil, AnalyzeConfigObservabilityOutput{}, err
	}
	a := data.Analyze(cfg)
	enabled := 0
	for _, g := range a.Groups {
		if g.Enabled {
			enabled++
		}
	}
	return &mcp.CallToolResult{
			Meta: map[string]any{"endpoints": len(a.Endpoints), "groups_enabled": enabled, "gaps": len(a.Gaps)},
		}, AnalyzeConfigObservabilityOutput{
			Analysis:       *a,
			KrakenDVersion: data.Catalog.KrakenDVersion,
			Guidance:       analyzeGuidance,
		}, nil
}

// GetDashboardRecipesInput defines input for get_dashboard_recipes.
type GetDashboardRecipesInput struct {
	Intents            []string `json:"intents,omitempty" jsonschema:"What the dashboard should answer: overview, traffic, errors, latency, layers, backends, resilience, ai, websocket, kafka, redis, agents (async agents), runtime, a recipe id, or all. With format grafana_panel keep it to 2-3 topics per call (the result must stay under ~45 KB; a larger request returns the size of each intent so you can split it). Default: overview"`
	SemanticConvention string   `json:"semantic_convention,omitempty" jsonschema:"Metric naming: empty for default names, 1.27 for OpenTelemetry semantic convention 1.27, or auto to detect it from metric_names (falls back to the config)"`
	MetricNames        []string `json:"metric_names,omitempty" jsonschema:"Live metric names from Prometheus (e.g. Grafana list_prometheus_metric_names). Used for auto-detection and to drop recipes whose metrics don't exist"`
	ScrapeLabels       []string `json:"scrape_labels,omitempty" jsonschema:"Scrape labels the dashboard filters by, each becoming a $variable. Default: job, instance. Add app only if Prometheus has it"`
	Endpoint           string   `json:"endpoint,omitempty" jsonschema:"Pin all queries to one endpoint (config route like /users/{id} or label value /users/:id) instead of the $endpoint variable"`
	Config             string   `json:"config,omitempty" jsonschema:"Optional rendered KrakenD config (JSON or file path): drops recipes for disabled metric groups and fills config-specific parameters such as rate-limited routes"`
	Format             string   `json:"format,omitempty" jsonschema:"promql (default) or grafana_panel: a ready Grafana panel per recipe (title, unit, thresholds, status colors) instead of separate title/unit/targets"`
	PanelsFile         string   `json:"panels_file,omitempty" jsonschema:"Write the ready Grafana panels to this .json file (relative to the project directory) instead of returning them inline; implies format grafana_panel and has no size limit. Pass the file to the skill's build_dashboard.py, which lays the panels out from a short layout. The result then lists recipe ids and flags only"`
	OnlyWithData       bool     `json:"only_with_data,omitempty" jsonschema:"Drop recipes whose metrics aren't in metric_names yet. Default false: recipes the config enables are kept and flagged no_data_yet, so dashboards include error/timeout/breaker panels before the first error happens"`
}

// GetDashboardRecipes returns tested PromQL recipes rendered for the user's setup.
func GetDashboardRecipes(_ context.Context, _ *mcp.CallToolRequest, input GetDashboardRecipesInput) (*mcp.CallToolResult, observability.RecipeResponse, error) {
	data, err := loadObservabilityData()
	if err != nil {
		return nil, observability.RecipeResponse{}, err
	}
	req := observability.RecipeRequest{
		Intents:            input.Intents,
		SemanticConvention: input.SemanticConvention,
		MetricNames:        input.MetricNames,
		ScrapeLabels:       input.ScrapeLabels,
		Endpoint:           input.Endpoint,
		Format:             input.Format,
		OnlyWithData:       input.OnlyWithData,
	}
	var panelsPath string
	if input.PanelsFile != "" {
		if panelsPath, err = projectJSONPath(input.PanelsFile); err != nil {
			return nil, observability.RecipeResponse{}, err
		}
		req.Format, req.NoSizeLimit = "grafana_panel", true
	}
	if input.Config != "" {
		cfg, err := parseObservabilityConfig(input.Config)
		if err != nil {
			return nil, observability.RecipeResponse{}, err
		}
		req.Analysis = data.Analyze(cfg)
	}
	resp, err := data.RenderRecipes(req)
	if err != nil {
		return nil, observability.RecipeResponse{}, err
	}
	if panelsPath != "" {
		if err := writePanelsFile(panelsPath, resp); err != nil {
			return nil, observability.RecipeResponse{}, err
		}
	}
	return &mcp.CallToolResult{
		Meta: map[string]any{"recipes": len(resp.Recipes), "unavailable": len(resp.Unavailable), "semantic_convention": resp.SemanticConvention},
	}, *resp, nil
}

// GetMetricsCatalogInput defines input for get_metrics_catalog.
type GetMetricsCatalogInput struct {
	Groups             []string `json:"groups,omitempty" jsonschema:"Metric groups to return: global, proxy, backend, backend-roundtrip, backend-connection, backend-payload, circuit-breaker, redis-pool, messaging, websocket, ai, runtime. Default: all"`
	Query              string   `json:"query,omitempty" jsonschema:"Case-insensitive search in metric names, OTel names, meanings and labels (e.g. token, timeout, status)"`
	Names              []string `json:"names,omitempty" jsonschema:"Exact metric names to look up, in either naming convention, with or without _bucket/_count/_sum, or OTel instrument names. Unknown names are listed in not_found"`
	SemanticConvention string   `json:"semantic_convention,omitempty" jsonschema:"Naming to return: empty for default names, 1.27 for OpenTelemetry semantic convention 1.27, or auto to detect it from metric_names"`
	MetricNames        []string `json:"metric_names,omitempty" jsonschema:"Live metric names from Prometheus, used when semantic_convention is auto"`
}

// GetMetricsCatalog returns what KrakenD metrics mean: names, types, units, labels, the config
// that enables them and known gotchas.
func GetMetricsCatalog(_ context.Context, _ *mcp.CallToolRequest, input GetMetricsCatalogInput) (*mcp.CallToolResult, observability.CatalogResponse, error) {
	data, err := loadObservabilityData()
	if err != nil {
		return nil, observability.CatalogResponse{}, err
	}
	resp, err := data.QueryCatalog(observability.CatalogRequest{
		Groups:             input.Groups,
		Query:              input.Query,
		Names:              input.Names,
		SemanticConvention: input.SemanticConvention,
		MetricNames:        input.MetricNames,
	})
	if err != nil {
		return nil, observability.CatalogResponse{}, err
	}
	return &mcp.CallToolResult{
		Meta: map[string]any{"metrics": len(resp.Metrics), "not_found": len(resp.NotFound), "semantic_convention": resp.SemanticConvention},
	}, *resp, nil
}

// LintDashboardInput defines input for lint_dashboard.
type LintDashboardInput struct {
	Dashboard          string   `json:"dashboard" jsonschema:"Grafana dashboard JSON, as a file path (preferred) or a JSON string. A {dashboard: ...} wrapper is accepted"`
	UIDPrefix          string   `json:"uid_prefix,omitempty" jsonschema:"Required uid prefix, e.g. krakend-ai-"`
	RequiredTags       []string `json:"required_tags,omitempty" jsonschema:"Tags the dashboard must have, e.g. [krakend, generated-by-claude]"`
	SemanticConvention string   `json:"semantic_convention,omitempty" jsonschema:"1.27 to also return every query that changes under OpenTelemetry semantic convention 1.27, with its converted form (for queries you wrote yourself; get_dashboard_recipes already converts recipes)"`
	Expand             bool     `json:"expand,omitempty" jsonschema:"Also return every query with Grafana variables replaced (All = .*), ready for a Prometheus instant query"`
}

// LintDashboard checks a dashboard against the KrakenD metrics catalog and portability rules.
func LintDashboard(_ context.Context, _ *mcp.CallToolRequest, input LintDashboardInput) (*mcp.CallToolResult, observability.LintResult, error) {
	data, err := loadObservabilityData()
	if err != nil {
		return nil, observability.LintResult{}, err
	}
	content, err := readConfigContent(input.Dashboard)
	if err != nil {
		return nil, observability.LintResult{}, err
	}
	var dash map[string]any
	if err := json.Unmarshal([]byte(content), &dash); err != nil {
		return nil, observability.LintResult{}, fmt.Errorf("invalid dashboard JSON: %w", err)
	}
	if input.SemanticConvention != "" && input.SemanticConvention != observability.Convention127 {
		return nil, observability.LintResult{}, fmt.Errorf("semantic_convention must be empty or \"1.27\", got %q", input.SemanticConvention)
	}
	res := data.LintDashboard(dash, observability.LintOptions{
		UIDPrefix: input.UIDPrefix, RequiredTags: input.RequiredTags,
		SemanticConvention: input.SemanticConvention, Expand: input.Expand,
	})
	return &mcp.CallToolResult{
		Meta: map[string]any{"errors": len(res.Errors), "warnings": len(res.Warnings), "queries": res.Queries},
	}, res, nil
}

// RegisterObservabilityTools registers get_metrics_catalog, analyze_config_observability,
// get_dashboard_recipes and lint_dashboard.
func RegisterObservabilityTools(server *mcp.Server) error {
	if _, err := loadObservabilityData(); err != nil {
		return err
	}
	mcp.AddTool(server,
		&mcp.Tool{
			Name: "get_metrics_catalog",
			Description: "Look up what KrakenD metrics mean, as exposed in Prometheus: name, OpenTelemetry instrument, type, unit, labels and their meaning, the config that enables each metric, edition, and gotchas (lazy metrics, naming quirks, label pitfalls). " +
				"Filter by group, search text or exact names; returns names in the default or OpenTelemetry semantic convention 1.27 naming (auto-detected from metric_names).",
		},
		GetMetricsCatalog,
	)
	mcp.AddTool(server,
		&mcp.Tool{
			Name: "analyze_config_observability",
			Description: "Predict the metrics a KrakenD configuration exposes: enabled metric groups (HTTP layers, backend round trip/payload/connection, circuit breakers, Redis pools, Kafka, WebSocket, AI), " +
				"the label values to expect (endpoint and backend routes, hosts, circuit breaker names, LLM providers/models, topics), rate-limited endpoints, " +
				"async agents, and gaps: config changes that would enable missing metrics, with snippets. Use before building Grafana dashboards. " +
				"Show the user the gaps relevant to their request (what each adds, the option, the snippet) and offer to apply them.",
		},
		AnalyzeConfigObservability,
	)
	mcp.AddTool(server,
		&mcp.Tool{
			Name: "get_dashboard_recipes",
			Description: "Get tested PromQL recipes for KrakenD Grafana dashboards by intent (overview, latency, errors, backends, ai, ...). " +
				"Renders selectors for the user's scrape labels, converts to OpenTelemetry semantic convention 1.27 names when needed (auto-detected from metric_names), " +
				"drops recipes the configuration doesn't enable (with the reason and the config fix to suggest to the user), keeps recipes whose metrics simply haven't happened yet (flagged no_data_yet, e.g. error panels before the first error), and optionally returns Grafana panel JSON.",
		},
		GetDashboardRecipes,
	)
	mcp.AddTool(server,
		&mcp.Tool{
			Name: "lint_dashboard",
			Description: "Check a Grafana dashboard for KrakenD metrics before pushing it: unknown metric names (against the KrakenD metrics catalog, both naming conventions), " +
				"rate()/increase() over gauges, histogram_quantile without le, undefined or unused variables, overlapping panels, missing units, " +
				"and anything that ties the JSON to the environment it was built in (datasource uids, literal job/instance/host values, saved variable selections), " +
				"so it imports into a production Grafana unchanged. Optionally returns every query expanded for Prometheus, and 1.27 conversions of hand-written queries. Fix every error.",
		},
		LintDashboard,
	)
	return nil
}

// projectJSONPath resolves a panels_file path inside the working directory (the user's project):
// the tool never writes anywhere else, and only .json files.
func projectJSONPath(p string) (string, error) {
	if !strings.HasSuffix(strings.ToLower(p), ".json") {
		return "", fmt.Errorf("panels_file must be a .json file, got %q", p)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(wd, p)
	}
	abs = filepath.Clean(abs)
	if rel, err := filepath.Rel(wd, abs); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("panels_file must be inside the project directory (%s), got %q", wd, p)
	}
	return abs, nil
}

// writePanelsFile saves the panels as {"order": [ids], "panels": {id: panel}, "variables": [...]}
// and strips them from the response.
func writePanelsFile(path string, resp *observability.RecipeResponse) error {
	panels := make(map[string]any, len(resp.Recipes))
	order := make([]string, 0, len(resp.Recipes))
	for i := range resp.Recipes {
		r := &resp.Recipes[i]
		panels[r.ID] = r.GrafanaPanel
		order = append(order, r.ID)
		if t, ok := r.GrafanaPanel["title"].(string); ok {
			r.Title = t // keep titles in the result so the layout can be planned
		}
		r.GrafanaPanel = nil
	}
	b, err := json.MarshalIndent(map[string]any{"order": order, "panels": panels, "variables": resp.Variables}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing panels_file: %w", err)
	}
	resp.PanelsFile = path
	return nil
}
