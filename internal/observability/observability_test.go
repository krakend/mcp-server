package observability

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Fixtures: testdata/krakend-full.json is a config with every component enabled, and the
// exposition files are the real Prometheus scrapes of that config under traffic, with the
// default naming and with semantic_convention 1.27.

func loadTestData(t *testing.T) *Data {
	t.Helper()
	cat, err := os.ReadFile("../../tools/data/observability/catalog.json")
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	rec, err := os.ReadFile("../../tools/data/observability/recipes.json")
	if err != nil {
		t.Fatalf("read recipes: %v", err)
	}
	d, err := Load(cat, rec)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return d
}

func loadConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse config: %v", err)
	}
	return cfg
}

var seriesRe = regexp.MustCompile(`^([a-zA-Z_:][a-zA-Z0-9_:]*)\{(.*)\} `)
var labelRe = regexp.MustCompile(`(\w+)="((?:[^"\\]|\\.)*)"`)

// observedLabelValues returns metric -> label -> values from an exposition file.
func observedLabelValues(t *testing.T, path string) map[string]map[string]map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read exposition: %v", err)
	}
	out := map[string]map[string]map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		m := seriesRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if out[m[1]] == nil {
			out[m[1]] = map[string]map[string]bool{}
		}
		for _, l := range labelRe.FindAllStringSubmatch(m[2], -1) {
			if out[m[1]][l[1]] == nil {
				out[m[1]][l[1]] = map[string]bool{}
			}
			out[m[1]][l[1]][l[2]] = true
		}
	}
	return out
}

