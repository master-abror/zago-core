package application_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/audit"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/cache"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes RoleWriter memakai PostgreSQL 18 sungguhan dan role app_user. Pemberi di fixture memegang
// rwHeld di level organisasi; rwOutside terdaftar di katalog tetapi TIDAK dipegang pemberi.

var _ application.RoleStore = (*repository.Roles)(nil)

var rwHeld = []string{"role.read", "role.manage", "invoice.read", "invoice.approve"}

const rwOutside = "invoice.delete"

type rwInvalidator struct {
	mu      sync.Mutex
	targets []cache.Targets
	fail    error
}

func (i *rwInvalidator) InvalidateInTx(_ context.Context, t cache.Targets) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.fail != nil {
		return i.fail
	}
	i.targets = append(i.targets, t)
	return nil
}

func (i *rwInvalidator) calls() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.targets)
}

func (i *rwInvalidator) last() cache.Targets {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.targets[len(i.targets)-1]
}

type rwEnv struct {
	db     *testpg.DB
	org    testkit.Organization
	admin  testkit.User
	grant  application.Grantor
	inv    *rwInvalidator
	writer *application.RoleWriter
}

func rwWriter(t *testing.T, db *testpg.DB, inv application.TxInvalidator) *application.RoleWriter {
	t.Helper()
	w, err := application.NewRoleWriter(kernel.NewTxManager(db.App, nil), repository.NewRoles(db.App),
		audit.New(db.App, nil), kernel.NewBus(db.App, nil), inv)
	require.NoError(t, err)
	return w
}

func newRWEnv(t *testing.T) rwEnv {
	t.Helper()
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	admin := testkit.NewUser(t, db, testkit.InOrganization(org))
	held := testkit.RoleWith(t, db, rwHeld)
	oid := org.ID
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: held, SubjectType: "user", SubjectID: admin.ID,
		ScopeType: "organization", OrganizationID: &oid,
	})
	testkit.EnsurePermission(t, db, rwOutside)
	inv := &rwInvalidator{}
	scopeOrg := org.ID
	return rwEnv{
		db:     db,
		org:    org,
		admin:  admin,
		grant:  application.Grantor{UserID: admin.ID, Scope: domain.ScopeContext{OrganizationID: &scopeOrg}},
		inv:    inv,
		writer: rwWriter(t, db, inv),
	}
}

func rwInput(slug string, codes ...string) application.CreateRoleInput {
	return application.CreateRoleInput{Name: "Role " + slug, Slug: slug, Type: domain.RoleTypeCustom, Permissions: codes}
}

func (e rwEnv) mustCreate(t *testing.T, slug string, codes ...string) application.RoleResult {
	t.Helper()
	res, err := e.writer.CreateRole(ctx, e.grant, rwInput(slug, codes...))
	require.NoError(t, err)
	return res
}

func rwCodes(t *testing.T, db *testpg.DB, roleID uuid.UUID) []string {
	t.Helper()
	rows, err := db.App.Query(ctx, `
		SELECT p.code FROM role_permissions rp
		JOIN permissions p ON p.id = rp.permission_id
		WHERE rp.role_id = $1`, roleID)
	require.NoError(t, err)
	codes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	// Diurutkan di sisi Go: urutan ORDER BY mengikuti collation database, bukan urutan byte.
	sort.Strings(codes)
	return codes
}

func rwActivities(t *testing.T, db *testpg.DB, action, result string, org uuid.UUID) int {
	t.Helper()
	return count(t, db, `SELECT count(*) FROM activities WHERE action = $1 AND result = $2 AND organization_id = $3`,
		action, result, org)
}

func rwEvents(t *testing.T, db *testpg.DB, name string, org, aggregate uuid.UUID) int {
	t.Helper()
	return count(t, db, `SELECT count(*) FROM outbox_events WHERE event_name = $1 AND organization_id = $2 AND aggregate_id = $3`,
		name, org, aggregate)
}

func TestNewRoleWriterRequiresItsDependencies(t *testing.T) {
	_, err := application.NewRoleWriter(nil, nil, nil, nil, nil)
	require.Error(t, err)
}

