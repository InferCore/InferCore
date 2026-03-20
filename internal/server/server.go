package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/infercore/infercore/internal/adapters/mock"
	"github.com/infercore/infercore/internal/adapters/vllm"
	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/cost"
	"github.com/infercore/infercore/internal/interfaces"
	"github.com/infercore/infercore/internal/policy"
	"github.com/infercore/infercore/internal/reliability"
	"github.com/infercore/infercore/internal/router"
	"github.com/infercore/infercore/internal/slo"
	"github.com/infercore/infercore/internal/telemetry"
	"github.com/infercore/infercore/internal/traceutil"
	"github.com/infercore/infercore/internal/types"
	"github.com/infercore/infercore/internal/upstream"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/singleflight"
)

type Server struct {
	cfg         *config.Config
	policy      interfaces.PolicyEngine
	router      interfaces.Router
	reliability interfaces.ReliabilityManager
	sloEngine   interfaces.SLOEngine
	telemetry   interfaces.TelemetryExporter
	adapters    map[string]interfaces.BackendAdapter

	healthMu       sync.Mutex
	healthSnapshot map[string]bool
	healthAt       time.Time
	healthFlight   singleflight.Group

	inferInflight atomic.Int32

	promReg         *prometheus.Registry
	inferReqCounter prometheus.Counter
	httpReqCounter  *prometheus.CounterVec

	timeoutSpikeMu  sync.Mutex
	timeoutSpikeWin time.Time
	timeoutSpikeCnt int
}

// minTimeoutsPerMinuteForSpike is the rolling 1-minute threshold for scaling_signals.timeout_spike.
const minTimeoutsPerMinuteForSpike = 3

// HTTPLayerTimeouts returns net/http.Server timeouts.
// If server.http.{read,write,idle}_timeout_ms are > 0, those values are used; otherwise defaults are
// derived from server.request_timeout_ms (read: infer budget + 2m for body, write: budget + 30s, idle: 2m).
func HTTPLayerTimeouts(cfg *config.Config) (read, write, idle time.Duration) {
	ms := cfg.Server.RequestTimeoutMS
	if ms <= 0 {
		ms = 30000
	}
	req := time.Duration(ms) * time.Millisecond
	read = req + 2*time.Minute
	write = req + 30*time.Second
	idle = 2 * time.Minute
	if x := cfg.Server.HTTP.ReadTimeoutMS; x > 0 {
		read = time.Duration(x) * time.Millisecond
	}
	if x := cfg.Server.HTTP.WriteTimeoutMS; x > 0 {
		write = time.Duration(x) * time.Millisecond
	}
	if x := cfg.Server.HTTP.IdleTimeoutMS; x > 0 {
		idle = time.Duration(x) * time.Millisecond
	}
	return read, write, idle
}

func (s *Server) inferRequestTimeout() time.Duration {
	if s.cfg.Server.RequestTimeoutMS <= 0 {
		return 30 * time.Second
	}
	return time.Duration(s.cfg.Server.RequestTimeoutMS) * time.Millisecond
}

// inferBudgetHTTPStatus returns 504 only when the infer-scoped context hit its deadline.
// Shorter per-backend timeouts also surface as context.DeadlineExceeded but must stay 502.
func inferBudgetHTTPStatus(ctx context.Context, err error) (status int, code string, ok bool) {
	if err == nil {
		return 0, "", false
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		return 0, "", false
	}
	if ctx.Err() == nil {
		return 0, "", false
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, errCodeGatewayTimeout, true
	}
	return 0, "", false
}

func New(cfg *config.Config) *Server {
	return NewWithDependencies(cfg, slo.NewMemoryEngineFromConfig(cfg.SLO), telemetry.NewExporterFromConfig(cfg.Telemetry))
}

