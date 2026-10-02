// Package app berisi logika start/stop tiga binary (api, worker, migrate) tanpa bergantung
// pada pgx/go-redis: dependensi eksternal masuk lewat interface Dependencies sehingga alur
// fail-fast dan graceful shutdown bisa dites dengan fake. Adapter nyata ada di internal/infra.
package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"

	"platform/backend/internal/health"
	"platform/backend/internal/httpserver"
	"platform/backend/pkg/config"
	"platform/backend/pkg/logger"
)

// Exit code proses.
const (
	ExitOK     = 0
	ExitFailed = 1 // gagal saat berjalan (dependensi, bind port, shutdown tidak tuntas)
	ExitConfig = 2 // konfigurasi/argumen tidak valid — terjadi SEBELUM port di-bind
)

// Resource adalah dependensi eksternal yang sudah dibuka (pool PostgreSQL, klien Redis).
type Resource struct {
	Name  string
	Check func(context.Context) error // dipakai /health/ready
	Close func()
}

// Dependencies membuka dependensi eksternal. Implementasi nyata: internal/infra.
// Open TIDAK boleh memblokir menunggu server siap: server yang lambat dilaporkan
// oleh /health/ready, bukan mencegah proses start.
type Dependencies interface {
	Postgres(ctx context.Context, name, url string) (Resource, error)
	Redis(ctx context.Context, name, url string) (Resource, error)
}

// EnvMap mengubah os.Environ() menjadi map.
func EnvMap(environ []string) map[string]string {
	m := make(map[string]string, len(environ))
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

func newLogger(w io.Writer, cfg *config.Config, binary string) *slog.Logger {
	level, _ := logger.ParseLevel(cfg.LogLevel) // sudah divalidasi oleh config
	return logger.New(w, level).With("binary", binary)
}

// resources mengumpulkan Resource yang dibuka dan menutupnya dalam urutan terbalik.
type resources struct {
	log  *slog.Logger
	list []Resource
}

func (r *resources) add(res Resource) { r.list = append(r.list, res) }

func (r *resources) closeAll() {
	for i := len(r.list) - 1; i >= 0; i-- {
		r.log.Info("menutup dependensi", "name", r.list[i].Name)
		r.list[i].Close()
	}
	r.list = nil
}

func (r *resources) checkers() []health.Checker {
	out := make([]health.Checker, 0, len(r.list))
	for _, res := range r.list {
		out = append(out, health.NewChecker(res.Name, res.Check))
	}
	return out
}

// RunAPI menjalankan cmd/api. ln opsional (untuk tes); bila nil, mendengarkan di :PORT.
// Urutan: validasi konfigurasi -> buka dependensi -> bind port. Konfigurasi buruk selalu
// keluar dengan ExitConfig sebelum port di-bind.
func RunAPI(ctx context.Context, env map[string]string, deps Dependencies, stderr io.Writer, ln net.Listener) int {
	cfg, err := config.LoadFrom(config.RoleAPI, env)
	if err != nil {
		fmt.Fprintf(stderr, "api: %v\n", err)
		return ExitConfig
	}
	log := newLogger(stderr, cfg, "api")
	log.Info("api starting", "config", cfg)

	res := &resources{log: log}
	defer res.closeAll()

	for _, open := range []func() (Resource, error){
		func() (Resource, error) { return deps.Postgres(ctx, "postgres", cfg.DatabaseURL) },
		func() (Resource, error) { return deps.Redis(ctx, "redis", cfg.RedisURL) },
	} {
		r, err := open()
		if err != nil {
			log.Error("gagal membuka dependensi", "error", err)
			return ExitFailed
		}
		res.add(r)
	}

	handler := httpserver.NewHandler(httpserver.Options{
		Logger:         log,
		TrustedProxies: cfg.TrustedProxyPrefixes(),
		ReadyCheckers:  res.checkers(),
	})
	srv := httpserver.NewServer(fmt.Sprintf(":%d", cfg.Port), handler, log, httpserver.DefaultShutdownTimeout)

	if ln != nil {
		err = srv.Serve(ctx, ln)
	} else {
		err = srv.ListenAndServe(ctx)
	}
	if err != nil {
		log.Error("server berhenti dengan error", "error", err)
		return ExitFailed
	}
	log.Info("api stopped")
	return ExitOK
}

// RunWorker menjalankan cmd/worker. Di M00 worker hanya membuka dependensi dan menunggu sinyal
// berhenti; outbox relay dan job datang di M02/M05+ (docs/21). Shutdown: berhenti menerima
// pekerjaan baru, tutup koneksi.
func RunWorker(ctx context.Context, env map[string]string, deps Dependencies, stderr io.Writer) int {
	cfg, err := config.LoadFrom(config.RoleWorker, env)
	if err != nil {
		fmt.Fprintf(stderr, "worker: %v\n", err)
		return ExitConfig
	}
	log := newLogger(stderr, cfg, "worker")
	log.Info("worker starting", "config", cfg)

	res := &resources{log: log}
	defer res.closeAll()

	for _, open := range []func() (Resource, error){
		func() (Resource, error) { return deps.Postgres(ctx, "postgres", cfg.DatabaseURL) },
		func() (Resource, error) {
			return deps.Postgres(ctx, "postgres_maintenance", cfg.MaintenanceDatabaseURL)
		},
		func() (Resource, error) { return deps.Redis(ctx, "redis", cfg.RedisURL) },
	} {
		r, err := open()
		if err != nil {
			log.Error("gagal membuka dependensi", "error", err)
			return ExitFailed
		}
		res.add(r)
	}

	log.Info("worker ready; menunggu sinyal berhenti")
	<-ctx.Done()
	log.Info("worker shutdown dimulai")
	return ExitOK
}
