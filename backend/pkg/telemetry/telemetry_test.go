package telemetry_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/telemetry"
)

func newRegistry(t *testing.T) (*telemetry.Registry, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	return telemetry.NewRegistry(slog.New(slog.NewTextHandler(&logs, nil))), &logs
}

// scrape memanggil handler /metrics persis seperti Prometheus (GET, teks).
func scrape(t *testing.T, r *telemetry.Registry) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	r.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	return rec.Code, rec.Body.String()
}

// requireSample memastikan SATU baris sampel persis (nama{label} nilai) ada di keluaran.
func requireSample(t *testing.T, body, sample string) {
	t.Helper()
	require.Contains(t, "\n"+body, "\n"+sample+"\n", "sampel tidak ditemukan; keluaran:\n%s", body)
}

func TestHandlerExposesRuntimeMetrics(t *testing.T) {
	r, _ := newRegistry(t)
	code, body := scrape(t, r)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, body, "go_goroutines")
}

func TestHTTPMetricsUseRoutePatternMethodAndStatus(t *testing.T) {
	r, _ := newRegistry(t)
	h, err := r.NewHTTP()
	require.NoError(t, err)

	h.ObserveRequest("/api/v1/items/{id}", http.MethodGet, 200, 50*time.Millisecond)
	h.ObserveRequest("/api/v1/items/{id}", http.MethodGet, 200, 10*time.Millisecond)
	h.ObserveRequest("/api/v1/items/{id}", http.MethodDelete, 404, time.Millisecond)

	_, body := scrape(t, r)
	requireSample(t, body, `http_requests_total{method="GET",route="/api/v1/items/{id}",status="200"} 2`)
	requireSample(t, body, `http_requests_total{method="DELETE",route="/api/v1/items/{id}",status="404"} 1`)
	requireSample(t, body, `http_request_duration_seconds_count{method="GET",route="/api/v1/items/{id}",status="200"} 2`)
	require.Contains(t, body, "http_request_duration_seconds_bucket{")
}

// Kardinalitas label wajib terbatas (docs/14 §5): metode HTTP buatan klien tidak boleh membuat
// deret baru, dan request tanpa pola rute (404/405) jatuh ke satu label.
func TestHTTPMetricsKeepLabelCardinalityBounded(t *testing.T) {
	r, _ := newRegistry(t)
	h, err := r.NewHTTP()
	require.NoError(t, err)

	for _, m := range []string{"FOO", "BAR", "ATTACK-1", "ATTACK-2"} {
		h.ObserveRequest("/health", m, 200, time.Millisecond)
	}
	h.ObserveRequest("", http.MethodGet, 404, time.Millisecond)
	h.ObserveRequest("", http.MethodGet, 404, time.Millisecond)

	_, body := scrape(t, r)
	requireSample(t, body, `http_requests_total{method="OTHER",route="/health",status="200"} 4`)
	requireSample(t, body, `http_requests_total{method="GET",route="unmatched",status="404"} 2`)
	require.NotContains(t, body, "ATTACK")
	require.NotContains(t, body, `method="FOO"`)
}

func TestHTTPMetricsRejectDuplicateRegistration(t *testing.T) {
	r, _ := newRegistry(t)
	_, err := r.NewHTTP()
	require.NoError(t, err)
	_, err = r.NewHTTP()
	require.Error(t, err)
}

func TestWorkerMetrics(t *testing.T) {
	r, _ := newRegistry(t)
	w, err := r.NewWorker()
	require.NoError(t, err)

	w.ObserveJob("email.send", 20*time.Millisecond, nil)
	w.ObserveJob("email.send", 30*time.Millisecond, errors.New("smtp down"))
	w.OutboxDead()
	w.OutboxDead()

	_, body := scrape(t, r)
	requireSample(t, body, `worker_job_duration_seconds_count{job_type="email.send"} 2`)
	requireSample(t, body, `worker_job_failures_total{job_type="email.send"} 1`)
	requireSample(t, body, `outbox_events_dead_total 2`)
}

