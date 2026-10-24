// Package service memuat layanan autentikasi (docs/05 §19): sesi, kunci akun, dan seterusnya.
package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/repository"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/pkg/logger"
)

// Kunci dan channel Redis (docs/05 §7).
const (
	sessionKeyPrefix      = "session:"       // session:{hex(sha256(token))} -> cachedSession (JSON)
	userSessionsKeyPrefix = "user_sessions:" // user_sessions:{user_id}     -> set hex(sha256(token))

	// ChannelSessionRevoked adalah channel Pub/Sub yang dipantau Hub WebSocket (docs/05 §7.3).
	ChannelSessionRevoked = "session.revoked"

	// DefaultActivityInterval: last_activity_at diperbarui paling sering sekali per interval per
	// sesi (docs/05 §12.1). Juga batas atas umur entri Redis basi bila penghapusan gagal.
	DefaultActivityInterval = time.Minute

	tokenBytes     = 32
	tokenLen       = 43 // panjang base64url tanpa padding untuk 32 byte
	cacheOpTimeout = 2 * time.Second
	purgeAttempts  = 3
	degradedLogGap = 30 * time.Second
)

// Alasan pencabutan sesi (dibawa ke event session.revoked dan security event).
const (
	ReasonLogout          = "logout"
	ReasonRevokedByUser   = "revoked_by_user"
	ReasonRevokeOthers    = "revoke_others"
	ReasonRevokeAll       = "revoke_all"
	ReasonRotated         = "rotated"
	ReasonPasswordChanged = "password_changed"
	ReasonIdleTimeout     = "idle_timeout"
)

// Timeouts adalah dua jam sesi (docs/05 §12.1).
type Timeouts struct {
	Idle     time.Duration // sejak aktivitas terakhir
	Absolute time.Duration // sejak dibuat; tidak dapat diperpanjang
}

// TimeoutSource menyediakan timeout aktif. Implementasi awal membaca konfigurasi; deployment
// yang lebih ketat dapat memakai settings platform tanpa mengubah layanan ini.
type TimeoutSource interface {
	SessionTimeouts(ctx context.Context) Timeouts
}

// StaticTimeouts adalah TimeoutSource bernilai tetap (dari konfigurasi).
type StaticTimeouts Timeouts

// SessionTimeouts mengimplementasikan TimeoutSource.
func (s StaticTimeouts) SessionTimeouts(context.Context) Timeouts { return Timeouts(s) }

// cachedSession adalah nilai di Redis (docs/05 §7), ditambah last_activity_at agar timeout idle
// bisa diperiksa tanpa menyentuh PostgreSQL pada setiap request. Tidak memuat token.
type cachedSession struct {
	SessionID      uuid.UUID        `json:"session_id"`
	UserID         uuid.UUID        `json:"user_id"`
	OrganizationID *uuid.UUID       `json:"organization_id,omitempty"`
	AuthLevel      domain.AuthLevel `json:"auth_level"`
	CreatedAt      time.Time        `json:"created_at"`
	ExpiresAt      time.Time        `json:"expires_at"`
	LastActivityAt time.Time        `json:"last_activity_at"`
}

func cachedFromRow(r repository.Session) cachedSession {
	return cachedSession{
		SessionID: r.ID, UserID: r.UserID, OrganizationID: r.OrganizationID,
		AuthLevel: domain.AuthLevel(r.AuthLevel), CreatedAt: r.CreatedAt,
		ExpiresAt: r.ExpiresAt, LastActivityAt: r.LastActivityAt,
	}
}

func (c cachedSession) identity() *domain.Identity {
	return &domain.Identity{UserID: c.UserID, OrganizationID: c.OrganizationID, SessionID: c.SessionID, AuthLevel: c.AuthLevel}
}

