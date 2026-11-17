# KrakenD PromQL recipes

Tested against krakend-ee 3.0.1 + Prometheus. Each recipe uses the standard dashboard
variables from `dashboard-conventions.md`:

- `$job` / `$instance` - scrape labels (always present)
- `$endpoint` - KrakenD endpoint (`http_route` on global/proxy, `krakend_endpoint_route` on backend)
- `$backend_host` - backend host (`server_address`, http_client_* only)

Selectors are written as `{SEL}` (global/proxy) and `{BSEL}` (backend) placeholders:

```
SEL  = job=~"$job", instance=~"$instance", http_route=~"$endpoint"
BSEL = job=~"$job", instance=~"$instance", krakend_endpoint_route=~"$endpoint", krakend_endpoint_route!=""
```

The `!=""` keeps async agent backend calls (no `krakend_endpoint_route`) out of endpoint panels
when `$endpoint` is All. For the same reason, endpoint recipes on `krakend_proxy_*` add
`http_request_method!=""` (agent proxy series have no method).

Expand them literally into each query. If the user has no `app` label, do NOT add one.

**Naming convention.** All recipes use the default metric names. With
`layers.global.semantic_convention: "1.27"`, nine families are renamed (e.g. `http_server_duration` →
`http_server_request_duration_seconds`, `http_client_request_size_total` (counter) →
`http_client_request_body_size_bytes` (histogram, use `_sum`)). Don't hand-edit: run
`scripts/convert_semconv.py` on the finished dashboard, or `--expr` on a single query. Labels and
buckets are the same in both conventions.

Recipe IDs are stable. Cite them when you explain a panel ("p95 latency, recipe `latency.p95_by_endpoint`").

---

## Traffic

| id | title | panel | query | unit |
|---|---|---|---|---|
| `traffic.rps_total` | Requests / s | stat / timeseries | `sum(rate(http_server_duration_count{SEL}[$__rate_interval]))` | reqps |
| `traffic.rps_by_endpoint` | Requests / s by endpoint | timeseries | `sum by (http_route) (rate(http_server_duration_count{SEL, http_route!="404 Not Found"}[$__rate_interval]))` | reqps |
| `traffic.rps_by_status` | Requests / s by status | timeseries (stacked) | `sum by (http_response_status_code) (rate(http_server_duration_count{SEL}[$__rate_interval]))` (all requests, including unknown paths answered with 404 Not Found; the error ratios and latency panels leave those out) | reqps |
| `traffic.requests_in_range` | Requests | stat | `sum(increase(http_server_duration_count{SEL}[$__range]))` (all requests, including unknown paths answered with 404 Not Found; the error ratios and latency panels leave those out) | short |
| `traffic.top_endpoints` | Busiest endpoints | bargauge / table | `topk(10, sum by (http_route) (increase(http_server_duration_count{SEL, http_route!="404 Not Found"}[$__range])))` | short |
| `traffic.not_found_rate` | 404 Not Found / s | stat / timeseries | `sum(rate(http_server_duration_count{job=~"$job", instance=~"$instance", http_route="404 Not Found"}[$__rate_interval]))` | reqps |
| `traffic.egress_bytes` | Bytes sent to clients | timeseries | `sum(rate(http_server_response_size_sum{SEL}[$__rate_interval]))` | Bps |
| `traffic.rps_avg_in_range` | Avg requests / s | stat | `sum(increase(http_server_duration_count{SEL}[$__range])) / $__range_s` (all requests, including unknown paths answered with 404 Not Found; the error ratios and latency panels leave those out) | reqps |
| `traffic.response_size_avg_by_endpoint` | Avg response size by endpoint | bargauge / table | `sum by (http_route) (increase(http_server_response_size_sum{SEL, http_route!="404 Not Found"}[$__range])) / sum by (http_route) (increase(http_server_response_size_count{SEL, http_route!="404 Not Found"}[$__range]))` | bytes |
| `traffic.response_size_p95` | Response size p95 | timeseries | `histogram_quantile(0.95, sum by (le) (rate(http_server_response_size_bucket{SEL}[$__rate_interval])))` | bytes |
| `traffic.response_size_heatmap` | Response size heatmap | heatmap (format: heatmap) | `sum by (le) (increase(http_server_response_size_bucket{SEL}[$__rate_interval]))` | bytes |

## Errors

