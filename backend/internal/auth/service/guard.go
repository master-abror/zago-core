package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Penjaga login (docs/05 §12.3, ADR-0020 bagian 5 butir 47–53): kunci akun sementara, penghitung
// kegagalan per akun dan per IP, serta durasi kunci yang berlipat (jeda progresif).
//
// Aturan keras:
//   - Penjaga TIDAK PERNAH menggagalkan login yang sah karena galat Redis: galat berarti fallback memori
//     per proses dengan semantik yang sama (butir 51), bukan fail open dan bukan fail closed.
//   - Waktu berasal dari jam aplikasi (butir 52): nilai kunci menyimpan waktu kedaluwarsa dalam milidetik
//     (skrip Lua menerima now); TTL Redis hanya pembersihan (sisa + margin).
//   - Email tak pernah masuk kunci atau nilai Redis; IP hanya sebagai bagian kunci.

// ADR-0019: batas satu operasi Redis.
const guardRedisTimeout = 250 * time.Millisecond

// Setelah galat Redis, langsung ke memori selama ini.
const guardRedisBackoff = time.Second

// Batas identitas di tabel fallback memori (ADR-0020 butir 51).
const guardMemoryMaxKeys = 10000

// TTL Redis = sisa umur logis + margin (hanya pembersihan; kedaluwarsa logis ada di nilai).
const guardKeyMargin = time.Minute

// Umur penghitung strike (jeda progresif, ADR-0020 butir 49).
const guardStrikeTTL = 24 * time.Hour

// Batas atas semua durasi kebijakan.
const guardMaxWindow = 24 * time.Hour

// Kunci Redis (ADR-0020 butir 52). keyLock: user_id → waktu berakhir kunci (ms epoch).
// keyFail, keyStrike, keyIPFail: user_id atau ip → "jumlah:berakhir_ms".
const (
	keyLock   = "auth:lock:"
	keyFail   = "auth:fail:"
	keyStrike = "auth:strike:"
	keyIPFail = "auth:ipfail:"
)

// GuardPolicy adalah ambang penjaga login. Nilai nol TIDAK sah: pakai DefaultGuardPolicy sebagai dasar.
type GuardPolicy struct {
	MaxFailures   int           // kegagalan password per akun dalam FailureWindow sebelum kunci
	FailureWindow time.Duration // jendela penghitung (akun dan IP)
	LockDuration  time.Duration // durasi kunci pertama; berlipat per pengulangan
	LockMax       time.Duration // batas atas durasi kunci
	IPMaxFailures int           // kegagalan per IP dalam FailureWindow sebelum IP ditolak
}

// DefaultGuardPolicy adalah bawaan ADR-0020 butir 49–50: 5 kegagalan/15 menit mengunci 15 menit
// (berlipat sampai 60 menit); 30 kegagalan/15 menit per IP.
func DefaultGuardPolicy() GuardPolicy {
	return GuardPolicy{
		MaxFailures:   5,
		FailureWindow: 15 * time.Minute,
		LockDuration:  15 * time.Minute,
		LockMax:       60 * time.Minute,
		IPMaxFailures: 30,
	}
}

// Validate menolak ambang yang tak masuk akal atau berbahaya (mis. kunci tanpa batas atas).
func (p GuardPolicy) Validate() error {
	switch {
	case p.MaxFailures < 1 || p.MaxFailures > 100:
		return errors.New("guard: MaxFailures harus 1–100")
	case p.IPMaxFailures < 1 || p.IPMaxFailures > 10000:
		return errors.New("guard: IPMaxFailures harus 1–10000")
	case p.FailureWindow < time.Second || p.FailureWindow > guardMaxWindow:
		return errors.New("guard: FailureWindow harus antara 1s dan 24h")
	case p.LockDuration < time.Second || p.LockDuration > guardMaxWindow:
		return errors.New("guard: LockDuration harus antara 1s dan 24h")
	case p.LockMax < p.LockDuration || p.LockMax > guardMaxWindow:
		return errors.New("guard: LockMax harus antara LockDuration dan 24h")
	}
	return nil
}

