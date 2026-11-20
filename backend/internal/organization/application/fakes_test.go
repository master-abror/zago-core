package application_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/cache"
	authzdomain "github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/organization/application"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

var bg = context.Background()

// trace mengumpulkan urutan panggilan lintas semua fake, sehingga urutan kunci, tulis,
// invalidasi, audit, dan event dapat ditegaskan dalam satu daftar.
type trace struct {
	mu    sync.Mutex
	calls []string
}

func (t *trace) add(s string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.calls = append(t.calls, s)
}

func (t *trace) snapshot() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string{}, t.calls...)
}

type countingTx struct {
	mu sync.Mutex
	n  int
}

func (c *countingTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return fn(ctx)
}

func (c *countingTx) entered() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

type fakeStore struct {
	trace *trace

	current domain.Record

	updated domain.Record

	forUpdateErr error

	mu sync.Mutex

	access []domain.Access

	lastUpdate domain.UpdateInput
}

func (s *fakeStore) note(name string, a domain.Access) {
	s.trace.add(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.access = append(s.access, a)
}

func (s *fakeStore) Get(_ context.Context, a domain.Access, _ uuid.UUID) (domain.Record, error) {
	s.note("Get", a)
	return s.current, nil
}

func (s *fakeStore) List(_ context.Context, a domain.Access, _ domain.ListQuery) (domain.Page, error) {
	s.note("List", a)
	return domain.Page{Items: []domain.Record{}}, nil
}

func (s *fakeStore) Insert(_ context.Context, a domain.Access, rec domain.Record) (domain.Record, error) {
	s.note("Insert", a)
	return rec, nil
}

func (s *fakeStore) ForUpdate(_ context.Context, a domain.Access, _ uuid.UUID) (domain.Record, error) {
	s.note("ForUpdate", a)
	return s.current, s.forUpdateErr
}

func (s *fakeStore) Update(_ context.Context, a domain.Access, _ uuid.UUID, in domain.UpdateInput) (domain.Record, error) {
	s.note("Update", a)
	s.lastUpdate = in
	return s.updated, nil
}

func (s *fakeStore) SetStatus(_ context.Context, a domain.Access, _ uuid.UUID, to string) (domain.Record, error) {
	s.note("SetStatus:"+to, a)
	rec := s.current
	rec.Status = to
	return rec, nil
}

type fakeLocker struct {
	trace *trace

	err error
}

func (l *fakeLocker) LockOrganization(_ context.Context, organizationID uuid.UUID) error {
	l.trace.add("LockOrganization:" + organizationID.String())
	return l.err
}

type fakeInvalidator struct {
	trace *trace

	err error
}

func (i *fakeInvalidator) InvalidateInTx(_ context.Context, t cache.Targets) error {
	for _, org := range t.Organizations {
		i.trace.add("Invalidate:" + org.String())
	}
	return i.err
}

type fakeAudit struct {
	trace *trace

	mu sync.Mutex

	entries []modulesdk.ActivityEntry

	actors []kernel.Actor
}

func (a *fakeAudit) Record(ctx context.Context, entry modulesdk.ActivityEntry) {
	a.trace.add("Audit:" + entry.Action)
	actor, _ := kernel.ActorFromContext(ctx)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, entry)
	a.actors = append(a.actors, actor)
}

func (a *fakeAudit) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.entries)
}

type fakeEvents struct {
	trace *trace

	mu sync.Mutex

	events []modulesdk.Event
}

func (e *fakeEvents) Publish(_ context.Context, ev modulesdk.Event) {
	e.trace.add("Event:" + ev.Name)
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
}

func (e *fakeEvents) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.events)
}

// env merakit Service di atas fake yang berbagi satu trace.
type env struct {
	svc *application.Service

	trace *trace

	store *fakeStore

	locker *fakeLocker

	audit *fakeAudit

	events *fakeEvents

	inv *fakeInvalidator

	tx *countingTx
}

func newEnv(t *testing.T, withInvalidator bool) *env {
	t.Helper()
	tr := &trace{}
	e := &env{
		trace:  tr,
		store:  &fakeStore{trace: tr},
		locker: &fakeLocker{trace: tr},
		audit:  &fakeAudit{trace: tr},
		events: &fakeEvents{trace: tr},
		inv:    &fakeInvalidator{trace: tr},
		tx:     &countingTx{},
	}
	var inv application.TxInvalidator
	if withInvalidator {
		inv = e.inv
	}
	svc, err := application.NewService(e.tx, e.store, e.locker, e.audit, e.events, inv)
	require.NoError(t, err)
	e.svc = svc
	return e
}

func platformCaller() application.Caller {
	return application.Caller{UserID: id.NewID()}
}

func tenantCaller(t *testing.T, org uuid.UUID) application.Caller {
	t.Helper()
	scope, err := authzdomain.NewScope(&org, nil)
	require.NoError(t, err)
	return application.Caller{UserID: id.NewID(), Scope: scope}
}

// activeRecord membentuk organisasi aktif dengan id tertentu.
func activeRecord(t *testing.T, organizationID uuid.UUID) domain.Record {
	t.Helper()
	rec, err := domain.NewRecord(organizationID, domain.CreateInput{Name: "Acme", Slug: "acme"})
	require.NoError(t, err)
	return rec
}

func strPtr(s string) *string { return &s }
