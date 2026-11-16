package httpserver_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/httpserver"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/pkg/id"
	"github.com/master-abror/zago-core/backend/pkg/logger"
)

// Tes perakitan T5d1 (ADR-0022 butir 25): rute otorisasi dipasang di grup API sesudah
// Authenticate -> CSRF, dan di dalamnya RequireAuth -> Scope -> Authorize(action) -> handler.
// ScopeResolver dan Authorizer diganti palsu (yang nyata diuji di paket authorization); yang
// dibuktikan di sini hanya urutan dan efeknya lewat NewHandler.

const (
	probeRelative = "/_authz/probe"
	probePath     = httpserver.APIPrefix + probeRelative
)

type authzFakeResolver struct {
	mu    sync.Mutex
	calls []authdomain.Identity
	scope domain.ScopeContext
	err   error
}

func (f *authzFakeResolver) Resolve(_ context.Context, ident authdomain.Identity, _ []string) (domain.ScopeContext, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ident)
	return f.scope, f.err
}

func (f *authzFakeResolver) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type authzFakeAuthorizer struct {
	mu       sync.Mutex
	checks   []domain.Check
	decision domain.Decision
	err      error
}

func (f *authzFakeAuthorizer) Can(_ context.Context, c domain.Check) (domain.Decision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks = append(f.checks, c)
	return f.decision, f.err
}

func (f *authzFakeAuthorizer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.checks)
}

// authzProbe adalah rute uji yang dipasang lewat AuthzMounter seperti handler asli (Authorize
// per rute); ia mencatat apa yang dilihat handler.
type authzProbe struct {
	mu          sync.Mutex
	reached     int
	scope       domain.ScopeContext
	hasScope    bool
	kernelScope kernel.Scope
	hasKernel   bool
}

func (p *authzProbe) Mount(r chi.Router, authorize func(action string) func(http.Handler) http.Handler) {
	r.With(authorize("role.read")).Get(probeRelative, func(w http.ResponseWriter, req *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.reached++
		p.scope, p.hasScope = domain.ScopeFromContext(req.Context())
		p.kernelScope, p.hasKernel = kernel.ScopeFromContext(req.Context())
		w.WriteHeader(http.StatusNoContent)
	})
}

func (p *authzProbe) reachedCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reached
}

type authzEnv struct {
	echoEnv
	opts       httpserver.Options
	mw         *authzhttp.Middleware
	resolver   *authzFakeResolver
	authorizer *authzFakeAuthorizer
	probe      *authzProbe
}

func (e authzEnv) get(opts ...reqOpt) *httptest.ResponseRecorder {
	return e.do(http.MethodGet, probePath, "", opts...)
}

// buildAuthz merakit NewHandler dengan rantai auth nyata (sesi palsu) dan rute otorisasi uji.
// withAuth=false atau withAuthz=false meninggalkan Options.Auth atau Options.Authz nil. Klien
// Redis tidak terjangkau dan tidak pernah dipakai: rute uji tidak memakai idempotency atau rate
// limit, jadi tes ini tidak membutuhkan Docker.
func buildAuthz(t *testing.T, withAuth, withAuthz bool) authzEnv {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = rdb.Close() })

	var buf bytes.Buffer
	log := logger.New(&buf, slog.LevelDebug)
	aud := &fakeAudit{}
	kern, err := httpserver.NewKernelServices(httpserver.KernelDeps{
		Redis: rdb, Log: log, SessionSecret: testSecret, Audit: aud,
	})
	require.NoError(t, err)

	org := id.NewID()
	e := authzEnv{
		resolver:   &authzFakeResolver{scope: domain.ScopeContext{OrganizationID: &org}},
		authorizer: &authzFakeAuthorizer{decision: domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}},
		probe:      &authzProbe{},
	}
	mw, err := authzhttp.New(authzhttp.Deps{
		Scope: e.resolver, Authorizer: e.authorizer, Audit: aud, Responder: kern.Responder, Log: log,
	})
	require.NoError(t, err)
	e.mw = mw

	sessions := newFakeSessions()
	e.opts = httpserver.Options{Logger: log, Kernel: kern}
	if withAuth {
		e.opts.Auth = newAuthMW(t, log, sessions)
	}
	if withAuthz {
		e.opts.Authz = &httpserver.AuthzAPI{Middleware: mw, Routes: []httpserver.AuthzMounter{e.probe}}
	}
	e.echoEnv = echoEnv{h: httpserver.NewHandler(e.opts), audit: aud, log: &buf, sessions: sessions}
	return e
}

