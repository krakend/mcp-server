// Package recipesgen turns the hand-edited observability/promql-recipes.md and
// observability/blueprints.md into tools/data/observability/recipes.json, the data behind
// get_dashboard_recipes. Run it with `go run ./cmd/observability-recipes`.
//
// Each recipe gets: id, section, intents, title, panel, unit, targets (expr + legend), notes,
// requires.metrics / requires.groups (from catalog.json), may_be_empty (+ reason), params.
// Build refuses inconsistent recipes (unknown metrics, duplicate legends, "/ s" titles on counts,
// missing $backend_host or 404 filters, recipe ids in panel notes, unknown blueprint recipes).
package recipesgen

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// sectionIntents maps a section heading to the intents a user might ask for.
var sectionIntents = map[string][]string{
	"Traffic": {"traffic"},
	"Errors":  {"errors"},
	"Latency": {"latency"},
	`"Where is the time spent?" (layer breakdown)`: {"latency", "layers"},
	"Backends":                    {"backends"},
	"Resilience (EE)":             {"resilience", "errors"},
	"AI gateway (EE)":             {"ai"},
	"WebSocket (EE)":              {"websocket"},
	"Kafka messaging (EE)":        {"kafka", "messaging"},
	"Redis connection pools (EE)": {"redis"},
	"Async agents (EE)":           {"agents", "async", "messaging"},
	"Quotas (EE)":                 {"quota", "quotas", "governance"},
	"Runtime":                     {"runtime"},
}

var panelTypes = []string{"timeseries", "stat", "bargauge", "table", "piechart", "heatmap", "gauge"}

// Placeholders are the selector shorthands recipes use; get_dashboard_recipes expands them.
var Placeholders = Obj{
	{"SEL", `job=~"$job", instance=~"$instance", http_route=~"$endpoint"`},
	{"BSEL", `job=~"$job", instance=~"$instance", krakend_endpoint_route=~"$endpoint", krakend_endpoint_route!=""`},
	{"J", `job=~"$job", instance=~"$instance"`},
}

var (
	metricRe       = regexp.MustCompile(`\b((?:http|krakend|go|process|websocket|messaging|db_client)_[a-z0-9_]+|up)\s*\{`)
	sectionRe      = regexp.MustCompile(`^## (.+)`)
	rowRe          = regexp.MustCompile("^\\| `([^`]+)` \\| (.*) \\| ([^|]*) \\|\\s*$")
	pairRe         = regexp.MustCompile("`([^`]+)`(?:\\s+as\\s+`([^`]+)`)?")
	asLegendRe     = regexp.MustCompile("(`[^`]+`)\\s+as\\s+`[^`]+`")
	backtickRe     = regexp.MustCompile("`([^`]+)`")
	exprJoinRe     = regexp.MustCompile(`\x00(\s+and\s+\x00)*`)
	spacesRe       = regexp.MustCompile(`\s+`)
	altPanelRe     = regexp.MustCompile(`/ (\w+)`)
	altPanelDropRe = regexp.MustCompile(`/ \w+`)
	nTargetsRe     = regexp.MustCompile(`\(\d+ targets\)`)
	thresholdsRe   = regexp.MustCompile(`\(thresholds ([^/)]+)/([^)]+)\)`)
	byRe           = regexp.MustCompile(`\bby \(([^)]*)\)`)
	quantileRe     = regexp.MustCompile(`^histogram_quantile\(([0-9.]+)`)
	rateRe         = regexp.MustCompile(`\b(rate|irate|deriv)\(|/ \$__range_s`)
	clientSelRe    = regexp.MustCompile(`\b(http_client_[a-z0-9_]+)\{([^}]*)\}`)
	byRouteRe      = regexp.MustCompile(`by \((le, )?http_route\b`)
	agentNoteRe    = regexp.MustCompile(`\(agent: ([^()]*(?:\([^()]*\)[^()]*)*)\)`)
	agentDropRe    = regexp.MustCompile(`\s*\(agent: [^()]*(?:\([^()]*\)[^()]*)*\)`)
	recipeIDRe     = regexp.MustCompile(`\b[a-z_]+\.[a-z0-9_/]+\b`)
	bpHeadRe       = regexp.MustCompile(`^## (\S+)\s*$`)
	bpRowRe        = regexp.MustCompile(`^- (Optional )?([^:]+): (.+)$`)
	bpIDRe         = regexp.MustCompile("`([a-z]+\\.[a-z0-9_/]+)`")
)

