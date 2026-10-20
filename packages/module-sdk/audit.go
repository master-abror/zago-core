package modulesdk

import (
	"context"

	"github.com/google/uuid"
)

// Nilai ActivityEntry.Result (kolom activities.result; docs/13 §2.2).
const (
	ResultSuccess = "success"
	ResultDenied  = "denied"
	ResultFailed  = "failed"
)

// ActivityEntry adalah satu catatan aktivitas yang dilaporkan modul. Aktor, organisasi, scope,
// IP, user agent, request id, dan trace id TIDAK diisi modul: perekam mengambilnya dari context
// supaya bentuk setiap activity seragam (docs/13 §2.2).
type ActivityEntry struct {
	Action       string    // mis. "invoice.approved"; wajib
	ResourceType string    // mis. "invoice"; opsional
	ResourceID   uuid.UUID // uuid.Nil = tidak ada
	// Result: ResultSuccess (default bila kosong), ResultDenied, atau ResultFailed.
	Result string
	// Metadata bebas, sudah melewati daftar redaksi sebelum disimpan (docs/13 §2.3): jangan
	// pernah menaruh password/token/rahasia di sini, tetapi bila terlanjur, nilainya disamarkan.
	Metadata map[string]any
}

// AuditRecorder merekam activity. Record tidak mengembalikan error (docs/10 §4):
//   - Di dalam TxManager.WithinTx, result=success ditulis pada transaksi yang SAMA dengan aksi;
//     kegagalan menulis membatalkan transaksi (fail-closed).
//   - result=denied/failed ditulis di transaksi TERPISAH, sehingga tetap tercatat walau
//     transaksi utama di-rollback.
type AuditRecorder interface {
	Record(ctx context.Context, entry ActivityEntry)
}
