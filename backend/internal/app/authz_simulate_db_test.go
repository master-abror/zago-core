package app_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/app"
	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	authzapp "github.com/master-abror/zago-core/backend/internal/authorization/application"
	authzdomain "github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzrepo "github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// Bukti perilaku butir 69h (ADR-0022): Simulator hasil perakitan memakai Authorizer yang SUDAH
// WithPolicies, sehingga policy yang menolak tampak pada hasil simulasi dan jawabannya sama dengan
// Authorizer.Can langsung. PostgreSQL nyata lewat app_user; tanpa Redis, jadi tanpa cache.

const simulatePolicyAction = "invoice.update"

type simDiscardAudit struct{}

func (simDiscardAudit) Record(context.Context, modulesdk.ActivityEntry) {}

func simQuietLog() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestAssembledSimulatorEnforcesPoliciesLikeProduction(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	admin := testkit.NewUser(t, db, testkit.InOrganization(org))
	bob := testkit.NewUser(t, db, testkit.InOrganization(org))
	oid := org.ID
	role := testkit.RoleWith(t, db, []string{simulatePolicyAction})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: bob.ID,
		ScopeType: "organization", OrganizationID: &oid,
	})

	sim, authorizer, err := app.AssembleSimulatorForTest(db.App, simQuietLog(), simDiscardAudit{})
	require.NoError(t, err)

	scope, err := authzdomain.NewScope(&oid, nil)
	require.NoError(t, err)
	caller := authzapp.Grantor{UserID: admin.ID, Scope: scope}
	input := authzapp.SimulateInput{UserID: bob.ID, Action: simulatePolicyAction}
	direct := func() authzdomain.Decision {
		t.Helper()
		d, err := authorizer.Can(ctx, authzdomain.Check{
			Identity: authdomain.Identity{UserID: bob.ID, OrganizationID: &oid},
			Scope:    scope,
			Action:   simulatePolicyAction,
		})
		require.NoError(t, err)
		return d
	}

	// Kontrol: tanpa policy, bob memang boleh dan simulasi sama dengan engine.
	before, err := sim.Simulate(ctx, caller, input)
	require.NoError(t, err)
	require.True(t, before.Allowed)
	require.Equal(t, direct().Reason, before.Reason)

	_, err = db.Migrator.Exec(ctx,
		`INSERT INTO policies (organization_id, name, code, effect, expression, actions)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		oid.String(), "Blokir semua", "block-all", "deny", "true", []string{simulatePolicyAction})
	require.NoError(t, err)

	denied, err := sim.Simulate(ctx, caller, input)
	require.NoError(t, err)
	require.False(t, denied.Allowed, "policy yang menolak harus tampak pada hasil simulasi")
	require.Equal(t, authzdomain.ReasonPolicyDenied, denied.Reason)
	engine := direct()
	require.False(t, engine.Allowed)
	require.Equal(t, engine.Reason, denied.Reason, "jawaban simulasi sama dengan Authorizer.Can langsung")

	// Policy tidak di-cache: dinonaktifkan, berlaku pada simulasi berikutnya.
	_, err = db.Migrator.Exec(ctx, `UPDATE policies SET status = 'disabled' WHERE code = 'block-all'`)
	require.NoError(t, err)
	after, err := sim.Simulate(ctx, caller, input)
	require.NoError(t, err)
	require.True(t, after.Allowed)
}

// Kontrol bahaya yang dijaga butir 69h: Simulator di atas Authorizer TANPA policy menjawab ALLOW
// untuk request yang produksi tolak, tanpa galat apa pun. Tes ini membuktikan bahwa tes di atas
// memang akan gagal bila perakitan kehilangan WithPolicies.
func TestControlSimulatorOnAnAuthorizerWithoutPoliciesAllowsWhatProductionDenies(t *testing.T) {
	ctx := context.Background()
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	admin := testkit.NewUser(t, db, testkit.InOrganization(org))
	bob := testkit.NewUser(t, db, testkit.InOrganization(org))
	oid := org.ID
	role := testkit.RoleWith(t, db, []string{simulatePolicyAction})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: bob.ID,
		ScopeType: "organization", OrganizationID: &oid,
	})
	_, err := db.Migrator.Exec(ctx,
		`INSERT INTO policies (organization_id, name, code, effect, expression, actions)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		oid.String(), "Blokir semua", "block-all", "deny", "true", []string{simulatePolicyAction})
	require.NoError(t, err)

	bare, err := authzapp.NewAuthorizer(authzrepo.NewEffective(db.App), nil)
	require.NoError(t, err)
	bareSim, err := authzapp.NewSimulator(bare, authzrepo.NewMembers(db.App), authzrepo.NewGroups(db.App), simDiscardAudit{})
	require.NoError(t, err)
	scope, err := authzdomain.NewScope(&oid, nil)
	require.NoError(t, err)

	got, err := bareSim.Simulate(ctx, authzapp.Grantor{UserID: admin.ID, Scope: scope}, authzapp.SimulateInput{UserID: bob.ID, Action: simulatePolicyAction})

	require.NoError(t, err)
	require.True(t, got.Allowed, "kontrol: tanpa WithPolicies policy deny diabaikan dan simulasi menyesatkan")
}
