package httpserver_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/repository"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	authhttp "github.com/master-abror/zago-core/backend/internal/auth/transport/http"
	"github.com/master-abror/zago-core/backend/internal/httpserver"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/testkit"
)

// Tes rute /auth/* lewat httpserver.NewHandler: rantai middleware nyata (Authenticate -> CSRF ->
// RequireAuth) di atas sesi palsu dan layanan auth palsu. Yang dibuktikan di sini adalah
// pemasangan: siapa boleh memanggil apa, kapan CSRF dituntut, dan rate limit login.

const loginBody = `{"email":"alice@example.test","password":"correct-horse-battery"}`

// fakeAuthSvc mengembalikan sesi milik pengguna A dengan tokenA, sehingga cookie hasil login
// langsung dikenali fakeSessions.
type fakeAuthSvc struct {
	mu          sync.Mutex
	loginCalls  int
	logoutCalls int
	loginErr    error
	gotLogout   domain.Identity

	rotateCalls int
	switchErr   error
	gotSwitch   domain.Identity
	gotOrg      uuid.UUID

	revokeCalls int
	gotRevoke   uuid.UUID

	changeCalls int
	changeErr   error
	gotChangeID domain.Identity
	eventsCalls int
	gotEventsID domain.Identity
}

// rotated adalah sesi hasil rotasi: pengguna A dengan tokenB/sessionB (dikenali fakeSessions).
func rotated() service.Issued {
	return service.Issued{Token: tokenB, Session: repository.Session{ID: sessionB, UserID: actorA, ExpiresAt: time.Now().Add(time.Hour)}}
}

func (f *fakeAuthSvc) Refresh(_ context.Context, id domain.Identity) (service.Issued, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rotateCalls++
	f.gotSwitch = id
	return rotated(), nil
}

func (f *fakeAuthSvc) SwitchOrganization(_ context.Context, id domain.Identity, orgID uuid.UUID) (service.Issued, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rotateCalls++
	f.gotSwitch, f.gotOrg = id, orgID
	if f.switchErr != nil {
		return service.Issued{}, f.switchErr
	}
	return rotated(), nil
}

func (f *fakeAuthSvc) ListSessions(_ context.Context, id domain.Identity) ([]service.SessionInfo, error) {
	return []service.SessionInfo{{ID: id.SessionID, AuthLevel: "password", Current: true, ExpiresAt: time.Now().Add(time.Hour)}}, nil
}

func (f *fakeAuthSvc) RevokeSession(_ context.Context, _ domain.Identity, target uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revokeCalls++
	f.gotRevoke = target
	return nil
}

func (f *fakeAuthSvc) RevokeOtherSessions(_ context.Context, _ domain.Identity) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revokeCalls++
	return 2, nil
}

func (f *fakeAuthSvc) ChangePassword(_ context.Context, id domain.Identity, _ service.ChangePasswordInput) (service.ChangePasswordResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changeCalls++
	f.gotChangeID = id
	if f.changeErr != nil {
		return service.ChangePasswordResult{}, f.changeErr
	}
	return service.ChangePasswordResult{Issued: rotated(), RevokedOthers: 1}, nil
}

func (f *fakeAuthSvc) ListSecurityEvents(_ context.Context, id domain.Identity, _ int, _ *service.EventPosition) ([]service.SecurityEventInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.eventsCalls++
	f.gotEventsID = id
	return nil, nil
}

func (f *fakeAuthSvc) passwordCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.changeCalls
}

func (f *fakeAuthSvc) revocations() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.revokeCalls
}

func (f *fakeAuthSvc) rotations() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rotateCalls
}

func newFakeAuthSvc() *fakeAuthSvc { return &fakeAuthSvc{} }

func (f *fakeAuthSvc) Login(_ context.Context, _ service.LoginInput) (service.LoginResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loginCalls++
	if f.loginErr != nil {
		return service.LoginResult{}, f.loginErr
	}
	return service.LoginResult{
		Issued: service.Issued{Token: tokenA, Session: repository.Session{ID: sessionA, UserID: actorA, ExpiresAt: time.Now().Add(time.Hour)}},
		User:   service.LoginUser{ID: actorA, Email: "alice@example.test", DisplayName: "Alice"},
	}, nil
}

