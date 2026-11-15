package application_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

type scriptedDecider struct {
	fn     func(domain.Check) (domain.Decision, error)
	checks []domain.Check
}

func (d *scriptedDecider) Can(_ context.Context, c domain.Check) (domain.Decision, error) {
	d.checks = append(d.checks, c)
	return d.fn(c)
}

type memberCall struct{ org, user uuid.UUID }

type memberStub struct {
	member bool
	err    error
	calls  []memberCall
}

func (m *memberStub) IsActiveMember(_ context.Context, orgID, userID uuid.UUID) (bool, error) {
	m.calls = append(m.calls, memberCall{org: orgID, user: userID})
	return m.member, m.err
}

type auditSpy struct {
	entries []modulesdk.ActivityEntry
	actors  []kernel.Actor
}

func (a *auditSpy) Record(ctx context.Context, e modulesdk.ActivityEntry) {
	a.entries = append(a.entries, e)
	actor, _ := kernel.ActorFromContext(ctx)
	a.actors = append(a.actors, actor)
}

type simRig struct {
	decider *scriptedDecider
	members *memberStub
	groups  *fakeChecker
	audit   *auditSpy
	sim     *application.Simulator
	org     uuid.UUID
	caller  uuid.UUID
	target  uuid.UUID
}

var errSimBoom = errors.New("boom")

func simScope(t *testing.T, org uuid.UUID, group *uuid.UUID) domain.ScopeContext {
	t.Helper()
	s, err := domain.NewScope(&org, group)
	require.NoError(t, err)
	return s
}

func newSimRig(t *testing.T) *simRig {
	t.Helper()
	r := &simRig{}
	r.decider = &scriptedDecider{}
	r.decider.fn = func(domain.Check) (domain.Decision, error) {
		return domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}, nil
	}
	r.members = &memberStub{member: true}
	r.groups = &fakeChecker{member: true}
	r.audit = &auditSpy{}
	r.org = id.NewID()
	r.caller = id.NewID()
	r.target = id.NewID()
	sim, err := application.NewSimulator(r.decider, r.members, r.groups, r.audit)
	require.NoError(t, err)
	r.sim = sim
	return r
}

func (r *simRig) orgCaller(t *testing.T) application.Grantor {
	t.Helper()
	return application.Grantor{UserID: r.caller, Scope: simScope(t, r.org, nil)}
}

func (r *simRig) groupCaller(t *testing.T, group uuid.UUID) application.Grantor {
	t.Helper()
	return application.Grantor{UserID: r.caller, Scope: simScope(t, r.org, &group)}
}

func (r *simRig) input(action string) application.SimulateInput {
	return application.SimulateInput{UserID: r.target, Action: action}
}

func (r *simRig) requireNoSideEffects(t *testing.T) {
	t.Helper()
	require.Empty(t, r.members.calls, "tidak boleh ada pencarian anggota")
	require.Empty(t, r.groups.calls, "tidak boleh ada pencarian grup")
	require.Empty(t, r.decider.checks, "engine tidak boleh ditanya")
	require.Empty(t, r.audit.entries, "tidak boleh ada audit")
}

func TestNewSimulatorRequiresEveryDependency(t *testing.T) {
	r := newSimRig(t)

	_, err := application.NewSimulator(nil, r.members, r.groups, r.audit)
	require.Error(t, err)
	_, err = application.NewSimulator(r.decider, nil, r.groups, r.audit)
	require.Error(t, err)
	_, err = application.NewSimulator(r.decider, r.members, nil, r.audit)
	require.Error(t, err)
	_, err = application.NewSimulator(r.decider, r.members, r.groups, nil)
	require.Error(t, err)
}

func TestSimulatePlatformCallerIsRejectedWithoutTouchingAnything(t *testing.T) {
	r := newSimRig(t)
	g := application.Grantor{UserID: r.caller, Scope: domain.ScopeContext{}}

	_, err := r.sim.Simulate(context.Background(), g, r.input(permission.OrganizationRead))

	require.ErrorIs(t, err, domain.ErrTargetOutsideScope)
	r.requireNoSideEffects(t)
}

