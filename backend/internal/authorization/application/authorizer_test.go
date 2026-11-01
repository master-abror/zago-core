package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/cache"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes Authorizer murni (tanpa DB/Redis): loader dan cache adalah fake yang mencatat panggilan,
// sehingga yang dibuktikan adalah keputusan dan urutan pemeriksaan, bukan SQL.

func ptrID(u uuid.UUID) *uuid.UUID { return &u }

type fakeLoader struct {
	eff   domain.EffectivePermissions
	err   error
	calls int
	user  uuid.UUID
	scope domain.ScopeContext
}

func (f *fakeLoader) Load(_ context.Context, userID uuid.UUID, scope domain.ScopeContext) (domain.EffectivePermissions, error) {
	f.calls++
	f.user, f.scope = userID, scope
	return f.eff, f.err
}

type fakeCache struct {
	lookup  cache.Lookup
	lookups int
	stored  []cache.Lookup
	storedE []domain.EffectivePermissions
}

func (f *fakeCache) Lookup(context.Context, uuid.UUID, domain.ScopeContext) cache.Lookup {
	f.lookups++
	return f.lookup
}

func (f *fakeCache) Store(_ context.Context, l cache.Lookup, e domain.EffectivePermissions) {
	f.stored = append(f.stored, l)
	f.storedE = append(f.storedE, e)
}

func grants(t *testing.T, g map[string]domain.DecisionLevel) domain.EffectivePermissions {
	t.Helper()
	e, err := domain.EffectiveFromGrants(g)
	require.NoError(t, err)
	return e
}

type fixture struct {
	user, org, group uuid.UUID
	loader           *fakeLoader
	cache            *fakeCache
	authz            *application.Authorizer
}

func newFixture(t *testing.T, g map[string]domain.DecisionLevel) fixture {
	t.Helper()
	f := fixture{user: id.NewID(), org: id.NewID(), group: id.NewID(), loader: &fakeLoader{}, cache: &fakeCache{}}
	f.loader.eff = grants(t, g)
	a, err := application.NewAuthorizer(f.loader, f.cache)
	require.NoError(t, err)
	f.authz = a
	return f
}

// check membangun Check sah untuk identitas anggota organisasi tanpa grup aktif.
func (f fixture) check(action string) domain.Check {
	return domain.Check{
		Identity: authdomain.Identity{UserID: f.user, OrganizationID: ptrID(f.org)},
		Scope:    domain.ScopeContext{OrganizationID: ptrID(f.org)},
		Action:   action,
	}
}

func (f fixture) inGroup(c domain.Check) domain.Check {
	c.Scope.ActiveGroupID = ptrID(f.group)
	return c
}

func TestGrantedAtOrganizationLevel(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"user.read": domain.LevelOrganization})

	d, err := f.authz.Can(ctx, f.check("user.read"))

	require.NoError(t, err)
	require.Equal(t, domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}, d)
	require.Equal(t, f.user, f.loader.user)
	require.Equal(t, f.org, *f.loader.scope.OrganizationID)
}

func TestPlatformLevelStaysPlatform(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"organization.read": domain.LevelPlatform})

	d, err := f.authz.Can(ctx, f.check("organization.read"))

	require.NoError(t, err)
	require.True(t, d.Allowed)
	require.Equal(t, domain.LevelPlatform, d.Level)
}

func TestMissingPermissionIsDeniedWithZeroLevel(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"user.read": domain.LevelOrganization})

	d, err := f.authz.Can(ctx, f.check("user.delete"))

	require.NoError(t, err)
	require.Equal(t, domain.Decision{Allowed: false, Reason: domain.ReasonNoPermission}, d)
}

func TestEmptyEffectiveSetDeniesEverything(t *testing.T) {
	f := newFixture(t, nil)

	for _, action := range []string{"user.read", "organization.read", "profile.read"} {
		d, err := f.authz.Can(ctx, f.check(action))
		require.NoError(t, err)
		require.False(t, d.Allowed, action)
	}
}