func TestAuthzRouteWithoutSessionIs401AndNothingElseRuns(t *testing.T) {
	e := buildAuthz(t, true, true)

	requireError(t, e.get(), http.StatusUnauthorized, "authentication_required")

	require.Zero(t, e.resolver.count(), "Scope tidak boleh berjalan sebelum RequireAuth lolos")
	require.Zero(t, e.authorizer.count(), "engine tidak boleh ditanya tanpa identitas")
	require.Zero(t, e.probe.reachedCount())
}

func TestAuthzRouteResolvesScopeFromTheSessionIdentityThenAuthorizes(t *testing.T) {
	e := buildAuthz(t, true, true)

	rec := e.get(sessionHeaders(tokenA, sessionA)...)

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Equal(t, 1, e.resolver.count())
	require.Equal(t, actorA, e.resolver.calls[0].UserID, "Scope memakai Identity dari sesi")
	require.Equal(t, 1, e.authorizer.count())
	check := e.authorizer.checks[0]
	require.Equal(t, "role.read", check.Action, "aksi per rute dipasang lewat Authorize(action)")
	require.Equal(t, actorA, check.Identity.UserID)
	require.NotNil(t, check.Scope.OrganizationID, "Check memakai scope hasil Scope, bukan tebakan")
	require.Equal(t, 1, e.probe.reachedCount())
}

func TestAuthzHandlerSeesScopeContextAndKernelScope(t *testing.T) {
	e := buildAuthz(t, true, true)
	org := *e.resolver.scope.OrganizationID

	rec := e.get(sessionHeaders(tokenA, sessionA)...)

	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.True(t, e.probe.hasScope)
	require.Equal(t, org, *e.probe.scope.OrganizationID)
	require.True(t, e.probe.hasKernel, "activity yang ditulis handler harus membawa scope")
	require.Equal(t, kernel.Scope{Type: "organization", ID: org}, e.probe.kernelScope)
}

func TestAuthzDeniedDecisionIs403AndHandlerDoesNotRun(t *testing.T) {
	e := buildAuthz(t, true, true)
	e.authorizer.decision = domain.Decision{Allowed: false, Reason: domain.ReasonNoPermission}

	rec := e.get(sessionHeaders(tokenA, sessionA)...)

	requireError(t, rec, http.StatusForbidden, "permission_denied")
	require.Zero(t, e.probe.reachedCount())
	require.Equal(t, 1, e.audit.count(), "penolakan direkam sebagai activity")
	require.Equal(t, "authz.access_denied", e.audit.entries[0].Action)
}

func TestAuthzEngineFailureIs503NeverAllow(t *testing.T) {
	e := buildAuthz(t, true, true)
	e.authorizer.err = context.DeadlineExceeded
	e.authorizer.decision = domain.Decision{Allowed: true}

	rec := e.get(sessionHeaders(tokenA, sessionA)...)

	requireError(t, rec, http.StatusServiceUnavailable, "service_unavailable")
	require.Zero(t, e.probe.reachedCount(), "galat engine menang atas Decision, tidak pernah izin")
}

func TestAuthzScopeResolutionFailureIs503AndStopsBeforeTheEngine(t *testing.T) {
	e := buildAuthz(t, true, true)
	e.resolver.err = context.DeadlineExceeded

	rec := e.get(sessionHeaders(tokenA, sessionA)...)

	requireError(t, rec, http.StatusServiceUnavailable, "service_unavailable")
	require.Zero(t, e.authorizer.count())
	require.Zero(t, e.probe.reachedCount())
}

func TestAuthzRoutesAreNotMountedWithoutAuthMiddleware(t *testing.T) {
	e := buildAuthz(t, false, true)

	rec := e.get(hdr("Cookie", testCookieName+"="+tokenA))

	requireError(t, rec, http.StatusNotFound, "resource_not_found")
	require.Zero(t, e.resolver.count(), "tanpa Auth rute otorisasi tidak boleh terpasang (fail closed)")
	require.Zero(t, e.authorizer.count())
	require.Zero(t, e.probe.reachedCount())
}

