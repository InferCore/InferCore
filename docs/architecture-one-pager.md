# InferCore Architecture One-Pager

## Project Definition

InferCore is an open-source AI Inference Control Plane.

It runs above model serving and data plane systems and provides:

- Intelligent routing
- Cost-aware decisions
- Fallback and degrade orchestration
- AI-native SLO signals
- Multi-tenant policy enforcement
- Observability and scaling-signal outputs

InferCore is **not** a model server. It does not execute token generation and does not replace vLLM, Triton, Ray Serve, or KServe.

## Design Goals

InferCore addresses the missing system decision layer in existing serving stacks:

- **Route**: Send different requests to different backends/models
- **Protect**: Gracefully degrade on timeout, overload, or failure
- **Optimize**: Trade off latency, quality, and cost with explicit policy
- **Signal**: Export TTFT, TPOT, queue depth, fallback rate, and related AI-native signals
- **Isolate**: Support tenant priority, quota, budget, and fairness

## System Boundaries

### InferCore owns

- Request ingress and normalization
- Routing decisions
- Tenant and policy enforcement
- Fallback and reliability orchestration
- Cost estimation and budget-based routing
- AI SLO signal generation
- Metrics/traces/events export

### InferCore does not own

- Model inference execution
- CUDA/GPU kernel optimization
- Training and MLOps pipeline
- Actual autoscaling execution
- Advanced dashboards and long-term analytics

## High-Level Architecture

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
[ Execution Adapter Layer ] ← vLLM / Triton / Ray Serve / Mock
    ↓
Inference Backends

Parallel outputs:
- Metrics Exporter
- Trace/Event Exporter
- AI SLO Signal Engine
- Scaling Signals Exporter
```

## Core Flow

1. Client sends an inference request to InferCore.
2. Gateway parses tenant, task type, priority, and metadata.
3. Policy Engine evaluates quota, budget, priority, and guardrails.
4. Router Engine selects a route based on cost, SLO targets, and backend state.
5. Execution Adapter invokes the selected backend.
6. On timeout/failure, Reliability Policy triggers fallback/degrade.
7. InferCore records and exports TTFT, TPOT, latency, fallback, and cost signals.

## Key Differentiators

### 1) Cost-aware routing

InferCore answers not only "can this run?" but also "should this use the expensive model?"

### 2) AI-native SLO

InferCore tracks:

- TTFT
- TPOT
- Completion latency
- Fallback rate
- Invalid output rate

### 3) Reliability orchestration

InferCore supports:

- Timeout handling
- Fallback chains
- Degrade mode
- Queue overflow protection

### 4) Multi-tenant policy

InferCore supports:

- Tenant priority
- Budget classes
- Quota and fairness
- Noisy-neighbor protection

### 5) Scaling signals

InferCore does not scale directly, but exports:

- Queue depth
- TTFT degradation
- Timeout spikes
- Fallback spikes

These signals can be consumed by Kubernetes HPA, KEDA, or custom autoscalers.

## Scaling signals (implementation)

The service exposes `scaling_signals` on `GET /status` and matching `infercore_scaling_*` Prometheus gauges (queue depth, timeout spike hint, rolling TTFT/fallback aggregates, optional `max_concurrency` hints). These are intended for HPA/KEDA or custom autoscalers; they reflect **per-replica** in-memory state unless extended with shared stores.
