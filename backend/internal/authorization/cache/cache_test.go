package cache_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/cache"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes memakai Redis 8 sungguhan (testkit.Redis). Galat Redis disuntikkan lewat hook go-redis
// sehingga perilaku "Redis mati" terbukti tanpa mematikan container.

var bg = context.Background()

func ptr(u uuid.UUID) *uuid.UUID { return &u }

// clock adalah jam palsu yang dapat dimajukan.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Unix(1_800_000_000, 0)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// faultHook membuat setiap perintah Redis gagal selama fail=true.
type faultHook struct{ fail *atomic.Bool }

func (h faultHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h faultHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if h.fail.Load() {
			return errors.New("redis: kegagalan yang disuntikkan")
		}
		return next(ctx, cmd)
	}
}

func (h faultHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

type rig struct {
	rdb   *redis.Client
	clk   *clock
	fault *atomic.Bool
	c     *cache.Cache
}

func newRig(t *testing.T) rig {
	t.Helper()
	rdb := testkit.Redis(t).Client(t)
	fault := &atomic.Bool{}
	rdb.AddHook(faultHook{fail: fault})
	clk := newClock()
	return rig{rdb: rdb, clk: clk, fault: fault, c: cache.New(rdb, cache.Options{Now: clk.Now, TTL: time.Minute})}
}

func scopeOf(t *testing.T, org, grp *uuid.UUID) domain.ScopeContext {
	t.Helper()
	s, err := domain.NewScope(org, grp)
	require.NoError(t, err)
	return s
}

func effOf(t *testing.T, grants map[string]domain.DecisionLevel) domain.EffectivePermissions {
	t.Helper()
	e, err := domain.EffectiveFromGrants(grants)
	require.NoError(t, err)
	return e
}

func (r rig) verOf(t *testing.T, key string) int64 {
	t.Helper()
	v, err := r.rdb.Get(bg, key).Int64()
	require.NoError(t, err)
	return v
}

func TestMissStoreHitRoundTrip(t *testing.T) {
	r := newRig(t)
	org, user := id.NewID(), id.NewID()
	scope := scopeOf(t, ptr(org), nil)
	want := effOf(t, map[string]domain.DecisionLevel{"user.read": domain.LevelOrganization, "org.read": domain.LevelPlatform})

	miss := r.c.Lookup(bg, user, scope)
	require.False(t, miss.Hit)
	require.NotEmpty(t, miss.Key, "miss dengan Redis sehat harus membawa kunci untuk Store")

	r.c.Store(bg, miss, want)
	hit := r.c.Lookup(bg, user, scope)

	require.True(t, hit.Hit)
	require.Equal(t, want.Grants(), hit.Effective.Grants())
	require.Equal(t, miss.Key, hit.Key)
	require.True(t, strings.HasPrefix(hit.Key, "authz:eff:"))
	require.Contains(t, hit.Key, user.String())
	require.Contains(t, hit.Key, org.String())
}

func TestStoredEntryHasTTL(t *testing.T) {
	r := newRig(t)
	scope := scopeOf(t, ptr(id.NewID()), nil)
	l := r.c.Lookup(bg, id.NewID(), scope)
	r.c.Store(bg, l, effOf(t, map[string]domain.DecisionLevel{"user.read": domain.LevelGroup}))

	ttl, err := r.rdb.TTL(bg, l.Key).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, time.Duration(0), "entri tanpa TTL tidak boleh ada")
	require.LessOrEqual(t, ttl, time.Minute)
}

func TestEntriesAreIsolatedByUserGroupAndOrganization(t *testing.T) {
	r := newRig(t)
	orgA, orgB, alice, bob, grp := id.NewID(), id.NewID(), id.NewID(), id.NewID(), id.NewID()
	base := scopeOf(t, ptr(orgA), nil)
	r.c.Store(bg, r.c.Lookup(bg, alice, base),
		effOf(t, map[string]domain.DecisionLevel{"user.read": domain.LevelOrganization}))

	require.True(t, r.c.Lookup(bg, alice, base).Hit, "kontrol")
	require.False(t, r.c.Lookup(bg, bob, base).Hit, "user lain tidak berbagi entri")
	require.False(t, r.c.Lookup(bg, alice, scopeOf(t, ptr(orgB), nil)).Hit, "organisasi lain tidak berbagi entri")
	require.False(t, r.c.Lookup(bg, alice, scopeOf(t, ptr(orgA), ptr(grp))).Hit, "grup aktif berbeda tidak berbagi entri")
	require.False(t, r.c.Lookup(bg, alice, scopeOf(t, nil, nil)).Hit, "scope platform tidak berbagi entri")
}

