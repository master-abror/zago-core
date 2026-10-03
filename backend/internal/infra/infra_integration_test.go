package infra_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"platform/backend/internal/app"
	"platform/backend/internal/health"
	"platform/backend/internal/httpserver"
	"platform/backend/internal/infra"
	"platform/backend/pkg/logger"
)

// Tes integrasi MEMBUTUHKAN Docker (testcontainers). Tanpa Docker tes di-skip, bukan gagal.
// PostgreSQL 18 / Redis 8 sesungguhnya — tanpa mock database (docs/22 §10.1).

func start(t *testing.T, image, port string, env map[string]string) (testcontainers.Container, string) {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        image,
			ExposedPorts: []string{port + "/tcp"},
			Env:          env,
			WaitingFor:   wait.ForListeningPort(port + "/tcp").WithStartupTimeout(90 * time.Second),
		},
		Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(c) })

	host, err := c.Host(ctx)
	require.NoError(t, err)
	mapped, err := c.MappedPort(ctx, port+"/tcp")
	require.NoError(t, err)
	return c, host + ":" + mapped.Port()
}

func readyStatus(t *testing.T, checkers ...health.Checker) int {
	t.Helper()
	h := httpserver.NewHandler(httpserver.Options{
		Logger:        logger.New(testWriter{t}, slog.LevelDebug),
		ReadyTimeout:  2 * time.Second,
		ReadyCheckers: checkers,
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	return rec.Code
}

func check(res app.Resource) health.Checker { return health.NewChecker(res.Name, res.Check) }

func TestReadyFlipsTo503WhenPostgresStops(t *testing.T) {
	c, addr := start(t, "postgres:18", "5432", map[string]string{
		"POSTGRES_PASSWORD": "postgres", "POSTGRES_DB": "platform",
	})
	ctx := context.Background()

	pg, err := infra.Deps{}.Postgres(ctx, "postgres", "postgres://postgres:postgres@"+addr+"/platform")
	require.NoError(t, err)
	defer pg.Close()

	require.Eventually(t, func() bool { return readyStatus(t, check(pg)) == http.StatusOK },
		30*time.Second, 250*time.Millisecond, "ready harus 200 saat PostgreSQL hidup")

	timeout := 5 * time.Second
	require.NoError(t, c.Stop(ctx, &timeout))

	require.Eventually(t, func() bool { return readyStatus(t, check(pg)) == http.StatusServiceUnavailable },
		30*time.Second, 250*time.Millisecond, "ready harus 503 saat PostgreSQL mati")
}

func TestReadyFlipsTo503WhenRedisStops(t *testing.T) {
	c, addr := start(t, "redis:8", "6379", nil)
	ctx := context.Background()

	rd, err := infra.Deps{}.Redis(ctx, "redis", "redis://"+addr)
	require.NoError(t, err)
	defer rd.Close()

	require.Eventually(t, func() bool { return readyStatus(t, check(rd)) == http.StatusOK },
		30*time.Second, 250*time.Millisecond)

	timeout := 5 * time.Second
	require.NoError(t, c.Stop(ctx, &timeout))

	require.Eventually(t, func() bool { return readyStatus(t, check(rd)) == http.StatusServiceUnavailable },
		30*time.Second, 250*time.Millisecond)
}

func TestInvalidURLsAreRejectedWithoutLeakingCredentials(t *testing.T) {
	_, err := infra.Deps{}.Postgres(context.Background(), "postgres", "postgres://u:TOP-SECRET@%zz/db")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "TOP-SECRET")

	_, err = infra.Deps{}.Redis(context.Background(), "redis", "http://:TOP-SECRET@host")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "TOP-SECRET")
}
