package repository_test

import (
	"bytes"
	"sort"
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

// Tes jalur BACA assignment memakai PostgreSQL 18 sungguhan dan role app_user (db.App). Jalur baca
// ini memfilter tenant sendiri (tidak ada jalur lain yang melindunginya), jadi tes negatif lintas
// organisasi DUA ARAH dan terhadap scope platform ada di sini (docs/12 §6, ADR-0022 butir 49).

func arlReader(db *testpg.DB) *repository.AssignmentReader {
	return repository.NewAssignmentReader(db.App)
}

func arlIDs(rows []domain.AssignmentView) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func arlFull(now time.Time) domain.AssignmentListQuery {
	return domain.AssignmentListQuery{Status: domain.AssignmentFilterAll, Limit: domain.MaxPageSize + 1, Now: now}
}

// arlOrgAssignment menyisipkan assignment user pada scope organisasi org untuk subjek baru.
func arlOrgAssignment(t *testing.T, db *testpg.DB, role testkit.Role, org uuid.UUID) testkit.Assignment {
	t.Helper()
	return testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: id.NewID(), ScopeType: "organization", OrganizationID: &org})
}

type arlStamp struct {
	id uuid.UUID
	at time.Time
}

func TestAssignmentReaderListIsTenantFilteredInEveryDirection(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	role := testkit.NewRole(t, db)
	inA := arlOrgAssignment(t, db, role, orgA.ID)
	inB := arlOrgAssignment(t, db, role, orgB.ID)
	platform := testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: id.NewID(), ScopeType: "platform"})
	reader := arlReader(db)
	now := time.Now()

	rowsA, err := reader.List(bg, rrOrg(orgA), arlFull(now))
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{inA.ID}, arlIDs(rowsA), "tenant A hanya melihat assignment organisasinya")

	rowsB, err := reader.List(bg, rrOrg(orgB), arlFull(now))
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{inB.ID}, arlIDs(rowsB), "kebalikannya juga berlaku")

	rowsP, err := reader.List(bg, nil, arlFull(now))
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{platform.ID}, arlIDs(rowsP), "scope platform hanya melihat assignment platform, bukan isi organisasi")
}

func TestAssignmentReaderListReadsOrganizationAndGroupScopesBack(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	role := testkit.NewRole(t, db)
	orgID := org.ID
	groupID := id.NewID()
	canonical := testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: id.NewID(), ScopeType: "organization", OrganizationID: &orgID})
	selfScoped := testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: id.NewID(), ScopeType: "organization", ScopeID: &orgID, OrganizationID: &orgID})
	inGroup := testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: id.NewID(), ScopeType: "group", ScopeID: &groupID, OrganizationID: &orgID})

	rows, err := arlReader(db).List(bg, rrOrg(org), arlFull(time.Now()))

	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{canonical.ID, selfScoped.ID, inGroup.ID}, arlIDs(rows))
	byID := map[uuid.UUID]domain.AssignmentView{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	require.Equal(t, domain.OrganizationTarget(org.ID), byID[canonical.ID].Scope)
	require.Equal(t, domain.OrganizationTarget(org.ID), byID[selfScoped.ID].Scope, "kedua bentuk kolom scope organisasi dibaca sebagai target yang sama")
	require.Equal(t, domain.GroupTarget(org.ID, groupID), byID[inGroup.ID].Scope)
	require.Equal(t, domain.SubjectUser, byID[canonical.ID].SubjectType)
}

func TestAssignmentReaderListFiltersByStatusAndDefaultsToActive(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	role := testkit.NewRole(t, db)
	now := time.Now()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	mk := func(expires *time.Time, revoked bool) uuid.UUID {
		spec := testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: id.NewID(), ScopeType: "organization", OrganizationID: ptr(org.ID), ExpiresAt: expires, Revoked: revoked}
		return testkit.AssignRole(t, db, spec).ID
	}
	active := mk(nil, false)
	activeFuture := mk(&future, false)
	expired := mk(&past, false)
	revoked := mk(nil, true)
	revokedAndExpired := mk(&past, true)
	reader := arlReader(db)

	list := func(status string) []domain.AssignmentView {
		rows, err := reader.List(bg, rrOrg(org), domain.AssignmentListQuery{Status: status, Limit: 50, Now: now})
		require.NoError(t, err, status)
		return rows
	}

	require.ElementsMatch(t, []uuid.UUID{active, activeFuture}, arlIDs(list("")), "bawaan hanya assignment yang berlaku")
	require.ElementsMatch(t, []uuid.UUID{active, activeFuture}, arlIDs(list(domain.AssignmentFilterActive)))
	require.ElementsMatch(t, []uuid.UUID{revoked, revokedAndExpired}, arlIDs(list(domain.AssignmentFilterRevoked)))
	require.ElementsMatch(t, []uuid.UUID{expired}, arlIDs(list(domain.AssignmentFilterExpired)), "yang dicabut tidak dihitung kedaluwarsa")
	require.ElementsMatch(t, []uuid.UUID{active, activeFuture, expired, revoked, revokedAndExpired}, arlIDs(list(domain.AssignmentFilterAll)))
	for _, status := range []string{domain.AssignmentFilterActive, domain.AssignmentFilterRevoked, domain.AssignmentFilterExpired} {
		for _, r := range list(status) {
			require.Equal(t, status, r.State(now), "status baris harus sama dengan filter yang meloloskannya")
		}
	}
}

