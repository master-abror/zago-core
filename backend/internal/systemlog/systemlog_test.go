package systemlog_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/systemlog"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

var ctx = context.Background()

type row struct {
	Service, Environment, Level, EventCode, Message string
	RequestID, TraceID                              *string
	Metadata                                        string
}

func rows(t *testing.T, db *testpg.DB) []row {
	t.Helper()
	rs, err := db.App.Query(ctx, `
		SELECT service, environment, level, event_code, message, request_id, trace_id, metadata::text
		  FROM system_logs ORDER BY created_at, id`)
	require.NoError(t, err)
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		require.NoError(t, rs.Scan(&r.Service, &r.Environment, &r.Level, &r.EventCode, &r.Message,
			&r.RequestID, &r.TraceID, &r.Metadata))
		out = append(out, r)
	}
	require.NoError(t, rs.Err())
	return out
}

func newWriter(t *testing.T, opts ...systemlog.Option) (*systemlog.Writer, *testpg.DB) {
	t.Helper()
	db := testkit.NewDB(t)
	return systemlog.New(db.App, nil, "worker", "test", opts...), db
}

func TestLogWritesRowWithContextCorrelationAndRedactedMetadata(t *testing.T) {
	w, db := newWriter(t)
	rctx := kernel.WithTraceID(kernel.WithRequestID(ctx, "req-12345678"), "trace-abc")

	w.Log(rctx, systemlog.LevelError, "email.send_failed", "pengiriman email gagal", map[string]any{
		"attempt": 3, "smtp_password": "CANARY-PW", "note": "ok",
	})

	got := rows(t, db)
	require.Len(t, got, 1)
	r := got[0]
	require.Equal(t, "worker", r.Service)
	require.Equal(t, "test", r.Environment)
	require.Equal(t, "error", r.Level)
	require.Equal(t, "email.send_failed", r.EventCode)
	require.Equal(t, "pengiriman email gagal", r.Message)
	require.Equal(t, "req-12345678", *r.RequestID)
	require.Equal(t, "trace-abc", *r.TraceID)
	require.JSONEq(t, `{"attempt":3,"smtp_password":"[REDACTED]","note":"ok"}`, r.Metadata)
	require.NotContains(t, r.Metadata, "CANARY-PW")
}

func TestLogScrubsSecretShapedMessage(t *testing.T) {
	w, db := newWriter(t)
	w.Log(ctx, systemlog.LevelWarn, "db.connect_failed", "gagal konek postgres://app_user:CANARYpw@db:5432/x", nil)

	got := rows(t, db)
	require.Len(t, got, 1)
	require.NotContains(t, got[0].Message, "CANARYpw")
	require.JSONEq(t, `{}`, got[0].Metadata)
	require.Nil(t, got[0].RequestID)
}

func TestLogRateLimitsPerEventCodeAndReportsSuppressedCount(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	w, db := newWriter(t,
		systemlog.WithRateLimit(3, time.Minute),
		systemlog.WithClock(func() time.Time { return now }))

	for i := 0; i < 8; i++ {
		w.Log(ctx, systemlog.LevelError, "redis.pool_exhausted", "pool habis", nil)
	}
	w.Log(ctx, systemlog.LevelError, "other.event", "kode lain tidak terpengaruh", nil)
	require.Len(t, rows(t, db), 4, "3 baris pertama + 1 kode lain")

	now = now.Add(61 * time.Second)
	w.Log(ctx, systemlog.LevelError, "redis.pool_exhausted", "pool habis lagi", nil)

	got := rows(t, db)
	require.Len(t, got, 5)
	require.JSONEq(t, `{"suppressed":5}`, got[4].Metadata, "jumlah yang ditahan dilaporkan di jendela berikutnya")
}

func TestLogNeverPropagatesWriteFailure(t *testing.T) {
	w, db := newWriter(t)
	_, err := db.Migrator.Exec(ctx, `DROP TABLE system_logs`)
	require.NoError(t, err)

	require.NotPanics(t, func() {
		w.Log(ctx, systemlog.LevelError, "any.event", "database tak punya tabelnya", nil)
	})
}

func TestLogRejectsInvalidInputWithoutWriting(t *testing.T) {
	w, db := newWriter(t)
	w.Log(ctx, "verbose", "ok.code", "level salah", nil)
	w.Log(ctx, systemlog.LevelInfo, "Bad Code", "kode salah", nil)
	w.Log(ctx, systemlog.LevelInfo, "", "kode kosong", nil)
	w.Log(ctx, systemlog.LevelInfo, strings.Repeat("a", 151), "kode terlalu panjang", nil)
	w.Log(ctx, systemlog.LevelInfo, "ok.code", "", nil)
	require.Empty(t, rows(t, db))
}

func TestLogBoundsMessageAndMetadataSize(t *testing.T) {
	w, db := newWriter(t)
	w.Log(ctx, systemlog.LevelInfo, "big.message", strings.Repeat("x", 5000), nil)
	w.Log(ctx, systemlog.LevelInfo, "big.metadata", "metadata besar", map[string]any{"blob": strings.Repeat("y", 40_000)})

	got := rows(t, db)
	require.Len(t, got, 2)
	require.Len(t, got[0].Message, 2000)
	require.NotContains(t, got[1].Metadata, "yyyy")
	require.Contains(t, got[1].Metadata, "_metadata_truncated")
}

func TestLogStillWritesWhenRequestContextIsAlreadyCanceled(t *testing.T) {
	w, db := newWriter(t)
	cctx, cancel := context.WithCancel(ctx)
	cancel()

	w.Log(cctx, systemlog.LevelError, "client.aborted", "klien putus di tengah proses", nil)
	require.Len(t, rows(t, db), 1)
}

func TestRelayOnDeadWritesSystemLogRow(t *testing.T) {
	w, db := newWriter(t)
	hook := systemlog.RelayOnDead(w)
	evID := id.NewID()

	hook(ctx, modulesdk.Event{ID: evID, Name: "finance.invoice.created", Attempt: 8}, errors.New("smtp timeout"))

	got := rows(t, db)
	require.Len(t, got, 1)
	require.Equal(t, "outbox.event_dead", got[0].EventCode)
	require.Equal(t, "error", got[0].Level)
	require.Contains(t, got[0].Metadata, evID.String())
	require.Contains(t, got[0].Metadata, "finance.invoice.created")
	require.Contains(t, got[0].Metadata, "smtp timeout")
}
