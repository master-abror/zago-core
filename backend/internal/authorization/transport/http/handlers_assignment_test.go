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
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes handler role-assignments. Seperti tes handler role: Authorize SUNGGUHAN dengan Authorizer
// palsu supaya aksi per rute terbukti; Authenticate dan Scope diganti middleware kecil. Tes replay
// Idempotency-Key yang memakai Redis nyata ada di handlers_assignment_idem_test.go.

var _ authzhttp.AssignmentReads = (*repository.AssignmentReader)(nil)

var _ authzhttp.AssignmentManagement = (*application.AssignmentWriter)(nil)

type ahReads struct {
	orgs []*uuid.UUID

	queries []domain.AssignmentListQuery

	rows []domain.AssignmentView

	err error
}

func (f *ahReads) List(_ context.Context, org *uuid.UUID, q domain.AssignmentListQuery) ([]domain.AssignmentView, error) {
	f.orgs = append(f.orgs, org)
	f.queries = append(f.queries, q)
	return f.rows, f.err
}

type ahWrites struct {
	calls []string

	grantors []application.Grantor

	inputs []application.CreateAssignmentInput

	ids []uuid.UUID

	result application.AssignmentResult

	err error
}

func (f *ahWrites) Create(_ context.Context, g application.Grantor, in application.CreateAssignmentInput) (application.AssignmentResult, error) {
	f.calls = append(f.calls, "create")
	f.grantors = append(f.grantors, g)
	f.inputs = append(f.inputs, in)
	return f.result, f.err
}

func (f *ahWrites) Revoke(_ context.Context, g application.Grantor, assignmentID uuid.UUID) error {
	f.calls = append(f.calls, "revoke")
	f.grantors = append(f.grantors, g)
	f.ids = append(f.ids, assignmentID)
	return f.err
}

type ahRig struct {
	router http.Handler

	authz *fakeAuthorizer

	reads *ahReads

	write *ahWrites

	ident authdomain.Identity

	scope domain.ScopeContext

	anonymous bool

	idemReached int
}

const ahCursorSecret = "0123456789abcdef0123456789abcdef"

// newAssignmentRig merakit rute dengan Authorize sungguhan. idem nil = penerus yang hanya mencatat
// bahwa ia tercapai (idemReached); tes replay memberi Idempotency sungguhan.
func newAssignmentRig(t *testing.T, ident authdomain.Identity, idem func(http.Handler) http.Handler) *ahRig {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	scope, err := domain.NewScope(ident.OrganizationID, nil)
	require.NoError(t, err)
	rg := &ahRig{
		authz: &fakeAuthorizer{decision: domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}},
		reads: &ahReads{},
		write: &ahWrites{},
		ident: ident,
		scope: scope,
	}
	if idem == nil {
		idem = func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rg.idemReached++
				next.ServeHTTP(w, r)
			})
		}
	}
	mw, err := authzhttp.New(authzhttp.Deps{Scope: &fakeResolver{}, Authorizer: rg.authz, Audit: &fakeAudit{}, Responder: rs, Log: log})
	require.NoError(t, err)
	cursors, err := httpx.NewCursorCodec(ahCursorSecret)
	require.NoError(t, err)
	h, err := authzhttp.NewAssignmentHandlers(rs, cursors, rg.reads, rg.write)
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
	h.Mount(router, mw.Authorize, idem)
	rg.router = router
	return rg
}

func (g *ahRig) do(method, path, body string) *httptest.ResponseRecorder {
	return g.doKey(method, path, body, "")
}

func (g *ahRig) doKey(method, path, body, key string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	g.router.ServeHTTP(w, req)
	return w
}

func (g *ahRig) untouched() bool {
	return len(g.write.calls) == 0 && len(g.reads.queries) == 0 && g.idemReached == 0
}

func ahPostBody(scopeType string) string {
	return fmt.Sprintf(`{"role_id":%q,"subject_type":"user","subject_id":%q,"scope_type":%q}`, id.NewID().String(), id.NewID().String(), scopeType)
}