func TestAssignmentReaderListRejectsUnknownStatusInsteadOfListingEverything(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	arlOrgAssignment(t, db, testkit.NewRole(t, db), org.ID)

	rows, err := arlReader(db).List(bg, rrOrg(org), domain.AssignmentListQuery{Status: "bogus", Limit: 10, Now: time.Now()})

	require.Error(t, err)
	require.Empty(t, rows, "filter yang tidak dikenal tidak boleh jatuh ke 'semua'")
}

func TestAssignmentReaderListFiltersByRoleAndSubjectWithoutWideningTheScope(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	roleOne := testkit.NewRole(t, db)
	roleTwo := testkit.NewRole(t, db)
	subject := id.NewID()
	orgAID, orgBID := orgA.ID, orgB.ID
	oneForSubject := testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: roleOne, SubjectType: "user", SubjectID: subject, ScopeType: "organization", OrganizationID: &orgAID})
	twoForSubject := testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: roleTwo, SubjectType: "user", SubjectID: subject, ScopeType: "organization", OrganizationID: &orgAID})
	oneForOther := arlOrgAssignment(t, db, roleOne, orgA.ID)
	sameSubjectOtherOrg := testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: roleOne, SubjectType: "user", SubjectID: subject, ScopeType: "organization", OrganizationID: &orgBID})
	reader := arlReader(db)
	now := time.Now()

	byRole := arlFull(now)
	byRole.RoleID = &roleOne.ID
	rows, err := reader.List(bg, rrOrg(orgA), byRole)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{oneForSubject.ID, oneForOther.ID}, arlIDs(rows))

	bySubject := arlFull(now)
	bySubject.SubjectID = &subject
	rows, err = reader.List(bg, rrOrg(orgA), bySubject)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{oneForSubject.ID, twoForSubject.ID}, arlIDs(rows), "subjek yang sama di organisasi lain tidak ikut")
	require.NotContains(t, arlIDs(rows), sameSubjectOtherOrg.ID)

	both := arlFull(now)
	both.RoleID = &roleTwo.ID
	both.SubjectID = &subject
	rows, err = reader.List(bg, rrOrg(orgA), both)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{twoForSubject.ID}, arlIDs(rows), "kedua filter berlaku bersamaan")

	other := arlFull(now)
	other.SubjectID = &subject
	rows, err = reader.List(bg, rrOrg(orgB), other)
	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{sameSubjectOtherOrg.ID}, arlIDs(rows), "arah sebaliknya")
}

func TestAssignmentReaderListPagesByKeysetWithTiesAndCoversEverythingOnce(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	role := testkit.NewRole(t, db)
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	offsets := []time.Duration{0, time.Hour, time.Hour, 2 * time.Hour, -time.Hour}
	var want []arlStamp
	for _, off := range offsets {
		a := arlOrgAssignment(t, db, role, org.ID)
		at := base.Add(off)
		asExec(t, db, `UPDATE role_assignments SET created_at = $2 WHERE id = $1`, a.ID.String(), at)
		want = append(want, arlStamp{id: a.ID, at: at})
	}
	sort.Slice(want, func(i, j int) bool {
		if !want[i].at.Equal(want[j].at) {
			return want[i].at.After(want[j].at)
		}
		return bytes.Compare(want[i].id[:], want[j].id[:]) > 0
	})
	wantIDs := make([]uuid.UUID, 0, len(want))
	for _, w := range want {
		wantIDs = append(wantIDs, w.id)
	}
	reader := arlReader(db)

	var got []uuid.UUID
	var after *domain.AssignmentPosition
	pages := 0
	for {
		q := arlFull(time.Now())
		q.Limit = 2
		q.After = after
		rows, err := reader.List(bg, rrOrg(org), q)
		require.NoError(t, err)
		if len(rows) == 0 {
			break
		}
		pages++
		require.LessOrEqual(t, len(rows), 2)
		got = append(got, arlIDs(rows)...)
		last := rows[len(rows)-1]
		after = &domain.AssignmentPosition{CreatedAt: last.CreatedAt, ID: last.ID}
		require.Less(t, pages, 10, "paginasi tidak boleh berputar tanpa akhir")
	}

	require.Equal(t, wantIDs, got, "urutan (created_at, id) menurun, setiap baris tepat sekali, termasuk dua baris dengan created_at sama")
	require.Equal(t, 3, pages)
}

