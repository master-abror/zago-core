package repository_test

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/identity/domain"
	"github.com/master-abror/zago-core/backend/internal/identity/repository"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	orgdomain "github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

var bg = context.Background()

func userRepo(db *testpg.DB) *repository.Users { return repository.NewUsers(db.App) }

func strPtr(s string) *string { return &s }

func tenantOf(t *testing.T, o testkit.Organization) orgdomain.Access {
	t.Helper()
	a, err := orgdomain.TenantAccess(o.ID)
	require.NoError(t, err)
	return a
}

func execSQL(t *testing.T, db *testpg.DB, sql string, args ...any) {
	t.Helper()
	_, err := db.Migrator.Exec(bg, sql, args...)
	require.NoError(t, err)
}

func setDisplayName(t *testing.T, db *testpg.DB, u testkit.User, name string) {
	t.Helper()
	execSQL(t, db, `UPDATE users SET display_name = $2 WHERE id = $1`, u.ID.String(), name)
}

func setUserStatus(t *testing.T, db *testpg.DB, u testkit.User, status string) {
	t.Helper()
	execSQL(t, db, `UPDATE users SET status = $2 WHERE id = $1`, u.ID.String(), status)
}

func softDeleteUser(t *testing.T, db *testpg.DB, u testkit.User) {
	t.Helper()
	execSQL(t, db, `UPDATE users SET deleted_at = now() WHERE id = $1`, u.ID.String())
}

// addMembership menambahkan keanggotaan u di o dengan status tertentu.
func addMembership(t *testing.T, db *testpg.DB, o testkit.Organization, u testkit.User, status string) {
	t.Helper()
	execSQL(t, db,
		`INSERT INTO organization_memberships (id, organization_id, user_id, status) VALUES ($1, $2, $3, $4)`,
		id.NewID().String(), o.ID.String(), u.ID.String(), status)
}

func setMembershipStatus(t *testing.T, db *testpg.DB, o testkit.Organization, u testkit.User, status string) {
	t.Helper()
	execSQL(t, db,
		`UPDATE organization_memberships SET status = $3 WHERE organization_id = $1 AND user_id = $2`,
		o.ID.String(), u.ID.String(), status)
}

// fillUser mengisi kolom nullable user lewat role pemilik skema.
func fillUser(t *testing.T, db *testpg.DB, u testkit.User, username, first, last, phone, avatar string) {
	t.Helper()
	execSQL(t, db,
		`UPDATE users SET username = $2, first_name = $3, last_name = $4, phone = $5, avatar_url = $6 WHERE id = $1`,
		u.ID.String(), username, first, last, phone, avatar)
}

// protectedColumns menggabungkan kolom yang TIDAK boleh berubah lewat jalur ubah profil/user.
func protectedColumns(t *testing.T, db *testpg.DB, u testkit.User) string {
	t.Helper()
	var s string
	require.NoError(t, db.Migrator.QueryRow(bg,
		`SELECT concat_ws('|', id, email, coalesce(username, ''), status, coalesce(last_login_at::text, ''), created_at::text, coalesce(deleted_at::text, ''))
		   FROM users WHERE id = $1`, u.ID.String()).Scan(&s))
	return s
}

func containsID(p domain.Page, userID uuid.UUID) bool {
	for _, rec := range p.Items {
		if rec.ID == userID {
			return true
		}
	}
	return false
}

func itemIDs(p domain.Page) []uuid.UUID {
	out := []uuid.UUID{}
	for _, rec := range p.Items {
		out = append(out, rec.ID)
	}
	return out
}

func mustSelf(t *testing.T, db *testpg.DB, u testkit.User) domain.Record {
	t.Helper()
	rec, err := userRepo(db).GetSelf(bg, u.ID)
	require.NoError(t, err)
	return rec
}