// ahFields memetakan details.fields galat validasi menjadi field -> code.
func ahFields(t *testing.T, w *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	e, ok := rhBody(t, w)["error"].(map[string]any)
	require.True(t, ok, w.Body.String())
	details, ok := e["details"].(map[string]any)
	require.True(t, ok, w.Body.String())
	raw, ok := details["fields"].([]any)
	require.True(t, ok, w.Body.String())
	out := map[string]string{}
	for _, f := range raw {
		m, _ := f.(map[string]any)
		name, _ := m["field"].(string)
		code, _ := m["code"].(string)
		out[name] = code
	}
	return out
}

func ahPagination(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	meta, ok := rhBody(t, w)["meta"].(map[string]any)
	require.True(t, ok, w.Body.String())
	p, ok := meta["pagination"].(map[string]any)
	require.True(t, ok, w.Body.String())
	return p
}

type ahRoute struct {
	method string

	path string

	body string

	action string

	status int
}

func ahRoutes() []ahRoute {
	aid := id.NewID().String()
	return []ahRoute{
		{http.MethodGet, "/role-assignments", "", permission.RoleRead, http.StatusOK},
		{http.MethodPost, "/role-assignments", ahPostBody("organization"), permission.RoleAssign, http.StatusCreated},
		{http.MethodDelete, "/role-assignments/" + aid, "", permission.RoleAssign, http.StatusNoContent},
	}
}

func TestAssignmentRoutesAskTheDocumentedActionForEachRoute(t *testing.T) {
	for _, c := range ahRoutes() {
		rg := newAssignmentRig(t, tenantIdentity(), nil)

		w := rg.do(c.method, c.path, c.body)

		label := c.method + " " + c.path
		require.Equal(t, c.status, w.Code, label+": "+w.Body.String())
		require.Len(t, rg.authz.checks, 1, label)
		require.Equal(t, c.action, rg.authz.checks[0].Action, label)
	}
}

func TestEveryAssignmentRouteIsDeniedBeforeAnyReadWriteOrIdempotency(t *testing.T) {
	for _, c := range ahRoutes() {
		rg := newAssignmentRig(t, tenantIdentity(), nil)
		rg.authz.decision = domain.Decision{Allowed: false, Reason: domain.ReasonNoPermission}

		w := rg.do(c.method, c.path, c.body)

		label := c.method + " " + c.path
		require.Equal(t, http.StatusForbidden, w.Code, label)
		require.Equal(t, httpx.PermissionDenied.Name, rhErrCode(t, w), label)
		require.True(t, rg.untouched(), label+": Authorize harus mendahului baca, tulis, dan idempotensi")
	}
}

func TestAssignmentRoutesRejectAnonymousCallers(t *testing.T) {
	for _, c := range ahRoutes() {
		rg := newAssignmentRig(t, tenantIdentity(), nil)
		rg.anonymous = true

		w := rg.do(c.method, c.path, c.body)

		label := c.method + " " + c.path
		require.Equal(t, http.StatusUnauthorized, w.Code, label)
		require.Equal(t, httpx.AuthenticationRequired.Name, rhErrCode(t, w), label)
		require.True(t, rg.untouched(), label)
	}
}

func TestAssignmentAuthorizeRunsBeforeBodyDecoding(t *testing.T) {
	rg := newAssignmentRig(t, tenantIdentity(), nil)
	rg.authz.decision = domain.Decision{Allowed: false, Reason: domain.ReasonNoPermission}

	w := rg.do(http.MethodPost, "/role-assignments", "bukan json")

	require.Equal(t, http.StatusForbidden, w.Code, "penolakan izin mendahului validasi body")
	require.Empty(t, rg.write.calls)
}

