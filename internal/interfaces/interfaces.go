package interfaces

import (
	"context"
	"time"

	"github.com/infercore/infercore/internal/types"
)

type Router interface {
	SelectRoute(ctx context.Context, req types.InferenceRequest, state types.RuntimeState) (types.RouteDecision, error)
}

type PolicyEngine interface {
	Evaluate(ctx context.Context, req types.InferenceRequest) (types.PolicyDecision, error)
}

type BackendAdapter interface {
	Name() string
	Invoke(ctx context.Context, req types.BackendRequest) (types.BackendResponse, error)
	Health(ctx context.Context) error
	Metadata() types.BackendMetadata
}

type ReliabilityManager interface {
	ExecuteWithFallback(
		ctx context.Context,
		req types.InferenceRequest,
		primary types.RouteDecision,
		fallback []types.RouteDecision,
	) (types.ExecutionResult, error)
}

type CostEngine interface {
	Estimate(req types.InferenceRequest, backend types.BackendMetadata) types.CostEstimate
}

type SLOEngine interface {
	RecordStart(requestID string)
	RecordFirstToken(requestID string, ts time.Time)
	RecordCompletion(requestID string, ts time.Time)
	RecordFallback(requestID string, reason string)
	Snapshot(requestID string) types.SLOSnapshot
}

type TelemetryExporter interface {
	EmitMetric(name string, value float64, labels map[string]string)
	EmitEvent(event types.Event)
	EmitTrace(trace types.TraceRecord)
}

type ScalingSignalProvider interface {
	CurrentSignals() types.ScalingSignals
}
