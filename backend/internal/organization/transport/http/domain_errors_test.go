package orghttp_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	orghttp "github.com/master-abror/zago-core/backend/internal/organization/transport/http"
)

type sentinelCase struct {
	name   string
	err    error
	status int
	code   string
}

// sentinelCases memuat SEMUA sentinel organisasi dan anggota (ADR-0023 butir 10 dan 17) beserta
// status dan kode yang diharapkan. Daftar ini dipaksa lengkap oleh
// TestEveryExportedSentinelHasAMapping: sentinel baru tanpa baris di sini menggagalkan tes.
func sentinelCases() []sentinelCase {
	return []sentinelCase{
		{"ErrPlatformRequired", domain.ErrPlatformRequired, http.StatusForbidden, "permission_denied"},
		{"ErrOrganizationNotFound", domain.ErrOrganizationNotFound, http.StatusNotFound, "organization_not_found"},
		{"ErrMemberNotFound", domain.ErrMemberNotFound, http.StatusNotFound, "member_not_found"},
		{"ErrSlugTaken", domain.ErrSlugTaken, http.StatusConflict, "organization_slug_taken"},
		{"ErrOrganizationArchived", domain.ErrOrganizationArchived, http.StatusConflict, "organization_archived"},
		{"ErrInvalidTransition", domain.ErrInvalidTransition, http.StatusConflict, "invalid_status_transition"},
		{"ErrMemberExists", domain.ErrMemberExists, http.StatusConflict, "member_exists"},
		{"ErrCannotRemoveSelf", domain.ErrCannotRemoveSelf, http.StatusConflict, "cannot_remove_self"},
		{"ErrUserNotEligible", domain.ErrUserNotEligible, http.StatusUnprocessableEntity, "user_not_eligible"},
		{"ErrInvalidAccess", domain.ErrInvalidAccess, http.StatusInternalServerError, "internal_error"},
		{"ErrNoTransaction", domain.ErrNoTransaction, http.StatusInternalServerError, "internal_error"},
		{"ErrInvalidID", domain.ErrInvalidID, http.StatusInternalServerError, "internal_error"},
		{"ErrInvalidStatus", domain.ErrInvalidStatus, http.StatusInternalServerError, "internal_error"},
		{"ErrInvalidName", domain.ErrInvalidName, http.StatusBadRequest, "validation_failed"},
		{"ErrInvalidLegalName", domain.ErrInvalidLegalName, http.StatusBadRequest, "validation_failed"},
		{"ErrInvalidSlug", domain.ErrInvalidSlug, http.StatusBadRequest, "validation_failed"},
		{"ErrInvalidLocale", domain.ErrInvalidLocale, http.StatusBadRequest, "validation_failed"},
		{"ErrInvalidTimezone", domain.ErrInvalidTimezone, http.StatusBadRequest, "validation_failed"},
		{"ErrInvalidFilter", domain.ErrInvalidFilter, http.StatusBadRequest, "validation_failed"},
		{"ErrInvalidCursor", domain.ErrInvalidCursor, http.StatusBadRequest, "validation_failed"},
		{"ErrNothingToUpdate", domain.ErrNothingToUpdate, http.StatusBadRequest, "validation_failed"},
	}
}

