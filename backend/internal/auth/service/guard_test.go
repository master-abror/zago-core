package service_test

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/service"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Penjaga login (ADR-0020 bagian 5). Tes memakai Redis sungguhan dan jam palsu (tanpa tidur nyata):
// kedaluwarsa logis ada di nilai kunci, jadi memajukan jam cukup untuk melewati kunci dan jendela.

type guardHarness struct {
	t    *testing.T
	ctx  context.Context
	rdb  redis.UniversalClient
	clk  *fakeClock
	logs *syncBuf
	g    *service.Guard
}

func newGuardHarness(t *testing.T, rdb redis.UniversalClient, p service.GuardPolicy, opts ...service.GuardOption) *guardHarness {
	t.Helper()
	h := &guardHarness{t: t, ctx: context.Background(), rdb: rdb, clk: &fakeClock{}, logs: &syncBuf{}}
	log := slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	opts = append([]service.GuardOption{service.WithGuardClock(h.clk.now)}, opts...)
	g, err := service.NewGuard(rdb, p, log, opts...)
	require.NoError(t, err)
	h.g = g
	return h
}

func redisGuard(t *testing.T, p service.GuardPolicy, opts ...service.GuardOption) *guardHarness {
	t.Helper()
	return newGuardHarness(t, testkit.Redis(t).Client(t), p, opts...)
}

// guardFailN mencatat n kegagalan password untuk akun uid dan mengembalikan hasil yang terakhir.
func guardFailN(h *guardHarness, uid uuid.UUID, ip string, n int) service.FailureResult {
	h.t.Helper()
	var res service.FailureResult
	for i := 0; i < n; i++ {
		res = h.g.RecordFailure(h.ctx, &uid, ip)
	}
	return res
}

func TestGuardLocksAtThresholdAndExpiresByClock(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	uid := id.NewID()

	for i := 1; i < 5; i++ {
		res := h.g.RecordFailure(h.ctx, &uid, "")
		require.False(t, res.Locked, "kegagalan ke-%d belum boleh mengunci", i)
		require.Equal(t, i, res.Failures)
		require.False(t, h.g.AccountLocked(h.ctx, uid), "belum terkunci setelah %d kegagalan", i)
	}

	res := h.g.RecordFailure(h.ctx, &uid, "")
	require.True(t, res.Locked, "kegagalan ke-5 memasang kunci")
	require.Equal(t, 5, res.Failures)
	require.Equal(t, 15*time.Minute, res.LockFor)
	require.Equal(t, 1, res.Strikes)
	require.True(t, h.g.AccountLocked(h.ctx, uid))

	h.clk.advance(15*time.Minute - 5*time.Second)
	require.True(t, h.g.AccountLocked(h.ctx, uid), "kunci masih berlaku sebelum durasi habis")
	h.clk.advance(10 * time.Second)
	require.False(t, h.g.AccountLocked(h.ctx, uid), "kunci berakhir menurut jam aplikasi (TTL)")
}

func TestGuardProgressiveLockDoublesUpToCap(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	uid := id.NewID()

	want := []time.Duration{15 * time.Minute, 30 * time.Minute, 60 * time.Minute, 60 * time.Minute}
	for i, w := range want {
		res := guardFailN(h, uid, "", 5)
		require.True(t, res.Locked, "kunci ke-%d", i+1)
		require.Equal(t, w, res.LockFor, "durasi kunci ke-%d", i+1)
		require.Equal(t, i+1, res.Strikes)
		require.True(t, h.g.AccountLocked(h.ctx, uid))
		h.clk.advance(w + time.Minute)
		require.False(t, h.g.AccountLocked(h.ctx, uid))
	}
}

func TestGuardStrikesExpireAfterADay(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	uid := id.NewID()

	require.True(t, guardFailN(h, uid, "", 5).Locked)
	h.clk.advance(25 * time.Hour)
	res := guardFailN(h, uid, "", 5)
	require.True(t, res.Locked)
	require.Equal(t, 15*time.Minute, res.LockFor, "strike lama sudah gugur: kembali ke durasi dasar")
	require.Equal(t, 1, res.Strikes)
}

func TestGuardFailureWindowResets(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	uid := id.NewID()

	guardFailN(h, uid, "", 4)
	h.clk.advance(16 * time.Minute)
	res := guardFailN(h, uid, "", 4)
	require.False(t, res.Locked, "jendela lama berakhir: empat kegagalan baru tidak mengunci")
	require.Equal(t, 4, res.Failures, "penghitung dimulai ulang di jendela baru")
	require.False(t, h.g.AccountLocked(h.ctx, uid))
}

