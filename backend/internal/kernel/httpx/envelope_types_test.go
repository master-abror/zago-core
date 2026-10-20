package httpx_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// decodeStrict mendekode ke T dan MENOLAK field tak dikenal: bila Responder menambah/mengganti field
// di kawat tetapi tipe dokumen (yang dirujuk anotasi swag) tidak ikut berubah, tes ini gagal.
func decodeStrict[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(rec.Body.String()))
	dec.DisallowUnknownFields()
	var v T
	require.NoError(t, dec.Decode(&v), rec.Body.String())
	return v
}

func TestEnvelopeMatchesSuccessWireShape(t *testing.T) {
	rs, _ := newResponder(t)
	rec := httptest.NewRecorder()

	rs.Created(rec, reqWithID("req-1"), map[string]string{"id": "abc"}, httpx.Meta{"limit": 10})

	got := decodeStrict[httpx.Envelope[map[string]string]](t, rec)
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, "abc", got.Data["id"])
	require.EqualValues(t, 10, got.Meta["limit"])
}

func TestEnvelopeOmitsEmptyMeta(t *testing.T) {
	rs, _ := newResponder(t)
	rec := httptest.NewRecorder()

	rs.OK(rec, reqWithID("req-1"), "pong", nil)

	require.NotContains(t, rec.Body.String(), `"meta"`)
	require.Equal(t, "pong", decodeStrict[httpx.Envelope[string]](t, rec).Data)
}

func TestErrorEnvelopeMatchesErrorWireShape(t *testing.T) {
	rs, _ := newResponder(t)
	rec := httptest.NewRecorder()

	rs.Error(rec, reqWithID("req-42"), httpx.Validation(httpx.FieldError{Field: "message", Code: "required"}))

	got := decodeStrict[httpx.ErrorEnvelope](t, rec)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "validation_failed", got.Error.Code)
	require.Equal(t, "req-42", got.Error.RequestID)
	require.NotEmpty(t, got.Error.Message)
	require.Contains(t, got.Error.Details, "fields")
}
