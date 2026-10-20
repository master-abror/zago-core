package kernel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/master-abror/zago-core/backend/pkg/logger"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// RelayConfig mengatur relay outbox. Nilai nol berarti default.
type RelayConfig struct {
	BatchSize      int           // maksimum event per RunOnce (default 20)
	PollInterval   time.Duration // jeda saat antrian kosong (default 1s)
	MaxAttempts    int           // percobaan sebelum dead-letter (default 8)
	BaseBackoff    time.Duration // backoff percobaan pertama (default 2s)
	MaxBackoff     time.Duration // batas atas backoff (default 5m)
	HandlerTimeout time.Duration // batas waktu seluruh handler satu event (default 30s)

	// Backoff, bila diisi, menggantikan perhitungan eksponensial bawaan. failures = jumlah
	// kegagalan beruntun (≥ 1).
	Backoff func(failures int) time.Duration

	// OnDead dipanggil setelah event menjadi dead-letter (docs/10 §6.2: system log error).
	// Dipanggil di luar transaksi relay.
	OnDead func(ctx context.Context, event modulesdk.Event, lastErr error)
}

func (c RelayConfig) withDefaults() RelayConfig {
	if c.BatchSize <= 0 {
		c.BatchSize = 20
	}
	if c.PollInterval <= 0 {
		c.PollInterval = time.Second
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 8
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = 2 * time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 5 * time.Minute
	}
	if c.HandlerTimeout <= 0 {
		c.HandlerTimeout = 30 * time.Second
	}
	if c.Backoff == nil {
		base, limit := c.BaseBackoff, c.MaxBackoff
		c.Backoff = func(failures int) time.Duration { return exponentialBackoff(base, limit, failures) }
	}
	return c
}

// exponentialBackoff = base·2^(failures-1), dibatasi limit, dengan jitter ±10% agar banyak event
// gagal serentak tidak dicoba ulang bersamaan.
func exponentialBackoff(base, limit time.Duration, failures int) time.Duration {
	d := base
	for i := 1; i < failures && d < limit; i++ {
		d *= 2
	}
	if d > limit {
		d = limit
	}
	return time.Duration(float64(d) * (0.9 + 0.2*rand.Float64()))
}

// Relay mengirim event outbox ke handler terdaftar (docs/10 §6.2). Dijalankan di worker; boleh
// ada banyak instance sekaligus (FOR UPDATE SKIP LOCKED mencegah event ganda diproses serentak).
//
// Pengiriman at-least-once: event diproses di transaksi SENDIRI per event. Bila handler gagal,
// transaksi di-ROLLBACK (efek samping DB handler dibatalkan, termasuk event lanjutan yang
// di-publish handler), lalu kegagalan dicatat di transaksi kedua: attempts+1, backoff, atau
// dead-letter setelah MaxAttempts. (Doc 10 §6.2 menggambarkan satu transaksi per batch; ini
// sedikit lebih ketat — ADR-0010.)
type Relay struct {
	tx  *TxManager
	log *slog.Logger
	cfg RelayConfig

	mu       sync.RWMutex
	handlers map[string][]modulesdk.EventHandler
}

// NewRelay membuat relay di atas pool runtime (app_user: boleh SELECT/UPDATE outbox_events).
func NewRelay(pool Pool, log *slog.Logger, cfg RelayConfig) *Relay {
	if log == nil {
		log = slog.Default()
	}
	return &Relay{
		tx:       NewTxManager(pool, log),
		log:      log,
		cfg:      cfg.withDefaults(),
		handlers: map[string][]modulesdk.EventHandler{},
	}
}

// Subscribe mendaftarkan handler yang menerima event eventName dari relay. Handler berjalan di
// dalam transaksi relay (ctx membawa transaksi: DBFrom dan Publish ikut atomik) dan HARUS
// idempoten — event yang sama bisa terkirim lebih dari sekali.
func (r *Relay) Subscribe(eventName string, handler modulesdk.EventHandler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.handlers[eventName] = append(r.handlers[eventName], handler)
}

// EventBus mengembalikan modulesdk.EventBus untuk worker: Subscribe mendaftar ke relay, Publish
// menulis ke outbox lewat pub. Dipakai sebagai argumen RegisterEventHandlers modul (docs/07 §8).
func (r *Relay) EventBus(pub modulesdk.EventBus) modulesdk.EventBus {
	return relayBus{pub: pub, relay: r}
}

type relayBus struct {
	pub   modulesdk.EventBus
	relay *Relay
}

