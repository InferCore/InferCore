# InferCore

InferCore is an open-source **AI Inference Control Plane**: a decision layer that sits **above** model serving and data plane systems.

It provides intelligent routing, cost-aware decisions, fallback and degrade orchestration, AI-native SLO signals, multi-tenant policy enforcement, and observability / scaling-signal exports.

**InferCore is not a model server.** It does not run token generation and does not replace vLLM, Triton, Ray Serve, or KServe.

## Design goals

InferCore fills the missing **system decision layer** in typical serving stacks:

| Goal | Meaning |
|------|---------|
| **Route** | Send different requests to different backends/models |
| **Protect** | Degrade gracefully on timeout, overload, or failure |
| **Optimize** | Trade off latency, quality, and cost with explicit policy |
| **Signal** | Export TTFT, TPOT, queue depth, fallback rate, and related AI-native metrics |
| **Isolate** | Tenant priority, quota, budget, and fairness |

## System boundaries

### InferCore owns

- Request ingress and normalization
- Routing decisions
- Tenant and policy enforcement
- Fallback and reliability orchestration
- Cost estimation and budget-based routing
- AI SLO signal generation
- Metrics / traces / events export

### InferCore does not own

- Model inference execution
- CUDA/GPU kernel optimization
- Training and MLOps pipelines
- Executing autoscaling (it can **export signals** for HPA/KEDA/custom autoscalers)
- Advanced dashboards and long-term analytics stores

## Architecture

```text
Client / SDK
    ↓
[ InferCore Gateway ]
    ↓
[ Request Normalizer ]
    ↓
[ Policy Engine ] ← tenant / budget / priority / guardrails
    ↓
[ Router Engine ] ← route selection / fallback planning / cost-aware decision
    ↓
[ Execution Adapter Layer ] ← vLLM / OpenAI-compatible HTTP / Mock / …
    ↓
Inference Backends

Parallel outputs:
- Metrics exporter (Prometheus text on /metrics)
- Trace / event exporters (configurable telemetry backends)
- In-memory AI SLO engine (bounded store; snapshots on responses)
- Scaling signals (/status.scaling_signals + infercore_scaling_* gauges)
```

## Request lifecycle

1. Client sends an inference request to InferCore (`POST /infer`).
2. Gateway parses tenant, task type, priority, and payload.
3. Policy engine evaluates quota, budget, priority, and guardrails.
4. Router selects a backend using rules, health, overload state, and optional cost optimization.
5. Execution adapter invokes the selected backend.
6. On timeout or classified failure, reliability rules may trigger fallback or degrade behavior.
7. InferCore records TTFT/TPOT/latency/fallback (where available) and exports metrics, events, and traces as configured.

## Differentiators

1. **Cost-aware routing** — Not only “can this run?” but “should this use the expensive model?” within budget and compatibility constraints.
2. **AI-native SLO** — TTFT, TPOT, completion latency, fallback markers; rolling hints exposed for operators (`/status`, Prometheus).
3. **Reliability orchestration** — Per-backend timeouts, configurable fallback chains, overload reject/degrade, health-aware routing.
4. **Multi-tenant policy** — Tenant classes, priorities, per-request budget estimates, per-tenant RPS windows (in-memory per replica).
5. **Scaling signals** — Queue depth, timeout-spike hints, TTFT degradation ratio, recent fallback rate, optional `max_concurrency` hints from config — intended for HPA/KEDA or custom autoscalers (**per-replica** unless you add shared state).

## Repository layout

This repository includes:

- In-scope/out-of-scope document (`docs/scope.md`)
- YAML configuration draft
- OpenAPI draft for key endpoints
- Go interface contracts for core modules
- Minimal runnable HTTP service

## Quickstart

### Prerequisites

- Go 1.22+

### Run

```bash
go run ./cmd/infercore
```

The service starts on `:8080`.

Use a custom config file:

```bash
INFERCORE_CONFIG=./configs/infercore.example.yaml go run ./cmd/infercore
```

### Make targets

```bash
make help     # list targets
make all      # fmt, vet, test
make test
make build    # writes bin/infercore
make run      # CONFIG=... optional (default configs/infercore.example.yaml)
```

### Docker

```bash
make docker-build   # image tag: infercore:local
docker run --rm -p 8080:8080 infercore:local
```

Mount your own config:

