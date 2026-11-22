package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authzrepo "github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/organization/application"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	orgrepo "github.com/master-abror/zago-core/backend/internal/organization/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes ber-DB MemberService memakai komponen sungguhan (transaksi, kunci advisory milik
// authorization, repository anggota, organisasi, dan assignment). Audit dan event memakai fake,
// jadi yang dibuktikan di sini adalah keadaan database. Setiap jalur tulis diuji dengan DUA
// organisasi dan dua arah.

func newMemberDBService(t *testing.T, db *testpg.DB, inv application.TxInvalidator) (*application.MemberService, *fakeAudit, *fakeEvents) {
	t.Helper()
	tr := &trace{}
	au := &fakeAudit{trace: tr}
	ev := &fakeEvents{trace: tr}
	svc, err := application.NewMemberService(
		kernel.NewTxManager(db.App, nil),
		orgrepo.NewOrganizationMembers(db.App),
		orgrepo.NewOrganizations(db.App),
		authzrepo.NewAssignments(db.App),
		application.LockFunc(authzrepo.LockOrganization),
		au, ev, inv,
	)
	require.NoError(t, err)
	return svc, au, ev
}

func memberStatusOf(t *testing.T, db *testpg.DB, organizationID, userID uuid.UUID) string {
	t.Helper()
	var s string
	require.NoError(t, db.Migrator.QueryRow(bg,
		`SELECT status FROM organization_memberships WHERE organization_id = $1 AND user_id = $2`,
		organizationID.String(), userID.String()).Scan(&s))
	return s
}

func memberRowCount(t *testing.T, db *testpg.DB, organizationID, userID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, db.Migrator.QueryRow(bg,
		`SELECT count(*) FROM organization_memberships WHERE organization_id = $1 AND user_id = $2`,
		organizationID.String(), userID.String()).Scan(&n))
	return n
}

func groupMemberStatusOf(t *testing.T, db *testpg.DB, g testkit.Group, userID uuid.UUID) string {
	t.Helper()
	var s string
	require.NoError(t, db.Migrator.QueryRow(bg,
		`SELECT status FROM group_memberships WHERE group_id = $1 AND user_id = $2`,
		g.ID.String(), userID.String()).Scan(&s))
	return s
}

func activeAssignmentsOf(t *testing.T, db *testpg.DB, userID, organizationID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, db.Migrator.QueryRow(bg,
		`SELECT count(*) FROM role_assignments WHERE subject_id = $1 AND organization_id = $2 AND revoked_at IS NULL`,
		userID.String(), organizationID.String()).Scan(&n))
	return n
}

func assignmentActive(t *testing.T, db *testpg.DB, a testkit.Assignment) bool {
	t.Helper()
	var ok bool
	require.NoError(t, db.Migrator.QueryRow(bg,
		`SELECT revoked_at IS NULL FROM role_assignments WHERE id = $1`, a.ID.String()).Scan(&ok))
	return ok
}

// assignInOrg menyisipkan satu assignment aktif milik userID yang terikat organisasi org.
func assignInOrg(t *testing.T, db *testpg.DB, userID uuid.UUID, org testkit.Organization, scopeType string, scopeID *uuid.UUID) testkit.Assignment {
	t.Helper()
	orgID := org.ID
	spec := testkit.AssignmentSpec{Role: testkit.NewRole(t, db), SubjectType: "user", SubjectID: userID}
	spec.OrganizationID = &orgID
	spec.ScopeType = scopeType
	spec.ScopeID = scopeID
	return testkit.AssignRole(t, db, spec)
}

// assignPlatform menyisipkan satu assignment aktif ber-scope platform milik userID.
func assignPlatform(t *testing.T, db *testpg.DB, userID uuid.UUID) testkit.Assignment {
	t.Helper()
	spec := testkit.AssignmentSpec{Role: testkit.NewRole(t, db), SubjectType: "user", SubjectID: userID}
	spec.ScopeType = "platform"
	return testkit.AssignRole(t, db, spec)
}

// newOperator membuat user sungguhan sebagai pemanggil platform. Add menulis invited_by yang
// berkunci asing ke users, jadi pemanggil untuk Add harus user yang ada (di produksi pemanggil
// selalu user yang sudah terautentikasi).
func newOperator(t *testing.T, db *testpg.DB) application.Caller {
	t.Helper()
	return application.Caller{UserID: testkit.NewUser(t, db).ID}
}

