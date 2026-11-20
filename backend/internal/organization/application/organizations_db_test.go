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
)

// Tes ber-DB membuktikan tiga lapis kewajiban ADR-0022 butir 54 untuk Archive dengan komponen
// sungguhan (transaksi, kunci advisory milik authorization, repository organisasi): kunci
// ditahan transaksi lain, dua pengarsipan serentak, dan batalnya semua perubahan bila invalidasi
// gagal. Audit dan event memakai fake, jadi yang dibuktikan di sini adalah keadaan database.

func newDBService(t *testing.T, db *testpg.DB, inv application.TxInvalidator) (*application.Service, *fakeAudit, *fakeEvents) {
	t.Helper()
	tr := &trace{}
	au := &fakeAudit{trace: tr}
	ev := &fakeEvents{trace: tr}
	svc, err := application.NewService(
		kernel.NewTxManager(db.App, nil),
		orgrepo.NewOrganizations(db.App),
		application.LockFunc(authzrepo.LockOrganization),
		au, ev, inv,
	)
	require.NoError(t, err)
	return svc, au, ev
}

func statusOf(t *testing.T, db *testpg.DB, organizationID uuid.UUID) string {
	t.Helper()
	var s string
	require.NoError(t, db.Migrator.QueryRow(bg, `SELECT status FROM organizations WHERE id = $1`, organizationID).Scan(&s))
	return s
}

func nameOf(t *testing.T, db *testpg.DB, organizationID uuid.UUID) string {
	t.Helper()
	var s string
	require.NoError(t, db.Migrator.QueryRow(bg, `SELECT name FROM organizations WHERE id = $1`, organizationID).Scan(&s))
	return s
}

func awaitErr(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(60 * time.Second):
		require.FailNow(t, "operasi tidak selesai dalam 60 detik")
		return nil
	}
}

