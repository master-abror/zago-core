package authhttp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// HeaderCSRF adalah header tempat klien mengirim token CSRF (docs/05 §8).
const HeaderCSRF = "X-CSRF-Token"

// Authenticator memvalidasi token cookie. *service.SessionService memenuhinya.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (*domain.Identity, error)
}

// Deps adalah dependensi Middleware.
type Deps struct {
	Auth      Authenticator
	Cookie    Cookie
	CSRF      *service.CSRF
	Origins   OriginPolicy
	Responder *httpx.Responder
	Log       *slog.Logger
}

// Middleware adalah rantai autentikasi HTTP. Urutan pemasangan yang benar:
//
//	Authenticate -> CSRF -> [RequireAuth pada rute terlindungi] -> handler
//
// Berbeda dari diagram docs/05 §9 (CSRF sebelum Authentication): token CSRF terikat ke id sesi,
// jadi sesi harus sudah dikenali sebelum token bisa diperiksa. Jaminan intinya tetap: tidak ada
// request yang mengubah state mencapai handler tanpa lolos CSRF (ADR-0020).
type Middleware struct {
	auth    Authenticator
	cookie  Cookie
	csrf    *service.CSRF
	origins OriginPolicy
	rs      *httpx.Responder
	log     *slog.Logger
}

// New membuat Middleware; semua dependensi kecuali Log wajib.
func New(d Deps) (*Middleware, error) {
	switch {
	case d.Auth == nil:
		return nil, errors.New("authhttp: Deps.Auth wajib")
	case d.CSRF == nil:
		return nil, errors.New("authhttp: Deps.CSRF wajib")
	case d.Responder == nil:
		return nil, errors.New("authhttp: Deps.Responder wajib")
	case d.Cookie.Name() == "":
		return nil, errors.New("authhttp: Deps.Cookie wajib (pakai NewCookie)")
	}
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	return &Middleware{auth: d.Auth, cookie: d.Cookie, csrf: d.CSRF, origins: d.Origins, rs: d.Responder, log: log}, nil
}

// Authenticate memasang Identity (dan kernel.Actor) bila cookie sesi sah. Tanpa cookie, atau
// dengan cookie yang tidak sah (kedaluwarsa, dicabut, tak dikenal), request lanjut ANONIM dan
// cookie basi dihapus — sehingga endpoint publik seperti login tidak tersandung cookie lama.
// Penolakan 401 adalah tugas RequireAuth. Kegagalan infrastruktur (mis. PostgreSQL) menjadi 503,
// bukan 401: klien tidak boleh mengira sesinya berakhir.
func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := m.cookie.Token(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		id, err := m.auth.Authenticate(r.Context(), token)
		switch {
		case err == nil:
			next.ServeHTTP(w, r.WithContext(withIdentity(r.Context(), *id)))
		case errors.Is(err, domain.ErrUnauthenticated):
			m.cookie.Clear(w)
			next.ServeHTTP(w, r)
		default:
			m.log.ErrorContext(r.Context(), "autentikasi gagal karena infrastruktur", "error", err)
			m.rs.Error(w, r, httpx.ServiceUnavailable.Wrap(err))
		}
	})
}

func withIdentity(ctx context.Context, id domain.Identity) context.Context {
	actor := kernel.Actor{UserID: id.UserID, Type: kernel.ActorUser}
	if id.OrganizationID != nil {
		actor.OrganizationID = *id.OrganizationID
	} else {
		actor.OrganizationID = uuid.Nil
	}
	return kernel.WithActor(domain.WithIdentity(ctx, id), actor)
}

// CSRF melindungi request pengubah-state (metode selain GET/HEAD/OPTIONS/TRACE):
//   - Origin, bila ada, harus persis salah satu ALLOWED_ORIGINS (peramban selalu mengirim Origin
//     pada POST lintas-origin; "null" ditolak). Klien non-peramban tanpa Origin lolos tahap ini.
//   - Bila request terautentikasi, header X-CSRF-Token wajib dan harus sah untuk sesi itu.
//   - Request anonim (login, forgot-password, ...) dilindungi SameSite=Lax + pemeriksaan Origin
//     (docs/05 §8): tidak ada sesi untuk diikat.
func (m *Middleware) CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		if origins := r.Header.Values("Origin"); len(origins) > 1 || (len(origins) == 1 && !m.origins.Allows(origins[0])) {
			m.denyCSRF(w, r, "origin")
			return
		}
		if id, ok := domain.IdentityFromContext(r.Context()); ok && !m.csrf.Verify(id.SessionID, r.Header.Get(HeaderCSRF)) {
			m.denyCSRF(w, r, "token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (m *Middleware) denyCSRF(w http.ResponseWriter, r *http.Request, reason string) {
	m.log.DebugContext(r.Context(), "csrf ditolak", "reason", reason) // tanpa nilai token/origin
	m.rs.Error(w, r, CSRFFailed.New())
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}

// RequireAuth menolak request anonim dengan 401 authentication_required. Dipasang pada rute
// yang butuh identitas.
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := domain.IdentityFromContext(r.Context()); !ok {
			m.rs.Error(w, r, httpx.AuthenticationRequired.New())
			return
		}
		next.ServeHTTP(w, r)
	})
}
