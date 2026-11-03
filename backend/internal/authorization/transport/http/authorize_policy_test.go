package authzhttp_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

func TestAuthorizePolicyDenialLooksLikeAnOrdinaryPermissionDeniedToTheClient(t *testing.T) {
	ident := tenantIdentity()
	scope := scopeOf(t, ident)
	audit := &fakeAudit{}
	r := newAuthzRig(t, &fakeAuthorizer{decision: domain.Decision{Reason: domain.ReasonPolicyDenied, PolicyCode: "block-closed"}}, audit)

	w := r.do(&ident, &scope)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, "permission_denied", errCode(t, w))
	require.False(t, r.reached)
	require.NotContains(t, w.Body.String(), "block-closed", "kode policy tidak boleh bocor ke klien")
	require.NotContains(t, w.Body.String(), "policy", "keberadaan policy tidak boleh bocor ke klien")

	require.Len(t, audit.calls, 1)
	require.Equal(t, domain.ReasonPolicyDenied, audit.calls[0].entry.Metadata["reason"])
	require.Equal(t, "block-closed", audit.calls[0].entry.Metadata["policy_code"])
	require.Equal(t, routeAction, audit.calls[0].entry.Metadata["required_action"])
}

func TestAuthorizePolicyEvaluationFailureHasItsOwn403Code(t *testing.T) {
	ident := tenantIdentity()
	scope := scopeOf(t, ident)
	audit := &fakeAudit{}
	r := newAuthzRig(t, &fakeAuthorizer{decision: domain.Decision{Reason: domain.ReasonPolicyEvaluationFailed, PolicyCode: "broken-one"}}, audit)

	w := r.do(&ident, &scope)

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Equal(t, "policy_evaluation_failed", errCode(t, w))
	require.False(t, r.reached)
	require.NotContains(t, w.Body.String(), "broken-one", "kode policy tidak boleh bocor ke klien")

	require.Len(t, audit.calls, 1)
	require.Equal(t, domain.ReasonPolicyEvaluationFailed, audit.calls[0].entry.Metadata["reason"])
	require.Equal(t, "broken-one", audit.calls[0].entry.Metadata["policy_code"])
}

func TestAuthorizePolicyCodeIsAuditedOnlyForPolicyReasons(t *testing.T) {
	ident := tenantIdentity()
	scope := scopeOf(t, ident)
	audit := &fakeAudit{}
	r := newAuthzRig(t, &fakeAuthorizer{decision: domain.Decision{Reason: domain.ReasonNoPermission, PolicyCode: "stray"}}, audit)

	r.do(&ident, &scope)

	require.Len(t, audit.calls, 1)
	require.NotContains(t, audit.calls[0].entry.Metadata, "policy_code")
}

func TestAuthorizeAllowedDecisionWithDeferredPoliciesStillReachesHandler(t *testing.T) {
	ident := tenantIdentity()
	scope := scopeOf(t, ident)
	dec := domain.Decision{Allowed: true, Reason: domain.ReasonGranted, Level: domain.LevelOrganization, DeferredPolicies: []string{"needs-resource"}}
	r := newAuthzRig(t, &fakeAuthorizer{decision: dec}, &fakeAudit{})

	w := r.do(&ident, &scope)

	require.Equal(t, http.StatusNoContent, w.Code)
	require.True(t, r.reached)
}

func TestPolicyEvaluationFailedIsRegisteredAs403WithItsOwnCode(t *testing.T) {
	got, ok := httpx.Lookup("policy_evaluation_failed")
	require.True(t, ok, "kode harus ada di registry httpx")
	require.Equal(t, authzhttp.PolicyEvaluationFailed, got)
	require.Equal(t, http.StatusForbidden, got.Status)
	require.NotEqual(t, httpx.PermissionDenied.Name, got.Name)
	require.NotEmpty(t, got.Message)
	require.NotContains(t, got.Message, "policy", "pesan klien tidak menyebut policy")
}
