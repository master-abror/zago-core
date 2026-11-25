package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/app"
	"github.com/master-abror/zago-core/backend/internal/audit"
	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Bukti perakitan rute organisasi (ADR-0023 butir 20): rute /api/v1/organizations* dilayani oleh
// perakit sungguhan (organizationRoutes + MountAuthzRoutes) dengan Scope dan Authorize SUNGGUHAN
// di atas PostgreSQL nyata dan perekam audit nyata. Yang TIDAK ikut: Authenticate, CSRF, sesi, dan
// Redis (tanpa cache, jadi setiap keputusan dihitung dari database). Identity disuntik tes lewat
// header X-Test-User dan X-Test-Org. Setiap tes memakai DUA organisasi bila menyangkut isolasi.

var orgAPIAllCodes = []string{
	permission.OrganizationRead,
	permission.OrganizationCreate,
	permission.OrganizationUpdate,
	permission.OrganizationArchive,
	permission.OrganizationMemberManage,
}

type orgAPILogBuffer struct {
	mu sync.Mutex

	buf bytes.Buffer
}

func (b *orgAPILogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *orgAPILogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type orgAPIRig struct {
	t *testing.T

	db *testpg.DB

	srv http.Handler
}

// orgAPIActor adalah pemanggil: org nil berarti identitas platform.
type orgAPIActor struct {
	user uuid.UUID

	org *uuid.UUID
}

type orgAPIError struct {
	Code string `json:"code"`

	Message string `json:"message"`
}

type orgAPIEnvelope struct {
	Data json.RawMessage `json:"data"`

	Error orgAPIError `json:"error"`
}

type orgAPIOrg struct {
	ID uuid.UUID `json:"id"`

	Slug string `json:"slug"`

	Status string `json:"status"`
}

type orgAPIMember struct {
	UserID uuid.UUID `json:"user_id"`

	Status string `json:"status"`
}

func orgAPIIdentify(req *http.Request) (authdomain.Identity, bool) {
	uid, err := uuid.Parse(req.Header.Get("X-Test-User"))
	if err != nil {
		return authdomain.Identity{}, false
	}
	ident := authdomain.Identity{UserID: uid, SessionID: id.NewID(), AuthLevel: authdomain.AuthLevelPassword}
	if raw := req.Header.Get("X-Test-Org"); raw != "" {
		oid, err := uuid.Parse(raw)
		if err != nil {
			return authdomain.Identity{}, false
		}
		ident.OrganizationID = &oid
	}
	return ident, true
}

func newOrgAPIRig(t *testing.T) *orgAPIRig {
	t.Helper()
	db := testkit.NewDB(t)
	logs := &orgAPILogBuffer{}
	log := slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelWarn}))
	t.Cleanup(func() {
		if t.Failed() {
			t.Log("log aplikasi:\n" + logs.String())
		}
	})
	srv, err := app.OrganizationAPIForTest(db.App, log, audit.New(db.App, log), orgAPIIdentify)
	require.NoError(t, err)
	return &orgAPIRig{t: t, db: db, srv: srv}
}

func (r *orgAPIRig) do(actor orgAPIActor, method, path, body string) *httptest.ResponseRecorder {
	r.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if actor.user != uuid.Nil {
		req.Header.Set("X-Test-User", actor.user.String())
	}
	if actor.org != nil {
		req.Header.Set("X-Test-Org", actor.org.String())
	}
	rec := httptest.NewRecorder()
	r.srv.ServeHTTP(rec, req)
	return rec
}

func (r *orgAPIRig) platformUser() orgAPIActor {
	r.t.Helper()
	u := testkit.NewUser(r.t, r.db)
	role := testkit.RoleWith(r.t, r.db, orgAPIAllCodes)
	testkit.AssignRole(r.t, r.db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: u.ID, ScopeType: "platform"})
	return orgAPIActor{user: u.ID}
}

// tenantUser membuat anggota aktif organisasi o yang memegang codes pada scope organisasi o.
func (r *orgAPIRig) tenantUser(o testkit.Organization, codes ...string) orgAPIActor {
	r.t.Helper()
	u := testkit.NewUser(r.t, r.db, testkit.InOrganization(o))
	oid := o.ID
	r.grantInOrg(u.ID, o, codes...)
	return orgAPIActor{user: u.ID, org: &oid}
}