func TestRoleWriterCreateRole(t *testing.T) {
	e := newRWEnv(t)
	in := rwInput("keuangan", "invoice.approve", "invoice.read", "invoice.read")
	in.Description = "mengelola kas"
	res, err := e.writer.CreateRole(ctx, e.grant, in)
	require.NoError(t, err)

	want := []string{"invoice.approve", "invoice.read"}
	require.Equal(t, want, res.Permissions)
	require.Equal(t, "active", res.Role.Status)
	require.Equal(t, want, rwCodes(t, e.db, res.Role.ID))
	require.Equal(t, "custom", text(t, e.db, `SELECT role_type FROM roles WHERE id = $1`, res.Role.ID))
	require.Equal(t, e.org.ID.String(), text(t, e.db, `SELECT organization_id::text FROM roles WHERE id = $1`, res.Role.ID))
	require.Equal(t, "mengelola kas", text(t, e.db, `SELECT description FROM roles WHERE id = $1`, res.Role.ID))
	require.Equal(t, 2, count(t, e.db, `SELECT count(*) FROM role_permissions WHERE role_id = $1 AND created_by = $2`,
		res.Role.ID, e.admin.ID))
	require.Equal(t, 1, rwActivities(t, e.db, "role.create", "success", e.org.ID))
	require.Equal(t, 1, rwEvents(t, e.db, "authz.role.created", e.org.ID, res.Role.ID))
	require.Equal(t, 0, e.inv.calls(), "role baru belum punya assignment: tidak ada cache yang basi")
}

func TestRoleWriterCreateRoleAboveCeilingIsDeniedAndAudited(t *testing.T) {
	e := newRWEnv(t)
	_, err := e.writer.CreateRole(ctx, e.grant, rwInput("naik", "invoice.read", rwOutside))
	require.ErrorIs(t, err, domain.ErrAuthorityCeiling)
	require.Equal(t, 0, count(t, e.db, `SELECT count(*) FROM roles WHERE slug = 'naik'`))
	require.Equal(t, 1, rwActivities(t, e.db, "role.create", "denied", e.org.ID))
	require.Equal(t, 0, rwActivities(t, e.db, "role.create", "success", e.org.ID))
}

func TestRoleWriterCreateRoleRejectsUnregisteredPermission(t *testing.T) {
	e := newRWEnv(t)
	_, err := e.writer.CreateRole(ctx, e.grant, rwInput("hantu", "invoice.read", "ghost.read"))
	require.ErrorIs(t, err, domain.ErrUnknownPermission)
	require.Equal(t, 0, count(t, e.db, `SELECT count(*) FROM roles WHERE slug = 'hantu'`))
}

func TestRoleWriterCreateRoleRejectsInvalidInput(t *testing.T) {
	e := newRWEnv(t)
	_, err := e.writer.CreateRole(ctx, e.grant, rwInput("member"))
	require.ErrorIs(t, err, domain.ErrReservedRoleSlug)
	_, err = e.writer.CreateRole(ctx, e.grant, rwInput("Slug Buruk"))
	require.ErrorIs(t, err, domain.ErrInvalidRoleSlug)
	_, err = e.writer.CreateRole(ctx, e.grant, rwInput("ok", "bukan-kode"))
	require.ErrorIs(t, err, domain.ErrInvalidGrant)
}

func TestRoleWriterRefusesPlatformScope(t *testing.T) {
	e := newRWEnv(t)
	platform := application.Grantor{UserID: e.admin.ID, Scope: domain.ScopeContext{}}
	_, err := e.writer.CreateRole(ctx, platform, rwInput("platform"))
	require.ErrorIs(t, err, domain.ErrTargetOutsideScope)
	_, err = e.writer.UpdateRole(ctx, platform, id.NewID(), "Nama", "")
	require.ErrorIs(t, err, domain.ErrTargetOutsideScope)
	_, err = e.writer.SetPermissions(ctx, platform, id.NewID(), nil)
	require.ErrorIs(t, err, domain.ErrTargetOutsideScope)
	require.ErrorIs(t, e.writer.ArchiveRole(ctx, platform, id.NewID()), domain.ErrTargetOutsideScope)
	require.Equal(t, 0, count(t, e.db, `SELECT count(*) FROM roles WHERE slug = 'platform'`))
}