// Result is the generated recipes.json plus counts for the summary line.
type Result struct {
	JSON       []byte
	Recipes    int
	Sections   int
	MayBeEmpty int
}

// Error lists every problem found, so one run shows all of them.
type Error struct{ Problems []string }

func (e *Error) Error() string { return "ERROR:\n  " + strings.Join(e.Problems, "\n  ") }

type catalogEntry struct {
	Name          string   `json:"name"`
	Group         string   `json:"group"`
	Series        []string `json:"series"`
	Labels        []string `json:"labels"`
	Notes         any      `json:"notes"`
	PredictedName any      `json:"predicted_name"`
}

type catalog struct {
	KrakendVersion any            `json:"krakend_version"`
	Metrics        []catalogEntry `json:"metrics"`
	Runtime        struct {
		Families []string `json:"families"`
	} `json:"runtime"`
}

func metricIndex(c catalog) map[string]catalogEntry {
	idx := map[string]catalogEntry{}
	for _, m := range c.Metrics {
		idx[m.Name] = m
		for _, s := range m.Series {
			idx[s] = m
		}
	}
	for _, fam := range c.Runtime.Families {
		for _, n := range []string{fam, fam + "_count", fam + "_sum", fam + "_bucket"} {
			idx[n] = catalogEntry{Group: "runtime", Name: fam}
		}
	}
	idx["up"] = catalogEntry{Group: "scrape", Name: "up"}
	return idx
}

// truthy mirrors Python truthiness for the loosely typed catalog fields.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case bool:
		return x
	case float64:
		return x != 0
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func pyFormatG(v float64) string { return strconv.FormatFloat(v, 'g', 6, 64) }

func legendFor(expr, title string, nTargets, i int) string {
	if m := byRe.FindStringSubmatch(expr); m != nil {
		var labels []string
		for _, l := range strings.Split(m[1], ",") {
			if l = strings.TrimSpace(l); l != "" && l != "le" {
				labels = append(labels, "{{"+l+"}}")
			}
		}
		if len(labels) > 0 {
			return strings.Join(labels, " ")
		}
	}
	if q := quantileRe.FindStringSubmatch(expr); q != nil {
		if f, err := strconv.ParseFloat(q[1], 64); err == nil {
			return "p" + pyFormatG(f*100)
		}
	}
	if nTargets > 1 {
		if m := metricRe.FindStringSubmatch(expr); m != nil {
			return m[1]
		}
		return fmt.Sprintf("%s %d", title, i+1)
	}
	return title
}

func thresholdValue(v string) (float64, error) {
	v = strings.TrimSpace(v)
	parse := func(s string) (float64, error) { return strconv.ParseFloat(strings.TrimSpace(s), 64) }
	switch {
	case strings.HasSuffix(v, "%"):
		f, err := parse(v[:len(v)-1])
		return f / 100, err
	case strings.HasSuffix(v, "ms"):
		f, err := parse(v[:len(v)-2])
		return f / 1000, err
	case strings.HasSuffix(v, "s"):
		return parse(v[:len(v)-1])
	}
	return parse(v)
}