// grantInOrg memberi userID role dengan codes pada scope organisasi o, tanpa membuat keanggotaan.
func (r *orgAPIRig) grantInOrg(userID uuid.UUID, o testkit.Organization, codes ...string) {
	r.t.Helper()
	oid := o.ID
	spec := testkit.AssignmentSpec{Role: testkit.RoleWith(r.t, r.db, codes), SubjectType: "user", SubjectID: userID}
	spec.ScopeType = "organization"
	spec.OrganizationID = &oid
	testkit.AssignRole(r.t, r.db, spec)
}

func (r *orgAPIRig) count(query string, args ...any) int {
	r.t.Helper()
	var n int
	require.NoError(r.t, r.db.Migrator.QueryRow(context.Background(), query, args...).Scan(&n))
	return n
}

// audits menghitung activity sukses dengan aksi tertentu pada organisasi o.
func (r *orgAPIRig) audits(action string, o uuid.UUID) int {
	r.t.Helper()
	return r.count(`SELECT count(*) FROM activities WHERE action = $1 AND result = 'success' AND organization_id = $2`, action, o.String())
}

// events menghitung event outbox dengan nama tertentu pada organisasi o.
func (r *orgAPIRig) events(name string, o uuid.UUID) int {
	r.t.Helper()
	return r.count(`SELECT count(*) FROM outbox_events WHERE event_name = $1 AND organization_id = $2`, name, o.String())
}

// orgState membaca nama dan status organisasi langsung dari database.
func (r *orgAPIRig) orgState(o uuid.UUID) (string, string) {
	r.t.Helper()
	var name, status string
	require.NoError(r.t, r.db.Migrator.QueryRow(context.Background(), `SELECT name, status FROM organizations WHERE id = $1`, o.String()).Scan(&name, &status))
	return name, status
}

func (r *orgAPIRig) memberStatus(o, u uuid.UUID) string {
	r.t.Helper()
	var s string
	require.NoError(r.t, r.db.Migrator.QueryRow(context.Background(),
		`SELECT status FROM organization_memberships WHERE organization_id = $1 AND user_id = $2`,
		o.String(), u.String()).Scan(&s))
	return s
}

func orgAPIDecode(t *testing.T, rec *httptest.ResponseRecorder) orgAPIEnvelope {
	t.Helper()
	body := rec.Body.Bytes()
	require.NotEmpty(t, body, "body respons tidak boleh kosong (status %d)", rec.Code)
	var env orgAPIEnvelope
	require.NoError(t, json.Unmarshal(body, &env), "body: %s", body)
	return env
}

func orgAPIRequireError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) orgAPIError {
	t.Helper()
	require.Equal(t, status, rec.Code, "body: %s", rec.Body.String())
	env := orgAPIDecode(t, rec)
	require.Equal(t, code, env.Error.Code)
	return env.Error
}

func orgAPIRequireOrg(t *testing.T, rec *httptest.ResponseRecorder, status int) orgAPIOrg {
	t.Helper()
	require.Equal(t, status, rec.Code, "body: %s", rec.Body.String())
	var out orgAPIOrg
	require.NoError(t, json.Unmarshal(orgAPIDecode(t, rec).Data, &out))
	require.NotEqual(t, uuid.Nil, out.ID)
	return out
}

func orgAPIRequireMember(t *testing.T, rec *httptest.ResponseRecorder, status int) orgAPIMember {
	t.Helper()
	require.Equal(t, status, rec.Code, "body: %s", rec.Body.String())
	var out orgAPIMember
	require.NoError(t, json.Unmarshal(orgAPIDecode(t, rec).Data, &out))
	return out
}

func orgAPIPath(o uuid.UUID, rest string) string {
	return "/api/v1/organizations/" + o.String() + rest
}

func orgAPIMemberBody(userID uuid.UUID) string {
	return `{"user_id":"` + userID.String() + `"}`
}