func TestGetIsScopedToTheCallerOrganizationInBothDirections(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	userA := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	userB := testkit.NewUser(t, db, testkit.InOrganization(orgB))
	accA := tenantOf(t, orgA)
	accB := tenantOf(t, orgB)

	got, err := repo.Get(bg, accA, userA.ID)
	require.NoError(t, err)
	require.Equal(t, userA.ID, got.ID)
	require.Equal(t, userA.Email, got.Email)
	got, err = repo.Get(bg, accB, userB.ID)
	require.NoError(t, err)
	require.Equal(t, userB.ID, got.ID)

	_, err = repo.Get(bg, accA, userB.ID)
	require.ErrorIs(t, err, domain.ErrUserNotFound, "A tidak membaca user B")
	_, err = repo.Get(bg, accB, userA.ID)
	require.ErrorIs(t, err, domain.ErrUserNotFound, "B tidak membaca user A")

	plat := orgdomain.PlatformAccess()
	_, err = repo.Get(bg, plat, userA.ID)
	require.NoError(t, err, "platform membaca user A")
	_, err = repo.Get(bg, plat, userB.ID)
	require.NoError(t, err, "platform membaca user B")
}

func TestGetOtherOrganizationMissingAndSoftDeletedAreIndistinguishable(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	userA := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	userB := testkit.NewUser(t, db, testkit.InOrganization(orgB))
	gone := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	softDeleteUser(t, db, gone)
	accA := tenantOf(t, orgA)
	accB := tenantOf(t, orgB)

	_, errMissing := repo.Get(bg, accA, id.NewID())
	_, errOther := repo.Get(bg, accA, userB.ID)
	_, errDeleted := repo.Get(bg, accA, gone.ID)
	require.ErrorIs(t, errMissing, domain.ErrUserNotFound)
	require.ErrorIs(t, errOther, domain.ErrUserNotFound)
	require.ErrorIs(t, errDeleted, domain.ErrUserNotFound)
	require.Equal(t, errMissing.Error(), errOther.Error(), "pesan identik: organisasi lain dan tidak ada")
	require.Equal(t, errMissing.Error(), errDeleted.Error(), "pesan identik: soft-deleted dan tidak ada")

	_, errOther = repo.Get(bg, accB, userA.ID)
	require.ErrorIs(t, errOther, domain.ErrUserNotFound)
	require.Equal(t, errMissing.Error(), errOther.Error(), "pesan identik arah sebaliknya")

	_, err := repo.Get(bg, orgdomain.PlatformAccess(), gone.ID)
	require.ErrorIs(t, err, domain.ErrUserNotFound, "platform pun tidak melihat user soft-deleted")
}

func TestGetUserInTwoOrganizationsIsVisibleToBothButNotToThird(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	orgC := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	testkit.AddOrganizationMember(t, db, orgB, u)

	_, err := repo.Get(bg, tenantOf(t, orgA), u.ID)
	require.NoError(t, err)
	_, err = repo.Get(bg, tenantOf(t, orgB), u.ID)
	require.NoError(t, err)
	_, err = repo.Get(bg, tenantOf(t, orgC), u.ID)
	require.ErrorIs(t, err, domain.ErrUserNotFound)
}

func TestGetVisibilityFollowsMembershipStatus(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	org := testkit.NewOrganization(t, db)
	acc := tenantOf(t, org)

	for _, status := range []string{"invited", "active", "suspended"} {
		u := testkit.NewUser(t, db)
		addMembership(t, db, org, u, status)
		_, err := repo.Get(bg, acc, u.ID)
		require.NoError(t, err, "keanggotaan %s tetap terlihat", status)
	}

	removed := testkit.NewUser(t, db)
	addMembership(t, db, org, removed, "removed")
	_, err := repo.Get(bg, acc, removed.ID)
	require.ErrorIs(t, err, domain.ErrUserNotFound, "keanggotaan removed tidak memberi penglihatan")
	_, err = repo.Get(bg, orgdomain.PlatformAccess(), removed.ID)
	require.NoError(t, err, "platform tetap melihat")

	active := testkit.NewUser(t, db, testkit.InOrganization(org))
	setMembershipStatus(t, db, org, active, "removed")
	_, err = repo.Get(bg, acc, active.ID)
	require.ErrorIs(t, err, domain.ErrUserNotFound, "removed sesudah active juga tidak terlihat")
}

