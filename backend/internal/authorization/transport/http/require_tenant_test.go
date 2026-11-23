package authzhttp_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// tenantGate memasang RequireTenant di depan handler yang mencatat apakah ia dicapai.
func tenantGate(t *testing.T, reached *bool) http.Handler {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw, err := authzhttp.New(authzhttp.Deps{Scope: &fakeResolver{}, Responder: httpx.NewResponder(log), Log: log})
	require.NoError(t, err)
	return mw.RequireTenant(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusNoContent)
	}))
}

// serveWithScope menjalankan h dengan ScopeContext di context; scope nil berarti context
// tanpa ScopeContext sama sekali (urutan middleware salah).
func serveWithScope(h http.Handler, scope *domain.ScopeContext) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if scope != nil {
		req = req.WithContext(domain.WithScope(req.Context(), *scope))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestRequireTenantPassesTenantScope(t *testing.T) {
	var reached bool
	org := id.NewID()
	scope, err := domain.NewScope(&org, nil)
	require.NoError(t, err)

	w := serveWithScope(tenantGate(t, &reached), &scope)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, reached)
}

func TestRequireTenantPassesTenantScopeWithActiveGroup(t *testing.T) {
	var reached bool
	org := id.NewID()
	group := id.NewID()
	scope, err := domain.NewScope(&org, &group)
	require.NoError(t, err)

	w := serveWithScope(tenantGate(t, &reached), &scope)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, reached)
}

func TestRequireTenantRejectsPlatformScopeWith400AndNeverReachesHandler(t *testing.T) {
	var reached bool
	scope := domain.ScopeContext{}
	require.True(t, scope.IsPlatform())

	w := serveWithScope(tenantGate(t, &reached), &scope)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Equal(t, "invalid_scope", errCode(t, w))
	require.Contains(t, w.Body.String(), "organization context")
	require.False(t, reached, "rute tenant tidak boleh berjalan di scope platform")
}

func TestRequireTenantFailsClosedWithoutScopeInContext(t *testing.T) {
	var reached bool

	w := serveWithScope(tenantGate(t, &reached), nil)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Equal(t, "internal_error", errCode(t, w))
	require.False(t, reached, "tanpa Scope request tidak boleh diteruskan")
}

func TestRequireTenantRejectionBodyHasNoOrganizationIDs(t *testing.T) {
	var reached bool
	scope := domain.ScopeContext{}

	w := serveWithScope(tenantGate(t, &reached), &scope)

	require.NotEmpty(t, w.Body.String())
	require.NotContains(t, w.Body.String(), "organization_id")
}
