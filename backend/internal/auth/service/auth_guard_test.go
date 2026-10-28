package service_test

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/security"
)

// Login dengan penjaga (ADR-0020 bagian 5): Postgres dan Redis sungguhan; jam palsu dibagi dengan SessionService.

// newGuardedAuthHarness seperti newAuthHarness, tetapi dengan LoginGuard bawaan. rdb nil = Redis harness.
func newGuardedAuthHarness(t *testing.T, rdb redis.UniversalClient) *authHarness {
	t.Helper()
	h := newHarness(t)
	if rdb == nil {
		rdb = h.rdb
	}
	base, err := security.NewArgon2idHasher(testParams, 2)
	require.NoError(t, err)
	spy := &spyHasher{PasswordHasher: base}
	aud := &authAudit{}
	log := slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	g, err := service.NewGuard(rdb, service.DefaultGuardPolicy(), log, service.WithGuardClock(h.clk.now))
	require.NoError(t, err)
	as, err := service.NewAuthService(service.AuthDeps{
		Store: h.store, Tx: h.tm, Sessions: h.svc, Hasher: spy, Audit: aud, Bus: kernel.NewBus(h.db.App, nil),
		Guard: g, Log: log,
	})
	require.NoError(t, err)
	return &authHarness{harness: h, auth: as, spy: spy, audit: aud}
}

// loginFrom melakukan login dengan asal klien (IP) tertentu.
func (a *authHarness) loginFrom(ip, email, password string) (service.LoginResult, error) {
	a.t.Helper()
	ctx := kernel.WithClient(a.ctx, kernel.Client{IP: ip, UserAgent: "agen-uji/1.0"})
	return a.auth.Login(ctx, service.LoginInput{Email: email, Password: password})
}

func (a *authHarness) eventCount(u testkit.User, eventType string) int {
	a.t.Helper()
	return a.count(`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2`, u.ID, eventType)
}

// authKeys mengembalikan semua kunci Redis milik penjaga (awalan "auth:").
func (a *authHarness) authKeys() []string {
	a.t.Helper()
	keys, err := a.rdb.Keys(a.ctx, "auth:*").Result()
	require.NoError(a.t, err)
	return keys
}

func TestLoginLocksTheAccountHidesTheLockAndKeepsExistingSessions(t *testing.T) {
	a := newGuardedAuthHarness(t, nil)
	org := testkit.NewOrganization(t, a.db)
	u := a.newUserWithPassword(goodPassword, testkit.InOrganization(org))
	const ip = "203.0.113.10"

	first, err := a.loginFrom(ip, u.Email, goodPassword) // sesi yang harus tetap sah setelah kunci
	require.NoError(t, err)

	for i := 1; i <= 5; i++ {
		_, err := a.loginFrom(ip, u.Email, "salah-sekali-"+strconv.Itoa(i))
		require.ErrorIs(t, err, domain.ErrInvalidCredentials, "kegagalan ke-%d", i)
	}
	require.Equal(t, 5, a.eventCount(u, service.EventLoginFailed))
	require.Equal(t, 1, a.eventCount(u, service.EventAccountLocked), "event kunci ditulis sekali")
	require.Equal(t, 1, a.count(`SELECT count(*) FROM security_events
		WHERE user_id = $1 AND event_type = $2 AND result = 'failure'
		  AND (metadata->>'failures')::int = 5 AND (metadata->>'lock_seconds')::int = 900`,
		u.ID, service.EventAccountLocked))

	compares, dummies := a.spy.compares.Load(), a.spy.dummies.Load()
	sessions, events := a.sessionCount(u), len(a.events(u))

	// Password BENAR saat kunci aktif: tetap invalid_credentials (bukan account_locked), tanpa sesi, tanpa catatan.
	_, err = a.loginFrom(ip, u.Email, goodPassword)
	require.Equal(t, domain.ErrInvalidCredentials, err)
	require.NotErrorIs(t, err, domain.ErrAccountLocked, "kunci Redis tak pernah terlihat (butir 47)")
	require.Equal(t, sessions, a.sessionCount(u), "password benar pun tak menghasilkan sesi saat terkunci")
	require.Equal(t, events, len(a.events(u)), "percobaan saat terkunci tak dicatat")
	require.Equal(t, compares, a.spy.compares.Load(), "password tak diverifikasi terhadap hash asli saat terkunci")
	require.Equal(t, dummies+1, a.spy.dummies.Load(), "biaya Argon2 setara lewat verifikasi dummy")

	_, err = a.svc.Authenticate(a.ctx, first.Issued.Token)
	require.NoError(t, err, "kunci hanya menutup login baru; sesi yang ada tetap sah (butir 48)")

	a.clk.advance(16 * time.Minute)
	_, err = a.loginFrom(ip, u.Email, goodPassword)
	require.NoError(t, err, "kunci berakhir menurut TTL")
}

