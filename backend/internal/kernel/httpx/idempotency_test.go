package httpx_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/logger"
)

func newIdempotency(t *testing.T, rdb redis.UniversalClient, tweak func(*httpx.IdempotencyOptions)) *httpx.Idempotency {
	t.Helper()
	rs, _ := newResponder(t)
	o := httpx.IdempotencyOptions{
		Redis:     rdb,
		Responder: rs,
		Subject: func(r *http.Request) (string, bool) {
			u := r.Header.Get("X-Test-User")
			return u, u != ""
		},
		Route: func(*http.Request) string { return "/things" },
	}
	if tweak != nil {
		tweak(&o)
	}
	i, err := httpx.NewIdempotency(o)
	require.NoError(t, err)
	return i
}

// countingHandler membalas 201 dengan nomor panggilan; Set-Cookie harus TIDAK ikut diputar ulang.
func countingHandler(calls *atomic.Int32) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "session=rahasia")
		w.Header().Set("Location", fmt.Sprintf("/things/%d", n))
		w.WriteHeader(http.StatusCreated)
		_, _ = fmt.Fprintf(w, `{"data":{"n":%d}}`, n)
	})
}

func idemDo(h http.Handler, user, key, body string) *httptest.ResponseRecorder {
	return idemDoQuery(h, user, key, body, "")
}

