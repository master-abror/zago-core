package repository_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes effective permissions (docs/06 §5.2, §14). Dibangun di atas `world` dari groups_test.go
// (satu organisasi, satu grup aktif, alice anggota aktif keduanya) dan dijalankan lewat app_user.

const (
	permRead   = "thing.read"
	permWrite  = "thing.write"
	permDelete = "thing.delete"
)

func ptr(u uuid.UUID) *uuid.UUID { return &u }

// load menghitung effective permissions user pada scope (org, group); nil = tanpa organisasi/grup.
func (w world) load(t *testing.T, user uuid.UUID, org, group *uuid.UUID) domain.EffectivePermissions {
	t.Helper()
	scope, err := domain.NewScope(org, group)
	require.NoError(t, err)
	e, err := repository.NewEffective(w.db.App).Load(bg, user, scope)
	require.NoError(t, err)
	return e
}

// loadOrg adalah load pada organisasi world tanpa grup aktif.
func (w world) loadOrg(t *testing.T, user uuid.UUID) domain.EffectivePermissions {
	t.Helper()
	return w.load(t, user, ptr(w.org.ID), nil)
}

// assignOrg memberi user role pada scope organisasi (scope_id NULL) di organisasi world.
func (w world) assignOrg(t *testing.T, role testkit.Role, user uuid.UUID) testkit.Assignment {
	t.Helper()
	return testkit.AssignRole(t, w.db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: user,
		ScopeType: "organization", OrganizationID: ptr(w.org.ID),
	})
}

func requireGrants(t *testing.T, e domain.EffectivePermissions, want map[string]domain.DecisionLevel) {
	t.Helper()
	require.Equal(t, want, e.Grants())
}

func TestRolePermissionViaUserAssignmentIsAllowedAtOrganizationLevel(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.RoleWith(t, db, []string{permRead, permWrite})
	w.assignOrg(t, role, w.alice.ID)

	requireGrants(t, w.loadOrg(t, w.alice.ID), map[string]domain.DecisionLevel{
		permRead: domain.LevelOrganization, permWrite: domain.LevelOrganization,
	})
}

func TestUserWithoutAssignmentGetsNothing(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	testkit.RoleWith(t, db, []string{permRead}) // role ada, tidak ditetapkan ke siapa pun

	require.Equal(t, 0, w.loadOrg(t, w.alice.ID).Len())
}

func TestUnknownUserGetsNothing(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.RoleWith(t, db, []string{permRead})
	w.assignOrg(t, role, w.alice.ID)

	require.Equal(t, 0, w.loadOrg(t, id.NewID()).Len())
}

func TestAssignmentDoesNotTransferBetweenUsers(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	bob := testkit.NewUser(t, db, testkit.InOrganization(w.org))
	role := testkit.RoleWith(t, db, []string{permRead})
	w.assignOrg(t, role, w.alice.ID)

	require.True(t, w.loadOrg(t, w.alice.ID).Has(permRead), "kontrol")
	require.Equal(t, 0, w.loadOrg(t, bob.ID).Len(), "bob tidak mewarisi assignment alice")
}

func TestRevokedAssignmentIsDenied(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.RoleWith(t, db, []string{permRead})
	a := w.assignOrg(t, role, w.alice.ID)
	require.True(t, w.loadOrg(t, w.alice.ID).Has(permRead), "kontrol sebelum dicabut")

	w.exec(t, `UPDATE role_assignments SET revoked_at = now() WHERE id = $1`, a.ID.String())

	require.Equal(t, 0, w.loadOrg(t, w.alice.ID).Len(), "pencabutan berlaku pada pembacaan berikutnya")
}

func TestExpiredAssignmentIsDeniedAndFutureExpiryIsAllowed(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.RoleWith(t, db, []string{permRead})
	past, future := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)

	expired := testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: w.alice.ID,
		ScopeType: "organization", OrganizationID: ptr(w.org.ID), ExpiresAt: &past,
	})
	require.Equal(t, 0, w.loadOrg(t, w.alice.ID).Len(), "assignment kedaluwarsa ditolak")

	w.exec(t, `UPDATE role_assignments SET expires_at = $1 WHERE id = $2`, future, expired.ID.String())
	require.True(t, w.loadOrg(t, w.alice.ID).Has(permRead), "assignment yang belum kedaluwarsa berlaku")
}

