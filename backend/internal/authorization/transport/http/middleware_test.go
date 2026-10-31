package authzhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

type resolveCall struct {
	identity authdomain.Identity
	header   []string
}

type fakeResolver struct {
	scope domain.ScopeContext
	err   error
	calls []resolveCall
}

func (f *fakeResolver) Resolve(_ context.Context, ident authdomain.Identity, header []string) (domain.ScopeContext, error) {
	f.calls = append(f.calls, resolveCall{identity: ident, header: header})
	return f.scope, f.err
}

type rig struct {
	handler http.Handler
	reached bool
	seen    domain.ScopeContext
	hasSeen bool
}

func newRig(t *testing.T, res *fakeResolver) *rig {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw, err := authzhttp.New(authzhttp.Deps{Scope: res, Responder: httpx.NewResponder(log), Log: log})
	require.NoError(t, err)

	r := &rig{}
	r.handler = mw.Scope(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.reached = true
		r.seen, r.hasSeen = domain.ScopeFromContext(req.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	return r
}

func tenantIdentity() authdomain.Identity {
	org := id.NewID()
	return authdomain.Identity{UserID: id.NewID(), OrganizationID: &org, SessionID: id.NewID(), AuthLevel: authdomain.AuthLevelPassword}
}

func (r *rig) do(headerValues []string, ident *authdomain.Identity) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	for _, v := range headerValues {
		req.Header.Add(authzhttp.HeaderActiveGroup, v)
	}
	if ident != nil {
		req = req.WithContext(authdomain.WithIdentity(req.Context(), *ident))
	}
	w := httptest.NewRecorder()
	r.handler.ServeHTTP(w, req)
	return w
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return body.Error.Code
}

func TestNewRequiresDependencies(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := authzhttp.New(authzhttp.Deps{Responder: httpx.NewResponder(log)})
	require.Error(t, err)
	_, err = authzhttp.New(authzhttp.Deps{Scope: &fakeResolver{}})
	require.Error(t, err)
}

func TestScopeRejectsRequestWithoutIdentityAndNeverResolves(t *testing.T) {
	res := &fakeResolver{}
	r := newRig(t, res)

	w := r.do([]string{id.NewID().String()}, nil)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, "authentication_required", errCode(t, w))
	require.Empty(t, res.calls, "tanpa Identity resolver tidak boleh dipanggil")
	require.False(t, r.reached, "request tidak boleh lanjut tanpa scope")
}

func TestScopePassesIdentityAndEveryHeaderValueToTheResolver(t *testing.T) {
	ident := tenantIdentity()
	res := &fakeResolver{}
	r := newRig(t, res)
	g1, g2 := id.NewID().String(), id.NewID().String()

	r.do([]string{g1, g2}, &ident)

	require.Len(t, res.calls, 1)
	require.Equal(t, ident, res.calls[0].identity)
	require.Equal(t, []string{g1, g2}, res.calls[0].header,
		"semua nilai header diteruskan: resolver yang memutuskan header ganda dibuang")
}

func TestScopeStoresResolvedScopeForTheNextHandler(t *testing.T) {
	ident := tenantIdentity()
	grp := id.NewID()
	scope, err := domain.NewScope(ident.OrganizationID, &grp)
	require.NoError(t, err)
	r := newRig(t, &fakeResolver{scope: scope})

	w := r.do([]string{grp.String()}, &ident)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, r.reached)
	require.True(t, r.hasSeen)
	gotOrg, _ := r.seen.Tenant()
	gotGrp, ok := r.seen.Group()
	require.Equal(t, *ident.OrganizationID, gotOrg)
	require.True(t, ok)
	require.Equal(t, grp, gotGrp)
}

func TestScopeForPlatformIdentityIsPlatformLevel(t *testing.T) {
	platform := authdomain.Identity{UserID: id.NewID(), SessionID: id.NewID(), AuthLevel: authdomain.AuthLevelPassword}
	scope, err := domain.NewScope(nil, nil)
	require.NoError(t, err)
	r := newRig(t, &fakeResolver{scope: scope})

	w := r.do(nil, &platform)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, r.hasSeen)
	require.True(t, r.seen.IsPlatform())
}

func TestScopeFailsClosedWith503WhenResolutionFails(t *testing.T) {
	ident := tenantIdentity()
	r := newRig(t, &fakeResolver{err: errors.New("dial tcp 10.0.0.5:5432: connection refused")})

	w := r.do([]string{id.NewID().String()}, &ident)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Equal(t, "service_unavailable", errCode(t, w))
	require.False(t, r.reached, "request tidak boleh lanjut tanpa scope")
	require.NotContains(t, w.Body.String(), "10.0.0.5", "detail infrastruktur tidak boleh bocor ke klien")
	require.NotContains(t, w.Body.String(), "connection refused")
}
