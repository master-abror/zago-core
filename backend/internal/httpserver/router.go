// Package httpserver merakit router chi, middleware dasar, dan server HTTP dengan graceful
// shutdown (docs/10 §11–§12).
package httpserver

import (
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	authhttp "github.com/master-abror/zago-core/backend/internal/auth/transport/http"
	"github.com/master-abror/zago-core/backend/internal/health"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/telemetry"
)

// DefaultRequestTimeout membatasi waktu pemrosesan satu request biasa.
const DefaultRequestTimeout = 30 * time.Second

// Options adalah dependensi router.
type Options struct {
	Logger         *slog.Logger
	TrustedProxies []netip.Prefix
	RequestTimeout time.Duration
	ReadyTimeout   time.Duration
	ReadyCheckers  []health.Checker
	// Kernel adalah toolkit httpx yang dirakit composition root (nil bila tidak tersedia).
	Kernel *KernelServices
	// DevEndpoints mendaftarkan endpoint dev/test (/api/v1/_kernel/echo). Hanya berlaku bila
	// Kernel terisi. Composition root menyalakannya HANYA untuk development/test (ADR-0016).
	DevEndpoints bool
	// Metrics, bila diisi, memasang middleware metrik HTTP (docs/14 §5). nil = tanpa metrik.
	Metrics *telemetry.HTTP
	// Auth adalah rantai autentikasi HTTP (docs/05 §9). Bila diisi, grup rute API dilewatkan
	// melalui Authenticate -> CSRF; rute yang butuh identitas memasang Auth.RequireAuth sendiri.
	// nil = tanpa autentikasi: rute yang butuh identitas menolak SEMUA request dengan 401
	// (fail closed), tidak pernah membiarkannya lolos.
	Auth *authhttp.Middleware
}

// NewHandler membangun handler root. Urutan middleware (terluar -> terdalam): RequestID,
// RealIP, ClientContext, AccessLog, [Metrics], Recoverer, SecurityHeaders, Timeout, lalu —
// hanya untuk grup rute API — Authenticate dan CSRF. AccessLog dan Metrics berada DI LUAR
// Recoverer agar request yang panik tetap tercatat dengan status 500. Endpoint /health* berada
// di luar grup API: probe tidak pernah menyentuh sesi (Redis/PostgreSQL) dan tidak ikut
// terblokir bila autentikasi sedang terganggu.
func NewHandler(o Options) http.Handler {
	log := o.Logger
	if log == nil {
		log = slog.Default()
	}
	timeout := o.RequestTimeout
	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}

	rs := httpx.NewResponder(log)
	r := chi.NewRouter()
	mws := []func(http.Handler) http.Handler{
		RequestID,
		RealIP(o.TrustedProxies),
		ClientContext,
		AccessLog(log),
	}
	if o.Metrics != nil {
		mws = append(mws, Metrics(o.Metrics))
	}
	mws = append(mws, Recoverer(log), SecurityHeaders, middleware.Timeout(timeout))
	r.Use(mws...)

	live := health.Live()
	r.Method(http.MethodGet, "/health", live)
	r.Method(http.MethodGet, "/health/live", live)
	r.Method(http.MethodGet, "/health/ready", health.Ready(log, o.ReadyTimeout, o.ReadyCheckers...))

	r.Group(func(api chi.Router) {
		if o.Auth != nil {
			api.Use(o.Auth.Authenticate, o.Auth.CSRF)
		}
		if o.DevEndpoints && o.Kernel != nil {
			mountEcho(api, o.Kernel, o.Auth)
		}
	})

	r.NotFound(func(w http.ResponseWriter, req *http.Request) { rs.Error(w, req, httpx.ResourceNotFound.New()) })
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		rs.Error(w, req, httpx.MethodNotAllowed.New())
	})
	return r
}
