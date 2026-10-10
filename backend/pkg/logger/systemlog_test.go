package logger_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/logger"
)

// recordingSink mencatat setiap entri yang sampai ke sink beserta context-nya.
type recordingSink struct {
	mu      sync.Mutex
	entries []logger.SystemEntry
	ctxs    []context.Context
}

func (s *recordingSink) SystemLog(ctx context.Context, e logger.SystemEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, e)
	s.ctxs = append(s.ctxs, ctx)
}

func (s *recordingSink) got() []logger.SystemEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]logger.SystemEntry(nil), s.entries...)
}

type panicSink struct{}

func (panicSink) SystemLog(context.Context, logger.SystemEntry) { panic("sink rusak") }

func newGated(t *testing.T, level slog.Level) (*slog.Logger, *bytes.Buffer, *logger.Gate, *recordingSink) {
	t.Helper()
	var buf bytes.Buffer
	gate := logger.NewGate()
	sink := &recordingSink{}
	gate.Attach(sink)
	return logger.New(&buf, level, logger.WithSystemGate(gate)), &buf, gate, sink
}

func TestSystemLogAttrIsStrippedFromStdoutAndDeliveredToSink(t *testing.T) {
	log, buf, _, sink := newGated(t, slog.LevelInfo)

	log.ErrorContext(context.Background(), "job gagal", "job", "email", logger.SystemLog("worker.job_failed"))

	lines := decodeLines(t, buf)
	require.Len(t, lines, 1)
	require.Equal(t, "job gagal", lines[0]["msg"])
	require.Equal(t, "email", lines[0]["job"], "atribut biasa tetap tampil di stdout")
	require.NotContains(t, buf.String(), "worker.job_failed", "penanda system log tidak boleh bocor ke stdout")

	got := sink.got()
	require.Len(t, got, 1)
	require.Equal(t, "worker.job_failed", got[0].EventCode)
	require.Equal(t, slog.LevelError, got[0].Level)
	require.Equal(t, "job gagal", got[0].Message)
	require.Equal(t, map[string]any{"job": "email"}, got[0].Fields)
}

func TestLogWithoutMarkerIsNotDeliveredToSink(t *testing.T) {
	log, buf, _, sink := newGated(t, slog.LevelInfo)

	log.ErrorContext(context.Background(), "hanya stdout", "k", "v")

	require.Len(t, decodeLines(t, buf), 1)
	require.Empty(t, sink.got())
}

func TestSystemLogFieldsFlattenGroupsAndStringifyErrors(t *testing.T) {
	log, _, _, sink := newGated(t, slog.LevelInfo)

	log.WarnContext(context.Background(), "campuran",
		"error", errors.New("smtp timeout"),
		"n", 3,
		slog.Group("job", "id", 7, slog.Group("retry", "count", 2)),
		logger.SystemLog("mix.fields"))

	got := sink.got()
	require.Len(t, got, 1)
	require.Equal(t, "smtp timeout", got[0].Fields["error"], "error dikirim sebagai teks, bukan objek")
	require.EqualValues(t, 3, got[0].Fields["n"])
	require.EqualValues(t, 7, got[0].Fields["job.id"])
	require.EqualValues(t, 2, got[0].Fields["job.retry.count"])
	require.NotContains(t, got[0].Fields, "binary", "atribut With() tidak ikut ke system log")
}

func TestSystemLogCarriesRequestAndTraceIDInContext(t *testing.T) {
	log, buf, _, sink := newGated(t, slog.LevelInfo)
	ctx := logger.WithTraceID(logger.WithRequestID(context.Background(), "req-1"), "trace-1")

	log.ErrorContext(ctx, "gagal", logger.SystemLog("x.failed"))

	require.Len(t, sink.ctxs, 1)
	require.Equal(t, "req-1", logger.RequestID(sink.ctxs[0]))
	require.Equal(t, "trace-1", logger.TraceID(sink.ctxs[0]))
	lines := decodeLines(t, buf)
	require.Equal(t, "req-1", lines[0]["request_id"])
	require.Equal(t, "trace-1", lines[0]["trace_id"])
}

