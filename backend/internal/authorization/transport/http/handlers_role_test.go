package authzhttp_test

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes handler role dan katalog permission. Middleware Authorize yang SUNGGUHAN dipakai (dengan
// Authorizer palsu) supaya aksi yang dipasang per rute terbukti; Authenticate dan Scope diganti
// middleware kecil yang menaruh Identity dan ScopeContext di context.

// Kepatuhan antarmuka sisi pemakai terhadap tipe sungguhan (transport tidak mengimpor repository,
// jadi buktinya di sini).
var _ authzhttp.RoleReads = (*repository.RoleReader)(nil)

var _ authzhttp.PermissionReads = (*repository.PermissionReader)(nil)

var _ authzhttp.RoleManagement = (*application.RoleWriter)(nil)

type rhReads struct {
	listOrgs []*uuid.UUID

	listQueries []domain.RoleListQuery

	getOrgs []*uuid.UUID

	getIDs []uuid.UUID

	page domain.RolePage

	row domain.RoleRecord

	codes []string

	listErr error

	getErr error
}

func (f *rhReads) List(_ context.Context, org *uuid.UUID, q domain.RoleListQuery) (domain.RolePage, error) {
	f.listOrgs = append(f.listOrgs, org)
	f.listQueries = append(f.listQueries, q)
	return f.page, f.listErr
}

func (f *rhReads) Get(_ context.Context, org *uuid.UUID, roleID uuid.UUID) (domain.RoleRecord, []string, error) {
	f.getOrgs = append(f.getOrgs, org)
	f.getIDs = append(f.getIDs, roleID)
	return f.row, f.codes, f.getErr
}

type rhWrites struct {
	calls []string

	grantors []application.Grantor

	created []application.CreateRoleInput

	roleIDs []uuid.UUID

	names []string

	descriptions []string

	requested [][]string

	result application.RoleResult

	err error
}

func (f *rhWrites) CreateRole(_ context.Context, g application.Grantor, in application.CreateRoleInput) (application.RoleResult, error) {
	f.calls = append(f.calls, "create")
	f.grantors = append(f.grantors, g)
	f.created = append(f.created, in)
	return f.result, f.err
}

func (f *rhWrites) UpdateRole(_ context.Context, g application.Grantor, roleID uuid.UUID, name, description string) (application.RoleResult, error) {
	f.calls = append(f.calls, "update")
	f.grantors = append(f.grantors, g)
	f.roleIDs = append(f.roleIDs, roleID)
	f.names = append(f.names, name)
	f.descriptions = append(f.descriptions, description)
	return f.result, f.err
}

func (f *rhWrites) SetPermissions(_ context.Context, g application.Grantor, roleID uuid.UUID, requested []string) (application.RoleResult, error) {
	f.calls = append(f.calls, "set")
	f.grantors = append(f.grantors, g)
	f.roleIDs = append(f.roleIDs, roleID)
	f.requested = append(f.requested, requested)
	return f.result, f.err
}

func (f *rhWrites) ArchiveRole(_ context.Context, g application.Grantor, roleID uuid.UUID) error {
	f.calls = append(f.calls, "archive")
	f.grantors = append(f.grantors, g)
	f.roleIDs = append(f.roleIDs, roleID)
	return f.err
}

type rhPerms struct {
	windows []domain.Window

	page domain.PermissionPage

	err error
}

func (f *rhPerms) List(_ context.Context, w domain.Window) (domain.PermissionPage, error) {
	f.windows = append(f.windows, w)
	return f.page, f.err
}

type rhRig struct {
	router http.Handler

	authz *fakeAuthorizer

	reads *rhReads

	write *rhWrites

	perms *rhPerms

	ident authdomain.Identity

	scope domain.ScopeContext

	anonymous bool
}

func newRoleRig(t *testing.T, ident authdomain.Identity) *rhRig {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	scope, err := domain.NewScope(ident.OrganizationID, nil)
	require.NoError(t, err)
	rg := &rhRig{
		authz: &fakeAuthorizer{decision: domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}},
		reads: &rhReads{},
		write: &rhWrites{},
		perms: &rhPerms{},
		ident: ident,
		scope: scope,
	}
	mw, err := authzhttp.New(authzhttp.Deps{Scope: &fakeResolver{}, Authorizer: rg.authz, Audit: &fakeAudit{}, Responder: rs, Log: log})
	require.NoError(t, err)
	h, err := authzhttp.NewRoleHandlers(rs, rg.reads, rg.write, rg.perms)
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

func (g *rhRig) do(method, path, body string) *httptest.ResponseRecorder {
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

func (g *rhRig) untouched() bool {
	return len(g.write.calls) == 0 && len(g.reads.getIDs) == 0 && len(g.reads.listQueries) == 0 && len(g.perms.windows) == 0
}

func rhBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return out
}

func rhErrCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	e, ok := rhBody(t, w)["error"].(map[string]any)
	require.True(t, ok, w.Body.String())
	code, _ := e["code"].(string)
	return code
}

