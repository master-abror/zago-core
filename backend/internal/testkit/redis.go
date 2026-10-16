package testkit

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/master-abror/zago-core/backend/internal/testpg"
)

const redisImage = "redis:8"

var (
	redisOnce sync.Once
	redisSrv  *RedisServer
	redisErr  error
	redisStop func()
)

// RedisServer adalah Redis 8 bersama untuk satu paket tes.
type RedisServer struct {
	addr string
}

// Redis menyalakan Redis bersama pada pemanggilan pertama (satu container per paket tes).
// Tanpa Docker tes di-skip, KECUALI REQUIRE_DOCKER=1 — maka gagal (tak ada "hijau palsu").
// Tes yang memakai Redis TIDAK boleh memakai t.Parallel: Client mengosongkan seluruh data.
func Redis(t *testing.T) *RedisServer {
	t.Helper()
	redisOnce.Do(func() { redisSrv, redisErr = startRedis(t) })
	require.NoError(t, redisErr)
	if redisSrv == nil {
		// sync.Once menganggap selesai walau tes pertama di-skip (Docker tak sehat): skip juga di sini.
		t.Skip("Redis tes tidak tersedia (Docker tidak sehat)")
	}
	return redisSrv
}

// Addr mengembalikan host:port server.
func (r *RedisServer) Addr() string { return r.addr }

// URL mengembalikan redis://host:port.
func (r *RedisServer) URL() string { return "redis://" + r.addr }

// Client membuat klien baru dan mengosongkan seluruh data (FLUSHALL) supaya tiap tes mulai bersih.
// Klien ditutup otomatis saat tes selesai.
func (r *RedisServer) Client(t *testing.T) *redis.Client {
	t.Helper()
	c := redis.NewClient(&redis.Options{Addr: r.addr})
	t.Cleanup(func() { _ = c.Close() })
	require.NoError(t, c.FlushAll(context.Background()).Err(), "gagal mengosongkan Redis tes")
	return c
}

func startRedis(t *testing.T) (*RedisServer, error) {
	t.Helper()
	testpg.ConfigureReaper()
	if os.Getenv("REQUIRE_DOCKER") != "1" {
		testcontainers.SkipIfProviderIsNotHealthy(t)
	}
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        redisImage,
			ExposedPorts: []string{"6379/tcp"},
			// Tunggu berbasis log (seperti PostgreSQL), bukan polling port lewat Docker API yang
			// gagal acak saat Docker Desktop kewalahan; kesiapan jalur host:port diverifikasi
			// setelahnya oleh waitRedisReady (ADR-0017).
			WaitingFor: wait.ForLog("Ready to accept connections").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		return nil, fmt.Errorf("gagal menyalakan %s (Docker berjalan?): %w", redisImage, err)
	}
	host, err := c.Host(ctx)
	if err != nil {
		_ = testcontainers.TerminateContainer(c)
		return nil, err
	}
	port, err := c.MappedPort(ctx, "6379/tcp")
	if err != nil {
		_ = testcontainers.TerminateContainer(c)
		return nil, err
	}
	addr := net.JoinHostPort(host, port.Port())
	if err := waitRedisReady(ctx, addr, redisReadyTimeout); err != nil {
		_ = testcontainers.TerminateContainer(c)
		return nil, fmt.Errorf("gagal menyalakan %s: %w", redisImage, err)
	}
	redisStop = func() { _ = testcontainers.TerminateContainer(c) }
	return &RedisServer{addr: addr}, nil
}

func terminateRedis() {
	if redisStop != nil {
		redisStop()
	}
}
