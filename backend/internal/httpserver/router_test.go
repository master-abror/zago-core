package httpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/health"
	"github.com/master-abror/zago-core/backend/internal/httpserver"
	"github.com/master-abror/zago-core/backend/pkg/logger"
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

func TestUnknownRouteAndWrongMethodReturnErrorEnvelope(t *testing.T) {
	h, _ := newRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/tidak-ada", nil)
	req.Header.Set("X-Request-ID", "trace-abcdef09")
	rec := serve(h, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "resource_not_found", body.Error.Code)
	require.NotEmpty(t, body.Error.Message)
	require.Equal(t, "trace-abcdef09", body.Error.RequestID)

	rec = serve(h, httptest.NewRequest(http.MethodPost, "/health", nil))
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Contains(t, rec.Body.String(), `"method_not_allowed"`)
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
