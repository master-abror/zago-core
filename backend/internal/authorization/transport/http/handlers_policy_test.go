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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/internal/policy"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes handler policies: Authorize SUNGGUHAN dengan Authorizer palsu supaya aksi per rute terbukti
// (pola handlers_role_test.go dan handlers_assignment_test.go).

var _ authzhttp.PolicyReads = (*repository.PolicyReader)(nil)

var _ authzhttp.PolicyManagement = (*application.PolicyWriter)(nil)

type phReads struct {
	orgs []*uuid.UUID

	queries []domain.PolicyListQuery

	page domain.PolicyPage

	err error
}

func (f *phReads) List(_ context.Context, org *uuid.UUID, q domain.PolicyListQuery) (domain.PolicyPage, error) {
	f.orgs = append(f.orgs, org)
	f.queries = append(f.queries, q)
	return f.page, f.err
}

type phWrites struct {
	calls []string

	grantors []application.Grantor

	creates []application.CreatePolicyInput

	updates []application.UpdatePolicyInput

	ids []uuid.UUID

	result domain.PolicyRecord

	err error
}

func (f *phWrites) Create(_ context.Context, g application.Grantor, in application.CreatePolicyInput) (domain.PolicyRecord, error) {
	f.calls = append(f.calls, "create")
	f.grantors = append(f.grantors, g)
	f.creates = append(f.creates, in)
	return f.result, f.err
}

func (f *phWrites) Update(_ context.Context, g application.Grantor, policyID uuid.UUID, in application.UpdatePolicyInput) (domain.PolicyRecord, error) {
	f.calls = append(f.calls, "update")
	f.grantors = append(f.grantors, g)
	f.ids = append(f.ids, policyID)
	f.updates = append(f.updates, in)
	return f.result, f.err
}

type phRig struct {
	router http.Handler

	authz *fakeAuthorizer

	reads *phReads

	write *phWrites

	ident authdomain.Identity

	scope domain.ScopeContext

	anonymous bool
}

func newPolicyRig(t *testing.T, ident authdomain.Identity) *phRig {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	scope, err := domain.NewScope(ident.OrganizationID, nil)
	require.NoError(t, err)
	rg := &phRig{
		authz: &fakeAuthorizer{decision: domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}},
		reads: &phReads{},
		write: &phWrites{},
		ident: ident,
		scope: scope,
	}
	mw, err := authzhttp.New(authzhttp.Deps{Scope: &fakeResolver{}, Authorizer: rg.authz, Audit: &fakeAudit{}, Responder: rs, Log: log})
	require.NoError(t, err)
	h, err := authzhttp.NewPolicyHandlers(rs, rg.reads, rg.write)
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

func (g *phRig) do(method, path, body string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	g.router.ServeHTTP(w, req)
	return w
}

func (g *phRig) untouched() bool {
	return len(g.write.calls) == 0 && len(g.reads.queries) == 0
}

const phCreateBody = `{"code":"no_big","name":"Tanpa besar","effect":"deny","expression":"resource.amount > 1","actions":["invoice.update"]}`

type phRoute struct {
	method string

	path string

	body string

	action string

	status int
}

func phRoutes() []phRoute {
	pid := id.NewID().String()
	return []phRoute{
		{http.MethodGet, "/policies", "", permission.PolicyRead, http.StatusOK},
		{http.MethodPost, "/policies", phCreateBody, permission.PolicyManage, http.StatusCreated},
		{http.MethodPatch, "/policies/" + pid, `{"status":"disabled"}`, permission.PolicyManage, http.StatusOK},
	}
}

func TestPolicyRoutesAskTheDocumentedActionForEachRoute(t *testing.T) {
	for _, c := range phRoutes() {
		rg := newPolicyRig(t, tenantIdentity())

		w := rg.do(c.method, c.path, c.body)

		label := c.method + " " + c.path
		require.Equal(t, c.status, w.Code, label+": "+w.Body.String())
		require.Len(t, rg.authz.checks, 1, label)
		require.Equal(t, c.action, rg.authz.checks[0].Action, label)
	}
}

