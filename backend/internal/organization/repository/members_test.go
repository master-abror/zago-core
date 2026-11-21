package repository_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authzrepo "github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/internal/organization/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func memberRepo(db *testpg.DB) *repository.OrganizationMembers {
	return repository.NewOrganizationMembers(db.App)
}

// platformAdd menambahkan u ke o dengan akses platform; gagal bila ditolak.
func platformAdd(t *testing.T, db *testpg.DB, o testkit.Organization, u testkit.User) domain.Member {
	t.Helper()
	m, err := memberRepo(db).Add(bg, domain.PlatformAccess(), o.ID, u.ID, nil)
	require.NoError(t, err)
	return m
}

func setMembershipStatus(t *testing.T, db *testpg.DB, o testkit.Organization, u testkit.User, status string) {
	t.Helper()
	tag, err := db.Migrator.Exec(bg,
		`UPDATE organization_memberships SET status = $3 WHERE organization_id = $1 AND user_id = $2`,
		o.ID.String(), u.ID.String(), status)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected())
}

func setUserStatus(t *testing.T, db *testpg.DB, u testkit.User, status string) {
	t.Helper()
	_, err := db.Migrator.Exec(bg, `UPDATE users SET status = $2 WHERE id = $1`, u.ID.String(), status)
	require.NoError(t, err)
}

func softDeleteUser(t *testing.T, db *testpg.DB, u testkit.User) {
	t.Helper()
	_, err := db.Migrator.Exec(bg, `UPDATE users SET deleted_at = now() WHERE id = $1`, u.ID.String())
	require.NoError(t, err)
}

func membershipRows(t *testing.T, db *testpg.DB, o testkit.Organization, u testkit.User) int {
	t.Helper()
	var n int
	require.NoError(t, db.Migrator.QueryRow(bg,
		`SELECT count(*) FROM organization_memberships WHERE organization_id = $1 AND user_id = $2`,
		o.ID.String(), u.ID.String()).Scan(&n))
	return n
}

func membershipStatus(t *testing.T, db *testpg.DB, o testkit.Organization, u testkit.User) string {
	t.Helper()
	var status string
	require.NoError(t, db.Migrator.QueryRow(bg,
		`SELECT status FROM organization_memberships WHERE organization_id = $1 AND user_id = $2`,
		o.ID.String(), u.ID.String()).Scan(&status))
	return status
}

func userRowStatus(t *testing.T, db *testpg.DB, u testkit.User) string {
	t.Helper()
	var status string
	require.NoError(t, db.Migrator.QueryRow(bg, `SELECT status FROM users WHERE id = $1`, u.ID.String()).Scan(&status))
	return status
}

func groupStatus(t *testing.T, db *testpg.DB, g testkit.Group, u testkit.User) string {
	t.Helper()
	var status string
	require.NoError(t, db.Migrator.QueryRow(bg,
		`SELECT status FROM group_memberships WHERE group_id = $1 AND user_id = $2`,
		g.ID.String(), u.ID.String()).Scan(&status))
	return status
}

// isAuthzMember adalah jawaban mesin otorisasi (definisi "anggota aktif" milik authorization).
func isAuthzMember(t *testing.T, db *testpg.DB, o testkit.Organization, u testkit.User) bool {
	t.Helper()
	ok, err := authzrepo.NewMembers(db.App).IsActiveMember(bg, o.ID, u.ID)
	require.NoError(t, err)
	return ok
}

func memberUserIDs(items []domain.Member) []uuid.UUID {
	out := []uuid.UUID{}
	for _, m := range items {
		out = append(out, m.UserID)
	}
	return out
}

