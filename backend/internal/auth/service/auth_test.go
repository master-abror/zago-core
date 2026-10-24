package service_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/security"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// Tes AuthService memakai PostgreSQL dan Redis sungguhan lewat harness SessionService. Hasher
// memakai parameter Argon2id kecil agar cepat; perilakunya (PHC, rehash, dummy) tetap asli.

const goodPassword = "correct-horse-battery-staple"

var (
	testParams = security.Argon2Params{MemoryKiB: 64, Time: 1, Parallelism: 1}
	oldParams  = security.Argon2Params{MemoryKiB: 128, Time: 1, Parallelism: 1}
)

// spyHasher menghitung berapa kali verifikasi nyata dan verifikasi dummy dijalankan.
type spyHasher struct {
	security.PasswordHasher
	compares atomic.Int64
	dummies  atomic.Int64
}

func (s *spyHasher) Compare(ctx context.Context, hash, password string) error {
	s.compares.Add(1)
	return s.PasswordHasher.Compare(ctx, hash, password)
}

func (s *spyHasher) CompareDummy(ctx context.Context, password string) error {
	s.dummies.Add(1)
	return s.PasswordHasher.CompareDummy(ctx, password)
}

// authAudit mencatat activity beserta aktornya; failWith menandai transaksi rollback-only
// seperti yang dilakukan perekam audit nyata saat gagal menulis.
type authAudit struct {
	mu       sync.Mutex
	entries  []modulesdk.ActivityEntry
	actors   []kernel.Actor
	failWith error
}

func (a *authAudit) Record(ctx context.Context, e modulesdk.ActivityEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, e)
	actor, _ := kernel.ActorFromContext(ctx)
	a.actors = append(a.actors, actor)
	if a.failWith != nil {
		kernel.FailTx(ctx, a.failWith)
	}
}

func (a *authAudit) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.entries)
}

type authHarness struct {
	*harness
	auth  *service.AuthService
	spy   *spyHasher
	audit *authAudit
}

func newAuthHarness(t *testing.T) *authHarness {
	t.Helper()
	h := newHarness(t)
	base, err := security.NewArgon2idHasher(testParams, 2)
	require.NoError(t, err)
	spy := &spyHasher{PasswordHasher: base}
	aud := &authAudit{}
	as, err := service.NewAuthService(service.AuthDeps{
		Store: h.store, Tx: h.tm, Sessions: h.svc, Hasher: spy, Audit: aud,
		Log: slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
	})
	require.NoError(t, err)
	return &authHarness{harness: h, auth: as, spy: spy, audit: aud}
}

// newUserWithPassword membuat pengguna aktif beserta credential password.
func (a *authHarness) newUserWithPassword(password string, opts ...testkit.UserOption) testkit.User {
	a.t.Helper()
	u := testkit.NewUser(a.t, a.db, opts...)
	a.setHash(u, a.hash(a.spy, password))
	return u
}

func (a *authHarness) hash(h security.PasswordHasher, password string) string {
	a.t.Helper()
	out, err := h.Hash(a.ctx, password)
	require.NoError(a.t, err)
	return out
}

func (a *authHarness) setHash(u testkit.User, hash string) {
	a.t.Helper()
	_, err := a.db.Migrator.Exec(a.ctx,
		`INSERT INTO credentials (user_id, credential_type, secret_hash) VALUES ($1, 'password', $2)`, u.ID, hash)
	require.NoError(a.t, err)
}

func (a *authHarness) setStatus(u testkit.User, status string) {
	a.t.Helper()
	_, err := a.db.Migrator.Exec(a.ctx, `UPDATE users SET status = $1 WHERE id = $2`, status, u.ID)
	require.NoError(a.t, err)
}

func (a *authHarness) count(sql string, args ...any) int {
	a.t.Helper()
	var n int
	require.NoError(a.t, a.db.Migrator.QueryRow(a.ctx, sql, args...).Scan(&n))
	return n
}

func (a *authHarness) sessionCount(u testkit.User) int {
	a.t.Helper()
	return a.count(`SELECT count(*) FROM sessions WHERE user_id = $1`, u.ID)
}

type eventRow struct {
	Type      string
	Result    string
	Reason    string
	SessionID *uuid.UUID
}

