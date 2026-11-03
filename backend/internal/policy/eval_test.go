package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/id"
)

func longList() []any {
	l := make([]any, 0, MaxListElements)
	for i := 0; i < MaxListElements; i++ {
		l = append(l, int64(i+2))
	}
	return l
}

func baseEnv() MapEnv {
	env := MapEnv{}
	env["current_user"] = map[string]any{"id": "u-1", "organization_id": "o-1"}
	env["scope"] = map[string]any{"organization_id": "o-1", "active_group_id": nil}
	env["resource"] = map[string]any{
		"type": "invoice", "status": "open", "amount": int64(100), "limit": 100.0, "owner_id": "u-1",
		"tags": []string{"a", "b"}, "closed": false, "note": nil, "l": longList(),
	}
	return env
}

func evalExpr(t *testing.T, expr string, env Env) (bool, error) {
	t.Helper()
	p, err := Parse(expr)
	require.NoError(t, err, "parse %q", expr)
	return p.Eval(env)
}

func TestEvalTruthTable(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{`resource.status == "open"`, true},
		{`resource.status != "open"`, false},
		{`resource.amount <= 100`, true},
		{`resource.amount < resource.limit`, false},
		{`resource.amount == resource.limit`, true},
		{`resource.amount >= 100.5`, false},
		{`resource.owner_id == current_user.id`, true},
		{`resource.note == null`, true},
		{`resource.note != null`, false},
		{`resource.status == null`, false},
		{`resource.status in ["open", "draft"]`, true},
		{`resource.status in ["closed", "archived"]`, false},
		{`"a" in resource.tags`, true},
		{`"z" in resource.tags`, false},
		{`current_user.id in []`, false},
		{`1 in [1, 2.0]`, true},
		{`true || false && false`, true},
		{`(true || false) && false`, false},
		{`false || true && false`, false},
		{`!false == true`, true},
		{`!resource.closed`, true},
		{`!(resource.amount > 50)`, false},
		{`lower("ABC") == "abc"`, true},
		{`len(resource.tags) == 2`, true},
		{`len("héllo") == 5`, true},
		{`startsWith(resource.status, "op")`, true},
		{`endsWith(resource.status, "en")`, true},
		{`contains(resource.status, "pe")`, true},
		{`contains(resource.status, "zz")`, false},
		{`resource.amount > -5`, true},
		{`-1.5 < 0`, true},
		{`resource.amount >= 100 && resource.status == "open"`, true},
		{`scope.active_group_id == null`, true},
		{`scope.organization_id == current_user.organization_id`, true},
		{`"a" < "b"`, true},
		{`"b" <= "a"`, false},
		{`resource.closed == false`, true},
		{`resource.closed`, false},
		{`true`, true},
		{`false`, false},
	}
	env := baseEnv()
	for _, c := range cases {
		got, err := evalExpr(t, c.expr, env)
		require.NoError(t, err, c.expr)
		require.Equal(t, c.want, got, c.expr)
	}
}

func TestEvalShortCircuitSkipsUnneededOperands(t *testing.T) {
	env := baseEnv()
	got, err := evalExpr(t, `true || resource.missing == 1`, env)
	require.NoError(t, err)
	require.True(t, got)

	got, err = evalExpr(t, `false && resource.missing == 1`, env)
	require.NoError(t, err)
	require.False(t, got)

	_, err = evalExpr(t, `false || resource.missing == 1`, env)
	require.ErrorIs(t, err, ErrAttributeUnavailable)
	_, err = evalExpr(t, `true && resource.missing == 1`, env)
	require.ErrorIs(t, err, ErrAttributeUnavailable)
}