func TestOrganizationAPIPlatformCreatesOrganizationsWithAuditAndEvent(t *testing.T) {
	rig := newOrgAPIRig(t)
	platform := rig.platformUser()

	alpha := orgAPIRequireOrg(t, rig.do(platform, http.MethodPost, "/api/v1/organizations", `{"slug":"alpha-co","name":"Alpha Co"}`), http.StatusCreated)
	beta := orgAPIRequireOrg(t, rig.do(platform, http.MethodPost, "/api/v1/organizations", `{"slug":"beta-co","name":"Beta Co"}`), http.StatusCreated)

	require.Equal(t, "alpha-co", alpha.Slug)
	require.Equal(t, "active", alpha.Status)
	require.NotEqual(t, alpha.ID, beta.ID)
	require.Equal(t, 1, rig.count(`SELECT count(*) FROM organizations WHERE slug = 'alpha-co'`))
	require.Equal(t, 1, rig.audits("organization.create", alpha.ID))
	require.Equal(t, 1, rig.audits("organization.create", beta.ID))
	require.Equal(t, 1, rig.events("organization.created", alpha.ID))
	require.Equal(t, 1, rig.events("organization.created", beta.ID))

	// Slug yang sama saat organisasi lain sudah ada: 409 dan tidak ada yang tertulis.
	dup := rig.do(platform, http.MethodPost, "/api/v1/organizations", `{"slug":"alpha-co","name":"Alpha Lagi"}`)
	orgAPIRequireError(t, dup, http.StatusConflict, "organization_slug_taken")
	require.Equal(t, 1, rig.count(`SELECT count(*) FROM organizations WHERE slug = 'alpha-co'`))
	require.Equal(t, 2, rig.count(`SELECT count(*) FROM activities WHERE action = 'organization.create' AND result = 'success'`))
	require.Equal(t, 2, rig.count(`SELECT count(*) FROM outbox_events WHERE event_name = 'organization.created'`))

	got := orgAPIRequireOrg(t, rig.do(platform, http.MethodGet, orgAPIPath(alpha.ID, ""), ""), http.StatusOK)
	require.Equal(t, alpha.ID, got.ID)

	var list []orgAPIOrg
	listRec := rig.do(platform, http.MethodGet, "/api/v1/organizations", "")
	require.Equal(t, http.StatusOK, listRec.Code, "body: %s", listRec.Body.String())
	require.NoError(t, json.Unmarshal(orgAPIDecode(t, listRec).Data, &list))
	seen := map[uuid.UUID]bool{}
	for _, o := range list {
		seen[o.ID] = true
	}
	require.True(t, seen[alpha.ID] && seen[beta.ID], "platform melihat kedua organisasi")
}

func TestOrganizationAPITenantCannotCreateOrganizationsEvenWithTheAction(t *testing.T) {
	rig := newOrgAPIRig(t)
	org := testkit.NewOrganization(t, rig.db)
	tenant := rig.tenantUser(org, orgAPIAllCodes...)
	platform := rig.platformUser()

	rec := rig.do(tenant, http.MethodPost, "/api/v1/organizations", `{"slug":"gamma-co","name":"Gamma Co"}`)

	orgAPIRequireError(t, rec, http.StatusForbidden, "permission_denied")
	require.Equal(t, 0, rig.count(`SELECT count(*) FROM organizations WHERE slug = 'gamma-co'`))
	require.Equal(t, 0, rig.count(`SELECT count(*) FROM activities WHERE action = 'organization.create' AND result = 'success'`))
	require.Equal(t, 0, rig.count(`SELECT count(*) FROM outbox_events WHERE event_name = 'organization.created'`))

	// Kontrol: permintaan yang sama dari platform berhasil, jadi penolakan di atas memang karena scope.
	orgAPIRequireOrg(t, rig.do(platform, http.MethodPost, "/api/v1/organizations", `{"slug":"gamma-co","name":"Gamma Co"}`), http.StatusCreated)
}

func TestOrganizationAPIAnonymousRequestIsRejectedBeforeTheHandler(t *testing.T) {
	rig := newOrgAPIRig(t)

	rec := rig.do(orgAPIActor{}, http.MethodPost, "/api/v1/organizations", `{"slug":"anon-co","name":"Anon Co"}`)

	orgAPIRequireError(t, rec, http.StatusUnauthorized, "authentication_required")
	require.Equal(t, 0, rig.count(`SELECT count(*) FROM organizations WHERE slug = 'anon-co'`))
}

