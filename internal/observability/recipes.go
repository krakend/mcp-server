package observability

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// RecipeRequest selects and renders recipes.
type RecipeRequest struct {
	Intents            []string  // e.g. overview, latency, errors, backends, ai; "all" for everything
	SemanticConvention string    // "", "1.27" or "auto"
	MetricNames        []string  // live metric names, used for "auto" and availability checks
	ScrapeLabels       []string  // dashboard variables to filter by (default job, instance)
	Endpoint           string    // pin selectors to one endpoint (config route or label value)
	Analysis           *Analysis // optional: filters by enabled groups and fills config parameters
	Format             string    // "promql" (default) or "grafana_panel"
	// OnlyWithData drops recipes whose metrics aren't in MetricNames yet. Off by default: the
	// config decides what a dashboard should have, live data only says what happened so far.
	OnlyWithData bool
	// NoSizeLimit: the panels go to a file, not into the tool result.
	NoSizeLimit bool
}

// RecipeResponse is the rendered result.
type RecipeResponse struct {
	KrakenDVersion     string              `json:"krakend_version"`
	SemanticConvention string              `json:"semantic_convention"`
	ConventionSource   string              `json:"convention_source"`
	Selectors          map[string]string   `json:"selectors"`
	Variables          []string            `json:"variables"`
	Recipes            []RenderedRecipe    `json:"recipes"`
	Unavailable        []UnavailableRecipe `json:"unavailable"`
	SectionNotes       map[string]string   `json:"section_notes,omitempty"`
	// NoDataReasons explains each no_data_reason code used by the recipes (sent once, not per recipe).
	NoDataReasons map[string]string `json:"no_data_reasons,omitempty"`
	// Blueprints: for topic intents (overview, runtime, backends, agents), the row layout with the
	// recipes this response actually contains. Use it as the dashboard layout.
	Blueprints map[string]Blueprint `json:"blueprints,omitempty"`
	// PanelsFile is where the grafana_panel JSON was written (panels_file), instead of inline.
	PanelsFile string   `json:"panels_file,omitempty"`
	Warnings   []string `json:"warnings"`
}

// RenderedRecipe is a recipe ready to put in a dashboard.
type RenderedRecipe struct {
	ID        string   `json:"id"`
	Section   string   `json:"section"`
	Title     string   `json:"title,omitempty"`
	Panel     string   `json:"panel,omitempty"`
	AltPanels []string `json:"alt_panels,omitempty"`
	Unit      string   `json:"unit,omitempty"`
	// Targets, Title, Panel, Unit and Notes are left out with format grafana_panel: the panel has them.
	Targets []Target `json:"targets,omitempty"`
	Notes   string   `json:"notes,omitempty"`
	// AgentNotes are for you, not for the panel (kept with format grafana_panel).
	AgentNotes       string `json:"agent_notes,omitempty"`
	MayBeEmpty       bool   `json:"may_be_empty,omitempty"`
	MayBeEmptyReason string `json:"may_be_empty_reason,omitempty"`
	// NoDataYet: some metrics of this recipe are not in Prometheus yet (metric_names). The panel is
	// still valid and fills in when those events happen (first error, first Kafka message...).
	NoDataYet      bool     `json:"no_data_yet,omitempty"`
	MissingMetrics []string `json:"missing_metrics,omitempty"`
	// NoDataReason is a key of RecipeResponse.NoDataReasons.
	NoDataReason string         `json:"no_data_reason,omitempty"`
	GrafanaPanel map[string]any `json:"grafana_panel,omitempty"`

	thresholds []ThresholdStep
	stacked    bool
	bars       bool
}

const (
	noDataLinuxOnly = "linux_only"
	noDataNotYet    = "not_happened_yet"
	noDataUnknown   = "not_in_prometheus"
)

