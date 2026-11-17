package app

import (
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authsvc "github.com/master-abror/zago-core/backend/internal/auth/service"
	authzapp "github.com/master-abror/zago-core/backend/internal/authorization/application"
)

// Pencabutan sesi saat assignment platform dicabut (T5d5, ADR-0022 butir 76): perakitan wajib
// memasang pencabut sesi, dan SessionService memenuhi antarmuka milik pemakainya langsung.

// fakeSessionRevoker memenuhi authzapp.SessionRevoker tanpa I/O.
type fakeSessionRevoker struct{}

func (fakeSessionRevoker) RevokeAll(context.Context, uuid.UUID, string) (int, error) { return 0, nil }

var _ authzapp.SessionRevoker = (*authsvc.SessionService)(nil)

func TestSessionRevokeReasonMatchesTheAuthServiceReason(t *testing.T) {
	require.Equal(t, authsvc.ReasonRevokeAll, authzapp.SessionRevokeReason)
}

func TestHTTPAPIRefusesAStackWithoutASessionRevoker(t *testing.T) {
	var buf bytes.Buffer
	log := jsonLogger(&buf)
	s, err := newAuthzStack(nil, nil, log)
	require.NoError(t, err)

	api, err := s.httpAPI(nil, discardAudit{}, newTestKernel(t, log), log)

	require.Error(t, err, "tanpa pencabut sesi, sesi platform tetap hidup setelah assignment dicabut")
	require.Nil(t, api)
}

func TestWithSessionRevokerReturnsACopyAndKeepsTheOriginalUnchanged(t *testing.T) {
	var buf bytes.Buffer
	s, err := newAuthzStack(nil, nil, jsonLogger(&buf))
	require.NoError(t, err)

	wired := s.withSessionRevoker(fakeSessionRevoker{})

	require.True(t, s.sessions == nil, "stack asal tidak berubah")
	require.False(t, wired.sessions == nil)
	require.Same(t, s.authorizer, wired.authorizer)
}
