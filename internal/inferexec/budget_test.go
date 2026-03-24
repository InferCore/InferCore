package inferexec

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestInferBudgetHTTPStatus_NoError(t *testing.T) {
	st, code, ok := InferBudgetHTTPStatus(context.Background(), nil)
	if ok || st != 0 || code != "" {
		t.Fatalf("expected no match, got status=%d code=%q ok=%v", st, code, ok)
	}
}

func TestInferBudgetHTTPStatus_NonDeadlineError(t *testing.T) {
	st, _, ok := InferBudgetHTTPStatus(context.Background(), errors.New("other"))
	if ok {
		t.Fatalf("expected no match for generic error")
	}
	if st != 0 {
		t.Fatalf("status=%d", st)
	}
}

func TestInferBudgetHTTPStatus_DeadlineExceededWithExpiredContext(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()
	st, code, ok := InferBudgetHTTPStatus(ctx, context.DeadlineExceeded)
	if !ok || st != http.StatusGatewayTimeout || code != "gateway_timeout" {
		t.Fatalf("got status=%d code=%q ok=%v", st, code, ok)
	}
}

func TestInferBudgetHTTPStatus_DeadlineErrorButContextStillOk(t *testing.T) {
	// err is deadline-shaped but ctx not cancelled -> no mapping
	st, _, ok := InferBudgetHTTPStatus(context.Background(), context.DeadlineExceeded)
	if ok || st != 0 {
		t.Fatalf("expected no match when ctx.Err() is nil, got ok=%v status=%d", ok, st)
	}
}
