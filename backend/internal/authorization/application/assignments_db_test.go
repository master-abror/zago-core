package application_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/audit"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes AssignmentWriter memakai PostgreSQL 18 sungguhan dan role app_user. Pemberi tenant
// memegang awHeld di level organisasi; awBeyond terdaftar di katalog tetapi TIDAK dipegang.

var _ application.AssignmentStore = (*repository.Assignments)(nil)

var awHeld = []string{"role.assign", "invoice.read", "invoice.approve"}

const awBeyond = "invoice.delete"

type awEnv struct {
	db     *testpg.DB
	org    testkit.Organization
	admin  testkit.User
	member testkit.User
	group  testkit.Group
	role   testkit.Role
	beyond testkit.Role
	grant  application.Grantor
	inv    *rwInvalidator
	writer *application.AssignmentWriter
}

func awWriter(t *testing.T, db *testpg.DB, inv application.TxInvalidator) *application.AssignmentWriter {
	t.Helper()
	w, err := application.NewAssignmentWriter(kernel.NewTxManager(db.App, nil), repository.NewAssignments(db.App),
		audit.New(db.App, nil), kernel.NewBus(db.App, nil), inv)
	require.NoError(t, err)
	return w
}

func awAssignOrg(t *testing.T, db *testpg.DB, role testkit.Role, user, org uuid.UUID) testkit.Assignment {
	t.Helper()
	return testkit.AssignRole(t, db, testkit.AssignmentSpec{Role: role, SubjectType: "user", SubjectID: user, ScopeType: "organization", OrganizationID: &org})
}

// awSystemRole membuat role sistem dengan slug tertentu (database tes baru tidak punya role
// sistem bawaan, jadi slug tidak bentrok).
func awSystemRole(t *testing.T, db *testpg.DB, slug string, codes ...string) testkit.Role {
	t.Helper()
	r := testkit.RoleWith(t, db, codes)
	_, err := db.Migrator.Exec(ctx, `UPDATE roles SET slug = $2 WHERE id = $1`, r.ID, slug)
	require.NoError(t, err)
	r.Slug = slug
	return r
}

func newAWEnv(t *testing.T) awEnv {
	t.Helper()
	var e awEnv
	e.db = testkit.NewDB(t)
	e.org = testkit.NewOrganization(t, e.db)
	e.admin = testkit.NewUser(t, e.db, testkit.InOrganization(e.org))
	e.member = testkit.NewUser(t, e.db, testkit.InOrganization(e.org))
	e.group = testkit.NewGroup(t, e.db, e.org, e.admin)
	awAssignOrg(t, e.db, testkit.RoleWith(t, e.db, awHeld), e.admin.ID, e.org.ID)
	e.role = testkit.RoleWith(t, e.db, []string{"invoice.read"}, testkit.RoleInOrganization(e.org))
	e.beyond = testkit.RoleWith(t, e.db, []string{awBeyond}, testkit.RoleInOrganization(e.org))
	scopeOrg := e.org.ID
	e.grant = application.Grantor{UserID: e.admin.ID, Scope: domain.ScopeContext{OrganizationID: &scopeOrg}}
	e.inv = &rwInvalidator{}
	e.writer = awWriter(t, e.db, e.inv)
	return e
}

func (e awEnv) input(subject uuid.UUID) application.CreateAssignmentInput {
	return application.CreateAssignmentInput{RoleID: e.role.ID, SubjectType: domain.SubjectUser, SubjectID: subject, Scope: domain.OrganizationTarget(e.org.ID)}
}

func (e awEnv) rows(t *testing.T, roleID uuid.UUID) int {
	t.Helper()
	return count(t, e.db, `SELECT count(*) FROM role_assignments WHERE role_id = $1 AND revoked_at IS NULL`, roleID)
}

func awActs(t *testing.T, db *testpg.DB, action, result string) int {
	t.Helper()
	return count(t, db, `SELECT count(*) FROM activities WHERE action = $1 AND result = $2`, action, result)
}

func awEvts(t *testing.T, db *testpg.DB, name string) int {
	t.Helper()
	return count(t, db, `SELECT count(*) FROM outbox_events WHERE event_name = $1`, name)
}

func awExec(t *testing.T, db *testpg.DB, sql string, args ...any) {
	t.Helper()
	_, err := db.Migrator.Exec(ctx, sql, args...)
	require.NoError(t, err)
}