func TestArchivedRoleIsDenied(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.RoleWith(t, db, []string{permRead}, testkit.RoleStatus("archived"))
	w.assignOrg(t, role, w.alice.ID)

	require.Equal(t, 0, w.loadOrg(t, w.alice.ID).Len())
}

func TestWrongOrganizationIsDenied(t *testing.T) {
	db := testkit.NewDB(t)
	a := newWorld(t, db)
	orgB := testkit.NewOrganization(t, db)
	testkit.AddOrganizationMember(t, db, orgB, a.alice)
	role := testkit.RoleWith(t, db, []string{permRead})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{ // assignment HANYA di organisasi B
		Role: role, SubjectType: "user", SubjectID: a.alice.ID,
		ScopeType: "organization", OrganizationID: ptr(orgB.ID),
	})

	require.True(t, a.load(t, a.alice.ID, ptr(orgB.ID), nil).Has(permRead), "kontrol: berlaku di organisasi B")
	require.Equal(t, 0, a.loadOrg(t, a.alice.ID).Len(), "tidak berlaku di organisasi A walau alice anggota keduanya")
}

func TestOrganizationScopeIdPointingElsewhereDoesNotApply(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	orgB := testkit.NewOrganization(t, db)
	role := testkit.RoleWith(t, db, []string{permRead})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: w.alice.ID,
		ScopeType: "organization", ScopeID: ptr(orgB.ID), OrganizationID: ptr(w.org.ID),
	})

	require.Equal(t, 0, w.loadOrg(t, w.alice.ID).Len(), "scope_id organisasi lain tidak berlaku")

	w.exec(t, `UPDATE role_assignments SET scope_id = $1 WHERE subject_id = $2`, w.org.ID.String(), w.alice.ID.String())
	require.True(t, w.loadOrg(t, w.alice.ID).Has(permRead), "kontrol: scope_id = organisasi sendiri berlaku")
}

func TestRoleOfAnotherOrganizationAssignedHereIsIgnored(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	orgB := testkit.NewOrganization(t, db)
	foreign := testkit.RoleWith(t, db, []string{permRead}, testkit.RoleInOrganization(orgB))
	w.assignOrg(t, foreign, w.alice.ID)

	require.Equal(t, 0, w.loadOrg(t, w.alice.ID).Len(), "role milik organisasi B tidak boleh berlaku di organisasi A")

	own := testkit.RoleWith(t, db, []string{permWrite}, testkit.RoleInOrganization(w.org))
	w.assignOrg(t, own, w.alice.ID)
	requireGrants(t, w.loadOrg(t, w.alice.ID), map[string]domain.DecisionLevel{permWrite: domain.LevelOrganization})
}

func TestGroupScopedAssignmentAppliesOnlyToItsOwnActiveGroup(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	groupB := testkit.NewGroup(t, db, w.org, w.alice)
	testkit.AddGroupMember(t, db, groupB, w.alice, "active")
	role := testkit.RoleWith(t, db, []string{permRead})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: w.alice.ID,
		ScopeType: "group", ScopeID: ptr(w.group.ID), OrganizationID: ptr(w.org.ID),
	})

	requireGrants(t, w.load(t, w.alice.ID, ptr(w.org.ID), ptr(w.group.ID)),
		map[string]domain.DecisionLevel{permRead: domain.LevelGroup})
	require.Equal(t, 0, w.load(t, w.alice.ID, ptr(w.org.ID), ptr(groupB.ID)).Len(), "grup A tidak berlaku saat grup aktif = B")
	require.Equal(t, 0, w.load(t, w.alice.ID, ptr(w.org.ID), nil).Len(), "tanpa grup aktif assignment grup tidak berlaku")
}

