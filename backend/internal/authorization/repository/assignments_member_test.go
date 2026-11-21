package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes RevokeUserInOrganization (ADR-0023 butir 15). Semua memakai PostgreSQL sungguhan; jalur
// tulis diuji dengan DUA organisasi dan dua arah supaya filter organization_id terbukti.

// rmuAssign menyisipkan satu assignment aktif milik userID di organisasi org dengan role baru.
func rmuAssign(t *testing.T, db *testpg.DB, userID uuid.UUID, org testkit.Organization, scopeType string, scopeID *uuid.UUID) testkit.Assignment {
	t.Helper()
	orgID := org.ID
	spec := testkit.AssignmentSpec{Role: testkit.NewRole(t, db), SubjectType: domain.SubjectUser, SubjectID: userID}
	spec.OrganizationID = &orgID
	spec.ScopeType = scopeType
	spec.ScopeID = scopeID
	return testkit.AssignRole(t, db, spec)
}

// rmuActive menghitung assignment user yang belum dicabut di organisasi itu.
func rmuActive(t *testing.T, db *testpg.DB, userID, orgID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, db.Migrator.QueryRow(bg,
		`SELECT count(*) FROM role_assignments WHERE subject_id = $1 AND organization_id = $2 AND revoked_at IS NULL`,
		userID.String(), orgID.String()).Scan(&n))
	return n
}

// rmuIsActive melaporkan apakah baris assignment itu belum dicabut.
func rmuIsActive(t *testing.T, db *testpg.DB, a testkit.Assignment) bool {
	t.Helper()
	return asFlag(t, db, `SELECT revoked_at IS NULL FROM role_assignments WHERE id = $1`, a.ID.String())
}

// rmuRevoke menjalankan RevokeUserInOrganization di dalam transaksi sungguhan.
func rmuRevoke(t *testing.T, db *testpg.DB, orgID, userID uuid.UUID) int {
	t.Helper()
	var n int
	err := asTx(db, func(ctx context.Context) error {
		var rerr error
		n, rerr = asRepo(db).RevokeUserInOrganization(ctx, orgID, userID)
		return rerr
	})
	require.NoError(t, err)
	return n
}

func TestRevokeUserInOrganizationRevokesEveryOrganizationBoundScope(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db, testkit.InOrganization(org))
	group := testkit.NewGroup(t, db, org, user)
	groupID := group.ID
	orgScopeID := org.ID
	resourceID := id.NewID()

	rmuAssign(t, db, user.ID, org, "organization", nil)
	rmuAssign(t, db, user.ID, org, "organization", &orgScopeID)
	rmuAssign(t, db, user.ID, org, "group", &groupID)
	rmuAssign(t, db, user.ID, org, "resource", &resourceID)
	rmuAssign(t, db, user.ID, org, "own", nil)
	require.Equal(t, 5, rmuActive(t, db, user.ID, org.ID))

	require.Equal(t, 5, rmuRevoke(t, db, org.ID, user.ID))
	require.Equal(t, 0, rmuActive(t, db, user.ID, org.ID))
	require.Equal(t, 0, rmuRevoke(t, db, org.ID, user.ID), "panggilan kedua tidak menemukan apa pun")
}

func TestRevokeUserInOrganizationAlsoRevokesExpiredButUnrevokedRows(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db, testkit.InOrganization(org))
	orgID := org.ID
	past := time.Now().Add(-time.Hour)

	spec := testkit.AssignmentSpec{Role: testkit.NewRole(t, db), SubjectType: domain.SubjectUser, SubjectID: user.ID}
	spec.OrganizationID = &orgID
	spec.ScopeType = "organization"
	spec.ExpiresAt = &past
	expired := testkit.AssignRole(t, db, spec)

	require.Equal(t, 1, rmuRevoke(t, db, org.ID, user.ID))
	require.False(t, rmuIsActive(t, db, expired))
}

