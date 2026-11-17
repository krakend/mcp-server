package observability

import (
	"fmt"
	"sort"
	"strings"
)

// CatalogRequest filters the metrics catalog.
type CatalogRequest struct {
	Groups             []string // catalog groups (global, backend-roundtrip, circuit-breaker, ai, ...)
	Query              string   // case-insensitive search in names, meanings and labels
	Names              []string // exact metric names (either convention, series suffixes allowed) or OTel names
	SemanticConvention string   // "", "1.27" or "auto"
	MetricNames        []string // live metric names for "auto"
}

// CatalogResponse is the filtered catalog.
type CatalogResponse struct {
	KrakenDVersion     string            `json:"krakend_version"`
	SemanticConvention string            `json:"semantic_convention"`
	ConventionSource   string            `json:"convention_source"`
	ConventionInfo     string            `json:"convention_info,omitempty"`
	Groups             map[string]Group  `json:"groups"`
	Metrics            []CatalogMetric   `json:"metrics"`
	Runtime            *RuntimeInfo      `json:"runtime,omitempty"`
	LabelMeanings      map[string]string `json:"label_meanings"`
	Gotchas            []string          `json:"gotchas"`
	NotFound           []string          `json:"not_found,omitempty"`
}

// CatalogMetric is a catalog entry expressed in the requested naming convention.
type CatalogMetric struct {
	Metric
	// DefaultName is the default-convention name when Name was converted to semconv 1.27.
	DefaultName string `json:"default_name,omitempty"`
}

const runtimeGroup = "runtime"

// QueryCatalog returns the catalog entries matching the request.
func (d *Data) QueryCatalog(req CatalogRequest) (*CatalogResponse, error) {
	resp := &CatalogResponse{
		KrakenDVersion: d.Catalog.KrakenDVersion,
		Groups:         map[string]Group{},
		Metrics:        []CatalogMetric{},
		LabelMeanings:  map[string]string{},
		Gotchas:        d.Catalog.Gotchas,
	}
	if d.Catalog.SemanticConventions != nil {
		resp.ConventionInfo = d.Catalog.SemanticConventions.Option + ". " + d.Catalog.SemanticConventions.Detect
	}

	switch req.SemanticConvention {
	case ConventionDefault, Convention127:
		resp.SemanticConvention, resp.ConventionSource = req.SemanticConvention, "request"
	case "auto", "detect":
		conv, status := d.DetectConvention(req.MetricNames)
		resp.SemanticConvention, resp.ConventionSource = conv, status
	default:
		return nil, fmt.Errorf("semantic_convention must be \"\", \"1.27\" or \"auto\", got %q", req.SemanticConvention)
	}

	groups := map[string]bool{}
	for _, g := range req.Groups {
		g = strings.ToLower(strings.TrimSpace(g))
		if _, ok := d.Catalog.Groups[g]; !ok && g != runtimeGroup {
			return nil, fmt.Errorf("unknown group %q; valid groups: %s", g, strings.Join(d.groupNames(), ", "))
		}
		groups[g] = true
	}
	query := strings.ToLower(strings.TrimSpace(req.Query))
	wanted := map[string]bool{}
	for _, n := range req.Names {
		wanted[strings.TrimSpace(n)] = true
	}
	found := map[string]bool{}

	for _, m := range d.Catalog.Metrics {
		if len(groups) > 0 && !groups[m.Group] {
			continue
		}
		if len(wanted) > 0 {
			hit := ""
			for _, n := range metricNamesOf(m) {
				if wanted[n] {
					hit = n
				}
			}
			if hit == "" {
				continue
			}
			found[hit] = true
		}
		if query != "" && !metricMatches(m, query) {
			continue
		}
		cm := CatalogMetric{Metric: m}
		if resp.SemanticConvention == Convention127 && m.Semconv127 != nil && m.Semconv127.Name != "" {
			alt := m.Semconv127
			cm.DefaultName = m.Name
			cm.OtelName, cm.Name, cm.Type, cm.Unit, cm.Series = alt.OtelName, alt.Name, alt.Type, alt.Unit, alt.Series
		}
		cm.Semconv127 = nil
		if resp.SemanticConvention == ConventionDefault {
			cm.Semconv127 = m.Semconv127 // keep the alternative visible for the default convention
		}
		resp.Metrics = append(resp.Metrics, cm)
		resp.Groups[m.Group] = d.Catalog.Groups[m.Group]
		for _, l := range m.Labels {
			if meaning, ok := d.Catalog.LabelMeanings[l]; ok {
				resp.LabelMeanings[l] = meaning
			}
		}
	}

	// runtime families are not instruments in the catalog; include them when asked for
	includeRuntime := groups[runtimeGroup] || (len(groups) == 0 && len(wanted) == 0 &&
		(query == "" || strings.Contains(strings.ToLower(strings.Join(d.Catalog.Runtime.Families, " ")), query)))
	for n := range wanted {
		for _, f := range d.Catalog.Runtime.Families {
			if n == f || strings.HasPrefix(n, f+"_") {
				found[n] = true
				includeRuntime = true
			}
		}
	}
	if includeRuntime {
		rt := d.Catalog.Runtime
		resp.Runtime = &rt
	}

	for n := range wanted {
		if !found[n] {
			resp.NotFound = append(resp.NotFound, n)
		}
	}
	sort.Strings(resp.NotFound)
	return resp, nil
}

func (d *Data) groupNames() []string {
	names := []string{runtimeGroup}
	for g := range d.Catalog.Groups {
		names = append(names, g)
	}
	sort.Strings(names)
	return names
}

// metricNamesOf lists every name a metric can be referred to by.
func metricNamesOf(m Metric) []string {
	names := append([]string{m.Name, m.OtelName}, m.Series...)
	if alt := m.Semconv127; alt != nil {
		names = append(names, alt.Name, alt.OtelName)
		names = append(names, alt.Series...)
	}
	return names
}

func metricMatches(m Metric, q string) bool {
	fields := append(metricNamesOf(m), m.Meaning, m.Notes, m.Group)
	fields = append(fields, m.Labels...)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}
