# Dashboard blueprints

What a dashboard *about* a topic contains, following the structure of KrakenD's official
dashboards (github.com/krakend/telemetry-dashboards): a stat row answers "how is it right now",
then rows of time series answer "how did it change".

`go run ./cmd/observability-recipes` turns this file into the `blueprints` of recipes.json, and
`get_dashboard_recipes` with the topic intent returns exactly these recipes plus the row layout.

Format: `## <intent>`, then one bullet per row: `- <Row title>: \`recipe.id\`, ...`. A row
starting with `Optional` is included only when the user's config enables its recipes. Any other
line is a note for the agent.

## overview

- Stats: `traffic.requests_in_range`, `traffic.rps_avg_in_range`, `latency.avg_in_range`, `latency.percentiles_in_range`, `errors.ratio_5xx`, `errors.ratio_4xx`, `runtime.nodes_up`, `errors.status_split_in_range`
- Traffic: `traffic.rps_by_endpoint`, `traffic.rps_by_status`, `traffic.not_found_rate`
- Latency: `latency.p50/p95/p99`, `latency.p95_by_endpoint`, `latency.avg_by_endpoint`, `latency.slowest_endpoints`, `latency.heatmap`
- Errors: `errors.ratio_5xx_by_endpoint`, `errors.non2xx_by_endpoint_in_range`, `errors.backend_failed_calls`, `errors.backend_timeouts`
- Backends: `backends.rps_by_backend`, `latency.backend_host_p95`, `errors.backend_status_codes`
- Optional Resilience: `cb.opened_in_range`, `cb.transitions`, `ratelimit.rejections`
- Optional Quotas: `quota.rejected_in_range`, `quota.rejection_ratio`
- Optional Async agents: `agents.messages_rate`, `agents.error_ratio`, `agents.processing_p95`, `agents.backend_status`
- Optional Kafka: `kafka.published_rate`, `kafka.consumed_rate`, `kafka.failures`, `kafka.publish_p95`
- Optional AI: `ai.tokens_rate_by_model`, `ai.tokens_in_range`, `ai.guard_block_ratio`
- Optional WebSocket: `ws.open_sessions`, `ws.msgs_in_rate`, `ws.msgs_out_rate`
- Optional Redis: `redis.pool_saturation`, `redis.cmd_p95`, `redis.timeouts`
- Runtime: `runtime.cpu`, `runtime.rss`, `runtime.goroutines`

Optional rows go after Backends, only when the config enables them; without a config they are left out.

## runtime

- Stats: `runtime.nodes_up`, `runtime.uptime`, `runtime.restarts`, `runtime.memory_avg_in_range`, `runtime.gomaxprocs`, `runtime.gogc`, `runtime.cpu_saturation`, `runtime.fd_usage`, `runtime.memory_limit_usage`
- CPU & scheduling: `runtime.cpu`, `runtime.goroutines`, `runtime.threads`
- Memory: `runtime.rss`, `runtime.memory_breakdown`, `runtime.heap_vs_gc_target`, `runtime.heap_objects`
- Allocations & GC: `runtime.alloc_rate`, `runtime.mallocs_frees`, `runtime.gc_rate`, `runtime.gc_time_fraction`, `runtime.gc_pause_quantiles`
- Process: `runtime.open_fds`, `runtime.network_io`, `runtime.go_version`
- Traffic: `traffic.rps_total`

Needs `go_metrics` / `process_metrics`. The traffic panel is context, to read memory and CPU against load. With several nodes, repeat the rows per node (`"repeat": "instance"`) when the user wants them side by side. `runtime.network_io` is Linux only and `runtime.memory_limit_usage` needs `GOMEMLIMIT`: keep both, production usually has them.

## backends

- Stats: `errors.backend_failed_in_range`, `backends.slowest`
- Throughput: `backends.rps_by_backend`, `backends.started_vs_canceled`
- Latency: `backends.avg_by_backend`, `latency.backend_host_p95`, `latency.backend_heatmap`
- Failures: `errors.backend_status_codes`, `errors.backend_timeouts`, `errors.backend_transport_failures`, `errors.backend_canceled`
- Connections: `backends.connection_setup_avg`, `backends.conn_pool_wait_p95`, `backends.dns_p95`, `backends.tls_p95`
- Payload: `backends.response_size_avg`, `backends.ingress_bytes`, `backends.no_content_length`

## agents

- Stats: `agents.messages_in_range`, `agents.error_ratio`
- Messages: `agents.messages_rate`, `agents.backend_failures`
- Processing time: `agents.processing_p95`, `agents.processing_avg`
- Backend: `agents.backend_p95`, `agents.backend_status`

Never filter agent panels with `$endpoint`: agent names are not endpoints.
