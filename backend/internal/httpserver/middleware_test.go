package httpserver_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/httpserver"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/pkg/logger"
)

func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &m), l)
		out = append(out, m)
	}
	return out
}

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// ---------- RequestID ----------

func TestRequestIDGeneratedWhenAbsent(t *testing.T) {
	var seen string
	h := httpserver.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = logger.RequestID(r.Context())
	}))
	rec := serve(h, httptest.NewRequest(http.MethodGet, "/", nil))

	got := rec.Header().Get("X-Request-ID")
	require.NotEmpty(t, got)
	require.Equal(t, got, seen, "ID di context harus sama dengan di header respons")
	u, err := uuid.Parse(got)
	require.NoError(t, err)
	require.Equal(t, uuid.Version(7), u.Version())
}

func TestRequestIDValidClientValueIsKept(t *testing.T) {
	h := httpserver.RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-ID", "client-trace-0001")
	require.Equal(t, "client-trace-0001", serve(h, req).Header().Get("X-Request-ID"))
}

func TestRequestIDInvalidClientValueIsReplaced(t *testing.T) {
	for _, bad := range []string{"short", strings.Repeat("a", 65), "has space in it", "inject\"quote0001", "emoji-😀-0000001", "semi;colon-00001"} {
		h := httpserver.RequestID(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-ID", bad)
		got := serve(h, req).Header().Get("X-Request-ID")
		require.NotEqual(t, bad, got, "nilai %q harus ditolak", bad)
		_, err := uuid.Parse(got)
		require.NoError(t, err)
	}
}

// ---------- RealIP ----------

func clientIPFor(t *testing.T, trusted []string, remote string, xff ...string) string {
	t.Helper()
	var prefixes []netip.Prefix
	for _, s := range trusted {
		prefixes = append(prefixes, netip.MustParsePrefix(s))
	}
	var got string
	h := httpserver.RealIP(prefixes)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = httpserver.ClientIP(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	for _, v := range xff {
		req.Header.Add("X-Forwarded-For", v)
	}
	serve(h, req)
	return got
}

func TestRealIP(t *testing.T) {
	proxies := []string{"10.0.0.0/8"}
	cases := []struct {
		name    string
		trusted []string
		remote  string
		xff     []string
		want    string
	}{
		{"tanpa proxy tepercaya: XFF diabaikan (anti-spoof)", nil, "198.51.100.7:4000", []string{"1.2.3.4"}, "198.51.100.7"},
		{"peer tidak tepercaya mengirim XFF palsu", proxies, "198.51.100.7:4000", []string{"1.2.3.4"}, "198.51.100.7"},
		{"peer tepercaya tanpa XFF", proxies, "10.0.0.1:4000", nil, "10.0.0.1"},
		{"peer tepercaya, satu entri XFF", proxies, "10.0.0.1:4000", []string{"203.0.113.9"}, "203.0.113.9"},
		{"rantai proxy: ambil terkanan yang tak tepercaya", proxies, "10.0.0.1:4000", []string{"203.0.113.9, 10.0.0.2"}, "203.0.113.9"},
		{"klien menyisipkan entri kiri palsu", proxies, "10.0.0.1:4000", []string{"1.2.3.4, 203.0.113.9"}, "203.0.113.9"},
		{"beberapa header XFF digabung", proxies, "10.0.0.1:4000", []string{"1.2.3.4", "203.0.113.9, 10.0.0.2"}, "203.0.113.9"},
		{"semua tepercaya: entri paling kiri", proxies, "10.0.0.1:4000", []string{"10.0.0.5, 10.0.0.2"}, "10.0.0.5"},
		{"entri XFF tidak valid: berhenti di peer", proxies, "10.0.0.1:4000", []string{"bukan-ip"}, "10.0.0.1"},
		{"entri tidak valid di kanan", proxies, "10.0.0.1:4000", []string{"203.0.113.9, sampah"}, "10.0.0.1"},
		{"IPv6", []string{"fd00::/8"}, "[fd00::1]:4000", []string{"2001:db8::7"}, "2001:db8::7"},
		{"IPv4-mapped IPv6 dinormalkan", proxies, "[::ffff:10.0.0.1]:4000", []string{"203.0.113.9"}, "203.0.113.9"},
		{"RemoteAddr tidak valid", proxies, "garbage", []string{"203.0.113.9"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, clientIPFor(t, tc.trusted, tc.remote, tc.xff...))
		})
	}
}

// ---------- ClientContext ----------

func TestClientContextStoresIPAndUserAgent(t *testing.T) {
	prefixes := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	var got kernel.Client
	var ok bool
	h := httpserver.RealIP(prefixes)(httpserver.ClientContext(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok = kernel.ClientFromContext(r.Context())
	})))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.1.2.3:4567"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	req.Header.Set("User-Agent", "smoke-agent/1.0")
	serve(h, req)

	require.True(t, ok)
	require.Equal(t, "203.0.113.7", got.IP)
	require.Equal(t, "smoke-agent/1.0", got.UserAgent)
}