// lockDuration menghitung durasi kunci ke-strikes (1-based): LockDuration, lalu berlipat dua per
// pengulangan, dibatasi LockMax. Skrip Lua accountFailScript menghitung hal yang sama.
func lockDuration(p GuardPolicy, strikes int) time.Duration {
	d := p.LockDuration
	for i := 1; i < strikes && d < p.LockMax; i++ {
		d *= 2
	}
	return min(d, p.LockMax)
}

// FailureResult adalah hasil RecordFailure. Locked hanya true pada kegagalan yang BARU memasang kunci
// (sekali per kunci; dipakai untuk menulis event auth.account.locked).
type FailureResult struct {
	Failures int           // nilai penghitung akun setelah kegagalan ini (0 = tak dihitung)
	Locked   bool          // kunci baru dipasang oleh kegagalan ini
	LockFor  time.Duration // durasi kunci yang dipasang (hanya bila Locked)
	Strikes  int           // strike ke-berapa (hanya bila Locked)
}

// LoginGuard adalah kontrak penjaga login bagi AuthService. Tidak ada metode yang mengembalikan galat:
// galat infrastruktur ditangani di dalam implementasi (fallback memori).
type LoginGuard interface {
	// IPBlocked: apakah IP ini sudah melewati ambang kegagalan (ditolak 429). ip kosong = tak dibatasi.
	IPBlocked(ctx context.Context, ip string) (retryAfter time.Duration, blocked bool)
	// AccountLocked: apakah kunci sementara akun aktif. Dipanggil hanya untuk akun yang dikenal.
	AccountLocked(ctx context.Context, userID uuid.UUID) bool
	// RecordFailure mencatat satu kegagalan password. userID nil = email tak dikenal (hanya penghitung IP).
	// Kegagalan saat kunci aktif tidak dihitung dan tidak memperpanjang kunci.
	RecordFailure(ctx context.Context, userID *uuid.UUID, ip string) FailureResult
	// RecordSuccess mereset penghitung dan strike akun (BUKAN penghitung IP, ADR-0020 butir 50).
	RecordSuccess(ctx context.Context, userID uuid.UUID)
}

// Guard adalah LoginGuard berbasis Redis dengan fallback memori per proses.
type Guard struct {
	rdb       redis.UniversalClient
	policy    GuardPolicy
	log       *slog.Logger
	now       func() time.Time
	mem       *memGuard
	timeout   time.Duration
	downUntil atomic.Int64 // UnixNano (jam NYATA); sebelum ini Redis dianggap mati
}

var _ LoginGuard = (*Guard)(nil)

// GuardOption mengubah Guard (terutama untuk tes).
type GuardOption func(*Guard)

// WithGuardClock mengganti jam aplikasi (untuk tes).
func WithGuardClock(now func() time.Time) GuardOption { return func(g *Guard) { g.now = now } }

// WithGuardRedisTimeout mengganti batas satu operasi Redis (bawaan 250 ms). Nilai ≤ 0 diabaikan.
func WithGuardRedisTimeout(d time.Duration) GuardOption {
	return func(g *Guard) {
		if d > 0 {
			g.timeout = d
		}
	}
}

// WithGuardMemoryLimit mengganti batas identitas di tabel fallback (bawaan 10.000; untuk tes). Nilai ≤ 0 diabaikan.
func WithGuardMemoryLimit(n int) GuardOption {
	return func(g *Guard) {
		if n > 0 {
			g.mem.max = n
		}
	}
}

// NewGuard membuat penjaga. rdb wajib; policy harus sah (mulai dari DefaultGuardPolicy); log nil → slog.Default.
func NewGuard(rdb redis.UniversalClient, policy GuardPolicy, log *slog.Logger, opts ...GuardOption) (*Guard, error) {
	if rdb == nil {
		return nil, errors.New("guard: klien Redis wajib")
	}
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.Default()
	}
	g := &Guard{rdb: rdb, policy: policy, log: log, now: time.Now, mem: newMemGuard(guardMemoryMaxKeys), timeout: guardRedisTimeout}
	for _, o := range opts {
		o(g)
	}
	return g, nil
}

