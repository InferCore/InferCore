package telemetry

import (
	"log"

	"github.com/infercore/infercore/internal/types"
)

type LogExporter struct{}

func NewLogExporter() *LogExporter {
	return &LogExporter{}
}

func (e *LogExporter) EmitMetric(name string, value float64, labels map[string]string) {
	log.Printf("telemetry=metric name=%s value=%f labels=%v", name, value, labels)
}

func (e *LogExporter) EmitEvent(event types.Event) {
	log.Printf("telemetry=event name=%s timestamp=%d labels=%v", event.Name, event.Timestamp, event.Labels)
}

func (e *LogExporter) EmitTrace(trace types.TraceRecord) {
	log.Printf("telemetry=trace trace_id=%s span_id=%s name=%s start=%d end=%d labels=%v",
		trace.TraceID, trace.SpanID, trace.Name, trace.StartUnix, trace.EndUnix, trace.Labels)
}

func (e *LogExporter) StatusSummary() map[string]any {
	return map[string]any{
		"type": "log",
	}
}
