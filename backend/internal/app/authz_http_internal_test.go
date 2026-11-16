package app

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// httpAPI hanya merakit objek (tanpa I/O): db nil di sini tidak pernah dipakai.
func TestAuthzStackHTTPAPIWiresTheEngineIntoScopeAndAuthorize(t *testing.T) {
	var buf bytes.Buffer
	log := jsonLogger(&buf)
	s, err := newAuthzStack(nil, nil, log)
	require.NoError(t, err)

	api, err := s.httpAPI(nil, nil, httpx.NewResponder(log), log)

	require.NoError(t, err)
	require.NotNil(t, api)
	require.NotNil(t, api.Middleware)
	// Authorize panik saat dipasang bila Deps.Authorizer tidak terisi: tidak panik berarti engine
	// stack ini sudah dipasang ke middleware.
	require.NotPanics(t, func() { _ = api.Middleware.Authorize("role.read") })
	require.Empty(t, api.Routes, "T5d1 hanya merakit middleware; rute ditambahkan task berikutnya")
}

func TestAuthzStackHTTPAPIRefusesAnUnassembledStack(t *testing.T) {
	var buf bytes.Buffer
	log := jsonLogger(&buf)

	api, err := authzStack{}.httpAPI(nil, nil, httpx.NewResponder(log), log)

	require.Error(t, err, "stack tanpa authorizer tidak boleh menghasilkan middleware yang menolak atau meloloskan secara diam-diam")
	require.Nil(t, api)
}
