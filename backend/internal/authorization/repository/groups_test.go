package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes memakai PostgreSQL 18 sungguhan dan role app_user (hak runtime): sekaligus membuktikan
// kueri verifikasi grup tidak butuh hak di luar yang dimiliki runtime. Baris fixture ditulis lewat
// db.Migrator; yang diuji adalah kode repositori lewat db.App.

var bg = context.Background()

// world adalah satu organisasi dengan satu grup aktif dan satu anggota aktif (alice).
type world struct {
	db    *testpg.DB
	org   testkit.Organization
	alice testkit.User
	group testkit.Group
}

func newWorld(t *testing.T, db *testpg.DB) world {
	t.Helper()
	org := testkit.NewOrganization(t, db)
	alice := testkit.NewUser(t, db, testkit.InOrganization(org))
	group := testkit.NewGroup(t, db, org, alice)
	testkit.AddGroupMember(t, db, group, alice, "active")
	return world{db: db, org: org, alice: alice, group: group}
}

func (w world) check(t *testing.T, user, org, group uuid.UUID) bool {
	t.Helper()
	ok, err := repository.NewGroups(w.db.App).IsActiveGroupMember(bg, user, org, group)
	require.NoError(t, err)
	return ok
}

func (w world) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	_, err := w.db.Migrator.Exec(bg, sql, args...)
	require.NoError(t, err)
}

func TestActiveMemberOfActiveGroupIsVerified(t *testing.T) {
	w := newWorld(t, testkit.NewDB(t))

	require.True(t, w.check(t, w.alice.ID, w.org.ID, w.group.ID))
}

func TestNonActiveGroupMembershipStatusesAreRejected(t *testing.T) {
	db := testkit.NewDB(t)
	for _, status := range []string{"invited", "suspended", "removed"} {
		t.Run(status, func(t *testing.T) {
			w := newWorld(t, db)
			w.exec(t, `UPDATE group_memberships SET status = $1 WHERE group_id = $2 AND user_id = $3`,
				status, w.group.ID.String(), w.alice.ID.String())

			require.False(t, w.check(t, w.alice.ID, w.org.ID, w.group.ID))
		})
	}
}

func TestArchivedGroupIsRejected(t *testing.T) {
	w := newWorld(t, testkit.NewDB(t))
	w.exec(t, `UPDATE groups SET status = 'archived' WHERE id = $1`, w.group.ID.String())

	require.False(t, w.check(t, w.alice.ID, w.org.ID, w.group.ID))
}

func TestSoftDeletedGroupIsRejected(t *testing.T) {
	w := newWorld(t, testkit.NewDB(t))
	w.exec(t, `UPDATE groups SET deleted_at = now() WHERE id = $1`, w.group.ID.String())

	require.False(t, w.check(t, w.alice.ID, w.org.ID, w.group.ID))
}

func TestInactiveOrganizationMembershipInvalidatesGroupMembership(t *testing.T) {
	db := testkit.NewDB(t)
	for _, status := range []string{"suspended", "removed"} {
		t.Run(status, func(t *testing.T) {
			w := newWorld(t, db)
			w.exec(t, `UPDATE organization_memberships SET status = $1 WHERE organization_id = $2 AND user_id = $3`,
				status, w.org.ID.String(), w.alice.ID.String())

			require.False(t, w.check(t, w.alice.ID, w.org.ID, w.group.ID),
				"keanggotaan grup tidak berlaku bila keanggotaan organisasinya tidak aktif")
		})
	}
}

func TestGroupOfAnotherOrganizationIsRejectedEvenWhenUserIsMemberThere(t *testing.T) {
	db := testkit.NewDB(t)
	a := newWorld(t, db)
	orgB := testkit.NewOrganization(t, db)
	testkit.AddOrganizationMember(t, db, orgB, a.alice)
	groupB := testkit.NewGroup(t, db, orgB, a.alice)
	testkit.AddGroupMember(t, db, groupB, a.alice, "active")

	require.True(t, a.check(t, a.alice.ID, orgB.ID, groupB.ID), "kontrol: di organisasi B grup B sah")
	require.False(t, a.check(t, a.alice.ID, a.org.ID, groupB.ID),
		"sesi organisasi A tidak boleh memakai grup organisasi B")
	require.False(t, a.check(t, a.alice.ID, orgB.ID, a.group.ID),
		"sesi organisasi B tidak boleh memakai grup organisasi A")
}

func TestUserWhoIsNotInTheGroupIsRejected(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	bob := testkit.NewUser(t, db, testkit.InOrganization(w.org))

	require.False(t, w.check(t, bob.ID, w.org.ID, w.group.ID), "bob anggota organisasi tapi bukan anggota grup")
}

func TestMembershipDoesNotTransferBetweenUsers(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	bob := testkit.NewUser(t, db, testkit.InOrganization(w.org))
	otherGroup := testkit.NewGroup(t, db, w.org, w.alice)
	testkit.AddGroupMember(t, db, otherGroup, bob, "active")

	require.True(t, w.check(t, bob.ID, w.org.ID, otherGroup.ID), "kontrol")
	require.False(t, w.check(t, bob.ID, w.org.ID, w.group.ID), "bob tidak mewarisi grup alice")
	require.False(t, w.check(t, w.alice.ID, w.org.ID, otherGroup.ID), "alice tidak mewarisi grup bob")
}

func TestUnknownIdentifiersAreRejected(t *testing.T) {
	w := newWorld(t, testkit.NewDB(t))

	require.False(t, w.check(t, w.alice.ID, w.org.ID, id.NewID()))
	require.False(t, w.check(t, id.NewID(), w.org.ID, w.group.ID))
	require.False(t, w.check(t, w.alice.ID, id.NewID(), w.group.ID))
}

func TestDatabaseFailureIsAnErrorNotADenial(t *testing.T) {
	w := newWorld(t, testkit.NewDB(t))
	canceled, cancel := context.WithCancel(bg)
	cancel()

	ok, err := repository.NewGroups(w.db.App).IsActiveGroupMember(canceled, w.alice.ID, w.org.ID, w.group.ID)

	require.Error(t, err, "kegagalan infrastruktur tidak boleh terbaca sebagai 'bukan anggota'")
	require.False(t, ok)
}
