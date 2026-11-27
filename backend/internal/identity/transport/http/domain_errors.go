package identityhttp

import (
	"errors"

	"github.com/master-abror/zago-core/backend/internal/identity/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	orgdomain "github.com/master-abror/zago-core/backend/internal/organization/domain"
)

// programmingErrors adalah galat yang hanya terjadi bila kode memanggil layanan atau repository
// dengan masukan yang tidak mungkin berasal dari klien yang sudah divalidasi DTO dan middleware:
// kesalahan server (500), bukan kesalahan klien dan bukan gangguan sementara (503). Access
// berasal dari paket organisasi (ADR-0023 butir 26a).
var programmingErrors = []error{
	orgdomain.ErrInvalidAccess,
	domain.ErrNoTransaction,
	domain.ErrInvalidID,
}

type codeRule struct {
	err  error
	code httpx.Code
}

// codeRules memetakan galat ke kode API sendiri atau kode umum. ErrPlatformRequired memakai
// permission_denied yang seragam: pemanggil tenant tidak mendapat petunjuk apa pun tentang
// operasi platform-saja (ADR-0023 butir 23).
var codeRules = []codeRule{
	{orgdomain.ErrPlatformRequired, httpx.PermissionDenied},
	{domain.ErrUserNotFound, UserNotFound},
}

type fieldRule struct {
	err   error
	field string
	code  string
	msg   string
}

// fieldRules memetakan galat masukan ke validation_failed dengan satu entri details.fields.
// Pesan tetap dan tidak memuat nilai klien; nama field mengikuti DTO dan parameter query.
var fieldRules = []fieldRule{
	{domain.ErrInvalidDisplayName, "display_name", "invalid", "The display name is invalid."},
	{domain.ErrInvalidFirstName, "first_name", "invalid", "The first name is invalid."},
	{domain.ErrInvalidLastName, "last_name", "invalid", "The last name is invalid."},
	{domain.ErrInvalidPhone, "phone", "invalid", "The phone number is invalid."},
	{domain.ErrInvalidAvatarURL, "avatar_url", "invalid", "The avatar URL must be an https URL."},
	{domain.ErrInvalidLocale, "locale", "invalid", "The locale is invalid."},
	{domain.ErrInvalidTimezone, "timezone", "invalid", "The timezone is invalid."},
	{domain.ErrInvalidFilter, "status", "invalid", "The status filter is not recognized."},
	{domain.ErrNothingToUpdate, "", "empty_patch", "The request changes nothing."},
}

// DomainError memetakan galat layanan identitas ke galat HTTP (ADR-0023 butir 21-26). Handler
// memanggilnya pada setiap galat dari layanan lalu menyerahkan hasilnya ke Responder.Error.
//
//   - Galat yang sudah *httpx.Error dikembalikan apa adanya.
//   - nil dan galat pemrograman (programmingErrors): 500.
//   - codeRules: kode sendiri atau kode umum (403, 404), menurut daftar.
//   - fieldRules: 400 validation_failed dengan satu entri details.fields.
//   - Selain itu (PostgreSQL gagal, konteks dibatalkan, dsb.): 503, gagal-tertutup.
//
// Penyebab (cause) hanya masuk log, tidak pernah ke respons. Galat tak dikenal tidak pernah
// menjadi sukses atau izin.
func DomainError(err error) *httpx.Error {
	if err == nil {
		return httpx.InternalError.New()
	}
	var ae *httpx.Error
	if errors.As(err, &ae) {
		return ae
	}
	for _, p := range programmingErrors {
		if errors.Is(err, p) {
			return httpx.InternalError.Wrap(err)
		}
	}
	for _, r := range codeRules {
		if errors.Is(err, r.err) {
			return r.code.Wrap(err)
		}
	}
	for _, r := range fieldRules {
		if errors.Is(err, r.err) {
			fields := []httpx.FieldError{{Field: r.field, Code: r.code, Message: r.msg}}
			return httpx.ValidationFailed.Wrap(err).WithDetails(map[string]any{"fields": fields})
		}
	}
	return httpx.ServiceUnavailable.Wrap(err)
}
