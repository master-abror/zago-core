package repository_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
)

// Tes jalur BACA katalog permission (PostgreSQL 18 sungguhan, role app_user).

func prReader(db *testpg.DB) *repository.PermissionReader {
	return repository.NewPermissionReader(db.App)
}

func prCodes(items []domain.PermissionRecord) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Code)
	}
	return out
}

func TestPermissionReaderListsCoreAndModulePermissionsSortedByCode(t *testing.T) {
	db := testkit.NewDB(t)
	testkit.EnsurePermission(t, db, "role.read")
	testkit.EnsurePermission(t, db, "invoice.read")
	pwModulePermission(t, db, "billing", "billing.invoice", "export")

	page, err := prReader(db).List(bg, domain.Window{Limit: domain.MaxPageSize})

	require.NoError(t, err)
	require.Equal(t, 3, page.Total)
	require.Equal(t, []string{"billing.invoice.export", "invoice.read", "role.read"}, prCodes(page.Items))

	byCode := map[string]domain.PermissionRecord{}
	for _, it := range page.Items {
		byCode[it.Code] = it
	}
	core := byCode["role.read"]
	require.Equal(t, "role", core.Resource)
	require.Equal(t, "read", core.Action)
	require.Equal(t, "", core.Module, "permission core tidak punya modul")
	require.True(t, core.System)

	mod := byCode["billing.invoice.export"]
	require.Equal(t, "billing.invoice", mod.Resource)
	require.Equal(t, "export", mod.Action)
	require.Equal(t, "billing", mod.Module)
	require.Equal(t, "milik modul", mod.Description)
	require.False(t, mod.System)
}

func TestPermissionReaderListPagesCoverTheCatalogWithoutOverlap(t *testing.T) {
	db := testkit.NewDB(t)
	for _, code := range []string{"a.one", "b.two", "c.three", "d.four", "e.five"} {
		testkit.EnsurePermission(t, db, code)
	}
	reader := prReader(db)

	full, err := reader.List(bg, domain.Window{Limit: domain.MaxPageSize})
	require.NoError(t, err)
	require.Equal(t, 5, full.Total)

	collected := []string{}
	for offset := 0; offset < full.Total; offset += 2 {
		page, err := reader.List(bg, domain.Window{Offset: offset, Limit: 2})
		require.NoError(t, err)
		require.Equal(t, 5, page.Total)
		require.LessOrEqual(t, len(page.Items), 2)
		collected = append(collected, prCodes(page.Items)...)
	}
	require.Equal(t, prCodes(full.Items), collected)

	beyond, err := reader.List(bg, domain.Window{Offset: 100, Limit: 2})
	require.NoError(t, err)
	require.NotNil(t, beyond.Items, "halaman kosong tetap irisan, bukan null")
	require.Empty(t, beyond.Items)
	require.Equal(t, 5, beyond.Total)
}

func TestPermissionReaderListOnEmptyCatalogReturnsEmptySliceNotNil(t *testing.T) {
	db := testkit.NewDB(t)

	page, err := prReader(db).List(bg, domain.Window{Limit: 10})

	require.NoError(t, err)
	require.NotNil(t, page.Items)
	require.Empty(t, page.Items)
	require.Equal(t, 0, page.Total)
}