func NewWithDependencies(cfg *config.Config, sloEngine interfaces.SLOEngine, telemetryExporter interfaces.TelemetryExporter) *Server {
	adapters := make(map[string]interfaces.BackendAdapter, len(cfg.Backends))
	for _, backend := range cfg.Backends {
		adapter, ok := buildAdapter(backend)
		if !ok {
			log.Printf("event=adapter_init_skipped backend=%s type=%s reason=%q", backend.Name, backend.Type, "unsupported backend type")
			continue
		}
		adapters[backend.Name] = adapter
	}

	costEngine := cost.NewSimpleEngine()

	srv := &Server{
		cfg:         cfg,
		policy:      policy.NewBasicEngine(cfg),
		router:      router.NewRuleRouter(cfg, costEngine),
		reliability: reliability.NewManager(cfg, adapters),
		sloEngine:   sloEngine,
		telemetry:   telemetryExporter,
		adapters:    adapters,
	}
	srv.initPrometheusMetrics()
	return srv
}

func buildAdapter(backend config.BackendConfig) (interfaces.BackendAdapter, bool) {
	switch backend.Type {
	case "mock":
		return mock.New(backend), true
	case "vllm", "openai", "openai_compatible":
		return vllm.New(backend), true
	default:
		return nil, false
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/status", s.status)
	mux.HandleFunc("/metrics", s.metrics)
	mux.HandleFunc("/infer", s.infer)
	return s.withOptionalInfercoreAuth(s.withRequestLoggingAndMetrics(mux))
}

// Shutdown releases telemetry resources (e.g. OTLP batch flush). Call on process exit.
func (s *Server) Shutdown(ctx context.Context) error {
	type shutdowner interface {
		Shutdown(context.Context) error
	}
	if x, ok := s.telemetry.(shutdowner); ok {
		return x.Shutdown(ctx)
	}
	return nil
}

func (s *Server) effectiveInfercoreAPIKey() string {
	if k := strings.TrimSpace(os.Getenv("INFERCORE_API_KEY")); k != "" {
		return k
	}
	return strings.TrimSpace(s.cfg.Server.InfercoreAPIKey)
}

func (s *Server) withOptionalInfercoreAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := s.effectiveInfercoreAPIKey()
		if key == "" || r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		if !infercoreAuthOK(r, key) {
			writeError(w, http.StatusUnauthorized, "", errCodeUnauthorized, "invalid or missing API key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func infercoreAuthOK(r *http.Request, want string) bool {
	if v := strings.TrimSpace(r.Header.Get("X-InferCore-Api-Key")); v != "" && v == want {
		return true
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	fields := strings.Fields(auth)
	if len(fields) == 2 && strings.EqualFold(fields[0], "Bearer") && fields[1] == want {
		return true
	}
	return false
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	health := s.cachedBackendHealth(r.Context())
	backends := make([]map[string]string, 0, len(s.cfg.Backends))
	for _, backend := range s.cfg.Backends {
		status := "unhealthy"
		if h, ok := health[backend.Name]; ok && h {
			status = "healthy"
		}
		backends = append(backends, map[string]string{
			"name":   backend.Name,
			"status": status,
		})
	}

	sig := s.CurrentSignals()
	writeJSON(w, http.StatusOK, map[string]any{
		"service":         "infercore",
		"backends":        backends,
		"queue_depth":     s.inferInflight.Load(),
		"telemetry":       s.telemetryStatus(),
		"scaling_signals": sig,
	})
}

func cloneBoolMap(m map[string]bool) map[string]bool {
	if m == nil {
		return nil
	}
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// beginInferLoad enforces reliability.overload: "reject" returns reject=true without consuming a slot;
// "degrade" allows the request and sets overloadDegrade when at/above the limit. QueueLimit <= 0 disables checks.
// Concurrency slots are acquired with CAS so active /infer calls cannot exceed the limit (reject mode).
func (s *Server) beginInferLoad() (done func(), overloadDegrade bool, reject bool) {
	nop := func() {}
	limit := s.cfg.Reliability.Overload.QueueLimit
	if limit <= 0 {
		s.inferInflight.Add(1)
		return func() { s.inferInflight.Add(-1) }, false, false
	}
	action := strings.ToLower(strings.TrimSpace(s.cfg.Reliability.Overload.Action))
	if action == "" {
		action = "degrade"
	}
	lim := int32(limit)
	if action == "reject" {
		for {
			cur := s.inferInflight.Load()
			if cur >= lim {
				return nop, false, true
			}
			if s.inferInflight.CompareAndSwap(cur, cur+1) {
				return func() { s.inferInflight.Add(-1) }, false, false
			}
		}
	}
	for {
		cur := s.inferInflight.Load()
		degraded := cur >= lim
		if s.inferInflight.CompareAndSwap(cur, cur+1) {
			return func() { s.inferInflight.Add(-1) }, degraded, false
		}
	}
}

// cachedBackendHealth probes adapter.Health per backend with TTL cache (shared by routing and /status).
func (s *Server) cachedBackendHealth(ctx context.Context) map[string]bool {
	ttl := time.Duration(s.cfg.Server.HealthCacheTTLMS) * time.Millisecond
	if ttl <= 0 {
		ttl = 2 * time.Second
	}
	perCheck := time.Duration(s.cfg.Server.HealthCheckPerMS) * time.Millisecond
	if perCheck <= 0 {
		perCheck = 1500 * time.Millisecond
	}

	if snap := s.peekFreshHealthSnapshot(ttl); snap != nil {
		return snap
	}

	v, _, _ := s.healthFlight.Do("backend_health", func() (interface{}, error) {
		if snap := s.peekFreshHealthSnapshot(ttl); snap != nil {
			return snap, nil
		}
		out := s.probeBackendHealth(perCheck)
		s.healthMu.Lock()
		s.healthSnapshot = out
		s.healthAt = time.Now()
		s.healthMu.Unlock()
		return out, nil
	})
	m, _ := v.(map[string]bool)
	return cloneBoolMap(m)
}

func (s *Server) peekFreshHealthSnapshot(ttl time.Duration) map[string]bool {
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	if s.healthSnapshot != nil && time.Since(s.healthAt) < ttl {
		return cloneBoolMap(s.healthSnapshot)
	}
	return nil
}

// probeBackendHealth runs Health with per-backend timeouts independent of the caller context
// (e.g. short /infer request_timeout_ms must not cancel health probes).
func (s *Server) probeBackendHealth(perCheck time.Duration) map[string]bool {
	out := make(map[string]bool, len(s.cfg.Backends))
	for _, b := range s.cfg.Backends {
		adapter, ok := s.adapters[b.Name]
		if !ok {
			out[b.Name] = false
			continue
		}
		hctx, cancel := context.WithTimeout(context.Background(), perCheck)
		err := adapter.Health(hctx)
		cancel()
		out[b.Name] = err == nil
	}
	return out
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Telemetry.MetricsEnabled {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = fmt.Fprintln(w, "# infercore metrics disabled by telemetry.metrics_enabled=false")
		return
	}

	promhttp.HandlerFor(s.promReg, promhttp.HandlerOpts{
		EnableOpenMetrics: false,
	}).ServeHTTP(w, r)
}

func (s *Server) infer(w http.ResponseWriter, r *http.Request) {
	traceID := traceutil.NewTraceID()
	spanID := traceutil.NewSpanID()
	traceStart := time.Now()

	if r.Method != http.MethodPost {
		s.emitInferTrace(traceID, spanID, traceStart, "", "", "", "method_not_allowed")
		writeError(w, http.StatusMethodNotAllowed, "", errCodeMethodNotAllowed, "method not allowed")
		return
	}

	var req types.InferenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.emitInferTrace(traceID, spanID, traceStart, "", "", "", "invalid_json")
		writeError(w, http.StatusBadRequest, "", errCodeInvalidRequest, "invalid JSON request body")
		return
	}

	if strings.TrimSpace(req.TenantID) == "" {
		s.emitInferTrace(traceID, spanID, traceStart, "", "", "", "missing_tenant_id")
		writeError(w, http.StatusBadRequest, "", errCodeInvalidRequest, "tenant_id is required")
		return
	}
	if strings.TrimSpace(req.TaskType) == "" {
		s.emitInferTrace(traceID, spanID, traceStart, "", req.TenantID, "", "missing_task_type")
		writeError(w, http.StatusBadRequest, "", errCodeInvalidRequest, "task_type is required")
		return
	}
	if req.Input == nil {
		s.emitInferTrace(traceID, spanID, traceStart, "", req.TenantID, "", "missing_input")
		writeError(w, http.StatusBadRequest, "", errCodeInvalidRequest, "input is required")
		return
	}
	if req.Options.MaxTokens <= 0 {
		s.emitInferTrace(traceID, spanID, traceStart, "", req.TenantID, "", "invalid_max_tokens")
		writeError(w, http.StatusBadRequest, "", errCodeInvalidOptions, "options.max_tokens must be > 0")
		return
	}

	requestID := uuid.NewString()
	s.inferReqCounter.Inc()
	now := time.Now().UnixMilli()
	req.RequestID = requestID

	inferCtx, cancel := context.WithTimeout(r.Context(), s.inferRequestTimeout())
	defer cancel()

	policyDecision, err := s.policy.Evaluate(inferCtx, req)
	if err != nil {
		if st, ec, ok := inferBudgetHTTPStatus(inferCtx, err); ok {
			s.noteTimeoutForScaling(err)
			s.emitInferTrace(traceID, spanID, traceStart, requestID, req.TenantID, "", "gateway_timeout")
			writeError(w, st, requestID, ec, "inference request deadline exceeded")
			return
		}
		s.emitInferTrace(traceID, spanID, traceStart, requestID, req.TenantID, "", "policy_error")
		writeError(w, http.StatusInternalServerError, requestID, errCodePolicyError, err.Error())
		return
	}
	if !policyDecision.Allowed {
		log.Printf("event=policy_rejected request_id=%s tenant_id=%s reason=%q", requestID, req.TenantID, policyDecision.Reason)
		s.emitInferTrace(traceID, spanID, traceStart, requestID, req.TenantID, "", "policy_rejected")
		writeError(w, http.StatusTooManyRequests, requestID, errCodePolicyRejected, policyDecision.Reason)
		return
	}
	req = policyDecision.Normalized

	release, overloadDegrade, rejectOverload := s.beginInferLoad()
	if rejectOverload {
		log.Printf("event=overload_rejected request_id=%s tenant_id=%s", requestID, req.TenantID)
		s.emitInferTrace(traceID, spanID, traceStart, requestID, req.TenantID, "", "overload")
		writeError(w, http.StatusServiceUnavailable, requestID, errCodeOverload, "inference concurrency limit exceeded")
		return
	}
	defer release()

	health := s.cachedBackendHealth(inferCtx)
	primary, err := s.router.SelectRoute(inferCtx, req, types.RuntimeState{
		QueueDepth:      int(s.inferInflight.Load()),
		BackendHealth:   health,
		OverloadDegrade: overloadDegrade,
	})
	if err != nil {
		if st, ec, ok := inferBudgetHTTPStatus(inferCtx, err); ok {
			s.noteTimeoutForScaling(err)
			s.emitInferTrace(traceID, spanID, traceStart, requestID, req.TenantID, "", "gateway_timeout")
			writeError(w, st, requestID, ec, "inference request deadline exceeded")
			return
		}
		s.emitInferTrace(traceID, spanID, traceStart, requestID, req.TenantID, "", "route_error")
		writeError(w, http.StatusInternalServerError, requestID, errCodeRouteError, err.Error())
		return
	}

	fallback := s.buildFallback(primary.BackendName, health)
	start := time.Now()
	s.sloEngine.RecordStart(requestID)
	execution, err := s.reliability.ExecuteWithFallback(inferCtx, req, primary, fallback)
	if err != nil {
		s.noteTimeoutForScaling(err)
		if st, ec, ok := inferBudgetHTTPStatus(inferCtx, err); ok {
			log.Printf("event=infer_deadline request_id=%s tenant_id=%s backend=%s", requestID, req.TenantID, primary.BackendName)
			s.emitInferTrace(traceID, spanID, traceStart, requestID, req.TenantID, primary.BackendName, "gateway_timeout")
			writeError(w, st, requestID, ec, "inference request deadline exceeded")
			return
		}
		log.Printf("event=execution_failed request_id=%s tenant_id=%s backend=%s error=%q", requestID, req.TenantID, primary.BackendName, err.Error())
		s.emitInferTrace(traceID, spanID, traceStart, requestID, req.TenantID, primary.BackendName, "execution_failed")
		writeError(w, http.StatusBadGateway, requestID, errCodeExecutionFailed, err.Error())
		return
	}

	s.writeInferSuccess(w, inferSuccessParams{
		traceID:         traceID,
		spanID:          spanID,
		traceStart:      traceStart,
		requestID:       requestID,
		nowUnixMs:       now,
		req:             req,
		policyDecision:  policyDecision,
		primary:         primary,
		execution:       execution,
		executionStart:  start,
		overloadDegrade: overloadDegrade,
	})
}

type inferSuccessParams struct {
	traceID         string
	spanID          string
	traceStart      time.Time
	requestID       string
	nowUnixMs       int64
	req             types.InferenceRequest
	policyDecision  types.PolicyDecision
	primary         types.RouteDecision
	execution       types.ExecutionResult
	executionStart  time.Time
	overloadDegrade bool
}

func (s *Server) writeInferSuccess(w http.ResponseWriter, p inferSuccessParams) {
	chosenBackendCfg, _ := s.cfg.BackendByName(p.execution.BackendName)
	endWall := time.Now()
	latency := endWall.Sub(p.executionStart).Milliseconds()

	firstTok := endWall
	compAt := endWall
	if p.execution.Timing != nil {
		if p.execution.Timing.TTFTMs > 0 {
			firstTok = p.executionStart.Add(time.Duration(p.execution.Timing.TTFTMs) * time.Millisecond)
		}
		if p.execution.Timing.CompletionLatencyMs > 0 {
			compAt = p.executionStart.Add(time.Duration(p.execution.Timing.CompletionLatencyMs) * time.Millisecond)
		}
	}
	s.sloEngine.RecordFirstToken(p.requestID, firstTok)
	s.sloEngine.RecordCompletion(p.requestID, compAt)

	resp := types.InferenceResponse{
		RequestID:         p.requestID,
		TraceID:           p.traceID,
		SelectedBackend:   p.execution.BackendName,
		RouteReason:       p.primary.Reason,
		PolicyReason:      p.policyDecision.Reason,
		EffectivePriority: p.req.Priority,
		Status:            p.execution.Status,
		Result:            p.execution.Output,
		Metrics: types.InferenceMetrics{
			TTFTMs:               0,
			TPOTMs:               0,
			CompletionLatencyMs:  latency,
			EstimatedCost:        chosenBackendCfg.Cost.Unit,
			QueueWaitTimeMs:      0,
			ServerReceivedAtUnix: p.nowUnixMs,
		},
		Fallback: types.FallbackState{
			Triggered: p.execution.UsedFallback,
			Reason:    fallbackReason(p.execution.UsedFallback),
		},
		Degrade: types.DegradeState{
			Triggered: false,
		},
	}
	if p.execution.UsedFallback {
		s.sloEngine.RecordFallback(p.requestID, "execution_fallback")
		log.Printf("event=fallback_triggered request_id=%s tenant_id=%s primary=%s selected=%s", p.requestID, p.req.TenantID, p.primary.BackendName, p.execution.BackendName)
	}

	snapshot := s.sloEngine.Snapshot(p.requestID)
	resp.Metrics.TTFTMs = snapshot.TTFTMs
	resp.Metrics.TPOTMs = snapshot.TPOTMs
	resp.Metrics.CompletionLatencyMs = snapshot.CompletionLatencyMs
	if p.execution.Timing != nil && p.execution.Timing.TPOTMs > 0 {
		resp.Metrics.TPOTMs = p.execution.Timing.TPOTMs
	}
	resp.Fallback.Triggered = snapshot.FallbackTriggered
	resp.Degrade = deriveDegradeState(p.execution.Output)
	if p.overloadDegrade {
		const msg = "load shedding: cost optimization skipped due to concurrency limit"
		if resp.Degrade.Triggered {
			resp.Degrade.Reason = resp.Degrade.Reason + "; " + msg
		} else {
			resp.Degrade = types.DegradeState{Triggered: true, Reason: msg}
		}
	}

	s.emitTelemetryMetric("infercore_ttft_ms", float64(resp.Metrics.TTFTMs), map[string]string{
		"tenant_id": p.req.TenantID,
		"backend":   p.execution.BackendName,
	})
	s.emitTelemetryMetric("infercore_completion_latency_ms", float64(resp.Metrics.CompletionLatencyMs), map[string]string{
		"tenant_id": p.req.TenantID,
		"backend":   p.execution.BackendName,
	})
	s.emitTelemetryEvent(types.Event{
		Name:      "infer_request_completed",
		Timestamp: time.Now().UnixMilli(),
		Labels: map[string]string{
			"request_id": p.requestID,
			"tenant_id":  p.req.TenantID,
			"backend":    p.execution.BackendName,
			"status":     p.execution.Status,
		},
	})
	s.emitInferTrace(p.traceID, p.spanID, p.traceStart, p.requestID, p.req.TenantID, p.execution.BackendName, "success")
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) buildFallback(primary string, health map[string]bool) []types.RouteDecision {
	out := make([]types.RouteDecision, 0)
	for _, rule := range s.cfg.Reliability.FallbackRules {
		if rule.FromBackend != primary {
			continue
		}
		backendCfg, ok := s.cfg.BackendByName(rule.FallbackTo)
		if !ok {
			continue
		}
		if !router.BackendHealthOK(health, backendCfg.Name) {
			continue
		}
		out = append(out, types.RouteDecision{
			BackendName:   backendCfg.Name,
			Reason:        "fallback-from-" + primary,
			EstimatedCost: backendCfg.Cost.Unit,
		})
	}
	return out
}

func fallbackReason(triggered bool) string {
	if triggered {
		return "primary backend execution failed, fallback applied"
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, requestID, code, message string) {
	writeJSON(w, status, map[string]any{
		"request_id": requestID,
		"status":     "error",
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

func (s *Server) telemetryStatus() map[string]any {
	type statusProvider interface {
		StatusSummary() map[string]any
	}
	if p, ok := s.telemetry.(statusProvider); ok {
		return p.StatusSummary()
	}
	return map[string]any{"type": "unknown"}
}

func (s *Server) emitInferTrace(traceID, spanID string, start time.Time, requestID, tenantID, backend, result string) {
	if !s.cfg.Telemetry.TracingEnabled {
		return
	}
	s.telemetry.EmitTrace(types.TraceRecord{
		TraceID:   traceID,
		SpanID:    spanID,
		Name:      "infer_request",
		StartUnix: start.UnixMilli(),
		EndUnix:   time.Now().UnixMilli(),
		Labels: map[string]string{
			"request_id": requestID,
			"tenant_id":  tenantID,
			"backend":    backend,
			"result":     result,
		},
	})
}

func (s *Server) emitTelemetryMetric(name string, value float64, labels map[string]string) {
	if !s.cfg.Telemetry.MetricsEnabled {
		return
	}
	s.telemetry.EmitMetric(name, value, labels)
}

func (s *Server) emitTelemetryEvent(event types.Event) {
	s.telemetry.EmitEvent(event)
}

func deriveDegradeState(output map[string]any) types.DegradeState {
	if output == nil {
		return types.DegradeState{Triggered: false}
	}
	streamDegraded, _ := output["stream_degraded"].(bool)
	if streamDegraded {
		return types.DegradeState{
			Triggered: true,
			Reason:    "stream request degraded to non-stream mode",
		}
	}
	return types.DegradeState{Triggered: false}
}

func (s *Server) withRequestLoggingAndMetrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)

		statusCode := rec.statusCode
		s.recordHTTPMetric(r.URL.Path, r.Method, statusCode)
		log.Printf("event=http_request path=%s method=%s status=%d latency_ms=%d", r.URL.Path, r.Method, statusCode, time.Since(start).Milliseconds())
	})
}

func (s *Server) recordHTTPMetric(path, method string, statusCode int) {
	if s.httpReqCounter == nil {
		return
	}
	s.httpReqCounter.WithLabelValues(path, method, strconv.Itoa(statusCode)).Inc()
}

func (s *Server) initPrometheusMetrics() {
	reg := prometheus.NewRegistry()
	s.inferReqCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "infercore",
		Name:      "requests_total",
		Help:      "Total /infer requests that passed validation and were assigned a request_id",
	})
	s.httpReqCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "infercore",
		Name:      "http_requests_total",
		Help:      "Total HTTP requests by path, method, and status code",
	}, []string{"path", "method", "status"})
	inflightGauge := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "infercore",
		Name:      "infer_inflight",
		Help:      "Current /infer executions in flight (after overload admission)",
	}, func() float64 { return float64(s.inferInflight.Load()) })
	ttftGauge := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "infercore",
		Name:      "scaling_ttft_degradation_ratio",
		Help:      "Rolling TTFT degradation ratio from in-memory SLO store (>1 suggests recent slowdown)",
	}, func() float64 { return s.CurrentSignals().TTFTDegradationRatio })
	fallbackGauge := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "infercore",
		Name:      "scaling_recent_fallback_rate",
		Help:      "Fraction of recent completed requests that recorded fallback (in-memory SLO window)",
	}, func() float64 { return s.CurrentSignals().RecentFallbackRate })
	timeoutGauge := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: "infercore",
		Name:      "scaling_timeout_spike",
		Help:      "1 if infer timeouts in the last rolling minute exceeded threshold, else 0",
	}, func() float64 {
		if s.timeoutSpikeActive() {
			return 1
		}
		return 0
	})
	reg.MustRegister(s.inferReqCounter, s.httpReqCounter, inflightGauge, ttftGauge, fallbackGauge, timeoutGauge)
	s.promReg = reg
}

