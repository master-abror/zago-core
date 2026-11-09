package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/policy"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes jalur tulis policy memakai PostgreSQL 18 sungguhan dan role app_user (db.App); fixture ditulis
// lewat db.Migrator.

var _ policy.ActionCatalog = (*repository.ActionCatalog)(nil)

func pwRepo(db *testpg.DB) *repository.Policies { return repository.NewPolicies(db.App) }

func pwRec(org *uuid.UUID, code string) domain.PolicyRecord {
	return domain.PolicyRecord{ID: id.NewID(), OrganizationID: org, Code: code, Name: "Policy " + code, Description: "uraian", Effect: policy.EffectDeny, Expression: `resource.status == "x"`, Actions: []string{"invoice.update"}, Status: domain.PolicyActive}
}

func pwInsert(t *testing.T, repo *repository.Policies, rec domain.PolicyRecord) domain.PolicyRecord {
	t.Helper()
	stored, err := repo.Insert(bg, rec)
	require.NoError(t, err)
	return stored
}

func pwModulePermission(t *testing.T, db *testpg.DB, moduleCode, resource, action string) {
	t.Helper()
	moduleID := id.NewID()
	_, err := db.Migrator.Exec(bg, "INSERT INTO modules (id, code, name, version, manifest) VALUES ($1, $2, $2, '1.0.0', '{}'::jsonb)", moduleID, moduleCode)
	require.NoError(t, err)
	_, err = db.Migrator.Exec(bg, `INSERT INTO permissions (id, module_id, resource, action, code, description, is_system)
		VALUES ($1, $2, $3, $4, $5, 'milik modul', false)`, id.NewID(), moduleID, resource, action, resource+"."+action)
	require.NoError(t, err)
}

func TestActionCatalogCombinesCoreCodesWithModuleCodes(t *testing.T) {
	cat := repository.NewActionCatalog([]string{"billing.invoice.export", "Bad..Code", "nodot"})

	require.True(t, cat.HasCode("role.manage"), "kode core berasal dari kode")
	require.True(t, cat.HasCode("billing.invoice.export"), "kode modul ikut")
	require.False(t, cat.HasCode("role.nonexistent"))
	require.False(t, cat.HasCode("Bad..Code"), "kode modul yang salah bentuk diabaikan")
	require.False(t, cat.HasCode("nodot"))
	require.False(t, cat.HasCode(""))

	require.True(t, cat.HasResource("role"))
	require.True(t, cat.HasResource("organization"))
	require.True(t, cat.HasResource("organization.member"), "resource = semua segmen sebelum titik terakhir")
	require.True(t, cat.HasResource("billing.invoice"))
	require.False(t, cat.HasResource("billing"), "tidak ada kode yang resource-nya persis billing")
	require.False(t, cat.HasResource("role.manage"), "kode lengkap bukan resource")
	require.False(t, cat.HasResource(""))
}

func TestPoliciesCatalogReadsModuleCodesFromTheTableAndCoreFromCode(t *testing.T) {
	db := testkit.NewDB(t)
	pwModulePermission(t, db, "billing", "billing.invoice", "export")
	testkit.EnsurePermission(t, db, "ghost.core")

	cat, err := pwRepo(db).Catalog(bg)
	require.NoError(t, err)
	require.True(t, cat.HasCode("billing.invoice.export"), "kode modul dibaca dari tabel permissions")
	require.True(t, cat.HasResource("billing.invoice"))
	require.True(t, cat.HasCode("role.manage"), "kode core dari kode")
	require.False(t, cat.HasCode("ghost.core"), "baris core di tabel yang tidak ada di kode tidak dihitung")
	require.False(t, cat.HasResource("ghost"))
}

func TestPoliciesInsertAndForUpdateRoundTrip(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	orgID := org.ID
	repo := pwRepo(db)

	rec := pwRec(&orgID, "invoice.freeze")
	rec.Actions = []string{"invoice.delete", "invoice.update"}
	stored := pwInsert(t, repo, rec)
	require.False(t, stored.CreatedAt.IsZero())
	require.False(t, stored.UpdatedAt.IsZero())

	got, err := repo.ForUpdate(bg, rec.ID, &orgID)
	require.NoError(t, err)
	require.Equal(t, rec.ID, got.ID)
	require.NotNil(t, got.OrganizationID)
	require.Equal(t, orgID, *got.OrganizationID)
	require.Equal(t, "invoice.freeze", got.Code)
	require.Equal(t, rec.Name, got.Name)
	require.Equal(t, "uraian", got.Description)
	require.Equal(t, policy.EffectDeny, got.Effect)
	require.Equal(t, rec.Expression, got.Expression)
	require.Equal(t, []string{"invoice.delete", "invoice.update"}, got.Actions)
	require.Equal(t, domain.PolicyActive, got.Status)
	require.True(t, got.CreatedAt.Equal(stored.CreatedAt))
}

