package observability

import (
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Analysis is what a KrakenD configuration will expose as metrics: which catalog groups are
// enabled, the label values to expect, and what is missing for complete dashboards.
type Analysis struct {
	ConfigVersion      int                    `json:"config_version,omitempty"`
	TelemetryEnabled   bool                   `json:"telemetry_enabled"`
	SemanticConvention string                 `json:"semantic_convention"`
	Exporters          []Exporter             `json:"exporters"`
	Layers             LayerFlags             `json:"layers"`
	Groups             map[string]GroupStatus `json:"groups"`
	TimeBuckets        []float64              `json:"time_buckets,omitempty"`
	Endpoints          []EndpointInfo         `json:"endpoints"`
	AsyncAgents        []AsyncAgentInfo       `json:"async_agents,omitempty"`
	// PromptGuardRoutes are the endpoints (label values) with ai/prompt-guard: blocks return 500.
	PromptGuardRoutes []string            `json:"prompt_guard_routes,omitempty"`
	LabelValues       map[string][]string `json:"label_values"`
	Gaps              []Gap               `json:"gaps"`
	Warnings          []string            `json:"warnings"`
}

// Exporter is a telemetry/opentelemetry exporter.
type Exporter struct {
	Type           string `json:"type"` // prometheus | otlp
	Name           string `json:"name"`
	Host           string `json:"host,omitempty"`
	Port           int    `json:"port,omitempty"`
	GoMetrics      bool   `json:"go_metrics,omitempty"`
	ProcessMetrics bool   `json:"process_metrics,omitempty"`
	Disabled       bool   `json:"disabled,omitempty"`
}

// LayerFlags are the effective service-level layer settings.
type LayerFlags struct {
	Global             bool `json:"global"`
	Proxy              bool `json:"proxy"`
	Backend            bool `json:"backend"`
	RoundTrip          bool `json:"round_trip"`
	ReadPayload        bool `json:"read_payload"`
	DetailedConnection bool `json:"detailed_connection"`
}

// GroupStatus says whether a catalog group will produce metrics.
type GroupStatus struct {
	Enabled bool   `json:"enabled"`
	Partial bool   `json:"partial,omitempty"` // only some endpoints/backends (per-endpoint overrides)
	Reason  string `json:"reason"`
}

// EndpointInfo is one endpoint with the label values it produces.
type EndpointInfo struct {
	Route      string         `json:"route"`       // as written in the config
	LabelValue string         `json:"label_value"` // http_route value on global/proxy metrics
	Method     string         `json:"method"`
	Features   []string       `json:"features,omitempty"`
	RateLimit  *RateLimitInfo `json:"rate_limit,omitempty"`
	// Quota is the governance/quota name on this endpoint; over-quota requests get 429.
	Quota     string        `json:"quota,omitempty"`
	WebSocket bool          `json:"websocket,omitempty"`
	Backends  []BackendInfo `json:"backends"`
}

// AsyncAgentInfo is one async agent. Agents have no HTTP server layer: their proxy series use the
// agent name as http_route (without http_request_method), and their backend series have no
// krakend_endpoint_route / krakend_endpoint_method (verified on krakend-ee eefea5cf).
type AsyncAgentInfo struct {
	Name       string        `json:"name"`
	LabelValue string        `json:"label_value"` // http_route value on krakend_proxy_duration
	Driver     string        `json:"driver,omitempty"`
	Topic      string        `json:"topic,omitempty"`
	Backends   []BackendInfo `json:"backends"`
}

// RateLimitInfo describes how rate-limit rejections of an endpoint surface.
type RateLimitInfo struct {
	Namespaces []string `json:"namespaces"`
	Status     int      `json:"status"`
	Note       string   `json:"note"`
}

// BackendInfo is one backend (including workflow backends) with its label values.
type BackendInfo struct {
	URLPattern     string    `json:"url_pattern"`
	LabelValue     string    `json:"label_value"` // http_route value on backend/http_client metrics
	Hosts          []string  `json:"hosts,omitempty"`
	ServerAddress  string    `json:"server_address,omitempty"`
	ServerPort     string    `json:"server_port,omitempty"`
	Features       []string  `json:"features,omitempty"`
	CircuitBreaker string    `json:"circuit_breaker,omitempty"` // cb_name label value
	LLM            []LLMInfo `json:"llm,omitempty"`
	KafkaTopics    []string  `json:"kafka_topics,omitempty"`
	// WorkflowEndpoint is set on backends that run inside a workflow: their metrics report
	// this workflow endpoint (as a label value) in krakend_endpoint_route, not the parent endpoint.
	WorkflowEndpoint string `json:"workflow_endpoint,omitempty"`
}

// LLMInfo is a provider/model pair that will show up in krakend_llm_tokens_* labels.
type LLMInfo struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
}

