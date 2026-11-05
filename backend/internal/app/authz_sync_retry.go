package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	authzapp "github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/permission"
)

// errSyncSetup menandai kegagalan merakit syncer: itu kesalahan kode (katalog atau definisi role
// sistem tidak sah), bukan dependensi yang sedang tidak tersedia, jadi tidak pernah di-retry.
var errSyncSetup = errors.New("syncer otorisasi tidak dapat dirakit")

// syncRetryable memutuskan apakah kegagalan sinkron katalog layak diulang (ADR-0022 butir 46).
// Yang tidak diulang hanyalah yang terbukti deterministik: kode core yang dimiliki modul
// (permission.ErrCodeConflict) dan kegagalan merakit syncer. Selain itu, termasuk galat yang
// tidak dikenal, diulang: ruginya paling banyak jeda total kebijakan sebelum ExitFailed.
func syncRetryable(err error) bool {
	return !errors.Is(err, permission.ErrCodeConflict) && !errors.Is(err, errSyncSetup)
}

// defaultSyncRetry adalah kebijakan start API: 5 percobaan dengan jeda 1, 2, 4, lalu 8 detik
// (total 15 detik tidur), dan tiap percobaan dibatasi 30 detik. Terburuk: ±165 detik sebelum
// ExitFailed (dependensi menggantung); database yang menolak koneksi: ±15 detik.
func defaultSyncRetry() retryPolicy {
	var p retryPolicy
	p.Attempts = 5
	p.BaseDelay = time.Second
	p.MaxDelay = 8 * time.Second
	p.AttemptTimeout = 30 * time.Second
	return p
}

// syncRetryPolicy adalah kebijakan yang dipakai RunAPI. Variabel (bukan pemanggilan langsung)
// supaya tes RunAPI tidak menunggu jeda sungguhan; hanya export_test.go yang mengubahnya.
var syncRetryPolicy = defaultSyncRetry

// syncAuthorizationWithRetry menjalankan syncAuthorization dengan retry terbatas (T4d). Tiap
// percobaan adalah satu transaksi utuh yang di-rollback bila gagal dan sinkronnya idempoten
// (advisory lock + konvergensi), sehingga mengulang aman. Setelah percobaan habis galat
// dikembalikan dan pemanggil keluar dengan ExitFailed: API tidak pernah melayani request dengan
// katalog atau role yang belum sinkron.
func syncAuthorizationWithRetry(ctx context.Context, db authzapp.Beginner, inv authzapp.Invalidator, log *slog.Logger, p retryPolicy) error {
	return retryStartup(ctx, p, log, "sinkron katalog otorisasi", syncRetryable, func(c context.Context) error {
		return syncAuthorization(c, db, inv, log)
	})
}