func TestAuthzRoutesAreNotMountedWithoutOptionsAuthz(t *testing.T) {
	e := buildAuthz(t, true, false)

	rec := e.get(sessionHeaders(tokenA, sessionA)...)

	requireError(t, rec, http.StatusNotFound, "resource_not_found")
	require.Zero(t, e.probe.reachedCount())
}

func TestAuthzMountedRoutesStillRunBehindAuthenticateAndCSRF(t *testing.T) {
	e := buildAuthz(t, true, true)

	// GET dilewatkan CSRF (metode aman); pemeriksaan token CSRF untuk POST ada di auth_wiring_test.
	// Yang dibuktikan di sini: sesi tak dikenal ditolak oleh Authenticate sebelum Scope berjalan.
	rec := e.get(hdr("Cookie", testCookieName+"=token-tidak-dikenal"))

	requireError(t, rec, http.StatusUnauthorized, "authentication_required")
	require.Zero(t, e.resolver.count())
	require.Zero(t, e.probe.reachedCount())
}

func TestAuthzAPIWithoutMiddlewareMountsNothing(t *testing.T) {
	e := buildAuthz(t, true, false)
	opts := e.opts
	opts.Authz = &httpserver.AuthzAPI{Routes: []httpserver.AuthzMounter{e.probe}}
	e.h = httpserver.NewHandler(opts)

	rec := e.get(sessionHeaders(tokenA, sessionA)...)

	requireError(t, rec, http.StatusNotFound, "resource_not_found")
	require.Zero(t, e.probe.reachedCount(), "rute tidak boleh terpasang tanpa Scope dan Authorize")
}

func TestAuthzNilMounterInRoutesIsSkippedNotPanicking(t *testing.T) {
	e := buildAuthz(t, true, false)
	opts := e.opts
	opts.Authz = &httpserver.AuthzAPI{Middleware: e.mw, Routes: []httpserver.AuthzMounter{nil, e.probe}}

	var h http.Handler
	require.NotPanics(t, func() { h = httpserver.NewHandler(opts) })
	e.h = h

	rec := e.get(sessionHeaders(tokenA, sessionA)...)
	require.Equal(t, http.StatusNoContent, rec.Code, "mounter nil dilewati, mounter sesudahnya tetap terpasang")
}

func passAuthorize(string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}

// Handler mendaftarkan jalur relatif (/roles) sesuai swagger /api/v1/roles: prefiks harus
// ditambahkan sekali oleh pemasang, lewat With maupun metode langsung.
func TestMountAuthzRoutesPrefixesRelativePathsWithAPIV1(t *testing.T) {
	probe := &authzProbe{}
	r := chi.NewRouter()

	httpserver.MountAuthzRoutes(r, []httpserver.AuthzMounter{probe}, passAuthorize)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, probePath, nil))
	require.Equal(t, http.StatusNoContent, rec.Code)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, probeRelative, nil))
	require.Equal(t, http.StatusNotFound, rec.Code, "jalur tanpa prefiks tidak boleh terpasang")
	require.Equal(t, 1, probe.reachedCount())
}

func TestMountAuthzRoutesPanicsOnRegistrationItCannotPrefix(t *testing.T) {
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	cases := map[string]func(r chi.Router){
		"Handle":     func(r chi.Router) { r.Handle("/x", h) },
		"HandleFunc": func(r chi.Router) { r.HandleFunc("/x", h) },
		"Method":     func(r chi.Router) { r.Method(http.MethodGet, "/x", h) },
		"MethodFunc": func(r chi.Router) { r.MethodFunc(http.MethodGet, "/x", h) },
		"Head":       func(r chi.Router) { r.Head("/x", h) },
		"Options":    func(r chi.Router) { r.Options("/x", h) },
		"Mount":      func(r chi.Router) { r.Mount("/x", h) },
		"Route":      func(r chi.Router) { r.Route("/x", func(chi.Router) {}) },
		"Group":      func(r chi.Router) { r.Group(func(chi.Router) {}) },
	}
	for name, register := range cases {
		mount := httpserver.AuthzMounterFunc(func(r chi.Router, _ func(string) func(http.Handler) http.Handler) { register(r) })

		require.Panics(t, func() {
			httpserver.MountAuthzRoutes(chi.NewRouter(), []httpserver.AuthzMounter{mount}, passAuthorize)
		}, name)
	}
}
