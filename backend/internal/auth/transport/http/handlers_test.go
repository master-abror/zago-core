package authhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/repository"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	authhttp "github.com/master-abror/zago-core/backend/internal/auth/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// Tes handler dengan layanan palsu (AuthService nyata diuji terhadap PostgreSQL+Redis di paket
// service, dan alur penuhnya di e2e infra). Handler dipanggil langsung: yang diuji di sini adalah
// validasi, pemetaan kesalahan, cookie, dan bentuk respons.

const (
	sessionToken = "token-sesi-rahasia-0123456789abcdef0123456789"
	cookieName   = "platform_session"
)

var (
	hUser    = uuid.MustParse("0198a000-0000-7000-8000-0000000000c1")
	hSession = uuid.MustParse("0198a000-0000-7000-8000-0000000000c2")
	hOrg     = uuid.MustParse("0198a000-0000-7000-8000-0000000000c3")
	hNow     = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
)

type fakeSvc struct {
	mu         sync.Mutex
	calls      int
	loginRes   service.LoginResult
	loginErr   error
	logoutErr  error
	profile    service.Profile
	profileErr error
	gotLogin   service.LoginInput
	gotLogout  domain.Identity
}

func (f *fakeSvc) Login(_ context.Context, in service.LoginInput) (service.LoginResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.gotLogin = in
	return f.loginRes, f.loginErr
}

func (f *fakeSvc) Logout(_ context.Context, id domain.Identity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.gotLogout = id
	return f.logoutErr
}

func (f *fakeSvc) Profile(_ context.Context, _ uuid.UUID) (service.Profile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.profile, f.profileErr
}

func (f *fakeSvc) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type handlerRig struct {
	h    *authhttp.Handlers
	svc  *fakeSvc
	csrf *service.CSRF
}

func newHandlerRig(t *testing.T) *handlerRig {
	t.Helper()
	csrf, err := service.NewCSRF(secret)
	require.NoError(t, err)
	cookie, err := authhttp.NewCookie("platform_session", false)
	require.NoError(t, err)
	svc := &fakeSvc{loginRes: service.LoginResult{
		Issued:         service.Issued{Token: sessionToken, Session: repository.Session{ID: hSession, UserID: hUser, ExpiresAt: hNow.Add(12 * time.Hour)}},
		User:           service.LoginUser{ID: hUser, Email: "alice@example.test", DisplayName: "Alice"},
		OrganizationID: func() *uuid.UUID { o := hOrg; return &o }(),
	}}
	h, err := authhttp.NewHandlers(authhttp.HandlerDeps{
		Service: svc, Cookie: cookie, CSRF: csrf,
		Responder: httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil))),
		Now:       func() time.Time { return hNow },
	})
	require.NoError(t, err)
	return &handlerRig{h: h, svc: svc, csrf: csrf}
}

func jsonReq(method, body string) *http.Request {
	r := httptest.NewRequest(method, "/api/v1/auth/x", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func withIdentity(r *http.Request) *http.Request {
	org := hOrg
	return r.WithContext(domain.WithIdentity(r.Context(), domain.Identity{
		UserID: hUser, SessionID: hSession, OrganizationID: &org, AuthLevel: domain.AuthLevelPassword,
	}))
}

func call(h http.HandlerFunc, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func dataOf(t *testing.T, w *httptest.ResponseRecorder, dst any) {
	t.Helper()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env), w.Body.String())
	require.NoError(t, json.Unmarshal(env.Data, dst), w.Body.String())
}

func sessionCookies(w *httptest.ResponseRecorder) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieName {
			out = append(out, c)
		}
	}
	return out
}

func TestNewHandlersRequiresDependencies(t *testing.T) {
	r := newHandlerRig(t)
	cookie, err := authhttp.NewCookie("platform_session", false)
	require.NoError(t, err)
	full := authhttp.HandlerDeps{Service: r.svc, Cookie: cookie, CSRF: r.csrf, Responder: httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil)))}

	_, err = authhttp.NewHandlers(full)
	require.NoError(t, err)
	for name, mutate := range map[string]func(*authhttp.HandlerDeps){
		"service":   func(d *authhttp.HandlerDeps) { d.Service = nil },
		"csrf":      func(d *authhttp.HandlerDeps) { d.CSRF = nil },
		"responder": func(d *authhttp.HandlerDeps) { d.Responder = nil },
		"cookie":    func(d *authhttp.HandlerDeps) { d.Cookie = authhttp.Cookie{} },
	} {
		d := full
		mutate(&d)
		_, err := authhttp.NewHandlers(d)
		require.Error(t, err, name)
	}
}

