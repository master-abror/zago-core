package audit_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/audit"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
	"github.com/master-abror/zago-core/backend/pkg/logger"
	"github.com/master-abror/zago-core/backend/pkg/redact"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

var (
	ctx     = context.Background()
	errBoom = errors.New("boom")
)

type sinkCall struct {
	Level, Code, Message string
	Meta                 map[string]any
}

type fakeSink struct {
	mu    sync.Mutex
	calls []sinkCall
}

func (f *fakeSink) Log(_ context.Context, level, code, message string, meta map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, sinkCall{level, code, message, meta})
}

func (f *fakeSink) snapshot() []sinkCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sinkCall(nil), f.calls...)
}

type env struct {
	db   *testpg.DB
	tm   *kernel.TxManager
	rec  *audit.Recorder
	sink *fakeSink
	logs *bytes.Buffer
}

// newEnv menyiapkan database tes + tabel probe "state bisnis" + Recorder dengan sink fake.
func newEnv(t *testing.T, opts ...audit.Option) env {
	t.Helper()
	db := testkit.NewDB(t)
	_, err := db.Migrator.Exec(ctx, `CREATE TABLE audit_state_probe (id uuid PRIMARY KEY, note text NOT NULL)`)
	require.NoError(t, err)
	_, err = db.Migrator.Exec(ctx, `GRANT SELECT, INSERT, UPDATE, DELETE ON audit_state_probe TO app_user`)
	require.NoError(t, err)

	sink := &fakeSink{}
	var logs bytes.Buffer
	log := logger.New(&logs, slog.LevelDebug)
	all := append([]audit.Option{audit.WithSystemLog(sink)}, opts...)
	return env{db: db, tm: kernel.NewTxManager(db.App, log), rec: audit.New(db.App, log, all...), sink: sink, logs: &logs}
}

func (e env) saveState(ctx context.Context, t *testing.T) {
	t.Helper()
	_, err := kernel.DBFrom(ctx, e.db.App).Exec(ctx,
		`INSERT INTO audit_state_probe (id, note) VALUES ($1, 'state')`, id.NewID().String())
	require.NoError(t, err)
}

func (e env) stateCount(t *testing.T) int {
	t.Helper()
	var n int
	require.NoError(t, e.db.App.QueryRow(ctx, `SELECT count(*) FROM audit_state_probe`).Scan(&n))
	return n
}

type activity struct {
	OrgID, ActorID                    *string
	ActorType, Action                 string
	ResourceType, ResourceID          *string
	ScopeType, ScopeID                *string
	Result                            string
	IP, UserAgent, RequestID, TraceID *string
	Metadata                          string
}

func (e env) activities(t *testing.T) []activity {
	t.Helper()
	rs, err := e.db.App.Query(ctx, `
		SELECT organization_id::text, actor_user_id::text, actor_type, action, resource_type, resource_id::text,
		       scope_type, scope_id::text, result, host(ip_address), user_agent, request_id, trace_id, metadata::text
		  FROM activities ORDER BY created_at, id`)
	require.NoError(t, err)
	defer rs.Close()
	var out []activity
	for rs.Next() {
		var a activity
		require.NoError(t, rs.Scan(&a.OrgID, &a.ActorID, &a.ActorType, &a.Action, &a.ResourceType, &a.ResourceID,
			&a.ScopeType, &a.ScopeID, &a.Result, &a.IP, &a.UserAgent, &a.RequestID, &a.TraceID, &a.Metadata))
		out = append(out, a)
	}
	require.NoError(t, rs.Err())
	return out
}

// requestCtx meniru context request yang sudah diautentikasi.
func requestCtx(user, org uuid.UUID) context.Context {
	c := kernel.WithRequestID(ctx, "req-12345678")
	c = kernel.WithTraceID(c, "trace-abc")
	c = kernel.WithActor(c, kernel.Actor{UserID: user, OrganizationID: org})
	c = kernel.WithScope(c, kernel.Scope{Type: "organization", ID: org})
	return kernel.WithClient(c, kernel.Client{IP: "203.0.113.7", UserAgent: "Mozilla/5.0 (tes)"})
}