func (f *fakeAuthSvc) Logout(_ context.Context, id domain.Identity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logoutCalls++
	f.gotLogout = id
	return nil
}

func (f *fakeAuthSvc) Profile(_ context.Context, userID uuid.UUID) (service.Profile, error) {
	return service.Profile{ID: userID, Email: "alice@example.test", DisplayName: "Alice", Locale: "id-ID", Timezone: "Asia/Jakarta"}, nil
}

func (f *fakeAuthSvc) calls() (login, logout int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loginCalls, f.logoutCalls
}

func newAuthHandlers(t *testing.T, log *slog.Logger, svc authhttp.Service) *authhttp.Handlers {
	t.Helper()
	cookie, err := authhttp.NewCookie("platform_session", false)
	require.NoError(t, err)
	cursors, err := httpx.NewCursorCodec(testSecret)
	require.NoError(t, err)
	h, err := authhttp.NewHandlers(authhttp.HandlerDeps{Service: svc, Cookie: cookie, CSRF: testCSRF, Cursors: cursors, Responder: httpx.NewResponder(log)})
	require.NoError(t, err)
	return h
}

func (e echoEnv) login(opts ...reqOpt) *httptest.ResponseRecorder {
	return e.do(http.MethodPost, httpserver.AuthPath+"/login", loginBody, opts...)
}

// sessionSetCookies mengembalikan semua Set-Cookie bernama cookie sesi, sesuai urutan header.
func sessionSetCookies(rec *httptest.ResponseRecorder) []*http.Cookie {
	var out []*http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == testCookieName {
			out = append(out, c)
		}
	}
	return out
}

func TestAuthLoginIsPublicAndSetsTheSessionCookie(t *testing.T) {
	e := liveEcho(t)

	rec := e.login() // tanpa cookie dan tanpa CSRF: login adalah rute anonim

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	cks := sessionSetCookies(rec)
	require.Len(t, cks, 1)
	require.Equal(t, tokenA, cks[0].Value)
	require.True(t, cks[0].HttpOnly)
	var out authhttp.LoginResponse
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &out))
	require.True(t, testCSRF.Verify(sessionA, out.CSRFToken))
	login, _ := e.authSvc.calls()
	require.Equal(t, 1, login)
}

func TestAuthLoginChecksTheOriginEvenThoughItIsAnonymous(t *testing.T) {
	e := liveEcho(t)

	for _, bad := range []string{"https://evil.example", "null"} {
		requireError(t, e.login(hdr("Origin", bad)), http.StatusForbidden, "csrf_failed")
	}
	login, _ := e.authSvc.calls()
	require.Zero(t, login, "Origin asing tidak boleh sampai ke layanan login")

	require.Equal(t, http.StatusOK, e.login(hdr("Origin", testOrigin)).Code)
}

func TestAuthLoginWithAStaleCookieClearsItThenIssuesAFreshOne(t *testing.T) {
	e := liveEcho(t)

	rec := e.login(hdr("Cookie", testCookieName+"=cookie-lama-yang-sudah-mati"))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	cks := sessionSetCookies(rec)
	require.Len(t, cks, 2, "satu penghapusan cookie basi, satu cookie baru")
	require.Less(t, cks[0].MaxAge, 0)
	require.Equal(t, tokenA, cks[1].Value, "yang terakhir menang di peramban")
}

func TestAuthLoginFailureIs401WithoutACookie(t *testing.T) {
	e := liveEcho(t)
	e.authSvc.loginErr = domain.ErrInvalidCredentials

	rec := e.login()

	requireError(t, rec, http.StatusUnauthorized, "invalid_credentials")
	require.Empty(t, sessionSetCookies(rec))
}

func TestAuthLoginIsRateLimitedPerClientIP(t *testing.T) {
	e := liveEcho(t)
	e.authSvc.loginErr = domain.ErrInvalidCredentials

	for i := 0; i < httpserver.LoginRule.Limit; i++ {
		rec := e.login()
		require.Equal(t, http.StatusUnauthorized, rec.Code, "percobaan ke-"+strconv.Itoa(i+1))
	}
	rec := e.login()

	requireError(t, rec, http.StatusTooManyRequests, "rate_limit_exceeded")
	require.Equal(t, "0", rec.Header().Get("X-RateLimit-Remaining"))
	login, _ := e.authSvc.calls()
	require.Equal(t, httpserver.LoginRule.Limit, login, "permintaan yang dibatasi tidak sampai ke layanan")
}

