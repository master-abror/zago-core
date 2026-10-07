package kernel_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

const invoiceCreated = "finance.invoice.created"

type relayEnv struct {
	db    *testpg.DB
	tm    *kernel.TxManager
	bus   *kernel.Bus
	relay *kernel.Relay
}

func newRelayEnv(t *testing.T, cfg kernel.RelayConfig) relayEnv {
	t.Helper()
	db, tm := probeDB(t)
	return relayEnv{db: db, tm: tm, bus: kernel.NewBus(db.App, nil), relay: kernel.NewRelay(db.App, nil, cfg)}
}

// publish menulis satu event (autocommit) dan mengembalikan ID-nya.
func (e relayEnv) publish(t *testing.T, name string, payload map[string]any) uuid.UUID {
	t.Helper()
	ev := mustEvent(t, name, payload)
	ev.ID = id.NewID()
	e.bus.Publish(kernel.WithTraceID(kernel.WithRequestID(ctx, "req-12345678"), "trace-abc"), ev)
	return ev.ID
}

// makeDue memajukan semua event pending supaya jatuh tempo sekarang.
func (e relayEnv) makeDue(t *testing.T) {
	t.Helper()
	_, err := e.db.Migrator.Exec(ctx,
		`UPDATE outbox_events SET available_at = now() - interval '1 second' WHERE status = 'pending'`)
	require.NoError(t, err)
}

func (e relayEnv) row(t *testing.T, evID uuid.UUID) outboxRow {
	t.Helper()
	for _, r := range outboxRows(t, e.db) {
		if r.ID == evID.String() {
			return r
		}
	}
	t.Fatalf("event %s tidak ada di outbox", evID)
	return outboxRow{}
}

func (e relayEnv) runOnce(t *testing.T) int {
	t.Helper()
	n, err := e.relay.RunOnce(ctx)
	require.NoError(t, err)
	return n
}

func TestRelayDeliversInOrderRestoresCorrelationAndMarksProcessed(t *testing.T) {
	e := newRelayEnv(t, kernel.RelayConfig{})
	org := id.NewID()

	var got []modulesdk.Event
	var reqIDs, traceIDs []string
	e.relay.Subscribe(invoiceCreated, func(hctx context.Context, ev modulesdk.Event) error {
		got = append(got, ev)
		reqIDs = append(reqIDs, kernel.RequestIDFromContext(hctx))
		traceIDs = append(traceIDs, kernel.TraceIDFromContext(hctx))
		return nil
	})

	var ids []uuid.UUID
	for i := 0; i < 3; i++ {
		ev := mustEvent(t, invoiceCreated, map[string]any{"n": i})
		ev.ID, ev.OrganizationID, ev.AggregateType = id.NewID(), org, "invoice"
		e.bus.Publish(kernel.WithTraceID(kernel.WithRequestID(ctx, "req-12345678"), "trace-abc"), ev)
		ids = append(ids, ev.ID)
	}

	require.Equal(t, 3, e.runOnce(t))
	require.Len(t, got, 3)
	for i, ev := range got {
		require.Equal(t, ids[i], ev.ID, "urutan publish dipertahankan")
		require.Equal(t, invoiceCreated, ev.Name)
		require.Equal(t, 1, ev.Attempt)
		require.Equal(t, org, ev.OrganizationID)
		require.Equal(t, "invoice", ev.AggregateType)
		require.Equal(t, "req-12345678", ev.RequestID)
		require.Equal(t, "trace-abc", ev.TraceID)
		require.False(t, ev.OccurredAt.IsZero())
		require.JSONEq(t, `{"n":`+string(rune('0'+i))+`}`, string(ev.Payload))
		require.Equal(t, "req-12345678", reqIDs[i], "request id dipulihkan ke ctx handler")
		require.Equal(t, "trace-abc", traceIDs[i], "trace id dipulihkan ke ctx handler")
	}

	var processed int
	require.NoError(t, e.db.App.QueryRow(ctx,
		`SELECT count(*) FROM outbox_events WHERE status = 'processed' AND processed_at IS NOT NULL`).Scan(&processed))
	require.Equal(t, 3, processed)
	require.Zero(t, e.runOnce(t), "tidak ada pengiriman ulang")
}

