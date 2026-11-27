package application_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/audit"
	"github.com/master-abror/zago-core/backend/internal/identity/application"
	"github.com/master-abror/zago-core/backend/internal/identity/domain"
	identityrepo "github.com/master-abror/zago-core/backend/internal/identity/repository"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	orgdomain "github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// Tes ber-DB memakai komponen sungguhan: TxManager, repository identity, audit.Recorder, dan
// kernel.Bus (outbox). Yang dibuktikan di sini adalah keadaan tabel users, activities, dan
// outbox_events; Authenticate, CSRF, Scope, dan Authorize tidak ikut (tes perakitan T1b-4).

func newDBService(t *testing.T, db *testpg.DB, events application.EventPublisher) *application.Service {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if events == nil {
		events = kernel.NewBus(db.App, log)
	}
	svc, err := application.NewService(
		kernel.NewTxManager(db.App, log),
		identityrepo.NewUsers(db.App),
		audit.New(db.App, log),
		events,
	)
	require.NoError(t, err)
	return svc
}

func countRows(t *testing.T, db *testpg.DB, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.Migrator.QueryRow(bg, sql, args...).Scan(&n))
	return n
}

func displayNameOf(t *testing.T, db *testpg.DB, userID uuid.UUID) string {
	t.Helper()
	var s string
	require.NoError(t, db.Migrator.QueryRow(bg, `SELECT display_name FROM users WHERE id = $1`, userID.String()).Scan(&s))
	return s
}

func dbTenantCaller(t *testing.T, org testkit.Organization, u testkit.User) application.Caller {
	t.Helper()
	c := tenantCaller(t, org.ID)
	c.UserID = u.ID
	return c
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

type failingEvents struct{}

func (failingEvents) Publish(ctx context.Context, _ modulesdk.Event) {
	kernel.FailTx(ctx, errors.New("outbox gagal"))
}

func TestUpdateProfileWritesRowAuditAndOutboxTogether(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db, testkit.InOrganization(org))
	svc := newDBService(t, db, nil)

	in := domain.UpdateInput{DisplayName: strPtr("Nama Baru"), Phone: strPtr("+62 812 3456 7890")}
	out, err := svc.UpdateProfile(bg, dbTenantCaller(t, org, u), in)
	require.NoError(t, err)
	require.Equal(t, "Nama Baru", out.DisplayName)
	require.Equal(t, "Nama Baru", displayNameOf(t, db, u.ID))

	require.Equal(t, 1, countRows(t, db,
		`SELECT count(*) FROM activities
		  WHERE action = 'profile.update' AND result = 'success' AND resource_type = 'user'
		    AND organization_id = $1 AND actor_user_id = $2 AND resource_id = $2
		    AND metadata->'changed_fields' = '["display_name","phone"]'::jsonb`,
		org.ID.String(), u.ID.String()))
	require.Equal(t, 1, countRows(t, db,
		`SELECT count(*) FROM outbox_events
		  WHERE event_name = 'user.updated' AND aggregate_type = 'user'
		    AND organization_id = $1 AND aggregate_id = $2`,
		org.ID.String(), u.ID.String()))

	require.Equal(t, 0, countRows(t, db, `SELECT count(*) FROM activities WHERE metadata::text LIKE '%812 3456%'`), "nilai phone tidak masuk audit")
	require.Equal(t, 0, countRows(t, db, `SELECT count(*) FROM outbox_events WHERE payload::text LIKE '%812 3456%'`), "nilai phone tidak masuk outbox")
}

func TestUpdateUserByPlatformStoresNullOrganizationInAuditAndOutbox(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	target := testkit.NewUser(t, db, testkit.InOrganization(org))
	operator := testkit.NewUser(t, db)
	svc := newDBService(t, db, nil)

	caller := application.Caller{UserID: operator.ID}
	out, err := svc.UpdateUser(bg, caller, target.ID, domain.UpdateInput{DisplayName: strPtr("Diubah Operator")})
	require.NoError(t, err)
	require.Equal(t, "Diubah Operator", out.DisplayName)

	require.Equal(t, 1, countRows(t, db,
		`SELECT count(*) FROM activities
		  WHERE action = 'user.update' AND result = 'success' AND resource_type = 'user'
		    AND organization_id IS NULL AND actor_user_id = $1 AND resource_id = $2
		    AND metadata->'changed_fields' = '["display_name"]'::jsonb`,
		operator.ID.String(), target.ID.String()), "organization_id kosong diterima audit")
	require.Equal(t, 1, countRows(t, db,
		`SELECT count(*) FROM outbox_events
		  WHERE event_name = 'user.updated' AND organization_id IS NULL AND aggregate_id = $1`,
		target.ID.String()), "organization_id kosong diterima outbox")
}

func TestUpdateUserForTenantWritesNothing(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	admin := testkit.NewUser(t, db, testkit.InOrganization(org))
	target := testkit.NewUser(t, db, testkit.InOrganization(org))
	svc := newDBService(t, db, nil)

	_, err := svc.UpdateUser(bg, dbTenantCaller(t, org, admin), target.ID, domain.UpdateInput{DisplayName: strPtr("Diubah Tenant")})
	require.ErrorIs(t, err, orgdomain.ErrPlatformRequired)
	require.Equal(t, target.DisplayName, displayNameOf(t, db, target.ID))
	require.Equal(t, 0, countRows(t, db, `SELECT count(*) FROM activities WHERE action = 'user.update'`))
	require.Equal(t, 0, countRows(t, db, `SELECT count(*) FROM outbox_events WHERE event_name = 'user.updated'`))
}

