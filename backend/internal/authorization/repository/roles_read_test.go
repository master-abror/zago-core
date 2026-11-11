package repository_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes jalur BACA role memakai PostgreSQL 18 sungguhan dan role app_user (db.App). Tes negatif
// lintas organisasi ada di sini karena setiap kueri baca wajib memfilter tenant (docs/12 §6).

func rrReader(db *testpg.DB) *repository.RoleReader { return repository.NewRoleReader(db.App) }

// rrOrg mengembalikan penunjuk ke id organisasi (nil berarti scope platform).
func rrOrg(o testkit.Organization) *uuid.UUID {
	v := o.ID
	return &v
}

func rrIDs(items []domain.RoleRecord) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func rrFull() domain.RoleListQuery {
	return domain.RoleListQuery{Window: domain.Window{Limit: domain.MaxPageSize}}
}

func TestRoleReaderListShowsOwnAndSystemRolesNeverAnotherOrganization(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	sys := testkit.NewRole(t, db)
	mine := testkit.NewRole(t, db, testkit.RoleInOrganization(orgA))
	theirs := testkit.NewRole(t, db, testkit.RoleInOrganization(orgB))

	pageA, err := rrReader(db).List(bg, rrOrg(orgA), rrFull())
	require.NoError(t, err)
	idsA := rrIDs(pageA.Items)
	require.Contains(t, idsA, sys.ID, "template sistem terlihat")
	require.Contains(t, idsA, mine.ID)
	require.NotContains(t, idsA, theirs.ID, "role organisasi lain tidak boleh terbaca")
	require.Equal(t, len(pageA.Items), pageA.Total)

	pageB, err := rrReader(db).List(bg, rrOrg(orgB), rrFull())
	require.NoError(t, err)
	idsB := rrIDs(pageB.Items)
	require.Contains(t, idsB, theirs.ID)
	require.NotContains(t, idsB, mine.ID, "kebalikannya juga berlaku")
}

func TestRoleReaderListAtPlatformScopeSeesOnlySystemRoles(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	sys := testkit.NewRole(t, db)
	custom := testkit.NewRole(t, db, testkit.RoleInOrganization(org))

	page, err := rrReader(db).List(bg, nil, rrFull())
	require.NoError(t, err)
	ids := rrIDs(page.Items)
	require.Contains(t, ids, sys.ID)
	require.NotContains(t, ids, custom.ID, "scope platform tidak melihat isi organisasi")
	require.Equal(t, 1, page.Total)
}

func TestRoleReaderListFiltersByStatusAndDefaultsToActive(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	active := testkit.NewRole(t, db, testkit.RoleInOrganization(org))
	archived := testkit.NewRole(t, db, testkit.RoleInOrganization(org), testkit.RoleStatus("archived"))
	reader := rrReader(db)

	def, err := reader.List(bg, rrOrg(org), rrFull())
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{active.ID}, rrIDs(def.Items), "bawaan hanya role aktif")
	require.Equal(t, 1, def.Total)

	onlyArchived, err := reader.List(bg, rrOrg(org), domain.RoleListQuery{Status: domain.RoleFilterArchived, Window: domain.Window{Limit: 10}})
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{archived.ID}, rrIDs(onlyArchived.Items))
	require.Equal(t, 1, onlyArchived.Total)

	all, err := reader.List(bg, rrOrg(org), domain.RoleListQuery{Status: domain.RoleFilterAll, Window: domain.Window{Limit: 10}})
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{active.ID, archived.ID}, rrIDs(all.Items))
	require.Equal(t, 2, all.Total)
}

func TestRoleReaderListRejectsUnknownStatusInsteadOfListingEverything(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	testkit.NewRole(t, db, testkit.RoleInOrganization(org))

	page, err := rrReader(db).List(bg, rrOrg(org), domain.RoleListQuery{Status: "bogus", Window: domain.Window{Limit: 10}})

	require.Error(t, err)
	require.Empty(t, page.Items, "filter yang tidak dikenal tidak boleh jatuh ke 'semua'")
}

func TestRoleReaderListPagesAreStableAndCoverEverything(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	for i := 0; i < 5; i++ {
		testkit.NewRole(t, db, testkit.RoleInOrganization(org))
	}
	reader := rrReader(db)
	full, err := reader.List(bg, rrOrg(org), rrFull())
	require.NoError(t, err)
	require.Equal(t, 5, full.Total)

	collected := []uuid.UUID{}
	for offset := 0; offset < full.Total; offset += 2 {
		page, err := reader.List(bg, rrOrg(org), domain.RoleListQuery{Window: domain.Window{Offset: offset, Limit: 2}})
		require.NoError(t, err)
		require.Equal(t, 5, page.Total)
		require.LessOrEqual(t, len(page.Items), 2)
		collected = append(collected, rrIDs(page.Items)...)
	}
	require.Equal(t, rrIDs(full.Items), collected, "halaman berurutan menyusun daftar penuh tanpa duplikat atau lubang")

	beyond, err := reader.List(bg, rrOrg(org), domain.RoleListQuery{Window: domain.Window{Offset: 100, Limit: 2}})
	require.NoError(t, err)
	require.NotNil(t, beyond.Items, "halaman kosong tetap irisan, bukan null")
	require.Empty(t, beyond.Items)
	require.Equal(t, 5, beyond.Total)
}

