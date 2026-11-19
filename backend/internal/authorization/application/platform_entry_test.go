package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	idgen "github.com/master-abror/zago-core/backend/pkg/id"
)

type fakeDecider struct {
	decision domain.Decision
	err      error
	checks   []domain.Check
}

func (f *fakeDecider) Can(_ context.Context, c domain.Check) (domain.Decision, error) {
	f.checks = append(f.checks, c)
	return f.decision, f.err
}

func TestNewPlatformEntryRequiresADecider(t *testing.T) {
	_, err := application.NewPlatformEntry(nil)
	require.Error(t, err)
}

func TestPlatformEntryAsksTheEngineAtPlatformScopeWithoutAnOrganization(t *testing.T) {
	userID := idgen.NewID()
	d := &fakeDecider{decision: domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelPlatform}}
	entry, err := application.NewPlatformEntry(d)
	require.NoError(t, err)

	ok, err := entry.CanEnterOrganizations(ctx, userID)

	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, d.checks, 1)
	require.Equal(t, authdomain.Identity{UserID: userID}, d.checks[0].Identity,
		"Identity tanpa organisasi: organisasi sesi pengguna tidak ikut")
	require.True(t, d.checks[0].Scope.IsPlatform())
	require.Equal(t, permission.OrganizationRead, d.checks[0].Action)
	require.Nil(t, d.checks[0].Resource)
}

func TestPlatformEntryAdmitsOnlyAnAllowedPlatformLevelDecision(t *testing.T) {
	for _, tc := range []struct {
		name     string
		decision domain.Decision
	}{
		{"no permission", domain.Decision{Reason: domain.ReasonNoPermission}},
		{"scope mismatch", domain.Decision{Reason: domain.ReasonScopeMismatch}},
		{"zero decision", domain.Decision{}},
		{"allowed at organization level", domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization}},
		{"allowed at group level", domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelGroup}},
		{"allowed without a level", domain.Decision{Allowed: true, Reason: domain.ReasonGranted}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry, err := application.NewPlatformEntry(&fakeDecider{decision: tc.decision})
			require.NoError(t, err)

			ok, err := entry.CanEnterOrganizations(ctx, idgen.NewID())

			require.NoError(t, err)
			require.False(t, ok)
		})
	}
}

func TestPlatformEntryEngineErrorIsAnErrorNeverAnAdmission(t *testing.T) {
	boom := errors.New("postgres tidak dapat dijangkau")
	// Decision ikut terisi ALLOW bersama galat: galat harus menang.
	d := &fakeDecider{decision: domain.Decision{Allowed: true, Level: domain.LevelPlatform}, err: boom}
	entry, err := application.NewPlatformEntry(d)
	require.NoError(t, err)

	ok, err := entry.CanEnterOrganizations(ctx, idgen.NewID())

	require.ErrorIs(t, err, boom)
	require.False(t, ok)
}

// Tes integrasi: PlatformEntry + Authorizer + repository.Effective (PostgreSQL 18, app_user).
func TestPlatformEntryThroughTheRealEngineOnlyAdmitsPlatformGrants(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	oid := org.ID

	root := testkit.NewUser(t, db)
	revoked := testkit.NewUser(t, db)
	tenantAdmin := testkit.NewUser(t, db, testkit.InOrganization(org))
	nobody := testkit.NewUser(t, db, testkit.InOrganization(org))

	role := testkit.RoleWith(t, db, []string{permission.OrganizationRead})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: root.ID, ScopeType: "platform",
	})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: revoked.ID, ScopeType: "platform", Revoked: true,
	})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: tenantAdmin.ID, ScopeType: "organization", OrganizationID: &oid,
	})

	authz, err := application.NewAuthorizer(repository.NewEffective(db.App), nil)
	require.NoError(t, err)
	entry, err := application.NewPlatformEntry(authz)
	require.NoError(t, err)

	for _, tc := range []struct {
		name string
		user testkit.User
		want bool
	}{
		{"platform grant", root, true},
		{"revoked platform grant", revoked, false},
		{"organization-level grant is not a ticket into other organizations", tenantAdmin, false},
		{"member without any grant", nobody, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := entry.CanEnterOrganizations(ctx, tc.user.ID)

			require.NoError(t, err)
			require.Equal(t, tc.want, ok)
		})
	}
}
