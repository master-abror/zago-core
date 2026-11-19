package authzhttp_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func smdFieldCodes(t *testing.T, err error) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, f := range fieldsOf(t, err) {
		out[f.Field] = f.Code
	}
	return out
}

func smdValid() authzhttp.SimulateRequest {
	return authzhttp.SimulateRequest{UserID: id.NewID(), Action: "role.read"}
}

func TestSimulateRequestRejectsOrganizationFromTheBody(t *testing.T) {
	for _, key := range []string{"organization_id", "scope", "scope_type", "tenant_id", "evaluated"} {
		err := decodeBody(t, fmt.Sprintf(`{%q:"x"}`, key), &authzhttp.SimulateRequest{})
		require.Error(t, err, key)
		require.True(t, httpx.HasCode(err, httpx.ValidationFailed), key)
		require.Equal(t, "unknown_field", fieldsOf(t, err)[0].Code, key)
	}

	err := decodeBody(t, `{"resource":{"organization_id":"x"}}`, &authzhttp.SimulateRequest{})
	require.Error(t, err, "organisasi sumber daya juga tidak boleh datang dari body")
	require.True(t, httpx.HasCode(err, httpx.ValidationFailed))
	require.Equal(t, "unknown_field", fieldsOf(t, err)[0].Code)
}

func TestSimulateRequestValidateReportsEveryMissingFieldAtOnce(t *testing.T) {
	err := authzhttp.SimulateRequest{}.Validate()
	require.Error(t, err)
	require.True(t, httpx.HasCode(err, httpx.ValidationFailed))
	require.Equal(t, map[string]string{"user_id": "required", "action": "required"}, smdFieldCodes(t, err))

	req := authzhttp.SimulateRequest{Resource: &authzhttp.SimulateResourceRequest{}}
	err = req.Validate()
	require.Equal(t, map[string]string{
		"user_id":       "required",
		"action":        "required",
		"resource.type": "required",
		"resource.id":   "required",
	}, smdFieldCodes(t, err))
}

func TestSimulateRequestValidateAcceptsAWellFormedBody(t *testing.T) {
	require.NoError(t, smdValid().Validate())

	req := smdValid()
	group, owner := id.NewID(), id.NewID()
	req.ActiveGroupID = &group
	req.Resource = &authzhttp.SimulateResourceRequest{Type: "invoice", ID: id.NewID(), OwnerID: &owner, GroupID: &group}
	require.NoError(t, req.Validate())
}

func TestSimulateRequestValidateRejectsMalformedActions(t *testing.T) {
	for _, action := range []string{"bad", "Role.Read", "role..read", "role.", ".read", "role read"} {
		req := smdValid()
		req.Action = action

		err := req.Validate()

		require.Error(t, err, action)
		require.Equal(t, map[string]string{"action": "invalid"}, smdFieldCodes(t, err), action)
	}
}

func TestSimulateRequestValidateRejectsNilIdentifiers(t *testing.T) {
	req := smdValid()
	nilID := uuid.Nil
	req.ActiveGroupID = &nilID
	req.Resource = &authzhttp.SimulateResourceRequest{Type: "invoice", ID: id.NewID(), OwnerID: &nilID, GroupID: &nilID}

	err := req.Validate()

	require.Equal(t, map[string]string{
		"active_group_id":   "invalid",
		"resource.owner_id": "invalid",
		"resource.group_id": "invalid",
	}, smdFieldCodes(t, err))
}

func TestSimulateRequestValidateChecksTheResourceTypePattern(t *testing.T) {
	good := []string{"invoice", "a", "a_1", "order_item2", strings.Repeat("a", 100)}
	bad := []string{"Invoice", "1a", "_a", "a-b", "a b", "é", "a.b", strings.Repeat("a", 101)}

	for _, typ := range good {
		req := smdValid()
		req.Resource = &authzhttp.SimulateResourceRequest{Type: typ, ID: id.NewID()}
		require.NoError(t, req.Validate(), typ)
	}
	for _, typ := range bad {
		req := smdValid()
		req.Resource = &authzhttp.SimulateResourceRequest{Type: typ, ID: id.NewID()}

		err := req.Validate()

		require.Error(t, err, typ)
		require.Equal(t, map[string]string{"resource.type": "invalid"}, smdFieldCodes(t, err), typ)
	}
}

