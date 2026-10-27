package service_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/repository"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
)

// Semua tes memakai PostgreSQL dan Redis sungguhan (docs/22 §10.1). Waktu dimajukan lewat jam
// palsu yang berjalan di atas jam nyata, supaya last_activity_at/created_at dari PostgreSQL
// (now()) tetap sebanding dengan jam layanan.

const (
	idle     = 30 * time.Minute
	absolute = 12 * time.Hour
)

type fakeClock struct{ offset atomic.Int64 }

func (c *fakeClock) now() time.Time          { return time.Now().Add(time.Duration(c.offset.Load())) }
func (c *fakeClock) advance(d time.Duration) { c.offset.Add(int64(d)) }

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type harness struct {
	t     *testing.T
	ctx   context.Context
	db    *testpg.DB
	rdb   *redis.Client
	store *repository.Store
	tm    *kernel.TxManager
	clk   *fakeClock
	logs  *syncBuf
	svc   *service.SessionService
	user  testkit.User
}

func newHarness(t *testing.T) *harness {
	return newHarnessWith(t, service.StaticTimeouts{Idle: idle, Absolute: absolute})
}

func newHarnessWith(t *testing.T, tmo service.TimeoutSource) *harness {
	t.Helper()
	db := testkit.NewDB(t)
	rdb := testkit.Redis(t).Client(t)
	h := &harness{
		t: t, ctx: context.Background(), db: db, rdb: rdb,
		store: repository.NewStore(db.App), tm: kernel.NewTxManager(db.App, nil),
		clk: &fakeClock{}, logs: &syncBuf{}, user: testkit.NewUser(t, db),
	}
	h.svc = h.newService(rdb, tmo)
	return h
}