func TestOrganizationAPITenantIsolationBothDirections(t *testing.T) {
	rig := newOrgAPIRig(t)
	orgA := testkit.NewOrganization(t, rig.db)
	orgB := testkit.NewOrganization(t, rig.db)
	codes := []string{permission.OrganizationRead, permission.OrganizationUpdate, permission.OrganizationArchive, permission.OrganizationMemberManage}
	ua := rig.tenantUser(orgA, codes...)
	ub := rig.tenantUser(orgB, codes...)
	nameA, _ := rig.orgState(orgA.ID)
	nameB, _ := rig.orgState(orgB.ID)

	// attack menjalankan semua rute yang menunjuk victim dari actor; semuanya harus 404 seragam
	// (atau 403 untuk Add yang hanya boleh platform), dan tidak ada yang boleh tertulis.
	attack := func(actor orgAPIActor, victim testkit.Organization, victimUser orgAPIActor) {
		t.Helper()
		missing := rig.do(actor, http.MethodGet, orgAPIPath(id.NewID(), ""), "")
		missingErr := orgAPIRequireError(t, missing, http.StatusNotFound, "organization_not_found")

		foreign := rig.do(actor, http.MethodGet, orgAPIPath(victim.ID, ""), "")
		foreignErr := orgAPIRequireError(t, foreign, http.StatusNotFound, "organization_not_found")
		require.Equal(t, missingErr.Message, foreignErr.Message, "organisasi milik tenant lain tidak boleh dibedakan dari yang tidak ada")

		orgAPIRequireError(t, rig.do(actor, http.MethodPatch, orgAPIPath(victim.ID, ""), `{"name":"dibajak"}`), http.StatusNotFound, "organization_not_found")
		orgAPIRequireError(t, rig.do(actor, http.MethodDelete, orgAPIPath(victim.ID, ""), ""), http.StatusNotFound, "organization_not_found")
		orgAPIRequireError(t, rig.do(actor, http.MethodGet, orgAPIPath(victim.ID, "/members"), ""), http.StatusNotFound, "organization_not_found")
		orgAPIRequireError(t, rig.do(actor, http.MethodGet, orgAPIPath(victim.ID, "/members/"+victimUser.user.String()), ""), http.StatusNotFound, "member_not_found")
		orgAPIRequireError(t, rig.do(actor, http.MethodDelete, orgAPIPath(victim.ID, "/members/"+victimUser.user.String()), ""), http.StatusNotFound, "organization_not_found")
		orgAPIRequireError(t, rig.do(actor, http.MethodPost, orgAPIPath(victim.ID, "/members"), orgAPIMemberBody(actor.user)), http.StatusForbidden, "permission_denied")
	}

	attack(ua, orgB, ub)
	attack(ub, orgA, ua)

	afterA, statusA := rig.orgState(orgA.ID)
	afterB, statusB := rig.orgState(orgB.ID)
	require.Equal(t, nameA, afterA)
	require.Equal(t, nameB, afterB)
	require.Equal(t, "active", statusA)
	require.Equal(t, "active", statusB)
	require.Equal(t, "active", rig.memberStatus(orgA.ID, ua.user))
	require.Equal(t, "active", rig.memberStatus(orgB.ID, ub.user))
	require.Equal(t, 0, rig.count(`SELECT count(*) FROM activities WHERE result = 'success' AND action LIKE 'organization.%'`))
	require.Equal(t, 0, rig.count(`SELECT count(*) FROM outbox_events`))

	// Daftar tenant hanya memuat organisasinya sendiri, dan ubah milik sendiri berhasil.
	var list []orgAPIOrg
	listRec := rig.do(ua, http.MethodGet, "/api/v1/organizations", "")
	require.Equal(t, http.StatusOK, listRec.Code, "body: %s", listRec.Body.String())
	require.NoError(t, json.Unmarshal(orgAPIDecode(t, listRec).Data, &list))
	require.Len(t, list, 1)
	require.Equal(t, orgA.ID, list[0].ID)

	orgAPIRequireOrg(t, rig.do(ua, http.MethodPatch, orgAPIPath(orgA.ID, ""), `{"name":"A Baru"}`), http.StatusOK)
	renamed, _ := rig.orgState(orgA.ID)
	require.Equal(t, "A Baru", renamed)
	require.Equal(t, 1, rig.audits("organization.update", orgA.ID))
	require.Equal(t, 0, rig.audits("organization.update", orgB.ID))
}

