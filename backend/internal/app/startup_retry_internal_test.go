package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/permission"
)

var errRetryBoom = errors.New("koneksi putus")

// secs menghindari ekspresi biner di daftar argumen (penjajaran gofmt tidak perlu ditebak).
func secs(n int) time.Duration {
	return time.Duration(n) * time.Second
}

// waitRecorder menggantikan tidur sungguhan: mencatat jeda yang diminta dan tidak menunggu.
type waitRecorder struct {
	delays []time.Duration
	err    error
}

func (w *waitRecorder) wait(_ context.Context, d time.Duration) error {
	w.delays = append(w.delays, d)
	return w.err
}

func retryAlways(error) bool {
	return true
}

func testRetryPolicy(w *waitRecorder) retryPolicy {
	var p retryPolicy
	p.Attempts = 5
	p.BaseDelay = time.Second
	p.MaxDelay = 8 * time.Second
	p.wait = w.wait
	return p
}

func TestRetryStartupSucceedsOnFirstAttemptWithoutNoise(t *testing.T) {
	w := &waitRecorder{}
	var buf bytes.Buffer
	calls := 0

	err := retryStartup(context.Background(), testRetryPolicy(w), jsonLogger(&buf), "sinkron uji", retryAlways, func(context.Context) error {
		calls++
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.Empty(t, w.delays)
	require.Empty(t, buf.String(), "keberhasilan pada percobaan pertama tidak menulis log")
}

func TestRetryStartupSucceedsAfterTransientFailuresWithBackoff(t *testing.T) {
	w := &waitRecorder{}
	var buf bytes.Buffer
	calls := 0

	err := retryStartup(context.Background(), testRetryPolicy(w), jsonLogger(&buf), "sinkron uji", retryAlways, func(context.Context) error {
		calls++
		if calls <= 2 {
			return errRetryBoom
		}
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, 3, calls)
	require.Equal(t, []time.Duration{secs(1), secs(2)}, w.delays)
	out := buf.String()
	require.Equal(t, 2, strings.Count(out, "akan dicoba lagi"))
	require.Contains(t, out, "berhasil setelah percobaan ulang")
	require.Contains(t, out, `"attempts":3`)
}

func TestRetryStartupGivesUpAfterTheAttemptLimitAndKeepsTheCause(t *testing.T) {
	w := &waitRecorder{}
	var buf bytes.Buffer
	calls := 0

	err := retryStartup(context.Background(), testRetryPolicy(w), jsonLogger(&buf), "sinkron uji", retryAlways, func(context.Context) error {
		calls++
		return errRetryBoom
	})

	require.Error(t, err)
	require.ErrorIs(t, err, errRetryBoom, "galat asli harus tetap bisa ditelusuri")
	require.Contains(t, err.Error(), "5 percobaan")
	require.Equal(t, 5, calls)
	require.Equal(t, []time.Duration{secs(1), secs(2), secs(4), secs(8)}, w.delays, "tidak ada jeda sesudah percobaan terakhir")
	require.Equal(t, 4, strings.Count(buf.String(), "akan dicoba lagi"))
	require.NotContains(t, buf.String(), "berhasil setelah")
}

func TestRetryStartupCapsTheDelayAtMaxDelay(t *testing.T) {
	w := &waitRecorder{}
	var buf bytes.Buffer
	p := testRetryPolicy(w)
	p.Attempts = 6
	p.MaxDelay = 3 * time.Second

	err := retryStartup(context.Background(), p, jsonLogger(&buf), "sinkron uji", retryAlways, func(context.Context) error {
		return errRetryBoom
	})

	require.ErrorIs(t, err, errRetryBoom)
	require.Equal(t, []time.Duration{secs(1), secs(2), secs(3), secs(3), secs(3)}, w.delays)
}

func TestRetryStartupTreatsAttemptsBelowOneAsOneAttempt(t *testing.T) {
	w := &waitRecorder{}
	var buf bytes.Buffer
	p := testRetryPolicy(w)
	p.Attempts = 0
	calls := 0

	err := retryStartup(context.Background(), p, jsonLogger(&buf), "sinkron uji", retryAlways, func(context.Context) error {
		calls++
		return errRetryBoom
	})

	require.ErrorIs(t, err, errRetryBoom)
	require.Equal(t, 1, calls)
	require.Empty(t, w.delays)
	require.NotContains(t, err.Error(), "percobaan", "satu percobaan tidak diberi keterangan jumlah")
}

func TestRetryStartupDoesNotRetryNonRetryableErrors(t *testing.T) {
	w := &waitRecorder{}
	var buf bytes.Buffer
	calls := 0
	conflict := fmt.Errorf("sinkron katalog otorisasi: %w", fmt.Errorf("%w: %q", permission.ErrCodeConflict, "x.y.z"))

	err := retryStartup(context.Background(), testRetryPolicy(w), jsonLogger(&buf), "sinkron uji", syncRetryable, func(context.Context) error {
		calls++
		return conflict
	})

	require.ErrorIs(t, err, permission.ErrCodeConflict)
	require.Equal(t, 1, calls, "kegagalan deterministik tidak diulang")
	require.Empty(t, w.delays)
	require.NotContains(t, err.Error(), "percobaan")
}

func TestRetryStartupStopsAtANonRetryableErrorAfterTransientOnes(t *testing.T) {
	w := &waitRecorder{}
	var buf bytes.Buffer
	calls := 0

	err := retryStartup(context.Background(), testRetryPolicy(w), jsonLogger(&buf), "sinkron uji", syncRetryable, func(context.Context) error {
		calls++
		if calls == 1 {
			return errRetryBoom
		}
		return permission.ErrCodeConflict
	})

	require.ErrorIs(t, err, permission.ErrCodeConflict)
	require.Equal(t, 2, calls)
	require.Equal(t, []time.Duration{secs(1)}, w.delays)
	require.Contains(t, err.Error(), "2 percobaan")
}

func TestRetryStartupDoesNotRunWhenTheContextIsAlreadyCancelled(t *testing.T) {
	w := &waitRecorder{}
	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0

	err := retryStartup(ctx, testRetryPolicy(w), jsonLogger(&buf), "sinkron uji", retryAlways, func(context.Context) error {
		calls++
		return nil
	})

	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, calls, "konteks batal: operasi tidak dijalankan")
	require.Empty(t, w.delays)
}

func TestRetryStartupStopsWhenTheContextIsCancelledDuringAnAttempt(t *testing.T) {
	w := &waitRecorder{}
	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0

	err := retryStartup(ctx, testRetryPolicy(w), jsonLogger(&buf), "sinkron uji", retryAlways, func(context.Context) error {
		calls++
		cancel()
		return errRetryBoom
	})

	require.ErrorIs(t, err, errRetryBoom, "galat asli dikembalikan apa adanya")
	require.Equal(t, 1, calls, "pembatalan (shutdown) tidak di-retry")
	require.Empty(t, w.delays)
}

func TestRetryStartupStopsWhenTheWaitIsInterrupted(t *testing.T) {
	w := &waitRecorder{err: context.Canceled}
	var buf bytes.Buffer
	calls := 0

	err := retryStartup(context.Background(), testRetryPolicy(w), jsonLogger(&buf), "sinkron uji", retryAlways, func(context.Context) error {
		calls++
		return errRetryBoom
	})

	require.ErrorIs(t, err, errRetryBoom)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, calls)
	require.Len(t, w.delays, 1)
}

func TestRetryStartupBoundsEachAttemptWithTheAttemptTimeout(t *testing.T) {
	w := &waitRecorder{}
	var buf bytes.Buffer
	p := testRetryPolicy(w)
	p.Attempts = 2
	p.AttemptTimeout = 10 * time.Millisecond
	calls := 0

	err := retryStartup(context.Background(), p, jsonLogger(&buf), "sinkron uji", syncRetryable, func(ctx context.Context) error {
		calls++
		<-ctx.Done()
		return ctx.Err()
	})

	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, 2, calls, "percobaan yang menggantung habis waktunya dan diulang (konteks induk tidak batal)")
	require.Len(t, w.delays, 1)
}

