package app_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/app"
	"github.com/master-abror/zago-core/backend/internal/kernel"
)

// failingPool adalah kernel.Pool yang menolak semua operasi: sinkron otorisasi harus gagal pada
// Begin, dan RunAPI tidak boleh sampai ke tahap berikutnya.
type failingPool struct{ err error }

func (p failingPool) Begin(context.Context) (pgx.Tx, error) { return nil, p.err }

func (p failingPool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, p.err
}

func (p failingPool) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, p.err }

func (p failingPool) QueryRow(context.Context, string, ...any) pgx.Row { return errRow{err: p.err} }

type errRow struct{ err error }

func (r errRow) Scan(...any) error { return r.err }

// poolDeps membungkus fakeDeps dan mengisi Resource.Pool pada resource PostgreSQL.
type poolDeps struct {
	*fakeDeps
	pool kernel.Pool
}

func (d poolDeps) Postgres(ctx context.Context, spec app.PostgresSpec) (app.Resource, error) {
	r, err := d.fakeDeps.Postgres(ctx, spec)
	r.Pool = d.pool
	return r, err
}

func TestAPIExitsFailedBeforeBindWhenAuthorizationSyncFails(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()

	base := &fakeDeps{}
	deps := poolDeps{fakeDeps: base, pool: failingPool{err: errors.New("postgres tidak siap")}}
	var stderr bytes.Buffer

	code := app.RunAPI(context.Background(), devEnv(), deps, &stderr, ln)

	require.Equal(t, app.ExitFailed, code)
	require.Contains(t, stderr.String(), "gagal menyinkronkan katalog otorisasi")
	require.NotContains(t, stderr.String(), "SECRET-DB-PW", "URL database tidak boleh bocor ke log")
	opened, closed := base.snapshot()
	require.NotEmpty(t, opened)
	require.ElementsMatch(t, opened, closed, "semua dependensi yang dibuka harus ditutup")

	c, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	require.NoError(t, err, "listener harus masih terbuka: proses keluar sebelum bind/serve")
	_ = c.Close()
}
