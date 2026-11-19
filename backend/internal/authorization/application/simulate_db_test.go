package application_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes ber-DB Simulator: Authorizer, Members, dan Groups SUNGGUHAN di atas PostgreSQL. Dua
// organisasi supaya penolakan lintas organisasi terbukti dua arah (ADR-0022 butir 69).

const simGroupProbe = "folder.read"

type simDB struct {
	db     *testpg.DB
	authz  *application.Authorizer
	sim    *application.Simulator
	audit  *auditSpy
	orgA   testkit.Organization
	orgB   testkit.Organization
	adminA testkit.User
	adminB testkit.User
	bob    testkit.User
	carol  testkit.User
}

func (s *simDB) grant(t *testing.T, u testkit.User, org testkit.Organization, codes ...string) {
	t.Helper()
	role := testkit.RoleWith(t, s.db, codes)
	oid := org.ID
	testkit.AssignRole(t, s.db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: u.ID,
		ScopeType: "organization", OrganizationID: &oid,
	})
}

func (s *simDB) grantInGroup(t *testing.T, u testkit.User, org testkit.Organization, g testkit.Group, code string) {
	t.Helper()
	role := testkit.RoleWith(t, s.db, []string{code})
	oid := org.ID
	gid := g.ID
	testkit.AssignRole(t, s.db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: u.ID,
		ScopeType: "group", ScopeID: &gid, OrganizationID: &oid,
	})
}

func newSimDB(t *testing.T) *simDB {
	t.Helper()
	s := &simDB{}
	s.db = testkit.NewDB(t)
	s.orgA = testkit.NewOrganization(t, s.db)
	s.orgB = testkit.NewOrganization(t, s.db)
	s.adminA = testkit.NewUser(t, s.db, testkit.InOrganization(s.orgA))
	s.adminB = testkit.NewUser(t, s.db, testkit.InOrganization(s.orgB))
	s.bob = testkit.NewUser(t, s.db, testkit.InOrganization(s.orgA))
	s.carol = testkit.NewUser(t, s.db, testkit.InOrganization(s.orgB))
	s.grant(t, s.bob, s.orgA, permProbe)
	s.grant(t, s.carol, s.orgB, permProbe)

	authz, err := application.NewAuthorizer(repository.NewEffective(s.db.App), nil)
	require.NoError(t, err)
	s.authz = authz
	s.audit = &auditSpy{}
	sim, err := application.NewSimulator(authz, repository.NewMembers(s.db.App), repository.NewGroups(s.db.App), s.audit)
	require.NoError(t, err)
	s.sim = sim
	return s
}

func (s *simDB) orgGrantor(t *testing.T, caller testkit.User, org testkit.Organization) application.Grantor {
	t.Helper()
	oid := org.ID
	scope, err := domain.NewScope(&oid, nil)
	require.NoError(t, err)
	return application.Grantor{UserID: caller.ID, Scope: scope}
}

func (s *simDB) groupGrantor(t *testing.T, caller testkit.User, org testkit.Organization, group uuid.UUID) application.Grantor {
	t.Helper()
	oid := org.ID
	scope, err := domain.NewScope(&oid, &group)
	require.NoError(t, err)
	return application.Grantor{UserID: caller.ID, Scope: scope}
}

// direct bertanya langsung ke Authorizer.Can untuk user di organisasi, tanpa Simulator.
func (s *simDB) direct(t *testing.T, u testkit.User, org testkit.Organization, group *uuid.UUID, action string) domain.Decision {
	t.Helper()
	oid := org.ID
	scope, err := domain.NewScope(&oid, group)
	require.NoError(t, err)
	d, err := s.authz.Can(context.Background(), domain.Check{
		Identity: authdomain.Identity{UserID: u.ID, OrganizationID: &oid},
		Scope:    scope,
		Action:   action,
	})
	require.NoError(t, err)
	return d
}