func TestRouteLabel(t *testing.T) {
	cases := map[string]string{
		"/users/{id}":        "/users/:id",
		"/a/{x}/b/{y}":       "/a/:x/b/:y",
		"":                   "/",
		"/__debug/static":    "/__debug/static",
		"/{file}":            "/:file",
		"/orders/{resp0_id}": "/orders/:resp0_id",
	}
	for in, want := range cases {
		if got := RouteLabel(in); got != want {
			t.Errorf("RouteLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAnalyzeFullConfig(t *testing.T) {
	d := loadTestData(t)
	a := d.Analyze(loadConfig(t, "testdata/krakend-full.json"))

	if !a.TelemetryEnabled || a.SemanticConvention != "" {
		t.Fatalf("telemetry=%v semconv=%q", a.TelemetryEnabled, a.SemanticConvention)
	}
	for _, g := range []string{"global", "proxy", "backend", "backend-roundtrip", "backend-payload",
		"backend-connection", "circuit-breaker", "redis-pool", "messaging", "websocket", "ai", "runtime"} {
		if !a.Groups[g].Enabled {
			t.Errorf("group %s should be enabled: %+v", g, a.Groups[g])
		}
	}
	if len(a.Gaps) != 0 {
		t.Errorf("expected no gaps for a fully enabled config, got %+v", a.Gaps)
	}

	var limited []string
	for _, ep := range a.Endpoints {
		if ep.RateLimit != nil {
			limited = append(limited, ep.LabelValue)
		}
	}
	// qos/ratelimit/service is set, so every endpoint is rate limited
	if len(limited) != len(a.Endpoints) {
		t.Errorf("service rate limit should mark all %d endpoints, got %d", len(a.Endpoints), len(limited))
	}

	want := map[string][]string{
		"cb_name":         {"cb-a", "cb-b-http", "cb-breaker"},
		"provider":        {"anthropic", "openai"},
		"messaging_topic": {"krakend-demo"},
		"pool_name":       {"localhost:6379"},
	}
	for label, values := range want {
		for _, v := range values {
			if !slices.Contains(a.LabelValues[label], v) {
				t.Errorf("label %s: missing %q in %v", label, v, a.LabelValues[label])
			}
		}
	}
	for _, w := range []string{"AMQP and generic pubsub", "Prompt guard blocks return HTTP 500", "200 OK", "WebSocket server metrics"} {
		found := false
		for _, got := range a.Warnings {
			if strings.Contains(got, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing warning containing %q in %v", w, a.Warnings)
		}
	}
}

// Every label value KrakenD actually exposed must have been predicted from the config.
func TestPredictedLabelValuesMatchRealScrape(t *testing.T) {
	d := loadTestData(t)
	a := d.Analyze(loadConfig(t, "testdata/krakend-full.json"))
	obs := observedLabelValues(t, "testdata/exposition-default.txt")

	checks := []struct {
		metric, label, predicted string
	}{
		{"http_server_duration_count", "http_route", "http_route@global"},
		{"krakend_proxy_duration_count", "http_route", "http_route@global"},
		{"krakend_backend_duration_count", "krakend_endpoint_route", "krakend_endpoint_route"},
		{"krakend_backend_duration_count", "http_route", "http_route@backend"},
		{"http_client_duration_count", "server_address", "server_address"},
		{"http_client_duration_count", "server_port", "server_port"},
		{"krakend_cb_state_close_to_open_total", "cb_name", "cb_name"},
		{"krakend_llm_tokens_total", "provider", "provider"},
		{"krakend_llm_tokens_total", "model", "model"},
		{"messaging_write_body_size_bytes_count", "messaging_topic", "messaging_topic"},
		{"db_client_connections_max", "pool_name", "pool_name"},
		{"websocket_client_connections_count_total", "url_path", "url_path"},
	}
	for _, c := range checks {
		seen := obs[c.metric][c.label]
		if len(seen) == 0 {
			t.Errorf("fixture has no %s{%s}: test data out of date?", c.metric, c.label)
			continue
		}
		for v := range seen {
			if !slices.Contains(a.LabelValues[c.predicted], v) {
				t.Errorf("%s{%s=%q} observed but not predicted (predicted %s: %v)",
					c.metric, c.label, v, c.predicted, a.LabelValues[c.predicted])
			}
		}
	}
}

func TestAnalyzeGaps(t *testing.T) {
	d := loadTestData(t)
	cfg := map[string]any{
		"version": float64(3),
		"extra_config": map[string]any{
			"telemetry/opentelemetry": map[string]any{
				"exporters": map[string]any{"prometheus": []any{map[string]any{"name": "p"}}},
			},
		},
		"endpoints": []any{map[string]any{"endpoint": "/a", "backend": []any{map[string]any{"url_pattern": "/x", "host": []any{"http://svc:8080"}}}}},
	}
	// No layers.backend.metrics: KrakenD enables every backend option (verified on eefea5cf).
	a := d.Analyze(cfg)
	for _, g := range []string{"backend-roundtrip", "backend-payload", "backend-connection"} {
		if !a.Groups[g].Enabled {
			t.Errorf("%s should be on without a layers.backend.metrics block", g)
		}
	}
	for _, g := range a.Gaps {
		if strings.HasPrefix(g.Option, "layers.backend.metrics.") {
			t.Errorf("unexpected backend gap with default layers: %s", g.Option)
		}
	}

	// Once layers.backend.metrics exists, options it doesn't set are off.
	tel := cfg["extra_config"].(map[string]any)["telemetry/opentelemetry"].(map[string]any)
	tel["layers"] = map[string]any{"backend": map[string]any{"metrics": map[string]any{"round_trip": true}}}
	a = d.Analyze(cfg)
	if !a.Groups["backend-roundtrip"].Enabled || a.Groups["backend-payload"].Enabled || a.Groups["backend-connection"].Enabled {
		t.Errorf("explicit metrics block: want only round_trip on, got %+v", a.Layers)
	}
	var options []string
	for _, g := range a.Gaps {
		options = append(options, g.Option)
		if g.Option == "layers.backend.metrics.read_payload" {
			if len(g.Unlocks) == 0 || g.Snippet == nil {
				t.Errorf("read_payload gap should list recipes and a snippet: %+v", g)
			}
			m := g.Snippet["telemetry/opentelemetry"].(map[string]any)["layers"].(map[string]any)["backend"].(map[string]any)["metrics"].(map[string]any)
			if m["round_trip"] != true || m["read_payload"] != true {
				t.Errorf("snippet must keep round_trip on while adding read_payload: %v", m)
			}
		}
	}
	for _, want := range []string{"layers.backend.metrics.read_payload",
		"layers.backend.metrics.detailed_connection", "exporters.prometheus[].go_metrics / process_metrics"} {
		if !slices.Contains(options, want) {
			t.Errorf("missing gap %q in %v", want, options)
		}
	}

	none := d.Analyze(map[string]any{"endpoints": []any{}})
	if none.TelemetryEnabled || len(none.Gaps) != 1 || none.Gaps[0].Snippet == nil {
		t.Errorf("no telemetry should yield exactly one gap with a snippet: %+v", none.Gaps)
	}
}

func TestBackendOverrideEnablesGroupPartially(t *testing.T) {
	d := loadTestData(t)
	cfg := map[string]any{
		"extra_config": map[string]any{"telemetry/opentelemetry": map[string]any{
			"exporters": map[string]any{"prometheus": []any{map[string]any{"name": "p"}}},
			"layers":    map[string]any{"backend": map[string]any{"metrics": map[string]any{}}}}},
		"endpoints": []any{map[string]any{"endpoint": "/a", "backend": []any{map[string]any{
			"url_pattern": "/x",
			"extra_config": map[string]any{"telemetry/opentelemetry": map[string]any{
				"backend": map[string]any{"metrics": map[string]any{"round_trip": true}}}},
		}}}},
	}
	g := d.Analyze(cfg).Groups["backend-roundtrip"]
	if !g.Enabled || !g.Partial {
		t.Errorf("per-backend round_trip should enable the group partially: %+v", g)
	}
}

func TestConvertExprSemconv127(t *testing.T) {
	d := loadTestData(t)
	cases := map[string]string{
		`histogram_quantile(0.95, sum by (le) (rate(http_server_duration_bucket{job="x"}[5m])))`: `histogram_quantile(0.95, sum by (le) (rate(http_server_request_duration_seconds_bucket{job="x"}[5m])))`,
		`sum(rate(http_client_request_size_total[5m]))`:                                          `sum(rate(http_client_request_body_size_bytes_sum[5m]))`,
		`rate(http_client_duration_count[1m]) / rate(http_server_response_size_sum[1m])`:         `rate(http_client_request_duration_seconds_count[1m]) / rate(http_server_response_body_size_bytes_sum[1m])`,
		`http_client_request_dns_duration_count`:                                                 `http_client_request_dns_duration_seconds_count`,
		`krakend_backend_duration_bucket`:                                                        `krakend_backend_duration_bucket`,
	}
	for in, want := range cases {
		if got := d.ConvertExpr(in, Convention127); got != want {
			t.Errorf("ConvertExpr(%s)\n got %s\nwant %s", in, got, want)
		}
		if got := d.ConvertExpr(in, ConventionDefault); got != in {
			t.Errorf("default convention must not change %s", in)
		}
	}
}

// Every metric a converted recipe uses must exist in the real 1.27 scrape.
func TestConvertedRecipesUseRealSemconv127Names(t *testing.T) {
	d := loadTestData(t)
	obs := observedLabelValues(t, "testdata/exposition-semconv127.txt")
	resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"all"}, SemanticConvention: Convention127})
	if err != nil {
		t.Fatal(err)
	}
	metricRe := regexp.MustCompile(`\b((?:http|krakend|websocket|messaging|db_client)_[a-z0-9_]+)\{`)
	for _, r := range resp.Recipes {
		for _, tg := range r.Targets {
			if strings.Contains(tg.Expr, "or vector(0)") { // event-only counters, 0 until the first event
				continue
			}
			for _, m := range metricRe.FindAllStringSubmatch(tg.Expr, -1) {
				name := m[1]
				if strings.HasSuffix(name, "_bucket") { // buckets were trimmed from the fixture
					name = strings.TrimSuffix(name, "_bucket") + "_count"
				}
				if obs[name] == nil && !r.MayBeEmpty {
					t.Errorf("recipe %s uses %s, not in the 1.27 scrape", r.ID, m[1])
				}
			}
		}
	}
}

func TestDetectConvention(t *testing.T) {
	d := loadTestData(t)
	cases := []struct {
		names        []string
		conv, status string
	}{
		{[]string{"http_server_duration_bucket", "krakend_proxy_duration_bucket"}, ConventionDefault, DetectionFound},
		{[]string{"http_server_request_duration_seconds_bucket", "krakend_proxy_duration_bucket"}, Convention127, DetectionFound},
		{[]string{"http_server_duration_bucket", "http_server_request_duration_seconds_bucket"}, ConventionDefault, DetectionMixed},
		{[]string{"go_goroutines"}, ConventionDefault, DetectionUnknown},
	}
	for _, c := range cases {
		conv, status := d.DetectConvention(c.names)
		if conv != c.conv || status != c.status {
			t.Errorf("DetectConvention(%v) = %q,%q want %q,%q", c.names, conv, status, c.conv, c.status)
		}
	}
}

func TestRenderRecipes(t *testing.T) {
	d := loadTestData(t)
	leftover := regexp.MustCompile(`\{SEL|\{BSEL|\{J[},]|\$ratelimit_status`)

	t.Run("default intent and selectors", func(t *testing.T) {
		resp, err := d.RenderRecipes(RecipeRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Recipes) == 0 {
			t.Fatal("overview should return recipes")
		}
		for _, r := range resp.Recipes {
			for _, tg := range r.Targets {
				if leftover.MatchString(tg.Expr) {
					t.Errorf("%s: unexpanded placeholder in %s", r.ID, tg.Expr)
				}
			}
		}
		if resp.Selectors["SEL"] != `job=~"$job", instance=~"$instance", http_route=~"$endpoint"` {
			t.Errorf("SEL = %s", resp.Selectors["SEL"])
		}
		all, _ := d.RenderRecipes(RecipeRequest{Intents: []string{"all"}})
		for _, v := range []string{"job", "instance", "endpoint", "backend_host"} {
			if !slices.Contains(all.Variables, v) {
				t.Errorf("variables %v missing %s", all.Variables, v)
			}
		}
	})

	t.Run("custom scrape labels and pinned endpoint", func(t *testing.T) {
		resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"layers"}, ScrapeLabels: []string{"app"}, Endpoint: "/users/{id}"})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range resp.Recipes {
			for _, tg := range r.Targets {
				if strings.Contains(tg.Expr, "$job") || strings.Contains(tg.Expr, "$endpoint") {
					t.Errorf("%s still uses default variables: %s", r.ID, tg.Expr)
				}
				if !strings.Contains(tg.Expr, `="/users/:id"`) {
					t.Errorf("%s not pinned to /users/:id: %s", r.ID, tg.Expr)
				}
			}
		}
	})

	t.Run("rate limit uses config routes", func(t *testing.T) {
		a := d.Analyze(loadConfig(t, "testdata/krakend-full.json"))
		resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"ratelimit.rejections"}, Analysis: a})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Recipes) != 1 {
			t.Fatalf("want 1 recipe, got %d (%+v)", len(resp.Recipes), resp.Unavailable)
		}
		expr := resp.Recipes[0].Targets[0].Expr
		if !strings.Contains(expr, `/secure/:id`) || !strings.Contains(expr, `=~"503"`) || leftover.MatchString(expr) {
			t.Errorf("unexpected rate limit expr: %s", expr)
		}
	})

	t.Run("disabled groups become unavailable with a fix", func(t *testing.T) {
		cfg := map[string]any{"extra_config": map[string]any{"telemetry/opentelemetry": map[string]any{
			"exporters": map[string]any{"prometheus": []any{map[string]any{"name": "p"}}},
			"layers":    map[string]any{"backend": map[string]any{"metrics": map[string]any{}}}}},
			"endpoints": []any{}}
		resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"backends"}, Analysis: d.Analyze(cfg)})
		if err != nil {
			t.Fatal(err)
		}
		var fixes []string
		for _, u := range resp.Unavailable {
			fixes = append(fixes, u.Fix)
		}
		if !slices.Contains(fixes, "layers.backend.metrics.round_trip") {
			t.Errorf("expected round_trip fix among %v", fixes)
		}
	})

	t.Run("auto convention; missing live metrics are flagged, not dropped", func(t *testing.T) {
		names := []string{"http_server_request_duration_seconds_bucket", "http_server_request_duration_seconds_count",
			"http_server_request_duration_seconds_sum"}
		resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"latency"}, SemanticConvention: "auto", MetricNames: names})
		if err != nil {
			t.Fatal(err)
		}
		if resp.SemanticConvention != Convention127 {
			t.Fatalf("convention = %q", resp.SemanticConvention)
		}
		if len(resp.Unavailable) != 0 {
			t.Errorf("live data must not remove recipes, got unavailable %+v", resp.Unavailable)
		}
		flagged := 0
		for _, r := range resp.Recipes {
			for _, tg := range r.Targets {
				if strings.Contains(tg.Expr, "http_server_duration") {
					t.Errorf("%s not converted: %s", r.ID, tg.Expr)
				}
			}
			uses := strings.Contains(fmt.Sprint(r.Targets), "krakend_backend_duration")
			if uses && (!r.NoDataYet || r.NoDataReason == "" || len(r.MissingMetrics) == 0) {
				t.Errorf("%s uses metrics absent from metric_names: should be kept and flagged no_data_yet", r.ID)
			}
			if r.NoDataYet {
				flagged++
			}
		}
		if flagged == 0 {
			t.Error("expected some recipes flagged no_data_yet")
		}
	})

	t.Run("errors before the first error: kept with config, dropped only with only_with_data", func(t *testing.T) {
		a := d.Analyze(loadConfig(t, "testdata/krakend-full.json"))
		// a gateway that has served traffic but never failed: no 5xx, no timeouts, no breaker trips yet
		names := []string{"http_server_duration_bucket", "http_server_duration_count", "http_server_duration_sum",
			"krakend_backend_duration_bucket", "krakend_backend_duration_count", "krakend_backend_duration_sum",
			"http_client_duration_bucket", "http_client_duration_count", "http_client_duration_sum"}
		resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"errors", "resilience"}, MetricNames: names, Analysis: a, Format: "grafana_panel"})
		if err != nil {
			t.Fatal(err)
		}
		ids := map[string]RenderedRecipe{}
		for _, r := range resp.Recipes {
			ids[r.ID] = r
		}
		for _, id := range []string{"errors.backend_timeouts", "cb.opened_in_range", "cb.transitions"} {
			r, ok := ids[id]
			if !ok {
				t.Errorf("%s must be kept: the config enables it (unavailable: %+v)", id, resp.Unavailable)
				continue
			}
			if !r.NoDataYet || !strings.Contains(resp.NoDataReasons[r.NoDataReason], "config enables") {
				t.Errorf("%s: want no_data_yet with a config-based reason, got %+v", id, r)
			}
			fd := r.GrafanaPanel["fieldConfig"].(map[string]any)["defaults"].(map[string]any)
			if fd["noValue"] != nil || strings.Contains(r.GrafanaPanel["description"].(string), "No data") {
				t.Errorf("%s: panels must not carry data/test status (plain Grafana 'No data')", id)
			}
		}

		strict, _ := d.RenderRecipes(RecipeRequest{Intents: []string{"errors", "resilience"}, MetricNames: names, Analysis: a, OnlyWithData: true})
		for _, r := range strict.Recipes {
			if r.NoDataYet {
				t.Errorf("only_with_data must not return no_data_yet recipes: %s", r.ID)
			}
		}
		if len(strict.Unavailable) == 0 {
			t.Error("only_with_data should drop recipes without live metrics")
		}
	})

	t.Run("grafana panel format", func(t *testing.T) {
		resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"errors.ratio_5xx"}, Format: "grafana_panel"})
		if err != nil {
			t.Fatal(err)
		}
		p := resp.Recipes[0].GrafanaPanel
		if p["type"] != "stat" || !strings.Contains(p["description"].(string), "errors.ratio_5xx") {
			t.Errorf("unexpected panel: %+v", p)
		}
	})

	t.Run("invalid input", func(t *testing.T) {
		if _, err := d.RenderRecipes(RecipeRequest{SemanticConvention: "1.30"}); err == nil {
			t.Error("unknown convention should fail")
		}
		if _, err := d.RenderRecipes(RecipeRequest{ScrapeLabels: []string{`job"}`}}); err == nil {
			t.Error("invalid label name should fail")
		}
		if _, err := d.RenderRecipes(RecipeRequest{Format: "svg"}); err == nil {
			t.Error("unknown format should fail")
		}
	})
}