// holdOrganizationLock menahan kunci advisory organisasi di transaksi lain sampai release
// dipanggil (tepat sekali). holderDone menerima hasil transaksi penahan.
func holdOrganizationLock(t *testing.T, db *testpg.DB, organizationID uuid.UUID) (func(), <-chan error) {
	t.Helper()
	held := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- kernel.NewTxManager(db.App, nil).WithinTx(bg, func(ctx context.Context) error {
			if err := authzrepo.LockOrganization(ctx, organizationID); err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	select {
	case <-held:
	case err := <-holderDone:
		require.FailNow(t, "penahan kunci berakhir sebelum menahan kunci", "err=%v", err)
	case <-time.After(30 * time.Second):
		require.FailNow(t, "kunci tidak pernah diambil")
	}
	return func() { close(release) }, holderDone
}

func TestMemberRemoveRevokesOnlyTheTargetOrganizationsAccess(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	testkit.AddOrganizationMember(t, db, orgB, user)
	bystander := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	groupA := testkit.NewGroup(t, db, orgA, user)
	groupB := testkit.NewGroup(t, db, orgB, user)
	testkit.AddGroupMember(t, db, groupA, user, "active")
	testkit.AddGroupMember(t, db, groupB, user, "active")
	testkit.AddGroupMember(t, db, groupA, bystander, "active")
	groupAID := groupA.ID
	assignInOrg(t, db, user.ID, orgA, "organization", nil)
	assignInOrg(t, db, user.ID, orgA, "group", &groupAID)
	inB := assignInOrg(t, db, user.ID, orgB, "organization", nil)
	platform := assignPlatform(t, db, user.ID)
	bystanderAssignment := assignInOrg(t, db, bystander.ID, orgA, "organization", nil)
	require.Equal(t, 2, activeAssignmentsOf(t, db, user.ID, orgA.ID))

	svc, au, ev := newMemberDBService(t, db, nil)
	got, err := svc.Remove(bg, platformCaller(), orgA.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MemberRemoved, got.Status)

	require.Equal(t, domain.MemberRemoved, memberStatusOf(t, db, orgA.ID, user.ID))
	require.Equal(t, domain.MemberActive, memberStatusOf(t, db, orgB.ID, user.ID), "organisasi lain tidak tersentuh")
	require.Equal(t, domain.MemberActive, memberStatusOf(t, db, orgA.ID, bystander.ID), "anggota lain tidak tersentuh")
	require.Equal(t, domain.MemberRemoved, groupMemberStatusOf(t, db, groupA, user.ID))
	require.Equal(t, domain.MemberActive, groupMemberStatusOf(t, db, groupB, user.ID), "grup organisasi lain tidak tersentuh")
	require.Equal(t, domain.MemberActive, groupMemberStatusOf(t, db, groupA, bystander.ID))
	require.Equal(t, 0, activeAssignmentsOf(t, db, user.ID, orgA.ID))
	require.True(t, assignmentActive(t, db, inB), "assignment di organisasi lain tetap")
	require.True(t, assignmentActive(t, db, platform), "assignment platform tetap")
	require.True(t, assignmentActive(t, db, bystanderAssignment), "assignment user lain tetap")

	require.Equal(t, 1, au.count())
	require.Equal(t, 1, ev.count())
	meta := au.entries[0].Metadata
	require.Equal(t, 1, meta["groups_removed"])
	require.Equal(t, 2, meta["assignments_revoked"])
}

func TestMemberServiceKeepsTenantsApartEndToEnd(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	userA := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	userB := testkit.NewUser(t, db, testkit.InOrganization(orgB))
	outsider := testkit.NewUser(t, db)
	assignA := assignInOrg(t, db, userA.ID, orgA, "organization", nil)
	assignB := assignInOrg(t, db, userB.ID, orgB, "organization", nil)
	svc, au, ev := newMemberDBService(t, db, nil)
	callerA := tenantCaller(t, orgA.ID)
	callerB := tenantCaller(t, orgB.ID)

	_, err := svc.Remove(bg, callerA, orgB.ID, userB.ID)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound, "A tidak menghapus anggota B")
	_, err = svc.Remove(bg, callerB, orgA.ID, userA.ID)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound, "B tidak menghapus anggota A")
	_, err = svc.Remove(bg, callerA, orgA.ID, userB.ID)
	require.ErrorIs(t, err, domain.ErrMemberNotFound, "user B bukan anggota A")
	_, err = svc.Remove(bg, callerB, orgB.ID, userA.ID)
	require.ErrorIs(t, err, domain.ErrMemberNotFound, "user A bukan anggota B")
	_, err = svc.Get(bg, callerA, orgB.ID, userB.ID)
	require.ErrorIs(t, err, domain.ErrMemberNotFound, "Get lintas organisasi menjadi anggota-tidak-ada (repository Get)")
	_, err = svc.List(bg, callerB, orgA.ID, domain.MemberListQuery{})
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound)
	_, err = svc.Add(bg, callerA, orgA.ID, outsider.ID)
	require.ErrorIs(t, err, domain.ErrPlatformRequired)
	_, err = svc.Add(bg, callerA, orgB.ID, outsider.ID)
	require.ErrorIs(t, err, domain.ErrPlatformRequired)

	require.Equal(t, domain.MemberActive, memberStatusOf(t, db, orgA.ID, userA.ID))
	require.Equal(t, domain.MemberActive, memberStatusOf(t, db, orgB.ID, userB.ID))
	require.True(t, assignmentActive(t, db, assignA))
	require.True(t, assignmentActive(t, db, assignB))
	require.Equal(t, 0, memberRowCount(t, db, orgA.ID, outsider.ID))
	require.Equal(t, 0, memberRowCount(t, db, orgB.ID, outsider.ID))
	require.Equal(t, 0, au.count(), "percobaan lintas organisasi tidak meninggalkan audit")
	require.Equal(t, 0, ev.count(), "dan tidak menerbitkan event")

	_, err = svc.Remove(bg, callerA, orgA.ID, callerA.UserID)
	require.ErrorIs(t, err, domain.ErrCannotRemoveSelf)

	_, err = svc.Remove(bg, callerA, orgA.ID, userA.ID)
	require.NoError(t, err, "tenant menghapus anggota organisasinya sendiri")
	require.Equal(t, domain.MemberRemoved, memberStatusOf(t, db, orgA.ID, userA.ID))
	require.False(t, assignmentActive(t, db, assignA))
	require.Equal(t, domain.MemberActive, memberStatusOf(t, db, orgB.ID, userB.ID))
	require.True(t, assignmentActive(t, db, assignB))
	require.Equal(t, 1, au.count())
	require.Equal(t, 1, ev.count())
}