func TestListAssignmentsUsesOnlyTheCallerOrganizationFromScope(t *testing.T) {
	orgA, orgB := tenantIdentity(), tenantIdentity()

	a := newAssignmentRig(t, orgA, nil)
	w := a.do(http.MethodGet, "/role-assignments?organization_id="+orgB.OrganizationID.String(), "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, a.reads.orgs, 1)
	require.NotNil(t, a.reads.orgs[0])
	require.Equal(t, *orgA.OrganizationID, *a.reads.orgs[0], "organisasi dari query tidak boleh dipakai")

	b := newAssignmentRig(t, orgB, nil)
	w = b.do(http.MethodGet, "/role-assignments?organization_id="+orgA.OrganizationID.String(), "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, *orgB.OrganizationID, *b.reads.orgs[0])

	p := newAssignmentRig(t, platformIdentity(), nil)
	w = p.do(http.MethodGet, "/role-assignments?organization_id="+orgA.OrganizationID.String(), "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, p.reads.orgs, 1)
	require.Nil(t, p.reads.orgs[0], "pemanggil platform hanya melihat assignment platform")
}

func TestListAssignmentsDefaultsAndFilterPassThrough(t *testing.T) {
	rg := newAssignmentRig(t, tenantIdentity(), nil)
	w := rg.do(http.MethodGet, "/role-assignments", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	q := rg.reads.queries[0]
	require.Equal(t, domain.AssignmentFilterActive, q.Status)
	require.Equal(t, domain.DefaultPageSize+1, q.Limit, "satu baris pengintai untuk has_more")
	require.Nil(t, q.After)
	require.Nil(t, q.RoleID)
	require.Nil(t, q.SubjectID)
	require.False(t, q.Now.IsZero())

	roleID, subjectID := id.NewID(), id.NewID()
	path := "/role-assignments?status=all&limit=500&role_id=" + roleID.String() + "&subject_id=" + subjectID.String()
	w = rg.do(http.MethodGet, path, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	q = rg.reads.queries[1]
	require.Equal(t, domain.AssignmentFilterAll, q.Status)
	require.Equal(t, domain.MaxPageSize+1, q.Limit, "limit dipangkas ke maksimum")
	require.NotNil(t, q.RoleID)
	require.Equal(t, roleID, *q.RoleID)
	require.NotNil(t, q.SubjectID)
	require.Equal(t, subjectID, *q.SubjectID)
}

func TestListAssignmentsReportsEveryBadParameterAtOnce(t *testing.T) {
	rg := newAssignmentRig(t, tenantIdentity(), nil)

	w := rg.do(http.MethodGet, "/role-assignments?status=bogus&role_id=x&subject_id="+uuid.Nil.String()+"&limit=0", "")

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, httpx.ValidationFailed.Name, rhErrCode(t, w))
	fields := ahFields(t, w)
	require.Len(t, fields, 4, w.Body.String())
	for _, name := range []string{"status", "role_id", "subject_id", "limit"} {
		require.Contains(t, fields, name)
	}
	require.Empty(t, rg.reads.queries)
}

func ahViews(n int) []domain.AssignmentView {
	base := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	views := make([]domain.AssignmentView, 0, n)
	for i := 0; i < n; i++ {
		var v domain.AssignmentView
		v.ID = id.NewID()
		v.RoleID = id.NewID()
		v.SubjectType = "user"
		v.SubjectID = id.NewID()
		v.Scope = domain.PlatformTarget()
		v.CreatedAt = base.Add(-time.Duration(i) * time.Minute)
		views = append(views, v)
	}
	return views
}

func TestListAssignmentsPaginatesWithAKeysetCursor(t *testing.T) {
	rg := newAssignmentRig(t, tenantIdentity(), nil)
	rg.reads.rows = ahViews(3)

	w := rg.do(http.MethodGet, "/role-assignments?limit=2", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	data, ok := rhBody(t, w)["data"].([]any)
	require.True(t, ok, w.Body.String())
	require.Len(t, data, 2, "baris pengintai tidak ikut respons")
	p := ahPagination(t, w)
	require.Equal(t, true, p["has_more"])
	next, _ := p["next_cursor"].(string)
	require.NotEmpty(t, next)
	require.Equal(t, 3, rg.reads.queries[0].Limit)

	second := rg.reads.rows[1]
	w = rg.do(http.MethodGet, "/role-assignments?limit=2&cursor="+next, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	after := rg.reads.queries[1].After
	require.NotNil(t, after)
	require.Equal(t, second.ID, after.ID)
	require.True(t, after.CreatedAt.Equal(second.CreatedAt))

	rg.reads.rows = ahViews(2)
	w = rg.do(http.MethodGet, "/role-assignments?limit=2", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	p = ahPagination(t, w)
	require.Equal(t, false, p["has_more"])
	require.Nil(t, p["next_cursor"])
}

func TestListAssignmentsCursorIsBoundToFilterAndOrganization(t *testing.T) {
	identA := tenantIdentity()
	a := newAssignmentRig(t, identA, nil)
	a.reads.rows = ahViews(3)
	w := a.do(http.MethodGet, "/role-assignments?limit=2", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	next, _ := ahPagination(t, w)["next_cursor"].(string)
	require.NotEmpty(t, next)
	calls := len(a.reads.queries)

	// filter berbeda
	w = a.do(http.MethodGet, "/role-assignments?limit=2&status=all&cursor="+next, "")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "cursor_mismatch", ahFields(t, w)["cursor"])

	// organisasi berbeda, rahasia cursor sama
	b := newAssignmentRig(t, tenantIdentity(), nil)
	w = b.do(http.MethodGet, "/role-assignments?limit=2&cursor="+next, "")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "cursor_mismatch", ahFields(t, w)["cursor"])
	require.Empty(t, b.reads.queries)

	// token rusak
	w = a.do(http.MethodGet, "/role-assignments?cursor=bukan-cursor", "")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "invalid_cursor", ahFields(t, w)["cursor"])
	require.Len(t, a.reads.queries, calls, "cursor yang ditolak tidak boleh sampai ke pembaca")
}

func TestListAssignmentsEmptyResultIsAnEmptyArrayWithoutOrganizationID(t *testing.T) {
	rg := newAssignmentRig(t, tenantIdentity(), nil)

	w := rg.do(http.MethodGet, "/role-assignments", "")

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	data, ok := rhBody(t, w)["data"].([]any)
	require.True(t, ok, "data harus larik, bukan null: "+w.Body.String())
	require.Empty(t, data)

	rg.reads.rows = ahViews(1)
	w = rg.do(http.MethodGet, "/role-assignments", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "organization_id")
}

func TestListAssignmentsReaderFailureIsServiceUnavailableWithoutLeaking(t *testing.T) {
	rg := newAssignmentRig(t, tenantIdentity(), nil)
	rg.reads.err = errors.New("pg down: relation role_assignments")

	w := rg.do(http.MethodGet, "/role-assignments", "")

	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Equal(t, httpx.ServiceUnavailable.Name, rhErrCode(t, w))
	require.NotContains(t, w.Body.String(), "pg down")
}

func TestCreateAssignmentDerivesGrantorFromIdentityAndScopeAndReturnsIt(t *testing.T) {
	rg := newAssignmentRig(t, tenantIdentity(), nil)
	orgID := *rg.ident.OrganizationID
	rec := domain.AssignmentRecord{ID: id.NewID(), RoleID: id.NewID(), SubjectType: "user", SubjectID: id.NewID(), Scope: domain.OrganizationTarget(orgID), CreatedAt: time.Now()}
	rg.write.result = application.AssignmentResult{Assignment: rec, Role: domain.RoleRecord{Name: "Kasir", Slug: "kasir", Status: "active"}}

	body := fmt.Sprintf(`{"role_id":%q,"subject_type":"user","subject_id":%q,"scope_type":"organization","expires_at":"2099-01-02T03:04:05Z"}`, rec.RoleID.String(), rec.SubjectID.String())
	w := rg.do(http.MethodPost, "/role-assignments", body)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, []string{"create"}, rg.write.calls)
	g := rg.write.grantors[0]
	require.Equal(t, rg.ident.UserID, g.UserID)
	require.Equal(t, rg.scope, g.Scope, "scope berasal dari ScopeContext, bukan dari request")
	in := rg.write.inputs[0]
	require.Equal(t, rec.RoleID, in.RoleID)
	require.Equal(t, "user", in.SubjectType)
	require.Equal(t, rec.SubjectID, in.SubjectID)
	require.Equal(t, domain.OrganizationTarget(orgID), in.Scope)
	require.NotNil(t, in.ExpiresAt)

	data := rhData(t, w)
	require.Equal(t, rec.ID.String(), data["id"])
	require.Equal(t, "organization", data["scope_type"])
	require.Equal(t, "Kasir", data["role_name"])
	require.Equal(t, "active", data["status"])
	require.NotContains(t, w.Body.String(), "organization_id")
}

func TestCreateAssignmentRejectsOrganizationFromTheBody(t *testing.T) {
	rg := newAssignmentRig(t, tenantIdentity(), nil)
	body := fmt.Sprintf(`{"role_id":%q,"subject_type":"user","subject_id":%q,"scope_type":"organization","organization_id":%q}`, id.NewID().String(), id.NewID().String(), id.NewID().String())

	w := rg.do(http.MethodPost, "/role-assignments", body)

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, "unknown_field", ahFields(t, w)["organization_id"])
	require.Empty(t, rg.write.calls)
}

func TestCreateAssignmentReportsEveryInvalidFieldAtOnce(t *testing.T) {
	rg := newAssignmentRig(t, tenantIdentity(), nil)

	w := rg.do(http.MethodPost, "/role-assignments", `{}`)

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	fields := ahFields(t, w)
	require.Len(t, fields, 4, w.Body.String())
	for _, name := range []string{"role_id", "subject_type", "subject_id", "scope_type"} {
		require.Equal(t, "required", fields[name], name)
	}
	require.Empty(t, rg.write.calls)
}

func TestCreateAssignmentPlatformCallerCannotTargetAnOrganizationOrGroup(t *testing.T) {
	rg := newAssignmentRig(t, platformIdentity(), nil)

	w := rg.do(http.MethodPost, "/role-assignments", ahPostBody("organization"))
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Equal(t, authzhttp.ScopeMismatch.Name, rhErrCode(t, w))

	body := fmt.Sprintf(`{"role_id":%q,"subject_type":"user","subject_id":%q,"scope_type":"group","group_id":%q}`, id.NewID().String(), id.NewID().String(), id.NewID().String())
	w = rg.do(http.MethodPost, "/role-assignments", body)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Equal(t, authzhttp.ScopeMismatch.Name, rhErrCode(t, w))
	require.Empty(t, rg.write.calls, "penulis tidak dipanggil untuk target di luar scope platform")

	w = rg.do(http.MethodPost, "/role-assignments", ahPostBody("platform"))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, []string{"create"}, rg.write.calls)
	require.Equal(t, domain.PlatformTarget(), rg.write.inputs[0].Scope)
}

func TestCreateAssignmentPlatformScopeFromTenantCallerIsLeftToTheWriter(t *testing.T) {
	rg := newAssignmentRig(t, tenantIdentity(), nil)
	rg.write.err = domain.ErrTargetOutsideScope

	w := rg.do(http.MethodPost, "/role-assignments", ahPostBody("platform"))

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Equal(t, authzhttp.ScopeMismatch.Name, rhErrCode(t, w))
	require.Equal(t, []string{"create"}, rg.write.calls)
	require.Equal(t, domain.PlatformTarget(), rg.write.inputs[0].Scope)
}

type ahErrCase struct {
	err error

	status int

	code string
}

func TestCreateAssignmentMapsEveryWriterErrorWithoutLeaking(t *testing.T) {
	cases := []ahErrCase{
		{domain.ErrAuthorityCeiling, http.StatusForbidden, authzhttp.GrantExceedsAuthority.Name},
		{domain.ErrTargetOutsideScope, http.StatusForbidden, authzhttp.ScopeMismatch.Name},
		{domain.ErrAssignmentScope, http.StatusBadRequest, authzhttp.InvalidScope.Name},
		{domain.ErrRoleNotFound, http.StatusNotFound, authzhttp.RoleNotFound.Name},
		{domain.ErrRoleArchived, http.StatusConflict, authzhttp.RoleArchived.Name},
		{domain.ErrAssignmentExists, http.StatusConflict, authzhttp.AssignmentExists.Name},
		{domain.ErrSubjectNotFound, http.StatusBadRequest, httpx.ValidationFailed.Name},
		{domain.ErrGroupNotFound, http.StatusBadRequest, httpx.ValidationFailed.Name},
		{domain.ErrAssignmentSubject, http.StatusBadRequest, httpx.ValidationFailed.Name},
		{domain.ErrAssignmentExpiry, http.StatusBadRequest, httpx.ValidationFailed.Name},
		{domain.ErrInvalidGrantTarget, http.StatusInternalServerError, httpx.InternalError.Name},
		{fmt.Errorf("lapisan: %w", domain.ErrAssignmentExists), http.StatusConflict, authzhttp.AssignmentExists.Name},
		{errors.New("pg down: role_assignments"), http.StatusServiceUnavailable, httpx.ServiceUnavailable.Name},
	}
	for _, c := range cases {
		rg := newAssignmentRig(t, tenantIdentity(), nil)
		rg.write.err = c.err

		w := rg.do(http.MethodPost, "/role-assignments", ahPostBody("organization"))

		require.Equal(t, c.status, w.Code, c.err.Error()+": "+w.Body.String())
		require.Equal(t, c.code, rhErrCode(t, w), c.err.Error())
		require.NotContains(t, w.Body.String(), "pg down")
		require.NotContains(t, w.Body.String(), "lapisan")
	}
}

func TestRevokeAssignmentDerivesGrantorAndAnswersNoContent(t *testing.T) {
	rg := newAssignmentRig(t, tenantIdentity(), nil)
	aid := id.NewID()

	w := rg.do(http.MethodDelete, "/role-assignments/"+aid.String(), "")

	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	require.Empty(t, w.Body.String())
	require.Equal(t, []string{"revoke"}, rg.write.calls)
	require.Equal(t, []uuid.UUID{aid}, rg.write.ids)
	require.Equal(t, rg.ident.UserID, rg.write.grantors[0].UserID)
	require.Equal(t, rg.scope, rg.write.grantors[0].Scope)
}

func TestRevokeAssignmentMalformedIDIsNotFoundWithoutTouchingTheWriter(t *testing.T) {
	for _, raw := range []string{"bukan-uuid", uuid.Nil.String(), "123"} {
		rg := newAssignmentRig(t, tenantIdentity(), nil)

		w := rg.do(http.MethodDelete, "/role-assignments/"+raw, "")

		require.Equal(t, http.StatusNotFound, w.Code, raw)
		require.Equal(t, httpx.ResourceNotFound.Name, rhErrCode(t, w), raw)
		require.Empty(t, rg.write.calls, raw)
	}
}

func TestRevokeAssignmentMapsEveryWriterErrorWithoutLeaking(t *testing.T) {
	cases := []ahErrCase{
		{domain.ErrAssignmentNotFound, http.StatusNotFound, httpx.ResourceNotFound.Name},
		{domain.ErrAssignmentRevoked, http.StatusConflict, authzhttp.AssignmentRevoked.Name},
		{domain.ErrLastSuperAdmin, http.StatusConflict, authzhttp.LastSuperAdmin.Name},
		{domain.ErrAuthorityCeiling, http.StatusForbidden, authzhttp.GrantExceedsAuthority.Name},
		{domain.ErrRoleNotFound, http.StatusNotFound, authzhttp.RoleNotFound.Name},
		{errors.New("pg down: role_assignments"), http.StatusServiceUnavailable, httpx.ServiceUnavailable.Name},
	}
	for _, c := range cases {
		rg := newAssignmentRig(t, tenantIdentity(), nil)
		rg.write.err = c.err

		w := rg.do(http.MethodDelete, "/role-assignments/"+id.NewID().String(), "")

		require.Equal(t, c.status, w.Code, c.err.Error()+": "+w.Body.String())
		require.Equal(t, c.code, rhErrCode(t, w), c.err.Error())
		require.NotContains(t, w.Body.String(), "pg down")
	}
}

func TestAssignmentHandlersRefuseMissingDependenciesAndGuards(t *testing.T) {
	rs := httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil)))
	cursors, err := httpx.NewCursorCodec(ahCursorSecret)
	require.NoError(t, err)
	reads, write := &ahReads{}, &ahWrites{}

	_, err = authzhttp.NewAssignmentHandlers(nil, cursors, reads, write)
	require.Error(t, err)
	_, err = authzhttp.NewAssignmentHandlers(rs, nil, reads, write)
	require.Error(t, err)
	_, err = authzhttp.NewAssignmentHandlers(rs, cursors, nil, write)
	require.Error(t, err)
	_, err = authzhttp.NewAssignmentHandlers(rs, cursors, reads, nil)
	require.Error(t, err)

	h, err := authzhttp.NewAssignmentHandlers(rs, cursors, reads, write)
	require.NoError(t, err)
	pass := func(next http.Handler) http.Handler { return next }
	authorize := func(string) func(http.Handler) http.Handler { return pass }
	require.Panics(t, func() { h.Mount(chi.NewRouter(), nil, pass) })
	require.Panics(t, func() { h.Mount(chi.NewRouter(), authorize, nil) })
	require.NotPanics(t, func() { h.Mount(chi.NewRouter(), authorize, pass) })
}
