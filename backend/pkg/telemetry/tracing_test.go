package telemetry_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/master-abror/zago-core/backend/pkg/telemetry"
)

// recordingExporter menyimpan span yang diekspor. Berbeda dari tracetest.InMemoryExporter, Shutdown
// TIDAK menghapus span, sehingga tes bisa memeriksa hasil setelah provider dimatikan (flush).
type recordingExporter struct {
	mu    sync.Mutex
	spans []sdktrace.ReadOnlySpan
}

func (e *recordingExporter) ExportSpans(_ context.Context, s []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.spans = append(e.spans, s...)
	return nil
}

func (e *recordingExporter) Shutdown(context.Context) error { return nil }

func (e *recordingExporter) snapshot() []sdktrace.ReadOnlySpan {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]sdktrace.ReadOnlySpan(nil), e.spans...)
}

// isolateOTelGlobals mengembalikan provider/propagator global setelah tes (OTel menyimpannya global).
func isolateOTelGlobals(t *testing.T) {
	t.Helper()
	tp, prop := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(prop)
		otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {}))
	})
}

func TestSetupTracingWithoutEndpointIsNoop(t *testing.T) {
	isolateOTelGlobals(t)
	otel.SetTracerProvider(noop.NewTracerProvider())
	before, beforeProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()

	shutdown, err := telemetry.SetupTracing(context.Background(), telemetry.TracingConfig{Service: "zago-api"})
	require.NoError(t, err)

	require.True(t, before == otel.GetTracerProvider(), "tanpa endpoint, provider global tidak boleh disentuh")
	require.Equal(t, beforeProp, otel.GetTextMapPropagator(), "propagator global tidak boleh disentuh")
	_, span := otel.Tracer("t").Start(context.Background(), "op")
	require.False(t, span.IsRecording(), "tanpa endpoint, span tidak merekam apa pun")
	span.End()
	require.NoError(t, shutdown(context.Background()))
}

func TestInstallTracingExportsSpansWithServiceResource(t *testing.T) {
	isolateOTelGlobals(t)
	exp := &recordingExporter{}
	shutdown := telemetry.InstallTracing(telemetry.TracingConfig{Service: "zago-api", Environment: "staging"}, exp)

	ctx, span := otel.Tracer("t").Start(context.Background(), "http.request")
	require.True(t, span.IsRecording())
	_, child := otel.Tracer("t").Start(ctx, "db.query")
	child.End()
	span.End()

	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, shutdown(sctx), "shutdown harus mem-flush batch")

	spans := exp.snapshot()
	require.Len(t, spans, 2)
	names := []string{spans[0].Name(), spans[1].Name()}
	require.ElementsMatch(t, []string{"http.request", "db.query"}, names)

	attrs := map[string]string{}
	for _, kv := range spans[0].Resource().Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsString()
	}
	require.Equal(t, "zago-api", attrs["service.name"])
	require.Equal(t, "staging", attrs["deployment.environment.name"])
	require.Contains(t, attrs, "telemetry.sdk.name", "atribut SDK bawaan tetap ada")
}

func TestInstallTracingPropagatesW3CTraceContextAndBaggage(t *testing.T) {
	isolateOTelGlobals(t)
	shutdown := telemetry.InstallTracing(telemetry.TracingConfig{Service: "zago-api"}, &recordingExporter{})
	defer func() { _ = shutdown(context.Background()) }()

	prop := otel.GetTextMapPropagator()
	require.Contains(t, prop.Fields(), "traceparent")
	require.Contains(t, prop.Fields(), "baggage")

	ctx, span := otel.Tracer("t").Start(context.Background(), "client")
	carrier := propagation.MapCarrier{}
	prop.Inject(ctx, carrier)
	require.NotEmpty(t, carrier["traceparent"])

	remote := trace.SpanContextFromContext(prop.Extract(context.Background(), carrier))
	require.True(t, remote.IsRemote())
	require.Equal(t, span.SpanContext().TraceID(), remote.TraceID(), "trace id harus terbawa lintas proses")
	span.End()
}

func TestInstallTracingRoutesOTelErrorsToLogger(t *testing.T) {
	isolateOTelGlobals(t)
	var logs bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	shutdown := telemetry.InstallTracing(telemetry.TracingConfig{Service: "zago-api", Log: log}, &recordingExporter{})
	defer func() { _ = shutdown(context.Background()) }()

	otel.Handle(errors.New("collector tidak terjangkau"))

	require.Contains(t, logs.String(), "collector tidak terjangkau")
	require.Contains(t, logs.String(), "level=WARN")
	require.Contains(t, logs.String(), "opentelemetry")
}

func TestOTLPTracesEndpointMapsBaseURLToExporterOptions(t *testing.T) {
	for name, tc := range map[string]struct {
		in       string
		host     string
		insecure bool
		path     string
	}{
		"http dengan port":        {"http://collector:4318", "collector:4318", true, "/v1/traces"},
		"https tanpa port":        {"https://otel.example.org", "otel.example.org", false, "/v1/traces"},
		"prefiks path":            {"http://gateway:8080/otlp", "gateway:8080", true, "/otlp/v1/traces"},
		"prefiks path berakhiran": {"https://gw.example.org/otlp/", "gw.example.org", false, "/otlp/v1/traces"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := telemetry.ParseOTLPEndpoint(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.host, got.Host)
			require.Equal(t, tc.insecure, got.Insecure)
			require.Equal(t, tc.path, got.TracesPath)
		})
	}
}

func TestOTLPTracesEndpointRejectsUnusableValues(t *testing.T) {
	for name, in := range map[string]string{
		"tanpa skema":        "collector:4318",
		"skema tak didukung": "ftp://collector:4318",
		"tanpa host":         "http://",
		"membawa kredensial": "https://user:secret@collector:4318",
		"membawa query":      "https://collector:4318/?token=abc",
		"membawa fragment":   "https://collector:4318/#x",
		"kosong":             "",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := telemetry.ParseOTLPEndpoint(in)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret", "kredensial tidak boleh bocor ke pesan galat")
		})
	}
}