func TestRoleWriterCreateRoleBoundaryRules(t *testing.T) {
	e := newRWEnv(t)
	other := testkit.NewOrganization(t, e.db)
	otherAdmin := testkit.NewUser(t, e.db, testkit.InOrganization(other))
	foreign := testkit.NewGroup(t, e.db, other, otherAdmin)
	mine := testkit.NewGroup(t, e.db, e.org, e.admin)
	missing := id.NewID()

	n := 0
	create := func(roleType string, group *uuid.UUID) (application.RoleResult, error) {
		n++
		in := rwInput(fmt.Sprintf("batas-%d", n))
		in.Type = roleType
		in.GroupID = group
		return e.writer.CreateRole(ctx, e.grant, in)
	}

	_, err := create(domain.RoleTypeSystem, nil)
	require.ErrorIs(t, err, domain.ErrRoleTypeForbidden)
	_, err = create(domain.RoleTypeGroup, nil)
	require.ErrorIs(t, err, domain.ErrInvalidRole)
	_, err = create(domain.RoleTypeOrganization, &mine.ID)
	require.ErrorIs(t, err, domain.ErrInvalidRole)
	_, err = create("aneh", nil)
	require.ErrorIs(t, err, domain.ErrInvalidRole)
	_, err = create(domain.RoleTypeGroup, &foreign.ID)
	require.ErrorIs(t, err, domain.ErrGroupNotFound, "grup organisasi lain tidak dibedakan dari grup yang tidak ada")
	_, err = create(domain.RoleTypeGroup, &missing)
	require.ErrorIs(t, err, domain.ErrGroupNotFound)

	res, err := create(domain.RoleTypeGroup, &mine.ID)
	require.NoError(t, err)
	require.NotNil(t, res.Role.GroupID)
	require.Equal(t, mine.ID, *res.Role.GroupID)
	_, err = create(domain.RoleTypeOrganization, nil)
	require.NoError(t, err)
	_, err = create(domain.RoleTypeCustom, nil)
	require.NoError(t, err)
}

func TestRoleWriterGroupAdminIsBoundedToTheirGroup(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	boss := testkit.NewUser(t, db, testkit.InOrganization(org))
	ga := testkit.NewUser(t, db, testkit.InOrganization(org))
	groupA := testkit.NewGroup(t, db, org, boss)
	groupB := testkit.NewGroup(t, db, org, boss)
	testkit.AddGroupMember(t, db, groupA, ga, "active")
	role := testkit.RoleWith(t, db, []string{"invoice.read"})
	testkit.EnsurePermission(t, db, "invoice.approve")
	oid, gid := org.ID, groupA.ID
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: ga.ID,
		ScopeType: "group", ScopeID: &gid, OrganizationID: &oid,
	})
	scope, err := domain.NewScope(&oid, &gid)
	require.NoError(t, err)
	g := application.Grantor{UserID: ga.ID, Scope: scope}
	w := rwWriter(t, db, nil)

	groupRole := func(slug string, group uuid.UUID, codes ...string) error {
		in := rwInput(slug, codes...)
		in.Type = domain.RoleTypeGroup
		in.GroupID = &group
		_, err := w.CreateRole(ctx, g, in)
		return err
	}

	require.NoError(t, groupRole("a-baca", groupA.ID, "invoice.read"))
	require.ErrorIs(t, groupRole("a-setuju", groupA.ID, "invoice.approve"), domain.ErrAuthorityCeiling,
		"tidak boleh memberi permission yang tidak dipegang")
	require.ErrorIs(t, groupRole("b-baca", groupB.ID, "invoice.read"), domain.ErrAuthorityCeiling,
		"permission level grup hanya berlaku di grup aktif")

	orgRole := rwInput("org-baca", "invoice.read")
	orgRole.Type = domain.RoleTypeOrganization
	_, err = w.CreateRole(ctx, g, orgRole)
	require.ErrorIs(t, err, domain.ErrAuthorityCeiling, "Group Admin tidak dapat membuat role level organisasi")
}