// awHold mengambil kunci di transaksi terpisah dan menahannya sampai fungsi yang dikembalikan
// dipanggil.
func awHold(t *testing.T, db *testpg.DB, lock func(context.Context) error) func() {
	t.Helper()
	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- kernel.NewTxManager(db.App, nil).WithinTx(ctx, func(txc context.Context) error {
			if err := lock(txc); err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	select {
	case <-held:
	case err := <-done:
		require.FailNow(t, "kunci tidak terambil", "%v", err)
	}
	return func() {
		close(release)
		require.NoError(t, <-done)
	}
}

// awStaysBlocked memastikan operasi belum selesai selama kunci ditahan.
func awStaysBlocked(t *testing.T, done <-chan error, what string) {
	t.Helper()
	select {
	case err := <-done:
		require.FailNow(t, what+" selesai padahal kunci ditahan", "%v", err)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestAssignmentWriterRequiresItsDependencies(t *testing.T) {
	db := testkit.NewDB(t)
	tx := kernel.NewTxManager(db.App, nil)
	store := repository.NewAssignments(db.App)
	rec := audit.New(db.App, nil)
	bus := kernel.NewBus(db.App, nil)

	_, err := application.NewAssignmentWriter(nil, store, rec, bus, nil)
	require.Error(t, err)
	_, err = application.NewAssignmentWriter(tx, nil, rec, bus, nil)
	require.Error(t, err)
	_, err = application.NewAssignmentWriter(tx, store, nil, bus, nil)
	require.Error(t, err)
	_, err = application.NewAssignmentWriter(tx, store, rec, nil, nil)
	require.Error(t, err)
	_, err = application.NewAssignmentWriter(tx, store, rec, bus, nil)
	require.NoError(t, err, "invalidator boleh nil (tanpa Redis)")
}

func TestAssignmentWriterCreatesAnOrganizationAssignment(t *testing.T) {
	e := newAWEnv(t)

	res, err := e.writer.Create(ctx, e.grant, e.input(e.member.ID))
	require.NoError(t, err)
	require.Equal(t, e.role.ID, res.Role.ID)
	require.Equal(t, domain.OrganizationTarget(e.org.ID), res.Assignment.Scope)
	require.False(t, res.Assignment.CreatedAt.IsZero())
	require.NotNil(t, res.Assignment.AssignedBy)
	require.Equal(t, e.admin.ID, *res.Assignment.AssignedBy)

	require.Equal(t, 1, count(t, e.db,
		`SELECT count(*) FROM role_assignments WHERE id = $1 AND scope_id IS NULL AND assigned_by = $2 AND subject_id = $3 AND revoked_at IS NULL`,
		res.Assignment.ID, e.admin.ID, e.member.ID))
	require.Equal(t, 1, count(t, e.db,
		`SELECT count(*) FROM activities WHERE action = 'role_assignment.create' AND result = 'success' AND resource_type = 'role_assignment' AND resource_id = $1 AND organization_id = $2`,
		res.Assignment.ID, e.org.ID))
	require.Equal(t, 1, count(t, e.db,
		`SELECT count(*) FROM outbox_events WHERE event_name = 'authz.role_assignment.created' AND aggregate_id = $1 AND organization_id = $2`,
		res.Assignment.ID, e.org.ID))
	require.Equal(t, 1, e.inv.calls())
	require.Equal(t, []uuid.UUID{e.org.ID}, e.inv.last().Organizations)
	require.False(t, e.inv.last().Platform)
}

func TestAssignmentWriterWorksWithoutACache(t *testing.T) {
	e := newAWEnv(t)
	w := awWriter(t, e.db, nil)

	_, err := w.Create(ctx, e.grant, e.input(e.member.ID))
	require.NoError(t, err)
	require.Equal(t, 1, e.rows(t, e.role.ID))
	require.Equal(t, 0, e.inv.calls())
}

func TestAssignmentWriterCreatesGroupScopedAndGroupSubjectAssignments(t *testing.T) {
	e := newAWEnv(t)

	inGroup := e.input(e.member.ID)
	inGroup.Scope = domain.GroupTarget(e.org.ID, e.group.ID)
	res, err := e.writer.Create(ctx, e.grant, inGroup)
	require.NoError(t, err)
	require.Equal(t, 1, count(t, e.db,
		`SELECT count(*) FROM role_assignments WHERE id = $1 AND scope_type = 'group' AND scope_id = $2`, res.Assignment.ID, e.group.ID))

	forGroup := e.input(e.group.ID)
	forGroup.SubjectType = domain.SubjectGroup
	_, err = e.writer.Create(ctx, e.grant, forGroup)
	require.NoError(t, err)

	forOrg := e.input(e.org.ID)
	forOrg.SubjectType = domain.SubjectOrganization
	_, err = e.writer.Create(ctx, e.grant, forOrg)
	require.NoError(t, err)

	require.Equal(t, 3, e.rows(t, e.role.ID))
	require.Equal(t, 3, e.inv.calls())
}

func TestAssignmentWriterRefusesToGiveWhatTheGrantorDoesNotHold(t *testing.T) {
	e := newAWEnv(t)
	in := e.input(e.member.ID)
	in.RoleID = e.beyond.ID

	_, err := e.writer.Create(ctx, e.grant, in)
	require.ErrorIs(t, err, domain.ErrAuthorityCeiling)
	var ce *domain.CeilingError
	require.True(t, errors.As(err, &ce))
	require.Equal(t, []string{awBeyond}, ce.Missing)

	require.Equal(t, 0, e.rows(t, e.beyond.ID), "tidak ada yang ditulis")
	require.Equal(t, 1, awActs(t, e.db, "role_assignment.create", "denied"), "penolakan diaudit di transaksi terpisah")
	require.Equal(t, 0, awActs(t, e.db, "role_assignment.create", "success"))
	require.Equal(t, 0, awEvts(t, e.db, "authz.role_assignment.created"))
	require.Equal(t, 0, e.inv.calls())
}

func TestAssignmentWriterRefusesTargetsOutsideTheGrantorScope(t *testing.T) {
	e := newAWEnv(t)
	other := testkit.NewOrganization(t, e.db)
	otherGroup := testkit.NewGroup(t, e.db, other, e.admin)

	toOtherOrg := e.input(e.member.ID)
	toOtherOrg.Scope = domain.OrganizationTarget(other.ID)
	_, err := e.writer.Create(ctx, e.grant, toOtherOrg)
	require.ErrorIs(t, err, domain.ErrTargetOutsideScope, "organisasi lain")

	toPlatform := e.input(e.member.ID)
	toPlatform.Scope = domain.PlatformTarget()
	_, err = e.writer.Create(ctx, e.grant, toPlatform)
	require.ErrorIs(t, err, domain.ErrTargetOutsideScope, "grantor tenant tidak memberi di scope platform")

	toOtherGroup := e.input(e.member.ID)
	toOtherGroup.Scope = domain.GroupTarget(other.ID, otherGroup.ID)
	_, err = e.writer.Create(ctx, e.grant, toOtherGroup)
	require.ErrorIs(t, err, domain.ErrTargetOutsideScope, "grup organisasi lain")

	require.Equal(t, 3, awActs(t, e.db, "role_assignment.create", "denied"))
	require.Equal(t, 0, e.rows(t, e.role.ID))
	require.Equal(t, 0, e.inv.calls())
}

func TestAssignmentWriterEnforcesRoleRules(t *testing.T) {
	e := newAWEnv(t)
	other := testkit.NewOrganization(t, e.db)
	foreign := testkit.RoleWith(t, e.db, []string{"invoice.read"}, testkit.RoleInOrganization(other))
	archived := testkit.NewRole(t, e.db, testkit.RoleInOrganization(e.org), testkit.RoleStatus("archived"))
	superAdmin := awSystemRole(t, e.db, domain.RoleSlugSuperAdmin, "invoice.read")

	in := e.input(e.member.ID)
	in.RoleID = foreign.ID
	_, err := e.writer.Create(ctx, e.grant, in)
	require.ErrorIs(t, err, domain.ErrRoleNotFound, "role organisasi lain tidak terlihat")

	in.RoleID = id.NewID()
	_, err = e.writer.Create(ctx, e.grant, in)
	require.ErrorIs(t, err, domain.ErrRoleNotFound)

	in.RoleID = archived.ID
	_, err = e.writer.Create(ctx, e.grant, in)
	require.ErrorIs(t, err, domain.ErrRoleArchived)

	in.RoleID = superAdmin.ID
	_, err = e.writer.Create(ctx, e.grant, in)
	require.ErrorIs(t, err, domain.ErrAssignmentScope, "super-admin tidak boleh di scope organisasi")
	require.Equal(t, 1, awActs(t, e.db, "role_assignment.create", "denied"), "hanya penolakan kebijakan yang diaudit denied")

	require.Equal(t, 0, count(t, e.db, `SELECT count(*) FROM role_assignments WHERE subject_id = $1`, e.member.ID))
	require.Equal(t, 0, e.inv.calls())
}

func TestAssignmentWriterRejectsSubjectsThatAreNotValidInThisOrganization(t *testing.T) {
	e := newAWEnv(t)
	other := testkit.NewOrganization(t, e.db)
	otherGroup := testkit.NewGroup(t, e.db, other, e.admin)
	outsider := testkit.NewUser(t, e.db)
	suspended := testkit.NewUser(t, e.db, testkit.InOrganization(e.org))
	awExec(t, e.db, `UPDATE users SET status = 'suspended' WHERE id = $1`, suspended.ID)

	user := func(u uuid.UUID) application.CreateAssignmentInput { return e.input(u) }
	subject := func(typ string, sid uuid.UUID) application.CreateAssignmentInput {
		in := e.input(sid)
		in.SubjectType = typ
		return in
	}
	scoped := func(scope domain.GrantTarget) application.CreateAssignmentInput {
		in := e.input(e.member.ID)
		in.Scope = scope
		return in
	}
	cases := []struct {
		name string
		in   application.CreateAssignmentInput
		want error
	}{
		{"bukan anggota organisasi", user(outsider.ID), domain.ErrSubjectNotFound},
		{"user tidak aktif", user(suspended.ID), domain.ErrSubjectNotFound},
		{"user tidak ada", user(id.NewID()), domain.ErrSubjectNotFound},
		{"grup organisasi lain", subject(domain.SubjectGroup, otherGroup.ID), domain.ErrSubjectNotFound},
		{"grup tidak ada", subject(domain.SubjectGroup, id.NewID()), domain.ErrSubjectNotFound},
		{"organisasi lain sebagai subjek", subject(domain.SubjectOrganization, other.ID), domain.ErrAssignmentSubject},
		{"subjek service", subject(domain.SubjectService, id.NewID()), domain.ErrAssignmentSubject},
		{"grup scope tidak ada", scoped(domain.GroupTarget(e.org.ID, id.NewID())), domain.ErrGroupNotFound},
		{"grup scope milik organisasi lain", scoped(domain.GroupTarget(e.org.ID, otherGroup.ID)), domain.ErrGroupNotFound},
	}
	for _, c := range cases {
		_, err := e.writer.Create(ctx, e.grant, c.in)
		require.ErrorIs(t, err, c.want, c.name)
	}

	require.Equal(t, 0, e.rows(t, e.role.ID), "tidak ada yang ditulis")
	require.Equal(t, 0, e.inv.calls())
	require.Equal(t, 0, awEvts(t, e.db, "authz.role_assignment.created"))
	require.Equal(t, 0, awActs(t, e.db, "role_assignment.create", "denied"), "kegagalan validasi bukan penolakan kebijakan")
}

func TestAssignmentWriterRejectsDuplicatesInBothOrganizationForms(t *testing.T) {
	e := newAWEnv(t)
	second := testkit.NewUser(t, e.db, testkit.InOrganization(e.org))
	orgID := e.org.ID
	testkit.AssignRole(t, e.db, testkit.AssignmentSpec{Role: e.role, SubjectType: "user", SubjectID: second.ID, ScopeType: "organization", ScopeID: &orgID, OrganizationID: &orgID})

	_, err := e.writer.Create(ctx, e.grant, e.input(e.member.ID))
	require.NoError(t, err)
	_, err = e.writer.Create(ctx, e.grant, e.input(e.member.ID))
	require.ErrorIs(t, err, domain.ErrAssignmentExists, "bentuk scope_id NULL")
	_, err = e.writer.Create(ctx, e.grant, e.input(second.ID))
	require.ErrorIs(t, err, domain.ErrAssignmentExists, "bentuk scope_id = id organisasi adalah assignment yang sama")

	require.Equal(t, 2, e.rows(t, e.role.ID))
	require.Equal(t, 1, e.inv.calls(), "hanya pembuatan yang berhasil menaikkan versi cache")
	require.Equal(t, 1, awEvts(t, e.db, "authz.role_assignment.created"))
}

func TestAssignmentWriterEnforcesExpiry(t *testing.T) {
	e := newAWEnv(t)
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(48 * time.Hour)

	in := e.input(e.member.ID)
	in.ExpiresAt = &past
	_, err := e.writer.Create(ctx, e.grant, in)
	require.ErrorIs(t, err, domain.ErrAssignmentExpiry)
	require.Equal(t, 0, e.rows(t, e.role.ID))

	in.ExpiresAt = &future
	res, err := e.writer.Create(ctx, e.grant, in)
	require.NoError(t, err)
	require.Equal(t, 1, count(t, e.db, `SELECT count(*) FROM role_assignments WHERE id = $1 AND expires_at IS NOT NULL`, res.Assignment.ID))
}

func TestAssignmentWriterRollsBackWhenInvalidationFails(t *testing.T) {
	e := newAWEnv(t)
	e.inv.fail = errors.New("redis down")

	_, err := e.writer.Create(ctx, e.grant, e.input(e.member.ID))
	require.ErrorIs(t, err, e.inv.fail)
	require.Equal(t, 0, e.rows(t, e.role.ID), "kegagalan invalidasi membatalkan seluruh transaksi")
	require.Equal(t, 0, awActs(t, e.db, "role_assignment.create", "success"))
	require.Equal(t, 0, awEvts(t, e.db, "authz.role_assignment.created"))

	e.inv.fail = nil
	_, err = e.writer.Create(ctx, e.grant, e.input(e.member.ID))
	require.NoError(t, err, "tidak ada sisa dari percobaan yang dibatalkan")
	require.Equal(t, 1, e.rows(t, e.role.ID))
}

func TestAssignmentWriterRevokesAnAssignment(t *testing.T) {
	e := newAWEnv(t)
	asg := awAssignOrg(t, e.db, e.role, e.member.ID, e.org.ID)

	require.NoError(t, e.writer.Revoke(ctx, e.grant, asg.ID))
	require.Equal(t, 1, count(t, e.db, `SELECT count(*) FROM role_assignments WHERE id = $1 AND revoked_at IS NOT NULL`, asg.ID))
	require.Equal(t, 1, count(t, e.db,
		`SELECT count(*) FROM activities WHERE action = 'role_assignment.revoke' AND result = 'success' AND resource_id = $1 AND organization_id = $2`,
		asg.ID, e.org.ID))
	require.Equal(t, 1, count(t, e.db,
		`SELECT count(*) FROM outbox_events WHERE event_name = 'authz.role_assignment.revoked' AND aggregate_id = $1 AND organization_id = $2`,
		asg.ID, e.org.ID))
	require.Equal(t, 1, e.inv.calls())
	require.Equal(t, []uuid.UUID{e.org.ID}, e.inv.last().Organizations)

	require.ErrorIs(t, e.writer.Revoke(ctx, e.grant, asg.ID), domain.ErrAssignmentRevoked)
	require.Equal(t, 1, awActs(t, e.db, "role_assignment.revoke", "success"), "pencabutan ulang tidak menulis audit baru")
	require.Equal(t, 1, e.inv.calls())

	require.ErrorIs(t, e.writer.Revoke(ctx, e.grant, id.NewID()), domain.ErrAssignmentNotFound)
}

func TestAssignmentWriterRevokeNeverTouchesOtherTenantsOrPlatformAssignments(t *testing.T) {
	e := newAWEnv(t)
	other := testkit.NewOrganization(t, e.db)
	otherUser := testkit.NewUser(t, e.db, testkit.InOrganization(other))
	otherRole := testkit.RoleWith(t, e.db, []string{"invoice.read"}, testkit.RoleInOrganization(other))
	foreign := awAssignOrg(t, e.db, otherRole, otherUser.ID, other.ID)
	superAdmin := awSystemRole(t, e.db, domain.RoleSlugSuperAdmin, "invoice.read")
	platform := testkit.AssignRole(t, e.db, testkit.AssignmentSpec{Role: superAdmin, SubjectType: "user", SubjectID: e.member.ID, ScopeType: "platform"})

	require.ErrorIs(t, e.writer.Revoke(ctx, e.grant, foreign.ID), domain.ErrAssignmentNotFound, "organisasi lain")
	require.ErrorIs(t, e.writer.Revoke(ctx, e.grant, platform.ID), domain.ErrAssignmentNotFound, "grantor tenant tidak melihat assignment platform")

	require.Equal(t, 2, count(t, e.db,
		`SELECT count(*) FROM role_assignments WHERE (id = $1 OR id = $2) AND revoked_at IS NULL`, foreign.ID, platform.ID))
	require.Equal(t, 0, e.inv.calls())
}

func TestAssignmentWriterRevokeAppliesTheAuthorityCeiling(t *testing.T) {
	e := newAWEnv(t)
	asg := awAssignOrg(t, e.db, e.beyond, e.member.ID, e.org.ID)

	err := e.writer.Revoke(ctx, e.grant, asg.ID)
	require.ErrorIs(t, err, domain.ErrAuthorityCeiling)
	require.Equal(t, 1, count(t, e.db, `SELECT count(*) FROM role_assignments WHERE id = $1 AND revoked_at IS NULL`, asg.ID))
	require.Equal(t, 1, awActs(t, e.db, "role_assignment.revoke", "denied"))
	require.Equal(t, 0, e.inv.calls())
}

func TestAssignmentWriterGroupLevelGrantorIsLimitedToItsActiveGroup(t *testing.T) {
	e := newAWEnv(t)
	gAdmin := testkit.NewUser(t, e.db, testkit.InOrganization(e.org))
	testkit.AddGroupMember(t, e.db, e.group, gAdmin, "active")
	otherGroup := testkit.NewGroup(t, e.db, e.org, e.admin)
	orgID, groupID := e.org.ID, e.group.ID
	held := testkit.RoleWith(t, e.db, []string{"role.assign", "invoice.read"})
	testkit.AssignRole(t, e.db, testkit.AssignmentSpec{Role: held, SubjectType: "user", SubjectID: gAdmin.ID, ScopeType: "group", ScopeID: &groupID, OrganizationID: &orgID})
	scope, err := domain.NewScope(&orgID, &groupID)
	require.NoError(t, err)
	g := application.Grantor{UserID: gAdmin.ID, Scope: scope}

	inGroup := e.input(e.member.ID)
	inGroup.Scope = domain.GroupTarget(e.org.ID, e.group.ID)
	_, err = e.writer.Create(ctx, g, inGroup)
	require.NoError(t, err, "di grup aktifnya sendiri")

	inOrg := e.input(e.member.ID)
	_, err = e.writer.Create(ctx, g, inOrg)
	require.ErrorIs(t, err, domain.ErrAuthorityCeiling, "level group tidak mencakup organisasi")

	inOther := e.input(e.member.ID)
	inOther.Scope = domain.GroupTarget(e.org.ID, otherGroup.ID)
	_, err = e.writer.Create(ctx, g, inOther)
	require.ErrorIs(t, err, domain.ErrAuthorityCeiling, "level group tidak mencakup grup lain")

	require.Equal(t, 1, e.rows(t, e.role.ID))
	require.Equal(t, 2, awActs(t, e.db, "role_assignment.create", "denied"))
}

func TestAssignmentWriterSerializesConcurrentIdenticalCreates(t *testing.T) {
	e := newAWEnv(t)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = e.writer.Create(ctx, e.grant, e.input(e.member.ID))
		}()
	}
	wg.Wait()

	ok, dup := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, domain.ErrAssignmentExists):
			dup++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, ok)
	require.Equal(t, 1, dup)
	require.Equal(t, 1, e.rows(t, e.role.ID))
	require.Equal(t, 1, e.inv.calls())
}