func TestMemberRemoveWaitsForTheOrganizationAdvisoryLock(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db, testkit.InOrganization(org))
	svc, _, _ := newMemberDBService(t, db, nil)
	release, holderDone := holdOrganizationLock(t, db, org.ID)

	done := make(chan error, 1)
	go func() {
		_, err := svc.Remove(bg, platformCaller(), org.ID, user.ID)
		done <- err
	}()
	select {
	case err := <-done:
		release()
		require.FailNow(t, "Remove selesai padahal kunci organisasi ditahan transaksi lain", "err=%v", err)
	case <-time.After(300 * time.Millisecond):
	}
	require.Equal(t, domain.MemberActive, memberStatusOf(t, db, org.ID, user.ID), "belum ada perubahan selama kunci ditahan")

	release()
	require.NoError(t, awaitErr(t, holderDone))
	require.NoError(t, awaitErr(t, done))
	require.Equal(t, domain.MemberRemoved, memberStatusOf(t, db, org.ID, user.ID))
}

func TestMemberAddWaitsForTheOrganizationAdvisoryLock(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db)
	operator := newOperator(t, db)
	svc, _, _ := newMemberDBService(t, db, nil)
	release, holderDone := holdOrganizationLock(t, db, org.ID)

	done := make(chan error, 1)
	go func() {
		_, err := svc.Add(bg, operator, org.ID, user.ID)
		done <- err
	}()
	select {
	case err := <-done:
		release()
		require.FailNow(t, "Add selesai padahal kunci organisasi ditahan transaksi lain", "err=%v", err)
	case <-time.After(300 * time.Millisecond):
	}
	require.Equal(t, 0, memberRowCount(t, db, org.ID, user.ID), "belum ada baris selama kunci ditahan")

	release()
	require.NoError(t, awaitErr(t, holderDone))
	require.NoError(t, awaitErr(t, done))
	require.Equal(t, domain.MemberActive, memberStatusOf(t, db, org.ID, user.ID))
}

