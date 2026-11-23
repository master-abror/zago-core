package authzhttp

import (
	"net/http"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// RequireTenant menolak request yang ScopeContext-nya bersifat platform (tanpa organisasi).
// Dipasang untuk rute yang hanya bermakna di dalam satu organisasi (grup, undangan, dan
// sejenisnya), sesudah Scope dan sebelum Authorize:
//
//	Authenticate -> CSRF -> RequireAuth -> Scope -> RequireTenant -> Authorize(action) -> handler
//
// Aturan (ADR-0023 butir 18):
//
//   - Scope tenant (dengan atau tanpa grup aktif): request diteruskan.
//   - Scope platform: 400 invalid_scope. Operator platform yang memanggil rute tenant harus
//     lebih dulu memilih organisasi (pindah organisasi), jadi ini kesalahan klien, bukan 403.
//   - Tidak ada ScopeContext pada context (urutan pemasangan salah): 500 internal_error dan
//     dicatat, tidak pernah diteruskan (gagal-tertutup), sama seperti Authorize.
//
// Organisasi yang dipakai handler tetap HANYA dari ScopeContext, bukan dari body, path, atau
// query; middleware ini tidak membaca apa pun dari request.
func (m *Middleware) RequireTenant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, found := domain.ScopeFromContext(r.Context())
		if !found {
			m.log.ErrorContext(r.Context(), "RequireTenant berjalan tanpa Scope: urutan middleware salah")
			m.rs.Error(w, r, httpx.InternalError.New())
			return
		}
		if _, isTenant := scope.Tenant(); !isTenant {
			m.rs.Error(w, r, InvalidScope.New().WithMessage("This operation requires an organization context."))
			return
		}
		next.ServeHTTP(w, r)
	})
}
