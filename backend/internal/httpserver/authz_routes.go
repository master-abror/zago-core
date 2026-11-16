package httpserver

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	authhttp "github.com/master-abror/zago-core/backend/internal/auth/transport/http"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
)

// AuthzMounter memasang rute otorisasi pada router yang SUDAH melewati Authenticate -> CSRF ->
// RequireAuth -> Scope. authorize adalah (*authzhttp.Middleware).Authorize: setiap rute wajib
// memasangnya sendiri dengan aksi katalog (ADR-0022 butir 25). RoleHandlers, PolicyHandlers, dan
// SimulateHandlers memenuhinya langsung; AssignmentHandlers (yang membutuhkan Idempotency) dibungkus
// AuthzMounterFunc oleh composition root.
type AuthzMounter interface {
	Mount(r chi.Router, authorize func(action string) func(http.Handler) http.Handler)
}

// AuthzMounterFunc adalah adaptor fungsi untuk AuthzMounter.
type AuthzMounterFunc func(r chi.Router, authorize func(action string) func(http.Handler) http.Handler)

// Mount memanggil f.
func (f AuthzMounterFunc) Mount(r chi.Router, authorize func(action string) func(http.Handler) http.Handler) {
	f(r, authorize)
}

// AuthzAPI adalah rakitan HTTP Authorization Engine untuk NewHandler: middleware Scope/Authorize
// dan rute yang memakainya. httpserver tidak mengenal handler tertentu; composition root yang
// mengisi Routes.
type AuthzAPI struct {
	Middleware *authzhttp.Middleware
	Routes     []AuthzMounter
}

// mountAuthz memasang semua rute otorisasi di grup yang sama dengan rute terlindungi lain.
// Urutan di grup API: Authenticate -> CSRF (dipasang NewHandler), lalu di sini RequireAuth ->
// Scope, lalu Authorize(action) per rute (dipasang AuthzMounter). Tanpa auth atau middleware
// otorisasi tidak ada rute yang terpasang (fail closed: tidak ada handler yang dapat dicapai).
func mountAuthz(r chi.Router, auth *authhttp.Middleware, a *AuthzAPI) {
	if auth == nil || a == nil || a.Middleware == nil {
		return
	}
	g := r.With(auth.RequireAuth, a.Middleware.Scope)
	for _, m := range a.Routes {
		if m != nil {
			m.Mount(g, a.Middleware.Authorize)
		}
	}
}