func TestAuthMeRequiresASession(t *testing.T) {
	e := liveEcho(t)

	requireError(t, e.do(http.MethodGet, httpserver.AuthPath+"/me", ""), http.StatusUnauthorized, "authentication_required")

	rec := e.do(http.MethodGet, httpserver.AuthPath+"/me", "", hdr("Cookie", testCookieName+"="+tokenA))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out authhttp.MeResponse
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &out))
	require.Equal(t, actorA, out.User.ID)
	require.Equal(t, sessionA, out.Session.ID)
	require.Nil(t, out.OrganizationID)
}

func TestAuthMeWithAnUnknownCookieIs401AndClearsIt(t *testing.T) {
	e := liveEcho(t)

	rec := e.do(http.MethodGet, httpserver.AuthPath+"/me", "", hdr("Cookie", testCookieName+"=token-tidak-dikenal"))

	requireError(t, rec, http.StatusUnauthorized, "authentication_required")
	cks := sessionSetCookies(rec)
	require.Len(t, cks, 1)
	require.Less(t, cks[0].MaxAge, 0)
}

func TestAuthCSRFEndpointIssuesATokenForTheCallersSession(t *testing.T) {
	e := liveEcho(t)

	requireError(t, e.do(http.MethodGet, httpserver.AuthPath+"/csrf", ""), http.StatusUnauthorized, "authentication_required")

	rec := e.do(http.MethodGet, httpserver.AuthPath+"/csrf", "", hdr("Cookie", testCookieName+"="+tokenB))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out authhttp.CSRFResponse
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &out))
	require.True(t, testCSRF.Verify(sessionB, out.CSRFToken))
	require.False(t, testCSRF.Verify(sessionA, out.CSRFToken))
}

func TestAuthLogoutNeedsASessionAndAValidCSRFToken(t *testing.T) {
	e := liveEcho(t)
	path := httpserver.AuthPath + "/logout"

	requireError(t, e.do(http.MethodPost, path, ""), http.StatusUnauthorized, "authentication_required")
	requireError(t, e.do(http.MethodPost, path, "", hdr("Cookie", testCookieName+"="+tokenA)), http.StatusForbidden, "csrf_failed")
	requireError(t, e.do(http.MethodPost, path, "",
		hdr("Cookie", testCookieName+"="+tokenA), hdr(authhttp.HeaderCSRF, testCSRF.Token(sessionB))), http.StatusForbidden, "csrf_failed")
	_, logout := e.authSvc.calls()
	require.Zero(t, logout, "tanpa sesi dan CSRF yang sah, logout tidak boleh jalan")

	rec := e.do(http.MethodPost, path, "", sessionHeaders(tokenA, sessionA)...)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	_, logout = e.authSvc.calls()
	require.Equal(t, 1, logout)
	require.Equal(t, sessionA, e.authSvc.gotLogout.SessionID)
	cks := sessionSetCookies(rec)
	require.Len(t, cks, 1)
	require.Less(t, cks[0].MaxAge, 0, "cookie dihapus")
}

func TestAuthRoutesAreNotMountedWithoutTheirDependencies(t *testing.T) {
	// Tanpa Auth/AuthAPI, endpoint /auth/* tidak ada (404), bukan terbuka tanpa perlindungan.
	e := buildEcho(t, testkit.Redis(t).Client(t), true, false)

	require.Equal(t, http.StatusNotFound, e.login().Code)
	require.Equal(t, http.StatusNotFound, e.do(http.MethodGet, httpserver.AuthPath+"/me", "").Code)
	login, logout := e.authSvc.calls()
	require.Zero(t, login+logout)
}

