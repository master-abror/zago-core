package authzhttp_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes handler simulate: Authorize SUNGGUHAN dengan Authorizer palsu supaya aksi per rute terbukti
// (pola handlers_policy_test.go), ditambah satu rig dengan application.Simulator SUNGGUHAN untuk
// kasus yang bergantung pada aturannya (scope platform, target seragam).

var _ authzhttp.PermissionSimulation = (*application.Simulator)(nil)

type smhSim struct {
	grantors []application.Grantor
	inputs   []application.SimulateInput
	result   application.SimulationResult
	err      error
}

func (f *smhSim) Simulate(_ context.Context, g application.Grantor, in application.SimulateInput) (application.SimulationResult, error) {
	f.grantors = append(f.grantors, g)
	f.inputs = append(f.inputs, in)
	return f.result, f.err
}

type smhMembers struct {
	member bool
	calls  int
}

func (m *smhMembers) IsActiveMember(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	m.calls++
	return m.member, nil
}

type smhGroups struct{ calls int }

func (g *smhGroups) IsActiveGroupMember(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (bool, error) {
	g.calls++
	return true, nil
}

type smhRig struct {
	router    http.Handler
	authz     *fakeAuthorizer
	ident     authdomain.Identity
	scope     domain.ScopeContext
	anonymous bool
}

func newSimulateRig(t *testing.T, ident authdomain.Identity, sim authzhttp.PermissionSimulation) *smhRig {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	scope, err := domain.NewScope(ident.OrganizationID, nil)
	require.NoError(t, err)
	rg := &smhRig{ident: ident, scope: scope}
	rg.authz = &fakeAuthorizer{decision: domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}}
	mw, err := authzhttp.New(authzhttp.Deps{Scope: &fakeResolver{}, Authorizer: rg.authz, Audit: &fakeAudit{}, Responder: rs, Log: log})
	require.NoError(t, err)
	h, err := authzhttp.NewSimulateHandlers(rs, sim)
	require.NoError(t, err)

	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if !rg.anonymous {
				ctx = authdomain.WithIdentity(ctx, rg.ident)
				ctx = domain.WithScope(ctx, rg.scope)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	h.Mount(router, mw.Authorize)
	rg.router = router
	return rg
}

func (g *smhRig) post(body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/permissions/simulate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	g.router.ServeHTTP(w, req)
	return w
}

func smhBody() string {
	return fmt.Sprintf(`{"user_id":%q,"action":"role.read"}`, id.NewID().String())
}

func TestNewSimulateHandlersRequiresItsDependencies(t *testing.T) {
	rs := httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil)))

	_, err := authzhttp.NewSimulateHandlers(nil, &smhSim{})
	require.Error(t, err)
	_, err = authzhttp.NewSimulateHandlers(rs, nil)
	require.Error(t, err)
}

func TestSimulateMountPanicsWithoutAnAuthorizeFunction(t *testing.T) {
	rs := httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h, err := authzhttp.NewSimulateHandlers(rs, &smhSim{})
	require.NoError(t, err)

	require.Panics(t, func() { h.Mount(chi.NewRouter(), nil) })
}

func TestSimulateRouteAsksPermissionSimulateWithTheCallersScope(t *testing.T) {
	ident := tenantIdentity()
	sim := &smhSim{result: application.SimulationResult{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}}
	rg := newSimulateRig(t, ident, sim)

	w := rg.post(smhBody())

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, rg.authz.checks, 1)
	c := rg.authz.checks[0]
	require.Equal(t, permission.PermissionSimulate, c.Action)
	require.Equal(t, ident.UserID, c.Identity.UserID, "yang diperiksa adalah PEMANGGIL, bukan target")
	require.Equal(t, rg.scope, c.Scope)
	require.Nil(t, c.Resource)
	require.Len(t, sim.inputs, 1)
}

func TestSimulateRouteIsDeniedBeforeAnySimulation(t *testing.T) {
	sim := &smhSim{}
	rg := newSimulateRig(t, tenantIdentity(), sim)
	rg.authz.decision = domain.Decision{Allowed: false, Reason: domain.ReasonNoPermission}

	w := rg.post(smhBody())

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, httpx.PermissionDenied.Name, rhErrCode(t, w))
	require.Empty(t, sim.inputs)
}