func TestLockedLoginCostsAndLooksLikeAnyOtherFailure(t *testing.T) {
	a := newGuardedAuthHarness(t, nil)
	u := a.newUserWithPassword(goodPassword)
	const ip = "203.0.113.11"
	snap := func() (int64, int64) { return a.spy.compares.Load(), a.spy.dummies.Load() }

	c0, d0 := snap()
	_, errWrong := a.loginFrom(ip, u.Email, "salah-1")
	c1, d1 := snap()
	require.Equal(t, domain.ErrInvalidCredentials, errWrong)
	require.Equal(t, [2]int64{c0 + 1, d0}, [2]int64{c1, d1}, "password salah: satu verifikasi nyata")

	_, errUnknown := a.loginFrom(ip, "tak-dikenal@example.org", "x")
	c2, d2 := snap()
	require.Equal(t, domain.ErrInvalidCredentials, errUnknown)
	require.Equal(t, [2]int64{c1, d1 + 1}, [2]int64{c2, d2}, "email tak dikenal: satu verifikasi dummy")

	for i := 2; i <= 5; i++ {
		_, err := a.loginFrom(ip, u.Email, "salah-"+strconv.Itoa(i))
		require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	}
	c3, d3 := snap()

	_, errLockedCorrect := a.loginFrom(ip, u.Email, goodPassword)
	_, errLockedWrong := a.loginFrom(ip, u.Email, "salah-lagi")
	c4, d4 := snap()
	require.Equal(t, domain.ErrInvalidCredentials, errLockedCorrect)
	require.Equal(t, domain.ErrInvalidCredentials, errLockedWrong)
	require.Equal(t, [2]int64{c3, d3 + 2}, [2]int64{c4, d4}, "terkunci: tiap percobaan satu verifikasi dummy, sama mahalnya")
}

func TestLoginCountsOnlyWrongPasswordsOfKnownAccounts(t *testing.T) {
	a := newGuardedAuthHarness(t, nil)
	const ip = "203.0.113.12"

	suspended := a.newUserWithPassword(goodPassword)
	a.setStatus(suspended, "suspended")
	adminLocked := a.newUserWithPassword(goodPassword)
	a.setStatus(adminLocked, "locked")
	for i := 0; i < 10; i++ {
		_, err := a.loginFrom(ip, suspended.Email, goodPassword)
		require.ErrorIs(t, err, domain.ErrAccountSuspended)
		_, err = a.loginFrom(ip, adminLocked.Email, goodPassword)
		require.ErrorIs(t, err, domain.ErrAccountLocked, "kunci administratif (users.status) tetap 423 setelah password benar")
	}
	require.Empty(t, a.authKeys(), "kegagalan status setelah password benar tidak menaikkan penghitung apa pun")

	for i := 0; i < 10; i++ {
		_, err := a.loginFrom(ip, fmt.Sprintf("tak-dikenal-%d@example.org", i), "x")
		require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	}
	keys := a.authKeys()
	require.Equal(t, []string{"auth:ipfail:" + ip}, keys, "email tak dikenal hanya menaikkan penghitung IP, tak ada kunci akun")
}

func TestLoginSuccessResetsTheFailureCounter(t *testing.T) {
	a := newGuardedAuthHarness(t, nil)
	u := a.newUserWithPassword(goodPassword)
	const ip = "203.0.113.13"

	for i := 0; i < 4; i++ {
		_, err := a.loginFrom(ip, u.Email, "salah-sekali")
		require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	}
	_, err := a.loginFrom(ip, u.Email, goodPassword)
	require.NoError(t, err)

	for i := 0; i < 4; i++ {
		_, err := a.loginFrom(ip, u.Email, "salah-sekali")
		require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	}
	require.Zero(t, a.eventCount(u, service.EventAccountLocked), "4 + sukses + 4 bukan 8 kegagalan beruntun")

	_, err = a.loginFrom(ip, u.Email, "salah-sekali")
	require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	require.Equal(t, 1, a.eventCount(u, service.EventAccountLocked), "kegagalan ke-5 sejak reset mengunci")
}