func TestAssignmentWriterCreateWaitsForTheOrganizationLock(t *testing.T) {
	e := newAWEnv(t)
	release := awHold(t, e.db, func(c context.Context) error { return repository.LockOrganization(c, e.org.ID) })
	done := make(chan error, 1)
	go func() {
		_, err := e.writer.Create(ctx, e.grant, e.input(e.member.ID))
		done <- err
	}()

	awStaysBlocked(t, done, "Create")
	require.Equal(t, 0, e.rows(t, e.role.ID), "belum ada yang ditulis selama kunci ditahan")
	release()
	require.NoError(t, <-done)
	require.Equal(t, 1, e.rows(t, e.role.ID))
}

func TestAssignmentWriterRevokeWaitsForTheOrganizationLock(t *testing.T) {
	e := newAWEnv(t)
	asg := awAssignOrg(t, e.db, e.role, e.member.ID, e.org.ID)
	release := awHold(t, e.db, func(c context.Context) error { return repository.LockOrganization(c, e.org.ID) })
	done := make(chan error, 1)
	go func() { done <- e.writer.Revoke(ctx, e.grant, asg.ID) }()

	awStaysBlocked(t, done, "Revoke")
	require.Equal(t, 1, e.rows(t, e.role.ID), "belum dicabut selama kunci ditahan")
	release()
	require.NoError(t, <-done)
	require.Equal(t, 0, e.rows(t, e.role.ID))
}