func TestQueryCatalog(t *testing.T) {
	d := loadTestData(t)

	t.Run("everything", func(t *testing.T) {
		resp, err := d.QueryCatalog(CatalogRequest{})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.Metrics) != len(d.Catalog.Metrics) || resp.Runtime == nil || len(resp.Gotchas) == 0 {
			t.Errorf("metrics=%d runtime=%v gotchas=%d", len(resp.Metrics), resp.Runtime != nil, len(resp.Gotchas))
		}
	})

	t.Run("by group with label meanings", func(t *testing.T) {
		resp, err := d.QueryCatalog(CatalogRequest{Groups: []string{"ai"}})
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range resp.Metrics {
			if m.Group != "ai" {
				t.Errorf("unexpected group %s for %s", m.Group, m.Name)
			}
		}
		if len(resp.Metrics) != 4 || resp.LabelMeanings["model"] == "" || resp.Runtime != nil {
			t.Errorf("ai: metrics=%d model meaning=%q runtime=%v", len(resp.Metrics), resp.LabelMeanings["model"], resp.Runtime != nil)
		}
	})

	t.Run("search", func(t *testing.T) {
		resp, _ := d.QueryCatalog(CatalogRequest{Query: "timeout"})
		var names []string
		for _, m := range resp.Metrics {
			names = append(names, m.Name)
		}
		if !slices.Contains(names, "http_client_request_timedout_count_total") {
			t.Errorf("query timeout -> %v", names)
		}
	})

	t.Run("names in both conventions and not found", func(t *testing.T) {
		resp, err := d.QueryCatalog(CatalogRequest{Names: []string{
			"http_server_request_duration_seconds_bucket", "krakend_proxy_duration_count", "go_goroutines", "nope_total"}})
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, m := range resp.Metrics {
			names = append(names, m.Name)
		}
		if !slices.Contains(names, "http_server_duration") || !slices.Contains(names, "krakend_proxy_duration") {
			t.Errorf("names lookup -> %v", names)
		}
		if resp.Runtime == nil {
			t.Error("go_goroutines should include runtime info")
		}
		if !slices.Equal(resp.NotFound, []string{"nope_total"}) {
			t.Errorf("not_found = %v", resp.NotFound)
		}
	})

	t.Run("semconv 1.27 naming", func(t *testing.T) {
		resp, err := d.QueryCatalog(CatalogRequest{Groups: []string{"global"}, SemanticConvention: "auto",
			MetricNames: []string{"http_server_request_duration_seconds_bucket"}})
		if err != nil {
			t.Fatal(err)
		}
		if resp.SemanticConvention != Convention127 {
			t.Fatalf("convention = %q", resp.SemanticConvention)
		}
		for _, m := range resp.Metrics {
			if m.DefaultName == "" || m.Name == m.DefaultName {
				t.Errorf("%s not converted (default %q)", m.Name, m.DefaultName)
			}
			if m.Semconv127 != nil {
				t.Errorf("%s: converted entries should not repeat semconv_1_27", m.Name)
			}
		}
		def, _ := d.QueryCatalog(CatalogRequest{Groups: []string{"global"}})
		if def.Metrics[0].Semconv127 == nil {
			t.Error("default naming should show the 1.27 alternative")
		}
	})

	t.Run("invalid", func(t *testing.T) {
		if _, err := d.QueryCatalog(CatalogRequest{Groups: []string{"bogus"}}); err == nil || !strings.Contains(err.Error(), "valid groups") {
			t.Errorf("unknown group should list valid groups, got %v", err)
		}
		if _, err := d.QueryCatalog(CatalogRequest{SemanticConvention: "2"}); err == nil {
			t.Error("unknown convention should fail")
		}
	})
}