func TestAuthRefreshNeedsASessionAndCSRFAndRotatesTheCookie(t *testing.T) {
	e := liveEcho(t)
	path := httpserver.AuthPath + "/refresh"

	requireError(t, e.do(http.MethodPost, path, ""), http.StatusUnauthorized, "authentication_required")
	requireError(t, e.do(http.MethodPost, path, "", hdr("Cookie", testCookieName+"="+tokenA)), http.StatusForbidden, "csrf_failed")
	require.Zero(t, e.authSvc.rotations(), "tanpa sesi dan CSRF sah, rotasi tidak boleh jalan")

	rec := e.do(http.MethodPost, path, "", sessionHeaders(tokenA, sessionA)...)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, e.authSvc.rotations())
	require.Equal(t, sessionA, e.authSvc.gotSwitch.SessionID, "layanan menerima identitas dari sesi")
	cks := sessionSetCookies(rec)
	require.Len(t, cks, 1)
	require.Equal(t, tokenB, cks[0].Value)
	var out authhttp.RefreshResponse
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &out))
	require.True(t, testCSRF.Verify(sessionB, out.CSRFToken))
	require.False(t, testCSRF.Verify(sessionA, out.CSRFToken))
}

func TestAuthSwitchOrganizationNeedsASessionAndCSRFAndMapsNonMembership(t *testing.T) {
	e := liveEcho(t)
	path := httpserver.AuthPath + "/switch-organization"
	org := uuid.MustParse("0198a000-0000-7000-8000-0000000000d1")
	body := `{"organization_id":"` + org.String() + `"}`

	requireError(t, e.do(http.MethodPost, path, body, hdr("Content-Type", "application/json")), http.StatusUnauthorized, "authentication_required")
	requireError(t, e.do(http.MethodPost, path, body, hdr("Content-Type", "application/json"), hdr("Cookie", testCookieName+"="+tokenA)),
		http.StatusForbidden, "csrf_failed")
	require.Zero(t, e.authSvc.rotations())

	e.authSvc.switchErr = domain.ErrNotOrganizationMember
	rec := e.do(http.MethodPost, path, body, append(sessionHeaders(tokenA, sessionA), hdr("Content-Type", "application/json"))...)
	requireError(t, rec, http.StatusForbidden, "permission_denied")
	require.Empty(t, sessionSetCookies(rec), "penolakan tidak menyentuh cookie")

	e.authSvc.switchErr = nil
	rec = e.do(http.MethodPost, path, body, append(sessionHeaders(tokenA, sessionA), hdr("Content-Type", "application/json"))...)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, org, e.authSvc.gotOrg)
	cks := sessionSetCookies(rec)
	require.Len(t, cks, 1)
	require.Equal(t, tokenB, cks[0].Value)
}

func TestSecuritySessionRoutesNeedASessionAndCSRFForStateChanges(t *testing.T) {
	e := liveEcho(t)
	base := httpserver.SecurityPath + "/sessions"
	target := uuid.MustParse("0198a000-0000-7000-8000-0000000000e1")

	// Daftar: butuh sesi, tidak butuh CSRF (GET).
	requireError(t, e.do(http.MethodGet, base, ""), http.StatusUnauthorized, "authentication_required")
	rec := e.do(http.MethodGet, base, "", hdr("Cookie", testCookieName+"="+tokenA))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list authhttp.SessionsResponse
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &list))
	require.Len(t, list.Sessions, 1)
	require.Equal(t, sessionA, list.Sessions[0].ID)
	require.True(t, list.Sessions[0].Current)

	// DELETE dan revoke-others: anonim 401, tanpa CSRF 403, tidak ada pencabutan sebelum sah.
	requireError(t, e.do(http.MethodDelete, base+"/"+target.String(), ""), http.StatusUnauthorized, "authentication_required")
	requireError(t, e.do(http.MethodDelete, base+"/"+target.String(), "", hdr("Cookie", testCookieName+"="+tokenA)), http.StatusForbidden, "csrf_failed")
	requireError(t, e.do(http.MethodPost, base+"/revoke-others", ""), http.StatusUnauthorized, "authentication_required")
	requireError(t, e.do(http.MethodPost, base+"/revoke-others", "", hdr("Cookie", testCookieName+"="+tokenA)), http.StatusForbidden, "csrf_failed")
	require.Zero(t, e.authSvc.revocations())

	rec = e.do(http.MethodDelete, base+"/"+target.String(), "", sessionHeaders(tokenA, sessionA)...)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	require.Equal(t, target, e.authSvc.gotRevoke)
	require.Empty(t, sessionSetCookies(rec), "sesi lain: cookie tidak disentuh")

	rec = e.do(http.MethodPost, base+"/revoke-others", "", sessionHeaders(tokenA, sessionA)...)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var out authhttp.RevokeOthersResponse
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &out))
	require.Equal(t, 2, out.Revoked)
	require.Equal(t, 2, e.authSvc.revocations())
}

