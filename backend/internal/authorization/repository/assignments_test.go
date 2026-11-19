package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes memakai PostgreSQL 18 sungguhan dan role app_user. Baris fixture ditulis lewat db.Migrator;
// yang diuji adalah kode repositori lewat db.App, di dalam transaksi kernel.TxManager untuk
// metode yang mensyaratkannya.

func asRepo(db *testpg.DB) *repository.Assignments { return repository.NewAssignments(db.App) }

// asTx menjalankan fn di dalam transaksi sungguhan, seperti AssignmentWriter.
func asTx(db *testpg.DB, fn func(ctx context.Context) error) error {
	return kernel.NewTxManager(db.App, nil).WithinTx(bg, fn)
}

func asRec(roleID, subject uuid.UUID, scope domain.GrantTarget) domain.AssignmentRecord {
	return domain.AssignmentRecord{ID: id.NewID(), RoleID: roleID, SubjectType: domain.SubjectUser, SubjectID: subject, Scope: scope}
}

func asInsert(db *testpg.DB, rec domain.AssignmentRecord) (domain.AssignmentRecord, error) {
	var out domain.AssignmentRecord
	err := asTx(db, func(ctx context.Context) error {
		var err error
		out, err = asRepo(db).Insert(ctx, rec)
		return err
	})
	return out, err
}

func asFlag(t *testing.T, db *testpg.DB, sql string, args ...any) bool {
	t.Helper()
	var ok bool
	require.NoError(t, db.Migrator.QueryRow(bg, sql, args...).Scan(&ok))
	return ok
}

func asExec(t *testing.T, db *testpg.DB, sql string, args ...any) {
	t.Helper()
	_, err := db.Migrator.Exec(bg, sql, args...)
	require.NoError(t, err)
}

// asSuperAdminRole memakai role sistem super-admin bila sudah ada di database tes, selain itu
// membuatnya.
func asSuperAdminRole(t *testing.T, db *testpg.DB) testkit.Role {
	t.Helper()
	var rid uuid.UUID
	err := db.Migrator.QueryRow(bg, `SELECT id FROM roles WHERE slug = $1 AND role_type = 'system'`,
		domain.RoleSlugSuperAdmin).Scan(&rid)
	if errors.Is(err, pgx.ErrNoRows) {
		rid = id.NewID()
		_, err = db.Migrator.Exec(bg,
			`INSERT INTO roles (id, name, slug, role_type, status) VALUES ($1, 'Super Admin', $2, 'system', 'active')`,
			rid.String(), domain.RoleSlugSuperAdmin)
	}
	require.NoError(t, err)
	return testkit.Role{ID: rid, Slug: domain.RoleSlugSuperAdmin}
}

func asPlatformAssignment(t *testing.T, db *testpg.DB, role testkit.Role, user uuid.UUID) testkit.Assignment {
	t.Helper()
	return testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: user, ScopeType: "platform"})
}

func asOrgAssignment(t *testing.T, db *testpg.DB, role testkit.Role, user, org uuid.UUID) testkit.Assignment {
	t.Helper()
	return testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: user, ScopeType: "organization", OrganizationID: &org})
}

func TestAssignmentsStoreRefusesToRunOutsideATransaction(t *testing.T) {
	db := testkit.NewDB(t)
	repo := asRepo(db)

	require.ErrorIs(t, repo.LockOrganization(bg, id.NewID()), repository.ErrNoTransaction)
	require.ErrorIs(t, repo.LockPlatform(bg), repository.ErrNoTransaction)
	_, err := repo.ActiveSuperAdmins(bg)
	require.ErrorIs(t, err, repository.ErrNoTransaction, "hitungan di luar kunci tidak melindungi apa pun")
	scope, err := domain.NewScope(ptr(id.NewID()), nil)
	require.NoError(t, err)
	_, err = repo.GrantorEffective(bg, id.NewID(), scope)
	require.ErrorIs(t, err, repository.ErrNoTransaction, "himpunan pemberi tidak pernah dihitung di luar kunci tulis")
}

