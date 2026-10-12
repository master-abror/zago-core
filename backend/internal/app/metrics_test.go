package app_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/app"
)

// freePort meminta port kosong ke OS lalu melepasnya. Ada jeda sempit sebelum dipakai ulang; untuk
// tes ini dapat diterima karena tak ada proses lain yang berebut port ephemeral secepat itu.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	return port
}

func getBody(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := testClient.Get(url)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, string(b)
}

func metricsURL(port int) string { return fmt.Sprintf("http://127.0.0.1:%d/metrics", port) }

func requireMetricsUp(t *testing.T, port int) string {
	t.Helper()
	var body string
	require.Eventually(t, func() bool {
		resp, err := testClient.Get(metricsURL(port))
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		body = string(b)
		return resp.StatusCode == http.StatusOK
	}, 3*time.Second, 20*time.Millisecond)
	return body
}

func requirePortClosed(t *testing.T, port int) {
	t.Helper()
	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 200*time.Millisecond)
		if err != nil {
			return true
		}
		_ = c.Close()
		return false
	}, 3*time.Second, 20*time.Millisecond, "port metrics harus tertutup setelah proses berhenti")
}

// ---------- API ----------

func TestAPIServesMetricsOnSeparatePortOnly(t *testing.T) {
	port := freePort(t)
	env := devEnv()
	env["METRICS_PORT"] = strconv.Itoa(port)
	base, stop := startAPIEnv(t, &fakeDeps{}, env)

	require.Equal(t, http.StatusOK, status(t, base+"/health"))
	body := requireMetricsUp(t, port)
	require.Contains(t, body, "go_goroutines")
	require.Contains(t, body, `http_requests_total{method="GET",route="/health",status="200"} `,
		"request ke port API tercatat per pola rute")

	// /metrics TIDAK ada di port API, dan port metrics hanya melayani /metrics.
	require.Equal(t, http.StatusNotFound, status(t, base+"/metrics"))
	require.Equal(t, http.StatusNotFound, status(t, fmt.Sprintf("http://127.0.0.1:%d/health", port)))

	// Fake tanpa pool PostgreSQL: tak ada metrik pool/outbox yang bisa dibaca.
	require.NotContains(t, body, "db_pool_")
	require.NotContains(t, body, "outbox_events_pending")
	// Metrik worker tidak ada di API.
	require.NotContains(t, body, "outbox_events_dead_total")

	require.Equal(t, app.ExitOK, stop())
	requirePortClosed(t, port)
}

func TestAPIWithoutMetricsPortDoesNotExposeMetrics(t *testing.T) {
	base, stop := startAPI(t, &fakeDeps{})
	defer stop()
	require.Equal(t, http.StatusNotFound, status(t, base+"/metrics"))
}

func TestAPIMetricsPortInUseExitsWithErrorAndClosesResources(t *testing.T) {
	busy, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	defer func() { _ = busy.Close() }()
	main, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = main.Close() }()

	env := devEnv()
	env["METRICS_PORT"] = strconv.Itoa(busy.Addr().(*net.TCPAddr).Port)
	deps := &fakeDeps{}
	var stderr bytes.Buffer

	code := app.RunAPI(context.Background(), env, deps, &stderr, main)

	require.Equal(t, app.ExitFailed, code)
	require.Contains(t, stderr.String(), "metrics")
	_, closed := deps.snapshot()
	require.Equal(t, []string{"redis", "postgres"}, closed)
}

// ---------- Worker ----------

func startWorkerEnv(t *testing.T, deps *fakeDeps, env map[string]string) (stop func() int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	codeCh := make(chan int, 1)
	var stderr bytes.Buffer
	go func() { codeCh <- app.RunWorker(ctx, env, deps, &stderr) }()
	t.Cleanup(cancel)
	require.Eventually(t, func() bool { o, _ := deps.snapshot(); return len(o) == 3 }, 2*time.Second, 10*time.Millisecond)
	return func() int {
		cancel()
		select {
		case c := <-codeCh:
			return c
		case <-time.After(5 * time.Second):
			t.Fatal("RunWorker tidak berhenti setelah ctx dibatalkan")
			return -1
		}
	}
}

func TestWorkerServesMetricsOnWorkerMetricsPort(t *testing.T) {
	port := freePort(t)
	env := devEnv()
	env["WORKER_METRICS_PORT"] = strconv.Itoa(port)
	stop := startWorkerEnv(t, &fakeDeps{}, env)

	body := requireMetricsUp(t, port)
	require.Contains(t, body, "go_goroutines")
	require.Contains(t, body, "outbox_events_dead_total 0\n", "counter dead-letter milik worker")
	require.NotContains(t, body, "http_requests_total")

	require.Equal(t, app.ExitOK, stop())
	requirePortClosed(t, port)
}

// METRICS_PORT milik API: worker yang membaca .env yang sama tidak boleh ikut membuka port itu.
func TestWorkerIgnoresAPIMetricsPort(t *testing.T) {
	port := freePort(t)
	env := devEnv()
	env["METRICS_PORT"] = strconv.Itoa(port)
	stop := startWorkerEnv(t, &fakeDeps{}, env)

	time.Sleep(150 * time.Millisecond)
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 200*time.Millisecond)
	if err == nil {
		_ = c.Close()
		t.Fatal("worker tidak boleh membuka METRICS_PORT milik API")
	}
	require.Equal(t, app.ExitOK, stop())
}

func TestWorkerMetricsPortInUseExitsWithErrorAndClosesResources(t *testing.T) {
	busy, err := net.Listen("tcp", ":0")
	require.NoError(t, err)
	defer func() { _ = busy.Close() }()

	env := devEnv()
	env["WORKER_METRICS_PORT"] = strconv.Itoa(busy.Addr().(*net.TCPAddr).Port)
	deps := &fakeDeps{}
	var stderr bytes.Buffer

	code := app.RunWorker(context.Background(), env, deps, &stderr)

	require.Equal(t, app.ExitFailed, code)
	require.True(t, strings.Contains(stderr.String(), "metrics"))
	_, closed := deps.snapshot()
	require.Equal(t, []string{"redis", "postgres_maintenance", "postgres"}, closed)
}