func TestBumpOrganizationInvalidatesOnlyThatOrganization(t *testing.T) {
	r := newRig(t)
	orgA, orgB, user := id.NewID(), id.NewID(), id.NewID()
	sa, sb := scopeOf(t, ptr(orgA), nil), scopeOf(t, ptr(orgB), nil)
	eff := effOf(t, map[string]domain.DecisionLevel{"user.read": domain.LevelOrganization})
	r.c.Store(bg, r.c.Lookup(bg, user, sa), eff)
	r.c.Store(bg, r.c.Lookup(bg, user, sb), eff)

	require.NoError(t, r.c.BumpOrganization(bg, orgA))

	require.False(t, r.c.Lookup(bg, user, sa).Hit, "organisasi A harus miss setelah bump")
	require.True(t, r.c.Lookup(bg, user, sb).Hit, "organisasi B tidak ikut terinvalidasi")
}

func TestBumpPlatformInvalidatesEveryOrganizationAndPlatformScope(t *testing.T) {
	r := newRig(t)
	orgA, orgB, user := id.NewID(), id.NewID(), id.NewID()
	scopes := []domain.ScopeContext{scopeOf(t, ptr(orgA), nil), scopeOf(t, ptr(orgB), nil), scopeOf(t, nil, nil)}
	eff := effOf(t, map[string]domain.DecisionLevel{"user.read": domain.LevelPlatform})
	for _, s := range scopes {
		r.c.Store(bg, r.c.Lookup(bg, user, s), eff)
		require.True(t, r.c.Lookup(bg, user, s).Hit, "kontrol")
	}

	require.NoError(t, r.c.BumpPlatform(bg))

	for _, s := range scopes {
		require.False(t, r.c.Lookup(bg, user, s).Hit)
	}
}

func TestResultComputedBeforeABumpIsNeverReadAfterIt(t *testing.T) {
	r := newRig(t)
	org, user := id.NewID(), id.NewID()
	scope := scopeOf(t, ptr(org), nil)

	// request A: miss, mulai menghitung (versi dibaca di sini)
	lookup := r.c.Lookup(bg, user, scope)
	// data berubah dan versi naik sebelum A selesai menghitung
	require.NoError(t, r.c.BumpOrganization(bg, org))
	// A menyimpan hasil lamanya
	r.c.Store(bg, lookup, effOf(t, map[string]domain.DecisionLevel{"user.delete": domain.LevelOrganization}))

	after := r.c.Lookup(bg, user, scope)
	require.False(t, after.Hit, "hasil basi disimpan di bawah versi lama dan tidak pernah dibaca lagi")
	require.NotEqual(t, lookup.Key, after.Key)
}

func TestVersionCountersStartFromClockAndBumpByOne(t *testing.T) {
	r := newRig(t)
	org := id.NewID()
	r.c.Lookup(bg, id.NewID(), scopeOf(t, ptr(org), nil))
	orgKey, platKey := "authz:ver:"+org.String(), "authz:ver:platform"
	start := r.clk.Now().UnixMilli()

	require.Equal(t, start, r.verOf(t, orgKey))
	require.Equal(t, start, r.verOf(t, platKey))

	require.NoError(t, r.c.BumpOrganization(bg, org))
	require.NoError(t, r.c.BumpOrganization(bg, org))
	require.Equal(t, start+2, r.verOf(t, orgKey))
	require.Equal(t, start, r.verOf(t, platKey), "bump organisasi tidak menyentuh platform")
}

func TestLostVersionKeyNeverResurrectsAStaleEntry(t *testing.T) {
	r := newRig(t)
	org, user := id.NewID(), id.NewID()
	scope := scopeOf(t, ptr(org), nil)
	r.c.Store(bg, r.c.Lookup(bg, user, scope),
		effOf(t, map[string]domain.DecisionLevel{"user.read": domain.LevelOrganization}))
	require.True(t, r.c.Lookup(bg, user, scope).Hit, "kontrol")

	// kunci versi tergusur (maxmemory) sementara entri efektif masih ada
	require.NoError(t, r.rdb.Del(bg, "authz:ver:"+org.String()).Err())
	r.clk.Advance(5 * time.Second)

	require.False(t, r.c.Lookup(bg, user, scope).Hit, "versi baru berasal dari jam, tidak kembali ke angka lama")
}