func TestGetUserWithoutMembershipIsVisibleOnlyToPlatform(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	org := testkit.NewOrganization(t, db)
	loner := testkit.NewUser(t, db)

	_, err := repo.Get(bg, tenantOf(t, org), loner.ID)
	require.ErrorIs(t, err, domain.ErrUserNotFound)
	_, err = repo.Get(bg, orgdomain.PlatformAccess(), loner.ID)
	require.NoError(t, err)
}

func TestGetReturnsTheStoredFields(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db, testkit.InOrganization(org))
	fillUser(t, db, u, "budi.s", "Budi", "Santoso", "+62 812 3456 7890", "https://cdn.example.org/a.png")
	execSQL(t, db, `UPDATE users SET locale = 'id-ID', timezone = 'Asia/Jakarta', last_login_at = now() WHERE id = $1`, u.ID.String())

	got, err := repo.Get(bg, tenantOf(t, org), u.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Username)
	require.Equal(t, "budi.s", *got.Username)
	require.Equal(t, u.DisplayName, got.DisplayName)
	require.NotNil(t, got.FirstName)
	require.Equal(t, "Budi", *got.FirstName)
	require.NotNil(t, got.LastName)
	require.Equal(t, "Santoso", *got.LastName)
	require.Equal(t, u.Email, got.Email)
	require.NotNil(t, got.Phone)
	require.Equal(t, "+62 812 3456 7890", *got.Phone)
	require.NotNil(t, got.AvatarURL)
	require.Equal(t, "https://cdn.example.org/a.png", *got.AvatarURL)
	require.Equal(t, "id-ID", got.Locale)
	require.Equal(t, "Asia/Jakarta", got.Timezone)
	require.Equal(t, "active", got.Status)
	require.NotNil(t, got.LastLoginAt, "repository membawa last_login_at; penyembunyiannya untuk tenant adalah urusan DTO")
	require.False(t, got.CreatedAt.IsZero())
	require.False(t, got.UpdatedAt.IsZero())
}

func TestGetSelfIgnoresTenantsAndHidesSoftDeleted(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	loner := testkit.NewUser(t, db)

	got, err := repo.GetSelf(bg, loner.ID)
	require.NoError(t, err, "pemilik data membaca barisnya sendiri tanpa keanggotaan")
	require.Equal(t, loner.ID, got.ID)

	softDeleteUser(t, db, loner)
	_, err = repo.GetSelf(bg, loner.ID)
	require.ErrorIs(t, err, domain.ErrUserNotFound)

	_, err = repo.GetSelf(bg, uuid.Nil)
	require.ErrorIs(t, err, domain.ErrInvalidID)
	_, err = repo.Get(bg, orgdomain.PlatformAccess(), uuid.Nil)
	require.ErrorIs(t, err, domain.ErrInvalidID)
}

func TestZeroAccessIsRejectedByEveryAccessMethod(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db, testkit.InOrganization(org))
	var none orgdomain.Access
	name := "X"

	_, err := repo.Get(bg, none, u.ID)
	require.ErrorIs(t, err, orgdomain.ErrInvalidAccess)
	_, err = repo.ForUpdate(bg, none, u.ID)
	require.ErrorIs(t, err, orgdomain.ErrInvalidAccess)
	_, err = repo.List(bg, none, domain.ListQuery{})
	require.ErrorIs(t, err, orgdomain.ErrInvalidAccess)
	_, err = repo.Update(bg, none, u.ID, domain.UpdateInput{DisplayName: &name})
	require.ErrorIs(t, err, orgdomain.ErrInvalidAccess)

	require.Equal(t, u.DisplayName, mustSelf(t, db, u).DisplayName, "tidak ada yang tertulis")
}

