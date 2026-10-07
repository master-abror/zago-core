// Package logger membungkus log/slog: keluaran JSON, level dari konfigurasi, dan atribut
// request_id otomatis bila context membawanya (docs/10 §7, docs/14).
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

type ctxKey struct{}

// WithRequestID menyimpan request id pada context agar setiap log yang memakai context itu
// (logger.InfoContext, dst.) otomatis menyertakan atribut "request_id".
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, ctxKey{}, requestID)
}

// RequestID mengembalikan request id pada context, atau "" bila tidak ada.
func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(ctxKey{}).(string)
	return v
}

type traceKey struct{}

// WithTraceID menyimpan trace id pada context; setiap log yang memakai context itu menyertakan
// atribut "trace_id" (docs/14 §3, §6).
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceKey{}, traceID)
}

// TraceID mengembalikan trace id pada context, atau "" bila tidak ada.
func TraceID(ctx context.Context) string {
	v, _ := ctx.Value(traceKey{}).(string)
	return v
}

// ParseLevel mengubah "debug|info|warn|error" (tak peka huruf besar/kecil) menjadi slog.Level.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("level log tidak dikenal (pilihan: debug, info, warn, error)")
	}
}

// New membuat logger JSON yang menulis ke w pada level minimum yang diberikan.
func New(w io.Writer, level slog.Level) *slog.Logger {
	base := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(contextHandler{Handler: base})
}

// contextHandler menambahkan request_id dari context ke setiap record.
type contextHandler struct {
	slog.Handler
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if rid := RequestID(ctx); rid != "" {
		r.AddAttrs(slog.String("request_id", rid))
	}
	if tid := TraceID(ctx); tid != "" {
		r.AddAttrs(slog.String("trace_id", tid))
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{Handler: h.Handler.WithGroup(name)}
}