func TestRetryStartupWithoutAttemptTimeoutPassesTheContextThrough(t *testing.T) {
	w := &waitRecorder{}
	var buf bytes.Buffer
	hasDeadline := true

	err := retryStartup(context.Background(), testRetryPolicy(w), jsonLogger(&buf), "sinkron uji", retryAlways, func(ctx context.Context) error {
		_, hasDeadline = ctx.Deadline()
		return nil
	})

	require.NoError(t, err)
	require.False(t, hasDeadline)
}

func TestRetryPolicyDelayDoublesAndIsCapped(t *testing.T) {
	p := testRetryPolicy(&waitRecorder{})
	cases := []struct {
		failed int
		want   time.Duration
	}{
		{failed: 1, want: secs(1)},
		{failed: 2, want: secs(2)},
		{failed: 3, want: secs(4)},
		{failed: 4, want: secs(8)},
		{failed: 5, want: secs(8)},
		{failed: 9, want: secs(8)},
	}
	for _, c := range cases {
		require.Equal(t, c.want, p.delay(c.failed), "setelah kegagalan ke-%d", c.failed)
	}
}

func TestSleepContextReturnsNilAfterTheDelayAndTheContextErrorWhenCancelled(t *testing.T) {
	require.NoError(t, sleepContext(context.Background(), time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, sleepContext(ctx, time.Hour), context.Canceled, "tidak menunggu satu jam")
}

func TestSyncRetryableClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "galat koneksi biasa", err: errRetryBoom, want: true},
		{name: "batas waktu percobaan", err: context.DeadlineExceeded, want: true},
		{name: "konflik kode core", err: fmt.Errorf("sinkron: %w", permission.ErrCodeConflict), want: false},
		{name: "kode katalog tidak sah", err: fmt.Errorf("sinkron: %w", permission.ErrInvalidCode), want: false},
		{name: "deklarasi kode field rusak", err: fmt.Errorf("sinkron: %w", permission.ErrInvalidFieldDeclaration), want: false},
		{name: "syncer tidak dapat dirakit", err: fmt.Errorf("rakit syncer otorisasi: %w: %w", errSyncSetup, errRetryBoom), want: false},
	}
	for _, c := range cases {
		require.Equal(t, c.want, syncRetryable(c.err), c.name)
	}
}