func TestRelayMarksEventWithoutHandlersProcessed(t *testing.T) {
	e := newRelayEnv(t, kernel.RelayConfig{})
	evID := e.publish(t, "core.user.created", map[string]any{})

	require.Equal(t, 1, e.runOnce(t))
	require.Equal(t, "processed", e.row(t, evID).Status)
}

func TestRelayRetriesWithBackoffThenSucceeds(t *testing.T) {
	e := newRelayEnv(t, kernel.RelayConfig{
		MaxAttempts: 5,
		Backoff:     func(int) time.Duration { return time.Hour },
	})
	var attempts []int
	e.relay.Subscribe(invoiceCreated, func(_ context.Context, ev modulesdk.Event) error {
		attempts = append(attempts, ev.Attempt)
		if len(attempts) == 1 {
			return errors.New("gangguan sementara")
		}
		return nil
	})
	evID := e.publish(t, invoiceCreated, map[string]any{})

	require.Zero(t, e.runOnce(t), "kegagalan yang dijadwal ulang tidak dihitung selesai")
	r := e.row(t, evID)
	require.Equal(t, "pending", r.Status)
	require.Equal(t, 1, r.Attempts)
	require.NotNil(t, r.LastError)
	require.Contains(t, *r.LastError, "gangguan sementara")
	var farFuture bool
	require.NoError(t, e.db.App.QueryRow(ctx,
		`SELECT available_at > now() + interval '30 minutes' FROM outbox_events WHERE id = $1`, evID.String()).Scan(&farFuture))
	require.True(t, farFuture, "available_at digeser oleh backoff")

	require.Zero(t, e.runOnce(t))
	require.Equal(t, []int{1}, attempts, "belum jatuh tempo: handler tidak dipanggil lagi")

	e.makeDue(t)
	require.Equal(t, 1, e.runOnce(t))
	require.Equal(t, []int{1, 2}, attempts)
	r = e.row(t, evID)
	require.Equal(t, "processed", r.Status)
	require.Nil(t, r.LastError, "last_error dibersihkan setelah sukses")
}

func TestRelayMovesEventToDeadLetterAfterMaxAttempts(t *testing.T) {
	var deadIDs []uuid.UUID
	var deadErrs []error
	e := newRelayEnv(t, kernel.RelayConfig{
		MaxAttempts: 2,
		Backoff:     func(int) time.Duration { return 0 },
		OnDead: func(_ context.Context, ev modulesdk.Event, err error) {
			deadIDs = append(deadIDs, ev.ID)
			deadErrs = append(deadErrs, err)
		},
	})
	calls := 0
	e.relay.Subscribe(invoiceCreated, func(context.Context, modulesdk.Event) error {
		calls++
		return errors.New("selalu gagal")
	})
	evID := e.publish(t, invoiceCreated, map[string]any{})

	require.Zero(t, e.runOnce(t))
	require.Equal(t, 2, calls)
	r := e.row(t, evID)
	require.Equal(t, "dead", r.Status)
	require.Equal(t, 2, r.Attempts)
	require.Contains(t, *r.LastError, "selalu gagal")
	require.Equal(t, []uuid.UUID{evID}, deadIDs, "OnDead tepat sekali")
	require.ErrorContains(t, deadErrs[0], "selalu gagal")

	require.Zero(t, e.runOnce(t))
	require.Equal(t, 2, calls, "dead-letter tidak dicoba lagi")

	stats, err := kernel.ReadOutboxStats(ctx, e.db.App)
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.Dead)
	require.Zero(t, stats.Pending)
}

