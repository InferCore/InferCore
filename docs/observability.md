# InferCore Observability

This document describes the observability surface implemented in InferCore.

## Status

`GET /status` includes:

- `backends[]` — health derived from the **same TTL cache** as routing (not a separate probe per request).
- `queue_depth` — current `/infer` in-flight count (after admission through overload policy).
- `scaling_signals` — autoscaler-oriented snapshot: `queue_depth`, `timeout_spike` (rolling 1-minute infer timeout count), `ttft_degradation_ratio` / `recent_fallback_rate` from the in-memory SLO store, and `backend_saturation_hints` from `max_concurrency` in config.

## Request timeouts

- `server.request_timeout_ms` bounds policy + routing + backend execution **after** JSON decode on `POST /infer`. When this budget is exceeded, the API returns **504** with `error.code=gateway_timeout`. If a shorter **per-backend** `timeout_ms` fires first, the response stays **502** `execution_failed`.
- `server.http.read_timeout_ms`, `server.http.write_timeout_ms`, `server.http.idle_timeout_ms` optionally set `net/http.Server` timeouts (milliseconds). Zero or omitted → defaults derived from `request_timeout_ms` in `cmd/infercore` (read includes extra slack for large/slow request bodies; write includes slack after the infer deadline).
- Adapter `Health` checks use their own `server.health_check_per_backend_ms` budget on `context.Background()` so a tight infer deadline does not invalidate cached health results.

## Metrics

`GET /metrics` serves Prometheus text format via `prometheus/client_golang` (custom registry; no default Go/process collectors).

### Built-in metrics

- `infercore_requests_total`
  - Total number of accepted `/infer` requests assigned a request ID.
- `infercore_infer_inflight`
  - Gauge: requests currently inside `/infer` after passing overload admission (matches `/status.queue_depth`).
- `infercore_http_requests_total{path,method,status}`
  - HTTP request counter with labels:
    - `path`: request path (for example `/infer`, `/health`)
    - `method`: HTTP method
    - `status`: response status code
- `infercore_scaling_ttft_degradation_ratio` — gauge mirroring `scaling_signals.ttft_degradation_ratio`.
- `infercore_scaling_recent_fallback_rate` — gauge mirroring `scaling_signals.recent_fallback_rate`.
- `infercore_scaling_timeout_spike` — `1` when rolling-minute timeout count exceeds threshold, else `0`.

## Structured Events

InferCore emits structured log events for key decision/reliability outcomes:

- `policy_rejected`
- `execution_failed`
- `fallback_triggered`
- `http_request`

In addition, telemetry event export emits:

- `infer_request_completed`

## Traces (Basic Hooks)

InferCore emits a trace record per `/infer` lifecycle using the telemetry exporter.

### Trace identity

- `trace_id`: generated per request
- `span_id`: generated per request
- `name`: `infer_request`

### Trace labels

- `request_id`
- `tenant_id`
- `backend`
- `result`

### Typical `result` values

- `success`
- `method_not_allowed`
- `invalid_json`
- `missing_tenant_id`
- `missing_task_type`
- `missing_input`
- `invalid_max_tokens`
- `policy_error`
- `policy_rejected`
- `route_error`
- `execution_failed`

## SLO Signals (in-memory engine)

InferCore computes request-level SLO signals in memory (bounded by `slo.max_records` and `slo.max_age_ms`):

- `ttft_ms` — from adapter timing when available (streaming: first token; non-stream: full completion latency as proxy)
- `tpot_ms` — streaming adapters may set from post–first-token duration; otherwise a small in-engine approximation
- `completion_latency_ms`
- `fallback_triggered`

These values are returned in `/infer` response metrics and used for telemetry export.

## Response Correlation Fields

`POST /infer` success response includes:

- `request_id`
- `trace_id`

These fields can be used to correlate API responses with logs/events/traces.

## Current Limitations

- No persistent metrics/event/trace storage yet.
- Telemetry exporter supports:
  - `log` (stdout logs)
  - `otlp-http-stub` (logging stub)
  - `otlp-http` — **OpenTelemetry SDK** OTLP/HTTP **protobuf** to a standard Collector (`/v1/traces`, `/v1/metrics`)
  - `otlp-http-json` — legacy JSON payloads (non-standard OTLP; for custom bridges only)
- Exporter status summary is exposed via `GET /status` under `telemetry`.
- Trace model is basic (single span per completed `/infer`); span IDs are SDK-generated with `infercore.trace_id` / `infercore.span_id` attributes for correlation.
- `EmitEvent` is not mapped to OTLP Logs when using `otlp-http` (no-op for events on that exporter).

## Reliability Trigger Reference

Current fallback trigger values accepted by config:

- `timeout`
- `backend_unhealthy`
- `upstream_4xx`
- `upstream_5xx`
- `backend_error`