func TestGroupScopedAssignmentIgnoredWhenItsGroupIsNoLongerActive(t *testing.T) {
	db := testkit.NewDB(t)
	for _, tc := range []struct{ name, stmt string }{
		{"diarsipkan", `UPDATE groups SET status = 'archived' WHERE id = $1`},
		{"dihapus lunak", `UPDATE groups SET deleted_at = now() WHERE id = $1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t, db)
			role := testkit.RoleWith(t, db, []string{permRead})
			testkit.AssignRole(t, db, testkit.AssignmentSpec{
				Role: role, SubjectType: "user", SubjectID: w.alice.ID,
				ScopeType: "group", ScopeID: ptr(w.group.ID), OrganizationID: ptr(w.org.ID),
			})
			require.True(t, w.load(t, w.alice.ID, ptr(w.org.ID), ptr(w.group.ID)).Has(permRead), "kontrol")

			w.exec(t, tc.stmt, w.group.ID.String())

			require.Equal(t, 0, w.load(t, w.alice.ID, ptr(w.org.ID), ptr(w.group.ID)).Len())
		})
	}
}

func TestGroupScopeIdFromAnotherOrganizationNeverApplies(t *testing.T) {
	db := testkit.NewDB(t)
	a := newWorld(t, db)
	orgB := testkit.NewOrganization(t, db)
	testkit.AddOrganizationMember(t, db, orgB, a.alice)
	groupB := testkit.NewGroup(t, db, orgB, a.alice)
	testkit.AddGroupMember(t, db, groupB, a.alice, "active")
	role := testkit.RoleWith(t, db, []string{permRead})
	// assignment di organisasi A, tetapi scope_id menunjuk grup organisasi B
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: a.alice.ID,
		ScopeType: "group", ScopeID: ptr(groupB.ID), OrganizationID: ptr(a.org.ID),
	})

	// scope yang dipalsukan (grup B dipasang pada organisasi A, seolah verifikasi dilewati)
	require.Equal(t, 0, a.load(t, a.alice.ID, ptr(a.org.ID), ptr(groupB.ID)).Len())
}

func TestPlatformAssignmentAppliesRegardlessOfActiveGroupAndOrganization(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.RoleWith(t, db, []string{permRead})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: w.alice.ID, ScopeType: "platform",
	})
	want := map[string]domain.DecisionLevel{permRead: domain.LevelPlatform}

	requireGrants(t, w.load(t, w.alice.ID, nil, nil), want)
	requireGrants(t, w.load(t, w.alice.ID, ptr(w.org.ID), nil), want)
	requireGrants(t, w.load(t, w.alice.ID, ptr(w.org.ID), ptr(w.group.ID)), want)
}

func TestPlatformIdentityWithoutMembershipGetsOnlyPlatformGrants(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	root := testkit.NewUser(t, db) // tanpa keanggotaan organisasi mana pun
	platformRole := testkit.RoleWith(t, db, []string{permRead})
	orgRole := testkit.RoleWith(t, db, []string{permWrite})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: platformRole, SubjectType: "user", SubjectID: root.ID, ScopeType: "platform",
	})
	w.assignOrg(t, orgRole, root.ID) // assignment organisasi tanpa keanggotaan: tidak berlaku

	want := map[string]domain.DecisionLevel{permRead: domain.LevelPlatform}
	requireGrants(t, w.load(t, root.ID, nil, nil), want)
	requireGrants(t, w.load(t, root.ID, ptr(w.org.ID), nil), want) // masuk lewat switch-organization
}

func TestOrganizationScopedAssignmentsIgnoredForPlatformScope(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.RoleWith(t, db, []string{permRead})
	w.assignOrg(t, role, w.alice.ID)

	require.Equal(t, 0, w.load(t, w.alice.ID, nil, nil).Len(), "scope platform tidak membawa izin organisasi")
}

func TestPlatformScopedAssignmentsOnlyCountForUserSubjects(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.RoleWith(t, db, []string{permRead})
	for _, subject := range []struct {
		typ string
		id  uuid.UUID
	}{
		{"group", w.group.ID},
		{"organization", w.org.ID},
		{"service", w.alice.ID},
	} {
		testkit.AssignRole(t, db, testkit.AssignmentSpec{
			Role: role, SubjectType: subject.typ, SubjectID: subject.id, ScopeType: "platform",
		})
	}

	require.Equal(t, 0, w.load(t, w.alice.ID, ptr(w.org.ID), ptr(w.group.ID)).Len(),
		"keanggotaan grup/organisasi tidak boleh menjadi jalur menuju otoritas platform")
	require.Equal(t, 0, w.load(t, w.alice.ID, nil, nil).Len())
}

func TestGroupSubjectAssignmentIsInheritedByActiveMembersOnly(t *testing.T) {
	db := testkit.NewDB(t)
	role := testkit.RoleWith(t, db, []string{permRead})

	for _, status := range []string{"active", "invited", "suspended", "removed"} {
		t.Run(status, func(t *testing.T) {
			w := newWorld(t, db)
			bob := testkit.NewUser(t, db, testkit.InOrganization(w.org))
			testkit.AddGroupMember(t, db, w.group, bob, status)
			testkit.AssignRole(t, db, testkit.AssignmentSpec{
				Role: role, SubjectType: "group", SubjectID: w.group.ID,
				ScopeType: "organization", OrganizationID: ptr(w.org.ID),
			})

			got := w.loadOrg(t, bob.ID)
			if status == "active" {
				requireGrants(t, got, map[string]domain.DecisionLevel{permRead: domain.LevelOrganization})
			} else {
				require.Equal(t, 0, got.Len(), "anggota %s tidak mewarisi assignment grup", status)
			}
		})
	}
}

func TestGroupSubjectAssignmentNotInheritedFromArchivedOrForeignGroup(t *testing.T) {
	db := testkit.NewDB(t)
	role := testkit.RoleWith(t, db, []string{permRead})

	t.Run("grup diarsipkan", func(t *testing.T) {
		w := newWorld(t, db)
		testkit.AssignRole(t, db, testkit.AssignmentSpec{
			Role: role, SubjectType: "group", SubjectID: w.group.ID,
			ScopeType: "organization", OrganizationID: ptr(w.org.ID),
		})
		require.True(t, w.loadOrg(t, w.alice.ID).Has(permRead), "kontrol")
		w.exec(t, `UPDATE groups SET status = 'archived' WHERE id = $1`, w.group.ID.String())

		require.Equal(t, 0, w.loadOrg(t, w.alice.ID).Len())
	})

	t.Run("bukan anggota grup", func(t *testing.T) {
		w := newWorld(t, db)
		bob := testkit.NewUser(t, db, testkit.InOrganization(w.org))
		testkit.AssignRole(t, db, testkit.AssignmentSpec{
			Role: role, SubjectType: "group", SubjectID: w.group.ID,
			ScopeType: "organization", OrganizationID: ptr(w.org.ID),
		})

		require.Equal(t, 0, w.loadOrg(t, bob.ID).Len())
	})

	t.Run("grup organisasi lain", func(t *testing.T) {
		a := newWorld(t, db)
		b := newWorld(t, db) // organisasi, grup, dan alice sendiri
		testkit.AssignRole(t, db, testkit.AssignmentSpec{
			Role: role, SubjectType: "group", SubjectID: b.group.ID,
			ScopeType: "organization", OrganizationID: ptr(a.org.ID),
		})

		require.Equal(t, 0, a.loadOrg(t, a.alice.ID).Len())
		require.Equal(t, 0, a.loadOrg(t, b.alice.ID).Len())
	})
}

func TestOrganizationSubjectAssignmentAppliesToActiveMembersOnly(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	bob := testkit.NewUser(t, db, testkit.InOrganization(w.org))
	outsider := testkit.NewUser(t, db) // bukan anggota
	role := testkit.RoleWith(t, db, []string{permRead})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "organization", SubjectID: w.org.ID,
		ScopeType: "organization", OrganizationID: ptr(w.org.ID),
	})

	want := map[string]domain.DecisionLevel{permRead: domain.LevelOrganization}
	requireGrants(t, w.loadOrg(t, w.alice.ID), want)
	requireGrants(t, w.loadOrg(t, bob.ID), want)
	require.Equal(t, 0, w.loadOrg(t, outsider.ID).Len(), "non-anggota tidak mendapat izin organisasi")

	w.exec(t, `UPDATE organization_memberships SET status = 'suspended' WHERE user_id = $1`, bob.ID.String())
	require.Equal(t, 0, w.loadOrg(t, bob.ID).Len(), "anggota ditangguhkan kehilangan izin organisasi")
}

func TestInactiveOrganizationMembershipLosesTenantGrantsButNotPlatformGrants(t *testing.T) {
	db := testkit.NewDB(t)
	for _, status := range []string{"invited", "suspended", "removed"} {
		t.Run(status, func(t *testing.T) {
			w := newWorld(t, db)
			orgRole := testkit.RoleWith(t, db, []string{permRead})
			platformRole := testkit.RoleWith(t, db, []string{permWrite})
			w.assignOrg(t, orgRole, w.alice.ID)
			testkit.AssignRole(t, db, testkit.AssignmentSpec{
				Role: platformRole, SubjectType: "user", SubjectID: w.alice.ID, ScopeType: "platform",
			})
			w.exec(t, `UPDATE organization_memberships SET status = $1 WHERE user_id = $2`, status, w.alice.ID.String())

			requireGrants(t, w.loadOrg(t, w.alice.ID), map[string]domain.DecisionLevel{permWrite: domain.LevelPlatform})
		})
	}
}

func TestNonActiveOrganizationHasNoTenantGrants(t *testing.T) {
	db := testkit.NewDB(t)
	for _, tc := range []struct{ name, stmt string }{
		{"suspended", `UPDATE organizations SET status = 'suspended' WHERE id = $1`},
		{"archived", `UPDATE organizations SET status = 'archived' WHERE id = $1`},
		{"dihapus lunak", `UPDATE organizations SET deleted_at = now() WHERE id = $1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t, db)
			role := testkit.RoleWith(t, db, []string{permRead})
			w.assignOrg(t, role, w.alice.ID)
			require.True(t, w.loadOrg(t, w.alice.ID).Has(permRead), "kontrol")

			w.exec(t, tc.stmt, w.org.ID.String())

			require.Equal(t, 0, w.loadOrg(t, w.alice.ID).Len())
		})
	}
}