```bash
docker run --rm -p 8080:8080 \
  -v "$(pwd)/configs/my.yaml:/app/config.yaml:ro" \
  -e INFERCORE_CONFIG=/app/config.yaml \
  infercore:local
```

### CI

GitHub Actions workflow (on `push`/`pull_request` to `main` or `master`): `.github/workflows/ci.yml` runs `go vet` and `go test ./...`.

### Try APIs

Health:

```bash
curl -s http://localhost:8080/health
```

Status:

```bash
curl -s http://localhost:8080/status
```

Infer:

```bash
curl -s -X POST http://localhost:8080/infer \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "team-a",
    "task_type": "simple",
    "priority": "normal",
    "input": {"text": "Summarize this article"},
    "options": {"stream": false, "max_tokens": 256}
  }'
```

Example success response (trimmed):

```json
{
  "request_id": "550e8400-e29b-41d4-a716-446655440000",
  "trace_id": "7f5f6f0d9f984f9a8e3d7b4f8f1d2a3c",
  "selected_backend": "small-model",
  "route_reason": "standard-simple-route",
  "status": "success"
}
```

Metrics:

```bash
curl -s http://localhost:8080/metrics
```

Smoke test script:

```bash
bash ./scripts/smoke.sh
```

## Core Endpoints

- `POST /infer`
- `GET /health`
- `GET /status`
- `GET /metrics`

API draft: `api/openapi.yaml`

## Current runtime behavior

High-level summary of what the running service does today. For streaming details see [`docs/streaming-and-fallback.md`](docs/streaming-and-fallback.md); for metrics/logs see [`docs/observability.md`](docs/observability.md).

### Config & validation

- YAML loaded at startup (`INFERCORE_CONFIG`, or the default example path).
- Validates unique backend/tenant names, routing rule names, route/fallback backend references, and non-empty fallback triggers.
- Allowed fallback triggers: `timeout`, `backend_unhealthy`, `backend_error`.

### Request timeouts & HTTP server

- **`server.request_timeout_ms`** — wall-clock budget for each `/infer` after JSON body validation (policy + routing + backend). If this fires: **504** and `error.code=gateway_timeout`. If a per-backend **`timeout_ms`** fires first: **502** `execution_failed`.
- **`cmd/infercore`** sets `http.Server` `ReadTimeout` / `WriteTimeout` / `IdleTimeout` from `server.http.{read,write,idle}_timeout_ms` when set; otherwise derives from `request_timeout_ms` plus conventional slack (read / write / keep-alive).

### Tenant policy & routing

- **Policy:** reject unknown tenants, per-request budget gate (light estimate), per-tenant RPS limit (in-memory, 1s window), priority normalized from tenant defaults. **`priority`** may be omitted on `/infer`; it is filled from tenant config.
- **Routing:** rules by tenant class / task type / priority.
- **Health-aware routing:** skips backends failing `adapter.Health`, cached with `server.health_cache_ttl_ms` (same cache drives `/status` backend fields). If the default backend is unhealthy, uses the first healthy backend in config order (`healthy-fallback-order`). If none healthy: `route_error` on `/infer`.

### Overload, cost, reliability

- **Overload:** `reliability.overload.queue_limit` and `action` — `reject` → **503** `overload`; `degrade` → skip cost optimization and set `degrade` in the JSON. In-flight: `infercore_infer_inflight` on `/metrics`; `/status.queue_depth` matches the same counter.
- **Cost:** may pick a cheaper compatible backend within budget (healthy backends only).
- **Fallback:** timeout-aware chain from reliability config; structured event logs on policy rejection, execution failure, and fallback.

### `/infer` responses & errors

- Success includes **`trace_id`**; also **`policy_reason`** and **`effective_priority`** for debugging.
- Errors: `{ request_id, status, error: { code, message } }`.
- **`degrade`** appears when upstream streaming is degraded (see streaming doc).

### Backend adapters

| Kind | Config `type` | Notes |
|------|----------------|--------|
| Mock | `mock` | For tests / load profiles. |
| OpenAI-compatible | `vllm`, `openai`, `openai_compatible` | Same code path: **Chat Completions** + optional `GET` health. Supports `api_key` (default `Authorization: Bearer …`), optional `auth_header_name`, `headers`, **`health_path`** (default `/health`; many clouds use `/v1/models`), **`default_model`**. |
| Gemini (native) | `gemini` | `generateContent` / `streamGenerateContent`, API key. Example: [`configs/infercore.example.yaml`](configs/infercore.example.yaml). |
| Qwen (DashScope) | `openai_compatible` | OpenAI-compatible base, e.g. `https://dashscope.aliyuncs.com/compatible-mode/v1` with `health_path: /models`, `default_model`, `api_key` — **no** separate adapter. |