func rhData(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	data, ok := rhBody(t, w)["data"].(map[string]any)
	require.True(t, ok, w.Body.String())
	return data
}

type rhRoute struct {
	method string

	path string

	body string

	action string

	status int
}

func rhRoutes() []rhRoute {
	rid := id.NewID().String()
	return []rhRoute{
		{http.MethodGet, "/roles", "", permission.RoleRead, http.StatusOK},
		{http.MethodPost, "/roles", `{"name":"A","slug":"a","type":"custom","permissions":[]}`, permission.RoleManage, http.StatusCreated},
		{http.MethodGet, "/roles/" + rid, "", permission.RoleRead, http.StatusOK},
		{http.MethodPatch, "/roles/" + rid, `{"name":"B"}`, permission.RoleManage, http.StatusOK},
		{http.MethodDelete, "/roles/" + rid, "", permission.RoleManage, http.StatusNoContent},
		{http.MethodGet, "/roles/" + rid + "/permissions", "", permission.RoleRead, http.StatusOK},
		{http.MethodPut, "/roles/" + rid + "/permissions", `{"permissions":[]}`, permission.PermissionAssign, http.StatusOK},
		{http.MethodGet, "/permissions", "", permission.PermissionRead, http.StatusOK},
	}
}

func TestRoleRoutesAskTheDocumentedActionForEachRoute(t *testing.T) {
	for _, c := range rhRoutes() {
		rg := newRoleRig(t, tenantIdentity())

		w := rg.do(c.method, c.path, c.body)

		label := c.method + " " + c.path
		require.Equal(t, c.status, w.Code, label+": "+w.Body.String())
		require.Len(t, rg.authz.checks, 1, label)
		require.Equal(t, c.action, rg.authz.checks[0].Action, label)
	}
}

func TestEveryRoleRouteIsDeniedBeforeAnyReadOrWrite(t *testing.T) {
	for _, c := range rhRoutes() {
		rg := newRoleRig(t, tenantIdentity())
		rg.authz.decision = domain.Decision{Allowed: false, Reason: domain.ReasonNoPermission}

		w := rg.do(c.method, c.path, c.body)

		label := c.method + " " + c.path
		require.Equal(t, http.StatusForbidden, w.Code, label)
		require.Equal(t, httpx.PermissionDenied.Name, rhErrCode(t, w), label)
		require.True(t, rg.untouched(), label+": tidak boleh ada baca atau tulis sebelum Authorize meloloskan")
	}
}

func TestRoleRoutesRejectAnonymousCallers(t *testing.T) {
	for _, c := range rhRoutes() {
		rg := newRoleRig(t, tenantIdentity())
		rg.anonymous = true

		w := rg.do(c.method, c.path, c.body)

		label := c.method + " " + c.path
		require.Equal(t, http.StatusUnauthorized, w.Code, label)
		require.Equal(t, httpx.AuthenticationRequired.Name, rhErrCode(t, w), label)
		require.True(t, rg.untouched(), label)
	}
}

func TestAuthorizeRunsBeforeBodyDecoding(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())
	rg.authz.decision = domain.Decision{Allowed: false, Reason: domain.ReasonNoPermission}

	w := rg.do(http.MethodPost, "/roles", "bukan json")

	require.Equal(t, http.StatusForbidden, w.Code, "penolakan izin mendahului validasi body")
	require.Empty(t, rg.write.calls)
}

