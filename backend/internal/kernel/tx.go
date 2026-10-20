package kernel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// rollbackTimeout membatasi ROLLBACK yang dijalankan setelah context request dibatalkan.
const rollbackTimeout = 5 * time.Second

// ErrTxRollbackOnly dikembalikan WithinTx terluar ketika sesuatu di dalam transaksi menandainya
// gagal (FailTx, atau WithinTx bersarang yang mengembalikan error) tetapi fn terluar tetap
// mengembalikan nil. Transaksi di-ROLLBACK: pekerjaan separuh tidak pernah ter-commit.
var ErrTxRollbackOnly = errors.New("kernel: transaksi ditandai rollback-only")

type ctxKey int

// Seluruh kunci context kernel didefinisikan di satu tempat (docs/10 §10). Tambahkan di AKHIR.
const (
	ctxKeyTx ctxKey = iota
	ctxKeyActor
	ctxKeyScope
	ctxKeyClient
)

// DBTX adalah himpunan kecil operasi query yang dipenuhi pgxpool.Pool maupun pgx.Tx, sehingga
// kode repository ditulis sekali dan berjalan di dalam maupun di luar transaksi (lihat DBFrom).
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Pool adalah yang dibutuhkan TxManager dari pool; dipenuhi *pgxpool.Pool.
type Pool interface {
	DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// txState adalah keadaan satu transaksi yang dibawa context. pgx.Tx TIDAK aman dipakai
// serentak dari banyak goroutine: jangan teruskan context transaksi ke goroutine lain.
type txState struct {
	tx pgx.Tx

	mu          sync.Mutex
	cause       error
	afterCommit []func()
}

func (s *txState) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cause == nil {
		s.cause = err
	}
}

func (s *txState) failure() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cause
}

func (s *txState) addAfterCommit(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.afterCommit = append(s.afterCommit, fn)
}

func (s *txState) takeAfterCommit() []func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.afterCommit
	s.afterCommit = nil
	return out
}

func stateFrom(ctx context.Context) *txState {
	st, _ := ctx.Value(ctxKeyTx).(*txState)
	return st
}

// DetachTx mengembalikan ctx tanpa transaksi (nilai lain tetap). Dipakai untuk pekerjaan yang
// berjalan SETELAH commit: transaksi pada ctx asal sudah selesai dan tidak boleh dipakai lagi.
func DetachTx(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyTx, (*txState)(nil))
}

// InTx melaporkan apakah ctx membawa transaksi yang sedang terbuka.
func InTx(ctx context.Context) bool { return stateFrom(ctx) != nil }

// TxFromContext mengembalikan transaksi pgx pada ctx, bila ada.
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	if st := stateFrom(ctx); st != nil {
		return st.tx, true
	}
	return nil, false
}

// DBFrom mengembalikan transaksi pada ctx bila ada, selain itu fallback (biasanya pool).
// Repository memakai ini sehingga SQL yang sama berjalan di dalam WithinTx maupun di luarnya.
func DBFrom(ctx context.Context, fallback DBTX) DBTX {
	if st := stateFrom(ctx); st != nil {
		return st.tx
	}
	return fallback
}

// FailTx menandai transaksi pada ctx sebagai rollback-only dan melaporkan apakah ada transaksi.
// Dipakai penulis audit/outbox yang tidak mengembalikan error (kontrak module-sdk): kegagalan
// menulis tidak boleh dibiarkan lolos — operasi bisnis ikut dibatalkan (fail-closed, docs/13 §2.2).
func FailTx(ctx context.Context, err error) bool {
	st := stateFrom(ctx)
	if st == nil {
		return false
	}
	st.fail(err)
	return true
}

// AfterCommit menjadwalkan fn setelah transaksi pada ctx ter-COMMIT; bila di-rollback, fn tidak
// pernah jalan. Tanpa transaksi pada ctx, fn langsung dijalankan. fn tidak boleh gagal bisnis:
// panic di dalamnya ditangkap dan dicatat, tidak membatalkan commit yang sudah terjadi.
func AfterCommit(ctx context.Context, fn func()) {
	if st := stateFrom(ctx); st != nil {
		st.addAfterCommit(fn)
		return
	}
	fn()
}

// TxManager menjalankan fungsi di dalam satu transaksi database (docs/10 §6).
type TxManager struct {
	pool Pool
	log  *slog.Logger
}

// NewTxManager membuat TxManager di atas pool. log opsional (default slog.Default()).
func NewTxManager(pool Pool, log *slog.Logger) *TxManager {
	if log == nil {
		log = slog.Default()
	}
	return &TxManager{pool: pool, log: log}
}

// WithinTx menjalankan fn di dalam satu transaksi (READ COMMITTED). Context yang diteruskan ke
// fn membawa transaksinya: DBFrom, penulis outbox, dan penulis audit memakainya otomatis.
//
//   - fn mengembalikan nil dan tidak ada yang menandai gagal → COMMIT, lalu hook AfterCommit jalan.
//   - fn mengembalikan error, atau panic → ROLLBACK (tetap berjalan walau ctx sudah dibatalkan);
//     error dikembalikan apa adanya, panic diteruskan.
//   - Dipanggil di dalam transaksi lain pada ctx → BERGABUNG tanpa savepoint (satu transaksi
//     logis). Bila fn bersarang mengembalikan error, transaksi ditandai rollback-only: walau kode
//     pemanggil menelan error itu, WithinTx terluar mengembalikan ErrTxRollbackOnly dan ROLLBACK.
func (m *TxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if fn == nil {
		return errors.New("kernel: WithinTx membutuhkan fn")
	}

	if st := stateFrom(ctx); st != nil {
		if err := fn(ctx); err != nil {
			st.fail(err)
			return err
		}
		return nil
	}

	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("mulai transaksi: %w", err)
	}
	st := &txState{tx: tx}

	committed := false
	defer func() {
		if committed {
			return
		}
		rbCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
		defer cancel()
		if rbErr := tx.Rollback(rbCtx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			m.log.ErrorContext(ctx, "rollback transaksi gagal", "error", rbErr)
		}
	}()

	if err = fn(context.WithValue(ctx, ctxKeyTx, st)); err != nil {
		return err
	}
	if cause := st.failure(); cause != nil {
		return fmt.Errorf("%w: %w", ErrTxRollbackOnly, cause)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaksi: %w", err)
	}
	committed = true

	m.runAfterCommit(ctx, st.takeAfterCommit())
	return nil
}

func (m *TxManager) runAfterCommit(ctx context.Context, hooks []func()) {
	for _, hook := range hooks {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					m.log.ErrorContext(ctx, "hook after-commit panik", "panic", fmt.Sprint(rec))
				}
			}()
			hook()
		}()
	}
}