func TestSimulateAsksTheEngineAboutTheTargetInTheCallersOrganization(t *testing.T) {
	r := newSimRig(t)
	r.decider.fn = func(domain.Check) (domain.Decision, error) {
		return domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelGroup}, nil
	}

	res, err := r.sim.Simulate(context.Background(), r.orgCaller(t), r.input(permission.RoleRead))

	require.NoError(t, err)
	require.True(t, res.Allowed)
	require.Equal(t, domain.ReasonGranted, res.Reason)
	require.Equal(t, domain.LevelGroup, res.Level)
	require.Empty(t, res.PolicyCode)
	require.Empty(t, res.DeferredPolicies)

	require.Len(t, r.decider.checks, 1, "tanpa policy tidak ada pertanyaan policy.read")
	c := r.decider.checks[0]
	require.Equal(t, r.target, c.Identity.UserID, "identitas simulasi adalah target, bukan pemanggil")
	require.NotNil(t, c.Identity.OrganizationID)
	require.Equal(t, r.org, *c.Identity.OrganizationID)
	require.NotNil(t, c.Scope.OrganizationID)
	require.Equal(t, r.org, *c.Scope.OrganizationID)
	require.Nil(t, c.Scope.ActiveGroupID)
	require.Equal(t, permission.RoleRead, c.Action)
	require.Nil(t, c.Resource)

	require.Equal(t, []memberCall{{org: r.org, user: r.target}}, r.members.calls)
	require.Empty(t, r.groups.calls)
}

func TestSimulateDeniedResultHasNoLevel(t *testing.T) {
	r := newSimRig(t)
	r.decider.fn = func(domain.Check) (domain.Decision, error) {
		return domain.Decision{Reason: domain.ReasonNoPermission}, nil
	}

	res, err := r.sim.Simulate(context.Background(), r.orgCaller(t), r.input(permission.RoleManage))

	require.NoError(t, err)
	require.False(t, res.Allowed)
	require.Equal(t, domain.ReasonNoPermission, res.Reason)
	require.Empty(t, res.Level)
}

func TestSimulateRejectsATargetThatIsNotAnActiveMember(t *testing.T) {
	r := newSimRig(t)
	r.members.member = false

	_, err := r.sim.Simulate(context.Background(), r.orgCaller(t), r.input(permission.RoleRead))

	require.ErrorIs(t, err, domain.ErrSimulationTargetNotFound)
	require.Empty(t, r.decider.checks, "engine tidak ditanya untuk target yang tidak sah")
	require.Empty(t, r.audit.entries)
	require.Equal(t, []memberCall{{org: r.org, user: r.target}}, r.members.calls)
}

func TestSimulateDoesNotTurnAMembershipLookupFailureIntoNotFound(t *testing.T) {
	r := newSimRig(t)
	r.members.err = errSimBoom

	_, err := r.sim.Simulate(context.Background(), r.orgCaller(t), r.input(permission.RoleRead))

	require.ErrorIs(t, err, errSimBoom)
	require.NotErrorIs(t, err, domain.ErrSimulationTargetNotFound)
	require.Empty(t, r.decider.checks)
	require.Empty(t, r.audit.entries)
}

func TestSimulateOrganizationCallerMayAskForAGroupTheTargetBelongsTo(t *testing.T) {
	r := newSimRig(t)
	group := id.NewID()
	in := r.input(permission.RoleRead)
	in.ActiveGroupID = &group

	_, err := r.sim.Simulate(context.Background(), r.orgCaller(t), in)

	require.NoError(t, err)
	require.Equal(t, []checkCall{{user: r.target, org: r.org, group: group}}, r.groups.calls)
	require.Len(t, r.decider.checks, 1)
	require.NotNil(t, r.decider.checks[0].Scope.ActiveGroupID)
	require.Equal(t, group, *r.decider.checks[0].Scope.ActiveGroupID)
}

func TestSimulateDropsAGroupTheTargetDoesNotBelongToLikeTheHeaderDoes(t *testing.T) {
	r := newSimRig(t)
	r.groups.member = false
	group := id.NewID()
	in := r.input(permission.RoleRead)
	in.ActiveGroupID = &group

	_, err := r.sim.Simulate(context.Background(), r.orgCaller(t), in)

	require.NoError(t, err, "pemanggil level organisasi: grup yang tidak sah dibuang, bukan galat")
	require.Len(t, r.decider.checks, 1)
	require.Nil(t, r.decider.checks[0].Scope.ActiveGroupID)
}