func TestRelayRollsBackHandlerSideEffectsOfFailedAttempt(t *testing.T) {
	e := newRelayEnv(t, kernel.RelayConfig{MaxAttempts: 5, Backoff: func(int) time.Duration { return 0 }})
	calls := 0
	e.relay.Subscribe(invoiceCreated, func(hctx context.Context, _ modulesdk.Event) error {
		calls++
		if err := insertProbe(hctx, e.db.App, "efek-samping"); err != nil {
			return err
		}
		if calls == 1 {
			return errors.New("gagal setelah menulis")
		}
		return nil
	})
	e.publish(t, invoiceCreated, map[string]any{})

	require.Equal(t, 1, e.runOnce(t))
	require.Equal(t, 2, calls)
	require.Equal(t, 1, countProbe(t, e.db), "efek percobaan gagal dibatalkan; hanya percobaan sukses yang tersisa")
}

func TestRelayTreatsPanicAndTimeoutAsFailures(t *testing.T) {
	e := newRelayEnv(t, kernel.RelayConfig{
		MaxAttempts:    2,
		HandlerTimeout: 50 * time.Millisecond,
		Backoff:        func(int) time.Duration { return 0 },
	})
	e.relay.Subscribe("core.user.created", func(context.Context, modulesdk.Event) error { panic("handler rusak") })
	e.relay.Subscribe("core.user.deleted", func(hctx context.Context, _ modulesdk.Event) error {
		<-hctx.Done()
		return hctx.Err()
	})
	panicID := e.publish(t, "core.user.created", map[string]any{})
	slowID := e.publish(t, "core.user.deleted", map[string]any{})

	require.Zero(t, e.runOnce(t))
	p, s := e.row(t, panicID), e.row(t, slowID)
	require.Equal(t, "dead", p.Status)
	require.Contains(t, *p.LastError, "handler panik")
	require.Equal(t, "dead", s.Status)
	require.Contains(t, *s.LastError, "deadline exceeded")
}

func TestRelayTreatsFailTxByHandlerAsFailure(t *testing.T) {
	e := newRelayEnv(t, kernel.RelayConfig{MaxAttempts: 5, Backoff: func(int) time.Duration { return time.Hour }})
	e.relay.Subscribe(invoiceCreated, func(hctx context.Context, _ modulesdk.Event) error {
		kernel.FailTx(hctx, errBoom) // mis. audit gagal ditulis di dalam handler
		return nil
	})
	evID := e.publish(t, invoiceCreated, map[string]any{})

	require.Zero(t, e.runOnce(t))
	r := e.row(t, evID)
	require.Equal(t, "pending", r.Status, "tidak boleh ditandai processed")
	require.Equal(t, 1, r.Attempts)
	require.Contains(t, *r.LastError, "boom")
}

func TestRelayHandlerPublishIsAtomicWithDelivery(t *testing.T) {
	e := newRelayEnv(t, kernel.RelayConfig{MaxAttempts: 5, Backoff: func(int) time.Duration { return time.Hour }})
	fail := false
	e.relay.Subscribe(invoiceCreated, func(hctx context.Context, ev modulesdk.Event) error {
		e.bus.Publish(hctx, mustEvent(t, "finance.invoice.settled", map[string]any{"from": ev.ID.String()}))
		if fail {
			return errors.New("gagal setelah publish")
		}
		return nil
	})

	// Gagal: event lanjutan ikut di-rollback.
	fail = true
	first := e.publish(t, invoiceCreated, map[string]any{})
	require.Zero(t, e.runOnce(t))
	require.Len(t, outboxRows(t, e.db), 1, "hanya event asli; event lanjutan tidak bocor")
	require.Equal(t, "pending", e.row(t, first).Status)

	// Sukses: event lanjutan tertulis atomik bersama penandaan processed. Karena sudah ter-commit
	// dan jatuh tempo, siklus yang sama ikut memprosesnya (tanpa handler → processed): 2 selesai.
	fail = false
	e.makeDue(t)
	require.Equal(t, 2, e.runOnce(t))
	rows := outboxRows(t, e.db)
	require.Len(t, rows, 2)
	require.Equal(t, "processed", e.row(t, first).Status)
	require.Equal(t, "finance.invoice.settled", rows[1].Name)
	require.Equal(t, "processed", rows[1].Status)
	require.Contains(t, rows[1].Payload, first.String(), "payload event lanjutan memuat id asal")
}

