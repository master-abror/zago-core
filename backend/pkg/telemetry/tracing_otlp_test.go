package telemetry_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/master-abror/zago-core/backend/pkg/telemetry"
)

// Ekspor OTLP/HTTP nyata (otlptracehttp) ke server HTTP lokal: membuktikan pemetaan endpoint ->
// path "/v1/traces" (termasuk prefiks path) dan bahwa shutdown mem-flush batch.
func TestSetupTracingExportsSpansToOTLPHTTPEndpoint(t *testing.T) {
	for name, prefix := range map[string]string{"tanpa prefiks": "", "dengan prefiks path": "/otlp"} {
		t.Run(name, func(t *testing.T) {
			isolateOTelGlobals(t)

			type request struct {
				path, contentType string
				bytes             int
			}
			got := make(chan request, 8)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				got <- request{path: r.URL.Path, contentType: r.Header.Get("Content-Type"), bytes: len(b)}
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			shutdown, err := telemetry.SetupTracing(context.Background(), telemetry.TracingConfig{
				Endpoint: srv.URL + prefix, Service: "zago-api", Environment: "test",
			})
			require.NoError(t, err)

			_, span := otel.Tracer("t").Start(context.Background(), "http.request")
			span.End()

			sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			require.NoError(t, shutdown(sctx))

			select {
			case r := <-got:
				require.Equal(t, prefix+"/v1/traces", r.path)
				require.Equal(t, "application/x-protobuf", r.contentType)
				require.Positive(t, r.bytes, "badan permintaan berisi span")
			case <-time.After(10 * time.Second):
				t.Fatal("kolektor tidak menerima ekspor setelah shutdown")
			}
		})
	}
}

func TestSetupTracingRejectsInvalidEndpointWithoutLeakingIt(t *testing.T) {
	_, err := telemetry.SetupTracing(context.Background(), telemetry.TracingConfig{
		Endpoint: "https://user:SECRET-PW@collector:4318",
	})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "SECRET-PW")
}