func TestGuardSuccessResetsAccountCountersAndStrikes(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	uid := id.NewID()

	guardFailN(h, uid, "", 4)
	h.g.RecordSuccess(h.ctx, uid)
	res := guardFailN(h, uid, "", 4)
	require.False(t, res.Locked, "login sukses mereset penghitung akun")
	require.Equal(t, 4, res.Failures)

	// Strike juga direset: kunci berikutnya kembali ke durasi dasar.
	require.True(t, guardFailN(h, uid, "", 1).Locked)
	h.clk.advance(16 * time.Minute)
	h.g.RecordSuccess(h.ctx, uid)
	res = guardFailN(h, uid, "", 5)
	require.True(t, res.Locked)
	require.Equal(t, 15*time.Minute, res.LockFor)
	require.Equal(t, 1, res.Strikes)
}

func TestGuardFailuresDuringLockAreNotCountedAndDoNotExtend(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	uid := id.NewID()

	require.True(t, guardFailN(h, uid, "", 5).Locked)
	h.clk.advance(10 * time.Minute)
	for i := 0; i < 3; i++ {
		res := h.g.RecordFailure(h.ctx, &uid, "")
		require.False(t, res.Locked, "kunci aktif: tak dipasang ulang")
		require.Zero(t, res.Failures, "kunci aktif: percobaan tak dihitung")
	}
	require.True(t, h.g.AccountLocked(h.ctx, uid))

	h.clk.advance(5*time.Minute + 10*time.Second)
	require.False(t, h.g.AccountLocked(h.ctx, uid), "kunci tidak diperpanjang oleh percobaan saat terkunci")
	require.Equal(t, 1, guardFailN(h, uid, "", 1).Failures, "penghitung tidak tercemar percobaan saat terkunci")
}

func TestGuardConcurrentFailuresLockExactlyOnce(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	uid := id.NewID()

	var locked atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if h.g.RecordFailure(h.ctx, &uid, "").Locked {
				locked.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(1), locked.Load(), "tepat satu kegagalan yang memasang kunci (satu event auth.account.locked)")
	require.True(t, h.g.AccountLocked(h.ctx, uid))
}

func TestGuardAccountsAreIndependent(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	a, b := id.NewID(), id.NewID()

	require.True(t, guardFailN(h, a, "", 5).Locked)
	require.True(t, h.g.AccountLocked(h.ctx, a))
	require.False(t, h.g.AccountLocked(h.ctx, b), "kunci satu akun tak menyentuh akun lain")
	require.Equal(t, 1, guardFailN(h, b, "", 1).Failures)
}

func TestGuardIPBlocksAfterThresholdAndSuccessDoesNotReset(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	const ip = "203.0.113.7"

	for i := 0; i < 29; i++ {
		h.g.RecordFailure(h.ctx, nil, ip) // email tak dikenal: hanya penghitung IP
	}
	_, blocked := h.g.IPBlocked(h.ctx, ip)
	require.False(t, blocked, "29 kegagalan: belum ditolak")

	h.g.RecordFailure(h.ctx, nil, ip)
	retry, blocked := h.g.IPBlocked(h.ctx, ip)
	require.True(t, blocked, "kegagalan ke-30: IP ditolak")
	require.Greater(t, retry, 14*time.Minute)
	require.LessOrEqual(t, retry, 15*time.Minute)

	_, other := h.g.IPBlocked(h.ctx, "203.0.113.8")
	require.False(t, other, "IP lain tak terpengaruh")

	// Login sukses (mis. penyerang dengan akun sah sendiri) TIDAK mereset penghitung IP.
	h.g.RecordSuccess(h.ctx, id.NewID())
	_, blocked = h.g.IPBlocked(h.ctx, ip)
	require.True(t, blocked, "penghitung IP tidak direset oleh login sukses")

	h.clk.advance(15*time.Minute + time.Second)
	_, blocked = h.g.IPBlocked(h.ctx, ip)
	require.False(t, blocked, "jendela IP berakhir")
}

func TestGuardIPCountsKnownAndUnknownAccountsAlike(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	const ip = "198.51.100.20"

	accounts := make([]uuid.UUID, 30)
	for i := range accounts {
		accounts[i] = id.NewID()
		h.g.RecordFailure(h.ctx, &accounts[i], ip) // satu kegagalan per akun dikenal
	}
	_, blocked := h.g.IPBlocked(h.ctx, ip)
	require.True(t, blocked, "kegagalan akun dikenal ikut menaikkan penghitung IP")
	for _, a := range accounts {
		require.False(t, h.g.AccountLocked(h.ctx, a), "satu kegagalan per akun tidak mengunci akun mana pun")
	}
}

