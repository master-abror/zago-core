package httpserver_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/testkit"
)

// Tes pemasangan rantai autentikasi di NewHandler (T4a, docs/05 §9): Authenticate -> CSRF di grup
// API, RequireAuth per-rute, health di luar grup, dan fail closed tanpa Options.Auth. Sesi
// diganti sesi palsu (fakeSessions); SessionService nyata diuji di paket auth/service dan e2e.

const echoBody = `{"message":"halo"}`

func TestAuthAnonymousPostIs401AndHandlerDoesNotRun(t *testing.T) {
	e := liveEcho(t)

	requireError(t, e.post(echoBody, hdr("Idempotency-Key", "key-anon-0001")), http.StatusUnauthorized, "authentication_required")

	require.Zero(t, e.audit.count())
	require.Zero(t, e.sessions.calls.Load(), "tanpa cookie, penyimpanan sesi tidak disentuh")
}

func TestAuthSessionWithoutCSRFTokenIs403(t *testing.T) {
	e := liveEcho(t)

	rec := e.post(echoBody, hdr("Cookie", testCookieName+"="+tokenA), hdr("Idempotency-Key", "key-nocsrf-001"))

	requireError(t, rec, http.StatusForbidden, "csrf_failed")
	require.Zero(t, e.audit.count())
}

func TestAuthCSRFTokenOfAnotherSessionIs403(t *testing.T) {
	e := liveEcho(t)

	rec := e.post(echoBody,
		hdr("Cookie", testCookieName+"="+tokenA),
		hdr("X-CSRF-Token", testCSRF.Token(sessionB)),
		hdr("Idempotency-Key", "key-othercsrf-1"))

	requireError(t, rec, http.StatusForbidden, "csrf_failed")
	require.Zero(t, e.audit.count())
}

func TestAuthOriginPolicyAppliesToAuthenticatedPost(t *testing.T) {
	e := liveEcho(t)

	for _, bad := range []string{"https://evil.example", "null", "http://app.example.com"} {
		rec := e.post(echoBody, append(okHeaders("key-origin-bad1"), hdr("Origin", bad))...)
		requireError(t, rec, http.StatusForbidden, "csrf_failed")
	}
	require.Zero(t, e.audit.count(), "Origin yang ditolak tidak boleh sampai ke handler")

	rec := e.post(echoBody, append(okHeaders("key-origin-good"), hdr("Origin", testOrigin))...)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, 1, e.audit.count())
}

func TestAuthUnknownCookieIs401AndClearsTheCookie(t *testing.T) {
	e := liveEcho(t)

	rec := e.post(echoBody, hdr("Cookie", testCookieName+"=token-tidak-dikenal"), hdr("Idempotency-Key", "key-unknown-01"))

	requireError(t, rec, http.StatusUnauthorized, "authentication_required")
	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == testCookieName {
			cleared = c.MaxAge < 0 && c.Value == ""
		}
	}
	require.True(t, cleared, "cookie basi harus dihapus; Set-Cookie: %v", rec.Header().Values("Set-Cookie"))
	require.Zero(t, e.audit.count())
}

func TestAuthInfrastructureFailureIs503NotAnonymous(t *testing.T) {
	e := liveEcho(t)
	e.sessions.fail = errors.New("postgres down")

	requireError(t, e.post(echoBody, okHeaders("key-infra-0001")...), http.StatusServiceUnavailable, "service_unavailable")
	require.Zero(t, e.audit.count(), "gangguan infrastruktur bukan alasan membiarkan request lolos")

	requireError(t, e.post(echoBody, hdr("Idempotency-Key", "key-infra-0002")), http.StatusUnauthorized, "authentication_required")
	require.EqualValues(t, 1, e.sessions.calls.Load(), "request tanpa cookie tidak menyentuh penyimpanan sesi")
}

func TestAuthHealthEndpointsAreOutsideTheAuthGroup(t *testing.T) {
	e := liveEcho(t)
	e.sessions.fail = errors.New("postgres down")

	for _, p := range []string{"/health", "/health/live"} {
		rec := e.do(http.MethodGet, p, "", hdr("Cookie", testCookieName+"="+tokenA))
		require.Equal(t, http.StatusOK, rec.Code, p)
	}
	require.Zero(t, e.sessions.calls.Load(), "probe tidak boleh menyentuh sesi")
}

func TestAuthMissingMiddlewareFailsClosed(t *testing.T) {
	e := buildEcho(t, testkit.Redis(t).Client(t), true, false)

	requireError(t, e.post(echoBody, hdr("Idempotency-Key", "key-noauth-001"), hdr("X-Dev-Actor", actorA.String())),
		http.StatusUnauthorized, "authentication_required")
	requireError(t, e.post(echoBody, okHeaders("key-noauth-002")...), http.StatusUnauthorized, "authentication_required")
	require.Zero(t, e.audit.count())

	rec := e.do(http.MethodGet, itemsPath, "")
	require.Equal(t, http.StatusOK, rec.Code, "rute publik tetap melayani tanpa Auth")
}
