package kernel_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

var errBoom = errors.New("boom")

// probeDB menyiapkan tabel sementara yang HANYA ada di database tes ini; semua tulis/baca
// memakai role runtime app_user, persis seperti produksi.
func probeDB(t *testing.T) (*testpg.DB, *kernel.TxManager) {
	t.Helper()
	db := testkit.NewDB(t)
	_, err := db.Migrator.Exec(ctx, `CREATE TABLE kernel_tx_probe (id uuid PRIMARY KEY, note text NOT NULL)`)
	require.NoError(t, err)
	_, err = db.Migrator.Exec(ctx, `GRANT SELECT, INSERT, UPDATE, DELETE ON kernel_tx_probe TO app_user`)
	require.NoError(t, err)
	return db, kernel.NewTxManager(db.App, nil)
}

// insertProbe adalah "repository": SQL yang sama berjalan di dalam maupun di luar transaksi.
func insertProbe(ctx context.Context, pool kernel.DBTX, note string) error {
	_, err := kernel.DBFrom(ctx, pool).Exec(ctx,
		`INSERT INTO kernel_tx_probe (id, note) VALUES ($1, $2)`, id.NewID().String(), note)
	return err
}

func countProbe(t *testing.T, db *testpg.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.App.QueryRow(ctx, `SELECT count(*) FROM kernel_tx_probe`).Scan(&n))
	return n
}

