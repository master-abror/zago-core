package systemlog_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/systemlog"
)

// deadlineDB adalah kernel.DBTX palsu (BUKAN database: hanya mengamati context INSERT). Bila block
// true, Exec menunggu sampai context habis lalu mengembalikan ctx.Err() — meniru database yang macet.
type deadlineDB struct {
	block     bool
	remaining chan time.Duration
}

func newDeadlineDB(block bool) *deadlineDB {
	return &deadlineDB{block: block, remaining: make(chan time.Duration, 1)}
}

func (d *deadlineDB) Exec(ctx context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	if dl, ok := ctx.Deadline(); ok {
		d.remaining <- time.Until(dl)
	} else {
		d.remaining <- -1
	}
	if d.block {
		<-ctx.Done()
		return pgconn.CommandTag{}, ctx.Err()
	}
	return pgconn.CommandTag{}, nil
}

func (d *deadlineDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	panic("tidak dipakai Writer.Log")
}

func (d *deadlineDB) QueryRow(context.Context, string, ...any) pgx.Row {
	panic("tidak dipakai Writer.Log")
}

func TestWriteTimeoutDefaultsToTwoSeconds(t *testing.T) {
	db := newDeadlineDB(false)
	w := systemlog.New(db, nil, "api", "test")

	w.Log(ctx, systemlog.LevelError, "any.event", "pesan", nil)

	got := <-db.remaining
	require.Greater(t, got, time.Second, "sisa batas harus mendekati 2 detik")
	require.LessOrEqual(t, got, 2*time.Second)
}

func TestWithWriteTimeoutOverridesDefault(t *testing.T) {
	db := newDeadlineDB(false)
	w := systemlog.New(db, nil, "api", "test", systemlog.WithWriteTimeout(30*time.Second))

	w.Log(ctx, systemlog.LevelError, "any.event", "pesan", nil)

	got := <-db.remaining
	require.Greater(t, got, 10*time.Second)
	require.LessOrEqual(t, got, 30*time.Second)
}

func TestWithWriteTimeoutIgnoresNonPositiveValue(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		db := newDeadlineDB(false)
		w := systemlog.New(db, nil, "api", "test", systemlog.WithWriteTimeout(d))

		w.Log(ctx, systemlog.LevelError, "any.event", "pesan", nil)

		got := <-db.remaining
		require.Greater(t, got, time.Second, "nilai %v diabaikan, bawaan 2 detik tetap berlaku", d)
		require.LessOrEqual(t, got, 2*time.Second)
	}
}

func TestLogReturnsAfterWriteTimeoutWhenDatabaseStalls(t *testing.T) {
	db := newDeadlineDB(true)
	var out bytes.Buffer
	w := systemlog.New(db, slog.New(slog.NewTextHandler(&out, nil)), "api", "test",
		systemlog.WithWriteTimeout(50*time.Millisecond))

	start := time.Now()
	require.NotPanics(t, func() { w.Log(ctx, systemlog.LevelError, "db.stalled", "database macet", nil) })

	require.Less(t, time.Since(start), 5*time.Second, "Log tidak boleh menggantung melewati batas tulis")
	require.Contains(t, out.String(), "system log gagal ditulis")
	require.Contains(t, out.String(), "context deadline exceeded")
}
