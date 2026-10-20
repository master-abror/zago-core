package app_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/master-abror/zago-core/backend/internal/app"
)

// resetOTel mengembalikan provider global ke no-op sebelum dan sesudah tes (OTel menyimpannya global).
func resetOTel(t *testing.T) {
	t.Helper()
	otel.SetTracerProvider(noop.NewTracerProvider())
	t.Cleanup(func() { otel.SetTracerProvider(noop.NewTracerProvider()) })
}

func spanRecords() bool {
	_, span := otel.Tracer("app_test").Start(context.Background(), "probe")
	defer span.End()
	return span.IsRecording()
}

// Endpoint tak terjangkau sengaja: pembuatan exporter tidak boleh memblokir start, dan tanpa span
// tidak ada ekspor yang gagal.
const unreachableOTLP = "http://127.0.0.1:1"

func TestAPIWithoutOTLPEndpointKeepsTracingNoop(t *testing.T) {
	resetOTel(t)
	base, stop := startAPI(t, &fakeDeps{})
	defer stop()

	require.Equal(t, 200, status(t, base+"/health"))
	require.False(t, spanRecords(), "tanpa OTEL_EXPORTER_OTLP_ENDPOINT, tracing no-op (docs/14 §6)")
}

func TestAPIStartsTracingWhenOTLPEndpointIsSetAndShutsItDown(t *testing.T) {
	resetOTel(t)
	env := devEnv()
	env["OTEL_EXPORTER_OTLP_ENDPOINT"] = unreachableOTLP
	base, stop := startAPIEnv(t, &fakeDeps{}, env)

	require.Equal(t, 200, status(t, base+"/health"), "kolektor yang belum siap tidak mencegah API melayani")
	require.True(t, spanRecords(), "provider tracing terpasang selama API berjalan")

	require.Equal(t, app.ExitOK, stop())
	require.False(t, spanRecords(), "provider dimatikan (flush) saat API berhenti")
}

func TestAPIRejectsInvalidOTLPEndpointAsConfigErrorWithoutLeakingIt(t *testing.T) {
	env := devEnv()
	env["OTEL_EXPORTER_OTLP_ENDPOINT"] = "https://user:SECRET-OTEL-PW@collector:4318"
	deps := &fakeDeps{}
	var stderr bytes.Buffer

	code := app.RunAPI(context.Background(), env, deps, &stderr, nil)

	require.Equal(t, app.ExitConfig, code)
	require.Contains(t, stderr.String(), "OTEL_EXPORTER_OTLP_ENDPOINT")
	require.NotContains(t, stderr.String(), "SECRET-OTEL-PW")
	opened, _ := deps.snapshot()
	require.Empty(t, opened)
}

func TestWorkerStartsTracingWhenOTLPEndpointIsSetAndShutsItDown(t *testing.T) {
	resetOTel(t)
	env := devEnv()
	env["OTEL_EXPORTER_OTLP_ENDPOINT"] = unreachableOTLP
	stop := startWorkerEnv(t, &fakeDeps{}, env)

	require.Eventually(t, spanRecords, 2*time.Second, 10*time.Millisecond,
		"provider tracing terpasang selama worker berjalan")

	require.Equal(t, app.ExitOK, stop())
	require.False(t, spanRecords(), "provider dimatikan saat worker berhenti")
}
