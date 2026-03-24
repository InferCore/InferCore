package inferexec

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/execution"
	"github.com/infercore/infercore/internal/types"
)

type stubPolicy struct {
	decision types.PolicyDecision
	err      error
}

func (s *stubPolicy) Evaluate(ctx context.Context, req types.AIRequest) (types.PolicyDecision, error) {
	if s.err != nil {
		return types.PolicyDecision{}, s.err
	}
	return s.decision, nil
}

type stubRouter struct {
	primary types.RouteDecision
	err     error
}

func (s *stubRouter) SelectRoute(ctx context.Context, req types.AIRequest, state types.RuntimeState) (types.RouteDecision, error) {
	return s.primary, s.err
}

type stubReliability struct {
	res types.ExecutionResult
	err error
}

func (s *stubReliability) ExecuteWithFallback(ctx context.Context, req types.AIRequest, primary types.RouteDecision, fallback []types.RouteDecision) (types.ExecutionResult, error) {
	return s.res, s.err
}

func (s *stubReliability) ExecuteWithFallbackOpts(ctx context.Context, req types.AIRequest, primary types.RouteDecision, fallback []types.RouteDecision, opts types.ReliabilityExecuteOptions) (types.ExecutionResult, error) {
	return s.res, s.err
}

type stubSLO struct{}

func (stubSLO) RecordStart(string) {}
func (stubSLO) RecordFirstToken(string, time.Time) {}
func (stubSLO) RecordCompletion(string, time.Time) {}
func (stubSLO) RecordFallback(string, string) {}
func (stubSLO) Snapshot(string) types.SLOSnapshot {
	return types.SLOSnapshot{}
}

type stubLedger struct {
	failCalls int
}

func (s *stubLedger) CreateRequest(ctx context.Context, traceID, requestID string, req types.AIRequest, now time.Time) {
}
func (s *stubLedger) Fail(ctx context.Context, requestID string) { s.failCalls++ }
func (s *stubLedger) UpdatePolicy(ctx context.Context, requestID string, snap []byte, routeReason, backend *string) {
}
func (s *stubLedger) MarkSuccess(ctx context.Context, requestID, backend, routeReason string) {}

func baseReq() *types.AIRequest {
	return &types.AIRequest{
		TenantID: "t1",
		TaskType: "chat",
		Priority: "normal",
		Input:    map[string]any{"text": "hi"},
		Options:  types.RequestOptions{Stream: false, MaxTokens: 128},
	}
}

func TestOrchestrator_PolicyRejected(t *testing.T) {
	req := baseReq()
	norm := types.NormalizeAIRequest(*req)
	o := &Orchestrator{
		Policy: &stubPolicy{
			decision: types.PolicyDecision{Allowed: false, Reason: "quota", Normalized: norm},
		},
		Config: &config.Config{},
		Ledger: &stubLedger{},
	}
	sw := &execution.StepWriter{Store: nil}
	out := o.Run(context.Background(), RunInput{
		RequestID: "rid",
		Req:       req,
		SW:        sw,
	})
	if out.Failure == nil || out.Failure.HTTPStatus != http.StatusTooManyRequests {
		t.Fatalf("got %+v", out)
	}
}

func TestOrchestrator_AgentNotImplemented(t *testing.T) {
	req := baseReq()
	req.RequestType = types.RequestTypeAgent
	norm := types.NormalizeAIRequest(*req)
	o := &Orchestrator{
		Policy: &stubPolicy{
			decision: types.PolicyDecision{Allowed: true, Reason: "ok", Normalized: norm},
		},
		Config:        &config.Config{},
		Ledger:        &stubLedger{},
		BeginInferLoad: func() (func(), bool, bool) { return func() {}, false, false },
		CachedHealth:   func(context.Context) map[string]bool { return nil },
		BuildFallback:  func(string, map[string]bool) []types.RouteDecision { return nil },
	}
	sw := &execution.StepWriter{Store: nil}
	out := o.Run(context.Background(), RunInput{RequestID: "rid", Req: req, SW: sw})
	if out.Failure == nil || out.Failure.ErrorCode != "agent_not_implemented" {
		t.Fatalf("got %+v", out)
	}
}

func TestOrchestrator_MisconfiguredMissingHooks(t *testing.T) {
	req := baseReq()
	norm := types.NormalizeAIRequest(*req)
	o := &Orchestrator{
		Policy: &stubPolicy{
			decision: types.PolicyDecision{Allowed: true, Reason: "ok", Normalized: norm},
		},
		Config: &config.Config{},
	}
	out := o.Run(context.Background(), RunInput{RequestID: "rid", Req: req, SW: &execution.StepWriter{Store: nil}})
	if out.Failure == nil || out.Failure.ErrorCode != "route_error" {
		t.Fatalf("got %+v", out)
	}
}

func TestOrchestrator_OverloadReject(t *testing.T) {
	req := baseReq()
	norm := types.NormalizeAIRequest(*req)
	o := &Orchestrator{
		Policy: &stubPolicy{
			decision: types.PolicyDecision{Allowed: true, Reason: "ok", Normalized: norm},
		},
		Config: &config.Config{},
		Ledger: &stubLedger{},
		BeginInferLoad: func() (func(), bool, bool) {
			return func() {}, false, true
		},
		CachedHealth: func(context.Context) map[string]bool { return map[string]bool{"b": true} },
		BuildFallback: func(string, map[string]bool) []types.RouteDecision { return nil },
	}
	out := o.Run(context.Background(), RunInput{RequestID: "rid", Req: req, SW: &execution.StepWriter{Store: nil}})
	if out.Failure == nil || out.Failure.ErrorCode != "overload" {
		t.Fatalf("got %+v", out)
	}
}