### Streaming (OpenAI-compatible)

- With **`options.stream=true`**, uses upstream SSE when supported; a JSON body instead is treated as **`stream_degraded`**. InferCore still returns **aggregated JSON** to the client.

### SLO, telemetry & metrics

- **SLO store** (in-memory, bounded by `slo.max_records` / `slo.max_age_ms`): TTFT, TPOT (when adapter provides it), completion latency, fallback markers.
- **Telemetry exporters:** `log`, `otlp-http-stub`, `otlp-http` (OTLP/HTTP protobuf), `otlp-http-json` (legacy JSON). Switches: `telemetry.metrics_enabled`, `telemetry.tracing_enabled`. OTLP: `otlp_flush_interval_ms`, `otlp_timeout_ms` (batching for `otlp-http`).
- Exporter emits metric/event logs per completed inference; trace hooks add trace/span IDs and result labels.
- **`GET /status`:** exporter summary and **`scaling_signals`** (queue depth, timeout hint, rolling TTFT/fallback from SLO, `max_concurrency` hints).
- **`GET /metrics`:** Prometheus `client_golang` — e.g. `infercore_requests_total`, `infercore_infer_inflight`, `infercore_http_requests_total`, plus `infercore_scaling_*` gauges aligned with scaling signals.
- HTTP requests: `infercore_http_requests_total` with path/method/status labels.

### Optional API key & shutdown

- Gate with **`server.infercore_api_key`** or **`INFERCORE_API_KEY`**: send `X-InferCore-Api-Key` or `Authorization: Bearer …` on `/infer`, `/status`, `/metrics`. **`/health`** stays unauthenticated.
- On **SIGINT/SIGTERM**: graceful HTTP shutdown and OTLP flush (`Server.Shutdown`).

## Multi-instance deployment

Horizontal scale is typically **multiple InferCore replicas behind a load balancer**. Each process keeps its own in-memory policy windows, SLO store, overload counter, and health cache, so **global** RPS limits, concurrency caps, and rolling SLO ratios are **per replica** unless you add shared state (e.g. Redis) or aggregate in Prometheus. Use `/metrics` and `/status.scaling_signals` as inputs to cluster-level autoscaling.

## Project Structure

- `cmd/infercore`: service entrypoint
- `internal/server`: HTTP handlers and routing
- `internal/interfaces`: core module contracts
- `internal/types`: shared core data structures
- `configs`: YAML configuration examples
- `docs`: architecture and scope docs
- `api`: OpenAPI contract draft

## Load testing

- Guide: `docs/load-testing.md` (throughput with [hey](https://github.com/rakyll/hey), overload / rate-limit / 504 behavior)
- Config: `configs/infercore.loadtest.yaml` (mock-only, high `queue_limit`, `rate_limit_rps: 0`)
- Script: `make load-infer` or `bash ./scripts/load-infer.sh` (env: `BASE_URL`, `DURATION`, `CONCURRENCY`, `QPS`, `INFERCORE_API_KEY`)

## Documents

- License: [`LICENSE`](LICENSE) (Apache-2.0)
- Architecture (full one-pager copy for print/PDF): `docs/architecture-one-pager.md`
- Scope: `docs/scope.md`
- Config example: `configs/infercore.example.yaml`
- Observability: `docs/observability.md`
- Load testing: `docs/load-testing.md`
- Streaming & fallback: `docs/streaming-and-fallback.md`
- Planned hardening: `docs/future-work.md`

## License

InferCore is licensed under the **Apache License, Version 2.0**. See the [`LICENSE`](LICENSE) file for the full text.

SPDX-License-Identifier: Apache-2.0

Third-party dependencies are subject to their own licenses (see module metadata and your `go.sum` / vendor tree as applicable).

## Next Implementation Steps

1. Optional Prometheus scrape endpoint; OTLP Logs for telemetry events.
2. Extend policy with quota windows and richer guardrail hooks.
3. Add benchmark scenarios and baseline-vs-infercore scripts.
4. Client-facing SSE from InferCore (passthrough/proxy streaming).
5. Add config versioning, hot reload, and migration tooling.
