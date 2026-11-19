package policy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/permission"
)

// valCatalog adalah katalog tiruan: kode dan resource turunannya.
type valCatalog struct {
	codes     map[string]bool
	resources map[string]bool
}

func (c valCatalog) HasCode(code string) bool { return c.codes[code] }

func (c valCatalog) HasResource(resource string) bool { return c.resources[resource] }

func newValCatalog(t *testing.T, codes ...string) valCatalog {
	t.Helper()
	c := valCatalog{codes: map[string]bool{}, resources: map[string]bool{}}
	for _, code := range codes {
		resource, _, err := permission.Split(code)
		require.NoError(t, err, code)
		c.codes[code] = true
		c.resources[resource] = true
	}
	return c
}

func defaultValCatalog(t *testing.T) valCatalog {
	t.Helper()
	return newValCatalog(t, "invoice.read", "invoice.approve", "organization.read", "organization.member.manage")
}

func validDraft() Draft {
	return Draft{
		Code:        "invoice_high_value",
		Name:        "Invoice bernilai tinggi",
		Description: "Menolak persetujuan invoice di atas batas.",
		Effect:      EffectDeny,
		Expression:  `resource.amount > 1000`,
		Actions:     []string{"invoice.approve"},
	}
}

func TestValidateAcceptsAWellFormedDraft(t *testing.T) {
	cat := defaultValCatalog(t)

	require.NoError(t, Validate(validDraft(), cat))

	allow := validDraft()
	allow.Effect = EffectAllow
	allow.Description = ""
	allow.Actions = []string{"invoice.read", "invoice.approve"}
	require.NoError(t, Validate(allow, cat))
}

func TestValidateAcceptsWildcardsOnlyForResourcesThatExist(t *testing.T) {
	cat := defaultValCatalog(t)
	cases := []struct {
		action string
		want   error
	}{
		{action: "invoice.*"},
		{action: "organization.*"},
		{action: "organization.member.*"},
		{action: "organization.billing.*", want: ErrUnknownAction},
		{action: "payment.*", want: ErrUnknownAction},
		{action: "organization.member.manage"},
		{action: "organization.member", want: ErrUnknownAction},
	}
	for _, c := range cases {
		d := validDraft()
		d.Actions = []string{c.action}
		err := Validate(d, cat)
		if c.want == nil {
			require.NoError(t, err, c.action)
		} else {
			require.ErrorIs(t, err, c.want, c.action)
		}
	}
}

func TestValidateWildcardDoesNotCoverNestedResources(t *testing.T) {
	// Katalog hanya punya organization.member.manage: resource "organization" tidak ada, jadi
	// "organization.*" tidak akan pernah cocok dengan apa pun (ActionMatches) dan ditolak.
	cat := newValCatalog(t, "organization.member.manage")

	d := validDraft()
	d.Actions = []string{"organization.*"}
	require.ErrorIs(t, Validate(d, cat), ErrUnknownAction)

	d.Actions = []string{"organization.member.*"}
	require.NoError(t, Validate(d, cat))
}

