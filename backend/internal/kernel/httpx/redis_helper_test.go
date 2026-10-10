package httpx_test

import (
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/master-abror/zago-core/backend/internal/testkit"
)

// testRedis mengembalikan klien ke Redis 8 asli yang sudah dikosongkan (tanpa mock).
func testRedis(t *testing.T) redis.UniversalClient {
	t.Helper()
	return testkit.Redis(t).Client(t)
}

// deadRedis mengembalikan klien ke alamat yang menolak koneksi: mensimulasikan Redis mati.
func deadRedis(t *testing.T) redis.UniversalClient {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = c.Close() })
	return c
}
