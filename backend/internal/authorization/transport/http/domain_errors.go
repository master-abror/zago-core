package authzhttp

import (
	"errors"

	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/policy"
)

// programmingErrors adalah galat yang hanya terjadi bila kode memanggil engine atau penulis
// dengan masukan yang tidak mungkin berasal dari klien yang sudah divalidasi DTO: kesalahan
// server (500), bukan kesalahan klien.
var programmingErrors = []error{
	application.ErrInvalidCheck,
	domain.ErrInvalidScope,
	domain.ErrInvalidGrantTarget,
	domain.ErrInvalidRole,
	domain.ErrInvalidAssignment,
	policy.ErrCatalogUnavailable,
}

type codeRule struct {
	err  error
	code httpx.Code
}

// codeRules memetakan galat ke kode API sendiri atau kode umum. Urutannya adalah prioritas
// bila satu rantai galat memuat lebih dari satu sentinel: batas otoritas dan scope lebih dulu.
var codeRules = []codeRule{
	{domain.ErrAuthorityCeiling, GrantExceedsAuthority},
	{domain.ErrTargetOutsideScope, ScopeMismatch},
	{domain.ErrAssignmentScope, InvalidScope},
	{domain.ErrRoleNotFound, RoleNotFound},
	{domain.ErrUnknownPermission, PermissionNotFound},
	{domain.ErrRoleSlugTaken, RoleSlugTaken},
	{domain.ErrSystemRoleImmutable, RoleImmutable},
	{domain.ErrRoleArchived, RoleArchived},
	{domain.ErrAssignmentNotFound, httpx.ResourceNotFound},
	{domain.ErrAssignmentExists, AssignmentExists},
	{domain.ErrAssignmentRevoked, AssignmentRevoked},
	{domain.ErrLastSuperAdmin, LastSuperAdmin},
	{domain.ErrPolicyNotFound, httpx.ResourceNotFound},
	{domain.ErrPolicyCodeTaken, PolicyCodeTaken},
	{policy.ErrTooManyPolicies, PolicyLimitReached},
}

type fieldRule struct {
	err   error
	field string
	code  string
	msg   string
}

// fieldRules memetakan galat masukan ke validation_failed dengan satu entri details.fields.
// Pesan tetap dan tidak memuat nilai klien; nama field mengikuti DTO. Subjek dan grup yang
// tidak ditemukan memakai satu bentuk seragam (tidak ada, organisasi lain, dan tidak aktif tidak
// dapat dibedakan), jadi respons bukan alat menebak id.
var fieldRules = []fieldRule{
	{domain.ErrInvalidRoleName, "name", "invalid", "The role name is invalid."},
	{domain.ErrInvalidRoleSlug, "slug", "invalid", "The role slug is invalid."},
	{domain.ErrReservedRoleSlug, "slug", "reserved", "The role slug is reserved."},
	{domain.ErrInvalidRoleDescription, "description", "invalid", "The role description is invalid."},
	{domain.ErrRoleTypeForbidden, "type", "forbidden", "This role type cannot be created through the API."},
	{domain.ErrTooManyPermissions, "permissions", "too_many", "Too many permissions on one role."},
	{domain.ErrInvalidGrant, "permissions", "invalid", "A permission code is malformed."},
	{domain.ErrGroupNotFound, "group_id", "not_found", "The group was not found."},
	{domain.ErrSubjectNotFound, "subject_id", "not_found", "The subject was not found."},
	{domain.ErrAssignmentSubject, "subject_type", "invalid", "The subject is not valid for this assignment."},
	{domain.ErrAssignmentExpiry, "expires_at", "invalid", "The expiry is not valid for this assignment."},
	{policy.ErrInvalidPolicyCode, "code", "invalid", "The policy code is invalid."},
	{policy.ErrInvalidName, "name", "invalid", "The policy name is invalid."},
	{policy.ErrInvalidDescription, "description", "invalid", "The policy description is invalid."},
	{policy.ErrInvalidEffect, "effect", "invalid", "The policy effect must be allow or deny."},
	{policy.ErrSyntax, "expression", "syntax", "The policy expression is not valid."},
	{policy.ErrTooComplex, "expression", "too_complex", "The policy expression exceeds the allowed limits."},
	{policy.ErrInvalidActions, "actions", "invalid", "The policy actions are invalid."},
	{policy.ErrUnknownAction, "actions", "unknown", "A policy action is not a registered permission."},
	{domain.ErrProtectedPolicyAction, "actions", "protected", "Policy management actions cannot be restricted by a policy."},
	{domain.ErrInvalidPolicyStatus, "status", "invalid", "The policy status is invalid."},
	{domain.ErrEmptyPolicyPatch, "", "empty_patch", "The request changes nothing."},
}

// DomainError memetakan galat jalur pengelolaan otorisasi (RoleWriter, AssignmentWriter,
// PolicyWriter, dan validasi policy) ke galat HTTP (ADR-0022 butir 62). Handler memanggilnya
// pada setiap galat dari penulis lalu menyerahkan hasilnya ke Responder.Error.
//
//   - Galat yang sudah *httpx.Error dikembalikan apa adanya.
//   - nil dan galat pemrograman (programmingErrors): 500.
//   - codeRules: kode sendiri atau kode umum (403 grant_exceeds_authority, 403 scope_mismatch,
//     404, 409, dan seterusnya), menurut daftar.
//   - fieldRules: 400 validation_failed dengan satu entri details.fields.
//   - Selain itu (PostgreSQL gagal, konteks dibatalkan, dsb.): 503, gagal-tertutup.
//
// Penyebab (cause) hanya masuk log, tidak pernah ke respons. Respons tidak memuat nilai yang
// dikirim klien, daftar permission yang kurang, indeks, atau posisi ekspresi. Galat tak dikenal
// tidak pernah menjadi sukses atau izin.
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
