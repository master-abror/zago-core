package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Rate limiter (docs/08 §7, docs/10 §15, ADR-0014): jendela tetap (fixed window) di Redis lewat
// satu skrip Lua atomik. Header X-RateLimit-Limit/-Remaining/-Reset ada di setiap respons yang
// dibatasi; 429 memakai envelope error standar + Retry-After.
//
// Bila Redis tak tersedia: endpoint Sensitive jatuh ke batas konservatif per-instance di memori
// (separuh Limit), endpoint biasa dilewatkan (fail open) dan kegagalannya dicatat.

const (
	rlLimitHeader     = "X-RateLimit-Limit"
	rlRemainingHeader = "X-RateLimit-Remaining"
	rlResetHeader     = "X-RateLimit-Reset"

	rlRedisTimeout  = 250 * time.Millisecond
	rlRedisBackoff  = time.Second // setelah galat Redis, langsung ke fallback selama ini
	rlMemoryMaxKeys = 10000
	rlKeyPrefix     = "rl"
)

// Fixed window: INCR; set kedaluwarsa pada hit pertama (dan perbaiki bila kunci hilang TTL-nya).
var rlScript = redis.NewScript(`
local c = redis.call('INCR', KEYS[1])
local ttl = redis.call('PTTL', KEYS[1])
if c == 1 or ttl < 0 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
  ttl = tonumber(ARGV[1])
end
return {c, ttl}
`)

// Rule adalah satu batas laju.
type Rule struct {
	Name      string        // bagian kunci Redis; unik per kebijakan, mis. "auth.login"
	Limit     int           // permintaan per jendela, ≥ 1
	Window    time.Duration // panjang jendela, ≥ 1 ms
	Sensitive bool          // true → fallback memori saat Redis mati; false → fail open
}

func (r Rule) validate() error {
	if r.Name == "" || r.Limit < 1 || r.Window < time.Millisecond {
		return errors.New("httpx: Rule butuh Name, Limit ≥ 1, dan Window ≥ 1ms")
	}
	return nil
}

// KeyFunc mengembalikan identitas yang dibatasi (mis. "ip:203.0.113.7" atau "user:<uuid>").
// String kosong berarti request ini tidak dibatasi oleh aturan tersebut.
type KeyFunc func(*http.Request) string

// RateResult adalah hasil satu pemeriksaan.
type RateResult struct {
	Allowed    bool
	Limit      int
	Remaining  int
	ResetAt    time.Time
	RetryAfter time.Duration
	Skipped    bool // Redis mati pada aturan non-sensitif: tidak dihitung, tanpa header
}

// RateLimiter membatasi laju request.
type RateLimiter struct {
	rdb       redis.UniversalClient
	rs        *Responder
	log       *slog.Logger
	mem       *memoryLimiter
	now       func() time.Time
	downUntil atomic.Int64 // UnixNano; sebelum ini Redis dianggap mati
}

// NewRateLimiter membuat limiter. log nil → slog.Default.
func NewRateLimiter(rdb redis.UniversalClient, rs *Responder, log *slog.Logger) (*RateLimiter, error) {
	if rdb == nil || rs == nil {
		return nil, errors.New("httpx: NewRateLimiter butuh klien Redis dan Responder")
	}
	if log == nil {
		log = slog.Default()
	}
	return &RateLimiter{rdb: rdb, rs: rs, log: log, mem: newMemoryLimiter(rlMemoryMaxKeys), now: time.Now}, nil
}

// Check menghitung satu request untuk identitas id pada aturan rule.
func (l *RateLimiter) Check(ctx context.Context, rule Rule, id string) RateResult {
	now := l.now()
	if now.UnixNano() >= l.downUntil.Load() {
		res, err := l.checkRedis(ctx, rule, id, now)
		if err == nil {
			return res
		}
		if prev := l.downUntil.Swap(now.Add(rlRedisBackoff).UnixNano()); now.UnixNano() >= prev {
			l.log.WarnContext(ctx, "rate limiter: Redis tak tersedia, memakai fallback",
				slog.String("rule", rule.Name), slog.Bool("sensitive", rule.Sensitive), slog.Any("error", err))
		}
	}
	if rule.Sensitive {
		return l.mem.check(rule, id, now)
	}
	return RateResult{Allowed: true, Skipped: true}
}

