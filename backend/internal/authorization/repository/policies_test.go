package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/policy"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
)

// Tes memakai PostgreSQL 18 sungguhan; baris fixture ditulis lewat db.Migrator dan kode yang diuji
// berjalan lewat db.App (hak runtime app_user).

func seedPolicy(t *testing.T, db *testpg.DB, org *uuid.UUID, code, effect, status string, actions ...string) {
	t.Helper()
	_, err := db.Migrator.Exec(bg,
		`INSERT INTO policies (organization_id, name, code, effect, expression, status, actions)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		org, "Policy "+code, code, effect, `resource.status == "x"`, status, actions)
	require.NoError(t, err)
}

func policyCodes(ps []policy.Policy) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Code)
	}
	return out
}

func TestForActionReturnsGlobalAndOwnOrganizationPoliciesOnly(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	idA, idB := orgA.ID, orgB.ID

	seedPolicy(t, db, nil, "global-1", "deny", "active", "invoice.update")
	seedPolicy(t, db, &idA, "org-a", "deny", "active", "invoice.update")
	seedPolicy(t, db, &idB, "org-b", "allow", "active", "invoice.update")

	repo := repository.NewPolicies(db.App)

	got, err := repo.ForAction(bg, &idA, "invoice.update")
	require.NoError(t, err)
	require.Equal(t, []string{"global-1", "org-a"}, policyCodes(got), "organisasi A tidak boleh melihat policy B")

	got, err = repo.ForAction(bg, &idB, "invoice.update")
	require.NoError(t, err)
	require.Equal(t, []string{"global-1", "org-b"}, policyCodes(got))

	got, err = repo.ForAction(bg, nil, "invoice.update")
	require.NoError(t, err)
	require.Equal(t, []string{"global-1"}, policyCodes(got), "scope platform hanya melihat policy global")
}

func TestForActionMatchesExactCodeAndResourceWildcardOnly(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	oid := org.ID

	seedPolicy(t, db, &oid, "exact", "deny", "active", "invoice.update")
	seedPolicy(t, db, &oid, "wild", "deny", "active", "invoice.*")
	seedPolicy(t, db, &oid, "multi", "deny", "active", "invoice.read", "invoice.update")
	seedPolicy(t, db, &oid, "other-action", "deny", "active", "invoice.read")
	seedPolicy(t, db, &oid, "other-resource", "deny", "active", "report.*")
	seedPolicy(t, db, &oid, "nested-wild", "deny", "active", "invoice.line.*")

	repo := repository.NewPolicies(db.App)

	got, err := repo.ForAction(bg, &oid, "invoice.update")
	require.NoError(t, err)
	require.Equal(t, []string{"exact", "multi", "wild"}, policyCodes(got))

	got, err = repo.ForAction(bg, &oid, "invoice.line.update")
	require.NoError(t, err)
	require.Equal(t, []string{"nested-wild"}, policyCodes(got), "invoice.* tidak mencakup invoice.line.update")
}

func TestForActionDoesNotLetParentWildcardCoverNestedResources(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	oid := org.ID

	seedPolicy(t, db, &oid, "parent", "deny", "active", "organization.*")
	seedPolicy(t, db, &oid, "member", "deny", "active", "organization.member.*")

	got, err := repository.NewPolicies(db.App).ForAction(bg, &oid, "organization.member.manage")
	require.NoError(t, err)
	require.Equal(t, []string{"member"}, policyCodes(got))
}

func TestForActionSkipsDisabledPolicies(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	oid := org.ID

	seedPolicy(t, db, &oid, "on", "deny", "active", "invoice.update")
	seedPolicy(t, db, &oid, "off", "deny", "disabled", "invoice.update")
	seedPolicy(t, db, nil, "global-off", "deny", "disabled", "invoice.update")

	got, err := repository.NewPolicies(db.App).ForAction(bg, &oid, "invoice.update")
	require.NoError(t, err)
	require.Equal(t, []string{"on"}, policyCodes(got))
}

func TestForActionReturnsEveryFieldOfThePolicy(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	oid := org.ID

	_, err := db.Migrator.Exec(bg,
		`INSERT INTO policies (organization_id, name, code, effect, expression, actions)
		 VALUES ($1, 'P', 'full', 'allow', 'resource.amount <= 1000', ARRAY['invoice.update', 'invoice.*'])`, &oid)
	require.NoError(t, err)

	got, err := repository.NewPolicies(db.App).ForAction(bg, &oid, "invoice.update")
	require.NoError(t, err)
	require.Equal(t, []policy.Policy{{
		Code:       "full",
		Effect:     policy.EffectAllow,
		Expression: "resource.amount <= 1000",
		Actions:    []string{"invoice.update", "invoice.*"},
	}}, got)
}

func TestForActionWithoutMatchesReturnsNoPolicies(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	oid := org.ID

	got, err := repository.NewPolicies(db.App).ForAction(bg, &oid, "invoice.update")
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestForActionErrorsAreNeverEmptyResults(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	oid := org.ID
	repo := repository.NewPolicies(db.App)

	_, err := repo.ForAction(bg, &oid, "bukan-kode")
	require.Error(t, err, "aksi tak sah")

	cancelled, cancel := context.WithCancel(bg)
	cancel()
	got, err := repo.ForAction(cancelled, &oid, "invoice.update")
	require.Error(t, err, "kegagalan kueri harus galat, bukan daftar kosong")
	require.Nil(t, got)
}