// exportedSentinelNames membaca KODE SUMBER paket domain, application, dan repository organisasi
// dan mengembalikan nama setiap variabel terekspor berawalan Err.
func exportedSentinelNames(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	var names []string
	for _, pkg := range []string{"domain", "application", "repository"} {
		dir := filepath.Join("..", "..", pkg)
		entries, err := os.ReadDir(dir)
		require.NoError(t, err, dir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			f, perr := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, 0)
			require.NoError(t, perr, e.Name())
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.VAR {
					continue
				}
				for _, spec := range gd.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, n := range vs.Names {
						if strings.HasPrefix(n.Name, "Err") {
							names = append(names, n.Name)
						}
					}
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

// render menyerahkan galat ke Responder seperti handler dan mengembalikan respons.
func render(t *testing.T, err *httpx.Error) *httptest.ResponseRecorder {
	t.Helper()
	rs := httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()
	rs.Error(w, req, err)
	return w
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeErrorBody(t *testing.T, w *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var b errorBody
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &b), w.Body.String())
	return b
}

func fieldsOf(t *testing.T, err *httpx.Error) []httpx.FieldError {
	t.Helper()
	fields, ok := err.Details["fields"].([]httpx.FieldError)
	require.True(t, ok, "details.fields harus []httpx.FieldError")
	return fields
}

func TestOrganizationCodesAreRegisteredWithTheirStatuses(t *testing.T) {
	cases := []struct {
		code   httpx.Code
		status int
	}{
		{orghttp.OrganizationNotFound, http.StatusNotFound},
		{orghttp.MemberNotFound, http.StatusNotFound},
		{orghttp.OrganizationSlugTaken, http.StatusConflict},
		{orghttp.OrganizationArchived, http.StatusConflict},
		{orghttp.InvalidStatusTransition, http.StatusConflict},
		{orghttp.MemberExists, http.StatusConflict},
		{orghttp.CannotRemoveSelf, http.StatusConflict},
		{orghttp.UserNotEligible, http.StatusUnprocessableEntity},
	}
	for _, c := range cases {
		got, ok := httpx.Lookup(c.code.Name)
		require.True(t, ok, "kode %s harus ada di registry", c.code.Name)
		require.Equal(t, c.code, got)
		require.Equal(t, c.status, got.Status, c.code.Name)
		require.NotEmpty(t, got.Message, c.code.Name)
	}
}

func TestDomainErrorMapsEverySentinelToItsCodeAndStatus(t *testing.T) {
	for _, c := range sentinelCases() {
		for _, err := range []error{c.err, fmt.Errorf("service: %w", c.err)} {
			got := orghttp.DomainError(err)
			require.Equal(t, c.status, got.Code.Status, c.name)
			require.Equal(t, c.code, got.Code.Name, c.name)
			require.NotEqual(t, "service_unavailable", got.Code.Name, "%s jatuh ke 503", c.name)
			require.ErrorIs(t, got, c.err, "penyebab harus tetap dapat ditelusuri di log: %s", c.name)
		}
	}
}

func TestEveryExportedSentinelHasAMapping(t *testing.T) {
	found := exportedSentinelNames(t)
	require.NotEmpty(t, found, "pemindai tidak menemukan sentinel: jalur paket salah")

	var want []string
	for _, c := range sentinelCases() {
		want = append(want, c.name)
	}
	sort.Strings(want)

	require.Equal(t, want, found, "setiap sentinel terekspor harus punya baris di sentinelCases dan aturan di DomainError")
}

func TestDomainErrorFieldRulesNameTheirField(t *testing.T) {
	cases := []struct {
		err   error
		field string
		code  string
	}{
		{domain.ErrInvalidName, "name", "invalid"},
		{domain.ErrInvalidLegalName, "legal_name", "invalid"},
		{domain.ErrInvalidSlug, "slug", "invalid"},
		{domain.ErrInvalidLocale, "default_locale", "invalid"},
		{domain.ErrInvalidTimezone, "default_timezone", "invalid"},
		{domain.ErrInvalidFilter, "filter", "invalid"},
		{domain.ErrInvalidCursor, "cursor", "invalid_cursor"},
		{domain.ErrNothingToUpdate, "", "empty_patch"},
	}
	for _, c := range cases {
		got := orghttp.DomainError(c.err)
		fields := fieldsOf(t, got)
		require.Len(t, fields, 1, c.err.Error())
		require.Equal(t, c.field, fields[0].Field, c.err.Error())
		require.Equal(t, c.code, fields[0].Code, c.err.Error())
		require.NotEmpty(t, fields[0].Message, c.err.Error())
	}
}

func TestDomainErrorPassesThroughHTTPErrorsUnchanged(t *testing.T) {
	in := httpx.PayloadTooLarge.New()
	require.Same(t, in, orghttp.DomainError(in))
	require.Same(t, in, orghttp.DomainError(fmt.Errorf("decode: %w", in)))
}

func TestDomainErrorNilAndUnknownFailClosed(t *testing.T) {
	require.Equal(t, http.StatusInternalServerError, orghttp.DomainError(nil).Code.Status)

	unknown := errors.New("pq: connection refused")
	got := orghttp.DomainError(unknown)
	require.Equal(t, http.StatusServiceUnavailable, got.Code.Status)
	require.Equal(t, "service_unavailable", got.Code.Name)
	require.ErrorIs(t, got, unknown)
}

func TestDomainErrorResponsesNeverLeakSentinelTextOrCause(t *testing.T) {
	for _, c := range sentinelCases() {
		w := render(t, orghttp.DomainError(fmt.Errorf("select organizations: %w", c.err)))
		body := w.Body.String()
		require.NotEmpty(t, body, c.name)
		require.NotContains(t, body, "organization: ", c.name)
		require.NotContains(t, body, "select organizations", c.name)
		require.Equal(t, c.code, decodeErrorBody(t, w).Error.Code, c.name)
	}
}

func TestDomainErrorNotFoundIsIdenticalNoMatterHowTheServiceWrapsIt(t *testing.T) {
	// Organisasi milik tenant lain dan organisasi yang tidak ada sama-sama ErrOrganizationNotFound;
	// pembungkus berbeda tidak boleh mengubah satu bit pun dari respons.
	a := render(t, orghttp.DomainError(domain.ErrOrganizationNotFound))
	b := render(t, orghttp.DomainError(fmt.Errorf("get: %w", domain.ErrOrganizationNotFound)))

	require.Equal(t, a.Code, b.Code)
	require.Equal(t, decodeErrorBody(t, a), decodeErrorBody(t, b))
	require.Equal(t, "organization_not_found", decodeErrorBody(t, a).Error.Code)

	c := render(t, orghttp.DomainError(domain.ErrMemberNotFound))
	d := render(t, orghttp.DomainError(fmt.Errorf("get: %w", domain.ErrMemberNotFound)))
	require.Equal(t, decodeErrorBody(t, c), decodeErrorBody(t, d))
}

func TestDomainErrorPlatformRequiredIsTheUniformPermissionDenied(t *testing.T) {
	w := render(t, orghttp.DomainError(domain.ErrPlatformRequired))

	require.Equal(t, http.StatusForbidden, w.Code)
	body := decodeErrorBody(t, w)
	require.Equal(t, httpx.PermissionDenied.Name, body.Error.Code)
	require.Equal(t, httpx.PermissionDenied.Message, body.Error.Message)
	require.NotContains(t, strings.ToLower(w.Body.String()), "platform")
}
