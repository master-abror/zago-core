package repository_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/internal/organization/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

var bg = context.Background()

func orgRepo(db *testpg.DB) *repository.Organizations { return repository.NewOrganizations(db.App) }

func strPtr(s string) *string { return &s }

func tenantOf(t *testing.T, o testkit.Organization) domain.Access {
	t.Helper()
	a, err := domain.TenantAccess(o.ID)
	require.NoError(t, err)
	return a
}

func newRecord(t *testing.T, slug string) domain.Record {
	t.Helper()
	rec, err := domain.NewRecord(id.NewID(), domain.CreateInput{Name: "Org " + slug, Slug: slug})
	require.NoError(t, err)
	return rec
}

// mustGet membaca organisasi dengan akses platform; gagal bila tidak ada.
func mustGet(t *testing.T, db *testpg.DB, organizationID uuid.UUID) domain.Record {
	t.Helper()
	rec, err := orgRepo(db).Get(bg, domain.PlatformAccess(), organizationID)
	require.NoError(t, err)
	return rec
}

func containsID(p domain.Page, organizationID uuid.UUID) bool {
	for _, rec := range p.Items {
		if rec.ID == organizationID {
			return true
		}
	}
	return false
}

func countBySlug(t *testing.T, db *testpg.DB, slug string) int {
	t.Helper()
	var n int
	require.NoError(t, db.Migrator.QueryRow(bg, `SELECT count(*) FROM organizations WHERE slug = $1`, slug).Scan(&n))
	return n
}

func TestGetIsScopedToTheCallerOrganizationInBothDirections(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	accA := tenantOf(t, orgA)
	accB := tenantOf(t, orgB)

	got, err := repo.Get(bg, accA, orgA.ID)
	require.NoError(t, err)
	require.Equal(t, orgA.ID, got.ID)
	require.Equal(t, orgA.Slug, got.Slug)
	got, err = repo.Get(bg, accB, orgB.ID)
	require.NoError(t, err)
	require.Equal(t, orgB.ID, got.ID)

	_, err = repo.Get(bg, accA, orgB.ID)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound, "A tidak membaca B")
	_, err = repo.Get(bg, accB, orgA.ID)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound, "B tidak membaca A")

	plat := domain.PlatformAccess()
	_, err = repo.Get(bg, plat, orgA.ID)
	require.NoError(t, err, "platform membaca A")
	_, err = repo.Get(bg, plat, orgB.ID)
	require.NoError(t, err, "platform membaca B")
}

func TestGetOtherOrganizationIsIndistinguishableFromMissing(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)

	_, errOther := repo.Get(bg, tenantOf(t, orgA), orgB.ID)
	_, errMissing := repo.Get(bg, tenantOf(t, orgA), id.NewID())
	require.ErrorIs(t, errOther, domain.ErrOrganizationNotFound)
	require.ErrorIs(t, errMissing, domain.ErrOrganizationNotFound)
	require.Equal(t, errMissing.Error(), errOther.Error(), "pesan galat identik")

	_, errOther = repo.Get(bg, tenantOf(t, orgB), orgA.ID)
	require.ErrorIs(t, errOther, domain.ErrOrganizationNotFound)
	require.Equal(t, errMissing.Error(), errOther.Error(), "pesan galat identik arah sebaliknya")
}

func TestListSeesOnlyTheCallerOrganization(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)

	pageA, err := repo.List(bg, tenantOf(t, orgA), domain.ListQuery{})
	require.NoError(t, err)
	require.Equal(t, 1, pageA.Total)
	require.Len(t, pageA.Items, 1)
	require.Equal(t, orgA.ID, pageA.Items[0].ID)

	pageB, err := repo.List(bg, tenantOf(t, orgB), domain.ListQuery{})
	require.NoError(t, err)
	require.Equal(t, 1, pageB.Total)
	require.Len(t, pageB.Items, 1)
	require.Equal(t, orgB.ID, pageB.Items[0].ID)

	all, err := repo.List(bg, domain.PlatformAccess(), domain.ListQuery{})
	require.NoError(t, err)
	require.GreaterOrEqual(t, all.Total, 2)
	require.True(t, containsID(all, orgA.ID))
	require.True(t, containsID(all, orgB.ID))
}