func TestAddCreatesAnActiveMembershipForPlatform(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	admin := testkit.NewUser(t, db)
	u := testkit.NewUser(t, db)

	m, err := memberRepo(db).Add(bg, domain.PlatformAccess(), org.ID, u.ID, &admin.ID)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, m.ID)
	require.Equal(t, org.ID, m.OrganizationID)
	require.Equal(t, u.ID, m.UserID)
	require.Equal(t, domain.MemberActive, m.Status)
	require.NotNil(t, m.JoinedAt)
	require.NotNil(t, m.InvitedBy)
	require.Equal(t, admin.ID, *m.InvitedBy)
	require.Equal(t, u.DisplayName, m.DisplayName)
	require.Equal(t, "active", m.UserStatus)
	require.True(t, isAuthzMember(t, db, org, u), "mesin otorisasi harus melihat anggota baru sebagai aktif")
}

func TestAddByTenantIsRefusedEvenForItsOwnOrganization(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db)

	_, err := memberRepo(db).Add(bg, tenantOf(t, org), org.ID, u.ID, nil)
	require.ErrorIs(t, err, domain.ErrPlatformRequired)
	require.Zero(t, membershipRows(t, db, org, u), "tidak ada yang ditulis")

	_, err = memberRepo(db).Add(bg, domain.Access{}, org.ID, u.ID, nil)
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	require.Zero(t, membershipRows(t, db, org, u))
}

func TestAddRejectsIneligibleUsersWithOneError(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	suspended := testkit.NewUser(t, db)
	setUserStatus(t, db, suspended, "suspended")
	deactivated := testkit.NewUser(t, db)
	setUserStatus(t, db, deactivated, "deactivated")
	deleted := testkit.NewUser(t, db)
	softDeleteUser(t, db, deleted)
	unknown := testkit.User{ID: id.NewID()}

	for _, u := range []testkit.User{suspended, deactivated, deleted, unknown} {
		_, err := memberRepo(db).Add(bg, domain.PlatformAccess(), org.ID, u.ID, nil)
		require.ErrorIs(t, err, domain.ErrUserNotEligible, "user %s", u.ID)
		require.Zero(t, membershipRows(t, db, org, u), "user %s", u.ID)
	}
}

func TestAddTwiceReportsMemberExistsForEveryOpenStatus(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db)
	platformAdd(t, db, org, u)

	for _, status := range []string{domain.MemberActive, domain.MemberInvited, domain.MemberSuspended} {
		setMembershipStatus(t, db, org, u, status)
		_, err := memberRepo(db).Add(bg, domain.PlatformAccess(), org.ID, u.ID, nil)
		require.ErrorIs(t, err, domain.ErrMemberExists, "status %s", status)
		require.Equal(t, 1, membershipRows(t, db, org, u))
		require.Equal(t, status, membershipStatus(t, db, org, u), "baris tidak boleh berubah")
	}
}

func TestAddToAnUnknownOrganizationIsNotFound(t *testing.T) {
	db := testkit.NewDB(t)
	u := testkit.NewUser(t, db)

	_, err := memberRepo(db).Add(bg, domain.PlatformAccess(), id.NewID(), u.ID, nil)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound)
}

func TestAddReactivatesARemovedMembershipOnTheSameRow(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	first := testkit.NewUser(t, db)
	second := testkit.NewUser(t, db)
	u := testkit.NewUser(t, db)

	m1, err := memberRepo(db).Add(bg, domain.PlatformAccess(), org.ID, u.ID, &first.ID)
	require.NoError(t, err)
	_, err = memberRepo(db).Remove(bg, domain.PlatformAccess(), org.ID, u.ID)
	require.NoError(t, err)
	require.False(t, isAuthzMember(t, db, org, u))

	m2, err := memberRepo(db).Add(bg, domain.PlatformAccess(), org.ID, u.ID, &second.ID)
	require.NoError(t, err)
	require.Equal(t, m1.ID, m2.ID, "baris yang sama dipakai ulang (uq_org_memberships)")
	require.Equal(t, domain.MemberActive, m2.Status)
	require.NotNil(t, m2.InvitedBy)
	require.Equal(t, second.ID, *m2.InvitedBy)
	require.Equal(t, 1, membershipRows(t, db, org, u))
	require.True(t, isAuthzMember(t, db, org, u))
}

