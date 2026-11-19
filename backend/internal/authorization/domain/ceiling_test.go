package domain_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// holds membangun peta grant: semua code pada satu level.
func holds(level domain.DecisionLevel, codes ...string) map[string]domain.DecisionLevel {
	m := map[string]domain.DecisionLevel{}
	for _, c := range codes {
		m[c] = level
	}
	return m
}

// actorOf menggabungkan beberapa peta grant menjadi EffectivePermissions pemberi.
func actorOf(t *testing.T, parts ...map[string]domain.DecisionLevel) domain.EffectivePermissions {
	t.Helper()
	all := map[string]domain.DecisionLevel{}
	for _, p := range parts {
		for c, l := range p {
			all[c] = l
		}
	}
	e, err := domain.EffectiveFromGrants(all)
	require.NoError(t, err)
	return e
}

func tenantScope(t *testing.T, org uuid.UUID, grp *uuid.UUID) domain.ScopeContext {
	t.Helper()
	s, err := domain.NewScope(&org, grp)
	require.NoError(t, err)
	return s
}

func platformScope(t *testing.T) domain.ScopeContext {
	t.Helper()
	s, err := domain.NewScope(nil, nil)
	require.NoError(t, err)
	return s
}

func TestCheckCeilingAllowsWhatTheActorHoldsAtOrganizationLevel(t *testing.T) {
	org, grp := id.NewID(), id.NewID()
	actor := actorOf(t, holds(domain.LevelOrganization, "role.read", "role.manage"))
	scope := tenantScope(t, org, nil)
	codes := []string{"role.manage", "role.read"}

	require.NoError(t, domain.CheckCeiling(actor, scope, domain.OrganizationTarget(org), codes))
	require.NoError(t, domain.CheckCeiling(actor, scope, domain.GroupTarget(org, grp), codes), "level organisasi mencakup grup di dalamnya")
}

func TestCheckCeilingRejectsPermissionsTheActorLacksAndListsThemSorted(t *testing.T) {
	org := id.NewID()
	actor := actorOf(t, holds(domain.LevelOrganization, "role.read"))
	scope := tenantScope(t, org, nil)

	err := domain.CheckCeiling(actor, scope, domain.OrganizationTarget(org),
		[]string{"role.read", "user.delete", "invoice.approve", "user.delete"})

	require.ErrorIs(t, err, domain.ErrAuthorityCeiling)
	var ce *domain.CeilingError
	require.True(t, errors.As(err, &ce))
	require.Equal(t, []string{"invoice.approve", "user.delete"}, ce.Missing, "terurut dan tanpa duplikat")
	require.Equal(t, 2, ce.Total)
	require.Contains(t, err.Error(), "2 permission")
	require.NotContains(t, err.Error(), "user.delete", "pesan galat hanya memuat hitungan, bukan kode")
}

func TestCheckCeilingCapsTheMissingListButKeepsTheTotal(t *testing.T) {
	org := id.NewID()
	var codes []string
	for i := 0; i < 60; i++ {
		codes = append(codes, fmt.Sprintf("perm.code%03d", i))
	}

	err := domain.CheckCeiling(domain.EffectivePermissions{}, tenantScope(t, org, nil), domain.OrganizationTarget(org), codes)

	var ce *domain.CeilingError
	require.True(t, errors.As(err, &ce))
	require.Len(t, ce.Missing, domain.MaxCeilingMissing)
	require.Equal(t, 60, ce.Total)
	require.Equal(t, "perm.code000", ce.Missing[0])
	require.Equal(t, "perm.code049", ce.Missing[domain.MaxCeilingMissing-1])
}

func TestCheckCeilingTreatsAnEmptyActorAsHoldingNothing(t *testing.T) {
	org := id.NewID()

	err := domain.CheckCeiling(domain.EffectivePermissions{}, tenantScope(t, org, nil), domain.OrganizationTarget(org), []string{"role.read"})

	require.ErrorIs(t, err, domain.ErrAuthorityCeiling, "himpunan kosong gagal-tertutup")
}

func TestCheckCeilingGroupLevelHoldingsCoverOnlyTheActiveGroup(t *testing.T) {
	org, active, other := id.NewID(), id.NewID(), id.NewID()
	actor := actorOf(t, holds(domain.LevelGroup, "role.read", "role.manage"))
	scope := tenantScope(t, org, &active)
	codes := []string{"role.read", "role.manage"}

	require.NoError(t, domain.CheckCeiling(actor, scope, domain.GroupTarget(org, active), codes))

	err := domain.CheckCeiling(actor, scope, domain.GroupTarget(org, other), codes)
	require.ErrorIs(t, err, domain.ErrAuthorityCeiling, "grup lain di organisasi yang sama tidak tercakup level group")
	var ce *domain.CeilingError
	require.True(t, errors.As(err, &ce))
	require.Equal(t, 2, ce.Total)

	err = domain.CheckCeiling(actor, scope, domain.OrganizationTarget(org), codes)
	require.ErrorIs(t, err, domain.ErrAuthorityCeiling, "level group tidak pernah mencakup organisasi (eskalasi)")
}

