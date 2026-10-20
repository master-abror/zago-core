package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/master-abror/zago-core/backend/internal/httpserver"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/pkg/telemetry"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// metricsShutdownTimeout membatasi pengurasan scrape yang sedang berjalan saat berhenti.
const metricsShutdownTimeout = 5 * time.Second

// setupAPIMetrics merakit registry metrik API: HTTP per pola rute, statistik pool, dan gauge
// outbox (dibaca dari database saat scrape, jadi tetap terlihat walau worker mati — docs/14 §5).
// appPool boleh nil (fake di tes): metrik pool/outbox dilewati.
func setupAPIMetrics(log *slog.Logger, appPool kernel.Pool) (*telemetry.Registry, *telemetry.HTTP, error) {
	reg := telemetry.NewRegistry(log)
	httpMetrics, err := reg.NewHTTP()
	if err != nil {
		return nil, nil, err
	}
	if appPool != nil {
		if err := registerPoolMetrics(reg, "postgres", appPool); err != nil {
			return nil, nil, err
		}
		if err := reg.RegisterOutbox(outboxReader(appPool)); err != nil {
			return nil, nil, err
		}
	}
	return reg, httpMetrics, nil
}

// setupWorkerMetrics merakit registry metrik worker: job, dead-letter, dan statistik kedua pool.
func setupWorkerMetrics(log *slog.Logger, appPool, maintenancePool kernel.Pool) (*telemetry.Registry, *telemetry.Worker, error) {
	reg := telemetry.NewRegistry(log)
	w, err := reg.NewWorker()
	if err != nil {
		return nil, nil, err
	}
	for name, pool := range map[string]kernel.Pool{"postgres": appPool, "postgres_maintenance": maintenancePool} {
		if pool == nil {
			continue
		}
		if err := registerPoolMetrics(reg, name, pool); err != nil {
			return nil, nil, err
		}
	}
	return reg, w, nil
}

// registerPoolMetrics mendaftarkan statistik pool; pool tanpa Stat() (fake di tes) dilewati.
func registerPoolMetrics(reg *telemetry.Registry, name string, pool kernel.Pool) error {
	if _, ok := kernel.StatsOf(pool); !ok {
		return nil
	}
	return reg.RegisterPool(name, func() telemetry.PoolStat {
		s, _ := kernel.StatsOf(pool)
		return telemetry.PoolStat{Acquired: s.Acquired, Idle: s.Idle, Max: s.Max}
	})
}

// outboxReader menghubungkan kernel.ReadOutboxStats ke gauge telemetry.
func outboxReader(db kernel.DBTX) telemetry.OutboxReader {
	return func(ctx context.Context) (telemetry.OutboxStats, error) {
		s, err := kernel.ReadOutboxStats(ctx, db)
		if err != nil {
			return telemetry.OutboxStats{}, err
		}
		return telemetry.OutboxStats{Pending: s.Pending, OldestPendingAge: s.OldestPendingAge}, nil
	}
}

// startMetricsServer membuka /metrics di :port (port TERPISAH dari API, tidak pernah publik —
// compose tidak mem-publish-nya). Bind dilakukan SINKRON: port bentrok menjadi error saat start,
// bukan kegagalan diam-diam di goroutine. Server berhenti saat ctx dibatalkan atau stop dipanggil;
// stop menunggu server benar-benar selesai, jadi aman dipanggil sebelum pool ditutup.
func startMetricsServer(ctx context.Context, port int, reg *telemetry.Registry, log *slog.Logger) (stop func(), err error) {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", addr, err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", reg.Handler())
	srv := httpserver.NewServer(addr, mux, log, metricsShutdownTimeout)

	sctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.Serve(sctx, ln); err != nil {
			log.Error("server metrics berhenti dengan error", "error", err)
		}
	}()
	log.Info("metrics aktif", "port", port)
	return func() {
		cancel()
		<-done
	}, nil
}

// onDeadHook merangkai hook dead-letter relay: mencatat ke system log, lalu menaikkan
// outbox_events_dead_total bila metrik worker aktif (wm nil = metrik nonaktif).
func onDeadHook(logDead func(context.Context, modulesdk.Event, error), wm *telemetry.Worker) func(context.Context, modulesdk.Event, error) {
	return func(ctx context.Context, ev modulesdk.Event, lastErr error) {
		logDead(ctx, ev, lastErr)
		if wm != nil {
			wm.OutboxDead()
		}
	}
}