func TestSecurityRevokeOthersIsNotShadowedByTheSessionIDRoute(t *testing.T) {
	e := liveEcho(t)

	// "revoke-others" adalah rute statis POST; sebagai {id} pada DELETE ia hanya ID cacat (404).
	rec := e.do(http.MethodDelete, httpserver.SecurityPath+"/sessions/revoke-others", "", sessionHeaders(tokenA, sessionA)...)
	requireError(t, rec, http.StatusNotFound, "resource_not_found")
	require.Zero(t, e.authSvc.revocations())
}

// ---- POST /security/password dan GET /security/events (T4d-2, ADR-0020 butir 42–46) ----

const passwordBodyJSON = `{"current_password":"password-lama-yang-benar-12","new_password":"password-baru-yang-kuat-34"}`

func TestSecurityPasswordNeedsASessionAndAValidCSRFToken(t *testing.T) {
	e := liveEcho(t)
	path := httpserver.SecurityPath + "/password"

	requireError(t, e.do(http.MethodPost, path, passwordBodyJSON), http.StatusUnauthorized, "authentication_required")
	requireError(t, e.do(http.MethodPost, path, passwordBodyJSON, hdr("Cookie", testCookieName+"="+tokenA)), http.StatusForbidden, "csrf_failed")
	requireError(t, e.do(http.MethodPost, path, passwordBodyJSON,
		hdr("Cookie", testCookieName+"="+tokenA), hdr(authhttp.HeaderCSRF, testCSRF.Token(sessionB))), http.StatusForbidden, "csrf_failed")
	requireError(t, e.do(http.MethodPost, path, passwordBodyJSON,
		hdr("Cookie", testCookieName+"="+tokenA), hdr(authhttp.HeaderCSRF, testCSRF.Token(sessionA)), hdr("Origin", "https://evil.example")),
		http.StatusForbidden, "csrf_failed")
	require.Zero(t, e.authSvc.passwordCalls(), "tanpa sesi dan CSRF yang sah, ganti password tidak boleh jalan")

	rec := e.do(http.MethodPost, path, passwordBodyJSON, sessionHeaders(tokenA, sessionA)...)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, 1, e.authSvc.passwordCalls())
	require.Equal(t, sessionA, e.authSvc.gotChangeID.SessionID)
	require.Equal(t, actorA, e.authSvc.gotChangeID.UserID)
	cks := sessionSetCookies(rec)
	require.Len(t, cks, 1)
	require.Equal(t, tokenB, cks[0].Value, "cookie sesi hasil rotasi")
	var out authhttp.ChangePasswordResponse
	require.NoError(t, json.Unmarshal(decode(t, rec).Data, &out))
	require.True(t, testCSRF.Verify(sessionB, out.CSRFToken))
}

func TestSecurityPasswordDoesNotRequireAnIdempotencyKey(t *testing.T) {
	e := liveEcho(t)

	rec := e.do(http.MethodPost, httpserver.SecurityPath+"/password", passwordBodyJSON, sessionHeaders(tokenA, sessionA)...)

	require.Equal(t, http.StatusOK, rec.Code, "ADR-0020 butir 42: tanpa Idempotency-Key")
	require.Empty(t, rec.Header().Get("Idempotent-Replayed"))
}

func TestSecurityPasswordIsRateLimitedPerUserNotPerSession(t *testing.T) {
	e := liveEcho(t)
	e.authSvc.changeErr = domain.ErrInvalidCredentials
	path := httpserver.SecurityPath + "/password"

	for i := 0; i < httpserver.PasswordRule.Limit; i++ {
		rec := e.do(http.MethodPost, path, passwordBodyJSON, sessionHeaders(tokenA, sessionA)...)
		require.Equal(t, http.StatusUnauthorized, rec.Code, "percobaan ke-"+strconv.Itoa(i+1))
	}
	rec := e.do(http.MethodPost, path, passwordBodyJSON, sessionHeaders(tokenA, sessionA)...)

	requireError(t, rec, http.StatusTooManyRequests, "rate_limit_exceeded")
	require.Equal(t, httpserver.PasswordRule.Limit, e.authSvc.passwordCalls(), "permintaan yang dibatasi tidak sampai ke layanan")
	require.Empty(t, sessionSetCookies(rec))

	// Pengguna lain (satu IP yang sama) tidak ikut terkunci: kuncinya per pengguna.
	other := e.do(http.MethodPost, path, passwordBodyJSON, sessionHeaders(tokenB, sessionB)...)
	require.Equal(t, http.StatusUnauthorized, other.Code, other.Body.String())
}

