package orghttp

import (
	"errors"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
)

// programmingErrors adalah galat yang hanya terjadi bila kode memanggil layanan atau repository
// dengan masukan yang tidak mungkin berasal dari klien yang sudah divalidasi DTO dan middleware:
// kesalahan server (500), bukan kesalahan klien dan bukan gangguan sementara (503).
var programmingErrors = []error{
	domain.ErrInvalidAccess,
	domain.ErrNoTransaction,
	domain.ErrInvalidID,
	domain.ErrInvalidStatus,
}

type codeRule struct {
	err  error
	code httpx.Code
}

// codeRules memetakan galat ke kode API sendiri atau kode umum (ADR-0023 butir 18).
// ErrPlatformRequired memakai permission_denied yang seragam: pemanggil tenant tidak
// mendapat petunjuk apa pun tentang operasi platform-saja.
var codeRules = []codeRule{
	{domain.ErrPlatformRequired, httpx.PermissionDenied},
	{domain.ErrOrganizationNotFound, OrganizationNotFound},
	{domain.ErrMemberNotFound, MemberNotFound},
	{domain.ErrSlugTaken, OrganizationSlugTaken},
	{domain.ErrOrganizationArchived, OrganizationArchived},
	{domain.ErrInvalidTransition, InvalidStatusTransition},
	{domain.ErrMemberExists, MemberExists},
	{domain.ErrCannotRemoveSelf, CannotRemoveSelf},
	{domain.ErrUserNotEligible, UserNotEligible},
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
	{domain.ErrInvalidName, "name", "invalid", "The organization name is invalid."},
	{domain.ErrInvalidLegalName, "legal_name", "invalid", "The legal name is invalid."},
	{domain.ErrInvalidSlug, "slug", "invalid", "The slug is invalid."},
	{domain.ErrInvalidLocale, "default_locale", "invalid", "The default locale is invalid."},
	{domain.ErrInvalidTimezone, "default_timezone", "invalid", "The default timezone is invalid."},
	{domain.ErrInvalidFilter, "status", "invalid", "The status filter is not recognized."},
	{domain.ErrInvalidCursor, "cursor", "invalid_cursor", "The cursor is invalid."},
	{domain.ErrNothingToUpdate, "", "empty_patch", "The request changes nothing."},
}

// DomainError memetakan galat layanan organisasi dan anggota ke galat HTTP (ADR-0023 butir 18).
// Handler memanggilnya pada setiap galat dari layanan lalu menyerahkan hasilnya ke
// Responder.Error.
//
//   - Galat yang sudah *httpx.Error dikembalikan apa adanya.
//   - nil dan galat pemrograman (programmingErrors): 500.
//   - codeRules: kode sendiri atau kode umum (403, 404, 409, 422), menurut daftar.
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
