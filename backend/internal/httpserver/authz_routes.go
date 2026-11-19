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

// APIPrefix adalah prefiks semua rute otorisasi. Handler mendaftarkan jalur relatif (/roles,
// /permissions, ...) sesuai anotasi swagger /api/v1/...; prefiks ditambahkan di sini, sekali.
const APIPrefix = "/api/v1"

// MountAuthzRoutes memasang routes pada r dengan prefiks APIPrefix dan authorize yang diberikan.
// r harus sudah melewati Authenticate -> CSRF -> RequireAuth -> Scope. Hanya With dan
// Get/Post/Put/Patch/Delete yang dapat dipakai mounter; pendaftaran lain (Handle, Route, Mount,
// Group, ...) panik saat dipasang karena tidak dapat diberi prefiks dengan benar, sehingga tidak
// pernah ada rute yang terpasang diam-diam di jalur yang salah.
func MountAuthzRoutes(r chi.Router, routes []AuthzMounter, authorize func(action string) func(http.Handler) http.Handler) {
	p := prefixRouter{Router: r, prefix: APIPrefix}
	for _, m := range routes {
		if m != nil {
			m.Mount(p, authorize)
		}
	}
}

// prefixRouter menambahkan prefiks pada setiap pola yang didaftarkan lewat metode yang didukung.
type prefixRouter struct {
	chi.Router
	prefix string
}

func (p prefixRouter) With(mws ...func(http.Handler) http.Handler) chi.Router {
	return prefixRouter{Router: p.Router.With(mws...), prefix: p.prefix}
}

func (p prefixRouter) Get(pattern string, h http.HandlerFunc) {
	p.Router.Get(p.prefix+pattern, h)
}

func (p prefixRouter) Post(pattern string, h http.HandlerFunc) {
	p.Router.Post(p.prefix+pattern, h)
}

func (p prefixRouter) Put(pattern string, h http.HandlerFunc) {
	p.Router.Put(p.prefix+pattern, h)
}

func (p prefixRouter) Patch(pattern string, h http.HandlerFunc) {
	p.Router.Patch(p.prefix+pattern, h)
}

func (p prefixRouter) Delete(pattern string, h http.HandlerFunc) {
	p.Router.Delete(p.prefix+pattern, h)
}

func (prefixRouter) unsupported(what string) {
	panic("httpserver: rute otorisasi tidak boleh memakai " + what + "; hanya With dan Get/Post/Put/Patch/Delete (prefiks API tidak dapat diterapkan)")
}

func (p prefixRouter) Handle(string, http.Handler) {
	p.unsupported("Handle")
}

func (p prefixRouter) HandleFunc(string, http.HandlerFunc) {
	p.unsupported("HandleFunc")
}

func (p prefixRouter) Method(string, string, http.Handler) {
	p.unsupported("Method")
}

func (p prefixRouter) MethodFunc(string, string, http.HandlerFunc) {
	p.unsupported("MethodFunc")
}

func (p prefixRouter) Connect(string, http.HandlerFunc) {
	p.unsupported("Connect")
}

func (p prefixRouter) Head(string, http.HandlerFunc) {
	p.unsupported("Head")
}

func (p prefixRouter) Options(string, http.HandlerFunc) {
	p.unsupported("Options")
}

func (p prefixRouter) Trace(string, http.HandlerFunc) {
	p.unsupported("Trace")
}

func (p prefixRouter) Mount(string, http.Handler) {
	p.unsupported("Mount")
}

func (p prefixRouter) Route(string, func(chi.Router)) chi.Router {
	p.unsupported("Route")
	return nil
}

func (p prefixRouter) Group(func(chi.Router)) chi.Router {
	p.unsupported("Group")
	return nil
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
	MountAuthzRoutes(g, a.Routes, a.Middleware.Authorize)
}