func TestListSeesOnlyTheCallerOrganizationMembers(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	a1 := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	a2 := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	b1 := testkit.NewUser(t, db, testkit.InOrganization(orgB))
	shared := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	testkit.AddOrganizationMember(t, db, orgB, shared)

	pageA, err := repo.List(bg, tenantOf(t, orgA), domain.ListQuery{})
	require.NoError(t, err)
	require.Equal(t, 3, pageA.Total)
	require.True(t, containsID(pageA, a1.ID))
	require.True(t, containsID(pageA, a2.ID))
	require.True(t, containsID(pageA, shared.ID))
	require.False(t, containsID(pageA, b1.ID), "A tidak melihat user eksklusif B")

	pageB, err := repo.List(bg, tenantOf(t, orgB), domain.ListQuery{})
	require.NoError(t, err)
	require.Equal(t, 2, pageB.Total)
	require.True(t, containsID(pageB, b1.ID))
	require.True(t, containsID(pageB, shared.ID))
	require.False(t, containsID(pageB, a1.ID), "B tidak melihat user eksklusif A")
	require.False(t, containsID(pageB, a2.ID))

	pagePlat, err := repo.List(bg, orgdomain.PlatformAccess(), domain.ListQuery{})
	require.NoError(t, err)
	for _, u := range []testkit.User{a1, a2, b1, shared} {
		require.True(t, containsID(pagePlat, u.ID), "platform melihat semua")
	}
}

func TestListHidesRemovedMembersFromTheTenantOnly(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	org := testkit.NewOrganization(t, db)
	keep := testkit.NewUser(t, db, testkit.InOrganization(org))
	gone := testkit.NewUser(t, db, testkit.InOrganization(org))
	setMembershipStatus(t, db, org, gone, "removed")

	tenantPage, err := repo.List(bg, tenantOf(t, org), domain.ListQuery{})
	require.NoError(t, err)
	require.Equal(t, 1, tenantPage.Total)
	require.True(t, containsID(tenantPage, keep.ID))
	require.False(t, containsID(tenantPage, gone.ID))

	platPage, err := repo.List(bg, orgdomain.PlatformAccess(), domain.ListQuery{})
	require.NoError(t, err)
	require.True(t, containsID(platPage, gone.ID), "platform tidak dibatasi keanggotaan")
}

func TestListFiltersByStatusAndHidesSoftDeleted(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	org := testkit.NewOrganization(t, db)
	acc := tenantOf(t, org)
	byStatus := map[string]testkit.User{}
	for _, s := range []string{"active", "pending", "suspended", "locked", "deactivated", "deleted"} {
		u := testkit.NewUser(t, db, testkit.InOrganization(org))
		setUserStatus(t, db, u, s)
		byStatus[s] = u
	}
	gone := testkit.NewUser(t, db, testkit.InOrganization(org))
	softDeleteUser(t, db, gone)

	open, err := repo.List(bg, acc, domain.ListQuery{})
	require.NoError(t, err)
	require.Equal(t, 4, open.Total, "bawaan = open: pending, active, suspended, locked")
	require.False(t, containsID(open, byStatus["deactivated"].ID))
	require.False(t, containsID(open, byStatus["deleted"].ID))
	require.False(t, containsID(open, gone.ID))

	open2, err := repo.List(bg, acc, domain.ListQuery{Filter: domain.FilterOpen})
	require.NoError(t, err)
	require.Equal(t, open.Total, open2.Total)

	all, err := repo.List(bg, acc, domain.ListQuery{Filter: domain.FilterAll})
	require.NoError(t, err)
	require.Equal(t, 6, all.Total)
	require.True(t, containsID(all, byStatus["deactivated"].ID))
	require.False(t, containsID(all, gone.ID), "soft-deleted tidak pernah terlihat, termasuk pada filter all")

	one, err := repo.List(bg, acc, domain.ListQuery{Filter: domain.StatusDeactivated})
	require.NoError(t, err)
	require.Equal(t, 1, one.Total)
	require.Equal(t, byStatus["deactivated"].ID, one.Items[0].ID)

	_, err = repo.List(bg, acc, domain.ListQuery{Filter: "bogus"})
	require.ErrorIs(t, err, domain.ErrInvalidFilter)
}