func TestUpdateWithoutChangeWritesNoAuditOrOutbox(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db, testkit.InOrganization(org))
	svc := newDBService(t, db, nil)

	same := domain.UpdateInput{DisplayName: strPtr(u.DisplayName)}
	_, err := svc.UpdateProfile(bg, dbTenantCaller(t, org, u), same)
	require.NoError(t, err)
	_, err = svc.UpdateUser(bg, application.Caller{UserID: u.ID}, u.ID, same)
	require.NoError(t, err)

	require.Equal(t, 0, countRows(t, db, `SELECT count(*) FROM activities WHERE action IN ('profile.update', 'user.update')`))
	require.Equal(t, 0, countRows(t, db, `SELECT count(*) FROM outbox_events WHERE event_name = 'user.updated'`))
}

func TestUpdateUserHidesUsersOfOtherOrganizationsOnlyFromTenants(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	userA := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	userB := testkit.NewUser(t, db, testkit.InOrganization(orgB))
	svc := newDBService(t, db, nil)

	_, err := svc.GetUser(bg, dbTenantCaller(t, orgA, userA), userB.ID)
	require.ErrorIs(t, err, domain.ErrUserNotFound, "A tidak membaca user B lewat service")
	_, err = svc.GetUser(bg, dbTenantCaller(t, orgB, userB), userA.ID)
	require.ErrorIs(t, err, domain.ErrUserNotFound, "B tidak membaca user A lewat service")

	_, err = svc.GetUser(bg, application.Caller{UserID: userA.ID}, userB.ID)
	require.NoError(t, err, "platform membaca user mana pun")

	page, err := svc.ListUsers(bg, dbTenantCaller(t, orgA, userA), domain.ListQuery{})
	require.NoError(t, err)
	require.Equal(t, 1, page.Total)
	require.Equal(t, userA.ID, page.Items[0].ID)
}

func TestUpdateProfileRollsBackWhenTheOutboxWriteFails(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db, testkit.InOrganization(org))
	svc := newDBService(t, db, failingEvents{})

	_, err := svc.UpdateProfile(bg, dbTenantCaller(t, org, u), domain.UpdateInput{DisplayName: strPtr("Tidak Jadi")})
	require.ErrorIs(t, err, kernel.ErrTxRollbackOnly)
	require.Equal(t, u.DisplayName, displayNameOf(t, db, u.ID), "ubah dibatalkan")
	require.Equal(t, 0, countRows(t, db, `SELECT count(*) FROM activities WHERE action = 'profile.update'`), "audit ikut dibatalkan")
}

func TestUpdateProfileWaitsForTheRowLock(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db, testkit.InOrganization(org))
	svc := newDBService(t, db, nil)
	repo := identityrepo.NewUsers(db.App)

	held := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	go func() {
		holderDone <- kernel.NewTxManager(db.App, nil).WithinTx(bg, func(ctx context.Context) error {
			if _, err := repo.ForUpdateSelf(ctx, u.ID); err != nil {
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
		require.FailNow(t, "kunci baris tidak pernah diambil")
	}

	done := make(chan error, 1)
	go func() {
		_, err := svc.UpdateProfile(bg, dbTenantCaller(t, org, u), domain.UpdateInput{DisplayName: strPtr("Menunggu Kunci")})
		done <- err
	}()
	select {
	case err := <-done:
		close(release)
		require.FailNow(t, "UpdateProfile selesai padahal baris dikunci transaksi lain", "err=%v", err)
	case <-time.After(300 * time.Millisecond):
	}
	require.Equal(t, u.DisplayName, displayNameOf(t, db, u.ID), "belum ada perubahan selama kunci ditahan")

	close(release)
	require.NoError(t, awaitErr(t, holderDone))
	require.NoError(t, awaitErr(t, done))
	require.Equal(t, "Menunggu Kunci", displayNameOf(t, db, u.ID))
}

func TestConcurrentProfileUpdatesBothSucceedAndTheLastWriteWins(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	u := testkit.NewUser(t, db, testkit.InOrganization(org))
	svc := newDBService(t, db, nil)
	caller := dbTenantCaller(t, org, u)

	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() {
		_, err := svc.UpdateProfile(bg, caller, domain.UpdateInput{DisplayName: strPtr("Penulis Satu")})
		first <- err
	}()
	go func() {
		_, err := svc.UpdateProfile(bg, caller, domain.UpdateInput{DisplayName: strPtr("Penulis Dua")})
		second <- err
	}()
	require.NoError(t, awaitErr(t, first))
	require.NoError(t, awaitErr(t, second))

	final := displayNameOf(t, db, u.ID)
	require.Contains(t, []string{"Penulis Satu", "Penulis Dua"}, final, "salah satu menang, tidak ada nilai campuran")
	require.Equal(t, 2, countRows(t, db, `SELECT count(*) FROM activities WHERE action = 'profile.update' AND result = 'success'`),
		"kedua perubahan tercatat karena diserialkan kunci baris")
}