// Every field of catalog.json metrics must be loaded: a field missing from Metric is silently
// dropped and never reaches the tools.
func TestCatalogFieldsRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("../../tools/data/observability/catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var generic struct {
		Metrics []map[string]any `json:"metrics"`
	}
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	d := loadTestData(t)
	for i, m := range d.Catalog.Metrics {
		back, _ := json.Marshal(m)
		var got map[string]any
		_ = json.Unmarshal(back, &got)
		for k, v := range generic.Metrics[i] {
			if _, ok := got[k]; !ok && v != nil && v != false && v != "" {
				t.Errorf("%s: field %q in catalog.json is not loaded into Metric", m.Name, k)
			}
		}
	}
}

// Runtime families carry their Prometheus type (gauges must not go through rate()), and event-only
// instruments confirmed in another scrape use the real name instead of a prediction.
func TestRuntimeDetailsAndVerifiedNames(t *testing.T) {
	d := loadTestData(t)
	rt := d.Catalog.Runtime
	if rt.Meaning == "" {
		t.Error("runtime meaning not loaded")
	}
	for fam, want := range map[string]string{
		"go_memstats_heap_objects":      "gauge",
		"go_memstats_alloc_bytes_total": "counter",
		"go_gc_duration_seconds":        "summary",
	} {
		if got := rt.Details[fam].Type; got != want {
			t.Errorf("runtime %s: type %q, want %q", fam, got, want)
		}
	}
	if !rt.Details["process_network_receive_bytes_total"].LinuxOnly {
		t.Error("process_network_receive_bytes_total should be marked linux_only")
	}
	if len(rt.Families) != len(rt.Details) {
		t.Errorf("families (%d) and details (%d) differ", len(rt.Families), len(rt.Details))
	}
	for _, m := range d.Catalog.Metrics {
		if m.OtelName != "messaging.write.failure.count" && m.OtelName != "messaging.read.failure.count" {
			continue
		}
		if !strings.HasSuffix(m.Name, "_failure_count_seconds") || m.Type != "histogram" || m.PredictedName || m.VerifiedName == "" {
			t.Errorf("%s: got name=%s type=%s predicted=%v verified=%q", m.OtelName, m.Name, m.Type, m.PredictedName, m.VerifiedName)
		}
	}
}