func TestAssignmentsInsertWritesTheCanonicalColumnsPerScope(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	orgRole := testkit.NewRole(t, db, testkit.RoleInOrganization(w.org))
	expires := time.Now().Add(24 * time.Hour)

	orgRec := asRec(orgRole.ID, w.alice.ID, domain.OrganizationTarget(w.org.ID))
	orgRec.AssignedBy = ptr(w.alice.ID)
	stored, err := asInsert(db, orgRec)
	require.NoError(t, err)
	require.False(t, stored.CreatedAt.IsZero(), "created_at berasal dari database")
	require.Equal(t, orgRec.ID, stored.ID)
	require.True(t, asFlag(t, db,
		`SELECT scope_type = 'organization' AND scope_id IS NULL AND organization_id = $2 AND assigned_by = $3
		   FROM role_assignments WHERE id = $1`, orgRec.ID, w.org.ID, w.alice.ID), "bentuk kanonik: scope_id NULL")

	grpRec := asRec(orgRole.ID, w.alice.ID, domain.GroupTarget(w.org.ID, w.group.ID))
	grpRec.ExpiresAt = &expires
	_, err = asInsert(db, grpRec)
	require.NoError(t, err)
	require.True(t, asFlag(t, db,
		`SELECT scope_type = 'group' AND scope_id = $2 AND organization_id = $3 AND expires_at IS NOT NULL
		   FROM role_assignments WHERE id = $1`, grpRec.ID, w.group.ID, w.org.ID))

	sa := asSuperAdminRole(t, db)
	platRec := asRec(sa.ID, w.alice.ID, domain.PlatformTarget())
	_, err = asInsert(db, platRec)
	require.NoError(t, err)
	require.True(t, asFlag(t, db,
		`SELECT scope_type = 'platform' AND scope_id IS NULL AND organization_id IS NULL AND assigned_by IS NULL
		   FROM role_assignments WHERE id = $1`, platRec.ID))

	_, err = asInsert(db, asRec(orgRole.ID, w.alice.ID, domain.GrantTarget{}))
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget, "target tidak sah ditolak sebelum menyentuh database")
}

func TestAssignmentsInsertRejectsAnActiveDuplicateButAllowsItAfterRevocation(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.NewRole(t, db, testkit.RoleInOrganization(w.org))
	scope := domain.OrganizationTarget(w.org.ID)

	first := asRec(role.ID, w.alice.ID, scope)
	_, err := asInsert(db, first)
	require.NoError(t, err)

	_, err = asInsert(db, asRec(role.ID, w.alice.ID, scope))
	require.ErrorIs(t, err, domain.ErrAssignmentExists, "indeks unik dipetakan ke galat domain")

	require.NoError(t, asTx(db, func(ctx context.Context) error {
		return asRepo(db).Revoke(ctx, first.ID)
	}))
	_, err = asInsert(db, asRec(role.ID, w.alice.ID, scope))
	require.NoError(t, err, "setelah dicabut, assignment yang sama boleh diberikan lagi")
}