func TestRelaysRunConcurrentlyWithoutDuplicateDelivery(t *testing.T) {
	e := newRelayEnv(t, kernel.RelayConfig{})

	var mu sync.Mutex
	seen := map[uuid.UUID]int{}
	handler := func(_ context.Context, ev modulesdk.Event) error {
		time.Sleep(5 * time.Millisecond) // memberi peluang tumpang-tindih antar relay
		mu.Lock()
		seen[ev.ID]++
		mu.Unlock()
		return nil
	}

	const events = 30
	want := map[uuid.UUID]bool{}
	for i := 0; i < events; i++ {
		want[e.publish(t, invoiceCreated, map[string]any{"i": i})] = true
	}

	var wg sync.WaitGroup
	for r := 0; r < 3; r++ {
		relay := kernel.NewRelay(e.db.App, nil, kernel.RelayConfig{BatchSize: 5})
		relay.Subscribe(invoiceCreated, handler)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				n, err := relay.RunOnce(ctx)
				if err != nil || n == 0 {
					return
				}
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, seen, events)
	for evID, count := range seen {
		require.True(t, want[evID])
		require.Equal(t, 1, count, "event %s terkirim %d kali", evID, count)
	}
	stats, err := kernel.ReadOutboxStats(ctx, e.db.App)
	require.NoError(t, err)
	require.Zero(t, stats.Pending)
}

func TestOutboxStatsReportsDepthDeadAndOldestAge(t *testing.T) {
	e := newRelayEnv(t, kernel.RelayConfig{})

	empty, err := kernel.ReadOutboxStats(ctx, e.db.App)
	require.NoError(t, err)
	require.Equal(t, kernel.OutboxStats{}, empty)

	e.publish(t, invoiceCreated, map[string]any{})
	deadID := e.publish(t, invoiceCreated, map[string]any{})
	_, err = e.db.Migrator.Exec(ctx, `UPDATE outbox_events SET status = 'dead' WHERE id = $1`, deadID.String())
	require.NoError(t, err)

	stats, err := kernel.ReadOutboxStats(ctx, e.db.App)
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.Pending)
	require.EqualValues(t, 1, stats.Dead)
	require.Greater(t, stats.OldestPendingAge, time.Duration(0))
	require.Less(t, stats.OldestPendingAge, time.Minute)
}

func TestRelayRunProcessesEventsUntilContextIsCanceled(t *testing.T) {
	e := newRelayEnv(t, kernel.RelayConfig{PollInterval: 20 * time.Millisecond})
	var mu sync.Mutex
	delivered := 0
	e.relay.Subscribe(invoiceCreated, func(context.Context, modulesdk.Event) error {
		mu.Lock()
		delivered++
		mu.Unlock()
		return nil
	})

	rctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- e.relay.Run(rctx) }()

	e.publish(t, invoiceCreated, map[string]any{})
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return delivered == 1 },
		5*time.Second, 20*time.Millisecond)

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run tidak berhenti setelah ctx dibatalkan")
	}
}

func TestRelayEventBusAdapterSubscribesToRelayAndPublishesToOutbox(t *testing.T) {
	e := newRelayEnv(t, kernel.RelayConfig{})
	bus := e.relay.EventBus(e.bus)

	called := 0
	bus.Subscribe(invoiceCreated, func(context.Context, modulesdk.Event) error { called++; return nil })
	bus.Publish(ctx, mustEvent(t, invoiceCreated, map[string]any{}))

	require.Len(t, outboxRows(t, e.db), 1)
	require.Equal(t, 1, e.runOnce(t))
	require.Equal(t, 1, called)
}