func (a *authHarness) events(u testkit.User) []eventRow {
	a.t.Helper()
	rows, err := a.db.Migrator.Query(a.ctx,
		`SELECT event_type, result, COALESCE(metadata->>'reason', ''), session_id
		   FROM security_events WHERE user_id = $1 ORDER BY created_at, id`, u.ID)
	require.NoError(a.t, err)
	defer rows.Close()
	var out []eventRow
	for rows.Next() {
		var e eventRow
		require.NoError(a.t, rows.Scan(&e.Type, &e.Result, &e.Reason, &e.SessionID))
		out = append(out, e)
	}
	require.NoError(a.t, rows.Err())
	return out
}

func (a *authHarness) login(email, password string) (service.LoginResult, error) {
	a.t.Helper()
	return a.auth.Login(a.ctx, service.LoginInput{Email: email, Password: password})
}

func TestNewAuthServiceRequiresDependencies(t *testing.T) {
	h := newHarness(t)
	hasher, err := security.NewArgon2idHasher(testParams, 1)
	require.NoError(t, err)
	full := service.AuthDeps{Store: h.store, Tx: h.tm, Sessions: h.svc, Hasher: hasher}

	_, err = service.NewAuthService(full)
	require.NoError(t, err)

	for name, mutate := range map[string]func(*service.AuthDeps){
		"store":    func(d *service.AuthDeps) { d.Store = nil },
		"tx":       func(d *service.AuthDeps) { d.Tx = nil },
		"sessions": func(d *service.AuthDeps) { d.Sessions = nil },
		"hasher":   func(d *service.AuthDeps) { d.Hasher = nil },
	} {
		d := full
		mutate(&d)
		_, err := service.NewAuthService(d)
		require.Error(t, err, name)
	}
}

func TestLoginSucceedsAndRecordsSessionEventAndActivityTogether(t *testing.T) {
	a := newAuthHarness(t)
	org := testkit.NewOrganization(t, a.db)
	u := a.newUserWithPassword(goodPassword, testkit.InOrganization(org))
	a.ctx = kernel.WithClient(a.ctx, kernel.Client{IP: "203.0.113.9", UserAgent: "agen-uji/1.0"})

	// Email dinormalisasi: spasi dan huruf besar tidak menghalangi login.
	res, err := a.login("  "+strings.ToUpper(u.Email)+" ", goodPassword)

	require.NoError(t, err)
	require.NotEmpty(t, res.Issued.Token)
	require.Equal(t, u.ID, res.User.ID)
	require.NotNil(t, res.OrganizationID)
	require.Equal(t, org.ID, *res.OrganizationID)

	id, err := a.svc.Authenticate(a.ctx, res.Issued.Token)
	require.NoError(t, err, "token yang diterbitkan harus langsung sah")
	require.Equal(t, u.ID, id.UserID)
	require.Equal(t, res.Issued.Session.ID, id.SessionID)
	require.Equal(t, domain.AuthLevelPassword, id.AuthLevel)

	require.Equal(t, 1, a.sessionCount(u))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM users WHERE id = $1 AND last_login_at IS NOT NULL`, u.ID))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM credentials WHERE user_id = $1 AND last_used_at IS NOT NULL`, u.ID))

	evs := a.events(u)
	require.Len(t, evs, 1)
	require.Equal(t, service.EventLoginSuccess, evs[0].Type)
	require.Equal(t, "success", evs[0].Result)
	require.NotNil(t, evs[0].SessionID)
	require.Equal(t, res.Issued.Session.ID, *evs[0].SessionID)
	require.Equal(t, 1, a.count(
		`SELECT count(*) FROM security_events WHERE user_id = $1 AND host(ip_address) = '203.0.113.9' AND user_agent = 'agen-uji/1.0'`, u.ID))

	require.Equal(t, 1, a.audit.count())
	require.Equal(t, "auth.login", a.audit.entries[0].Action)
	require.Equal(t, res.Issued.Session.ID, a.audit.entries[0].ResourceID)
	require.Equal(t, u.ID, a.audit.actors[0].UserID, "aktor activity adalah pengguna yang login")
	require.Equal(t, org.ID, a.audit.actors[0].OrganizationID)
}

func TestLoginWithoutOrganizationIsPlatformIdentity(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)

	res, err := a.login(u.Email, goodPassword)

	require.NoError(t, err)
	require.Nil(t, res.OrganizationID)
	id, err := a.svc.Authenticate(a.ctx, res.Issued.Token)
	require.NoError(t, err)
	require.Nil(t, id.OrganizationID)
}

