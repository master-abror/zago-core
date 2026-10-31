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
)

// HeaderActiveGroup adalah header petunjuk grup aktif (docs/06 §4.2). Nilainya TIDAK dipercaya:
// ScopeResolver memverifikasinya ke database pada setiap request.
const HeaderActiveGroup = "X-Active-Group-ID"

// ScopeResolver menyelesaikan ScopeContext. *application.ScopeResolver memenuhinya.
type ScopeResolver interface {
	Resolve(ctx context.Context, id authdomain.Identity, groupHeader []string) (domain.ScopeContext, error)
}

// Deps adalah dependensi Middleware.
type Deps struct {
	Scope     ScopeResolver
	Responder *httpx.Responder
	Log       *slog.Logger
}

// Middleware adalah rantai HTTP Authorization Engine. Urutan pemasangan pada rute terlindungi:
//
//	Authenticate -> CSRF -> RequireAuth -> Scope -> [Authorize(action), T3] -> handler
type Middleware struct {
	scope ScopeResolver
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
	return &Middleware{scope: d.Scope, rs: d.Responder, log: log}, nil
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
		if err != nil {
			m.log.ErrorContext(r.Context(), "resolusi scope gagal", "error", err)
			m.rs.Error(w, r, httpx.ServiceUnavailable.Wrap(err))
			return
		}
		next.ServeHTTP(w, r.WithContext(domain.WithScope(r.Context(), scope)))
	})
}
