package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
)

func TestClampWindowKeepsSaneValues(t *testing.T) {
	got := domain.ClampWindow(domain.Window{Offset: 50, Limit: 10})

	require.Equal(t, 50, got.Offset)
	require.Equal(t, 10, got.Limit)
}

func TestClampWindowRepairsNegativeOffsetAndMissingLimit(t *testing.T) {
	got := domain.ClampWindow(domain.Window{Offset: -7, Limit: 0})

	require.Equal(t, 0, got.Offset, "offset negatif tidak boleh sampai ke SQL")
	require.Equal(t, domain.DefaultPageSize, got.Limit, "limit nol memakai ukuran bawaan")

	neg := domain.ClampWindow(domain.Window{Offset: 0, Limit: -3})
	require.Equal(t, domain.DefaultPageSize, neg.Limit, "limit negatif memakai ukuran bawaan")
}

func TestClampWindowCapsLimitAtMaxPageSize(t *testing.T) {
	got := domain.ClampWindow(domain.Window{Offset: 0, Limit: domain.MaxPageSize + 1})
	require.Equal(t, domain.MaxPageSize, got.Limit)

	huge := domain.ClampWindow(domain.Window{Offset: 0, Limit: 1 << 30})
	require.Equal(t, domain.MaxPageSize, huge.Limit, "permintaan halaman raksasa dipangkas, bukan dilayani")

	exact := domain.ClampWindow(domain.Window{Offset: 0, Limit: domain.MaxPageSize})
	require.Equal(t, domain.MaxPageSize, exact.Limit)
}

func TestValidRoleFilterAcceptsOnlyKnownValues(t *testing.T) {
	require.True(t, domain.ValidRoleFilter(domain.RoleFilterActive))
	require.True(t, domain.ValidRoleFilter(domain.RoleFilterArchived))
	require.True(t, domain.ValidRoleFilter(domain.RoleFilterAll))

	require.False(t, domain.ValidRoleFilter(""), "kosong ditangani pemanggil sebagai bawaan, bukan nilai sah")
	require.False(t, domain.ValidRoleFilter("ACTIVE"))
	require.False(t, domain.ValidRoleFilter("active "))
	require.False(t, domain.ValidRoleFilter("everything"))
}