func TestAddDoesNotReactivateARemovedMembershipOfAnIneligibleUser(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db)
	platformAdd(t, db, org, u)
	_, err := memberRepo(db).Remove(bg, domain.PlatformAccess(), org.ID, u.ID)
	require.NoError(t, err)
	setUserStatus(t, db, u, "suspended")

	_, err = memberRepo(db).Add(bg, domain.PlatformAccess(), org.ID, u.ID, nil)
	require.ErrorIs(t, err, domain.ErrUserNotEligible)
	require.Equal(t, domain.MemberRemoved, membershipStatus(t, db, org, u))
}

func TestAddUniquenessIsPerOrganization(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db)

	mA := platformAdd(t, db, orgA, u)
	mB := platformAdd(t, db, orgB, u)
	require.NotEqual(t, mA.ID, mB.ID)
	require.Equal(t, orgA.ID, mA.OrganizationID)
	require.Equal(t, orgB.ID, mB.OrganizationID)

	_, err := memberRepo(db).Add(bg, domain.PlatformAccess(), orgA.ID, u.ID, nil)
	require.ErrorIs(t, err, domain.ErrMemberExists)
	require.Equal(t, 1, membershipRows(t, db, orgA, u))
	require.Equal(t, 1, membershipRows(t, db, orgB, u))
}

func TestRemoveMarksRemovedAndKeepsTheRowAndTheUser(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db, testkit.InOrganization(org))
	require.True(t, isAuthzMember(t, db, org, u))

	m, err := memberRepo(db).Remove(bg, tenantOf(t, org), org.ID, u.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MemberRemoved, m.Status)
	require.Equal(t, u.ID, m.UserID)
	require.Equal(t, 1, membershipRows(t, db, org, u), "baris tetap ada")
	require.Equal(t, domain.MemberRemoved, membershipStatus(t, db, org, u))
	require.Equal(t, "active", userRowStatus(t, db, u), "user tidak dihapus atau diubah")
	require.False(t, isAuthzMember(t, db, org, u))

	_, again := memberRepo(db).Remove(bg, tenantOf(t, org), org.ID, u.ID)
	require.ErrorIs(t, again, domain.ErrMemberNotFound)
	_, never := memberRepo(db).Remove(bg, tenantOf(t, org), org.ID, id.NewID())
	require.ErrorIs(t, never, domain.ErrMemberNotFound)
	require.Equal(t, again.Error(), never.Error(), "sudah dihapus dan tidak pernah ada tidak boleh dapat dibedakan")
}

func TestRemoveIsScopedToTheCallerOrganizationInBothDirections(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	userA := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	userB := testkit.NewUser(t, db, testkit.InOrganization(orgB))
	shared := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	testkit.AddOrganizationMember(t, db, orgB, shared)

	_, err := memberRepo(db).Remove(bg, tenantOf(t, orgB), orgA.ID, userA.ID)
	require.ErrorIs(t, err, domain.ErrMemberNotFound)
	_, err = memberRepo(db).Remove(bg, tenantOf(t, orgA), orgB.ID, userB.ID)
	require.ErrorIs(t, err, domain.ErrMemberNotFound)
	require.True(t, isAuthzMember(t, db, orgA, userA), "keanggotaan A tidak tersentuh")
	require.True(t, isAuthzMember(t, db, orgB, userB), "keanggotaan B tidak tersentuh")

	// Pemanggil tenant yang menunjuk organisasinya sendiri tidak dapat menghapus anggota
	// organisasi lain: user bersama hanya kehilangan keanggotaan di organisasi pemanggil.
	_, err = memberRepo(db).Remove(bg, tenantOf(t, orgA), orgA.ID, userB.ID)
	require.ErrorIs(t, err, domain.ErrMemberNotFound)
	_, err = memberRepo(db).Remove(bg, tenantOf(t, orgA), orgA.ID, shared.ID)
	require.NoError(t, err)
	require.False(t, isAuthzMember(t, db, orgA, shared))
	require.True(t, isAuthzMember(t, db, orgB, shared), "keanggotaan shared di B tidak boleh ikut hilang")
	require.True(t, isAuthzMember(t, db, orgB, userB))
}