func TestListFiltersByStatusAndPaginates(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	plat := domain.PlatformAccess()
	x := testkit.NewOrganization(t, db)
	y := testkit.NewOrganization(t, db)
	z := testkit.NewOrganization(t, db)
	_, err := repo.SetStatus(bg, plat, y.ID, domain.StatusArchived)
	require.NoError(t, err)
	_, err = repo.SetStatus(bg, plat, z.ID, domain.StatusSuspended)
	require.NoError(t, err)

	open, err := repo.List(bg, plat, domain.ListQuery{})
	require.NoError(t, err)
	require.True(t, containsID(open, x.ID))
	require.True(t, containsID(open, z.ID))
	require.False(t, containsID(open, y.ID), "bawaan tidak memuat organisasi terarsip")

	archived, err := repo.List(bg, plat, domain.ListQuery{Filter: domain.StatusArchived})
	require.NoError(t, err)
	require.True(t, containsID(archived, y.ID))
	require.False(t, containsID(archived, x.ID))
	require.False(t, containsID(archived, z.ID))

	active, err := repo.List(bg, plat, domain.ListQuery{Filter: domain.StatusActive})
	require.NoError(t, err)
	require.True(t, containsID(active, x.ID))
	require.False(t, containsID(active, y.ID))
	require.False(t, containsID(active, z.ID))

	suspended, err := repo.List(bg, plat, domain.ListQuery{Filter: domain.StatusSuspended})
	require.NoError(t, err)
	require.True(t, containsID(suspended, z.ID))
	require.False(t, containsID(suspended, x.ID))

	all, err := repo.List(bg, plat, domain.ListQuery{Filter: domain.FilterAll})
	require.NoError(t, err)
	require.True(t, containsID(all, x.ID))
	require.True(t, containsID(all, y.ID))
	require.True(t, containsID(all, z.ID))

	first, err := repo.List(bg, plat, domain.ListQuery{Filter: domain.FilterAll, Window: domain.Window{Limit: 1}})
	require.NoError(t, err)
	require.Len(t, first.Items, 1)
	require.GreaterOrEqual(t, first.Total, 3)
	second, err := repo.List(bg, plat, domain.ListQuery{Filter: domain.FilterAll, Window: domain.Window{Offset: 1, Limit: 1}})
	require.NoError(t, err)
	require.Len(t, second.Items, 1)
	require.NotEqual(t, first.Items[0].ID, second.Items[0].ID)

	_, err = repo.List(bg, plat, domain.ListQuery{Filter: "bogus"})
	require.ErrorIs(t, err, domain.ErrInvalidFilter)
}

func TestListArchivedOrganizationIsStillScopedToTheCaller(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	_, err := repo.SetStatus(bg, domain.PlatformAccess(), orgB.ID, domain.StatusArchived)
	require.NoError(t, err)

	page, err := repo.List(bg, tenantOf(t, orgA), domain.ListQuery{Filter: domain.FilterAll})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, orgA.ID, page.Items[0].ID)
}

func TestInsertCreatesOrganizationForPlatform(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	plat := domain.PlatformAccess()
	rec, err := domain.NewRecord(id.NewID(), domain.CreateInput{
		Name:            "Acme",
		Slug:            "acme-insert",
		LegalName:       strPtr("Acme Ltd"),
		DefaultLocale:   "id-ID",
		DefaultTimezone: "Asia/Jakarta",
	})
	require.NoError(t, err)

	got, err := repo.Insert(bg, plat, rec)
	require.NoError(t, err)
	require.Equal(t, rec.ID, got.ID)
	require.Equal(t, "Acme", got.Name)
	require.Equal(t, "acme-insert", got.Slug)
	require.NotNil(t, got.LegalName)
	require.Equal(t, "Acme Ltd", *got.LegalName)
	require.Equal(t, domain.StatusActive, got.Status)
	require.Equal(t, "id-ID", got.DefaultLocale)
	require.Equal(t, "Asia/Jakarta", got.DefaultTimezone)
	require.False(t, got.CreatedAt.IsZero())
	require.False(t, got.UpdatedAt.IsZero())
	require.Nil(t, got.ArchivedAt)

	read := mustGet(t, db, rec.ID)
	require.Equal(t, got.Slug, read.Slug)
	require.Equal(t, got.Name, read.Name)
}