func TestSimulateGroupLookupFailureIsAnError(t *testing.T) {
	r := newSimRig(t)
	r.groups.err = errSimBoom
	group := id.NewID()
	in := r.input(permission.RoleRead)
	in.ActiveGroupID = &group

	_, err := r.sim.Simulate(context.Background(), r.orgCaller(t), in)

	require.ErrorIs(t, err, errSimBoom)
	require.Empty(t, r.decider.checks, "kegagalan verifikasi tidak boleh jatuh ke scope tanpa grup")
	require.Empty(t, r.audit.entries)
}

func TestSimulateGroupCallerIsPinnedToTheirOwnGroup(t *testing.T) {
	callerGroup := id.NewID()

	t.Run("default memakai grup pemanggil", func(t *testing.T) {
		r := newSimRig(t)

		_, err := r.sim.Simulate(context.Background(), r.groupCaller(t, callerGroup), r.input(permission.RoleRead))

		require.NoError(t, err)
		require.Equal(t, []checkCall{{user: r.target, org: r.org, group: callerGroup}}, r.groups.calls)
		require.NotNil(t, r.decider.checks[0].Scope.ActiveGroupID)
		require.Equal(t, callerGroup, *r.decider.checks[0].Scope.ActiveGroupID)
	})

	t.Run("grup yang sama secara eksplisit sah", func(t *testing.T) {
		r := newSimRig(t)
		in := r.input(permission.RoleRead)
		own := callerGroup
		in.ActiveGroupID = &own

		_, err := r.sim.Simulate(context.Background(), r.groupCaller(t, callerGroup), in)

		require.NoError(t, err)
	})

	t.Run("grup lain ditolak sebelum pencarian apa pun", func(t *testing.T) {
		r := newSimRig(t)
		in := r.input(permission.RoleRead)
		other := id.NewID()
		in.ActiveGroupID = &other

		_, err := r.sim.Simulate(context.Background(), r.groupCaller(t, callerGroup), in)

		require.ErrorIs(t, err, domain.ErrTargetOutsideScope)
		r.requireNoSideEffects(t)
	})

	t.Run("target bukan anggota grup pemanggil adalah tidak ditemukan", func(t *testing.T) {
		r := newSimRig(t)
		r.groups.member = false

		_, err := r.sim.Simulate(context.Background(), r.groupCaller(t, callerGroup), r.input(permission.RoleRead))

		require.ErrorIs(t, err, domain.ErrSimulationTargetNotFound)
		require.Empty(t, r.decider.checks, "tidak boleh jatuh ke scope organisasi yang lebih luas")
		require.Empty(t, r.audit.entries)
	})
}

func TestSimulateBuildsTheResourceInTheCallersOrganization(t *testing.T) {
	r := newSimRig(t)
	owner := id.NewID()
	resGroup := id.NewID()
	resID := id.NewID()
	in := r.input(permission.RoleRead)
	in.Resource = &application.SimulateResource{Type: "invoice", ID: resID, OwnerID: &owner, GroupID: &resGroup}
	in.Resource.Attrs = map[string]any{"status": "draft", "amount": float64(1500)}

	_, err := r.sim.Simulate(context.Background(), r.orgCaller(t), in)

	require.NoError(t, err)
	require.Len(t, r.decider.checks, 1)
	ref := r.decider.checks[0].Resource
	require.NotNil(t, ref)
	require.Equal(t, "invoice", ref.Type)
	require.Equal(t, resID, ref.ID)
	require.NotNil(t, ref.OrganizationID)
	require.Equal(t, r.org, *ref.OrganizationID, "organisasi resource selalu organisasi pemanggil")
	require.NotNil(t, ref.OwnerID)
	require.Equal(t, owner, *ref.OwnerID)
	require.NotNil(t, ref.GroupID)
	require.Equal(t, resGroup, *ref.GroupID)
	require.Equal(t, map[string]any{"status": "draft", "amount": float64(1500)}, ref.Attrs)

	owner = id.NewID()
	require.NotEqual(t, owner, *ref.OwnerID, "ResourceRef tidak boleh beralias pointer masukan")
}

