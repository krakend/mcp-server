package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const fullConfigFixture = "../internal/observability/testdata/krakend-full.json"

func TestAnalyzeConfigObservabilityTool(t *testing.T) {
	_, out, err := AnalyzeConfigObservability(context.Background(), nil, AnalyzeConfigObservabilityInput{Config: fullConfigFixture})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.TelemetryEnabled || len(out.Endpoints) == 0 || out.KrakenDVersion == "" || out.Guidance == "" {
		t.Errorf("incomplete output: telemetry=%v endpoints=%d version=%q", out.TelemetryEnabled, len(out.Endpoints), out.KrakenDVersion)
	}
}

func TestAnalyzeConfigObservabilityRejectsTemplates(t *testing.T) {
	tmpl := filepath.Join(t.TempDir(), "krakend.tmpl")
	if err := os.WriteFile(tmpl, []byte(`{"version": 3, "endpoints": [{{ template "endpoints.tmpl" . }}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := AnalyzeConfigObservability(context.Background(), nil, AnalyzeConfigObservabilityInput{Config: tmpl})
	if err == nil || !strings.Contains(err.Error(), "Flexible Configuration") {
		t.Errorf("expected a flexible configuration error, got %v", err)
	}
}

func TestGetDashboardRecipesTool(t *testing.T) {
	_, out, err := GetDashboardRecipes(context.Background(), nil, GetDashboardRecipesInput{
		Intents: []string{"resilience", "errors"},
		Config:  fullConfigFixture,
		Format:  "grafana_panel",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Recipes) == 0 {
		t.Fatal("expected recipes")
	}
	for _, r := range out.Recipes {
		if r.GrafanaPanel == nil {
			t.Errorf("%s: missing grafana_panel", r.ID)
		}
	}

	_, _, err = GetDashboardRecipes(context.Background(), nil, GetDashboardRecipesInput{SemanticConvention: "2.0"})
	if err == nil {
		t.Error("invalid semantic_convention should fail")
	}
}

// Registration computes JSON schemas for the input/output types and panics on types the SDK
// can't describe (e.g. recursive structs), which would crash the server at startup.
func TestRegisterObservabilityTools(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	if err := RegisterObservabilityTools(server); err != nil {
		t.Fatalf("register: %v", err)
	}
}

func TestGetMetricsCatalogTool(t *testing.T) {
	_, out, err := GetMetricsCatalog(context.Background(), nil, GetMetricsCatalogInput{Groups: []string{"circuit-breaker"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Metrics) == 0 || out.KrakenDVersion == "" {
		t.Errorf("metrics=%d version=%q", len(out.Metrics), out.KrakenDVersion)
	}
	if _, _, err := GetMetricsCatalog(context.Background(), nil, GetMetricsCatalogInput{Groups: []string{"nope"}}); err == nil {
		t.Error("unknown group should fail")
	}
}

func TestLintDashboardTool(t *testing.T) {
	dash := `{"uid": "krakend-ai-x", "tags": ["krakend", "generated-by-claude"], "title": "t",
	  "templating": {"list": [{"name": "datasource", "type": "datasource", "current": {}}, {"name": "job", "type": "query", "includeAll": true, "current": {"text": "All", "value": "$__all"}}]},
	  "panels": [{"id": 1, "type": "timeseries", "title": "Goroutines", "description": "d",
	    "datasource": {"type": "prometheus", "uid": "${datasource}"}, "gridPos": {"x": 0, "y": 0, "w": 12, "h": 8},
	    "fieldConfig": {"defaults": {"unit": "short"}},
	    "targets": [{"refId": "A", "expr": "rate(go_goroutines{job=~\"$job\", instance=\"localhost:9090\"}[$__rate_interval])"},
	                {"refId": "B", "expr": "sum(rate(http_server_duration_count{job=~\"$job\"}[$__rate_interval]))"}]}]}`
	_, res, err := LintDashboard(context.Background(), nil, LintDashboardInput{Dashboard: dash, UIDPrefix: "krakend-ai-",
		RequiredTags: []string{"krakend", "generated-by-claude"}, Expand: true, SemanticConvention: "1.27"})
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(res.Errors, "\n")
	for _, want := range []string{"which is a gauge", "environment-specific", "hostname from this environment"} {
		if !strings.Contains(all, want) {
			t.Errorf("want an error containing %q, got:\n%s", want, all)
		}
	}
	if len(res.Expanded) != 2 || strings.Contains(res.Expanded[1].Expr, "$") {
		t.Errorf("want 2 expanded queries without variables: %+v", res.Expanded)
	}
	if len(res.Converted) != 1 || !strings.Contains(res.Converted[0].To, "http_server_request_duration_seconds_count") {
		t.Errorf("want the http_server query converted to 1.27: %+v", res.Converted)
	}
}