// Metrik worker hanya ada di proses worker: registry yang tak memanggil NewWorker (API) tidak
// boleh menampilkan counter dead-letter bernilai 0 yang menyesatkan.
func TestAPIRegistryHasNoWorkerMetrics(t *testing.T) {
	r, _ := newRegistry(t)
	_, err := r.NewHTTP()
	require.NoError(t, err)

	_, body := scrape(t, r)
	require.NotContains(t, body, "outbox_events_dead_total")
	require.NotContains(t, body, "worker_job_")
}

func TestPoolMetricsReadFreshSnapshotPerScrape(t *testing.T) {
	r, _ := newRegistry(t)
	acquired := int32(3)
	require.NoError(t, r.RegisterPool("postgres", func() telemetry.PoolStat {
		return telemetry.PoolStat{Acquired: acquired, Idle: 2, Max: 10}
	}))
	require.NoError(t, r.RegisterPool("postgres_maintenance", func() telemetry.PoolStat {
		return telemetry.PoolStat{Acquired: 1, Idle: 0, Max: 2}
	}))

	_, body := scrape(t, r)
	requireSample(t, body, `db_pool_acquired_connections{pool="postgres"} 3`)
	requireSample(t, body, `db_pool_idle_connections{pool="postgres"} 2`)
	requireSample(t, body, `db_pool_max_connections{pool="postgres"} 10`)
	requireSample(t, body, `db_pool_acquired_connections{pool="postgres_maintenance"} 1`)

	acquired = 7
	_, body = scrape(t, r)
	requireSample(t, body, `db_pool_acquired_connections{pool="postgres"} 7`)
}

func TestRegisterPoolRejectsDuplicateName(t *testing.T) {
	r, _ := newRegistry(t)
	stat := func() telemetry.PoolStat { return telemetry.PoolStat{} }
	require.NoError(t, r.RegisterPool("postgres", stat))
	require.Error(t, r.RegisterPool("postgres", stat))
}

func TestOutboxGaugesReflectPendingAndOldestAge(t *testing.T) {
	r, _ := newRegistry(t)
	var deadline time.Time
	stats := telemetry.OutboxStats{Pending: 4, OldestPendingAge: 90 * time.Second}
	require.NoError(t, r.RegisterOutbox(func(ctx context.Context) (telemetry.OutboxStats, error) {
		deadline, _ = ctx.Deadline()
		return stats, nil
	}))

	_, body := scrape(t, r)
	requireSample(t, body, `outbox_events_pending 4`)
	requireSample(t, body, `outbox_oldest_pending_age_seconds 90`)
	require.False(t, deadline.IsZero(), "pembacaan outbox saat scrape harus punya batas waktu")
	require.WithinDuration(t, time.Now().Add(2*time.Second), deadline, time.Second)

	stats = telemetry.OutboxStats{}
	_, body = scrape(t, r)
	requireSample(t, body, `outbox_events_pending 0`)
	requireSample(t, body, `outbox_oldest_pending_age_seconds 0`)
}

// Database mati tidak boleh menjatuhkan scrape: metrik lain tetap keluar, gauge outbox hilang
// (alert memakai absent()), dan penyebabnya dilog tanpa membocorkan apa pun ke klien.
func TestOutboxReadFailureDoesNotFailScrape(t *testing.T) {
	r, logs := newRegistry(t)
	require.NoError(t, r.RegisterOutbox(func(context.Context) (telemetry.OutboxStats, error) {
		return telemetry.OutboxStats{}, errors.New("koneksi database putus")
	}))

	code, body := scrape(t, r)
	require.Equal(t, http.StatusOK, code)
	require.Contains(t, body, "go_goroutines")
	require.False(t, strings.Contains(body, "outbox_events_pending"), "gauge tak boleh muncul saat gagal dibaca")
	require.NotContains(t, body, "koneksi database putus", "detail error tidak bocor ke klien")
	require.Contains(t, logs.String(), "koneksi database putus")
}

func TestRegisterOutboxRejectsSecondReader(t *testing.T) {
	r, _ := newRegistry(t)
	read := func(context.Context) (telemetry.OutboxStats, error) { return telemetry.OutboxStats{}, nil }
	require.NoError(t, r.RegisterOutbox(read))
	require.Error(t, r.RegisterOutbox(read))
}
