// Package audit mengimplementasikan modulesdk.AuditRecorder (docs/13 §2.2–2.3, docs/10 §6.1).
//
// Perekam mengisi aktor, organisasi, scope, IP, user agent, request id, dan trace id dari
// context — penulis modul cukup memanggil Record(ctx, entry). Metadata disamarkan lewat daftar
// redaksi sebelum disimpan.
//
// Atomisitas:
//   - result=success: ditulis lewat transaksi pada ctx (DBFrom), sehingga ter-commit atau batal
//     bersama aksi bisnisnya. Kegagalan menulis menandai transaksi rollback-only: operasi ikut
//     gagal (fail-closed), tidak pernah hilang diam-diam.
//   - result=denied/failed: ditulis di transaksi TERPISAH (satu statement pada pool, tidak lewat
//     transaksi ctx) sehingga tetap tercatat walau transaksi utama di-rollback. Kegagalannya
//     dicatat keras (log + system log) tetapi tidak membatalkan apa pun: aksi memang sudah ditolak.
//     Catatan: penulisan ini butuh satu koneksi pool tambahan selagi transaksi utama masih terbuka.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/pkg/id"
	"github.com/master-abror/zago-core/backend/pkg/redact"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

const (
	maxActionLen       = 150
	maxResourceTypeLen = 150
	maxUserAgentLen    = 512
	maxMetadataBytes   = 16 * 1024
	ownTxTimeout       = 5 * time.Second

	// EventWriteFailed adalah event_code system log saat activity gagal direkam.
	EventWriteFailed = "audit.write_failed"
)

const insertSQL = `
INSERT INTO activities (id, organization_id, actor_user_id, actor_type, action, resource_type, resource_id,
                        scope_type, scope_id, result, ip_address, user_agent, request_id, trace_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::inet, $12, $13, $14, $15::jsonb)`

// SystemLogger adalah tujuan laporan kegagalan perekaman (dipenuhi *systemlog.Writer).
type SystemLogger interface {
	Log(ctx context.Context, level, eventCode, message string, metadata map[string]any)
}

// Recorder memenuhi modulesdk.AuditRecorder.
type Recorder struct {
	db       kernel.DBTX
	log      *slog.Logger
	redactor *redact.Redactor
	sysLog   SystemLogger
}

var _ modulesdk.AuditRecorder = (*Recorder)(nil)

// Option mengubah perilaku Recorder.
type Option func(*Recorder)

// WithRedactor mengganti Redactor (mis. dengan kunci sensitif deklarasi modul).
func WithRedactor(r *redact.Redactor) Option { return func(rec *Recorder) { rec.redactor = r } }

// WithSystemLog menyalurkan kegagalan perekaman ke system_logs (event_code "audit.write_failed").
func WithSystemLog(s SystemLogger) Option { return func(rec *Recorder) { rec.sysLog = s } }