func TestOrchestrator_SuccessInference(t *testing.T) {
	req := baseReq()
	norm := types.NormalizeAIRequest(*req)
	primary := types.RouteDecision{BackendName: "be1", Reason: "rule", EstimatedCost: 1}
	execRes := types.ExecutionResult{
		Status:       "success",
		BackendName:  "be1",
		Output:       map[string]any{"text": "out"},
		UsedFallback: false,
	}
	o := &Orchestrator{
		Policy: &stubPolicy{
			decision: types.PolicyDecision{Allowed: true, Reason: "ok", Normalized: norm},
		},
		Router:        &stubRouter{primary: primary},
		Reliability:   &stubReliability{res: execRes},
		SLO:           stubSLO{},
		Config:        &config.Config{},
		Ledger:        &stubLedger{},
		BeginInferLoad: func() (func(), bool, bool) { return func() {}, false, false },
		CachedHealth:   func(context.Context) map[string]bool { return map[string]bool{"be1": true} },
		BuildFallback:  func(string, map[string]bool) []types.RouteDecision { return nil },
		InferInflight:  func() int32 { return 0 },
	}
	out := o.Run(context.Background(), RunInput{RequestID: "rid", Req: req, SW: &execution.StepWriter{Store: nil}})
	if out.Success == nil || out.Success.ExecRes.Output["text"] != "out" {
		t.Fatalf("got %+v", out)
	}
}

func TestOrchestrator_RAGErrorWithParse(t *testing.T) {
	req := baseReq()
	req.RequestType = types.RequestTypeRAG
	norm := types.NormalizeAIRequest(*req)
	primary := types.RouteDecision{BackendName: "be1", Reason: "rule", EstimatedCost: 1}
	o := &Orchestrator{
		Policy: &stubPolicy{
			decision: types.PolicyDecision{Allowed: true, Reason: "ok", Normalized: norm},
		},
		Router:      &stubRouter{primary: primary},
		Reliability: &stubReliability{},
		SLO:         stubSLO{},
		Config:      &config.Config{},
		Ledger:      &stubLedger{},
		BeginInferLoad: func() (func(), bool, bool) { return func() {}, false, false },
		CachedHealth:   func(context.Context) map[string]bool { return map[string]bool{"be1": true} },
		BuildFallback:  func(string, map[string]bool) []types.RouteDecision { return nil },
		InferInflight:  func() int32 { return 0 },
		RunRAG: func(ctx context.Context, sw *execution.StepWriter, req *types.AIRequest) error {
			return errors.New("rag boom")
		},
		ParseRAGError: func(err error) (trace string, httpStatus int, errCode, msg string, ok bool) {
			return "rag_trace", http.StatusBadRequest, "rag_not_configured", err.Error(), true
		},
	}
	out := o.Run(context.Background(), RunInput{RequestID: "rid", Req: req, SW: &execution.StepWriter{Store: nil}})
	if out.Failure == nil || out.Failure.HTTPStatus != http.StatusBadRequest {
		t.Fatalf("got %+v", out)
	}
}

func TestOrchestrator_PolicyDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()
	req := baseReq()
	o := &Orchestrator{
		Policy: &stubPolicy{
			err: context.DeadlineExceeded,
		},
		Config: &config.Config{},
		Ledger: &stubLedger{},
	}
	out := o.Run(ctx, RunInput{RequestID: "rid", Req: req, SW: &execution.StepWriter{Store: nil}})
	if out.Failure == nil || out.Failure.ErrorCode != "gateway_timeout" {
		t.Fatalf("got %+v", out)
	}
}

func TestOrchestrator_ExecutionFail(t *testing.T) {
	req := baseReq()
	norm := types.NormalizeAIRequest(*req)
	primary := types.RouteDecision{BackendName: "be1", Reason: "rule", EstimatedCost: 1}
	o := &Orchestrator{
		Policy: &stubPolicy{
			decision: types.PolicyDecision{Allowed: true, Reason: "ok", Normalized: norm},
		},
		Router:      &stubRouter{primary: primary},
		Reliability: &stubReliability{err: errors.New("exec failed")},
		SLO:         stubSLO{},
		Config:      &config.Config{},
		Ledger:      &stubLedger{},
		BeginInferLoad: func() (func(), bool, bool) { return func() {}, false, false },
		CachedHealth:   func(context.Context) map[string]bool { return map[string]bool{"be1": true} },
		BuildFallback:  func(string, map[string]bool) []types.RouteDecision { return nil },
		InferInflight:  func() int32 { return 0 },
	}
	out := o.Run(context.Background(), RunInput{RequestID: "rid", Req: req, SW: &execution.StepWriter{Store: nil}})
	if out.Failure == nil || out.Failure.ErrorCode != "execution_failed" {
		t.Fatalf("got %+v", out)
	}
}
