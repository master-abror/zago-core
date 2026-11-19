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

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

const routeAction = "organization.read"

type fakeAuthorizer struct {
	decision domain.Decision
	err      error
	checks   []domain.Check
}

func (f *fakeAuthorizer) Can(_ context.Context, c domain.Check) (domain.Decision, error) {
	f.checks = append(f.checks, c)
	return f.decision, f.err
}

type auditCall struct {
	entry    modulesdk.ActivityEntry
	scope    kernel.Scope
	hasScope bool
}

type fakeAudit struct {
	calls []auditCall
}

func (f *fakeAudit) Record(ctx context.Context, e modulesdk.ActivityEntry) {
	s, ok := kernel.ScopeFromContext(ctx)
	f.calls = append(f.calls, auditCall{entry: e, scope: s, hasScope: ok})
}

type authzRig struct {
	handler http.Handler
	reached bool
}

func newAuthzRig(t *testing.T, authz *fakeAuthorizer, audit modulesdk.AuditRecorder) *authzRig {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw, err := authzhttp.New(authzhttp.Deps{
		Scope:      &fakeResolver{},
		Authorizer: authz,
		Audit:      audit,
		Responder:  httpx.NewResponder(log),
		Log:        log,
	})
	require.NoError(t, err)

	r := &authzRig{}
	r.handler = mw.Authorize(routeAction)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		r.reached = true
		w.WriteHeader(http.StatusNoContent)
	}))
	return r
}

func (r *authzRig) do(ident *authdomain.Identity, scope *domain.ScopeContext) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	ctx := req.Context()
	if ident != nil {
		ctx = authdomain.WithIdentity(ctx, *ident)
	}
	if scope != nil {
		ctx = domain.WithScope(ctx, *scope)
	}
	w := httptest.NewRecorder()
	r.handler.ServeHTTP(w, req.WithContext(ctx))
	return w
}

func scopeOf(t *testing.T, ident authdomain.Identity) domain.ScopeContext {
	t.Helper()
	s, err := domain.NewScope(ident.OrganizationID, nil)
	require.NoError(t, err)
	return s
}

func platformIdentity() authdomain.Identity {
	return authdomain.Identity{UserID: id.NewID(), SessionID: id.NewID(), AuthLevel: authdomain.AuthLevelPassword}
}

func TestAuthorizeAllowedReachesHandlerWithTheExactCheck(t *testing.T) {
	ident := tenantIdentity()
	scope := scopeOf(t, ident)
	authz := &fakeAuthorizer{decision: domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}}
	audit := &fakeAudit{}
	r := newAuthzRig(t, authz, audit)

	w := r.do(&ident, &scope)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, r.reached)
	require.Len(t, authz.checks, 1)
	require.Equal(t, ident, authz.checks[0].Identity)
	require.Equal(t, scope, authz.checks[0].Scope)
	require.Equal(t, routeAction, authz.checks[0].Action)
	require.Nil(t, authz.checks[0].Resource, "Authorize memeriksa tanpa sumber daya tertentu")
	require.Empty(t, audit.calls, "izin tidak direkam sebagai penolakan")
}

func TestAuthorizePassesPlatformIdentityUnchanged(t *testing.T) {
	ident := platformIdentity()
	scope := scopeOf(t, ident)
	authz := &fakeAuthorizer{decision: domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelPlatform}}
	r := newAuthzRig(t, authz, nil)

	w := r.do(&ident, &scope)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Len(t, authz.checks, 1)
	require.Nil(t, authz.checks[0].Identity.OrganizationID)
	require.True(t, authz.checks[0].Scope.IsPlatform())
}

func TestAuthorizeDeniedIsUniform403AndNeverReachesHandler(t *testing.T) {
	for _, tc := range []struct {
		name       string
		decision   domain.Decision
		wantCode   string
		wantReason string
	}{
		{"no permission", domain.Decision{Reason: domain.ReasonNoPermission}, "permission_denied", "no_permission"},
		{"scope mismatch", domain.Decision{Reason: domain.ReasonScopeMismatch}, "scope_mismatch", "scope_mismatch"},
		{"zero decision", domain.Decision{}, "permission_denied", "unspecified"},
		{"unknown reason", domain.Decision{Reason: "whatever"}, "permission_denied", "unspecified"},
		{"granted reason but not allowed", domain.Decision{Reason: domain.ReasonGranted, Level: domain.LevelPlatform}, "permission_denied", "unspecified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ident := tenantIdentity()
			scope := scopeOf(t, ident)
			audit := &fakeAudit{}
			r := newAuthzRig(t, &fakeAuthorizer{decision: tc.decision}, audit)

			w := r.do(&ident, &scope)

			require.Equal(t, http.StatusForbidden, w.Code)
			require.Equal(t, tc.wantCode, errCode(t, w))
			require.False(t, r.reached)
			require.NotContains(t, w.Body.String(), routeAction, "aksi tidak boleh bocor ke klien")
			require.NotContains(t, w.Body.String(), "no_permission", "alasan internal tidak boleh bocor ke klien")

			require.Len(t, audit.calls, 1)
			require.Equal(t, authzhttp.ActivityAccessDenied, audit.calls[0].entry.Action)
			require.Equal(t, modulesdk.ResultDenied, audit.calls[0].entry.Result)
			require.Equal(t, routeAction, audit.calls[0].entry.Metadata["required_action"])
			require.Equal(t, tc.wantReason, audit.calls[0].entry.Metadata["reason"])
			require.Equal(t, http.MethodGet, audit.calls[0].entry.Metadata["method"])
		})
	}
}