func TestPlatformCanRemoveFromAnyOrganization(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db, testkit.InOrganization(org))

	m, err := memberRepo(db).Remove(bg, domain.PlatformAccess(), org.ID, u.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MemberRemoved, m.Status)
}

func TestMemberGetIsScopedToTheCallerOrganizationInBothDirections(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	userA := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	userB := testkit.NewUser(t, db, testkit.InOrganization(orgB))
	repo := memberRepo(db)

	got, err := repo.Get(bg, tenantOf(t, orgA), orgA.ID, userA.ID)
	require.NoError(t, err)
	require.Equal(t, userA.ID, got.UserID)
	require.Equal(t, orgA.ID, got.OrganizationID)

	_, crossAB := repo.Get(bg, tenantOf(t, orgB), orgA.ID, userA.ID)
	require.ErrorIs(t, crossAB, domain.ErrMemberNotFound)
	_, crossBA := repo.Get(bg, tenantOf(t, orgA), orgB.ID, userB.ID)
	require.ErrorIs(t, crossBA, domain.ErrMemberNotFound)
	_, wrongOrg := repo.Get(bg, domain.PlatformAccess(), orgA.ID, userB.ID)
	require.ErrorIs(t, wrongOrg, domain.ErrMemberNotFound, "user B bukan anggota A bahkan bagi platform")

	_, missing := repo.Get(bg, tenantOf(t, orgB), orgB.ID, id.NewID())
	require.ErrorIs(t, missing, domain.ErrMemberNotFound)
	require.Equal(t, crossAB.Error(), missing.Error(), "milik organisasi lain dan tidak ada tidak boleh dapat dibedakan")
}

func TestGetReturnsRemovedMembershipsWithTheirStatus(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db, testkit.InOrganization(org))
	_, err := memberRepo(db).Remove(bg, domain.PlatformAccess(), org.ID, u.ID)
	require.NoError(t, err)

	got, err := memberRepo(db).Get(bg, tenantOf(t, org), org.ID, u.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MemberRemoved, got.Status)
}

func TestInvalidAccessAndIDsAreRejectedBeforeAnyQuery(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db, testkit.InOrganization(org))
	repo := memberRepo(db)
	zero := domain.Access{}

	_, err := repo.Get(bg, zero, org.ID, u.ID)
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	_, err = repo.List(bg, zero, org.ID, domain.MemberListQuery{})
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	_, err = repo.Remove(bg, zero, org.ID, u.ID)
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	_, err = repo.RemoveFromGroups(bg, zero, org.ID, u.ID)
	require.ErrorIs(t, err, domain.ErrInvalidAccess)

	platform := domain.PlatformAccess()
	_, err = repo.Get(bg, platform, uuid.Nil, u.ID)
	require.ErrorIs(t, err, domain.ErrInvalidID)
	_, err = repo.Get(bg, platform, org.ID, uuid.Nil)
	require.ErrorIs(t, err, domain.ErrInvalidID)
	_, err = repo.List(bg, platform, uuid.Nil, domain.MemberListQuery{})
	require.ErrorIs(t, err, domain.ErrInvalidID)
	_, err = repo.Remove(bg, platform, org.ID, uuid.Nil)
	require.ErrorIs(t, err, domain.ErrInvalidID)
	_, err = repo.Add(bg, platform, uuid.Nil, u.ID, nil)
	require.ErrorIs(t, err, domain.ErrInvalidID)
	_, err = repo.Add(bg, platform, org.ID, uuid.Nil, nil)
	require.ErrorIs(t, err, domain.ErrInvalidID)
	_, err = repo.RemoveFromGroups(bg, platform, org.ID, uuid.Nil)
	require.ErrorIs(t, err, domain.ErrInvalidID)
}