func TestSystemLogIsNoopWithoutGateOrSink(t *testing.T) {
	var buf bytes.Buffer

	// Tanpa gate sama sekali.
	logger.New(&buf, slog.LevelInfo).ErrorContext(context.Background(), "a", logger.SystemLog("x.a"))
	// Gate ada tetapi belum ada sink yang di-attach.
	logger.New(&buf, slog.LevelInfo, logger.WithSystemGate(logger.NewGate())).
		ErrorContext(context.Background(), "b", logger.SystemLog("x.b"))

	lines := decodeLines(t, &buf)
	require.Len(t, lines, 2, "log tetap tertulis ke stdout")
	require.NotContains(t, buf.String(), "x.a")
	require.NotContains(t, buf.String(), "x.b")
}

func TestGateAttachedAfterLoggerCreatedAndDerived(t *testing.T) {
	var buf bytes.Buffer
	gate := logger.NewGate()
	log := logger.New(&buf, slog.LevelInfo, logger.WithSystemGate(gate)).With("binary", "api").WithGroup("g")
	sink := &recordingSink{}

	log.Error("sebelum attach", logger.SystemLog("x.before"))
	gate.Attach(sink)
	log.Error("sesudah attach", logger.SystemLog("x.after"))
	gate.Detach()
	log.Error("sesudah detach", logger.SystemLog("x.detached"))

	got := sink.got()
	require.Len(t, got, 1, "hanya log antara Attach dan Detach yang sampai ke sink")
	require.Equal(t, "x.after", got[0].EventCode)
	require.Len(t, decodeLines(t, &buf), 3)
}

func TestSystemLogBelowMinimumLevelIsNotDelivered(t *testing.T) {
	log, buf, _, sink := newGated(t, slog.LevelWarn)

	log.InfoContext(context.Background(), "info", logger.SystemLog("x.info"))

	require.Empty(t, sink.got(), "level di bawah minimum logger tidak diproses sama sekali")
	require.Empty(t, bytes.TrimSpace(buf.Bytes()))
}

func TestSystemLogEmptyCodeIsIgnoredButStripped(t *testing.T) {
	log, buf, _, sink := newGated(t, slog.LevelInfo)

	log.ErrorContext(context.Background(), "tanpa kode", logger.SystemLog(""))

	require.Empty(t, sink.got())
	lines := decodeLines(t, buf)
	require.Len(t, lines, 1)
	require.Equal(t, "tanpa kode", lines[0]["msg"])
	require.NotContains(t, buf.String(), "system_log", "penanda tetap dibuang walau kodenya kosong")
}

func TestPanickingSinkNeverBreaksLoggingAndIsReported(t *testing.T) {
	var buf bytes.Buffer
	gate := logger.NewGate()
	gate.Attach(panicSink{})
	log := logger.New(&buf, slog.LevelInfo, logger.WithSystemGate(gate))

	require.NotPanics(t, func() {
		log.ErrorContext(context.Background(), "tetap tertulis", logger.SystemLog("x.panic"))
	})

	lines := decodeLines(t, &buf)
	require.Len(t, lines, 2, "baris asli + peringatan bahwa sink panik")
	require.Equal(t, "tetap tertulis", lines[0]["msg"])
	require.Equal(t, "WARN", lines[1]["level"])
	require.Contains(t, lines[1]["msg"], "sink system log")
}

func TestGateAttachDetachIsRaceFree(t *testing.T) {
	var buf bytes.Buffer
	gate := logger.NewGate()
	log := logger.New(syncWriter{&buf, &sync.Mutex{}}, slog.LevelInfo, logger.WithSystemGate(gate))
	sink := &recordingSink{}

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 200 {
				log.Error("x", logger.SystemLog("x.race"))
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 200 {
			gate.Attach(sink)
			gate.Detach()
		}
	}()
	wg.Wait()
}

// syncWriter membuat bytes.Buffer aman dipakai banyak goroutine.
type syncWriter struct {
	b  *bytes.Buffer
	mu *sync.Mutex
}

func (w syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}