func TestSimulateRouteRejectsAnonymousCallers(t *testing.T) {
	sim := &smhSim{}
	rg := newSimulateRig(t, tenantIdentity(), sim)
	rg.anonymous = true

	w := rg.post(smhBody())

	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Equal(t, httpx.AuthenticationRequired.Name, rhErrCode(t, w))
	require.Empty(t, sim.inputs)
}

func TestSimulateAuthorizeRunsBeforeBodyDecoding(t *testing.T) {
	sim := &smhSim{}
	rg := newSimulateRig(t, tenantIdentity(), sim)
	rg.authz.decision = domain.Decision{Allowed: false, Reason: domain.ReasonNoPermission}

	w := rg.post("bukan json")

	require.Equal(t, http.StatusForbidden, w.Code, "403 mendahului 400 body cacat")
	require.Empty(t, sim.inputs)
}

func TestSimulateDerivesTheGrantorFromIdentityAndScopeOnly(t *testing.T) {
	ident := tenantIdentity()
	sim := &smhSim{}
	rg := newSimulateRig(t, ident, sim)

	w := rg.post(smhBody())

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, sim.grantors, 1)
	require.Equal(t, ident.UserID, sim.grantors[0].UserID)
	require.Equal(t, rg.scope, sim.grantors[0].Scope)

	other := id.NewID().String()
	w = rg.post(fmt.Sprintf(`{"user_id":%q,"action":"role.read","organization_id":%q}`, id.NewID().String(), other))
	require.Equal(t, http.StatusBadRequest, w.Code, "organisasi dari body ditolak")
	require.Equal(t, httpx.ValidationFailed.Name, rhErrCode(t, w))
	require.Len(t, sim.inputs, 1, "simulator tidak dipanggil untuk body yang ditolak")
}

func TestSimulateValidationFailureReportsEveryFieldAndSkipsTheSimulator(t *testing.T) {
	sim := &smhSim{}
	rg := newSimulateRig(t, tenantIdentity(), sim)

	w := rg.post(`{}`)

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, httpx.ValidationFailed.Name, rhErrCode(t, w))
	require.Equal(t, map[string]string{"user_id": "required", "action": "required"}, ahFields(t, w))
	require.Empty(t, sim.inputs)
}

func TestSimulatePassesTheDecodedQuestionToTheSimulator(t *testing.T) {
	sim := &smhSim{}
	rg := newSimulateRig(t, tenantIdentity(), sim)
	userID, groupID, resID, ownerID := id.NewID(), id.NewID(), id.NewID(), id.NewID()
	body := fmt.Sprintf(`{"user_id":%q,"action":"invoice.approve","active_group_id":%q,"resource":{"type":"invoice","id":%q,"owner_id":%q,"attrs":{"amount":5}}}`,
		userID, groupID, resID, ownerID)

	w := rg.post(body)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, sim.inputs, 1)
	in := sim.inputs[0]
	require.Equal(t, userID, in.UserID)
	require.Equal(t, "invoice.approve", in.Action)
	require.NotNil(t, in.ActiveGroupID)
	require.Equal(t, groupID, *in.ActiveGroupID)
	require.NotNil(t, in.Resource)
	require.Equal(t, "invoice", in.Resource.Type)
	require.Equal(t, resID, in.Resource.ID)
	require.Equal(t, ownerID, *in.Resource.OwnerID)
	require.Nil(t, in.Resource.GroupID)
	require.Equal(t, map[string]any{"amount": float64(5)}, in.Resource.Attrs)
}

func TestSimulateResponseShapeForAllowAndDeny(t *testing.T) {
	sim := &smhSim{result: application.SimulationResult{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelGroup}}
	rg := newSimulateRig(t, tenantIdentity(), sim)

	w := rg.post(smhBody())
	data := rhData(t, w)
	require.Equal(t, "ALLOW", data["result"])
	require.Equal(t, "granted", data["reason"])
	require.Equal(t, "group", data["level"])
	require.Nil(t, data["policy_code"])
	require.Equal(t, []any{}, data["deferred_policies"])
	require.NotContains(t, w.Body.String(), "evaluated")

	sim.result = application.SimulationResult{Reason: domain.ReasonPolicyDenied, PolicyCode: "no_weekend"}
	data = rhData(t, rg.post(smhBody()))
	require.Equal(t, "DENY", data["result"])
	require.Equal(t, "policy_denied", data["reason"])
	require.Nil(t, data["level"])
	require.Equal(t, "no_weekend", data["policy_code"])
}