func TestRoleWriterDuplicateSlugIsRejectedAndRolledBack(t *testing.T) {
	e := newRWEnv(t)
	e.mustCreate(t, "kembar", "invoice.read")
	_, err := e.writer.CreateRole(ctx, e.grant, rwInput("kembar", "invoice.read"))
	require.ErrorIs(t, err, domain.ErrRoleSlugTaken)
	require.Equal(t, 1, count(t, e.db, `SELECT count(*) FROM roles WHERE slug = 'kembar'`))
	require.Equal(t, 1, rwActivities(t, e.db, "role.create", "success", e.org.ID), "percobaan kedua tidak meninggalkan audit sukses")
}

func TestRoleWriterConcurrentCreatesOfOneSlugYieldExactlyOneRole(t *testing.T) {
	e := newRWEnv(t)
	const workers = 8
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = e.writer.CreateRole(ctx, e.grant, rwInput("serentak", "invoice.read"))
		}(i)
	}
	wg.Wait()

	created, taken := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			created++
		case errors.Is(err, domain.ErrRoleSlugTaken):
			taken++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, created)
	require.Equal(t, workers-1, taken)
	require.Equal(t, 1, count(t, e.db, `SELECT count(*) FROM roles WHERE slug = 'serentak'`))
}

func TestRoleWriterUpdateRole(t *testing.T) {
	e := newRWEnv(t)
	created := e.mustCreate(t, "keuangan", "invoice.read")

	res, err := e.writer.UpdateRole(ctx, e.grant, created.Role.ID, "  Keuangan Baru  ", "deskripsi baru")
	require.NoError(t, err)
	require.Equal(t, "Keuangan Baru", res.Role.Name)
	require.Equal(t, "keuangan", res.Role.Slug, "slug tidak dapat diubah")
	require.Equal(t, "Keuangan Baru", text(t, e.db, `SELECT name FROM roles WHERE id = $1`, created.Role.ID))
	require.Equal(t, "deskripsi baru", text(t, e.db, `SELECT description FROM roles WHERE id = $1`, created.Role.ID))
	require.Equal(t, 1, rwActivities(t, e.db, "role.update", "success", e.org.ID))
	require.Equal(t, 1, rwEvents(t, e.db, "authz.role.updated", e.org.ID, created.Role.ID))

	_, err = e.writer.UpdateRole(ctx, e.grant, created.Role.ID, "Keuangan Baru", "deskripsi baru")
	require.NoError(t, err)
	require.Equal(t, 1, rwActivities(t, e.db, "role.update", "success", e.org.ID), "tanpa perubahan tidak ada audit baru")
	require.Equal(t, 0, e.inv.calls(), "mengganti nama tidak mengubah himpunan efektif")

	_, err = e.writer.UpdateRole(ctx, e.grant, created.Role.ID, "   ", "")
	require.ErrorIs(t, err, domain.ErrInvalidRoleName)
}

func TestRoleWriterCannotTouchARoleHoldingPermissionsAboveTheGrantor(t *testing.T) {
	e := newRWEnv(t)
	above := testkit.RoleWith(t, e.db, []string{"invoice.read", rwOutside}, testkit.RoleInOrganization(e.org))

	_, err := e.writer.UpdateRole(ctx, e.grant, above.ID, "Diubah", "")
	require.ErrorIs(t, err, domain.ErrAuthorityCeiling)
	_, err = e.writer.SetPermissions(ctx, e.grant, above.ID, []string{"invoice.read"})
	require.ErrorIs(t, err, domain.ErrAuthorityCeiling, "mencabut izin di atas pemberi pun ditolak")
	require.ErrorIs(t, e.writer.ArchiveRole(ctx, e.grant, above.ID), domain.ErrAuthorityCeiling)

	require.Equal(t, []string{rwOutside, "invoice.read"}, rwCodes(t, e.db, above.ID))
	require.Equal(t, "active", text(t, e.db, `SELECT status FROM roles WHERE id = $1`, above.ID))
	require.Equal(t, "Role "+above.ID.String(), text(t, e.db, `SELECT name FROM roles WHERE id = $1`, above.ID))
	require.Equal(t, 0, e.inv.calls())
	require.Equal(t, 1, rwActivities(t, e.db, "role.update", "denied", e.org.ID))
	require.Equal(t, 1, rwActivities(t, e.db, "role.permissions.update", "denied", e.org.ID))
	require.Equal(t, 1, rwActivities(t, e.db, "role.archive", "denied", e.org.ID))
}

