package repository_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// activeMember memanggil Members.IsActiveMember lewat pool runtime (app_user).
func activeMember(t *testing.T, db *testpg.DB, org, user uuid.UUID) bool {
	t.Helper()
	ok, err := repository.NewMembers(db.App).IsActiveMember(bg, org, user)
	require.NoError(t, err)
	return ok
}

func TestMembersActiveMemberIsFound(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)

	require.True(t, activeMember(t, db, w.org.ID, w.alice.ID))
}

func TestMembersNonActiveMembershipStatusesAreRejected(t *testing.T) {
	db := testkit.NewDB(t)
	for _, status := range []string{"invited", "suspended", "removed"} {
		t.Run(status, func(t *testing.T) {
			w := newWorld(t, db)
			w.exec(t, `UPDATE organization_memberships SET status = $1 WHERE organization_id = $2 AND user_id = $3`,
				status, w.org.ID.String(), w.alice.ID.String())

			require.False(t, activeMember(t, db, w.org.ID, w.alice.ID))
		})
	}
}

func TestMembersInactiveUserIsRejected(t *testing.T) {
	db := testkit.NewDB(t)
	for _, status := range []string{"pending", "suspended", "locked", "deactivated", "deleted"} {
		t.Run(status, func(t *testing.T) {
			w := newWorld(t, db)
			require.True(t, activeMember(t, db, w.org.ID, w.alice.ID), "kontrol: sebelum diubah")
			w.exec(t, `UPDATE users SET status = $1 WHERE id = $2`, status, w.alice.ID.String())

			require.False(t, activeMember(t, db, w.org.ID, w.alice.ID))
		})
	}
}

func TestMembersSoftDeletedUserIsRejected(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)
	w.exec(t, `UPDATE users SET deleted_at = now() WHERE id = $1`, w.alice.ID.String())

	require.False(t, activeMember(t, db, w.org.ID, w.alice.ID))
}

func TestMembersAreScopedToTheAskedOrganizationInBothDirections(t *testing.T) {
	db := testkit.NewDB(t)
	a := newWorld(t, db)
	b := newWorld(t, db)

	require.True(t, activeMember(t, db, a.org.ID, a.alice.ID), "kontrol: alice anggota A")
	require.True(t, activeMember(t, db, b.org.ID, b.alice.ID), "kontrol: alice B anggota B")
	require.False(t, activeMember(t, db, b.org.ID, a.alice.ID), "anggota A bukan anggota B")
	require.False(t, activeMember(t, db, a.org.ID, b.alice.ID), "anggota B bukan anggota A")
}

func TestMembersUserInTwoOrganizationsIsMemberOfEachOnlyWhileActiveThere(t *testing.T) {
	db := testkit.NewDB(t)
	a := newWorld(t, db)
	orgB := testkit.NewOrganization(t, db)
	testkit.AddOrganizationMember(t, db, orgB, a.alice)

	require.True(t, activeMember(t, db, a.org.ID, a.alice.ID))
	require.True(t, activeMember(t, db, orgB.ID, a.alice.ID))

	a.exec(t, `UPDATE organization_memberships SET status = 'suspended' WHERE organization_id = $1 AND user_id = $2`,
		orgB.ID.String(), a.alice.ID.String())

	require.True(t, activeMember(t, db, a.org.ID, a.alice.ID), "keanggotaan A tidak ikut tertangguh")
	require.False(t, activeMember(t, db, orgB.ID, a.alice.ID))
}

func TestMembersUnknownIdentifiersAreRejected(t *testing.T) {
	db := testkit.NewDB(t)
	w := newWorld(t, db)

	require.False(t, activeMember(t, db, w.org.ID, id.NewID()))
	require.False(t, activeMember(t, db, id.NewID(), w.alice.ID))
	require.False(t, activeMember(t, db, uuid.Nil, uuid.Nil))
}
