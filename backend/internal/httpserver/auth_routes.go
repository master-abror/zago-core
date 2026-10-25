package httpserver

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	authhttp "github.com/master-abror/zago-core/backend/internal/auth/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// AuthPath adalah prefiks endpoint autentikasi.
const AuthPath = "/api/v1/auth"

// LoginRule membatasi percobaan login per IP klien. Sensitive: bila Redis mati, pembatasan jatuh
// ke memori proses (bukan fail open), karena login adalah target tebakan password. Pembatasan per
// akun, jeda progresif, dan kunci akun menyusul di T5.
var LoginRule = httpx.Rule{Name: "auth.login", Limit: 10, Window: time.Minute, Sensitive: true}

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
}