// Async agents: proxy series named after the agent, backend series without krakend_endpoint_route
// (verified on krakend-ee eefea5cf with an AMQP agent posting to Elasticsearch).
func TestAsyncAgents(t *testing.T) {
	d := loadTestData(t)
	cfg := map[string]any{
		"extra_config": map[string]any{"telemetry/opentelemetry": map[string]any{
			"exporters": map[string]any{"prometheus": []any{map[string]any{"name": "p"}}}}},
		"endpoints": []any{map[string]any{"endpoint": "/a", "backend": []any{map[string]any{
			"url_pattern": "/x", "host": []any{"http://svc:8080"}}}}},
		"async_agent": []any{
			map[string]any{"name": "events-to-elasticsearch", "consumer": map[string]any{"topic": "events"},
				"extra_config": map[string]any{"async/amqp": map[string]any{"name": "q"}},
				"backend":      []any{map[string]any{"url_pattern": "/events/_doc", "method": "POST", "host": []any{"http://localhost:9201"}}}},
			map[string]any{"name": "orders", "consumer": map[string]any{"topic": "orders"},
				"extra_config": map[string]any{"async/kafka": map[string]any{}},
				"backend":      []any{map[string]any{"url_pattern": "/orders", "host": []any{"http://svc:8080"}}}},
		},
	}
	a := d.Analyze(cfg)
	if len(a.AsyncAgents) != 2 {
		t.Fatalf("want 2 agents, got %+v", a.AsyncAgents)
	}
	es := a.AsyncAgents[0]
	if es.LabelValue != "events-to-elasticsearch" || es.Driver != "async/amqp" || len(es.Backends) != 1 ||
		es.Backends[0].LabelValue != "/events/_doc" || es.Backends[0].ServerPort != "9201" {
		t.Errorf("unexpected agent: %+v", es)
	}
	if got := a.LabelValues["http_route@agent"]; !slices.Equal(got, []string{"events-to-elasticsearch", "orders"}) {
		t.Errorf("http_route@agent = %v", got)
	}
	if slices.Contains(a.LabelValues["krakend_endpoint_route"], "") {
		t.Error("agent backends must not add an empty krakend_endpoint_route")
	}
	if !slices.Contains(a.LabelValues["messaging_topic"], "orders") || slices.Contains(a.LabelValues["messaging_topic"], "events") {
		t.Errorf("only kafka agents emit messaging_* topics: %v", a.LabelValues["messaging_topic"])
	}
	if !slices.ContainsFunc(a.Warnings, func(w string) bool { return strings.HasPrefix(w, "Async agents") }) {
		t.Error("missing async agent warning")
	}

	resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"agents"}, Analysis: a})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Recipes) == 0 {
		t.Error("agents intent returned no recipes")
	}
	delete(cfg, "async_agent")
	resp, err = d.RenderRecipes(RecipeRequest{Intents: []string{"agents"}, Analysis: d.Analyze(cfg)})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Recipes) != 0 || len(resp.Unavailable) == 0 {
		t.Errorf("without agents, agent recipes should be unavailable: %d recipes, %d unavailable", len(resp.Recipes), len(resp.Unavailable))
	}
}