func TestWithinTxCommitsAndHidesWorkUntilCommit(t *testing.T) {
	db, tm := probeDB(t)

	err := tm.WithinTx(ctx, func(ctx context.Context) error {
		require.True(t, kernel.InTx(ctx))
		require.NoError(t, insertProbe(ctx, db.App, "a"))
		require.Zero(t, countProbe(t, db), "baris belum terlihat dari koneksi lain sebelum commit")
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, countProbe(t, db))
}

func TestWithinTxRollsBackOnError(t *testing.T) {
	db, tm := probeDB(t)

	err := tm.WithinTx(ctx, func(ctx context.Context) error {
		require.NoError(t, insertProbe(ctx, db.App, "a"))
		return errBoom
	})
	require.ErrorIs(t, err, errBoom)
	require.Zero(t, countProbe(t, db))
}

func TestWithinTxRollsBackOnPanicAndRepanics(t *testing.T) {
	db, tm := probeDB(t)

	require.PanicsWithValue(t, "meledak", func() {
		_ = tm.WithinTx(ctx, func(ctx context.Context) error {
			require.NoError(t, insertProbe(ctx, db.App, "a"))
			panic("meledak")
		})
	})
	require.Zero(t, countProbe(t, db))
	require.NoError(t, db.App.Ping(ctx), "koneksi tidak boleh bocor/rusak setelah panic")
}

func TestWithinTxRollsBackEvenWhenContextIsCanceled(t *testing.T) {
	db, tm := probeDB(t)

	cctx, cancel := context.WithCancel(ctx)
	err := tm.WithinTx(cctx, func(ctx context.Context) error {
		require.NoError(t, insertProbe(ctx, db.App, "a"))
		cancel()
		return ctx.Err()
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, countProbe(t, db))
	require.NoError(t, db.App.Ping(ctx))
}

func TestWithinTxRejectsNilFunc(t *testing.T) {
	require.Error(t, kernel.NewTxManager(nil, nil).WithinTx(ctx, nil))
}

func TestWithinTxReportsBeginFailureWithoutRunningFn(t *testing.T) {
	db := testkit.NewDB(t)
	pool, err := kernel.NewPool(ctx, kernel.PoolConfig{
		Name: "app", URL: testkit.PostgresURL(db, testpg.AppRole), MaxConns: 1, StatementTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	pool.Close()

	called := false
	err = kernel.NewTxManager(pool, nil).WithinTx(ctx, func(context.Context) error {
		called = true
		return nil
	})
	require.ErrorContains(t, err, "mulai transaksi")
	require.False(t, called)
}

func TestNestedWithinTxJoinsOuterTransaction(t *testing.T) {
	db, tm := probeDB(t)

	// Keduanya ter-commit bersama.
	require.NoError(t, tm.WithinTx(ctx, func(ctx context.Context) error {
		require.NoError(t, insertProbe(ctx, db.App, "luar"))
		return tm.WithinTx(ctx, func(ctx context.Context) error {
			return insertProbe(ctx, db.App, "dalam")
		})
	}))
	require.Equal(t, 2, countProbe(t, db))

	// Rollback luar membatalkan pekerjaan bersarang yang sudah "sukses".
	err := tm.WithinTx(ctx, func(ctx context.Context) error {
		require.NoError(t, tm.WithinTx(ctx, func(ctx context.Context) error {
			return insertProbe(ctx, db.App, "dalam")
		}))
		return errBoom
	})
	require.ErrorIs(t, err, errBoom)
	require.Equal(t, 2, countProbe(t, db), "tidak ada baris baru")
}

func TestNestedErrorSwallowedByCallerStillRollsBackEverything(t *testing.T) {
	db, tm := probeDB(t)

	err := tm.WithinTx(ctx, func(ctx context.Context) error {
		require.NoError(t, insertProbe(ctx, db.App, "luar"))
		inner := tm.WithinTx(ctx, func(context.Context) error { return errBoom })
		require.ErrorIs(t, inner, errBoom)
		return nil // kode pemanggil menelan error: tidak boleh jadi commit separuh
	})
	require.ErrorIs(t, err, kernel.ErrTxRollbackOnly)
	require.ErrorIs(t, err, errBoom, "penyebab asli tetap terlacak")
	require.Zero(t, countProbe(t, db))
}

func TestFailTxMarksTransactionRollbackOnly(t *testing.T) {
	db, tm := probeDB(t)

	require.False(t, kernel.FailTx(ctx, errBoom), "tanpa transaksi: tidak ada yang ditandai")

	err := tm.WithinTx(ctx, func(ctx context.Context) error {
		require.NoError(t, insertProbe(ctx, db.App, "a"))
		require.True(t, kernel.FailTx(ctx, errBoom))
		return nil
	})
	require.ErrorIs(t, err, kernel.ErrTxRollbackOnly)
	require.ErrorIs(t, err, errBoom)
	require.Zero(t, countProbe(t, db))
}

func TestDBFromPicksTransactionOverFallback(t *testing.T) {
	db, tm := probeDB(t)

	require.False(t, kernel.InTx(ctx))
	_, ok := kernel.TxFromContext(ctx)
	require.False(t, ok)
	require.Same(t, db.App, kernel.DBFrom(ctx, db.App))

	require.NoError(t, tm.WithinTx(ctx, func(ctx context.Context) error {
		tx, ok := kernel.TxFromContext(ctx)
		require.True(t, ok)
		require.Same(t, tx, kernel.DBFrom(ctx, db.App))
		return nil
	}))
}

func TestAfterCommitRunsOnlyAfterCommitInOrder(t *testing.T) {
	db, tm := probeDB(t)

	var order []string
	require.NoError(t, tm.WithinTx(ctx, func(ctx context.Context) error {
		require.NoError(t, insertProbe(ctx, db.App, "a"))
		kernel.AfterCommit(ctx, func() { order = append(order, "satu") })
		kernel.AfterCommit(ctx, func() { order = append(order, "dua") })
		require.Empty(t, order, "belum jalan sebelum commit")
		return nil
	}))
	require.Equal(t, []string{"satu", "dua"}, order)

	// Rollback: hook tidak pernah jalan.
	order = nil
	_ = tm.WithinTx(ctx, func(ctx context.Context) error {
		kernel.AfterCommit(ctx, func() { order = append(order, "tidak-boleh") })
		return errBoom
	})
	require.Empty(t, order)

	// Hook yang dijadwalkan dari WithinTx bersarang ikut transaksi terluar.
	order = nil
	_ = tm.WithinTx(ctx, func(ctx context.Context) error {
		_ = tm.WithinTx(ctx, func(ctx context.Context) error {
			kernel.AfterCommit(ctx, func() { order = append(order, "dalam") })
			return nil
		})
		return errBoom
	})
	require.Empty(t, order)
}

func TestAfterCommitWithoutTransactionRunsImmediately(t *testing.T) {
	ran := false
	kernel.AfterCommit(ctx, func() { ran = true })
	require.True(t, ran)
}

func TestAfterCommitPanicDoesNotFailCommitOrSkipLaterHooks(t *testing.T) {
	db, tm := probeDB(t)

	laterRan := false
	err := tm.WithinTx(ctx, func(ctx context.Context) error {
		require.NoError(t, insertProbe(ctx, db.App, "a"))
		kernel.AfterCommit(ctx, func() { panic("hook rusak") })
		kernel.AfterCommit(ctx, func() { laterRan = true })
		return nil
	})
	require.NoError(t, err)
	require.True(t, laterRan)
	require.Equal(t, 1, countProbe(t, db), "commit sudah terjadi dan tetap berlaku")
}

func TestWithinTxIsSafeUnderConcurrentUse(t *testing.T) {
	db, tm := probeDB(t)

	const workers = 6
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- tm.WithinTx(ctx, func(ctx context.Context) error {
				return insertProbe(ctx, db.App, "paralel")
			})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, workers, countProbe(t, db))
}
