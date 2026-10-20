package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/telemetry"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

func scrapeBody(t *testing.T, reg *telemetry.Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

// Jalur dead-letter asli (subscriber yang gagal terus) belum bisa dipicu dari composition root
// di M02; hook-nya diuji langsung: hook log dipanggil DAN counter dead-letter naik.
func TestOnDeadHookLogsAndCountsDeadLetters(t *testing.T) {
	reg := telemetry.NewRegistry(nil)
	wm, err := reg.NewWorker()
	require.NoError(t, err)

	var logged []error
	hook := onDeadHook(func(_ context.Context, _ modulesdk.Event, lastErr error) { logged = append(logged, lastErr) }, wm)

	hook(context.Background(), modulesdk.Event{}, errors.New("handler gagal 1"))
	hook(context.Background(), modulesdk.Event{}, errors.New("handler gagal 2"))

	require.Len(t, logged, 2, "hook log tetap dipanggil")
	require.Contains(t, scrapeBody(t, reg), "outbox_events_dead_total 2\n")
}

func TestOnDeadHookWithoutMetricsOnlyLogs(t *testing.T) {
	var calls int
	hook := onDeadHook(func(context.Context, modulesdk.Event, error) { calls++ }, nil)

	require.NotPanics(t, func() { hook(context.Background(), modulesdk.Event{}, errors.New("x")) })
	require.Equal(t, 1, calls)
}