// New membuat Recorder di atas pool runtime (app_user: boleh INSERT/SELECT activities saja).
func New(db kernel.DBTX, log *slog.Logger, opts ...Option) *Recorder {
	if log == nil {
		log = slog.Default()
	}
	r := &Recorder{db: db, log: log, redactor: redact.Default()}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// record adalah baris activities yang siap ditulis.
type record struct {
	args   []any
	result string
}

// Record merekam satu activity. Lihat dokumentasi paket untuk semantik transaksi.
func (r *Recorder) Record(ctx context.Context, entry modulesdk.ActivityEntry) {
	rec, err := r.build(ctx, entry)
	if err != nil {
		// Entri tak valid adalah bug pemanggil: jangan biarkan operasi lolos tanpa jejak.
		r.reportFailure(ctx, entry, err, true)
		return
	}

	if rec.result == modulesdk.ResultSuccess {
		if _, err = kernel.DBFrom(ctx, r.db).Exec(ctx, insertSQL, rec.args...); err != nil {
			r.reportFailure(ctx, entry, fmt.Errorf("tulis activity: %w", err), true)
		}
		return
	}

	// denied/failed: transaksi sendiri, tahan pembatalan ctx (penolakan justru penting dicatat).
	wctx, cancel := context.WithTimeout(context.WithoutCancel(kernel.DetachTx(ctx)), ownTxTimeout)
	defer cancel()
	if _, err = r.db.Exec(wctx, insertSQL, rec.args...); err != nil {
		r.reportFailure(ctx, entry, fmt.Errorf("tulis activity (%s): %w", rec.result, err), false)
	}
}

// reportFailure mencatat kegagalan dengan keras. failTx=true menandai transaksi pada ctx
// rollback-only (fail-closed).
func (r *Recorder) reportFailure(ctx context.Context, entry modulesdk.ActivityEntry, err error, failTx bool) {
	inTx := false
	if failTx {
		inTx = kernel.FailTx(ctx, fmt.Errorf("audit %q: %w", entry.Action, err))
	}
	r.log.ErrorContext(ctx, "activity gagal direkam",
		"action", entry.Action, "result", entry.Result,
		"transaction_marked_rollback_only", inTx, "error", err)
	if r.sysLog != nil {
		r.sysLog.Log(ctx, "error", EventWriteFailed, "activity gagal direkam", map[string]any{
			"action": entry.Action, "result": entry.Result, "error": err.Error(),
		})
	}
}

func (r *Recorder) build(ctx context.Context, e modulesdk.ActivityEntry) (record, error) {
	if e.Action == "" || len(e.Action) > maxActionLen {
		return record{}, fmt.Errorf("action wajib diisi dan ≤%d karakter", maxActionLen)
	}
	if len(e.ResourceType) > maxResourceTypeLen {
		return record{}, fmt.Errorf("resource_type maksimal %d karakter", maxResourceTypeLen)
	}
	result := e.Result
	switch result {
	case "":
		result = modulesdk.ResultSuccess
	case modulesdk.ResultSuccess, modulesdk.ResultDenied, modulesdk.ResultFailed:
	default:
		return record{}, fmt.Errorf("result %q tidak dikenal (success|denied|failed)", result)
	}

	actorType, actorID, orgID := kernel.ActorSystem, uuid.Nil, uuid.Nil
	if a, ok := kernel.ActorFromContext(ctx); ok {
		actorID, orgID = a.UserID, a.OrganizationID
		switch a.Type {
		case "":
			if actorID != uuid.Nil {
				actorType = kernel.ActorUser
			}
		case kernel.ActorUser, kernel.ActorService, kernel.ActorSystem:
			actorType = a.Type
		default:
			return record{}, fmt.Errorf("actor type %q tidak dikenal", a.Type)
		}
	}

	var scopeType any
	var scopeID any
	if s, ok := kernel.ScopeFromContext(ctx); ok {
		scopeType, scopeID = kernel.NullString(s.Type), kernel.NullUUID(s.ID)
	}

	var ip, userAgent any
	if c, ok := kernel.ClientFromContext(ctx); ok {
		if addr, err := netip.ParseAddr(c.IP); err == nil {
			ip = addr.WithZone("").String()
		}
		ua := c.UserAgent
		if len(ua) > maxUserAgentLen {
			ua = ua[:maxUserAgentLen]
		}
		userAgent = kernel.NullString(ua)
	}

	meta, err := r.redactor.Map(e.Metadata)
	if err != nil {
		return record{}, fmt.Errorf("metadata tidak dapat di-encode: %w", err)
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return record{}, fmt.Errorf("metadata tidak dapat di-encode: %w", err)
	}
	if len(raw) > maxMetadataBytes {
		raw = []byte(fmt.Sprintf(`{"_metadata_truncated":true,"_original_bytes":%d}`, len(raw)))
	}

	return record{
		result: result,
		args: []any{
			id.NewID().String(), kernel.NullUUID(orgID), kernel.NullUUID(actorID), actorType,
			e.Action, kernel.NullString(e.ResourceType), kernel.NullUUID(e.ResourceID),
			scopeType, scopeID, result, ip, userAgent,
			kernel.NullString(kernel.RequestIDFromContext(ctx)), kernel.NullString(kernel.TraceIDFromContext(ctx)),
			string(raw),
		},
	}, nil
}