func TestCreateRoleDerivesGrantorFromIdentityAndScopeAndReturnsDetail(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())
	orgID := *rg.ident.OrganizationID
	rg.write.result = application.RoleResult{
		Role:        domain.RoleRecord{ID: id.NewID(), OrganizationID: &orgID, Name: "Kasir", Slug: "kasir", Type: domain.RoleTypeCustom, Status: "active"},
		Permissions: []string{"invoice.read"},
	}

	w := rg.do(http.MethodPost, "/roles", `{"name":"Kasir","slug":"kasir","type":"custom","description":"d","permissions":["invoice.read"]}`)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, []string{"create"}, rg.write.calls)
	g := rg.write.grantors[0]
	require.Equal(t, rg.ident.UserID, g.UserID)
	require.Equal(t, rg.scope, g.Scope, "scope berasal dari ScopeContext, bukan dari request")
	in := rg.write.created[0]
	require.Equal(t, "Kasir", in.Name)
	require.Equal(t, "kasir", in.Slug)
	require.Equal(t, "custom", in.Type)
	require.Equal(t, "d", in.Description)
	require.Equal(t, []string{"invoice.read"}, in.Permissions)

	data := rhData(t, w)
	require.Equal(t, "kasir", data["slug"])
	require.Equal(t, false, data["is_system"])
	require.Equal(t, []any{"invoice.read"}, data["permissions"])
	require.NotContains(t, data, "organization_id", "organisasi tidak ditampilkan di respons role")
}

func TestCreateRoleRejectsOrganizationAndScopeFieldsInTheBody(t *testing.T) {
	for _, field := range []string{"organization_id", "scope", "scope_type", "scope_id"} {
		rg := newRoleRig(t, tenantIdentity())
		body := `{"name":"A","slug":"a","type":"custom","permissions":[],"` + field + `":"x"}`

		w := rg.do(http.MethodPost, "/roles", body)

		require.Equal(t, http.StatusBadRequest, w.Code, field)
		require.Equal(t, httpx.ValidationFailed.Name, rhErrCode(t, w), field)
		require.Empty(t, rg.write.calls, field+": penulis tidak boleh dipanggil")
	}
}

func TestRoleWriterErrorsAreMappedThroughDomainError(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{domain.ErrRoleSlugTaken, http.StatusConflict, "role_slug_taken"},
		{domain.ErrAuthorityCeiling, http.StatusForbidden, "grant_exceeds_authority"},
		{domain.ErrTargetOutsideScope, http.StatusForbidden, "scope_mismatch"},
		{domain.ErrRoleNotFound, http.StatusNotFound, "role_not_found"},
		{domain.ErrSystemRoleImmutable, http.StatusConflict, "role_immutable"},
		{errors.New("boom"), http.StatusServiceUnavailable, httpx.ServiceUnavailable.Name},
	}
	for _, c := range cases {
		rg := newRoleRig(t, tenantIdentity())
		rg.write.err = c.err

		w := rg.do(http.MethodPost, "/roles", `{"name":"A","slug":"a","type":"custom","permissions":[]}`)

		require.Equal(t, c.status, w.Code, c.code)
		require.Equal(t, c.code, rhErrCode(t, w))
	}
}

func TestGetRoleReadsWithTheOrganizationOfTheScope(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())
	orgID := *rg.ident.OrganizationID
	rid := id.NewID()
	rg.reads.row = domain.RoleRecord{ID: rid, Name: "Kasir", Slug: "kasir", Type: domain.RoleTypeCustom, Status: "active"}
	rg.reads.codes = []string{"invoice.read", "role.read"}

	w := rg.do(http.MethodGet, "/roles/"+rid.String(), "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, rg.reads.getOrgs, 1)
	require.NotNil(t, rg.reads.getOrgs[0])
	require.Equal(t, orgID, *rg.reads.getOrgs[0])
	require.Equal(t, rid, rg.reads.getIDs[0])
	data := rhData(t, w)
	require.Equal(t, []any{"invoice.read", "role.read"}, data["permissions"])
	require.NotContains(t, data, "organization_id")
}