func TestLoginIPGateRejectsBeforeLookupAndHidesAccountExistence(t *testing.T) {
	a := newGuardedAuthHarness(t, nil)
	u := a.newUserWithPassword(goodPassword)
	const bad = "198.51.100.30"

	for i := 0; i < 30; i++ {
		_, err := a.loginFrom(bad, fmt.Sprintf("tak-ada-%d@example.org", i), "x")
		require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	}
	compares, dummies := a.spy.compares.Load(), a.spy.dummies.Load()

	_, errKnown := a.loginFrom(bad, u.Email, goodPassword)
	_, errUnknown := a.loginFrom(bad, "tak-ada-x@example.org", "x")
	var throttledKnown, throttledUnknown *domain.ThrottledError
	require.ErrorAs(t, errKnown, &throttledKnown, "akun dikenal + password benar dari IP yang ditolak: 429")
	require.ErrorAs(t, errUnknown, &throttledUnknown, "email tak dikenal: respons yang sama")
	require.Greater(t, throttledKnown.RetryAfter, 14*time.Minute)
	require.Equal(t, compares, a.spy.compares.Load(), "gerbang IP menolak sebelum Argon2")
	require.Equal(t, dummies, a.spy.dummies.Load(), "gerbang IP menolak sebelum Argon2")
	require.Zero(t, a.sessionCount(u))

	_, err := a.loginFrom("198.51.100.31", u.Email, goodPassword)
	require.NoError(t, err, "IP lain tak terpengaruh")
}

func TestLoginIPGateGroupsIPv6ByPrefix64(t *testing.T) {
	a := newGuardedAuthHarness(t, nil)
	u := a.newUserWithPassword(goodPassword)

	for i := 1; i <= 30; i++ { // 30 alamat berbeda di /64 yang sama
		_, err := a.loginFrom(fmt.Sprintf("2001:db8:1:2::%x", i), fmt.Sprintf("tak-ada-%d@example.org", i), "x")
		require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	}
	_, err := a.loginFrom("2001:db8:1:2:ffff::9", u.Email, goodPassword)
	var throttled *domain.ThrottledError
	require.ErrorAs(t, err, &throttled, "alamat baru di /64 yang sama ikut ditolak")

	_, err = a.loginFrom("2001:db8:1:3::1", u.Email, goodPassword)
	require.NoError(t, err, "/64 lain tak terpengaruh")
}

func TestChangePasswordFailuresNeverTouchTheLoginGuard(t *testing.T) {
	a := newGuardedAuthHarness(t, nil)
	org := testkit.NewOrganization(t, a.db)
	u := a.newUserWithPassword(goodPassword, testkit.InOrganization(org))
	_, ident := a.loginIdentity(u)

	for i := 0; i < 10; i++ {
		_, err := a.auth.ChangePassword(a.ctx, ident, service.ChangePasswordInput{Current: "salah-sekali-ini", New: newPassword})
		require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	}
	require.Empty(t, a.authKeys(), "ADR-0020 butir 45: kegagalan ganti password tidak menaikkan penghitung kunci")
	require.Zero(t, a.eventCount(u, service.EventAccountLocked))

	_, err := a.login(u.Email, goodPassword)
	require.NoError(t, err, "akun tidak terkunci oleh kegagalan ganti password")
}

func TestLoginWithRedisDownStillWorksAndStillLocks(t *testing.T) {
	dead := deadRedis()
	t.Cleanup(func() { _ = dead.Close() })
	a := newGuardedAuthHarness(t, dead)
	u := a.newUserWithPassword(goodPassword)
	const ip = "203.0.113.14"

	_, err := a.loginFrom(ip, u.Email, goodPassword)
	require.NoError(t, err, "Redis penjaga mati: login sah tetap jalan (degraded)")

	for i := 0; i < 5; i++ {
		_, err := a.loginFrom(ip, u.Email, "salah-sekali")
		require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	}
	_, err = a.loginFrom(ip, u.Email, goodPassword)
	require.Equal(t, domain.ErrInvalidCredentials, err, "kunci tetap berlaku lewat fallback memori (bukan fail open)")
	require.Equal(t, 1, a.eventCount(u, service.EventAccountLocked))
	require.True(t, strings.Contains(a.logs.String(), "login guard: Redis tak tersedia"))
}

func TestConcurrentWrongPasswordsWriteExactlyOneLockedEvent(t *testing.T) {
	a := newGuardedAuthHarness(t, nil)
	u := a.newUserWithPassword(goodPassword)
	const ip = "203.0.113.15"

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = a.loginFrom(ip, u.Email, "salah-paralel")
		}()
	}
	wg.Wait()

	require.Equal(t, 1, a.eventCount(u, service.EventAccountLocked), "tepat satu event kunci walau 20 percobaan bersamaan")
	require.GreaterOrEqual(t, a.eventCount(u, service.EventLoginFailed), 5)
	_, err := a.loginFrom(ip, u.Email, goodPassword)
	require.Equal(t, domain.ErrInvalidCredentials, err)
}
