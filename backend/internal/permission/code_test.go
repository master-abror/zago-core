package permission_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/permission"
)

func TestSplitSeparatesAtTheLastDot(t *testing.T) {
	cases := []struct {
		code, resource, action string
	}{
		{"role.read", "role", "read"},
		{"organization.member.manage", "organization.member", "manage"},
		{"activity.read_own", "activity", "read_own"},
		{"invoice.field.internal_cost.read", "invoice.field.internal_cost", "read"},
		{"a1.b2", "a1", "b2"},
	}
	for _, c := range cases {
		resource, action, err := permission.Split(c.code)
		require.NoError(t, err, c.code)
		require.Equal(t, c.resource, resource, c.code)
		require.Equal(t, c.action, action, c.code)
	}
}

func TestSplitRejectsMalformedCodes(t *testing.T) {
	longSegment := strings.Repeat("a", 151)
	bad := []string{
		"",
		"role",
		".read",
		"role.",
		"role..read",
		"Role.read",
		"role.Read",
		"1role.read",
		"role.1read",
		"role-x.read",
		"role.read ",
		" role.read",
		"role.read\n",
		"role.re ad",
		"role/read",
		"role.read;drop",
		"péran.read",
		longSegment + ".read",
		"role." + strings.Repeat("a", 101),
		strings.Repeat("a.", 200) + "b",
	}
	for _, code := range bad {
		_, _, err := permission.Split(code)
		require.ErrorIs(t, err, permission.ErrInvalidCode, "kode %q harus ditolak", code)
	}
}

func TestSplitErrorMessageIsBoundedForHugeInput(t *testing.T) {
	_, _, err := permission.Split(strings.Repeat("x", 10_000))
	require.ErrorIs(t, err, permission.ErrInvalidCode)
	require.Less(t, len(err.Error()), 300, "kode dari manifest modul tidak boleh membanjiri log")
}

func TestNamespaceAndCoreNamespaces(t *testing.T) {
	require.Equal(t, "role", permission.Namespace("role.read"))
	require.Equal(t, "organization", permission.Namespace("organization.member.manage"))
	require.Equal(t, "", permission.Namespace("role"))

	for _, code := range []string{"role.read", "user.anything", "organization.nope", "system_log.export", "message.send"} {
		require.True(t, permission.IsCoreNamespace(code), code)
	}
	for _, code := range []string{"invoice.approve", "billing.read", "role", "", "roles.read"} {
		require.False(t, permission.IsCoreNamespace(code), code)
	}
}
