package role_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/bootstrap"
	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/internal/role"
)

// Daftar di bawah menyematkan docs/06 §5.5 secara terpisah dari system.go (sumber kebenaran kedua,
// ditulis sebagai string literal, bukan konstanta). Mengubah set default adalah keputusan sadar.
var specMember = []string{
	"profile.read", "profile.update", "notification.read", "inbox.read",
	"conversation.read", "conversation.create", "message.send", "activity.read_own",
}

var specOrgAdminExtra = []string{
	"organization.read", "organization.update", "organization.member.manage",
	"user.read", "user.create", "user.update", "user.delete",
	"group.read", "group.create", "group.update", "group.delete", "group.manage_members",
	"role.read", "role.manage", "role.assign",
	"permission.read", "permission.assign", "permission.simulate",
	"policy.read", "policy.manage",
	"module.read", "module.enable",
	"settings.read", "settings.update",
	"activity.read_all", "activity.export", "activity.read_own",
}

var specGroupAdminExtra = []string{
	"group.read", "group.update", "group.manage_members",
	"user.read",
	"role.read", "role.manage", "role.assign",
	"permission.read", "permission.assign", "permission.simulate",
	"activity.read_group", "activity.export",
}

func sorted(in ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range in {
		for _, s := range list {
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	return out
}

func bySlug(t *testing.T) map[string]role.SystemRole {
	t.Helper()
	out := map[string]role.SystemRole{}
	for _, r := range role.SystemRoles() {
		require.NotContains(t, out, r.Slug, "slug ganda")
		out[r.Slug] = r
	}
	return out
}

func TestSystemRolesAreTheFourSpecRolesInSyncOrder(t *testing.T) {
	var slugs []string
	for _, r := range role.SystemRoles() {
		slugs = append(slugs, r.Slug)
		require.NotEmpty(t, r.Name)
		require.NotEmpty(t, r.Description)
	}
	require.Equal(t, []string{"super-admin", "organization-admin", "group-admin", "member"}, slugs)
}

func TestSuperAdminSlugMatchesBootstrapAdminRow(t *testing.T) {
	require.Equal(t, bootstrap.SuperAdminSlug, role.SlugSuperAdmin,
		"role minimal bootstrap-admin harus diadopsi by slug, bukan digandakan")
}

func TestSuperAdminHoldsEverySystemPermissionByDefinition(t *testing.T) {
	sa := bySlug(t)[role.SlugSuperAdmin]
	require.True(t, sa.AllSystemPermissions)
	require.Empty(t, sa.Permissions, "Super Admin tidak memakai daftar tetap")
}

func TestDefaultSetsMatchTheSpec(t *testing.T) {
	roles := bySlug(t)
	require.Equal(t, sorted(specMember), sorted(roles[role.SlugMember].Permissions))
	require.Equal(t, sorted(specOrgAdminExtra, specMember), sorted(roles[role.SlugOrgAdmin].Permissions))
	require.Equal(t, sorted(specGroupAdminExtra, specMember), sorted(roles[role.SlugGroupAdmin].Permissions))
}

func TestDefaultSetsAreSubsetsOfTheCatalogWithoutDuplicates(t *testing.T) {
	in := map[string]bool{}
	for _, d := range permission.Catalog() {
		in[d.Code] = true
	}
	for _, r := range role.SystemRoles() {
		seen := map[string]bool{}
		for _, code := range r.Permissions {
			require.True(t, in[code], "%s memuat %q yang tidak ada di katalog", r.Slug, code)
			require.False(t, seen[code], "%s memuat %q dua kali", r.Slug, code)
			seen[code] = true
		}
	}
}

func TestMemberSetIsContainedInBothAdminTemplates(t *testing.T) {
	roles := bySlug(t)
	for _, admin := range []string{role.SlugOrgAdmin, role.SlugGroupAdmin} {
		have := map[string]bool{}
		for _, code := range roles[admin].Permissions {
			have[code] = true
		}
		for _, code := range roles[role.SlugMember].Permissions {
			require.True(t, have[code], "%s harus memuat set Member (%q)", admin, code)
		}
	}
}

// Tes negatif eskalasi: hak tingkat platform dan hak yang melampaui grup tidak boleh ikut
// ke template yang bisa ditetapkan oleh admin organisasi/grup.
func TestAdminTemplatesDoNotCarryPlatformOrOutOfScopeAuthority(t *testing.T) {
	roles := bySlug(t)
	has := func(slug, code string) bool {
		for _, c := range roles[slug].Permissions {
			if c == code {
				return true
			}
		}
		return false
	}
	for _, code := range []string{
		"organization.create", "organization.archive", "module.manage", "system_log.read",
	} {
		require.False(t, has(role.SlugOrgAdmin, code), "Organization Admin tidak boleh memegang %q", code)
		require.False(t, has(role.SlugGroupAdmin, code), "Group Admin tidak boleh memegang %q", code)
		require.False(t, has(role.SlugMember, code), "Member tidak boleh memegang %q", code)
	}
	for _, code := range []string{
		"organization.read", "organization.update", "organization.member.manage",
		"user.create", "user.update", "user.delete",
		"group.create", "group.delete", "policy.read", "policy.manage",
		"module.read", "module.enable", "settings.read", "settings.update", "activity.read_all",
	} {
		require.False(t, has(role.SlugGroupAdmin, code), "Group Admin tidak boleh memegang %q", code)
		require.False(t, has(role.SlugMember, code), "Member tidak boleh memegang %q", code)
	}
}

func TestSystemRolesReturnsIndependentCopies(t *testing.T) {
	first := role.SystemRoles()
	for i := range first {
		first[i].Name = "hacked"
		for j := range first[i].Permissions {
			first[i].Permissions[j] = "hacked.code"
		}
	}
	for _, r := range role.SystemRoles() {
		require.NotEqual(t, "hacked", r.Name)
		for _, code := range r.Permissions {
			require.NotEqual(t, "hacked.code", code)
		}
	}
}
