package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/health"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func ok(name string) health.Checker {
	return health.NewChecker(name, func(context.Context) error { return nil })
}

func failing(name, msg string) health.Checker {
	return health.NewChecker(name, func(context.Context) error { return errors.New(msg) })
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

type body struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) body {
	t.Helper()
	var b body
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	return b
}

func TestLiveAlways200(t *testing.T) {
	rec := get(health.Live(), "/health/live")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ok", decode(t, rec).Status)
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
}

func TestReadyAllHealthy(t *testing.T) {
	rec := get(health.Ready(discard, time.Second, ok("postgres"), ok("redis")), "/health/ready")
	require.Equal(t, http.StatusOK, rec.Code)
	b := decode(t, rec)
	require.Equal(t, "ok", b.Status)
	require.Equal(t, map[string]string{"postgres": "ok", "redis": "ok"}, b.Checks)
}

func TestReadyReturns503WhenADependencyFails(t *testing.T) {
	rec := get(health.Ready(discard, time.Second, failing("postgres", "dial tcp 10.1.2.3:5432: connection refused"), ok("redis")), "/health/ready")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	b := decode(t, rec)
	require.Equal(t, "unavailable", b.Status)
	require.Equal(t, "unavailable", b.Checks["postgres"])
	require.Equal(t, "ok", b.Checks["redis"])
}

func TestReadyBodyLeaksNoErrorDetails(t *testing.T) {
	rec := get(health.Ready(discard, time.Second, failing("postgres", "dial tcp 10.1.2.3:5432: password=hunter2")), "/health/ready")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	for _, leak := range []string{"10.1.2.3", "5432", "hunter2", "dial"} {
		require.NotContains(t, rec.Body.String(), leak)
	}
}

func TestReadyTimesOutSlowCheckerEvenIfItIgnoresContext(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	stuck := health.NewChecker("postgres", func(context.Context) error {
		<-release // sengaja mengabaikan ctx
		return nil
	})

	start := time.Now()
	rec := get(health.Ready(discard, 100*time.Millisecond, stuck, ok("redis")), "/health/ready")
	elapsed := time.Since(start)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Less(t, elapsed, 2*time.Second, "respons tidak boleh menunggu checker yang macet")
	require.Equal(t, "unavailable", decode(t, rec).Checks["postgres"])
}

func TestReadyChecksRunInParallel(t *testing.T) {
	slow := func(name string) health.Checker {
		return health.NewChecker(name, func(ctx context.Context) error {
			select {
			case <-time.After(200 * time.Millisecond):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}
	start := time.Now()
	rec := get(health.Ready(discard, time.Second, slow("a"), slow("b"), slow("c")), "/health/ready")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Less(t, time.Since(start), 500*time.Millisecond, "tiga checker 200ms harus selesai paralel, bukan 600ms")
}

func TestReadyWithNoCheckersIsOK(t *testing.T) {
	rec := get(health.Ready(discard, time.Second), "/health/ready")
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, strings.Contains(rec.Body.String(), `"ok"`))
}