func TestSimulateDBGivesTheSameAnswerAsTheEngine(t *testing.T) {
	s := newSimDB(t)

	for _, action := range []string{permProbe, "thing.write", "folder.read"} {
		want := s.direct(t, s.bob, s.orgA, nil, action)

		got, err := s.sim.Simulate(context.Background(), s.orgGrantor(t, s.adminA, s.orgA), application.SimulateInput{UserID: s.bob.ID, Action: action})

		require.NoError(t, err, action)
		require.Equal(t, want.Allowed, got.Allowed, action)
		require.Equal(t, want.Reason, got.Reason, action)
		require.Equal(t, want.Level, got.Level, action)
	}

	got, err := s.sim.Simulate(context.Background(), s.orgGrantor(t, s.adminA, s.orgA), application.SimulateInput{UserID: s.bob.ID, Action: permProbe})
	require.NoError(t, err)
	require.True(t, got.Allowed, "kontrol: bob memang memegang thing.read di organisasinya")
	require.Equal(t, domain.LevelOrganization, got.Level)
}

func TestSimulateDBNeverCrossesOrganizationsInEitherDirection(t *testing.T) {
	s := newSimDB(t)

	_, errAtoB := s.sim.Simulate(context.Background(), s.orgGrantor(t, s.adminA, s.orgA), application.SimulateInput{UserID: s.carol.ID, Action: permProbe})
	_, errBtoA := s.sim.Simulate(context.Background(), s.orgGrantor(t, s.adminB, s.orgB), application.SimulateInput{UserID: s.bob.ID, Action: permProbe})
	_, errUnknown := s.sim.Simulate(context.Background(), s.orgGrantor(t, s.adminA, s.orgA), application.SimulateInput{UserID: id.NewID(), Action: permProbe})

	require.ErrorIs(t, errAtoB, domain.ErrSimulationTargetNotFound, "anggota B tidak terlihat dari A")
	require.ErrorIs(t, errBtoA, domain.ErrSimulationTargetNotFound, "anggota A tidak terlihat dari B")
	require.ErrorIs(t, errUnknown, domain.ErrSimulationTargetNotFound)
	require.Equal(t, errUnknown.Error(), errAtoB.Error(), "organisasi lain dan tidak ada tidak boleh dapat dibedakan")
	require.Equal(t, errUnknown.Error(), errBtoA.Error())
	require.Empty(t, s.audit.entries, "simulasi yang ditolak tidak meninggalkan jejak sukses")
}

func TestSimulateDBMemberOfTwoOrganizationsIsJudgedByTheCallersOrganizationOnly(t *testing.T) {
	s := newSimDB(t)
	dual := testkit.NewUser(t, s.db, testkit.InOrganization(s.orgA))
	testkit.AddOrganizationMember(t, s.db, s.orgB, dual)
	s.grant(t, dual, s.orgB, permProbe)

	fromA, err := s.sim.Simulate(context.Background(), s.orgGrantor(t, s.adminA, s.orgA), application.SimulateInput{UserID: dual.ID, Action: permProbe})
	require.NoError(t, err)
	fromB, err := s.sim.Simulate(context.Background(), s.orgGrantor(t, s.adminB, s.orgB), application.SimulateInput{UserID: dual.ID, Action: permProbe})
	require.NoError(t, err)

	require.False(t, fromA.Allowed, "hak di B tidak bocor ke simulasi dari A")
	require.Equal(t, domain.ReasonNoPermission, fromA.Reason)
	require.True(t, fromB.Allowed)
	require.Equal(t, domain.LevelOrganization, fromB.Level)
}

func TestSimulateDBSuspendedMembershipIsNotFound(t *testing.T) {
	s := newSimDB(t)
	_, err := s.db.Migrator.Exec(context.Background(),
		`UPDATE organization_memberships SET status = 'suspended' WHERE organization_id = $1 AND user_id = $2`,
		s.orgA.ID.String(), s.bob.ID.String())
	require.NoError(t, err)

	_, err = s.sim.Simulate(context.Background(), s.orgGrantor(t, s.adminA, s.orgA), application.SimulateInput{UserID: s.bob.ID, Action: permProbe})

	require.ErrorIs(t, err, domain.ErrSimulationTargetNotFound)
}

