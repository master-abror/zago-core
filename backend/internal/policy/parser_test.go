package policy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAcceptsWellFormedExpressions(t *testing.T) {
	exprs := []string{
		`resource.status != "closed"`,
		`resource.owner_id == current_user.id`,
		`resource.amount <= 1000 && resource.status in ["open", "draft"]`,
		`!resource.archived`,
		`(resource.a == 1 || resource.b == 2) && !(scope.organization_id == null)`,
		`startsWith(resource.code, "INV-")`,
		`len(resource.tags) > 0`,
		`lower(resource.status) == "open"`,
		`true`,
		`false`,
		`resource.amount > -5`,
		`resource.amount > -5.25`,
		`resource.ok`,
		`current_user.id in []`,
		`1 in [1, 2.0, "x", null, true]`,
		`contains(lower(resource.title), "urgent") || endsWith(resource.code, "-X")`,
	}
	for _, e := range exprs {
		p, err := Parse(e)
		require.NoError(t, err, e)
		require.NotNil(t, p, e)
	}
}

func TestParseRejectsMalformedAndHostileExpressions(t *testing.T) {
	exprs := []string{
		// bentuk salah
		"", "   ", `"x"`, `1`, `null`, `[true]`, `lower("A")`, `len("a")`,
		`status == 1`, `foo.bar == 1`, `resource.a.b == 1`, `resource. == 1`, `resource.in == 1`,
		`1 == 1 == true`, `resource.a == resource.b == resource.c`, `1 < 2 in [true]`,
		`(`, `)`, `(true`, `true)`, `resource.x ==`, `== 1`, `[1,]`, `[1 2]`, `[`, `lower(`,
		`unknown(1)`, `lower()`, `lower("a", "b")`, `startsWith("a")`,
		`- "a"`, `-resource.x == 1`, `1 + 2`, `!`, `resource.x === 1`, `in`, `true true`, `true,`,
		`1. == 1`, `99999999999999999999 == 1`, `1e5 == 1`, `0x10 == 16`,
		// upaya menjalankan kode atau menyuntikkan
		`os.Exit(1)`, `exec("rm -rf /")`, `__import__("os")`, `resource.status.__proto__ == 1`,
		`"; DROP TABLE users; --"`, "`ls`", `${HOME} == 1`, `(function(){})()`, `import "os"`,
		`resource.status == 'open'`, `resource.x; resource.y`, `resource.x /* c */ == 1`, `resource.x // c`,
		`resource["status"] == 1`, `resource.status() == 1`, `current_user.id.String() == "x"`,
		`eval("true")`, `system("id")`, `new Object()`, `a => a`,
	}
	for _, e := range exprs {
		p, err := Parse(e)
		require.ErrorIs(t, err, ErrSyntax, "%q", e)
		require.Nil(t, p, "%q", e)
	}
}

func TestParseDepthLimit(t *testing.T) {
	nest := func(open, body, shut string, n int) string {
		return strings.Repeat(open, n) + body + strings.Repeat(shut, n)
	}

	_, err := Parse(nest("(", "true", ")", MaxDepth))
	require.NoError(t, err, "kurung tepat di batas")
	_, err = Parse(nest("(", "true", ")", MaxDepth+1))
	require.ErrorIs(t, err, ErrTooComplex, "kurung")

	_, err = Parse(strings.Repeat("!", MaxDepth) + "true")
	require.NoError(t, err, "! tepat di batas")
	_, err = Parse(strings.Repeat("!", MaxDepth+1) + "true")
	require.ErrorIs(t, err, ErrTooComplex, "!")

	_, err = Parse("1 in " + nest("[", "1", "]", MaxDepth+1))
	require.ErrorIs(t, err, ErrTooComplex, "daftar bersarang")

	_, err = Parse(strings.Repeat("startsWith(", MaxDepth+1))
	require.ErrorIs(t, err, ErrTooComplex, "panggilan bersarang")
}

func TestParseListElementLimit(t *testing.T) {
	list := func(n int) string {
		return "1 in [" + strings.TrimSuffix(strings.Repeat("1, ", n), ", ") + "]"
	}
	_, err := Parse(list(MaxListElements))
	require.NoError(t, err)
	_, err = Parse(list(MaxListElements + 1))
	require.ErrorIs(t, err, ErrTooComplex)
}

func TestParseLimitsPropagateFromTokenizer(t *testing.T) {
	_, err := Parse(strings.Repeat("true && ", MaxTokens) + "true")
	require.ErrorIs(t, err, ErrTooComplex)
	_, err = Parse(strings.Repeat("a", MaxExpressionBytes+1))
	require.ErrorIs(t, err, ErrTooComplex)
}

func TestParseErrorsDoNotEchoLongInput(t *testing.T) {
	_, err := Parse(`resource.status == "` + strings.Repeat("x", 200))
	require.ErrorIs(t, err, ErrSyntax)
	require.Less(t, len(err.Error()), 200, "pesan tidak boleh menggandakan input")
}
