package orghttp_test

import (
	"context"
	"encoding/json"
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
	authzdomain "github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/organization/application"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	orghttp "github.com/master-abror/zago-core/backend/internal/organization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes handler organisasi dan anggota. Middleware Authorize yang SUNGGUHAN dipakai (dengan
// Authorizer palsu yang mencatat aksi) supaya aksi per rute terbukti; Authenticate dan Scope
// diganti middleware kecil yang menaruh Identity dan ScopeContext di context. Layanan diganti
// fake yang mencatat panggilan, sehingga "tidak sampai ke layanan" dapat ditegaskan.

type fakeOrgs struct {
	calls []string

	caller application.Caller

	gotID uuid.UUID

	gotList domain.ListQuery

	gotCreate domain.CreateInput

	gotUpdate domain.UpdateInput

	rec domain.Record

	page domain.Page

	err error
}

func (f *fakeOrgs) Get(_ context.Context, c application.Caller, organizationID uuid.UUID) (domain.Record, error) {
	f.calls = append(f.calls, "get")
	f.caller = c
	f.gotID = organizationID
	return f.rec, f.err
}

func (f *fakeOrgs) List(_ context.Context, c application.Caller, q domain.ListQuery) (domain.Page, error) {
	f.calls = append(f.calls, "list")
	f.caller = c
	f.gotList = q
	return f.page, f.err
}

func (f *fakeOrgs) Create(_ context.Context, c application.Caller, in domain.CreateInput) (domain.Record, error) {
	f.calls = append(f.calls, "create")
	f.caller = c
	f.gotCreate = in
	return f.rec, f.err
}

func (f *fakeOrgs) Update(_ context.Context, c application.Caller, organizationID uuid.UUID, in domain.UpdateInput) (domain.Record, error) {
	f.calls = append(f.calls, "update")
	f.caller = c
	f.gotID = organizationID
	f.gotUpdate = in
	return f.rec, f.err
}

func (f *fakeOrgs) Archive(_ context.Context, c application.Caller, organizationID uuid.UUID) (domain.Record, error) {
	f.calls = append(f.calls, "archive")
	f.caller = c
	f.gotID = organizationID
	return f.rec, f.err
}

type fakeMembers struct {
	calls []string

	caller application.Caller

	gotOrg uuid.UUID

	gotUser uuid.UUID

	gotList domain.MemberListQuery

	member domain.Member

	page domain.MemberPage

	err error
}

func (f *fakeMembers) Get(_ context.Context, c application.Caller, organizationID, userID uuid.UUID) (domain.Member, error) {
	f.calls = append(f.calls, "get")
	f.caller = c
	f.gotOrg = organizationID
	f.gotUser = userID
	return f.member, f.err
}

func (f *fakeMembers) List(_ context.Context, c application.Caller, organizationID uuid.UUID, q domain.MemberListQuery) (domain.MemberPage, error) {
	f.calls = append(f.calls, "list")
	f.caller = c
	f.gotOrg = organizationID
	f.gotList = q
	return f.page, f.err
}

func (f *fakeMembers) Add(_ context.Context, c application.Caller, organizationID, userID uuid.UUID) (domain.Member, error) {
	f.calls = append(f.calls, "add")
	f.caller = c
	f.gotOrg = organizationID
	f.gotUser = userID
	return f.member, f.err
}

func (f *fakeMembers) Remove(_ context.Context, c application.Caller, organizationID, userID uuid.UUID) (domain.Member, error) {
	f.calls = append(f.calls, "remove")
	f.caller = c
	f.gotOrg = organizationID
	f.gotUser = userID
	return f.member, f.err
}

type fakeAuth struct {
	decision authzdomain.Decision

	checks []authzdomain.Check
}

func (f *fakeAuth) Can(_ context.Context, c authzdomain.Check) (authzdomain.Decision, error) {
	f.checks = append(f.checks, c)
	return f.decision, nil
}

type fakeScopes struct{}

func (fakeScopes) Resolve(_ context.Context, _ authdomain.Identity, _ []string) (authzdomain.ScopeContext, error) {
	return authzdomain.ScopeContext{}, nil
}

type hrig struct {
	router http.Handler

	auth *fakeAuth

	orgs *fakeOrgs

	members *fakeMembers

	ident authdomain.Identity

	org uuid.UUID

	anonymous bool
}

// newHRig membangun router dengan rute sungguhan. platform=true menaruh ScopeContext platform;
// selain itu scope tenant organisasi g.org.
func newHRig(t *testing.T, platform bool) *hrig {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)

	g := &hrig{auth: &fakeAuth{}, orgs: &fakeOrgs{}, members: &fakeMembers{}}
	g.auth.decision = authzdomain.Decision{Allowed: true, Reason: authzdomain.ReasonGranted, Level: authzdomain.LevelOrganization}
	g.ident = authdomain.Identity{UserID: id.NewID()}
	g.org = id.NewID()

	var scope authzdomain.ScopeContext
	if !platform {
		s, err := authzdomain.NewScope(&g.org, nil)
		require.NoError(t, err)
		scope = s
		g.ident.OrganizationID = &g.org
	}

	mw, err := authzhttp.New(authzhttp.Deps{Scope: fakeScopes{}, Authorizer: g.auth, Responder: rs, Log: log})
	require.NoError(t, err)
	h, err := orghttp.NewHandlers(rs, g.orgs, g.members)
	require.NoError(t, err)

	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if !g.anonymous {
				ctx = authdomain.WithIdentity(ctx, g.ident)
				ctx = authzdomain.WithScope(ctx, scope)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	h.Mount(router, mw.Authorize)
	g.router = router
	return g
}

func (g *hrig) do(method, path, body string) *httptest.ResponseRecorder {
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

// untouched melaporkan bahwa tidak ada layanan yang dipanggil.
func (g *hrig) untouched() bool {
	return len(g.orgs.calls) == 0 && len(g.members.calls) == 0
}

func bodyMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m), w.Body.String())
	return m
}

func dataMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	d, ok := bodyMap(t, w)["data"].(map[string]any)
	require.True(t, ok, "data harus objek: %s", w.Body.String())
	return d
}

func paginationOf(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	meta, ok := bodyMap(t, w)["meta"].(map[string]any)
	require.True(t, ok, "meta harus ada: %s", w.Body.String())
	p, ok := meta["pagination"].(map[string]any)
	require.True(t, ok, "meta.pagination harus ada: %s", w.Body.String())
	return p
}

type routeCase struct {
	method string

	path string

	body string

	action string

	status int
}

func routeCases(org, uid uuid.UUID) []routeCase {
	o := org.String()
	u := uid.String()
	return []routeCase{
		{http.MethodGet, "/organizations", "", permission.OrganizationRead, http.StatusOK},
		{http.MethodPost, "/organizations", `{"slug":"acme","name":"Acme"}`, permission.OrganizationCreate, http.StatusCreated},
		{http.MethodGet, "/organizations/" + o, "", permission.OrganizationRead, http.StatusOK},
		{http.MethodPatch, "/organizations/" + o, `{"name":"Baru"}`, permission.OrganizationUpdate, http.StatusOK},
		{http.MethodDelete, "/organizations/" + o, "", permission.OrganizationArchive, http.StatusNoContent},
		{http.MethodGet, "/organizations/" + o + "/members", "", permission.OrganizationMemberManage, http.StatusOK},
		{http.MethodPost, "/organizations/" + o + "/members", fmt.Sprintf(`{"user_id":%q}`, u), permission.OrganizationMemberManage, http.StatusCreated},
		{http.MethodGet, "/organizations/" + o + "/members/" + u, "", permission.OrganizationMemberManage, http.StatusOK},
		{http.MethodDelete, "/organizations/" + o + "/members/" + u, "", permission.OrganizationMemberManage, http.StatusNoContent},
	}
}

func TestEveryRouteChecksItsDocumentedActionAndSucceeds(t *testing.T) {
	for _, c := range routeCases(id.NewID(), id.NewID()) {
		g := newHRig(t, true)
		w := g.do(c.method, c.path, c.body)

		require.Equal(t, c.status, w.Code, "%s %s: %s", c.method, c.path, w.Body.String())
		require.Len(t, g.auth.checks, 1, "%s %s", c.method, c.path)
		require.Equal(t, c.action, g.auth.checks[0].Action, "%s %s", c.method, c.path)
		require.False(t, g.untouched(), "%s %s harus sampai ke layanan", c.method, c.path)
	}
}