func TestDefaultSyncRetryPinsTheBoundedBackoff(t *testing.T) {
	p := defaultSyncRetry()

	require.Equal(t, 5, p.Attempts)
	var total time.Duration
	for n := 1; n < p.Attempts; n++ {
		total += p.delay(n)
	}
	require.Equal(t, secs(15), total, "jeda 1+2+4+8 detik; tidak ada jeda sesudah percobaan terakhir")
	require.Positive(t, p.AttemptTimeout, "tiap percobaan dibatasi waktu supaya koneksi menggantung tidak menahan start tanpa batas")
	require.Nil(t, p.wait, "produksi memakai tidur sungguhan (sleepContext)")
}

type countingBeginner struct {
	calls int
	err   error
}

func (b *countingBeginner) Begin(context.Context) (pgx.Tx, error) {
	b.calls++
	return nil, b.err
}

func TestSyncAuthorizationWithRetryRetriesTransientDatabaseFailuresThenGivesUp(t *testing.T) {
	w := &waitRecorder{}
	var buf bytes.Buffer
	b := &countingBeginner{err: errRetryBoom}
	p := testRetryPolicy(w)
	p.Attempts = 3

	err := syncAuthorizationWithRetry(context.Background(), b, nil, jsonLogger(&buf), p)

	require.Error(t, err, "database yang tetap gagal = galat (pemanggil: ExitFailed), tidak pernah lolos diam-diam")
	require.ErrorIs(t, err, errRetryBoom)
	require.Contains(t, err.Error(), "sinkron katalog otorisasi")
	require.Contains(t, err.Error(), "3 percobaan")
	require.Equal(t, 3, b.calls)
	require.Equal(t, []time.Duration{secs(1), secs(2)}, w.delays)
}

func TestSyncAuthorizationWithRetryDoesNotTouchTheDatabaseWhenCancelled(t *testing.T) {
	w := &waitRecorder{}
	var buf bytes.Buffer
	b := &countingBeginner{err: errRetryBoom}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := syncAuthorizationWithRetry(ctx, b, nil, jsonLogger(&buf), testRetryPolicy(w))

	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, b.calls)
}