func TestPoliciesInsertStoresAnEmptyDescriptionAsNull(t *testing.T) {
	db := testkit.NewDB(t)
	rec := pwRec(nil, "global.nodesc")
	rec.Description = ""
	pwInsert(t, pwRepo(db), rec)

	require.True(t, asFlag(t, db, `SELECT description IS NULL FROM policies WHERE id = $1`, rec.ID))
	got, err := pwRepo(db).ForUpdate(bg, rec.ID, nil)
	require.NoError(t, err)
	require.Equal(t, "", got.Description)
	require.Nil(t, got.OrganizationID)
}

func TestPoliciesInsertMapsDuplicateCodesPerScope(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	idA, idB := orgA.ID, orgB.ID
	repo := pwRepo(db)

	pwInsert(t, repo, pwRec(&idA, "dup.code"))
	_, err := repo.Insert(bg, pwRec(&idA, "dup.code"))
	require.ErrorIs(t, err, domain.ErrPolicyCodeTaken, "kode ganda dalam satu organisasi")

	disabled := pwRec(&idA, "dup.code")
	disabled.Status = domain.PolicyDisabled
	_, err = repo.Insert(bg, disabled)
	require.ErrorIs(t, err, domain.ErrPolicyCodeTaken, "status tidak memengaruhi keunikan kode")

	pwInsert(t, repo, pwRec(&idB, "dup.code"))
	pwInsert(t, repo, pwRec(nil, "dup.code"))
	_, err = repo.Insert(bg, pwRec(nil, "dup.code"))
	require.ErrorIs(t, err, domain.ErrPolicyCodeTaken, "kode ganda pada policy global (indeks parsial)")
}

func TestPoliciesInsertKeepsTheDatabaseFences(t *testing.T) {
	db := testkit.NewDB(t)
	repo := pwRepo(db)

	badActions := pwRec(nil, "fence.actions")
	badActions.Actions = []string{"bad"}
	_, err := repo.Insert(bg, badActions)
	require.Error(t, err, "ck_policies_actions menolak actions yang salah bentuk")
	require.NotErrorIs(t, err, domain.ErrPolicyCodeTaken)

	badEffect := pwRec(nil, "fence.effect")
	badEffect.Effect = policy.Effect("maybe")
	_, err = repo.Insert(bg, badEffect)
	require.Error(t, err)
	require.NotErrorIs(t, err, domain.ErrPolicyCodeTaken)

	badStatus := pwRec(nil, "fence.status")
	badStatus.Status = "weird"
	_, err = repo.Insert(bg, badStatus)
	require.Error(t, err)
	require.NotErrorIs(t, err, domain.ErrPolicyCodeTaken)
}

func TestPoliciesForUpdateIsScopedExactly(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	idA, idB := orgA.ID, orgB.ID
	repo := pwRepo(db)

	inA := pwInsert(t, repo, pwRec(&idA, "scope.a"))
	global := pwInsert(t, repo, pwRec(nil, "scope.global"))

	_, err := repo.ForUpdate(bg, inA.ID, &idA)
	require.NoError(t, err)
	_, err = repo.ForUpdate(bg, global.ID, nil)
	require.NoError(t, err)

	_, err = repo.ForUpdate(bg, inA.ID, &idB)
	require.ErrorIs(t, err, domain.ErrPolicyNotFound, "organisasi lain tidak melihat policy A")
	_, err = repo.ForUpdate(bg, inA.ID, nil)
	require.ErrorIs(t, err, domain.ErrPolicyNotFound, "pemanggil platform tidak melihat policy organisasi")
	_, err = repo.ForUpdate(bg, global.ID, &idA)
	require.ErrorIs(t, err, domain.ErrPolicyNotFound, "pemanggil tenant tidak melihat policy global")
	_, err = repo.ForUpdate(bg, id.NewID(), &idA)
	require.ErrorIs(t, err, domain.ErrPolicyNotFound)
}