func TestBroadestLevelWinsWhenSeveralAssignmentsGrantTheSameCode(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.RoleWith(t, db, []string{permRead})
	groupRole := testkit.RoleWith(t, db, []string{permRead, permWrite})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: groupRole, SubjectType: "user", SubjectID: w.alice.ID,
		ScopeType: "group", ScopeID: ptr(w.group.ID), OrganizationID: ptr(w.org.ID),
	})
	scope := func() domain.ScopeContext {
		s, err := domain.NewScope(ptr(w.org.ID), ptr(w.group.ID))
		require.NoError(t, err)
		return s
	}
	load := func() domain.EffectivePermissions {
		e, err := repository.NewEffective(w.db.App).Load(bg, w.alice.ID, scope())
		require.NoError(t, err)
		return e
	}

	requireGrants(t, load(), map[string]domain.DecisionLevel{permRead: domain.LevelGroup, permWrite: domain.LevelGroup})

	w.assignOrg(t, role, w.alice.ID)
	requireGrants(t, load(), map[string]domain.DecisionLevel{permRead: domain.LevelOrganization, permWrite: domain.LevelGroup})

	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: w.alice.ID, ScopeType: "platform",
	})
	requireGrants(t, load(), map[string]domain.DecisionLevel{permRead: domain.LevelPlatform, permWrite: domain.LevelGroup})
}

