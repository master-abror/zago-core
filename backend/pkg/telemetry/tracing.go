package telemetry

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// TracingConfig mengonfigurasi tracing OpenTelemetry untuk satu proses (docs/14 §6).
type TracingConfig struct {
	Endpoint    string       // OTEL_EXPORTER_OTLP_ENDPOINT; kosong = no-op
	Service     string       // service.name, mis. "zago-api"
	Environment string       // deployment.environment.name
	Log         *slog.Logger // penerima galat OTel (kolektor tak terjangkau, dll.); nil = bawaan OTel
}

func noopShutdown(context.Context) error { return nil }

// SetupTracing menyalakan tracing OTLP/HTTP bila Endpoint terisi. Endpoint kosong = no-op penuh:
// tidak ada state global yang disentuh dan span yang dibuat kode tidak merekam apa pun. Mengembalikan
// shutdown yang HARUS dipanggil saat proses berhenti (mem-flush span yang masih dalam batch).
// Kegagalan mengirim ke kolektor tidak pernah menjatuhkan proses: dilaporkan lewat Log.
func SetupTracing(ctx context.Context, cfg TracingConfig) (shutdown func(context.Context) error, err error) {
	if cfg.Endpoint == "" {
		return noopShutdown, nil
	}
	ep, err := ParseOTLPEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	exp, err := newOTLPExporter(ctx, ep)
	if err != nil {
		return nil, fmt.Errorf("buat exporter OTLP: %w", err)
	}
	return InstallTracing(cfg, exp), nil
}

// InstallTracing memasang provider SDK dengan exporter yang diberikan sebagai provider global,
// beserta propagator W3C (traceparent + baggage) supaya trace berlanjut lintas proses, dan
// meneruskan galat OTel ke cfg.Log. Dipisah dari SetupTracing agar bisa dites dengan exporter
// in-memory tanpa kolektor. Span dikirim dalam batch; shutdown mem-flush.
func InstallTracing(cfg TracingConfig, exp sdktrace.SpanExporter) (shutdown func(context.Context) error) {
	service := cfg.Service
	if service == "" {
		service = "zago"
	}
	res := resource.NewSchemaless(
		attribute.String("service.name", service),
		attribute.String("deployment.environment.name", cfg.Environment),
	)
	if merged, err := resource.Merge(resource.Default(), res); err == nil {
		res = merged // atribut SDK bawaan (telemetry.sdk.*) tetap ikut
	}

	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	if cfg.Log != nil {
		log := cfg.Log
		otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
			log.Warn("opentelemetry: galat", "error", err)
		}))
	}
	return tp.Shutdown
}
