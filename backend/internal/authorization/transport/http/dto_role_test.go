package authzhttp_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// decodeBody mendekode body seperti handler: lewat httpx.DecodeJSON (field tak dikenal ditolak).
func decodeBody(t *testing.T, body string, dst any) error {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return httpx.DecodeJSON(httptest.NewRecorder(), req, dst, 0)
}

func TestRoleRequestsRejectOrganizationAndScopeFromTheBody(t *testing.T) {
	targets := []func() any{
		func() any { return &authzhttp.CreateRoleRequest{} },
		func() any { return &authzhttp.UpdateRoleRequest{} },
		func() any { return &authzhttp.SetPermissionsRequest{} },
	}
	for _, key := range []string{"organization_id", "scope", "scope_type", "tenant_id"} {
		for _, mk := range targets {
			err := decodeBody(t, fmt.Sprintf(`{%q:"x"}`, key), mk())
			require.Error(t, err, key)
			require.True(t, httpx.HasCode(err, httpx.ValidationFailed), key)
			fields := fieldsOf(t, err)
			require.Len(t, fields, 1, key)
			require.Equal(t, key, fields[0].Field)
			require.Equal(t, "unknown_field", fields[0].Code)
		}
	}
}

func TestCreateRoleRequestCarriesEveryFieldIntoTheWriterInput(t *testing.T) {
	gid := id.NewID()
	body := fmt.Sprintf(`{"name":"Finance Staff","slug":"finance-staff","type":"group","description":"Tim keuangan","group_id":%q,"permissions":["invoice.read","invoice.write"]}`, gid.String())
	var q authzhttp.CreateRoleRequest
	require.NoError(t, decodeBody(t, body, &q))

	in := q.Input()
	require.Equal(t, "Finance Staff", in.Name)
	require.Equal(t, "finance-staff", in.Slug)
	require.Equal(t, "group", in.Type)
	require.Equal(t, "Tim keuangan", in.Description)
	require.NotNil(t, in.GroupID)
	require.Equal(t, gid, *in.GroupID)
	require.Equal(t, []string{"invoice.read", "invoice.write"}, in.Permissions)
}

func TestCreateRoleRequestWithoutGroupHasNoGroupAndMalformedGroupIsRejected(t *testing.T) {
	var q authzhttp.CreateRoleRequest
	require.NoError(t, decodeBody(t, `{"name":"A","slug":"a","type":"custom"}`, &q))
	require.Nil(t, q.Input().GroupID)

	var bad authzhttp.CreateRoleRequest
	err := decodeBody(t, `{"name":"A","slug":"a","type":"group","group_id":"bukan-uuid"}`, &bad)
	require.Error(t, err)
	require.True(t, httpx.HasCode(err, httpx.ValidationFailed))
}

func TestUpdateRoleRequestRejectsAnEmptyPatch(t *testing.T) {
	for _, body := range []string{`{}`, `{"name":null}`, `{"name":null,"description":null}`} {
		var q authzhttp.UpdateRoleRequest
		require.NoError(t, decodeBody(t, body, &q), body)
		err := q.Validate()
		require.Error(t, err, body)
		require.True(t, httpx.HasCode(err, httpx.ValidationFailed), body)
		fields := fieldsOf(t, err)
		require.Len(t, fields, 1, body)
		require.Equal(t, "empty_patch", fields[0].Code, body)
	}
}

