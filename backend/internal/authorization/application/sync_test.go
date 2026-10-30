package application_test

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/bootstrap"
	"github.com/master-abror/zago-core/backend/internal/dbmigrate"
	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/internal/role"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
	"github.com/master-abror/zago-core/backend/pkg/security"
)

// Tes memakai PostgreSQL 18 sungguhan dan role app_user (hak runtime, BUKAN migrator): semua
// Run di bawah memakai db.App, jadi sekaligus membuktikan sinkron tidak butuh hak DDL.

var ctx = context.Background()

func newSyncer(t *testing.T) *application.Syncer {
	t.Helper()
	s, err := application.NewDefaultSyncer()
	require.NoError(t, err)
	return s
}

func count(t *testing.T, db *testpg.DB, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.App.QueryRow(ctx, query, args...).Scan(&n))
	return n
}

func text(t *testing.T, db *testpg.DB, query string, args ...any) string {
	t.Helper()
	var s string
	require.NoError(t, db.App.QueryRow(ctx, query, args...).Scan(&s))
	return s
}

func exec(t *testing.T, db *testpg.DB, query string, args ...any) {
	t.Helper()
	_, err := db.App.Exec(ctx, query, args...)
	require.NoError(t, err)
}

func roleCodes(t *testing.T, db *testpg.DB, slug string) []string {
	t.Helper()
	rows, err := db.App.Query(ctx, `
		SELECT p.code FROM role_permissions rp
		JOIN roles r ON r.id = rp.role_id
		JOIN permissions p ON p.id = rp.permission_id
		WHERE r.slug = $1 AND r.organization_id IS NULL`, slug)
	require.NoError(t, err)
	codes, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	// Diurutkan di sisi Go: urutan ORDER BY mengikuti collation database, bukan urutan byte.
	sort.Strings(codes)
	return codes
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func catalogCodes() []string {
	var codes []string
	for _, d := range permission.Catalog() {
		codes = append(codes, d.Code)
	}
	sort.Strings(codes)
	return codes
}

func roleID(t *testing.T, db *testpg.DB, slug string) uuid.UUID {
	t.Helper()
	var rid uuid.UUID
	require.NoError(t, db.App.QueryRow(ctx,
		"SELECT id FROM roles WHERE slug = $1 AND organization_id IS NULL", slug).Scan(&rid))
	return rid
}

func permissionID(t *testing.T, db *testpg.DB, code string) uuid.UUID {
	t.Helper()
	var pid uuid.UUID
	require.NoError(t, db.App.QueryRow(ctx, "SELECT id FROM permissions WHERE code = $1", code).Scan(&pid))
	return pid
}

func attach(t *testing.T, db *testpg.DB, rid, pid uuid.UUID) {
	t.Helper()
	exec(t, db, "INSERT INTO role_permissions (id, role_id, permission_id) VALUES ($1, $2, $3)",
		id.NewID(), rid, pid)
}

// insertModulePermission membuat modul dan satu permission miliknya (module_id terisi, bukan sistem).
func insertModulePermission(t *testing.T, db *testpg.DB, moduleCode, resource, action string) uuid.UUID {
	t.Helper()
	moduleID := id.NewID()
	exec(t, db, "INSERT INTO modules (id, code, name, version, manifest) VALUES ($1, $2, $2, '1.0.0', '{}'::jsonb)",
		moduleID, moduleCode)
	pid := id.NewID()
	exec(t, db, `
		INSERT INTO permissions (id, module_id, resource, action, code, description, is_system)
		VALUES ($1, $2, $3, $4, $5, 'milik modul', false)`,
		pid, moduleID, resource, action, resource+"."+action)
	return pid
}

func seededDB(t *testing.T) *testpg.DB {
	t.Helper()
	db := testkit.NewDB(t)
	_, err := dbmigrate.Runner{}.Seed(ctx, db.MigratorURL())
	require.NoError(t, err)
	return db
}

func newBootstrapper(t *testing.T) *bootstrap.Bootstrapper {
	t.Helper()
	h, err := security.NewArgon2idHasher(security.Argon2Params{MemoryKiB: 64, Time: 1, Parallelism: 1}, 1)
	require.NoError(t, err)
	b, err := bootstrap.New(h, security.PasswordPolicy{MinLength: 12})
	require.NoError(t, err)
	return b
}

func TestSyncFreshDatabaseCreatesCatalogAndSystemRoles(t *testing.T) {
	db := testkit.NewDB(t)

	res, err := newSyncer(t).Run(ctx, db.App)
	require.NoError(t, err)

	catalog := permission.Catalog()
	require.True(t, res.Changed())
	require.Len(t, res.Permissions.Created, len(catalog))
	require.Empty(t, res.Permissions.Orphans)
	require.Equal(t, []string{"super-admin", "organization-admin", "group-admin", "member"}, res.Roles.RolesCreated)

	require.Equal(t, len(catalog), count(t, db, "SELECT count(*) FROM permissions WHERE is_system AND module_id IS NULL"))
	require.Equal(t, 4, count(t, db,
		"SELECT count(*) FROM roles WHERE role_type = 'system' AND organization_id IS NULL AND group_id IS NULL AND status = 'active'"))

	// Super Admin memegang SEMUA permission core; role lain tepat sebesar set default docs/06 §5.5.
	require.Equal(t, catalogCodes(), roleCodes(t, db, role.SlugSuperAdmin))
	for _, r := range role.SystemRoles() {
		if r.AllSystemPermissions {
			continue
		}
		require.Equal(t, sortedCopy(r.Permissions), roleCodes(t, db, r.Slug), r.Slug)
	}

	// Satu activity sistem pada transaksi yang sama, tanpa organisasi.
	require.Equal(t, 1, count(t, db, `
		SELECT count(*) FROM activities
		WHERE action = $1 AND actor_type = 'system' AND scope_type = 'platform'
		  AND result = 'success' AND organization_id IS NULL`, application.ActionCatalogSynced))
	require.Equal(t, 1, count(t, db, "SELECT count(*) FROM activities"))
}

func TestSyncIsIdempotentAndWritesNoAuditWhenNothingChanged(t *testing.T) {
	db := testkit.NewDB(t)
	s := newSyncer(t)
	_, err := s.Run(ctx, db.App)
	require.NoError(t, err)

	before := map[string]int{}
	tables := []string{"permissions", "roles", "role_permissions", "activities"}
	for _, table := range tables {
		before[table] = count(t, db, "SELECT count(*) FROM "+table)
	}

	for i := 0; i < 3; i++ {
		res, err := s.Run(ctx, db.App)
		require.NoError(t, err)
		require.False(t, res.Changed(), "run ke-%d tanpa perubahan tidak boleh melaporkan perubahan", i+2)
	}
	for _, table := range tables {
		require.Equal(t, before[table], count(t, db, "SELECT count(*) FROM "+table), table)
	}
}

func TestSyncAdoptsTheMinimalSuperAdminRoleFromBootstrapBySlug(t *testing.T) {
	db := seededDB(t)
	boot, err := newBootstrapper(t).Run(ctx, db.App, bootstrap.Params{
		Email: "admin@example.org", Password: "correct-horse-battery-staple",
	})
	require.NoError(t, err)
	require.True(t, boot.RoleCreated)

	// Keadaan sesudah bootstrap-admin: satu role, tanpa permission.
	before := roleID(t, db, role.SlugSuperAdmin)
	require.Equal(t, 1, count(t, db, "SELECT count(*) FROM roles"))
	require.Equal(t, 0, count(t, db, "SELECT count(*) FROM role_permissions"))

	res, err := newSyncer(t).Run(ctx, db.App)
	require.NoError(t, err)

	// Diadopsi (bukan digandakan: uq_roles_global_slug akan menolak baris kedua).
	require.Equal(t, 4, count(t, db, "SELECT count(*) FROM roles"))
	require.Equal(t, before, roleID(t, db, role.SlugSuperAdmin))
	require.Equal(t, []string{"super-admin"}, res.Roles.RolesUpdated)
	require.Equal(t, []string{"organization-admin", "group-admin", "member"}, res.Roles.RolesCreated)
	require.Equal(t, catalogCodes(), roleCodes(t, db, role.SlugSuperAdmin))
	require.Equal(t, "Super Admin", text(t, db, "SELECT name FROM roles WHERE id = $1", before))

	// Assignment platform milik admin pertama tidak tersentuh.
	require.Equal(t, 1, count(t, db, `
		SELECT count(*) FROM role_assignments
		WHERE role_id = $1 AND subject_id = $2 AND scope_type = 'platform' AND revoked_at IS NULL`,
		before, boot.UserID))
}

func TestBootstrapAfterSyncReusesTheSyncedSuperAdminRole(t *testing.T) {
	db := seededDB(t)
	_, err := newSyncer(t).Run(ctx, db.App)
	require.NoError(t, err)
	synced := roleID(t, db, role.SlugSuperAdmin)

	boot, err := newBootstrapper(t).Run(ctx, db.App, bootstrap.Params{
		Email: "admin@example.org", Password: "correct-horse-battery-staple",
	})
	require.NoError(t, err)

	require.False(t, boot.RoleCreated, "bootstrap tidak boleh membuat role kedua")
	require.True(t, boot.AssignmentCreated)
	require.Equal(t, 4, count(t, db, "SELECT count(*) FROM roles"))
	require.Equal(t, synced, roleID(t, db, role.SlugSuperAdmin))
	require.Equal(t, catalogCodes(), roleCodes(t, db, role.SlugSuperAdmin))
}

func TestSyncNeverDeletesPermissionsAndReportsOrphans(t *testing.T) {
	db := testkit.NewDB(t)
	s := newSyncer(t)
	_, err := s.Run(ctx, db.App)
	require.NoError(t, err)

	// Permission core yang sudah tidak ada di katalog (mis. dihapus dari kode).
	exec(t, db, `
		INSERT INTO permissions (id, resource, action, code, description, is_system)
		VALUES ($1, 'legacy', 'thing', 'legacy.thing', 'peninggalan', true)`, id.NewID())

	res, err := s.Run(ctx, db.App)
	require.NoError(t, err)

	require.Equal(t, []string{"legacy.thing"}, res.Permissions.Orphans)
	require.False(t, res.Permissions.Changed())
	require.Equal(t, 1, count(t, db, "SELECT count(*) FROM permissions WHERE code = 'legacy.thing'"),
		"sinkron tidak pernah menghapus permission")

	// Super Admin tetap memegang SEMUA permission core is_system, termasuk orphan.
	require.Equal(t, 1, res.Roles.PermissionsAdded)
	require.Contains(t, roleCodes(t, db, role.SlugSuperAdmin), "legacy.thing")
	require.Len(t, roleCodes(t, db, role.SlugSuperAdmin), len(permission.Catalog())+1)
	// Role template lain tidak ikut mendapat orphan.
	require.NotContains(t, roleCodes(t, db, role.SlugOrgAdmin), "legacy.thing")
}

func TestSyncRefusesACoreCodeOwnedByAModuleAndLeavesNothingBehind(t *testing.T) {
	db := testkit.NewDB(t)
	insertModulePermission(t, db, "billing", "role", "read") // modul mengklaim kode core role.read

	_, err := newSyncer(t).Run(ctx, db.App)
	require.ErrorIs(t, err, permission.ErrCodeConflict)

	require.Equal(t, 1, count(t, db, "SELECT count(*) FROM permissions"), "hanya baris milik modul")
	require.Equal(t, "false", text(t, db, "SELECT is_system::text FROM permissions WHERE code = 'role.read'"),
		"baris milik modul tidak diambil alih")
	require.Equal(t, 0, count(t, db, "SELECT count(*) FROM roles"))
	require.Equal(t, 0, count(t, db, "SELECT count(*) FROM role_permissions"))
	require.Equal(t, 0, count(t, db, "SELECT count(*) FROM activities"))
}

func TestSyncConvergesSystemRolesToDefaultsAndLeavesModulePermissionsAlone(t *testing.T) {
	db := testkit.NewDB(t)
	s := newSyncer(t)
	_, err := s.Run(ctx, db.App)
	require.NoError(t, err)

	member := roleID(t, db, role.SlugMember)
	attach(t, db, member, permissionID(t, db, "user.delete")) // kelebihan core: eskalasi
	exec(t, db, "DELETE FROM role_permissions WHERE role_id = $1 AND permission_id = $2",
		member, permissionID(t, db, "inbox.read")) // kekurangan
	attach(t, db, member, insertModulePermission(t, db, "billing", "billing.invoice", "read"))
	exec(t, db, "UPDATE roles SET name = 'X', description = 'Y', status = 'archived' WHERE slug = 'super-admin' AND organization_id IS NULL")

	res, err := s.Run(ctx, db.App)
	require.NoError(t, err)

	require.Equal(t, 1, res.Roles.PermissionsAdded)
	require.Equal(t, 1, res.Roles.PermissionsRemoved)
	require.Equal(t, []string{"super-admin"}, res.Roles.RolesUpdated)

	var def role.SystemRole
	for _, r := range role.SystemRoles() {
		if r.Slug == role.SlugMember {
			def = r
		}
	}
	want := sortedCopy(append(append([]string(nil), def.Permissions...), "billing.invoice.read"))
	require.Equal(t, want, roleCodes(t, db, role.SlugMember),
		"set core kembali ke default; permission modul tidak disentuh")
	require.NotContains(t, roleCodes(t, db, role.SlugMember), "user.delete")

	require.Equal(t, "Super Admin", text(t, db, "SELECT name FROM roles WHERE slug = 'super-admin' AND organization_id IS NULL"))
	require.Equal(t, "active", text(t, db, "SELECT status FROM roles WHERE slug = 'super-admin' AND organization_id IS NULL"))
}

func TestSyncRepairsDriftedPermissionRows(t *testing.T) {
	db := testkit.NewDB(t)
	s := newSyncer(t)
	_, err := s.Run(ctx, db.App)
	require.NoError(t, err)

	exec(t, db, "UPDATE permissions SET description = 'diubah', resource = 'salah', is_system = false WHERE code = 'role.read'")

	res, err := s.Run(ctx, db.App)
	require.NoError(t, err)

	require.Equal(t, []string{"role.read"}, res.Permissions.Updated)
	require.Empty(t, res.Permissions.Created)
	require.Equal(t, "role", text(t, db, "SELECT resource FROM permissions WHERE code = 'role.read'"))
	require.Equal(t, "true", text(t, db, "SELECT is_system::text FROM permissions WHERE code = 'role.read'"))
	require.NotEqual(t, "diubah", text(t, db, "SELECT description FROM permissions WHERE code = 'role.read'"))
	require.Equal(t, catalogCodes(), roleCodes(t, db, role.SlugSuperAdmin))
}

func TestSyncRollsBackEverythingWhenTheAuditCannotBeWritten(t *testing.T) {
	db := testkit.NewDB(t)
	_, err := db.Migrator.Exec(ctx,
		`CREATE FUNCTION fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'audit sengaja gagal'; END $$`)
	require.NoError(t, err)
	_, err = db.Migrator.Exec(ctx,
		"CREATE TRIGGER trg_fail_audit BEFORE INSERT ON activities FOR EACH ROW EXECUTE FUNCTION fail_audit()")
	require.NoError(t, err)

	_, err = newSyncer(t).Run(ctx, db.App)
	require.Error(t, err)
	require.Contains(t, err.Error(), "audit sengaja gagal")

	for _, table := range []string{"permissions", "roles", "role_permissions", "activities"} {
		require.Equal(t, 0, count(t, db, "SELECT count(*) FROM "+table), "%s harus kosong setelah rollback", table)
	}
}

func TestSyncConcurrentRunsProduceOneCatalog(t *testing.T) {
	db := testkit.NewDB(t)
	s := newSyncer(t)

	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)
	results := make([]application.SyncResult, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = s.Run(ctx, db.App)
		}()
	}
	wg.Wait()

	changed := 0
	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
		if results[i].Changed() {
			changed++
		}
	}
	require.Equal(t, 1, changed, "tepat satu run yang mengubah; sisanya menunggu kunci lalu tidak mengubah apa pun")
	require.Equal(t, len(permission.Catalog()), count(t, db, "SELECT count(*) FROM permissions"))
	require.Equal(t, 4, count(t, db, "SELECT count(*) FROM roles"))
	require.Equal(t, 1, count(t, db, "SELECT count(*) FROM activities"))
}

