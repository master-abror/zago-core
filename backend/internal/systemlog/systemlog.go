// Package systemlog menulis tabel system_logs: subset kejadian operasional yang layak muncul di
// UI Super Admin (docs/13 §1, docs/14 §3). Berbeda dari activities: tidak berisi aksi pengguna.
//
// Penulisan bersifat best-effort dan TIDAK PERNAH menggagalkan pemanggil: Log tidak mengembalikan
// error, tidak membatalkan request, dan dibatasi lajunya per event_code supaya badai kegagalan
// tidak berubah menjadi badai tulis. Detail lengkap tetap ada di log stdout.
package systemlog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sync"
	"time"

	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/pkg/id"
	"github.com/master-abror/zago-core/backend/pkg/redact"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// Level adalah nilai kolom system_logs.level. Sengaja alias string agar antarmuka konsumen
// (mis. internal/audit) tidak perlu mengimpor paket ini.
type Level = string

// Level yang diizinkan constraint tabel.
const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
	LevelFatal Level = "fatal"
)

const (
	maxEventCodeLen  = 150
	maxMessageLen    = 2000
	maxMetadataBytes = 16 * 1024
	maxTrackedCodes  = 1000
	overflowCode     = "_overflow"
	writeTimeout     = 2 * time.Second

	defaultMaxPerWindow = 5
	defaultWindow       = time.Minute
)

var (
	eventCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_.]*$`)
	validLevels      = map[Level]struct{}{LevelDebug: {}, LevelInfo: {}, LevelWarn: {}, LevelError: {}, LevelFatal: {}}
)

const insertSQL = `
INSERT INTO system_logs (id, service, environment, level, event_code, message, request_id, trace_id, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)`

type bucket struct {
	windowStart time.Time
	written     int
	suppressed  int
}

// Writer menulis system_logs lewat pool runtime (app_user: boleh INSERT/SELECT saja).
type Writer struct {
	db          kernel.DBTX
	service     string
	environment string
	log         *slog.Logger
	redactor    *redact.Redactor
	maxPerWin   int
	window      time.Duration
	now         func() time.Time

	mu      sync.Mutex
	buckets map[string]*bucket
}

// Option mengubah perilaku Writer.
type Option func(*Writer)

// WithRateLimit membatasi jumlah baris per event_code per jendela (default 5 per menit).
func WithRateLimit(maxPerWindow int, window time.Duration) Option {
	return func(w *Writer) { w.maxPerWin, w.window = maxPerWindow, window }
}

// WithClock mengganti sumber waktu (untuk tes).
func WithClock(now func() time.Time) Option { return func(w *Writer) { w.now = now } }

// WithRedactor mengganti Redactor (mis. dengan kunci sensitif deklarasi modul).
func WithRedactor(r *redact.Redactor) Option { return func(w *Writer) { w.redactor = r } }

// New membuat Writer untuk service ("api", "worker") pada environment tertentu.
func New(db kernel.DBTX, log *slog.Logger, service, environment string, opts ...Option) *Writer {
	if log == nil {
		log = slog.Default()
	}
	w := &Writer{
		db: db, service: service, environment: environment, log: log,
		redactor:  redact.Default(),
		maxPerWin: defaultMaxPerWindow,
		window:    defaultWindow,
		now:       time.Now,
		buckets:   map[string]*bucket{},
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Log menulis satu baris system_logs. request_id dan trace_id diambil dari ctx. Input tak valid,
// laju terlampaui, atau kegagalan database tidak pernah sampai ke pemanggil: hanya dicatat ke
// log stdout (bukan ke system_logs lagi, agar tak berulang tanpa akhir).
func (w *Writer) Log(ctx context.Context, level Level, eventCode, message string, metadata map[string]any) {
	if err := validate(level, eventCode, message); err != nil {
		w.log.WarnContext(ctx, "system log ditolak", "event_code", eventCode, "error", err)
		return
	}
	suppressed, ok := w.allow(eventCode)
	if !ok {
		return
	}

	meta, err := w.redactor.Map(metadata)
	if err != nil {
		meta = map[string]any{"_metadata_error": "metadata tidak dapat di-encode"}
	}
	if suppressed > 0 {
		meta["suppressed"] = suppressed
	}
	raw, err := json.Marshal(meta)
	if err != nil || len(raw) > maxMetadataBytes {
		raw = []byte(fmt.Sprintf(`{"_metadata_truncated":true,"suppressed":%d}`, suppressed))
	}

	// Context request bisa sudah dibatalkan (klien putus) padahal kejadian ini justru penting.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
	defer cancel()
	_, err = w.db.Exec(wctx, insertSQL,
		id.NewID().String(), w.service, w.environment, level, eventCode,
		w.redactor.String(truncate(message, maxMessageLen)),
		kernel.NullString(kernel.RequestIDFromContext(ctx)), kernel.NullString(kernel.TraceIDFromContext(ctx)), string(raw))
	if err != nil {
		w.log.WarnContext(ctx, "system log gagal ditulis", "event_code", eventCode, "error", err)
	}
}

func validate(level Level, eventCode, message string) error {
	if _, ok := validLevels[level]; !ok {
		return fmt.Errorf("level %q tidak dikenal", level)
	}
	if len(eventCode) == 0 || len(eventCode) > maxEventCodeLen || !eventCodePattern.MatchString(eventCode) {
		return fmt.Errorf("event_code harus huruf kecil/angka/underscore/titik (≤%d karakter)", maxEventCodeLen)
	}
	if message == "" {
		return fmt.Errorf("message wajib diisi")
	}
	return nil
}

// allow menerapkan batas laju per event_code. Saat jendela baru dimulai, jumlah baris yang
// ditahan pada jendela sebelumnya dikembalikan (dicatat di metadata "suppressed").
func (w *Writer) allow(code string) (suppressed int, ok bool) {
	now := w.now()
	w.mu.Lock()
	defer w.mu.Unlock()

	b := w.buckets[code]
	if b == nil {
		if len(w.buckets) >= maxTrackedCodes {
			code = overflowCode
			b = w.buckets[code]
		}
		if b == nil {
			b = &bucket{windowStart: now}
			w.buckets[code] = b
		}
	}
	if now.Sub(b.windowStart) >= w.window {
		suppressed = b.suppressed
		*b = bucket{windowStart: now}
	}
	if b.written >= w.maxPerWin {
		b.suppressed++
		return 0, false
	}
	b.written++
	return suppressed, true
}

// RelayOnDead menghasilkan hook kernel.RelayConfig.OnDead: event outbox yang menjadi
// dead-letter dicatat sebagai system log error "outbox.event_dead" (docs/10 §6.2).
func RelayOnDead(w *Writer) func(ctx context.Context, event modulesdk.Event, lastErr error) {
	return func(ctx context.Context, event modulesdk.Event, lastErr error) {
		meta := map[string]any{
			"event_id":   event.ID.String(),
			"event_name": event.Name,
			"attempts":   event.Attempt,
		}
		if lastErr != nil {
			meta["error"] = lastErr.Error()
		}
		w.Log(ctx, LevelError, "outbox.event_dead", "event outbox menjadi dead-letter setelah percobaan maksimal", meta)
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