// Gap is a configuration change that would enable more metrics.
type Gap struct {
	Option  string         `json:"option"`
	Reason  string         `json:"reason"`
	Unlocks []string       `json:"unlocks,omitempty"` // recipe ids
	Snippet map[string]any `json:"snippet,omitempty"`
}

var routeParam = regexp.MustCompile(`\{([^}]+)\}`)

// RouteLabel converts a config route or url_pattern to the value of the http_route label:
// {param} becomes :param and an empty pattern becomes "/".
func RouteLabel(route string) string {
	if route == "" {
		return "/"
	}
	return routeParam.ReplaceAllString(route, ":$1")
}

const defaultRateLimitStatus = 503

// NoTelemetryWarning tells the agent not to build dashboards for a config that exports nothing.
const NoTelemetryWarning = "STOP: this config has no telemetry/opentelemetry block, so KrakenD exports no metrics and any " +
	"dashboard would stay empty. Don't build or design one (not even against a scratch copy of the config). Tell the user, " +
	"show the extra_config.telemetry/opentelemetry gap snippet and offer to add it to their config; edit only after they " +
	"agree, then validate it, remind them to redeploy KrakenD and have Prometheus scrape the exporter port, and build " +
	"the dashboard from the updated config (Prometheus doesn't need data for that)."