func TestPoliciesActiveCountIsPerScopeAndIgnoresDisabled(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	idA, idB := orgA.ID, orgB.ID
	repo := pwRepo(db)

	for _, code := range []string{"a.one", "a.two"} {
		pwInsert(t, repo, pwRec(&idA, code))
	}
	off := pwRec(&idA, "a.off")
	off.Status = domain.PolicyDisabled
	pwInsert(t, repo, off)
	pwInsert(t, repo, pwRec(&idB, "b.one"))
	for _, code := range []string{"g.one", "g.two", "g.three"} {
		pwInsert(t, repo, pwRec(nil, code))
	}
	globalOff := pwRec(nil, "g.off")
	globalOff.Status = domain.PolicyDisabled
	pwInsert(t, repo, globalOff)

	n, err := repo.ActiveCount(bg, &idA)
	require.NoError(t, err)
	require.Equal(t, 2, n, "yang nonaktif tidak dihitung")
	n, err = repo.ActiveCount(bg, &idB)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	n, err = repo.ActiveCount(bg, nil)
	require.NoError(t, err)
	require.Equal(t, 3, n, "hitungan global tidak bercampur dengan organisasi")

	unknown := id.NewID()
	n, err = repo.ActiveCount(bg, &unknown)
	require.NoError(t, err)
	require.Equal(t, 0, n)
}

func TestPoliciesUpdateChangesContentButNotIdentity(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	orgID := org.ID
	repo := pwRepo(db)
	stored := pwInsert(t, repo, pwRec(&orgID, "upd.code"))

	next := stored
	next.Name = "Nama baru"
	next.Description = ""
	next.Effect = policy.EffectAllow
	next.Expression = `current_user.id == "x"`
	next.Actions = []string{"invoice.read", "invoice.update"}
	next.Status = domain.PolicyDisabled
	upd, err := repo.Update(bg, next)
	require.NoError(t, err)
	require.False(t, upd.UpdatedAt.Before(stored.UpdatedAt))

	got, err := repo.ForUpdate(bg, stored.ID, &orgID)
	require.NoError(t, err)
	require.Equal(t, "upd.code", got.Code, "kode tidak berubah")
	require.Equal(t, "Nama baru", got.Name)
	require.Equal(t, "", got.Description)
	require.Equal(t, policy.EffectAllow, got.Effect)
	require.Equal(t, `current_user.id == "x"`, got.Expression)
	require.Equal(t, []string{"invoice.read", "invoice.update"}, got.Actions)
	require.Equal(t, domain.PolicyDisabled, got.Status)
	require.True(t, got.CreatedAt.Equal(stored.CreatedAt), "CreatedAt tidak berubah")
	require.True(t, asFlag(t, db, `SELECT description IS NULL FROM policies WHERE id = $1`, stored.ID))
}

func TestPoliciesUpdateNeverCrossesScopes(t *testing.T) {
	db := testkit.NewDB(t)
	orgA := testkit.NewOrganization(t, db)
	orgB := testkit.NewOrganization(t, db)
	idA, idB := orgA.ID, orgB.ID
	repo := pwRepo(db)
	inB := pwInsert(t, repo, pwRec(&idB, "cross.b"))
	global := pwInsert(t, repo, pwRec(nil, "cross.global"))

	hijack := inB
	hijack.Name = "dibajak"
	hijack.OrganizationID = &idA
	_, err := repo.Update(bg, hijack)
	require.ErrorIs(t, err, domain.ErrPolicyNotFound, "organisasi A tidak dapat mengubah policy B")

	hijack.OrganizationID = nil
	_, err = repo.Update(bg, hijack)
	require.ErrorIs(t, err, domain.ErrPolicyNotFound, "pemanggil platform tidak dapat mengubah policy organisasi")

	hijackGlobal := global
	hijackGlobal.Name = "dibajak"
	hijackGlobal.OrganizationID = &idA
	_, err = repo.Update(bg, hijackGlobal)
	require.ErrorIs(t, err, domain.ErrPolicyNotFound, "pemanggil tenant tidak dapat mengubah policy global")

	gotB, err := repo.ForUpdate(bg, inB.ID, &idB)
	require.NoError(t, err)
	require.Equal(t, inB.Name, gotB.Name)
	gotG, err := repo.ForUpdate(bg, global.ID, nil)
	require.NoError(t, err)
	require.Equal(t, global.Name, gotG.Name)
}

func TestPolicyWritesRefuseToLockOutsideATransaction(t *testing.T) {
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	repo := pwRepo(db)

	require.ErrorIs(t, repo.LockOrganization(bg, org.ID), repository.ErrNoTransaction)
	require.ErrorIs(t, repo.LockPlatform(bg), repository.ErrNoTransaction)

	err := asTx(db, func(ctx context.Context) error {
		if err := repo.LockOrganization(ctx, org.ID); err != nil {
			return err
		}
		return repo.LockPlatform(ctx)
	})
	require.NoError(t, err, "di dalam transaksi kedua kunci terambil")
}
