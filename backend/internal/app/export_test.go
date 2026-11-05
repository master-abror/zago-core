package app

import (
	"context"
	"testing"
	"time"
)

// UseInstantSyncRetry mengganti kebijakan retry sinkron katalog di RunAPI dengan attempts
// percobaan tanpa jeda sungguhan, dan memulihkannya saat tes selesai. Hanya untuk tes: jangan
// dipakai bersama t.Parallel (mengubah variabel paket).
func UseInstantSyncRetry(t testing.TB, attempts int) {
	t.Helper()
	prev := syncRetryPolicy
	t.Cleanup(func() { syncRetryPolicy = prev })
	syncRetryPolicy = func() retryPolicy {
		p := defaultSyncRetry()
		p.Attempts = attempts
		p.wait = func(context.Context, time.Duration) error { return nil }
		return p
	}
}
