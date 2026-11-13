package repository_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/policy"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
)

// Tes jalur BACA policy memakai PostgreSQL 18 sungguhan dan role app_user (db.App). Fixture lewat
// seedPolicy (db.Migrator). Tes negatif lintas organisasi ada di sini karena setiap kueri baca
// wajib memfilter tenant (docs/12 §6).

func plrReader(db *testpg.DB) *repository.PolicyReader { return repository.NewPolicyReader(db.App) }

func plrOrg(o testkit.Organization) *uuid.UUID {
	v := o.ID
	return &v
}

func plrCodes(items []domain.PolicyRecord) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Code)
	}
	return out
}

func plrFull() domain.PolicyListQuery {
	return domain.PolicyListQuery{Window: domain.Window{Limit: domain.MaxPageSize}}
}

func TestPolicyReaderListShowsOwnAndGlobalNeverAnotherOrganization(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	seedPolicy(t, db, nil, "global-one", "deny", "active", "invoice.update")
	seedPolicy(t, db, plrOrg(orgA), "a-one", "deny", "active", "invoice.update")
	seedPolicy(t, db, plrOrg(orgB), "b-one", "deny", "active", "invoice.update")

	pageA, err := plrReader(db).List(bg, plrOrg(orgA), plrFull())
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"global-one", "a-one"}, plrCodes(pageA.Items))
	require.Equal(t, 2, pageA.Total)

	pageB, err := plrReader(db).List(bg, plrOrg(orgB), plrFull())
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"global-one", "b-one"}, plrCodes(pageB.Items), "kebalikannya juga berlaku")
	require.Equal(t, 2, pageB.Total)
}

func TestPolicyReaderListAtPlatformScopeSeesOnlyGlobalPolicies(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	seedPolicy(t, db, nil, "global-one", "deny", "active", "invoice.update")
	seedPolicy(t, db, plrOrg(orgA), "a-one", "deny", "active", "invoice.update")

	page, err := plrReader(db).List(bg, nil, plrFull())

	require.NoError(t, err)
	require.Equal(t, []string{"global-one"}, plrCodes(page.Items))
	require.Equal(t, 1, page.Total)
}

func TestPolicyReaderMarksGlobalPoliciesByNilOrganization(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	seedPolicy(t, db, nil, "global-one", "deny", "active", "invoice.update")
	seedPolicy(t, db, plrOrg(orgA), "a-one", "deny", "active", "invoice.update")

	page, err := plrReader(db).List(bg, plrOrg(orgA), plrFull())

	require.NoError(t, err)
	require.Len(t, page.Items, 2)
	for _, it := range page.Items {
		switch it.Code {
		case "global-one":
			require.True(t, it.Global())
			require.Nil(t, it.OrganizationID)
		case "a-one":
			require.False(t, it.Global())
			require.NotNil(t, it.OrganizationID)
			require.Equal(t, orgA.ID, *it.OrganizationID)
		default:
			require.Fail(t, "policy tak terduga", it.Code)
		}
	}
}

func TestPolicyReaderListFiltersByStatusAndDefaultsToAll(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	seedPolicy(t, db, plrOrg(org), "on", "deny", "active", "invoice.update")
	seedPolicy(t, db, plrOrg(org), "off", "deny", "disabled", "invoice.update")

	def, err := plrReader(db).List(bg, plrOrg(org), plrFull())
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"on", "off"}, plrCodes(def.Items), "bawaan memuat policy nonaktif")

	q := plrFull()
	q.Status = domain.PolicyActive
	active, err := plrReader(db).List(bg, plrOrg(org), q)
	require.NoError(t, err)
	require.Equal(t, []string{"on"}, plrCodes(active.Items))
	require.Equal(t, 1, active.Total)

	q.Status = domain.PolicyDisabled
	disabled, err := plrReader(db).List(bg, plrOrg(org), q)
	require.NoError(t, err)
	require.Equal(t, []string{"off"}, plrCodes(disabled.Items))

	q.Status = domain.PolicyFilterAll
	all, err := plrReader(db).List(bg, plrOrg(org), q)
	require.NoError(t, err)
	require.Len(t, all.Items, 2)
}

func TestPolicyReaderListRejectsUnknownStatusInsteadOfListingEverything(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	seedPolicy(t, db, plrOrg(org), "on", "deny", "active", "invoice.update")

	q := plrFull()
	q.Status = "bogus"
	page, err := plrReader(db).List(bg, plrOrg(org), q)

	require.Error(t, err)
	require.Empty(t, page.Items)
}

func TestPolicyReaderListPaginatesInAStableOrderWithTotal(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	for _, code := range []string{"p1", "p2", "p3", "p4", "p5"} {
		seedPolicy(t, db, plrOrg(org), code, "deny", "active", "invoice.update")
	}

	var got []string
	for _, offset := range []int{0, 2, 4} {
		page, err := plrReader(db).List(bg, plrOrg(org), domain.PolicyListQuery{Window: domain.Window{Offset: offset, Limit: 2}})
		require.NoError(t, err)
		require.Equal(t, 5, page.Total)
		got = append(got, plrCodes(page.Items)...)
	}

	require.Equal(t, []string{"p1", "p2", "p3", "p4", "p5"}, got)
}

func TestPolicyReaderListReturnsEveryFieldWithSortedActions(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	oid := org.ID
	_, err := db.Migrator.Exec(bg,
		`INSERT INTO policies (organization_id, name, code, effect, expression, actions)
		 VALUES ($1, 'Nama', 'full', 'allow', 'resource.amount <= 1000', ARRAY['invoice.update', 'invoice.*'])`, &oid)
	require.NoError(t, err)

	page, err := plrReader(db).List(bg, &oid, plrFull())

	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	got := page.Items[0]
	require.NotEqual(t, uuid.Nil, got.ID)
	require.Equal(t, "full", got.Code)
	require.Equal(t, "Nama", got.Name)
	require.Equal(t, "", got.Description, "deskripsi NULL menjadi string kosong")
	require.Equal(t, policy.Effect("allow"), got.Effect)
	require.Equal(t, "resource.amount <= 1000", got.Expression)
	require.Equal(t, []string{"invoice.*", "invoice.update"}, got.Actions)
	require.Equal(t, domain.PolicyActive, got.Status)
	require.False(t, got.CreatedAt.IsZero())
	require.False(t, got.UpdatedAt.IsZero())
}