func idemDoQuery(h http.Handler, user, key, body, query string) *httptest.ResponseRecorder {
	target := "/things"
	if query != "" {
		target += "?" + query
	}
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	r = r.WithContext(logger.WithRequestID(r.Context(), "req-idem"))
	r.Header.Set("Content-Type", "application/json")
	if user != "" {
		r.Header.Set("X-Test-User", user)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	e, ok := decodeBody(t, rec)["error"].(map[string]any)
	require.True(t, ok, rec.Body.String())
	return e["code"].(string)
}

func TestNewIdempotencyValidatesOptions(t *testing.T) {
	rs, _ := newResponder(t)
	_, err := httpx.NewIdempotency(httpx.IdempotencyOptions{Responder: rs})
	require.Error(t, err)
}

// Skenario 1 (docs/21 M02): key sama + body sama → respons sama, tanpa eksekusi ulang.
func TestIdempotencySameKeySameBodyReplays(t *testing.T) {
	var calls atomic.Int32
	h := newIdempotency(t, testRedis(t), nil).Require()(countingHandler(&calls))

	first := idemDo(h, "user-1", "key-0001", `{"a":1}`)
	require.Equal(t, http.StatusCreated, first.Code)
	require.Empty(t, first.Header().Get("Idempotent-Replayed"))

	second := idemDo(h, "user-1", "key-0001", `{"a":1}`)
	require.Equal(t, http.StatusCreated, second.Code)
	require.Equal(t, first.Body.String(), second.Body.String())
	require.Equal(t, "true", second.Header().Get("Idempotent-Replayed"))
	require.Equal(t, "application/json", second.Header().Get("Content-Type"))
	require.Equal(t, "/things/1", second.Header().Get("Location"))
	require.Empty(t, second.Header().Get("Set-Cookie"), "header sesi tidak boleh diputar ulang")
	require.EqualValues(t, 1, calls.Load(), "handler hanya dieksekusi sekali")
}

// Skenario 2: key sama + body beda → 422 idempotency_key_conflict.
func TestIdempotencySameKeyDifferentBodyConflicts(t *testing.T) {
	var calls atomic.Int32
	h := newIdempotency(t, testRedis(t), nil).Require()(countingHandler(&calls))

	require.Equal(t, http.StatusCreated, idemDo(h, "user-1", "key-0001", `{"a":1}`).Code)
	rec := idemDo(h, "user-1", "key-0001", `{"a":2}`)
	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	require.Equal(t, "idempotency_key_conflict", errorCode(t, rec))
	require.EqualValues(t, 1, calls.Load())

	// Hasil asli tetap bisa diputar ulang dengan body aslinya.
	again := idemDo(h, "user-1", "key-0001", `{"a":1}`)
	require.Equal(t, "true", again.Header().Get("Idempotent-Replayed"))
	require.Contains(t, again.Body.String(), `"n":1`)
}

func TestIdempotencyQueryStringIsPartOfTheRequestHash(t *testing.T) {
	var calls atomic.Int32
	h := newIdempotency(t, testRedis(t), nil).Require()(countingHandler(&calls))
	require.Equal(t, http.StatusCreated, idemDoQuery(h, "user-1", "key-0001", `{}`, "dry_run=0").Code)
	rec := idemDoQuery(h, "user-1", "key-0001", `{}`, "dry_run=1")
	require.Equal(t, "idempotency_key_conflict", errorCode(t, rec))
}

// Skenario 3: duplikat saat yang pertama masih berjalan → 409 idempotency_in_progress.
func TestIdempotencyConcurrentDuplicateIsInProgress(t *testing.T) {
	rdb := testRedis(t)
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var inFlightTTL atomic.Int64

	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		keys, _ := rdb.Keys(r.Context(), "idem:*").Result()
		if len(keys) == 1 {
			inFlightTTL.Store(int64(rdb.TTL(r.Context(), keys[0]).Val() / time.Second))
		}
		close(started)
		<-release
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"data":{"ok":true}}`)
	})
	h := newIdempotency(t, rdb, nil).Require()(slow)

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- idemDo(h, "user-1", "key-0001", `{"a":1}`) }()
	<-started

	dup := idemDo(h, "user-1", "key-0001", `{"a":1}`)
	require.Equal(t, http.StatusConflict, dup.Code)
	require.Equal(t, "idempotency_in_progress", errorCode(t, dup))
	require.Greater(t, inFlightTTL.Load(), int64(0), "kunci 'sedang berjalan' harus punya TTL")
	require.LessOrEqual(t, inFlightTTL.Load(), int64(60))

	close(release)
	first := <-done
	require.Equal(t, http.StatusCreated, first.Code)

	replayed := idemDo(h, "user-1", "key-0001", `{"a":1}`)
	require.Equal(t, "true", replayed.Header().Get("Idempotent-Replayed"))
	require.EqualValues(t, 1, calls.Load())
}

// Banyak request paralel dengan key sama: tepat satu dieksekusi.
func TestIdempotencyParallelRequestsExecuteOnce(t *testing.T) {
	var calls atomic.Int32
	slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(http.StatusCreated)
	})
	h := newIdempotency(t, testRedis(t), nil).Require()(slow)

	const n = 20
	var wg sync.WaitGroup
	var created, inProgress, replayed atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := idemDo(h, "user-1", "key-par", `{"a":1}`)
			switch {
			case rec.Header().Get("Idempotent-Replayed") == "true":
				replayed.Add(1)
			case rec.Code == http.StatusCreated:
				created.Add(1)
			case rec.Code == http.StatusConflict:
				inProgress.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, calls.Load())
	require.EqualValues(t, 1, created.Load())
	require.EqualValues(t, n, created.Load()+inProgress.Load()+replayed.Load())
}

func TestIdempotencyIsScopedPerUser(t *testing.T) {
	var calls atomic.Int32
	h := newIdempotency(t, testRedis(t), nil).Require()(countingHandler(&calls))

	a := idemDo(h, "user-a", "shared-key", `{"a":1}`)
	b := idemDo(h, "user-b", "shared-key", `{"a":1}`)
	require.Equal(t, http.StatusCreated, a.Code)
	require.Equal(t, http.StatusCreated, b.Code)
	require.Empty(t, b.Header().Get("Idempotent-Replayed"), "user lain tidak boleh menerima respons yang tersimpan milik user a")
	require.EqualValues(t, 2, calls.Load())

	require.Contains(t, idemDo(h, "user-a", "shared-key", `{"a":1}`).Body.String(), `"n":1`)
	require.Contains(t, idemDo(h, "user-b", "shared-key", `{"a":1}`).Body.String(), `"n":2`)
	require.EqualValues(t, 2, calls.Load())
}

func TestIdempotencyKeyIsRequiredAndValidated(t *testing.T) {
	var calls atomic.Int32
	h := newIdempotency(t, testRedis(t), nil).Require()(countingHandler(&calls))

	rec := idemDo(h, "user-1", "", `{}`)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "validation_failed", errorCode(t, rec))
	require.Contains(t, rec.Body.String(), "Idempotency-Key")

	for _, bad := range []string{"has space", "semi;colon", "slash/er", strings.Repeat("a", 129), "új"} {
		rec := idemDo(h, "user-1", bad, `{}`)
		require.Equal(t, http.StatusBadRequest, rec.Code, bad)
		require.Equal(t, "validation_failed", errorCode(t, rec), bad)
	}
	require.EqualValues(t, 0, calls.Load())
}

func TestIdempotencyRequiresAuthenticatedSubject(t *testing.T) {
	var calls atomic.Int32
	h := newIdempotency(t, testRedis(t), nil).Require()(countingHandler(&calls))
	rec := idemDo(h, "", "key-0001", `{}`)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Equal(t, "authentication_required", errorCode(t, rec))
	require.EqualValues(t, 0, calls.Load())
}

// Hanya 2xx yang disimpan: kegagalan harus bisa dicoba ulang dengan key yang sama.
func TestIdempotencyOnlySuccessIsStored(t *testing.T) {
	rdb := testRedis(t)
	var calls atomic.Int32
	statuses := []int{http.StatusInternalServerError, http.StatusUnprocessableEntity, http.StatusCreated}
	h := newIdempotency(t, rdb, nil).Require()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(calls.Add(1))
		w.WriteHeader(statuses[min(n, len(statuses))-1])
	}))

	require.Equal(t, http.StatusInternalServerError, idemDo(h, "user-1", "key-0001", `{}`).Code)
	n, _ := rdb.Exists(context.Background(), "idem:user-1:POST:/things:key-0001").Result()
	require.EqualValues(t, 0, n, "kunci dilepas setelah kegagalan")

	require.Equal(t, http.StatusUnprocessableEntity, idemDo(h, "user-1", "key-0001", `{}`).Code)
	require.Equal(t, http.StatusCreated, idemDo(h, "user-1", "key-0001", `{}`).Code)
	require.Equal(t, "true", idemDo(h, "user-1", "key-0001", `{}`).Header().Get("Idempotent-Replayed"))
	require.EqualValues(t, 3, calls.Load())
}

func TestIdempotencyPanicReleasesTheKey(t *testing.T) {
	var calls atomic.Int32
	h := newIdempotency(t, testRedis(t), nil).Require()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			panic("handler meledak")
		}
		w.WriteHeader(http.StatusCreated)
	}))

	func() {
		defer func() { require.NotNil(t, recover()) }()
		idemDo(h, "user-1", "key-0001", `{}`)
	}()
	require.Equal(t, http.StatusCreated, idemDo(h, "user-1", "key-0001", `{}`).Code, "kunci tak boleh macet setelah panic")
}

func TestIdempotencyFailsClosedWhenRedisIsDown(t *testing.T) {
	var calls atomic.Int32
	h := newIdempotency(t, deadRedis(t), nil).Require()(countingHandler(&calls))

	rec := idemDo(h, "user-1", "key-0001", `{}`)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Equal(t, "service_unavailable", errorCode(t, rec))
	require.NotContains(t, rec.Body.String(), "127.0.0.1")
	require.EqualValues(t, 0, calls.Load(), "tanpa Redis handler tidak boleh dieksekusi")
}

func TestIdempotencyKeyFormatAndTTL(t *testing.T) {
	rdb := testRedis(t)
	var calls atomic.Int32
	h := newIdempotency(t, rdb, nil).Require()(countingHandler(&calls))
	require.Equal(t, http.StatusCreated, idemDo(h, "user-1", "key-0001", `{}`).Code)

	keys, err := rdb.Keys(context.Background(), "idem:*").Result()
	require.NoError(t, err)
	require.Equal(t, []string{"idem:user-1:POST:/things:key-0001"}, keys)
	ttl := rdb.TTL(context.Background(), keys[0]).Val()
	require.Greater(t, ttl, 23*time.Hour+59*time.Minute)
	require.LessOrEqual(t, ttl, 24*time.Hour)
}

func TestIdempotencyHandlerStillReadsTheBody(t *testing.T) {
	h := newIdempotency(t, testRedis(t), nil).Require()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(b)
	}))
	rec := idemDo(h, "user-1", "key-0001", `{"hello":"dunia"}`)
	require.Equal(t, `{"hello":"dunia"}`, rec.Body.String())
}

func TestIdempotencyOversizedRequestBodyIs413(t *testing.T) {
	var calls atomic.Int32
	h := newIdempotency(t, testRedis(t), func(o *httpx.IdempotencyOptions) { o.MaxRequestBytes = 16 }).Require()(countingHandler(&calls))
	rec := idemDo(h, "user-1", "key-0001", strings.Repeat("x", 100))
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Equal(t, "payload_too_large", errorCode(t, rec))
	require.EqualValues(t, 0, calls.Load())
}

// Respons yang terlalu besar untuk disimpan tidak boleh "diputar ulang" setengah-setengah:
// kunci dilepas sehingga request ulang dieksekusi lagi (ADR-0014).
func TestIdempotencyOversizedResponseIsNotStored(t *testing.T) {
	rdb := testRedis(t)
	var calls atomic.Int32
	h := newIdempotency(t, rdb, func(o *httpx.IdempotencyOptions) { o.MaxResponseBytes = 10 }).Require()(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, strings.Repeat("y", 50))
		}))

	first := idemDo(h, "user-1", "key-0001", `{}`)
	require.Equal(t, strings.Repeat("y", 50), first.Body.String(), "klien tetap menerima respons utuh")
	n, _ := rdb.Exists(context.Background(), "idem:user-1:POST:/things:key-0001").Result()
	require.EqualValues(t, 0, n)
	require.Empty(t, idemDo(h, "user-1", "key-0001", `{}`).Header().Get("Idempotent-Replayed"))
	require.EqualValues(t, 2, calls.Load())
}

// Request lambat yang kuncinya sudah kedaluwarsa tidak boleh menimpa hasil request lain.
func TestIdempotencyExpiredLockOwnerCannotOverwriteNewerResult(t *testing.T) {
	var calls atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	h := newIdempotency(t, testRedis(t), func(o *httpx.IdempotencyOptions) { o.InProgressTTL = 200 * time.Millisecond }).Require()(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n := calls.Add(1)
			if n == 1 {
				close(started)
				<-release
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"n":%d}`, n)
		}))

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- idemDo(h, "user-1", "key-0001", `{}`) }()
	<-started
	time.Sleep(350 * time.Millisecond) // kunci A kedaluwarsa

	b := idemDo(h, "user-1", "key-0001", `{}`) // B merebut kunci dan selesai
	require.Equal(t, `{"n":2}`, b.Body.String())

	close(release)
	a := <-done
	require.Equal(t, `{"n":1}`, a.Body.String(), "A tetap menjawab kliennya sendiri")

	c := idemDo(h, "user-1", "key-0001", `{}`)
	require.Equal(t, "true", c.Header().Get("Idempotent-Replayed"))
	require.Equal(t, `{"n":2}`, c.Body.String(), "hasil B tidak boleh tertimpa A")
}