// awPlatform adalah dunia scope platform: dua pemberi platform (peran ops, BUKAN super-admin)
// dan dua Super Admin. Pemberi bukan super-admin supaya hitungan super-admin aktif tidak
// bercampur dengan hak pemberi.
type awPlatform struct {
	db     *testpg.DB
	sa     testkit.Role
	ops    testkit.Role
	grants [2]application.Grantor
	admins [2]testkit.Assignment
	inv    *rwInvalidator
	writer *application.AssignmentWriter
}

func newAWPlatform(t *testing.T) awPlatform {
	t.Helper()
	var p awPlatform
	p.db = testkit.NewDB(t)
	p.sa = awSystemRole(t, p.db, domain.RoleSlugSuperAdmin, "role.assign")
	p.ops = testkit.RoleWith(t, p.db, []string{"role.assign"})
	for i := range p.grants {
		u := testkit.NewUser(t, p.db)
		testkit.AssignRole(t, p.db, testkit.AssignmentSpec{Role: p.ops, SubjectType: "user", SubjectID: u.ID, ScopeType: "platform"})
		p.grants[i] = application.Grantor{UserID: u.ID}
	}
	for i := range p.admins {
		u := testkit.NewUser(t, p.db)
		p.admins[i] = testkit.AssignRole(t, p.db, testkit.AssignmentSpec{Role: p.sa, SubjectType: "user", SubjectID: u.ID, ScopeType: "platform"})
	}
	p.inv = &rwInvalidator{}
	p.writer = awWriter(t, p.db, p.inv)
	return p
}

