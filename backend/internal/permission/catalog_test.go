package permission_test

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/permission"
)

// specCodes menyematkan katalog docs/06 §5.4 (41 kode) sebagai sumber kebenaran kedua. Menambah
// atau menghapus kode core adalah keputusan sadar: ubah daftar ini bersama catalog.go dan ADR.
var specCodes = []string{
	"organization.read", "organization.create", "organization.update", "organization.archive", "organization.member.manage",
	"user.read", "user.create", "user.update", "user.delete",
	"profile.read", "profile.update",
	"group.read", "group.create", "group.update", "group.delete", "group.manage_members",
	"role.read", "role.manage", "role.assign",
	"permission.read", "permission.assign", "permission.simulate",
	"policy.read", "policy.manage",
	"module.read", "module.manage", "module.enable",
	"settings.read", "settings.update",
	"activity.read_own", "activity.read_group", "activity.read_all", "activity.export",
	"system_log.read",
	"notification.read",
	"inbox.read",
	"conversation.read", "conversation.create", "conversation.manage",
	"message.send", "message.moderate",
}

func TestCatalogMatchesTheSpecExactly(t *testing.T) {
	var got []string
	for _, d := range permission.Catalog() {
		got = append(got, d.Code)
	}
	want := append([]string(nil), specCodes...)
	sort.Strings(got)
	sort.Strings(want)
	require.Len(t, want, 41)
	require.Equal(t, want, got)
}

func TestCatalogEntriesAreWellFormedAndUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range permission.Catalog() {
		require.False(t, seen[d.Code], "kode ganda %q", d.Code)
		seen[d.Code] = true

		resource, action, err := permission.Split(d.Code)
		require.NoError(t, err, d.Code)
		require.NotEmpty(t, resource)
		require.NotEmpty(t, action)
		require.NotEmpty(t, d.Description, "deskripsi kosong untuk %q", d.Code)
		require.True(t, permission.IsCoreNamespace(d.Code), d.Code)
	}
}

func TestCatalogReturnsACopy(t *testing.T) {
	first := permission.Catalog()
	first[0].Code = "hacked.code"
	first[0].Description = "hacked"
	second := permission.Catalog()
	require.Equal(t, "organization.read", second[0].Code, "mengubah hasil Catalog() tidak boleh memengaruhi katalog")
}

func TestConstantsAreInTheCatalog(t *testing.T) {
	in := map[string]bool{}
	for _, d := range permission.Catalog() {
		in[d.Code] = true
	}
	for _, code := range []string{
		permission.OrganizationRead, permission.OrganizationMemberManage, permission.UserDelete,
		permission.GroupManageMembers, permission.RoleAssign, permission.PermissionSimulate,
		permission.PolicyManage, permission.ModuleEnable, permission.SettingsUpdate,
		permission.ActivityReadAll, permission.SystemLogRead, permission.InboxRead, permission.MessageModerate,
	} {
		require.True(t, in[code], "konstanta %q tidak ada di katalog", code)
	}
}