func TestLoginWrongPasswordAndUnknownEmailAreIndistinguishableAndBothVerifyFully(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)

	_, errWrong := a.login(u.Email, "password-yang-salah-sekali")
	require.ErrorIs(t, errWrong, domain.ErrInvalidCredentials)
	require.EqualValues(t, 1, a.spy.compares.Load())
	require.Zero(t, a.spy.dummies.Load())

	_, errUnknown := a.login("tidak-ada@example.test", "password-yang-salah-sekali")
	require.ErrorIs(t, errUnknown, domain.ErrInvalidCredentials)
	require.EqualValues(t, 1, a.spy.compares.Load(), "email tak dikenal tidak boleh membandingkan hash sungguhan")
	require.EqualValues(t, 1, a.spy.dummies.Load(), "email tak dikenal tetap menjalankan verifikasi penuh terhadap hash dummy")
	require.Equal(t, errWrong.Error(), errUnknown.Error(), "kedua kegagalan identik bagi pemanggil")

	require.Zero(t, a.sessionCount(u))
	evs := a.events(u)
	require.Len(t, evs, 1, "hanya akun yang dikenal yang mendapat event login gagal")
	require.Equal(t, service.EventLoginFailed, evs[0].Type)
	require.Equal(t, "failure", evs[0].Result)
	require.Equal(t, "bad_password", evs[0].Reason)
	require.Zero(t, a.count(`SELECT count(*) FROM security_events WHERE user_id IS NULL`), "email tak dikenal tidak menulis baris")
	require.Zero(t, a.audit.count())
}

func TestLoginUserWithoutPasswordCredentialRunsDummyVerification(t *testing.T) {
	a := newAuthHarness(t)
	u := testkit.NewUser(t, a.db) // tanpa credential

	_, err := a.login(u.Email, goodPassword)

	require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	require.EqualValues(t, 1, a.spy.dummies.Load())
	require.Zero(t, a.spy.compares.Load())
	require.Zero(t, a.sessionCount(u))
}

func TestLoginRevealsAccountStatusOnlyAfterTheCorrectPassword(t *testing.T) {
	cases := []struct {
		status  string
		correct error
	}{
		{"suspended", domain.ErrAccountSuspended},
		{"deactivated", domain.ErrAccountSuspended},
		{"locked", domain.ErrAccountLocked},
		{"pending", domain.ErrInvalidCredentials},
		{"deleted", domain.ErrInvalidCredentials},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			a := newAuthHarness(t)
			u := a.newUserWithPassword(goodPassword)
			a.setStatus(u, tc.status)

			_, err := a.login(u.Email, "password-yang-salah-sekali")
			require.ErrorIs(t, err, domain.ErrInvalidCredentials, "password salah: status tidak boleh terlihat")

			_, err = a.login(u.Email, goodPassword)
			require.ErrorIs(t, err, tc.correct)

			require.Zero(t, a.sessionCount(u), "akun yang ditolak tidak mendapat sesi")
			require.Zero(t, a.audit.count())
			evs := a.events(u)
			require.Len(t, evs, 2)
			require.Equal(t, "status_"+tc.status, evs[1].Reason)
		})
	}
}

func TestLoginTreatsEmptyAndOverlongPasswordsAsInvalidCredentials(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)

	for name, pw := range map[string]string{
		"kosong":      "",
		"melebihi256": strings.Repeat("x", security.MaxPasswordBytes+1),
	} {
		_, err := a.login(u.Email, pw)
		require.ErrorIs(t, err, domain.ErrInvalidCredentials, name)
		_, err = a.login("tidak-ada@example.test", pw)
		require.ErrorIs(t, err, domain.ErrInvalidCredentials, name+" (email tak dikenal)")
	}
	require.Zero(t, a.sessionCount(u))
}

func TestLoginRehashesStaleParametersOnlyAfterASuccessfulLogin(t *testing.T) {
	a := newAuthHarness(t)
	old, err := security.NewArgon2idHasher(oldParams, 1)
	require.NoError(t, err)
	stale := a.hash(old, goodPassword)
	require.True(t, a.spy.NeedsRehash(stale), "prasyarat tes: hash lama memang basi")

	wrongUser := testkit.NewUser(t, a.db)
	a.setHash(wrongUser, stale)
	_, err = a.login(wrongUser.Email, "password-yang-salah-sekali")
	require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	var stored string
	require.NoError(t, a.db.Migrator.QueryRow(a.ctx, `SELECT secret_hash FROM credentials WHERE user_id = $1`, wrongUser.ID).Scan(&stored))
	require.Equal(t, stale, stored, "login gagal tidak boleh mengubah hash")

	okUser := testkit.NewUser(t, a.db)
	a.setHash(okUser, stale)
	_, err = a.login(okUser.Email, goodPassword)
	require.NoError(t, err)
	require.NoError(t, a.db.Migrator.QueryRow(a.ctx, `SELECT secret_hash FROM credentials WHERE user_id = $1`, okUser.ID).Scan(&stored))
	require.NotEqual(t, stale, stored)
	require.False(t, a.spy.NeedsRehash(stored), "hash kini memakai parameter terkini")
	require.NoError(t, a.spy.PasswordHasher.Compare(a.ctx, stored, goodPassword), "password yang sama tetap berlaku")
}