func TestSimulateRejectsBadResourceInputBeforeAnyLookup(t *testing.T) {
	cases := []struct {
		name string
		res  application.SimulateResource
		want error
	}{
		{"atribut tidak sah", application.SimulateResource{Type: "invoice", ID: id.NewID(), Attrs: map[string]any{"k": map[string]any{}}}, domain.ErrInvalidSimulationAttrs},
		{"atribut dicadangkan", application.SimulateResource{Type: "invoice", ID: id.NewID(), Attrs: map[string]any{"owner_id": "x"}}, domain.ErrReservedSimulationAttr},
		{"tipe kosong", application.SimulateResource{ID: id.NewID()}, application.ErrInvalidCheck},
		{"id kosong", application.SimulateResource{Type: "invoice"}, application.ErrInvalidCheck},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := newSimRig(t)
			in := r.input(permission.RoleRead)
			res := c.res
			in.Resource = &res

			_, err := r.sim.Simulate(context.Background(), r.orgCaller(t), in)

			require.ErrorIs(t, err, c.want)
			r.requireNoSideEffects(t)
		})
	}
}

func TestSimulateRejectsAMalformedAction(t *testing.T) {
	for _, action := range []string{"", "bad", "Role.Read", "role..read", "role.", ".read", "role read"} {
		t.Run(fmt.Sprintf("%q", action), func(t *testing.T) {
			r := newSimRig(t)

			_, err := r.sim.Simulate(context.Background(), r.orgCaller(t), r.input(action))

			require.ErrorIs(t, err, application.ErrInvalidCheck)
			r.requireNoSideEffects(t)
		})
	}
}

// policyAwareDecider menjawab pertanyaan simulasi dengan keputusan target, dan pertanyaan
// policy.read pemanggil dengan readPolicies.
func policyAwareDecider(target domain.Decision, readPolicies bool) func(domain.Check) (domain.Decision, error) {
	return func(c domain.Check) (domain.Decision, error) {
		if c.Action == permission.PolicyRead {
			return domain.Decision{Allowed: readPolicies, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}, nil
		}
		return target, nil
	}
}

func TestSimulatePolicyDetailsRequireThePolicyReadPermissionOfTheCaller(t *testing.T) {
	denied := domain.Decision{Reason: domain.ReasonPolicyDenied, PolicyCode: "no_weekend"}
	deferred := domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization, DeferredPolicies: []string{"needs_owner"}}

	t.Run("pemanggil boleh membaca policy: kode terlihat", func(t *testing.T) {
		r := newSimRig(t)
		r.decider.fn = policyAwareDecider(denied, true)

		res, err := r.sim.Simulate(context.Background(), r.orgCaller(t), r.input(permission.RoleRead))

		require.NoError(t, err)
		require.False(t, res.Allowed)
		require.Equal(t, domain.ReasonPolicyDenied, res.Reason)
		require.Equal(t, "no_weekend", res.PolicyCode)

		require.Len(t, r.decider.checks, 2)
		q := r.decider.checks[1]
		require.Equal(t, r.caller, q.Identity.UserID, "pertanyaan policy.read adalah tentang PEMANGGIL")
		require.Equal(t, permission.PolicyRead, q.Action)
		require.Equal(t, r.orgCaller(t).Scope, q.Scope)
		require.Nil(t, q.Resource)
	})

	t.Run("pemanggil tanpa policy.read: kode dikosongkan, alasan tetap", func(t *testing.T) {
		r := newSimRig(t)
		r.decider.fn = policyAwareDecider(denied, false)

		res, err := r.sim.Simulate(context.Background(), r.orgCaller(t), r.input(permission.RoleRead))

		require.NoError(t, err)
		require.False(t, res.Allowed)
		require.Equal(t, domain.ReasonPolicyDenied, res.Reason)
		require.Empty(t, res.PolicyCode)
	})

	t.Run("policy tertunda mengikuti aturan yang sama", func(t *testing.T) {
		visible := newSimRig(t)
		visible.decider.fn = policyAwareDecider(deferred, true)
		res, err := visible.sim.Simulate(context.Background(), visible.orgCaller(t), visible.input(permission.RoleRead))
		require.NoError(t, err)
		require.True(t, res.Allowed)
		require.Equal(t, []string{"needs_owner"}, res.DeferredPolicies)

		hidden := newSimRig(t)
		hidden.decider.fn = policyAwareDecider(deferred, false)
		res, err = hidden.sim.Simulate(context.Background(), hidden.orgCaller(t), hidden.input(permission.RoleRead))
		require.NoError(t, err)
		require.True(t, res.Allowed)
		require.Empty(t, res.DeferredPolicies)
	})

	t.Run("galat pada pertanyaan policy.read adalah galat", func(t *testing.T) {
		r := newSimRig(t)
		r.decider.fn = func(c domain.Check) (domain.Decision, error) {
			if c.Action == permission.PolicyRead {
				return domain.Decision{}, errSimBoom
			}
			return denied, nil
		}

		_, err := r.sim.Simulate(context.Background(), r.orgCaller(t), r.input(permission.RoleRead))

		require.ErrorIs(t, err, errSimBoom)
		require.Empty(t, r.audit.entries, "simulasi yang gagal tidak diaudit sebagai sukses")
	})
}

