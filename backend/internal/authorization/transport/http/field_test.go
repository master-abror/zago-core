package authzhttp_test

import (
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
)

func TestFieldPermissionDeniedIsRegisteredAs403WithItsOwnCode(t *testing.T) {
	got, ok := httpx.Lookup("field_permission_denied")
	require.True(t, ok, "kode harus ada di registry httpx")
	require.Equal(t, authzhttp.FieldPermissionDenied, got)
	require.Equal(t, http.StatusForbidden, got.Status)
	require.NotEqual(t, httpx.PermissionDenied.Name, got.Name, "kode sendiri, bukan permission_denied")
	require.NotEqual(t, authzhttp.ScopeMismatch.Name, got.Name)
	require.NotEmpty(t, got.Message)
}

func TestFieldErrorMapsEveryFailureAndNeverAllows(t *testing.T) {
	denied := &domain.FieldDeniedError{Fields: []string{"internal_cost"}, Total: 1}
	infra := errors.New("pg down")

	require.True(t, httpx.HasCode(authzhttp.FieldError(denied), authzhttp.FieldPermissionDenied))
	require.True(t, httpx.HasCode(authzhttp.FieldError(fmt.Errorf("simpan: %w", denied)), authzhttp.FieldPermissionDenied), "galat terbungkus tetap dikenali")
	require.True(t, httpx.HasCode(authzhttp.FieldError(domain.ErrFieldScopeMismatch), authzhttp.ScopeMismatch))

	programming := []error{
		application.ErrInvalidCheck,
		domain.ErrInvalidScope,
		domain.ErrInvalidFieldRules,
		domain.ErrInvalidFieldCode,
		domain.ErrFieldTypeMismatch,
		nil,
	}
	for _, err := range programming {
		require.True(t, httpx.HasCode(authzhttp.FieldError(err), httpx.InternalError), "galat %v harus 500", err)
	}

	mapped := authzhttp.FieldError(infra)
	require.True(t, httpx.HasCode(mapped, httpx.ServiceUnavailable), "infrastruktur gagal = 503, bukan izin")
	require.ErrorIs(t, mapped, infra, "penyebab dibawa untuk log")
}

func TestFieldPermissionDeniedRendersWithoutFieldNames(t *testing.T) {
	rs := httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	w := httptest.NewRecorder()

	rs.Error(w, req, authzhttp.FieldError(&domain.FieldDeniedError{Fields: []string{"internal_cost", "margin"}, Total: 2}))

	body := w.Body.String()
	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, body, "field_permission_denied", "tes tidak boleh lulus karena body kosong")
	require.NotContains(t, body, "internal_cost", "nama field dari klien tidak digaungkan")
	require.NotContains(t, body, "margin")
}

func TestFieldErrorNeverRendersTheCauseToTheClient(t *testing.T) {
	rs := httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()

	rs.Error(w, req, authzhttp.FieldError(errors.New("pg down at 10.0.0.5")))

	body := w.Body.String()
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.NotEmpty(t, body)
	require.NotContains(t, body, "pg down")
	require.NotContains(t, body, "10.0.0.5")
}