func TestLoginIsAtomicWhenTheActivityCannotBeWritten(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	a.audit.failWith = errors.New("audit tidak bisa menulis")

	_, err := a.login(u.Email, goodPassword)

	require.Error(t, err)
	require.NotErrorIs(t, err, domain.ErrInvalidCredentials, "ini kegagalan infrastruktur, bukan password salah")
	require.ErrorIs(t, err, kernel.ErrTxRollbackOnly)
	require.Zero(t, a.sessionCount(u), "tanpa jejak audit tidak ada sesi")
	require.Zero(t, a.count(`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2`, u.ID, service.EventLoginSuccess))
	require.Zero(t, a.count(`SELECT count(*) FROM users WHERE id = $1 AND last_login_at IS NOT NULL`, u.ID))
	keys, err := a.rdb.Keys(a.ctx, "session:*").Result()
	require.NoError(t, err)
	require.Empty(t, keys, "entri Redis tidak boleh ada untuk transaksi yang di-rollback")
}

func TestLoginWithCanceledContextIsNotReportedAsWrongCredentials(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	ctx, cancel := context.WithCancel(a.ctx)
	cancel()

	_, err := a.auth.Login(ctx, service.LoginInput{Email: u.Email, Password: goodPassword})

	require.Error(t, err)
	require.NotErrorIs(t, err, domain.ErrInvalidCredentials)
	require.Zero(t, a.sessionCount(u))
}

func TestLogoutRevokesTheSessionRecordsItAndIsIdempotent(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	res, err := a.login(u.Email, goodPassword)
	require.NoError(t, err)
	id, err := a.svc.Authenticate(a.ctx, res.Issued.Token)
	require.NoError(t, err)

	require.NoError(t, a.auth.Logout(a.ctx, *id))

	_, err = a.svc.Authenticate(a.ctx, res.Issued.Token)
	require.ErrorIs(t, err, domain.ErrUnauthenticated, "token lama tidak berlaku lagi")
	require.Equal(t, 1, a.count(`SELECT count(*) FROM sessions WHERE id = $1 AND revoked_at IS NOT NULL`, res.Issued.Session.ID))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2`, u.ID, service.EventLogout))
	require.Equal(t, 2, a.audit.count(), "login + logout")
	require.Equal(t, "auth.logout", a.audit.entries[1].Action)

	require.NoError(t, a.auth.Logout(a.ctx, *id), "logout kedua tidak error")
	require.Equal(t, 1, a.count(`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2`, u.ID, service.EventLogout), "dan tidak menulis event ganda")
	require.Equal(t, 2, a.audit.count())
}

func TestLogoutCannotRevokeAnotherUsersSession(t *testing.T) {
	a := newAuthHarness(t)
	victim := a.newUserWithPassword(goodPassword)
	attacker := testkit.NewUser(t, a.db)
	res, err := a.login(victim.Email, goodPassword)
	require.NoError(t, err)

	forged := domain.Identity{UserID: attacker.ID, SessionID: res.Issued.Session.ID, AuthLevel: domain.AuthLevelPassword}
	require.NoError(t, a.auth.Logout(a.ctx, forged))

	_, err = a.svc.Authenticate(a.ctx, res.Issued.Token)
	require.NoError(t, err, "sesi korban tetap hidup")
	require.Zero(t, a.count(`SELECT count(*) FROM security_events WHERE event_type = $1`, service.EventLogout))
}

func TestProfileReturnsTheUserAndTreatsAMissingUserAsUnauthenticated(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)

	p, err := a.auth.Profile(a.ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, u.ID, p.ID)
	require.Equal(t, u.Email, p.Email)
	require.Equal(t, u.DisplayName, p.DisplayName)

	_, err = a.auth.Profile(a.ctx, uuid.New())
	require.ErrorIs(t, err, domain.ErrUnauthenticated)
}
