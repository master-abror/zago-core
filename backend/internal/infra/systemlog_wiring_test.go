package infra_test

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/app"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/logger"
)

// AttachSystemLog (dipakai RunAPI dan RunWorker) menghubungkan logger ke system_logs di
// PostgreSQL sungguhan: log bertanda ditulis dengan request_id dari context; setelah detach
// (sebelum pool ditutup) log bertanda hanya ke stdout.
func TestAttachSystemLogWritesFlaggedLogsUntilDetached(t *testing.T) {
	db := testkit.NewDB(t)
	var stdout bytes.Buffer
	gate := logger.NewGate()
	log := logger.New(&stdout, slog.LevelInfo, logger.WithSystemGate(gate))
	rctx := kernel.WithRequestID(context.Background(), "req-wire-0001")

	// Sebelum attach: hanya stdout.
	log.ErrorContext(rctx, "sebelum attach", logger.SystemLog("wiring.before"))

	w, detach := app.AttachSystemLog(gate, db.App, log, "api", "test")
	require.NotNil(t, w)
	log.ErrorContext(rctx, "selama attach", logger.SystemLog("wiring.attached"))
	detach()
	log.ErrorContext(rctx, "sesudah detach", logger.SystemLog("wiring.detached"))

	count := func(code string) (n int, requestID string, service string) {
		t.Helper()
		err := db.App.QueryRow(context.Background(),
			`SELECT count(*), COALESCE(max(request_id), ''), COALESCE(max(service), '')
			   FROM system_logs WHERE event_code = $1`, code).Scan(&n, &requestID, &service)
		require.NoError(t, err)
		return n, requestID, service
	}
	n, rid, svc := count("wiring.attached")
	require.Equal(t, 1, n)
	require.Equal(t, "req-wire-0001", rid)
	require.Equal(t, "api", svc)
	n, _, _ = count("wiring.before")
	require.Zero(t, n, "sebelum attach tidak ada sink")
	n, _, _ = count("wiring.detached")
	require.Zero(t, n, "setelah detach tidak ada sink")
	require.Equal(t, 3, bytes.Count(stdout.Bytes(), []byte("\n")), "ketiga log tetap tertulis ke stdout")
}