func TestOrganizationAPIRoutesWithoutTheActionWriteNothing(t *testing.T) {
	rig := newOrgAPIRig(t)
	org := testkit.NewOrganization(t, rig.db)
	reader := rig.tenantUser(org, permission.OrganizationRead)
	before, _ := rig.orgState(org.ID)

	orgAPIRequireError(t, rig.do(reader, http.MethodPatch, orgAPIPath(org.ID, ""), `{"name":"tanpa hak"}`), http.StatusForbidden, "permission_denied")
	orgAPIRequireError(t, rig.do(reader, http.MethodDelete, orgAPIPath(org.ID, ""), ""), http.StatusForbidden, "permission_denied")
	orgAPIRequireError(t, rig.do(reader, http.MethodPost, orgAPIPath(org.ID, "/members"), orgAPIMemberBody(reader.user)), http.StatusForbidden, "permission_denied")
	orgAPIRequireError(t, rig.do(reader, http.MethodGet, orgAPIPath(org.ID, "/members"), ""), http.StatusForbidden, "permission_denied")

	after, status := rig.orgState(org.ID)
	require.Equal(t, before, after)
	require.Equal(t, "active", status)
	require.Equal(t, 0, rig.count(`SELECT count(*) FROM activities WHERE result = 'success' AND action LIKE 'organization.%'`))
	require.Equal(t, 0, rig.count(`SELECT count(*) FROM outbox_events`))

	// Kontrol: rute yang aksinya dimiliki tetap terbuka.
	orgAPIRequireOrg(t, rig.do(reader, http.MethodGet, orgAPIPath(org.ID, ""), ""), http.StatusOK)
}

func TestOrganizationAPIMemberAddAndRemoveChangeEffectiveAccess(t *testing.T) {
	rig := newOrgAPIRig(t)
	org := testkit.NewOrganization(t, rig.db)
	platform := rig.platformUser()
	admin := rig.tenantUser(org, permission.OrganizationMemberManage)
	oid := org.ID

	// target belum anggota tetapi sudah punya role baca pada organisasi; hak baru berlaku saat
	// keanggotaan aktif (Effective mensyaratkan anggota aktif).
	target := testkit.NewUser(t, rig.db)
	rig.grantInOrg(target.ID, org, permission.OrganizationRead)
	targetActor := orgAPIActor{user: target.ID, org: &oid}
	orgAPIRequireError(t, rig.do(targetActor, http.MethodGet, orgAPIPath(org.ID, ""), ""), http.StatusForbidden, "permission_denied")

	added := orgAPIRequireMember(t, rig.do(platform, http.MethodPost, orgAPIPath(org.ID, "/members"), orgAPIMemberBody(target.ID)), http.StatusCreated)
	require.Equal(t, target.ID, added.UserID)
	require.Equal(t, "active", added.Status)
	require.Equal(t, "active", rig.memberStatus(org.ID, target.ID))
	require.Equal(t, 1, rig.audits("organization.member.add", org.ID))
	require.Equal(t, 1, rig.events("organization.member.added", org.ID))
	orgAPIRequireOrg(t, rig.do(targetActor, http.MethodGet, orgAPIPath(org.ID, ""), ""), http.StatusOK)

	orgAPIRequireError(t, rig.do(platform, http.MethodPost, orgAPIPath(org.ID, "/members"), orgAPIMemberBody(target.ID)), http.StatusConflict, "member_exists")
	require.Equal(t, 1, rig.audits("organization.member.add", org.ID))

	// Tenant boleh menghapus anggota lain di organisasinya, tetapi tidak dirinya sendiri.
	orgAPIRequireError(t, rig.do(admin, http.MethodDelete, orgAPIPath(org.ID, "/members/"+admin.user.String()), ""), http.StatusConflict, "cannot_remove_self")
	require.Equal(t, "active", rig.memberStatus(org.ID, admin.user))

	removed := rig.do(admin, http.MethodDelete, orgAPIPath(org.ID, "/members/"+target.ID.String()), "")
	require.Equal(t, http.StatusNoContent, removed.Code, "body: %s", removed.Body.String())
	require.Equal(t, "removed", rig.memberStatus(org.ID, target.ID))
	require.Equal(t, 0, rig.count(`SELECT count(*) FROM role_assignments WHERE subject_id = $1 AND revoked_at IS NULL`, target.ID.String()))
	require.Equal(t, 1, rig.audits("organization.member.remove", org.ID))
	require.Equal(t, 1, rig.events("organization.member.removed", org.ID))
	orgAPIRequireError(t, rig.do(targetActor, http.MethodGet, orgAPIPath(org.ID, ""), ""), http.StatusForbidden, "permission_denied")

	// Menambahkan kembali mengaktifkan baris yang sama tetapi TIDAK menghidupkan hak lama.
	again := orgAPIRequireMember(t, rig.do(platform, http.MethodPost, orgAPIPath(org.ID, "/members"), orgAPIMemberBody(target.ID)), http.StatusCreated)
	require.Equal(t, "active", again.Status)
	require.Equal(t, 1, rig.count(`SELECT count(*) FROM organization_memberships WHERE organization_id = $1 AND user_id = $2`, org.ID.String(), target.ID.String()))
	orgAPIRequireError(t, rig.do(targetActor, http.MethodGet, orgAPIPath(org.ID, ""), ""), http.StatusForbidden, "permission_denied")
}