func (l *RateLimiter) checkRedis(ctx context.Context, rule Rule, id string, now time.Time) (RateResult, error) {
	ctx, cancel := context.WithTimeout(ctx, rlRedisTimeout)
	defer cancel()
	vals, err := rlScript.Run(ctx, l.rdb, []string{rlKeyPrefix + ":" + rule.Name + ":" + id}, rule.Window.Milliseconds()).Slice()
	if err != nil {
		return RateResult{}, err
	}
	if len(vals) != 2 {
		return RateResult{}, errors.New("rate limiter: respons skrip tak terduga")
	}
	count, ok1 := vals[0].(int64)
	ttlMs, ok2 := vals[1].(int64)
	if !ok1 || !ok2 {
		return RateResult{}, errors.New("rate limiter: tipe respons skrip tak terduga")
	}
	retry := time.Duration(ttlMs) * time.Millisecond
	return RateResult{
		Allowed:    count <= int64(rule.Limit),
		Limit:      rule.Limit,
		Remaining:  int(max(0, int64(rule.Limit)-count)),
		ResetAt:    now.Add(retry),
		RetryAfter: retry,
	}, nil
}

// Middleware membatasi route dengan satu aturan. Panic bila aturan tak valid (bug pemrograman).
func (l *RateLimiter) Middleware(rule Rule, key KeyFunc) func(http.Handler) http.Handler {
	if err := rule.validate(); err != nil {
		panic(err)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := key(r)
			if id == "" {
				next.ServeHTTP(w, r)
				return
			}
			res := l.Check(r.Context(), rule, id)
			if !res.Skipped {
				setRateHeaders(w, res)
			}
			if !res.Allowed {
				l.rs.Error(w, r, RateLimitExceeded.New().WithRetryAfter(max(res.RetryAfter, time.Second)))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// setRateHeaders menulis trio header. Bila beberapa aturan berlaku, yang paling ketat (sisa paling
// sedikit) menang.
func setRateHeaders(w http.ResponseWriter, res RateResult) {
	h := w.Header()
	if cur, err := strconv.Atoi(h.Get(rlRemainingHeader)); err == nil && cur <= res.Remaining {
		return
	}
	reset := res.ResetAt.Unix()
	if res.ResetAt.Nanosecond() > 0 {
		reset++ // dibulatkan ke atas
	}
	h.Set(rlLimitHeader, strconv.Itoa(res.Limit))
	h.Set(rlRemainingHeader, strconv.Itoa(res.Remaining))
	h.Set(rlResetHeader, strconv.FormatInt(reset, 10))
}

// memoryLimiter adalah fallback per-instance: batas konservatif = Limit/2 (minimal 1).
type memoryLimiter struct {
	mu      sync.Mutex
	entries map[string]*memEntry
	maxKeys int
}

type memEntry struct {
	count   int
	resetAt time.Time
}

func newMemoryLimiter(maxKeys int) *memoryLimiter {
	return &memoryLimiter{entries: make(map[string]*memEntry), maxKeys: maxKeys}
}

func (m *memoryLimiter) check(rule Rule, id string, now time.Time) RateResult {
	limit := max(1, rule.Limit/2)
	k := rule.Name + "\x00" + id

	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[k]
	if !ok || !now.Before(e.resetAt) {
		if !ok && len(m.entries) >= m.maxKeys {
			m.purge(now)
			if len(m.entries) >= m.maxKeys { // penuh oleh jendela aktif: tolak identitas baru (fail closed)
				return RateResult{Allowed: false, Limit: limit, ResetAt: now.Add(rule.Window), RetryAfter: rule.Window}
			}
		}
		e = &memEntry{resetAt: now.Add(rule.Window)}
		m.entries[k] = e
	}
	e.count++
	return RateResult{
		Allowed:    e.count <= limit,
		Limit:      limit,
		Remaining:  max(0, limit-e.count),
		ResetAt:    e.resetAt,
		RetryAfter: e.resetAt.Sub(now),
	}
}

func (m *memoryLimiter) purge(now time.Time) {
	for k, e := range m.entries {
		if !now.Before(e.resetAt) {
			delete(m.entries, k)
		}
	}
}