func TestGetRoleAtPlatformScopePassesNilOrganization(t *testing.T) {
	rg := newRoleRig(t, platformIdentity())

	w := rg.do(http.MethodGet, "/roles/"+id.NewID().String(), "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, rg.reads.getOrgs, 1)
	require.Nil(t, rg.reads.getOrgs[0], "scope platform tidak pernah memakai organisasi lain")
}

func TestGetRoleNotFoundVariantsLookTheSame(t *testing.T) {
	missing := newRoleRig(t, tenantIdentity())
	missing.reads.getErr = domain.ErrRoleNotFound
	wMissing := missing.do(http.MethodGet, "/roles/"+id.NewID().String(), "")

	malformed := newRoleRig(t, tenantIdentity())
	wMalformed := malformed.do(http.MethodGet, "/roles/bukan-uuid", "")

	nilID := newRoleRig(t, tenantIdentity())
	wNil := nilID.do(http.MethodGet, "/roles/"+uuid.Nil.String(), "")

	for _, w := range []*httptest.ResponseRecorder{wMissing, wMalformed, wNil} {
		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
		require.Equal(t, "role_not_found", rhErrCode(t, w))
	}
	require.Empty(t, malformed.reads.getIDs, "id cacat tidak sampai ke repositori")
	require.Empty(t, nilID.reads.getIDs)
}

func TestListRolesUsesDefaultsAndPassesScopeOrganization(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())
	orgID := *rg.ident.OrganizationID

	w := rg.do(http.MethodGet, "/roles", "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, rg.reads.listQueries, 1)
	require.NotNil(t, rg.reads.listOrgs[0])
	require.Equal(t, orgID, *rg.reads.listOrgs[0])
	require.Equal(t, "", rg.reads.listQueries[0].Status)
	require.Equal(t, domain.Window{Offset: 0, Limit: domain.DefaultPageSize}, rg.reads.listQueries[0].Window)

	body := rhBody(t, w)
	require.Equal(t, []any{}, body["data"], "daftar kosong tampil sebagai [], bukan null")
	meta, ok := body["meta"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, map[string]any{"page": float64(1), "per_page": float64(25), "total": float64(0)}, meta["pagination"])
}

func TestListRolesTranslatesPageAndPerPageIntoAWindow(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())
	rg.reads.page = domain.RolePage{
		Items: []domain.RoleRecord{{ID: id.NewID(), Name: "Kasir", Slug: "kasir", Type: domain.RoleTypeCustom, Status: "active"}},
		Total: 42,
	}

	w := rg.do(http.MethodGet, "/roles?page=3&per_page=10&status=archived", "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, domain.RoleListQuery{Status: "archived", Window: domain.Window{Offset: 20, Limit: 10}}, rg.reads.listQueries[0])
	body := rhBody(t, w)
	data, ok := body["data"].([]any)
	require.True(t, ok)
	require.Len(t, data, 1)
	first, ok := data[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "kasir", first["slug"])
	require.NotContains(t, first, "organization_id")
	meta, ok := body["meta"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, map[string]any{"page": float64(3), "per_page": float64(10), "total": float64(42)}, meta["pagination"])
}

func TestListRolesClampsOversizedPerPageInsteadOfRejecting(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())

	w := rg.do(http.MethodGet, "/roles?per_page=100000", "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, domain.MaxPageSize, rg.reads.listQueries[0].Window.Limit)
	meta, ok := rhBody(t, w)["meta"].(map[string]any)
	require.True(t, ok)
	pg, ok := meta["pagination"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(domain.MaxPageSize), pg["per_page"], "meta melaporkan ukuran halaman yang berlaku")
}

func TestListRolesRejectsMalformedQueryWithoutTouchingTheRepository(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())

	w := rg.do(http.MethodGet, "/roles?page=0&per_page=abc&status=bogus", "")

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, httpx.ValidationFailed.Name, rhErrCode(t, w))
	e, ok := rhBody(t, w)["error"].(map[string]any)
	require.True(t, ok)
	details, ok := e["details"].(map[string]any)
	require.True(t, ok)
	fields, ok := details["fields"].([]any)
	require.True(t, ok)
	require.Len(t, fields, 3, "ketiga parameter yang salah dilaporkan sekaligus")
	require.True(t, rg.untouched())

	for _, q := range []string{"page=-1", "page=10001", "per_page=0", "per_page=-5", "page=1.5"} {
		w := rg.do(http.MethodGet, "/roles?"+q, "")
		require.Equal(t, http.StatusBadRequest, w.Code, q)
	}
	require.True(t, rg.untouched())
}

