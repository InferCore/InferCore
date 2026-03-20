package types

type RequestOptions struct {
	Stream    bool `json:"stream"`
	MaxTokens int  `json:"max_tokens"`
}

type InferenceRequest struct {
	RequestID string         `json:"request_id,omitempty"`
	TenantID  string         `json:"tenant_id"`
	TaskType  string         `json:"task_type"`
	Priority  string         `json:"priority"`
	Input     map[string]any `json:"input"`
	Options   RequestOptions `json:"options"`
}

type RouteDecision struct {
	BackendName   string   `json:"backend_name"`
	Reason        string   `json:"reason"`
	EstimatedCost float64  `json:"estimated_cost"`
	FallbackChain []string `json:"fallback_chain"`
}

type ExecutionResult struct {
	Status       string         `json:"status"`
	BackendName  string         `json:"backend_name"`
	Output       map[string]any `json:"output"`
	UsedFallback bool           `json:"used_fallback"`
	Error        error          `json:"error,omitempty"`
	Timing       *BackendTiming `json:"timing,omitempty"`
}

// BackendTiming is measured inside the adapter (wall clock from invoke start).
type BackendTiming struct {
	TTFTMs              int64 `json:"ttft_ms"`
	CompletionLatencyMs int64 `json:"completion_latency_ms"`
	TPOTMs              int64 `json:"tpot_ms"`
	Streamed            bool  `json:"streamed"`
}

type SLOSnapshot struct {
	TTFTMs              int64 `json:"ttft_ms"`
	TPOTMs              int64 `json:"tpot_ms"`
	CompletionLatencyMs int64 `json:"completion_latency_ms"`
	FallbackTriggered   bool  `json:"fallback_triggered"`
}

type InferenceMetrics struct {
	TTFTMs               int64   `json:"ttft_ms"`
	TPOTMs               int64   `json:"tpot_ms"`
	CompletionLatencyMs  int64   `json:"completion_latency_ms"`
	EstimatedCost        float64 `json:"estimated_cost"`
	QueueWaitTimeMs      int64   `json:"queue_wait_time_ms"`
	ServerReceivedAtUnix int64   `json:"server_received_at_unix_ms"`
}

type FallbackState struct {
	Triggered bool   `json:"triggered"`
	Reason    string `json:"reason,omitempty"`
}

type DegradeState struct {
	Triggered bool   `json:"triggered"`
	Reason    string `json:"reason,omitempty"`
}

type InferenceResponse struct {
	RequestID         string           `json:"request_id"`
	TraceID           string           `json:"trace_id,omitempty"`
	SelectedBackend   string           `json:"selected_backend"`
	RouteReason       string           `json:"route_reason"`
	PolicyReason      string           `json:"policy_reason,omitempty"`
	EffectivePriority string           `json:"effective_priority,omitempty"`
	Status            string           `json:"status"`
	Result            map[string]any   `json:"result"`
	Metrics           InferenceMetrics `json:"metrics"`
	Fallback          FallbackState    `json:"fallback"`
	Degrade           DegradeState     `json:"degrade"`
}

type PolicyDecision struct {
	Allowed    bool
	Reason     string
	Normalized InferenceRequest
}

type RuntimeState struct {
	QueueDepth         int
	BackendHealth      map[string]bool
	RecentTimeoutSpike bool
	// OverloadDegrade is set when infer concurrency is at/above configured limit and action is "degrade" (router may skip cost optimization).
	OverloadDegrade bool
}

type BackendRequest struct {
	InferenceRequest InferenceRequest
}

type BackendResponse struct {
	Output map[string]any
	Timing *BackendTiming
}

type BackendMetadata struct {
	Name           string
	Type           string
	Capabilities   []string
	CostUnit       float64
	MaxConcurrency int
}

type CostEstimate struct {
	UnitCost       float64
	EstimatedTotal float64
	BudgetFit      bool
}

type Event struct {
	Name      string
	Timestamp int64
	Labels    map[string]string
}

type TraceRecord struct {
	TraceID   string
	SpanID    string
	Name      string
	StartUnix int64
	EndUnix   int64
	Labels    map[string]string
}

type ScalingSignals struct {
	QueueDepth             int      `json:"queue_depth"`
	TimeoutSpike           bool     `json:"timeout_spike"`
	TTFTDegradationRatio   float64  `json:"ttft_degradation_ratio"`
	RecentFallbackRate     float64  `json:"recent_fallback_rate"`
	BackendSaturationHints []string `json:"backend_saturation_hints"`
}