// SessionService mengelola sesi: Redis untuk pencarian cepat, PostgreSQL sebagai sumber
// kebenaran (docs/05 §7). Redis mati = mode terdegradasi, bukan gagal (invarian 14).
//
// Semua tulisan PostgreSQL lewat Store.Q(ctx), jadi ikut transaksi pada ctx; efek ke Redis dan
// Pub/Sub dijadwalkan SETELAH commit (kernel.AfterCommit) agar transaksi yang di-rollback tidak
// meninggalkan jejak di Redis.
type SessionService struct {
	store    *repository.Store
	rdb      redis.UniversalClient
	timeouts TimeoutSource
	log      *slog.Logger
	now      func() time.Time
	interval time.Duration

	lastDegradedLog atomic.Int64 // unix nano peringatan terakhir (pembatas laju log)
}

// Option mengubah SessionService.
type Option func(*SessionService)

// WithClock mengganti sumber waktu (untuk tes).
func WithClock(now func() time.Time) Option { return func(s *SessionService) { s.now = now } }

// WithActivityInterval mengganti interval pembaruan aktivitas (default 1 menit).
func WithActivityInterval(d time.Duration) Option { return func(s *SessionService) { s.interval = d } }

// NewSessionService membuat layanan sesi. rdb wajib (klien tunggal composition root, ADR-0020);
// log opsional.
func NewSessionService(store *repository.Store, rdb redis.UniversalClient, timeouts TimeoutSource, log *slog.Logger, opts ...Option) *SessionService {
	if log == nil {
		log = slog.Default()
	}
	s := &SessionService{store: store, rdb: rdb, timeouts: timeouts, log: log, now: time.Now, interval: DefaultActivityInterval}
	for _, o := range opts {
		o(s)
	}
	return s
}

// ---- token ----