func TestCheckCeilingGroupLevelHoldingsDoNotCoverAGroupWithoutAnActiveGroup(t *testing.T) {
	org, grp := id.NewID(), id.NewID()
	actor := actorOf(t, holds(domain.LevelGroup, "role.read"))

	err := domain.CheckCeiling(actor, tenantScope(t, org, nil), domain.GroupTarget(org, grp), []string{"role.read"})

	require.ErrorIs(t, err, domain.ErrAuthorityCeiling)
}

func TestCheckCeilingMixedLevelsAreJudgedPerCode(t *testing.T) {
	org, grp := id.NewID(), id.NewID()
	actor := actorOf(t, holds(domain.LevelOrganization, "role.read"), holds(domain.LevelGroup, "role.manage"))
	scope := tenantScope(t, org, &grp)

	require.NoError(t, domain.CheckCeiling(actor, scope, domain.GroupTarget(org, grp), []string{"role.read", "role.manage"}))

	err := domain.CheckCeiling(actor, scope, domain.OrganizationTarget(org), []string{"role.read", "role.manage"})
	var ce *domain.CeilingError
	require.True(t, errors.As(err, &ce))
	require.Equal(t, []string{"role.manage"}, ce.Missing, "hanya kode level group yang tidak mencakup organisasi")
}

func TestCheckCeilingRejectsOtherOrganizationsEvenWithNoCodes(t *testing.T) {
	org, other, grp := id.NewID(), id.NewID(), id.NewID()
	actor := actorOf(t, holds(domain.LevelOrganization, "role.read"))
	scope := tenantScope(t, org, nil)

	for _, target := range []domain.GrantTarget{domain.OrganizationTarget(other), domain.GroupTarget(other, grp)} {
		require.ErrorIs(t, domain.CheckCeiling(actor, scope, target, []string{"role.read"}), domain.ErrTargetOutsideScope)
		require.ErrorIs(t, domain.CheckCeiling(actor, scope, target, nil), domain.ErrTargetOutsideScope, "tanpa kode pun tidak lolos")
	}
}

func TestCheckCeilingPlatformTargetNeedsAPlatformScopeAndPlatformLevel(t *testing.T) {
	org := id.NewID()
	platformActor := actorOf(t, holds(domain.LevelPlatform, "role.assign"))
	codes := []string{"role.assign"}

	require.NoError(t, domain.CheckCeiling(platformActor, platformScope(t), domain.PlatformTarget(), codes))

	require.ErrorIs(t, domain.CheckCeiling(platformActor, tenantScope(t, org, nil), domain.PlatformTarget(), codes), domain.ErrTargetOutsideScope,
		"identitas platform di dalam organisasi tidak boleh memberi di tingkat platform")

	orgOnly := actorOf(t, holds(domain.LevelOrganization, "role.assign"))
	require.ErrorIs(t, domain.CheckCeiling(orgOnly, platformScope(t), domain.PlatformTarget(), codes), domain.ErrAuthorityCeiling,
		"level organisasi tidak mencakup platform")
}

func TestCheckCeilingPlatformScopeCannotGrantInsideAnOrganization(t *testing.T) {
	org, grp := id.NewID(), id.NewID()
	actor := actorOf(t, holds(domain.LevelPlatform, "role.assign"))

	for _, target := range []domain.GrantTarget{domain.OrganizationTarget(org), domain.GroupTarget(org, grp)} {
		require.ErrorIs(t, domain.CheckCeiling(actor, platformScope(t), target, []string{"role.assign"}), domain.ErrTargetOutsideScope,
			"Super Admin masuk dulu ke organisasi (switch-organization); tidak ada jalur lintas-tenant")
	}
}

func TestCheckCeilingPlatformLevelHoldingsCoverTheOrganizationOfTheScope(t *testing.T) {
	org, grp := id.NewID(), id.NewID()
	actor := actorOf(t, holds(domain.LevelPlatform, "role.assign", "role.manage"))
	scope := tenantScope(t, org, nil)
	codes := []string{"role.assign", "role.manage"}

	require.NoError(t, domain.CheckCeiling(actor, scope, domain.OrganizationTarget(org), codes))
	require.NoError(t, domain.CheckCeiling(actor, scope, domain.GroupTarget(org, grp), codes))
}