func (h *harness) newService(rdb *redis.Client, tmo service.TimeoutSource) *service.SessionService {
	log := slog.New(slog.NewTextHandler(h.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return service.NewSessionService(h.store, rdb, tmo, log, service.WithClock(h.clk.now))
}

func (h *harness) create(userID uuid.UUID) service.Issued {
	h.t.Helper()
	iss, err := h.svc.Create(h.ctx, service.CreateParams{UserID: userID})
	require.NoError(h.t, err)
	return iss
}

func tokenHashHex(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (h *harness) row(token string) repository.Session {
	h.t.Helper()
	sum := sha256.Sum256([]byte(token))
	r, err := h.store.Q(h.ctx).GetSessionByTokenHash(h.ctx, sum[:])
	require.NoError(h.t, err)
	return r
}

func (h *harness) cached(token string) bool {
	h.t.Helper()
	n, err := h.rdb.Exists(h.ctx, "session:"+tokenHashHex(token)).Result()
	require.NoError(h.t, err)
	return n == 1
}

func (h *harness) requireUnauthenticated(token string) {
	h.t.Helper()
	_, err := h.svc.Authenticate(h.ctx, token)
	require.ErrorIs(h.t, err, domain.ErrUnauthenticated)
}

// ---- Create & penyimpanan ----

func TestCreateStoresOnlyTheTokenHash(t *testing.T) {
	h := newHarness(t)
	org := testkit.NewOrganization(t, h.db)
	u := testkit.NewUser(t, h.db, testkit.InOrganization(org))
	ip := netip.MustParseAddr("203.0.113.9")
	ua := "UA/1"
	iss, err := h.svc.Create(h.ctx, service.CreateParams{UserID: u.ID, OrganizationID: &org.ID, IP: &ip, UserAgent: &ua})
	require.NoError(t, err)

	require.Len(t, iss.Token, 43, "32 byte base64url tanpa padding")
	row := h.row(iss.Token)
	require.Equal(t, iss.Session.ID, row.ID)
	require.Len(t, row.TokenHash, 32)
	require.NotEqual(t, []byte(iss.Token), row.TokenHash)
	require.Equal(t, "password", row.AuthLevel)
	require.WithinDuration(t, h.clk.now().Add(absolute), row.ExpiresAt, 5*time.Second)

	// Redis: kunci memakai hash; tidak ada kunci atau nilai yang memuat token mentah.
	require.True(t, h.cached(iss.Token))
	keys, err := h.rdb.Keys(h.ctx, "*").Result()
	require.NoError(t, err)
	require.NotEmpty(t, keys)
	for _, k := range keys {
		require.NotContains(t, k, iss.Token)
		typ, _ := h.rdb.Type(h.ctx, k).Result()
		switch typ {
		case "string":
			v, _ := h.rdb.Get(h.ctx, k).Result()
			require.NotContains(t, v, iss.Token)
		case "set":
			vs, _ := h.rdb.SMembers(h.ctx, k).Result()
			for _, v := range vs {
				require.NotContains(t, v, iss.Token)
			}
		}
	}
	member, err := h.rdb.SIsMember(h.ctx, "user_sessions:"+u.ID.String(), tokenHashHex(iss.Token)).Result()
	require.NoError(t, err)
	require.True(t, member)
	ttl, err := h.rdb.TTL(h.ctx, "user_sessions:"+u.ID.String()).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, 11*time.Hour, "TTL set pengguna = timeout absolut penuh")

	// Token berbeda tiap sesi.
	other := h.create(u.ID)
	require.NotEqual(t, iss.Token, other.Token)
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	h := newHarness(t)
	_, err := h.svc.Create(h.ctx, service.CreateParams{UserID: h.user.ID, AuthLevel: "root"})
	require.Error(t, err)
	_, err = h.svc.Create(h.ctx, service.CreateParams{UserID: h.user.ID, ExpiresAt: h.clk.now().Add(-time.Minute)})
	require.Error(t, err, "sesi yang sudah kedaluwarsa tidak boleh dibuat")
}

// ---- Authenticate: jalur normal dan penolakan ----

func TestAuthenticateReturnsIdentity(t *testing.T) {
	h := newHarness(t)
	org := testkit.NewOrganization(t, h.db)
	u := testkit.NewUser(t, h.db, testkit.InOrganization(org))
	iss, err := h.svc.Create(h.ctx, service.CreateParams{UserID: u.ID, OrganizationID: &org.ID})
	require.NoError(t, err)

	id, err := h.svc.Authenticate(h.ctx, iss.Token)
	require.NoError(t, err)
	require.Equal(t, u.ID, id.UserID)
	require.Equal(t, iss.Session.ID, id.SessionID)
	require.Equal(t, domain.AuthLevelPassword, id.AuthLevel)
	require.NotNil(t, id.OrganizationID)
	require.Equal(t, org.ID, *id.OrganizationID)

	t.Run("identitas platform: tanpa organisasi", func(t *testing.T) {
		p := h.create(h.user.ID)
		id, err := h.svc.Authenticate(h.ctx, p.Token)
		require.NoError(t, err)
		require.Nil(t, id.OrganizationID)
	})
}

func TestAuthenticateRejectsMalformedAndUnknownTokens(t *testing.T) {
	h := newHarness(t)
	real := h.create(h.user.ID)
	flip := []byte(real.Token)
	flip[0] ^= 1 // satu karakter berbeda: token lain yang tak pernah dibuat

	for name, tok := range map[string]string{
		"kosong":               "",
		"spasi":                "   ",
		"terlalu pendek":       real.Token[:42],
		"terlalu panjang":      real.Token + "A",
		"awalan dipotong":      real.Token[1:],
		"karakter terlarang":   strings.Repeat("!", 43),
		"padding base64":       real.Token[:42] + "=",
		"injeksi":              "' OR '1'='1' --" + strings.Repeat("a", 29),
		"huruf besar-kecil":    strings.ToUpper(real.Token),
		"token spasi di ujung": real.Token + " ",
		"satu karakter beda":   string(flip),
		"hash sebagai token":   tokenHashHex(real.Token),
	} {
		t.Run(name, func(t *testing.T) {
			h.requireUnauthenticated(tok)
		})
	}
	// token asli tetap sah (penolakan di atas tidak merusak apa pun)
	_, err := h.svc.Authenticate(h.ctx, real.Token)
	require.NoError(t, err)
}

func TestAuthenticateGarbageDoesNotTouchStorage(t *testing.T) {
	h := newHarness(t)
	before, err := h.rdb.DBSize(h.ctx).Result()
	require.NoError(t, err)
	for range 20 {
		h.requireUnauthenticated(strings.Repeat("x", 43))
		h.requireUnauthenticated("pendek")
	}
	after, err := h.rdb.DBSize(h.ctx).Result()
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestWellFormedButNeverIssuedTokenIsRejectedAndNotCached(t *testing.T) {
	h := newHarness(t)
	never := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32)) // 43 karakter sah, tak pernah dibuat
	h.requireUnauthenticated(never)
	require.False(t, h.cached(never), "penolakan tidak boleh meninggalkan entri di Redis")
}

// ---- timeout ----

func TestIdleTimeoutRevokesTheSession(t *testing.T) {
	h := newHarness(t)
	iss := h.create(h.user.ID)

	h.clk.advance(idle - time.Minute)
	_, err := h.svc.Authenticate(h.ctx, iss.Token)
	require.NoError(t, err, "masih di dalam jendela idle")

	h.clk.advance(idle + time.Minute) // sejak aktivitas terakhir sudah > idle
	h.requireUnauthenticated(iss.Token)
	require.NotNil(t, h.row(iss.Token).RevokedAt, "sesi idle dicabut di PostgreSQL")
	require.False(t, h.cached(iss.Token))
	h.requireUnauthenticated(iss.Token) // tetap ditolak setelah dicabut
}

func TestIdleTimeoutAlsoEnforcedWhenRedisIsEmpty(t *testing.T) {
	h := newHarness(t)
	iss := h.create(h.user.ID)
	require.NoError(t, h.rdb.FlushAll(h.ctx).Err())
	h.clk.advance(idle + time.Minute)
	h.requireUnauthenticated(iss.Token)
	require.NotNil(t, h.row(iss.Token).RevokedAt)
}

func TestAbsoluteTimeoutCannotBeExtendedByActivity(t *testing.T) {
	h := newHarnessWith(t, service.StaticTimeouts{Idle: 24 * time.Hour, Absolute: time.Hour})
	iss := h.create(h.user.ID)

	for range 5 { // aktivitas terus-menerus selama 50 menit
		h.clk.advance(10 * time.Minute)
		_, err := h.svc.Authenticate(h.ctx, iss.Token)
		require.NoError(t, err)
	}
	h.clk.advance(11 * time.Minute) // melewati 1 jam sejak dibuat
	h.requireUnauthenticated(iss.Token)
	require.False(t, h.cached(iss.Token))

	t.Run("juga lewat jalur PostgreSQL", func(t *testing.T) {
		require.NoError(t, h.rdb.FlushAll(h.ctx).Err())
		h.requireUnauthenticated(iss.Token)
	})
}

func TestActivityIsRecordedAtMostOncePerInterval(t *testing.T) {
	h := newHarness(t)
	iss := h.create(h.user.ID)
	created := h.row(iss.Token).LastActivityAt

	h.clk.advance(20 * time.Second)
	_, err := h.svc.Authenticate(h.ctx, iss.Token)
	require.NoError(t, err)
	require.WithinDuration(t, created, h.row(iss.Token).LastActivityAt, time.Millisecond, "di bawah satu menit: tanpa tulisan")

	h.clk.advance(90 * time.Second)
	_, err = h.svc.Authenticate(h.ctx, iss.Token)
	require.NoError(t, err)
	require.WithinDuration(t, h.clk.now(), h.row(iss.Token).LastActivityAt, 2*time.Second, "lewat satu menit: tercatat")
}

func TestActivityKeepsASessionAliveAcrossManyIdleWindows(t *testing.T) {
	h := newHarness(t)
	iss := h.create(h.user.ID)
	for range 8 { // 8 x 25 menit = 200 menit > idle, tapi aktif tiap 25 menit
		h.clk.advance(25 * time.Minute)
		_, err := h.svc.Authenticate(h.ctx, iss.Token)
		require.NoError(t, err)
	}
}

// ---- pencabutan ----

func TestRevokeRemovesAccessEverywhere(t *testing.T) {
	h := newHarness(t)
	iss := h.create(h.user.ID)
	_, err := h.svc.Authenticate(h.ctx, iss.Token)
	require.NoError(t, err)

	require.NoError(t, h.svc.Revoke(h.ctx, h.user.ID, iss.Session.ID, service.ReasonLogout))

	h.requireUnauthenticated(iss.Token)
	require.False(t, h.cached(iss.Token))
	member, err := h.rdb.SIsMember(h.ctx, "user_sessions:"+h.user.ID.String(), tokenHashHex(iss.Token)).Result()
	require.NoError(t, err)
	require.False(t, member)
	require.NotNil(t, h.row(iss.Token).RevokedAt)

	t.Run("mencabut dua kali", func(t *testing.T) {
		err := h.svc.Revoke(h.ctx, h.user.ID, iss.Session.ID, service.ReasonLogout)
		require.ErrorIs(t, err, domain.ErrSessionNotFound)
	})
}

func TestRevokeCannotTouchAnotherUsersSession(t *testing.T) {
	h := newHarness(t)
	victim := testkit.NewUser(t, h.db)
	vs := h.create(victim.ID)

	err := h.svc.Revoke(h.ctx, h.user.ID, vs.Session.ID, service.ReasonRevokedByUser)
	require.ErrorIs(t, err, domain.ErrSessionNotFound)
	err = h.svc.Revoke(h.ctx, h.user.ID, uuid.Nil, service.ReasonRevokedByUser)
	require.ErrorIs(t, err, domain.ErrSessionNotFound)

	_, err = h.svc.Authenticate(h.ctx, vs.Token)
	require.NoError(t, err, "sesi korban tetap hidup")
	require.True(t, h.cached(vs.Token))
}

func TestRevokeOthersAndAll(t *testing.T) {
	h := newHarness(t)
	stranger := testkit.NewUser(t, h.db)
	keep, a, b := h.create(h.user.ID), h.create(h.user.ID), h.create(h.user.ID)
	foreign := h.create(stranger.ID)

	n, err := h.svc.RevokeOthers(h.ctx, h.user.ID, keep.Session.ID, service.ReasonRevokeOthers)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	h.requireUnauthenticated(a.Token)
	h.requireUnauthenticated(b.Token)
	_, err = h.svc.Authenticate(h.ctx, keep.Token)
	require.NoError(t, err)
	_, err = h.svc.Authenticate(h.ctx, foreign.Token)
	require.NoError(t, err, "sesi user lain tak tersentuh")

	members, err := h.rdb.SMembers(h.ctx, "user_sessions:"+h.user.ID.String()).Result()
	require.NoError(t, err)
	require.Equal(t, []string{tokenHashHex(keep.Token)}, members)

	n, err = h.svc.RevokeAll(h.ctx, h.user.ID, service.ReasonRevokeAll)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	h.requireUnauthenticated(keep.Token)
	_, err = h.svc.Authenticate(h.ctx, foreign.Token)
	require.NoError(t, err)

	n, err = h.svc.RevokeAll(h.ctx, h.user.ID, service.ReasonRevokeAll)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestRevokedEventIsPublishedWithoutSecrets(t *testing.T) {
	h := newHarness(t)
	iss := h.create(h.user.ID)

	sub := h.rdb.Subscribe(h.ctx, service.ChannelSessionRevoked)
	defer func() { _ = sub.Close() }()
	_, err := sub.Receive(h.ctx) // konfirmasi langganan aktif
	require.NoError(t, err)

	require.NoError(t, h.svc.Revoke(h.ctx, h.user.ID, iss.Session.ID, service.ReasonRevokedByUser))

	rctx, cancel := context.WithTimeout(h.ctx, 5*time.Second)
	defer cancel()
	msg, err := sub.ReceiveMessage(rctx)
	require.NoError(t, err)
	var ev struct {
		SessionID uuid.UUID `json:"session_id"`
		UserID    uuid.UUID `json:"user_id"`
		Reason    string    `json:"reason"`
	}
	require.NoError(t, json.Unmarshal([]byte(msg.Payload), &ev))
	require.Equal(t, iss.Session.ID, ev.SessionID)
	require.Equal(t, h.user.ID, ev.UserID)
	require.Equal(t, service.ReasonRevokedByUser, ev.Reason)
	require.NotContains(t, msg.Payload, iss.Token)
	require.NotContains(t, msg.Payload, tokenHashHex(iss.Token))
}

func TestListShowsOnlyLiveSessionsOfTheUser(t *testing.T) {
	h := newHarness(t)
	stranger := testkit.NewUser(t, h.db)
	live := h.create(h.user.ID)
	gone := h.create(h.user.ID)
	_ = h.create(stranger.ID)
	require.NoError(t, h.svc.Revoke(h.ctx, h.user.ID, gone.Session.ID, service.ReasonRevokedByUser))

	got, err := h.svc.List(h.ctx, h.user.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, live.Session.ID, got[0].ID)

	h.clk.advance(idle + time.Minute)
	got, err = h.svc.List(h.ctx, h.user.ID)
	require.NoError(t, err)
	require.Empty(t, got, "sesi idle tidak ditampilkan sebagai aktif")
}

// ---- Redis hilang / mati ----

func TestRedisDataLossDoesNotLogEveryoneOut(t *testing.T) {
	h := newHarness(t)
	iss := h.create(h.user.ID)
	require.NoError(t, h.rdb.FlushAll(h.ctx).Err())
	require.False(t, h.cached(iss.Token))

	_, err := h.svc.Authenticate(h.ctx, iss.Token)
	require.NoError(t, err, "PostgreSQL otoritatif")
	require.True(t, h.cached(iss.Token), "Redis diisi ulang oleh pencarian pertama")
}

func deadRedis() *redis.Client {
	return redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond, MaxRetries: -1})
}

