package domain_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/role"
)

func TestNormalizeRoleDraftTrimsTextAndKeepsSlug(t *testing.T) {
	got, err := domain.NormalizeRoleDraft(domain.RoleDraft{
		Name:        "  Keuangan  ",
		Slug:        "keuangan-2",
		Description: "\tmengelola kas\n  ",
	})
	require.NoError(t, err)
	require.Equal(t, "Keuangan", got.Name)
	require.Equal(t, "keuangan-2", got.Slug)
	require.Equal(t, "mengelola kas", got.Description)
}

func TestNormalizeRoleDraftAcceptsEmptyDescription(t *testing.T) {
	got, err := domain.NormalizeRoleDraft(domain.RoleDraft{Name: "Keuangan", Slug: "keuangan"})
	require.NoError(t, err)
	require.Equal(t, "", got.Description)
}

func TestNormalizeRoleDraftRejectsBadNames(t *testing.T) {
	bad := []string{
		"",
		"   ",
		strings.Repeat("a", domain.MaxRoleNameLen+1),
		strings.Repeat("é", domain.MaxRoleNameLen+1),
		"nul\x00byte",
		"dua\nbaris",
		"tab\tdi-tengah",
		"\xff\xfe",
	}
	for _, name := range bad {
		_, err := domain.NormalizeRoleDraft(domain.RoleDraft{Name: name, Slug: "keuangan"})
		require.ErrorIs(t, err, domain.ErrInvalidRoleName, "nama %q", name)
	}
}

func TestNormalizeRoleDraftCountsNameLengthInCharactersNotBytes(t *testing.T) {
	name := strings.Repeat("é", domain.MaxRoleNameLen)
	got, err := domain.NormalizeRoleDraft(domain.RoleDraft{Name: name, Slug: "keuangan"})
	require.NoError(t, err)
	require.Equal(t, name, got.Name)
}

func TestNormalizeRoleDraftRejectsBadSlugs(t *testing.T) {
	bad := []string{
		"",
		"Keuangan",
		"keu angan",
		"keu_angan",
		"-keuangan",
		"keuangan-",
		"keu--angan",
		"keuangan.id",
		"keuangançe",
		strings.Repeat("a", domain.MaxRoleSlugLen+1),
	}
	for _, slug := range bad {
		_, err := domain.NormalizeRoleDraft(domain.RoleDraft{Name: "Keuangan", Slug: slug})
		require.ErrorIs(t, err, domain.ErrInvalidRoleSlug, "slug %q", slug)
	}
}

func TestNormalizeRoleDraftAcceptsWellFormedSlugs(t *testing.T) {
	good := []string{"a", "keuangan", "keuangan-2", "2024-audit", strings.Repeat("a", domain.MaxRoleSlugLen)}
	for _, slug := range good {
		_, err := domain.NormalizeRoleDraft(domain.RoleDraft{Name: "Keuangan", Slug: slug})
		require.NoError(t, err, "slug %q", slug)
	}
}

func TestNormalizeRoleDraftReservesEverySystemRoleSlug(t *testing.T) {
	slugs := []string{
		domain.RoleSlugSuperAdmin,
		domain.RoleSlugOrgAdmin,
		domain.RoleSlugGroupAdmin,
		domain.RoleSlugMember,
	}
	// Setiap role di katalog internal/role juga harus dicadangkan, supaya role sistem baru tidak
	// bisa ditiru namanya oleh role buatan administrator.
	for _, r := range role.SystemRoles() {
		slugs = append(slugs, r.Slug)
	}
	for _, slug := range slugs {
		_, err := domain.NormalizeRoleDraft(domain.RoleDraft{Name: "Tiruan", Slug: slug})
		require.ErrorIs(t, err, domain.ErrReservedRoleSlug, "slug %q", slug)
	}
}

func TestNormalizeRoleDraftDescriptionRules(t *testing.T) {
	ok, err := domain.NormalizeRoleDraft(domain.RoleDraft{
		Name:        "Keuangan",
		Slug:        "keuangan",
		Description: "baris satu\r\nbaris dua\tkolom",
	})
	require.NoError(t, err)
	require.Contains(t, ok.Description, "baris dua")

	long := strings.Repeat("é", domain.MaxRoleDescriptionLen)
	_, err = domain.NormalizeRoleDraft(domain.RoleDraft{Name: "Keuangan", Slug: "keuangan", Description: long})
	require.NoError(t, err, "tepat di batas dihitung dalam karakter")

	bad := []string{
		long + "x",
		"bel\x07",
		"nul\x00",
		"\xff",
	}
	for _, d := range bad {
		_, err := domain.NormalizeRoleDraft(domain.RoleDraft{Name: "Keuangan", Slug: "keuangan", Description: d})
		require.ErrorIs(t, err, domain.ErrInvalidRoleDescription, "deskripsi %q", d)
	}
}

func TestNormalizeRoleTextFollowsTheSameRules(t *testing.T) {
	name, desc, err := domain.NormalizeRoleText("  Baru  ", "  isi  ")
	require.NoError(t, err)
	require.Equal(t, "Baru", name)
	require.Equal(t, "isi", desc)

	_, _, err = domain.NormalizeRoleText("   ", "isi")
	require.ErrorIs(t, err, domain.ErrInvalidRoleName)

	_, _, err = domain.NormalizeRoleText("Baru", "bel\x07")
	require.ErrorIs(t, err, domain.ErrInvalidRoleDescription)
}

func TestNormalizePermissionCodesDeduplicatesAndSorts(t *testing.T) {
	in := []string{"role.read", "user.read", "role.read", "audit.read"}
	got, err := domain.NormalizePermissionCodes(in)
	require.NoError(t, err)
	require.Equal(t, []string{"audit.read", "role.read", "user.read"}, got)
	require.Equal(t, []string{"role.read", "user.read", "role.read", "audit.read"}, in, "masukan tidak boleh diubah")
}

func TestNormalizePermissionCodesEmptyIsAnEmptyNonNilSet(t *testing.T) {
	got, err := domain.NormalizePermissionCodes(nil)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestNormalizePermissionCodesRejectsMalformedCodes(t *testing.T) {
	bad := []string{
		"",
		" role.read",
		"role.read ",
		"Role.read",
		"role",
		"role..read",
		"role.*",
		"*",
		"role.read\n",
		strings.Repeat("a", 300) + ".read",
	}
	for _, code := range bad {
		_, err := domain.NormalizePermissionCodes([]string{"user.read", code})
		require.ErrorIs(t, err, domain.ErrInvalidGrant, "kode %q", code)
	}
}

func TestNormalizePermissionCodesLimitCountsDistinctCodes(t *testing.T) {
	atLimit := make([]string, 0, domain.MaxRolePermissions)
	for i := 0; i < domain.MaxRolePermissions; i++ {
		atLimit = append(atLimit, fmt.Sprintf("res.a%d", i))
	}
	got, err := domain.NormalizePermissionCodes(atLimit)
	require.NoError(t, err)
	require.Len(t, got, domain.MaxRolePermissions)

	over := append(append([]string{}, atLimit...), "res.satu_lagi")
	_, err = domain.NormalizePermissionCodes(over)
	require.ErrorIs(t, err, domain.ErrTooManyPermissions)

	// Duplikat tidak dihitung: seribu salinan satu kode tetap satu kode.
	dups := make([]string, 0, 1000)
	for i := 0; i < 1000; i++ {
		dups = append(dups, "role.read")
	}
	got, err = domain.NormalizePermissionCodes(dups)
	require.NoError(t, err)
	require.Equal(t, []string{"role.read"}, got)
}
