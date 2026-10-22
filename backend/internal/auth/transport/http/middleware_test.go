package authhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	authhttp "github.com/master-abror/zago-core/backend/internal/auth/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

const (
	secret = "unit-test-secret-unit-test-secret-0123"
	origin = "https://app.example.com"
)

// fakeAuth menggantikan SessionService di tes unit middleware (SessionService sendiri diuji
// terhadap PostgreSQL+Redis nyata di paket service).
type fakeAuth struct {
	sessions map[string]domain.Identity
	fail     error // bila diisi, semua panggilan gagal dengan galat ini
	calls    int
}

func (f *fakeAuth) Authenticate(_ context.Context, token string) (*domain.Identity, error) {
	f.calls++
	if f.fail != nil {
		return nil, f.fail
	}
	if i, ok := f.sessions[token]; ok {
		return &i, nil
	}
	return nil, domain.ErrUnauthenticated
}

type rig struct {
	t      *testing.T
	auth   *fakeAuth
	csrf   *service.CSRF
	cookie authhttp.Cookie
	h      http.Handler
	alice  domain.Identity
	bob    domain.Identity
}

func newRig(t *testing.T) *rig {
	t.Helper()
	csrf, err := service.NewCSRF(secret)
	require.NoError(t, err)
	cookie, err := authhttp.NewCookie("platform_session", true)
	require.NoError(t, err)

	org := id.NewID()
	r := &rig{
		t: t, csrf: csrf, cookie: cookie,
		alice: domain.Identity{UserID: id.NewID(), OrganizationID: &org, SessionID: id.NewID(), AuthLevel: domain.AuthLevelPassword},
		bob:   domain.Identity{UserID: id.NewID(), SessionID: id.NewID(), AuthLevel: domain.AuthLevelPassword},
	}
	r.auth = &fakeAuth{sessions: map[string]domain.Identity{"alice-token": r.alice, "bob-token": r.bob}}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	m, err := authhttp.New(authhttp.Deps{
		Auth: r.auth, Cookie: cookie, CSRF: csrf, Origins: authhttp.NewOriginPolicy([]string{origin}),
		Responder: httpx.NewResponder(log), Log: log,
	})
	require.NoError(t, err)

	mux := http.NewServeMux()
	// publik: tanpa RequireAuth (mis. login)
	mux.HandleFunc("/public", func(w http.ResponseWriter, req *http.Request) { respondWho(w, req) })
	// terlindungi
	mux.Handle("/private", m.RequireAuth(http.HandlerFunc(respondWho)))
	r.h = m.Authenticate(m.CSRF(mux))
	return r
}

type who struct {
	Authenticated bool   `json:"authenticated"`
	UserID        string `json:"user_id"`
	SessionID     string `json:"session_id"`
	OrgID         string `json:"org_id"`
	ActorUserID   string `json:"actor_user_id"`
	ActorOrgID    string `json:"actor_org_id"`
}

func respondWho(w http.ResponseWriter, r *http.Request) {
	out := who{}
	if i, ok := domain.IdentityFromContext(r.Context()); ok {
		out.Authenticated, out.UserID, out.SessionID = true, i.UserID.String(), i.SessionID.String()
		if i.OrganizationID != nil {
			out.OrgID = i.OrganizationID.String()
		}
	}
	if a, ok := kernel.ActorFromContext(r.Context()); ok {
		out.ActorUserID = a.UserID.String()
		if a.OrganizationID != uuid.Nil {
			out.ActorOrgID = a.OrganizationID.String()
		}
	}
	_ = json.NewEncoder(w).Encode(out)
}

type req struct {
	method, path string
	token        string // nilai cookie sesi ("" = tanpa cookie)
	csrf         string
	origin       string
	extra        map[string][]string
}

func (r *rig) do(q req) *httptest.ResponseRecorder {
	r.t.Helper()
	hr := httptest.NewRequest(q.method, q.path, nil)
	if q.token != "" {
		hr.AddCookie(&http.Cookie{Name: r.cookie.Name(), Value: q.token})
	}
	if q.csrf != "" {
		hr.Header.Set(authhttp.HeaderCSRF, q.csrf)
	}
	if q.origin != "" {
		hr.Header.Set("Origin", q.origin)
	}
	for k, vs := range q.extra {
		for _, v := range vs {
			hr.Header.Add(k, v)
		}
	}
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, hr)
	return w
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return body.Error.Code
}

func decodeWho(t *testing.T, w *httptest.ResponseRecorder) who {
	t.Helper()
	var out who
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return out
}

func clearsCookie(r *rig, w *httptest.ResponseRecorder) bool {
	for _, ck := range w.Result().Cookies() {
		if ck.Name == r.cookie.Name() && ck.MaxAge < 0 {
			return true
		}
	}
	return false
}

// ---- Authenticate / RequireAuth ----