func TestNewSyncerRejectsInconsistentDefinitionsBeforeTouchingTheDatabase(t *testing.T) {
	good := permission.Catalog()
	okRole := role.SystemRole{Slug: "x", Name: "X", Permissions: []string{"role.read"}}

	_, err := application.NewSyncer(good, []role.SystemRole{
		{Slug: "x", Name: "X", Permissions: []string{"nope.code"}},
	})
	require.ErrorContains(t, err, "tidak ada di katalog")

	_, err = application.NewSyncer(good, []role.SystemRole{
		{Slug: "x", Name: "X", Permissions: []string{"role.read", "role.read"}},
	})
	require.ErrorContains(t, err, "dua kali")

	_, err = application.NewSyncer(good, []role.SystemRole{okRole, okRole})
	require.ErrorContains(t, err, "slug role sistem ganda")

	_, err = application.NewSyncer(good, []role.SystemRole{{Slug: "", Name: "X"}})
	require.Error(t, err)

	_, err = application.NewSyncer([]permission.Def{{Code: "BAD CODE", Description: "x"}}, nil)
	require.ErrorIs(t, err, permission.ErrInvalidCode)

	_, err = application.NewSyncer([]permission.Def{{Code: "a.b"}, {Code: "a.b"}}, nil)
	require.ErrorContains(t, err, "kode ganda")

	_, err = application.NewSyncer(good, role.SystemRoles())
	require.NoError(t, err)
}