// Analyze inspects a parsed KrakenD configuration.
func (d *Data) Analyze(config map[string]any) *Analysis {
	a := &Analysis{
		Groups:      map[string]GroupStatus{},
		LabelValues: map[string][]string{},
		Endpoints:   []EndpointInfo{},
		Gaps:        []Gap{},
		Warnings:    []string{},
		Exporters:   []Exporter{},
	}
	if v, ok := config["version"].(float64); ok {
		a.ConfigVersion = int(v)
	}
	root := asMap(config["extra_config"])
	tel := asMap(root["telemetry/opentelemetry"])
	a.TelemetryEnabled = tel != nil
	labels := newLabelSet()

	// ---- service-level telemetry
	if tel != nil {
		exp := asMap(tel["exporters"])
		for _, p := range asSlice(exp["prometheus"]) {
			pm := asMap(p)
			port := asInt(pm["port"])
			if port == 0 {
				port = 9090
			}
			a.Exporters = append(a.Exporters, Exporter{
				Type: "prometheus", Name: asString(pm["name"]), Port: port,
				GoMetrics: asBool(pm["go_metrics"]), ProcessMetrics: asBool(pm["process_metrics"]),
				Disabled: asBool(pm["disable_metrics"]),
			})
		}
		for _, o := range asSlice(exp["otlp"]) {
			om := asMap(o)
			a.Exporters = append(a.Exporters, Exporter{
				Type: "otlp", Name: asString(om["name"]), Host: asString(om["host"]), Port: asInt(om["port"]),
				Disabled: asBool(om["disable_metrics"]),
			})
		}
		layers := asMap(tel["layers"])
		global := asMap(layers["global"])
		bm, hasBackendMetrics := asMap(layers["backend"])["metrics"].(map[string]any)
		a.Layers = LayerFlags{
			Global:  !asBool(global["disable_metrics"]),
			Proxy:   !asBool(asMap(layers["proxy"])["disable_metrics"]),
			Backend: !asBool(bm["disable_stage"]),
			// Without a layers.backend.metrics object KrakenD enables every backend option.
			// Once the object exists, an option it doesn't set is off (verified on krakend-ee
			// eefea5cf: no layers / layers.global only / layers.backend.traces only -> all on;
			// layers.backend.metrics {} -> all off).
			RoundTrip:          !hasBackendMetrics || asBool(bm["round_trip"]),
			ReadPayload:        !hasBackendMetrics || asBool(bm["read_payload"]),
			DetailedConnection: !hasBackendMetrics || asBool(bm["detailed_connection"]),
		}
		a.SemanticConvention = asString(global["semantic_convention"])
		for _, b := range asSlice(asMap(tel["histograms"])["time_buckets"]) {
			if f, ok := b.(float64); ok {
				a.TimeBuckets = append(a.TimeBuckets, f)
			}
		}
	}

	// ---- endpoints and backends
	rootHosts := asStrings(config["host"])
	providers := aiProviders(root)
	var overrides layerOverrides
	debugEndpoints := asBool(config["debug_endpoint"]) || asBool(config["echo_endpoint"])
	serviceRateLimit := nsPresent(root, "qos/ratelimit/service", "qos/ratelimit/service/redis")
	var unnamedCB, amqpOrPubsub, guards, wildcards, seqPlaceholders int
	var promptGuardEndpoints []string

	for _, e := range asSlice(config["endpoints"]) {
		em := asMap(e)
		route := asString(em["endpoint"])
		method := strings.ToUpper(asString(em["method"]))
		if method == "" {
			method = "GET"
		}
		eec := asMap(em["extra_config"])
		ep := EndpointInfo{Route: route, LabelValue: RouteLabel(route), Method: method,
			Features: sortedKeys(eec), Backends: []BackendInfo{}}
		if strings.Contains(route, "*") {
			wildcards++
		}
		labels.add("http_route@global", ep.LabelValue)
		labels.add("http_request_method", method)

		if ns := presentNamespaces(eec, "qos/ratelimit/router", "qos/ratelimit/router/redis", "qos/ratelimit/tiered"); len(ns) > 0 || serviceRateLimit {
			if serviceRateLimit {
				ns = append(ns, "qos/ratelimit/service")
			}
			ep.RateLimit = &RateLimitInfo{Namespaces: ns, Status: defaultRateLimitStatus,
				Note: "No dedicated rate-limit metric: rejections are http_server_duration_count with this status on this route (observed 503 on KrakenD EE 3.0.x)."}
		}
		if q := asMap(eec["governance/quota"]); q != nil {
			ep.Quota = asString(q["quota_name"])
			if ep.Quota == "" {
				ep.Quota = "unnamed"
			}
		}
		if ws := asMap(eec["websocket"]); ws != nil {
			ep.WebSocket = !asBool(ws["disable_otel_metrics"])
		}
		if _, ok := eec["ai/prompt-guard"]; ok {
			guards++
			promptGuardEndpoints = append(promptGuardEndpoints, ep.LabelValue)
		}
		overrides.collect(asMap(eec["telemetry/opentelemetry"]))

		for _, b := range asSlice(em["backend"]) {
			ep.Backends = append(ep.Backends, d.analyzeBackend(asMap(b), rootHosts, ep, labels, &overrides, providers)...)
		}
		{
			for _, b := range ep.Backends {
				if b.CircuitBreaker == "unnamed" {
					unnamedCB++
				}
				for _, f := range b.Features {
					if strings.HasPrefix(f, "backend/amqp/") || f == "backend/pubsub/publisher" || f == "backend/pubsub/subscriber" {
						amqpOrPubsub++
					}
					if f == "ai/prompt-guard" {
						guards++
					}
				}
				if strings.Contains(b.URLPattern, "{resp") {
					seqPlaceholders++
				}
			}
		}
		a.Endpoints = append(a.Endpoints, ep)
	}
	labels.add("http_route@global", "404 Not Found")
	if debugEndpoints {
		labels.add("http_route@global", "200 OK")
	}

	// ---- async agents
	for _, ag := range asSlice(config["async_agent"]) {
		am := asMap(ag)
		aec := asMap(am["extra_config"])
		info := AsyncAgentInfo{Name: asString(am["name"]), Topic: asString(asMap(am["consumer"])["topic"]),
			Backends: []BackendInfo{}}
		info.LabelValue = info.Name
		for _, k := range sortedKeys(aec) {
			if strings.HasPrefix(k, "async/") {
				info.Driver = k
			}
		}
		labels.add("http_route@agent", info.LabelValue)
		if info.Driver == "async/kafka" && info.Topic != "" {
			labels.add("messaging_topic", info.Topic)
			labels.add("messaging_origin", "asyncagent")
		}
		overrides.collect(asMap(aec["telemetry/opentelemetry"]))
		for _, b := range asSlice(am["backend"]) {
			info.Backends = append(info.Backends, d.analyzeBackend(asMap(b), rootHosts, EndpointInfo{}, labels, &overrides, providers)...)
		}
		a.AsyncAgents = append(a.AsyncAgents, info)
	}

	// ---- redis pools
	redisPools := 0
	if r := asMap(root["redis"]); r != nil {
		for _, p := range asSlice(r["connection_pools"]) {
			pm := asMap(p)
			if !asBool(asMap(pm["opentelemetry"])["disable_metrics"]) {
				redisPools++
				labels.add("pool_name", asString(pm["address"]))
			}
		}
		for _, c := range asSlice(r["clusters"]) {
			cm := asMap(c)
			if !asBool(asMap(cm["opentelemetry"])["disable_metrics"]) {
				redisPools++
				for _, addr := range asStrings(cm["addresses"]) {
					labels.add("pool_name", addr)
				}
			}
		}
	}

	// ---- groups
	prometheusOn := false
	runtimeOn := false
	for _, ex := range a.Exporters {
		if !ex.Disabled {
			if ex.Type == "prometheus" {
				prometheusOn = true
			}
			runtimeOn = runtimeOn || ex.GoMetrics || ex.ProcessMetrics
		}
	}
	set := func(group string, on bool, partial bool, reason string) {
		if !a.TelemetryEnabled {
			// Without telemetry nothing is exported: that is the cause, whatever the feature config says.
			if on || partial {
				reason = "no telemetry/opentelemetry in the config (the feature is configured: " + reason + ")"
			} else {
				reason = "no telemetry/opentelemetry in the config; the feature also needs " + reason
			}
		}
		a.Groups[group] = GroupStatus{Enabled: a.TelemetryEnabled && (on || partial), Partial: !on && partial, Reason: reason}
	}
	set("global", a.Layers.Global, overrides.global, "layers.global.disable_metrics")
	set("proxy", a.Layers.Proxy, overrides.proxy, "layers.proxy.disable_metrics")
	set("backend", a.Layers.Backend, overrides.stage, "layers.backend.metrics.disable_stage")
	set("backend-roundtrip", a.Layers.RoundTrip, overrides.roundTrip, "layers.backend.metrics.round_trip")
	set("backend-payload", a.Layers.ReadPayload, overrides.readPayload, "layers.backend.metrics.read_payload")
	set("backend-connection", a.Layers.DetailedConnection, overrides.detailedConn, "layers.backend.metrics.detailed_connection")
	set("circuit-breaker", len(labels.values("cb_name")) > 0, false, "qos/circuit-breaker on a backend")
	set("redis-pool", redisPools > 0, false, "redis.connection_pools / redis.clusters")
	set("messaging", len(labels.values("messaging_topic")) > 0, false, "backend/pubsub/*/kafka backends")
	set("websocket", len(labels.values("url_path")) > 0 || anyWebSocket(a.Endpoints), false, "websocket endpoints")
	set("ai", len(labels.values("provider")) > 0 || guards > 0, false, "ai/llm, ai/router or ai/prompt-guard")
	set("runtime", runtimeOn, false, "exporters.prometheus[].go_metrics / process_metrics")
	a.Groups["scrape"] = GroupStatus{Enabled: a.TelemetryEnabled, Reason: "Prometheus 'up' series"}
	a.LabelValues = labels.all()

	d.addGaps(a, prometheusOn, runtimeOn)
	switch {
	case !a.TelemetryEnabled:
		a.Warnings = append([]string{NoTelemetryWarning}, a.Warnings...)
	case !prometheusOn:
		a.Warnings = append([]string{"No enabled Prometheus exporter: metrics only go out via OTLP. Dashboards still work " +
			"if that OTLP store translates names like Prometheus does (Mimir, Prometheus OTLP receiver...). Mention the " +
			"exporters.prometheus gap only if the user scrapes KrakenD with Prometheus."}, a.Warnings...)
	}

	// ---- warnings
	serviceOn := map[string]bool{"round_trip": a.Layers.RoundTrip, "read_payload": a.Layers.ReadPayload,
		"detailed_connection": a.Layers.DetailedConnection}
	var offForSome []string
	for _, k := range []string{"round_trip", "read_payload", "detailed_connection"} {
		if overrides.omitted[k] && serviceOn[k] {
			offForSome = append(offForSome, k)
		}
	}
	if len(a.AsyncAgents) > 0 {
		a.Warnings = append(a.Warnings, "Async agents: their proxy series use the agent name as http_route and have no "+
			"http_request_method; their backend series have no krakend_endpoint_route. Selecting one endpoint in "+
			"$endpoint hides them: use the agents.* recipes (they select agents with http_request_method=\"\" / "+
			"krakend_endpoint_route=\"\") in their own row. Agents produce no http_server_* metrics.")
	}
	if len(offForSome) > 0 {
		a.Warnings = append(a.Warnings, "Some backends override telemetry/opentelemetry backend.metrics without "+
			strings.Join(offForSome, ", ")+": those options are off for those backends only, so their series are missing from the related panels.")
	}
	if a.SemanticConvention == Convention127 {
		a.Warnings = append(a.Warnings, "semantic_convention is 1.27: http_server_* and http_client_* metrics use the 1.27 names (e.g. http_server_request_duration_seconds). get_dashboard_recipes converts recipes automatically.")
	}
	if len(a.TimeBuckets) > 0 {
		a.Warnings = append(a.Warnings, "Custom histograms.time_buckets: histogram le values (and therefore SLO thresholds) follow time_buckets, not KrakenD defaults.")
	}
	cbUses := map[string][]string{}
	for _, ep := range a.Endpoints {
		for _, b := range ep.Backends {
			if b.CircuitBreaker != "" && b.CircuitBreaker != "unnamed" {
				cbUses[b.CircuitBreaker] = append(cbUses[b.CircuitBreaker], ep.Route+" -> "+b.URLPattern)
			}
		}
	}
	cbNames := make([]string, 0, len(cbUses))
	for name := range cbUses {
		cbNames = append(cbNames, name)
	}
	sort.Strings(cbNames)
	for _, name := range cbNames {
		if uses := cbUses[name]; len(uses) > 1 {
			a.Warnings = append(a.Warnings, "Circuit breaker name \""+name+"\" is used by "+strconv.Itoa(len(uses))+" backends ("+
				strings.Join(uses, ", ")+"). They are separate breakers (each trips on its own) reported under one cb_name, "+
				"so their krakend_cb_* series add up and panels can't tell which one tripped. Suggest giving each "+
				"qos/circuit-breaker its own name in the reply; until then breaker panels show them combined.")
		}
	}
	if unnamedCB > 1 {
		a.Warnings = append(a.Warnings, "Several circuit breakers have no name: they all report cb_name=\"unnamed\" and can't be told apart. Set qos/circuit-breaker.name.")
	}
	if amqpOrPubsub > 0 {
		a.Warnings = append(a.Warnings, "AMQP and generic pubsub (NATS, SQS, ...) backends emit no messaging_* metrics; only their endpoints' HTTP-layer metrics. Only Kafka backends are instrumented.")
	}
	a.PromptGuardRoutes = promptGuardEndpoints
	if guards > 0 {
		a.Warnings = append(a.Warnings, "Prompt guard blocks return HTTP 500, so they count as 5xx in http_server_duration. Use krakend_promptguard_check_requests_total{krakend_promptguard_blocked=\"true\"} to separate them. Guarded endpoints: "+strings.Join(promptGuardEndpoints, ", "))
	}
	if debugEndpoints {
		a.Warnings = append(a.Warnings, "debug_endpoint/echo_endpoint are enabled: calls to /__debug/ and /__echo/ are counted with http_route=\"200 OK\". Exclude http_route=~\"[0-9]{3} .*\" from per-endpoint views.")
	}
	if wildcards > 0 {
		a.Warnings = append(a.Warnings, "Wildcard endpoints: the http_route label value for wildcard routes is not verified; confirm with list_prometheus_label_values.")
	}
	if seqPlaceholders > 0 {
		a.Warnings = append(a.Warnings, "Sequential-proxy placeholders ({resp0_...}) in url_pattern: the backend http_route label is expected to keep the :resp0_... pattern; not verified.")
	}
	if a.Groups["websocket"].Enabled {
		a.Warnings = append(a.Warnings, "WebSocket server metrics carry no labels: they can't be split per endpoint.")
	}
	return a
}

