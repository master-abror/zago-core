package httpserver_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"platform/backend/internal/httpserver"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func startServer(t *testing.T, h http.Handler, shutdownTimeout time.Duration) (addr string, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ctx, cancelFn := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	srv := httpserver.NewServer(ln.Addr().String(), h, quiet, shutdownTimeout)
	go func() { errCh <- srv.Serve(ctx, ln) }()
	t.Cleanup(cancelFn)
	return ln.Addr().String(), cancelFn, errCh
}

var noKeepAlive = &http.Client{
	Timeout:   5 * time.Second,
	Transport: &http.Transport{DisableKeepAlives: true},
}

func TestGracefulShutdownFinishesInFlightRequest(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		_, _ = w.Write([]byte("selesai"))
	})
	addr, cancel, done := startServer(t, h, 5*time.Second)

	type result struct {
		body string
		code int
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		resp, err := noKeepAlive.Get("http://" + addr + "/lambat")
		if err != nil {
			resCh <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		resCh <- result{body: string(b), code: resp.StatusCode}
	}()

	<-entered // request sedang diproses
	cancel()  // SIGTERM

	select {
	case err := <-done:
		t.Fatalf("server berhenti padahal masih ada request in-flight (err=%v)", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)

	res := <-resCh
	require.NoError(t, res.err)
	require.Equal(t, http.StatusOK, res.code)
	require.Equal(t, "selesai", res.body)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Serve tidak kembali setelah request in-flight selesai")
	}
}

func TestShutdownStopsAcceptingNewConnections(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release
	})
	addr, cancel, done := startServer(t, h, 5*time.Second)

	go func() { _, _ = noKeepAlive.Get("http://" + addr + "/") }()
	<-entered
	cancel()

	require.Eventually(t, func() bool {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return false
		}
		return true
	}, 2*time.Second, 20*time.Millisecond, "koneksi baru harus ditolak setelah shutdown dimulai")

	close(release)
	require.NoError(t, <-done)
}

func TestShutdownTimeoutForcesCloseAndReportsError(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(entered)
		<-release // tidak pernah selesai dalam batas waktu
	})
	addr, cancel, done := startServer(t, h, 150*time.Millisecond)
	t.Cleanup(func() { close(release) })

	go func() { _, _ = noKeepAlive.Get("http://" + addr + "/") }()
	<-entered

	start := time.Now()
	cancel()

	select {
	case err := <-done:
		require.Error(t, err)
		require.Less(t, time.Since(start), 3*time.Second)
	case <-time.After(5 * time.Second):
		t.Fatal("Serve menggantung melewati shutdown timeout")
	}
}

func TestServeReturnsNilWhenIdleAndCancelled(t *testing.T) {
	_, cancel, done := startServer(t, http.NotFoundHandler(), time.Second)
	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Serve tidak kembali")
	}
}
