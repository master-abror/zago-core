package httpx_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/logger"
)

func newLimiter(t *testing.T, rdb redis.UniversalClient) *httpx.RateLimiter {
	t.Helper()
	rs, _ := newResponder(t)
	l, err := httpx.NewRateLimiter(rdb, rs, nil)
	require.NoError(t, err)
	return l
}

func headerKey(name string) httpx.KeyFunc {
	return func(r *http.Request) string { return r.Header.Get(name) }
}

func okHandler(calls *atomic.Int32) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
}

func rlDo(h http.Handler, id string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r = r.WithContext(logger.WithRequestID(r.Context(), "req-rl"))
	if id != "" {
		r.Header.Set("X-Id", id)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestNewRateLimiterValidates(t *testing.T) {
	rs, _ := newResponder(t)
	_, err := httpx.NewRateLimiter(nil, rs, nil)
	require.Error(t, err)
	_, err = httpx.NewRateLimiter(deadRedis(t), nil, nil)
	require.Error(t, err)
}

func TestRateLimitInvalidRulePanics(t *testing.T) {
	l := newLimiter(t, deadRedis(t))
	for _, r := range []httpx.Rule{{}, {Name: "x", Limit: 0, Window: time.Second}, {Name: "x", Limit: 1}, {Limit: 1, Window: time.Second}} {
		require.Panics(t, func() { l.Middleware(r, headerKey("X-Id")) })
	}
}

func TestRateLimitAllowsUpToLimitThen429(t *testing.T) {
	var calls atomic.Int32
	l := newLimiter(t, testRedis(t))
	h := l.Middleware(httpx.Rule{Name: "t.basic", Limit: 3, Window: 5 * time.Second}, headerKey("X-Id"))(okHandler(&calls))

	for i, wantRemaining := range []string{"2", "1", "0"} {
		rec := rlDo(h, "ip-1")
		require.Equal(t, http.StatusNoContent, rec.Code, "request %d", i+1)
		require.Equal(t, "3", rec.Header().Get("X-RateLimit-Limit"))
		require.Equal(t, wantRemaining, rec.Header().Get("X-RateLimit-Remaining"))
		reset, err := strconv.ParseInt(rec.Header().Get("X-RateLimit-Reset"), 10, 64)
		require.NoError(t, err)
		require.Greater(t, reset, time.Now().Unix()-1)
		require.LessOrEqual(t, reset, time.Now().Add(6*time.Second).Unix())
	}

	rec := rlDo(h, "ip-1")
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "rate_limit_exceeded", errorCode(t, rec))
	require.Equal(t, "0", rec.Header().Get("X-RateLimit-Remaining"))
	retry, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, retry, 1)
	require.LessOrEqual(t, retry, 5)
	require.EqualValues(t, 3, calls.Load(), "handler tidak dijalankan saat dibatasi")
}

func TestRateLimitIsPerIdentity(t *testing.T) {
	var calls atomic.Int32
	l := newLimiter(t, testRedis(t))
	h := l.Middleware(httpx.Rule{Name: "t.ident", Limit: 1, Window: 5 * time.Second}, headerKey("X-Id"))(okHandler(&calls))

	require.Equal(t, http.StatusNoContent, rlDo(h, "a").Code)
	require.Equal(t, http.StatusTooManyRequests, rlDo(h, "a").Code)
	require.Equal(t, http.StatusNoContent, rlDo(h, "b").Code)
}

func TestRateLimitWindowResets(t *testing.T) {
	var calls atomic.Int32
	l := newLimiter(t, testRedis(t))
	h := l.Middleware(httpx.Rule{Name: "t.reset", Limit: 1, Window: 300 * time.Millisecond}, headerKey("X-Id"))(okHandler(&calls))

	require.Equal(t, http.StatusNoContent, rlDo(h, "a").Code)
	require.Equal(t, http.StatusTooManyRequests, rlDo(h, "a").Code)
	require.Eventually(t, func() bool { return rlDo(h, "a").Code == http.StatusNoContent }, 3*time.Second, 50*time.Millisecond)
}