func TestSimulateEngineErrorIsPropagatedWithoutAudit(t *testing.T) {
	r := newSimRig(t)
	r.decider.fn = func(domain.Check) (domain.Decision, error) { return domain.Decision{}, errSimBoom }

	_, err := r.sim.Simulate(context.Background(), r.orgCaller(t), r.input(permission.RoleRead))

	require.ErrorIs(t, err, errSimBoom)
	require.Empty(t, r.audit.entries)
}

func TestSimulateRecordsOneAuditEntryWithoutResourceAttributeValues(t *testing.T) {
	r := newSimRig(t)
	const secret = "nilai-atribut-rahasia"
	r.decider.fn = policyAwareDecider(domain.Decision{Reason: domain.ReasonPolicyDenied, PolicyCode: "no_weekend"}, false)
	in := r.input(permission.RoleRead)
	in.Resource = &application.SimulateResource{Type: "invoice", ID: id.NewID(), Attrs: map[string]any{"note": secret}}

	_, err := r.sim.Simulate(context.Background(), r.orgCaller(t), in)

	require.NoError(t, err)
	require.Len(t, r.audit.entries, 1)
	e := r.audit.entries[0]
	require.Equal(t, application.AuditActionSimulate, e.Action)
	require.Equal(t, "permission.simulate", e.Action)
	require.Equal(t, "user", e.ResourceType)
	require.Equal(t, r.target, e.ResourceID)
	require.Empty(t, e.Result, "bawaan success")
	require.Equal(t, permission.RoleRead, e.Metadata["action"])
	require.Equal(t, "deny", e.Metadata["result"])
	require.Equal(t, domain.ReasonPolicyDenied, e.Metadata["reason"])
	require.Equal(t, "invoice", e.Metadata["resource_type"])
	require.Equal(t, "no_weekend", e.Metadata["policy_code"], "jejak audit memuat kode policy walau respons menyembunyikannya")
	require.NotContains(t, fmt.Sprint(e), secret)

	require.Len(t, r.audit.actors, 1)
	require.Equal(t, r.caller, r.audit.actors[0].UserID, "aktor adalah pemanggil, bukan target")
	require.Equal(t, r.org, r.audit.actors[0].OrganizationID)
}

func TestSimulateAuditRecordsTheVerifiedGroupOnly(t *testing.T) {
	r := newSimRig(t)
	group := id.NewID()
	in := r.input(permission.RoleRead)
	in.ActiveGroupID = &group

	_, err := r.sim.Simulate(context.Background(), r.orgCaller(t), in)
	require.NoError(t, err)
	require.Len(t, r.audit.entries, 1)
	require.Equal(t, group.String(), r.audit.entries[0].Metadata["active_group_id"])

	dropped := newSimRig(t)
	dropped.groups.member = false
	_, err = dropped.sim.Simulate(context.Background(), dropped.orgCaller(t), in)
	require.NoError(t, err)
	require.Len(t, dropped.audit.entries, 1)
	require.NotContains(t, dropped.audit.entries[0].Metadata, "active_group_id", "grup yang dibuang tidak dicatat sebagai grup simulasi")
}