| id | title | panel | query | unit |
|---|---|---|---|---|
| `errors.ratio_5xx` | 5xx ratio | stat (thresholds 1%/5%) | `(sum(rate(http_server_duration_count{SEL, http_route!="404 Not Found", http_response_status_code=~"5.."}[$__rate_interval])) or vector(0)) / sum(rate(http_server_duration_count{SEL, http_route!="404 Not Found"}[$__rate_interval]))` | percentunit |
| `errors.ratio_5xx_by_endpoint` | 5xx ratio by endpoint | timeseries / table | `(sum by (http_route) (rate(http_server_duration_count{SEL, http_route!="404 Not Found", http_response_status_code=~"5.."}[$__rate_interval])) or 0 * sum by (http_route) (rate(http_server_duration_count{SEL, http_route!="404 Not Found"}[$__rate_interval]))) / sum by (http_route) (rate(http_server_duration_count{SEL, http_route!="404 Not Found"}[$__rate_interval]))` | percentunit |
| `errors.ratio_4xx` | 4xx ratio | stat (thresholds 5%/20%) | `(sum(rate(http_server_duration_count{SEL, http_route!="404 Not Found", http_response_status_code=~"4.."}[$__rate_interval])) or vector(0)) / sum(rate(http_server_duration_count{SEL, http_route!="404 Not Found"}[$__rate_interval]))` (requests to the config's endpoints; unknown paths are counted in the "404 Not Found / s" panel) | percentunit |
| `errors.status_split_in_range` | Responses by status | piechart | `sum by (http_response_status_code) (increase(http_server_duration_count{SEL}[$__range]))` (all requests, including unknown paths answered with 404 Not Found; the error ratios and latency panels leave those out) | short |
| `errors.non2xx_by_endpoint_in_range` | Non-2xx responses by endpoint | table | `sum by (http_route, http_response_status_code) (increase(http_server_duration_count{SEL, http_route!="404 Not Found", http_response_status_code!~"2.."}[$__range]))` (which endpoint returns which error code, in the time range) | short |
| `errors.backend_failed_calls` | Failed backend calls / s | timeseries | `sum by (krakend_endpoint_route, http_route) (rate(krakend_backend_duration_count{BSEL, error="true"}[$__rate_interval])) or 0 * sum by (krakend_endpoint_route, http_route) (rate(krakend_backend_duration_count{BSEL}[$__rate_interval]))` | reqps |
| `errors.backend_failed_in_range` | Failed backend calls | stat (red if > 0) | `sum(increase(krakend_backend_duration_count{BSEL, error="true"}[$__range])) or vector(0)` | short |
| `errors.backend_status_codes` | Backend responses by status | timeseries (stacked) | `sum by (server_address, http_response_status_code) (rate(http_client_duration_count{BSEL, server_address=~"$backend_host"}[$__rate_interval]))` | reqps |
| `errors.backend_timeouts` | Backend timeouts / s | timeseries | `sum by (krakend_endpoint_route, http_route) (rate(http_client_request_timedout_count_total{BSEL}[$__rate_interval])) or 0 * sum by (krakend_endpoint_route, http_route) (rate(http_client_duration_count{BSEL, server_address=~"$backend_host"}[$__rate_interval]))` | reqps |
| `errors.backend_transport_failures` | Backend transport failures / s | timeseries | `sum by (server_address) (rate(http_client_request_failed_count_total{BSEL, server_address=~"$backend_host"}[$__rate_interval])) or 0 * sum by (server_address) (rate(http_client_duration_count{BSEL, server_address=~"$backend_host"}[$__rate_interval]))` | reqps |
| `errors.partial_responses` | Partial responses / s | timeseries | `sum by (http_route) (rate(krakend_proxy_duration_count{SEL, http_request_method!="", complete="false"}[$__rate_interval])) or 0 * sum by (http_route) (rate(krakend_proxy_duration_count{SEL, http_request_method!=""}[$__rate_interval]))` | reqps |
| `errors.proxy_errors` | Responses with a failed backend / s | timeseries | `sum by (http_route) (rate(krakend_proxy_duration_count{SEL, http_request_method!="", error="true"}[$__rate_interval])) or 0 * sum by (http_route) (rate(krakend_proxy_duration_count{SEL, http_request_method!=""}[$__rate_interval]))` (at least one backend failed; with a single backend the whole response failed) | reqps |
| `errors.backend_canceled` | Canceled backend calls / s | timeseries | `sum by (krakend_endpoint_route, server_address) (rate(http_client_request_canceled_count_total{BSEL}[$__rate_interval])) or 0 * sum by (krakend_endpoint_route, server_address) (rate(http_client_duration_count{BSEL, server_address=~"$backend_host"}[$__rate_interval]))` (the client left or the endpoint timeout fired first) | reqps |

Error recipes are written to show **0, not "No data"**, before the first error: stats use
`or vector(0)`, per-endpoint ratios fall back to `0 * <traffic>`. Keep that pattern for new error
panels. A gateway that hasn't failed yet should still get its error dashboard.

`errors.partial_responses` matters for aggregation endpoints: `complete="false"` means at least
one backend failed and the client got a degraded, merged response.

## Latency

Use `histogram_quantile` over `sum by (le, ...)`. Never average quantiles.

| id | title | panel | query | unit |
|---|---|---|---|---|
| `latency.p50/p95/p99` | Latency p50 / p95 / p99 | timeseries (3 targets) | `histogram_quantile(0.5, sum by (le) (rate(http_server_duration_bucket{SEL, http_route!="404 Not Found"}[$__rate_interval])))` and `histogram_quantile(0.95, sum by (le) (rate(http_server_duration_bucket{SEL, http_route!="404 Not Found"}[$__rate_interval])))` and `histogram_quantile(0.99, sum by (le) (rate(http_server_duration_bucket{SEL, http_route!="404 Not Found"}[$__rate_interval])))` | s |
| `latency.p95_by_endpoint` | p95 latency by endpoint | timeseries | `histogram_quantile(0.95, sum by (le, http_route) (rate(http_server_duration_bucket{SEL, http_route!="404 Not Found"}[$__rate_interval])))` | s |
| `latency.slowest_endpoints` | Slowest endpoints (p95) | bargauge / table | `topk(10, histogram_quantile(0.95, sum by (le, http_route) (rate(http_server_duration_bucket{SEL, http_route!="404 Not Found"}[$__range]))))` | s |
| `latency.avg_by_endpoint` | Avg latency by endpoint | table | `sum by (http_route) (rate(http_server_duration_sum{SEL, http_route!="404 Not Found"}[$__rate_interval])) / sum by (http_route) (rate(http_server_duration_count{SEL, http_route!="404 Not Found"}[$__rate_interval]))` | s |
| `latency.heatmap` | Latency heatmap | heatmap (format: heatmap) | `sum by (le) (increase(http_server_duration_bucket{SEL, http_route!="404 Not Found"}[$__rate_interval]))` | s |
| `latency.backend_p95` | Backend p95 by backend | timeseries | `histogram_quantile(0.95, sum by (le, krakend_endpoint_route, http_route) (rate(krakend_backend_duration_bucket{BSEL}[$__rate_interval])))` | s |
| `latency.backend_host_p95` | Backend p95 by host | timeseries | `histogram_quantile(0.95, sum by (le, server_address) (rate(http_client_duration_bucket{BSEL, server_address=~"$backend_host"}[$__rate_interval])))` | s |
| `latency.slo_under_300ms` | Requests under 300 ms | stat (thresholds 99%/95%) | `sum(rate(http_server_duration_bucket{SEL, http_route!="404 Not Found", le="0.3"}[$__rate_interval])) / sum(rate(http_server_duration_count{SEL, http_route!="404 Not Found"}[$__rate_interval]))` | percentunit |
| `latency.percentiles_in_range` | Latency p50 / p90 / p95 / p99 | stat (4 targets) (thresholds 0.3s/1s) | `histogram_quantile(0.5, sum by (le) (rate(http_server_duration_bucket{SEL, http_route!="404 Not Found"}[$__range])))` as `p50` and `histogram_quantile(0.9, sum by (le) (rate(http_server_duration_bucket{SEL, http_route!="404 Not Found"}[$__range])))` as `p90` and `histogram_quantile(0.95, sum by (le) (rate(http_server_duration_bucket{SEL, http_route!="404 Not Found"}[$__range])))` as `p95` and `histogram_quantile(0.99, sum by (le) (rate(http_server_duration_bucket{SEL, http_route!="404 Not Found"}[$__range])))` as `p99` (estimated from histogram buckets: requests faster than the first bucket, 10 ms by default, show as 5-10 ms; compare with the average) (agent: percentiles are interpolated inside histogram buckets; when most requests fall in the first bucket, ≤ 10 ms with default buckets, they read about 5 / 9 / 9.5 / 9.9 ms whatever the real latency. Compare with `latency.avg_in_range`: if the average is far below, say in the reply that latency is under the first bucket and lead with the average; per endpoint, `latency.avg_by_endpoint`) | s |
| `latency.avg_in_range` | Avg latency | stat (thresholds 0.3s/1s) | `sum(increase(http_server_duration_sum{SEL, http_route!="404 Not Found"}[$__range])) / sum(increase(http_server_duration_count{SEL, http_route!="404 Not Found"}[$__range]))` | s |
| `latency.fastest_endpoints` | Fastest endpoints (p95) | bargauge / table | `bottomk(10, histogram_quantile(0.95, sum by (le, http_route) (rate(http_server_duration_bucket{SEL, http_route!="404 Not Found"}[$__range]))))` | s |
| `latency.proxy_p95_by_endpoint` | Proxy p95 by endpoint | timeseries | `histogram_quantile(0.95, sum by (le, http_route) (rate(krakend_proxy_duration_bucket{SEL, http_request_method!=""}[$__rate_interval])))` | s |
| `latency.backend_heatmap` | Backend latency heatmap | heatmap (format: heatmap) | `sum by (le) (increase(http_client_duration_bucket{BSEL, server_address=~"$backend_host"}[$__rate_interval]))` | s |

Default `le` boundaries in 3.0.1 (seconds): 0.01, 0.02, 0.05, 0.075, 0.1, 0.125, 0.15, 0.175,
0.2, 0.25, 0.3, 0.35, 0.5, 0.75, 1.0, 1.5, 2.0, 3.5, 5.0, 10.0. An SLO threshold must be one of
them. Write the string exactly as Prometheus does (`le="1.0"`, not `le="1"`). Confirm with
`list_prometheus_label_values(labelName="le", matches=http_server_duration_bucket)`.

## "Where is the time spent?" (layer breakdown)

The signature KrakenD view: one panel, four targets, **one endpoint**. Use **averages**
(`_sum / _count`), not percentiles. Layer differences are often below a millisecond, and
histogram percentiles are interpolated inside buckets (0.2-0.25 s, ...), so every layer gets
the same p95 and the lines stack on top of each other. Averages are exact.

| id | legend | query | unit |
|---|---|---|---|
| `layers.global_avg` | Total (client view) | `sum(rate(http_server_duration_sum{SEL}[$__rate_interval])) / sum(rate(http_server_duration_count{SEL}[$__rate_interval]))` | s |
| `layers.proxy_avg` | Proxy (fan-out + merge) | `sum(rate(krakend_proxy_duration_sum{SEL, http_request_method!=""}[$__rate_interval])) / sum(rate(krakend_proxy_duration_count{SEL, http_request_method!=""}[$__rate_interval]))` | s |
| `layers.backend_avg` | Slowest backend | `max(sum by (http_route) (rate(krakend_backend_duration_sum{BSEL}[$__rate_interval])) / sum by (http_route) (rate(krakend_backend_duration_count{BSEL}[$__rate_interval])))` | s |
| `layers.roundtrip_avg` | Network round trip (slowest) | `max(sum by (http_route) (rate(http_client_duration_sum{BSEL, server_address=~"$backend_host"}[$__rate_interval])) / sum by (http_route) (rate(http_client_duration_count{BSEL, server_address=~"$backend_host"}[$__rate_interval])))` | s |
| `layers.gateway_overhead` | KrakenD overhead | `(sum(rate(http_server_duration_sum{SEL}[$__rate_interval])) / sum(rate(http_server_duration_count{SEL}[$__rate_interval]))) - (max(sum by (http_route) (rate(http_client_duration_sum{BSEL, server_address=~"$backend_host"}[$__rate_interval])) / sum by (http_route) (rate(http_client_duration_count{BSEL, server_address=~"$backend_host"}[$__rate_interval]))))` | s |

Read it as: total - proxy ≈ router work (auth, validation, rate limiting, security);
proxy - slowest backend ≈ aggregation/manipulation; backend - round trip ≈ decoding/filtering.
`max(... by (http_route))` matters. Backends of an aggregated endpoint run in parallel, so the
slowest one sets the time. A plain average across backends halves it on a 2-backend endpoint.
A `stat` panel with `layers.gateway_overhead` in ms makes a strong demo line ("KrakenD adds 0.2 ms").

**Only use this for ONE endpoint.** Each layer measures a different set of requests: 404s,
rate-limited and auth-rejected requests never reach the proxy or the backends. With
`$endpoint = All`, the proxy line can sit *above* the total line. Pin the panel to one endpoint
(`http_route="/orders"` and `krakend_endpoint_route="/orders"`), or repeat it per endpoint with
`"repeat": "endpoint"`.

For tail latency of a single layer, the p95 versions (`histogram_quantile(0.95, sum by (le) (rate(<metric>_bucket{...}[$__rate_interval])))`)
are fine. Just don't put several layers' p95 on one panel to compare them.

## Backends

| id | title | panel | query | unit |
|---|---|---|---|---|
| `backends.rps_by_host` | Backend calls / s by host | timeseries | `sum by (server_address) (rate(http_client_request_started_count_total{BSEL, server_address=~"$backend_host"}[$__rate_interval]))` | reqps |
| `backends.calls_per_endpoint` | Backend calls / s by endpoint | table | `sum by (krakend_endpoint_route, http_route) (rate(krakend_backend_duration_count{BSEL}[$__rate_interval]))` | reqps |
| `backends.fanout_ratio` | Backend calls per request | stat / table | `sum by (krakend_endpoint_route) (rate(krakend_backend_duration_count{BSEL}[$__rate_interval])) / on (krakend_endpoint_route) group_left label_replace(sum by (http_route) (rate(krakend_proxy_duration_count{job=~"$job", instance=~"$instance"}[$__rate_interval])), "krakend_endpoint_route", "$1", "http_route", "(.*)")` | short |
| `backends.conn_pool_wait_p95` | Connection pool wait p95 | timeseries | `histogram_quantile(0.95, sum by (le, server_address) (rate(http_client_request_get_conn_duration_bucket{BSEL, server_address=~"$backend_host"}[$__rate_interval])))` | s |
| `backends.dns_p95` | DNS lookup p95 | timeseries | `histogram_quantile(0.95, sum by (le, server_address) (rate(http_client_request_dns_duration_bucket{BSEL, server_address=~"$backend_host"}[$__rate_interval])))` | s |
| `backends.ingress_bytes` | Bytes read from backends | timeseries | `sum by (krakend_endpoint_route) (rate(http_client_response_read_size_b_total{BSEL}[$__rate_interval]))` | Bps |
| `backends.body_read_p95` | Body read time p95 | timeseries | `histogram_quantile(0.95, sum by (le, krakend_endpoint_route) (rate(http_client_response_read_time_hist_seconds_bucket{BSEL}[$__rate_interval])))` | s |
| `backends.rps_by_backend` | Backend calls / s by backend | timeseries | `sum by (krakend_endpoint_route, server_address, http_route) (rate(http_client_duration_count{BSEL, server_address=~"$backend_host"}[$__rate_interval]))` | reqps |
| `backends.avg_by_backend` | Avg backend latency | timeseries / table | `sum by (server_address, http_route) (rate(http_client_duration_sum{BSEL, server_address=~"$backend_host"}[$__rate_interval])) / sum by (server_address, http_route) (rate(http_client_duration_count{BSEL, server_address=~"$backend_host"}[$__rate_interval]))` | s |
| `backends.slowest` | Slowest backends (p95) | bargauge / table | `topk(10, histogram_quantile(0.95, sum by (le, server_address, http_route) (rate(http_client_duration_bucket{BSEL, server_address=~"$backend_host"}[$__range]))))` | s |
| `backends.started_vs_canceled` | Backend calls started vs canceled | timeseries | `sum(rate(http_client_request_started_count_total{BSEL, server_address=~"$backend_host"}[$__rate_interval]))` as `Started` and `sum(rate(http_client_request_canceled_count_total{BSEL}[$__rate_interval])) or vector(0)` as `Canceled` | reqps |
| `backends.connection_setup_avg` | Connection setup (avg) | timeseries | `sum(rate(http_client_request_get_conn_duration_sum{BSEL, server_address=~"$backend_host"}[$__rate_interval])) / sum(rate(http_client_request_get_conn_duration_count{BSEL, server_address=~"$backend_host"}[$__rate_interval]))` as `Get connection` and `sum(rate(http_client_request_dns_duration_sum{BSEL, server_address=~"$backend_host"}[$__rate_interval])) / sum(rate(http_client_request_dns_duration_count{BSEL, server_address=~"$backend_host"}[$__rate_interval]))` as `DNS` and `sum(rate(http_client_request_tls_duration_sum{BSEL, server_address=~"$backend_host"}[$__rate_interval])) / sum(rate(http_client_request_tls_duration_count{BSEL, server_address=~"$backend_host"}[$__rate_interval]))` as `TLS handshake` | s |
| `backends.tls_p95` | TLS handshake p95 | timeseries | `histogram_quantile(0.95, sum by (le, server_address) (rate(http_client_request_tls_duration_bucket{BSEL, server_address=~"$backend_host"}[$__rate_interval])))` | s |
| `backends.response_size_avg` | Avg backend response size | timeseries | `sum by (server_address) (rate(http_client_response_size_sum{BSEL, server_address=~"$backend_host"}[$__rate_interval])) / sum by (server_address) (rate(http_client_response_size_count{BSEL, server_address=~"$backend_host"}[$__rate_interval]))` | bytes |
| `backends.no_content_length` | Responses without Content-Length | timeseries | `sum by (server_address, http_route) (rate(http_client_response_no_content_length_total{BSEL, server_address=~"$backend_host"}[$__rate_interval]))` (chunked responses: KrakenD can't size them upfront) | reqps |

DNS/conn series only produce points when new connections are opened. An empty result
during steady traffic is normal (keep-alive). Say so in the panel description instead of
dropping the panel if the user asked for it.

## Resilience (EE)

| id | title | panel | query | unit |
|---|---|---|---|---|
| `cb.opened_in_range` | Breaker opened | stat (red if > 0) | `sum by (cb_name) (increase(krakend_cb_state_close_to_open_total{J}[$__range])) + (sum by (cb_name) (increase(krakend_cb_state_half_to_open_total{J}[$__range])) or 0 * sum by (cb_name) (increase(krakend_cb_state_close_to_open_total{J}[$__range])))` (agent: the `or 0 *` keeps it working before the first failed recovery, when half_to_open has no series yet) | short |
| `cb.failed_recoveries` | Failed breaker recoveries | stat | `sum by (cb_name) (increase(krakend_cb_state_half_to_open_total{job=~"$job", instance=~"$instance"}[$__range]))` | short |
| `cb.transitions` | Breaker state changes | timeseries (bars) | `sum by (cb_name) (round(increase(krakend_cb_state_close_to_open_total{job=~"$job", instance=~"$instance"}[$__rate_interval])))` as `{{cb_name}} closed → open (tripped)` and `sum by (cb_name) (round(increase(krakend_cb_state_open_to_half_total{job=~"$job", instance=~"$instance"}[$__rate_interval])))` as `{{cb_name}} open → half-open (probing)` and `sum by (cb_name) (round(increase(krakend_cb_state_half_to_open_total{job=~"$job", instance=~"$instance"}[$__rate_interval])))` as `{{cb_name}} half-open → open (probe failed)` and `sum by (cb_name) (round(increase(krakend_cb_state_half_to_close_total{job=~"$job", instance=~"$instance"}[$__rate_interval])))` as `{{cb_name}} half-open → closed (recovered)` (agent: `round()` because `increase()` extrapolates counts to fractions like 2.67) | short |
| `cb.transitions_in_range` | Breaker state changes in range | bargauge | `sum by (cb_name) (increase(krakend_cb_state_close_to_open_total{job=~"$job", instance=~"$instance"}[$__range]))` as `{{cb_name}} closed → open (tripped)` and `sum by (cb_name) (increase(krakend_cb_state_open_to_half_total{job=~"$job", instance=~"$instance"}[$__range]))` as `{{cb_name}} open → half-open (probing)` and `sum by (cb_name) (increase(krakend_cb_state_half_to_open_total{job=~"$job", instance=~"$instance"}[$__range]))` as `{{cb_name}} half-open → open (probe failed)` and `sum by (cb_name) (increase(krakend_cb_state_half_to_close_total{job=~"$job", instance=~"$instance"}[$__range]))` as `{{cb_name}} half-open → closed (recovered)` | short |
| `cb.consecutive_failures` | Consecutive failures | timeseries | `max by (cb_name) (krakend_cb_errors_consecutive_failures{job=~"$job", instance=~"$instance"})` | short |
| `cb.window_counts` | Breaker window: successes vs failures | timeseries | `max by (cb_name) (krakend_cb_errors_total_successes{J})` as `{{cb_name}} successes` and `max by (cb_name) (krakend_cb_errors_total_failures{J})` as `{{cb_name}} failures` Successes and failures counted in the breaker's current interval (agent: gauges, never rate() them) | short |
| `ratelimit.rejections` | Rate-limited requests / s | timeseries | `sum by (http_route) (rate(http_server_duration_count{SEL, http_response_status_code=~"$ratelimit_status"}[$__rate_interval]))` restricted to endpoints that have `qos/ratelimit/router` | reqps |

A breaker that stays down shows a single `close_to_open` and then a steady stream of
`open_to_half` + `half_to_open` (each failed probe). Counting only `close_to_open` reports
"0 trips" while the backend is fully cut off. Always include `half_to_open`.

There is no dedicated rate-limit metric. Get the list of rate-limited endpoints and their
rejection status (default 503, or `status_code` override if any) from the KrakenD config, and filter
`http_route` to those endpoints. Label the panel "Rejected by rate limit (inferred)".

## Quotas (EE)

`governance/quota` has no dedicated metric: a request over its quota gets **429**, so rejections
are `http_server_duration_count` with status 429 on the endpoints that have a quota. With the
config, the recipes are restricted to those endpoints; without it, every 429 counts.

| id | title | panel | query | unit |
|---|---|---|---|---|
| `quota.rejections` | Quota rejections / s | timeseries | `sum by (http_route) (rate(http_server_duration_count{SEL, http_response_status_code="429"}[$__rate_interval])) or 0 * sum by (http_route) (rate(http_server_duration_count{SEL}[$__rate_interval]))` | reqps |
| `quota.rejection_ratio` | Quota rejection ratio | timeseries (thresholds 5%/20%) | `(sum by (http_route) (rate(http_server_duration_count{SEL, http_response_status_code="429"}[$__rate_interval])) or 0 * sum by (http_route) (rate(http_server_duration_count{SEL}[$__rate_interval]))) / sum by (http_route) (rate(http_server_duration_count{SEL}[$__rate_interval]))` (share of requests rejected because the client used up its quota) | percentunit |
| `quota.rejected_in_range` | Quota rejections | stat (red if > 0) | `sum(increase(http_server_duration_count{SEL, http_response_status_code="429"}[$__range])) or vector(0)` | short |

## AI gateway (EE)

`J = job=~"$job", instance=~"$instance"`. These metrics have no endpoint label, except the prompt guard.

| id | title | panel | query | unit |
|---|---|---|---|---|
| `ai.tokens_rate_by_model` | Tokens / s by model | timeseries (stacked) | `sum by (provider, model) (rate(krakend_llm_tokens_total{J}[$__rate_interval]))` | short (tokens/s) |
| `ai.tokens_in_range` | Tokens | stat | `sum(increase(krakend_llm_tokens_total{J}[$__range]))` | short |
| `ai.tokens_in_vs_out` | Input vs output tokens / s | timeseries | `sum(rate(krakend_llm_tokens_in_total{J}[$__rate_interval]))` as `Input tokens` and `sum(rate(krakend_llm_tokens_out_total{J}[$__rate_interval]))` as `Output tokens` | short |
| `ai.tokens_by_model_in_range` | Tokens by model | bargauge / table | `sum by (provider, model) (increase(krakend_llm_tokens_total{J}[$__range]))` | short |
| `ai.output_ratio` | Output token share | stat | `sum(increase(krakend_llm_tokens_out_total{J}[$__range])) / sum(increase(krakend_llm_tokens_in_total{J}[$__range]))` | percentunit |
| `ai.guard_checks` | Guard checks / s | timeseries | `sum by (http_route, krakend_promptguard_blocked) (rate(krakend_promptguard_check_requests_total{J}[$__rate_interval]))` | reqps |
| `ai.guard_block_ratio` | Guard block ratio | stat (thresholds 1%/10%) | `(sum(rate(krakend_promptguard_check_requests_total{J, krakend_promptguard_blocked="true"}[$__rate_interval])) or vector(0)) / sum(rate(krakend_promptguard_check_requests_total{J}[$__rate_interval]))` | percentunit |
| `ai.guard_blocked_in_range` | Blocked by guards | stat (red if > 0) | `sum(increase(krakend_promptguard_check_requests_total{J, krakend_promptguard_blocked="true"}[$__range])) or vector(0)` | short |

Cost: tokens × price. If the user gives prices, multiply in PromQL per model
(`sum by (model) (increase(krakend_llm_tokens_in_total{J}[$__range])) * on (model) group_left ...`
is overkill). Prefer one stat per model with a constant multiplier, e.g.
`sum(increase(krakend_llm_tokens_in_total{J, model="gpt-5-nano"}[$__range])) * 0.00000005`, unit `currencyUSD`.
Blocked prompts return 500 to clients. On AI endpoints, explain that in the error panel description.

## WebSocket (EE)

| id | title | panel | query | unit |
|---|---|---|---|---|
| `ws.open_sessions` | Open WebSocket sessions | stat / timeseries | `sum(websocket_server_connections_count_b_total{J}) - sum(websocket_server_disconnections_count_b_total{J})` | short |
| `ws.connects_rate` | WebSocket connects / s | timeseries | `sum(rate(websocket_server_connections_count_b_total{J}[$__rate_interval]))` as `Connections` and `sum(rate(websocket_server_disconnections_count_b_total{J}[$__rate_interval]))` as `Disconnections` | ops |
| `ws.msgs_in_rate` | Messages from clients / s | timeseries | `sum(rate(websocket_server_request_size_hist_b_count{J}[$__rate_interval]))` (client → KrakenD) | ops |
| `ws.msgs_out_rate` | Messages to clients / s | timeseries | `sum(rate(websocket_server_response_size_hist_b_count{J}[$__rate_interval]))` (KrakenD → client) | ops |
| `ws.bytes_in_out` | WebSocket bytes in / out | timeseries | `sum(rate(websocket_server_request_size_hist_b_sum{J}[$__rate_interval]))` as `client → KrakenD` and `sum(rate(websocket_server_response_size_hist_b_sum{J}[$__rate_interval]))` as `KrakenD → client` | Bps |
| `ws.backend_connections` | Backend WebSocket connections / s | timeseries | `sum by (url_path) (rate(websocket_client_connections_count_total{J}[$__rate_interval]))` | ops |
| `ws.backend_open_sessions` | Open backend WebSocket sessions | stat / timeseries | `sum by (url_path) (websocket_client_connections_count_total{J}) - sum by (url_path) (websocket_client_disconnections_count_b_total{J})` | short |
| `ws.msg_size_p95` | Message size p95 | timeseries | `histogram_quantile(0.95, sum by (le) (rate(websocket_server_request_size_hist_b_bucket{J}[$__rate_interval])))` as `client → KrakenD` and `histogram_quantile(0.95, sum by (le) (rate(websocket_server_response_size_hist_b_bucket{J}[$__rate_interval])))` as `KrakenD → client` | bytes |
| `ws.backend_msgs_rate` | Backend messages / s | timeseries | `sum(rate(websocket_client_request_size_hist_b_count{J}[$__rate_interval]))` as `KrakenD → backend` and `sum(rate(websocket_client_response_size_hist_b_count{J}[$__rate_interval]))` as `backend → KrakenD` | ops |

`ws.open_sessions` is approximate: counters reset when KrakenD restarts, which can push the
difference below zero for a while. Server-side metrics have no endpoint label.

## Async agents (EE)

Agents consume messages (`async/amqp`, `async/kafka`) and call their backends without an HTTP
request, so they have **no `http_server_*` series**. Each message shows up as:
- `krakend_proxy_duration{http_route="<agent name>"}` **without `http_request_method`** (endpoints
  always have it): one count per message processed.
- `krakend_backend_duration` / `http_client_*` with `http_route=<backend url_pattern>` and **no
  `krakend_endpoint_route`**.

The recipes select agents with `http_request_method=""` (proxy) and `krakend_endpoint_route=""`
(backends), so they never mix with endpoints. Put them in their own row, and don't filter them with
`$endpoint` (agent names aren't endpoints). Kafka agents also emit `messaging_*` with
`messaging_origin="asyncagent"`: use `kafka.consumed_by_origin` and `kafka.ack_avg_by_origin`.

| id | title | panel | query | unit |
|---|---|---|---|---|
| `agents.messages_rate` | Agent messages / s | timeseries | `sum by (http_route) (rate(krakend_proxy_duration_count{J, http_request_method=""}[$__rate_interval]))` (messages processed per agent) | ops |
| `agents.messages_in_range` | Agent messages | stat / table | `sum by (http_route) (increase(krakend_proxy_duration_count{J, http_request_method=""}[$__range]))` | short |
| `agents.error_ratio` | Agent error ratio | timeseries / stat (thresholds 1%/5%) | `(sum by (http_route) (rate(krakend_proxy_duration_count{J, http_request_method="", error="true"}[$__rate_interval])) or 0 * sum by (http_route) (rate(krakend_proxy_duration_count{J, http_request_method=""}[$__rate_interval]))) / sum by (http_route) (rate(krakend_proxy_duration_count{J, http_request_method=""}[$__rate_interval]))` (messages whose backend call failed) | percentunit |
| `agents.processing_p95` | Agent processing p95 | timeseries | `histogram_quantile(0.95, sum by (le, http_route) (rate(krakend_proxy_duration_bucket{J, http_request_method=""}[$__rate_interval])))` | s |
| `agents.processing_avg` | Agent processing (avg) | timeseries | `sum by (http_route) (rate(krakend_proxy_duration_sum{J, http_request_method=""}[$__rate_interval])) / sum by (http_route) (rate(krakend_proxy_duration_count{J, http_request_method=""}[$__rate_interval]))` | s |
| `agents.backend_p95` | Agent backend p95 | timeseries | `histogram_quantile(0.95, sum by (le, http_route) (rate(krakend_backend_duration_bucket{J, krakend_endpoint_route=""}[$__rate_interval])))` | s |
| `agents.backend_status` | Agent backend responses by status | timeseries (stacked) | `sum by (server_address, http_route, http_response_status_code) (rate(http_client_duration_count{J, krakend_endpoint_route=""}[$__rate_interval]))` | reqps |
| `agents.backend_failures` | Agent backend failures / s | timeseries | `sum by (http_route) (rate(krakend_backend_duration_count{J, krakend_endpoint_route="", error="true"}[$__rate_interval])) or 0 * sum by (http_route) (rate(krakend_backend_duration_count{J, krakend_endpoint_route=""}[$__rate_interval]))` | reqps |

## Kafka messaging (EE)

| id | title | panel | query | unit |
|---|---|---|---|---|
| `kafka.published_rate` | Published messages / s | timeseries | `sum by (messaging_topic) (rate(messaging_write_body_duration_seconds_count{J}[$__rate_interval]))` | ops |
| `kafka.consumed_rate` | Consumed messages / s | timeseries | `sum by (messaging_topic) (rate(messaging_read_body_duration_seconds_count{J}[$__rate_interval]))` | ops |
| `kafka.publish_p95` | Publish p95 | timeseries | `histogram_quantile(0.95, sum by (le, messaging_topic) (rate(messaging_write_body_duration_seconds_bucket{J}[$__rate_interval])))` | s |
| `kafka.ack_p95` | Ack p95 | timeseries | `histogram_quantile(0.95, sum by (le, messaging_topic) (rate(messaging_read_ack_duration_seconds_bucket{J}[$__rate_interval])))` | s |
| `kafka.msg_size_avg` | Avg published message size | stat | `sum(increase(messaging_write_body_size_bytes_sum{J}[$__range])) / sum(increase(messaging_write_body_size_bytes_count{J}[$__range]))` | bytes |
| `kafka.failures` | Kafka failures | stat (red if > 0) | `sum(increase(messaging_write_failure_count_seconds_count{J}[$__range])) or vector(0)` as `Publish failures` and `sum(increase(messaging_read_failure_count_seconds_count{J}[$__range])) or vector(0)` as `Consume failures` | short |
| `kafka.failures_by_topic` | Kafka failures / s by topic | timeseries | `sum by (messaging_origin, messaging_topic) (rate(messaging_read_failure_count_seconds_count{J}[$__rate_interval]))` as `consume {{messaging_origin}} {{messaging_topic}}` and `sum by (messaging_topic) (rate(messaging_write_failure_count_seconds_count{J}[$__rate_interval]))` as `publish {{messaging_topic}}` | ops |
| `kafka.failure_avg_duration` | Time to fail (avg) by topic | timeseries | `sum by (messaging_topic) (rate(messaging_write_failure_count_seconds_sum{J}[$__rate_interval])) / sum by (messaging_topic) (rate(messaging_write_failure_count_seconds_count{J}[$__rate_interval]))` as `publish {{messaging_topic}}` and `sum by (messaging_origin, messaging_topic) (rate(messaging_read_failure_count_seconds_sum{J}[$__rate_interval])) / sum by (messaging_origin, messaging_topic) (rate(messaging_read_failure_count_seconds_count{J}[$__rate_interval]))` as `consume {{messaging_origin}} {{messaging_topic}}` (how long an operation ran before failing: close to the client timeout means the broker is unreachable or slow, near zero means it rejected the message) | s |
| `kafka.consumed_by_origin` | Consumed messages / s by origin | timeseries | `sum by (messaging_origin, messaging_topic) (rate(messaging_read_body_duration_seconds_count{J}[$__rate_interval]))` (`subscriber` = endpoint backends, `asyncagent` = async agents) | ops |
| `kafka.publish_avg` | Publish (avg) | timeseries | `sum by (messaging_topic) (rate(messaging_write_body_duration_seconds_sum{J}[$__rate_interval])) / sum by (messaging_topic) (rate(messaging_write_body_duration_seconds_count{J}[$__rate_interval]))` | s |
| `kafka.ack_avg_by_origin` | Ack (avg) by origin | timeseries | `sum by (messaging_origin, messaging_topic) (rate(messaging_read_ack_duration_seconds_sum{J}[$__rate_interval])) / sum by (messaging_origin, messaging_topic) (rate(messaging_read_ack_duration_seconds_count{J}[$__rate_interval]))` (time processing a message before acking it) | s |
| `kafka.publish_heatmap` | Publish latency heatmap | heatmap (format: heatmap) | `sum by (le) (increase(messaging_write_body_duration_seconds_bucket{J}[$__rate_interval]))` | s |
| `kafka.consumed_msg_size_avg` | Avg consumed message size | stat | `sum(increase(messaging_read_body_size_bytes_sum{J}[$__range])) / sum(increase(messaging_read_body_size_bytes_count{J}[$__range]))` | bytes |

Failures are **histograms** (`messaging_*_failure_count_seconds`: how long the failing call took);
count them with `_count`. They only appear after the first failure.

Only Kafka backends emit these. NATS/SQS pubsub and AMQP endpoints are only visible through the
HTTP layer metrics of their endpoints.

## Redis connection pools (EE)

Used by Redis rate limits, quotas and semantic cache. `pool_name` is the Redis address.

| id | title | panel | query | unit |
|---|---|---|---|---|
| `redis.pool_saturation` | Redis pool usage | gauge (thresholds 70%/90%) | `max by (pool_name) (db_client_connections_usage{J, state="used"}) / max by (pool_name) (db_client_connections_max{J})` | percentunit |
| `redis.conns_by_state` | Redis connections by state | timeseries (stacked) | `sum by (pool_name, state) (db_client_connections_usage{J})` | short |
| `redis.cmd_p95` | Redis command p95 | timeseries | `histogram_quantile(0.95, sum by (le, pool_name, type) (rate(db_client_connections_use_time_milliseconds_bucket{J}[$__rate_interval])))` | ms |
| `redis.cmd_errors` | Redis command errors / s | timeseries | `sum by (pool_name) (rate(db_client_connections_use_time_milliseconds_count{J, status="error"}[$__rate_interval]))` | ops |
| `redis.timeouts` | Redis pool timeouts | stat (red if > 0) | `sum(delta(db_client_connections_timeouts{J}[$__range]))` | short |
| `redis.waits` | Redis pool waits / s | timeseries | `sum by (pool_name) (deriv(db_client_connections_waits{J}[5m]))` | ops |
| `redis.pool_config` | Redis pool settings | table | `max by (pool_name) (db_client_connections_max{J})` as `max` and `max by (pool_name) (db_client_connections_idle_min{J})` as `idle min` and `max by (pool_name) (db_client_connections_idle_max{J})` as `idle max` | short |
| `redis.cmd_avg` | Redis command (avg) | timeseries | `sum by (pool_name) (rate(db_client_connections_use_time_milliseconds_sum{J}[$__rate_interval])) / sum by (pool_name) (rate(db_client_connections_use_time_milliseconds_count{J}[$__rate_interval]))` | ms |
| `redis.conn_create_avg` | Redis connection setup (avg) | timeseries | `sum by (pool_name) (rate(db_client_connections_create_time_milliseconds_sum{J}[$__rate_interval])) / sum by (pool_name) (rate(db_client_connections_create_time_milliseconds_count{J}[$__rate_interval]))` | ms |
| `redis.pool_reuse_ratio` | Redis connection reuse | stat | `sum by (pool_name) (delta(db_client_connections_hits{J}[$__range])) / (sum by (pool_name) (delta(db_client_connections_hits{J}[$__range])) + sum by (pool_name) (delta(db_client_connections_misses{J}[$__range])))` (share of commands that got an idle pooled connection) | percentunit |

`hits`, `misses`, `waits` and `timeouts` are cumulative values exported as **gauges**. Use
`delta`/`deriv`, never `rate`. Command latency is in **milliseconds**, unlike everything else.

## Runtime

Go runtime (`go_*`, needs `go_metrics`) and process (`process_*`, needs `process_metrics`) metrics,
one series per node. **Check the type before using `rate()`:** `go_memstats_*_bytes`,
`go_memstats_heap_objects`, `go_goroutines`, `go_threads` and `process_open_fds` are gauges (use
them as they are, or `avg_over_time`/`max_over_time`); only `*_total` are counters.
`go_gc_duration_seconds` is a summary: the `quantile` label holds pause quantiles of recent
cycles, `rate(_count)` is GCs per second and `rate(_sum)` is the fraction of time stopped for GC.
Per-node panels group `by (instance)`; with several targets on one panel, pin it to one node or
repeat it per `instance`.

| id | title | panel | query | unit |
|---|---|---|---|---|
| `runtime.nodes_up` | Nodes up | stat (red if < 1) | `count(up{J} == 1) or vector(0)` | short |
| `runtime.uptime` | Uptime | stat | `min(time() - process_start_time_seconds{J})` (most recently restarted node) | s |
| `runtime.restarts` | Restarts | stat (red if > 0) | `sum(changes(process_start_time_seconds{J}[$__range]))` | short |
| `runtime.go_version` | Go version | table | `count by (version) (go_info{J})` | short |
| `runtime.cpu` | CPU (cores) | timeseries | `sum by (instance) (rate(process_cpu_seconds_total{J}[$__rate_interval]))` | short (cores) |
| `runtime.cpu_saturation` | CPU saturation | gauge (thresholds 70%/90%) | `max by (instance) (rate(process_cpu_seconds_total{J}[$__rate_interval]) / go_sched_gomaxprocs_threads{J})` (CPU used against the cores Go may use) | percentunit |
| `runtime.gomaxprocs` | GOMAXPROCS | stat | `max(go_sched_gomaxprocs_threads{J})` | short |
| `runtime.rss` | Resident memory (RSS) | timeseries | `sum by (instance) (process_resident_memory_bytes{J})` | bytes |
| `runtime.memory_avg_in_range` | Avg memory (RSS) | stat | `avg(avg_over_time(process_resident_memory_bytes{J}[$__range]))` (per node) | bytes |
| `runtime.memory_breakdown` | Go memory breakdown | timeseries | `sum(go_memstats_sys_bytes{J})` as `Obtained from OS` and `sum(go_memstats_heap_inuse_bytes{J})` as `Heap in use` and `sum(go_memstats_heap_idle_bytes{J}) - sum(go_memstats_heap_released_bytes{J})` as `Heap idle (kept)` and `sum(go_memstats_heap_released_bytes{J})` as `Heap released to OS` and `sum(go_memstats_stack_inuse_bytes{J})` as `Stacks` | bytes |
| `runtime.heap_inuse` | Heap in use | timeseries | `sum by (instance) (go_memstats_heap_inuse_bytes{J})` | bytes |
| `runtime.heap_vs_gc_target` | Heap vs GC target | timeseries | `sum(go_memstats_heap_alloc_bytes{J})` as `Live heap` and `sum(go_memstats_next_gc_bytes{J})` as `Next GC at` | bytes |
| `runtime.memory_limit_usage` | Memory limit used | gauge (thresholds 80%/95%) | `max by (instance) (go_memstats_sys_bytes{J} / (go_gc_gomemlimit_bytes{J} < 1e18))` (agent: empty without `GOMEMLIMIT`; keep it, production containers often set it) | percentunit |
| `runtime.alloc_rate` | Allocation rate | timeseries | `sum by (instance) (rate(go_memstats_alloc_bytes_total{J}[$__rate_interval]))` | Bps |
| `runtime.mallocs_frees` | Mallocs vs frees / s | timeseries | `sum(rate(go_memstats_mallocs_total{J}[$__rate_interval]))` as `Allocations` and `sum(rate(go_memstats_frees_total{J}[$__rate_interval]))` as `Frees` | ops |
| `runtime.heap_objects` | Heap objects | timeseries | `sum by (instance) (go_memstats_heap_objects{J})` | short |
| `runtime.gc_rate` | GC cycles / s | timeseries | `sum by (instance) (rate(go_gc_duration_seconds_count{J}[$__rate_interval]))` | ops (GC/s) |
| `runtime.gc_time_fraction` | Time in GC pauses | timeseries | `sum by (instance) (rate(go_gc_duration_seconds_sum{J}[$__rate_interval]))` (share of wall time stopped for GC) | percentunit |
| `runtime.gc_pause_quantiles` | GC pause quantiles | timeseries | `max by (quantile) (go_gc_duration_seconds{J})` | s |
| `runtime.gc_pause_p99` | GC pause p99 | timeseries | `max by (instance) (go_gc_duration_seconds{J, quantile="1.0"})` (quantile 1.0 = longest recent pause) | s |
| `runtime.gogc` | GOGC | stat | `max(go_gc_gogc_percent{J})` | short |
| `runtime.goroutines` | Goroutines | timeseries | `sum by (instance) (go_goroutines{J})` | short |
| `runtime.threads` | OS threads | timeseries | `sum by (instance) (go_threads{J})` | short |
| `runtime.open_fds` | Open file descriptors | timeseries | `sum by (instance) (process_open_fds{J})` | short |
| `runtime.fd_usage` | File descriptors used | gauge (thresholds 70%/90%) | `max by (instance) (process_open_fds{J} / process_max_fds{J})` | percentunit |
| `runtime.network_io` | Network I/O | timeseries | `sum(rate(process_network_receive_bytes_total{J}[$__rate_interval]))` as `Received` and `sum(rate(process_network_transmit_bytes_total{J}[$__rate_interval]))` as `Sent` (agent: Linux only: empty on macOS/Windows dev machines, present in Linux containers and servers; keep it) | Bps |

Goroutines track open connections: each in-flight request, keep-alive client and backend
connection holds at least one. A goroutine count that keeps growing while traffic is flat is a leak
or stuck backends; compare it with `backends.conn_pool_wait_p95` and timeouts.
`runtime.memory_limit_usage` only shows data when the container sets `GOMEMLIMIT`, and
`runtime.network_io` only on Linux. Keep both even if this environment shows nothing: production
often differs (see "Panels that depend on where KrakenD runs" in `dashboard-conventions.md`).
