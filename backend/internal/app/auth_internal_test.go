package app

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/config"
)

// newAuthStack hanya merakit objek (tanpa I/O): klien Redis dan pool di sini tidak pernah dipakai.
func TestNewAuthStackAssemblesFromConfigAndRejectsBadInputs(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = rdb.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	good := config.Config{
		CookieName: "platform_session", CookieSecure: true,
		SessionSecret:      strings.Repeat("s", 40),
		AllowedOrigins:     []string{"https://app.example.com"},
		SessionIdleTimeout: 30 * time.Minute, SessionAbsoluteTimeout: 12 * time.Hour,
		PasswordArgon2MemoryKiB: 64, PasswordArgon2Time: 1, PasswordArgon2Parallelism: 1, PasswordHashConcurrency: 1,
	}

	good.LoginMaxFailures = 5
	good.LoginFailureWindow = 15 * time.Minute
	good.LoginLockDuration = 15 * time.Minute
	good.LoginLockMax = time.Hour
	good.LoginIPMaxFailures = 30

	stack, err := newAuthStack(&good, nil, rdb, nil, rs, log)
	require.NoError(t, err)
	require.NotNil(t, stack.mw)
	require.NotNil(t, stack.api)

	badCookie := good
	badCookie.CookieName = "__Host-x"
	_, err = newAuthStack(&badCookie, nil, rdb, nil, rs, log)
	require.ErrorContains(t, err, "cookie sesi")

	shortSecret := good
	shortSecret.SessionSecret = "pendek"
	_, err = newAuthStack(&shortSecret, nil, rdb, nil, rs, log)
	require.ErrorContains(t, err, "csrf")

	badParams := good
	badParams.PasswordArgon2MemoryKiB = 0
	_, err = newAuthStack(&badParams, nil, rdb, nil, rs, log)
	require.ErrorContains(t, err, "hasher password")

	badGuard := good
	badGuard.LoginLockMax = badGuard.LoginLockDuration - time.Second
	_, err = newAuthStack(&badGuard, nil, rdb, nil, rs, log)
	require.ErrorContains(t, err, "penjaga login", "kebijakan penjaga tak sah (atau tak diisi) menggagalkan perakitan")
}
