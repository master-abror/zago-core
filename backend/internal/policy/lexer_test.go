package policy

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func kindsOf(toks []token) []tokKind {
	out := make([]tokKind, 0, len(toks))
	for _, tk := range toks {
		out = append(out, tk.kind)
	}
	return out
}

func TestTokenizeKinds(t *testing.T) {
	cases := []struct {
		src  string
		want []tokKind
	}{
		{`resource.status != "open"`, []tokKind{tokIdent, tokDot, tokIdent, tokNe, tokString, tokEOF}},
		{`1 <= 2.5 && !true || null`, []tokKind{tokInt, tokLe, tokFloat, tokAnd, tokNot, tokTrue, tokOr, tokNull, tokEOF}},
		{`x in [1, 2]`, []tokKind{tokIdent, tokIn, tokLBracket, tokInt, tokComma, tokInt, tokRBracket, tokEOF}},
		{`f(-3)`, []tokKind{tokIdent, tokLParen, tokMinus, tokInt, tokRParen, tokEOF}},
		{`1.`, []tokKind{tokInt, tokDot, tokEOF}},
		{`a<b>c`, []tokKind{tokIdent, tokLt, tokIdent, tokGt, tokIdent, tokEOF}},
		{"a \t\n\r b", []tokKind{tokIdent, tokIdent, tokEOF}},
		{`a>=b==c`, []tokKind{tokIdent, tokGe, tokIdent, tokEq, tokIdent, tokEOF}},
	}
	for _, c := range cases {
		toks, err := tokenize(c.src)
		require.NoError(t, err, c.src)
		require.Equal(t, c.want, kindsOf(toks), c.src)
	}
}

func TestTokenizeDecodesStringEscapes(t *testing.T) {
	toks, err := tokenize(`"a\"b\\c\nd\te é"`)
	require.NoError(t, err)
	require.Equal(t, tokString, toks[0].kind)
	require.Equal(t, "a\"b\\c\nd\te é", toks[0].text)
}

func TestTokenizeRecordsPositions(t *testing.T) {
	toks, err := tokenize(`ab == "x"`)
	require.NoError(t, err)
	require.Equal(t, 0, toks[0].pos)
	require.Equal(t, 3, toks[1].pos)
	require.Equal(t, 6, toks[2].pos)
}

func TestTokenizeRejectsUnknownInput(t *testing.T) {
	srcs := []string{
		`a = 1`, `a & b`, `a | b`, `a @ b`, `'x'`, `"abc`, `"a\qb"`, "\"a\x01b\"", `12abc`, `1_000`,
		"é", "\xff", "a\x00b", "`ls`", `$x`, `a ; b`, `{}`, `"abc\`, "\"a\x7fb\"", `a # b`, `a ? b`,
	}
	for _, src := range srcs {
		_, err := tokenize(src)
		require.ErrorIs(t, err, ErrSyntax, "%q", src)
	}
}

func TestTokenizeErrorReportsPositionWithoutEchoingInput(t *testing.T) {
	_, err := tokenize(`a = 1`)
	require.ErrorContains(t, err, "posisi 2")

	_, err = tokenize("a \x01 b")
	require.ErrorIs(t, err, ErrSyntax)
	require.NotContains(t, err.Error(), "\x01")
}

func TestTokenizeLimits(t *testing.T) {
	_, err := tokenize(strings.Repeat("a", MaxExpressionBytes+1))
	require.ErrorIs(t, err, ErrTooComplex, "panjang sumber")

	toks, err := tokenize(strings.Repeat("a ", MaxTokens))
	require.NoError(t, err, "tepat MaxTokens")
	require.Equal(t, tokEOF, toks[len(toks)-1].kind)
	_, err = tokenize(strings.Repeat("a ", MaxTokens+1))
	require.ErrorIs(t, err, ErrTooComplex, "jumlah token")

	_, err = tokenize(strings.Repeat("a", MaxIdentBytes))
	require.NoError(t, err)
	_, err = tokenize(strings.Repeat("a", MaxIdentBytes+1))
	require.ErrorIs(t, err, ErrTooComplex, "pengenal")

	_, err = tokenize(`"` + strings.Repeat("a", MaxStringBytes) + `"`)
	require.NoError(t, err)
	_, err = tokenize(`"` + strings.Repeat("a", MaxStringBytes+1) + `"`)
	require.ErrorIs(t, err, ErrTooComplex, "string")

	_, err = tokenize(strings.Repeat("1", MaxNumberBytes))
	require.NoError(t, err)
	_, err = tokenize(strings.Repeat("1", MaxNumberBytes+1))
	require.ErrorIs(t, err, ErrTooComplex, "angka")
}
