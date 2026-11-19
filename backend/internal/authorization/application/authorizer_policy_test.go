package application_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/policy"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes Authorizer + policy tanpa DB: PolicySource adalah fake yang mencatat panggilan, sehingga yang
// dibuktikan adalah keputusan dan urutan pemeriksaan (ADR-0022 butir 31-32).

const policyAction = "invoice.update"

type fakePolicies struct {
	list   []policy.Policy
	err    error
	calls  int
	org    *uuid.UUID
	action string
}

func (f *fakePolicies) ForAction(_ context.Context, org *uuid.UUID, action string) ([]policy.Policy, error) {
	f.calls++
	f.org, f.action = org, action
	return f.list, f.err
}

func withPolicies(t *testing.T, f fixture, ps ...policy.Policy) (fixture, *fakePolicies) {
	t.Helper()
	src := &fakePolicies{list: ps}
	f.authz = f.authz.WithPolicies(src)
	return f, src
}

func rule(code string, effect policy.Effect, expr string) policy.Policy {
	return policy.Policy{Code: code, Effect: effect, Expression: expr, Actions: []string{policyAction}}
}

func invoiceGrants(t *testing.T) fixture {
	t.Helper()
	return newFixture(t, map[string]domain.DecisionLevel{policyAction: domain.LevelOrganization})
}

func (f fixture) onInvoice(c domain.Check, attrs map[string]any) domain.Check {
	c.Resource = &domain.ResourceRef{
		Type: "invoice", ID: id.NewID(), OrganizationID: ptrID(f.org), Attrs: attrs,
	}
	return c
}

func TestPolicyDenyOverridesAnOtherwiseGrantedPermission(t *testing.T) {
	f, _ := withPolicies(t, invoiceGrants(t), rule("block-closed", policy.EffectDeny, `resource.status == "closed"`))

	closed, err := f.authz.Can(ctx, f.onInvoice(f.check(policyAction), map[string]any{"status": "closed"}))
	require.NoError(t, err)
	require.Equal(t, domain.Decision{Reason: domain.ReasonPolicyDenied, PolicyCode: "block-closed"}, closed)

	open, err := f.authz.Can(ctx, f.onInvoice(f.check(policyAction), map[string]any{"status": "open"}))
	require.NoError(t, err)
	require.Equal(t, domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}, open)
}

func TestPolicyAllowIsAnExtraConditionNeverAGrant(t *testing.T) {
	// tanpa permission: policy allow true tidak mengubah apa pun dan tidak pernah dimuat
	noPerm := newFixture(t, map[string]domain.DecisionLevel{})
	noPerm, src := withPolicies(t, noPerm, rule("always", policy.EffectAllow, `true`))
	d, err := noPerm.authz.Can(ctx, noPerm.check(policyAction))
	require.NoError(t, err)
	require.Equal(t, domain.Decision{Reason: domain.ReasonNoPermission}, d)
	require.Zero(t, src.calls, "policy tidak dimuat bila permission gagal")

	// dengan permission: allow false menolak, allow true meloloskan
	f, _ := withPolicies(t, invoiceGrants(t), rule("small-only", policy.EffectAllow, `resource.amount <= 1000`))
	big, err := f.authz.Can(ctx, f.onInvoice(f.check(policyAction), map[string]any{"amount": 5000}))
	require.NoError(t, err)
	require.Equal(t, domain.Decision{Reason: domain.ReasonPolicyDenied, PolicyCode: "small-only"}, big)

	small, err := f.authz.Can(ctx, f.onInvoice(f.check(policyAction), map[string]any{"amount": 10}))
	require.NoError(t, err)
	require.True(t, small.Allowed)
}

func TestPoliciesAreNotLoadedWhenScopeMismatchesOrCheckIsInvalid(t *testing.T) {
	f, src := withPolicies(t, invoiceGrants(t), rule("p", policy.EffectDeny, `false`))

	c := f.check(policyAction)
	c.Identity = authdomain.Identity{UserID: f.user, OrganizationID: ptrID(id.NewID())}
	d, err := f.authz.Can(ctx, c)
	require.NoError(t, err)
	require.Equal(t, domain.ReasonScopeMismatch, d.Reason)

	_, err = f.authz.Can(ctx, f.check("bukan-kode"))
	require.Error(t, err)

	require.Zero(t, src.calls)
}

func TestPolicySourceReceivesScopeOrganizationAndAction(t *testing.T) {
	f, src := withPolicies(t, invoiceGrants(t))

	_, err := f.authz.Can(ctx, f.check(policyAction))
	require.NoError(t, err)
	require.Equal(t, 1, src.calls)
	require.NotNil(t, src.org)
	require.Equal(t, f.org, *src.org)
	require.Equal(t, policyAction, src.action)
}

func TestPolicySourceReceivesNilOrganizationForPlatformScope(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{policyAction: domain.LevelPlatform})
	f, src := withPolicies(t, f)

	d, err := f.authz.Can(ctx, domain.Check{
		Identity: authdomain.Identity{UserID: f.user},
		Scope:    domain.ScopeContext{},
		Action:   policyAction,
	})
	require.NoError(t, err)
	require.True(t, d.Allowed)
	require.Equal(t, 1, src.calls)
	require.Nil(t, src.org, "scope platform: hanya policy global")
}