func TestLoginSetsCookieAndReturnsCSRFTokenButNeverTheSessionToken(t *testing.T) {
	r := newHandlerRig(t)

	w := call(r.h.Login, jsonReq(http.MethodPost, `{"email":"alice@example.test","password":"correct-horse-battery"}`))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	cks := sessionCookies(w)
	require.Len(t, cks, 1)
	require.Equal(t, sessionToken, cks[0].Value)
	require.True(t, cks[0].HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, cks[0].SameSite)
	require.Equal(t, "/", cks[0].Path)
	require.Equal(t, 12*3600, cks[0].MaxAge, "cookie berlaku sampai batas absolut sesi")

	var out authhttp.LoginResponse
	dataOf(t, w, &out)
	require.Equal(t, hUser, out.User.ID)
	require.Equal(t, "alice@example.test", out.User.Email)
	require.Equal(t, "Alice", out.User.DisplayName)
	require.NotNil(t, out.OrganizationID)
	require.Equal(t, hOrg, *out.OrganizationID)
	require.True(t, r.csrf.Verify(hSession, out.CSRFToken), "csrf_token terikat ke sesi yang baru dibuat")
	require.True(t, out.ExpiresAt.Equal(hNow.Add(12*time.Hour)))
	require.NotContains(t, w.Body.String(), sessionToken, "token sesi hanya boleh ada di cookie")
}

func TestLoginPassesTrimmedEmailAndUntouchedPasswordToTheService(t *testing.T) {
	r := newHandlerRig(t)

	w := call(r.h.Login, jsonReq(http.MethodPost, `{"email":"  alice@example.test ","password":"  spasi di tepi  "}`))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "alice@example.test", r.svc.gotLogin.Email)
	require.Equal(t, "  spasi di tepi  ", r.svc.gotLogin.Password, "password tidak boleh di-trim")
}

func TestLoginMapsServiceFailuresToStableCodesWithoutACookie(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"invalid", domain.ErrInvalidCredentials, http.StatusUnauthorized, "invalid_credentials"},
		{"invalid wrapped", fmt.Errorf("lapisan: %w", domain.ErrInvalidCredentials), http.StatusUnauthorized, "invalid_credentials"},
		{"suspended", domain.ErrAccountSuspended, http.StatusForbidden, "account_suspended"},
		{"locked", domain.ErrAccountLocked, http.StatusLocked, "account_locked"},
		{"deadline", fmt.Errorf("auth: %w", context.DeadlineExceeded), http.StatusServiceUnavailable, "service_unavailable"},
		{"unknown", errors.New("pq: koneksi putus ke 10.0.0.5 rahasia-internal"), http.StatusInternalServerError, "internal_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newHandlerRig(t)
			r.svc.loginErr = tc.err

			w := call(r.h.Login, jsonReq(http.MethodPost, `{"email":"alice@example.test","password":"salah-salah-salah"}`))

			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errCode(t, w))
			require.Empty(t, w.Result().Cookies(), "kegagalan tidak boleh menyetel atau menghapus cookie")
			require.NotContains(t, w.Body.String(), "rahasia-internal", "teks kesalahan internal tidak bocor")
			require.NotContains(t, w.Body.String(), "alice@example.test", "respons gagal tidak memantulkan email")
			require.NotContains(t, w.Body.String(), "salah-salah-salah", "respons gagal tidak memantulkan password")
		})
	}
}

func TestLoginRejectsMalformedRequestsWithoutCallingTheService(t *testing.T) {
	long := strings.Repeat("a", 250) + "@example.test"
	cases := []struct {
		name        string
		body        string
		contentType string
		status      int
		code        string
	}{
		{"objek kosong", `{}`, "application/json", http.StatusBadRequest, "validation_failed"},
		{"email kosong", `{"email":"","password":"x"}`, "application/json", http.StatusBadRequest, "validation_failed"},
		{"email tanpa @", `{"email":"bukan-email","password":"x"}`, "application/json", http.StatusBadRequest, "validation_failed"},
		{"email terlalu panjang", `{"email":"` + long + `","password":"x"}`, "application/json", http.StatusBadRequest, "validation_failed"},
		{"password kosong", `{"email":"a@example.test","password":""}`, "application/json", http.StatusBadRequest, "validation_failed"},
		{"field tak dikenal", `{"email":"a@example.test","password":"x","remember":true}`, "application/json", http.StatusBadRequest, "validation_failed"},
		{"JSON rusak", `{"email":`, "application/json", http.StatusBadRequest, "validation_failed"},
		{"bukan JSON", `email=a@example.test`, "application/x-www-form-urlencoded", http.StatusUnsupportedMediaType, "unsupported_media_type"},
		{"body terlalu besar", `{"email":"a@example.test","password":"` + strings.Repeat("p", 8192) + `"}`, "application/json", http.StatusRequestEntityTooLarge, "payload_too_large"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newHandlerRig(t)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", tc.contentType)

			w := call(r.h.Login, req)

			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.code, errCode(t, w))
			require.Zero(t, r.svc.callCount(), "request cacat tidak boleh sampai ke layanan")
			require.Empty(t, w.Result().Cookies())
		})
	}
}

