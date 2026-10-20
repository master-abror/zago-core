package kernel_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

type outboxRow struct {
	ID        string
	Name      string
	Status    string
	Attempts  int
	Payload   string
	OrgID     *string
	AggType   *string
	AggID     *string
	RequestID *string
	TraceID   *string
	LastError *string
}

func outboxRows(t *testing.T, db *testpg.DB) []outboxRow {
	t.Helper()
	rows, err := db.App.Query(ctx, `
		SELECT id::text, event_name, status, attempts, payload::text, organization_id::text,
		       aggregate_type, aggregate_id::text, request_id, trace_id, last_error
		  FROM outbox_events ORDER BY created_at, id`)
	require.NoError(t, err)
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var r outboxRow
		require.NoError(t, rows.Scan(&r.ID, &r.Name, &r.Status, &r.Attempts, &r.Payload, &r.OrgID,
			&r.AggType, &r.AggID, &r.RequestID, &r.TraceID, &r.LastError))
		out = append(out, r)
	}
	require.NoError(t, rows.Err())
	return out
}

func mustEvent(t *testing.T, name string, payload any) modulesdk.Event {
	t.Helper()
	ev, err := modulesdk.NewEvent(name, payload)
	require.NoError(t, err)
	return ev
}

func TestPublishInTxWritesOutboxRowAtomicallyWithState(t *testing.T) {
	db, tm := probeDB(t)
	bus := kernel.NewBus(db.App, nil)
	org, agg := id.NewID(), id.NewID()
	rctx := kernel.WithTraceID(kernel.WithRequestID(ctx, "req-12345678"), "trace-abc")

	require.NoError(t, tm.WithinTx(rctx, func(ctx context.Context) error {
		require.NoError(t, insertProbe(ctx, db.App, "state"))
		ev := mustEvent(t, "finance.invoice.approved", map[string]any{"amount": 42})
		ev.OrganizationID, ev.AggregateType, ev.AggregateID = org, "invoice", agg
		bus.Publish(ctx, ev)
		require.Empty(t, outboxRows(t, db), "event belum terlihat sebelum commit")
		return nil
	}))

	rows := outboxRows(t, db)
	require.Len(t, rows, 1)
	r := rows[0]
	require.Equal(t, "finance.invoice.approved", r.Name)
	require.Equal(t, "pending", r.Status)
	require.Zero(t, r.Attempts)
	require.JSONEq(t, `{"amount":42}`, r.Payload)
	require.Equal(t, org.String(), *r.OrgID)
	require.Equal(t, "invoice", *r.AggType)
	require.Equal(t, agg.String(), *r.AggID)
	require.Equal(t, "req-12345678", *r.RequestID, "request id mengalir dari context")
	require.Equal(t, "trace-abc", *r.TraceID, "trace id mengalir dari context")
	require.Equal(t, 1, countProbe(t, db))
}

func TestPublishRollsBackWithTransaction(t *testing.T) {
	db, tm := probeDB(t)
	bus := kernel.NewBus(db.App, nil)

	err := tm.WithinTx(ctx, func(ctx context.Context) error {
		require.NoError(t, insertProbe(ctx, db.App, "state"))
		bus.Publish(ctx, mustEvent(t, "finance.invoice.approved", map[string]any{}))
		return errBoom
	})
	require.ErrorIs(t, err, errBoom)
	require.Empty(t, outboxRows(t, db))
	require.Zero(t, countProbe(t, db))
}

func TestPublishGeneratesIDAndKeepsCallerSuppliedID(t *testing.T) {
	db, _ := probeDB(t)
	bus := kernel.NewBus(db.App, nil)

	bus.Publish(ctx, mustEvent(t, "core.user.created", map[string]any{}))
	fixed := id.NewID()
	ev := mustEvent(t, "core.user.created", map[string]any{})
	ev.ID = fixed
	bus.Publish(ctx, ev)

	rows := outboxRows(t, db)
	require.Len(t, rows, 2)
	generated, err := uuid.Parse(rows[0].ID)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, generated)
	require.Equal(t, fixed.String(), rows[1].ID)
	require.Nil(t, rows[0].OrgID, "tanpa organisasi → NULL")
	require.JSONEq(t, `{}`, rows[0].Payload)
}

func TestPublishWithoutTransactionIsDurableAutocommit(t *testing.T) {
	db, _ := probeDB(t)
	bus := kernel.NewBus(db.App, nil)

	var delivered []modulesdk.Event
	bus.Subscribe("core.role.changed", func(_ context.Context, ev modulesdk.Event) error {
		delivered = append(delivered, ev)
		return nil
	})
	ev := mustEvent(t, "core.role.changed", map[string]any{"role": "admin"})
	bus.Publish(ctx, ev)

	require.Len(t, outboxRows(t, db), 1)
	require.Len(t, delivered, 1, "handler in-process langsung jalan setelah tulis outbox berhasil")
	require.NotEqual(t, uuid.Nil, delivered[0].ID)
	require.JSONEq(t, `{"role":"admin"}`, string(delivered[0].Payload))
}

func TestPublishAcceptsTwoSegmentEventNames(t *testing.T) {
	db, _ := probeDB(t)
	bus := kernel.NewBus(db.App, nil)
	bus.Publish(ctx, mustEvent(t, "role_permissions.changed", map[string]any{}))
	require.Len(t, outboxRows(t, db), 1)
}

