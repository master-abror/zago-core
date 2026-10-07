package kernel

import (
	"context"

	"github.com/master-abror/zago-core/backend/pkg/logger"
)

// Accessor context bertipe (docs/10 §10). request id dan trace id disimpan oleh pkg/logger —
// satu sumber kebenaran — sehingga setiap log yang memakai context ikut membawanya.

// WithRequestID menyimpan request id pada context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return logger.WithRequestID(ctx, requestID)
}

// RequestIDFromContext mengembalikan request id, atau "" bila tidak ada.
func RequestIDFromContext(ctx context.Context) string { return logger.RequestID(ctx) }

// WithTraceID menyimpan trace id pada context.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	return logger.WithTraceID(ctx, traceID)
}

// TraceIDFromContext mengembalikan trace id, atau "" bila tidak ada.
func TraceIDFromContext(ctx context.Context) string { return logger.TraceID(ctx) }