func TestValidateRejectsUnknownAndMalformedActions(t *testing.T) {
	cat := defaultValCatalog(t)
	long := strings.Repeat("a", permission.MaxCodeLength) + ".read"
	cases := []struct {
		name    string
		actions []string
		want    error
	}{
		{name: "nil", actions: nil, want: ErrInvalidActions},
		{name: "kosong", actions: []string{}, want: ErrInvalidActions},
		{name: "action tidak ada di katalog", actions: []string{"invoice.delete"}, want: ErrUnknownAction},
		{name: "salah satu tidak ada", actions: []string{"invoice.read", "invoice.delete"}, want: ErrUnknownAction},
		{name: "ganda", actions: []string{"invoice.read", "invoice.read"}, want: ErrInvalidActions},
		{name: "huruf besar", actions: []string{"Invoice.read"}, want: ErrInvalidActions},
		{name: "satu segmen", actions: []string{"invoice"}, want: ErrInvalidActions},
		{name: "kosong string", actions: []string{""}, want: ErrInvalidActions},
		{name: "bintang saja", actions: []string{"*"}, want: ErrInvalidActions},
		{name: "wildcard tanpa resource", actions: []string{".*"}, want: ErrInvalidActions},
		{name: "wildcard di tengah", actions: []string{"invoice.*.read"}, want: ErrInvalidActions},
		{name: "wildcard ganda", actions: []string{"invoice.*.*"}, want: ErrInvalidActions},
		{name: "dua bintang", actions: []string{"invoice.**"}, want: ErrInvalidActions},
		{name: "spasi", actions: []string{"invoice. read"}, want: ErrInvalidActions},
		{name: "spasi di ujung", actions: []string{"invoice.read "}, want: ErrInvalidActions},
		{name: "terlalu panjang", actions: []string{long}, want: ErrInvalidActions},
		{name: "wildcard ganda dengan duplikat", actions: []string{"invoice.*", "invoice.*"}, want: ErrInvalidActions},
	}
	for _, c := range cases {
		d := validDraft()
		d.Actions = c.actions
		require.ErrorIs(t, Validate(d, cat), c.want, c.name)
	}
}

func TestValidateActionLimitIsFiftyInclusive(t *testing.T) {
	var codes, actions []string
	for i := 0; i < MaxActionsPerPolicy+1; i++ {
		code := fmt.Sprintf("res.a%02d", i)
		codes = append(codes, code)
		actions = append(actions, code)
	}
	cat := newValCatalog(t, codes...)

	d := validDraft()
	d.Actions = actions[:MaxActionsPerPolicy]
	require.NoError(t, Validate(d, cat))

	d.Actions = actions
	require.ErrorIs(t, Validate(d, cat), ErrInvalidActions)
}

func TestValidateErrorsNeverEchoClientValues(t *testing.T) {
	cat := defaultValCatalog(t)
	d := validDraft()
	d.Actions = []string{"secret.thing"}
	err := Validate(d, cat)
	require.ErrorIs(t, err, ErrUnknownAction)
	require.NotContains(t, err.Error(), "secret.thing")

	d.Actions = []string{"Secret.Thing"}
	err = Validate(d, cat)
	require.ErrorIs(t, err, ErrInvalidActions)
	require.NotContains(t, err.Error(), "Secret")

	d = validDraft()
	d.Code = "Secret-Code"
	err = Validate(d, cat)
	require.ErrorIs(t, err, ErrInvalidPolicyCode)
	require.NotContains(t, err.Error(), "Secret")
}

func TestValidateRejectsMalformedCodes(t *testing.T) {
	cat := defaultValCatalog(t)
	cases := []string{"", "Upper", "1abc", "a b", "a..b", ".a", "a.", "a-b", "a\n", "a\x00b", "é", strings.Repeat("a", MaxCodeLength+1)}
	for _, code := range cases {
		d := validDraft()
		d.Code = code
		require.ErrorIs(t, Validate(d, cat), ErrInvalidPolicyCode, "%q", code)
	}

	for _, code := range []string{"a", "invoice_high_value", "billing.invoice.limit", strings.Repeat("a", MaxCodeLength)} {
		d := validDraft()
		d.Code = code
		require.NoError(t, Validate(d, cat), "%q", code)
	}
}

func TestValidateNames(t *testing.T) {
	cat := defaultValCatalog(t)
	bad := []string{"", "  ", " x", "x ", "a\x00b", "a\nb", "a\tb", "\xff", strings.Repeat("a", MaxNameRunes+1), strings.Repeat("é", MaxNameRunes+1)}
	for _, name := range bad {
		d := validDraft()
		d.Name = name
		require.ErrorIs(t, Validate(d, cat), ErrInvalidName, "%q", name)
	}

	for _, name := range []string{"x", "Invoice bernilai tinggi", strings.Repeat("a", MaxNameRunes), strings.Repeat("é", MaxNameRunes)} {
		d := validDraft()
		d.Name = name
		require.NoError(t, Validate(d, cat), "panjang dihitung dalam karakter, bukan byte")
	}
}

