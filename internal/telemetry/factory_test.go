package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/infercore/infercore/internal/config"
)

func TestNewExporterFromConfig_Log(t *testing.T) {
	exp := NewExporterFromConfig(config.TelemetryConfig{Exporter: "log"})
	if _, ok := exp.(*LogExporter); !ok {
		t.Fatalf("expected LogExporter, got %T", exp)
	}
}

func TestNewExporterFromConfig_OTLPStub(t *testing.T) {
	exp := NewExporterFromConfig(config.TelemetryConfig{
		Exporter:     "otlp-http-stub",
		OTLPEndpoint: "http://localhost:4318",
	})
	if _, ok := exp.(*OTLPHTTPStubExporter); !ok {
		t.Fatalf("expected OTLPHTTPStubExporter, got %T", exp)
	}
}

func TestNewExporterFromConfig_OTLPHTTP(t *testing.T) {
	exp := NewExporterFromConfig(config.TelemetryConfig{
		Exporter:      "otlp-http",
		OTLPEndpoint:  "http://localhost:4318/v1/traces",
		OTLPTimeoutMS: 500,
		OTLPFlushMS:   100,
	})
	o, ok := exp.(*OtelOTLPExporter)
	if !ok {
		t.Fatalf("expected OtelOTLPExporter, got %T", exp)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = o.Shutdown(ctx)
}

func TestNewExporterFromConfig_OTLPHTTPJSON(t *testing.T) {
	exp := NewExporterFromConfig(config.TelemetryConfig{
		Exporter:      "otlp-http-json",
		OTLPEndpoint:  "http://localhost:4318",
		OTLPTimeoutMS: 500,
		OTLPRetries:   1,
	})
	if _, ok := exp.(*OTLPHTTPExporter); !ok {
		t.Fatalf("expected OTLPHTTPExporter, got %T", exp)
	}
}