func TestInsertIsRejectedForTenantAndZeroAccess(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	org := testkit.NewOrganization(t, db)
	rec := newRecord(t, "acme-denied")

	_, err := repo.Insert(bg, tenantOf(t, org), rec)
	require.ErrorIs(t, err, domain.ErrPlatformRequired)
	require.Equal(t, 0, countBySlug(t, db, "acme-denied"), "tenant tidak membuat organisasi")

	_, err = repo.Insert(bg, domain.Access{}, rec)
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	require.Equal(t, 0, countBySlug(t, db, "acme-denied"), "akses nol tidak membuat organisasi")
}

func TestInsertRejectsRecordNotInNormalForm(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	rec := newRecord(t, "acme-badform")
	rec.Slug = "Acme-BadForm"

	_, err := repo.Insert(bg, domain.PlatformAccess(), rec)
	require.ErrorIs(t, err, domain.ErrInvalidSlug)
	require.Equal(t, 0, countBySlug(t, db, "Acme-BadForm"))
	require.Equal(t, 0, countBySlug(t, db, "acme-badform"))
}

func TestInsertSlugIsUniqueAcrossTwoOrganizations(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	plat := domain.PlatformAccess()
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)

	dupA := newRecord(t, orgA.Slug)
	_, err := repo.Insert(bg, plat, dupA)
	require.ErrorIs(t, err, domain.ErrSlugTaken, "slug organisasi A sudah dipakai")
	dupB := newRecord(t, orgB.Slug)
	_, err = repo.Insert(bg, plat, dupB)
	require.ErrorIs(t, err, domain.ErrSlugTaken, "slug organisasi B sudah dipakai")

	fresh := newRecord(t, "acme-fresh")
	_, err = repo.Insert(bg, plat, fresh)
	require.NoError(t, err, "slug baru diterima walau dua organisasi lain ada")

	sameName := newRecord(t, "acme-same-name")
	sameName.Name = fresh.Name
	_, err = repo.Insert(bg, plat, sameName)
	require.NoError(t, err, "name tidak unik")
}

func TestInsertJoinsTheTransactionAndRollsBack(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	rec := newRecord(t, "acme-rollback")
	sentinel := errors.New("batalkan")

	err := kernel.NewTxManager(db.App, nil).WithinTx(bg, func(ctx context.Context) error {
		_, ierr := repo.Insert(ctx, domain.PlatformAccess(), rec)
		require.NoError(t, ierr)
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
	require.Equal(t, 0, countBySlug(t, db, "acme-rollback"), "rollback membatalkan insert")
}

func TestUpdateIsScopedToTheCallerOrganizationInBothDirections(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	accA := tenantOf(t, orgA)
	accB := tenantOf(t, orgB)

	renamed := "Renamed A"
	got, err := repo.Update(bg, accA, orgA.ID, domain.UpdateInput{Name: &renamed})
	require.NoError(t, err)
	require.Equal(t, renamed, got.Name)
	require.Equal(t, orgA.Slug, got.Slug, "slug tidak berubah")
	require.Equal(t, orgB.Name, mustGet(t, db, orgB.ID).Name, "B tidak tersentuh")

	hijack := "Hijacked"
	_, err = repo.Update(bg, accA, orgB.ID, domain.UpdateInput{Name: &hijack})
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound, "A tidak mengubah B")
	require.Equal(t, orgB.Name, mustGet(t, db, orgB.ID).Name)

	_, err = repo.Update(bg, accB, orgA.ID, domain.UpdateInput{Name: &hijack})
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound, "B tidak mengubah A")
	require.Equal(t, renamed, mustGet(t, db, orgA.ID).Name)

	viaPlatform := "Renamed B"
	got, err = repo.Update(bg, domain.PlatformAccess(), orgB.ID, domain.UpdateInput{Name: &viaPlatform})
	require.NoError(t, err)
	require.Equal(t, viaPlatform, got.Name, "platform mengubah B")
}

