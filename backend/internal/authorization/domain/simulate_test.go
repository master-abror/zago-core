package domain_test

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/policy"
)

func TestNormalizeSimulationAttrsEmptyIsNil(t *testing.T) {
	got, err := domain.NormalizeSimulationAttrs(nil)
	require.NoError(t, err)
	require.Nil(t, got)

	got, err = domain.NormalizeSimulationAttrs(map[string]any{})
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestNormalizeSimulationAttrsAcceptsScalarsAndScalarLists(t *testing.T) {
	in := map[string]any{
		"status":   "draft",
		"amount":   float64(1500),
		"count":    7,
		"big":      int64(9000000000),
		"urgent":   true,
		"note":     nil,
		"tags":     []any{"a", "b", float64(3), true, nil},
		"_private": "x",
		"Mixed9":   "y",
		"num":      json.Number("12"),
		"ratio":    json.Number("0.25"),
	}
	got, err := domain.NormalizeSimulationAttrs(in)
	require.NoError(t, err)
	require.Len(t, got, len(in))
	require.Equal(t, "draft", got["status"])
	require.Equal(t, float64(1500), got["amount"])
	require.Equal(t, int64(7), got["count"])
	require.Equal(t, int64(9000000000), got["big"])
	require.Equal(t, true, got["urgent"])
	require.Nil(t, got["note"])
	require.Equal(t, []any{"a", "b", float64(3), true, nil}, got["tags"])
	require.Equal(t, int64(12), got["num"])
	require.Equal(t, 0.25, got["ratio"])
}

func TestNormalizeSimulationAttrsReturnsCopies(t *testing.T) {
	tags := []any{"a", "b"}
	in := map[string]any{"tags": tags, "k": "v"}

	got, err := domain.NormalizeSimulationAttrs(in)
	require.NoError(t, err)

	got["k"] = "diubah"
	tags[0] = "diubah"
	require.Equal(t, "v", in["k"], "peta masukan tidak boleh berubah")
	require.Equal(t, []any{"a", "b"}, got["tags"], "daftar hasil bukan alias daftar masukan")
}

func TestNormalizeSimulationAttrsRejectsReservedNames(t *testing.T) {
	for _, key := range []string{"type", "id", "organization_id", "owner_id", "group_id"} {
		t.Run(key, func(t *testing.T) {
			_, err := domain.NormalizeSimulationAttrs(map[string]any{key: "x"})
			require.ErrorIs(t, err, domain.ErrReservedSimulationAttr)
		})
	}
}

func TestNormalizeSimulationAttrsRejectsBadNames(t *testing.T) {
	for _, key := range []string{"", "1abc", "a-b", "a b", "a.b", "a/b", "é", "a\x00b", strings.Repeat("a", domain.MaxSimulationAttrKeyLen+1)} {
		t.Run(fmt.Sprintf("%q", key), func(t *testing.T) {
			_, err := domain.NormalizeSimulationAttrs(map[string]any{key: "x"})
			require.ErrorIs(t, err, domain.ErrInvalidSimulationAttrs)
		})
	}

	_, err := domain.NormalizeSimulationAttrs(map[string]any{strings.Repeat("a", domain.MaxSimulationAttrKeyLen): "x"})
	require.NoError(t, err, "kontrol: panjang tepat pada batas sah")
}

func TestNormalizeSimulationAttrsRejectsUnsupportedValues(t *testing.T) {
	cases := []struct {
		name string
		v    any
	}{
		{"objek", map[string]any{"a": "b"}},
		{"daftar_bersarang", []any{[]any{"a"}}},
		{"daftar_objek", []any{map[string]any{"a": "b"}}},
		{"nan", math.NaN()},
		{"inf", math.Inf(1)},
		{"number_buruk", json.Number("abc")},
		{"number_besar", json.Number("1e999")},
		{"slice_string", []string{"a"}},
		{"struct", struct{}{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := domain.NormalizeSimulationAttrs(map[string]any{"k": c.v})
			require.ErrorIs(t, err, domain.ErrInvalidSimulationAttrs)
		})
	}
}

func TestNormalizeSimulationAttrsEnforcesSizeLimits(t *testing.T) {
	tooMany := map[string]any{}
	for i := 0; i <= domain.MaxSimulationAttrs; i++ {
		tooMany[fmt.Sprintf("k%d", i)] = "v"
	}
	_, err := domain.NormalizeSimulationAttrs(tooMany)
	require.ErrorIs(t, err, domain.ErrInvalidSimulationAttrs)

	atLimit := map[string]any{}
	for i := 0; i < domain.MaxSimulationAttrs; i++ {
		atLimit[fmt.Sprintf("k%d", i)] = "v"
	}
	_, err = domain.NormalizeSimulationAttrs(atLimit)
	require.NoError(t, err, "kontrol: tepat pada batas sah")

	_, err = domain.NormalizeSimulationAttrs(map[string]any{"s": strings.Repeat("x", domain.MaxSimulationAttrStringBytes+1)})
	require.ErrorIs(t, err, domain.ErrInvalidSimulationAttrs)
	_, err = domain.NormalizeSimulationAttrs(map[string]any{"s": strings.Repeat("x", domain.MaxSimulationAttrStringBytes)})
	require.NoError(t, err, "kontrol: string tepat pada batas sah")

	items := make([]any, policy.MaxListElements+1)
	_, err = domain.NormalizeSimulationAttrs(map[string]any{"l": items})
	require.ErrorIs(t, err, domain.ErrInvalidSimulationAttrs)
	_, err = domain.NormalizeSimulationAttrs(map[string]any{"l": items[:policy.MaxListElements]})
	require.NoError(t, err, "kontrol: daftar tepat pada batas sah")

	_, err = domain.NormalizeSimulationAttrs(map[string]any{"l": []any{strings.Repeat("x", domain.MaxSimulationAttrStringBytes+1)}})
	require.ErrorIs(t, err, domain.ErrInvalidSimulationAttrs, "elemen daftar tunduk pada batas string")
}

func TestNormalizeSimulationAttrsErrorsDoNotEchoClientInput(t *testing.T) {
	const secret = "nilai-rahasia-klien"
	_, err := domain.NormalizeSimulationAttrs(map[string]any{"bad-" + secret: "x"})
	require.Error(t, err)
	require.NotContains(t, err.Error(), secret)

	_, err = domain.NormalizeSimulationAttrs(map[string]any{"k": map[string]any{secret: secret}})
	require.Error(t, err)
	require.NotContains(t, err.Error(), secret)
}