func TestResourceAndOwnScopedAssignmentsAreNotPartOfTheEffectiveSet(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.RoleWith(t, db, []string{permRead})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: w.alice.ID,
		ScopeType: "resource", ScopeID: ptr(id.NewID()), OrganizationID: ptr(w.org.ID),
	})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: w.alice.ID,
		ScopeType: "own", OrganizationID: ptr(w.org.ID),
	})

	require.Equal(t, 0, w.load(t, w.alice.ID, ptr(w.org.ID), ptr(w.group.ID)).Len(),
		"scope resource dan own dievaluasi saat pengecekan, bukan di himpunan yang di-cache")
}

func TestServiceSubjectAssignmentIsNeverAppliedToUsers(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.RoleWith(t, db, []string{permRead})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "service", SubjectID: w.alice.ID,
		ScopeType: "organization", OrganizationID: ptr(w.org.ID),
	})

	require.Equal(t, 0, w.loadOrg(t, w.alice.ID).Len())
}

func TestDuplicateGrantsFromManyRolesCollapse(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	r1 := testkit.RoleWith(t, db, []string{permRead, permWrite})
	r2 := testkit.RoleWith(t, db, []string{permWrite, permDelete})
	w.assignOrg(t, r1, w.alice.ID)
	w.assignOrg(t, r2, w.alice.ID)

	require.Equal(t, []string{permDelete, permRead, permWrite}, w.loadOrg(t, w.alice.ID).Codes())
}