func TestSecurityEventsRequireASessionButNoCSRFTokenAndUseTheCallersIdentity(t *testing.T) {
	e := liveEcho(t)
	path := httpserver.SecurityPath + "/events"

	requireError(t, e.do(http.MethodGet, path, ""), http.StatusUnauthorized, "authentication_required")
	require.Zero(t, e.authSvc.eventsCalls)

	rec := e.do(http.MethodGet, path+"?user_id="+actorB.String(), "", hdr("Cookie", testCookieName+"="+tokenA))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, actorA, e.authSvc.gotEventsID.UserID, "identitas dari sesi, bukan dari query")
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

func TestAuthLoginMapsTheIPGateTo429WithRetryAfterAndNoCookie(t *testing.T) {
	e := liveEcho(t)
	e.authSvc.loginErr = &domain.ThrottledError{RetryAfter: 90 * time.Second}

	rec := e.login()

	requireError(t, rec, http.StatusTooManyRequests, "rate_limit_exceeded")
	require.Equal(t, "90", rec.Header().Get("Retry-After"))
	require.Empty(t, sessionSetCookies(rec), "gerbang IP tidak menyentuh cookie")
}

func TestAuthLoginIPGateRetryAfterIsAtLeastOneSecond(t *testing.T) {
	e := liveEcho(t)
	e.authSvc.loginErr = &domain.ThrottledError{RetryAfter: 200 * time.Millisecond}

	rec := e.login()

	requireError(t, rec, http.StatusTooManyRequests, "rate_limit_exceeded")
	require.Equal(t, "1", rec.Header().Get("Retry-After"))
}

func TestAuthRefreshIsRateLimitedPerUser(t *testing.T) {
	e := liveEcho(t)
	path := httpserver.AuthPath + "/refresh"

	for i := 0; i < httpserver.RefreshRule.Limit; i++ {
		rec := e.do(http.MethodPost, path, "", sessionHeaders(tokenA, sessionA)...)
		require.Equal(t, http.StatusOK, rec.Code, "refresh ke-"+strconv.Itoa(i+1))
	}
	rec := e.do(http.MethodPost, path, "", sessionHeaders(tokenA, sessionA)...)

	requireError(t, rec, http.StatusTooManyRequests, "rate_limit_exceeded")
	require.Equal(t, httpserver.RefreshRule.Limit, e.authSvc.rotations(), "permintaan yang dibatasi tidak sampai ke layanan")
	require.Empty(t, sessionSetCookies(rec))

	other := e.do(http.MethodPost, path, "", sessionHeaders(tokenB, sessionB)...)
	require.Equal(t, http.StatusOK, other.Code, "pengguna lain tidak ikut dibatasi: kuncinya per pengguna")
}

func TestAuthSwitchOrganizationIsRateLimitedPerUser(t *testing.T) {
	e := liveEcho(t)
	path := httpserver.AuthPath + "/switch-organization"
	org := uuid.MustParse("0198a000-0000-7000-8000-0000000000d2")
	body := `{"organization_id":"` + org.String() + `"}`
	opts := append(sessionHeaders(tokenA, sessionA), hdr("Content-Type", "application/json"))

	for i := 0; i < httpserver.SwitchOrganizationRule.Limit; i++ {
		rec := e.do(http.MethodPost, path, body, opts...)
		require.Equal(t, http.StatusOK, rec.Code, "switch ke-"+strconv.Itoa(i+1))
	}
	rec := e.do(http.MethodPost, path, body, opts...)

	requireError(t, rec, http.StatusTooManyRequests, "rate_limit_exceeded")
	require.Equal(t, httpserver.SwitchOrganizationRule.Limit, e.authSvc.rotations(), "permintaan yang dibatasi tidak sampai ke layanan")
	require.Empty(t, sessionSetCookies(rec))
}