func TestPolicyLoadFailureIsAnErrorNeverAGrant(t *testing.T) {
	f := invoiceGrants(t)
	boom := errors.New("db policy gagal")
	src := &fakePolicies{err: boom}
	f.authz = f.authz.WithPolicies(src)

	d, err := f.authz.Can(ctx, f.check(policyAction))

	require.ErrorIs(t, err, boom)
	require.Equal(t, domain.Decision{}, d)
	require.False(t, d.Allowed)
}

func TestPolicyEvaluationFailureDeniesFailClosed(t *testing.T) {
	cases := []struct {
		name string
		pol  policy.Policy
	}{
		{"atribut hilang", rule("p", policy.EffectDeny, `resource.missing == 1`)},
		{"tipe salah", rule("p", policy.EffectDeny, `resource.status == 1`)},
		{"ekspresi rusak", rule("p", policy.EffectDeny, `resource.status ==`)},
		{"efek tak dikenal", rule("p", policy.Effect("permit"), `true`)},
	}
	for _, c := range cases {
		f, _ := withPolicies(t, invoiceGrants(t), c.pol)
		d, err := f.authz.Can(ctx, f.onInvoice(f.check(policyAction), map[string]any{"status": "open"}))
		require.NoError(t, err, c.name)
		require.Equal(t, domain.Decision{Reason: domain.ReasonPolicyEvaluationFailed, PolicyCode: "p"}, d, c.name)
	}
}

func TestResourceDependentPolicyIsDeferredWhenThereIsNoResource(t *testing.T) {
	f, _ := withPolicies(t, invoiceGrants(t), rule("block-closed", policy.EffectDeny, `resource.status == "closed"`))

	routeLevel, err := f.authz.Can(ctx, f.check(policyAction))
	require.NoError(t, err)
	want := domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization, DeferredPolicies: []string{"block-closed"}}
	require.Equal(t, want, routeLevel)

	// pengecekan tingkat sumber daya (handler/service) menerapkan policy yang ditunda
	resourceLevel, err := f.authz.Can(ctx, f.onInvoice(f.check(policyAction), map[string]any{"status": "closed"}))
	require.NoError(t, err)
	require.Equal(t, domain.ReasonPolicyDenied, resourceLevel.Reason)
	require.False(t, resourceLevel.Allowed)
}

func TestIdentityOnlyPolicyStillAppliesWithoutAResource(t *testing.T) {
	f := invoiceGrants(t)
	expr := fmt.Sprintf(`current_user.id == %q`, f.user.String())
	f, _ = withPolicies(t, f, rule("ban-user", policy.EffectDeny, expr))

	d, err := f.authz.Can(ctx, f.check(policyAction))

	require.NoError(t, err)
	require.Equal(t, domain.Decision{Reason: domain.ReasonPolicyDenied, PolicyCode: "ban-user"}, d)
}

func TestScopeAttributesAreAvailableToPolicies(t *testing.T) {
	f, _ := withPolicies(t, invoiceGrants(t),
		rule("no-group", policy.EffectAllow, `scope.active_group_id == null`),
		rule("same-org", policy.EffectAllow, `scope.organization_id == current_user.organization_id`),
	)

	d, err := f.authz.Can(ctx, f.check(policyAction))
	require.NoError(t, err)
	require.True(t, d.Allowed)

	d, err = f.authz.Can(ctx, f.inGroup(f.check(policyAction)))
	require.NoError(t, err)
	require.Equal(t, domain.Decision{Reason: domain.ReasonPolicyDenied, PolicyCode: "no-group"}, d)
}

func TestCoreResourceAttributesCannotBeSpoofedThroughAttrs(t *testing.T) {
	f := invoiceGrants(t)
	stranger := id.NewID()
	f, _ = withPolicies(t, f,
		rule("owner-only", policy.EffectAllow, `resource.owner_id == current_user.id`),
		rule("typed", policy.EffectAllow, `resource.type == "invoice"`),
		rule("no-stranger", policy.EffectDeny, fmt.Sprintf(`resource.owner_id == %q`, stranger.String())),
	)

	c := f.onInvoice(f.check(policyAction), map[string]any{
		"owner_id": stranger.String(), "type": "payment", "organization_id": stranger.String(),
	})
	c.Resource.OwnerID = ptrID(f.user)

	d, err := f.authz.Can(ctx, c)

	require.NoError(t, err)
	require.True(t, d.Allowed, "owner_id dan type berasal dari ResourceRef, bukan dari Attrs: %+v", d)
}

func TestWithPoliciesReturnsACopyAndLeavesTheOriginalUntouched(t *testing.T) {
	f := invoiceGrants(t)
	denyAll := &fakePolicies{list: []policy.Policy{rule("deny-all", policy.EffectDeny, `true`)}}
	restricted := f.authz.WithPolicies(denyAll)

	orig, err := f.authz.Can(ctx, f.check(policyAction))
	require.NoError(t, err)
	require.True(t, orig.Allowed, "Authorizer asal tidak berubah")
	require.Zero(t, denyAll.calls)

	got, err := restricted.Can(ctx, f.check(policyAction))
	require.NoError(t, err)
	require.Equal(t, domain.ReasonPolicyDenied, got.Reason)

	cleared := restricted.WithPolicies(nil)
	again, err := cleared.Can(ctx, f.check(policyAction))
	require.NoError(t, err)
	require.True(t, again.Allowed, "WithPolicies(nil) melepas policy")
}