func TestAssignmentsActiveDuplicateTreatsBothOrganizationFormsAsOne(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	repo := asRepo(db)
	role := testkit.NewRole(t, db, testkit.RoleInOrganization(w.org))
	otherRole := testkit.NewRole(t, db, testkit.RoleInOrganization(w.org))
	bob := testkit.NewUser(t, db, testkit.InOrganization(w.org))
	carol := testkit.NewUser(t, db, testkit.InOrganization(w.org))
	dave := testkit.NewUser(t, db, testkit.InOrganization(w.org))
	erin := testkit.NewUser(t, db, testkit.InOrganization(w.org))
	orgID := w.org.ID
	scope := domain.OrganizationTarget(w.org.ID)

	asOrgAssignment(t, db, role, w.alice.ID, w.org.ID)
	testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: bob.ID, ScopeType: "organization", ScopeID: &orgID, OrganizationID: &orgID})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: dave.ID, ScopeType: "organization", OrganizationID: &orgID, Revoked: true})
	past := time.Now().Add(-time.Hour)
	testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: erin.ID, ScopeType: "organization", OrganizationID: &orgID, ExpiresAt: &past})

	dup := func(r testkit.Role, user uuid.UUID, target domain.GrantTarget) bool {
		ok, err := repo.ActiveDuplicate(bg, r.ID, domain.SubjectUser, user, target)
		require.NoError(t, err)
		return ok
	}
	require.True(t, dup(role, w.alice.ID, scope), "bentuk scope_id NULL")
	require.True(t, dup(role, bob.ID, scope), "bentuk scope_id = id organisasi adalah assignment yang sama")
	require.False(t, dup(role, carol.ID, scope), "subjek lain")
	require.False(t, dup(otherRole, w.alice.ID, scope), "role lain")
	require.False(t, dup(role, dave.ID, scope), "yang sudah dicabut tidak dihitung")
	require.True(t, dup(role, erin.ID, scope), "kedaluwarsa tetapi belum dicabut tetap dihitung, sama seperti indeks unik")

	otherOrg := testkit.NewOrganization(t, db)
	require.False(t, dup(role, w.alice.ID, domain.OrganizationTarget(otherOrg.ID)), "organisasi lain")

	g2 := testkit.NewGroup(t, db, w.org, w.alice)
	testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: carol.ID, ScopeType: "group", ScopeID: &w.group.ID, OrganizationID: &orgID})
	require.True(t, dup(role, carol.ID, domain.GroupTarget(w.org.ID, w.group.ID)))
	require.False(t, dup(role, carol.ID, domain.GroupTarget(w.org.ID, g2.ID)), "grup lain")
	require.False(t, dup(role, carol.ID, scope), "scope grup bukan scope organisasi")

	sa := asSuperAdminRole(t, db)
	asPlatformAssignment(t, db, sa, w.alice.ID)
	okPlatform, err := repo.ActiveDuplicate(bg, sa.ID, domain.SubjectUser, w.alice.ID, domain.PlatformTarget())
	require.NoError(t, err)
	require.True(t, okPlatform)
	okOther, err := repo.ActiveDuplicate(bg, sa.ID, domain.SubjectUser, bob.ID, domain.PlatformTarget())
	require.NoError(t, err)
	require.False(t, okOther)

	_, err = repo.ActiveDuplicate(bg, role.ID, domain.SubjectUser, w.alice.ID, domain.GrantTarget{})
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget)
}

func TestAssignmentsForUpdateFiltersByTenant(t *testing.T) {
	db := testkit.NewDB(t)
	a := newWorld(t, db)
	b := newWorld(t, db)
	roleA := testkit.NewRole(t, db, testkit.RoleInOrganization(a.org))
	sa := asSuperAdminRole(t, db)
	orgAsg := asOrgAssignment(t, db, roleA, a.alice.ID, a.org.ID)
	platAsg := asPlatformAssignment(t, db, sa, a.alice.ID)
	aOrg := a.org.ID
	explicit := testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: roleA, SubjectType: "group", SubjectID: a.group.ID, ScopeType: "organization", ScopeID: &aOrg, OrganizationID: &aOrg})
	resource := testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: roleA, SubjectType: "user", SubjectID: a.alice.ID, ScopeType: "resource", ScopeID: ptr(id.NewID()), OrganizationID: &aOrg})
	bOrg := b.org.ID

	require.NoError(t, asTx(db, func(ctx context.Context) error {
		repo := asRepo(db)

		rec, err := repo.ForUpdate(ctx, orgAsg.ID, &aOrg)
		require.NoError(t, err)
		require.Equal(t, roleA.ID, rec.RoleID)
		require.Equal(t, domain.SubjectUser, rec.SubjectType)
		require.Equal(t, a.alice.ID, rec.SubjectID)
		require.Equal(t, domain.OrganizationTarget(a.org.ID), rec.Scope)
		require.Nil(t, rec.RevokedAt)
		require.False(t, rec.CreatedAt.IsZero())

		_, err = repo.ForUpdate(ctx, orgAsg.ID, &bOrg)
		require.ErrorIs(t, err, domain.ErrAssignmentNotFound, "organisasi lain tidak melihat assignment ini")
		_, err = repo.ForUpdate(ctx, orgAsg.ID, nil)
		require.ErrorIs(t, err, domain.ErrAssignmentNotFound, "scope platform tidak melihat assignment organisasi")
		_, err = repo.ForUpdate(ctx, id.NewID(), &aOrg)
		require.ErrorIs(t, err, domain.ErrAssignmentNotFound, "tidak ada dan bukan milikmu tidak dibedakan")

		plat, err := repo.ForUpdate(ctx, platAsg.ID, nil)
		require.NoError(t, err)
		require.Equal(t, domain.PlatformTarget(), plat.Scope)
		_, err = repo.ForUpdate(ctx, platAsg.ID, &aOrg)
		require.ErrorIs(t, err, domain.ErrAssignmentNotFound, "scope organisasi tidak melihat assignment platform")

		same, err := repo.ForUpdate(ctx, explicit.ID, &aOrg)
		require.NoError(t, err)
		require.Equal(t, domain.SubjectGroup, same.SubjectType)
		require.Equal(t, domain.OrganizationTarget(a.org.ID), same.Scope, "bentuk scope_id = id organisasi dibaca sebagai scope organisasi yang sama")

		_, err = repo.ForUpdate(ctx, resource.ID, &aOrg)
		require.ErrorIs(t, err, domain.ErrInvalidGrantTarget, "scope yang tidak dipahami jalur tulis gagal-tertutup")
		require.NotErrorIs(t, err, domain.ErrAssignmentNotFound)
		return nil
	}))
}