func TestDeniedRoutesNeverReachTheServices(t *testing.T) {
	for _, c := range routeCases(id.NewID(), id.NewID()) {
		g := newHRig(t, false)
		g.auth.decision = authzdomain.Decision{Reason: authzdomain.ReasonNoPermission}
		w := g.do(c.method, c.path, c.body)

		require.Equal(t, http.StatusForbidden, w.Code, "%s %s", c.method, c.path)
		require.Equal(t, "permission_denied", decodeErrorBody(t, w).Error.Code, "%s %s", c.method, c.path)
		require.True(t, g.untouched(), "%s %s ditolak tetapi layanan dipanggil", c.method, c.path)
		require.Len(t, g.auth.checks, 1)
	}
}

func TestAnonymousRequestsAreRejectedBeforeAnyCheckOrService(t *testing.T) {
	for _, c := range routeCases(id.NewID(), id.NewID()) {
		g := newHRig(t, false)
		g.anonymous = true
		w := g.do(c.method, c.path, c.body)

		require.Equal(t, http.StatusUnauthorized, w.Code, "%s %s", c.method, c.path)
		require.True(t, g.untouched(), "%s %s", c.method, c.path)
		require.Empty(t, g.auth.checks, "%s %s", c.method, c.path)
	}
}

func TestTenantCallerComesFromIdentityAndScopeNeverFromThePath(t *testing.T) {
	g := newHRig(t, false)
	other := id.NewID()

	w := g.do(http.MethodGet, "/organizations/"+other.String(), "")
	require.Equal(t, http.StatusOK, w.Code)

	got, ok := g.orgs.caller.Scope.Tenant()
	require.True(t, ok)
	require.Equal(t, g.org, got, "Scope pemanggil adalah organisasinya, bukan {id} dari path")
	require.Equal(t, g.ident.UserID, g.orgs.caller.UserID)
	require.Equal(t, other, g.orgs.gotID, "{id} diteruskan apa adanya ke layanan, yang memutuskan 404")

	g.members.err = domain.ErrOrganizationNotFound
	w = g.do(http.MethodGet, "/organizations/"+other.String()+"/members", "")
	require.Equal(t, http.StatusNotFound, w.Code)
	gotOrg, ok := g.members.caller.Scope.Tenant()
	require.True(t, ok)
	require.Equal(t, g.org, gotOrg)
	require.Equal(t, other, g.members.gotOrg)
}

func TestPlatformCallerIsPassedAsPlatform(t *testing.T) {
	g := newHRig(t, true)

	w := g.do(http.MethodGet, "/organizations", "")

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, g.orgs.caller.Scope.IsPlatform())
	require.Equal(t, g.ident.UserID, g.orgs.caller.UserID)
}

func TestListOrganizationsPagingAndFilterReachTheService(t *testing.T) {
	g := newHRig(t, true)
	g.orgs.page = domain.Page{Items: []domain.Record{{ID: id.NewID()}}, Total: 123}

	w := g.do(http.MethodGet, "/organizations", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, domain.Window{Offset: 0, Limit: 50}, g.orgs.gotList.Window)
	require.Empty(t, g.orgs.gotList.Filter)
	p := paginationOf(t, w)
	require.EqualValues(t, 1, p["page"])
	require.EqualValues(t, 50, p["per_page"])
	require.EqualValues(t, 123, p["total"])

	w = g.do(http.MethodGet, "/organizations?page=3&per_page=20&status=archived", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, domain.Window{Offset: 40, Limit: 20}, g.orgs.gotList.Window)
	require.Equal(t, "archived", g.orgs.gotList.Filter)

	w = g.do(http.MethodGet, "/organizations?per_page=1000", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 100, g.orgs.gotList.Window.Limit, "per_page dipangkas ke maksimum")
	require.EqualValues(t, 100, paginationOf(t, w)["per_page"])
}