func anyWebSocket(eps []EndpointInfo) bool {
	for _, e := range eps {
		if e.WebSocket {
			return true
		}
	}
	return false
}

// analyzeBackend returns the backend followed by its workflow backends (flattened, so the
// output has no recursive types: MCP output schemas can't describe cycles).
func (d *Data) analyzeBackend(bm map[string]any, rootHosts []string, ep EndpointInfo, labels *labelSet, ov *layerOverrides, providers map[string]LLMInfo) []BackendInfo {
	bec := asMap(bm["extra_config"])
	hosts := asStrings(bm["host"])
	if len(hosts) == 0 {
		hosts = rootHosts
	}
	b := BackendInfo{
		URLPattern: asString(bm["url_pattern"]),
		Hosts:      hosts,
		Features:   sortedKeys(bec),
	}
	b.LabelValue = RouteLabel(b.URLPattern)
	ov.collect(asMap(bec["telemetry/opentelemetry"]))

	httpLike := true
	for _, f := range b.Features {
		if strings.HasPrefix(f, "backend/pubsub/") || strings.HasPrefix(f, "backend/amqp/") ||
			f == "backend/lambda" || f == "backend/static-filesystem" {
			httpLike = false
		}
	}
	labels.add("http_route@backend", b.LabelValue)
	if ep.LabelValue != "" { // async agent backends have no krakend_endpoint_route
		labels.add("krakend_endpoint_route", ep.LabelValue)
	}
	if httpLike && len(hosts) > 0 && !ep.WebSocket {
		if u, err := url.Parse(hosts[0]); err == nil && u.Hostname() != "" {
			b.ServerAddress = u.Hostname()
			b.ServerPort = u.Port()
			if b.ServerPort == "" {
				if u.Scheme == "https" {
					b.ServerPort = "443"
				} else {
					b.ServerPort = "80"
				}
			}
			labels.add("server_address", b.ServerAddress)
			labels.add("server_port", b.ServerPort)
		}
	}
	if ep.WebSocket && len(hosts) > 0 {
		labels.add("url_path", strings.TrimRight(hosts[0], "/")+"/"+strings.TrimLeft(b.URLPattern, "/"))
	}
	for _, ns := range []string{"qos/circuit-breaker", "qos/circuit-breaker/http"} {
		if cb := asMap(bec[ns]); cb != nil {
			name := asString(cb["name"])
			if name == "" {
				name = "unnamed"
			}
			b.CircuitBreaker = name
			labels.add("cb_name", name)
		}
	}
	if llm := asMap(bec["ai/llm"]); llm != nil {
		for _, vendor := range []string{"openai", "anthropic", "gemini", "mistral", "bedrock", "alibaba"} {
			if vm := asMap(llm[vendor]); vm != nil {
				for _, ver := range vm {
					model := asString(asMap(asMap(ver)["variables"])["model"])
					b.LLM = append(b.LLM, LLMInfo{Provider: vendor, Model: model})
				}
			}
		}
		if p := asString(llm["provider"]); p != "" {
			b.LLM = append(b.LLM, providerByName(providers, p))
		}
	}
	if r := asMap(bec["ai/router"]); r != nil {
		names := []string{asString(r["failover"])}
		for _, rt := range asSlice(r["routes"]) {
			rm := asMap(rt)
			names = append(names, asString(rm["provider"]))
			names = append(names, asStrings(rm["providers"])...)
		}
		seen := map[string]bool{}
		for _, n := range names {
			if n != "" && !seen[n] {
				seen[n] = true
				b.LLM = append(b.LLM, providerByName(providers, n))
			}
		}
	}
	for _, l := range b.LLM {
		labels.add("provider", l.Provider)
		labels.add("model", l.Model)
	}
	if k := asMap(bec["backend/pubsub/publisher/kafka"]); k != nil {
		if t := asString(asMap(k["writer"])["topic"]); t != "" {
			b.KafkaTopics = append(b.KafkaTopics, t)
		}
	}
	if k := asMap(bec["backend/pubsub/subscriber/kafka"]); k != nil {
		b.KafkaTopics = append(b.KafkaTopics, asStrings(asMap(k["reader"])["topics"])...)
	}
	for _, t := range b.KafkaTopics {
		labels.add("messaging_topic", t)
	}
	if wf := asMap(bec["workflow"]); wf != nil {
		// workflow backends report the workflow's own endpoint as krakend_endpoint_route
		// (verified: /workflow/{id} with workflow endpoint /wf/{id} -> krakend_endpoint_route="/wf/:id")
		wfRoute := asString(wf["endpoint"])
		wep := EndpointInfo{Route: wfRoute, LabelValue: RouteLabel(wfRoute)}
		var nested []BackendInfo
		for _, wb := range asSlice(wf["backend"]) {
			for _, n := range d.analyzeBackend(asMap(wb), rootHosts, wep, labels, ov, providers) {
				if n.WorkflowEndpoint == "" {
					n.WorkflowEndpoint = wep.LabelValue
				}
				nested = append(nested, n)
			}
		}
		return append([]BackendInfo{b}, nested...)
	}
	return []BackendInfo{b}
}