func TestMemberRemoveDoesNotWaitForAnotherOrganizationsLock(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	userB := testkit.NewUser(t, db, testkit.InOrganization(orgB))
	svc, _, _ := newMemberDBService(t, db, nil)
	release, holderDone := holdOrganizationLock(t, db, orgA.ID)

	done := make(chan error, 1)
	go func() {
		_, err := svc.Remove(bg, platformCaller(), orgB.ID, userB.ID)
		done <- err
	}()
	require.NoError(t, awaitErr(t, done), "kunci organisasi A tidak menghalangi organisasi B")
	release()
	require.NoError(t, awaitErr(t, holderDone))
	require.Equal(t, domain.MemberRemoved, memberStatusOf(t, db, orgB.ID, userB.ID))
}

func TestMemberConcurrentRemovesSucceedExactlyOnce(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db, testkit.InOrganization(org))
	assignment := assignInOrg(t, db, user.ID, org, "organization", nil)
	svc, au, ev := newMemberDBService(t, db, nil)

	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := svc.Remove(bg, platformCaller(), org.ID, user.ID)
			results <- err
		}()
	}
	succeeded, rejected := 0, 0
	for i := 0; i < 2; i++ {
		err := awaitErr(t, results)
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, domain.ErrMemberNotFound):
			rejected++
		default:
			require.NoError(t, err, "galat tak terduga")
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, rejected)
	require.Equal(t, 1, au.count(), "satu audit untuk satu penghapusan")
	require.Equal(t, 1, ev.count(), "satu event untuk satu penghapusan")
	require.Equal(t, domain.MemberRemoved, memberStatusOf(t, db, org.ID, user.ID))
	require.False(t, assignmentActive(t, db, assignment))
}

func TestMemberConcurrentAddsSucceedExactlyOnce(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db)
	operator := newOperator(t, db)
	svc, au, ev := newMemberDBService(t, db, nil)

	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := svc.Add(bg, operator, org.ID, user.ID)
			results <- err
		}()
	}
	succeeded, rejected := 0, 0
	for i := 0; i < 2; i++ {
		err := awaitErr(t, results)
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, domain.ErrMemberExists):
			rejected++
		default:
			require.NoError(t, err, "galat tak terduga")
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, rejected)
	require.Equal(t, 1, au.count())
	require.Equal(t, 1, ev.count())
	require.Equal(t, 1, memberRowCount(t, db, org.ID, user.ID))
}

func TestMemberRemoveRollsBackEverythingWhenInvalidationFails(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db, testkit.InOrganization(org))
	group := testkit.NewGroup(t, db, org, user)
	testkit.AddGroupMember(t, db, group, user, "active")
	groupID := group.ID
	orgWide := assignInOrg(t, db, user.ID, org, "organization", nil)
	groupWide := assignInOrg(t, db, user.ID, org, "group", &groupID)
	inv := &fakeInvalidator{trace: &trace{}, err: errors.New("redis mati")}
	svc, au, ev := newMemberDBService(t, db, inv)

	_, err := svc.Remove(bg, platformCaller(), org.ID, user.ID)
	require.ErrorIs(t, err, inv.err)
	require.Equal(t, domain.MemberActive, memberStatusOf(t, db, org.ID, user.ID), "keanggotaan dikembalikan")
	require.Equal(t, domain.MemberActive, groupMemberStatusOf(t, db, group, user.ID), "keanggotaan grup dikembalikan")
	require.True(t, assignmentActive(t, db, orgWide), "pencabutan assignment dikembalikan")
	require.True(t, assignmentActive(t, db, groupWide))
	require.Equal(t, 0, au.count())
	require.Equal(t, 0, ev.count())
}

func TestMemberAddRollsBackWhenInvalidationFails(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db)
	operator := newOperator(t, db)
	inv := &fakeInvalidator{trace: &trace{}, err: errors.New("redis mati")}
	svc, au, ev := newMemberDBService(t, db, inv)

	_, err := svc.Add(bg, operator, org.ID, user.ID)
	require.ErrorIs(t, err, inv.err)
	require.Equal(t, 0, memberRowCount(t, db, org.ID, user.ID), "baris keanggotaan tidak tertinggal")
	require.Equal(t, 0, au.count())
	require.Equal(t, 0, ev.count())
}