func TestClientContextIgnoresSpoofedForwardedForFromUntrustedPeer(t *testing.T) {
	var got kernel.Client
	h := httpserver.RealIP(nil)(httpserver.ClientContext(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = kernel.ClientFromContext(r.Context())
	})))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.9:1111"
	req.Header.Set("X-Forwarded-For", "203.0.113.7")
	serve(h, req)
	require.Equal(t, "198.51.100.9", got.IP)
}

func TestClientContextCapsUserAgentLength(t *testing.T) {
	var got kernel.Client
	h := httpserver.ClientContext(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = kernel.ClientFromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("User-Agent", strings.Repeat("a", 2000))
	serve(h, req)
	require.Len(t, got.UserAgent, 512)
}

// ---------- Recoverer ----------

func TestRecovererTurnsPanicInto500WithoutLeaking(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(&buf, slog.LevelInfo)
	calls := 0
	h := httpserver.Recoverer(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/boom" {
			panic("rahasia-internal-xyz")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	rec := serve(h, httptest.NewRequest(http.MethodGet, "/boom", nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), `"internal_error"`)
	require.NotContains(t, rec.Body.String(), "rahasia-internal-xyz")
	require.NotContains(t, rec.Body.String(), "goroutine")

	lines := logLines(t, &buf)
	require.Len(t, lines, 1)
	require.Equal(t, "ERROR", lines[0]["level"])
	require.Contains(t, lines[0]["panic"], "rahasia-internal-xyz")
	require.Contains(t, lines[0]["stack"], "goroutine")

	// Proses tetap hidup dan melayani request berikutnya.
	require.Equal(t, http.StatusNoContent, serve(h, httptest.NewRequest(http.MethodGet, "/ok", nil)).Code)
	require.Equal(t, 2, calls)
}

func TestRecovererRepanicsOnAbortHandler(t *testing.T) {
	h := httpserver.Recoverer(logger.New(&bytes.Buffer{}, slog.LevelInfo))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	require.PanicsWithValue(t, http.ErrAbortHandler, func() {
		serve(h, httptest.NewRequest(http.MethodGet, "/", nil))
	})
}

// ---------- AccessLog ----------

func TestAccessLogRecordsFieldsButNeverTheQueryString(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(&buf, slog.LevelInfo)
	h := httpserver.RequestID(httpserver.RealIP(nil)(httpserver.AccessLog(log)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte("abc"))
		}))))

	req := httptest.NewRequest(http.MethodPost, "/api/x?token=SECRET-TOKEN", nil)
	req.RemoteAddr = "198.51.100.7:1234"
	req.Header.Set("X-Request-ID", "trace-abcdef01")
	serve(h, req)

	lines := logLines(t, &buf)
	require.Len(t, lines, 1)
	l := lines[0]
	require.Equal(t, "POST", l["method"])
	require.Equal(t, "/api/x", l["path"])
	require.EqualValues(t, http.StatusTeapot, l["status"])
	require.EqualValues(t, 3, l["bytes"])
	require.Equal(t, "198.51.100.7", l["client_ip"])
	require.Equal(t, "trace-abcdef01", l["request_id"])
	require.Contains(t, l, "duration_ms")
	require.NotContains(t, buf.String(), "SECRET-TOKEN")
}

func TestAccessLogHealthProbesAreDebugOnly(t *testing.T) {
	var buf bytes.Buffer
	log := logger.New(&buf, slog.LevelInfo)
	h := httpserver.AccessLog(log)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	serve(h, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	require.Empty(t, strings.TrimSpace(buf.String()), "probe tidak boleh membanjiri log level info")

	var dbg bytes.Buffer
	h = httpserver.AccessLog(logger.New(&dbg, slog.LevelDebug))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	serve(h, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	require.Len(t, logLines(t, &dbg), 1)
}

func TestAccessLogServerErrorIsErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	h := httpserver.AccessLog(logger.New(&buf, slog.LevelInfo))(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	serve(h, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
	lines := logLines(t, &buf)
	require.Len(t, lines, 1)
	require.Equal(t, "ERROR", lines[0]["level"])
}