func TestEvalFailsClosedOnTypeAndShapeErrors(t *testing.T) {
	cases := []struct {
		expr string
		want error
	}{
		{`resource.missing == 1`, ErrAttributeUnavailable},
		{`current_user.missing == 1`, ErrAttributeUnavailable},
		{`resource.status == 1`, ErrEvaluation},
		{`resource.status < 1`, ErrEvaluation},
		{`resource.note < 1`, ErrEvaluation},
		{`resource.owner_id == 5`, ErrEvaluation},
		{`resource.closed == 0`, ErrEvaluation},
		{`resource.tags == resource.tags`, ErrEvaluation},
		{`1 in 1`, ErrEvaluation},
		{`1 in [[1]]`, ErrEvaluation},
		{`[1] in [1]`, ErrEvaluation},
		{`"a" in ["a", 1]`, ErrEvaluation},
		{`"a" in [1, "a"]`, ErrEvaluation},
		{`resource.status && true`, ErrEvaluation},
		{`true && resource.status`, ErrEvaluation},
		{`!resource.status`, ErrEvaluation},
		{`resource.amount`, ErrEvaluation},
		{`lower(1) == "a"`, ErrEvaluation},
		{`len(5) == 1`, ErrEvaluation},
		{`startsWith(1, "a")`, ErrEvaluation},
		{`contains("a", 1)`, ErrEvaluation},
	}
	env := baseEnv()
	for _, c := range cases {
		got, err := evalExpr(t, c.expr, env)
		require.ErrorIs(t, err, c.want, c.expr)
		require.False(t, got, "galat tidak boleh disertai true: %s", c.expr)
	}
}

func TestEvalWithoutResourceDistinguishesNoResource(t *testing.T) {
	env := MapEnv{}
	env["current_user"] = map[string]any{"id": "u-1"}

	_, err := evalExpr(t, `resource.status == "open"`, env)
	require.ErrorIs(t, err, ErrNoResource)
	require.NotErrorIs(t, err, ErrAttributeUnavailable)

	got, err := evalExpr(t, `current_user.id == "u-1" || resource.status == "x"`, env)
	require.NoError(t, err, "hubung-singkat: sumber daya tidak dibutuhkan")
	require.True(t, got)

	_, err = evalExpr(t, `current_user.id == "zz" || resource.status == "x"`, env)
	require.ErrorIs(t, err, ErrNoResource)

	got, err = evalExpr(t, `current_user.id == "zz" && resource.status == "x"`, env)
	require.NoError(t, err)
	require.False(t, got)

	// akar selain resource yang hilang BUKAN penundaan
	_, err = evalExpr(t, `scope.organization_id == "o-1"`, env)
	require.ErrorIs(t, err, ErrAttributeUnavailable)
	require.NotErrorIs(t, err, ErrNoResource)
}

func TestEvalNormalizesSupportedAttributeTypes(t *testing.T) {
	u := id.NewID()
	var nilID *uuid.UUID

	cases := []struct {
		name string
		raw  any
		expr string
	}{
		{"int", int(5), `resource.v == 5`},
		{"int8", int8(5), `resource.v == 5`},
		{"int16", int16(5), `resource.v == 5`},
		{"int32", int32(5), `resource.v == 5`},
		{"int64", int64(5), `resource.v == 5`},
		{"uint8", uint8(5), `resource.v == 5`},
		{"uint16", uint16(5), `resource.v == 5`},
		{"uint32", uint32(5), `resource.v == 5`},
		{"uint", uint(5), `resource.v == 5`},
		{"uint64", uint64(5), `resource.v == 5`},
		{"float32", float32(2.5), `resource.v == 2.5`},
		{"float64", 2.5, `resource.v == 2.5`},
		{"json.Number int", json.Number("7"), `resource.v == 7`},
		{"json.Number float", json.Number("7.5"), `resource.v == 7.5`},
		{"uuid.UUID", u, fmt.Sprintf(`resource.v == %q`, u.String())},
		{"*uuid.UUID", &u, fmt.Sprintf(`resource.v == %q`, u.String())},
		{"*uuid.UUID nil", nilID, `resource.v == null`},
		{"nil", nil, `resource.v == null`},
		{"bool", true, `resource.v == true`},
		{"[]any", []any{"b", "a"}, `"a" in resource.v`},
		{"[]string", []string{"x", "y"}, `len(resource.v) == 2`},
	}
	for _, c := range cases {
		env := MapEnv{}
		env["resource"] = map[string]any{"v": c.raw}
		got, err := evalExpr(t, c.expr, env)
		require.NoError(t, err, c.name)
		require.True(t, got, c.name)
	}
}

