package policy

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func pol(code string, effect Effect, expr string, actions ...string) Policy {
	return Policy{Code: code, Effect: effect, Expression: expr, Actions: actions}
}

func noResourceEnv() MapEnv {
	env := MapEnv{}
	env["current_user"] = map[string]any{"id": "u-1", "organization_id": "o-1"}
	env["scope"] = map[string]any{"organization_id": "o-1", "active_group_id": nil}
	return env
}

const act = "invoice.update"

func TestActionMatches(t *testing.T) {
	cases := []struct {
		patterns []string
		action   string
		want     bool
	}{
		{[]string{"invoice.read"}, "invoice.read", true},
		{[]string{"invoice.read"}, "invoice.update", false},
		{[]string{"invoice.*"}, "invoice.read", true},
		{[]string{"invoice.*"}, "invoice.line.read", false},
		{[]string{"invoice.line.*"}, "invoice.line.read", true},
		{[]string{"organization.*"}, "organization.member.manage", false},
		{[]string{"organization.member.*"}, "organization.member.manage", true},
		{[]string{"report.read", "invoice.*"}, "invoice.update", true},
		{[]string{}, "invoice.read", false},
		{nil, "invoice.read", false},
		{[]string{"*"}, "invoice.read", false},
		{[]string{".*"}, "invoice.read", false},
		{[]string{"invoice.*"}, "invoiceX.read", false},
		{[]string{"invoice.*"}, "invoice", false},
		{[]string{"invoice.*"}, "", false},
	}
	for _, c := range cases {
		require.Equal(t, c.want, ActionMatches(c.patterns, c.action), "%v vs %q", c.patterns, c.action)
	}
}

func TestZeroOutcomeIsNotAPermit(t *testing.T) {
	require.False(t, Outcome{}.Permits())
	require.Equal(t, VerdictFailed, Outcome{}.Verdict)
}

func TestEvaluateWithoutPoliciesPermits(t *testing.T) {
	out := Evaluate(nil, act, baseEnv())
	require.True(t, out.Permits())
	require.Empty(t, out.Deferred)
}

func TestEvaluateDenyAndAllowOnlyRestrict(t *testing.T) {
	env := baseEnv()
	cases := []struct {
		name string
		pol  Policy
		want Verdict
	}{
		{"deny true menolak", pol("p", EffectDeny, `resource.status == "open"`, act), VerdictDenied},
		{"deny false meloloskan", pol("p", EffectDeny, `resource.status == "closed"`, act), VerdictPermit},
		{"allow true meloloskan", pol("p", EffectAllow, `resource.status == "open"`, act), VerdictPermit},
		{"allow false menolak", pol("p", EffectAllow, `resource.status == "closed"`, act), VerdictDenied},
		{"wildcard resource berlaku", pol("p", EffectDeny, `true`, "invoice.*"), VerdictDenied},
		{"action lain diabaikan", pol("p", EffectDeny, `true`, "invoice.read"), VerdictPermit},
		{"wildcard resource lain diabaikan", pol("p", EffectDeny, `true`, "report.*"), VerdictPermit},
	}
	for _, c := range cases {
		out := Evaluate([]Policy{c.pol}, act, env)
		require.Equal(t, c.want, out.Verdict, c.name)
		if c.want == VerdictDenied {
			require.Equal(t, "p", out.Code, c.name)
		}
	}
}

func TestEvaluateDenyWinsRegardlessOfInputOrder(t *testing.T) {
	env := baseEnv()
	allowTrue := pol("a-allow", EffectAllow, `true`, act)
	denyTrue := pol("b-deny", EffectDeny, `true`, act)
	for _, in := range [][]Policy{{allowTrue, denyTrue}, {denyTrue, allowTrue}} {
		out := Evaluate(in, act, env)
		require.Equal(t, Outcome{Verdict: VerdictDenied, Code: "b-deny"}, out)
	}

	allowFalse := pol("a-allow", EffectAllow, `false`, act)
	denyFalse := pol("b-deny", EffectDeny, `false`, act)
	for _, in := range [][]Policy{{allowFalse, denyFalse}, {denyFalse, allowFalse}} {
		out := Evaluate(in, act, env)
		require.Equal(t, Outcome{Verdict: VerdictDenied, Code: "a-allow"}, out, "satu allow yang salah cukup untuk menolak")
	}
}