func TestRecordInTransactionCommitsWithStateAndFillsFieldsFromContext(t *testing.T) {
	e := newEnv(t)
	user, org, rid := id.NewID(), id.NewID(), id.NewID()

	require.NoError(t, e.tm.WithinTx(requestCtx(user, org), func(ctx context.Context) error {
		e.saveState(ctx, t)
		e.rec.Record(ctx, modulesdk.ActivityEntry{
			Action: "invoice.approved", ResourceType: "invoice", ResourceID: rid,
			Result:   modulesdk.ResultSuccess,
			Metadata: map[string]any{"amount": 42, "password": "CANARY-PW", "note": "ok"},
		})
		require.Empty(t, e.activities(t), "belum terlihat sebelum commit")
		return nil
	}))

	got := e.activities(t)
	require.Len(t, got, 1)
	a := got[0]
	require.Equal(t, org.String(), *a.OrgID)
	require.Equal(t, user.String(), *a.ActorID)
	require.Equal(t, "user", a.ActorType)
	require.Equal(t, "invoice.approved", a.Action)
	require.Equal(t, "invoice", *a.ResourceType)
	require.Equal(t, rid.String(), *a.ResourceID)
	require.Equal(t, "organization", *a.ScopeType)
	require.Equal(t, org.String(), *a.ScopeID)
	require.Equal(t, "success", a.Result)
	require.Equal(t, "203.0.113.7", *a.IP)
	require.Equal(t, "Mozilla/5.0 (tes)", *a.UserAgent)
	require.Equal(t, "req-12345678", *a.RequestID)
	require.Equal(t, "trace-abc", *a.TraceID)
	require.JSONEq(t, `{"amount":42,"password":"[REDACTED]","note":"ok"}`, a.Metadata)
	require.NotContains(t, a.Metadata, "CANARY-PW")
	require.Equal(t, 1, e.stateCount(t))
}

func TestRecordRollsBackWithTransaction(t *testing.T) {
	e := newEnv(t)

	err := e.tm.WithinTx(ctx, func(ctx context.Context) error {
		e.saveState(ctx, t)
		e.rec.Record(ctx, modulesdk.ActivityEntry{Action: "invoice.approved"})
		return errBoom
	})
	require.ErrorIs(t, err, errBoom)
	require.Empty(t, e.activities(t), "tidak ada jejak aksi yang tidak pernah terjadi")
	require.Zero(t, e.stateCount(t))
}

func TestDeniedAndFailedAreWrittenInSeparateTransactionAndSurviveRollback(t *testing.T) {
	e := newEnv(t)

	err := e.tm.WithinTx(requestCtx(id.NewID(), id.NewID()), func(ctx context.Context) error {
		e.saveState(ctx, t)
		e.rec.Record(ctx, modulesdk.ActivityEntry{
			Action: "invoice.approve", Result: modulesdk.ResultDenied, Metadata: map[string]any{"field": "salary"},
		})
		e.rec.Record(ctx, modulesdk.ActivityEntry{Action: "invoice.approve", Result: modulesdk.ResultFailed})
		return errBoom
	})
	require.ErrorIs(t, err, errBoom)

	got := e.activities(t)
	require.Len(t, got, 2, "jejak penolakan dan kegagalan tetap ada walau transaksi utama batal")
	require.Equal(t, "denied", got[0].Result)
	require.Equal(t, "failed", got[1].Result)
	require.Equal(t, "req-12345678", *got[0].RequestID)
	require.Zero(t, e.stateCount(t), "state bisnis tetap batal")
}

func TestSuccessWriteFailureFailsTransactionClosed(t *testing.T) {
	e := newEnv(t)
	_, err := e.db.Migrator.Exec(ctx, `DROP TABLE activities`)
	require.NoError(t, err)

	err = e.tm.WithinTx(ctx, func(ctx context.Context) error {
		e.saveState(ctx, t)
		e.rec.Record(ctx, modulesdk.ActivityEntry{Action: "invoice.approved"})
		return nil
	})
	require.ErrorIs(t, err, kernel.ErrTxRollbackOnly)
	require.Zero(t, e.stateCount(t), "aksi tanpa audit tidak boleh ter-commit")

	calls := e.sink.snapshot()
	require.Len(t, calls, 1)
	require.Equal(t, audit.EventWriteFailed, calls[0].Code)
	require.Equal(t, "invoice.approved", calls[0].Meta["action"])
}

func TestDeniedWriteFailureIsReportedButDoesNotFailTransaction(t *testing.T) {
	e := newEnv(t)
	_, err := e.db.Migrator.Exec(ctx, `DROP TABLE activities`)
	require.NoError(t, err)

	err = e.tm.WithinTx(ctx, func(ctx context.Context) error {
		e.saveState(ctx, t)
		e.rec.Record(ctx, modulesdk.ActivityEntry{Action: "invoice.approve", Result: modulesdk.ResultDenied})
		return nil
	})
	require.NoError(t, err, "penolakan yang gagal dicatat tidak membatalkan transaksi lain")
	require.Equal(t, 1, e.stateCount(t))
	require.Len(t, e.sink.snapshot(), 1)
	require.Contains(t, e.logs.String(), "activity gagal direkam")
}

func TestRecordOutsideTransactionWritesImmediately(t *testing.T) {
	e := newEnv(t)
	e.rec.Record(ctx, modulesdk.ActivityEntry{Action: "auth.login", Result: modulesdk.ResultSuccess})
	require.Len(t, e.activities(t), 1)
}

