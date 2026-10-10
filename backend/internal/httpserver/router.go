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

	"github.com/master-abror/zago-core/backend/internal/health"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
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
}

// NewHandler membangun handler root. Urutan middleware (terluar -> terdalam): RequestID,
// RealIP, ClientContext, AccessLog, Recoverer, SecurityHeaders, Timeout. AccessLog berada DI LUAR Recoverer
// agar request yang panik tetap tercatat dengan status 500.
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
	r.Use(
		RequestID,
		RealIP(o.TrustedProxies),
		ClientContext,
		AccessLog(log),
		Recoverer(log),
		SecurityHeaders,
		middleware.Timeout(timeout),
	)

	live := health.Live()
	r.Method(http.MethodGet, "/health", live)
	r.Method(http.MethodGet, "/health/live", live)
	r.Method(http.MethodGet, "/health/ready", health.Ready(log, o.ReadyTimeout, o.ReadyCheckers...))

	r.NotFound(func(w http.ResponseWriter, req *http.Request) { rs.Error(w, req, httpx.ResourceNotFound.New()) })
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		rs.Error(w, req, httpx.MethodNotAllowed.New())
	})
	return r
}
