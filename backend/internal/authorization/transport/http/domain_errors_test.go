package authzhttp_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/policy"
)

// renderErr menyerahkan galat ke Responder seperti handler dan mengembalikan respons.
func renderErr(t *testing.T, err *httpx.Error) *httptest.ResponseRecorder {
	t.Helper()
	rs := httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	w := httptest.NewRecorder()
	rs.Error(w, req, err)
	return w
}

// fieldsOf mengambil details.fields dari galat validasi.
func fieldsOf(t *testing.T, err error) []httpx.FieldError {
	t.Helper()
	var ae *httpx.Error
	require.ErrorAs(t, err, &ae)
	fields, ok := ae.Details["fields"].([]httpx.FieldError)
	require.True(t, ok, "details.fields harus []httpx.FieldError")
	return fields
}

func TestManagementCodesAreRegisteredWithTheirStatuses(t *testing.T) {
	cases := []struct {
		code   httpx.Code
		status int
	}{
		{authzhttp.RoleNotFound, http.StatusNotFound},
		{authzhttp.PermissionNotFound, http.StatusUnprocessableEntity},
		{authzhttp.GrantExceedsAuthority, http.StatusForbidden},
		{authzhttp.InvalidScope, http.StatusBadRequest},
		{authzhttp.RoleSlugTaken, http.StatusConflict},
		{authzhttp.RoleImmutable, http.StatusConflict},
		{authzhttp.RoleArchived, http.StatusConflict},
		{authzhttp.AssignmentExists, http.StatusConflict},
		{authzhttp.AssignmentRevoked, http.StatusConflict},
		{authzhttp.LastSuperAdmin, http.StatusConflict},
		{authzhttp.PolicyCodeTaken, http.StatusConflict},
		{authzhttp.PolicyLimitReached, http.StatusConflict},
	}
	for _, c := range cases {
		got, ok := httpx.Lookup(c.code.Name)
		require.True(t, ok, "kode %s harus ada di registry", c.code.Name)
		require.Equal(t, c.code, got)
		require.Equal(t, c.status, got.Status, c.code.Name)
		require.NotEmpty(t, got.Message, c.code.Name)
		require.NotEqual(t, httpx.PermissionDenied.Name, got.Name)
	}
}

func TestDomainErrorMapsEverySentinelToItsCode(t *testing.T) {
	cases := []struct {
		err  error
		code httpx.Code
	}{
		{domain.ErrAuthorityCeiling, authzhttp.GrantExceedsAuthority},
		{domain.ErrTargetOutsideScope, authzhttp.ScopeMismatch},
		{domain.ErrAssignmentScope, authzhttp.InvalidScope},
		{domain.ErrRoleNotFound, authzhttp.RoleNotFound},
		{domain.ErrUnknownPermission, authzhttp.PermissionNotFound},
		{domain.ErrRoleSlugTaken, authzhttp.RoleSlugTaken},
		{domain.ErrSystemRoleImmutable, authzhttp.RoleImmutable},
		{domain.ErrRoleArchived, authzhttp.RoleArchived},
		{domain.ErrAssignmentNotFound, httpx.ResourceNotFound},
		{domain.ErrAssignmentExists, authzhttp.AssignmentExists},
		{domain.ErrAssignmentRevoked, authzhttp.AssignmentRevoked},
		{domain.ErrLastSuperAdmin, authzhttp.LastSuperAdmin},
		{domain.ErrPolicyNotFound, httpx.ResourceNotFound},
		{domain.ErrPolicyCodeTaken, authzhttp.PolicyCodeTaken},
		{policy.ErrTooManyPolicies, authzhttp.PolicyLimitReached},
	}
	for _, c := range cases {
		got := authzhttp.DomainError(c.err)
		require.True(t, httpx.HasCode(got, c.code), "galat %v harus %s, dapat %s", c.err, c.code.Name, got.Code.Name)
		require.ErrorIs(t, got, c.err, "penyebab dibawa untuk log")

		wrapped := authzhttp.DomainError(fmt.Errorf("simpan: %w", c.err))
		require.True(t, httpx.HasCode(wrapped, c.code), "galat terbungkus %v tetap dikenali", c.err)
	}
}

func TestDomainErrorMapsInputErrorsToOneFieldEach(t *testing.T) {
	cases := []struct {
		err   error
		field string
		code  string
	}{
		{domain.ErrInvalidRoleName, "name", "invalid"},
		{domain.ErrInvalidRoleSlug, "slug", "invalid"},
		{domain.ErrReservedRoleSlug, "slug", "reserved"},
		{domain.ErrInvalidRoleDescription, "description", "invalid"},
		{domain.ErrRoleTypeForbidden, "type", "forbidden"},
		{domain.ErrTooManyPermissions, "permissions", "too_many"},
		{domain.ErrInvalidGrant, "permissions", "invalid"},
		{domain.ErrGroupNotFound, "group_id", "not_found"},
		{domain.ErrSubjectNotFound, "subject_id", "not_found"},
		{domain.ErrAssignmentSubject, "subject_type", "invalid"},
		{domain.ErrAssignmentExpiry, "expires_at", "invalid"},
		{policy.ErrInvalidPolicyCode, "code", "invalid"},
		{policy.ErrInvalidName, "name", "invalid"},
		{policy.ErrInvalidDescription, "description", "invalid"},
		{policy.ErrInvalidEffect, "effect", "invalid"},
		{policy.ErrSyntax, "expression", "syntax"},
		{policy.ErrTooComplex, "expression", "too_complex"},
		{policy.ErrInvalidActions, "actions", "invalid"},
		{policy.ErrUnknownAction, "actions", "unknown"},
		{domain.ErrProtectedPolicyAction, "actions", "protected"},
		{domain.ErrInvalidPolicyStatus, "status", "invalid"},
		{domain.ErrEmptyPolicyPatch, "", "empty_patch"},
	}
	for _, c := range cases {
		got := authzhttp.DomainError(fmt.Errorf("validasi: %w", c.err))
		require.True(t, httpx.HasCode(got, httpx.ValidationFailed), "galat %v harus validation_failed", c.err)
		require.ErrorIs(t, got, c.err, "penyebab dibawa untuk log")
		fields := fieldsOf(t, got)
		require.Len(t, fields, 1, "%v", c.err)
		require.Equal(t, c.field, fields[0].Field, "%v", c.err)
		require.Equal(t, c.code, fields[0].Code, "%v", c.err)
		require.NotEmpty(t, fields[0].Message, "%v", c.err)
	}
}

