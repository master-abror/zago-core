package repository_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Regresi migrasi 035 (ADR-0022 butir 65): sebelum 035, indeks unik assignment tidak memuat
// organisasi, sehingga role sistem yang sama untuk user yang sama di organisasi kedua gagal sebagai
// ErrAssignmentExists padahal bukan duplikat. Tes ini memakai jalur Insert dan ActiveDuplicate
// sungguhan, bukan hanya DDL.

func TestAssignmentsInsertAllowsTheSameRoleAndSubjectInTwoOrganizations(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	role := testkit.NewRole(t, db)
	subject := id.NewID()
	inA := domain.OrganizationTarget(orgA.ID)
	inB := domain.OrganizationTarget(orgB.ID)

	_, err := asInsert(db, asRec(role.ID, subject, inA))
	require.NoError(t, err)
	err = asTx(db, func(ctx context.Context) error {
		dup, derr := asRepo(db).ActiveDuplicate(ctx, role.ID, domain.SubjectUser, subject, inB)
		require.NoError(t, derr)
		require.False(t, dup, "assignment di organisasi A bukan duplikat untuk organisasi B")
		return nil
	})
	require.NoError(t, err)
	_, err = asInsert(db, asRec(role.ID, subject, inB))
	require.NoError(t, err, "organisasi B menerima assignment yang sama")

	_, err = asInsert(db, asRec(role.ID, subject, inA))
	require.ErrorIs(t, err, domain.ErrAssignmentExists, "duplikat di organisasi A tetap ditolak")
	_, err = asInsert(db, asRec(role.ID, subject, inB))
	require.ErrorIs(t, err, domain.ErrAssignmentExists, "duplikat di organisasi B tetap ditolak")
}