func TestGroupLevelGrantAppliesOnlyToResourcesOfTheActiveGroup(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"group.update": domain.LevelGroup})
	other := id.NewID()
	cases := []struct {
		name     string
		resource *domain.ResourceRef
		allowed  bool
	}{
		{"tanpa resource (cek rute)", nil, true},
		{"resource di grup aktif", &domain.ResourceRef{Type: "group", ID: id.NewID(), OrganizationID: ptrID(f.org), GroupID: ptrID(f.group)}, true},
		{"resource di grup lain", &domain.ResourceRef{Type: "group", ID: id.NewID(), OrganizationID: ptrID(f.org), GroupID: ptrID(other)}, false},
		{"resource tanpa grup", &domain.ResourceRef{Type: "group", ID: id.NewID(), OrganizationID: ptrID(f.org)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := f.inGroup(f.check("group.update"))
			c.Resource = tc.resource

			d, err := f.authz.Can(ctx, c)

			require.NoError(t, err)
			require.Equal(t, tc.allowed, d.Allowed)
			if tc.allowed {
				require.Equal(t, domain.LevelGroup, d.Level)
			} else {
				require.Equal(t, domain.ReasonNoPermission, d.Reason)
			}
		})
	}
}

func TestOrganizationLevelGrantCoversResourcesOfAnyGroup(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"group.update": domain.LevelOrganization})
	c := f.inGroup(f.check("group.update"))
	c.Resource = &domain.ResourceRef{Type: "group", ID: id.NewID(), OrganizationID: ptrID(f.org), GroupID: ptrID(id.NewID())}

	d, err := f.authz.Can(ctx, c)

	require.NoError(t, err)
	require.True(t, d.Allowed)
}

func TestResourceOfAnotherOrganizationIsScopeMismatchWithoutTouchingStorage(t *testing.T) {
	// pemegang izin level PLATFORM pun ditolak: organisasi sumber daya harus sama dengan scope
	f := newFixture(t, map[string]domain.DecisionLevel{"user.read": domain.LevelPlatform})
	c := f.check("user.read")
	c.Resource = &domain.ResourceRef{Type: "user", ID: id.NewID(), OrganizationID: ptrID(id.NewID())}

	d, err := f.authz.Can(ctx, c)

	require.NoError(t, err)
	require.Equal(t, domain.Decision{Allowed: false, Reason: domain.ReasonScopeMismatch}, d)
	require.Equal(t, 0, f.loader.calls, "kecocokan tenant diperiksa sebelum ke database")
	require.Equal(t, 0, f.cache.lookups)
}

func TestResourceWithoutOrganizationInTenantScopeIsScopeMismatch(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"user.read": domain.LevelOrganization})
	c := f.check("user.read")
	c.Resource = &domain.ResourceRef{Type: "user", ID: id.NewID()}

	d, err := f.authz.Can(ctx, c)

	require.NoError(t, err)
	require.Equal(t, domain.ReasonScopeMismatch, d.Reason)
	require.False(t, d.Allowed)
}

func TestTenantResourceInPlatformScopeIsScopeMismatch(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"user.read": domain.LevelPlatform})
	c := domain.Check{
		Identity: authdomain.Identity{UserID: f.user},
		Scope:    domain.ScopeContext{},
		Action:   "user.read",
		Resource: &domain.ResourceRef{Type: "user", ID: id.NewID(), OrganizationID: ptrID(f.org)},
	}

	d, err := f.authz.Can(ctx, c)

	require.NoError(t, err)
	require.Equal(t, domain.Decision{Allowed: false, Reason: domain.ReasonScopeMismatch}, d)
}

func TestPlatformResourceInPlatformScopeIsAllowed(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"organization.read": domain.LevelPlatform})
	c := domain.Check{
		Identity: authdomain.Identity{UserID: f.user},
		Action:   "organization.read",
		Resource: &domain.ResourceRef{Type: "organization", ID: id.NewID()},
	}

	d, err := f.authz.Can(ctx, c)

	require.NoError(t, err)
	require.True(t, d.Allowed)
	require.Equal(t, domain.LevelPlatform, d.Level)
}

func TestIdentityAndScopeOrganizationMustMatch(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"user.read": domain.LevelPlatform})
	otherOrg := id.NewID()
	cases := []struct {
		name     string
		identity *uuid.UUID
		scope    *uuid.UUID
	}{
		{"organisasi berbeda", ptrID(f.org), ptrID(otherOrg)},
		{"identitas platform dengan scope organisasi", nil, ptrID(f.org)},
		{"identitas organisasi dengan scope platform", ptrID(f.org), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := domain.Check{
				Identity: authdomain.Identity{UserID: f.user, OrganizationID: tc.identity},
				Scope:    domain.ScopeContext{OrganizationID: tc.scope},
				Action:   "user.read",
			}

			d, err := f.authz.Can(ctx, c)

			require.NoError(t, err)
			require.Equal(t, domain.Decision{Allowed: false, Reason: domain.ReasonScopeMismatch}, d)
			require.Equal(t, 0, f.loader.calls)
		})
	}
}

