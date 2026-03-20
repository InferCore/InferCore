package telemetry

import (
	"log"
	"time"

	"github.com/infercore/infercore/internal/config"
	"github.com/infercore/infercore/internal/interfaces"
)

func NewExporterFromConfig(cfg config.TelemetryConfig) interfaces.TelemetryExporter {
	switch cfg.Exporter {
	case "otlp-http":
		exp, err := NewOtelOTLPExporter(cfg)
		if err != nil {
			log.Printf("telemetry: otlp-http (OTel SDK) init failed, falling back to log exporter: %v", err)
			return NewLogExporter()
		}
		return exp
	case "otlp-http-json":
		return NewOTLPHTTPExporter(
			cfg.OTLPEndpoint,
			time.Duration(cfg.OTLPTimeoutMS)*time.Millisecond,
			cfg.OTLPRetries,
			cfg.OTLPBatchSize,
			time.Duration(cfg.OTLPFlushMS)*time.Millisecond,
		)
	case "otlp-http-stub":
		return NewOTLPHTTPStubExporter(cfg.OTLPEndpoint)
	case "log":
		fallthrough
	default:
		return NewLogExporter()
	}
}
