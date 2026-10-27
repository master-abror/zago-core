package httpserver

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	authhttp "github.com/master-abror/zago-core/backend/internal/auth/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// AuthPath dan SecurityPath adalah prefiks endpoint autentikasi dan keamanan akun.
const (
	AuthPath     = "/api/v1/auth"
	SecurityPath = "/api/v1/security"
)

// LoginRule membatasi percobaan login per IP klien. Sensitive: bila Redis mati, pembatasan jatuh
// ke memori proses (bukan fail open), karena login adalah target tebakan password. Pembatasan per
// akun, jeda progresif, dan kunci akun menyusul di T5.
var LoginRule = httpx.Rule{Name: "auth.login", Limit: 10, Window: time.Minute, Sensitive: true}

// PasswordRule membatasi ganti password per pengguna (ADR-0020 butir 45): endpoint terautentikasi ini
// memverifikasi password saat ini, jadi pencuri sesi bisa memakainya untuk menebak. Sensitive: bila
// Redis mati, pembatasan jatuh ke memori proses. Tidak mengunci akun (itu urusan login, T5).
var PasswordRule = httpx.Rule{Name: "security.password", Limit: 5, Window: time.Minute, Sensitive: true}

// userRateKey mengidentifikasi pengguna terautentikasi untuk rate limit; kosong (tidak dibatasi)
// bila tak ada identitas (RequireAuth sudah menolaknya lebih dulu).
func userRateKey(r *http.Request) string {
	id, ok := domain.IdentityFromContext(r.Context())
	if !ok {
		return ""
	}
	return "user:" + id.UserID.String()
}

// ipRateKey mengidentifikasi klien untuk rate limit; kosong (tidak dibatasi) bila IP tak diketahui.
func ipRateKey(r *http.Request) string {
	ip := ClientIP(r.Context())
	if ip == "" {
		return ""
	}
	return "ip:" + ip
}

// mountAuth memasang endpoint /auth/*. Login publik (anonim), dibatasi per IP; sisanya butuh
// sesi (RequireAuth). Authenticate dan CSRF sudah dipasang di grup API oleh NewHandler, jadi
// setiap POST terautentikasi (logout, refresh, switch-organization) menuntut X-CSRF-Token.
func mountAuth(r chi.Router, k *KernelServices, mw *authhttp.Middleware, h *authhttp.Handlers) {
	r.With(k.RateLimiter.Middleware(LoginRule, ipRateKey)).Post(AuthPath+"/login", h.Login)
	r.With(mw.RequireAuth).Post(AuthPath+"/logout", h.Logout)
	r.With(mw.RequireAuth).Get(AuthPath+"/me", h.Me)
	r.With(mw.RequireAuth).Get(AuthPath+"/csrf", h.CSRF)
	r.With(mw.RequireAuth).Post(AuthPath+"/refresh", h.Refresh)
	r.With(mw.RequireAuth).Post(AuthPath+"/switch-organization", h.SwitchOrganization)

	r.With(mw.RequireAuth).Get(SecurityPath+"/sessions", h.ListSessions)
	r.With(mw.RequireAuth).Delete(SecurityPath+"/sessions/{id}", h.RevokeSession)
	r.With(mw.RequireAuth).Post(SecurityPath+"/sessions/revoke-others", h.RevokeOtherSessions)

	// Tanpa Idempotency-Key (ADR-0020 butir 42). RequireAuth lebih dulu: kunci rate limit memakai identitas.
	r.With(mw.RequireAuth, k.RateLimiter.Middleware(PasswordRule, userRateKey)).Post(SecurityPath+"/password", h.ChangePassword)
	r.With(mw.RequireAuth).Get(SecurityPath+"/events", h.ListSecurityEvents)
}