func TestAuthorizeDeniedStillDeniesWithoutAnAuditRecorder(t *testing.T) {
	ident := tenantIdentity()
	scope := scopeOf(t, ident)
	r := newAuthzRig(t, &fakeAuthorizer{decision: domain.Decision{Reason: domain.ReasonNoPermission}}, nil)

	w := r.do(&ident, &scope)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.False(t, r.reached)
}

func TestAuthorizeAuditCarriesTheRequestScope(t *testing.T) {
	orgIdent := tenantIdentity()
	grp := id.NewID()
	groupScope, err := domain.NewScope(orgIdent.OrganizationID, &grp)
	require.NoError(t, err)
	plat := platformIdentity()

	for _, tc := range []struct {
		name      string
		ident     authdomain.Identity
		scope     domain.ScopeContext
		wantType  string
		wantScope func() string
	}{
		{"organization", orgIdent, scopeOf(t, orgIdent), "organization", func() string { return orgIdent.OrganizationID.String() }},
		{"group", orgIdent, groupScope, "group", func() string { return grp.String() }},
		{"platform", plat, scopeOf(t, plat), "platform", func() string { return "00000000-0000-0000-0000-000000000000" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			audit := &fakeAudit{}
			r := newAuthzRig(t, &fakeAuthorizer{decision: domain.Decision{Reason: domain.ReasonNoPermission}}, audit)
			ident, scope := tc.ident, tc.scope

			r.do(&ident, &scope)

			require.Len(t, audit.calls, 1)
			require.True(t, audit.calls[0].hasScope)
			require.Equal(t, tc.wantType, audit.calls[0].scope.Type)
			require.Equal(t, tc.wantScope(), audit.calls[0].scope.ID.String())
		})
	}
}

func TestAuthorizeWithoutIdentityIs401AndNeverAsksTheEngine(t *testing.T) {
	ident := tenantIdentity()
	scope := scopeOf(t, ident)
	authz := &fakeAuthorizer{decision: domain.Decision{Allowed: true, Level: domain.LevelPlatform}}
	audit := &fakeAudit{}
	r := newAuthzRig(t, authz, audit)

	w := r.do(nil, &scope)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, "authentication_required", errCode(t, w))
	require.Empty(t, authz.checks)
	require.Empty(t, audit.calls)
	require.False(t, r.reached)
}

func TestAuthorizeWithoutScopeIsAServerErrorAndNeverAsksTheEngine(t *testing.T) {
	ident := tenantIdentity()
	authz := &fakeAuthorizer{decision: domain.Decision{Allowed: true, Level: domain.LevelPlatform}}
	r := newAuthzRig(t, authz, &fakeAudit{})

	w := r.do(&ident, nil)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.Equal(t, "internal_error", errCode(t, w))
	require.Empty(t, authz.checks)
	require.False(t, r.reached, "salah urutan middleware tidak boleh membuka rute")
}

func TestAuthorizeEngineErrorNeverBecomesAllow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"infrastructure", errors.New("dial tcp 10.0.0.5:5432: connection refused"), http.StatusServiceUnavailable, "service_unavailable"},
		{"invalid check", fmt.Errorf("%w: aksi", application.ErrInvalidCheck), http.StatusInternalServerError, "internal_error"},
		{"invalid scope", fmt.Errorf("wrap: %w", domain.ErrInvalidScope), http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ident := tenantIdentity()
			scope := scopeOf(t, ident)
			// Decision ikut terisi ALLOW bersama galat: galat harus menang.
			authz := &fakeAuthorizer{
				decision: domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelPlatform},
				err:      tc.err,
			}
			audit := &fakeAudit{}
			r := newAuthzRig(t, authz, audit)

			w := r.do(&ident, &scope)

			require.Equal(t, tc.wantStatus, w.Code)
			require.Equal(t, tc.wantCode, errCode(t, w))
			require.False(t, r.reached)
			require.Empty(t, audit.calls, "galat engine bukan penolakan: tidak direkam sebagai denied")
			require.NotContains(t, w.Body.String(), "10.0.0.5", "detail infrastruktur tidak boleh bocor ke klien")
			require.NotContains(t, w.Body.String(), "connection refused")
		})
	}
}

func TestAuthorizeUsesTheActionOfItsRoute(t *testing.T) {
	ident := tenantIdentity()
	scope := scopeOf(t, ident)
	authz := &fakeAuthorizer{decision: domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw, err := authzhttp.New(authzhttp.Deps{Scope: &fakeResolver{}, Authorizer: authz, Responder: httpx.NewResponder(log), Log: log})
	require.NoError(t, err)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	for _, action := range []string{"user.read", "role.assign"} {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req = req.WithContext(domain.WithScope(authdomain.WithIdentity(req.Context(), ident), scope))
		mw.Authorize(action)(next).ServeHTTP(httptest.NewRecorder(), req)
	}

	require.Len(t, authz.checks, 2)
	require.Equal(t, "user.read", authz.checks[0].Action)
	require.Equal(t, "role.assign", authz.checks[1].Action)
}

func TestAuthorizePanicsAtWiringWhenMisconfigured(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)

	withoutAuthorizer, err := authzhttp.New(authzhttp.Deps{Scope: &fakeResolver{}, Responder: rs, Log: log})
	require.NoError(t, err)
	require.Panics(t, func() { withoutAuthorizer.Authorize(routeAction) })

	mw, err := authzhttp.New(authzhttp.Deps{Scope: &fakeResolver{}, Authorizer: &fakeAuthorizer{}, Responder: rs, Log: log})
	require.NoError(t, err)
	require.NotPanics(t, func() { mw.Authorize(routeAction) })
	for _, bad := range []string{"", "organization", "Organization.read", "organization.", ".read", "organization.read "} {
		require.Panics(t, func() { mw.Authorize(bad) }, "aksi %q", bad)
	}
}