func TestAnonymousRequestPassesOnlyOnPublicRoutes(t *testing.T) {
	r := newRig(t)

	w := r.do(req{method: http.MethodGet, path: "/public"})
	require.Equal(t, http.StatusOK, w.Code)
	require.False(t, decodeWho(t, w).Authenticated)
	require.Zero(t, r.auth.calls, "tanpa cookie: tidak menyentuh penyimpanan sesi")

	w = r.do(req{method: http.MethodGet, path: "/private"})
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, "authentication_required", errCode(t, w))
}

func TestValidCookieSetsIdentityAndActor(t *testing.T) {
	r := newRig(t)
	w := r.do(req{method: http.MethodGet, path: "/private", token: "alice-token"})
	require.Equal(t, http.StatusOK, w.Code)

	got := decodeWho(t, w)
	require.True(t, got.Authenticated)
	require.Equal(t, r.alice.UserID.String(), got.UserID)
	require.Equal(t, r.alice.SessionID.String(), got.SessionID)
	require.Equal(t, r.alice.OrganizationID.String(), got.OrgID)
	require.Equal(t, r.alice.UserID.String(), got.ActorUserID, "kernel.Actor ikut terisi untuk audit dan idempotency")
	require.Equal(t, r.alice.OrganizationID.String(), got.ActorOrgID)

	t.Run("identitas platform tanpa organisasi", func(t *testing.T) {
		got := decodeWho(t, r.do(req{method: http.MethodGet, path: "/private", token: "bob-token"}))
		require.Equal(t, r.bob.UserID.String(), got.ActorUserID)
		require.Empty(t, got.OrgID)
		require.Empty(t, got.ActorOrgID)
	})
}

func TestInvalidCookieIsAnonymousAndGetsCleared(t *testing.T) {
	r := newRig(t)
	for name, tok := range map[string]string{
		"tak dikenal": "not-a-session",
		"sampah":      "' OR 1=1 --",
	} {
		t.Run(name, func(t *testing.T) {
			w := r.do(req{method: http.MethodGet, path: "/public", token: tok})
			require.Equal(t, http.StatusOK, w.Code, "endpoint publik tidak tersandung cookie basi")
			require.False(t, decodeWho(t, w).Authenticated)
			require.True(t, clearsCookie(r, w), "cookie basi dihapus")

			w = r.do(req{method: http.MethodGet, path: "/private", token: tok})
			require.Equal(t, http.StatusUnauthorized, w.Code)
			require.Equal(t, "authentication_required", errCode(t, w))
		})
	}
}

func TestValidSessionDoesNotClearTheCookie(t *testing.T) {
	r := newRig(t)
	w := r.do(req{method: http.MethodGet, path: "/private", token: "alice-token"})
	require.Empty(t, w.Result().Cookies())
}

func TestInfrastructureFailureIs503NotLoggedOut(t *testing.T) {
	r := newRig(t)
	r.auth.fail = errors.New("pgx: connection refused")

	w := r.do(req{method: http.MethodGet, path: "/private", token: "alice-token"})
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, "service_unavailable", errCode(t, w))
	require.False(t, clearsCookie(r, w), "gangguan sementara tidak boleh menghapus cookie sesi")
	require.NotContains(t, w.Body.String(), "connection refused", "galat internal tidak bocor ke klien")

	w = r.do(req{method: http.MethodGet, path: "/public", token: "alice-token"})
	require.Equal(t, http.StatusServiceUnavailable, w.Code, "endpoint publik pun tidak menganggap pengguna anonim saat gangguan")
}

// ---- CSRF ----

func TestSafeMethodsNeedNoCSRFToken(t *testing.T) {
	r := newRig(t)
	for _, m := range []string{http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace} {
		w := r.do(req{method: m, path: "/public", token: "alice-token"})
		require.Equal(t, http.StatusOK, w.Code, m)
	}
}

func TestStateChangingRequestsNeedAValidCSRFToken(t *testing.T) {
	r := newRig(t)
	good := r.csrf.Token(r.alice.SessionID)

	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, "PROPFIND"} {
		t.Run(m, func(t *testing.T) {
			w := r.do(req{method: m, path: "/private", token: "alice-token", csrf: good})
			require.Equal(t, http.StatusOK, w.Code, "token sah")

			w = r.do(req{method: m, path: "/private", token: "alice-token"})
			require.Equal(t, http.StatusForbidden, w.Code, "token hilang")
			require.Equal(t, "csrf_failed", errCode(t, w))

			w = r.do(req{method: m, path: "/private", token: "alice-token", csrf: "bukan-token"})
			require.Equal(t, http.StatusForbidden, w.Code, "token salah")
			require.Equal(t, "csrf_failed", errCode(t, w))
		})
	}
}

func TestCSRFTokenFromAnotherSessionIsRejected(t *testing.T) {
	r := newRig(t)
	bobsToken := r.csrf.Token(r.bob.SessionID)

	w := r.do(req{method: http.MethodPost, path: "/private", token: "alice-token", csrf: bobsToken})
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, "csrf_failed", errCode(t, w))
}

