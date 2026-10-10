package systemlog_test

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/pkg/logger"
)

// Log bertanda logger.SystemLog menjadi baris system_logs lewat Writer.Sink: request_id/trace_id
// dari context, atribut menjadi metadata yang DIREDAKSI, dan penanda tidak muncul di stdout.
func TestSinkWritesFlaggedLogWithCorrelationAndRedaction(t *testing.T) {
	w, db := newWriter(t)
	gate := logger.NewGate()
	gate.Attach(w.Sink())
	var stdout bytes.Buffer
	log := logger.New(&stdout, slog.LevelInfo, logger.WithSystemGate(gate))
	rctx := kernel.WithTraceID(kernel.WithRequestID(ctx, "req-sink-0001"), "trace-sink-1")

	log.ErrorContext(rctx, "job gagal", "error", errors.New("smtp timeout"),
		"smtp_password", "CANARY-PW", logger.SystemLog("worker.job_failed"))

	got := rows(t, db)
	require.Len(t, got, 1)
	require.Equal(t, "error", got[0].Level)
	require.Equal(t, "worker.job_failed", got[0].EventCode)
	require.Equal(t, "job gagal", got[0].Message)
	require.NotNil(t, got[0].RequestID)
	require.Equal(t, "req-sink-0001", *got[0].RequestID)
	require.NotNil(t, got[0].TraceID)
	require.Equal(t, "trace-sink-1", *got[0].TraceID)
	require.Contains(t, got[0].Metadata, "smtp timeout")
	require.NotContains(t, got[0].Metadata, "CANARY-PW", "kunci sensitif diredaksi Writer")
	require.NotContains(t, stdout.String(), "worker.job_failed", "penanda tidak bocor ke stdout")
}

func TestSinkMapsSlogLevelsToTableLevels(t *testing.T) {
	w, db := newWriter(t)
	gate := logger.NewGate()
	gate.Attach(w.Sink())
	log := logger.New(&bytes.Buffer{}, slog.LevelDebug, logger.WithSystemGate(gate))

	log.Debug("d", logger.SystemLog("lvl.debug"))
	log.Info("i", logger.SystemLog("lvl.info"))
	log.Warn("w", logger.SystemLog("lvl.warn"))
	log.Error("e", logger.SystemLog("lvl.error"))

	byCode := map[string]string{}
	for _, r := range rows(t, db) {
		byCode[r.EventCode] = r.Level
	}
	require.Equal(t, map[string]string{
		"lvl.debug": "debug", "lvl.info": "info", "lvl.warn": "warn", "lvl.error": "error",
	}, byCode)
}

// Event code yang tak valid ditolak Writer (hanya dicatat ke stdout) dan tidak menggagalkan log.
func TestSinkInvalidEventCodeNeverFailsTheCaller(t *testing.T) {
	w, db := newWriter(t)
	gate := logger.NewGate()
	gate.Attach(w.Sink())
	var stdout bytes.Buffer
	log := logger.New(&stdout, slog.LevelInfo, logger.WithSystemGate(gate))

	require.NotPanics(t, func() { log.Error("x", logger.SystemLog("Kode Tidak Valid!")) })

	require.Empty(t, rows(t, db))
	require.Contains(t, stdout.String(), `"msg":"x"`)
}
