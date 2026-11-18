package observability

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// LintOptions selects the convention checks a caller wants on top of the generic ones.
type LintOptions struct {
	// UIDPrefix, if set, is required on the dashboard uid (e.g. "krakend-ai-").
	UIDPrefix string
	// RequiredTags must all be present in the dashboard tags.
	RequiredTags []string
	// SemanticConvention "1.27" also returns every query converted to the 1.27 metric names.
	SemanticConvention string
	// Expand returns every query with Grafana variables replaced, ready to run against Prometheus.
	Expand bool
}

// LintResult is the outcome of LintDashboard.
type LintResult struct {
	Errors    []string        `json:"errors"`
	Warnings  []string        `json:"warnings"`
	Queries   int             `json:"queries"`
	Expanded  []LintQuery     `json:"expanded,omitempty"`
	Converted []ConvertedExpr `json:"converted,omitempty"`
}

// LintQuery is one panel query with its variables expanded.
type LintQuery struct {
	Panel string `json:"panel"`
	RefID string `json:"ref_id"`
	Expr  string `json:"expr"`
}

// ConvertedExpr is a query that changes under semantic convention 1.27.
type ConvertedExpr struct {
	Panel string `json:"panel"`
	RefID string `json:"ref_id"`
	From  string `json:"from"`
	To    string `json:"to"`
}

var (
	lintBuiltinVars = map[string]bool{"__rate_interval": true, "__interval": true, "__range": true, "__range_s": true,
		"__range_ms": true, "__interval_ms": true, "__from": true, "__to": true, "__all": true, "__dashboard": true, "__name": true}
	lintExpand = []struct{ from, to string }{ // longest first
		{"$__rate_interval", "1m"}, {"$__range_s", "3600"}, {"$__interval", "30s"}, {"$__range", "1h"},
	}
	lintKnownPrefixes = []string{"http_", "krakend_", "go_", "process_", "websocket_", "db_client_", "messaging_"}
	lintUnitless      = map[string]bool{"row": true, "text": true, "news": true, "dashlist": true, "logs": true}

	reVarUse        = regexp.MustCompile(`\$\{?([A-Za-z_]\w*)`) // not $1 (label_replace)
	reVarAny        = regexp.MustCompile(`\$\{?[A-Za-z_]\w*\}?`)
	reGroupedBy     = regexp.MustCompile(`\bby\s*\(`)
	reMetricSel     = regexp.MustCompile(`\b([a-z_:][a-z0-9_:]*)\s*\{`)
	reByLe          = regexp.MustCompile(`by\s*\([^)]*\ble\b`)
	reRateInterval  = regexp.MustCompile(`(rate|increase)\([^)]*\[\$__interval\]`)
	reRegexFmt      = regexp.MustCompile(`\$\{\w+:regex\}`)
	reRateOver      = regexp.MustCompile(`\b(rate|irate|increase)\(\s*([a-z_:][a-z0-9_:]*)\s*[{\[]`)
	reStatusText    = regexp.MustCompile(`(?i)none yet|until the first|no data (yet|until)|not (yet )?(verified|tested)|empty until`)
	reEnvLabel      = regexp.MustCompile(`\b(job|instance|server_address|host|pod|namespace|cluster)\s*=~?\s*"([^"$]+)"`)
	reEnvHost       = regexp.MustCompile(`localhost|127\.0\.0\.1|host\.docker\.internal`)
	histogramSuffix = []string{"_bucket", "_count", "_sum"}
)

// knownMetricNames lists every Prometheus series name the catalog knows, in both conventions.
func (d *Data) knownMetricNames() map[string]bool {
	known := map[string]bool{"up": true}
	add := func(name, typ string, series []string) {
		if name == "" {
			return
		}
		known[name] = true
		for _, s := range series {
			known[s] = true
		}
		if typ == "histogram" {
			for _, s := range histogramSuffix {
				known[name+s] = true
			}
		}
	}
	for _, m := range d.Catalog.Metrics {
		add(m.Name, m.Type, m.Series)
		if alt := m.Semconv127; alt != nil {
			add(alt.Name, alt.Type, alt.Series)
		}
	}
	for _, fam := range d.Catalog.Runtime.Families {
		for _, s := range append([]string{""}, histogramSuffix...) {
			known[fam+s] = true
		}
	}
	return known
}

// gaugeNames lists families that must not go through rate()/increase(): gauges and summary quantiles.
func (d *Data) gaugeNames() map[string]bool {
	g := map[string]bool{}
	for _, m := range d.Catalog.Metrics {
		if m.Name != "" && m.Type == "gauge" {
			g[m.Name] = true
		}
	}
	for fam, info := range d.Catalog.Runtime.Details {
		if info.Type == "gauge" || info.Type == "summary" {
			g[fam] = true
		}
	}
	return g
}

