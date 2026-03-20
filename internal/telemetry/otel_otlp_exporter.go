package telemetry

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/types"
)

// OtelOTLPExporter sends OTLP/HTTP protobuf to a standard OpenTelemetry Collector
// (traces to /v1/traces, metrics to /v1/metrics).
type OtelOTLPExporter struct {
	tp       *sdktrace.TracerProvider
	mp       *sdkmetric.MeterProvider
	tracer   trace.Tracer
	meter    metric.Meter
	mu       sync.Mutex
	counters map[string]metric.Float64Counter
}

func NewOtelOTLPExporter(cfg config.TelemetryConfig) (*OtelOTLPExporter, error) {
	ctx := context.Background()
	base, err := normalizeOTLPBaseURL(cfg.OTLPEndpoint)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(cfg.OTLPTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceNameKey.String("infercore")),
	)
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	traceExp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(base),
		otlptracehttp.WithTimeout(timeout),
	)
	if err != nil {
		return nil, fmt.Errorf("otlp trace exporter: %w", err)
	}

	batchTimeout := time.Duration(cfg.OTLPFlushMS) * time.Millisecond
	if batchTimeout <= 0 {
		batchTimeout = time.Second
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp, sdktrace.WithBatchTimeout(batchTimeout)),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	tracer := tp.Tracer("infercore")

	metricExp, err := otlpmetrichttp.New(ctx,
		otlpmetrichttp.WithEndpointURL(base),
		otlpmetrichttp.WithTimeout(timeout),
	)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, fmt.Errorf("otlp metric exporter: %w", err)
	}

	flush := time.Duration(cfg.OTLPFlushMS) * time.Millisecond
	if flush <= 0 {
		flush = time.Second
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(flush))),
	)
	otel.SetMeterProvider(mp)
	meter := mp.Meter("infercore")

	return &OtelOTLPExporter{
		tp:       tp,
		mp:       mp,
		tracer:   tracer,
		meter:    meter,
		counters: make(map[string]metric.Float64Counter),
	}, nil
}

func normalizeOTLPBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty otlp endpoint")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" {
		u, err = url.Parse("http://" + raw)
		if err != nil {
			return "", err
		}
	}
	if u.Host == "" {
		return "", fmt.Errorf("invalid otlp endpoint host in %q", raw)
	}
	return strings.TrimRight(fmt.Sprintf("%s://%s", u.Scheme, u.Host), "/"), nil
}

func (e *OtelOTLPExporter) EmitMetric(name string, value float64, labels map[string]string) {
	e.mu.Lock()
	c, ok := e.counters[name]
	if !ok {
		var err error
		c, err = e.meter.Float64Counter(name)
		if err != nil {
			e.mu.Unlock()
			return
		}
		e.counters[name] = c
	}
	e.mu.Unlock()

	var attrs []attribute.KeyValue
	for k, v := range labels {
		attrs = append(attrs, attribute.String(k, v))
	}
	c.Add(context.Background(), value, metric.WithAttributes(attrs...))
}

func (e *OtelOTLPExporter) EmitEvent(event types.Event) {
	// Mapped to trace span events when a span is active; standalone events are dropped here.
	_ = event
}

func (e *OtelOTLPExporter) EmitTrace(tr types.TraceRecord) {
	_, span := e.tracer.Start(context.Background(), tr.Name,
		trace.WithTimestamp(time.UnixMilli(tr.StartUnix)),
		trace.WithSpanKind(trace.SpanKindServer),
	)
	span.SetAttributes(attribute.String("infercore.trace_id", tr.TraceID))
	span.SetAttributes(attribute.String("infercore.span_id", tr.SpanID))
	for k, v := range tr.Labels {
		span.SetAttributes(attribute.String(k, v))
	}
	span.End(trace.WithTimestamp(time.UnixMilli(tr.EndUnix)))
}

func (e *OtelOTLPExporter) StatusSummary() map[string]any {
	return map[string]any{
		"type":     "otlp-http",
		"protocol": "otlp-http-protobuf",
		"standard": true,
	}
}

// Shutdown flushes OTLP exporters (call on process exit).
func (e *OtelOTLPExporter) Shutdown(ctx context.Context) error {
	_ = e.mp.Shutdown(ctx)
	return e.tp.Shutdown(ctx)
}