func TestAssignmentsRevokeIsOneWay(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.NewRole(t, db, testkit.RoleInOrganization(w.org))
	asg := asOrgAssignment(t, db, role, w.alice.ID, w.org.ID)

	require.NoError(t, asTx(db, func(ctx context.Context) error {
		return asRepo(db).Revoke(ctx, asg.ID)
	}))
	require.True(t, asFlag(t, db, `SELECT revoked_at IS NOT NULL FROM role_assignments WHERE id = $1`, asg.ID))

	err := asTx(db, func(ctx context.Context) error {
		return asRepo(db).Revoke(ctx, asg.ID)
	})
	require.ErrorIs(t, err, domain.ErrAssignmentRevoked, "pencabutan kedua tidak mengubah apa pun")
}

func TestAssignmentsActiveSuperAdminsCountsOnlyUsableOnes(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	sa := asSuperAdminRole(t, db)
	notSuper := testkit.NewRole(t, db)
	orgID := w.org.ID
	past := time.Now().Add(-time.Hour)
	newUser := func() testkit.User { return testkit.NewUser(t, db) }
	count := func() int {
		var n int
		require.NoError(t, asTx(db, func(ctx context.Context) error {
			var err error
			n, err = asRepo(db).ActiveSuperAdmins(ctx)
			return err
		}))
		return n
	}
	require.Equal(t, 0, count(), "database baru tidak punya super-admin")

	counted := newUser()
	asPlatformAssignment(t, db, sa, counted.ID)
	require.Equal(t, 1, count())

	revoked := newUser()
	testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: sa, SubjectType: "user", SubjectID: revoked.ID, ScopeType: "platform", Revoked: true})
	expired := newUser()
	testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: sa, SubjectType: "user", SubjectID: expired.ID, ScopeType: "platform", ExpiresAt: &past})
	suspended := newUser()
	asPlatformAssignment(t, db, sa, suspended.ID)
	asExec(t, db, `UPDATE users SET status = 'suspended' WHERE id = $1`, suspended.ID)
	deleted := newUser()
	asPlatformAssignment(t, db, sa, deleted.ID)
	asExec(t, db, `UPDATE users SET deleted_at = now() WHERE id = $1`, deleted.ID)
	otherRole := newUser()
	asPlatformAssignment(t, db, notSuper, otherRole.ID)
	atOrg := newUser()
	asOrgAssignment(t, db, sa, atOrg.ID, orgID)
	require.Equal(t, 1, count(), "dicabut, kedaluwarsa, user tidak aktif/dihapus, role lain, dan scope organisasi tidak dihitung")

	second := newUser()
	asPlatformAssignment(t, db, sa, second.ID)
	require.Equal(t, 2, count())

	asExec(t, db, `UPDATE roles SET status = 'archived' WHERE id = $1`, sa.ID)
	require.Equal(t, 0, count(), "role super-admin yang diarsipkan tidak memberi hak, jadi tidak dihitung")
}