func TestListIsScopedToTheCallerOrganizationInBothDirections(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	a1 := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	a2 := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	b1 := testkit.NewUser(t, db, testkit.InOrganization(orgB))
	shared := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	testkit.AddOrganizationMember(t, db, orgB, shared)
	repo := memberRepo(db)

	pageA, err := repo.List(bg, tenantOf(t, orgA), orgA.ID, domain.MemberListQuery{})
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{a1.ID, a2.ID, shared.ID}, memberUserIDs(pageA.Items))
	for _, m := range pageA.Items {
		require.Equal(t, orgA.ID, m.OrganizationID, "baris organisasi lain tidak boleh muncul")
	}

	pageB, err := repo.List(bg, tenantOf(t, orgB), orgB.ID, domain.MemberListQuery{})
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{b1.ID, shared.ID}, memberUserIDs(pageB.Items))

	_, err = repo.List(bg, tenantOf(t, orgB), orgA.ID, domain.MemberListQuery{})
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound)
	_, err = repo.List(bg, tenantOf(t, orgA), orgB.ID, domain.MemberListQuery{})
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound)
	_, unknown := repo.List(bg, tenantOf(t, orgA), id.NewID(), domain.MemberListQuery{})
	require.ErrorIs(t, unknown, domain.ErrOrganizationNotFound)

	platformB, err := repo.List(bg, domain.PlatformAccess(), orgB.ID, domain.MemberListQuery{})
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{b1.ID, shared.ID}, memberUserIDs(platformB.Items))
}

func TestListOfAnOrganizationWithoutMembersIsAnEmptyNonNilPage(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)

	page, err := memberRepo(db).List(bg, domain.PlatformAccess(), org.ID, domain.MemberListQuery{})
	require.NoError(t, err)
	require.NotNil(t, page.Items)
	require.Empty(t, page.Items)
	require.Empty(t, page.NextCursor)
}

func TestListPaginatesWithCursorWithoutGapsOrDuplicates(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	want := []uuid.UUID{}
	for i := 0; i < 5; i++ {
		want = append(want, testkit.NewUser(t, db, testkit.InOrganization(org)).ID)
	}
	repo := memberRepo(db)
	acc := tenantOf(t, org)

	all := []domain.Member{}
	cursor := ""
	pages := 0
	for {
		page, err := repo.List(bg, acc, org.ID, domain.MemberListQuery{Limit: 2, Cursor: cursor})
		require.NoError(t, err)
		require.LessOrEqual(t, len(page.Items), 2)
		all = append(all, page.Items...)
		pages++
		require.Less(t, pages, 10, "paginasi tidak boleh berputar")
		if page.NextCursor == "" {
			break
		}
		require.Len(t, page.Items, 2, "halaman yang masih punya kelanjutan harus penuh")
		cursor = page.NextCursor
	}
	require.Equal(t, 3, pages)
	require.ElementsMatch(t, want, memberUserIDs(all), "semua anggota muncul tepat satu kali")
	for i := 1; i < len(all); i++ {
		prev := all[i-1]
		cur := all[i]
		ordered := prev.CreatedAt.Before(cur.CreatedAt) ||
			(prev.CreatedAt.Equal(cur.CreatedAt) && prev.ID.String() < cur.ID.String())
		require.True(t, ordered, "urutan (created_at, id) harus naik pada baris %d", i)
	}
}

