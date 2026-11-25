package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	authzapp "github.com/master-abror/zago-core/backend/internal/authorization/application"
	authzrepo "github.com/master-abror/zago-core/backend/internal/authorization/repository"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/httpserver"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// UseInstantSyncRetry mengganti kebijakan retry sinkron katalog di RunAPI dengan attempts
// percobaan tanpa jeda sungguhan, dan memulihkannya saat tes selesai. Hanya untuk tes: jangan
// dipakai bersama t.Parallel (mengubah variabel paket).
func UseInstantSyncRetry(t testing.TB, attempts int) {
	t.Helper()
	prev := syncRetryPolicy
	t.Cleanup(func() { syncRetryPolicy = prev })
	syncRetryPolicy = func() retryPolicy {
		p := defaultSyncRetry()
		p.Attempts = attempts
		p.wait = func(context.Context, time.Duration) error { return nil }
		return p
	}
}

// AssembleSimulatorForTest merakit authzStack seperti produksi (tanpa Redis) lalu Simulator-nya
// lewat newSimulator, dan mengembalikan Authorizer stack itu supaya tes dapat membandingkan
// jawaban simulasi dengan Authorizer.Can langsung. Hanya untuk tes paket app_test (tes ber-DB
// tidak boleh berada di paket internal: siklus impor lewat testkit).
func AssembleSimulatorForTest(db kernel.DBTX, log *slog.Logger, audit modulesdk.AuditRecorder) (*authzapp.Simulator, *authzapp.Authorizer, error) {
	s, err := newAuthzStack(db, nil, log)
	if err != nil {
		return nil, nil, err
	}
	sim, err := s.newSimulator(db, audit)
	if err != nil {
		return nil, nil, err
	}
	return sim, s.authorizer, nil
}

// OrganizationAPIForTest merakit rute organisasi seperti produksi (tanpa Redis): perakit
// organizationRoutes, middleware Scope dan Authorize SUNGGUHAN di atas pool PostgreSQL nyata, dan
// pemasangan lewat httpserver.MountAuthzRoutes (prefiks /api/v1). Authenticate, CSRF, dan sesi
// TIDAK ikut: identify menentukan Identity setiap request (ok=false berarti request anonim).
// Hanya untuk tes paket app_test (ADR-0023 butir 20).
func OrganizationAPIForTest(pool kernel.Pool, log *slog.Logger, audit modulesdk.AuditRecorder, identify func(*http.Request) (authdomain.Identity, bool)) (http.Handler, error) {
	if identify == nil {
		return nil, errors.New("identify wajib ada")
	}
	s, err := newAuthzStack(pool, nil, log)
	if err != nil {
		return nil, err
	}
	rs := httpx.NewResponder(log)
	routes, err := s.organizationRoutes(pool, audit, rs, log)
	if err != nil {
		return nil, err
	}
	resolver, err := authzapp.NewScopeResolver(authzrepo.NewGroups(pool), log)
	if err != nil {
		return nil, err
	}
	mw, err := authzhttp.New(authzhttp.Deps{Scope: resolver, Authorizer: s.authorizer, Audit: audit, Responder: rs, Log: log})
	if err != nil {
		return nil, err
	}
	inject := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if ident, ok := identify(req); ok {
				req = req.WithContext(authdomain.WithIdentity(req.Context(), ident))
			}
			next.ServeHTTP(w, req)
		})
	}
	r := chi.NewRouter()
	httpserver.MountAuthzRoutes(r.With(inject, mw.Scope), []httpserver.AuthzMounter{routes}, mw.Authorize)
	return r, nil
}