// Skrip kegagalan akun (atomik). KEYS: fail, strike, lock. ARGV: now_ms, window_ms, max, base_ms,
// lockmax_ms, margin_ms, strike_ttl_ms. Hasil {hitungan, durasi_kunci_ms, strike}; {0,0,0} = kunci sudah
// aktif (tak ada yang dicatat). Nilai "jumlah:berakhir_ms"; jendela baru dimulai bila yang lama berakhir.
var accountFailScript = redis.NewScript(`
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local max = tonumber(ARGV[3])
local base = tonumber(ARGV[4])
local lockmax = tonumber(ARGV[5])
local margin = tonumber(ARGV[6])
local strikettl = tonumber(ARGV[7])

local function parse(v)
  if not v then return 0, 0 end
  local c, e = string.match(v, '^(%d+):(%d+)$')
  if not c then return 0, 0 end
  return tonumber(c), tonumber(e)
end

local lock = redis.call('GET', KEYS[3])
if lock and tonumber(lock) and tonumber(lock) > now then
  return {0, 0, 0}
end

local count, exp = parse(redis.call('GET', KEYS[1]))
if exp <= now then count = 0; exp = now + window end
count = count + 1
if count < max then
  redis.call('SET', KEYS[1], string.format('%d:%d', count, exp), 'PX', string.format('%d', exp - now + margin))
  return {count, 0, 0}
end

local strikes, sexp = parse(redis.call('GET', KEYS[2]))
if sexp <= now then strikes = 0 end
strikes = strikes + 1
local dur = base
local i = 1
while i < strikes and dur < lockmax do dur = dur * 2; i = i + 1 end
if dur > lockmax then dur = lockmax end
redis.call('SET', KEYS[3], string.format('%d', now + dur), 'PX', string.format('%d', dur + margin))
redis.call('SET', KEYS[2], string.format('%d:%d', strikes, now + strikettl), 'PX', string.format('%d', strikettl + margin))
redis.call('DEL', KEYS[1])
return {count, dur, strikes}
`)

// Skrip kegagalan IP (atomik). KEYS: ipfail. ARGV: now_ms, window_ms, margin_ms. Hasil {hitungan, berakhir_ms}.
var ipFailScript = redis.NewScript(`
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local margin = tonumber(ARGV[3])
local c, e = 0, 0
local v = redis.call('GET', KEYS[1])
if v then
  local a, b = string.match(v, '^(%d+):(%d+)$')
  if a then c = tonumber(a); e = tonumber(b) end
end
if e <= now then c = 0; e = now + window end
c = c + 1
redis.call('SET', KEYS[1], string.format('%d:%d', c, e), 'PX', string.format('%d', e - now + margin))
return {c, e}
`)

// viaRedis menjalankan fn dengan batas waktu; false berarti Redis tak dipakai (sedang mundur, ctx
// dibatalkan, atau galat) dan pemanggil harus memakai memori. Galat membuat Redis dianggap mati 1 detik.
func (g *Guard) viaRedis(ctx context.Context, op string, fn func(context.Context) error) bool {
	if time.Now().UnixNano() < g.downUntil.Load() {
		return false
	}
	if ctx.Err() != nil {
		return false // klien membatalkan: bukan tanda Redis bermasalah
	}
	cctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	if err := fn(cctx); err != nil {
		if ctx.Err() == nil {
			g.markDown(ctx, op, err)
		}
		return false
	}
	return true
}

func (g *Guard) markDown(ctx context.Context, op string, err error) {
	now := time.Now().UnixNano()
	if prev := g.downUntil.Swap(time.Now().Add(guardRedisBackoff).UnixNano()); now >= prev {
		g.log.WarnContext(ctx, "login guard: Redis tak tersedia, memakai fallback memori",
			slog.String("op", op), slog.Any("error", err))
	}
}

// parseCounter membaca nilai "jumlah:berakhir_ms".
func parseCounter(v string) (n int, exp time.Time, ok bool) {
	a, b, found := strings.Cut(v, ":")
	if !found {
		return 0, time.Time{}, false
	}
	n, err1 := strconv.Atoi(a)
	ms, err2 := strconv.ParseInt(b, 10, 64)
	if err1 != nil || err2 != nil || n < 0 {
		return 0, time.Time{}, false
	}
	return n, time.UnixMilli(ms), true
}