func TestInvalidChecksAreErrorsNotDecisions(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"user.read": domain.LevelPlatform})
	cases := []struct {
		name   string
		mutate func(*domain.Check)
	}{
		{"aksi kosong", func(c *domain.Check) { c.Action = "" }},
		{"aksi hanya spasi", func(c *domain.Check) { c.Action = "  " }},
		{"aksi tanpa titik", func(c *domain.Check) { c.Action = "userread" }},
		{"aksi berspasi di ujung", func(c *domain.Check) { c.Action = "user.read " }},
		{"user nol", func(c *domain.Check) { c.Identity.UserID = uuid.Nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := f.check("user.read")
			tc.mutate(&c)

			d, err := f.authz.Can(ctx, c)

			require.ErrorIs(t, err, application.ErrInvalidCheck)
			require.False(t, d.Allowed)
		})
	}
	require.Equal(t, 0, f.loader.calls)
}

func TestInvalidScopeIsAnErrorNotADecision(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"user.read": domain.LevelPlatform})
	c := f.check("user.read")
	c.Scope = domain.ScopeContext{ActiveGroupID: ptrID(f.group)} // grup tanpa organisasi
	c.Identity.OrganizationID = nil

	d, err := f.authz.Can(ctx, c)

	require.ErrorIs(t, err, domain.ErrInvalidScope)
	require.False(t, d.Allowed)
	require.Equal(t, 0, f.loader.calls)
}

func TestCacheMissComputesStoresThenHitSkipsTheLoader(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"user.read": domain.LevelOrganization})
	f.cache.lookup = cache.Lookup{Key: "k1"} // miss yang boleh diisi

	d, err := f.authz.Can(ctx, f.check("user.read"))
	require.NoError(t, err)
	require.True(t, d.Allowed)
	require.Equal(t, 1, f.loader.calls)
	require.Len(t, f.cache.stored, 1)
	require.Equal(t, "k1", f.cache.stored[0].Key)
	require.Equal(t, f.loader.eff.Grants(), f.cache.storedE[0].Grants())

	// kini hit: loader tidak dipanggil dan tidak ada Store lagi
	f.cache.lookup = cache.Lookup{Hit: true, Key: "k1", Effective: grants(t, map[string]domain.DecisionLevel{"user.read": domain.LevelOrganization})}
	d, err = f.authz.Can(ctx, f.check("user.read"))
	require.NoError(t, err)
	require.True(t, d.Allowed)
	require.Equal(t, 1, f.loader.calls)
	require.Len(t, f.cache.stored, 1)
}

func TestCacheHitDecidesFromTheCachedSetAlone(t *testing.T) {
	f := newFixture(t, nil) // database akan menjawab "tidak ada izin"
	f.cache.lookup = cache.Lookup{Hit: true, Key: "k", Effective: grants(t, map[string]domain.DecisionLevel{"user.read": domain.LevelGroup})}

	d, err := f.authz.Can(ctx, f.check("user.read"))

	require.NoError(t, err)
	require.True(t, d.Allowed, "hit dilayani dari cache; kebenarannya dijaga invalidasi versi")
	require.Equal(t, 0, f.loader.calls)
}

func TestCacheUnavailableComputesFromTheDatabaseAndDoesNotWrite(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"user.read": domain.LevelOrganization})
	f.cache.lookup = cache.Lookup{} // Key kosong: Redis tidak tersedia

	d, err := f.authz.Can(ctx, f.check("user.read"))

	require.NoError(t, err)
	require.True(t, d.Allowed)
	require.Equal(t, 1, f.loader.calls)
	require.Empty(t, f.cache.stored[0].Key, "Store tetap dipanggil tetapi Key kosong membuatnya no-op di cache")
}

func TestLoaderFailureIsAnErrorAndNeverCached(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"user.read": domain.LevelPlatform})
	f.loader.err = errors.New("postgres mati")
	f.cache.lookup = cache.Lookup{Key: "k"}

	d, err := f.authz.Can(ctx, f.check("user.read"))

	require.Error(t, err)
	require.Equal(t, domain.Decision{}, d, "galat infrastruktur bukan keputusan (apalagi ALLOW)")
	require.Empty(t, f.cache.stored)
}

func TestAuthorizerWorksWithoutACache(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{"user.read": domain.LevelOrganization})
	a, err := application.NewAuthorizer(f.loader, nil)
	require.NoError(t, err)

	d, err := a.Can(ctx, f.check("user.read"))

	require.NoError(t, err)
	require.True(t, d.Allowed)
}

func TestNewAuthorizerRequiresALoader(t *testing.T) {
	_, err := application.NewAuthorizer(nil, nil)
	require.Error(t, err)
}