func TestRedisDownDegradesButDoesNotFail(t *testing.T) {
	h := newHarness(t)
	iss := h.create(h.user.ID)

	dead := deadRedis()
	t.Cleanup(func() { _ = dead.Close() })
	degraded := h.newService(dead, service.StaticTimeouts{Idle: idle, Absolute: absolute})

	id, err := degraded.Authenticate(h.ctx, iss.Token)
	require.NoError(t, err, "Redis mati: tetap terautentikasi lewat PostgreSQL")
	require.Equal(t, h.user.ID, id.UserID)
	require.Contains(t, h.logs.String(), "terdegradasi")
	require.Contains(t, h.logs.String(), "auth.session.redis_degraded")

	t.Run("sesi baru tetap bisa dibuat", func(t *testing.T) {
		n, err := degraded.Create(h.ctx, service.CreateParams{UserID: h.user.ID})
		require.NoError(t, err)
		_, err = degraded.Authenticate(h.ctx, n.Token)
		require.NoError(t, err)
	})

	t.Run("pencabutan tetap berlaku walau Redis mati", func(t *testing.T) {
		require.NoError(t, degraded.Revoke(h.ctx, h.user.ID, iss.Session.ID, service.ReasonLogout))
		_, err := degraded.Authenticate(h.ctx, iss.Token)
		require.ErrorIs(t, err, domain.ErrUnauthenticated)
		require.Contains(t, h.logs.String(), "auth.session.cache_purge_failed")
	})

	t.Run("sesi idle tetap ditolak", func(t *testing.T) {
		n := h.create(h.user.ID)
		h.clk.advance(idle + time.Minute)
		_, err := degraded.Authenticate(h.ctx, n.Token)
		require.ErrorIs(t, err, domain.ErrUnauthenticated)
	})
}