func TestListOrganizationsInvalidPagingNeverReachesTheService(t *testing.T) {
	for _, qs := range []string{"page=0", "page=abc", "page=10001", "per_page=0", "per_page=-1", "per_page=x"} {
		g := newHRig(t, true)
		w := g.do(http.MethodGet, "/organizations?"+qs, "")

		require.Equal(t, http.StatusBadRequest, w.Code, qs)
		require.Equal(t, "validation_failed", decodeErrorBody(t, w).Error.Code, qs)
		require.True(t, g.untouched(), qs)
	}
}

func TestListOrganizationsInvalidStatusFilterNamesTheStatusField(t *testing.T) {
	g := newHRig(t, true)
	g.orgs.err = domain.ErrInvalidFilter

	w := g.do(http.MethodGet, "/organizations?status=zzz", "")

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), `"field":"status"`)
}

func TestListOrganizationsEmptyListIsAnEmptyArray(t *testing.T) {
	g := newHRig(t, true)

	w := g.do(http.MethodGet, "/organizations", "")

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"data":[]`)
}

func TestCreateOrganizationMapsBodyAndResponse(t *testing.T) {
	g := newHRig(t, true)
	g.orgs.rec = domain.Record{ID: id.NewID(), Slug: "acme-id", Name: "Acme Indonesia", Status: domain.StatusActive}

	w := g.do(http.MethodPost, "/organizations", `{"slug":"acme-id","name":"Acme Indonesia","default_timezone":"Asia/Jakarta"}`)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, "acme-id", g.orgs.gotCreate.Slug)
	require.Equal(t, "Acme Indonesia", g.orgs.gotCreate.Name)
	require.Equal(t, "Asia/Jakarta", g.orgs.gotCreate.DefaultTimezone)
	d := dataMap(t, w)
	require.Equal(t, g.orgs.rec.ID.String(), d["id"])
	require.Equal(t, "active", d["status"])
	require.NotContains(t, d, "organization_id")
}

func TestCreateOrganizationRejectsBodiesThatCarryTenancyOrState(t *testing.T) {
	for _, body := range []string{
		`{"slug":"acme","name":"Acme","organization_id":"` + id.NewID().String() + `"}`,
		`{"slug":"acme","name":"Acme","status":"archived"}`,
		`{"slug":"acme","name":"Acme","id":"x"}`,
	} {
		g := newHRig(t, true)
		w := g.do(http.MethodPost, "/organizations", body)

		require.Equal(t, http.StatusBadRequest, w.Code, body)
		require.Contains(t, w.Body.String(), "unknown_field", body)
		require.True(t, g.untouched(), body)
	}
}

func TestCreateOrganizationWithoutABodyIsAClientErrorAndNeverReachesTheService(t *testing.T) {
	g := newHRig(t, true)

	w := g.do(http.MethodPost, "/organizations", "")

	require.GreaterOrEqual(t, w.Code, 400)
	require.Less(t, w.Code, 500)
	require.True(t, g.untouched())
}

func TestOrganizationServiceErrorsMapToTheirCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{domain.ErrPlatformRequired, http.StatusForbidden, "permission_denied"},
		{domain.ErrSlugTaken, http.StatusConflict, "organization_slug_taken"},
		{domain.ErrInvalidSlug, http.StatusBadRequest, "validation_failed"},
		{domain.ErrOrganizationNotFound, http.StatusNotFound, "organization_not_found"},
		{domain.ErrOrganizationArchived, http.StatusConflict, "organization_archived"},
		{domain.ErrInvalidTransition, http.StatusConflict, "invalid_status_transition"},
	}
	routes := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/organizations", `{"slug":"acme","name":"Acme"}`},
		{http.MethodPatch, "/organizations/" + id.NewID().String(), `{"name":"Baru"}`},
		{http.MethodDelete, "/organizations/" + id.NewID().String(), ""},
		{http.MethodGet, "/organizations/" + id.NewID().String(), ""},
	}
	for _, c := range cases {
		for _, rt := range routes {
			g := newHRig(t, true)
			g.orgs.err = fmt.Errorf("service: %w", c.err)

			w := g.do(rt.method, rt.path, rt.body)

			require.Equal(t, c.status, w.Code, "%s %s %v", rt.method, rt.path, c.err)
			require.Equal(t, c.code, decodeErrorBody(t, w).Error.Code, "%s %s %v", rt.method, rt.path, c.err)
		}
	}
}

func TestGetOrganizationMalformedAndMissingIDsAreIndistinguishable(t *testing.T) {
	g := newHRig(t, false)
	malformed := g.do(http.MethodGet, "/organizations/bukan-uuid", "")
	nilID := g.do(http.MethodGet, "/organizations/"+uuid.Nil.String(), "")
	require.True(t, g.untouched(), "ID cacat tidak boleh sampai ke layanan")

	g.orgs.err = domain.ErrOrganizationNotFound
	missing := g.do(http.MethodGet, "/organizations/"+id.NewID().String(), "")

	require.Equal(t, http.StatusNotFound, malformed.Code)
	require.Equal(t, malformed.Code, nilID.Code)
	require.Equal(t, malformed.Code, missing.Code)
	require.Equal(t, decodeErrorBody(t, malformed), decodeErrorBody(t, missing))
	require.Equal(t, decodeErrorBody(t, malformed), decodeErrorBody(t, nilID))
	require.Equal(t, "organization_not_found", decodeErrorBody(t, missing).Error.Code)
}

func TestUpdateOrganizationRejectsSlugStatusAndEmptyPatchesBeforeTheService(t *testing.T) {
	path := "/organizations/" + id.NewID().String()
	for _, body := range []string{`{"slug":"baru"}`, `{"status":"archived"}`, `{}`, `{"name":null}`} {
		g := newHRig(t, true)
		w := g.do(http.MethodPatch, path, body)

		require.Equal(t, http.StatusBadRequest, w.Code, body)
		require.Equal(t, "validation_failed", decodeErrorBody(t, w).Error.Code, body)
		require.True(t, g.untouched(), body)
	}
}

func TestUpdateOrganizationCarriesOnlyTheSentFields(t *testing.T) {
	g := newHRig(t, true)
	target := id.NewID()
	g.orgs.rec = domain.Record{ID: target, Name: "Baru"}

	w := g.do(http.MethodPatch, "/organizations/"+target.String(), `{"name":"Baru","legal_name":""}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, target, g.orgs.gotID)
	require.NotNil(t, g.orgs.gotUpdate.Name)
	require.Equal(t, "Baru", *g.orgs.gotUpdate.Name)
	require.NotNil(t, g.orgs.gotUpdate.LegalName)
	require.Empty(t, *g.orgs.gotUpdate.LegalName)
	require.Nil(t, g.orgs.gotUpdate.DefaultLocale)
	require.Nil(t, g.orgs.gotUpdate.DefaultTimezone)
	require.Equal(t, "Baru", dataMap(t, w)["name"])
}

