package authzhttp

import (
	"errors"

	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// FieldError memetakan galat FieldGuard dan FieldFilter ke galat HTTP (ADR-0022 butir 44).
// Handler memanggilnya pada galat dari For, Filter, Apply, dan CheckWrite, lalu menyerahkan
// hasilnya ke Responder.Error. Semua galat gagal-tertutup: tidak ada yang menjadi izin.
//
//   - Penulisan field terbatas yang tidak boleh ditulis: 403 field_permission_denied.
//   - Sumber daya di luar organisasi scope: 403 scope_mismatch.
//   - Kesalahan pemrograman atau definisi rusak (Check salah bentuk, scope tak sah, tipe atau
//     aturan tak sah, kode field rusak, tipe tidak cocok), atau err nil: 500.
//   - Selain itu (PostgreSQL atau Redis gagal dsb.): 503.
//
// Penyebab (cause) hanya masuk log, tidak pernah ke respons; respons field_permission_denied
// tidak memuat nama field.
func FieldError(err error) *httpx.Error {
	switch {
	case err == nil:
		return httpx.InternalError.New()
	case errors.Is(err, domain.ErrFieldPermissionDenied):
		return FieldPermissionDenied.Wrap(err)
	case errors.Is(err, domain.ErrFieldScopeMismatch):
		return ScopeMismatch.Wrap(err)
	case errors.Is(err, application.ErrInvalidCheck),
		errors.Is(err, domain.ErrInvalidScope),
		errors.Is(err, domain.ErrInvalidFieldRules),
		errors.Is(err, domain.ErrInvalidFieldCode),
		errors.Is(err, domain.ErrFieldTypeMismatch):
		return httpx.InternalError.Wrap(err)
	default:
		return httpx.ServiceUnavailable.Wrap(err)
	}
}
