package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

type createReq struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func jsonReq(body, contentType string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	return r
}

func fieldErrors(t *testing.T, err error) []httpx.FieldError {
	t.Helper()
	require.True(t, httpx.HasCode(err, httpx.ValidationFailed), "%v", err)
	var ae *httpx.Error
	require.ErrorAs(t, err, &ae)
	f, ok := ae.Details["fields"].([]httpx.FieldError)
	require.True(t, ok)
	return f
}

func TestDecodeJSONValid(t *testing.T) {
	for _, ct := range []string{"application/json", "application/json; charset=utf-8"} {
		var got createReq
		err := httpx.DecodeJSON(httptest.NewRecorder(), jsonReq(`{"name":"a","count":3}`, ct), &got, 0)
		require.NoError(t, err, ct)
		require.Equal(t, createReq{Name: "a", Count: 3}, got)
	}
}

func TestDecodeJSONRejectsWrongContentType(t *testing.T) {
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "application/jsonx"} {
		var got createReq
		err := httpx.DecodeJSON(httptest.NewRecorder(), jsonReq(`{}`, ct), &got, 0)
		require.True(t, httpx.HasCode(err, httpx.UnsupportedMediaType), "ct=%q err=%v", ct, err)
	}
}

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	var got createReq
	err := httpx.DecodeJSON(httptest.NewRecorder(), jsonReq(`{"name":"a","is_admin":true}`, "application/json"), &got, 0)
	f := fieldErrors(t, err)
	require.Equal(t, "is_admin", f[0].Field)
	require.Equal(t, "unknown_field", f[0].Code)
}

func TestDecodeJSONValidationCases(t *testing.T) {
	cases := []struct {
		name, body, code, field string
	}{
		{"kosong", ``, "required", ""},
		{"rusak", `{"name":`, "malformed_json", ""},
		{"sintaks salah", `{name: 1}`, "malformed_json", ""},
		{"tipe salah", `{"count":"tiga"}`, "invalid_type", "count"},
		{"dua nilai", `{"name":"a"} {"name":"b"}`, "multiple_values", ""},
		{"sampah di belakang", `{"name":"a"} xyz`, "malformed_json", ""},
	}
	for _, c := range cases {
		var got createReq
		err := httpx.DecodeJSON(httptest.NewRecorder(), jsonReq(c.body, "application/json"), &got, 0)
		f := fieldErrors(t, err)
		require.Equal(t, c.code, f[0].Code, c.name)
		require.Equal(t, c.field, f[0].Field, c.name)
	}
}

func TestDecodeJSONEnforcesBodyLimit(t *testing.T) {
	var got createReq
	big := `{"name":"` + strings.Repeat("a", 200) + `"}`
	err := httpx.DecodeJSON(httptest.NewRecorder(), jsonReq(big, "application/json"), &got, 64)
	require.True(t, httpx.HasCode(err, httpx.PayloadTooLarge), "%v", err)

	// Di bawah batas lolos.
	err = httpx.DecodeJSON(httptest.NewRecorder(), jsonReq(`{"name":"a"}`, "application/json"), &got, 64)
	require.NoError(t, err)
}
