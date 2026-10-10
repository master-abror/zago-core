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

// New membuat logger JSON yang menulis ke w pada level minimum yang diberikan. Opsi
// (mis. WithSystemGate) bersifat opsional.
func New(w io.Writer, level slog.Level, opts ...Option) *slog.Logger {
	base := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	h := contextHandler{Handler: base}
	for _, opt := range opts {
		opt(&h)
	}
	return slog.New(h)
}

// contextHandler menambahkan request_id/trace_id dari context ke setiap record dan, bila ada
// gate, meneruskan log bertanda SystemLog ke sink system_logs (docs/14 §3).
type contextHandler struct {
	slog.Handler
	gate *Gate
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	var (
		code   string
		fields map[string]any
	)
	if hasSystemLogMarker(r) {
		r, code, fields = splitSystemLog(r)
	}
	if rid := RequestID(ctx); rid != "" {
		r.AddAttrs(slog.String("request_id", rid))
	}
	if tid := TraceID(ctx); tid != "" {
		r.AddAttrs(slog.String("trace_id", tid))
	}
	err := h.Handler.Handle(ctx, r)
	if code != "" {
		h.deliver(ctx, SystemEntry{Level: r.Level, EventCode: code, Message: r.Message, Fields: fields})
	}
	return err
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{Handler: h.Handler.WithAttrs(attrs), gate: h.gate}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{Handler: h.Handler.WithGroup(name), gate: h.gate}
}