func TestUpdateRoleRequestMergeKeepsWhatWasNotSent(t *testing.T) {
	var onlyName authzhttp.UpdateRoleRequest
	require.NoError(t, decodeBody(t, `{"name":"Baru"}`, &onlyName))
	require.NoError(t, onlyName.Validate())
	name, desc := onlyName.Merge("Lama", "Deskripsi lama")
	require.Equal(t, "Baru", name)
	require.Equal(t, "Deskripsi lama", desc)

	var onlyDesc authzhttp.UpdateRoleRequest
	require.NoError(t, decodeBody(t, `{"description":"Deskripsi baru"}`, &onlyDesc))
	name, desc = onlyDesc.Merge("Lama", "Deskripsi lama")
	require.Equal(t, "Lama", name)
	require.Equal(t, "Deskripsi baru", desc)

	var wipe authzhttp.UpdateRoleRequest
	require.NoError(t, decodeBody(t, `{"description":""}`, &wipe))
	require.NoError(t, wipe.Validate(), "string kosong adalah perubahan sah (menghapus deskripsi)")
	name, desc = wipe.Merge("Lama", "Deskripsi lama")
	require.Equal(t, "Lama", name)
	require.Equal(t, "", desc)
}

func TestSetPermissionsRequestRequiresThePermissionsField(t *testing.T) {
	for _, body := range []string{`{}`, `{"permissions":null}`} {
		var q authzhttp.SetPermissionsRequest
		require.NoError(t, decodeBody(t, body, &q), body)
		err := q.Validate()
		require.Error(t, err, "%s harus ditolak supaya body salah ketik tidak menghapus permission", body)
		require.True(t, httpx.HasCode(err, httpx.ValidationFailed), body)
		fields := fieldsOf(t, err)
		require.Len(t, fields, 1, body)
		require.Equal(t, "permissions", fields[0].Field, body)
		require.Equal(t, "required", fields[0].Code, body)
	}

	var empty authzhttp.SetPermissionsRequest
	require.NoError(t, decodeBody(t, `{"permissions":[]}`, &empty))
	require.NoError(t, empty.Validate(), "irisan kosong adalah himpunan sah")
	require.NotNil(t, empty.Permissions)
	require.Empty(t, empty.Permissions)

	var some authzhttp.SetPermissionsRequest
	require.NoError(t, decodeBody(t, `{"permissions":["role.read"]}`, &some))
	require.NoError(t, some.Validate())
	require.Equal(t, []string{"role.read"}, some.Permissions)
}

func TestRoleDetailResponseHasAFixedShapeWithoutTheOrganization(t *testing.T) {
	org := id.NewID()
	var rec domain.RoleRecord
	rec.ID = id.NewID()
	rec.OrganizationID = &org
	rec.Name = "Admin Keuangan"
	rec.Slug = "admin-keuangan"
	rec.Description = "Mengelola keuangan"
	rec.Type = domain.RoleTypeCustom
	rec.Status = "active"

	raw, err := json.Marshal(authzhttp.NewRoleDetailResponse(application.RoleResult{Role: rec}))
	require.NoError(t, err)
	require.NotContains(t, string(raw), org.String(), "organization_id tidak ditampilkan")

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	require.ElementsMatch(t, []string{"id", "name", "slug", "description", "type", "status", "group_id", "is_system", "permissions"}, keys)
	require.Equal(t, rec.ID.String(), got["id"])
	require.Equal(t, "admin-keuangan", got["slug"])
	require.Nil(t, got["group_id"])
	require.Equal(t, false, got["is_system"])
	require.Equal(t, []any{}, got["permissions"], "permissions selalu ada: irisan kosong, bukan null")
}

func TestRoleResponseMarksSystemRolesAndCarriesPermissions(t *testing.T) {
	var rec domain.RoleRecord
	rec.ID = id.NewID()
	rec.Name = "Member"
	rec.Slug = "member"
	rec.Type = domain.RoleTypeSystem
	rec.Status = "active"

	require.True(t, authzhttp.NewRoleResponse(rec).IsSystem)
	rec.Type = domain.RoleTypeOrganization
	require.False(t, authzhttp.NewRoleResponse(rec).IsSystem)

	detail := authzhttp.NewRoleDetailResponse(application.RoleResult{Role: rec, Permissions: []string{"profile.read", "profile.update"}})
	require.Equal(t, []string{"profile.read", "profile.update"}, detail.Permissions)
	require.Equal(t, rec.ID, detail.ID, "field role tertanam diratakan")
}
