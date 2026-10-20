package httpserver

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// KernelDeps adalah bahan baku toolkit HTTP kernel yang dirakit composition root.
type KernelDeps struct {
	Redis         redis.UniversalClient // wajib: idempotency dan rate limit
	Log           *slog.Logger
	SessionSecret string                  // wajib ≥ 32 byte: kunci turunan cursor (ADR-0013)
	Audit         modulesdk.AuditRecorder // opsional: nil → endpoint tidak merekam activity
}

// KernelServices adalah satu set toolkit httpx yang dibagi semua endpoint (docs/10 §3): satu
// Responder, satu CursorCodec, satu Idempotency, satu RateLimiter. Dibuat sekali di composition
// root dan diteruskan lewat Options.Kernel.
type KernelServices struct {
	Responder   *httpx.Responder
	Cursors     *httpx.CursorCodec
	Idempotency *httpx.Idempotency
	RateLimiter *httpx.RateLimiter
	Audit       modulesdk.AuditRecorder
}

// NewKernelServices memvalidasi bahan baku dan merakit toolkit. Subject idempotency adalah
// user id aktor pada context (kernel.ActorFromContext); Route adalah pola chi, bukan path mentah,
// sehingga kunci Redis tidak bergantung pada nilai parameter path.
func NewKernelServices(d KernelDeps) (*KernelServices, error) {
	if d.Redis == nil {
		return nil, errors.New("httpserver: KernelDeps.Redis wajib diisi")
	}
	rs := httpx.NewResponder(d.Log)
	codec, err := httpx.NewCursorCodec(d.SessionSecret)
	if err != nil {
		return nil, err
	}
	idem, err := httpx.NewIdempotency(httpx.IdempotencyOptions{
		Redis: d.Redis, Responder: rs, Subject: actorSubject, Route: routePattern, Log: d.Log,
	})
	if err != nil {
		return nil, err
	}
	rl, err := httpx.NewRateLimiter(d.Redis, rs, d.Log)
	if err != nil {
		return nil, err
	}
	return &KernelServices{Responder: rs, Cursors: codec, Idempotency: idem, RateLimiter: rl, Audit: d.Audit}, nil
}

// actorSubject mengembalikan user id aktor request; ok=false bila belum terautentikasi.
func actorSubject(r *http.Request) (string, bool) {
	a, ok := kernel.ActorFromContext(r.Context())
	if !ok || a.UserID == uuid.Nil {
		return "", false
	}
	return a.UserID.String(), true
}

// routePattern mengembalikan pola rute chi yang cocok (mis. /api/v1/things/{id}); jatuh ke path
// mentah bila tidak ada konteks rute. Hanya bermakna untuk middleware yang dipasang per-endpoint
// (r.With), karena pada saat itu rute sudah dipilih.
func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return r.URL.Path
}
