// Package authzhttp memuat middleware HTTP Authorization Engine (docs/06 §13).
package authzhttp

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// HeaderActiveGroup adalah header petunjuk grup aktif (docs/06 §4.2). Nilainya TIDAK dipercaya:
// ScopeResolver memverifikasinya ke database pada setiap request.
const HeaderActiveGroup = "X-Active-Group-ID"

// ScopeResolver menyelesaikan ScopeContext. *application.ScopeResolver memenuhinya.
type ScopeResolver interface {
	Resolve(ctx context.Context, id authdomain.Identity, groupHeader []string) (domain.ScopeContext, error)
}

// Deps adalah dependensi Middleware. Scope dan Responder wajib. Authorizer wajib hanya bila rute
// memakai Authorize (Authorize panik saat dipasang tanpa Authorizer). Audit opsional: nil berarti
// penolakan tidak direkam; composition root selalu memasangnya. Log nil = slog.Default.
type Deps struct {
	Scope      ScopeResolver
	Authorizer Authorizer
	Audit      modulesdk.AuditRecorder
	Responder  *httpx.Responder
	Log        *slog.Logger
}

// Middleware adalah rantai HTTP Authorization Engine. Urutan pemasangan pada rute terlindungi:
//
//	Authenticate -> CSRF -> RequireAuth -> Scope -> Authorize(action) -> handler
type Middleware struct {
	scope ScopeResolver
	authz Authorizer
	audit modulesdk.AuditRecorder
	rs    *httpx.Responder
	log   *slog.Logger
}

// New membuat Middleware; semua dependensi kecuali Log wajib.
func New(d Deps) (*Middleware, error) {
	switch {
	case d.Scope == nil:
		return nil, errors.New("authzhttp: Deps.Scope wajib")
	case d.Responder == nil:
		return nil, errors.New("authzhttp: Deps.Responder wajib")
	}
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	return &Middleware{scope: d.Scope, authz: d.Authorizer, audit: d.Audit, rs: d.Responder, log: log}, nil
}

// Scope menyelesaikan ScopeContext request dan memasangnya pada context. Dipasang SESUDAH
// RequireAuth; bila Identity tidak ada (salah urutan pemasangan) request ditolak 401 (fail-closed)
// alih-alih lanjut tanpa scope. Kegagalan verifikasi karena infrastruktur menjadi 503, bukan 403:
// klien tidak boleh mengira dirinya ditolak, dan request tidak boleh lanjut tanpa scope.
func (m *Middleware) Scope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := authdomain.IdentityFromContext(r.Context())
		if !ok {
			m.rs.Error(w, r, httpx.AuthenticationRequired.New())
			return
		}
		scope, err := m.scope.Resolve(r.Context(), id, r.Header.Values(HeaderActiveGroup))
		if errors.Is(err, domain.ErrInvalidScope) {
			// Identity yang melanggar invarian scope (mis. UUID organisasi nol) adalah cacat data atau
			// kode, bukan gangguan sementara: 500, bukan 503 yang mengundang klien mencoba lagi.
			m.log.ErrorContext(r.Context(), "scope sesi melanggar invarian", "error", err)
			m.rs.Error(w, r, httpx.InternalError.Wrap(err))
			return
		}
		if err != nil {
			m.log.ErrorContext(r.Context(), "resolusi scope gagal", "error", err)
			m.rs.Error(w, r, httpx.ServiceUnavailable.Wrap(err))
			return
		}
		next.ServeHTTP(w, r.WithContext(domain.WithScope(r.Context(), scope)))
	})
}
