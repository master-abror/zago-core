package application_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func TestFieldGuardWithPostgresHidesRestrictedFieldsFromUsersWithoutTheirCodes(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	other := testkit.NewOrganization(t, db)
	alice := testkit.NewUser(t, db, testkit.InOrganization(org))
	bob := testkit.NewUser(t, db, testkit.InOrganization(org))
	carol := testkit.NewUser(t, db, testkit.InOrganization(other))
	oid, otherID := org.ID, other.ID

	costRole := testkit.RoleWith(t, db, []string{fieldCostRead})
	testkit.EnsurePermission(t, db, fieldCostWrite)
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: costRole, SubjectType: "user", SubjectID: alice.ID,
		ScopeType: "organization", OrganizationID: &oid,
	})

	authz, err := application.NewAuthorizer(repository.NewEffective(db.App), nil)
	require.NoError(t, err)
	guard, err := application.NewFieldGuard(authz, repository.NewFields(db.App))
	require.NoError(t, err)

	scope := domain.ScopeContext{OrganizationID: &oid}
	ref := domain.ResourceRef{Type: "invoice", ID: id.NewID(), OrganizationID: &oid}
	filter := func(userID uuid.UUID) map[string]any {
		t.Helper()
		out, err := guard.Filter(ctx, authdomain.Identity{UserID: userID, OrganizationID: &oid}, scope, ref, fieldObject())
		require.NoError(t, err)
		return out
	}

	// Hanya internal_cost (read dan write) yang sudah dideklarasikan: margin belum dibatasi.
	require.Equal(t, map[string]any{"number": "INV-1", "internal_cost": 250000, "margin": 0.2}, filter(alice.ID))
	require.Equal(t, map[string]any{"number": "INV-1", "margin": 0.2}, filter(bob.ID), "tanpa kode: internal_cost absen")

	// Deklarasi baru berlaku segera: definisi tidak di-cache.
	testkit.EnsurePermission(t, db, fieldMarginRead)
	require.Equal(t, map[string]any{"number": "INV-1", "internal_cost": 250000}, filter(alice.ID), "margin kini terbatas dan alice tidak memegangnya")
	require.Equal(t, map[string]any{"number": "INV-1"}, filter(bob.ID))

	// Hak tulis dipegang terpisah: alice punya read, tidak punya write.
	err = guard.CheckWrite(ctx, authdomain.Identity{UserID: alice.ID, OrganizationID: &oid}, scope, ref, map[string]any{"internal_cost": 1})
	require.ErrorIs(t, err, domain.ErrFieldPermissionDenied)
	require.NoError(t, guard.CheckWrite(ctx, authdomain.Identity{UserID: alice.ID, OrganizationID: &oid}, scope, ref, map[string]any{"number": "X"}))

	// Tipe tanpa field terbatas tidak ditapis sama sekali.
	billRef := domain.ResourceRef{Type: "bill", ID: id.NewID(), OrganizationID: &oid}
	out, err := guard.Filter(ctx, authdomain.Identity{UserID: bob.ID, OrganizationID: &oid}, scope, billRef, fieldObject())
	require.NoError(t, err)
	require.Equal(t, fieldObject(), out)

	// Organisasi lain: identitas carol bukan anggota scope ini.
	_, err = guard.Filter(ctx, authdomain.Identity{UserID: carol.ID, OrganizationID: &otherID}, scope, ref, fieldObject())
	require.ErrorIs(t, err, domain.ErrFieldScopeMismatch)
}
