package kernel

import (
	"context"

	"github.com/google/uuid"

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

// Jenis aktor (activities.actor_type).
const (
	ActorUser    = "user"
	ActorService = "service"
	ActorSystem  = "system"
)

// Actor adalah pelaku request: diisi middleware autentikasi (M03), dibaca perekam audit.
type Actor struct {
	UserID         uuid.UUID // uuid.Nil untuk service/system
	Type           string    // ActorUser (default bila UserID terisi), ActorService, ActorSystem
	OrganizationID uuid.UUID // organisasi aktif request; uuid.Nil bila tidak ada
}

// Scope adalah scope otorisasi request (mis. organization, group); kosong bila tidak ada.
type Scope struct {
	Type string
	ID   uuid.UUID
}

// Client adalah asal request: IP hasil RealIP yang tepercaya dan User-Agent.
type Client struct {
	IP        string
	UserAgent string
}

// WithActor menyimpan aktor pada context.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, ctxKeyActor, a)
}

// ActorFromContext mengembalikan aktor pada context, bila ada.
func ActorFromContext(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(ctxKeyActor).(Actor)
	return a, ok
}

// WithScope menyimpan scope otorisasi pada context.
func WithScope(ctx context.Context, s Scope) context.Context {
	return context.WithValue(ctx, ctxKeyScope, s)
}

// ScopeFromContext mengembalikan scope pada context, bila ada.
func ScopeFromContext(ctx context.Context) (Scope, bool) {
	s, ok := ctx.Value(ctxKeyScope).(Scope)
	return s, ok
}

// WithClient menyimpan asal request pada context.
func WithClient(ctx context.Context, c Client) context.Context {
	return context.WithValue(ctx, ctxKeyClient, c)
}

// ClientFromContext mengembalikan asal request pada context, bila ada.
func ClientFromContext(ctx context.Context) (Client, bool) {
	c, ok := ctx.Value(ctxKeyClient).(Client)
	return c, ok
}