// Targets of one recipe must be distinguishable in the legend.
func TestRecipeLegendsAreUnique(t *testing.T) {
	d := loadTestData(t)
	for _, r := range d.Recipes.Recipes {
		seen := map[string]bool{}
		for _, tg := range r.Targets {
			if seen[tg.Legend] {
				t.Errorf("%s: legend %q used by several targets", r.ID, tg.Legend)
			}
			seen[tg.Legend] = true
		}
	}
}

// A macOS scrape has no process_network_* series: the recipe must say it depends on the OS KrakenD
// runs on (keep it / ask), not that "nothing has happened yet".
func TestLinuxOnlyNoDataReason(t *testing.T) {
	d := loadTestData(t)
	live := []string{"go_goroutines", "process_cpu_seconds_total", "process_resident_memory_bytes"}
	resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"runtime"}, MetricNames: live})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range resp.Recipes {
		if r.ID == "runtime.network_io" {
			if why := resp.NoDataReasons[r.NoDataReason]; !r.NoDataYet || !strings.Contains(why, "Linux-only") || !strings.Contains(why, "ask the user") {
				t.Errorf("network_io: %+v", r)
			}
			return
		}
	}
	t.Error("runtime.network_io missing from runtime recipes")
}

// Panels from grafana_panel carry what dashboard-conventions.md asks for: thresholds from the
// recipe, status-class colors, a specific no-value text, and no duplicated fields.
func TestGrafanaPanelConventions(t *testing.T) {
	d := loadTestData(t)
	resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"errors", "runtime", "resilience", "traffic"}, Format: "grafana_panel", NoSizeLimit: true})
	if err != nil {
		t.Fatal(err)
	}
	panels := map[string]map[string]any{}
	for _, r := range resp.Recipes {
		if r.Title != "" || len(r.Targets) != 0 {
			t.Errorf("%s: grafana_panel output must not repeat title/targets outside the panel", r.ID)
		}
		panels[r.ID] = r.GrafanaPanel
	}
	defaults := func(id string) map[string]any {
		return panels[id]["fieldConfig"].(map[string]any)["defaults"].(map[string]any)
	}
	if th, ok := defaults("errors.ratio_5xx")["thresholds"].(map[string]any); !ok || len(th["steps"].([]ThresholdStep)) != 3 {
		t.Errorf("errors.ratio_5xx: want 3 threshold steps, got %v", defaults("errors.ratio_5xx")["thresholds"])
	}
	if _, ok := panels["errors.ratio_5xx"]["options"].(map[string]any)["reduceOptions"]; !ok {
		t.Error("stat panels need options.reduceOptions (lastNotNull)")
	}
	if cu := defaults("traffic.rps_by_status")["custom"].(map[string]any); cu["stacking"] == nil || cu["axisSoftMin"] != 0 {
		t.Errorf("stacked recipe must be stacked from 0: %v", cu)
	}
	if strings.Contains(panels["traffic.rps_by_status"]["description"].(string), "stacked") ||
		strings.Contains(panels["cb.window_counts"]["description"].(string), "rate()") {
		t.Errorf("panel descriptions must not carry panel settings or agent notes")
	}
	if c := defaults("traffic.requests_in_range")["color"].(map[string]any); c["mode"] != "fixed" {
		t.Errorf("stat without thresholds must use a neutral fixed color, got %v", c)
	}
	if defaults("traffic.response_size_heatmap")["unit"] != "short" ||
		panels["traffic.response_size_heatmap"]["options"].(map[string]any)["yAxis"].(map[string]any)["unit"] != "bytes" {
		t.Errorf("heatmap: bucket unit goes on the y axis, cells are counts")
	}
	if o := panels["traffic.top_endpoints"]["options"].(map[string]any); o["orientation"] != "horizontal" || o["namePlacement"] != "left" {
		t.Errorf("bargauge must show names beside horizontal bars: %v", o)
	}
	if v, ok := defaults("runtime.memory_limit_usage")["noValue"]; ok {
		t.Errorf("memory_limit_usage must not set noValue, got %v", v)
	}
	for _, id := range []string{"traffic.rps_by_status", "errors.backend_status_codes"} {
		if ov := panels[id]["fieldConfig"].(map[string]any)["overrides"].([]any); len(ov) < 4 ||
			!strings.Contains(fmt.Sprint(ov), "429") || !strings.Contains(fmt.Sprint(ov), "404") {
			t.Errorf("%s: want class colors plus distinct 404/429 shades, got %v", id, ov)
		}
	}
	if ov := panels["traffic.rps_by_endpoint"]["fieldConfig"].(map[string]any)["overrides"].([]any); len(ov) != 0 {
		t.Errorf("traffic.rps_by_endpoint must not get status overrides")
	}
	tbl := panels["errors.non2xx_by_endpoint_in_range"]
	if tr := fmt.Sprint(tbl["transformations"]); !strings.Contains(tr, "Time:true") || !strings.Contains(tr, "http_route:Endpoint") ||
		!strings.Contains(tr, "http_response_status_code:Status") || !strings.Contains(fmt.Sprint(tbl["options"]), "displayName:Non-2xx responses") ||
		!strings.Contains(tr, "http_route:1") || !strings.Contains(tr, "http_response_status_code:9") ||
		tbl["fieldConfig"].(map[string]any)["defaults"].(map[string]any)["decimals"] != 0 ||
		tbl["fieldConfig"].(map[string]any)["defaults"].(map[string]any)["unit"] != "locale" {
		t.Errorf("table panels must hide Time, name label columns and sort by value: %v %v", tbl["transformations"], tbl["options"])
	}
	if _, err := d.RenderRecipes(RecipeRequest{Intents: []string{"all"}, Format: "grafana_panel"}); err == nil {
		t.Error("all + grafana_panel must be refused (too large for a tool result)")
	}
}

