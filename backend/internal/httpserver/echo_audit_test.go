package httpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/audit"
	"github.com/master-abror/zago-core/backend/internal/httpserver"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/logger"
)

// Ujung-ke-ujung dengan PostgreSQL 18 dan Redis 8 sungguhan: perekam audit nyata menulis satu
// activity yang membawa request_id, aktor, IP, dan User-Agent request; kunci idempotensi yang
// sama memutar ulang respons tanpa menulis activity kedua; request_id yang sama muncul di log.
func TestEchoWritesOneActivityWithRequestIDActorAndClientAndLogsSameRequestID(t *testing.T) {
	db := testkit.NewDB(t)
	rdb := testkit.Redis(t).Client(t)
	var logs bytes.Buffer
	log := logger.New(&logs, slog.LevelDebug)
	kern, err := httpserver.NewKernelServices(httpserver.KernelDeps{
		Redis: rdb, Log: log, SessionSecret: testSecret, Audit: audit.New(db.App, log),
	})
	require.NoError(t, err)
	e := echoEnv{h: httpserver.NewHandler(httpserver.Options{Logger: log, Kernel: kern, DevEndpoints: true}), log: &logs}

	opts := append(okHeaders("key-audit-0001"), hdr("X-Request-ID", "req-audit-0001"), hdr("User-Agent", "audit-test/1.0"))
	first := e.post(`{"message":"jejak audit"}`, opts...)
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	replay := e.post(`{"message":"jejak audit"}`, opts...)
	require.Equal(t, "true", replay.Header().Get("Idempotent-Replayed"))

	var (
		n                                              int
		action, resourceType, result, actorType, actor string
		requestID, ip, userAgent, metadata             string
	)
	err = db.App.QueryRow(context.Background(), `SELECT count(*) FROM activities`).Scan(&n)
	require.NoError(t, err)
	require.Equal(t, 1, n, "replay tidak boleh menulis activity kedua")
	err = db.App.QueryRow(context.Background(), `
		SELECT action, COALESCE(resource_type, ''), result, actor_type, COALESCE(actor_user_id::text, ''),
		       COALESCE(request_id, ''), COALESCE(host(ip_address), ''), COALESCE(user_agent, ''), metadata::text
		  FROM activities`).Scan(&action, &resourceType, &result, &actorType, &actor, &requestID, &ip, &userAgent, &metadata)
	require.NoError(t, err)
	require.Equal(t, "kernel.echo", action)
	require.Equal(t, "kernel_echo", resourceType)
	require.Equal(t, "success", result)
	require.Equal(t, "user", actorType)
	require.Equal(t, actorA.String(), actor)
	require.Equal(t, "req-audit-0001", requestID)
	require.Equal(t, "192.0.2.10", ip)
	require.Equal(t, "audit-test/1.0", userAgent)
	require.Contains(t, metadata, `"message_length"`)
	require.NotContains(t, metadata, "jejak audit", "isi pesan tidak masuk audit")

	var sawRequestLog bool
	for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m))
		if m["msg"] == "http request" && m["path"] == httpserver.EchoPath {
			require.Equal(t, "req-audit-0001", m["request_id"], "log akses membawa request_id yang sama dengan activity")
			sawRequestLog = true
		}
	}
	require.True(t, sawRequestLog)
}
