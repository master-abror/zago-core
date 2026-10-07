package testkit

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
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
	if os.Getenv("REQUIRE_DOCKER") != "1" {
		testcontainers.SkipIfProviderIsNotHealthy(t)
	}
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        redisImage,
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(90 * time.Second),
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
	redisStop = func() { _ = testcontainers.TerminateContainer(c) }
	return &RedisServer{addr: host + ":" + port.Port()}, nil
}

func terminateRedis() {
	if redisStop != nil {
		redisStop()
	}
}