func TestRoleWithoutPermissionsGrantsNothing(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	w.assignOrg(t, testkit.NewRole(t, db), w.alice.ID)

	require.Equal(t, 0, w.loadOrg(t, w.alice.ID).Len())
}

func TestRolePermissionChangeIsVisibleOnNextLoad(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.RoleWith(t, db, []string{permRead})
	w.assignOrg(t, role, w.alice.ID)
	require.False(t, w.loadOrg(t, w.alice.ID).Has(permWrite), "kontrol")

	testkit.GrantPermission(t, db, role, testkit.EnsurePermission(t, db, permWrite))
	require.True(t, w.loadOrg(t, w.alice.ID).Has(permWrite), "izin baru terlihat tanpa menunggu apa pun")

	w.exec(t, `DELETE FROM role_permissions WHERE role_id = $1`, role.ID.String())
	require.Equal(t, 0, w.loadOrg(t, w.alice.ID).Len(), "izin yang dicabut langsung hilang")
}

func TestInvalidScopeIsRejectedBeforeTouchingTheDatabase(t *testing.T) {
	w := newWorld(t, testkit.NewDB(t))
	cases := []struct {
		name  string
		scope domain.ScopeContext
	}{
		{"organisasi UUID nol", domain.ScopeContext{OrganizationID: ptr(uuid.Nil)}},
		{"grup tanpa organisasi", domain.ScopeContext{ActiveGroupID: ptr(w.group.ID)}},
		{"grup UUID nol", domain.ScopeContext{OrganizationID: ptr(w.org.ID), ActiveGroupID: ptr(uuid.Nil)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, err := repository.NewEffective(w.db.App).Load(bg, w.alice.ID, tc.scope)

			require.ErrorIs(t, err, domain.ErrInvalidScope)
			require.Equal(t, 0, e.Len())
		})
	}
}

func TestNilUserIsRejected(t *testing.T) {
	w := newWorld(t, testkit.NewDB(t))
	scope, err := domain.NewScope(ptr(w.org.ID), nil)
	require.NoError(t, err)

	e, err := repository.NewEffective(w.db.App).Load(bg, uuid.Nil, scope)

	require.Error(t, err)
	require.Equal(t, 0, e.Len())
}

func TestEffectiveDatabaseFailureIsAnErrorNotAnEmptySet(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	role := testkit.RoleWith(t, db, []string{permRead})
	w.assignOrg(t, role, w.alice.ID)
	canceled, cancel := context.WithCancel(bg)
	cancel()
	scope, err := domain.NewScope(ptr(w.org.ID), nil)
	require.NoError(t, err)

	e, err := repository.NewEffective(w.db.App).Load(canceled, w.alice.ID, scope)

	require.Error(t, err, "kegagalan infrastruktur tidak boleh terbaca sebagai 'tidak punya izin'")
	require.Equal(t, 0, e.Len())
}
