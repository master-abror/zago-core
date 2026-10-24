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

// newAuthMiddleware hanya merakit objek (tanpa I/O): klien Redis di sini tidak pernah dihubungi.
func TestNewAuthMiddlewareAssemblesFromConfigAndRejectsBadInputs(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = rdb.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	rs := httpx.NewResponder(log)
	good := config.Config{
		CookieName: "platform_session", CookieSecure: true,
		SessionSecret:      strings.Repeat("s", 40),
		AllowedOrigins:     []string{"https://app.example.com"},
		SessionIdleTimeout: 30 * time.Minute, SessionAbsoluteTimeout: 12 * time.Hour,
	}

	mw, err := newAuthMiddleware(&good, nil, rdb, rs, log)
	require.NoError(t, err)
	require.NotNil(t, mw)

	badCookie := good
	badCookie.CookieName = "__Host-x"
	_, err = newAuthMiddleware(&badCookie, nil, rdb, rs, log)
	require.ErrorContains(t, err, "cookie sesi")

	shortSecret := good
	shortSecret.SessionSecret = "pendek"
	_, err = newAuthMiddleware(&shortSecret, nil, rdb, rs, log)
	require.ErrorContains(t, err, "csrf")
}