func TestAssignmentReaderAfterPositionNeverWidensTheScope(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	role := testkit.NewRole(t, db)
	inA := arlOrgAssignment(t, db, role, orgA.ID)
	inB := arlOrgAssignment(t, db, role, orgB.ID)
	q := arlFull(time.Now())
	q.After = &domain.AssignmentPosition{CreatedAt: time.Now().Add(24 * time.Hour), ID: id.NewID()}

	rowsB, err := arlReader(db).List(bg, rrOrg(orgB), q)

	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{inB.ID}, arlIDs(rowsB), "posisi dari halaman organisasi lain hanya mengurutkan, tidak membuka baris organisasi itu")
	require.NotContains(t, arlIDs(rowsB), inA.ID)
}

func TestAssignmentReaderListHonorsTheLimit(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	role := testkit.NewRole(t, db)
	for i := 0; i < 3; i++ {
		arlOrgAssignment(t, db, role, org.ID)
	}
	q := arlFull(time.Now())
	q.Limit = 2

	rows, err := arlReader(db).List(bg, rrOrg(org), q)

	require.NoError(t, err)
	require.Len(t, rows, 2)
}

func TestAssignmentReaderListCarriesTheRoleSummary(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	sys := testkit.NewRole(t, db)
	archived := testkit.NewRole(t, db, testkit.RoleInOrganization(org), testkit.RoleStatus("archived"))
	onSystem := arlOrgAssignment(t, db, sys, org.ID)
	onArchived := arlOrgAssignment(t, db, archived, org.ID)

	rows, err := arlReader(db).List(bg, rrOrg(org), arlFull(time.Now()))

	require.NoError(t, err)
	byID := map[uuid.UUID]domain.AssignmentView{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	require.Equal(t, "Role "+sys.ID.String(), byID[onSystem.ID].RoleName)
	require.Equal(t, sys.Slug, byID[onSystem.ID].RoleSlug)
	require.Equal(t, "active", byID[onSystem.ID].RoleStatus)
	require.Equal(t, archived.Slug, byID[onArchived.ID].RoleSlug)
	require.Equal(t, "archived", byID[onArchived.ID].RoleStatus, "role terarsip terlihat supaya UI tahu assignment itu tidak berlaku")
}

func TestAssignmentReaderListDoesNotLeakTheSummaryOfAnotherOrganizationsRole(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	foreign := testkit.NewRole(t, db, testkit.RoleInOrganization(orgB))
	// Jalur tulis tidak pernah membuat ini; fixture menulisnya langsung untuk membuktikan bahwa
	// kueri baca sendiri tidak membocorkan nama role organisasi lain.
	odd := arlOrgAssignment(t, db, foreign, orgA.ID)

	rows, err := arlReader(db).List(bg, rrOrg(orgA), arlFull(time.Now()))

	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, odd.ID, rows[0].ID)
	require.Empty(t, rows[0].RoleName, "nama role organisasi lain tidak boleh keluar")
	require.Empty(t, rows[0].RoleSlug)
	require.Empty(t, rows[0].RoleStatus)
}

func TestAssignmentReaderListHidesScopesThatTheAPICannotCreate(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	role := testkit.NewRole(t, db)
	orgID := org.ID
	resourceID := id.NewID()
	visible := arlOrgAssignment(t, db, role, org.ID)
	testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: id.NewID(), ScopeType: "own", OrganizationID: &orgID})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: id.NewID(), ScopeType: "resource", ScopeID: &resourceID, OrganizationID: &orgID})

	rows, err := arlReader(db).List(bg, rrOrg(org), arlFull(time.Now()))

	require.NoError(t, err, "baris scope resource/own tidak boleh membuat seluruh daftar gagal")
	require.Equal(t, []uuid.UUID{visible.ID}, arlIDs(rows))
}