func (b relayBus) Publish(ctx context.Context, e modulesdk.Event) { b.pub.Publish(ctx, e) }
func (b relayBus) Subscribe(name string, h modulesdk.EventHandler) {
	b.relay.Subscribe(name, h)
}

// Run memproses outbox sampai ctx dibatalkan, lalu mengembalikan nil. Error sementara
// (mis. database mati) dicatat dan dicoba lagi pada siklus berikutnya.
func (r *Relay) Run(ctx context.Context) error {
	for {
		n, err := r.RunOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			r.log.ErrorContext(ctx, "relay outbox gagal; dicoba lagi", "error", err)
		}
		if err == nil && n >= r.cfg.BatchSize {
			continue // kemungkinan masih ada antrian: jangan tidur
		}
		timer := time.NewTimer(r.cfg.PollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}

// RunOnce memproses hingga BatchSize event yang sudah jatuh tempo dan mengembalikan jumlah
// event yang selesai diproses (berhasil, atau tak punya handler). Event yang gagal dan dijadwal
// ulang tidak dihitung.
func (r *Relay) RunOnce(ctx context.Context) (int, error) {
	done := 0
	for i := 0; i < r.cfg.BatchSize; i++ {
		res, err := r.processNext(ctx)
		if err != nil {
			return done, err
		}
		switch res {
		case outcomeEmpty:
			return done, nil
		case outcomeProcessed:
			done++
		case outcomeRetry:
		}
	}
	return done, nil
}

type outcome int

const (
	outcomeEmpty outcome = iota
	outcomeProcessed
	outcomeRetry
)

const claimSQL = `
SELECT id::text, organization_id::text, event_name, aggregate_type, aggregate_id::text,
       payload::text, request_id, trace_id, attempts, created_at
FROM outbox_events
WHERE status = 'pending' AND available_at <= now()
ORDER BY available_at, created_at, id
LIMIT 1
FOR UPDATE SKIP LOCKED`

func (r *Relay) processNext(ctx context.Context) (outcome, error) {
	var (
		found      bool
		ev         modulesdk.Event
		attempts   int
		handlerErr error
	)
	err := r.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		ev, attempts, found, err = claimOne(ctx, r.tx.pool)
		if err != nil || !found {
			return err
		}
		ev.Attempt = attempts + 1
		if herr := r.dispatch(ctx, ev); herr != nil {
			handlerErr = herr
			return herr // ROLLBACK: efek samping handler dibatalkan
		}
		_, err = DBFrom(ctx, r.tx.pool).Exec(ctx,
			`UPDATE outbox_events SET status = 'processed', processed_at = now(), last_error = NULL WHERE id = $1`,
			ev.ID.String())
		return err
	})

	switch {
	case err == nil && !found:
		return outcomeEmpty, nil
	case err == nil:
		return outcomeProcessed, nil
	case handlerErr == nil && errors.Is(err, ErrTxRollbackOnly):
		// Handler "sukses" tetapi menandai transaksi gagal (mis. audit/outbox gagal ditulis).
		handlerErr = err
	case handlerErr == nil:
		return outcomeEmpty, err // kegagalan infrastruktur (database), bukan handler
	}

	if rerr := r.recordFailure(ctx, ev, attempts, handlerErr); rerr != nil {
		return outcomeEmpty, rerr
	}
	return outcomeRetry, nil
}

// claimOne mengambil satu event jatuh tempo dan mengunci barisnya selama transaksi berjalan.
func claimOne(ctx context.Context, pool DBTX) (ev modulesdk.Event, attempts int, found bool, err error) {
	var (
		idText, name, payload string
		orgID, aggType, aggID *string
		requestID, traceID    *string
		createdAt             time.Time
	)
	row := DBFrom(ctx, pool).QueryRow(ctx, claimSQL)
	if err = row.Scan(&idText, &orgID, &name, &aggType, &aggID, &payload, &requestID, &traceID, &attempts, &createdAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ev, 0, false, nil
		}
		return ev, 0, false, fmt.Errorf("klaim event outbox: %w", err)
	}
	ev = modulesdk.Event{
		Name:       name,
		Payload:    []byte(payload),
		OccurredAt: createdAt,
	}
	if ev.ID, err = uuid.Parse(idText); err != nil {
		return ev, 0, false, fmt.Errorf("id event outbox tak valid: %w", err)
	}
	if ev.OrganizationID, err = parseOptionalUUID(orgID); err != nil {
		return ev, 0, false, err
	}
	if ev.AggregateID, err = parseOptionalUUID(aggID); err != nil {
		return ev, 0, false, err
	}
	ev.AggregateType = deref(aggType)
	ev.RequestID = deref(requestID)
	ev.TraceID = deref(traceID)
	return ev, attempts, true, nil
}