func TestDomainErrorTreatsProgrammingErrorsAsServerFailures(t *testing.T) {
	programming := []error{
		application.ErrInvalidCheck,
		domain.ErrInvalidScope,
		domain.ErrInvalidGrantTarget,
		domain.ErrInvalidRole,
		domain.ErrInvalidAssignment,
		policy.ErrCatalogUnavailable,
		nil,
	}
	for _, err := range programming {
		got := authzhttp.DomainError(err)
		require.True(t, httpx.HasCode(got, httpx.InternalError), "galat %v harus 500", err)
	}
	wrapped := authzhttp.DomainError(fmt.Errorf("cek: %w", domain.ErrInvalidScope))
	require.True(t, httpx.HasCode(wrapped, httpx.InternalError))
	require.ErrorIs(t, wrapped, domain.ErrInvalidScope)
}

func TestDomainErrorUnknownFailuresAreUnavailableAndNeverLeak(t *testing.T) {
	infra := errors.New("pq: password=hunter2 connection refused")
	got := authzhttp.DomainError(fmt.Errorf("tulis: %w", infra))
	require.True(t, httpx.HasCode(got, httpx.ServiceUnavailable), "galat tak dikenal gagal tertutup 503")
	require.ErrorIs(t, got, infra, "penyebab dibawa untuk log")

	w := renderErr(t, got)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.NotEmpty(t, w.Body.String())
	require.NotContains(t, w.Body.String(), "hunter2")
	require.NotContains(t, w.Body.String(), "connection refused")

	require.True(t, httpx.HasCode(authzhttp.DomainError(context.DeadlineExceeded), httpx.ServiceUnavailable))
	require.True(t, httpx.HasCode(authzhttp.DomainError(context.Canceled), httpx.ServiceUnavailable))
}

func TestDomainErrorPassesThroughAnExistingHTTPError(t *testing.T) {
	want := authzhttp.RoleNotFound.New()
	got := authzhttp.DomainError(fmt.Errorf("handler: %w", want))
	require.Same(t, want, got)
}

func TestDomainErrorPriorityWhenAChainHoldsSeveralSentinels(t *testing.T) {
	both := errors.Join(domain.ErrRoleNotFound, domain.ErrAuthorityCeiling)
	require.True(t, httpx.HasCode(authzhttp.DomainError(both), authzhttp.GrantExceedsAuthority), "batas otoritas mendahului")

	withBug := errors.Join(domain.ErrAuthorityCeiling, domain.ErrInvalidScope)
	require.True(t, httpx.HasCode(authzhttp.DomainError(withBug), httpx.InternalError), "galat pemrograman mendahului semuanya")

	fieldAndCode := errors.Join(policy.ErrSyntax, domain.ErrPolicyCodeTaken)
	require.True(t, httpx.HasCode(authzhttp.DomainError(fieldAndCode), authzhttp.PolicyCodeTaken), "kode langsung mendahului galat field")
}

func TestGrantExceedsAuthorityResponseNeverListsPermissions(t *testing.T) {
	ce := &domain.CeilingError{Missing: []string{"invoice.secret_approve", "invoice.secret_void"}, Total: 2}
	got := authzhttp.DomainError(fmt.Errorf("beri role: %w", ce))
	require.True(t, httpx.HasCode(got, authzhttp.GrantExceedsAuthority))

	w := renderErr(t, got)
	body := w.Body.String()
	require.Equal(t, http.StatusForbidden, w.Code)
	require.NotEmpty(t, body)
	require.Equal(t, "grant_exceeds_authority", errCode(t, w))
	require.NotContains(t, body, "invoice.secret_approve")
	require.NotContains(t, body, "invoice.secret_void")
}

func TestValidationResponsesNeverEchoClientValuesIndexesOrPositions(t *testing.T) {
	unknownAction := fmt.Errorf("%w: actions[%d]", policy.ErrUnknownAction, 3)
	syntax := fmt.Errorf("%w (posisi %d): token %q", policy.ErrSyntax, 7, "rahasia_attr")
	for _, err := range []error{unknownAction, syntax} {
		w := renderErr(t, authzhttp.DomainError(err))
		body := w.Body.String()
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.NotEmpty(t, body)
		require.Equal(t, "validation_failed", errCode(t, w))
		require.NotContains(t, body, "actions[3]")
		require.NotContains(t, body, "posisi")
		require.NotContains(t, body, "rahasia_attr")
	}
}

func TestNotFoundResponseDoesNotEchoTheWrappedIdentifier(t *testing.T) {
	err := fmt.Errorf("role %s: %w", "9f1c-secret-id", domain.ErrRoleNotFound)
	w := renderErr(t, authzhttp.DomainError(err))
	require.Equal(t, http.StatusNotFound, w.Code)
	require.NotEmpty(t, w.Body.String())
	require.Equal(t, "role_not_found", errCode(t, w))
	require.NotContains(t, w.Body.String(), "9f1c-secret-id")
}