func TestUpdateChangesOnlyTheGivenFieldsAndClearsLegalName(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	org := testkit.NewOrganization(t, db)
	acc := tenantOf(t, org)

	got, err := repo.Update(bg, acc, org.ID, domain.UpdateInput{LegalName: strPtr("Acme Ltd"), DefaultTimezone: strPtr("Asia/Jakarta")})
	require.NoError(t, err)
	require.NotNil(t, got.LegalName)
	require.Equal(t, "Acme Ltd", *got.LegalName)
	require.Equal(t, "Asia/Jakarta", got.DefaultTimezone)
	require.Equal(t, org.Name, got.Name, "name tidak diubah")
	require.Equal(t, domain.DefaultLocale, got.DefaultLocale, "locale tidak diubah")

	got, err = repo.Update(bg, acc, org.ID, domain.UpdateInput{LegalName: strPtr("")})
	require.NoError(t, err)
	require.Nil(t, got.LegalName, "string kosong menghapus legal_name")
	require.Equal(t, "Asia/Jakarta", got.DefaultTimezone, "zona waktu tidak ikut berubah")
}

func TestUpdateRejectsEmptyAndInvalidInput(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	org := testkit.NewOrganization(t, db)
	acc := tenantOf(t, org)

	_, err := repo.Update(bg, acc, org.ID, domain.UpdateInput{})
	require.ErrorIs(t, err, domain.ErrNothingToUpdate)
	_, err = repo.Update(bg, acc, org.ID, domain.UpdateInput{DefaultTimezone: strPtr("Mars/Base")})
	require.ErrorIs(t, err, domain.ErrInvalidTimezone)
	require.Equal(t, domain.DefaultTimezone, mustGet(t, db, org.ID).DefaultTimezone)
}

func TestUpdateArchivedOrganization(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	plat := domain.PlatformAccess()
	_, err := repo.SetStatus(bg, plat, orgB.ID, domain.StatusArchived)
	require.NoError(t, err)
	name := "Late Rename"

	_, err = repo.Update(bg, plat, orgB.ID, domain.UpdateInput{Name: &name})
	require.ErrorIs(t, err, domain.ErrOrganizationArchived)
	_, err = repo.Update(bg, tenantOf(t, orgB), orgB.ID, domain.UpdateInput{Name: &name})
	require.ErrorIs(t, err, domain.ErrOrganizationArchived, "pemilik organisasi terarsip melihat status arsip")

	_, err = repo.Update(bg, tenantOf(t, orgA), orgB.ID, domain.UpdateInput{Name: &name})
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound, "organisasi lain tidak melihat status arsip B")
	require.Equal(t, orgB.Name, mustGet(t, db, orgB.ID).Name)
}

func TestSetStatusIsScopedToTheCallerOrganizationInBothDirections(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	accA := tenantOf(t, orgA)
	accB := tenantOf(t, orgB)

	_, err := repo.SetStatus(bg, accA, orgB.ID, domain.StatusSuspended)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound, "A tidak menangguhkan B")
	require.Equal(t, domain.StatusActive, mustGet(t, db, orgB.ID).Status)

	_, err = repo.SetStatus(bg, accB, orgA.ID, domain.StatusArchived)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound, "B tidak mengarsipkan A")
	a := mustGet(t, db, orgA.ID)
	require.Equal(t, domain.StatusActive, a.Status)
	require.Nil(t, a.ArchivedAt)

	got, err := repo.SetStatus(bg, accA, orgA.ID, domain.StatusSuspended)
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuspended, got.Status, "A menangguhkan dirinya sendiri")
	require.Equal(t, domain.StatusActive, mustGet(t, db, orgB.ID).Status)
}

