package telemetry

import (
	"errors"
	"net/url"
	"strings"
)

// OTLPEndpoint adalah URL dasar kolektor OTLP/HTTP yang sudah dipecah menjadi opsi exporter.
type OTLPEndpoint struct {
	Host       string // host[:port]
	Insecure   bool   // true bila skema http (tanpa TLS)
	TracesPath string // path lengkap sinyal trace, mis. "/v1/traces" atau "/otlp/v1/traces"
}

// ParseOTLPEndpoint memecah OTEL_EXPORTER_OTLP_ENDPOINT. Sesuai spesifikasi OTLP/HTTP, URL dasar
// ditambah "/v1/traces" (prefiks path pada URL dasar dipertahankan). Kredensial, query, dan fragment
// ditolak; pesan galat TIDAK memuat nilai masukan karena URL bisa membawa rahasia.
func ParseOTLPEndpoint(raw string) (OTLPEndpoint, error) {
	errInvalid := errors.New("endpoint OTLP harus URL http/https dengan host, tanpa kredensial, query, atau fragment")
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return OTLPEndpoint{}, errInvalid
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return OTLPEndpoint{}, errInvalid
	}
	return OTLPEndpoint{
		Host:       u.Host,
		Insecure:   u.Scheme == "http",
		TracesPath: strings.TrimRight(u.Path, "/") + "/v1/traces",
	}, nil
}