func TestListOrdersByDisplayNameThenIDAndPaginates(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	org := testkit.NewOrganization(t, db)
	acc := tenantOf(t, org)
	names := []string{"Echo", "Delta", "Alpha", "Delta", "Charlie", "Bravo"}
	type row struct {
		name string
		id   uuid.UUID
	}
	rows := []row{}
	for _, n := range names {
		u := testkit.NewUser(t, db, testkit.InOrganization(org))
		setDisplayName(t, db, u, n)
		rows = append(rows, row{name: n, id: u.ID})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].name != rows[j].name {
			return rows[i].name < rows[j].name
		}
		return rows[i].id.String() < rows[j].id.String()
	})
	want := []uuid.UUID{}
	for _, r := range rows {
		want = append(want, r.id)
	}

	got := []uuid.UUID{}
	for _, offset := range []int{0, 2, 4} {
		page, err := repo.List(bg, acc, domain.ListQuery{Window: domain.Window{Offset: offset, Limit: 2}})
		require.NoError(t, err)
		require.Equal(t, 6, page.Total, "total sama di setiap halaman")
		require.Len(t, page.Items, 2)
		got = append(got, itemIDs(page)...)
	}
	require.Equal(t, want, got, "urut (display_name, id) tanpa duplikat atau lompatan antar halaman")

	past, err := repo.List(bg, acc, domain.ListQuery{Window: domain.Window{Offset: 6, Limit: 2}})
	require.NoError(t, err)
	require.NotNil(t, past.Items)
	require.Empty(t, past.Items)
	require.Equal(t, 6, past.Total)

	full, err := repo.List(bg, acc, domain.ListQuery{})
	require.NoError(t, err)
	require.Equal(t, want, itemIDs(full), "jendela kosong memakai ukuran bawaan")
}

func TestUpdateIsPlatformOnly(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db, testkit.InOrganization(org))
	before := protectedColumns(t, db, u)
	in := domain.UpdateInput{DisplayName: strPtr("Diubah Tenant")}

	_, err := repo.Update(bg, tenantOf(t, org), u.ID, in)
	require.ErrorIs(t, err, orgdomain.ErrPlatformRequired, "tenant tidak boleh mengubah user lewat jalur ber-Access")
	require.Equal(t, u.DisplayName, mustSelf(t, db, u).DisplayName)

	got, err := repo.Update(bg, orgdomain.PlatformAccess(), u.ID, in)
	require.NoError(t, err)
	require.Equal(t, "Diubah Tenant", got.DisplayName)
	require.Equal(t, before, protectedColumns(t, db, u))
}

func TestUpdateChangesOnlyTheGivenFieldsAndClearsNullable(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	plat := orgdomain.PlatformAccess()
	u := testkit.NewUser(t, db)
	fillUser(t, db, u, "budi.s", "Budi", "Santoso", "+62 812 3456 7890", "https://cdn.example.org/a.png")
	before := mustSelf(t, db, u)

	got, err := repo.Update(bg, plat, u.ID, domain.UpdateInput{DisplayName: strPtr("Budi Baru")})
	require.NoError(t, err)
	require.Equal(t, "Budi Baru", got.DisplayName)
	require.Equal(t, before.FirstName, got.FirstName)
	require.Equal(t, before.LastName, got.LastName)
	require.Equal(t, before.Phone, got.Phone)
	require.Equal(t, before.AvatarURL, got.AvatarURL)
	require.Equal(t, before.Locale, got.Locale)
	require.Equal(t, before.Timezone, got.Timezone)
	require.False(t, got.UpdatedAt.Before(before.UpdatedAt), "updated_at tidak mundur")

	got, err = repo.Update(bg, plat, u.ID, domain.UpdateInput{Phone: strPtr(""), AvatarURL: strPtr(""), FirstName: strPtr("")})
	require.NoError(t, err)
	require.Nil(t, got.Phone, "string kosong menghapus phone")
	require.Nil(t, got.AvatarURL)
	require.Nil(t, got.FirstName)
	require.NotNil(t, got.LastName, "last_name tidak disebut, tidak berubah")
	require.Equal(t, "Santoso", *got.LastName)
	require.Equal(t, "Budi Baru", got.DisplayName)

	got, err = repo.Update(bg, plat, u.ID, domain.UpdateInput{Locale: strPtr("en"), Timezone: strPtr("UTC"), LastName: strPtr("Kusuma")})
	require.NoError(t, err)
	require.Equal(t, "en", got.Locale)
	require.Equal(t, "UTC", got.Timezone)
	require.NotNil(t, got.LastName)
	require.Equal(t, "Kusuma", *got.LastName)
}