func TestRoleWriterSetPermissionsReplacesTheSetAndInvalidates(t *testing.T) {
	e := newRWEnv(t)
	created := e.mustCreate(t, "keuangan", "invoice.read", "invoice.approve")

	res, err := e.writer.SetPermissions(ctx, e.grant, created.Role.ID, []string{"role.read", "invoice.approve"})
	require.NoError(t, err)
	want := []string{"invoice.approve", "role.read"}
	require.Equal(t, want, res.Permissions)
	require.Equal(t, want, rwCodes(t, e.db, created.Role.ID))
	require.Equal(t, 1, e.inv.calls())
	require.Equal(t, []uuid.UUID{e.org.ID}, e.inv.last().Organizations)
	require.False(t, e.inv.last().Platform)
	require.Equal(t, 1, rwActivities(t, e.db, "role.permissions.update", "success", e.org.ID))
	require.Equal(t, 1, rwEvents(t, e.db, "authz.role.permissions_changed", e.org.ID, created.Role.ID))

	_, err = e.writer.SetPermissions(ctx, e.grant, created.Role.ID, []string{"invoice.approve", "role.read"})
	require.NoError(t, err)
	require.Equal(t, 1, e.inv.calls(), "himpunan yang sama tidak menginvalidasi")
	require.Equal(t, 1, rwActivities(t, e.db, "role.permissions.update", "success", e.org.ID), "himpunan yang sama tidak diaudit")

	_, err = e.writer.SetPermissions(ctx, e.grant, created.Role.ID, nil)
	require.NoError(t, err)
	require.Empty(t, rwCodes(t, e.db, created.Role.ID), "himpunan kosong sah")
	require.Equal(t, 2, e.inv.calls())
}

func TestRoleWriterSetPermissionsIsAtomicOnBadInput(t *testing.T) {
	e := newRWEnv(t)
	created := e.mustCreate(t, "keuangan", "invoice.read")

	_, err := e.writer.SetPermissions(ctx, e.grant, created.Role.ID, []string{"invoice.approve", "ghost.read"})
	require.ErrorIs(t, err, domain.ErrUnknownPermission)
	_, err = e.writer.SetPermissions(ctx, e.grant, created.Role.ID, []string{"invoice.approve", rwOutside})
	require.ErrorIs(t, err, domain.ErrAuthorityCeiling)

	require.Equal(t, []string{"invoice.read"}, rwCodes(t, e.db, created.Role.ID))
	require.Equal(t, 0, e.inv.calls())
}

func TestRoleWriterInvalidationFailureRollsBackTheWrite(t *testing.T) {
	e := newRWEnv(t)
	created := e.mustCreate(t, "keuangan", "invoice.read")
	e.inv.fail = errors.New("redis mati")

	_, err := e.writer.SetPermissions(ctx, e.grant, created.Role.ID, []string{"invoice.approve"})
	require.ErrorContains(t, err, "redis mati")
	require.Equal(t, []string{"invoice.read"}, rwCodes(t, e.db, created.Role.ID), "gagal-tertutup: perubahan dibatalkan")
	require.Equal(t, 0, rwActivities(t, e.db, "role.permissions.update", "success", e.org.ID))

	require.ErrorContains(t, e.writer.ArchiveRole(ctx, e.grant, created.Role.ID), "redis mati")
	require.Equal(t, "active", text(t, e.db, `SELECT status FROM roles WHERE id = $1`, created.Role.ID))
}

