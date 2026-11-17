package application_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/audit"
	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	authrepo "github.com/master-abror/zago-core/backend/internal/auth/repository"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
)

// Pencabutan sesi sesudah assignment platform dicabut (T5d5, ADR-0022 butir 76). PostgreSQL 18
// sungguhan dan role app_user; satu tes memakai SessionService dan Redis sungguhan.

var _ application.SessionRevoker = (*service.SessionService)(nil)

type sessionCall struct {
	user        uuid.UUID
	reason      string
	ctxErr      error
	hasDeadline bool
	committed   int
}

// sessionSpy merekam setiap panggilan RevokeAll. Pada saat dipanggil ia membaca database lewat
// koneksi lain: committed = 1 hanya bila pencabutan assignment SUDAH di-commit.
type sessionSpy struct {
	t     *testing.T
	db    *testpg.DB
	watch uuid.UUID
	n     int
	err   error
	mu    sync.Mutex
	calls []sessionCall
}

func (s *sessionSpy) RevokeAll(c context.Context, userID uuid.UUID, reason string) (int, error) {
	call := sessionCall{user: userID, reason: reason, ctxErr: c.Err()}
	_, call.hasDeadline = c.Deadline()
	call.committed = count(s.t, s.db, `SELECT count(*) FROM role_assignments WHERE id = $1 AND revoked_at IS NOT NULL`, s.watch)
	s.mu.Lock()
	s.calls = append(s.calls, call)
	s.mu.Unlock()
	return s.n, s.err
}

func (s *sessionSpy) snapshot() []sessionCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]sessionCall(nil), s.calls...)
}

// cancelAfterTx membatalkan context request tepat setelah transaksi selesai: klien yang memutus
// koneksi sesudah commit.
type cancelAfterTx struct {
	inner  application.TxRunner
	cancel context.CancelFunc
}

func (c cancelAfterTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	err := c.inner.WithinTx(ctx, fn)
	c.cancel()
	return err
}

func subjectOf(t *testing.T, db *testpg.DB, assignmentID uuid.UUID) uuid.UUID {
	t.Helper()
	return uuid.MustParse(text(t, db, `SELECT subject_id::text FROM role_assignments WHERE id = $1`, assignmentID))
}

func sessionActs(t *testing.T, db *testpg.DB, assignmentID uuid.UUID, result string) int {
	t.Helper()
	return count(t, db, `SELECT count(*) FROM activities WHERE action = $1 AND result = $2 AND resource_id = $3`,
		application.AuditActionSessionsRevoke, result, assignmentID)
}

func TestPlatformAssignmentRevocationRevokesTheUsersSessionsAfterCommit(t *testing.T) {
	p := newAWPlatform(t)
	target := p.admins[0].ID
	subject := subjectOf(t, p.db, target)
	spy := &sessionSpy{t: t, db: p.db, watch: target, n: 3}
	w := p.writer.WithSessionRevoker(spy, nil)

	require.NoError(t, w.Revoke(ctx, p.grants[0], target))

	calls := spy.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, subject, calls[0].user, "sesi milik pemegang assignment yang dicabut")
	require.Equal(t, application.SessionRevokeReason, calls[0].reason)
	require.Equal(t, 1, calls[0].committed, "dipanggil SETELAH commit pencabutan assignment")
	require.Equal(t, 1, sessionActs(t, p.db, target, "success"))
	require.Equal(t, "3", text(t, p.db,
		`SELECT metadata->>'sessions' FROM activities WHERE action = $1 AND resource_id = $2`,
		application.AuditActionSessionsRevoke, target))
	require.Equal(t, 1, count(t, p.db,
		`SELECT count(*) FROM activities WHERE action = 'role_assignment.revoke' AND result = 'success' AND resource_id = $1`, target))
}

func TestSessionsAreNotRevokedForTenantAssignments(t *testing.T) {
	e := newAWEnv(t)
	a := awAssignOrg(t, e.db, e.role, e.member.ID, e.org.ID)
	spy := &sessionSpy{t: t, db: e.db, watch: a.ID}
	w := e.writer.WithSessionRevoker(spy, nil)

	require.NoError(t, w.Revoke(ctx, e.grant, a.ID))

	require.Empty(t, spy.snapshot(), "assignment organisasi tidak menyentuh sesi")
	require.Equal(t, 0, sessionActs(t, e.db, a.ID, "success"))
}

func TestSessionsAreNotRevokedWhenThePlatformRevocationIsRefused(t *testing.T) {
	p := newAWPlatform(t)
	first, second := p.admins[0].ID, p.admins[1].ID
	spy := &sessionSpy{t: t, db: p.db, watch: first}
	w := p.writer.WithSessionRevoker(spy, nil)
	require.NoError(t, w.Revoke(ctx, p.grants[0], first))
	require.Len(t, spy.snapshot(), 1)

	err := w.Revoke(ctx, p.grants[0], second)
	require.ErrorIs(t, err, domain.ErrLastSuperAdmin, "super-admin terakhir tidak boleh dicabut")

	err = w.Revoke(ctx, p.grants[0], first)
	require.Error(t, err, "assignment yang sudah dicabut")

	require.Len(t, spy.snapshot(), 1, "penolakan tidak mencabut sesi siapa pun")
	require.Equal(t, 0, sessionActs(t, p.db, second, "success"))
	require.Equal(t, 0, sessionActs(t, p.db, second, "failed"))
}

