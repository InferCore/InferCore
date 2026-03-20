package telemetry

import (
	"log"

	"github.com/infercore/infercore/internal/types"
)

type OTLPHTTPStubExporter struct {
	endpoint string
}

func NewOTLPHTTPStubExporter(endpoint string) *OTLPHTTPStubExporter {
	return &OTLPHTTPStubExporter{endpoint: endpoint}
}

func (e *OTLPHTTPStubExporter) EmitMetric(name string, value float64, labels map[string]string) {
	log.Printf("telemetry=otlp_http_stub kind=metric endpoint=%s name=%s value=%f labels=%v",
		e.endpoint, name, value, labels)
}

func (e *OTLPHTTPStubExporter) EmitEvent(event types.Event) {
	log.Printf("telemetry=otlp_http_stub kind=event endpoint=%s name=%s timestamp=%d labels=%v",
		e.endpoint, event.Name, event.Timestamp, event.Labels)
}

func (e *OTLPHTTPStubExporter) EmitTrace(trace types.TraceRecord) {
	log.Printf("telemetry=otlp_http_stub kind=trace endpoint=%s trace_id=%s span_id=%s name=%s start=%d end=%d labels=%v",
		e.endpoint, trace.TraceID, trace.SpanID, trace.Name, trace.StartUnix, trace.EndUnix, trace.Labels)
}

func (e *OTLPHTTPStubExporter) StatusSummary() map[string]any {
	return map[string]any{
		"type":     "otlp-http-stub",
		"endpoint": e.endpoint,
	}
}