// CurrentSignals implements interfaces.ScalingSignalProvider for autoscaler-facing hints.
func (s *Server) CurrentSignals() types.ScalingSignals {
	ttftRatio := 1.0
	var fbRate float64
	if me, ok := s.sloEngine.(*slo.MemoryEngine); ok {
		ttftRatio, fbRate = me.ScalingAggregates()
	}
	return types.ScalingSignals{
		QueueDepth:             int(s.inferInflight.Load()),
		TimeoutSpike:           s.timeoutSpikeActive(),
		TTFTDegradationRatio:   ttftRatio,
		RecentFallbackRate:     fbRate,
		BackendSaturationHints: s.backendSaturationHints(),
	}
}

func (s *Server) backendSaturationHints() []string {
	out := make([]string, 0)
	for _, b := range s.cfg.Backends {
		if b.MaxConcurrency > 0 {
			out = append(out, fmt.Sprintf("%s:max_concurrency=%d", b.Name, b.MaxConcurrency))
		}
	}
	return out
}

func (s *Server) noteTimeoutForScaling(err error) {
	if err == nil {
		return
	}
	if errors.Is(err, context.DeadlineExceeded) {
		s.recordTimeoutSpike()
		return
	}
	var ue *upstream.Error
	if errors.As(err, &ue) && ue.Kind == upstream.KindTimeout {
		s.recordTimeoutSpike()
	}
}

func (s *Server) recordTimeoutSpike() {
	s.timeoutSpikeMu.Lock()
	defer s.timeoutSpikeMu.Unlock()
	now := time.Now()
	if now.Sub(s.timeoutSpikeWin) > time.Minute {
		s.timeoutSpikeWin = now
		s.timeoutSpikeCnt = 0
	}
	s.timeoutSpikeCnt++
}

func (s *Server) timeoutSpikeActive() bool {
	s.timeoutSpikeMu.Lock()
	defer s.timeoutSpikeMu.Unlock()
	if time.Since(s.timeoutSpikeWin) > time.Minute {
		return false
	}
	return s.timeoutSpikeCnt >= minTimeoutsPerMinuteForSpike
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

var _ interfaces.ScalingSignalProvider = (*Server)(nil)