func TestWriterWithoutASessionRevokerLeavesSessionsAlone(t *testing.T) {
	p := newAWPlatform(t)
	target := p.admins[0].ID
	spy := &sessionSpy{t: t, db: p.db, watch: target}
	_ = p.writer.WithSessionRevoker(spy, nil)

	require.NoError(t, p.writer.Revoke(ctx, p.grants[0], target))

	require.Empty(t, spy.snapshot(), "WithSessionRevoker mengembalikan salinan; writer asal tidak berubah")
	require.Equal(t, 0, sessionActs(t, p.db, target, "success"))
}

func TestSessionRevocationFailureNeverUndoesTheCommit(t *testing.T) {
	p := newAWPlatform(t)
	target := p.admins[0].ID
	spy := &sessionSpy{t: t, db: p.db, watch: target, err: errors.New("redis down")}
	var logs bytes.Buffer
	w := p.writer.WithSessionRevoker(spy, slog.New(slog.NewTextHandler(&logs, nil)))

	require.NoError(t, w.Revoke(ctx, p.grants[0], target), "assignment sudah dicabut; kegagalan sesi tidak membatalkannya")

	require.Equal(t, 1, count(t, p.db, `SELECT count(*) FROM role_assignments WHERE id = $1 AND revoked_at IS NOT NULL`, target))
	require.Len(t, spy.snapshot(), 1, "satu percobaan, tanpa retry diam-diam")
	require.Equal(t, 1, sessionActs(t, p.db, target, "failed"))
	require.Equal(t, 0, sessionActs(t, p.db, target, "success"))
	require.Equal(t, "session_revoke_failed", text(t, p.db,
		`SELECT metadata->>'reason' FROM activities WHERE action = $1 AND resource_id = $2`,
		application.AuditActionSessionsRevoke, target))
	require.Equal(t, 0, count(t, p.db,
		`SELECT count(*) FROM activities WHERE action = $1 AND metadata::text LIKE '%redis down%'`,
		application.AuditActionSessionsRevoke), "teks galat mentah tidak masuk audit")
	require.Contains(t, logs.String(), "gagal mencabut sesi")
	require.Contains(t, logs.String(), "redis down")
}

func TestSessionRevocationSurvivesARequestCancelledAfterCommit(t *testing.T) {
	p := newAWPlatform(t)
	target := p.admins[0].ID
	spy := &sessionSpy{t: t, db: p.db, watch: target, n: 1}
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	tx := cancelAfterTx{inner: kernel.NewTxManager(p.db.App, nil), cancel: cancel}
	base, err := application.NewAssignmentWriter(tx, repository.NewAssignments(p.db.App), audit.New(p.db.App, nil), kernel.NewBus(p.db.App, nil), nil)
	require.NoError(t, err)
	w := base.WithSessionRevoker(spy, nil)

	require.NoError(t, w.Revoke(reqCtx, p.grants[0], target))

	require.Error(t, reqCtx.Err(), "prasyarat tes: request sudah dibatalkan")
	calls := spy.snapshot()
	require.Len(t, calls, 1)
	require.NoError(t, calls[0].ctxErr, "pencabutan sesi tidak ikut dibatalkan")
	require.True(t, calls[0].hasDeadline, "pencabutan sesi punya batas waktu sendiri")
	require.Equal(t, 1, sessionActs(t, p.db, target, "success"))
}

func TestRevokingAPlatformAssignmentEndsTheUsersRealSessions(t *testing.T) {
	p := newAWPlatform(t)
	rdb := testkit.Redis(t).Client(t)
	svc := service.NewSessionService(authrepo.NewStore(p.db.App), rdb,
		service.StaticTimeouts{Idle: time.Hour, Absolute: 24 * time.Hour}, nil)
	org := testkit.NewOrganization(t, p.db)
	orgID := org.ID
	subject := subjectOf(t, p.db, p.admins[0].ID)
	other := subjectOf(t, p.db, p.admins[1].ID)

	// Sesi hasil masuk-lewat-platform membawa organisasi; sesi rumah tidak.
	entered, err := svc.Create(ctx, service.CreateParams{UserID: subject, OrganizationID: &orgID})
	require.NoError(t, err)
	home, err := svc.Create(ctx, service.CreateParams{UserID: subject})
	require.NoError(t, err)
	control, err := svc.Create(ctx, service.CreateParams{UserID: other})
	require.NoError(t, err)
	for _, token := range []string{entered.Token, home.Token, control.Token} {
		_, err = svc.Authenticate(ctx, token)
		require.NoError(t, err, "prasyarat tes: semua sesi sah sebelum pencabutan")
	}

	w := p.writer.WithSessionRevoker(svc, nil)
	require.NoError(t, w.Revoke(ctx, p.grants[0], p.admins[0].ID))

	_, err = svc.Authenticate(ctx, entered.Token)
	require.ErrorIs(t, err, authdomain.ErrUnauthenticated, "sesi hasil masuk-lewat-platform tidak sah lagi")
	_, err = svc.Authenticate(ctx, home.Token)
	require.ErrorIs(t, err, authdomain.ErrUnauthenticated, "semua sesi user itu dicabut")
	_, err = svc.Authenticate(ctx, control.Token)
	require.NoError(t, err, "kontrol: sesi user lain tidak tersentuh")
	require.Equal(t, "2", text(t, p.db,
		`SELECT metadata->>'sessions' FROM activities WHERE action = $1 AND resource_id = $2`,
		application.AuditActionSessionsRevoke, p.admins[0].ID))
}