// LintDashboard checks a Grafana dashboard built from KrakenD metrics: broken or unknown metric
// names, rate() over gauges, histogram quantiles without le, undefined variables, layout overlaps,
// and anything that ties the JSON to the environment it was built in (it must import into a
// production Grafana unchanged).
func (d *Data) LintDashboard(dash map[string]any, opts LintOptions) LintResult {
	if inner, ok := dash["dashboard"].(map[string]any); ok && dash["panels"] == nil {
		dash = inner
	}
	res := LintResult{Errors: []string{}, Warnings: []string{}}
	errf := func(f string, a ...any) { res.Errors = append(res.Errors, fmt.Sprintf(f, a...)) }
	warnf := func(f string, a ...any) { res.Warnings = append(res.Warnings, fmt.Sprintf(f, a...)) }
	known, gauges := d.knownMetricNames(), d.gaugeNames()

	uid := asString(dash["uid"])
	if opts.UIDPrefix != "" && (!strings.HasPrefix(uid, opts.UIDPrefix) || strings.Contains(uid, "REPLACE")) {
		errf("uid must be '%s<slug>', got %q", opts.UIDPrefix, uid)
	}
	if len(uid) > 40 {
		errf("uid longer than 40 chars: %q", uid)
	}
	tags := map[string]bool{}
	for _, t := range asSlice(dash["tags"]) {
		tags[asString(t)] = true
	}
	for _, t := range opts.RequiredTags {
		if !tags[t] {
			errf("tags must include %q", t)
		}
	}
	if strings.Contains(asString(dash["title"])+asString(dash["description"]), "REPLACE") {
		errf("title/description still contain REPLACE")
	}

	templating := asSlice(asMap(dash["templating"])["list"])
	defined, used := map[string]bool{}, map[string]bool{}
	for _, v := range templating {
		defined[asString(asMap(v)["name"])] = true
	}

	type cell struct {
		where      string
		x, y, w, h float64
	}
	var cells []cell
	type query struct{ where, ref, expr string }
	var queries []query
	ids := map[string]bool{}

	var panels []map[string]any
	var walk func([]any)
	walk = func(ps []any) {
		for _, p := range ps {
			pm := asMap(p)
			if pm == nil {
				continue
			}
			panels = append(panels, pm)
			walk(asSlice(pm["panels"]))
		}
	}
	walk(asSlice(dash["panels"]))

	for _, p := range panels {
		id := fmt.Sprint(p["id"])
		ptype, title := asString(p["type"]), asString(p["title"])
		where := fmt.Sprintf("panel %s '%s'", id, title)
		if ids[id] {
			errf("%s: duplicate panel id", where)
		}
		ids[id] = true
		if gp := asMap(p["gridPos"]); gp == nil {
			errf("%s: missing gridPos", where)
		} else {
			c := cell{where, num(gp["x"]), num(gp["y"]), num(gp["w"]), num(gp["h"])}
			if c.x+c.w > 24 {
				errf("%s: gridPos exceeds 24 columns", where)
			}
			cells = append(cells, c)
		}
		if lintUnitless[ptype] {
			continue
		}
		if asString(asMap(p["datasource"])["uid"]) != "${datasource}" {
			errf("%s: datasource must be {type: prometheus, uid: ${datasource}}", where)
		}
		defaults := asMap(asMap(p["fieldConfig"])["defaults"])
		if asString(defaults["unit"]) == "" {
			warnf("%s: no unit set", where)
		}
		desc := asString(p["description"])
		if desc == "" {
			warnf("%s: no description", where)
		}
		if _, ok := defaults["noValue"]; ok {
			errf("%s: remove noValue: empty panels show Grafana's plain 'No data'", where)
		}
		if reStatusText.MatchString(title + " " + desc) {
			errf("%s: title/description mentions data or test status; say it in the reply, not on the panel", where)
		}
		targets := asSlice(p["targets"])
		if len(targets) == 0 {
			errf("%s: no targets", where)
		}
		for _, t := range targets {
			tm := asMap(t)
			expr, ref := asString(tm["expr"]), asString(tm["refId"])
			if expr == "" {
				errf("%s target %s: empty expr", where, ref)
				continue
			}
			queries = append(queries, query{where, ref, expr})
			for _, m := range reVarUse.FindAllStringSubmatch(expr, -1) {
				used[m[1]] = true
			}
			if lf := asString(tm["legendFormat"]); (lf == "" || lf == "__auto") && reGroupedBy.MatchString(expr) {
				warnf("%s target %s: grouped query without legendFormat", where, ref)
			}
			for _, m := range reMetricSel.FindAllStringSubmatch(expr, -1) {
				if hasAnyPrefix(m[1], lintKnownPrefixes) && !known[m[1]] {
					errf("%s: unknown metric '%s' (not in the KrakenD metrics catalog; verify with list_prometheus_metric_names)", where, m[1])
				}
			}
			if strings.Contains(expr, "histogram_quantile") && strings.Contains(expr, "_bucket") && !reByLe.MatchString(expr) {
				errf("%s: histogram_quantile without 'le' in by()", where)
			}
			if reRateInterval.MatchString(expr) {
				warnf("%s: use $__rate_interval instead of $__interval in rate/increase", where)
			}
			if reRegexFmt.MatchString(expr) {
				errf("%s: ${var:regex} produces escapes PromQL rejects (e.g. \\-); use plain $var", where)
			}
			for _, m := range reRateOver.FindAllStringSubmatch(expr, -1) {
				if gauges[m[2]] {
					errf("%s: %s() over '%s', which is a gauge (or summary quantile): use it as-is, avg_over_time/max_over_time, or delta/deriv", where, m[1], m[2])
				}
			}
		}
	}

	// Portability: the JSON is imported into production Grafanas; nothing from the dev environment.
	for _, v := range templating {
		vm := asMap(v)
		name := asString(vm["name"])
		if (name == "job" || name == "instance") && asString(vm["allValue"]) == ".*" {
			warnf("variable $%s: allValue '.*' makes All match every target of a shared Prometheus (up, process_*, go_*); remove it so All means the KrakenD ones found", name)
		}
		cur := asMap(vm["current"])
		if asString(vm["type"]) == "datasource" {
			if cur["value"] != nil && fmt.Sprint(cur["value"]) != "" || asString(cur["text"]) != "" {
				errf("variable $%s: current is set to a specific datasource; use {} so it picks the target Grafana's default", name)
			}
		} else if asBool(vm["includeAll"]) && len(cur) > 0 && !isAllValue(cur["value"]) {
			errf("variable $%s: current selection %v comes from this Grafana; reset to All", name, cur["text"])
		}
	}
	for _, q := range queries {
		for _, m := range reEnvLabel.FindAllStringSubmatch(q.expr, -1) {
			if m[2] != ".*" {
				errf("%s target %s: %s=\"%s\" is environment-specific; use a $variable", q.where, q.ref, m[1], m[2])
			}
		}
		if reEnvHost.MatchString(q.expr) {
			errf("%s target %s: hostname from this environment in the query; use a $variable", q.where, q.ref)
		}
	}

	var missing []string
	for v := range used {
		if !defined[v] && !lintBuiltinVars[v] && !strings.HasPrefix(v, "__") {
			missing = append(missing, v)
		}
	}
	sort.Strings(missing)
	for _, v := range missing {
		errf("variable $%s used but not defined in templating", v)
	}
	tplJSON, _ := json.Marshal(asMap(dash["templating"]))
	var unused []string
	for v := range defined {
		if v != "datasource" && !used[v] && !strings.Contains(string(tplJSON), "$"+v) {
			unused = append(unused, v)
		}
	}
	sort.Strings(unused)
	for _, v := range unused {
		warnf("variable $%s defined but never used", v)
	}

	for i, a := range cells {
		for _, b := range cells[i+1:] {
			if a.x < b.x+b.w && b.x < a.x+a.w && a.y < b.y+b.h && b.y < a.y+a.h {
				errf("overlapping gridPos: %s and %s", a.where, b.where)
			}
		}
	}

	res.Queries = len(queries)
	for _, q := range queries {
		if opts.Expand {
			e := q.expr
			for _, r := range lintExpand {
				e = strings.ReplaceAll(e, r.from, r.to)
			}
			res.Expanded = append(res.Expanded, LintQuery{Panel: q.where, RefID: q.ref, Expr: reVarAny.ReplaceAllString(e, ".*")})
		}
		if opts.SemanticConvention == Convention127 {
			if to := d.ConvertExpr(q.expr, Convention127); to != q.expr {
				res.Converted = append(res.Converted, ConvertedExpr{Panel: q.where, RefID: q.ref, From: q.expr, To: to})
			}
		}
	}
	return res
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func isAllValue(v any) bool {
	switch x := v.(type) {
	case string:
		return x == "$__all"
	case []any:
		return len(x) == 1 && asString(x[0]) == "$__all"
	}
	return false
}