var noDataExplanations = map[string]string{
	noDataLinuxOnly: "Linux-only metrics: KrakenD running on macOS/Windows (typical dev machines) never exports them; " +
		"on Linux (containers, most production servers) they are always there. Don't decide from the current machine: " +
		"the dashboard is for where KrakenD will run. Keep the panel by default; if you don't know where it will run, " +
		"ask the user whether production is Linux.",
	noDataNotYet: "Your config enables these metrics; Prometheus has no series yet because nothing of this kind has happened " +
		"(no traffic, errors, timeouts, breaker trips, messages... so far). Keep the panel: it fills in when it happens.",
	noDataUnknown: "Not in Prometheus yet: either nothing of this kind has happened so far, or the feature/layer isn't " +
		"enabled. Pass config to tell which.",
}

// UnavailableRecipe is a recipe filtered out, with the reason and how to enable it.
type UnavailableRecipe struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	Fix    string `json:"fix,omitempty"`
}

const defaultIntent = "overview"

var (
	scrapeSelectorText = `job=~"$job", instance=~"$instance"`
	endpointMatcher    = `http_route=~"$endpoint"`
	bEndpointMatcher   = `krakend_endpoint_route=~"$endpoint"`
	labelName          = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

// RenderRecipes selects recipes for the request and renders them.
// MaxPanelResponseBytes caps a grafana_panel response. Larger results don't fit in an agent's tool
// result (Claude Code's default is about 25k tokens) and end up truncated or in a side file.
const MaxPanelResponseBytes = 45_000

// RenderRecipes selects and renders recipes. With format grafana_panel it refuses responses over
// MaxPanelResponseBytes and says how big each requested intent is, so the caller can split the call.
func (d *Data) RenderRecipes(req RecipeRequest) (*RecipeResponse, error) {
	resp, err := d.renderRecipes(req)
	if err != nil || req.Format != "grafana_panel" || len(req.Intents) == 0 || req.NoSizeLimit {
		return resp, err
	}
	b, _ := json.Marshal(resp)
	if len(b) <= MaxPanelResponseBytes {
		return resp, nil
	}
	var sizes []string
	for _, in := range req.Intents {
		one := req
		one.Intents = []string{in}
		r, err := d.renderRecipes(one)
		if err != nil {
			continue
		}
		ob, _ := json.Marshal(r)
		sizes = append(sizes, fmt.Sprintf("%s ~%d KB (%d recipes)", in, (len(ob)+999)/1000, len(r.Recipes)))
	}
	return nil, fmt.Errorf("format grafana_panel with these intents returns ~%d KB, more than fits in a tool result "+
		"(limit %d KB). Pass panels_file to get them in a file instead (no size limit), or make several calls, "+
		"each with intents adding up to less than that (intents overlap, so a combination can be smaller than the sum): %s",
		(len(b)+999)/1000, MaxPanelResponseBytes/1000, strings.Join(sizes, ", "))
}

func (d *Data) renderRecipes(req RecipeRequest) (*RecipeResponse, error) {
	resp := &RecipeResponse{
		KrakenDVersion: d.Catalog.KrakenDVersion,
		Recipes:        []RenderedRecipe{},
		Unavailable:    []UnavailableRecipe{},
		Warnings:       []string{},
		SectionNotes:   map[string]string{},
		NoDataReasons:  map[string]string{},
	}

	// --- naming convention
	switch req.SemanticConvention {
	case ConventionDefault, Convention127:
		resp.SemanticConvention, resp.ConventionSource = req.SemanticConvention, "request"
	case "auto", "detect":
		conv, status := d.DetectConvention(req.MetricNames)
		switch status {
		case DetectionFound:
			resp.SemanticConvention, resp.ConventionSource = conv, "detected from metric_names"
		case DetectionMixed:
			resp.SemanticConvention, resp.ConventionSource = ConventionDefault, "mixed"
			resp.Warnings = append(resp.Warnings, "metric_names contain both naming conventions (several KrakenD versions or settings scraped together). Using default names; build one dashboard per convention or filter by job.")
		default:
			if req.Analysis != nil {
				resp.SemanticConvention, resp.ConventionSource = req.Analysis.SemanticConvention, "config"
			} else {
				resp.SemanticConvention, resp.ConventionSource = ConventionDefault, "default (nothing to detect from)"
			}
		}
	default:
		return nil, fmt.Errorf("semantic_convention must be \"\", \"1.27\" or \"auto\", got %q", req.SemanticConvention)
	}

	// --- selectors
	scrape := req.ScrapeLabels
	if len(scrape) == 0 {
		scrape = []string{"job", "instance"}
	}
	var parts []string
	for _, l := range scrape {
		if !labelName.MatchString(l) {
			return nil, fmt.Errorf("invalid scrape label %q", l)
		}
		parts = append(parts, fmt.Sprintf(`%s=~"$%s"`, l, l))
	}
	scrapeSel := strings.Join(parts, ", ")
	// With $endpoint = All (.*) the backend matcher would also match async agent series, whose
	// krakend_endpoint_route is empty: exclude them explicitly.
	epMatch, bepMatch := endpointMatcher, bEndpointMatcher+`, krakend_endpoint_route!=""`
	if req.Endpoint != "" {
		lv := RouteLabel(req.Endpoint)
		epMatch = fmt.Sprintf(`http_route="%s"`, escapeLabelValue(lv))
		bepMatch = fmt.Sprintf(`krakend_endpoint_route="%s"`, escapeLabelValue(lv))
	}
	resp.Selectors = map[string]string{
		"SEL":  joinSel(scrapeSel, epMatch),
		"BSEL": joinSel(scrapeSel, bepMatch),
		"J":    scrapeSel,
	}

	// --- live metric availability
	live := map[string]bool{}
	for _, n := range req.MetricNames {
		live[n] = true
	}

	intents := map[string]bool{}
	for _, i := range req.Intents {
		intents[strings.ToLower(strings.TrimSpace(i))] = true
	}
	if len(intents) == 0 {
		intents[defaultIntent] = true
	}
	if req.Analysis != nil && !req.Analysis.TelemetryEnabled {
		resp.Warnings = append(resp.Warnings, NoTelemetryWarning)
	}
	if intents["all"] && req.Format == "grafana_panel" && !req.NoSizeLimit {
		return nil, fmt.Errorf("intents \"all\" with format grafana_panel returns ~130 KB, more than a tool result can hold: " +
			"ask for the topics the dashboard needs (e.g. [\"overview\"], [\"runtime\"], [\"backends\", \"resilience\"]), " +
			"one call per topic if needed, or use format promql")
	}

	sections := map[string]bool{}
	for _, r := range d.Recipes.Recipes {
		optional := false
		if !intents["all"] && !matchesIntent(r, intents) {
			if !matchesOptional(r, intents) || req.Analysis == nil {
				continue
			}
			optional = true
		}
		if reason, fix := d.unavailableReason(r, req.Analysis); reason != "" {
			if !optional {
				resp.Unavailable = append(resp.Unavailable, UnavailableRecipe{ID: r.ID, Reason: reason, Fix: fix})
			}
			continue
		}
		rr := RenderedRecipe{
			ID: r.ID, Section: r.Section, Title: r.Title, Panel: r.Panel, AltPanels: r.AltPanels,
			Unit: r.Unit, Notes: r.Notes, MayBeEmpty: r.MayBeEmpty, MayBeEmptyReason: r.MayBeEmptyReason,
			thresholds: r.Thresholds, stacked: r.Stacked, bars: r.Bars, AgentNotes: r.AgentNotes,
		}
		skip := false
		for _, t := range r.Targets {
			expr := expandPlaceholders(t.Expr, scrapeSel, epMatch, bepMatch)
			var err error
			expr, err = applyParams(expr, r, req.Analysis, &rr)
			if err != nil {
				if !optional {
					resp.Unavailable = append(resp.Unavailable, UnavailableRecipe{ID: r.ID, Reason: err.Error()})
				}
				skip = true
				break
			}
			expr = d.ConvertExpr(expr, resp.SemanticConvention)
			rr.Targets = append(rr.Targets, Target{Expr: expr, Legend: t.Legend})
		}
		if skip {
			continue
		}
		if len(live) > 0 {
			if missing := d.missingLive(r, resp.SemanticConvention, live); len(missing) > 0 {
				if req.OnlyWithData {
					resp.Unavailable = append(resp.Unavailable, UnavailableRecipe{ID: r.ID,
						Reason: "only_with_data: not in Prometheus yet: " + strings.Join(missing, ", ")})
					continue
				}
				rr.NoDataYet, rr.MissingMetrics = true, missing
				switch {
				case d.allLinuxOnly(missing):
					rr.NoDataReason = noDataLinuxOnly
				case req.Analysis != nil:
					rr.NoDataReason = noDataNotYet
				default:
					rr.NoDataReason = noDataUnknown
				}
				resp.NoDataReasons[rr.NoDataReason] = noDataExplanations[rr.NoDataReason]
			}
		}
		if req.Analysis != nil && len(req.Analysis.PromptGuardRoutes) > 0 && countsServer5xx[r.ID] {
			add := "includes prompt-guard blocks on " + strings.Join(req.Analysis.PromptGuardRoutes, ", ") + ", which return 500"
			if n := strings.TrimSuffix(strings.TrimPrefix(rr.Notes, "("), ")"); n != "" {
				add = n + "; " + add
			}
			rr.Notes = add
		}
		if hasParam(r, "single_endpoint") && req.Endpoint == "" {
			rr.Notes = strings.TrimSpace(rr.Notes + " Only meaningful for ONE endpoint: pass endpoint, or make $endpoint single-select / repeat the panel per endpoint.")
		}
		if req.Format == "grafana_panel" {
			rr.GrafanaPanel = grafanaPanel(rr)
		}
		sections[r.Section] = true
		resp.Recipes = append(resp.Recipes, rr)
	}
	resp.Variables = dashboardVariables(resp.Recipes)
	resp.Blueprints = d.blueprintsFor(intents, resp.Recipes)
	if req.Format == "grafana_panel" {
		// The panel already carries title, type, unit, targets and notes (in its description):
		// don't send them twice.
		for i := range resp.Recipes {
			r := &resp.Recipes[i]
			r.Title, r.Panel, r.Unit, r.Notes, r.Targets, r.MayBeEmptyReason = "", "", "", "", nil, ""
		}
	}
	for s := range sections {
		if n := d.Recipes.SectionNotes[s]; n != "" {
			resp.SectionNotes[s] = n
		}
	}
	if req.Format != "" && req.Format != "promql" && req.Format != "grafana_panel" {
		return nil, fmt.Errorf("format must be \"promql\" or \"grafana_panel\", got %q", req.Format)
	}
	return resp, nil
}

var dashboardVar = regexp.MustCompile(`\$\{?([a-zA-Z_][a-zA-Z0-9_]*)`)

// dashboardVariables lists the Grafana variables the rendered queries use (built-ins excluded).
func dashboardVariables(recipes []RenderedRecipe) []string {
	seen := map[string]bool{}
	for _, r := range recipes {
		for _, t := range r.Targets {
			for _, m := range dashboardVar.FindAllStringSubmatch(t.Expr, -1) {
				if !strings.HasPrefix(m[1], "__") {
					seen[m[1]] = true
				}
			}
		}
	}
	return sortedKeysBool(seen)
}

// blueprintsFor returns the blueprints of the requested topic intents, keeping only the recipes
// that were rendered (disabled optional rows disappear).
func (d *Data) blueprintsFor(intents map[string]bool, rendered []RenderedRecipe) map[string]Blueprint {
	have := map[string]bool{}
	for _, r := range rendered {
		have[r.ID] = true
	}
	out := map[string]Blueprint{}
	for name, bp := range d.Recipes.Blueprints {
		if !intents[name] {
			continue
		}
		got := Blueprint{Notes: bp.Notes}
		for _, row := range bp.Rows {
			kept := BlueprintRow{Title: row.Title, Optional: row.Optional}
			for _, id := range row.Recipes {
				if have[id] {
					kept.Recipes = append(kept.Recipes, id)
				}
			}
			if len(kept.Recipes) > 0 {
				got.Rows = append(got.Rows, kept)
			}
		}
		out[name] = got
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// countsServer5xx: recipes whose 5xx include prompt-guard blocks (they return 500).
var countsServer5xx = map[string]bool{
	"errors.ratio_5xx": true, "errors.ratio_5xx_by_endpoint": true, "errors.status_split_in_range": true,
	"traffic.rps_by_status": true,
}

func matchesOptional(r Recipe, intents map[string]bool) bool {
	for _, i := range r.OptionalIntents {
		if intents[i] {
			return true
		}
	}
	return false
}

func matchesIntent(r Recipe, intents map[string]bool) bool {
	if intents[strings.ToLower(r.ID)] {
		return true
	}
	for _, i := range r.Intents {
		if intents[i] {
			return true
		}
	}
	// allow prefix intents such as "cb" or "redis"
	if dot := strings.IndexByte(r.ID, '.'); dot > 0 && intents[r.ID[:dot]] {
		return true
	}
	return false
}

func (d *Data) unavailableReason(r Recipe, a *Analysis) (reason, fix string) {
	if a == nil {
		return "", ""
	}
	for _, g := range r.Requires.Groups {
		st, ok := a.Groups[g]
		if ok && !st.Enabled {
			fix = st.Reason
			for _, gap := range a.Gaps {
				for _, id := range gap.Unlocks {
					if id == r.ID {
						fix = gap.Option
					}
				}
			}
			return fmt.Sprintf("metric group %q is not enabled by this configuration", g), fix
		}
	}
	if hasParam(r, "ratelimit_routes") && len(rateLimitedRoutes(a)) == 0 {
		return "no endpoint in this configuration has a rate limit", ""
	}
	if hasParam(r, "quota_routes") && len(quotaRoutes(a)) == 0 {
		return "no endpoint in this configuration has a quota", "governance/quota"
	}
	if r.Section == asyncAgentsSection && len(a.AsyncAgents) == 0 {
		return "no async_agent in this configuration", "async_agent"
	}
	return "", ""
}

// allLinuxOnly reports whether every missing metric is one the process collector only exports on Linux.
func (d *Data) allLinuxOnly(missing []string) bool {
	for _, m := range missing {
		if !d.Catalog.Runtime.Details[m].LinuxOnly {
			return false
		}
	}
	return len(missing) > 0
}

// asyncAgentsSection is the recipe section whose recipes only make sense with async agents.
const asyncAgentsSection = "Async agents (EE)"

func (d *Data) missingLive(r Recipe, convention string, live map[string]bool) []string {
	var missing []string
	for _, m := range r.Requires.Metrics {
		name := d.ConvertExpr(m, convention)
		if !live[name] {
			missing = append(missing, name)
		}
	}
	return missing
}

func expandPlaceholders(expr, scrapeSel, epMatch, bepMatch string) string {
	expr = strings.ReplaceAll(expr, "{SEL}", "{"+joinSel(scrapeSelectorText, endpointMatcher)+"}")
	expr = strings.ReplaceAll(expr, "{SEL,", "{"+joinSel(scrapeSelectorText, endpointMatcher)+",")
	expr = strings.ReplaceAll(expr, "{BSEL}", "{"+joinSel(scrapeSelectorText, bEndpointMatcher)+"}")
	expr = strings.ReplaceAll(expr, "{BSEL,", "{"+joinSel(scrapeSelectorText, bEndpointMatcher)+",")
	expr = strings.ReplaceAll(expr, "{J}", "{"+scrapeSelectorText+"}")
	expr = strings.ReplaceAll(expr, "{J,", "{"+scrapeSelectorText+",")
	// canonical selectors -> requested ones
	expr = strings.ReplaceAll(expr, endpointMatcher, epMatch)
	expr = strings.ReplaceAll(expr, bEndpointMatcher, bepMatch)
	expr = strings.ReplaceAll(expr, scrapeSelectorText, scrapeSel)
	// tidy selectors when there are no scrape labels
	expr = strings.ReplaceAll(expr, "{, ", "{")
	expr = strings.ReplaceAll(expr, ", }", "}")
	return expr
}

var endpointMatcherRe = regexp.MustCompile(`http_route(=~|=)"(?:[^"\\]|\\.)*"`)

func applyParams(expr string, r Recipe, a *Analysis, rr *RenderedRecipe) (string, error) {
	if hasParam(r, "quota_routes") {
		if a == nil {
			if !strings.Contains(rr.Notes, "every 429 counts") {
				rr.Notes = strings.TrimSpace(rr.Notes + " Pass config to restrict this to endpoints with a quota; without it every 429 counts.")
			}
			return expr, nil
		}
		var escaped []string
		for _, ep := range quotaRoutes(a) {
			escaped = append(escaped, escapeRegex(ep.LabelValue))
		}
		return endpointMatcherRe.ReplaceAllLiteralString(expr, fmt.Sprintf(`http_route=~"%s"`, strings.Join(escaped, "|"))), nil
	}
	if !hasParam(r, "ratelimit_routes") {
		return expr, nil
	}
	if a == nil {
		expr = strings.ReplaceAll(expr, "$ratelimit_status", strconv.Itoa(defaultRateLimitStatus))
		rr.Notes = strings.TrimSpace(rr.Notes + " Pass config to restrict this to rate-limited endpoints; without it every 503 counts.")
		return expr, nil
	}
	routes := rateLimitedRoutes(a)
	statuses := map[int]bool{}
	var escaped []string
	for _, ep := range routes {
		statuses[ep.RateLimit.Status] = true
		escaped = append(escaped, escapeRegex(ep.LabelValue))
	}
	var st []string
	for s := range statuses {
		st = append(st, strconv.Itoa(s))
	}
	sort.Strings(st)
	routeRe := fmt.Sprintf(`http_route=~"%s"`, strings.Join(escaped, "|"))
	// replace whatever endpoint matcher the selectors produced
	expr = endpointMatcherRe.ReplaceAllLiteralString(expr, routeRe)
	expr = strings.ReplaceAll(expr, "$ratelimit_status", strings.Join(st, "|"))
	rr.Notes = strings.TrimSpace(rr.Notes + " Inferred: status " + strings.Join(st, "|") + " on rate-limited endpoints; backend 5xx with the same status on those endpoints count too.")
	return expr, nil
}

func quotaRoutes(a *Analysis) []EndpointInfo {
	var out []EndpointInfo
	for _, ep := range a.Endpoints {
		if ep.Quota != "" {
			out = append(out, ep)
		}
	}
	return out
}

func rateLimitedRoutes(a *Analysis) []EndpointInfo {
	var out []EndpointInfo
	for _, ep := range a.Endpoints {
		if ep.RateLimit != nil {
			out = append(out, ep)
		}
	}
	return out
}

func hasParam(r Recipe, p string) bool {
	for _, x := range r.Params {
		if x == p {
			return true
		}
	}
	return false
}

func joinSel(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	}
	return a + ", " + b
}

// escapeLabelValue escapes a value for an exact PromQL label matcher.
func escapeLabelValue(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v)
}

// escapeRegex escapes a value for a PromQL regex label matcher (RE2 inside a string literal).
func escapeRegex(v string) string {
	return escapeLabelValue(regexp.QuoteMeta(v))
}

// grafanaPanel builds a panel JSON fragment following the skill's dashboard conventions.
// gridPos and id are left to the caller.
func grafanaPanel(r RenderedRecipe) map[string]any {
	ds := map[string]any{"type": "prometheus", "uid": "${datasource}"}
	var targets []any
	instant := r.Panel == "table" || r.Panel == "bargauge" || r.Panel == "piechart"
	for i, t := range r.Targets {
		tg := map[string]any{
			"refId":        string(rune('A' + i)),
			"expr":         t.Expr,
			"legendFormat": t.Legend,
		}
		if instant {
			tg["instant"] = true
			tg["range"] = false
			if r.Panel == "table" {
				tg["format"] = "table"
			}
		}
		if r.Panel == "heatmap" {
			tg["format"] = "heatmap"
			tg["legendFormat"] = "{{le}}"
		}
		targets = append(targets, tg)
	}
	desc := r.Title
	if n := strings.TrimSuffix(strings.TrimPrefix(r.Notes, "("), ")"); n != "" {
		desc += ": " + n
	}
	desc += " (recipe " + r.ID + ")"
	w, h := 12, 8
	if r.Panel == "stat" || r.Panel == "gauge" {
		w, h = 6, 4
	}
	panel := map[string]any{
		"type":        r.Panel,
		"title":       r.Title,
		"description": desc,
		"datasource":  ds,
		"gridPos":     map[string]any{"h": h, "w": w, "x": 0, "y": 0},
		"fieldConfig": map[string]any{"defaults": fieldDefaults(r), "overrides": statusOverrides(r)},
		"targets":     targets,
	}
	if r.Panel == "stat" || r.Panel == "gauge" || r.Panel == "bargauge" {
		panel["options"] = map[string]any{"reduceOptions": map[string]any{"calcs": []string{"lastNotNull"}, "fields": "", "values": false}}
	}
	if r.Panel == "bargauge" {
		// Horizontal bars with the series name beside each bar: with the defaults (auto
		// orientation) narrow panels drop the names and the bars can't be told apart.
		opts := panel["options"].(map[string]any)
		opts["orientation"] = "horizontal"
		opts["displayMode"] = "gradient"
		opts["namePlacement"] = "left"
		opts["showUnfilled"] = true
		opts["valueMode"] = "color"
	}
	if r.Panel == "table" {
		// Instant tables otherwise show a Time column and raw label names as headers.
		rename := map[string]any{}
		for label, name := range tableColumnNames {
			rename[label] = name
		}
		for _, t := range r.Targets {
			if strings.Contains(t.Expr, "krakend_endpoint_route") {
				rename["http_route"] = "Backend path" // backend metrics: http_route is the backend's
			}
		}
		valueCol := "Value"
		if len(r.Targets) > 1 {
			valueCol = "Value #A"
		} else {
			// "Avg latency by endpoint" -> "Avg latency": the value column says what it is
			valueCol = tableValueName(r.Title)
			rename["Value"] = valueCol
		}
		order := map[string]any{}
		for i, label := range tableColumnOrder {
			order[label] = i
		}
		order["Value"] = len(tableColumnOrder)
		panel["transformations"] = []any{map[string]any{"id": "organize", "options": map[string]any{
			"excludeByName": map[string]any{"Time": true, "job": true, "instance": true},
			"indexByName":   order,
			"renameByName":  rename,
		}}}
		if r.Unit == "short" {
			// counts from increase() are extrapolated (31.0, 438.6): show whole numbers, and in
			// full (6,865), since "short" with no decimals rounds 6.87 K to "7 K"
			defaults := panel["fieldConfig"].(map[string]any)["defaults"].(map[string]any)
			defaults["decimals"] = 0
			defaults["unit"] = "locale"
		}
		panel["options"] = map[string]any{
			"showHeader": true,
			"sortBy":     []any{map[string]any{"displayName": valueCol, "desc": true}},
		}
	}
	if r.Panel == "heatmap" {
		// The recipe unit is the bucket's (seconds, bytes): it belongs on the y axis. The cells
		// are counts; with the unit in the field defaults the color scale reads "3.31 mins".
		panel["options"] = map[string]any{
			"calculate":  false,
			"yAxis":      map[string]any{"unit": r.Unit},
			"cellValues": map[string]any{"unit": "short"},
		}
	}
	return panel
}

// tableColumnNames are the headers of label columns in table panels.
var tableColumnNames = map[string]string{
	"http_route":                "Endpoint",
	"krakend_endpoint_route":    "Endpoint",
	"http_response_status_code": "Status",
	"server_address":            "Backend host",
	"messaging_topic":           "Topic",
	"messaging_origin":          "Origin",
	"model":                     "Model",
	"provider":                  "Provider",
	"pool_name":                 "Pool",
	"cb_name":                   "Circuit breaker",
}

// tableColumnOrder puts the label columns of table panels in reading order: what (endpoint,
// backend, topic) before the breakdown (status), the value last.
var tableColumnOrder = []string{"krakend_endpoint_route", "http_route", "server_address", "messaging_origin",
	"messaging_topic", "provider", "model", "pool_name", "cb_name", "http_response_status_code"}

// tableValueName names a table's value column after the panel title, without its breakdown.
func tableValueName(title string) string {
	for _, sep := range []string{" by ", " in range", " / "} {
		if i := strings.Index(title, sep); i > 0 {
			title = title[:i]
		}
	}
	return title
}

// statusOverrides colors series by HTTP status class (2xx green, 3xx blue, 4xx orange, 5xx red)
// on panels whose legend shows the status code, as dashboard-conventions.md asks.
func statusOverrides(r RenderedRecipe) []any {
	byStatus := false
	for _, t := range r.Targets {
		if strings.HasSuffix(t.Legend, "{{http_response_status_code}}") {
			byStatus = true
		}
	}
	if !byStatus {
		return []any{}
	}
	var out []any
	color := func(pattern, c string) {
		out = append(out, map[string]any{
			"matcher": map[string]any{"id": "byRegexp", "options": "^(.* )?" + pattern + "$"},
			"properties": []any{map[string]any{"id": "color",
				"value": map[string]any{"mode": "fixed", "fixedColor": c}}},
		})
	}
	// Class colors first, then distinct shades for the common codes (later overrides win), so
	// 404 and 429 (or 500 and 503) stacked together can still be told apart.
	for _, c := range []struct{ class, color string }{{"2", "green"}, {"3", "blue"}, {"4", "orange"}, {"5", "red"}} {
		color(c.class+"\\d\\d", c.color)
	}
	for _, c := range []struct{ code, color string }{
		{"201", "semi-dark-green"}, {"204", "light-green"},
		{"400", "dark-orange"}, {"401", "yellow"}, {"403", "dark-yellow"}, {"404", "light-orange"}, {"429", "purple"},
		{"500", "red"}, {"502", "dark-red"}, {"503", "semi-dark-red"}, {"504", "light-red"},
	} {
		color(c.code, c.color)
	}
	return out
}

// fieldDefaults sets the unit and, for panels that may legitimately be empty, a "no data yet"
// text so an empty panel reads as "nothing happened" rather than "broken".
func fieldDefaults(r RenderedRecipe) map[string]any {
	d := map[string]any{"unit": r.Unit}
	if r.Panel == "heatmap" {
		d["unit"] = "short"
	}
	single := r.Panel == "stat" || r.Panel == "gauge" || r.Panel == "bargauge"
	if r.Panel == "timeseries" {
		// Start the y axis at 0: auto-scaling from the minimum makes small changes look dramatic.
		custom := map[string]any{"axisSoftMin": 0}
		if r.bars {
			custom["drawStyle"] = "bars"
			custom["fillOpacity"] = 80
		}
		if r.stacked {
			custom["stacking"] = map[string]any{"mode": "normal", "group": "A"}
			custom["fillOpacity"] = 60
		}
		d["custom"] = custom
	}
	switch {
	case len(r.thresholds) > 0:
		d["thresholds"] = map[string]any{"mode": "absolute", "steps": r.thresholds}
		if single {
			d["color"] = map[string]any{"mode": "thresholds"}
		}
	case single:
		// No thresholds: a neutral color. Grafana's default thresholds (red from 80) would paint
		// "16.8K requests" red.
		d["color"] = map[string]any{"mode": "fixed", "fixedColor": "blue"}
	}
	return d
}
