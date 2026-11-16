package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/httpserver"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

type discardAudit struct{}

func (discardAudit) Record(context.Context, modulesdk.ActivityEntry) {}

func newTestAuthzAPI(t *testing.T) *httpserver.AuthzAPI {
	t.Helper()
	var buf bytes.Buffer
	log := jsonLogger(&buf)
	s, err := newAuthzStack(nil, nil, log)
	require.NoError(t, err)

	api, err := s.httpAPI(nil, discardAudit{}, httpx.NewResponder(log), log)

	require.NoError(t, err)
	require.NotNil(t, api)
	return api
}

// httpAPI hanya merakit objek (tanpa I/O): pool nil di sini tidak pernah dipakai.
func TestAuthzStackHTTPAPIWiresTheEngineIntoScopeAndAuthorize(t *testing.T) {
	api := newTestAuthzAPI(t)

	require.NotNil(t, api.Middleware)
	// Authorize panik saat dipasang bila Deps.Authorizer tidak terisi: tidak panik berarti engine
	// stack ini sudah dipasang ke middleware.
	require.NotPanics(t, func() { _ = api.Middleware.Authorize("role.read") })
}

func TestAuthzStackHTTPAPIRefusesAnUnassembledStack(t *testing.T) {
	var buf bytes.Buffer
	log := jsonLogger(&buf)

	api, err := authzStack{}.httpAPI(nil, discardAudit{}, httpx.NewResponder(log), log)

	require.Error(t, err, "stack tanpa authorizer tidak boleh menghasilkan middleware")
	require.Nil(t, api)
}

func TestAuthzStackHTTPAPIRefusesAMissingAuditRecorder(t *testing.T) {
	var buf bytes.Buffer
	log := jsonLogger(&buf)
	s, err := newAuthzStack(nil, nil, log)
	require.NoError(t, err)

	api, err := s.httpAPI(nil, nil, httpx.NewResponder(log), log)

	require.Error(t, err, "penulis role, assignment, dan policy mencatat audit di transaksinya")
	require.Nil(t, api)
}

func TestAuthzStackTxInvalidatorIsANilInterfaceWithoutRedis(t *testing.T) {
	var buf bytes.Buffer

	s, err := newAuthzStack(nil, nil, jsonLogger(&buf))

	require.NoError(t, err)
	require.True(t, s.txInvalidator == nil, "tanpa Redis: interface nil, bukan pointer nil terbungkus (penulis menerimanya nil)")
}

func TestAuthzStackTxInvalidatorIsTheCacheWithRedis(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = rdb.Close() })
	var buf bytes.Buffer

	s, err := newAuthzStack(nil, rdb, jsonLogger(&buf))

	require.NoError(t, err)
	require.False(t, s.txInvalidator == nil)
}

// authzRouteTable memasang rute hasil httpAPI dengan Authorize pengganti yang menjawab aksinya
// sendiri tanpa memanggil handler; setiap rute yang terpasang dicatat sebagai "METHOD pola aksi".
// Rute yang lolos tanpa melewati Authorize akan menjalankan handler (pool nil) dan membuat tes gagal.
func authzRouteTable(t *testing.T, routes []httpserver.AuthzMounter) []string {
	t.Helper()
	r := chi.NewRouter()
	httpserver.MountAuthzRoutes(r, routes, func(action string) func(http.Handler) http.Handler {
		return func(http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-Authz-Action", action)
				w.WriteHeader(http.StatusNoContent)
			})
		}
	})

	var table []string
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		path := strings.ReplaceAll(route, "{id}", id.NewID().String())
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		require.Equal(t, http.StatusNoContent, rec.Code, "%s %s harus melewati Authorize sebelum handler", method, route)
		table = append(table, method+" "+route+" "+rec.Header().Get("X-Authz-Action"))
		return nil
	})
	require.NoError(t, err)
	sort.Strings(table)
	return table
}

// Tabel aksi per rute yang sudah dikonfirmasi pemilik (handover partial-15, keputusan yang sudah
// dikonfirmasi). Daftar ini adalah SELURUH rute otorisasi yang boleh terpasang pada tahap ini:
// rute tambahan atau aksi yang berbeda membuat tes ini gagal dan harus disengaja.
func TestAuthzHTTPAPIMountsExactlyTheConfirmedRoleRoutesWithTheirActions(t *testing.T) {
	api := newTestAuthzAPI(t)

	got := authzRouteTable(t, api.Routes)

	require.Equal(t, []string{
		"DELETE /api/v1/roles/{id} role.manage",
		"GET /api/v1/permissions permission.read",
		"GET /api/v1/roles role.read",
		"GET /api/v1/roles/{id} role.read",
		"GET /api/v1/roles/{id}/permissions role.read",
		"PATCH /api/v1/roles/{id} role.manage",
		"POST /api/v1/roles role.manage",
		"PUT /api/v1/roles/{id}/permissions permission.assign",
	}, got)
}