func TestEveryPolicyRouteIsDeniedBeforeAnyReadOrWrite(t *testing.T) {
	for _, c := range phRoutes() {
		rg := newPolicyRig(t, tenantIdentity())
		rg.authz.decision = domain.Decision{Allowed: false, Reason: domain.ReasonNoPermission}

		w := rg.do(c.method, c.path, c.body)

		label := c.method + " " + c.path
		require.Equal(t, http.StatusForbidden, w.Code, label)
		require.Equal(t, httpx.PermissionDenied.Name, rhErrCode(t, w), label)
		require.True(t, rg.untouched(), label)
	}
}

func TestPolicyRoutesRejectAnonymousCallers(t *testing.T) {
	for _, c := range phRoutes() {
		rg := newPolicyRig(t, tenantIdentity())
		rg.anonymous = true

		w := rg.do(c.method, c.path, c.body)

		label := c.method + " " + c.path
		require.Equal(t, http.StatusUnauthorized, w.Code, label)
		require.Equal(t, httpx.AuthenticationRequired.Name, rhErrCode(t, w), label)
		require.True(t, rg.untouched(), label)
	}
}

func TestPolicyAuthorizeRunsBeforeBodyDecodingAndIDParsing(t *testing.T) {
	rg := newPolicyRig(t, tenantIdentity())
	rg.authz.decision = domain.Decision{Allowed: false, Reason: domain.ReasonNoPermission}

	w := rg.do(http.MethodPost, "/policies", "bukan json")
	require.Equal(t, http.StatusForbidden, w.Code)

	w = rg.do(http.MethodPatch, "/policies/bukan-uuid", `{"status":"disabled"}`)
	require.Equal(t, http.StatusForbidden, w.Code, "403 mendahului 404 id cacat")
	require.Empty(t, rg.write.calls)
}