func TestUpdateRoleMergesPartialPatchWithCurrentValues(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())
	rid := id.NewID()
	rg.reads.row = domain.RoleRecord{ID: rid, Name: "Lama", Description: "uraian lama", Type: domain.RoleTypeCustom, Status: "active"}
	rg.write.result = application.RoleResult{Role: rg.reads.row}

	w := rg.do(http.MethodPatch, "/roles/"+rid.String(), `{"description":"uraian baru"}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "Lama", rg.write.names[0], "nama yang sedang berlaku dipertahankan")
	require.Equal(t, "uraian baru", rg.write.descriptions[0])
	require.Equal(t, rid, rg.write.roleIDs[0])
	require.Equal(t, rg.scope, rg.write.grantors[0].Scope)
	require.Equal(t, rg.ident.UserID, rg.write.grantors[0].UserID)
}

func TestUpdateRoleRejectsEmptyPatchAndUnknownRole(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())
	rid := id.NewID().String()

	empty := rg.do(http.MethodPatch, "/roles/"+rid, `{}`)
	require.Equal(t, http.StatusBadRequest, empty.Code, empty.Body.String())
	require.Empty(t, rg.write.calls)
	require.Empty(t, rg.reads.getIDs, "perubahan kosong ditolak sebelum membaca role")

	rg.reads.getErr = domain.ErrRoleNotFound
	missing := rg.do(http.MethodPatch, "/roles/"+rid, `{"name":"B"}`)
	require.Equal(t, http.StatusNotFound, missing.Code, missing.Body.String())
	require.Equal(t, "role_not_found", rhErrCode(t, missing))
	require.Empty(t, rg.write.calls, "role yang tidak terlihat tidak sampai ke penulis")
}

func TestUpdateRoleRejectsSlugAndOrganizationInTheBody(t *testing.T) {
	for _, field := range []string{"slug", "organization_id", "permissions"} {
		rg := newRoleRig(t, tenantIdentity())

		w := rg.do(http.MethodPatch, "/roles/"+id.NewID().String(), `{"name":"B","`+field+`":"x"}`)

		require.Equal(t, http.StatusBadRequest, w.Code, field)
		require.Empty(t, rg.write.calls, field)
	}
}

func TestArchiveRoleCallsWriterAndReturnsNoContent(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())
	rid := id.NewID()

	w := rg.do(http.MethodDelete, "/roles/"+rid.String(), "")

	require.Equal(t, http.StatusNoContent, w.Code)
	require.Empty(t, w.Body.String())
	require.Equal(t, []string{"archive"}, rg.write.calls)
	require.Equal(t, rid, rg.write.roleIDs[0])
	require.Equal(t, rg.scope, rg.write.grantors[0].Scope)
}

func TestArchiveRoleMapsWriterErrors(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())
	rg.write.err = domain.ErrSystemRoleImmutable

	w := rg.do(http.MethodDelete, "/roles/"+id.NewID().String(), "")

	require.Equal(t, http.StatusConflict, w.Code)
	require.Equal(t, "role_immutable", rhErrCode(t, w))
}

func TestGetRolePermissionsReturnsTheSortedCodesFromTheReader(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())
	rg.reads.codes = []string{"a.read", "b.read"}

	w := rg.do(http.MethodGet, "/roles/"+id.NewID().String()+"/permissions", "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []any{"a.read", "b.read"}, rhData(t, w)["permissions"])
}

func TestGetRolePermissionsReturnsEmptyArrayNotNull(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())

	w := rg.do(http.MethodGet, "/roles/"+id.NewID().String()+"/permissions", "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []any{}, rhData(t, w)["permissions"])
}

func TestSetRolePermissionsRequiresThePermissionsField(t *testing.T) {
	for _, body := range []string{`{}`, `{"permissions":null}`} {
		rg := newRoleRig(t, tenantIdentity())

		w := rg.do(http.MethodPut, "/roles/"+id.NewID().String()+"/permissions", body)

		require.Equal(t, http.StatusBadRequest, w.Code, body)
		require.Equal(t, httpx.ValidationFailed.Name, rhErrCode(t, w), body)
		require.Empty(t, rg.write.calls, body+": body yang salah ketik tidak boleh menghapus permission role")
	}
}

func TestSetRolePermissionsPassesTheFullSetAndReturnsTheResult(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())
	rid := id.NewID()
	rg.write.result = application.RoleResult{Permissions: []string{"invoice.read"}}

	w := rg.do(http.MethodPut, "/roles/"+rid.String()+"/permissions", `{"permissions":["invoice.read"]}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, [][]string{{"invoice.read"}}, rg.write.requested)
	require.Equal(t, rid, rg.write.roleIDs[0])
	require.Equal(t, rg.scope, rg.write.grantors[0].Scope)
	require.Equal(t, []any{"invoice.read"}, rhData(t, w)["permissions"])
}