func TestUpdateNeverTouchesProtectedColumns(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	u := testkit.NewUser(t, db)
	fillUser(t, db, u, "budi.s", "Budi", "Santoso", "+62 812 3456 7890", "https://cdn.example.org/a.png")
	execSQL(t, db, `UPDATE users SET last_login_at = now() WHERE id = $1`, u.ID.String())
	setUserStatus(t, db, u, "suspended")
	before := protectedColumns(t, db, u)

	in := domain.UpdateInput{}
	in.DisplayName = strPtr("A")
	in.FirstName = strPtr("B")
	in.LastName = strPtr("C")
	in.Phone = strPtr("12345")
	in.AvatarURL = strPtr("https://cdn.example.org/b.png")
	in.Locale = strPtr("en")
	in.Timezone = strPtr("UTC")
	_, err := repo.Update(bg, orgdomain.PlatformAccess(), u.ID, in)
	require.NoError(t, err)
	require.Equal(t, before, protectedColumns(t, db, u), "id, email, username, status, last_login_at, created_at, deleted_at utuh")

	_, err = repo.UpdateSelf(bg, u.ID, in)
	require.NoError(t, err)
	require.Equal(t, before, protectedColumns(t, db, u), "jalur Self juga tidak menyentuh kolom terlindung")
}

func TestUpdateMissingSoftDeletedAndInvalidInput(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	plat := orgdomain.PlatformAccess()
	u := testkit.NewUser(t, db)
	gone := testkit.NewUser(t, db)
	softDeleteUser(t, db, gone)
	goneBefore := protectedColumns(t, db, gone)
	in := domain.UpdateInput{DisplayName: strPtr("Baru")}

	_, err := repo.Update(bg, plat, id.NewID(), in)
	require.ErrorIs(t, err, domain.ErrUserNotFound)
	_, err = repo.Update(bg, plat, gone.ID, in)
	require.ErrorIs(t, err, domain.ErrUserNotFound, "soft-deleted tidak dapat diubah")
	require.Equal(t, goneBefore, protectedColumns(t, db, gone))
	var displayName string
	require.NoError(t, db.Migrator.QueryRow(bg, `SELECT display_name FROM users WHERE id = $1`, gone.ID.String()).Scan(&displayName))
	require.Equal(t, gone.DisplayName, displayName, "baris soft-deleted tidak berubah")

	_, err = repo.Update(bg, plat, uuid.Nil, in)
	require.ErrorIs(t, err, domain.ErrInvalidID)
	_, err = repo.Update(bg, plat, u.ID, domain.UpdateInput{})
	require.ErrorIs(t, err, domain.ErrNothingToUpdate)
	_, err = repo.Update(bg, plat, u.ID, domain.UpdateInput{Phone: strPtr("abc")})
	require.ErrorIs(t, err, domain.ErrInvalidPhone)
	_, err = repo.Update(bg, plat, u.ID, domain.UpdateInput{DisplayName: strPtr("Baik"), Timezone: strPtr("Mars/Base")})
	require.ErrorIs(t, err, domain.ErrInvalidTimezone)
	require.Equal(t, u.DisplayName, mustSelf(t, db, u).DisplayName, "masukan cacat tidak menulis sebagian")
}

