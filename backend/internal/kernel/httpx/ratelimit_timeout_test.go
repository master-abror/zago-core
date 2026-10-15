package httpx_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// blackholeRedis adalah server TCP yang MENERIMA koneksi tetapi tak pernah membalas: panggilan
// Redis hanya berakhir karena batas waktu context (bukan karena koneksi ditolak).
func blackholeRedis(t *testing.T) redis.UniversalClient {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	// ContextTimeoutEnabled meniru klien produksi (infra.Redis, ADR-0019): tanpanya go-redis mengabaikan
	// deadline context dan setiap panggilan ke server macet memakan 5 detik, apa pun opsi limiter.
	c := redis.NewClient(&redis.Options{
		Addr: ln.Addr().String(), DialTimeout: time.Second, MaxRetries: -1, ContextTimeoutEnabled: true,
	})
	t.Cleanup(func() {
		_ = c.Close()
		_ = ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, cn := range conns {
			_ = cn.Close()
		}
	})
	return c
}

// timeoutProbe mengirim satu request lewat limiter dan mengembalikan lama pemrosesan serta statusnya.
func timeoutProbe(t *testing.T, opts ...httpx.RateLimiterOption) (time.Duration, int) {
	t.Helper()
	rs, _ := newResponder(t)
	l, err := httpx.NewRateLimiter(blackholeRedis(t), rs, nil, opts...)
	require.NoError(t, err)
	h := l.Middleware(httpx.Rule{Name: "t.timeout", Limit: 5, Window: 5 * time.Second},
		func(*http.Request) string { return "a" })(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))

	rec := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return time.Since(start), rec.Code
}

// Batas bawah membuktikan bahwa timeout benar-benar ditunggu; batas atas (3 dtk, jauh di bawah 5 dtk
// bawaan go-redis tanpa ContextTimeoutEnabled) membuktikan bahwa timeout benar-benar dipotong.
// Keduanya longgar supaya mesin yang lambat tidak membuat tes gagal acak.

func TestRateLimiterWithRedisTimeoutBoundsTheRedisCall(t *testing.T) {
	elapsed, code := timeoutProbe(t, httpx.WithRedisTimeout(700*time.Millisecond))

	require.GreaterOrEqual(t, elapsed, 600*time.Millisecond, "batas 700ms harus ditunggu (bawaan 250ms sudah lewat)")
	require.Less(t, elapsed, 3*time.Second, "batas 700ms harus memotong panggilan")
	require.Equal(t, http.StatusNoContent, code, "aturan non-sensitif fail open saat Redis tak menjawab")
}

func TestRateLimiterRedisTimeoutDefaultsToTwoHundredFiftyMilliseconds(t *testing.T) {
	elapsed, code := timeoutProbe(t)

	require.GreaterOrEqual(t, elapsed, 200*time.Millisecond, "bawaan produksi 250ms tetap berlaku tanpa opsi")
	require.Less(t, elapsed, 3*time.Second, "bawaan 250ms harus memotong panggilan")
	require.Equal(t, http.StatusNoContent, code)
}

func TestRateLimiterRedisTimeoutIgnoresNonPositiveValues(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		elapsed, _ := timeoutProbe(t, httpx.WithRedisTimeout(d))
		require.GreaterOrEqual(t, elapsed, 200*time.Millisecond,
			"nilai %v diabaikan: bawaan 250ms tetap berlaku, bukan timeout instan", d)
		require.Less(t, elapsed, 3*time.Second, "nilai %v diabaikan: bawaan 250ms tetap memotong panggilan", d)
	}
}
