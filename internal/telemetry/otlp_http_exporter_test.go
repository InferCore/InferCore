package telemetry

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/infercore/infercore/internal/types"
)

func TestOTLPHTTPExporter_PostsPayload(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	exp := NewOTLPHTTPExporter(srv.URL, 500*time.Millisecond, 0, 1, 1*time.Second)
	exp.EmitMetric("m1", 1.0, map[string]string{"k": "v"})

	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected one HTTP call, got %d", calls)
	}
}

func TestOTLPHTTPExporter_Retries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := atomic.AddInt32(&calls, 1)
		if current <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	exp := NewOTLPHTTPExporter(srv.URL, 500*time.Millisecond, 2, 1, 1*time.Second)
	exp.EmitEvent(types.Event{
		Name:      "event",
		Timestamp: time.Now().UnixMilli(),
		Labels:    map[string]string{"ok": "1"},
	})

	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("expected three calls with retries, got %d", calls)
	}
}

func TestOTLPHTTPExporter_StatusSummary(t *testing.T) {
	exp := NewOTLPHTTPExporter("http://localhost:4318", 500*time.Millisecond, 1, 10, 2*time.Second)
	summary := exp.StatusSummary()
	if summary["type"] != "otlp-http-json" {
		t.Fatalf("unexpected exporter type summary: %v", summary["type"])
	}
}