func TestUpdateSelfChangesOnlyTheOwnRow(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	org := testkit.NewOrganization(t, db)
	me := testkit.NewUser(t, db, testkit.InOrganization(org))
	other := testkit.NewUser(t, db, testkit.InOrganization(org))
	loner := testkit.NewUser(t, db)
	otherBefore := mustSelf(t, db, other)

	got, err := repo.UpdateSelf(bg, me.ID, domain.UpdateInput{DisplayName: strPtr("Saya Baru")})
	require.NoError(t, err)
	require.Equal(t, me.ID, got.ID)
	require.Equal(t, "Saya Baru", got.DisplayName)
	require.Equal(t, otherBefore, mustSelf(t, db, other), "baris user lain tidak tersentuh")

	_, err = repo.UpdateSelf(bg, loner.ID, domain.UpdateInput{DisplayName: strPtr("Tanpa Organisasi")})
	require.NoError(t, err, "profil milik sendiri tidak bergantung pada keanggotaan")

	softDeleteUser(t, db, loner)
	_, err = repo.UpdateSelf(bg, loner.ID, domain.UpdateInput{DisplayName: strPtr("Lagi")})
	require.ErrorIs(t, err, domain.ErrUserNotFound)
	_, err = repo.UpdateSelf(bg, uuid.Nil, domain.UpdateInput{DisplayName: strPtr("Lagi")})
	require.ErrorIs(t, err, domain.ErrInvalidID)
	_, err = repo.UpdateSelf(bg, me.ID, domain.UpdateInput{})
	require.ErrorIs(t, err, domain.ErrNothingToUpdate)
}

func TestForUpdateRequiresATransactionAndIsScoped(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	userA := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	userB := testkit.NewUser(t, db, testkit.InOrganization(orgB))
	accA := tenantOf(t, orgA)

	_, err := repo.ForUpdate(bg, accA, userA.ID)
	require.ErrorIs(t, err, domain.ErrNoTransaction)
	_, err = repo.ForUpdateSelf(bg, userA.ID)
	require.ErrorIs(t, err, domain.ErrNoTransaction)

	err = kernel.NewTxManager(db.App, nil).WithinTx(bg, func(ctx context.Context) error {
		rec, ferr := repo.ForUpdate(ctx, accA, userA.ID)
		require.NoError(t, ferr)
		require.Equal(t, userA.ID, rec.ID)
		_, ferr = repo.ForUpdate(ctx, accA, userB.ID)
		require.ErrorIs(t, ferr, domain.ErrUserNotFound, "A tidak mengunci user B")
		self, ferr := repo.ForUpdateSelf(ctx, userB.ID)
		require.NoError(t, ferr)
		require.Equal(t, userB.ID, self.ID)
		return nil
	})
	require.NoError(t, err)
}

func TestUpdateJoinsTheTransactionAndRollsBack(t *testing.T) {
	db := testkit.NewDB(t)
	repo := userRepo(db)
	u := testkit.NewUser(t, db)
	sentinel := errors.New("batalkan")

	err := kernel.NewTxManager(db.App, nil).WithinTx(bg, func(ctx context.Context) error {
		_, uerr := repo.Update(ctx, orgdomain.PlatformAccess(), u.ID, domain.UpdateInput{DisplayName: strPtr("Tidak Jadi")})
		require.NoError(t, uerr)
		_, uerr = repo.UpdateSelf(ctx, u.ID, domain.UpdateInput{Locale: strPtr("en")})
		require.NoError(t, uerr)
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	after := mustSelf(t, db, u)
	require.Equal(t, u.DisplayName, after.DisplayName, "rollback membatalkan ubah")
	require.Equal(t, "en-US", after.Locale)
}