func (p awPlatform) activeAdmins(t *testing.T) int {
	t.Helper()
	return count(t, p.db, `SELECT count(*) FROM role_assignments WHERE role_id = $1 AND revoked_at IS NULL`, p.sa.ID)
}

func TestAssignmentWriterCreatesAPlatformAssignment(t *testing.T) {
	p := newAWPlatform(t)
	newcomer := testkit.NewUser(t, p.db)
	in := application.CreateAssignmentInput{RoleID: p.sa.ID, SubjectType: domain.SubjectUser, SubjectID: newcomer.ID, Scope: domain.PlatformTarget()}

	res, err := p.writer.Create(ctx, p.grants[0], in)
	require.NoError(t, err)
	require.Equal(t, domain.PlatformTarget(), res.Assignment.Scope)
	require.Equal(t, 1, count(t, p.db,
		`SELECT count(*) FROM role_assignments WHERE id = $1 AND scope_type = 'platform' AND organization_id IS NULL AND scope_id IS NULL`, res.Assignment.ID))
	require.Equal(t, 1, count(t, p.db,
		`SELECT count(*) FROM activities WHERE action = 'role_assignment.create' AND result = 'success' AND resource_id = $1`, res.Assignment.ID))
	require.Equal(t, 1, count(t, p.db,
		`SELECT count(*) FROM outbox_events WHERE event_name = 'authz.role_assignment.created' AND aggregate_id = $1`, res.Assignment.ID))
	require.Equal(t, 1, p.inv.calls())
	require.True(t, p.inv.last().Platform, "assignment platform menaikkan versi platform")
	require.Empty(t, p.inv.last().Organizations)
	require.Equal(t, 3, p.activeAdmins(t))

	_, err = p.writer.Create(ctx, p.grants[0], in)
	require.ErrorIs(t, err, domain.ErrAssignmentExists)
}