func TestListPoliciesUsesOnlyTheCallerOrganizationFromScope(t *testing.T) {
	orgA, orgB := tenantIdentity(), tenantIdentity()

	a := newPolicyRig(t, orgA)
	w := a.do(http.MethodGet, "/policies?organization_id="+orgB.OrganizationID.String(), "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotNil(t, a.reads.orgs[0])
	require.Equal(t, *orgA.OrganizationID, *a.reads.orgs[0], "organisasi dari query tidak boleh dipakai")

	b := newPolicyRig(t, orgB)
	w = b.do(http.MethodGet, "/policies?organization_id="+orgA.OrganizationID.String(), "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, *orgB.OrganizationID, *b.reads.orgs[0])

	p := newPolicyRig(t, platformIdentity())
	w = p.do(http.MethodGet, "/policies", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Nil(t, p.reads.orgs[0], "scope platform hanya melihat policy global")
}

func TestListPoliciesDefaultsPagingAndFilter(t *testing.T) {
	rg := newPolicyRig(t, tenantIdentity())

	w := rg.do(http.MethodGet, "/policies", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	q := rg.reads.queries[0]
	require.Equal(t, domain.PolicyFilterAll, q.Status)
	require.Equal(t, domain.Window{Offset: 0, Limit: domain.DefaultPageSize}, q.Window)

	w = rg.do(http.MethodGet, "/policies?status=disabled&page=3&per_page=500", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	q = rg.reads.queries[1]
	require.Equal(t, domain.PolicyDisabled, q.Status)
	require.Equal(t, domain.Window{Offset: 2 * domain.MaxPageSize, Limit: domain.MaxPageSize}, q.Window, "per_page dipangkas ke maksimum")
	p := ahPagination(t, w)
	require.EqualValues(t, 3, p["page"])
	require.EqualValues(t, domain.MaxPageSize, p["per_page"])
}

func TestListPoliciesReportsEveryBadParameterAtOnce(t *testing.T) {
	rg := newPolicyRig(t, tenantIdentity())

	w := rg.do(http.MethodGet, "/policies?status=bogus&page=0&per_page=x", "")

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	fields := ahFields(t, w)
	require.Len(t, fields, 3, w.Body.String())
	for _, name := range []string{"status", "page", "per_page"} {
		require.Equal(t, "invalid", fields[name], name)
	}
	require.Empty(t, rg.reads.queries)
}

func TestListPoliciesMarksGlobalPoliciesAndNeverShowsOrganizationID(t *testing.T) {
	rg := newPolicyRig(t, tenantIdentity())
	org := *rg.ident.OrganizationID
	now := time.Now()
	global := domain.PolicyRecord{ID: id.NewID(), Code: "g", Name: "G", Effect: "deny", Expression: "e", Status: "active", CreatedAt: now, UpdatedAt: now}
	own := domain.PolicyRecord{ID: id.NewID(), OrganizationID: &org, Code: "o", Name: "O", Effect: "allow", Expression: "e", Actions: []string{"invoice.update"}, Status: "disabled", CreatedAt: now, UpdatedAt: now}
	rg.reads.page = domain.PolicyPage{Items: []domain.PolicyRecord{global, own}, Total: 2}

	w := rg.do(http.MethodGet, "/policies", "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	data, ok := rhBody(t, w)["data"].([]any)
	require.True(t, ok, w.Body.String())
	require.Len(t, data, 2)
	first, _ := data[0].(map[string]any)
	second, _ := data[1].(map[string]any)
	require.Equal(t, true, first["global"])
	require.Equal(t, false, second["global"])
	require.Equal(t, []any{}, first["actions"], "actions tidak pernah null")
	require.NotContains(t, w.Body.String(), "organization_id")
	require.EqualValues(t, 2, ahPagination(t, w)["total"])
}

func TestListPoliciesEmptyResultIsAnEmptyArray(t *testing.T) {
	rg := newPolicyRig(t, tenantIdentity())

	w := rg.do(http.MethodGet, "/policies", "")

	data, ok := rhBody(t, w)["data"].([]any)
	require.True(t, ok, "data harus larik, bukan null: "+w.Body.String())
	require.Empty(t, data)
}

func TestListPoliciesReaderFailureIsServiceUnavailableWithoutLeaking(t *testing.T) {
	rg := newPolicyRig(t, tenantIdentity())
	rg.reads.err = errors.New("pg down: relation policies")

	w := rg.do(http.MethodGet, "/policies", "")

	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "pg down")
}

func TestCreatePolicyDerivesGrantorFromIdentityAndScopeAndReturnsIt(t *testing.T) {
	rg := newPolicyRig(t, tenantIdentity())
	org := *rg.ident.OrganizationID
	now := time.Now()
	rg.write.result = domain.PolicyRecord{ID: id.NewID(), OrganizationID: &org, Code: "no_big", Name: "Tanpa besar", Effect: "deny", Expression: "resource.amount > 1", Actions: []string{"invoice.update"}, Status: "active", CreatedAt: now, UpdatedAt: now}

	w := rg.do(http.MethodPost, "/policies", phCreateBody)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, []string{"create"}, rg.write.calls)
	require.Equal(t, rg.ident.UserID, rg.write.grantors[0].UserID)
	require.Equal(t, rg.scope, rg.write.grantors[0].Scope, "cakupan berasal dari ScopeContext, bukan dari request")
	in := rg.write.creates[0]
	require.Equal(t, "no_big", in.Code)
	require.Equal(t, policy.Effect("deny"), in.Effect)
	require.Equal(t, []string{"invoice.update"}, in.Actions)
	require.Equal(t, "", in.Status)

	data := rhData(t, w)
	require.Equal(t, rg.write.result.ID.String(), data["id"])
	require.Equal(t, false, data["global"])
	require.NotContains(t, w.Body.String(), "organization_id")
}

func TestCreatePolicyAtPlatformScopeReturnsAGlobalPolicy(t *testing.T) {
	rg := newPolicyRig(t, platformIdentity())
	rg.write.result = domain.PolicyRecord{ID: id.NewID(), Code: "no_big", Name: "N", Effect: "deny", Expression: "e", Actions: []string{"invoice.update"}, Status: "active"}

	w := rg.do(http.MethodPost, "/policies", phCreateBody)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Nil(t, rg.write.grantors[0].Scope.OrganizationID)
	require.Equal(t, true, rhData(t, w)["global"])
}

func TestCreatePolicyRejectsOrganizationFromTheBodyAndReportsEveryField(t *testing.T) {
	rg := newPolicyRig(t, tenantIdentity())

	body := fmt.Sprintf(`{"code":"x","name":"N","effect":"deny","expression":"e","actions":["a.b"],"organization_id":%q}`, id.NewID().String())
	w := rg.do(http.MethodPost, "/policies", body)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "unknown_field", ahFields(t, w)["organization_id"])

	w = rg.do(http.MethodPost, "/policies", `{"status":"bogus"}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	fields := ahFields(t, w)
	require.Len(t, fields, 6, w.Body.String())
	require.Equal(t, "invalid", fields["status"])
	require.Equal(t, "required", fields["actions"])
	require.Empty(t, rg.write.calls)
}

type phErrCase struct {
	err error

	status int

	code string
}

func TestCreatePolicyMapsEveryWriterErrorWithoutLeaking(t *testing.T) {
	cases := []phErrCase{
		{domain.ErrPolicyCodeTaken, http.StatusConflict, authzhttp.PolicyCodeTaken.Name},
		{policy.ErrTooManyPolicies, http.StatusConflict, authzhttp.PolicyLimitReached.Name},
		{domain.ErrProtectedPolicyAction, http.StatusBadRequest, httpx.ValidationFailed.Name},
		{policy.ErrSyntax, http.StatusBadRequest, httpx.ValidationFailed.Name},
		{policy.ErrUnknownAction, http.StatusBadRequest, httpx.ValidationFailed.Name},
		{policy.ErrInvalidActions, http.StatusBadRequest, httpx.ValidationFailed.Name},
		{domain.ErrTargetOutsideScope, http.StatusForbidden, authzhttp.ScopeMismatch.Name},
		{domain.ErrPolicyNotFound, http.StatusNotFound, httpx.ResourceNotFound.Name},
		{fmt.Errorf("lapisan: %w", domain.ErrPolicyCodeTaken), http.StatusConflict, authzhttp.PolicyCodeTaken.Name},
		{errors.New("pg down: policies"), http.StatusServiceUnavailable, httpx.ServiceUnavailable.Name},
	}
	for _, c := range cases {
		rg := newPolicyRig(t, tenantIdentity())
		rg.write.err = c.err

		w := rg.do(http.MethodPost, "/policies", phCreateBody)

		require.Equal(t, c.status, w.Code, c.err.Error()+": "+w.Body.String())
		require.Equal(t, c.code, rhErrCode(t, w), c.err.Error())
		require.NotContains(t, w.Body.String(), "pg down")
		require.NotContains(t, w.Body.String(), "lapisan")
	}
}

func TestUpdatePolicyPassesTheIDAndOnlyTheFieldsSent(t *testing.T) {
	rg := newPolicyRig(t, tenantIdentity())
	pid := id.NewID()
	org := *rg.ident.OrganizationID
	rg.write.result = domain.PolicyRecord{ID: pid, OrganizationID: &org, Code: "c", Name: "N", Effect: "deny", Expression: "e", Actions: []string{"a.b"}, Status: "disabled"}

	w := rg.do(http.MethodPatch, "/policies/"+pid.String(), `{"status":"disabled","name":null}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []uuid.UUID{pid}, rg.write.ids)
	require.Equal(t, rg.ident.UserID, rg.write.grantors[0].UserID)
	require.Equal(t, rg.scope, rg.write.grantors[0].Scope)
	in := rg.write.updates[0]
	require.NotNil(t, in.Status)
	require.Equal(t, "disabled", *in.Status)
	require.Nil(t, in.Name, "null sama dengan tidak dikirim")
	require.Nil(t, in.Description)
	require.Nil(t, in.Effect)
	require.Nil(t, in.Expression)
	require.Nil(t, in.Actions)
	require.Equal(t, "disabled", rhData(t, w)["status"])
}

func TestUpdatePolicyCarriesEffectAndTheFullActionSet(t *testing.T) {
	rg := newPolicyRig(t, tenantIdentity())

	w := rg.do(http.MethodPatch, "/policies/"+id.NewID().String(), `{"effect":"allow","actions":["invoice.update","invoice.delete"]}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	in := rg.write.updates[0]
	require.NotNil(t, in.Effect)
	require.Equal(t, policy.Effect("allow"), *in.Effect)
	require.Equal(t, []string{"invoice.update", "invoice.delete"}, in.Actions)

	w = rg.do(http.MethodPatch, "/policies/"+id.NewID().String(), `{"actions":[]}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	in = rg.write.updates[1]
	require.NotNil(t, in.Actions, "irisan kosong diteruskan supaya penulis yang menolaknya")
	require.Empty(t, in.Actions)
}

func TestUpdatePolicyRejectsEmptyPatchBadStatusAndImmutableFields(t *testing.T) {
	rg := newPolicyRig(t, tenantIdentity())
	path := "/policies/" + id.NewID().String()

	w := rg.do(http.MethodPatch, path, `{}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "empty_patch", ahFields(t, w)[""])

	w = rg.do(http.MethodPatch, path, `{"status":"bogus"}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "invalid", ahFields(t, w)["status"])

	for _, key := range []string{"code", "organization_id", "global", "id"} {
		w = rg.do(http.MethodPatch, path, fmt.Sprintf(`{%q:"x"}`, key))
		require.Equal(t, http.StatusBadRequest, w.Code, key)
		require.Equal(t, "unknown_field", ahFields(t, w)[key], key)
	}
	require.Empty(t, rg.write.calls)
}

func TestUpdatePolicyMalformedIDIsNotFoundWithoutTouchingTheWriter(t *testing.T) {
	for _, raw := range []string{"bukan-uuid", uuid.Nil.String(), "123"} {
		rg := newPolicyRig(t, tenantIdentity())

		w := rg.do(http.MethodPatch, "/policies/"+raw, `{"status":"disabled"}`)

		require.Equal(t, http.StatusNotFound, w.Code, raw)
		require.Equal(t, httpx.ResourceNotFound.Name, rhErrCode(t, w), raw)
		require.Empty(t, rg.write.calls, raw)
	}
}

func TestUpdatePolicyMapsEveryWriterErrorWithoutLeaking(t *testing.T) {
	cases := []phErrCase{
		{domain.ErrPolicyNotFound, http.StatusNotFound, httpx.ResourceNotFound.Name},
		{policy.ErrTooManyPolicies, http.StatusConflict, authzhttp.PolicyLimitReached.Name},
		{domain.ErrInvalidPolicyStatus, http.StatusBadRequest, httpx.ValidationFailed.Name},
		{domain.ErrEmptyPolicyPatch, http.StatusBadRequest, httpx.ValidationFailed.Name},
		{domain.ErrProtectedPolicyAction, http.StatusBadRequest, httpx.ValidationFailed.Name},
		{domain.ErrTargetOutsideScope, http.StatusForbidden, authzhttp.ScopeMismatch.Name},
		{errors.New("pg down: policies"), http.StatusServiceUnavailable, httpx.ServiceUnavailable.Name},
	}
	for _, c := range cases {
		rg := newPolicyRig(t, tenantIdentity())
		rg.write.err = c.err

		w := rg.do(http.MethodPatch, "/policies/"+id.NewID().String(), `{"status":"disabled"}`)

		require.Equal(t, c.status, w.Code, c.err.Error()+": "+w.Body.String())
		require.Equal(t, c.code, rhErrCode(t, w), c.err.Error())
		require.NotContains(t, w.Body.String(), "pg down")
	}
}

func TestPolicyHandlersRefuseMissingDependenciesAndGuards(t *testing.T) {
	rs := httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reads, write := &phReads{}, &phWrites{}

	_, err := authzhttp.NewPolicyHandlers(nil, reads, write)
	require.Error(t, err)
	_, err = authzhttp.NewPolicyHandlers(rs, nil, write)
	require.Error(t, err)
	_, err = authzhttp.NewPolicyHandlers(rs, reads, nil)
	require.Error(t, err)

	h, err := authzhttp.NewPolicyHandlers(rs, reads, write)
	require.NoError(t, err)
	pass := func(next http.Handler) http.Handler { return next }
	require.Panics(t, func() { h.Mount(chi.NewRouter(), nil) })
	require.NotPanics(t, func() { h.Mount(chi.NewRouter(), func(string) func(http.Handler) http.Handler { return pass }) })
}
