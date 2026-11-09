package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/policy"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func sampleRecord() domain.PolicyRecord {
	org := id.NewID()
	return domain.PolicyRecord{
		ID:             id.NewID(),
		OrganizationID: &org,
		Code:           "invoice.freeze",
		Name:           "Bekukan faktur",
		Description:    "uraian",
		Effect:         policy.EffectDeny,
		Expression:     `resource.status == "paid"`,
		Actions:        []string{"invoice.update", "invoice.delete"},
		Status:         domain.PolicyActive,
	}
}

func TestPolicyRecordGlobalFollowsTheOrganization(t *testing.T) {
	rec := sampleRecord()
	require.False(t, rec.Global())
	rec.OrganizationID = nil
	require.True(t, rec.Global(), "tanpa organisasi = policy global")
}

func TestPolicyRecordDraftCopiesTheFieldsAndTheActions(t *testing.T) {
	rec := sampleRecord()
	d := rec.Draft()
	require.Equal(t, rec.Code, d.Code)
	require.Equal(t, rec.Name, d.Name)
	require.Equal(t, rec.Description, d.Description)
	require.Equal(t, rec.Effect, d.Effect)
	require.Equal(t, rec.Expression, d.Expression)
	require.Equal(t, rec.Actions, d.Actions)

	d.Actions[0] = "x.y"
	require.Equal(t, "invoice.update", rec.Actions[0], "Draft tidak boleh berbagi irisan dengan record")
}

func TestPolicyRecordSameContentIgnoresActionOrderButNothingElse(t *testing.T) {
	a := sampleRecord()
	b := sampleRecord()
	b.Actions = []string{"invoice.delete", "invoice.update"}
	require.True(t, a.SameContent(b), "urutan actions tidak dihitung; ID, kode, dan cakupan juga tidak")

	changes := map[string]func(r *domain.PolicyRecord){
		"nama":      func(r *domain.PolicyRecord) { r.Name = "lain" },
		"deskripsi": func(r *domain.PolicyRecord) { r.Description = "lain" },
		"efek":      func(r *domain.PolicyRecord) { r.Effect = policy.EffectAllow },
		"ekspresi":  func(r *domain.PolicyRecord) { r.Expression = "true" },
		"status":    func(r *domain.PolicyRecord) { r.Status = domain.PolicyDisabled },
		"actions":   func(r *domain.PolicyRecord) { r.Actions = []string{"invoice.update"} },
	}
	for name, change := range changes {
		c := sampleRecord()
		change(&c)
		require.False(t, a.SameContent(c), "perubahan %s harus terdeteksi", name)
	}
}

func TestNormalizePolicyActionsSortsACopy(t *testing.T) {
	in := []string{"invoice.update", "invoice.delete", "account.read"}
	out := domain.NormalizePolicyActions(in)
	require.Equal(t, []string{"account.read", "invoice.delete", "invoice.update"}, out)
	require.Equal(t, []string{"invoice.update", "invoice.delete", "account.read"}, in, "masukan tidak diubah")
	require.Nil(t, domain.NormalizePolicyActions(nil))
}

func TestValidPolicyStatus(t *testing.T) {
	require.True(t, domain.ValidPolicyStatus("active"))
	require.True(t, domain.ValidPolicyStatus("disabled"))
	for _, bad := range []string{"", "Active", "enabled", "deleted", " active"} {
		require.False(t, domain.ValidPolicyStatus(bad), "status %q tidak sah", bad)
	}
}

func TestCheckPolicyActionsProtectsPolicyManagementOnly(t *testing.T) {
	protected := [][]string{
		{"policy.*"},
		{"policy.read"},
		{"policy.manage"},
		{"invoice.read", "policy.manage"},
		{"policy.anything"},
	}
	for _, actions := range protected {
		require.ErrorIs(t, domain.CheckPolicyActions(actions), domain.ErrProtectedPolicyAction, "%v", actions)
	}

	allowed := [][]string{
		{"invoice.read"},
		{"role.*", "role.manage", "organization.member.manage"},
		{"policy.field.secret.read"},
		{"policy.field.*"},
		{"policyx.read"},
		nil,
	}
	for _, actions := range allowed {
		require.NoError(t, domain.CheckPolicyActions(actions), "%v", actions)
	}
}

func TestCheckPolicyActionsFailsClosedOnMalformedActions(t *testing.T) {
	for _, bad := range []string{"", "policy", "Policy.manage", "policy..manage", ".*", "policy.manage "} {
		err := domain.CheckPolicyActions([]string{bad})
		require.Error(t, err, "%q", bad)
		require.NotErrorIs(t, err, domain.ErrProtectedPolicyAction, "%q", bad)
	}
}