func parseOptionalUUID(s *string) (uuid.UUID, error) {
	if s == nil {
		return uuid.Nil, nil
	}
	u, err := uuid.Parse(*s)
	if err != nil {
		return uuid.Nil, fmt.Errorf("uuid event outbox tak valid: %w", err)
	}
	return u, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// dispatch menjalankan semua handler event. Tanpa handler terdaftar, event dianggap selesai
// (tidak ada yang berminat di proses ini). Handler pertama yang gagal/panik menghentikan
// pengiriman; seluruh event dicoba ulang (handler idempoten).
func (r *Relay) dispatch(ctx context.Context, ev modulesdk.Event) error {
	r.mu.RLock()
	handlers := append([]modulesdk.EventHandler(nil), r.handlers[ev.Name]...)
	r.mu.RUnlock()
	if len(handlers) == 0 {
		return nil
	}

	if ev.RequestID != "" {
		ctx = logger.WithRequestID(ctx, ev.RequestID)
	}
	if ev.TraceID != "" {
		ctx = logger.WithTraceID(ctx, ev.TraceID)
	}
	ctx, cancel := context.WithTimeout(ctx, r.cfg.HandlerTimeout)
	defer cancel()

	for _, h := range handlers {
		if err := callHandler(ctx, h, ev); err != nil {
			return err
		}
	}
	return nil
}

func callHandler(ctx context.Context, h modulesdk.EventHandler, ev modulesdk.Event) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("handler panik: %v", rec)
		}
	}()
	return h(ctx, ev)
}

const maxLastErrorLen = 2000

// recordFailure mencatat satu kegagalan di transaksi sendiri. Pengaman `attempts = $n`: bila
// relay lain sudah menyentuh baris ini sejak rollback, pembaruan ini tidak menimpa.
func (r *Relay) recordFailure(ctx context.Context, ev modulesdk.Event, attempts int, cause error) error {
	failures := attempts + 1
	msg := truncate(cause.Error(), maxLastErrorLen)

	if failures >= r.cfg.MaxAttempts {
		tag, err := r.tx.pool.Exec(ctx,
			`UPDATE outbox_events SET status = 'dead', attempts = $2, last_error = $3
			 WHERE id = $1 AND status = 'pending' AND attempts = $4`,
			ev.ID.String(), failures, msg, attempts)
		if err != nil {
			return fmt.Errorf("catat dead-letter: %w", err)
		}
		if tag.RowsAffected() == 1 {
			r.log.ErrorContext(ctx, "event outbox menjadi dead-letter",
				"event_code", "outbox.event_dead", "event_id", ev.ID.String(), "event_name", ev.Name,
				"attempts", failures, "error", msg)
			if r.cfg.OnDead != nil {
				r.cfg.OnDead(ctx, ev, cause)
			}
		}
		return nil
	}

	delay := r.cfg.Backoff(failures)
	_, err := r.tx.pool.Exec(ctx,
		`UPDATE outbox_events
		    SET attempts = $2, last_error = $3,
		        available_at = now() + ($4::bigint * interval '1 millisecond')
		  WHERE id = $1 AND status = 'pending' AND attempts = $5`,
		ev.ID.String(), failures, msg, delay.Milliseconds(), attempts)
	if err != nil {
		return fmt.Errorf("catat kegagalan event: %w", err)
	}
	r.log.WarnContext(ctx, "handler event outbox gagal; dijadwal ulang",
		"event_id", ev.ID.String(), "event_name", ev.Name, "attempts", failures, "retry_in", delay.String(), "error", msg)
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// OutboxStats adalah ringkasan antrian untuk metrik (docs/14 §5).
type OutboxStats struct {
	Pending          int64
	Dead             int64
	OldestPendingAge time.Duration // 0 bila tak ada yang pending
}

// ReadOutboxStats membaca kedalaman antrian dan umur event pending tertua.
func ReadOutboxStats(ctx context.Context, db DBTX) (OutboxStats, error) {
	var (
		s       OutboxStats
		ageSecs float64
	)
	err := db.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE status = 'pending'),
		       count(*) FILTER (WHERE status = 'dead'),
		       COALESCE(EXTRACT(EPOCH FROM (now() - min(created_at) FILTER (WHERE status = 'pending')))::float8, 0)
		  FROM outbox_events`).Scan(&s.Pending, &s.Dead, &ageSecs)
	if err != nil {
		return OutboxStats{}, fmt.Errorf("baca statistik outbox: %w", err)
	}
	s.OldestPendingAge = time.Duration(ageSecs * float64(time.Second))
	return s, nil
}