// providerByName resolves an ai/providers entry name to its vendor and model. Unknown names
// are returned as-is so they still show up in the analysis.
func providerByName(providers map[string]LLMInfo, name string) LLMInfo {
	if p, ok := providers[name]; ok {
		return p
	}
	return LLMInfo{Provider: name}
}

// aiProviders indexes the service-level ai/providers inventory by name.
func aiProviders(root map[string]any) map[string]LLMInfo {
	out := map[string]LLMInfo{}
	for _, p := range asSlice(asMap(root["ai/providers"])["providers"]) {
		pm := asMap(p)
		if name := asString(pm["name"]); name != "" {
			out[name] = LLMInfo{Provider: asString(pm["provider"]), Model: asString(pm["model"])}
		}
	}
	return out
}

// layerOverrides records per-endpoint/backend telemetry/opentelemetry overrides that turn
// metrics on even when the service-level layers keep them off.
type layerOverrides struct {
	global, proxy, stage, roundTrip, readPayload, detailedConn bool
	// options some override's backend.metrics block leaves out (they are off for those backends)
	omitted map[string]bool
}

func (o *layerOverrides) collect(t map[string]any) {
	if t == nil {
		return
	}
	if g := asMap(t["global"]); g != nil && !asBool(g["disable_metrics"]) {
		o.global = true
	}
	if p := asMap(t["proxy"]); p != nil && !asBool(p["disable_metrics"]) {
		o.proxy = true
	}
	if m := asMap(asMap(t["backend"])["metrics"]); m != nil {
		o.stage = o.stage || !asBool(m["disable_stage"])
		o.roundTrip = o.roundTrip || asBool(m["round_trip"])
		o.readPayload = o.readPayload || asBool(m["read_payload"])
		o.detailedConn = o.detailedConn || asBool(m["detailed_connection"])
		for _, k := range []string{"round_trip", "read_payload", "detailed_connection"} {
			if !asBool(m[k]) {
				if o.omitted == nil {
					o.omitted = map[string]bool{}
				}
				o.omitted[k] = true
			}
		}
	}
}

