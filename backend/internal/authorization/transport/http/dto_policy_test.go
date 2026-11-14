package authzhttp_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/policy"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func validPolicyRequest() authzhttp.CreatePolicyRequest {
	return authzhttp.CreatePolicyRequest{Code: "no_big", Name: "Tanpa besar", Effect: "deny", Expression: "resource.amount > 1", Actions: []string{"invoice.update"}}
}

func TestPolicyRequestsRejectScopeAndOwnershipFieldsFromTheBody(t *testing.T) {
	for _, key := range []string{"organization_id", "global", "id", "scope", "created_at"} {
		err := decodeBody(t, fmt.Sprintf(`{%q:"x"}`, key), &authzhttp.CreatePolicyRequest{})
		require.Error(t, err, key)
		require.Equal(t, "unknown_field", fieldsOf(t, err)[0].Code, key)
	}
	for _, key := range []string{"code", "organization_id", "global", "id"} {
		err := decodeBody(t, fmt.Sprintf(`{%q:"x"}`, key), &authzhttp.UpdatePolicyRequest{})
		require.Error(t, err, key)
		require.Equal(t, "unknown_field", fieldsOf(t, err)[0].Code, key)
		require.Equal(t, key, fieldsOf(t, err)[0].Field)
	}
}

func TestCreatePolicyRequestValidateReportsEveryProblemAtOnce(t *testing.T) {
	err := (authzhttp.CreatePolicyRequest{Status: "bogus"}).Validate()
	require.Error(t, err)
	require.True(t, httpx.HasCode(err, httpx.ValidationFailed))
	fields := fieldsOf(t, err)
	require.Equal(t, []string{"code", "name", "effect", "expression", "actions", "status"}, asgFieldNames(fields))
	for _, f := range fields[:5] {
		require.Equal(t, "required", f.Code, f.Field)
	}
	require.Equal(t, "invalid", fields[5].Code)

	require.NoError(t, validPolicyRequest().Validate())
	for _, st := range []string{"", "active", "disabled"} {
		q := validPolicyRequest()
		q.Status = st
		require.NoError(t, q.Validate(), st)
	}

	q := validPolicyRequest()
	q.Actions = []string{}
	require.Equal(t, []string{"actions"}, asgFieldNames(fieldsOf(t, q.Validate())), "irisan kosong sama dengan hilang")
}

func TestCreatePolicyRequestInputCopiesActionsAndConvertsEffect(t *testing.T) {
	q := validPolicyRequest()
	q.Description = "d"
	q.Status = "disabled"

	in := q.Input()
	require.Equal(t, "no_big", in.Code)
	require.Equal(t, "Tanpa besar", in.Name)
	require.Equal(t, "d", in.Description)
	require.Equal(t, policy.Effect("deny"), in.Effect)
	require.Equal(t, "resource.amount > 1", in.Expression)
	require.Equal(t, "disabled", in.Status)

	q.Actions[0] = "diubah.setelahnya"
	require.Equal(t, []string{"invoice.update"}, in.Actions, "masukan penulis tidak berbagi irisan dengan body")
}

func TestUpdatePolicyRequestValidateRejectsEmptyPatchAndBadStatus(t *testing.T) {
	err := (authzhttp.UpdatePolicyRequest{}).Validate()
	require.Error(t, err)
	require.Equal(t, "empty_patch", fieldsOf(t, err)[0].Code)

	bad := "bogus"
	err = (authzhttp.UpdatePolicyRequest{Status: &bad}).Validate()
	require.Equal(t, []string{"status"}, asgFieldNames(fieldsOf(t, err)))

	ok := "disabled"
	require.NoError(t, (authzhttp.UpdatePolicyRequest{Status: &ok}).Validate())
	name := "N"
	require.NoError(t, (authzhttp.UpdatePolicyRequest{Name: &name}).Validate())
	require.NoError(t, (authzhttp.UpdatePolicyRequest{Actions: []string{}}).Validate(), "actions kosong diteruskan ke penulis yang menolaknya")
}

func TestUpdatePolicyRequestInputKeepsAbsentFieldsNil(t *testing.T) {
	status := "disabled"
	in := (authzhttp.UpdatePolicyRequest{Status: &status}).Input()
	require.NotNil(t, in.Status)
	require.Equal(t, "disabled", *in.Status)
	require.Nil(t, in.Name)
	require.Nil(t, in.Description)
	require.Nil(t, in.Effect)
	require.Nil(t, in.Expression)
	require.Nil(t, in.Actions)

	effect, desc := "allow", ""
	acts := []string{"a.b"}
	in = (authzhttp.UpdatePolicyRequest{Effect: &effect, Description: &desc, Actions: acts}).Input()
	require.NotNil(t, in.Effect)
	require.Equal(t, policy.Effect("allow"), *in.Effect)
	require.NotNil(t, in.Description)
	require.Equal(t, "", *in.Description, "string kosong menghapus deskripsi")
	acts[0] = "diubah.setelahnya"
	require.Equal(t, []string{"a.b"}, in.Actions)
	require.Nil(t, (authzhttp.UpdatePolicyRequest{}).Input().Status)
}

func TestPolicyResponseShapeGlobalMarkerAndActions(t *testing.T) {
	org := id.NewID()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	rec := domain.PolicyRecord{ID: id.NewID(), OrganizationID: &org, Code: "c", Name: "N", Description: "d", Effect: "deny", Expression: "e", Actions: []string{"a.b"}, Status: "active", CreatedAt: now, UpdatedAt: now}

	resp := authzhttp.NewPolicyResponse(rec)
	require.False(t, resp.Global)
	require.Equal(t, "deny", resp.Effect)
	require.Equal(t, []string{"a.b"}, resp.Actions)

	raw, err := json.Marshal(resp)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	require.NotContains(t, m, "organization_id")
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	require.ElementsMatch(t, []string{"id", "code", "name", "description", "effect", "expression", "actions", "status", "global", "created_at", "updated_at"}, keys)

	resp.Actions[0] = "diubah.setelahnya"
	require.Equal(t, []string{"a.b"}, rec.Actions, "respons tidak berbagi irisan dengan record")

	rec.OrganizationID = nil
	rec.Actions = nil
	global := authzhttp.NewPolicyResponse(rec)
	require.True(t, global.Global)
	require.NotNil(t, global.Actions, "actions tidak pernah null")
	require.Empty(t, global.Actions)
}
