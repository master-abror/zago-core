package httpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/master-abror/zago-core/backend/pkg/id"
	"github.com/master-abror/zago-core/backend/pkg/logger"
)

// requestIDPattern membatasi X-Request-ID dari klien: karakter aman, panjang wajar.
// Nilai lain diganti ID baru supaya klien tidak bisa menyisipkan sembarang teks ke log.
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// RequestID memakai X-Request-ID klien bila valid, selain itu membangkitkan UUIDv7. ID dikirim
// balik di header respons dan disimpan di context sehingga setiap log ikut membawanya.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-ID")
		if !requestIDPattern.MatchString(rid) {
			rid = id.NewID().String()
		}
		w.Header().Set("X-Request-ID", rid)
		next.ServeHTTP(w, r.WithContext(logger.WithRequestID(r.Context(), rid)))
	})
}

type clientIPKey struct{}

// ClientIP mengembalikan IP klien hasil RealIP ("" bila tidak diketahui).
func ClientIP(ctx context.Context) string {
	v, _ := ctx.Value(clientIPKey{}).(string)
	return v
}

// RealIP menentukan IP klien. X-Forwarded-For HANYA dipercaya bila peer langsung termasuk
// TRUSTED_PROXIES; selain itu header diabaikan (mencegah spoofing). Dengan peer tepercaya,
// dipakai entri terkanan yang bukan proxy tepercaya.
func RealIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	contains := func(a netip.Addr) bool {
		for _, p := range trusted {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := peerAddr(r.RemoteAddr)
			if ip.IsValid() && contains(ip) {
				var parts []string
				for _, h := range r.Header.Values("X-Forwarded-For") {
					parts = append(parts, strings.Split(h, ",")...)
				}
				for i := len(parts) - 1; i >= 0; i-- {
					a, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
					if err != nil {
						break
					}
					ip = a.Unmap()
					if !contains(ip) {
						break
					}
				}
			}
			val := ""
			if ip.IsValid() {
				val = ip.String()
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey{}, val)))
		})
	}
}

func peerAddr(remote string) netip.Addr {
	if ap, err := netip.ParseAddrPort(remote); err == nil {
		return ap.Addr().Unmap()
	}
	if a, err := netip.ParseAddr(remote); err == nil {
		return a.Unmap()
	}
	return netip.Addr{}
}

// AccessLog mencatat satu baris per request. Hanya path (tanpa query string, yang bisa memuat
// token) yang dicatat. Probe /health dicatat pada level debug agar log tidak banjir.
func AccessLog(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			level := slog.LevelInfo
			switch {
			case status >= 500:
				level = slog.LevelError
			case strings.HasPrefix(r.URL.Path, "/health"):
				level = slog.LevelDebug
			}
			log.LogAttrs(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", status),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("client_ip", ClientIP(r.Context())),
			)
		})
	}
}

// Recoverer mengubah panic handler menjadi 500 generik. Stack trace dan pesan panic hanya
// masuk log, tidak pernah ke respons.
func Recoverer(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				log.ErrorContext(r.Context(), "panic pada handler",
					"panic", fmt.Sprint(rec), "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal_error")
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders memasang header keamanan untuk respons API. CSP untuk SPA, HSTS, dan header
// lain milik reverse proxy (docs/15 §6, difinalkan di M15).
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// writeError menulis error JSON minimal. Bentuk envelope final ditetapkan di M02 (docs/08).
func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code}})
}
