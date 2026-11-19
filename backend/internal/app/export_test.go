package app

import (
	"context"
	"log/slog"
	"testing"
	"time"

	authzapp "github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
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

// AssembleSimulatorForTest merakit authzStack seperti produksi (tanpa Redis) lalu Simulator-nya
// lewat newSimulator, dan mengembalikan Authorizer stack itu supaya tes dapat membandingkan
// jawaban simulasi dengan Authorizer.Can langsung. Hanya untuk tes paket app_test (tes ber-DB
// tidak boleh berada di paket internal: siklus impor lewat testkit).
func AssembleSimulatorForTest(db kernel.DBTX, log *slog.Logger, audit modulesdk.AuditRecorder) (*authzapp.Simulator, *authzapp.Authorizer, error) {
	s, err := newAuthzStack(db, nil, log)
	if err != nil {
		return nil, nil, err
	}
	sim, err := s.newSimulator(db, audit)
	if err != nil {
		return nil, nil, err
	}
	return sim, s.authorizer, nil
}