func TestAssignmentWriterPlatformRules(t *testing.T) {
	p := newAWPlatform(t)
	org := testkit.NewOrganization(t, p.db)
	newcomer := testkit.NewUser(t, p.db)
	suspended := testkit.NewUser(t, p.db)
	awExec(t, p.db, `UPDATE users SET status = 'suspended' WHERE id = $1`, suspended.ID)
	orgRole := testkit.RoleWith(t, p.db, []string{"role.assign"}, testkit.RoleInOrganization(org))
	future := time.Now().Add(48 * time.Hour)
	base := application.CreateAssignmentInput{RoleID: p.sa.ID, SubjectType: domain.SubjectUser, SubjectID: newcomer.ID, Scope: domain.PlatformTarget()}

	toOrg := base
	toOrg.Scope = domain.OrganizationTarget(org.ID)
	_, err := p.writer.Create(ctx, p.grants[0], toOrg)
	require.ErrorIs(t, err, domain.ErrTargetOutsideScope, "Super Admin tidak memberi di organisasi dari scope platform")

	withExpiry := base
	withExpiry.ExpiresAt = &future
	_, err = p.writer.Create(ctx, p.grants[0], withExpiry)
	require.ErrorIs(t, err, domain.ErrAssignmentExpiry, "assignment platform tanpa expires_at")

	toSuspended := base
	toSuspended.SubjectID = suspended.ID
	_, err = p.writer.Create(ctx, p.grants[0], toSuspended)
	require.ErrorIs(t, err, domain.ErrSubjectNotFound)

	toGroup := base
	toGroup.SubjectType = domain.SubjectGroup
	_, err = p.writer.Create(ctx, p.grants[0], toGroup)
	require.ErrorIs(t, err, domain.ErrAssignmentSubject, "subjek platform hanya user")

	notSuperAdmin := base
	notSuperAdmin.RoleID = p.ops.ID
	_, err = p.writer.Create(ctx, p.grants[0], notSuperAdmin)
	require.ErrorIs(t, err, domain.ErrAssignmentScope, "hanya super-admin di scope platform")

	orgRoleAtPlatform := base
	orgRoleAtPlatform.RoleID = orgRole.ID
	_, err = p.writer.Create(ctx, p.grants[0], orgRoleAtPlatform)
	require.ErrorIs(t, err, domain.ErrRoleNotFound, "role organisasi tidak terlihat dari scope platform")

	require.Equal(t, 2, p.activeAdmins(t), "tidak ada yang ditulis")
	require.Equal(t, 0, p.inv.calls())
}