func TestEvalRejectsUnsupportedOrOversizedAttributes(t *testing.T) {
	cases := []struct {
		name string
		raw  any
	}{
		{"struct", struct{}{}},
		{"map", map[string]any{"a": 1}},
		{"daftar bersarang", []any{[]any{1}}},
		{"slice int tak didukung", []int{1}},
		{"uint64 terlalu besar", uint64(math.MaxUint64)},
		{"NaN", math.NaN()},
		{"+Inf", math.Inf(1)},
		{"float32 NaN", float32(math.NaN())},
		{"json.Number sampah", json.Number("abc")},
		{"string terlalu panjang", strings.Repeat("x", MaxAttrStringBytes+1)},
		{"daftar terlalu panjang", make([]any, MaxListElements+1)},
		{"channel", make(chan int)},
		{"fungsi", func() {}},
	}
	for _, c := range cases {
		env := MapEnv{}
		env["resource"] = map[string]any{"v": c.raw}
		got, err := evalExpr(t, `resource.v == null`, env)
		require.ErrorIs(t, err, ErrEvaluation, c.name)
		require.False(t, got, c.name)
	}
}

func TestEvalStepLimit(t *testing.T) {
	env := baseEnv()

	got, err := evalExpr(t, `1 in resource.l`, env)
	require.NoError(t, err, "satu pencarian di daftar 64 elemen masih di bawah batas")
	require.False(t, got)

	expr := strings.TrimSuffix(strings.Repeat("1 in resource.l || ", 20), " || ")
	p, err := Parse(expr)
	require.NoError(t, err, "ekspresi ini kecil (di bawah batas token) tetapi mahal dievaluasi")
	got, err = p.Eval(env)
	require.ErrorIs(t, err, ErrTooComplex)
	require.False(t, got)
}

func TestEvalErrorsNeverContainAttributeValues(t *testing.T) {
	env := MapEnv{}
	env["resource"] = map[string]any{
		"status": "SECRET-VALUE", "odd": struct{ S string }{S: "SECRET-VALUE"},
	}
	for _, expr := range []string{
		`resource.status == 1`, `resource.status < 1`, `resource.status && true`,
		`resource.odd == null`, `resource.status in 5`, `len(5) == resource.status`,
	} {
		_, err := evalExpr(t, expr, env)
		require.Error(t, err, expr)
		require.NotContains(t, err.Error(), "SECRET-VALUE", expr)
	}
}

func TestEvalPropagatesEnvErrors(t *testing.T) {
	boom := errors.New("env gagal")
	got, err := evalExpr(t, `resource.x == 1`, failingEnv{err: boom})
	require.ErrorIs(t, err, boom)
	require.False(t, got)
}

type failingEnv struct{ err error }

func (f failingEnv) Lookup(string, string) (any, error) { return nil, f.err }

func TestEvalRejectsNilProgramAndNilEnv(t *testing.T) {
	var nilProgram *Program
	_, err := nilProgram.Eval(baseEnv())
	require.ErrorIs(t, err, ErrEvaluation)

	_, err = (&Program{}).Eval(baseEnv())
	require.ErrorIs(t, err, ErrEvaluation)

	p, err := Parse(`true`)
	require.NoError(t, err)
	_, err = p.Eval(nil)
	require.ErrorIs(t, err, ErrEvaluation)
}

func TestProgramIsSafeForConcurrentUse(t *testing.T) {
	p, err := Parse(`resource.amount >= 100 && resource.status in ["open", "draft"]`)
	require.NoError(t, err)
	env := baseEnv()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				got, err := p.Eval(env)
				if err != nil || !got {
					t.Errorf("hasil tak terduga: %v %v", got, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