func TestAssignmentsRoleReadRespectsTheBoundary(t *testing.T) {
	db := testkit.NewDB(t)
	a := newWorld(t, db)
	b := newWorld(t, db)
	system := testkit.NewRole(t, db)
	roleA := testkit.NewRole(t, db, testkit.RoleInOrganization(a.org))
	repo := asRepo(db)
	aOrg, bOrg := a.org.ID, b.org.ID

	row, err := repo.Role(bg, system.ID, &aOrg)
	require.NoError(t, err)
	require.Equal(t, domain.RoleTypeSystem, row.Type)
	require.Equal(t, system.Slug, row.Slug)
	row, err = repo.Role(bg, system.ID, nil)
	require.NoError(t, err, "role sistem terbaca dari scope platform")
	require.Equal(t, system.ID, row.ID)

	row, err = repo.Role(bg, roleA.ID, &aOrg)
	require.NoError(t, err)
	require.Equal(t, roleA.ID, row.ID)
	require.NotNil(t, row.OrganizationID)
	require.Equal(t, aOrg, *row.OrganizationID)
	require.Equal(t, "active", row.Status)

	_, err = repo.Role(bg, roleA.ID, &bOrg)
	require.ErrorIs(t, err, domain.ErrRoleNotFound, "role organisasi lain")
	_, err = repo.Role(bg, roleA.ID, nil)
	require.ErrorIs(t, err, domain.ErrRoleNotFound, "scope platform hanya melihat role sistem")
	_, err = repo.Role(bg, id.NewID(), &aOrg)
	require.ErrorIs(t, err, domain.ErrRoleNotFound)
}

func TestAssignmentsSubjectChecks(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	other := newWorld(t, db)
	repo := asRepo(db)
	outsider := testkit.NewUser(t, db)
	suspended := testkit.NewUser(t, db, testkit.InOrganization(w.org))
	asExec(t, db, `UPDATE users SET status = 'suspended' WHERE id = $1`, suspended.ID)
	invited := testkit.NewUser(t, db, testkit.InOrganization(w.org))
	asExec(t, db, `UPDATE organization_memberships SET status = 'invited' WHERE user_id = $1`, invited.ID)

	userActive := func(u uuid.UUID) bool {
		ok, err := repo.UserActive(bg, u)
		require.NoError(t, err)
		return ok
	}
	require.True(t, userActive(w.alice.ID))
	require.True(t, userActive(outsider.ID), "user aktif tanpa organisasi sah untuk scope platform")
	require.False(t, userActive(suspended.ID))
	require.False(t, userActive(id.NewID()))

	member := func(org, u uuid.UUID) bool {
		ok, err := repo.OrganizationMember(bg, org, u)
		require.NoError(t, err)
		return ok
	}
	require.True(t, member(w.org.ID, w.alice.ID))
	require.False(t, member(w.org.ID, outsider.ID), "bukan anggota")
	require.False(t, member(other.org.ID, w.alice.ID), "anggota organisasi lain")
	require.False(t, member(w.org.ID, invited.ID), "keanggotaan belum aktif")
	require.False(t, member(w.org.ID, suspended.ID), "user tidak aktif")

	orgActive := func(org uuid.UUID) bool {
		ok, err := repo.OrganizationActive(bg, org)
		require.NoError(t, err)
		return ok
	}
	require.True(t, orgActive(w.org.ID))
	require.False(t, orgActive(id.NewID()))
	asExec(t, db, `UPDATE organizations SET status = 'suspended' WHERE id = $1`, w.org.ID)
	require.False(t, orgActive(w.org.ID))

	grpOK, err := repo.GroupActive(bg, w.org.ID, w.group.ID)
	require.NoError(t, err)
	require.True(t, grpOK)
	grpOther, err := repo.GroupActive(bg, other.org.ID, w.group.ID)
	require.NoError(t, err)
	require.False(t, grpOther, "grup milik organisasi lain")

	held := testkit.RoleWith(t, db, []string{"invoice.read", "invoice.approve"})
	codes, err := repo.PermissionCodes(bg, held.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"invoice.approve", "invoice.read"}, codes)
}