func TestBumpAfterLostVersionKeyStartsAboveTheClock(t *testing.T) {
	r := newRig(t)
	org := id.NewID()
	key := "authz:ver:" + org.String()

	require.NoError(t, r.c.BumpOrganization(bg, org)) // kunci belum ada

	require.Equal(t, r.clk.Now().UnixMilli()+1, r.verOf(t, key))
}

func TestCorruptOrForeignPayloadsAreTreatedAsMissAndOverwritten(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"bukan JSON", `{{{`},
		{"versi format lain", `{"v":2,"g":{"user.read":"organization"}}`},
		{"level tak dikenal", `{"v":1,"g":{"user.read":"galaxy"}}`},
		{"level own di cache", `{"v":1,"g":{"user.read":"own"}}`},
		{"kode kosong", `{"v":1,"g":{"":"group"}}`},
		{"tipe nilai salah", `{"v":1,"g":["user.read"]}`},
		{"string kosong", ``},
		{"tanpa versi format", `{"g":{"user.read":"group"}}`},
		{"null", `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			user := id.NewID()
			scope := scopeOf(t, ptr(id.NewID()), nil)
			l := r.c.Lookup(bg, user, scope)
			require.NoError(t, r.rdb.Set(bg, l.Key, tc.raw, time.Minute).Err())

			got := r.c.Lookup(bg, user, scope)
			require.False(t, got.Hit, "nilai tak sah tidak boleh menghasilkan izin")
			require.Equal(t, 0, got.Effective.Len())
			require.Equal(t, l.Key, got.Key)

			r.c.Store(bg, got, effOf(t, map[string]domain.DecisionLevel{"user.read": domain.LevelGroup}))
			require.True(t, r.c.Lookup(bg, user, scope).Hit, "Store menimpa nilai rusak")
		})
	}
}

func TestCorruptVersionCounterBypassesTheCache(t *testing.T) {
	for _, bad := range []string{"abc", "0", "-5", ""} {
		t.Run("nilai '"+bad+"'", func(t *testing.T) {
			r := newRig(t)
			org := id.NewID()
			require.NoError(t, r.rdb.Set(bg, "authz:ver:"+org.String(), bad, 0).Err())

			l := r.c.Lookup(bg, id.NewID(), scopeOf(t, ptr(org), nil))

			require.False(t, l.Hit)
			require.Empty(t, l.Key, "versi rusak: hitung dari PostgreSQL dan jangan menulis")
		})
	}
}

func TestRedisFailureMeansNoCacheNeverAnAnswer(t *testing.T) {
	r := newRig(t)
	user := id.NewID()
	scope := scopeOf(t, ptr(id.NewID()), nil)
	r.c.Store(bg, r.c.Lookup(bg, user, scope),
		effOf(t, map[string]domain.DecisionLevel{"user.read": domain.LevelOrganization}))
	require.True(t, r.c.Lookup(bg, user, scope).Hit, "kontrol")

	r.fault.Store(true)
	l := r.c.Lookup(bg, user, scope)

	require.False(t, l.Hit, "Redis gagal: entri lama tidak boleh dilayani")
	require.Empty(t, l.Key, "dan tidak boleh ada penulisan")
	require.Equal(t, 0, l.Effective.Len())
	r.c.Store(bg, l, effOf(t, map[string]domain.DecisionLevel{"user.read": domain.LevelOrganization})) // tidak panik
}

func TestBackoffSkipsRedisThenRecovers(t *testing.T) {
	r := newRig(t)
	user := id.NewID()
	scope := scopeOf(t, ptr(id.NewID()), nil)

	r.fault.Store(true)
	require.Empty(t, r.c.Lookup(bg, user, scope).Key)
	r.fault.Store(false)

	require.Empty(t, r.c.Lookup(bg, user, scope).Key, "selama backoff Redis dilewati walau sudah sehat")

	r.clk.Advance(2 * time.Second)
	require.NotEmpty(t, r.c.Lookup(bg, user, scope).Key, "setelah backoff Redis dicoba lagi")
}

func TestBumpReportsRedisFailureEvenDuringBackoff(t *testing.T) {
	r := newRig(t)
	scope := scopeOf(t, ptr(id.NewID()), nil)
	r.fault.Store(true)
	r.c.Lookup(bg, id.NewID(), scope) // memicu backoff
	require.Error(t, r.c.BumpPlatform(bg), "invalidasi tidak boleh diam-diam dilewati")

	r.fault.Store(false)
	require.NoError(t, r.c.BumpPlatform(bg), "invalidasi tidak ikut backoff: langsung dicoba")
}

func TestBumpRejectsNilOrganization(t *testing.T) {
	r := newRig(t)
	require.Error(t, r.c.BumpOrganization(bg, uuid.Nil))
}

func TestInvalidateInTxBumpsBeforeAndAfterCommit(t *testing.T) {
	r := newRig(t)
	tm := kernel.NewTxManager(testkit.NewDB(t).App, nil)
	org := id.NewID()
	key := "authz:ver:" + org.String()
	r.c.Lookup(bg, id.NewID(), scopeOf(t, ptr(org), nil)) // inisialisasi versi
	start := r.verOf(t, key)

	var inside int64
	err := tm.WithinTx(bg, func(ctx context.Context) error {
		require.NoError(t, r.c.InvalidateInTx(ctx, cache.Targets{Organizations: []uuid.UUID{org}}))
		inside = r.verOf(t, key)
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, start+1, inside, "bump pertama terjadi sebelum commit")
	require.Equal(t, start+2, r.verOf(t, key), "bump kedua terjadi setelah commit")
}

func TestInvalidateInTxAfterRollbackKeepsOnlyThePreCommitBump(t *testing.T) {
	r := newRig(t)
	tm := kernel.NewTxManager(testkit.NewDB(t).App, nil)
	org := id.NewID()
	key := "authz:ver:" + org.String()
	r.c.Lookup(bg, id.NewID(), scopeOf(t, ptr(org), nil))
	start := r.verOf(t, key)

	err := tm.WithinTx(bg, func(ctx context.Context) error {
		require.NoError(t, r.c.InvalidateInTx(ctx, cache.Targets{Organizations: []uuid.UUID{org}}))
		return errors.New("bisnis gagal")
	})

	require.Error(t, err)
	require.Equal(t, start+1, r.verOf(t, key), "rollback: hanya bump sebelum commit (tidak berbahaya)")
}

func TestInvalidateInTxFailureBeforeCommitAbortsTheTransaction(t *testing.T) {
	r := newRig(t)
	tm := kernel.NewTxManager(testkit.NewDB(t).App, nil)
	r.fault.Store(true)

	err := tm.WithinTx(bg, func(ctx context.Context) error {
		return r.c.InvalidateInTx(ctx, cache.Targets{Platform: true})
	})

	require.Error(t, err, "bump sebelum commit gagal: transaksi dibatalkan (fail-closed)")
}

func TestInvalidateInTxDeduplicatesAndIgnoresNilOrganizations(t *testing.T) {
	r := newRig(t)
	org := id.NewID()
	key := "authz:ver:" + org.String()
	r.c.Lookup(bg, id.NewID(), scopeOf(t, ptr(org), nil))
	start := r.verOf(t, key)

	// tanpa transaksi pada ctx: kedua bump berjalan langsung (sekali sebelum, sekali sesudah)
	require.NoError(t, r.c.InvalidateInTx(bg, cache.Targets{Organizations: []uuid.UUID{org, org, uuid.Nil}}))

	require.Equal(t, start+2, r.verOf(t, key), "org ganda dihitung sekali; UUID nol diabaikan")
}

func TestInvalidateInTxWithNoTargetsIsANoOp(t *testing.T) {
	r := newRig(t)
	r.fault.Store(true) // seandainya menyentuh Redis, akan gagal
	require.NoError(t, r.c.InvalidateInTx(bg, cache.Targets{}))
}

func TestVersionValuesAreDecimalIntegers(t *testing.T) {
	r := newRig(t)
	org := id.NewID()
	r.c.Lookup(bg, id.NewID(), scopeOf(t, ptr(org), nil))
	s, err := r.rdb.Get(bg, "authz:ver:"+org.String()).Result()
	require.NoError(t, err)
	_, perr := strconv.ParseInt(s, 10, 64)
	require.NoError(t, perr)
}