func TestArchiveWaitsForTheOrganizationAdvisoryLock(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	svc, _, _ := newDBService(t, db, nil)

	held := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- kernel.NewTxManager(db.App, nil).WithinTx(bg, func(ctx context.Context) error {
			if err := authzrepo.LockOrganization(ctx, org.ID); err != nil {
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

	done := make(chan error, 1)
	go func() {
		_, err := svc.Archive(bg, platformCaller(), org.ID)
		done <- err
	}()
	select {
	case err := <-done:
		close(release)
		require.FailNow(t, "Archive selesai padahal kunci organisasi ditahan transaksi lain", "err=%v", err)
	case <-time.After(300 * time.Millisecond):
	}
	require.Equal(t, domain.StatusActive, statusOf(t, db, org.ID), "belum ada perubahan selama kunci ditahan")

	close(release)
	require.NoError(t, awaitErr(t, holderDone))
	require.NoError(t, awaitErr(t, done))
	require.Equal(t, domain.StatusArchived, statusOf(t, db, org.ID))
}

func TestArchiveDoesNotWaitForAnotherOrganizationsLock(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	svc, _, _ := newDBService(t, db, nil)

	held := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- kernel.NewTxManager(db.App, nil).WithinTx(bg, func(ctx context.Context) error {
			if err := authzrepo.LockOrganization(ctx, orgA.ID); err != nil {
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

	done := make(chan error, 1)
	go func() {
		_, err := svc.Archive(bg, platformCaller(), orgB.ID)
		done <- err
	}()
	require.NoError(t, awaitErr(t, done), "kunci organisasi A tidak menghalangi organisasi B")
	close(release)
	require.NoError(t, awaitErr(t, holderDone))
	require.Equal(t, domain.StatusArchived, statusOf(t, db, orgB.ID))
	require.Equal(t, domain.StatusActive, statusOf(t, db, orgA.ID))
}

func TestConcurrentArchivesSucceedExactlyOnce(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	svc, au, ev := newDBService(t, db, nil)

	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := svc.Archive(bg, platformCaller(), org.ID)
			results <- err
		}()
	}
	succeeded, rejected := 0, 0
	for i := 0; i < 2; i++ {
		err := awaitErr(t, results)
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, domain.ErrInvalidTransition):
			rejected++
		default:
			require.NoError(t, err, "galat tak terduga")
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, rejected)
	require.Equal(t, 1, au.count(), "satu audit untuk satu pengarsipan")
	require.Equal(t, 1, ev.count(), "satu event untuk satu pengarsipan")
	require.Equal(t, domain.StatusArchived, statusOf(t, db, org.ID))
}

func TestArchiveRollsBackWhenInvalidationFails(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	inv := &fakeInvalidator{trace: &trace{}, err: errors.New("redis mati")}
	svc, au, ev := newDBService(t, db, inv)

	_, err := svc.Archive(bg, platformCaller(), org.ID)
	require.ErrorIs(t, err, inv.err)
	require.Equal(t, domain.StatusActive, statusOf(t, db, org.ID), "status dikembalikan oleh rollback")
	var stamped bool
	require.NoError(t, db.Migrator.QueryRow(bg, `SELECT deleted_at IS NOT NULL FROM organizations WHERE id = $1`, org.ID).Scan(&stamped))
	require.False(t, stamped, "deleted_at juga dikembalikan")
	require.Equal(t, 0, au.count())
	require.Equal(t, 0, ev.count())
}

func TestServiceKeepsTenantsApartEndToEnd(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	svc, au, ev := newDBService(t, db, nil)
	callerA := tenantCaller(t, orgA.ID)
	callerB := tenantCaller(t, orgB.ID)
	hijack := "Hijacked"

	_, err := svc.Get(bg, callerA, orgB.ID)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound, "A tidak membaca B")
	_, err = svc.Update(bg, callerA, orgB.ID, domain.UpdateInput{Name: &hijack})
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound, "A tidak mengubah B")
	_, err = svc.Archive(bg, callerA, orgB.ID)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound, "A tidak mengarsipkan B")
	_, err = svc.Update(bg, callerB, orgA.ID, domain.UpdateInput{Name: &hijack})
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound, "B tidak mengubah A")
	_, err = svc.Archive(bg, callerB, orgA.ID)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound, "B tidak mengarsipkan A")

	require.Equal(t, orgB.Name, nameOf(t, db, orgB.ID))
	require.Equal(t, orgA.Name, nameOf(t, db, orgA.ID))
	require.Equal(t, domain.StatusActive, statusOf(t, db, orgA.ID))
	require.Equal(t, domain.StatusActive, statusOf(t, db, orgB.ID))
	require.Equal(t, 0, au.count(), "percobaan lintas organisasi tidak meninggalkan audit")
	require.Equal(t, 0, ev.count(), "dan tidak menerbitkan event")

	renamed := "Renamed A"
	got, err := svc.Update(bg, callerA, orgA.ID, domain.UpdateInput{Name: &renamed})
	require.NoError(t, err)
	require.Equal(t, renamed, got.Name)
	require.Equal(t, orgB.Name, nameOf(t, db, orgB.ID))
	require.Equal(t, 1, au.count())
	require.Equal(t, 1, ev.count())

	page, err := svc.List(bg, callerA, domain.ListQuery{})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, orgA.ID, page.Items[0].ID)
}

func TestCreateThroughTheServicePersistsAndRejectsDuplicateSlugs(t *testing.T) {
	db := testkit.NewDB(t)
	existing := testkit.NewOrganization(t, db)
	svc, au, ev := newDBService(t, db, nil)
	plat := platformCaller()

	got, err := svc.Create(bg, plat, domain.CreateInput{Name: "Acme", Slug: "acme-service"})
	require.NoError(t, err)
	read, err := svc.Get(bg, plat, got.ID)
	require.NoError(t, err)
	require.Equal(t, "acme-service", read.Slug)
	require.Equal(t, 1, au.count())
	require.Equal(t, 1, ev.count())

	_, err = svc.Create(bg, plat, domain.CreateInput{Name: "Lain", Slug: "acme-service"})
	require.ErrorIs(t, err, domain.ErrSlugTaken)
	_, err = svc.Create(bg, plat, domain.CreateInput{Name: "Lain", Slug: existing.Slug})
	require.ErrorIs(t, err, domain.ErrSlugTaken, "slug organisasi lain juga ditolak")
	require.Equal(t, 1, au.count(), "penolakan tidak meninggalkan audit")
	require.Equal(t, 1, ev.count(), "dan tidak menerbitkan event")

	_, err = svc.Create(bg, tenantCaller(t, existing.ID), domain.CreateInput{Name: "Lain", Slug: "dari-tenant"})
	require.ErrorIs(t, err, domain.ErrPlatformRequired)
}
