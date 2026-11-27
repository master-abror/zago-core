package identityhttp_test

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

	"github.com/master-abror/zago-core/backend/internal/identity/domain"
	identityhttp "github.com/master-abror/zago-core/backend/internal/identity/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	orgdomain "github.com/master-abror/zago-core/backend/internal/organization/domain"
)

type sentinelCase struct {
	name   string
	err    error
	status int
	code   string
}

// sentinelCases memuat SEMUA sentinel milik paket identity (domain, application, repository)
// beserta status dan kode yang diharapkan. Daftar ini dipaksa lengkap oleh
// TestEveryExportedSentinelHasAMapping: sentinel baru tanpa baris di sini menggagalkan tes.
func sentinelCases() []sentinelCase {
	return []sentinelCase{
		{"ErrUserNotFound", domain.ErrUserNotFound, http.StatusNotFound, "user_not_found"},
		{"ErrInvalidID", domain.ErrInvalidID, http.StatusInternalServerError, "internal_error"},
		{"ErrNoTransaction", domain.ErrNoTransaction, http.StatusInternalServerError, "internal_error"},
		{"ErrInvalidDisplayName", domain.ErrInvalidDisplayName, http.StatusBadRequest, "validation_failed"},
		{"ErrInvalidFirstName", domain.ErrInvalidFirstName, http.StatusBadRequest, "validation_failed"},
		{"ErrInvalidLastName", domain.ErrInvalidLastName, http.StatusBadRequest, "validation_failed"},
		{"ErrInvalidPhone", domain.ErrInvalidPhone, http.StatusBadRequest, "validation_failed"},
		{"ErrInvalidAvatarURL", domain.ErrInvalidAvatarURL, http.StatusBadRequest, "validation_failed"},
		{"ErrInvalidLocale", domain.ErrInvalidLocale, http.StatusBadRequest, "validation_failed"},
		{"ErrInvalidTimezone", domain.ErrInvalidTimezone, http.StatusBadRequest, "validation_failed"},
		{"ErrInvalidFilter", domain.ErrInvalidFilter, http.StatusBadRequest, "validation_failed"},
		{"ErrNothingToUpdate", domain.ErrNothingToUpdate, http.StatusBadRequest, "validation_failed"},
	}
}

// borrowedCases memuat sentinel milik paket organisasi yang juga keluar dari layanan identity
// (Access, ADR-0023 butir 26a). Mereka bukan bagian dari pemindaian paket identity.
func borrowedCases() []sentinelCase {
	return []sentinelCase{
		{"ErrPlatformRequired", orgdomain.ErrPlatformRequired, http.StatusForbidden, "permission_denied"},
		{"ErrInvalidAccess", orgdomain.ErrInvalidAccess, http.StatusInternalServerError, "internal_error"},
	}
}

// exportedSentinelNames membaca KODE SUMBER paket domain, application, dan repository identity
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

func allCases() []sentinelCase {
	return append(sentinelCases(), borrowedCases()...)
}

func TestUserNotFoundCodeIsRegisteredWithItsStatus(t *testing.T) {
	got, ok := httpx.Lookup(identityhttp.UserNotFound.Name)
	require.True(t, ok, "kode harus ada di registry")
	require.Equal(t, identityhttp.UserNotFound, got)
	require.Equal(t, "user_not_found", got.Name)
	require.Equal(t, http.StatusNotFound, got.Status)
	require.NotEmpty(t, got.Message)
}

func TestDomainErrorMapsEverySentinelToItsCodeAndStatus(t *testing.T) {
	for _, c := range allCases() {
		for _, err := range []error{c.err, fmt.Errorf("service: %w", c.err)} {
			got := identityhttp.DomainError(err)
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
		{domain.ErrInvalidDisplayName, "display_name", "invalid"},
		{domain.ErrInvalidFirstName, "first_name", "invalid"},
		{domain.ErrInvalidLastName, "last_name", "invalid"},
		{domain.ErrInvalidPhone, "phone", "invalid"},
		{domain.ErrInvalidAvatarURL, "avatar_url", "invalid"},
		{domain.ErrInvalidLocale, "locale", "invalid"},
		{domain.ErrInvalidTimezone, "timezone", "invalid"},
		{domain.ErrInvalidFilter, "status", "invalid"},
		{domain.ErrNothingToUpdate, "", "empty_patch"},
	}
	for _, c := range cases {
		got := identityhttp.DomainError(c.err)
		fields := fieldsOf(t, got)
		require.Len(t, fields, 1, c.err.Error())
		require.Equal(t, c.field, fields[0].Field, c.err.Error())
		require.Equal(t, c.code, fields[0].Code, c.err.Error())
		require.NotEmpty(t, fields[0].Message, c.err.Error())
	}
}

func TestDomainErrorPassesThroughHTTPErrorsUnchanged(t *testing.T) {
	in := httpx.PayloadTooLarge.New()
	require.Same(t, in, identityhttp.DomainError(in))
	require.Same(t, in, identityhttp.DomainError(fmt.Errorf("decode: %w", in)))
}

func TestDomainErrorNilAndUnknownFailClosed(t *testing.T) {
	require.Equal(t, http.StatusInternalServerError, identityhttp.DomainError(nil).Code.Status)

	unknown := errors.New("pq: connection refused")
	got := identityhttp.DomainError(unknown)
	require.Equal(t, http.StatusServiceUnavailable, got.Code.Status)
	require.Equal(t, "service_unavailable", got.Code.Name)
	require.ErrorIs(t, got, unknown)
}

func TestDomainErrorResponsesNeverLeakSentinelTextOrCause(t *testing.T) {
	for _, c := range allCases() {
		w := render(t, identityhttp.DomainError(fmt.Errorf("select users: %w", c.err)))
		body := w.Body.String()
		require.NotEmpty(t, body, c.name)
		require.NotContains(t, body, "identity: ", c.name)
		require.NotContains(t, body, "organization: ", c.name)
		require.NotContains(t, body, "select users", c.name)
		require.Equal(t, c.code, decodeErrorBody(t, w).Error.Code, c.name)
	}
}

func TestDomainErrorNotFoundIsIdenticalNoMatterHowTheServiceWrapsIt(t *testing.T) {
	// User organisasi lain, user yang tidak ada, dan user soft-deleted sama-sama ErrUserNotFound;
	// pembungkus berbeda tidak boleh mengubah satu bit pun dari respons.
	a := render(t, identityhttp.DomainError(domain.ErrUserNotFound))
	b := render(t, identityhttp.DomainError(fmt.Errorf("get: %w", domain.ErrUserNotFound)))

	require.Equal(t, a.Code, b.Code)
	require.Equal(t, decodeErrorBody(t, a), decodeErrorBody(t, b))
	require.Equal(t, "user_not_found", decodeErrorBody(t, a).Error.Code)
}

func TestDomainErrorPlatformRequiredIsTheUniformPermissionDenied(t *testing.T) {
	w := render(t, identityhttp.DomainError(orgdomain.ErrPlatformRequired))

	require.Equal(t, http.StatusForbidden, w.Code)
	body := decodeErrorBody(t, w)
	require.Equal(t, httpx.PermissionDenied.Name, body.Error.Code)
	require.Equal(t, httpx.PermissionDenied.Message, body.Error.Message)
	require.NotContains(t, strings.ToLower(w.Body.String()), "platform")
}