// panelOptions pulls "(thresholds A/B)", "(red if < 1)" and "(red if > 0)" out of the panel
// cell. Thresholds become Grafana steps: green -> orange at A -> red at B (reversed when A > B,
// e.g. "99%/95%" for an SLO where higher is better).
func panelOptions(rid, extra string, problems *[]string) ([]any, string) {
	var steps []any
	step := func(color string, value any) Obj { return Obj{{"color", color}, {"value", value}} }
	if m := thresholdsRe.FindStringSubmatch(extra); m != nil {
		a, errA := thresholdValue(m[1])
		b, errB := thresholdValue(m[2])
		if errA != nil || errB != nil {
			*problems = append(*problems, fmt.Sprintf("%s: can't parse thresholds %q", rid, m[0]))
		} else if a <= b {
			steps = []any{step("green", nil), step("orange", Float(a)), step("red", Float(b))}
		} else {
			steps = []any{step("red", nil), step("orange", Float(b)), step("green", Float(a))}
		}
		extra = strings.ReplaceAll(extra, m[0], "")
	}
	if strings.Contains(extra, "(red if < 1)") {
		steps = []any{step("red", nil), step("green", 1)}
		extra = strings.ReplaceAll(extra, "(red if < 1)", "")
	}
	if strings.Contains(extra, "(red if > 0)") {
		steps = []any{step("green", nil), step("red", Float(0.001))}
		extra = strings.ReplaceAll(extra, "(red if > 0)", "")
	}
	return steps, strings.TrimSpace(spacesRe.ReplaceAllString(extra, " "))
}

type blueprintRow struct {
	title    string
	recipes  []string
	optional bool
}

type blueprint struct {
	intent string
	rows   []blueprintRow
	notes  []string
}

func parseBlueprints(text string) []*blueprint {
	var out []*blueprint
	var current *blueprint
	for _, line := range splitLines(text) {
		if h := bpHeadRe.FindStringSubmatch(line); h != nil {
			current = nil
			for _, bp := range out {
				if bp.intent == h[1] {
					current = bp
				}
			}
			if current == nil {
				current = &blueprint{intent: h[1]}
				out = append(out, current)
			}
			continue
		}
		if current == nil || strings.TrimSpace(line) == "" {
			continue
		}
		if row := bpRowRe.FindStringSubmatch(line); row != nil {
			r := blueprintRow{title: strings.TrimSpace(row[2]), recipes: []string{}, optional: row[1] != ""}
			for _, m := range bpIDRe.FindAllStringSubmatch(row[3], -1) {
				r.recipes = append(r.recipes, m[1])
			}
			current.rows = append(current.rows, r)
		} else {
			current.notes = append(current.notes, strings.TrimSpace(line))
		}
	}
	return out
}

// splitLines splits like Python's str.splitlines for \n and \r\n text.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

type recipe struct {
	id      string
	section string
	intents []string
	fields  Obj // everything after intents, in output order
	notes   string
	title   string
}

