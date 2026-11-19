package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// retryPolicy mengatur retry terbatas untuk langkah start yang bergantung pada dependensi
// sementara tidak tersedia (ADR-0022 butir 46; docs/10 §11 dan §15: kegagalan database tidak
// boleh langsung mematikan proses). Nilai nol bukan kebijakan yang berguna; pakai
// defaultSyncRetry atau isi eksplisit.
type retryPolicy struct {
	// Attempts adalah jumlah percobaan TOTAL (percobaan pertama termasuk); nilai di bawah 1
	// diperlakukan sebagai 1 (tanpa retry).
	Attempts int

	// BaseDelay adalah jeda sesudah kegagalan pertama; tiap jeda berikutnya digandakan.
	BaseDelay time.Duration

	// MaxDelay membatasi jeda (0 = tanpa batas; hanya aman untuk Attempts kecil).
	MaxDelay time.Duration

	// AttemptTimeout membatasi SATU percobaan (0 = tanpa batas). Tanpa ini, koneksi yang
	// menggantung menahan start tanpa batas dan retry tidak pernah sempat jalan.
	AttemptTimeout time.Duration

	// wait menggantikan tidur sungguhan di tes; nil = sleepContext.
	wait func(ctx context.Context, d time.Duration) error
}

// delay memberi jeda sesudah kegagalan ke-failed (dimulai dari 1): BaseDelay, 2x, 4x, ... sampai
// MaxDelay.
func (p retryPolicy) delay(failed int) time.Duration {
	d := p.BaseDelay
	for i := 1; i < failed; i++ {
		if p.MaxDelay > 0 && d >= p.MaxDelay {
			break
		}
		d *= 2
	}
	if p.MaxDelay > 0 && d > p.MaxDelay {
		d = p.MaxDelay
	}
	return d
}

// runAttempt menjalankan satu percobaan, dibatasi AttemptTimeout bila diset.
func (p retryPolicy) runAttempt(ctx context.Context, op func(context.Context) error) error {
	if p.AttemptTimeout <= 0 {
		return op(ctx)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, p.AttemptTimeout)
	defer cancel()
	return op(attemptCtx)
}

// sleepContext tidur selama d, atau kembali lebih awal dengan galat konteks bila ctx batal.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// afterAttempts memberi keterangan jumlah percobaan pada galat akhir; satu percobaan tidak diberi
// keterangan (tidak ada yang diulang).
func afterAttempts(err error, n int) error {
	if n <= 1 {
		return err
	}
	return fmt.Errorf("setelah %d percobaan: %w", n, err)
}

// retryStartup menjalankan op sampai berhasil, sampai percobaan habis, atau sampai galat yang
// tidak patut diulang. Semuanya gagal-tertutup: galat akhir selalu dikembalikan, tidak pernah
// ditelan. Bila konteks induk batal (mis. SIGTERM saat start), berhenti seketika tanpa retry, dan
// op tidak dijalankan bila konteks sudah batal sebelum percobaan. Bila retryable(err) false
// (kegagalan deterministik tidak membaik dengan menunggu), berhenti seketika. Bila percobaan
// habis, galat percobaan terakhir dikembalikan dengan keterangan jumlah percobaan.
//
// Setiap kegagalan yang akan diulang dicatat pada level warn (what, nomor percobaan, jeda, galat);
// galat akhir dicatat oleh pemanggil.
func retryStartup(
	ctx context.Context,
	p retryPolicy,
	log *slog.Logger,
	what string,
	retryable func(error) bool,
	op func(context.Context) error,
) error {
	if log == nil {
		log = slog.Default()
	}
	attempts := p.Attempts
	if attempts < 1 {
		attempts = 1
	}
	wait := p.wait
	if wait == nil {
		wait = sleepContext
	}

	var last error
	for n := 1; ; n++ {
		if cerr := ctx.Err(); cerr != nil {
			if last == nil {
				return fmt.Errorf("%s: dibatalkan sebelum percobaan %d: %w", what, n, cerr)
			}
			return fmt.Errorf("%s: dibatalkan sebelum percobaan %d: %w (galat terakhir: %w)", what, n, cerr, last)
		}

		err := p.runAttempt(ctx, op)
		if err == nil {
			if n > 1 {
				log.Info(what+" berhasil setelah percobaan ulang", "attempts", n)
			}
			return nil
		}
		// Konteks induk batal (bukan sekadar batas waktu satu percobaan): jangan diulang.
		if ctx.Err() != nil || !retryable(err) || n >= attempts {
			return afterAttempts(err, n)
		}

		d := p.delay(n)
		log.Warn(what+" gagal; akan dicoba lagi",
			"attempt", n, "max_attempts", attempts, "retry_in", d.String(), "error", err)
		if werr := wait(ctx, d); werr != nil {
			return fmt.Errorf("%w (percobaan ulang dibatalkan: %w)", afterAttempts(err, n), werr)
		}
		last = err
	}
}
