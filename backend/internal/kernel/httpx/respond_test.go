package httpx_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/logger"
)

func newResponder(t *testing.T) (*httpx.Responder, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	return httpx.NewResponder(logger.New(&buf, slog.LevelDebug)), &buf
}

func reqWithID(id string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	return r.WithContext(logger.WithRequestID(r.Context(), id))
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &m), rec.Body.String())
	return m
}

func logRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &m), l)
		out = append(out, m)
	}
	return out
}

func TestSuccessEnvelope(t *testing.T) {
	rs, _ := newResponder(t)

	rec := httptest.NewRecorder()
	rs.OK(rec, reqWithID("req-1"), map[string]any{"id": "abc"}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	body := decodeBody(t, rec)
	require.Equal(t, map[string]any{"id": "abc"}, body["data"])
	_, hasMeta := body["meta"]
	require.False(t, hasMeta, "meta kosong tidak ditulis")

	rec = httptest.NewRecorder()
	rs.Created(rec, reqWithID("req-1"), []int{1}, httpx.Meta{"pagination": map[string]any{"has_more": false}})
	require.Equal(t, http.StatusCreated, rec.Code)
	body = decodeBody(t, rec)
	require.Contains(t, body, "meta")
}

func TestEmptyCollectionIsArrayNotNull(t *testing.T) {
	rs, _ := newResponder(t)
	var none []string
	rec := httptest.NewRecorder()
	rs.OK(rec, reqWithID("req-1"), httpx.Items(none), nil)
	require.JSONEq(t, `{"data":[]}`, rec.Body.String())
}

func TestNoContent(t *testing.T) {
	rs, _ := newResponder(t)
	rec := httptest.NewRecorder()
	rs.NoContent(rec)
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Empty(t, rec.Body.String())
}

func TestUnserializableDataBecomes500(t *testing.T) {
	rs, _ := newResponder(t)
	rec := httptest.NewRecorder()
	rs.OK(rec, reqWithID("req-1"), make(chan int), nil)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "internal_error")
}

func TestAppErrorEnvelope(t *testing.T) {
	rs, buf := newResponder(t)
	rec := httptest.NewRecorder()
	err := fmt.Errorf("handler: %w", httpx.PermissionDenied.New().WithDetails(map[string]any{"permission": "invoice.approve"}))
	rs.Error(rec, reqWithID("req-42"), err)

	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	e := decodeBody(t, rec)["error"].(map[string]any)
	require.Equal(t, "permission_denied", e["code"])
	require.Equal(t, httpx.PermissionDenied.Message, e["message"])
	require.Equal(t, "req-42", e["request_id"])
	require.Equal(t, map[string]any{"permission": "invoice.approve"}, e["details"])
	require.Empty(t, strings.TrimSpace(buf.String()), "error 4xx tidak ditulis ke log oleh Responder")
}

func TestUnknownErrorNeverLeaksOriginalText(t *testing.T) {
	rs, buf := newResponder(t)
	rec := httptest.NewRecorder()
	rs.Error(rec, reqWithID("req-9"), errors.New(`pq: password authentication failed for user "platform_app" password=hunter2`))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "hunter2")
	require.NotContains(t, rec.Body.String(), "platform_app")
	e := decodeBody(t, rec)["error"].(map[string]any)
	require.Equal(t, "internal_error", e["code"])
	require.Equal(t, httpx.InternalError.Message, e["message"])
	require.Equal(t, "req-9", e["request_id"])
	_, hasDetails := e["details"]
	require.False(t, hasDetails)

	recs := logRecords(t, buf)
	require.Len(t, recs, 1)
	require.Equal(t, "ERROR", recs[0]["level"])
	require.Contains(t, recs[0]["error"], "hunter2", "detail lengkap hanya di log")
	require.Equal(t, "req-9", recs[0]["request_id"])
}

func TestInternalErrorIgnoresMessageAndDetails(t *testing.T) {
	rs, _ := newResponder(t)
	rec := httptest.NewRecorder()
	rs.Error(rec, reqWithID("req-1"), httpx.InternalError.New().WithMessage("sql: bocor").WithDetails(map[string]any{"q": "SELECT"}))
	require.NotContains(t, rec.Body.String(), "bocor")
	require.NotContains(t, rec.Body.String(), "SELECT")
}

func TestServerErrorLoggingRules(t *testing.T) {
	rs, buf := newResponder(t)

	// 5xx tanpa cause (mis. dari Recoverer): tidak ada yang perlu dicatat lagi.
	rs.Error(httptest.NewRecorder(), reqWithID("req-1"), httpx.InternalError.New())
	require.Empty(t, strings.TrimSpace(buf.String()))

	// 5xx dengan cause: dicatat.
	rs.Error(httptest.NewRecorder(), reqWithID("req-2"), httpx.ServiceUnavailable.Wrap(errors.New("redis: connection refused")))
	recs := logRecords(t, buf)
	require.Len(t, recs, 1)
	require.Contains(t, recs[0]["error"], "connection refused")
}

func TestMaxBytesErrorMapsTo413(t *testing.T) {
	rs, _ := newResponder(t)
	rec := httptest.NewRecorder()
	rs.Error(rec, reqWithID("req-1"), fmt.Errorf("read: %w", &http.MaxBytesError{Limit: 10}))
	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	require.Contains(t, rec.Body.String(), "payload_too_large")
}

func TestRetryAfterHeaderRoundsUp(t *testing.T) {
	rs, _ := newResponder(t)
	rec := httptest.NewRecorder()
	rs.Error(rec, reqWithID("req-1"), httpx.RateLimitExceeded.New().WithRetryAfter(1500*time.Millisecond))
	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, "2", rec.Header().Get("Retry-After"))
}

func TestErrorWithoutRequestIDOmitsField(t *testing.T) {
	rs, _ := newResponder(t)
	rec := httptest.NewRecorder()
	rs.Error(rec, httptest.NewRequest(http.MethodGet, "/", nil), httpx.ResourceNotFound.New())
	e := decodeBody(t, rec)["error"].(map[string]any)
	_, has := e["request_id"]
	require.False(t, has)
}