func TestSimulateDBGroupScopeFollowsMembershipLikeProduction(t *testing.T) {
	s := newSimDB(t)
	group := testkit.NewGroup(t, s.db, s.orgA, s.adminA)
	member := testkit.NewUser(t, s.db, testkit.InOrganization(s.orgA))
	outsider := testkit.NewUser(t, s.db, testkit.InOrganization(s.orgA))
	testkit.AddGroupMember(t, s.db, group, member, "active")
	s.grantInGroup(t, member, s.orgA, group, simGroupProbe)
	s.grantInGroup(t, outsider, s.orgA, group, simGroupProbe)
	caller := s.orgGrantor(t, s.adminA, s.orgA)

	t.Run("anggota grup dengan grup aktif memperoleh izin tingkat group", func(t *testing.T) {
		gid := group.ID
		got, err := s.sim.Simulate(context.Background(), caller, application.SimulateInput{UserID: member.ID, Action: simGroupProbe, ActiveGroupID: &gid})

		require.NoError(t, err)
		require.True(t, got.Allowed)
		require.Equal(t, domain.LevelGroup, got.Level)
		require.Equal(t, s.direct(t, member, s.orgA, &gid, simGroupProbe).Level, got.Level)
	})

	t.Run("tanpa grup aktif izin grup tidak berlaku", func(t *testing.T) {
		got, err := s.sim.Simulate(context.Background(), caller, application.SimulateInput{UserID: member.ID, Action: simGroupProbe})

		require.NoError(t, err)
		require.False(t, got.Allowed)
		require.Equal(t, domain.ReasonNoPermission, got.Reason)
	})

	t.Run("assignment grup tanpa keanggotaan: grup dibuang, izin tidak berlaku", func(t *testing.T) {
		gid := group.ID
		got, err := s.sim.Simulate(context.Background(), caller, application.SimulateInput{UserID: outsider.ID, Action: simGroupProbe, ActiveGroupID: &gid})

		require.NoError(t, err)
		require.False(t, got.Allowed, "simulasi tidak boleh memberi izin yang header produksi tidak akan memberi")
		require.Equal(t, domain.ReasonNoPermission, got.Reason)
	})
}

func TestSimulateDBGroupCallerSeesOnlyMembersOfTheirOwnGroup(t *testing.T) {
	s := newSimDB(t)
	group := testkit.NewGroup(t, s.db, s.orgA, s.adminA)
	other := testkit.NewGroup(t, s.db, s.orgA, s.adminA)
	member := testkit.NewUser(t, s.db, testkit.InOrganization(s.orgA))
	testkit.AddGroupMember(t, s.db, group, member, "active")
	s.grantInGroup(t, member, s.orgA, group, simGroupProbe)
	caller := s.groupGrantor(t, s.adminA, s.orgA, group.ID)

	t.Run("anggota grup pemanggil terlihat dengan grup pemanggil sebagai grup aktif", func(t *testing.T) {
		got, err := s.sim.Simulate(context.Background(), caller, application.SimulateInput{UserID: member.ID, Action: simGroupProbe})

		require.NoError(t, err)
		require.True(t, got.Allowed)
		require.Equal(t, domain.LevelGroup, got.Level)
	})

	t.Run("anggota organisasi di luar grup pemanggil tidak ditemukan", func(t *testing.T) {
		_, err := s.sim.Simulate(context.Background(), caller, application.SimulateInput{UserID: s.bob.ID, Action: permProbe})

		require.ErrorIs(t, err, domain.ErrSimulationTargetNotFound)
	})

	t.Run("grup lain ditolak", func(t *testing.T) {
		otherID := other.ID
		_, err := s.sim.Simulate(context.Background(), caller, application.SimulateInput{UserID: member.ID, Action: simGroupProbe, ActiveGroupID: &otherID})

		require.ErrorIs(t, err, domain.ErrTargetOutsideScope)
	})

	t.Run("anggota grup yang tidak aktif tidak ditemukan", func(t *testing.T) {
		invited := testkit.NewUser(t, s.db, testkit.InOrganization(s.orgA))
		testkit.AddGroupMember(t, s.db, group, invited, "removed")

		_, err := s.sim.Simulate(context.Background(), caller, application.SimulateInput{UserID: invited.ID, Action: simGroupProbe})

		require.ErrorIs(t, err, domain.ErrSimulationTargetNotFound)
	})
}
