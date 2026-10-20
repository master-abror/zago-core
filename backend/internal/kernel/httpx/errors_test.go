package httpx_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// Pemetaan HTTP eksplisit dari docs/08 §11 (+ I-08).
func TestKernelCodesMatchRegistry(t *testing.T) {
	want := map[string]int{
		"validation_failed":        http.StatusBadRequest,
		"idempotency_key_conflict": http.StatusUnprocessableEntity,
		"authentication_required":  http.StatusUnauthorized,
		"permission_denied":        http.StatusForbidden,
		"resource_not_found":       http.StatusNotFound,
		"method_not_allowed":       http.StatusMethodNotAllowed, // ADR-0012
		"version_conflict":         http.StatusConflict,
		"idempotency_in_progress":  http.StatusConflict,
		"payload_too_large":        http.StatusRequestEntityTooLarge,
		"unsupported_media_type":   http.StatusUnsupportedMediaType,
		"rate_limit_exceeded":      http.StatusTooManyRequests,
		"internal_error":           http.StatusInternalServerError,
		"service_unavailable":      http.StatusServiceUnavailable,
	}
	for name, status := range want {
		c, ok := httpx.Lookup(name)
		require.True(t, ok, name)
		require.Equal(t, status, c.Status, name)
		require.NotEmpty(t, c.Message, name)
	}
}

func TestRegister(t *testing.T) {
	c := httpx.Register(httpx.Code{Name: "finance.invoice_already_approved", Status: http.StatusConflict, Message: "Invoice already approved."})
	require.Equal(t, "finance.invoice_already_approved", c.Name)

	// Mendaftar ulang persis sama → idempoten.
	require.NotPanics(t, func() { httpx.Register(c) })
	// Nama sama, definisi beda → panic.
	require.Panics(t, func() {
		httpx.Register(httpx.Code{Name: c.Name, Status: http.StatusBadRequest, Message: c.Message})
	})
	// Nama tak valid / status bukan error → panic.
	for _, bad := range []httpx.Code{
		{Name: "", Status: 400}, {Name: "Camel", Status: 400}, {Name: "a.b.c", Status: 400},
		{Name: "ok_name", Status: 200}, {Name: "ok_name2", Status: 600},
	} {
		require.Panics(t, func() { httpx.Register(bad) }, bad.Name)
	}

	found := false
	for _, k := range httpx.Codes() {
		if k.Name == c.Name {
			found = true
		}
	}
	require.True(t, found)
}

func TestErrorWrapAndHelpers(t *testing.T) {
	cause := errors.New("pq: duplicate key")
	e := httpx.VersionConflict.Wrap(cause).WithMessage("stale version")
	require.ErrorIs(t, e, cause)
	require.Contains(t, e.Error(), "version_conflict")
	require.Contains(t, e.Error(), "pq: duplicate key")
	require.Equal(t, "stale version", e.PublicMessage())
	require.Equal(t, httpx.VersionConflict.Message, httpx.VersionConflict.New().PublicMessage())

	wrapped := fmt.Errorf("service: %w", e)
	require.True(t, httpx.HasCode(wrapped, httpx.VersionConflict))
	require.False(t, httpx.HasCode(wrapped, httpx.PermissionDenied))
	require.False(t, httpx.HasCode(errors.New("x"), httpx.VersionConflict))
}

func TestFieldsErr(t *testing.T) {
	var f httpx.Fields
	require.NoError(t, f.Err())
	f.Add("email", "required", "email is required")
	f.Add("name", "too_long", "")
	require.Equal(t, 2, f.Len())

	err := f.Err()
	require.True(t, httpx.HasCode(err, httpx.ValidationFailed))
	var ae *httpx.Error
	require.ErrorAs(t, err, &ae)
	fields, ok := ae.Details["fields"].([]httpx.FieldError)
	require.True(t, ok)
	require.Equal(t, "email", fields[0].Field)
	require.Equal(t, "too_long", fields[1].Code)
}
