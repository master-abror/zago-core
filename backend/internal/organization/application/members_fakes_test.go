package application_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/organization/application"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// memberFakeStore memenuhi application.MemberStore dan mencatat urutan panggilan ke trace yang
// sama dengan fake lain, sehingga urutan kunci, tulis, invalidasi, audit, dan event dapat
// ditegaskan dalam satu daftar.
type memberFakeStore struct {
	trace *trace

	member domain.Member

	groups int

	getErr error

	addErr error

	removeErr error

	groupsErr error

	mu sync.Mutex

	access []domain.Access
}

func (s *memberFakeStore) note(name string, a domain.Access) {
	s.trace.add(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.access = append(s.access, a)
}

func (s *memberFakeStore) Get(_ context.Context, a domain.Access, _, _ uuid.UUID) (domain.Member, error) {
	s.note("Member.Get", a)
	return s.member, s.getErr
}

func (s *memberFakeStore) List(_ context.Context, a domain.Access, _ uuid.UUID, _ domain.MemberListQuery) (domain.MemberPage, error) {
	s.note("Member.List", a)
	return domain.MemberPage{Items: []domain.Member{}}, nil
}

func (s *memberFakeStore) Add(_ context.Context, a domain.Access, _, _ uuid.UUID, _ *uuid.UUID) (domain.Member, error) {
	s.note("Member.Add", a)
	if s.addErr != nil {
		return domain.Member{}, s.addErr
	}
	return s.member, nil
}

func (s *memberFakeStore) Remove(_ context.Context, a domain.Access, _, _ uuid.UUID) (domain.Member, error) {
	s.note("Member.Remove", a)
	if s.removeErr != nil {
		return domain.Member{}, s.removeErr
	}
	rec := s.member
	rec.Status = domain.MemberRemoved
	return rec, nil
}

func (s *memberFakeStore) RemoveFromGroups(_ context.Context, a domain.Access, _, _ uuid.UUID) (int, error) {
	s.note("Member.RemoveFromGroups", a)
	return s.groups, s.groupsErr
}

// memberFakeOrgs memenuhi application.OrganizationReader.
type memberFakeOrgs struct {
	trace *trace

	rec domain.Record

	err error
}

func (o *memberFakeOrgs) Get(_ context.Context, _ domain.Access, _ uuid.UUID) (domain.Record, error) {
	o.trace.add("Org.Get")
	return o.rec, o.err
}

// memberFakeRevoker memenuhi application.AssignmentRevoker.
type memberFakeRevoker struct {
	trace *trace

	n int

	err error
}

func (r *memberFakeRevoker) RevokeUserInOrganization(_ context.Context, _, _ uuid.UUID) (int, error) {
	r.trace.add("Revoke")
	return r.n, r.err
}

// memberEnv merakit MemberService di atas fake yang berbagi satu trace.
type memberEnv struct {
	svc *application.MemberService

	trace *trace

	store *memberFakeStore

	orgs *memberFakeOrgs

	revoker *memberFakeRevoker

	locker *fakeLocker

	audit *fakeAudit

	events *fakeEvents

	inv *fakeInvalidator

	tx *countingTx

	orgID uuid.UUID

	userID uuid.UUID
}

// newMemberEnv membentuk env dengan organisasi aktif dan satu anggota aktif untuk orgID dan userID.
func newMemberEnv(t *testing.T, withInvalidator bool) *memberEnv {
	t.Helper()
	e := &memberEnv{}
	e.orgID = id.NewID()
	e.userID = id.NewID()
	e.trace = &trace{}
	e.store = &memberFakeStore{trace: e.trace}
	e.orgs = &memberFakeOrgs{trace: e.trace}
	e.revoker = &memberFakeRevoker{trace: e.trace}
	e.locker = &fakeLocker{trace: e.trace}
	e.audit = &fakeAudit{trace: e.trace}
	e.events = &fakeEvents{trace: e.trace}
	e.inv = &fakeInvalidator{trace: e.trace}
	e.tx = &countingTx{}
	e.store.member = domain.Member{ID: id.NewID(), OrganizationID: e.orgID, UserID: e.userID, Status: domain.MemberActive}
	e.orgs.rec = activeRecord(t, e.orgID)
	var inv application.TxInvalidator
	if withInvalidator {
		inv = e.inv
	}
	svc, err := application.NewMemberService(e.tx, e.store, e.orgs, e.revoker, e.locker, e.audit, e.events, inv)
	require.NoError(t, err)
	e.svc = svc
	return e
}

// lockStep adalah entri trace untuk kunci advisory organisasi env ini.
func (e *memberEnv) lockStep() string {
	return "LockOrganization:" + e.orgID.String()
}

// invalidateStep adalah entri trace untuk invalidasi cache organisasi env ini.
func (e *memberEnv) invalidateStep() string {
	return "Invalidate:" + e.orgID.String()
}
