package telemetry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/infercore/infercore/internal/types"
)

type OTLPHTTPExporter struct {
	endpoint   string
	httpClient *http.Client
	retries    int
	batchSize  int
	flushEvery time.Duration

	mu             sync.Mutex
	buffer         map[string][]map[string]any
	lastFlush      time.Time
	sentBatches    int
	sentRecords    int
	lastError      string
	lastStatusCode int
}

func NewOTLPHTTPExporter(endpoint string, timeout time.Duration, retries, batchSize int, flushEvery time.Duration) *OTLPHTTPExporter {
	if timeout <= 0 {
		timeout = 1 * time.Second
	}
	if retries < 0 {
		retries = 0
	}
	if batchSize <= 0 {
		batchSize = 10
	}
	if flushEvery <= 0 {
		flushEvery = 1 * time.Second
	}
	return &OTLPHTTPExporter{
		endpoint: endpoint,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		retries:    retries,
		batchSize:  batchSize,
		flushEvery: flushEvery,
		buffer: map[string][]map[string]any{
			"metrics": {},
			"events":  {},
			"traces":  {},
		},
		lastFlush: time.Now(),
	}
}

func (e *OTLPHTTPExporter) EmitMetric(name string, value float64, labels map[string]string) {
	e.enqueue("metrics", map[string]any{
		"name":   name,
		"value":  value,
		"labels": labels,
	})
}

func (e *OTLPHTTPExporter) EmitEvent(event types.Event) {
	e.enqueue("events", map[string]any{
		"name":      event.Name,
		"timestamp": event.Timestamp,
		"labels":    event.Labels,
	})
}

func (e *OTLPHTTPExporter) EmitTrace(trace types.TraceRecord) {
	e.enqueue("traces", map[string]any{
		"trace_id":   trace.TraceID,
		"span_id":    trace.SpanID,
		"name":       trace.Name,
		"start_unix": trace.StartUnix,
		"end_unix":   trace.EndUnix,
		"labels":     trace.Labels,
	})
}

func (e *OTLPHTTPExporter) StatusSummary() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	return map[string]any{
		"type":              "otlp-http-json",
		"endpoint":          e.endpoint,
		"batch_size":        e.batchSize,
		"flush_interval_ms": e.flushEvery.Milliseconds(),
		"sent_batches":      e.sentBatches,
		"sent_records":      e.sentRecords,
		"buffered_metrics":  len(e.buffer["metrics"]),
		"buffered_events":   len(e.buffer["events"]),
		"buffered_traces":   len(e.buffer["traces"]),
		"last_error":        e.lastError,
		"last_status_code":  e.lastStatusCode,
	}
}

func (e *OTLPHTTPExporter) enqueue(kind string, record map[string]any) {
	e.mu.Lock()
	e.buffer[kind] = append(e.buffer[kind], record)
	shouldFlushNow := len(e.buffer[kind]) >= e.batchSize || time.Since(e.lastFlush) >= e.flushEvery
	e.mu.Unlock()
	if shouldFlushNow {
		e.flush()
	}
}

func (e *OTLPHTTPExporter) flush() {
	e.mu.Lock()
	buffers := map[string][]map[string]any{
		"metrics": append([]map[string]any(nil), e.buffer["metrics"]...),
		"events":  append([]map[string]any(nil), e.buffer["events"]...),
		"traces":  append([]map[string]any(nil), e.buffer["traces"]...),
	}
	e.buffer["metrics"] = nil
	e.buffer["events"] = nil
	e.buffer["traces"] = nil
	e.lastFlush = time.Now()
	e.mu.Unlock()

	for kind, records := range buffers {
		if len(records) == 0 {
			continue
		}
		endpoint := endpointForKind(e.endpoint, kind)
		payload := buildPayload(kind, records)
		if err := e.post(endpoint, payload); err != nil {
			e.mu.Lock()
			e.lastError = err.Error()
			e.mu.Unlock()
		} else {
			e.mu.Lock()
			e.sentBatches++
			e.sentRecords += len(records)
			e.lastError = ""
			e.mu.Unlock()
		}
	}
}

func (e *OTLPHTTPExporter) post(endpoint string, payload map[string]any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var lastErr error
	for attempt := 0; attempt <= e.retries; attempt++ {
		req, reqErr := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(raw))
		if reqErr != nil {
			return reqErr
		}
		req.Header.Set("Content-Type", "application/json")
		resp, doErr := e.httpClient.Do(req)
		if doErr != nil {
			lastErr = doErr
			continue
		}
		_ = resp.Body.Close()
		e.mu.Lock()
		e.lastStatusCode = resp.StatusCode
		e.mu.Unlock()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("otlp http exporter got status %d", resp.StatusCode)
	}
	return lastErr
}

func endpointForKind(base, kind string) string {
	base = strings.TrimRight(base, "/")
	if strings.Contains(base, "/v1/") {
		return base
	}
	return base + "/v1/" + kind
}

func buildPayload(kind string, records []map[string]any) map[string]any {
	switch kind {
	case "metrics":
		return map[string]any{"resourceMetrics": []map[string]any{{"metrics": records}}}
	case "events":
		return map[string]any{"resourceEvents": []map[string]any{{"events": records}}}
	default:
		return map[string]any{"resourceSpans": []map[string]any{{"spans": records}}}
	}
}
