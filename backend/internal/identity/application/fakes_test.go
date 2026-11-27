package application_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authzdomain "github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/identity/application"
	"github.com/master-abror/zago-core/backend/internal/identity/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	orgdomain "github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

var bg = context.Background()

// trace mengumpulkan urutan panggilan lintas semua fake, sehingga urutan baca, tulis, audit, dan
// event dapat ditegaskan dalam satu daftar.
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

	getErr error

	forUpdateErr error

	updateErr error

	mu sync.Mutex

	access []orgdomain.Access

	userIDs []uuid.UUID

	lastUpdate domain.UpdateInput

	lastQuery domain.ListQuery
}

func (s *fakeStore) note(name string, userID uuid.UUID) {
	s.trace.add(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userIDs = append(s.userIDs, userID)
}

func (s *fakeStore) noteAccess(a orgdomain.Access) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.access = append(s.access, a)
}

func (s *fakeStore) Get(_ context.Context, a orgdomain.Access, userID uuid.UUID) (domain.Record, error) {
	s.note("Get", userID)
	s.noteAccess(a)
	return s.current, s.getErr
}

func (s *fakeStore) GetSelf(_ context.Context, userID uuid.UUID) (domain.Record, error) {
	s.note("GetSelf", userID)
	return s.current, s.getErr
}

func (s *fakeStore) ForUpdate(_ context.Context, a orgdomain.Access, userID uuid.UUID) (domain.Record, error) {
	s.note("ForUpdate", userID)
	s.noteAccess(a)
	return s.current, s.forUpdateErr
}

func (s *fakeStore) ForUpdateSelf(_ context.Context, userID uuid.UUID) (domain.Record, error) {
	s.note("ForUpdateSelf", userID)
	return s.current, s.forUpdateErr
}

func (s *fakeStore) List(_ context.Context, a orgdomain.Access, q domain.ListQuery) (domain.Page, error) {
	s.trace.add("List")
	s.noteAccess(a)
	s.mu.Lock()
	s.lastQuery = q
	s.mu.Unlock()
	return domain.Page{Items: []domain.Record{}}, nil
}

func (s *fakeStore) Update(_ context.Context, a orgdomain.Access, userID uuid.UUID, in domain.UpdateInput) (domain.Record, error) {
	s.note("Update", userID)
	s.noteAccess(a)
	s.mu.Lock()
	s.lastUpdate = in
	s.mu.Unlock()
	return s.updated, s.updateErr
}

func (s *fakeStore) UpdateSelf(_ context.Context, userID uuid.UUID, in domain.UpdateInput) (domain.Record, error) {
	s.note("UpdateSelf", userID)
	s.mu.Lock()
	s.lastUpdate = in
	s.mu.Unlock()
	return s.updated, s.updateErr
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

	audit *fakeAudit

	events *fakeEvents

	tx *countingTx
}

func newEnv(t *testing.T) *env {
	t.Helper()
	tr := &trace{}
	e := &env{
		trace:  tr,
		store:  &fakeStore{trace: tr},
		audit:  &fakeAudit{trace: tr},
		events: &fakeEvents{trace: tr},
		tx:     &countingTx{},
	}
	svc, err := application.NewService(e.tx, e.store, e.audit, e.events)
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

// storedRecord membentuk baris user yang tersimpan dengan id tertentu.
func storedRecord(userID uuid.UUID) domain.Record {
	rec := domain.Record{}
	rec.ID = userID
	rec.DisplayName = "Budi"
	rec.FirstName = strPtr("Budi")
	rec.Locale = "id-ID"
	rec.Timezone = "Asia/Jakarta"
	return rec
}

func strPtr(s string) *string { return &s }