// Entri Redis yang basi (penghapusan gagal) tidak boleh hidup lebih lama dari satu interval
// aktivitas: pembaruan aktivitas berikutnya memeriksa PostgreSQL.
func TestStaleRedisEntryAfterRevocationIsBoundedByTheActivityInterval(t *testing.T) {
	h := newHarness(t)
	iss := h.create(h.user.ID)

	// cabut di PostgreSQL SAJA, seolah penghapusan Redis gagal
	_, err := h.store.Q(h.ctx).RevokeSession(h.ctx, repository.RevokeSessionParams{SessionID: iss.Session.ID, UserID: h.user.ID})
	require.NoError(t, err)
	require.True(t, h.cached(iss.Token), "prasyarat: entri Redis basi masih ada")

	_, err = h.svc.Authenticate(h.ctx, iss.Token)
	require.NoError(t, err, "di dalam satu interval entri basi masih bisa terpakai (batas yang didokumentasikan)")

	h.clk.advance(service.DefaultActivityInterval + time.Second)
	h.requireUnauthenticated(iss.Token)
	require.False(t, h.cached(iss.Token), "entri basi dibersihkan")
}

// ---- transaksi ----

func TestCreateInsideRolledBackTransactionLeavesNoTrace(t *testing.T) {
	h := newHarness(t)
	boom := errors.New("boom")
	var token string
	err := h.tm.WithinTx(h.ctx, func(ctx context.Context) error {
		iss, err := h.svc.Create(ctx, service.CreateParams{UserID: h.user.ID})
		require.NoError(t, err)
		token = iss.Token
		require.False(t, h.cached(token), "belum ditulis ke Redis sebelum commit")
		return boom
	})
	require.ErrorIs(t, err, boom)
	require.False(t, h.cached(token))
	h.requireUnauthenticated(token)
	rows, err := h.svc.List(h.ctx, h.user.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestCreateInsideCommittedTransactionBecomesVisibleAfterCommit(t *testing.T) {
	h := newHarness(t)
	var token string
	err := h.tm.WithinTx(h.ctx, func(ctx context.Context) error {
		iss, err := h.svc.Create(ctx, service.CreateParams{UserID: h.user.ID})
		token = iss.Token
		require.False(t, h.cached(token))
		return err
	})
	require.NoError(t, err)
	require.True(t, h.cached(token))
	_, err = h.svc.Authenticate(h.ctx, token)
	require.NoError(t, err)
}

func TestRevokeInsideRolledBackTransactionKeepsTheSession(t *testing.T) {
	h := newHarness(t)
	iss := h.create(h.user.ID)
	boom := errors.New("boom")
	err := h.tm.WithinTx(h.ctx, func(ctx context.Context) error {
		require.NoError(t, h.svc.Revoke(ctx, h.user.ID, iss.Session.ID, service.ReasonLogout))
		return boom
	})
	require.ErrorIs(t, err, boom)
	_, err = h.svc.Authenticate(h.ctx, iss.Token)
	require.NoError(t, err, "rollback: sesi tidak boleh hilang dari Redis maupun PostgreSQL")
	require.True(t, h.cached(iss.Token))
}

// ---- rotasi ----

func TestRotateReplacesTokenKeepingTheAbsoluteLimit(t *testing.T) {
	h := newHarness(t)
	orgA := testkit.NewOrganization(t, h.db)
	orgB := testkit.NewOrganization(t, h.db)
	u := testkit.NewUser(t, h.db, testkit.InOrganization(orgA))
	old, err := h.svc.Create(h.ctx, service.CreateParams{UserID: u.ID, OrganizationID: &orgA.ID})
	require.NoError(t, err)
	oldID, err := h.svc.Authenticate(h.ctx, old.Token)
	require.NoError(t, err)

	h.clk.advance(10 * time.Minute)
	ip := netip.MustParseAddr("198.51.100.4")
	iss, err := h.svc.Rotate(h.ctx, *oldID, service.RotateOptions{IP: &ip})
	require.NoError(t, err)

	require.NotEqual(t, old.Token, iss.Token)
	require.NotEqual(t, old.Session.ID, iss.Session.ID, "id sesi ikut berganti")
	require.WithinDuration(t, old.Session.ExpiresAt, iss.Session.ExpiresAt, time.Millisecond, "rotasi tidak memperpanjang umur sesi")
	require.Equal(t, orgA.ID, *iss.Session.OrganizationID, "organisasi tidak berubah tanpa diminta")

	h.requireUnauthenticated(old.Token)
	require.False(t, h.cached(old.Token))
	require.NotNil(t, h.row(old.Token).RevokedAt)
	id, err := h.svc.Authenticate(h.ctx, iss.Token)
	require.NoError(t, err)
	require.Equal(t, u.ID, id.UserID)

	t.Run("ganti organisasi", func(t *testing.T) {
		sw, err := h.svc.Rotate(h.ctx, *id, service.RotateOptions{ChangeOrganization: true, OrganizationID: &orgB.ID})
		require.NoError(t, err)
		require.Equal(t, orgB.ID, *sw.Session.OrganizationID)
		h.requireUnauthenticated(iss.Token)
	})
}

func TestRotateRejectsDeadOrForeignSessions(t *testing.T) {
	h := newHarness(t)
	stranger := testkit.NewUser(t, h.db)
	mine := h.create(h.user.ID)
	mineID, err := h.svc.Authenticate(h.ctx, mine.Token)
	require.NoError(t, err)

	t.Run("sesi milik user lain", func(t *testing.T) {
		forged := *mineID
		forged.UserID = stranger.ID
		_, err := h.svc.Rotate(h.ctx, forged, service.RotateOptions{})
		require.ErrorIs(t, err, domain.ErrUnauthenticated)
		_, err = h.svc.Authenticate(h.ctx, mine.Token)
		require.NoError(t, err, "sesi asli tak tersentuh")
	})
	t.Run("sesi tak dikenal", func(t *testing.T) {
		_, err := h.svc.Rotate(h.ctx, domain.Identity{UserID: h.user.ID, SessionID: uuid.Nil}, service.RotateOptions{})
		require.ErrorIs(t, err, domain.ErrUnauthenticated)
	})
	t.Run("sesi sudah dicabut", func(t *testing.T) {
		gone := h.create(h.user.ID)
		goneID, err := h.svc.Authenticate(h.ctx, gone.Token)
		require.NoError(t, err)
		require.NoError(t, h.svc.Revoke(h.ctx, h.user.ID, gone.Session.ID, service.ReasonLogout))
		_, err = h.svc.Rotate(h.ctx, *goneID, service.RotateOptions{})
		require.ErrorIs(t, err, domain.ErrUnauthenticated)
	})
	t.Run("sesi idle", func(t *testing.T) {
		stale := h.create(h.user.ID)
		staleID, err := h.svc.Authenticate(h.ctx, stale.Token)
		require.NoError(t, err)
		h.clk.advance(idle + time.Minute)
		_, err = h.svc.Rotate(h.ctx, *staleID, service.RotateOptions{})
		require.ErrorIs(t, err, domain.ErrUnauthenticated)
	})
}

func TestRotateInsideRolledBackTransactionKeepsTheOldSession(t *testing.T) {
	h := newHarness(t)
	old := h.create(h.user.ID)
	id, err := h.svc.Authenticate(h.ctx, old.Token)
	require.NoError(t, err)

	boom := errors.New("boom")
	var newToken string
	err = h.tm.WithinTx(h.ctx, func(ctx context.Context) error {
		iss, err := h.svc.Rotate(ctx, *id, service.RotateOptions{})
		require.NoError(t, err)
		newToken = iss.Token
		return boom
	})
	require.ErrorIs(t, err, boom)
	_, err = h.svc.Authenticate(h.ctx, old.Token)
	require.NoError(t, err, "rollback: token lama tetap sah")
	h.requireUnauthenticated(newToken)
}

func TestConcurrentRotationsHaveExactlyOneWinner(t *testing.T) {
	h := newHarness(t)
	old := h.create(h.user.ID)
	id, err := h.svc.Authenticate(h.ctx, old.Token)
	require.NoError(t, err)

	const racers = 6
	var wg sync.WaitGroup
	var wins, losses atomic.Int32
	var mu sync.Mutex
	var tokens []string
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := h.tm.WithinTx(h.ctx, func(ctx context.Context) error {
				iss, err := h.svc.Rotate(ctx, *id, service.RotateOptions{})
				if err == nil {
					mu.Lock()
					tokens = append(tokens, iss.Token)
					mu.Unlock()
				}
				return err
			})
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, domain.ErrUnauthenticated):
				losses.Add(1)
			default:
				t.Errorf("galat tak terduga: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	require.EqualValues(t, 1, wins.Load(), "tepat satu pemenang")
	require.EqualValues(t, racers-1, losses.Load())
	require.Len(t, tokens, 1)
	h.requireUnauthenticated(old.Token)
	_, err = h.svc.Authenticate(h.ctx, tokens[0])
	require.NoError(t, err)
	live, err := h.svc.List(h.ctx, h.user.ID)
	require.NoError(t, err)
	require.Len(t, live, 1, "tidak ada sesi ganda yang hidup")
}

// ---- log ----

func TestLogsNeverContainTokensOrHashes(t *testing.T) {
	h := newHarness(t)
	iss := h.create(h.user.ID)
	_, _ = h.svc.Authenticate(h.ctx, iss.Token)
	h.clk.advance(idle + time.Minute)
	_, _ = h.svc.Authenticate(h.ctx, iss.Token) // jalur penolakan idle
	_ = h.svc.Revoke(h.ctx, h.user.ID, iss.Session.ID, service.ReasonLogout)
	dead := deadRedis()
	t.Cleanup(func() { _ = dead.Close() })
	degraded := h.newService(dead, service.StaticTimeouts{Idle: idle, Absolute: absolute})
	n := h.create(h.user.ID)
	_, _ = degraded.Authenticate(h.ctx, n.Token)

	out := h.logs.String()
	require.NotEmpty(t, out)
	for _, tok := range []string{iss.Token, n.Token} {
		require.NotContains(t, out, tok)
		require.NotContains(t, out, tokenHashHex(tok))
	}
}