func TestArchiveOrganizationAnswers204WithAnEmptyBody(t *testing.T) {
	g := newHRig(t, true)
	target := id.NewID()

	w := g.do(http.MethodDelete, "/organizations/"+target.String(), "")

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Empty(t, w.Body.String())
	require.Equal(t, target, g.orgs.gotID)
}

func TestListMembersPagingFilterAndCursorReachTheService(t *testing.T) {
	g := newHRig(t, true)
	target := id.NewID()
	path := "/organizations/" + target.String() + "/members"

	w := g.do(http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, target, g.members.gotOrg)
	require.Equal(t, 50, g.members.gotList.Limit)
	require.Empty(t, g.members.gotList.Cursor)
	require.Empty(t, g.members.gotList.Filter)

	w = g.do(http.MethodGet, path+"?limit=500&cursor=abc&status=active", "")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 100, g.members.gotList.Limit, "limit dipangkas ke maksimum")
	require.Equal(t, "abc", g.members.gotList.Cursor)
	require.Equal(t, "active", g.members.gotList.Filter)
}

func TestListMembersCursorMeta(t *testing.T) {
	g := newHRig(t, true)
	path := "/organizations/" + g.org.String() + "/members"

	w := g.do(http.MethodGet, path, "")
	p := paginationOf(t, w)
	require.Nil(t, p["next_cursor"], "halaman terakhir: next_cursor null")
	require.Equal(t, false, p["has_more"])
	require.Contains(t, w.Body.String(), `"data":[]`)

	g.members.page = domain.MemberPage{Items: []domain.Member{{ID: id.NewID(), UserID: id.NewID()}}, NextCursor: "next-opaque"}
	w = g.do(http.MethodGet, path, "")
	p = paginationOf(t, w)
	require.Equal(t, "next-opaque", p["next_cursor"])
	require.Equal(t, true, p["has_more"])
}

