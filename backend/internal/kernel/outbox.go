package kernel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// maxEventNameLen sama dengan lebar kolom outbox_events.event_name (varchar(200)).
const maxEventNameLen = 200

// eventNamePattern: huruf kecil/angka/underscore, dipisah titik, minimal dua segmen
// (<modul>.<entitas>.<aksi>; docs/07 §8 juga memakai bentuk dua segmen seperti role_permissions.changed).
var eventNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

const insertOutboxSQL = `
INSERT INTO outbox_events (id, organization_id, event_name, aggregate_type, aggregate_id, payload, request_id, trace_id)
VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8)`

// Bus memenuhi modulesdk.EventBus (docs/07 §8, docs/10 §6, errata B-01): dua jalur pengiriman.
//
//  1. Outbox transaksional (durable, lintas proses): Publish menulis outbox_events pada transaksi
//     yang ada di ctx — atau seketika (autocommit) bila tak ada. Relay di worker mengirimnya.
//  2. Handler in-process (Subscribe): dijalankan sinkron SETELAH commit di proses yang
//     mem-publish; hanya untuk reaksi lokal yang murah (mis. invalidasi cache authz).
//
// Publish tidak mengembalikan error (kontrak module-sdk). Event tak valid atau gagal ditulis
// menandai transaksi rollback-only (FailTx) sehingga operasi bisnis ikut batal — fail-closed.
type Bus struct {
	db  DBTX
	log *slog.Logger

	mu    sync.RWMutex
	local map[string][]modulesdk.EventHandler
}

var _ modulesdk.EventBus = (*Bus)(nil)

// NewBus membuat bus di atas db (pool runtime app_user). log opsional.
func NewBus(db DBTX, log *slog.Logger) *Bus {
	if log == nil {
		log = slog.Default()
	}
	return &Bus{db: db, log: log, local: map[string][]modulesdk.EventHandler{}}
}

// Subscribe mendaftarkan handler in-process untuk eventName. Dipanggil saat wiring (sebelum
// menerima traffic).
func (b *Bus) Subscribe(eventName string, handler modulesdk.EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.local[eventName] = append(b.local[eventName], handler)
}

// Publish menulis event ke outbox lalu menjadwalkan handler in-process setelah commit.
func (b *Bus) Publish(ctx context.Context, event modulesdk.Event) {
	ev, err := prepareEvent(ctx, event)
	if err != nil {
		b.publishFailed(ctx, event.Name, err)
		return
	}
	_, err = DBFrom(ctx, b.db).Exec(ctx, insertOutboxSQL,
		ev.ID.String(), nullUUID(ev.OrganizationID), ev.Name, nullString(ev.AggregateType),
		nullUUID(ev.AggregateID), string(ev.Payload), nullString(ev.RequestID), nullString(ev.TraceID))
	if err != nil {
		b.publishFailed(ctx, ev.Name, fmt.Errorf("tulis outbox: %w", err))
		return
	}
	AfterCommit(ctx, func() { b.dispatchLocal(DetachTx(ctx), ev) })
}

// publishFailed membatalkan transaksi (bila ada) dan mencatat sebabnya; tak pernah diam-diam.
func (b *Bus) publishFailed(ctx context.Context, name string, err error) {
	inTx := FailTx(ctx, fmt.Errorf("publish event %q: %w", name, err))
	b.log.ErrorContext(ctx, "event gagal dipublikasikan",
		"event_name", name, "transaction_marked_rollback_only", inTx, "error", err)
}

func (b *Bus) dispatchLocal(ctx context.Context, ev modulesdk.Event) {
	b.mu.RLock()
	handlers := append([]modulesdk.EventHandler(nil), b.local[ev.Name]...)
	b.mu.RUnlock()

	for _, h := range handlers {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					b.log.ErrorContext(ctx, "handler event in-process panik",
						"event_name", ev.Name, "event_id", ev.ID.String(), "panic", fmt.Sprint(rec))
				}
			}()
			if err := h(ctx, ev); err != nil {
				b.log.ErrorContext(ctx, "handler event in-process gagal",
					"event_name", ev.Name, "event_id", ev.ID.String(), "error", err)
			}
		}()
	}
}

// prepareEvent memvalidasi event dan mengisi nilai bawaan (ID, payload, korelasi, waktu).
func prepareEvent(ctx context.Context, ev modulesdk.Event) (modulesdk.Event, error) {
	if ev.Name == "" || len(ev.Name) > maxEventNameLen || !eventNamePattern.MatchString(ev.Name) {
		return ev, errors.New("nama event harus <modul>.<entitas>.<aksi> (huruf kecil, angka, underscore; ≤200 karakter)")
	}
	payload := bytes.TrimSpace(ev.Payload)
	switch {
	case len(payload) == 0:
		payload = []byte("{}")
	case payload[0] != '{' || !json.Valid(payload):
		return ev, errors.New("payload event harus objek JSON yang valid")
	}
	ev.Payload = payload
	if ev.ID == uuid.Nil {
		ev.ID = id.NewID()
	}
	if ev.RequestID == "" {
		ev.RequestID = RequestIDFromContext(ctx)
	}
	if ev.TraceID == "" {
		ev.TraceID = TraceIDFromContext(ctx)
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now()
	}
	return ev, nil
}

// nullUUID mengubah uuid.Nil menjadi NULL SQL; selain itu teks UUID.
func nullUUID(u uuid.UUID) any {
	if u == uuid.Nil {
		return nil
	}
	return u.String()
}

// nullString mengubah string kosong menjadi NULL SQL.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