func TestGuardEmptyIPIsNeverTracked(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	for i := 0; i < 100; i++ {
		h.g.RecordFailure(h.ctx, nil, "")
	}
	_, blocked := h.g.IPBlocked(h.ctx, "")
	require.False(t, blocked, "IP tak diketahui tidak dibatasi (rate limiter yang menjaga)")
	keys, err := h.rdb.Keys(h.ctx, "*").Result()
	require.NoError(t, err)
	require.Empty(t, keys, "IP kosong tidak menulis kunci")
}

func TestGuardRedisKeysAreNamedBoundedAndUnlockable(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	uid := id.NewID()
	const ip = "192.0.2.55"

	require.True(t, guardFailN(h, uid, ip, 5).Locked)

	keys, err := h.rdb.Keys(h.ctx, "*").Result()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{
		"auth:lock:" + uid.String(),
		"auth:strike:" + uid.String(),
		"auth:ipfail:" + ip,
	}, keys, "penghitung kegagalan akun dihapus saat kunci dipasang; tak ada kunci lain")
	for _, k := range keys {
		require.NotContains(t, k, "@", "email tak pernah masuk kunci")
		ttl, err := h.rdb.TTL(h.ctx, k).Result()
		require.NoError(t, err)
		require.Greater(t, ttl, time.Duration(0), "setiap kunci punya TTL: %s", k)
		require.LessOrEqual(t, ttl, 24*time.Hour+2*time.Minute, "TTL terbatas: %s", k)
	}

	v, err := h.rdb.Get(h.ctx, "auth:lock:"+uid.String()).Result()
	require.NoError(t, err)
	until, err := strconv.ParseInt(v, 10, 64)
	require.NoError(t, err, "nilai kunci = waktu berakhir dalam ms epoch")
	require.Greater(t, until, h.clk.now().UnixMilli())

	// Pembukaan kunci administratif sementara (ADR-0020 "Konsekuensi"): DEL auth:lock:<uuid>.
	require.NoError(t, h.rdb.Del(h.ctx, "auth:lock:"+uid.String()).Err())
	require.False(t, h.g.AccountLocked(h.ctx, uid))
}

func TestGuardMalformedLockValueFailsClosed(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	uid := id.NewID()
	require.NoError(t, h.rdb.Set(h.ctx, "auth:lock:"+uid.String(), "bukan-angka", time.Minute).Err())
	require.True(t, h.g.AccountLocked(h.ctx, uid), "nilai kunci rusak dianggap terkunci (gagal tertutup)")

	const ip = "192.0.2.77"
	require.NoError(t, h.rdb.Set(h.ctx, "auth:ipfail:"+ip, "rusak", time.Minute).Err())
	_, blocked := h.g.IPBlocked(h.ctx, ip)
	require.True(t, blocked, "penghitung IP rusak dianggap ditolak (gagal tertutup)")
}

func TestGuardCanceledContextDoesNotMarkRedisDown(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	uid := id.NewID()

	ctx, cancel := context.WithCancel(h.ctx)
	cancel()
	h.g.RecordFailure(ctx, &uid, "") // klien membatalkan: dihitung di memori, bukan tanda Redis mati

	res := h.g.RecordFailure(h.ctx, &uid, "")
	require.Equal(t, 1, res.Failures, "Redis masih dipakai (hitungannya terpisah dari memori)")
	require.NotContains(t, h.logs.String(), "Redis tak tersedia")
}

// ---- Redis mati: fallback memori ----

func TestGuardRedisDownFallsBackToMemoryWithSameSemantics(t *testing.T) {
	dead := deadRedis()
	t.Cleanup(func() { _ = dead.Close() })
	h := newGuardHarness(t, dead, service.DefaultGuardPolicy())
	uid := id.NewID()

	for i := 1; i < 5; i++ {
		res := h.g.RecordFailure(h.ctx, &uid, "")
		require.False(t, res.Locked)
		require.Equal(t, i, res.Failures)
	}
	res := h.g.RecordFailure(h.ctx, &uid, "")
	require.True(t, res.Locked, "Redis mati: kunci tetap berlaku lewat memori (bukan fail open)")
	require.Equal(t, 15*time.Minute, res.LockFor)
	require.True(t, h.g.AccountLocked(h.ctx, uid))
	require.Contains(t, h.logs.String(), "login guard: Redis tak tersedia")

	// Kegagalan saat terkunci tidak dihitung; kunci berakhir menurut jam; jeda progresif tetap berlaku.
	require.Zero(t, h.g.RecordFailure(h.ctx, &uid, "").Failures)
	h.clk.advance(16 * time.Minute)
	require.False(t, h.g.AccountLocked(h.ctx, uid))
	res = guardFailN(h, uid, "", 5)
	require.True(t, res.Locked)
	require.Equal(t, 30*time.Minute, res.LockFor, "strike di memori: kunci kedua berlipat")

	// Sukses mereset akun.
	h.clk.advance(31 * time.Minute)
	h.g.RecordSuccess(h.ctx, uid)
	require.Equal(t, 1, guardFailN(h, uid, "", 1).Failures)

	// Gerbang IP juga berjalan lewat memori.
	const ip = "203.0.113.99"
	for i := 0; i < 30; i++ {
		h.g.RecordFailure(h.ctx, nil, ip)
	}
	retry, blocked := h.g.IPBlocked(h.ctx, ip)
	require.True(t, blocked)
	require.Greater(t, retry, time.Duration(0))
}