func TestRevokeUserInOrganizationLeavesOtherRowsUntouched(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	other := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db, testkit.InOrganization(org))
	testkit.AddOrganizationMember(t, db, other, user)
	colleague := testkit.NewUser(t, db, testkit.InOrganization(org))
	group := testkit.NewGroup(t, db, org, user)
	orgID := org.ID

	target := rmuAssign(t, db, user.ID, org, "organization", nil)

	platformSpec := testkit.AssignmentSpec{Role: testkit.NewRole(t, db), SubjectType: domain.SubjectUser, SubjectID: user.ID}
	platformSpec.ScopeType = "platform"
	platform := testkit.AssignRole(t, db, platformSpec)

	inOther := rmuAssign(t, db, user.ID, other, "organization", nil)
	ofColleague := rmuAssign(t, db, colleague.ID, org, "organization", nil)

	groupSpec := testkit.AssignmentSpec{Role: testkit.NewRole(t, db), SubjectType: "group", SubjectID: group.ID}
	groupSpec.OrganizationID = &orgID
	groupSpec.ScopeType = "organization"
	ofGroup := testkit.AssignRole(t, db, groupSpec)

	oldSpec := testkit.AssignmentSpec{Role: testkit.NewRole(t, db), SubjectType: domain.SubjectUser, SubjectID: user.ID}
	oldSpec.OrganizationID = &orgID
	oldSpec.ScopeType = "organization"
	oldSpec.Revoked = true
	old := testkit.AssignRole(t, db, oldSpec)
	var before time.Time
	require.NoError(t, db.Migrator.QueryRow(bg, `SELECT revoked_at FROM role_assignments WHERE id = $1`, old.ID.String()).Scan(&before))

	require.Equal(t, 1, rmuRevoke(t, db, org.ID, user.ID), "hanya assignment aktif user itu di organisasi itu")

	require.False(t, rmuIsActive(t, db, target))
	require.True(t, rmuIsActive(t, db, platform), "assignment platform tidak disentuh")
	require.True(t, rmuIsActive(t, db, inOther), "assignment user yang sama di organisasi lain tidak disentuh")
	require.True(t, rmuIsActive(t, db, ofColleague), "assignment user lain tidak disentuh")
	require.True(t, rmuIsActive(t, db, ofGroup), "assignment ber-subjek grup tidak disentuh")

	var after time.Time
	require.NoError(t, db.Migrator.QueryRow(bg, `SELECT revoked_at FROM role_assignments WHERE id = $1`, old.ID.String()).Scan(&after))
	require.True(t, before.Equal(after), "revoked_at lama tidak ditimpa")
}

func TestRevokeUserInOrganizationIsScopedToTheGivenOrganizationBothWays(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	empty := testkit.NewOrganization(t, db)

	for _, firstA := range []bool{true, false} {
		user := testkit.NewUser(t, db, testkit.InOrganization(orgA))
		testkit.AddOrganizationMember(t, db, orgB, user)
		inA := rmuAssign(t, db, user.ID, orgA, "organization", nil)
		inB := rmuAssign(t, db, user.ID, orgB, "organization", nil)

		first, firstRow, secondRow := orgA, inA, inB
		if !firstA {
			first, firstRow, secondRow = orgB, inB, inA
		}
		require.Equal(t, 0, rmuRevoke(t, db, empty.ID, user.ID), "organisasi tanpa assignment tidak mencabut apa pun")
		require.True(t, rmuIsActive(t, db, firstRow))
		require.True(t, rmuIsActive(t, db, secondRow))

		require.Equal(t, 1, rmuRevoke(t, db, first.ID, user.ID))
		require.False(t, rmuIsActive(t, db, firstRow))
		require.True(t, rmuIsActive(t, db, secondRow), "organisasi lain tetap utuh")
	}
}

func TestRevokeUserInOrganizationRequiresATransaction(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db, testkit.InOrganization(org))
	row := rmuAssign(t, db, user.ID, org, "organization", nil)

	_, err := asRepo(db).RevokeUserInOrganization(bg, org.ID, user.ID)
	require.ErrorIs(t, err, repository.ErrNoTransaction)
	require.True(t, rmuIsActive(t, db, row), "tanpa transaksi tidak ada yang dicabut")
}

func TestRevokeUserInOrganizationRollsBackWithTheTransaction(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db, testkit.InOrganization(org))
	row := rmuAssign(t, db, user.ID, org, "organization", nil)
	errBoom := errors.New("gagal sengaja")

	err := asTx(db, func(ctx context.Context) error {
		n, rerr := asRepo(db).RevokeUserInOrganization(ctx, org.ID, user.ID)
		require.NoError(t, rerr)
		require.Equal(t, 1, n)
		return errBoom
	})
	require.ErrorIs(t, err, errBoom)
	require.True(t, rmuIsActive(t, db, row), "rollback mengembalikan assignment")
}

func TestRevokeUserInOrganizationRejectsNilIdentifiers(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db, testkit.InOrganization(org))

	err := asTx(db, func(ctx context.Context) error {
		_, rerr := asRepo(db).RevokeUserInOrganization(ctx, uuid.Nil, user.ID)
		return rerr
	})
	require.Error(t, err)
	err = asTx(db, func(ctx context.Context) error {
		_, rerr := asRepo(db).RevokeUserInOrganization(ctx, org.ID, uuid.Nil)
		return rerr
	})
	require.Error(t, err)
}