func TestSimulateRequestInputCarriesEveryFieldAndDoesNotAliasPointers(t *testing.T) {
	userID, groupID, resID, ownerID, resGroup := id.NewID(), id.NewID(), id.NewID(), id.NewID(), id.NewID()
	body := fmt.Sprintf(`{"user_id":%q,"action":"invoice.approve","active_group_id":%q,"resource":{"type":"invoice","id":%q,"owner_id":%q,"group_id":%q,"attrs":{"amount":1500,"status":"draft"}}}`,
		userID, groupID, resID, ownerID, resGroup)
	var q authzhttp.SimulateRequest
	require.NoError(t, decodeBody(t, body, &q))
	require.NoError(t, q.Validate())

	in := q.Input()

	require.Equal(t, userID, in.UserID)
	require.Equal(t, "invoice.approve", in.Action)
	require.NotNil(t, in.ActiveGroupID)
	require.Equal(t, groupID, *in.ActiveGroupID)
	require.NotNil(t, in.Resource)
	require.Equal(t, "invoice", in.Resource.Type)
	require.Equal(t, resID, in.Resource.ID)
	require.Equal(t, ownerID, *in.Resource.OwnerID)
	require.Equal(t, resGroup, *in.Resource.GroupID)
	require.Equal(t, map[string]any{"amount": float64(1500), "status": "draft"}, in.Resource.Attrs)

	*q.ActiveGroupID = id.NewID()
	*q.Resource.OwnerID = id.NewID()
	require.Equal(t, groupID, *in.ActiveGroupID, "masukan tidak beralias pointer body")
	require.Equal(t, ownerID, *in.Resource.OwnerID)
}

func TestSimulateRequestInputWithoutResourceHasNone(t *testing.T) {
	in := smdValid().Input()

	require.Nil(t, in.Resource)
	require.Nil(t, in.ActiveGroupID)
}

func TestNewSimulateResponseAllowCarriesLevelAndPolicyDetails(t *testing.T) {
	deferred := []string{"needs_owner"}
	resp := authzhttp.NewSimulateResponse(application.SimulationResult{
		Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelGroup, DeferredPolicies: deferred,
	})

	require.Equal(t, "ALLOW", resp.Result)
	require.Equal(t, "granted", resp.Reason)
	require.NotNil(t, resp.Level)
	require.Equal(t, "group", *resp.Level)
	require.Nil(t, resp.PolicyCode)
	require.Equal(t, []string{"needs_owner"}, resp.DeferredPolicies)

	deferred[0] = "diubah"
	require.Equal(t, []string{"needs_owner"}, resp.DeferredPolicies, "respons tidak beralias daftar hasil")
}

func TestNewSimulateResponseDenyHasNullLevelAndOptionalPolicyCode(t *testing.T) {
	resp := authzhttp.NewSimulateResponse(application.SimulationResult{
		Reason: domain.ReasonPolicyDenied, PolicyCode: "no_weekend",
	})

	require.Equal(t, "DENY", resp.Result)
	require.Nil(t, resp.Level)
	require.NotNil(t, resp.PolicyCode)
	require.Equal(t, "no_weekend", *resp.PolicyCode)
}

func TestSimulateResponseJSONHasStableShapeWithoutNullLists(t *testing.T) {
	raw, err := json.Marshal(authzhttp.NewSimulateResponse(application.SimulationResult{Reason: domain.ReasonNoPermission}))
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Equal(t, map[string]any{
		"result":            "DENY",
		"reason":            "no_permission",
		"level":             nil,
		"policy_code":       nil,
		"deferred_policies": []any{},
	}, got)
	require.NotContains(t, string(raw), "evaluated", "respons ringkas tanpa evaluated")
}