func TestOrganizationAPIArchiveClosesAccessAndFreezesTheOrganization(t *testing.T) {
	rig := newOrgAPIRig(t)
	orgA := testkit.NewOrganization(t, rig.db)
	orgB := testkit.NewOrganization(t, rig.db)
	platform := rig.platformUser()
	ua := rig.tenantUser(orgA, permission.OrganizationRead)
	ub := rig.tenantUser(orgB, permission.OrganizationRead)
	orgAPIRequireOrg(t, rig.do(ua, http.MethodGet, orgAPIPath(orgA.ID, ""), ""), http.StatusOK)

	rec := rig.do(platform, http.MethodDelete, orgAPIPath(orgA.ID, ""), "")

	require.Equal(t, http.StatusNoContent, rec.Code, "body: %s", rec.Body.String())
	_, statusA := rig.orgState(orgA.ID)
	_, statusB := rig.orgState(orgB.ID)
	require.Equal(t, "archived", statusA)
	require.Equal(t, "active", statusB, "organisasi lain tidak ikut berubah")
	require.Equal(t, 1, rig.audits("organization.archive", orgA.ID))
	require.Equal(t, 1, rig.events("organization.archived", orgA.ID))
	require.Equal(t, 0, rig.audits("organization.archive", orgB.ID))

	// Hak efektif anggota organisasi yang diarsipkan hilang pada request berikutnya; organisasi
	// lain tetap berjalan.
	orgAPIRequireError(t, rig.do(ua, http.MethodGet, orgAPIPath(orgA.ID, ""), ""), http.StatusForbidden, "permission_denied")
	orgAPIRequireOrg(t, rig.do(ub, http.MethodGet, orgAPIPath(orgB.ID, ""), ""), http.StatusOK)

	// Arsip bersifat akhir: ubah dan tambah anggota ditolak 409, tanpa tulisan baru.
	orgAPIRequireError(t, rig.do(platform, http.MethodPatch, orgAPIPath(orgA.ID, ""), `{"name":"setelah arsip"}`), http.StatusConflict, "organization_archived")
	orgAPIRequireError(t, rig.do(platform, http.MethodPost, orgAPIPath(orgA.ID, "/members"), orgAPIMemberBody(platform.user)), http.StatusConflict, "organization_archived")
	require.Equal(t, 0, rig.audits("organization.update", orgA.ID))
	require.Equal(t, 0, rig.audits("organization.member.add", orgA.ID))
}