func TestListHasNoNextCursorWhenThePageExactlyFitsTheLimit(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	for i := 0; i < 2; i++ {
		testkit.NewUser(t, db, testkit.InOrganization(org))
	}

	page, err := memberRepo(db).List(bg, tenantOf(t, org), org.ID, domain.MemberListQuery{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	require.Empty(t, page.NextCursor)
}

func TestListFiltersByStatus(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	active := testkit.NewUser(t, db, testkit.InOrganization(org))
	suspended := testkit.NewUser(t, db, testkit.InOrganization(org))
	invited := testkit.NewUser(t, db, testkit.InOrganization(org))
	removed := testkit.NewUser(t, db, testkit.InOrganization(org))
	setMembershipStatus(t, db, org, suspended, domain.MemberSuspended)
	setMembershipStatus(t, db, org, invited, domain.MemberInvited)
	_, err := memberRepo(db).Remove(bg, domain.PlatformAccess(), org.ID, removed.ID)
	require.NoError(t, err)
	repo := memberRepo(db)
	acc := tenantOf(t, org)

	byFilter := func(filter string) []uuid.UUID {
		page, err := repo.List(bg, acc, org.ID, domain.MemberListQuery{Filter: filter})
		require.NoError(t, err, "filter %q", filter)
		return memberUserIDs(page.Items)
	}
	require.ElementsMatch(t, []uuid.UUID{active.ID, suspended.ID, invited.ID}, byFilter(""))
	require.ElementsMatch(t, []uuid.UUID{active.ID, suspended.ID, invited.ID}, byFilter(domain.FilterOpen))
	require.ElementsMatch(t, []uuid.UUID{active.ID, suspended.ID, invited.ID, removed.ID}, byFilter(domain.FilterAll))
	require.ElementsMatch(t, []uuid.UUID{removed.ID}, byFilter(domain.MemberRemoved))
	require.ElementsMatch(t, []uuid.UUID{active.ID}, byFilter(domain.MemberActive))

	_, err = repo.List(bg, acc, org.ID, domain.MemberListQuery{Filter: "bogus"})
	require.ErrorIs(t, err, domain.ErrInvalidFilter)
	_, err = repo.List(bg, acc, org.ID, domain.MemberListQuery{Cursor: "sampah!!"})
	require.ErrorIs(t, err, domain.ErrInvalidCursor)
}

func TestListCarriesUserIdentityButNeverTheEmail(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db, testkit.InOrganization(org))

	page, err := memberRepo(db).List(bg, tenantOf(t, org), org.ID, domain.MemberListQuery{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, u.DisplayName, page.Items[0].DisplayName)
	require.Equal(t, "active", page.Items[0].UserStatus)
	// domain.Member tidak punya bidang email (ADR-0023 butir 12); email dibaca lewat jalur user.
}

func TestListWithACursorFromAnotherOrganizationStaysInsideTheCallerOrganization(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	for i := 0; i < 3; i++ {
		testkit.NewUser(t, db, testkit.InOrganization(orgA))
		testkit.NewUser(t, db, testkit.InOrganization(orgB))
	}
	repo := memberRepo(db)

	pageA, err := repo.List(bg, tenantOf(t, orgA), orgA.ID, domain.MemberListQuery{Limit: 1})
	require.NoError(t, err)
	require.NotEmpty(t, pageA.NextCursor)

	pageB, err := repo.List(bg, tenantOf(t, orgB), orgB.ID, domain.MemberListQuery{Limit: 100, Cursor: pageA.NextCursor})
	require.NoError(t, err)
	for _, m := range pageB.Items {
		require.Equal(t, orgB.ID, m.OrganizationID)
	}
}

func TestRemoveFromGroupsTouchesOnlyTheCallerOrganization(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	creatorA := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	creatorB := testkit.NewUser(t, db, testkit.InOrganization(orgB))
	shared := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	testkit.AddOrganizationMember(t, db, orgB, shared)

	activeA := testkit.NewGroup(t, db, orgA, creatorA)
	suspendedA := testkit.NewGroup(t, db, orgA, creatorA)
	removedA := testkit.NewGroup(t, db, orgA, creatorA)
	groupB := testkit.NewGroup(t, db, orgB, creatorB)
	testkit.AddGroupMember(t, db, activeA, shared, domain.MemberActive)
	testkit.AddGroupMember(t, db, suspendedA, shared, domain.MemberSuspended)
	testkit.AddGroupMember(t, db, removedA, shared, domain.MemberRemoved)
	testkit.AddGroupMember(t, db, groupB, shared, domain.MemberActive)
	repo := memberRepo(db)

	n, err := repo.RemoveFromGroups(bg, tenantOf(t, orgB), orgA.ID, shared.ID)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound)
	require.Zero(t, n)
	require.Equal(t, domain.MemberActive, groupStatus(t, db, activeA, shared), "tenant lain tidak mengubah apa pun")

	n, err = repo.RemoveFromGroups(bg, tenantOf(t, orgA), orgA.ID, shared.ID)
	require.NoError(t, err)
	require.Equal(t, 2, n, "yang aktif dan yang ditangguhkan; yang sudah removed tidak dihitung")
	require.Equal(t, domain.MemberRemoved, groupStatus(t, db, activeA, shared))
	require.Equal(t, domain.MemberRemoved, groupStatus(t, db, suspendedA, shared))
	require.Equal(t, domain.MemberRemoved, groupStatus(t, db, removedA, shared))
	require.Equal(t, domain.MemberActive, groupStatus(t, db, groupB, shared), "grup organisasi lain tidak tersentuh")

	n, err = repo.RemoveFromGroups(bg, tenantOf(t, orgA), orgA.ID, shared.ID)
	require.NoError(t, err)
	require.Zero(t, n, "pemanggilan ulang tidak mengubah apa pun")
}

func TestMembershipDefinitionMatchesTheAuthorizationEngine(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	repo := memberRepo(db)
	acc := tenantOf(t, org)

	// agree: keanggotaan active dan user active menurut repository ini harus sama dengan jawaban
	// mesin otorisasi (satu definisi "anggota aktif").
	agree := func(u testkit.User, want bool) {
		t.Helper()
		m, err := repo.Get(bg, acc, org.ID, u.ID)
		require.NoError(t, err)
		mine := m.Status == domain.MemberActive && m.UserStatus == "active"
		require.Equal(t, want, mine, "definisi repository, user %s", u.ID)
		require.Equal(t, want, isAuthzMember(t, db, org, u), "definisi mesin otorisasi, user %s", u.ID)
	}

	ok := testkit.NewUser(t, db, testkit.InOrganization(org))
	agree(ok, true)

	memberSuspended := testkit.NewUser(t, db, testkit.InOrganization(org))
	setMembershipStatus(t, db, org, memberSuspended, domain.MemberSuspended)
	agree(memberSuspended, false)

	memberInvited := testkit.NewUser(t, db, testkit.InOrganization(org))
	setMembershipStatus(t, db, org, memberInvited, domain.MemberInvited)
	agree(memberInvited, false)

	userSuspended := testkit.NewUser(t, db, testkit.InOrganization(org))
	setUserStatus(t, db, userSuspended, "suspended")
	agree(userSuspended, false)

	removed := testkit.NewUser(t, db, testkit.InOrganization(org))
	_, err := repo.Remove(bg, acc, org.ID, removed.ID)
	require.NoError(t, err)
	agree(removed, false)

	// User yang sudah dihapus lunak: Status keanggotaan masih active, tetapi mesin otorisasi
	// menolaknya; repository ini tidak memuat deleted_at, jadi hanya jawaban mesin yang diperiksa.
	deleted := testkit.NewUser(t, db, testkit.InOrganization(org))
	softDeleteUser(t, db, deleted)
	require.False(t, isAuthzMember(t, db, org, deleted))
}