func TestEvaluateReportsLowestCodeDeterministically(t *testing.T) {
	env := baseEnv()
	z := pol("z-deny", EffectDeny, `true`, act)
	a := pol("a-deny", EffectDeny, `true`, act)
	for _, in := range [][]Policy{{z, a}, {a, z}} {
		out := Evaluate(in, act, env)
		require.Equal(t, "a-deny", out.Code)
	}
}

func TestEvaluateDoesNotReorderCallersSlice(t *testing.T) {
	in := []Policy{pol("z", EffectDeny, `false`, act), pol("a", EffectDeny, `false`, act)}
	Evaluate(in, act, baseEnv())
	require.Equal(t, "z", in[0].Code)
	require.Equal(t, "a", in[1].Code)
}

func TestEvaluateFailsClosedOnAnyEvaluationProblem(t *testing.T) {
	env := baseEnv()
	cases := []struct {
		name string
		pol  Policy
	}{
		{"atribut hilang padahal sumber daya ada", pol("p", EffectDeny, `resource.missing == 1`, act)},
		{"tipe salah", pol("p", EffectDeny, `resource.status == 1`, act)},
		{"hasil bukan boolean", pol("p", EffectDeny, `resource.amount`, act)},
		{"ekspresi tidak dapat diurai", pol("p", EffectDeny, `resource.status ==`, act)},
		{"ekspresi kosong", pol("p", EffectAllow, ``, act)},
		{"efek tidak dikenal", pol("p", Effect("permit"), `true`, act)},
		{"efek kosong", pol("p", Effect(""), `false`, act)},
		{"melebihi batas langkah", pol("p", EffectDeny, strings.TrimSuffix(strings.Repeat("1 in resource.l || ", 20), " || "), act)},
		{"upaya menjalankan kode", pol("p", EffectDeny, `os.Exit(1)`, act)},
	}
	for _, c := range cases {
		out := Evaluate([]Policy{c.pol}, act, env)
		require.Equal(t, VerdictFailed, out.Verdict, c.name)
		require.Equal(t, "p", out.Code, c.name)
		require.False(t, out.Permits(), c.name)
	}
}

func TestEvaluateBrokenPolicyBlocksEvenWhenOthersWouldPermit(t *testing.T) {
	in := []Policy{
		pol("a-ok", EffectDeny, `false`, act),
		pol("b-broken", EffectDeny, `resource.status ==`, act),
		pol("c-ok", EffectAllow, `true`, act),
	}
	out := Evaluate(in, act, baseEnv())
	require.Equal(t, Outcome{Verdict: VerdictFailed, Code: "b-broken"}, out)
}

func TestEvaluateDefersPoliciesNeedingAResource(t *testing.T) {
	env := noResourceEnv()

	out := Evaluate([]Policy{
		pol("p1", EffectDeny, `resource.status == "closed"`, act),
		pol("p2", EffectAllow, `resource.amount < 1000`, act),
	}, act, env)
	require.True(t, out.Permits())
	require.Equal(t, []string{"p1", "p2"}, out.Deferred)

	// kebijakan yang hanya butuh identitas tetap berlaku walau ada yang ditunda sebelumnya
	out = Evaluate([]Policy{
		pol("p1", EffectDeny, `resource.status == "closed"`, act),
		pol("p2", EffectDeny, `current_user.id == "u-1"`, act),
	}, act, env)
	require.Equal(t, Outcome{Verdict: VerdictDenied, Code: "p2", Deferred: []string{"p1"}}, out)

	// penundaan hanya berlaku untuk ketiadaan sumber daya; akar lain yang hilang tetap gagal-tertutup
	out = Evaluate([]Policy{pol("p", EffectDeny, `scope.nothing == 1`, act)}, act, env)
	require.Equal(t, Outcome{Verdict: VerdictFailed, Code: "p"}, out)
}

func TestEvaluateCapsRelevantPolicies(t *testing.T) {
	build := func(n int, action string) []Policy {
		out := make([]Policy, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, pol(fmt.Sprintf("p-%03d", i), EffectDeny, `false`, action))
		}
		return out
	}
	env := baseEnv()

	require.True(t, Evaluate(build(MaxPoliciesPerCheck, act), act, env).Permits(), "tepat di batas")

	over := Evaluate(build(MaxPoliciesPerCheck+1, act), act, env)
	require.Equal(t, VerdictFailed, over.Verdict, "di atas batas")

	mixed := append(build(MaxPoliciesPerCheck+1, "report.read"), pol("only", EffectDeny, `false`, act))
	require.True(t, Evaluate(mixed, act, env).Permits(), "policy untuk action lain tidak dihitung")
}
