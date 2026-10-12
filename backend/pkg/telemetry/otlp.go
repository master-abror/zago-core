package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// newOTLPExporter membuat exporter OTLP/HTTP (protobuf). Koneksi dibuat saat ekspor pertama,
// jadi kolektor yang belum siap tidak mencegah proses start. Dipisah dari tracing.go supaya
// satu-satunya berkas yang bergantung pada otlptracehttp kecil dan mudah ditinjau.
func newOTLPExporter(ctx context.Context, ep OTLPEndpoint) (sdktrace.SpanExporter, error) {
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(ep.Host),
		otlptracehttp.WithURLPath(ep.TracesPath),
	}
	if ep.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}
	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return exp, nil
}
