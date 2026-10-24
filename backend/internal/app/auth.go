package app

import (
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"

	"github.com/master-abror/zago-core/backend/internal/auth/repository"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	authhttp "github.com/master-abror/zago-core/backend/internal/auth/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/config"
)

// newAuthMiddleware merakit rantai autentikasi HTTP (docs/05 §8–§9, ADR-0020) dari konfigurasi:
// cookie sesi, kebijakan Origin, CSRF terikat sesi, dan SessionService (PostgreSQL otoritatif,
// Redis sebagai cache). Hanya merakit objek: tidak ada I/O di sini.
func newAuthMiddleware(cfg *config.Config, db kernel.DBTX, rdb redis.UniversalClient, rs *httpx.Responder, log *slog.Logger) (*authhttp.Middleware, error) {
	cookie, err := authhttp.NewCookie(cfg.CookieName, cfg.CookieSecure)
	if err != nil {
		return nil, fmt.Errorf("cookie sesi: %w", err)
	}
	csrf, err := service.NewCSRF(cfg.SessionSecret)
	if err != nil {
		return nil, fmt.Errorf("csrf: %w", err)
	}
	sessions := service.NewSessionService(
		repository.NewStore(db), rdb,
		service.StaticTimeouts{Idle: cfg.SessionIdleTimeout, Absolute: cfg.SessionAbsoluteTimeout},
		log,
	)
	return authhttp.New(authhttp.Deps{
		Auth:      sessions,
		Cookie:    cookie,
		CSRF:      csrf,
		Origins:   authhttp.NewOriginPolicy(cfg.AllowedOrigins),
		Responder: rs,
		Log:       log,
	})
}