func TestSimulateMapsDomainErrorsToTheDocumentedResponses(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
		field  string
		fcode  string
	}{
		{domain.ErrSimulationTargetNotFound, http.StatusBadRequest, httpx.ValidationFailed.Name, "user_id", "not_found"},
		{domain.ErrInvalidSimulationAttrs, http.StatusBadRequest, httpx.ValidationFailed.Name, "resource.attrs", "invalid"},
		{domain.ErrReservedSimulationAttr, http.StatusBadRequest, httpx.ValidationFailed.Name, "resource.attrs", "reserved"},
		{domain.ErrTargetOutsideScope, http.StatusForbidden, authzhttp.ScopeMismatch.Name, "", ""},
		{fmt.Errorf("lapisan: %w", application.ErrInvalidCheck), http.StatusInternalServerError, httpx.InternalError.Name, "", ""},
		{errors.New("pg down: organization_memberships"), http.StatusServiceUnavailable, httpx.ServiceUnavailable.Name, "", ""},
	}
	for _, c := range cases {
		sim := &smhSim{err: c.err}
		rg := newSimulateRig(t, tenantIdentity(), sim)

		w := rg.post(smhBody())

		label := c.err.Error()
		require.Equal(t, c.status, w.Code, label+": "+w.Body.String())
		require.Equal(t, c.code, rhErrCode(t, w), label)
		if c.field != "" {
			require.Equal(t, map[string]string{c.field: c.fcode}, ahFields(t, w), label)
		}
		require.NotContains(t, w.Body.String(), "pg down", label)
	}
}

// newRealSimulator merakit Simulator sungguhan dengan penjawab palsu: decider palsu terpisah dari
// Authorizer milik middleware supaya dapat dihitung panggilannya.
func newRealSimulator(t *testing.T) (*application.Simulator, *smhMembers, *smhGroups, *fakeAuthorizer) {
	t.Helper()
	decider := &fakeAuthorizer{decision: domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}}
	members := &smhMembers{member: true}
	groups := &smhGroups{}
	sim, err := application.NewSimulator(decider, members, groups, &fakeAudit{})
	require.NoError(t, err)
	return sim, members, groups, decider
}

func TestSimulateRealSimulatorRejectsPlatformCallersWithScopeMismatch(t *testing.T) {
	sim, members, groups, decider := newRealSimulator(t)
	rg := newSimulateRig(t, platformIdentity(), sim)

	w := rg.post(smhBody())

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Equal(t, authzhttp.ScopeMismatch.Name, rhErrCode(t, w))
	require.Len(t, rg.authz.checks, 1, "kontrol: Authorize memang dilewati lebih dulu")
	require.Zero(t, members.calls)
	require.Zero(t, groups.calls)
	require.Empty(t, decider.checks, "engine tidak ditanya untuk pemanggil platform")
}

func TestSimulateRealSimulatorAnswersUniformlyForAnyNonMemberTarget(t *testing.T) {
	sim, members, _, decider := newRealSimulator(t)
	members.member = false
	rg := newSimulateRig(t, tenantIdentity(), sim)

	first := rg.post(smhBody())
	second := rg.post(smhBody())

	for _, w := range []*httptest.ResponseRecorder{first, second} {
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		require.Equal(t, map[string]string{"user_id": "not_found"}, ahFields(t, w))
	}
	require.Empty(t, decider.checks, "target tidak sah tidak pernah sampai ke engine")
}

func TestSimulateRealSimulatorAnswersForAMemberTargetThroughTheEngine(t *testing.T) {
	sim, _, _, decider := newRealSimulator(t)
	ident := tenantIdentity()
	rg := newSimulateRig(t, ident, sim)
	target := id.NewID()

	w := rg.post(fmt.Sprintf(`{"user_id":%q,"action":"role.read"}`, target.String()))

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "ALLOW", rhData(t, w)["result"])
	require.Len(t, decider.checks, 1)
	require.Equal(t, target, decider.checks[0].Identity.UserID)
	require.NotNil(t, decider.checks[0].Identity.OrganizationID)
	require.Equal(t, *ident.OrganizationID, *decider.checks[0].Identity.OrganizationID)
}