// A config without telemetry must make the tools say "stop", not hand out recipes to build an empty dashboard.
func TestNoTelemetryStops(t *testing.T) {
	d := loadTestData(t)
	a := d.Analyze(map[string]any{"version": 3.0, "endpoints": []any{}})
	if a.TelemetryEnabled || len(a.Warnings) == 0 || a.Warnings[0] != NoTelemetryWarning {
		t.Fatalf("want the STOP warning first, got %v", a.Warnings)
	}
	for g, st := range a.Groups {
		if g != "scrape" && !strings.Contains(st.Reason, "no telemetry/opentelemetry") {
			t.Errorf("group %s: with telemetry off the reason must say so, got %q", g, st.Reason)
		}
	}
	if len(a.Gaps) == 0 || a.Gaps[0].Option != "extra_config.telemetry/opentelemetry" {
		t.Fatalf("want the telemetry gap with its snippet, got %+v", a.Gaps)
	}
	resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"overview"}, Analysis: a})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range resp.Warnings {
		found = found || w == NoTelemetryWarning
	}
	if !found {
		t.Errorf("get_dashboard_recipes must repeat the STOP warning, got %v", resp.Warnings)
	}
}

// grafana_panel responses must fit in a tool result; too-large requests get per-intent sizes back.
func TestPanelResponseSizeLimit(t *testing.T) {
	d := loadTestData(t)
	a := d.Analyze(loadConfig(t, "testdata/krakend-full.json"))
	big := RecipeRequest{Intents: []string{"overview", "backends", "resilience", "runtime", "latency", "errors"}, Format: "grafana_panel", Analysis: a}
	_, err := d.RenderRecipes(big)
	if err == nil || !strings.Contains(err.Error(), "runtime ~") {
		t.Fatalf("want a size error listing per-intent sizes, got %v", err)
	}
	for _, in := range []string{"runtime", "backends", "resilience"} {
		if _, err := d.RenderRecipes(RecipeRequest{Intents: []string{in}, Format: "grafana_panel", Analysis: a}); err != nil {
			t.Errorf("single intent %s must fit: %v", in, err)
		}
	}
}

// Endpoint panels must not pick up async agent series when $endpoint is All.
func TestEndpointRecipesExcludeAgents(t *testing.T) {
	d := loadTestData(t)
	resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"backends", "errors", "latency"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range resp.Recipes {
		for _, tg := range r.Targets {
			if strings.Contains(tg.Expr, `krakend_endpoint_route=~"$endpoint"`) && !strings.Contains(tg.Expr, `krakend_endpoint_route!=""`) {
				t.Errorf("%s: backend selector must exclude agents (krakend_endpoint_route!=\"\"): %s", r.ID, tg.Expr)
			}
			if strings.Contains(tg.Expr, "krakend_proxy_duration") && strings.Contains(tg.Expr, `http_route=~"$endpoint"`) &&
				!strings.Contains(tg.Expr, `http_request_method!=""`) {
				t.Errorf("%s: proxy series with $endpoint must exclude agents (http_request_method!=\"\"): %s", r.ID, tg.Expr)
			}
		}
	}
}

// Topic intents return exactly the blueprint from dashboard-conventions.md.
func TestOverviewIntentIsTheBlueprint(t *testing.T) {
	d := loadTestData(t)
	resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"overview"}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range resp.Recipes {
		got[r.ID] = true
	}
	for _, id := range []string{"runtime.nodes_up", "backends.rps_by_backend", "runtime.cpu", "runtime.rss", "runtime.goroutines", "latency.heatmap"} {
		if !got[id] {
			t.Errorf("overview must include %s (overview blueprint)", id)
		}
	}
	if got["traffic.response_size_heatmap"] {
		t.Error("overview must not include recipes outside its blueprint")
	}
}