// IPBlocked: lihat LoginGuard.
func (g *Guard) IPBlocked(ctx context.Context, ip string) (time.Duration, bool) {
	if ip == "" {
		return 0, false
	}
	now := g.now()
	var (
		retry   time.Duration
		blocked bool
	)
	if g.viaRedis(ctx, "ip_blocked", func(c context.Context) error {
		v, err := g.rdb.Get(c, keyIPFail+ip).Result()
		if errors.Is(err, redis.Nil) {
			return nil
		}
		if err != nil {
			return err
		}
		n, exp, ok := parseCounter(v)
		if !ok {
			// Nilai rusak tak mungkin dari kode ini: gagal tertutup sampai TTL pembersihan.
			blocked, retry = true, guardKeyMargin
			return nil
		}
		if exp.After(now) && n >= g.policy.IPMaxFailures {
			blocked, retry = true, exp.Sub(now)
		}
		return nil
	}) {
		return retry, blocked
	}
	return g.mem.ipBlocked(now, ip, g.policy.IPMaxFailures)
}

// AccountLocked: lihat LoginGuard.
func (g *Guard) AccountLocked(ctx context.Context, userID uuid.UUID) bool {
	now := g.now()
	locked := false
	if g.viaRedis(ctx, "account_locked", func(c context.Context) error {
		v, err := g.rdb.Get(c, keyLock+userID.String()).Result()
		if errors.Is(err, redis.Nil) {
			return nil
		}
		if err != nil {
			return err
		}
		ms, perr := strconv.ParseInt(v, 10, 64)
		// Nilai rusak = terkunci (gagal tertutup); kunci kedaluwarsa menurut jam aplikasi = terbuka.
		locked = perr != nil || ms > now.UnixMilli()
		return nil
	}) {
		return locked
	}
	return g.mem.accountLocked(now, userID)
}

// RecordFailure: lihat LoginGuard.
func (g *Guard) RecordFailure(ctx context.Context, userID *uuid.UUID, ip string) FailureResult {
	now := g.now()
	if ip != "" {
		g.recordIP(ctx, now, ip)
	}
	if userID == nil {
		return FailureResult{}
	}
	return g.recordAccount(ctx, now, *userID)
}

func (g *Guard) recordIP(ctx context.Context, now time.Time, ip string) {
	if g.viaRedis(ctx, "record_ip_failure", func(c context.Context) error {
		return ipFailScript.Run(c, g.rdb, []string{keyIPFail + ip},
			now.UnixMilli(), g.policy.FailureWindow.Milliseconds(), guardKeyMargin.Milliseconds()).Err()
	}) {
		return
	}
	g.mem.ipFailure(now, ip, g.policy)
}

func (g *Guard) recordAccount(ctx context.Context, now time.Time, userID uuid.UUID) FailureResult {
	var res FailureResult
	if g.viaRedis(ctx, "record_failure", func(c context.Context) error {
		uid := userID.String()
		vals, err := accountFailScript.Run(c, g.rdb,
			[]string{keyFail + uid, keyStrike + uid, keyLock + uid},
			now.UnixMilli(), g.policy.FailureWindow.Milliseconds(), g.policy.MaxFailures,
			g.policy.LockDuration.Milliseconds(), g.policy.LockMax.Milliseconds(),
			guardKeyMargin.Milliseconds(), guardStrikeTTL.Milliseconds()).Slice()
		if err != nil {
			return err
		}
		if len(vals) != 3 {
			return fmt.Errorf("guard: respons skrip tak terduga (%d nilai)", len(vals))
		}
		n, ok1 := vals[0].(int64)
		lockMs, ok2 := vals[1].(int64)
		strikes, ok3 := vals[2].(int64)
		if !ok1 || !ok2 || !ok3 {
			return errors.New("guard: tipe respons skrip tak terduga")
		}
		res = FailureResult{
			Failures: int(n),
			Locked:   lockMs > 0,
			LockFor:  time.Duration(lockMs) * time.Millisecond,
			Strikes:  int(strikes),
		}
		return nil
	}) {
		return res
	}
	return g.mem.recordFailure(now, userID, g.policy)
}

// RecordSuccess: lihat LoginGuard. Membersihkan Redis dan memori (state fallback lama tak boleh tersisa).
func (g *Guard) RecordSuccess(ctx context.Context, userID uuid.UUID) {
	g.mem.success(userID)
	g.viaRedis(ctx, "record_success", func(c context.Context) error {
		uid := userID.String()
		return g.rdb.Del(c, keyFail+uid, keyStrike+uid).Err()
	})
}
