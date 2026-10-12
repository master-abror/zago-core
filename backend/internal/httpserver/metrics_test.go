package httpserver_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/httpserver"
	"github.com/master-abror/zago-core/backend/pkg/logger"
	"github.com/master-abror/zago-core/backend/pkg/telemetry"
)

func newMetrics(t *testing.T) (*telemetry.Registry, *telemetry.HTTP) {
	t.Helper()
	reg := telemetry.NewRegistry(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	m, err := reg.NewHTTP()
	require.NoError(t, err)
	return reg, m
}

func scrapeMetrics(t *testing.T, reg *telemetry.Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	return rec.Body.String()
}

func requireMetricLine(t *testing.T, body, line string) {
	t.Helper()
	require.Contains(t, "\n"+body, "\n"+line+"\n", "baris metrik tidak ditemukan; keluaran:\n%s", body)
}

// docs/14 §11: label route memakai POLA rute, bukan path mentah (tanpa ledakan kardinalitas).
func TestMetricsMiddlewareLabelsByRoutePatternNotRawPath(t *testing.T) {
	reg, m := newMetrics(t)
	r := chi.NewRouter()
	r.Use(httpserver.Metrics(m))
	r.Get("/items/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	for _, id := range []string{"1", "2", "3-secret-token"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items/"+id, nil))
	}

	body := scrapeMetrics(t, reg)
	requireMetricLine(t, body, `http_requests_total{method="GET",route="/items/{id}",status="200"} 3`)
	require.NotContains(t, body, "secret-token", "path mentah tidak boleh jadi label")
}

func TestMetricsMiddlewareRecordsHandlerStatusAndImplicit200(t *testing.T) {
	reg, m := newMetrics(t)
	r := chi.NewRouter()
	r.Use(httpserver.Metrics(m))
	r.Get("/teapot", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	r.Get("/implicit", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/teapot", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/implicit", nil))

	body := scrapeMetrics(t, reg)
	requireMetricLine(t, body, `http_requests_total{method="GET",route="/teapot",status="418"} 1`)
	requireMetricLine(t, body, `http_requests_total{method="GET",route="/implicit",status="200"} 1`)
}

// Handler yang panik dipulihkan Recoverer menjadi 500; request itu tetap harus terhitung.
func TestMetricsMiddlewareCountsPanickedRequestAs500(t *testing.T) {
	reg, m := newMetrics(t)
	var logs bytes.Buffer
	r := chi.NewRouter()
	r.Use(httpserver.Metrics(m), httpserver.Recoverer(logger.New(&logs, slog.LevelDebug)))
	r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("kaboom") })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/boom", nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code)

	requireMetricLine(t, scrapeMetrics(t, reg), `http_requests_total{method="GET",route="/boom",status="500"} 1`)
}

func TestNewHandlerWithMetricsGroupsUnknownPathsAsUnmatched(t *testing.T) {
	reg, m := newMetrics(t)
	var logs bytes.Buffer
	h := httpserver.NewHandler(httpserver.Options{Logger: logger.New(&logs, slog.LevelDebug), Metrics: m})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	for _, p := range []string{"/no/such/123", "/another/path/abc"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		require.Equal(t, http.StatusNotFound, rec.Code)
	}

	body := scrapeMetrics(t, reg)
	requireMetricLine(t, body, `http_requests_total{method="GET",route="/health",status="200"} 1`)
	requireMetricLine(t, body, `http_requests_total{method="GET",route="unmatched",status="404"} 2`)
	require.False(t, strings.Contains(body, "/no/such") || strings.Contains(body, "/another"))
}

func TestNewHandlerWithoutMetricsStillWorks(t *testing.T) {
	h, _ := newRouter(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, rec.Code)
}
