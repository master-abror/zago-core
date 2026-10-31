package application_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Resolusi scope terhadap PostgreSQL 18 sungguhan lewat role app_user: repositori nyata, bukan
// tiruan (docs/06 §14: scope grup A tidak berlaku saat header = grup B).

func dbResolver(t *testing.T, db *testpg.DB) *application.ScopeResolver {
	t.Helper()
	r, err := application.NewScopeResolver(repository.NewGroups(db.App), nil)
	require.NoError(t, err)
	return r
}

func sessionOf(u testkit.User, org testkit.Organization) authdomain.Identity {
	oid := org.ID
	return authdomain.Identity{UserID: u.ID, OrganizationID: &oid, SessionID: id.NewID(), AuthLevel: authdomain.AuthLevelPassword}
}

func TestActiveGroupFollowsTheHeaderOfEachRequestAndNeverLeaksToAnotherGroup(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	alice := testkit.NewUser(t, db, testkit.InOrganization(org))
	groupA := testkit.NewGroup(t, db, org, alice)
	groupB := testkit.NewGroup(t, db, org, alice)
	testkit.AddGroupMember(t, db, groupA, alice, "active")
	resolver := dbResolver(t, db)
	session := sessionOf(alice, org)

	scopeA, err := resolver.Resolve(ctx, session, []string{groupA.ID.String()})
	require.NoError(t, err)
	gotA, ok := scopeA.Group()
	require.True(t, ok)
	require.Equal(t, groupA.ID, gotA)

	scopeB, err := resolver.Resolve(ctx, session, []string{groupB.ID.String()})
	require.NoError(t, err, "header grup yang bukan miliknya dibuang, bukan galat")
	_, hasGroup := scopeB.Group()
	require.False(t, hasGroup, "grup A tidak boleh terbawa saat header = grup B (bukan anggota)")
	org1, _ := scopeB.Tenant()
	require.Equal(t, org.ID, org1, "organisasi sesi tetap")

	scopeNone, err := resolver.Resolve(ctx, session, nil)
	require.NoError(t, err)
	_, hasGroup = scopeNone.Group()
	require.False(t, hasGroup, "tanpa header tidak ada grup aktif; engine tidak memilihkan grup")
}

func TestRevokedGroupMembershipTakesEffectOnTheNextRequest(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	alice := testkit.NewUser(t, db, testkit.InOrganization(org))
	group := testkit.NewGroup(t, db, org, alice)
	testkit.AddGroupMember(t, db, group, alice, "active")
	resolver := dbResolver(t, db)
	session := sessionOf(alice, org)

	before, err := resolver.Resolve(ctx, session, []string{group.ID.String()})
	require.NoError(t, err)
	_, ok := before.Group()
	require.True(t, ok, "kontrol: sebelum dicabut grup aktif")

	_, err = db.Migrator.Exec(ctx, `UPDATE group_memberships SET status = 'removed' WHERE group_id = $1 AND user_id = $2`,
		group.ID.String(), alice.ID.String())
	require.NoError(t, err)

	after, err := resolver.Resolve(ctx, session, []string{group.ID.String()})
	require.NoError(t, err)
	_, ok = after.Group()
	require.False(t, ok, "pencabutan berlaku pada request berikutnya, sesi yang sama, tanpa menunggu TTL")
}

func TestGroupOfAnotherOrganizationIsDiscardedForTheSessionOrganization(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	alice := testkit.NewUser(t, db, testkit.InOrganization(orgA))
	testkit.AddOrganizationMember(t, db, orgB, alice)
	groupB := testkit.NewGroup(t, db, orgB, alice)
	testkit.AddGroupMember(t, db, groupB, alice, "active")
	resolver := dbResolver(t, db)

	inA, err := resolver.Resolve(ctx, sessionOf(alice, orgA), []string{groupB.ID.String()})
	require.NoError(t, err)
	_, ok := inA.Group()
	require.False(t, ok, "sesi organisasi A tidak boleh memakai grup organisasi B")

	inB, err := resolver.Resolve(ctx, sessionOf(alice, orgB), []string{groupB.ID.String()})
	require.NoError(t, err)
	got, ok := inB.Group()
	require.True(t, ok, "kontrol: di sesi organisasi B grup B sah")
	require.Equal(t, groupB.ID, got)
}

func TestPlatformIdentityNeverGetsAnActiveGroup(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	alice := testkit.NewUser(t, db, testkit.InOrganization(org))
	group := testkit.NewGroup(t, db, org, alice)
	testkit.AddGroupMember(t, db, group, alice, "active")
	platform := authdomain.Identity{UserID: alice.ID, SessionID: id.NewID(), AuthLevel: authdomain.AuthLevelPassword}

	scope, err := dbResolver(t, db).Resolve(ctx, platform, []string{group.ID.String()})

	require.NoError(t, err)
	require.True(t, scope.IsPlatform())
	_, ok := scope.Group()
	require.False(t, ok, "identitas tanpa organisasi tidak boleh punya grup aktif walau header menunjuk grup nyata")
}

func TestDatabaseFailureDuringResolveIsAnError(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	alice := testkit.NewUser(t, db, testkit.InOrganization(org))
	group := testkit.NewGroup(t, db, org, alice)
	testkit.AddGroupMember(t, db, group, alice, "active")
	canceled, cancel := context.WithCancel(ctx)
	cancel()

	_, err := dbResolver(t, db).Resolve(canceled, sessionOf(alice, org), []string{group.ID.String()})

	require.Error(t, err, "verifikasi yang tak bisa berjalan tidak boleh terbaca sebagai 'tanpa grup'")
}