func TestListMembersInvalidLimitNeverReachesTheService(t *testing.T) {
	for _, qs := range []string{"limit=0", "limit=abc", "limit=-3"} {
		g := newHRig(t, true)
		w := g.do(http.MethodGet, "/organizations/"+g.org.String()+"/members?"+qs, "")

		require.Equal(t, http.StatusBadRequest, w.Code, qs)
		require.True(t, g.untouched(), qs)
	}
}

func TestListMembersInvalidCursorNamesTheCursorField(t *testing.T) {
	g := newHRig(t, true)
	g.members.err = domain.ErrInvalidCursor

	w := g.do(http.MethodGet, "/organizations/"+g.org.String()+"/members?cursor=rusak", "")

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), `"field":"cursor"`)
	require.Contains(t, w.Body.String(), "invalid_cursor")
}

func TestMemberResponsesCarryNeitherEmailNorOrganizationID(t *testing.T) {
	g := newHRig(t, true)
	var m domain.Member
	m.ID = id.NewID()
	m.OrganizationID = g.org
	m.UserID = id.NewID()
	m.DisplayName = "Budi"
	m.Status = domain.MemberActive
	g.members.page = domain.MemberPage{Items: []domain.Member{m}}
	g.members.member = m

	list := g.do(http.MethodGet, "/organizations/"+g.org.String()+"/members", "")
	one := g.do(http.MethodGet, "/organizations/"+g.org.String()+"/members/"+m.UserID.String(), "")

	for _, w := range []*httptest.ResponseRecorder{list, one} {
		require.Equal(t, http.StatusOK, w.Code)
		require.NotEmpty(t, w.Body.String())
		require.NotContains(t, w.Body.String(), "email")
		require.NotContains(t, w.Body.String(), "organization_id")
		require.Contains(t, w.Body.String(), "Budi")
	}
}

func TestAddMemberPassesOrganizationFromPathAndUserFromBody(t *testing.T) {
	g := newHRig(t, true)
	uid := id.NewID()
	g.members.member = domain.Member{ID: id.NewID(), UserID: uid, Status: domain.MemberActive}

	w := g.do(http.MethodPost, "/organizations/"+g.org.String()+"/members", fmt.Sprintf(`{"user_id":%q}`, uid.String()))

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, g.org, g.members.gotOrg)
	require.Equal(t, uid, g.members.gotUser)
	require.Equal(t, uid.String(), dataMap(t, w)["user_id"])
}

func TestAddMemberRejectsBadBodiesBeforeTheService(t *testing.T) {
	path := "/organizations/" + id.NewID().String() + "/members"
	for _, body := range []string{
		`{}`,
		`{"user_id":null}`,
		`{"user_id":"bukan-uuid"}`,
		fmt.Sprintf(`{"user_id":%q,"organization_id":%q}`, id.NewID().String(), id.NewID().String()),
		fmt.Sprintf(`{"user_id":%q,"status":"active"}`, id.NewID().String()),
	} {
		g := newHRig(t, true)
		w := g.do(http.MethodPost, path, body)

		require.Equal(t, http.StatusBadRequest, w.Code, body)
		require.True(t, g.untouched(), body)
	}
}

func TestMemberServiceErrorsMapToTheirCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{domain.ErrPlatformRequired, http.StatusForbidden, "permission_denied"},
		{domain.ErrMemberExists, http.StatusConflict, "member_exists"},
		{domain.ErrUserNotEligible, http.StatusUnprocessableEntity, "user_not_eligible"},
		{domain.ErrOrganizationNotFound, http.StatusNotFound, "organization_not_found"},
		{domain.ErrOrganizationArchived, http.StatusConflict, "organization_archived"},
		{domain.ErrMemberNotFound, http.StatusNotFound, "member_not_found"},
		{domain.ErrCannotRemoveSelf, http.StatusConflict, "cannot_remove_self"},
	}
	o := id.NewID().String()
	u := id.NewID().String()
	routes := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/organizations/" + o + "/members", fmt.Sprintf(`{"user_id":%q}`, u)},
		{http.MethodGet, "/organizations/" + o + "/members/" + u, ""},
		{http.MethodDelete, "/organizations/" + o + "/members/" + u, ""},
		{http.MethodGet, "/organizations/" + o + "/members", ""},
	}
	for _, c := range cases {
		for _, rt := range routes {
			g := newHRig(t, true)
			g.members.err = fmt.Errorf("service: %w", c.err)

			w := g.do(rt.method, rt.path, rt.body)

			require.Equal(t, c.status, w.Code, "%s %s %v", rt.method, rt.path, c.err)
			require.Equal(t, c.code, decodeErrorBody(t, w).Error.Code, "%s %s %v", rt.method, rt.path, c.err)
		}
	}
}

func TestMemberRoutesRejectMalformedIDsAsNotFoundBeforeTheService(t *testing.T) {
	o := id.NewID().String()
	u := id.NewID().String()

	g := newHRig(t, true)
	badUser := g.do(http.MethodGet, "/organizations/"+o+"/members/bukan-uuid", "")
	badUserDelete := g.do(http.MethodDelete, "/organizations/"+o+"/members/"+uuid.Nil.String(), "")
	badOrg := g.do(http.MethodGet, "/organizations/bukan-uuid/members/"+u, "")
	badOrgList := g.do(http.MethodGet, "/organizations/bukan-uuid/members", "")

	require.True(t, g.untouched())
	require.Equal(t, "member_not_found", decodeErrorBody(t, badUser).Error.Code)
	require.Equal(t, "member_not_found", decodeErrorBody(t, badUserDelete).Error.Code)
	require.Equal(t, "organization_not_found", decodeErrorBody(t, badOrg).Error.Code)
	require.Equal(t, "organization_not_found", decodeErrorBody(t, badOrgList).Error.Code)
}

func TestRemoveMemberAnswers204WithAnEmptyBody(t *testing.T) {
	g := newHRig(t, false)
	uid := id.NewID()

	w := g.do(http.MethodDelete, "/organizations/"+g.org.String()+"/members/"+uid.String(), "")

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Empty(t, w.Body.String())
	require.Equal(t, g.org, g.members.gotOrg)
	require.Equal(t, uid, g.members.gotUser)
}

func TestUnknownServiceErrorsFailClosedWith503AndLeakNothing(t *testing.T) {
	for _, c := range routeCases(id.NewID(), id.NewID()) {
		g := newHRig(t, true)
		g.orgs.err = errors.New("pq: connection refused")
		g.members.err = errors.New("pq: connection refused")

		w := g.do(c.method, c.path, c.body)

		require.Equal(t, http.StatusServiceUnavailable, w.Code, "%s %s", c.method, c.path)
		require.Equal(t, "service_unavailable", decodeErrorBody(t, w).Error.Code, "%s %s", c.method, c.path)
		require.NotContains(t, w.Body.String(), "pq:", "%s %s", c.method, c.path)
	}
}

func TestNewHandlersRequiresEveryDependency(t *testing.T) {
	rs := httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil)))

	h, err := orghttp.NewHandlers(nil, &fakeOrgs{}, &fakeMembers{})
	require.Error(t, err)
	require.Nil(t, h)

	h, err = orghttp.NewHandlers(rs, nil, &fakeMembers{})
	require.Error(t, err)
	require.Nil(t, h)

	h, err = orghttp.NewHandlers(rs, &fakeOrgs{}, nil)
	require.Error(t, err)
	require.Nil(t, h)

	h, err = orghttp.NewHandlers(rs, &fakeOrgs{}, &fakeMembers{})
	require.NoError(t, err)
	require.NotNil(t, h)
}

func TestMountPanicsWithoutAnAuthorizer(t *testing.T) {
	rs := httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil)))
	h, err := orghttp.NewHandlers(rs, &fakeOrgs{}, &fakeMembers{})
	require.NoError(t, err)

	require.Panics(t, func() { h.Mount(chi.NewRouter(), nil) })
}
