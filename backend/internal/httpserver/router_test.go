package httpserver_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"platform/backend/internal/health"
	"platform/backend/internal/httpserver"
	"platform/backend/pkg/logger"
)

func newRouter(t *testing.T, checkers ...health.Checker) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return httpserver.NewHandler(httpserver.Options{
		Logger:        logger.New(&buf, slog.LevelDebug),
		ReadyCheckers: checkers,
	}), &buf
}

func TestHealthLiveEndpoints(t *testing.T) {
	h, _ := newRouter(t)
	for _, p := range []string{"/health", "/health/live"} {
		rec := serve(h, httptest.NewRequest(http.MethodGet, p, nil))
		require.Equal(t, http.StatusOK, rec.Code, p)
	}
}

func TestHealthLiveDoesNotTouchDependencies(t *testing.T) {
	called := false
	h, _ := newRouter(t, health.NewChecker("postgres", func(context.Context) error { called = true; return errors.New("down") }))
	rec := serve(h, httptest.NewRequest(http.MethodGet, "/health", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.False(t, called)
}

func TestHealthReadyReflectsDependencies(t *testing.T) {
	up := health.NewChecker("postgres", func(context.Context) error { return nil })
	down := health.NewChecker("postgres", func(context.Context) error { return errors.New("connection refused") })

	h, _ := newRouter(t, up)
	require.Equal(t, http.StatusOK, serve(h, httptest.NewRequest(http.MethodGet, "/health/ready", nil)).Code)

	h, _ = newRouter(t, down)
	require.Equal(t, http.StatusServiceUnavailable, serve(h, httptest.NewRequest(http.MethodGet, "/health/ready", nil)).Code)
}

func TestUnknownRouteAndWrongMethodReturnJSON(t *testing.T) {
	h, _ := newRouter(t)

	rec := serve(h, httptest.NewRequest(http.MethodGet, "/tidak-ada", nil))
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	require.Contains(t, rec.Body.String(), "not_found")

	rec = serve(h, httptest.NewRequest(http.MethodPost, "/health", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Contains(t, rec.Body.String(), "method_not_allowed")
}

func TestEveryResponseCarriesSecurityHeadersAndRequestID(t *testing.T) {
	h, _ := newRouter(t)
	for _, p := range []string{"/health", "/tidak-ada"} {
		rec := serve(h, httptest.NewRequest(http.MethodGet, p, nil))
		hd := rec.Header()
		require.Equal(t, "nosniff", hd.Get("X-Content-Type-Options"), p)
		require.Equal(t, "DENY", hd.Get("X-Frame-Options"), p)
		require.Equal(t, "no-referrer", hd.Get("Referrer-Policy"), p)
		require.Contains(t, hd.Get("Content-Security-Policy"), "default-src 'none'", p)
		require.NotEmpty(t, hd.Get("X-Request-ID"), p)
	}
}

func TestRouterLogsRequestsWithRequestID(t *testing.T) {
	h, buf := newRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/tidak-ada?x=1", nil)
	req.Header.Set("X-Request-ID", "trace-abcdef02")
	serve(h, req)

	lines := logLines(t, buf)
	require.Len(t, lines, 1)
	require.Equal(t, "trace-abcdef02", lines[0]["request_id"])
	require.EqualValues(t, http.StatusNotFound, lines[0]["status"])
}