func TestValidateDescriptions(t *testing.T) {
	cat := defaultValCatalog(t)
	for _, desc := range []string{"a\x00b", "\xff", strings.Repeat("a", MaxDescriptionRunes+1)} {
		d := validDraft()
		d.Description = desc
		require.ErrorIs(t, Validate(d, cat), ErrInvalidDescription, "%q", desc)
	}

	for _, desc := range []string{"", "baris satu\nbaris dua", strings.Repeat("é", MaxDescriptionRunes)} {
		d := validDraft()
		d.Description = desc
		require.NoError(t, Validate(d, cat))
	}
}

func TestValidateEffects(t *testing.T) {
	cat := defaultValCatalog(t)
	for _, eff := range []Effect{"", "ALLOW", "permit", "Deny", "allow "} {
		d := validDraft()
		d.Effect = eff
		require.ErrorIs(t, Validate(d, cat), ErrInvalidEffect, "%q", eff)
	}
}

func TestValidateRejectsBadExpressions(t *testing.T) {
	cat := defaultValCatalog(t)
	syntax := []string{"", "   ", `"x"`, `resource.amount >`, `exec("rm -rf /")`, `eval("true")`, `resource.a.b == 1`}
	for _, e := range syntax {
		d := validDraft()
		d.Expression = e
		require.ErrorIs(t, Validate(d, cat), ErrSyntax, "%q", e)
	}

	huge := `resource.x == "` + strings.Repeat("a", MaxExpressionBytes+100) + `"`
	d := validDraft()
	d.Expression = huge
	require.ErrorIs(t, Validate(d, cat), ErrTooComplex)

	d.Expression = `lower(resource.name) == "acme" && current_user.id != resource.owner_id`
	require.NoError(t, Validate(d, cat))
}

func TestValidateFailsClosedWithoutACatalog(t *testing.T) {
	require.ErrorIs(t, Validate(validDraft(), nil), ErrCatalogUnavailable)
}

func TestValidateDoesNotModifyTheDraft(t *testing.T) {
	cat := defaultValCatalog(t)
	d := validDraft()
	d.Actions = []string{"invoice.read", "invoice.*", "invoice.approve"}
	before := append([]string(nil), d.Actions...)

	require.NoError(t, Validate(d, cat))

	require.Equal(t, before, d.Actions)
}

func TestCheckPolicyCount(t *testing.T) {
	cases := []struct {
		name   string
		active int
		global bool
		want   error
	}{
		{name: "organisasi kosong", active: 0},
		{name: "organisasi satu di bawah batas", active: MaxPoliciesPerOrganization - 1},
		{name: "organisasi tepat di batas", active: MaxPoliciesPerOrganization, want: ErrTooManyPolicies},
		{name: "organisasi melampaui batas", active: MaxPoliciesPerOrganization + 7, want: ErrTooManyPolicies},
		{name: "global satu di bawah batas", active: MaxGlobalPolicies - 1, global: true},
		{name: "global tepat di batas", active: MaxGlobalPolicies, global: true, want: ErrTooManyPolicies},
		{name: "hitungan negatif organisasi", active: -1, want: ErrTooManyPolicies},
		{name: "hitungan negatif global", active: -1, global: true, want: ErrTooManyPolicies},
	}
	for _, c := range cases {
		err := CheckPolicyCount(c.active, c.global)
		if c.want == nil {
			require.NoError(t, err, c.name)
		} else {
			require.ErrorIs(t, err, c.want, c.name)
		}
	}
}

func TestWriteLimitsKeepEveryCheckWithinTheEvaluationLimit(t *testing.T) {
	// Sebuah check dapat melihat semua policy aktif organisasi ditambah semua policy global.
	require.LessOrEqual(t, MaxPoliciesPerOrganization+MaxGlobalPolicies, MaxPoliciesPerCheck)
}