func TestSharedCircuitBreakerNameWarns(t *testing.T) {
	d := loadTestData(t)
	cb := map[string]any{"qos/circuit-breaker": map[string]any{"name": "shared", "interval": 60.0, "timeout": 10.0, "max_errors": 1.0}}
	cfg := map[string]any{"version": 3.0, "extra_config": map[string]any{"telemetry/opentelemetry": map[string]any{
		"exporters": map[string]any{"prometheus": []any{map[string]any{"name": "p", "port": 9090.0}}}}},
		"endpoints": []any{
			map[string]any{"endpoint": "/a", "backend": []any{map[string]any{"url_pattern": "/x", "host": []any{"http://h"}, "extra_config": cb}}},
			map[string]any{"endpoint": "/b", "backend": []any{map[string]any{"url_pattern": "/y", "host": []any{"http://h"}, "extra_config": cb}}},
		}}
	a := d.Analyze(cfg)
	if !strings.Contains(strings.Join(a.Warnings, "\n"), `"shared" is used by 2 backends`) {
		t.Errorf("want a shared breaker name warning, got %v", a.Warnings)
	}
}

// Optional overview rows (Kafka, agents, resilience...) come in only when the config enables them.
func TestOverviewOptionalRows(t *testing.T) {
	d := loadTestData(t)
	full := d.Analyze(loadConfig(t, "testdata/krakend-full.json"))
	resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"overview"}, Analysis: full})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, r := range resp.Recipes {
		got[r.ID] = true
	}
	if !got["cb.opened_in_range"] || !got["kafka.published_rate"] {
		t.Errorf("full config enables breakers and Kafka: their overview rows must be included")
	}
	for _, u := range resp.Unavailable {
		for _, id := range []string{"cb.", "kafka.", "agents.", "ai.", "ws.", "redis.", "ratelimit."} {
			if strings.HasPrefix(u.ID, id) {
				t.Errorf("optional overview recipe %s must be skipped silently, not listed as unavailable", u.ID)
			}
		}
	}
	plain, _ := d.RenderRecipes(RecipeRequest{Intents: []string{"overview"}})
	for _, r := range plain.Recipes {
		if strings.HasPrefix(r.ID, "kafka.") {
			t.Errorf("without a config, optional rows are left out: %s", r.ID)
		}
	}
}

// With prompt guards, 5xx panels say that blocked prompts are part of them.
func TestPromptGuard5xxNote(t *testing.T) {
	d := loadTestData(t)
	a := d.Analyze(loadConfig(t, "testdata/krakend-full.json"))
	if len(a.PromptGuardRoutes) == 0 {
		t.Skip("test config has no prompt guard")
	}
	resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"errors.ratio_5xx"}, Analysis: a, Format: "grafana_panel"})
	if err != nil || len(resp.Recipes) != 1 {
		t.Fatalf("%v %+v", err, resp)
	}
	if desc := resp.Recipes[0].GrafanaPanel["description"].(string); !strings.Contains(desc, "prompt-guard blocks on") {
		t.Errorf("5xx description must mention prompt-guard blocks: %q", desc)
	}
}

// Topic intents return their blueprint rows, with disabled optional rows dropped.
func TestBlueprintRows(t *testing.T) {
	d := loadTestData(t)
	resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"overview"}})
	if err != nil {
		t.Fatal(err)
	}
	bp, ok := resp.Blueprints["overview"]
	if !ok || len(bp.Rows) == 0 || bp.Rows[0].Title != "Stats" {
		t.Fatalf("want the overview blueprint starting with Stats, got %+v", resp.Blueprints)
	}
	for _, r := range bp.Rows {
		if r.Optional {
			t.Errorf("without a config, optional row %q must be dropped", r.Title)
		}
	}
	full := d.Analyze(loadConfig(t, "testdata/krakend-full.json"))
	resp, _ = d.RenderRecipes(RecipeRequest{Intents: []string{"overview"}, Analysis: full})
	titles := ""
	for _, r := range resp.Blueprints["overview"].Rows {
		titles += r.Title + ","
	}
	if !strings.Contains(titles, "Resilience") || !strings.Contains(titles, "Kafka") {
		t.Errorf("full config enables breakers and Kafka: want those optional rows, got %s", titles)
	}
}

// Quota recipes are restricted to the endpoints with governance/quota.
func TestQuotaRecipes(t *testing.T) {
	d := loadTestData(t)
	cfg := map[string]any{"version": 3.0, "extra_config": map[string]any{"telemetry/opentelemetry": map[string]any{
		"exporters": map[string]any{"prometheus": []any{map[string]any{"name": "p", "port": 9090.0}}}}},
		"endpoints": []any{
			map[string]any{"endpoint": "/orders/{id}", "extra_config": map[string]any{"governance/quota": map[string]any{"quota_name": "public_api"}},
				"backend": []any{map[string]any{"url_pattern": "/x", "host": []any{"http://h"}}}},
			map[string]any{"endpoint": "/free", "backend": []any{map[string]any{"url_pattern": "/y", "host": []any{"http://h"}}}},
		}}
	resp, err := d.RenderRecipes(RecipeRequest{Intents: []string{"quota"}, Analysis: d.Analyze(cfg)})
	if err != nil || len(resp.Recipes) == 0 {
		t.Fatalf("%v %+v", err, resp.Unavailable)
	}
	for _, r := range resp.Recipes {
		for _, tg := range r.Targets {
			if !strings.Contains(tg.Expr, `http_route=~"/orders/:id"`) || strings.Contains(tg.Expr, "$endpoint") {
				t.Errorf("%s: want the quota endpoint only: %s", r.ID, tg.Expr)
			}
		}
	}
	none := d.Analyze(map[string]any{"version": 3.0, "extra_config": cfg["extra_config"], "endpoints": []any{}})
	resp, _ = d.RenderRecipes(RecipeRequest{Intents: []string{"quota"}, Analysis: none})
	if len(resp.Recipes) != 0 || len(resp.Unavailable) == 0 {
		t.Errorf("without quotas the recipes are unavailable: %+v", resp.Recipes)
	}
}
