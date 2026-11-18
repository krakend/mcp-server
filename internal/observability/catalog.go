// Package observability knows which metrics a KrakenD configuration produces and how to
// query them. It backs the analyze_config_observability and get_dashboard_recipes tools.
//
// Its data (catalog.json, recipes.json) is generated from a real KrakenD run with every
// component enabled and traffic through every feature; see the generator scripts referenced
// in each file's "_about" field. Nothing in this package talks to Prometheus or Grafana.
package observability

import (
	"encoding/json"
	"fmt"
)

// Catalog describes every metric instrument KrakenD registers, as exposed by the
// OpenTelemetry Prometheus exporter.
type Catalog struct {
	KrakenDVersion      string               `json:"krakend_version"`
	SemanticConventions *SemanticConventions `json:"semantic_conventions,omitempty"`
	LabelMeanings       map[string]string    `json:"label_meanings"`
	Groups              map[string]Group     `json:"groups"`
	Metrics             []Metric             `json:"metrics"`
	Runtime             RuntimeInfo          `json:"runtime"`
	Gotchas             []string             `json:"gotchas"`
}

// SemanticConventions documents the semantic_convention option and how to detect it.
type SemanticConventions struct {
	Option         string `json:"option"`
	Detect         string `json:"detect"`
	ChangedMetrics int    `json:"changed_metrics"`
}

// Group is a set of instruments enabled by the same configuration.
type Group struct {
	Edition   string `json:"edition"`
	EnabledBy string `json:"enabled_by"`
	Lazy      string `json:"lazy,omitempty"`
}

// Metric is one instrument with its Prometheus naming.
type Metric struct {
	OtelName      string   `json:"otel_name"`
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	Unit          string   `json:"unit,omitempty"`
	Group         string   `json:"group"`
	Labels        []string `json:"labels"`
	Meaning       string   `json:"meaning"`
	EnabledBy     string   `json:"enabled_by"`
	Edition       string   `json:"edition"`
	Help          string   `json:"help,omitempty"`
	Notes         string   `json:"notes,omitempty"`
	Series        []string `json:"series,omitempty"`
	Observed      bool     `json:"observed"`
	PredictedName bool     `json:"predicted_name,omitempty"`
	// VerifiedName says where the name/type was confirmed when this catalog run didn't export it.
	VerifiedName string     `json:"verified_name,omitempty"`
	Semconv127   *MetricAlt `json:"semconv_1_27,omitempty"`
}

// MetricAlt is the same instrument under semantic convention 1.27.
type MetricAlt struct {
	OtelName string   `json:"otel_name"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Unit     string   `json:"unit,omitempty"`
	Series   []string `json:"series,omitempty"`
}

// RuntimeInfo lists the Go runtime and process metric families.
type RuntimeInfo struct {
	Meaning   string                   `json:"meaning,omitempty"`
	EnabledBy string                   `json:"enabled_by"`
	Families  []string                 `json:"families"`
	Details   map[string]RuntimeFamily `json:"details,omitempty"`
}

// RuntimeFamily is the Prometheus type and help text of one runtime metric family.
type RuntimeFamily struct {
	Type      string `json:"type"`
	Help      string `json:"help,omitempty"`
	LinuxOnly bool   `json:"linux_only,omitempty"`
}

// RecipeSet is the collection of tested PromQL recipes.
type RecipeSet struct {
	KrakenDVersion string               `json:"krakend_version"`
	Placeholders   map[string]string    `json:"placeholders"`
	SectionNotes   map[string]string    `json:"section_notes"`
	Blueprints     map[string]Blueprint `json:"blueprints,omitempty"`
	Recipes        []Recipe             `json:"recipes"`
}

// Blueprint is the layout of a dashboard about one topic (overview, runtime, backends, agents).
type Blueprint struct {
	Rows  []BlueprintRow `json:"rows"`
	Notes string         `json:"notes,omitempty"`
}

// BlueprintRow is one dashboard row: its title and recipe ids in order. Optional rows are
// included only when the config enables their recipes.
type BlueprintRow struct {
	Title    string   `json:"title"`
	Recipes  []string `json:"recipes"`
	Optional bool     `json:"optional,omitempty"`
}

// Recipe is one dashboard panel's worth of PromQL, written with the default metric names and
// the {SEL}, {BSEL} and {J} selector placeholders.
type Recipe struct {
	ID      string   `json:"id"`
	Section string   `json:"section"`
	Intents []string `json:"intents"`
	// OptionalIntents: the recipe belongs to these intents' blueprints only when the config enables
	// it (e.g. the Kafka row of an overview). Without a config, or when disabled, it's skipped silently.
	OptionalIntents  []string `json:"optional_intents,omitempty"`
	Title            string   `json:"title"`
	Panel            string   `json:"panel"`
	AltPanels        []string `json:"alt_panels,omitempty"`
	Unit             string   `json:"unit"`
	Targets          []Target `json:"targets"`
	Requires         Requires `json:"requires"`
	Notes            string   `json:"notes,omitempty"`
	MayBeEmpty       bool     `json:"may_be_empty,omitempty"`
	MayBeEmptyReason string   `json:"may_be_empty_reason,omitempty"`
	Params           []string `json:"params,omitempty"`
	// AgentNotes are instructions for the agent; they never go on the panel.
	AgentNotes string `json:"agent_notes,omitempty"`
	Stacked    bool   `json:"stacked,omitempty"`
	// Bars draws a time series as bars (counts per interval).
	Bars bool `json:"bars,omitempty"`
	// Thresholds are Grafana threshold steps ({color, value}; the first has value null).
	Thresholds []ThresholdStep `json:"thresholds,omitempty"`
}

// ThresholdStep is one Grafana threshold step. Value nil is the base step.
type ThresholdStep struct {
	Color string   `json:"color"`
	Value *float64 `json:"value"`
}

// Target is one PromQL query of a recipe.
type Target struct {
	Expr   string `json:"expr"`
	Legend string `json:"legend"`
}

// Requires lists what must exist for a recipe to return data.
type Requires struct {
	Metrics []string `json:"metrics"`
	Groups  []string `json:"groups"`
}

// Data bundles the catalog and the recipes.
type Data struct {
	Catalog *Catalog
	Recipes *RecipeSet

	semconvRules []semconvRule
	metricByName map[string]*Metric
}

// Load parses catalog.json and recipes.json.
func Load(catalogJSON, recipesJSON []byte) (*Data, error) {
	var cat Catalog
	if err := json.Unmarshal(catalogJSON, &cat); err != nil {
		return nil, fmt.Errorf("invalid observability catalog: %w", err)
	}
	var rs RecipeSet
	if err := json.Unmarshal(recipesJSON, &rs); err != nil {
		return nil, fmt.Errorf("invalid observability recipes: %w", err)
	}
	if len(cat.Metrics) == 0 || len(rs.Recipes) == 0 {
		return nil, fmt.Errorf("observability data is empty (catalog: %d metrics, recipes: %d)", len(cat.Metrics), len(rs.Recipes))
	}
	d := &Data{Catalog: &cat, Recipes: &rs, metricByName: map[string]*Metric{}}
	for i := range cat.Metrics {
		m := &cat.Metrics[i]
		d.metricByName[m.Name] = m
		for _, s := range m.Series {
			d.metricByName[s] = m
		}
	}
	d.semconvRules = buildSemconvRules(&cat)
	return d, nil
}
