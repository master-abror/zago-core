package application_test

import (
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

// Tes integrasi tanpa Redis: Authorizer + repository.Effective + repository.Policies di PostgreSQL 18
// lewat app_user. Tanpa cache, sehingga perubahan policy berlaku pada request berikutnya.

func seedDBPolicy(t *testing.T, db *testpg.DB, org *uuid.UUID, code, effect, expr string, actions ...string) {
	t.Helper()
	_, err := db.Migrator.Exec(ctx,
		`INSERT INTO policies (organization_id, name, code, effect, expression, actions)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		org, "Policy "+code, code, effect, expr, actions)
	require.NoError(t, err)
}

func TestPoliciesFromPostgresRestrictAGrantedPermission(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	other := testkit.NewOrganization(t, db)
	alice := testkit.NewUser(t, db, testkit.InOrganization(org))
	role := testkit.RoleWith(t, db, []string{policyAction})
	oid, otherID := org.ID, other.ID
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: alice.ID,
		ScopeType: "organization", OrganizationID: &oid,
	})

	authz, err := application.NewAuthorizer(repository.NewEffective(db.App), nil)
	require.NoError(t, err)
	authz = authz.WithPolicies(repository.NewPolicies(db.App))

	can := func(status string) domain.Decision {
		t.Helper()
		d, err := authz.Can(ctx, domain.Check{
			Identity: authdomain.Identity{UserID: alice.ID, OrganizationID: &oid},
			Scope:    domain.ScopeContext{OrganizationID: &oid},
			Action:   policyAction,
			Resource: &domain.ResourceRef{
				Type: "invoice", ID: id.NewID(), OrganizationID: &oid, Attrs: map[string]any{"status": status},
			},
		})
		require.NoError(t, err)
		return d
	}

	require.True(t, can("closed").Allowed, "tanpa policy: izin")

	seedDBPolicy(t, db, &otherID, "other-org-block", "deny", `true`, policyAction)
	require.True(t, can("closed").Allowed, "policy organisasi lain tidak berlaku")

	seedDBPolicy(t, db, &oid, "block-closed", "deny", `resource.status == "closed"`, policyAction)
	denied := can("closed")
	require.Equal(t, domain.Decision{Reason: domain.ReasonPolicyDenied, PolicyCode: "block-closed"}, denied)
	require.True(t, can("open").Allowed)

	_, err = db.Migrator.Exec(ctx, `UPDATE policies SET status = 'disabled' WHERE code = 'block-closed'`)
	require.NoError(t, err)
	require.True(t, can("closed").Allowed, "policy dinonaktifkan: berlaku pada request berikutnya")

	seedDBPolicy(t, db, nil, "global-freeze", "deny", `resource.status == "frozen"`, "invoice.*")
	require.Equal(t, domain.ReasonPolicyDenied, can("frozen").Reason, "policy global berlaku untuk semua organisasi")
	require.True(t, can("open").Allowed)
}

func TestBrokenStoredPolicyDeniesFailClosed(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	alice := testkit.NewUser(t, db, testkit.InOrganization(org))
	role := testkit.RoleWith(t, db, []string{policyAction})
	oid := org.ID
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: alice.ID,
		ScopeType: "organization", OrganizationID: &oid,
	})
	seedDBPolicy(t, db, &oid, "broken", "deny", `resource.status ==`, policyAction)

	authz, err := application.NewAuthorizer(repository.NewEffective(db.App), nil)
	require.NoError(t, err)
	authz = authz.WithPolicies(repository.NewPolicies(db.App))

	d, err := authz.Can(ctx, domain.Check{
		Identity: authdomain.Identity{UserID: alice.ID, OrganizationID: &oid},
		Scope:    domain.ScopeContext{OrganizationID: &oid},
		Action:   policyAction,
	})

	require.NoError(t, err)
	require.Equal(t, domain.Decision{Reason: domain.ReasonPolicyEvaluationFailed, PolicyCode: "broken"}, d)
}