func TestInvalidEventMakesTransactionRollbackOnly(t *testing.T) {
	db, tm := probeDB(t)
	bus := kernel.NewBus(db.App, nil)

	valid := func() modulesdk.Event { return mustEvent(t, "finance.invoice.approved", map[string]any{}) }
	withName := func(n string) modulesdk.Event { e := valid(); e.Name = n; return e }
	withPayload := func(p string) modulesdk.Event { e := valid(); e.Payload = []byte(p); return e }

	for name, ev := range map[string]modulesdk.Event{
		"nama kosong":       withName(""),
		"huruf besar":       withName("Finance.Invoice.Approved"),
		"satu segmen":       withName("approved"),
		"spasi":             withName("finance.invoice approved"),
		"terlalu panjang":   withName(strings.Repeat("a", 201) + ".x.y"),
		"payload array":     withPayload(`[1,2]`),
		"payload string":    withPayload(`"teks"`),
		"payload rusak":     withPayload(`{bad`),
		"payload terpotong": withPayload(`{"a":`),
	} {
		err := tm.WithinTx(ctx, func(ctx context.Context) error {
			require.NoError(t, insertProbe(ctx, db.App, "state"))
			bus.Publish(ctx, ev)
			return nil // pemanggil tak melihat error: Publish tak mengembalikannya
		})
		require.ErrorIs(t, err, kernel.ErrTxRollbackOnly, name)
	}
	require.Empty(t, outboxRows(t, db))
	require.Zero(t, countProbe(t, db), "state ikut batal (fail-closed)")
}

func TestInvalidEventOutsideTransactionIsDroppedLoudlyNotSilentlyStored(t *testing.T) {
	db, _ := probeDB(t)
	bus := kernel.NewBus(db.App, nil)
	require.NotPanics(t, func() { bus.Publish(ctx, modulesdk.Event{Name: "BAD"}) })
	require.Empty(t, outboxRows(t, db))
}

func TestOutboxWriteFailureFailsTransactionClosed(t *testing.T) {
	db, tm := probeDB(t)
	bus := kernel.NewBus(db.App, nil)
	_, err := db.Migrator.Exec(ctx, `DROP TABLE outbox_events`)
	require.NoError(t, err)

	err = tm.WithinTx(ctx, func(ctx context.Context) error {
		require.NoError(t, insertProbe(ctx, db.App, "state"))
		bus.Publish(ctx, mustEvent(t, "finance.invoice.approved", map[string]any{}))
		return nil
	})
	require.ErrorIs(t, err, kernel.ErrTxRollbackOnly)
	require.Zero(t, countProbe(t, db), "gagal menulis outbox membatalkan perubahan state")
}

func TestLocalHandlersRunAfterCommitDetachedFromTransaction(t *testing.T) {
	db, tm := probeDB(t)
	bus := kernel.NewBus(db.App, nil)

	var sawInTx, sawCommitted []bool
	bus.Subscribe("finance.invoice.approved", func(hctx context.Context, _ modulesdk.Event) error {
		sawInTx = append(sawInTx, kernel.InTx(hctx))
		sawCommitted = append(sawCommitted, countProbe(t, db) == 1)
		return nil
	})

	// Rollback: handler tidak pernah jalan.
	_ = tm.WithinTx(ctx, func(ctx context.Context) error {
		require.NoError(t, insertProbe(ctx, db.App, "state"))
		bus.Publish(ctx, mustEvent(t, "finance.invoice.approved", map[string]any{}))
		return errBoom
	})
	require.Empty(t, sawInTx)

	require.NoError(t, tm.WithinTx(ctx, func(ctx context.Context) error {
		require.NoError(t, insertProbe(ctx, db.App, "state"))
		bus.Publish(ctx, mustEvent(t, "finance.invoice.approved", map[string]any{}))
		require.Empty(t, sawInTx, "belum jalan sebelum commit")
		return nil
	}))
	require.Equal(t, []bool{false}, sawInTx, "ctx handler tidak lagi membawa transaksi")
	require.Equal(t, []bool{true}, sawCommitted, "handler melihat state yang sudah ter-commit")
}

func TestLocalHandlerFailureOrPanicNeverBreaksPublish(t *testing.T) {
	db, tm := probeDB(t)
	bus := kernel.NewBus(db.App, nil)

	ran := 0
	bus.Subscribe("core.role.changed", func(context.Context, modulesdk.Event) error { return errors.New("gagal") })
	bus.Subscribe("core.role.changed", func(context.Context, modulesdk.Event) error { panic("rusak") })
	bus.Subscribe("core.role.changed", func(context.Context, modulesdk.Event) error { ran++; return nil })

	require.NoError(t, tm.WithinTx(ctx, func(ctx context.Context) error {
		require.NoError(t, insertProbe(ctx, db.App, "state"))
		bus.Publish(ctx, mustEvent(t, "core.role.changed", map[string]any{}))
		return nil
	}))
	require.Equal(t, 1, ran, "handler berikutnya tetap jalan")
	require.Equal(t, 1, countProbe(t, db))
	require.Len(t, outboxRows(t, db), 1)
}