func TestGuardMemoryTableFailsClosedWhenFullOfActiveEntries(t *testing.T) {
	dead := deadRedis()
	t.Cleanup(func() { _ = dead.Close() })
	h := newGuardHarness(t, dead, service.DefaultGuardPolicy(), service.WithGuardMemoryLimit(3))

	for _, ip := range []string{"192.0.2.1", "192.0.2.2", "192.0.2.3"} {
		h.g.RecordFailure(h.ctx, nil, ip)
	}
	retry, blocked := h.g.IPBlocked(h.ctx, "192.0.2.4")
	require.True(t, blocked, "tabel penuh oleh entri aktif: IP baru ditolak")
	require.GreaterOrEqual(t, retry, time.Second)

	_, blocked = h.g.IPBlocked(h.ctx, "192.0.2.1")
	require.False(t, blocked, "IP yang sudah terlacak (di bawah ambang) tidak diusir dan tidak ditolak")

	require.True(t, h.g.AccountLocked(h.ctx, id.NewID()), "akun baru saat tabel penuh: ditolak (gagal tertutup)")
	h.g.RecordFailure(h.ctx, nil, "192.0.2.9") // tak terlacak: tidak panik, tidak mengusir entri aktif
	_, blocked = h.g.IPBlocked(h.ctx, "192.0.2.4")
	require.True(t, blocked)

	h.clk.advance(16 * time.Minute)
	_, blocked = h.g.IPBlocked(h.ctx, "192.0.2.4")
	require.False(t, blocked, "entri berakhir dibersihkan: tabel punya tempat lagi")
	require.False(t, h.g.AccountLocked(h.ctx, id.NewID()))
}

// ---- Kebijakan ----

func TestGuardPolicyValidation(t *testing.T) {
	require.NoError(t, service.DefaultGuardPolicy().Validate())

	cases := []struct {
		name string
		mut  func(*service.GuardPolicy)
	}{
		{"MaxFailures nol", func(p *service.GuardPolicy) { p.MaxFailures = 0 }},
		{"MaxFailures terlalu besar", func(p *service.GuardPolicy) { p.MaxFailures = 101 }},
		{"IPMaxFailures nol", func(p *service.GuardPolicy) { p.IPMaxFailures = 0 }},
		{"IPMaxFailures terlalu besar", func(p *service.GuardPolicy) { p.IPMaxFailures = 10001 }},
		{"jendela nol", func(p *service.GuardPolicy) { p.FailureWindow = 0 }},
		{"jendela > 24h", func(p *service.GuardPolicy) { p.FailureWindow = 25 * time.Hour }},
		{"durasi kunci nol", func(p *service.GuardPolicy) { p.LockDuration = 0 }},
		{"LockMax < LockDuration", func(p *service.GuardPolicy) { p.LockMax = p.LockDuration - time.Second }},
		{"LockMax > 24h", func(p *service.GuardPolicy) { p.LockMax = 25 * time.Hour }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := service.DefaultGuardPolicy()
			c.mut(&p)
			require.Error(t, p.Validate())

			dead := deadRedis()
			t.Cleanup(func() { _ = dead.Close() })
			_, err := service.NewGuard(dead, p, nil)
			require.Error(t, err, "NewGuard menolak kebijakan tak sah")
		})
	}

	t.Run("nilai nol bukan kebijakan sah", func(t *testing.T) {
		require.Error(t, service.GuardPolicy{}.Validate())
	})
}

func TestNewGuardRequiresRedis(t *testing.T) {
	_, err := service.NewGuard(nil, service.DefaultGuardPolicy(), nil)
	require.Error(t, err)
}

func TestGuardLockedKeyNeverContainsEmailOrIPInValue(t *testing.T) {
	h := redisGuard(t, service.DefaultGuardPolicy())
	uid := id.NewID()
	const ip = "192.0.2.200"
	guardFailN(h, uid, ip, 5)

	keys, err := h.rdb.Keys(h.ctx, "*").Result()
	require.NoError(t, err)
	for _, k := range keys {
		v, err := h.rdb.Get(h.ctx, k).Result()
		require.NoError(t, err)
		require.False(t, strings.Contains(v, ip) || strings.Contains(v, "@"), "nilai %s tak memuat IP atau email", k)
	}
}