func TestDefaultsWithoutActorScopeOrClient(t *testing.T) {
	e := newEnv(t)
	e.rec.Record(ctx, modulesdk.ActivityEntry{Action: "system.cleanup"})

	got := e.activities(t)
	require.Len(t, got, 1)
	a := got[0]
	require.Equal(t, "system", a.ActorType)
	require.Nil(t, a.ActorID)
	require.Nil(t, a.OrgID)
	require.Nil(t, a.ScopeType)
	require.Nil(t, a.ScopeID)
	require.Nil(t, a.IP)
	require.Nil(t, a.UserAgent)
	require.Nil(t, a.ResourceType)
	require.Nil(t, a.ResourceID)
	require.Equal(t, "success", a.Result, "Result kosong → success")
	require.JSONEq(t, `{}`, a.Metadata)
}

func TestActorTypesAndInvalidClientIP(t *testing.T) {
	e := newEnv(t)
	svc := kernel.WithActor(ctx, kernel.Actor{Type: kernel.ActorService})
	e.rec.Record(svc, modulesdk.ActivityEntry{Action: "integration.sync"})

	badIP := kernel.WithClient(ctx, kernel.Client{IP: "bukan-ip", UserAgent: strings.Repeat("u", 600)})
	e.rec.Record(badIP, modulesdk.ActivityEntry{Action: "auth.login"})

	got := e.activities(t)
	require.Len(t, got, 2)
	require.Equal(t, "service", got[0].ActorType)
	require.Nil(t, got[1].IP, "IP tak valid → NULL, baris tetap tertulis")
	require.Len(t, *got[1].UserAgent, 512, "user agent dibatasi")
}

func TestInvalidEntriesAreRejectedLoudly(t *testing.T) {
	e := newEnv(t)
	valid := func() modulesdk.ActivityEntry { return modulesdk.ActivityEntry{Action: "invoice.approved"} }
	with := func(mut func(*modulesdk.ActivityEntry)) modulesdk.ActivityEntry { v := valid(); mut(&v); return v }

	for name, entry := range map[string]modulesdk.ActivityEntry{
		"action kosong":          with(func(v *modulesdk.ActivityEntry) { v.Action = "" }),
		"action terlalu panjang": with(func(v *modulesdk.ActivityEntry) { v.Action = strings.Repeat("a", 151) }),
		"resource type panjang":  with(func(v *modulesdk.ActivityEntry) { v.ResourceType = strings.Repeat("r", 151) }),
		"result tak dikenal":     with(func(v *modulesdk.ActivityEntry) { v.Result = "maybe" }),
		"metadata tak terenkode": with(func(v *modulesdk.ActivityEntry) { v.Metadata = map[string]any{"c": make(chan int)} }),
	} {
		err := e.tm.WithinTx(ctx, func(ctx context.Context) error {
			e.saveState(ctx, t)
			e.rec.Record(ctx, entry)
			return nil
		})
		require.ErrorIs(t, err, kernel.ErrTxRollbackOnly, name)

		require.NotPanics(t, func() { e.rec.Record(ctx, entry) }, name) // di luar transaksi: dicatat, tak ditulis
	}

	bad := kernel.WithActor(ctx, kernel.Actor{Type: "alien"})
	err := e.tm.WithinTx(bad, func(ctx context.Context) error {
		e.rec.Record(ctx, valid())
		return nil
	})
	require.ErrorIs(t, err, kernel.ErrTxRollbackOnly)

	require.Empty(t, e.activities(t))
	require.Zero(t, e.stateCount(t))
}

func TestOversizedMetadataIsReplacedByMarker(t *testing.T) {
	e := newEnv(t)
	e.rec.Record(ctx, modulesdk.ActivityEntry{
		Action: "import.finished", Metadata: map[string]any{"rows": strings.Repeat("z", 40_000)},
	})
	got := e.activities(t)
	require.Len(t, got, 1)
	require.NotContains(t, got[0].Metadata, "zzzz")
	require.Contains(t, got[0].Metadata, "_metadata_truncated")
}

func TestModuleDeclaredSensitiveKeysAreRedacted(t *testing.T) {
	e := newEnv(t, audit.WithRedactor(redact.New("national_id")))
	e.rec.Record(ctx, modulesdk.ActivityEntry{
		Action: "employee.updated", Metadata: map[string]any{"national_id": "CANARY-NIK", "name": "Budi"},
	})
	got := e.activities(t)
	require.Len(t, got, 1)
	require.NotContains(t, got[0].Metadata, "CANARY-NIK")
	require.Contains(t, got[0].Metadata, "Budi")
}

func TestRequestIDFlowsIntoActivityAndFailureLog(t *testing.T) {
	e := newEnv(t)
	rctx := kernel.WithRequestID(ctx, "req-corr-0001")

	e.rec.Record(rctx, modulesdk.ActivityEntry{Action: "auth.login"})
	e.rec.Record(rctx, modulesdk.ActivityEntry{}) // entri tak valid → log error membawa request_id

	got := e.activities(t)
	require.Len(t, got, 1)
	require.Equal(t, "req-corr-0001", *got[0].RequestID)
	require.Contains(t, e.logs.String(), `"request_id":"req-corr-0001"`)
}