func TestRateLimitConcurrentRequestsNeverExceedLimit(t *testing.T) {
	var calls atomic.Int32
	l := newLimiter(t, testRedis(t))
	h := l.Middleware(httpx.Rule{Name: "t.conc", Limit: 10, Window: 10 * time.Second}, headerKey("X-Id"))(okHandler(&calls))

	var wg sync.WaitGroup
	var allowed, denied atomic.Int32
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if rlDo(h, "a").Code == http.StatusNoContent {
				allowed.Add(1)
			} else {
				denied.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 10, allowed.Load())
	require.EqualValues(t, 50, denied.Load())
}

func TestRateLimitEmptyKeyIsNotLimited(t *testing.T) {
	var calls atomic.Int32
	l := newLimiter(t, testRedis(t))
	h := l.Middleware(httpx.Rule{Name: "t.empty", Limit: 1, Window: 5 * time.Second}, headerKey("X-Id"))(okHandler(&calls))
	for i := 0; i < 3; i++ {
		rec := rlDo(h, "")
		require.Equal(t, http.StatusNoContent, rec.Code)
		require.Empty(t, rec.Header().Get("X-RateLimit-Limit"))
	}
}

func TestRateLimitCounterHasTTL(t *testing.T) {
	rdb := testRedis(t)
	var calls atomic.Int32
	h := newLimiter(t, rdb).Middleware(httpx.Rule{Name: "t.ttl", Limit: 5, Window: 4 * time.Second}, headerKey("X-Id"))(okHandler(&calls))
	rlDo(h, "a")
	ttl := rdb.TTL(context.Background(), "rl:t.ttl:a").Val()
	require.Greater(t, ttl, time.Duration(0))
	require.LessOrEqual(t, ttl, 4*time.Second)
}

// Beberapa aturan pada satu route: header yang paling ketat menang, apa pun urutannya.
func TestRateLimitMostRestrictiveHeadersWin(t *testing.T) {
	loose := httpx.Rule{Name: "t.loose", Limit: 10, Window: 5 * time.Second}
	tight := httpx.Rule{Name: "t.tight", Limit: 3, Window: 5 * time.Second}

	for name, order := range map[string][]httpx.Rule{"longgar dulu": {loose, tight}, "ketat dulu": {tight, loose}} {
		l := newLimiter(t, testRedis(t))
		var calls atomic.Int32
		h := okHandler(&calls)
		for i := len(order) - 1; i >= 0; i-- { // order[0] menjadi middleware terluar
			h = l.Middleware(order[i], headerKey("X-Id"))(h)
		}
		rec := rlDo(h, "a")
		require.Equal(t, "3", rec.Header().Get("X-RateLimit-Limit"), name)
		require.Equal(t, "2", rec.Header().Get("X-RateLimit-Remaining"), name)
	}
}

// Redis mati: endpoint sensitif memakai fallback memori yang konservatif (Limit/2).
func TestRateLimitSensitiveFallsBackToMemoryWhenRedisIsDown(t *testing.T) {
	var calls atomic.Int32
	l := newLimiter(t, deadRedis(t))
	h := l.Middleware(httpx.Rule{Name: "t.login", Limit: 4, Window: 5 * time.Second, Sensitive: true}, headerKey("X-Id"))(okHandler(&calls))

	first := rlDo(h, "a")
	require.Equal(t, http.StatusNoContent, first.Code)
	require.Equal(t, "2", first.Header().Get("X-RateLimit-Limit"), "fallback = separuh Limit")
	require.Equal(t, http.StatusNoContent, rlDo(h, "a").Code)

	rec := rlDo(h, "a")
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "rate_limit_exceeded", errorCode(t, rec))
	require.EqualValues(t, 2, calls.Load())
	require.Equal(t, http.StatusNoContent, rlDo(h, "b").Code, "identitas lain tidak terpengaruh")
}

// Redis mati: endpoint biasa dilewatkan (fail open), tanpa header palsu.
func TestRateLimitNonSensitiveFailsOpenWhenRedisIsDown(t *testing.T) {
	var calls atomic.Int32
	l := newLimiter(t, deadRedis(t))
	h := l.Middleware(httpx.Rule{Name: "t.general", Limit: 1, Window: 5 * time.Second}, headerKey("X-Id"))(okHandler(&calls))
	for i := 0; i < 5; i++ {
		rec := rlDo(h, "a")
		require.Equal(t, http.StatusNoContent, rec.Code)
		require.Empty(t, rec.Header().Get("X-RateLimit-Limit"))
	}
	require.EqualValues(t, 5, calls.Load())
}