func TestCSRFTokenDoesNotSurviveSessionRotation(t *testing.T) {
	r := newRig(t)
	oldToken := r.csrf.Token(r.alice.SessionID)

	rotated := r.alice
	rotated.SessionID = id.NewID()
	r.auth.sessions["alice-rotated"] = rotated

	w := r.do(req{method: http.MethodPost, path: "/private", token: "alice-rotated", csrf: oldToken})
	require.Equal(t, http.StatusForbidden, w.Code, "id sesi baru: token lama tidak berlaku")
	w = r.do(req{method: http.MethodPost, path: "/private", token: "alice-rotated", csrf: r.csrf.Token(rotated.SessionID)})
	require.Equal(t, http.StatusOK, w.Code)
}

func TestCSRFDoesNotBlockHandlerWhenRejected(t *testing.T) {
	r := newRig(t)
	called := false
	m, err := authhttp.New(authhttp.Deps{
		Auth: r.auth, Cookie: r.cookie, CSRF: r.csrf, Responder: httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil))),
	})
	require.NoError(t, err)
	h := m.Authenticate(m.CSRF(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })))

	hr := httptest.NewRequest(http.MethodPost, "/x", nil)
	hr.AddCookie(&http.Cookie{Name: r.cookie.Name(), Value: "alice-token"})
	h.ServeHTTP(httptest.NewRecorder(), hr)
	require.False(t, called, "handler tidak boleh tersentuh bila CSRF gagal")
}

// ---- Origin ----

func TestOriginIsCheckedOnStateChangingRequests(t *testing.T) {
	r := newRig(t)
	good := r.csrf.Token(r.alice.SessionID)

	t.Run("anonim dengan Origin yang diizinkan lolos", func(t *testing.T) {
		w := r.do(req{method: http.MethodPost, path: "/public", origin: origin})
		require.Equal(t, http.StatusOK, w.Code)
	})
	t.Run("anonim tanpa Origin (klien non-peramban) lolos", func(t *testing.T) {
		w := r.do(req{method: http.MethodPost, path: "/public"})
		require.Equal(t, http.StatusOK, w.Code)
	})
	for name, o := range map[string]string{
		"origin asing":  "https://evil.example",
		"null":          "null",
		"skema lain":    "http://app.example.com",
		"slash penutup": origin + "/",
		"domain mirip":  "https://app.example.com.evil.test",
	} {
		t.Run("anonim ditolak: "+name, func(t *testing.T) {
			w := r.do(req{method: http.MethodPost, path: "/public", origin: o})
			require.Equal(t, http.StatusForbidden, w.Code)
			require.Equal(t, "csrf_failed", errCode(t, w))
		})
		t.Run("terautentikasi dengan token sah tetap ditolak: "+name, func(t *testing.T) {
			w := r.do(req{method: http.MethodPost, path: "/private", token: "alice-token", csrf: good, origin: o})
			require.Equal(t, http.StatusForbidden, w.Code, "pertahanan berlapis: Origin juga diperiksa")
			require.Equal(t, "csrf_failed", errCode(t, w))
		})
	}
	t.Run("lebih dari satu header Origin ditolak", func(t *testing.T) {
		w := r.do(req{method: http.MethodPost, path: "/public", extra: map[string][]string{"Origin": {origin, origin}}})
		require.Equal(t, http.StatusForbidden, w.Code)
	})
	t.Run("terautentikasi dengan Origin dan token sah lolos", func(t *testing.T) {
		w := r.do(req{method: http.MethodPost, path: "/private", token: "alice-token", csrf: good, origin: origin})
		require.Equal(t, http.StatusOK, w.Code)
	})
	t.Run("GET lintas-origin tidak dibatasi middleware CSRF", func(t *testing.T) {
		w := r.do(req{method: http.MethodGet, path: "/public", origin: "https://evil.example"})
		require.Equal(t, http.StatusOK, w.Code)
	})
}

// Request yang membawa cookie TIDAK sah bersama Origin asing tetap ditolak oleh pemeriksaan Origin
// (cookie dibuang, request diperlakukan anonim, Origin tetap dicek).
func TestForgedCookieWithForeignOriginIsRejected(t *testing.T) {
	r := newRig(t)
	w := r.do(req{method: http.MethodPost, path: "/public", token: "forged", origin: "https://evil.example"})
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	csrf, _ := service.NewCSRF(secret)
	cookie, _ := authhttp.NewCookie("platform_session", true)
	rs := httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil)))
	auth := &fakeAuth{}

	for name, d := range map[string]authhttp.Deps{
		"tanpa Auth":      {Cookie: cookie, CSRF: csrf, Responder: rs},
		"tanpa CSRF":      {Auth: auth, Cookie: cookie, Responder: rs},
		"tanpa Responder": {Auth: auth, Cookie: cookie, CSRF: csrf},
		"tanpa Cookie":    {Auth: auth, CSRF: csrf, Responder: rs},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := authhttp.New(d)
			require.Error(t, err)
		})
	}
}
