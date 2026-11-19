package repository_test

import (
	"context"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
)

func fieldCodes(t *testing.T, repo *repository.Fields, resourceType string) []string {
	t.Helper()
	codes, err := repo.FieldCodes(bg, resourceType)
	require.NoError(t, err)
	slices.Sort(codes)
	return codes
}

func TestFieldCodesReturnsOnlyCodesUnderTheTypesFieldPrefix(t *testing.T) {
	db := testkit.NewDB(t)
	for _, code := range []string{
		"invoice.field.internal_cost.read",
		"invoice.field.internal_cost.write",
		"invoice.field.margin.read",
		"invoice.field.x.field.y.read",
		"invoice.read",
		"invoicex.field.a.read",
		"bill.field.x.read",
	} {
		testkit.EnsurePermission(t, db, code)
	}
	repo := repository.NewFields(db.App)

	got := fieldCodes(t, repo, "invoice")
	require.Equal(t, []string{
		"invoice.field.internal_cost.read",
		"invoice.field.internal_cost.write",
		"invoice.field.margin.read",
		"invoice.field.x.field.y.read",
	}, got, "hanya awalan invoice.field. (superset bersarang boleh), bukan invoice.read, invoicex, atau bill")

	rules, err := domain.NewFieldRules("invoice", got)
	require.NoError(t, err, "keluaran repositori harus dapat langsung dipakai NewFieldRules")
	require.True(t, rules.Restricts("margin", domain.FieldRead))
	require.True(t, rules.Restricts("internal_cost", domain.FieldWrite))
	require.False(t, rules.Restricts("x", domain.FieldRead), "kode tipe bersarang disaring ulang oleh NewFieldRules")
	require.False(t, rules.Restricts("y", domain.FieldRead))
}

func TestFieldCodesTreatsUnderscoreInTheTypeLiterally(t *testing.T) {
	db := testkit.NewDB(t)
	testkit.EnsurePermission(t, db, "a_b.field.x.read")
	testkit.EnsurePermission(t, db, "axb.field.x.read")
	repo := repository.NewFields(db.App)

	require.Equal(t, []string{"a_b.field.x.read"}, fieldCodes(t, repo, "a_b"), "garis bawah bukan wildcard")
	require.Equal(t, []string{"axb.field.x.read"}, fieldCodes(t, repo, "axb"))
}

func TestFieldCodesIsEmptyWithoutFieldsAndRejectsInvalidTypesWithoutQuerying(t *testing.T) {
	db := testkit.NewDB(t)
	repo := repository.NewFields(db.App)

	require.Empty(t, fieldCodes(t, repo, "nothing"))

	for _, typ := range []string{"", "Invoice", "a%", "a b", "invoice."} {
		_, err := repo.FieldCodes(bg, typ)
		require.ErrorIs(t, err, domain.ErrInvalidFieldRules, "tipe %q", typ)
	}
}

func TestFieldCodesSurfacesDatabaseFailureAsAnErrorNotAnEmptyList(t *testing.T) {
	db := testkit.NewDB(t)
	testkit.EnsurePermission(t, db, "invoice.field.margin.read")
	repo := repository.NewFields(db.App)

	canceled, cancel := context.WithCancel(bg)
	cancel()
	codes, err := repo.FieldCodes(canceled, "invoice")
	require.Error(t, err)
	require.Empty(t, codes)
}
