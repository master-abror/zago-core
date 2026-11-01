package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
)

func TestDecisionLevelValid(t *testing.T) {
	for _, l := range []domain.DecisionLevel{
		domain.LevelPlatform, domain.LevelOrganization, domain.LevelGroup, domain.LevelResource, domain.LevelOwn,
	} {
		require.True(t, l.Valid(), string(l))
	}
	for _, l := range []domain.DecisionLevel{"", "PLATFORM", "global", "organisation", " group"} {
		require.False(t, l.Valid(), "level %q tidak boleh dianggap sah", l)
	}
}

func TestDecisionLevelBroadnessIsStrictOrder(t *testing.T) {
	order := []domain.DecisionLevel{
		domain.LevelPlatform, domain.LevelOrganization, domain.LevelGroup, domain.LevelResource, domain.LevelOwn,
	}
	for i, wide := range order {
		require.False(t, wide.BroaderThan(wide), "level tidak lebih luas dari dirinya sendiri: %s", wide)
		for _, narrow := range order[i+1:] {
			require.True(t, wide.BroaderThan(narrow), "%s harus lebih luas dari %s", wide, narrow)
			require.False(t, narrow.BroaderThan(wide), "%s tidak boleh lebih luas dari %s", narrow, wide)
		}
	}
}

func TestInvalidLevelIsNeverBroaderThanAnything(t *testing.T) {
	require.False(t, domain.DecisionLevel("").BroaderThan(domain.LevelOwn))
	require.False(t, domain.DecisionLevel("platform ").BroaderThan(domain.LevelOwn))
	require.True(t, domain.LevelOwn.BroaderThan(domain.DecisionLevel("")))
}

func TestZeroEffectivePermissionsGrantNothing(t *testing.T) {
	var e domain.EffectivePermissions
	require.False(t, e.Has("user.read"))
	_, ok := e.Level("user.read")
	require.False(t, ok)
	require.Equal(t, 0, e.Len())
	require.Empty(t, e.Codes())
	require.Empty(t, e.Grants())
}

func TestBuilderKeepsBroadestLevelPerCode(t *testing.T) {
	b := domain.NewEffectiveBuilder()
	require.NoError(t, b.Add("user.read", domain.LevelGroup))
	require.NoError(t, b.Add("user.read", domain.LevelOrganization))
	require.NoError(t, b.Add("user.read", domain.LevelGroup)) // yang lebih sempit tidak menurunkan
	require.NoError(t, b.Add("role.read", domain.LevelGroup))
	require.NoError(t, b.Add("organization.read", domain.LevelPlatform))
	require.NoError(t, b.Add("organization.read", domain.LevelOrganization))

	e := b.Build()
	l, ok := e.Level("user.read")
	require.True(t, ok)
	require.Equal(t, domain.LevelOrganization, l)
	l, _ = e.Level("role.read")
	require.Equal(t, domain.LevelGroup, l)
	l, _ = e.Level("organization.read")
	require.Equal(t, domain.LevelPlatform, l)
	require.Equal(t, 3, e.Len())
}

func TestBuilderRejectsInvalidGrants(t *testing.T) {
	cases := []struct {
		name  string
		code  string
		level domain.DecisionLevel
	}{
		{name: "kode kosong", code: "", level: domain.LevelOrganization},
		{name: "kode hanya spasi", code: "   ", level: domain.LevelOrganization},
		{name: "kode berspasi di ujung", code: "user.read ", level: domain.LevelOrganization},
		{name: "kode terlalu panjang", code: strings.Repeat("a", 256), level: domain.LevelOrganization},
		{name: "level kosong", code: "user.read", level: ""},
		{name: "level tak dikenal", code: "user.read", level: "galaxy"},
		{name: "level resource tidak boleh masuk himpunan cache", code: "user.read", level: domain.LevelResource},
		{name: "level own tidak boleh masuk himpunan cache", code: "user.read", level: domain.LevelOwn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := domain.NewEffectiveBuilder()
			require.ErrorIs(t, b.Add(tc.code, tc.level), domain.ErrInvalidGrant)
			require.Equal(t, 0, b.Build().Len(), "grant yang ditolak tidak boleh tersimpan")
		})
	}
}

func TestBuildCopiesSoLaterAddsDoNotLeak(t *testing.T) {
	b := domain.NewEffectiveBuilder()
	require.NoError(t, b.Add("user.read", domain.LevelOrganization))
	first := b.Build()

	require.NoError(t, b.Add("user.delete", domain.LevelPlatform))
	require.False(t, first.Has("user.delete"), "hasil Build sebelumnya tidak boleh berubah")
	require.True(t, b.Build().Has("user.delete"))
}

func TestGrantsAndCodesReturnCopies(t *testing.T) {
	b := domain.NewEffectiveBuilder()
	require.NoError(t, b.Add("b.read", domain.LevelOrganization))
	require.NoError(t, b.Add("a.read", domain.LevelGroup))
	e := b.Build()

	require.Equal(t, []string{"a.read", "b.read"}, e.Codes(), "kode terurut")

	g := e.Grants()
	g["evil.delete"] = domain.LevelPlatform
	g["a.read"] = domain.LevelPlatform
	require.False(t, e.Has("evil.delete"), "mengubah salinan Grants tidak boleh menambah izin")
	l, _ := e.Level("a.read")
	require.Equal(t, domain.LevelGroup, l, "mengubah salinan Grants tidak boleh menaikkan level")

	c := e.Codes()
	c[0] = "evil.delete"
	require.False(t, e.Has("evil.delete"))
}

func TestEffectiveFromGrantsRoundTripsAndValidates(t *testing.T) {
	src := map[string]domain.DecisionLevel{
		"user.read":         domain.LevelOrganization,
		"organization.read": domain.LevelPlatform,
	}
	e, err := domain.EffectiveFromGrants(src)
	require.NoError(t, err)
	require.Equal(t, src, e.Grants())

	src["user.read"] = domain.LevelPlatform
	l, _ := e.Level("user.read")
	require.Equal(t, domain.LevelOrganization, l, "EffectiveFromGrants menyalin peta masukan")

	_, err = domain.EffectiveFromGrants(map[string]domain.DecisionLevel{"user.read": "galaxy"})
	require.ErrorIs(t, err, domain.ErrInvalidGrant)
	_, err = domain.EffectiveFromGrants(map[string]domain.DecisionLevel{"": domain.LevelGroup})
	require.ErrorIs(t, err, domain.ErrInvalidGrant)

	empty, err := domain.EffectiveFromGrants(nil)
	require.NoError(t, err)
	require.Equal(t, 0, empty.Len())
}
