package domain_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func TestScopeColumnsUsesTheCanonicalFormPerLevel(t *testing.T) {
	org, grp := id.NewID(), id.NewID()

	typ, o, s, err := domain.ScopeColumns(domain.PlatformTarget())
	require.NoError(t, err)
	require.Equal(t, "platform", typ)
	require.Nil(t, o, "platform tidak punya organisasi")
	require.Nil(t, s)

	typ, o, s, err = domain.ScopeColumns(domain.OrganizationTarget(org))
	require.NoError(t, err)
	require.Equal(t, "organization", typ)
	require.NotNil(t, o)
	require.Equal(t, org, *o)
	require.Nil(t, s, "bentuk kanonik scope organisasi: scope_id NULL")

	typ, o, s, err = domain.ScopeColumns(domain.GroupTarget(org, grp))
	require.NoError(t, err)
	require.Equal(t, "group", typ)
	require.NotNil(t, o)
	require.NotNil(t, s)
	require.Equal(t, org, *o)
	require.Equal(t, grp, *s)
}

func TestScopeColumnsRejectsInvalidTargets(t *testing.T) {
	org := id.NewID()
	orgWithGroup := domain.GrantTarget{Level: domain.LevelOrganization, OrganizationID: ptr(org), GroupID: ptr(id.NewID())}
	platformWithOrg := domain.GrantTarget{Level: domain.LevelPlatform, OrganizationID: ptr(org)}
	resource := domain.GrantTarget{Level: domain.LevelResource, OrganizationID: ptr(org), GroupID: ptr(id.NewID())}
	own := domain.GrantTarget{Level: domain.LevelOwn}
	cases := []struct {
		name   string
		target domain.GrantTarget
	}{
		{name: "nilai nol", target: domain.GrantTarget{}},
		{name: "organisasi tanpa id", target: domain.GrantTarget{Level: domain.LevelOrganization}},
		{name: "organisasi dengan grup", target: orgWithGroup},
		{name: "platform dengan organisasi", target: platformWithOrg},
		{name: "level resource", target: resource},
		{name: "level own", target: own},
	}
	for _, c := range cases {
		typ, o, s, err := domain.ScopeColumns(c.target)
		require.ErrorIs(t, err, domain.ErrInvalidGrantTarget, c.name)
		require.Empty(t, typ, c.name)
		require.Nil(t, o, c.name)
		require.Nil(t, s, c.name)
	}
}

func TestScopeColumnsReturnsCopiesNotAliases(t *testing.T) {
	org, grp := id.NewID(), id.NewID()
	target := domain.GroupTarget(org, grp)

	_, o, s, err := domain.ScopeColumns(target)
	require.NoError(t, err)
	*o = id.NewID()
	*s = id.NewID()

	require.Equal(t, org, *target.OrganizationID, "mengubah hasil tidak boleh mengubah target")
	require.Equal(t, grp, *target.GroupID)
}

func TestTargetFromColumnsAcceptsBothOrganizationForms(t *testing.T) {
	org := id.NewID()
	want := domain.OrganizationTarget(org)

	nullForm, err := domain.TargetFromColumns("organization", ptr(org), nil)
	require.NoError(t, err)
	require.Equal(t, want, nullForm)

	explicitForm, err := domain.TargetFromColumns("organization", ptr(org), ptr(org))
	require.NoError(t, err)
	require.Equal(t, want, explicitForm, "scope_id = id organisasi adalah scope yang sama")
}

func TestTargetFromColumnsRejectsRowsItCannotUnderstand(t *testing.T) {
	org, grp, other := id.NewID(), id.NewID(), id.NewID()

	_, err := domain.TargetFromColumns("platform", ptr(org), nil)
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget, "platform dengan organisasi")
	_, err = domain.TargetFromColumns("platform", nil, ptr(grp))
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget, "platform dengan scope_id")
	_, err = domain.TargetFromColumns("organization", nil, nil)
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget, "organisasi tanpa organization_id")
	_, err = domain.TargetFromColumns("organization", ptr(org), ptr(other))
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget, "scope_id organisasi lain")
	_, err = domain.TargetFromColumns("group", ptr(org), nil)
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget, "grup tanpa scope_id")
	_, err = domain.TargetFromColumns("group", nil, ptr(grp))
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget, "grup tanpa organization_id")
	_, err = domain.TargetFromColumns("resource", ptr(org), ptr(grp))
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget, "scope resource tidak dipahami jalur tulis")
	_, err = domain.TargetFromColumns("own", nil, nil)
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget, "scope own tidak dipahami jalur tulis")
	_, err = domain.TargetFromColumns("", nil, nil)
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget, "tipe kosong")
	_, err = domain.TargetFromColumns("galaxy", ptr(org), nil)
	require.ErrorIs(t, err, domain.ErrInvalidGrantTarget, "tipe tak dikenal")
}

func TestScopeColumnsAndTargetFromColumnsRoundTrip(t *testing.T) {
	org, grp := id.NewID(), id.NewID()
	targets := []domain.GrantTarget{domain.PlatformTarget(), domain.OrganizationTarget(org), domain.GroupTarget(org, grp)}
	for _, want := range targets {
		typ, o, s, err := domain.ScopeColumns(want)
		require.NoError(t, err)
		got, err := domain.TargetFromColumns(typ, o, s)
		require.NoError(t, err)
		require.Equal(t, want, got, typ)
	}
}

func TestAssignmentRecordActive(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Hour), now.Add(time.Hour)

	require.True(t, domain.AssignmentRecord{}.Active(now), "tanpa masa berlaku dan belum dicabut")
	require.True(t, domain.AssignmentRecord{ExpiresAt: &future}.Active(now))
	require.False(t, domain.AssignmentRecord{ExpiresAt: &past}.Active(now), "kedaluwarsa")
	require.False(t, domain.AssignmentRecord{ExpiresAt: &now}.Active(now), "tepat di batas dianggap kedaluwarsa")
	require.False(t, domain.AssignmentRecord{RevokedAt: &past}.Active(now), "dicabut")
	require.False(t, domain.AssignmentRecord{RevokedAt: &past, ExpiresAt: &future}.Active(now), "dicabut mengalahkan masa berlaku")
}

func TestAssignmentRecordRevocationFeedsCheckRevocation(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	superAdmin := sysRole(domain.RoleSlugSuperAdmin)
	rec := domain.AssignmentRecord{ID: id.NewID(), Scope: domain.PlatformTarget()}

	ref := rec.Revocation(superAdmin, now)
	require.True(t, ref.Active)
	require.Equal(t, domain.PlatformTarget(), ref.Scope)
	require.ErrorIs(t, domain.CheckRevocation(ref, 1), domain.ErrLastSuperAdmin, "aktif dan terakhir")
	require.NoError(t, domain.CheckRevocation(ref, 2), "masih ada super-admin lain")

	revoked := domain.AssignmentRecord{ID: id.NewID(), Scope: domain.PlatformTarget(), RevokedAt: &past}
	require.False(t, revoked.Revocation(superAdmin, now).Active)
	require.NoError(t, domain.CheckRevocation(revoked.Revocation(superAdmin, now), 1), "yang tidak aktif tidak mengurangi yang aktif")
}
