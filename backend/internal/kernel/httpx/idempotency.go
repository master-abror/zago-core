package httpx

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/redis/go-redis/v9"
)

// Idempotency (docs/08 §6, errata I-07, ADR-0014): hasil request mutasi disimpan di Redis selama
// 24 jam di bawah kunci `idem:{subject}:{method}:{route}:{key}`. Request ulang dengan key dan body
// yang sama mendapat respons asli tanpa dieksekusi lagi; key sama dengan body berbeda → 422
// idempotency_key_conflict; duplikat yang datang saat yang pertama masih berjalan → 409
// idempotency_in_progress. Redis tak tersedia → fail closed (503), tidak pernah mengeksekusi
// tanpa perlindungan (docs/10 §15).

const (
	// IdempotencyKeyHeader adalah header request yang membawa kunci idempotensi.
	IdempotencyKeyHeader = "Idempotency-Key"
	// IdempotencyReplayedHeader ditambahkan pada respons yang diputar ulang dari penyimpanan.
	IdempotencyReplayedHeader = "Idempotent-Replayed"

	defaultIdempotencyTTL   = 24 * time.Hour
	defaultInProgressTTL    = 60 * time.Second
	defaultMaxResponseBytes = 1 << 20
	idemFinishTimeout       = 2 * time.Second
	idemMaxAcquireAttempts  = 3
	idemKeyPrefix           = "idem"
	idemStateInProgress     = "p"
	idemStateDone           = "d"
	idemFieldName           = "Idempotency-Key"
	idemOwnerBytes          = 16
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._~:-]{1,128}$`)

// Hanya header ini yang disimpan dan diputar ulang. Set-Cookie dan header sesi/CSRF TIDAK PERNAH
// ikut (replay tidak boleh menularkan kredensial).
var idemReplayHeaders = []string{"Content-Type", "Location"}

// Lua: operasi hanya berlaku bila kunci masih milik pemanggil (token owner cocok). Mencegah request
// lambat yang kuncinya sudah kedaluwarsa menimpa hasil request lain.
var (
	idemFinalizeScript = redis.NewScript(`
local cur = redis.call('GET', KEYS[1])
if not cur then return 0 end
local ok, obj = pcall(cjson.decode, cur)
if not ok or obj['o'] ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
return 1
`)
	idemReleaseScript = redis.NewScript(`
