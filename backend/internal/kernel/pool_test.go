package kernel_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
)

var ctx = context.Background()

func TestNewPoolAppliesSettingsWithoutConnecting(t *testing.T) {
	// Alamat tak terjangkau: NewPool harus lazy (tidak memblokir start proses).
	pool, err := kernel.NewPool(ctx, kernel.PoolConfig{
		Name: "app", URL: "postgres://u:pw@127.0.0.1:1/db",
		MaxConns: 7, StatementTimeout: 1500 * time.Millisecond,
	})
	require.NoError(t, err)
	defer pool.Close()

	cfg := pool.Config()
	require.EqualValues(t, 7, cfg.MaxConns)
	require.Equal(t, "1500", cfg.ConnConfig.RuntimeParams["statement_timeout"])
	require.Equal(t, "zago-app", cfg.ConnConfig.RuntimeParams["application_name"])
}

func TestNewPoolRejectsBadConfigWithoutLeakingURL(t *testing.T) {
	good := kernel.PoolConfig{Name: "app", URL: "postgres://u:pw@127.0.0.1:1/db", MaxConns: 2, StatementTimeout: time.Second}
	with := func(mut func(*kernel.PoolConfig)) kernel.PoolConfig {
		c := good
		mut(&c)
		return c
	}
	for name, cfg := range map[string]kernel.PoolConfig{
		"url rusak":      with(func(c *kernel.PoolConfig) { c.URL = "postgres://u:TOP-SECRET@%zz/db" }),
		"tanpa nama":     with(func(c *kernel.PoolConfig) { c.Name = "" }),
		"max conns nol":  with(func(c *kernel.PoolConfig) { c.MaxConns = 0 }),
		"min lebih>max":  with(func(c *kernel.PoolConfig) { c.MinConns = 5 }),
		"min negatif":    with(func(c *kernel.PoolConfig) { c.MinConns = -1 }),
		"tanpa timeout":  with(func(c *kernel.PoolConfig) { c.StatementTimeout = 0 }),
		"timeout sub-ms": with(func(c *kernel.PoolConfig) { c.StatementTimeout = time.Microsecond }),
	} {
		pool, err := kernel.NewPool(ctx, cfg)
		require.Error(t, err, name)
		require.Nil(t, pool, name)
		require.NotContains(t, err.Error(), "TOP-SECRET", name)
	}
}

func TestStatementTimeoutIsEnforcedByServer(t *testing.T) {
	db := testkit.NewDB(t)
	pool, err := kernel.NewPool(ctx, kernel.PoolConfig{
		Name: "app", URL: testkit.PostgresURL(db, testpg.AppRole),
		MaxConns: 2, StatementTimeout: 300 * time.Millisecond,
	})
	require.NoError(t, err)
	defer pool.Close()

	var timeout, appName, role string
	require.NoError(t, pool.QueryRow(ctx, "SHOW statement_timeout").Scan(&timeout))
	require.NoError(t, pool.QueryRow(ctx, "SHOW application_name").Scan(&appName))
	require.NoError(t, pool.QueryRow(ctx, "SELECT current_user").Scan(&role))
	require.Equal(t, "300ms", timeout)
	require.Equal(t, "zago-app", appName)
	require.Equal(t, testpg.AppRole, role)

	_, err = pool.Exec(ctx, "SELECT pg_sleep(5)")
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, "57014", pgErr.Code, "query_canceled oleh statement_timeout")
}
