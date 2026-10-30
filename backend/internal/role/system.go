// Package role memuat definisi role sistem yang dideklarasikan di kode (docs/06 §5.5, E:B-04) dan
// sinkronisasinya ke tabel roles/role_permissions. Role sistem bersifat global: role_type='system',
// organization_id dan group_id NULL (ck_roles_boundary), dan ditetapkan pada scope yang sesuai.
package role

import "github.com/master-abror/zago-core/backend/internal/permission"

// Slug role sistem (ADR-0022 butir 6). SlugSuperAdmin sama dengan baris minimal bootstrap-admin.
const (
	SlugSuperAdmin = "super-admin"
	SlugOrgAdmin   = "organization-admin"
	SlugGroupAdmin = "group-admin"
	SlugMember     = "member"
)

// SystemRole mendefinisikan satu role sistem.
type SystemRole struct {
	Slug        string
	Name        string
	Description string

	// AllSystemPermissions: role memegang SEMUA permission core is_system yang ada di database
	// (Super Admin), bukan daftar tetap. Permissions diabaikan bila true.
	AllSystemPermissions bool

	// Permissions adalah set default permission core (kode).
	Permissions []string
}

// SystemRoles mengembalikan definisi role sistem dalam urutan sinkron. Setiap pemanggilan
// membangun irisan baru, jadi pemanggil boleh mengubah hasilnya tanpa memengaruhi definisi.
func SystemRoles() []SystemRole {
	member := memberSet()
	return []SystemRole{
		{
			Slug:                 SlugSuperAdmin,
			Name:                 "Super Admin",
			Description:          "Akses penuh tingkat platform: seluruh permission core.",
			AllSystemPermissions: true,
		},
		{
			Slug:        SlugOrgAdmin,
			Name:        "Organization Admin",
			Description: "Administrator organisasi (template sistem, ditetapkan pada scope organization).",
			Permissions: union([]string{
				permission.OrganizationRead, permission.OrganizationUpdate, permission.OrganizationMemberManage,
				permission.UserRead, permission.UserCreate, permission.UserUpdate, permission.UserDelete,
				permission.GroupRead, permission.GroupCreate, permission.GroupUpdate, permission.GroupDelete, permission.GroupManageMembers,
				permission.RoleRead, permission.RoleManage, permission.RoleAssign,
				permission.PermissionRead, permission.PermissionAssign, permission.PermissionSimulate,
				permission.PolicyRead, permission.PolicyManage,
				permission.ModuleRead, permission.ModuleEnable,
				permission.SettingsRead, permission.SettingsUpdate,
				permission.ActivityReadAll, permission.ActivityExport, permission.ActivityReadOwn,
			}, member),
		},
		{
			Slug:        SlugGroupAdmin,
			Name:        "Group Admin",
			Description: "Administrator grup (template sistem, ditetapkan pada scope group; tunduk pada authority ceiling).",
			Permissions: union([]string{
				permission.GroupRead, permission.GroupUpdate, permission.GroupManageMembers,
				permission.UserRead,
				permission.RoleRead, permission.RoleManage, permission.RoleAssign,
				permission.PermissionRead, permission.PermissionAssign, permission.PermissionSimulate,
				permission.ActivityReadGroup, permission.ActivityExport,
			}, member),
		},
		{
			Slug:        SlugMember,
			Name:        "Member",
			Description: "Anggota organisasi (template sistem, ditetapkan pada scope organization).",
			Permissions: member,
		},
	}
}

func memberSet() []string {
	return []string{
		permission.ProfileRead, permission.ProfileUpdate,
		permission.NotificationRead, permission.InboxRead,
		permission.ConversationRead, permission.ConversationCreate,
		permission.MessageSend, permission.ActivityReadOwn,
	}
}

// union menggabungkan daftar kode tanpa duplikat, mempertahankan urutan kemunculan pertama.
func union(lists ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range lists {
		for _, code := range list {
			if !seen[code] {
				seen[code] = true
				out = append(out, code)
			}
		}
	}
	return out
}