local cur = redis.call('GET', KEYS[1])
if not cur then return 0 end
local ok, obj = pcall(cjson.decode, cur)
if not ok or obj['o'] ~= ARGV[1] then return 0 end
redis.call('DEL', KEYS[1])
return 1
`)
)

// IdempotencyOptions mengatur middleware idempotensi.
type IdempotencyOptions struct {
	Redis     redis.UniversalClient // wajib
	Responder *Responder            // wajib
	// Subject mengembalikan identitas pemanggil (mis. user id) yang menjadi bagian kunci Redis, sehingga
	// satu user tak pernah bisa memutar ulang respons user lain. Wajib. ok=false → 401.
	Subject func(*http.Request) (string, bool)
	// Route mengembalikan pola rute (mis. pola chi) untuk kunci; bawaan: r.URL.Path.
	Route func(*http.Request) string
	Log   *slog.Logger

	TTL              time.Duration // bawaan 24 jam
	InProgressTTL    time.Duration // umur kunci "sedang berjalan" bila proses mati; bawaan 60 dtk
	MaxRequestBytes  int64         // bawaan DefaultMaxBodyBytes (1 MiB)
	MaxResponseBytes int           // respons lebih besar tidak disimpan; bawaan 1 MiB
}

// Idempotency adalah middleware idempotensi berbasis Redis.
type Idempotency struct {
	rdb     redis.UniversalClient
	rs      *Responder
	subject func(*http.Request) (string, bool)
	route   func(*http.Request) string
	log     *slog.Logger
	ttl     time.Duration
	inProg  time.Duration
	maxReq  int64
	maxResp int
}

// NewIdempotency memvalidasi opsi dan membuat middleware.
func NewIdempotency(o IdempotencyOptions) (*Idempotency, error) {
	if o.Redis == nil || o.Responder == nil || o.Subject == nil {
		return nil, errors.New("httpx: IdempotencyOptions.Redis, Responder, dan Subject wajib diisi")
	}
	i := &Idempotency{
		rdb: o.Redis, rs: o.Responder, subject: o.Subject, route: o.Route, log: o.Log,
		ttl: o.TTL, inProg: o.InProgressTTL, maxReq: o.MaxRequestBytes, maxResp: o.MaxResponseBytes,
	}
	if i.route == nil {
		i.route = func(r *http.Request) string { return r.URL.Path }
	}
	if i.log == nil {
		i.log = slog.Default()
	}
	if i.ttl <= 0 {
		i.ttl = defaultIdempotencyTTL
	}
	if i.inProg <= 0 {
		i.inProg = defaultInProgressTTL
	}
	if i.maxReq <= 0 {
		i.maxReq = DefaultMaxBodyBytes
	}
	if i.maxResp <= 0 {
		i.maxResp = defaultMaxResponseBytes
	}
	return i, nil
}

// idemRecord adalah nilai yang disimpan di Redis.
type idemRecord struct {
	Owner  string            `json:"o"`
	State  string            `json:"s"`
	Hash   string            `json:"h"`
	Status int               `json:"c,omitempty"`
	Header map[string]string `json:"hd,omitempty"`
	Body   []byte            `json:"b,omitempty"`
}

// Require mengembalikan middleware yang MEWAJIBKAN Idempotency-Key (untuk route Idempotent(), 07 §5.1).
func (i *Idempotency) Require() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { i.serve(w, r, next) })
	}
}

func (i *Idempotency) serve(w http.ResponseWriter, r *http.Request, next http.Handler) {
	key := r.Header.Get(IdempotencyKeyHeader)
	if key == "" {
		i.rs.Error(w, r, Validation(FieldError{Field: idemFieldName, Code: "required", Message: "Idempotency-Key header is required"}))
		return
	}
	if !idempotencyKeyPattern.MatchString(key) {
		i.rs.Error(w, r, Validation(FieldError{Field: idemFieldName, Code: "invalid", Message: "Idempotency-Key must be 1-128 characters of A-Z a-z 0-9 . _ ~ : -"}))
		return
	}
	subject, ok := i.subject(r)
	if !ok || subject == "" {
		i.rs.Error(w, r, AuthenticationRequired.New())
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, i.maxReq))
	if err != nil {
		i.rs.Error(w, r, bodyReadError(err))
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	route := i.route(r)
	hash := requestHash(r.Method, route, r.URL.RawQuery, body)
	redisKey := idemKeyPrefix + ":" + subject + ":" + r.Method + ":" + route + ":" + key
	owner := newOwnerToken()
	inProgress := mustJSON(idemRecord{Owner: owner, State: idemStateInProgress, Hash: hash})

	for attempt := 0; attempt < idemMaxAcquireAttempts; attempt++ {
		_, err := i.rdb.SetArgs(r.Context(), redisKey, inProgress, redis.SetArgs{Mode: "NX", TTL: i.inProg}).Result()
		switch {
		case err == nil:
			i.execute(w, r, next, redisKey, owner, hash)
			return
		case !errors.Is(err, redis.Nil):
			i.rs.Error(w, r, ServiceUnavailable.Wrap(err))
			return
		}

		raw, err := i.rdb.Get(r.Context(), redisKey).Bytes()
		if errors.Is(err, redis.Nil) {
			continue // kedaluwarsa di antara SET dan GET: coba rebut lagi
		}
		if err != nil {
			i.rs.Error(w, r, ServiceUnavailable.Wrap(err))
			return
		}
		var rec idemRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			i.rs.Error(w, r, ServiceUnavailable.Wrap(err))
			return
		}
		switch {
		case rec.Hash != hash:
			i.rs.Error(w, r, IdempotencyKeyConflict.New())
		case rec.State == idemStateInProgress:
			i.rs.Error(w, r, IdempotencyInProgress.New())
		default:
			replay(w, rec)
		}
		return
	}
	i.rs.Error(w, r, ServiceUnavailable.Wrap(errors.New("idempotency: gagal merebut kunci setelah beberapa percobaan")))
}

func (i *Idempotency) execute(w http.ResponseWriter, r *http.Request, next http.Handler, redisKey, owner, hash string) {
	rec := &recorder{ResponseWriter: w, max: i.maxResp}
	finished := false
	defer func() {
		if !finished { // panic: lepaskan kunci supaya klien bisa mencoba lagi
			i.release(r.Context(), redisKey, owner)
		}
	}()

	next.ServeHTTP(rec, r)
	finished = true

	status := rec.statusCode()
	if status < 200 || status > 299 || rec.overflow {
		// Hanya keberhasilan yang disimpan; kegagalan dan respons terlalu besar dilepas (ADR-0014).
		if rec.overflow {
			i.log.WarnContext(r.Context(), "respons idempotent terlalu besar untuk disimpan; kunci dilepas",
				slog.Int("status", status), slog.Int("max_bytes", i.maxResp))
		}
		i.release(r.Context(), redisKey, owner)
		return
	}

	val := mustJSON(idemRecord{Owner: owner, State: idemStateDone, Hash: hash, Status: status, Header: rec.header, Body: rec.body.Bytes()})
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), idemFinishTimeout)
	defer cancel()
	n, err := idemFinalizeScript.Run(ctx, i.rdb, []string{redisKey}, owner, val, int(i.ttl.Seconds())).Int()
	if err != nil || n != 1 {
		i.log.ErrorContext(r.Context(), "gagal menyimpan hasil idempotency",
			slog.Any("error", err), slog.Int("owned", n))
	}
}

func (i *Idempotency) release(ctx context.Context, redisKey, owner string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), idemFinishTimeout)
	defer cancel()
	if err := idemReleaseScript.Run(ctx, i.rdb, []string{redisKey}, owner).Err(); err != nil && !errors.Is(err, redis.Nil) {
		i.log.ErrorContext(ctx, "gagal melepas kunci idempotency", slog.Any("error", err))
	}
}

func replay(w http.ResponseWriter, rec idemRecord) {
	h := w.Header()
	for k, v := range rec.Header {
		h.Set(k, v)
	}
	h.Set("Cache-Control", "no-store")
	h.Set(IdempotencyReplayedHeader, "true")
	w.WriteHeader(rec.Status)
	_, _ = w.Write(rec.Body)
}

func bodyReadError(err error) *Error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return PayloadTooLarge.Wrap(err)
	}
	return ValidationFailed.Wrap(err).WithMessage("The request body could not be read.")
}

func requestHash(method, route, rawQuery string, body []byte) string {
	h := sha256.New()
	for _, part := range []string{method, route, rawQuery} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

func newOwnerToken() string {
	b := make([]byte, idemOwnerBytes)
	if _, err := rand.Read(b); err != nil {
		panic("httpx: crypto/rand gagal: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic("httpx: json.Marshal gagal: " + err.Error()) // struct sederhana: tak mungkin
	}
	return string(b)
}

// recorder meneruskan respons ke klien sambil merekam status, header yang diizinkan, dan body.
type recorder struct {
	http.ResponseWriter
	max      int
	wrote    bool
	status   int
	header   map[string]string
	body     bytes.Buffer
	overflow bool
}

func (r *recorder) WriteHeader(code int) {
	if code >= 100 && code < 200 && code != http.StatusSwitchingProtocols {
		r.ResponseWriter.WriteHeader(code) // informasional: bukan respons akhir
		return
	}
	if !r.wrote {
		r.wrote = true
		r.status = code
		for _, name := range idemReplayHeaders {
			if v := r.ResponseWriter.Header().Get(name); v != "" {
				if r.header == nil {
					r.header = map[string]string{}
				}
				r.header[name] = v
			}
		}
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(p []byte) (int, error) {
	if !r.wrote {
		r.WriteHeader(http.StatusOK)
	}
	if !r.overflow {
		if r.body.Len()+len(p) > r.max {
			r.overflow = true
			r.body.Reset()
		} else {
			r.body.Write(p)
		}
	}
	return r.ResponseWriter.Write(p)
}

func (r *recorder) statusCode() int {
	if !r.wrote {
		return http.StatusOK
	}
	return r.status
}

// Unwrap mendukung http.ResponseController (Flush, Hijack, dst.).
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