// newToken membuat token 32 byte acak (base64url) dan SHA-256-nya. Token HANYA dibawa cookie;
// yang tersimpan di mana pun hanyalah hash (docs/05 §7.1).
func newToken() (token string, hash []byte, err error) {
	b := make([]byte, tokenBytes)
	if _, err = rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("auth: gagal membuat token sesi: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token), nil
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// validTokenFormat menolak token yang jelas bukan buatan kita sebelum menyentuh Redis/PostgreSQL:
// pemindaian token acak tidak boleh menjadi amplifikasi beban ke penyimpanan.
func validTokenFormat(token string) bool {
	if len(token) != tokenLen {
		return false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(token)
	return err == nil && len(b) == tokenBytes
}

// ---- Create / Rotate ----

// CreateParams adalah masukan Create.
type CreateParams struct {
	UserID         uuid.UUID
	OrganizationID *uuid.UUID // nil = identitas tingkat platform
	AuthLevel      domain.AuthLevel
	ExpiresAt      time.Time // nol = sekarang + timeout absolut
	IP             *netip.Addr
	UserAgent      *string
}

// Issued adalah sesi baru beserta token mentahnya. Token hanya ada di sini: serahkan ke cookie
// dan jangan disimpan atau dicatat.
type Issued struct {
	Token   string
	Session repository.Session
}

// Create membuat sesi baru (batas autentikasi: login, rotasi). Dipanggil di dalam transaksi
// pemanggil bila ada; entri Redis ditulis setelah commit.
func (s *SessionService) Create(ctx context.Context, p CreateParams) (Issued, error) {
	if p.AuthLevel == "" {
		p.AuthLevel = domain.AuthLevelPassword
	}
	if !p.AuthLevel.Valid() {
		return Issued{}, fmt.Errorf("auth: auth_level %q tidak dikenal", p.AuthLevel)
	}
	now := s.now()
	expires := p.ExpiresAt
	if expires.IsZero() {
		expires = now.Add(s.timeouts.SessionTimeouts(ctx).Absolute)
	}
	if !expires.After(now) {
		return Issued{}, errors.New("auth: expires_at sesi sudah lewat")
	}
	return s.insert(ctx, insertSpec{
		userID: p.UserID, org: p.OrganizationID, level: p.AuthLevel, expires: expires, ip: p.IP, ua: p.UserAgent,
	})
}

type insertSpec struct {
	userID  uuid.UUID
	org     *uuid.UUID
	level   domain.AuthLevel
	expires time.Time
	ip      *netip.Addr
	ua      *string
}

func (s *SessionService) insert(ctx context.Context, sp insertSpec) (Issued, error) {
	token, hash, err := newToken()
	if err != nil {
		return Issued{}, err
	}
	row, err := s.store.Q(ctx).InsertSession(ctx, repository.InsertSessionParams{
		TokenHash: hash, UserID: sp.userID, OrganizationID: sp.org, ExpiresAt: sp.expires,
		IpAddress: sp.ip, UserAgent: sp.ua, AuthLevel: string(sp.level),
	})
	if err != nil {
		return Issued{}, fmt.Errorf("auth: gagal menyimpan sesi: %w", err)
	}
	hexh, c := hex.EncodeToString(hash), cachedFromRow(row)
	s.afterCommit(ctx, func(cc context.Context) {
		if err := s.cachePut(cc, hexh, c); err != nil {
			s.warnDegraded(cc, err) // PostgreSQL otoritatif: pencarian pertama akan mengisi ulang Redis
		}
	})
	return Issued{Token: token, Session: row}, nil
}

// RotateOptions mengatur Rotate.
type RotateOptions struct {
	Reason string // ReasonRotated bila kosong; dibawa ke event session.revoked
	// ChangeOrganization true: sesi baru memakai OrganizationID (boleh nil); false: organisasi lama.
	ChangeOrganization bool
	OrganizationID     *uuid.UUID
	IP                 *netip.Addr
	UserAgent          *string
}

// Rotate mengganti token sesi (docs/05 §12.2): sesi lama dicabut, sesi baru dibuat untuk user yang
// sama dengan batas absolut yang SAMA (rotasi tidak memperpanjang umur sesi). Idle clock mulai
// ulang. Dipakai refresh, switch-organization, dan perubahan password. Pemanggil membungkusnya
// dalam transaksi bersama security event; bila dua rotasi berlomba, hanya satu yang menang.
func (s *SessionService) Rotate(ctx context.Context, cur domain.Identity, o RotateOptions) (Issued, error) {
	now, tm := s.now(), s.timeouts.SessionTimeouts(ctx)
	q := s.store.Q(ctx)

	old, err := q.GetSessionByIDForUser(ctx, repository.GetSessionByIDForUserParams{SessionID: cur.SessionID, UserID: cur.UserID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Issued{}, domain.ErrUnauthenticated
	}
	if err != nil {
		return Issued{}, fmt.Errorf("auth: gagal membaca sesi: %w", err)
	}
	if old.RevokedAt != nil || !now.Before(old.ExpiresAt) || now.Sub(old.LastActivityAt) > tm.Idle {
		return Issued{}, domain.ErrUnauthenticated
	}

	revoked, err := q.RevokeSession(ctx, repository.RevokeSessionParams{SessionID: old.ID, UserID: old.UserID})
	if errors.Is(err, pgx.ErrNoRows) { // kalah berlomba dengan rotasi/pencabutan lain
		return Issued{}, domain.ErrUnauthenticated
	}
	if err != nil {
		return Issued{}, fmt.Errorf("auth: gagal mencabut sesi lama: %w", err)
	}

	org := old.OrganizationID
	if o.ChangeOrganization {
		org = o.OrganizationID
	}
	issued, err := s.insert(ctx, insertSpec{
		userID: old.UserID, org: org, level: domain.AuthLevel(old.AuthLevel), expires: old.ExpiresAt, ip: o.IP, ua: o.UserAgent,
	})
	if err != nil {
		return Issued{}, err
	}
	reason := o.Reason
	if reason == "" {
		reason = ReasonRotated
	}
	s.afterCommit(ctx, func(cc context.Context) { s.purgeAndPublish(cc, revoked.TokenHash, old.UserID, old.ID, reason) })
	return issued, nil
}

// ---- Authenticate ----

// Authenticate memvalidasi token cookie dan mengembalikan Identity. Semua sebab penolakan
// (format salah, tak dikenal, dicabut, kedaluwarsa absolut atau idle) menjadi ErrUnauthenticated.
// Kegagalan infrastruktur PostgreSQL dikembalikan sebagai error biasa (bukan 401).
func (s *SessionService) Authenticate(ctx context.Context, token string) (*domain.Identity, error) {
	if !validTokenFormat(token) {
		return nil, s.deny(ctx, "format")
	}
	hash := hashToken(token)
	hexh := hex.EncodeToString(hash)
	tm, now := s.timeouts.SessionTimeouts(ctx), s.now()

	c, err := s.cacheGet(ctx, hexh)
	redisUp := err == nil
	if err != nil {
		s.warnDegraded(ctx, err)
	}
	if c != nil {
		return s.authFromCache(ctx, hash, hexh, c, tm, now)
	}
	return s.authFromDB(ctx, hash, hexh, redisUp, tm, now)
}

func (s *SessionService) authFromCache(ctx context.Context, hash []byte, hexh string, c *cachedSession, tm Timeouts, now time.Time) (*domain.Identity, error) {
	if !now.Before(c.ExpiresAt) {
		s.purge(ctx, hash, c.UserID)
		return nil, s.deny(ctx, "expired")
	}
	if now.Sub(c.LastActivityAt) > tm.Idle {
		s.revokeIdle(ctx, hash, c.UserID, c.SessionID)
		return nil, s.deny(ctx, "idle")
	}
	if now.Sub(c.LastActivityAt) >= s.interval {
		if !s.touch(ctx, c.SessionID, now) { // PostgreSQL: sesi sudah dicabut (mis. penghapusan Redis gagal)
			s.purge(ctx, hash, c.UserID)
			return nil, s.deny(ctx, "revoked")
		}
		c.LastActivityAt = now
		s.cacheTouch(ctx, hexh, *c)
	}
	return c.identity(), nil
}

func (s *SessionService) authFromDB(ctx context.Context, hash []byte, hexh string, redisUp bool, tm Timeouts, now time.Time) (*domain.Identity, error) {
	row, err := s.store.Q(ctx).GetSessionByTokenHash(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, s.deny(ctx, "unknown")
	}
	if err != nil {
		return nil, fmt.Errorf("auth: gagal membaca sesi: %w", err)
	}
	switch {
	case row.RevokedAt != nil:
		return nil, s.deny(ctx, "revoked")
	case !now.Before(row.ExpiresAt):
		return nil, s.deny(ctx, "expired")
	case now.Sub(row.LastActivityAt) > tm.Idle:
		s.revokeIdle(ctx, hash, row.UserID, row.ID)
		return nil, s.deny(ctx, "idle")
	}
	c := cachedFromRow(row)
	if now.Sub(c.LastActivityAt) >= s.interval {
		if !s.touch(ctx, c.SessionID, now) {
			return nil, s.deny(ctx, "revoked")
		}
		c.LastActivityAt = now
	}
	if redisUp { // isi ulang Redis (mis. setelah FLUSHALL/restart) supaya request berikut cepat
		if err := s.cachePut(ctx, hexh, c); err != nil {
			s.warnDegraded(ctx, err)
		}
	}
	return c.identity(), nil
}

// touch memajukan last_activity_at di PostgreSQL. false hanya bila sesi memang sudah dicabut;
// galat database tidak menjatuhkan sesi (dicatat, sesi dianggap hidup).
func (s *SessionService) touch(ctx context.Context, sessionID uuid.UUID, at time.Time) bool {
	n, err := s.store.Q(ctx).TouchSessionActivity(ctx, repository.TouchSessionActivityParams{SessionID: sessionID, At: at})
	if err != nil {
		s.log.WarnContext(ctx, "gagal memperbarui aktivitas sesi", "error", err)
		return true
	}
	return n > 0
}

func (s *SessionService) revokeIdle(ctx context.Context, hash []byte, userID, sessionID uuid.UUID) {
	if _, err := s.store.Q(ctx).RevokeSession(ctx, repository.RevokeSessionParams{SessionID: sessionID, UserID: userID}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.log.WarnContext(ctx, "gagal mencabut sesi idle", "error", err)
	}
	s.purge(ctx, hash, userID)
}

func (s *SessionService) deny(ctx context.Context, reason string) error {
	s.log.DebugContext(ctx, "sesi ditolak", "reason", reason) // tanpa token/hash
	return domain.ErrUnauthenticated
}

// ---- Revoke / List ----

// Revoke mencabut satu sesi milik userID. ErrSessionNotFound bila tidak ada/bukan miliknya/sudah dicabut.
func (s *SessionService) Revoke(ctx context.Context, userID, sessionID uuid.UUID, reason string) error {
	row, err := s.store.Q(ctx).RevokeSession(ctx, repository.RevokeSessionParams{SessionID: sessionID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrSessionNotFound
	}
	if err != nil {
		return fmt.Errorf("auth: gagal mencabut sesi: %w", err)
	}
	s.afterCommit(ctx, func(cc context.Context) { s.purgeAndPublish(cc, row.TokenHash, userID, row.ID, reason) })
	return nil
}

// RevokeOthers mencabut semua sesi aktif userID kecuali keep; mengembalikan jumlahnya.
func (s *SessionService) RevokeOthers(ctx context.Context, userID, keep uuid.UUID, reason string) (int, error) {
	rows, err := s.store.Q(ctx).RevokeOtherSessions(ctx, repository.RevokeOtherSessionsParams{UserID: userID, KeepSessionID: keep})
	if err != nil {
		return 0, fmt.Errorf("auth: gagal mencabut sesi lain: %w", err)
	}
	s.afterCommit(ctx, func(cc context.Context) {
		for _, r := range rows {
			s.purgeAndPublish(cc, r.TokenHash, userID, r.ID, reason)
		}
	})
	return len(rows), nil
}

// RevokeAll mencabut semua sesi aktif userID (mis. user ditangguhkan, reset password).
func (s *SessionService) RevokeAll(ctx context.Context, userID uuid.UUID, reason string) (int, error) {
	rows, err := s.store.Q(ctx).RevokeAllSessions(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("auth: gagal mencabut semua sesi: %w", err)
	}
	s.afterCommit(ctx, func(cc context.Context) {
		for _, r := range rows {
			s.purgeAndPublish(cc, r.TokenHash, userID, r.ID, reason)
		}
	})
	return len(rows), nil
}

// List mengembalikan sesi aktif userID (belum dicabut, belum kedaluwarsa, belum idle), terbaru dulu.
func (s *SessionService) List(ctx context.Context, userID uuid.UUID) ([]repository.Session, error) {
	rows, err := s.store.Q(ctx).ListActiveSessionsByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("auth: gagal membaca daftar sesi: %w", err)
	}
	idle, now := s.timeouts.SessionTimeouts(ctx).Idle, s.now()
	out := rows[:0]
	for _, r := range rows {
		if now.Sub(r.LastActivityAt) <= idle {
			out = append(out, r)
		}
	}
	return out, nil
}

// ---- Redis ----

func (s *SessionService) cctx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, cacheOpTimeout)
}

// cacheGet: (nil, nil) = tidak ada/rusak; (nil, err) = Redis tidak terjangkau.
func (s *SessionService) cacheGet(ctx context.Context, hexh string) (*cachedSession, error) {
	cc, cancel := s.cctx(ctx)
	defer cancel()
	b, err := s.rdb.Get(cc, sessionKeyPrefix+hexh).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c cachedSession
	if err := json.Unmarshal(b, &c); err != nil || c.SessionID == uuid.Nil {
		s.log.WarnContext(ctx, "entri sesi di Redis rusak; memakai PostgreSQL")
		return nil, nil
	}
	return &c, nil
}

func (s *SessionService) cachePut(ctx context.Context, hexh string, c cachedSession) error {
	ttl := c.ExpiresAt.Sub(s.now())
	if ttl <= 0 {
		return nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	cc, cancel := s.cctx(ctx)
	defer cancel()
	userKey := userSessionsKeyPrefix + c.UserID.String()
	_, err = s.rdb.Pipelined(cc, func(p redis.Pipeliner) error {
		p.Set(cc, sessionKeyPrefix+hexh, b, ttl)
		p.SAdd(cc, userKey, hexh)
		// TTL set = timeout absolut penuh: tidak pernah lebih pendek dari sesi mana pun di dalamnya.
		p.Expire(cc, userKey, s.timeouts.SessionTimeouts(ctx).Absolute)
		return nil
	})
	return err
}

// cacheTouch menulis last_activity_at baru TANPA mengubah TTL dan hanya bila kunci masih ada (XX):
// penulisan ini tidak boleh menghidupkan kembali entri yang baru saja dihapus pencabutan.
func (s *SessionService) cacheTouch(ctx context.Context, hexh string, c cachedSession) {
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	cc, cancel := s.cctx(ctx)
	defer cancel()
	err = s.rdb.SetArgs(cc, sessionKeyPrefix+hexh, b, redis.SetArgs{Mode: "XX", KeepTTL: true}).Err()
	if err != nil && !errors.Is(err, redis.Nil) {
		s.warnDegraded(ctx, err)
	}
}

func (s *SessionService) cacheDel(ctx context.Context, hexh string, userID uuid.UUID) error {
	cc, cancel := s.cctx(ctx)
	defer cancel()
	_, err := s.rdb.Pipelined(cc, func(p redis.Pipeliner) error {
		p.Del(cc, sessionKeyPrefix+hexh)
		p.SRem(cc, userSessionsKeyPrefix+userID.String(), hexh)
		return nil
	})
	return err
}

// purge menghapus entri Redis sebuah sesi dengan beberapa percobaan. Bila tetap gagal, entri basi
// hanya bertahan paling lama satu interval aktivitas: pembaruan aktivitas berikutnya memeriksa
// PostgreSQL dan menolak sesi yang sudah dicabut.
func (s *SessionService) purge(ctx context.Context, tokenHash []byte, userID uuid.UUID) {
	hexh := hex.EncodeToString(tokenHash)
	var err error
	for i := 1; i <= purgeAttempts; i++ {
		if err = s.cacheDel(ctx, hexh, userID); err == nil {
			return
		}
		if i == purgeAttempts || !sleep(ctx, time.Duration(i)*25*time.Millisecond) {
			break
		}
	}
	s.log.ErrorContext(ctx, "gagal menghapus entri sesi di Redis; kedaluwarsa paling lama satu interval aktivitas",
		logger.SystemLog("auth.session.cache_purge_failed"), "error", err)
}

// sleep menunggu d atau sampai ctx selesai; false bila ctx selesai lebih dulu.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

type revokedEvent struct {
	SessionID uuid.UUID `json:"session_id"`
	UserID    uuid.UUID `json:"user_id"`
	Reason    string    `json:"reason"`
}

func (s *SessionService) purgeAndPublish(ctx context.Context, tokenHash []byte, userID, sessionID uuid.UUID, reason string) {
	s.purge(ctx, tokenHash, userID)
	b, err := json.Marshal(revokedEvent{SessionID: sessionID, UserID: userID, Reason: reason})
	if err != nil {
		return
	}
	cc, cancel := s.cctx(ctx)
	defer cancel()
	if err := s.rdb.Publish(cc, ChannelSessionRevoked, b).Err(); err != nil {
		s.log.WarnContext(ctx, "gagal menerbitkan session.revoked", "error", err)
	}
}

// afterCommit menjadwalkan fn setelah commit transaksi pada ctx (atau segera bila tanpa transaksi),
// dengan context yang lepas dari pembatalan request dan dari transaksi, serta batas waktu sendiri.
func (s *SessionService) afterCommit(ctx context.Context, fn func(ctx context.Context)) {
	bg := kernel.DetachTx(context.WithoutCancel(ctx))
	kernel.AfterCommit(ctx, func() {
		c, cancel := context.WithTimeout(bg, 5*time.Second)
		defer cancel()
		fn(c)
	})
}

// warnDegraded mencatat bahwa Redis tak terjangkau (mode terdegradasi), dibatasi satu kali per
// 30 detik agar request deras tidak membanjiri log.
func (s *SessionService) warnDegraded(ctx context.Context, err error) {
	now := time.Now().UnixNano()
	last := s.lastDegradedLog.Load()
	if now-last < int64(degradedLogGap) || !s.lastDegradedLog.CompareAndSwap(last, now) {
		return
	}
	s.log.WarnContext(ctx, "Redis tak terjangkau; sesi dilayani dari PostgreSQL (terdegradasi)",
		logger.SystemLog("auth.session.redis_degraded"), "error", err)
}
