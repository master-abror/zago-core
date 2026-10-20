package modulesdk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Event adalah satu kejadian domain (docs/07 §8, docs/10 §6.2). Nama mengikuti
// <modul>.<entitas>.<aksi>, mis. "finance.invoice.approved" atau "core.user.deactivated".
//
// Pengiriman lewat outbox bersifat at-least-once: handler WAJIB idempoten dan menghilangkan
// duplikat berdasarkan ID.
type Event struct {
	// ID dibangkitkan oleh bus saat Publish bila nol. Stabil lintas percobaan pengiriman.
	ID             uuid.UUID
	Name           string
	OrganizationID uuid.UUID // uuid.Nil = event tidak terikat organisasi
	AggregateType  string    // opsional, mis. "invoice"
	AggregateID    uuid.UUID // uuid.Nil = tidak ada
	// Payload harus objek JSON. Bila kosong, bus memakai {}. Gunakan NewEvent agar payload
	// berupa struct tidak perlu di-encode manual.
	Payload json.RawMessage
	// RequestID dan TraceID diisi bus dari context saat Publish bila kosong, sehingga korelasi
	// log/trace mengalir dari API ke handler di worker (docs/14 §6).
	RequestID string
	TraceID   string
	// OccurredAt diisi saat Publish. Pada pengiriman relay, nilainya waktu event masuk outbox.
	OccurredAt time.Time
	// Attempt (mulai 1) hanya terisi pada pengiriman oleh relay; 0 pada handler in-process.
	Attempt int
}

// NewEvent membuat Event dengan payload hasil encode JSON dari payload (harus berupa objek).
func NewEvent(name string, payload any) (Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Event{}, fmt.Errorf("modulesdk: payload event %q tidak dapat di-encode JSON: %w", name, err)
	}
	return Event{Name: name, Payload: raw}, nil
}

// EventHandler memproses satu event. Error (atau panic) membuat relay mencoba lagi dengan
// backoff; setelah batas percobaan event menjadi dead-letter.
type EventHandler func(ctx context.Context, event Event) error

// EventBus adalah kontrak final bus event (docs/07 §8).
//
// Publish di dalam transaksi (context dari TxManager.WithinTx) menulis event ke outbox pada
// transaksi yang SAMA: ia ter-commit atau batal bersama perubahan state. Publish tidak
// mengembalikan error; kegagalan menulis membatalkan transaksi (fail-closed). Tanpa transaksi,
// event ditulis seketika (autocommit).
//
// Subscribe mendaftarkan handler untuk proses ini. Di API: handler in-process ringan yang jalan
// sinkron SETELAH commit (khusus core; modul tidak memakainya). Di worker (lewat
// RegisterEventHandlers): handler yang menerima event dari relay outbox.
type EventBus interface {
	Publish(ctx context.Context, event Event)
	Subscribe(eventName string, handler EventHandler)
}

// TxManager menjalankan fn di dalam satu transaksi database (docs/10 §6). WithinTx bersarang
// bergabung ke transaksi yang sudah ada.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
