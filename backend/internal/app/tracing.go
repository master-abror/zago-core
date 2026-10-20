package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/master-abror/zago-core/backend/pkg/config"
	"github.com/master-abror/zago-core/backend/pkg/telemetry"
)

// tracingShutdownTimeout membatasi flush span terakhir saat proses berhenti.
const tracingShutdownTimeout = 5 * time.Second

// startTracing menyalakan tracing OTLP bila OTEL_EXPORTER_OTLP_ENDPOINT terisi; kosong = no-op
// (docs/14 §6). stop mem-flush span dengan context baru (ctx proses sudah dibatalkan saat berhenti)
// dan HARUS dijalankan paling akhir, jadi dideklarasikan paling awal (defer LIFO).
func startTracing(ctx context.Context, cfg *config.Config, service string, log *slog.Logger) (stop func(), err error) {
	shutdown, err := telemetry.SetupTracing(ctx, telemetry.TracingConfig{
		Endpoint:    cfg.OTLPEndpoint,
		Service:     service,
		Environment: cfg.Environment,
		Log:         log,
	})
	if err != nil {
		return nil, err
	}
	if cfg.OTLPEndpoint != "" {
		log.Info("tracing OTLP aktif", "service", service)
	}
	return func() {
		sctx, cancel := context.WithTimeout(context.Background(), tracingShutdownTimeout)
		defer cancel()
		if err := shutdown(sctx); err != nil {
			log.Warn("shutdown tracing tidak tuntas", "error", err)
		}
	}, nil
}