func TestLoginReportsEveryInvalidFieldAtOnce(t *testing.T) {
	r := newHandlerRig(t)

	w := call(r.h.Login, jsonReq(http.MethodPost, `{}`))

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), `"field":"email"`)
	require.Contains(t, w.Body.String(), `"field":"password"`)
}

func TestMeReturnsTheProfileAndSessionOfTheCaller(t *testing.T) {
	r := newHandlerRig(t)
	uname := "alice"
	login := hNow.Add(-time.Hour)
	r.svc.profile = service.Profile{
		ID: hUser, Email: "alice@example.test", DisplayName: "Alice", Username: &uname,
		Locale: "id-ID", Timezone: "Asia/Jakarta", LastLoginAt: &login,
	}

	w := call(r.h.Me, withIdentity(httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var out authhttp.MeResponse
	dataOf(t, w, &out)
	require.Equal(t, hUser, out.User.ID)
	require.Equal(t, "alice@example.test", out.User.Email)
	require.Equal(t, "Asia/Jakarta", out.User.Timezone)
	require.NotNil(t, out.OrganizationID)
	require.Equal(t, hOrg, *out.OrganizationID)
	require.Equal(t, hSession, out.Session.ID)
	require.Equal(t, string(domain.AuthLevelPassword), out.Session.AuthLevel)
	require.Empty(t, w.Result().Cookies())
}

func TestMeWithoutIdentityOrWithAMissingUserIs401(t *testing.T) {
	r := newHandlerRig(t)

	w := call(r.h.Me, httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, "authentication_required", errCode(t, w))
	require.Zero(t, r.svc.callCount())

	r.svc.profileErr = domain.ErrUnauthenticated
	w = call(r.h.Me, withIdentity(httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, "authentication_required", errCode(t, w))
}

func TestCSRFEndpointReturnsATokenBoundToTheCallersSession(t *testing.T) {
	r := newHandlerRig(t)

	w := call(r.h.CSRF, withIdentity(httptest.NewRequest(http.MethodGet, "/api/v1/auth/csrf", nil)))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	var out authhttp.CSRFResponse
	dataOf(t, w, &out)
	require.True(t, r.csrf.Verify(hSession, out.CSRFToken))
	require.False(t, r.csrf.Verify(uuid.New(), out.CSRFToken), "tidak berlaku untuk sesi lain")

	w = call(r.h.CSRF, httptest.NewRequest(http.MethodGet, "/api/v1/auth/csrf", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestLogoutRevokesTheCallersSessionAndClearsTheCookie(t *testing.T) {
	r := newHandlerRig(t)

	w := call(r.h.Logout, withIdentity(httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)))

	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	require.Empty(t, w.Body.String())
	require.Equal(t, hUser, r.svc.gotLogout.UserID)
	require.Equal(t, hSession, r.svc.gotLogout.SessionID)
	cks := sessionCookies(w)
	require.Len(t, cks, 1)
	require.Less(t, cks[0].MaxAge, 0, "cookie dihapus")
	require.Empty(t, cks[0].Value)
}

func TestLogoutKeepsTheCookieWhenRevocationFailsAndNeedsAnIdentity(t *testing.T) {
	r := newHandlerRig(t)
	r.svc.logoutErr = errors.New("redis dan postgres tidak terjangkau")

	w := call(r.h.Logout, withIdentity(httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)))
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Empty(t, w.Result().Cookies(), "bila pencabutan gagal, cookie dibiarkan agar klien bisa mengulang")

	r2 := newHandlerRig(t)
	w = call(r2.h.Logout, httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Zero(t, r2.svc.callCount())
}