func (d *Data) addGaps(a *Analysis, prometheusOn, runtimeOn bool) {
	unlocks := func(group string) []string {
		var ids []string
		for _, r := range d.Recipes.Recipes {
			for _, g := range r.Requires.Groups {
				if g == group {
					ids = append(ids, r.ID)
					break
				}
			}
		}
		return ids
	}
	// The snippet must keep the options that are already on: once layers.backend.metrics
	// exists, every option it leaves out is off.
	backendMetrics := func(key string) map[string]any {
		m := map[string]any{key: true}
		for k, on := range map[string]bool{"round_trip": a.Layers.RoundTrip, "read_payload": a.Layers.ReadPayload,
			"detailed_connection": a.Layers.DetailedConnection} {
			if on {
				m[k] = true
			}
		}
		if !a.Layers.Backend {
			m["disable_stage"] = true
		}
		return map[string]any{"telemetry/opentelemetry": map[string]any{"layers": map[string]any{
			"backend": map[string]any{"metrics": m}}}}
	}
	if !a.TelemetryEnabled {
		a.Gaps = append(a.Gaps, Gap{
			Option: "extra_config.telemetry/opentelemetry",
			Reason: "No OpenTelemetry configuration: KrakenD exposes no metrics at all.",
			Snippet: map[string]any{"telemetry/opentelemetry": map[string]any{
				"service_name": "krakend",
				"exporters": map[string]any{"prometheus": []any{map[string]any{
					"name": "prometheus", "port": 9090, "go_metrics": true, "process_metrics": true}}},
				"layers": map[string]any{"backend": map[string]any{"metrics": map[string]any{
					"round_trip": true, "read_payload": true, "detailed_connection": true}}},
			}},
		})
		return
	}
	if !prometheusOn {
		a.Gaps = append(a.Gaps, Gap{
			Option: "telemetry/opentelemetry.exporters.prometheus",
			Reason: "No enabled Prometheus exporter. Metrics only go out via OTLP; recipes assume a Prometheus-compatible store (Mimir, Prometheus with OTLP receiver, ...) where names follow the same translation.",
		})
	}
	if g := a.Groups["backend-roundtrip"]; !g.Enabled {
		a.Gaps = append(a.Gaps, Gap{Option: "layers.backend.metrics.round_trip",
			Reason:  "Without it there are no backend status codes, server_address, timeouts or transport failures.",
			Unlocks: unlocks("backend-roundtrip"), Snippet: backendMetrics("round_trip")})
	}
	if g := a.Groups["backend-payload"]; !g.Enabled {
		a.Gaps = append(a.Gaps, Gap{Option: "layers.backend.metrics.read_payload",
			Reason:  "Without it there are no backend body size/read time metrics.",
			Unlocks: unlocks("backend-payload"), Snippet: backendMetrics("read_payload")})
	}
	if g := a.Groups["backend-connection"]; !g.Enabled {
		a.Gaps = append(a.Gaps, Gap{Option: "layers.backend.metrics.detailed_connection",
			Reason:  "Without it there are no DNS, connection pool and TLS timings.",
			Unlocks: unlocks("backend-connection"), Snippet: backendMetrics("detailed_connection")})
	}
	if !runtimeOn {
		a.Gaps = append(a.Gaps, Gap{Option: "exporters.prometheus[].go_metrics / process_metrics",
			Reason:  "No Go runtime / process metrics (memory, goroutines, CPU).",
			Unlocks: unlocks("runtime")})
	}
	for _, g := range []string{"global", "proxy", "backend"} {
		if !a.Groups[g].Enabled {
			a.Gaps = append(a.Gaps, Gap{Option: a.Groups[g].Reason,
				Reason: "Layer metrics disabled.", Unlocks: unlocks(g)})
		}
	}
}

// ---- label set

type labelSet struct{ m map[string]map[string]bool }

func newLabelSet() *labelSet { return &labelSet{m: map[string]map[string]bool{}} }

func (l *labelSet) add(label, value string) {
	if value == "" {
		return
	}
	if l.m[label] == nil {
		l.m[label] = map[string]bool{}
	}
	l.m[label][value] = true
}

func (l *labelSet) values(label string) []string { return sortedKeysBool(l.m[label]) }

func (l *labelSet) all() map[string][]string {
	out := make(map[string][]string, len(l.m))
	for k, v := range l.m {
		out[k] = sortedKeysBool(v)
	}
	return out
}

// ---- generic JSON helpers

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asSlice(v any) []any {
	s, _ := v.([]any)
	return s
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func asInt(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func asStrings(v any) []string {
	var out []string
	for _, x := range asSlice(v) {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedKeysBool(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func nsPresent(m map[string]any, ns ...string) bool { return len(presentNamespaces(m, ns...)) > 0 }

func presentNamespaces(m map[string]any, ns ...string) []string {
	var out []string
	for _, n := range ns {
		if _, ok := m[n]; ok {
			out = append(out, n)
		}
	}
	return out
}
