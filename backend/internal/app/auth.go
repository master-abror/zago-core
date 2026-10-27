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
	"github.com/master-abror/zago-core/backend/pkg/security"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// authStack adalah hasil perakitan autentikasi HTTP: rantai middleware (Authenticate, CSRF,
// RequireAuth) dan handler endpoint /auth/*.
type authStack struct {
	mw  *authhttp.Middleware
	api *authhttp.Handlers
}

// newAuthStack merakit autentikasi HTTP (docs/05 §3, §8–§9, ADR-0020) dari konfigurasi: cookie
// sesi, kebijakan Origin, CSRF terikat sesi, hasher Argon2id, SessionService (PostgreSQL
// otoritatif, Redis sebagai cache), AuthService, dan handler. Hanya merakit objek: tidak ada I/O.
// audit boleh nil (activity tidak direkam).
func newAuthStack(cfg *config.Config, pool kernel.Pool, rdb redis.UniversalClient, audit modulesdk.AuditRecorder, rs *httpx.Responder, log *slog.Logger) (authStack, error) {
	cookie, err := authhttp.NewCookie(cfg.CookieName, cfg.CookieSecure)
	if err != nil {
		return authStack{}, fmt.Errorf("cookie sesi: %w", err)
	}
	csrf, err := service.NewCSRF(cfg.SessionSecret)
	if err != nil {
		return authStack{}, fmt.Errorf("csrf: %w", err)
	}
	hasher, err := security.NewArgon2idHasher(cfg.Argon2Params(), cfg.PasswordHashConcurrency)
	if err != nil {
		return authStack{}, fmt.Errorf("hasher password: %w", err)
	}

	store := repository.NewStore(pool)
	sessions := service.NewSessionService(
		store, rdb,
		service.StaticTimeouts{Idle: cfg.SessionIdleTimeout, Absolute: cfg.SessionAbsoluteTimeout},
		log,
	)
	authSvc, err := service.NewAuthService(service.AuthDeps{
		Store: store, Tx: kernel.NewTxManager(pool, log), Sessions: sessions, Hasher: hasher,
		Policy: cfg.PasswordPolicy(), Bus: kernel.NewBus(pool, log), Audit: audit, Log: log,
	})
	if err != nil {
		return authStack{}, fmt.Errorf("layanan auth: %w", err)
	}

	mw, err := authhttp.New(authhttp.Deps{
		Auth:      sessions,
		Cookie:    cookie,
		CSRF:      csrf,
		Origins:   authhttp.NewOriginPolicy(cfg.AllowedOrigins),
		Responder: rs,
		Log:       log,
	})
	if err != nil {
		return authStack{}, fmt.Errorf("middleware auth: %w", err)
	}
	cursors, err := httpx.NewCursorCodec(cfg.SessionSecret)
	if err != nil {
		return authStack{}, fmt.Errorf("cursor: %w", err)
	}
	api, err := authhttp.NewHandlers(authhttp.HandlerDeps{Service: authSvc, Cookie: cookie, CSRF: csrf, Cursors: cursors, Responder: rs})
	if err != nil {
		return authStack{}, fmt.Errorf("handler auth: %w", err)
	}
	return authStack{mw: mw, api: api}, nil
}
