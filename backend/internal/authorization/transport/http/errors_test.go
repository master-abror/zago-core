package authzhttp_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

func TestScopeMismatchIsRegisteredAs403WithItsOwnCode(t *testing.T) {
	got, ok := httpx.Lookup("scope_mismatch")
	require.True(t, ok, "kode harus ada di registry httpx")
	require.Equal(t, authzhttp.ScopeMismatch, got)
	require.Equal(t, http.StatusForbidden, got.Status)
	require.NotEqual(t, httpx.PermissionDenied.Name, got.Name, "kode sendiri, bukan permission_denied")
	require.NotEmpty(t, got.Message)
}

func TestScopeMismatchRendersThroughResponder(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	w := httptest.NewRecorder()

	rs.Error(w, req, authzhttp.ScopeMismatch.New())

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, "scope_mismatch", errCode(t, w))
}
