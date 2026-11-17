package observability

import (
	"regexp"
	"sort"
	"strings"
)

// Semantic conventions selectable with telemetry/opentelemetry layers.global.semantic_convention.
const (
	ConventionDefault = ""
	Convention127     = "1.27"
)

type semconvRule struct {
	old     string
	pattern *regexp.Regexp
	replace func(match []string) string
}

// buildSemconvRules derives rename rules from the catalog's semconv_1_27 fields.
func buildSemconvRules(cat *Catalog) []semconvRule {
	var rules []semconvRule
	for _, m := range cat.Metrics {
		alt := m.Semconv127
		if alt == nil || alt.Name == "" || m.Name == "" {
			continue
		}
		oldName, newName := m.Name, alt.Name
		switch {
		case m.Type == "counter" && alt.Type == "histogram":
			// counter X_total becomes histogram Y: its running total is Y_sum
			rules = append(rules, semconvRule{
				old:     oldName,
				pattern: regexp.MustCompile(`\b` + regexp.QuoteMeta(oldName) + `\b`),
				replace: func([]string) string { return newName + "_sum" },
			})
		case m.Type == "histogram":
			rules = append(rules, semconvRule{
				old:     oldName,
				pattern: regexp.MustCompile(`\b` + regexp.QuoteMeta(oldName) + `(_bucket|_count|_sum)?\b`),
				replace: func(g []string) string { return newName + g[1] },
			})
		default:
			rules = append(rules, semconvRule{
				old:     oldName,
				pattern: regexp.MustCompile(`\b` + regexp.QuoteMeta(oldName) + `\b`),
				replace: func([]string) string { return newName },
			})
		}
	}
	// longest names first so a short name never rewrites part of a longer one
	sort.SliceStable(rules, func(i, j int) bool { return len(rules[i].old) > len(rules[j].old) })
	return rules
}

// ConvertExpr rewrites a PromQL expression written with default metric names to the given
// semantic convention. Labels and buckets are identical in both conventions.
func (d *Data) ConvertExpr(expr, convention string) string {
	if convention != Convention127 {
		return expr
	}
	for _, r := range d.semconvRules {
		expr = r.pattern.ReplaceAllStringFunc(expr, func(s string) string {
			return r.replace(r.pattern.FindStringSubmatch(s))
		})
	}
	return expr
}

// Detection results of DetectConvention.
const (
	DetectionFound   = "detected"
	DetectionMixed   = "mixed"
	DetectionUnknown = "unknown"
)

// DetectConvention guesses the semantic convention from live metric names (for example the
// output of Grafana's list_prometheus_metric_names). status is DetectionMixed when both
// conventions are present (several KrakenD versions or settings scraped together) and
// DetectionUnknown when the names say nothing; the default convention is returned then.
func (d *Data) DetectConvention(metricNames []string) (convention, status string) {
	var hasDefault, has127 bool
	for _, n := range metricNames {
		m, found := d.metricByName[n]
		if found && m.Semconv127 != nil {
			hasDefault = true
		}
		for _, mm := range d.Catalog.Metrics {
			if mm.Semconv127 == nil {
				continue
			}
			if n == mm.Semconv127.Name || strings.HasPrefix(n, mm.Semconv127.Name+"_") {
				has127 = true
			}
		}
	}
	switch {
	case has127 && !hasDefault:
		return Convention127, DetectionFound
	case hasDefault && !has127:
		return ConventionDefault, DetectionFound
	case hasDefault && has127:
		return ConventionDefault, DetectionMixed
	}
	return ConventionDefault, DetectionUnknown
}