func TestSetRolePermissionsWithEmptySetClearsAndReturnsEmptyArray(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())

	w := rg.do(http.MethodPut, "/roles/"+id.NewID().String()+"/permissions", `{"permissions":[]}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, rg.write.requested, 1)
	require.NotNil(t, rg.write.requested[0], "irisan kosong sah dan diteruskan apa adanya")
	require.Empty(t, rg.write.requested[0])
	require.Equal(t, []any{}, rhData(t, w)["permissions"])
}

func TestSetRolePermissionsMapsCeilingAndUnknownPermissionErrors(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{domain.ErrAuthorityCeiling, http.StatusForbidden, "grant_exceeds_authority"},
		{domain.ErrUnknownPermission, http.StatusUnprocessableEntity, "permission_not_found"},
		{domain.ErrRoleArchived, http.StatusConflict, "role_archived"},
	}
	for _, c := range cases {
		rg := newRoleRig(t, tenantIdentity())
		rg.write.err = c.err

		w := rg.do(http.MethodPut, "/roles/"+id.NewID().String()+"/permissions", `{"permissions":["x.read"]}`)

		require.Equal(t, c.status, w.Code, c.code)
		require.Equal(t, c.code, rhErrCode(t, w))
	}
}

func TestListPermissionsReturnsCatalogWithNullModuleForCoreCodes(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())
	rg.perms.page = domain.PermissionPage{
		Items: []domain.PermissionRecord{
			{Code: "role.read", Resource: "role", Action: "read", Description: "baca role", System: true},
			{Code: "billing.invoice.export", Resource: "billing.invoice", Action: "export", Module: "billing"},
		},
		Total: 2,
	}

	w := rg.do(http.MethodGet, "/permissions?page=2&per_page=5", "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []domain.Window{{Offset: 5, Limit: 5}}, rg.perms.windows)
	body := rhBody(t, w)
	data, ok := body["data"].([]any)
	require.True(t, ok)
	require.Len(t, data, 2)
	core, ok := data[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "role.read", core["code"])
	require.Nil(t, core["module"], "permission core tidak punya modul")
	require.Equal(t, true, core["is_system"])
	mod, ok := data[1].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "billing", mod["module"])
	require.Equal(t, false, mod["is_system"])
	meta, ok := body["meta"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, map[string]any{"page": float64(2), "per_page": float64(5), "total": float64(2)}, meta["pagination"])
}

func TestListPermissionsRejectsMalformedPaging(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())

	w := rg.do(http.MethodGet, "/permissions?page=x", "")

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Empty(t, rg.perms.windows)
}

func TestReaderFailureOnListBecomesFailClosedUnavailable(t *testing.T) {
	rg := newRoleRig(t, tenantIdentity())
	rg.reads.listErr = errors.New("koneksi putus")

	w := rg.do(http.MethodGet, "/roles", "")

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.NotContains(t, w.Body.String(), "koneksi putus", "teks galat internal tidak dikirim ke klien")
}

func TestNewRoleHandlersRequiresEveryDependency(t *testing.T) {
	rs := httpx.NewResponder(nil)
	reads, write, perms := &rhReads{}, &rhWrites{}, &rhPerms{}

	_, err := authzhttp.NewRoleHandlers(nil, reads, write, perms)
	require.Error(t, err)
	_, err = authzhttp.NewRoleHandlers(rs, nil, write, perms)
	require.Error(t, err)
	_, err = authzhttp.NewRoleHandlers(rs, reads, nil, perms)
	require.Error(t, err)
	_, err = authzhttp.NewRoleHandlers(rs, reads, write, nil)
	require.Error(t, err)

	h, err := authzhttp.NewRoleHandlers(rs, reads, write, perms)
	require.NoError(t, err)
	require.NotNil(t, h)
}

func TestMountPanicsWithoutAuthorizeInsteadOfLeavingRoutesUnguarded(t *testing.T) {
	h, err := authzhttp.NewRoleHandlers(httpx.NewResponder(nil), &rhReads{}, &rhWrites{}, &rhPerms{})
	require.NoError(t, err)

	require.Panics(t, func() { h.Mount(chi.NewRouter(), nil) })
}