func TestRoleWriterNeverChangesSystemRoles(t *testing.T) {
	e := newRWEnv(t)
	sys := testkit.NewRole(t, e.db)

	_, err := e.writer.UpdateRole(ctx, e.grant, sys.ID, "Dibajak", "")
	require.ErrorIs(t, err, domain.ErrSystemRoleImmutable)
	_, err = e.writer.SetPermissions(ctx, e.grant, sys.ID, []string{"invoice.read"})
	require.ErrorIs(t, err, domain.ErrSystemRoleImmutable)
	require.ErrorIs(t, e.writer.ArchiveRole(ctx, e.grant, sys.ID), domain.ErrSystemRoleImmutable)

	require.Equal(t, "active", text(t, e.db, `SELECT status FROM roles WHERE id = $1`, sys.ID))
	require.Empty(t, rwCodes(t, e.db, sys.ID))
	require.Equal(t, "Role "+sys.ID.String(), text(t, e.db, `SELECT name FROM roles WHERE id = $1`, sys.ID))
	require.Equal(t, 1, rwActivities(t, e.db, "role.archive", "denied", e.org.ID))
}

func TestRoleWriterIsTenantScoped(t *testing.T) {
	e := newRWEnv(t)
	other := testkit.NewOrganization(t, e.db)
	foreign := testkit.RoleWith(t, e.db, []string{"invoice.read"}, testkit.RoleInOrganization(other))

	_, err := e.writer.UpdateRole(ctx, e.grant, foreign.ID, "Diambil alih", "")
	require.ErrorIs(t, err, domain.ErrRoleNotFound)
	_, err = e.writer.SetPermissions(ctx, e.grant, foreign.ID, []string{"invoice.read", "invoice.approve"})
	require.ErrorIs(t, err, domain.ErrRoleNotFound)
	require.ErrorIs(t, e.writer.ArchiveRole(ctx, e.grant, foreign.ID), domain.ErrRoleNotFound)
	_, err = e.writer.UpdateRole(ctx, e.grant, id.NewID(), "Hantu", "")
	require.ErrorIs(t, err, domain.ErrRoleNotFound, "role yang tidak ada tidak dibedakan dari role organisasi lain")

	require.Equal(t, []string{"invoice.read"}, rwCodes(t, e.db, foreign.ID))
	require.Equal(t, "active", text(t, e.db, `SELECT status FROM roles WHERE id = $1`, foreign.ID))
	require.Equal(t, 0, e.inv.calls())
}

func TestRoleWriterArchiveRole(t *testing.T) {
	e := newRWEnv(t)
	created := e.mustCreate(t, "keuangan", "invoice.read")

	require.NoError(t, e.writer.ArchiveRole(ctx, e.grant, created.Role.ID))
	require.Equal(t, "archived", text(t, e.db, `SELECT status FROM roles WHERE id = $1`, created.Role.ID))
	require.Equal(t, 1, e.inv.calls())
	require.Equal(t, []uuid.UUID{e.org.ID}, e.inv.last().Organizations)
	require.Equal(t, 1, rwActivities(t, e.db, "role.archive", "success", e.org.ID))
	require.Equal(t, 1, rwEvents(t, e.db, "authz.role.archived", e.org.ID, created.Role.ID))

	require.ErrorIs(t, e.writer.ArchiveRole(ctx, e.grant, created.Role.ID), domain.ErrRoleArchived)
	_, err := e.writer.UpdateRole(ctx, e.grant, created.Role.ID, "Baru", "")
	require.ErrorIs(t, err, domain.ErrRoleArchived)
	_, err = e.writer.SetPermissions(ctx, e.grant, created.Role.ID, []string{"invoice.approve"})
	require.ErrorIs(t, err, domain.ErrRoleArchived)
	require.Equal(t, 1, e.inv.calls())
}

func TestRoleStoreRefusesToRunOutsideATransaction(t *testing.T) {
	e := newRWEnv(t)
	store := repository.NewRoles(e.db.App)
	require.ErrorIs(t, store.LockOrganization(ctx, e.org.ID), repository.ErrNoTransaction)
	require.ErrorIs(t, repository.LockPlatform(ctx), repository.ErrNoTransaction)
	_, err := store.GrantorEffective(ctx, e.admin.ID, e.grant.Scope)
	require.ErrorIs(t, err, repository.ErrNoTransaction, "himpunan pemberi tidak pernah dihitung di luar kunci tulis")
}