func TestCheckCeilingAllowsAnEmptyCodeListInsideTheScope(t *testing.T) {
	org := id.NewID()

	require.NoError(t, domain.CheckCeiling(domain.EffectivePermissions{}, tenantScope(t, org, nil), domain.OrganizationTarget(org), nil))
}

func TestCheckCeilingRejectsInvalidCodesBeforeJudgingTheCeiling(t *testing.T) {
	org := id.NewID()
	actor := actorOf(t, holds(domain.LevelOrganization, "role.read"))
	scope := tenantScope(t, org, nil)
	long := strings.Repeat("a", 256)

	for _, bad := range []string{"", " role.read", "role.read ", "\trole.read", long} {
		err := domain.CheckCeiling(actor, scope, domain.OrganizationTarget(org), []string{"user.delete", bad})
		require.ErrorIs(t, err, domain.ErrInvalidGrant, "kode %q", bad)
		require.NotErrorIs(t, err, domain.ErrAuthorityCeiling)
	}
}

func TestCheckCeilingRejectsMalformedTargets(t *testing.T) {
	org, grp := id.NewID(), id.NewID()
	actor := actorOf(t, holds(domain.LevelPlatform, "role.read"))
	scope := tenantScope(t, org, nil)
	cases := []struct {
		name   string
		target domain.GrantTarget
	}{
		{name: "nilai nol", target: domain.GrantTarget{}},
		{name: "level resource", target: domain.GrantTarget{Level: domain.LevelResource, OrganizationID: &org}},
		{name: "level own", target: domain.GrantTarget{Level: domain.LevelOwn}},
		{name: "level asing", target: domain.GrantTarget{Level: "galaxy", OrganizationID: &org}},
		{name: "platform dengan organisasi", target: domain.GrantTarget{Level: domain.LevelPlatform, OrganizationID: &org}},
		{name: "platform dengan grup", target: domain.GrantTarget{Level: domain.LevelPlatform, GroupID: &grp}},
		{name: "organisasi tanpa id", target: domain.GrantTarget{Level: domain.LevelOrganization}},
		{name: "organisasi id nol", target: domain.GrantTarget{Level: domain.LevelOrganization, OrganizationID: ptr(uuid.Nil)}},
		{name: "organisasi dengan grup", target: domain.GrantTarget{Level: domain.LevelOrganization, OrganizationID: &org, GroupID: &grp}},
		{name: "grup tanpa grup", target: domain.GrantTarget{Level: domain.LevelGroup, OrganizationID: &org}},
		{name: "grup id nol", target: domain.GrantTarget{Level: domain.LevelGroup, OrganizationID: &org, GroupID: ptr(uuid.Nil)}},
		{name: "grup tanpa organisasi", target: domain.GrantTarget{Level: domain.LevelGroup, GroupID: &grp}},
	}
	for _, c := range cases {
		require.ErrorIs(t, c.target.Validate(), domain.ErrInvalidGrantTarget, c.name)
		require.ErrorIs(t, domain.CheckCeiling(actor, scope, c.target, []string{"role.read"}), domain.ErrInvalidGrantTarget, c.name)
	}
}

func TestCheckCeilingRejectsAMalformedActorScope(t *testing.T) {
	org, grp := id.NewID(), id.NewID()
	actor := actorOf(t, holds(domain.LevelOrganization, "role.read"))
	cases := []domain.ScopeContext{
		{OrganizationID: ptr(uuid.Nil)},
		{OrganizationID: &org, ActiveGroupID: ptr(uuid.Nil)},
		{ActiveGroupID: &grp},
	}
	for _, s := range cases {
		err := domain.CheckCeiling(actor, s, domain.OrganizationTarget(org), []string{"role.read"})
		require.ErrorIs(t, err, domain.ErrInvalidScope)
	}
}

func TestCheckCeilingDoesNotModifyTheCodeList(t *testing.T) {
	org := id.NewID()
	actor := actorOf(t, holds(domain.LevelOrganization, "b.read"))
	codes := []string{"z.read", "b.read", "a.read", "z.read"}
	before := append([]string(nil), codes...)

	err := domain.CheckCeiling(actor, tenantScope(t, org, nil), domain.OrganizationTarget(org), codes)

	require.ErrorIs(t, err, domain.ErrAuthorityCeiling)
	require.Equal(t, before, codes)
}

func TestGrantTargetsDoNotShareStateWithEachOther(t *testing.T) {
	org, grp := id.NewID(), id.NewID()

	a := domain.GroupTarget(org, grp)
	b := domain.GroupTarget(org, grp)
	*a.OrganizationID = uuid.Nil
	*a.GroupID = uuid.Nil

	require.Error(t, a.Validate())
	require.NoError(t, b.Validate(), "mengubah satu target tidak boleh memengaruhi target lain")
	require.Equal(t, org, *b.OrganizationID)
	require.Equal(t, grp, *b.GroupID)
}