func TestRoleReaderListToleratesNegativeOffsetAndZeroLimit(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	testkit.NewRole(t, db, testkit.RoleInOrganization(org))

	page, err := rrReader(db).List(bg, rrOrg(org), domain.RoleListQuery{Window: domain.Window{Offset: -5, Limit: 0}})

	require.NoError(t, err, "jendela dipangkas, tidak sampai ke SQL sebagai nilai negatif")
	require.Len(t, page.Items, 1)
}

func TestRoleReaderGetReturnsOwnRoleWithItsOwnSortedPermissionsOnly(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	role := testkit.RoleWith(t, db, []string{"role.read", "invoice.read", "invoice.delete"}, testkit.RoleInOrganization(org))
	testkit.RoleWith(t, db, []string{"user.read"}, testkit.RoleInOrganization(org))

	got, codes, err := rrReader(db).Get(bg, rrOrg(org), role.ID)

	require.NoError(t, err)
	require.Equal(t, role.ID, got.ID)
	require.Equal(t, role.Slug, got.Slug)
	require.Equal(t, "Role "+role.ID.String(), got.Name)
	require.Equal(t, domain.RoleTypeCustom, got.Type)
	require.Equal(t, "active", got.Status)
	require.NotNil(t, got.OrganizationID)
	require.Equal(t, org.ID, *got.OrganizationID)
	require.Equal(t, []string{"invoice.delete", "invoice.read", "role.read"}, codes, "terurut menurut byte dan tanpa permission role lain")
}

func TestRoleReaderGetReturnsEmptyNonNilPermissionsForRoleWithoutAny(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	role := testkit.NewRole(t, db, testkit.RoleInOrganization(org))

	_, codes, err := rrReader(db).Get(bg, rrOrg(org), role.ID)

	require.NoError(t, err)
	require.NotNil(t, codes)
	require.Empty(t, codes)
}

func TestRoleReaderGetReadsSystemTemplatesFromTenantAndPlatformScope(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	sys := testkit.RoleWith(t, db, []string{"role.read"})

	fromTenant, tenantCodes, err := rrReader(db).Get(bg, rrOrg(org), sys.ID)
	require.NoError(t, err)
	require.Equal(t, domain.RoleTypeSystem, fromTenant.Type)
	require.Nil(t, fromTenant.OrganizationID)
	require.Equal(t, []string{"role.read"}, tenantCodes)

	fromPlatform, _, err := rrReader(db).Get(bg, nil, sys.ID)
	require.NoError(t, err)
	require.Equal(t, sys.ID, fromPlatform.ID)
}

func TestRoleReaderGetHidesRoleOfAnotherOrganizationAsNotFound(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	theirs := testkit.RoleWith(t, db, []string{"role.manage"}, testkit.RoleInOrganization(orgB))

	_, codes, errOther := rrReader(db).Get(bg, rrOrg(orgA), theirs.ID)
	require.ErrorIs(t, errOther, domain.ErrRoleNotFound)
	require.Nil(t, codes, "tidak ada permission role organisasi lain yang bocor")

	_, _, errMissing := rrReader(db).Get(bg, rrOrg(orgA), id.NewID())
	require.ErrorIs(t, errMissing, domain.ErrRoleNotFound)
	require.Equal(t, errMissing.Error(), errOther.Error(), "role milik orang lain tidak dapat dibedakan dari role yang tidak ada")

	_, _, errPlatform := rrReader(db).Get(bg, nil, theirs.ID)
	require.ErrorIs(t, errPlatform, domain.ErrRoleNotFound, "scope platform juga tidak melihat isi organisasi")

	_, _, errOwner := rrReader(db).Get(bg, rrOrg(orgB), theirs.ID)
	require.NoError(t, errOwner, "pemiliknya sendiri tetap dapat membacanya")
}

func TestRoleReaderGetReturnsArchivedRole(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	role := testkit.NewRole(t, db, testkit.RoleInOrganization(org), testkit.RoleStatus("archived"))

	got, _, err := rrReader(db).Get(bg, rrOrg(org), role.ID)

	require.NoError(t, err)
	require.Equal(t, "archived", got.Status)
}