func TestSetStatusTransitions(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	plat := domain.PlatformAccess()
	org := testkit.NewOrganization(t, db)

	got, err := repo.SetStatus(bg, plat, org.ID, domain.StatusSuspended)
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuspended, got.Status)
	require.Nil(t, got.ArchivedAt)

	_, err = repo.SetStatus(bg, plat, org.ID, domain.StatusSuspended)
	require.ErrorIs(t, err, domain.ErrInvalidTransition, "status yang sama ditolak")

	got, err = repo.SetStatus(bg, plat, org.ID, domain.StatusActive)
	require.NoError(t, err)
	require.Equal(t, domain.StatusActive, got.Status)

	_, err = repo.SetStatus(bg, plat, org.ID, domain.StatusActive)
	require.ErrorIs(t, err, domain.ErrInvalidTransition, "active ke active ditolak")

	got, err = repo.SetStatus(bg, plat, org.ID, domain.StatusArchived)
	require.NoError(t, err)
	require.Equal(t, domain.StatusArchived, got.Status)
	require.NotNil(t, got.ArchivedAt, "mengarsipkan mengisi deleted_at")
	var stamped bool
	require.NoError(t, db.Migrator.QueryRow(bg, `SELECT deleted_at IS NOT NULL FROM organizations WHERE id = $1`, org.ID).Scan(&stamped))
	require.True(t, stamped)

	for _, to := range []string{domain.StatusActive, domain.StatusSuspended, domain.StatusArchived} {
		_, err = repo.SetStatus(bg, plat, org.ID, to)
		require.ErrorIs(t, err, domain.ErrInvalidTransition, "archived tidak punya jalan keluar: "+to)
	}
	require.Equal(t, domain.StatusArchived, mustGet(t, db, org.ID).Status)

	_, err = repo.SetStatus(bg, plat, org.ID, "bogus")
	require.ErrorIs(t, err, domain.ErrInvalidStatus)
	_, err = repo.SetStatus(bg, plat, id.NewID(), domain.StatusSuspended)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound)
}

func TestSuspendedOrganizationRemainsVisibleToItsOwner(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	org := testkit.NewOrganization(t, db)
	_, err := repo.SetStatus(bg, domain.PlatformAccess(), org.ID, domain.StatusSuspended)
	require.NoError(t, err)

	got, err := repo.Get(bg, tenantOf(t, org), org.ID)
	require.NoError(t, err)
	require.Equal(t, domain.StatusSuspended, got.Status)
}

func TestZeroAccessIsRejectedByEveryMethod(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	org := testkit.NewOrganization(t, db)
	var none domain.Access
	name := "X"

	_, err := repo.Get(bg, none, org.ID)
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	_, err = repo.List(bg, none, domain.ListQuery{})
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	_, err = repo.Update(bg, none, org.ID, domain.UpdateInput{Name: &name})
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	_, err = repo.SetStatus(bg, none, org.ID, domain.StatusSuspended)
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	_, err = repo.ForUpdate(bg, none, org.ID)
	require.ErrorIs(t, err, domain.ErrInvalidAccess)

	require.Equal(t, org.Name, mustGet(t, db, org.ID).Name)
	require.Equal(t, domain.StatusActive, mustGet(t, db, org.ID).Status)
}

func TestForUpdateRequiresATransactionAndIsScoped(t *testing.T) {
	db := testkit.NewDB(t)
	repo := orgRepo(db)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	accA := tenantOf(t, orgA)

	_, err := repo.ForUpdate(bg, accA, orgA.ID)
	require.ErrorIs(t, err, domain.ErrNoTransaction)

	err = kernel.NewTxManager(db.App, nil).WithinTx(bg, func(ctx context.Context) error {
		rec, ferr := repo.ForUpdate(ctx, accA, orgA.ID)
		require.NoError(t, ferr)
		require.Equal(t, orgA.ID, rec.ID)
		_, ferr = repo.ForUpdate(ctx, accA, orgB.ID)
		require.ErrorIs(t, ferr, domain.ErrOrganizationNotFound, "A tidak mengunci B")
		return nil
	})
	require.NoError(t, err)
}