func TestAssignmentWriterNeverRevokesTheLastSuperAdmin(t *testing.T) {
	p := newAWPlatform(t)

	require.NoError(t, p.writer.Revoke(ctx, p.grants[0], p.admins[0].ID))
	require.Equal(t, 1, p.activeAdmins(t))
	require.True(t, p.inv.last().Platform)

	err := p.writer.Revoke(ctx, p.grants[0], p.admins[1].ID)
	require.ErrorIs(t, err, domain.ErrLastSuperAdmin)
	require.Equal(t, 1, p.activeAdmins(t), "super-admin terakhir tetap aktif")
	require.Equal(t, 1, count(t, p.db,
		`SELECT count(*) FROM activities WHERE action = 'role_assignment.revoke' AND result = 'denied' AND resource_id = $1`, p.admins[1].ID))
	require.Equal(t, 1, p.inv.calls(), "penolakan tidak menaikkan versi cache")
}

func TestAssignmentWriterSerializesConcurrentSuperAdminRevocations(t *testing.T) {
	p := newAWPlatform(t)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = p.writer.Revoke(ctx, p.grants[i], p.admins[i].ID)
		}()
	}
	wg.Wait()

	ok, last := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, domain.ErrLastSuperAdmin):
			last++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, ok, "tepat satu pencabutan lolos")
	require.Equal(t, 1, last, "yang lain melihat hanya satu super-admin tersisa")
	require.Equal(t, 1, p.activeAdmins(t), "platform tidak pernah kehilangan super-admin terakhir")
}

func TestAssignmentWriterPlatformRevokeWaitsForThePlatformLock(t *testing.T) {
	p := newAWPlatform(t)
	release := awHold(t, p.db, repository.LockPlatform)
	done := make(chan error, 1)
	go func() { done <- p.writer.Revoke(ctx, p.grants[0], p.admins[0].ID) }()

	awStaysBlocked(t, done, "Revoke platform")
	require.Equal(t, 2, p.activeAdmins(t), "belum dicabut selama kunci platform ditahan")
	release()
	require.NoError(t, <-done)
	require.Equal(t, 1, p.activeAdmins(t))
}