// Build generates recipes.json from the two markdown sources and catalog.json.
func Build(recipesMD, blueprintsMD string, catalogJSON []byte) (*Result, error) {
	var cat catalog
	if err := json.Unmarshal(catalogJSON, &cat); err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}
	idx := metricIndex(cat)

	var recipes []*recipe
	var problems []string
	sectionNotes := Obj{}
	var section string
	var header []string
	var notesBuf []string
	haveSection := false
	flushNotes := func() {
		if haveSection && len(notesBuf) > 0 {
			sectionNotes = sectionNotes.Set(section, strings.TrimSpace(strings.Join(notesBuf, "\n")))
		}
	}

	for _, line := range splitLines(recipesMD) {
		if h := sectionRe.FindStringSubmatch(line); h != nil {
			flushNotes()
			section, header, notesBuf, haveSection = strings.TrimSpace(h[1]), nil, nil, true
			continue
		}
		if !haveSection {
			continue
		}
		if strings.HasPrefix(line, "| id |") {
			header = nil
			for _, c := range strings.Split(strings.Trim(line, "|"), "|") {
				header = append(header, strings.TrimSpace(c))
			}
			continue
		}
		if strings.HasPrefix(line, "|---") {
			continue
		}
		row := rowRe.FindStringSubmatch(line)
		if row == nil || header == nil {
			if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "|") {
				notesBuf = append(notesBuf, line)
			}
			continue
		}
		rid, middle, unit := row[1], row[2], strings.TrimSpace(row[3])
		title := ""
		if len(header) > 1 && header[1] == "title" {
			parts := strings.SplitN(middle, " | ", 2)
			if len(parts) < 2 {
				problems = append(problems, rid+": can't split the title column")
				continue
			}
			title, middle = strings.TrimSpace(parts[0]), parts[1]
		}
		parts := strings.SplitN(middle, " | ", 2)
		if len(parts) < 2 {
			problems = append(problems, rid+": can't split the panel and query columns")
			continue
		}
		second, queryCell := parts[0], parts[1]

		// `expr` as `Legend`: explicit legend for that target
		var exprs []string
		legends := map[string]string{}
		for _, p := range pairRe.FindAllStringSubmatch(queryCell, -1) {
			if strings.ContainsAny(p[1], "({") {
				exprs = append(exprs, p[1])
			}
			if p[2] != "" {
				legends[p[1]] = p[2]
			}
		}
		queryCell = asLegendRe.ReplaceAllString(queryCell, "$1")
		// drop the PromQL spans (and the "and" joining them), keep other backticked words as text
		note := backtickRe.ReplaceAllStringFunc(queryCell, func(m string) string {
			inner := m[1 : len(m)-1]
			if contains(exprs, inner) {
				return "\x00"
			}
			return inner
		})
		note = exprJoinRe.ReplaceAllString(note, "")
		note = strings.Trim(spacesRe.ReplaceAllString(note, " "), " ;,")

		panel, legendCol := "timeseries", ""
		var altPanels []string
		var thresholds []any
		stacked, bars := false, false
		isLegendTable := len(header) > 1 && header[1] == "legend"
		if title == "" && !isLegendTable {
			problems = append(problems, rid+": no title (add a title column to the table)")
			continue
		}
		if isLegendTable {
			legendCol = strings.TrimSpace(second)
			title = legendCol
		} else {
			ptype := strings.TrimSpace(strings.Split(second, " ")[0])
			if contains(panelTypes, ptype) {
				panel = ptype
			}
			extra := strings.TrimSpace(second[len(ptype):])
			thresholds, extra = panelOptions(rid, extra, &problems)
			stacked, bars = strings.Contains(extra, "(stacked)"), strings.Contains(extra, "(bars)")
			// panel settings, not notes: applied to the panel JSON (or implied by the panel type)
			for _, marker := range []string{"(stacked)", "(bars)", "(format: heatmap)"} {
				extra = strings.ReplaceAll(extra, marker, "")
			}
			for _, m := range altPanelRe.FindAllStringSubmatch(extra, -1) {
				altPanels = append(altPanels, m[1])
			}
			extra = strings.TrimSpace(altPanelDropRe.ReplaceAllString(extra, ""))
			extra = strings.TrimSpace(nTargetsRe.ReplaceAllString(extra, ""))
			if extra != "" {
				if note != "" {
					extra += "; " + note
				}
				note = strings.TrimSpace(extra)
			}
		}
		if len(exprs) == 0 {
			problems = append(problems, rid+": no PromQL found")
			continue
		}

		var targets []any
		seen := map[string]bool{}
		var legendList []string
		for i, e := range exprs {
			l := legends[e]
			if l == "" {
				l = legendCol
			}
			if l == "" {
				l = legendFor(e, title, len(exprs), i)
			}
			targets = append(targets, Obj{{"expr", e}, {"legend", l}})
			seen[l] = true
			legendList = append(legendList, l)
		}
		if len(seen) < len(targets) {
			problems = append(problems, fmt.Sprintf("%s: several targets share a legend (%s); name them with `expr` as `Legend`",
				rid, pyList(legendList)))
		}
		if strings.Contains(title, "/ s") {
			for _, e := range exprs {
				if !rateRe.MatchString(e) {
					problems = append(problems, fmt.Sprintf("%s: title %s says '/ s' but the query isn't a rate", rid, pyRepr(title)))
					break
				}
			}
		}
		for _, e := range exprs {
			for _, m := range clientSelRe.FindAllStringSubmatch(e, -1) {
				entry := idx[m[1]]
				if contains(entry.Labels, "server_address") && !strings.Contains(m[2], "server_address") &&
					!strings.Contains(m[2], `krakend_endpoint_route=""`) {
					problems = append(problems, fmt.Sprintf("%s: %s has server_address but the selector doesn't filter "+
						`by $backend_host (add server_address=~"$backend_host")`, rid, m[1]))
				}
			}
		}
		joined := strings.Join(exprs, " ")
		if byRouteRe.MatchString(joined) && strings.Contains(joined, "http_server_") &&
			!strings.Contains(joined, "404 Not Found") &&
			!strings.HasPrefix(rid, "layers.") && !strings.HasPrefix(rid, "ratelimit.") && !strings.HasPrefix(rid, "quota.") {
			problems = append(problems, rid+`: groups http_server_* by endpoint without http_route!="404 Not Found"`)
		}

		metrics, groups := map[string]bool{}, map[string]bool{}
		emptyReasons := map[string]bool{}
		for _, e := range exprs {
			for _, m := range metricRe.FindAllStringSubmatch(e, -1) {
				name := m[1]
				entry, ok := idx[name]
				if !ok {
					problems = append(problems, fmt.Sprintf("%s: metric %s not in catalog", rid, name))
					continue
				}
				metrics[name] = true
				groups[entry.Group] = true
				notes, _ := entry.Notes.(string)
				if strings.Contains(notes, "first occurrence") || truthy(entry.PredictedName) {
					emptyReasons[entry.Name+" is only exported after the first event"] = true
				}
			}
		}
		zeroFallback := true
		for _, e := range exprs {
			zeroFallback = zeroFallback && strings.Contains(e, "or vector(0)")
		}

		intents, ok := sectionIntents[section]
		if !ok {
			intents = []string{strings.ToLower(section)}
		}
		u := "short"
		if unit != "" {
			u = strings.Split(unit, " ")[0]
		}
		r := &recipe{id: rid, section: section, intents: append([]string{}, intents...), title: title}
		r.fields = Obj{
			{"title", title},
			{"panel", panel},
			{"unit", u},
			{"targets", targets},
			{"requires", Obj{{"metrics", sortedKeys(metrics)}, {"groups", sortedKeys(groups)}}},
		}
		if len(thresholds) > 0 {
			r.fields = append(r.fields, KV{"thresholds", thresholds})
		}
		if len(altPanels) > 0 {
			kept := []string{}
			for _, p := range altPanels {
				if contains(panelTypes, p) {
					kept = append(kept, p)
				}
			}
			r.fields = append(r.fields, KV{"alt_panels", kept})
		}
		// "(agent: ...)" notes are instructions for the agent, never shown on the panel
		var agentNotes []string
		for _, m := range agentNoteRe.FindAllStringSubmatch(note, -1) {
			agentNotes = append(agentNotes, strings.TrimSpace(m[1]))
		}
		note = strings.Trim(agentDropRe.ReplaceAllString(note, ""), " ;,")
		if note != "" {
			r.fields = append(r.fields, KV{"notes", note})
			r.notes = note
		}
		if len(agentNotes) > 0 {
			r.fields = append(r.fields, KV{"agent_notes", strings.Join(agentNotes, "; ")})
		}
		if stacked {
			r.fields = append(r.fields, KV{"stacked", true})
		}
		if bars {
			r.fields = append(r.fields, KV{"bars", true})
		}
		if len(emptyReasons) > 0 && !zeroFallback {
			r.fields = append(r.fields, KV{"may_be_empty", true}, KV{"may_be_empty_reason", strings.Join(sortedKeys(emptyReasons), "; ")})
		}
		var params []string
		if strings.Contains(joined, "$ratelimit_status") {
			params = []string{"ratelimit_routes", "ratelimit_status"}
		}
		if strings.HasPrefix(rid, "quota.") {
			params = []string{"quota_routes"}
		}
		if strings.HasPrefix(rid, "layers.") {
			params = append(params, "single_endpoint")
		}
		if params != nil {
			r.fields = append(r.fields, KV{"params", params})
		}
		recipes = append(recipes, r)
	}
	flushNotes()
	if len(problems) > 0 {
		return nil, &Error{problems}
	}

	byID := map[string]*recipe{}
	counts := map[string]int{}
	for _, r := range recipes {
		byID[r.id] = r
		counts[r.id]++
	}
	blueprints := parseBlueprints(blueprintsMD)
	optional := map[*recipe][]string{}
	for _, bp := range blueprints {
		for _, row := range bp.rows {
			for _, rid := range row.recipes {
				r, ok := byID[rid]
				switch {
				case !ok:
					problems = append(problems, fmt.Sprintf("blueprint %s row %s lists unknown recipe %s", pyRepr(bp.intent), pyRepr(row.title), rid))
				case row.optional:
					if !contains(r.intents, bp.intent) {
						optional[r] = append(optional[r], bp.intent)
					}
				case !contains(r.intents, bp.intent):
					r.intents = append(r.intents, bp.intent)
				}
			}
		}
	}
	// notes end up in panel descriptions, read by dashboard users: name panels, not recipe ids
	for _, r := range recipes {
		mentioned := map[string]bool{}
		for _, id := range recipeIDRe.FindAllString(r.notes, -1) {
			if byID[id] != nil {
				mentioned[id] = true
			}
		}
		for _, id := range sortedKeys(mentioned) {
			problems = append(problems, fmt.Sprintf("%s: notes mention recipe id %s; name the panel (%q) instead, or move it to an (agent: ...) note",
				r.id, id, byID[id].title))
		}
	}
	if len(problems) > 0 {
		return nil, &Error{problems}
	}
	var dupes []string
	for id, n := range counts {
		if n > 1 {
			dupes = append(dupes, id)
		}
	}
	if len(dupes) > 0 {
		sort.Strings(dupes)
		return nil, &Error{[]string{"duplicate recipe ids: " + pyList(dupes)}}
	}

	bpObj := Obj{}
	for _, bp := range blueprints {
		rows := []any{}
		for _, row := range bp.rows {
			rows = append(rows, Obj{{"title", row.title}, {"recipes", row.recipes}, {"optional", row.optional}})
		}
		bpObj = bpObj.Set(bp.intent, Obj{{"rows", rows}, {"notes", strings.Join(bp.notes, " ")}})
	}
	recipeList := []any{}
	mayBeEmpty := 0
	for _, r := range recipes {
		o := Obj{{"id", r.id}, {"section", r.section}, {"intents", r.intents}}
		o = append(o, r.fields...)
		if oi := optional[r]; len(oi) > 0 {
			o = append(o, KV{"optional_intents", oi})
		}
		if o.Get("may_be_empty") == true {
			mayBeEmpty++
		}
		recipeList = append(recipeList, o)
	}
	out := Obj{
		{"_about", "GENERATED from observability/promql-recipes.md and observability/blueprints.md by cmd/observability-recipes. Do not edit by hand."},
		{"krakend_version", cat.KrakendVersion},
		{"placeholders", Placeholders},
		{"section_notes", sectionNotes},
		{"blueprints", bpObj},
		{"recipes", recipeList},
	}
	return &Result{JSON: append(Marshal(out), '\n'), Recipes: len(recipes), Sections: len(sectionNotes), MayBeEmpty: mayBeEmpty}, nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func pyRepr(s string) string { return "'" + strings.ReplaceAll(s, "'", `\'`) + "'" }

func pyList(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = pyRepr(s)
	}
	return "[" + strings.Join(q, ", ") + "]"
}