func TestMemberAddRulesEndToEnd(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	other := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db)
	suspended := testkit.NewUser(t, db)
	_, err := db.Migrator.Exec(bg, `UPDATE users SET status = 'suspended' WHERE id = $1`, suspended.ID.String())
	require.NoError(t, err)
	svc, au, ev := newMemberDBService(t, db, nil)
	plat := newOperator(t, db)

	got, err := svc.Add(bg, plat, org.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MemberActive, got.Status)
	require.Equal(t, user.ID, got.UserID)
	require.Equal(t, org.ID, got.OrganizationID)
	require.NotNil(t, got.InvitedBy)
	require.Equal(t, plat.UserID, *got.InvitedBy, "pemberi tercatat")
	require.Equal(t, 0, memberRowCount(t, db, other.ID, user.ID), "organisasi lain tidak ikut")

	_, err = svc.Add(bg, plat, org.ID, user.ID)
	require.ErrorIs(t, err, domain.ErrMemberExists)
	_, err = svc.Add(bg, plat, org.ID, suspended.ID)
	require.ErrorIs(t, err, domain.ErrUserNotEligible)
	_, err = svc.Add(bg, plat, org.ID, id.NewID())
	require.ErrorIs(t, err, domain.ErrUserNotEligible)
	_, err = svc.Add(bg, plat, id.NewID(), user.ID)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound)
	require.Equal(t, 0, memberRowCount(t, db, org.ID, suspended.ID))

	require.Equal(t, 1, au.count(), "hanya penambahan yang berhasil meninggalkan audit")
	require.Equal(t, 1, ev.count())
}

func TestMemberReAddAfterRemoveReusesTheRowAndDoesNotResurrectAccess(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	user := testkit.NewUser(t, db, testkit.InOrganization(org))
	group := testkit.NewGroup(t, db, org, user)
	testkit.AddGroupMember(t, db, group, user, "active")
	groupID := group.ID
	orgWide := assignInOrg(t, db, user.ID, org, "organization", nil)
	groupWide := assignInOrg(t, db, user.ID, org, "group", &groupID)
	svc, _, _ := newMemberDBService(t, db, nil)
	operator := newOperator(t, db)

	_, err := svc.Remove(bg, platformCaller(), org.ID, user.ID)
	require.NoError(t, err)
	got, err := svc.Add(bg, operator, org.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, domain.MemberActive, got.Status)

	require.Equal(t, 1, memberRowCount(t, db, org.ID, user.ID), "baris yang sama dipakai ulang")
	require.False(t, assignmentActive(t, db, orgWide), "assignment lama tidak hidup lagi")
	require.False(t, assignmentActive(t, db, groupWide))
	require.Equal(t, domain.MemberRemoved, groupMemberStatusOf(t, db, group, user.ID), "keanggotaan grup tidak dipulihkan")
	require.Equal(t, 0, activeAssignmentsOf(t, db, user.ID, org.ID))
}

func TestMemberWritesAreRejectedForAnArchivedOrganization(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	member := testkit.NewUser(t, db, testkit.InOrganization(org))
	candidate := testkit.NewUser(t, db)
	assignment := assignInOrg(t, db, member.ID, org, "organization", nil)
	_, err := db.Migrator.Exec(bg, `UPDATE organizations SET status = 'archived', deleted_at = now() WHERE id = $1`, org.ID.String())
	require.NoError(t, err)
	svc, au, ev := newMemberDBService(t, db, nil)

	_, err = svc.Add(bg, platformCaller(), org.ID, candidate.ID)
	require.ErrorIs(t, err, domain.ErrOrganizationArchived)
	_, err = svc.Remove(bg, platformCaller(), org.ID, member.ID)
	require.ErrorIs(t, err, domain.ErrOrganizationArchived)

	require.Equal(t, 0, memberRowCount(t, db, org.ID, candidate.ID))
	require.Equal(t, domain.MemberActive, memberStatusOf(t, db, org.ID, member.ID))
	require.True(t, assignmentActive(t, db, assignment))
	require.Equal(t, 0, au.count())
	require.Equal(t, 0, ev.count())
}
