package policy

import (
	"errors"
	"math/rand"
	"strings"
	"testing"
)

// Sifat yang dijaga untuk MASUKAN APA PUN (ADR-0022 butir 33): tidak panik, tidak menggantung (dijamin
// batas token, kedalaman, dan langkah), setiap galat adalah sentinel yang dikenal, dan Eval tidak pernah
// mengembalikan true bersama galat.
//
// Seed di bawah berjalan sebagai tes biasa. Fuzzing sungguhan (opsional, di luar CI):
//
//	go test ./backend/internal/policy -run '^$' -fuzz FuzzParseEval -fuzztime 60s

func isKnownEvalError(err error) bool {
	return errors.Is(err, ErrEvaluation) || errors.Is(err, ErrTooComplex) ||
		errors.Is(err, ErrNoResource) || errors.Is(err, ErrAttributeUnavailable)
}

func checkParseEval(t *testing.T, src string, env Env) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panik untuk %q: %v", src, r)
		}
	}()
	p, err := Parse(src)
	if err != nil {
		if !errors.Is(err, ErrSyntax) && !errors.Is(err, ErrTooComplex) {
			t.Fatalf("Parse(%q) mengembalikan galat di luar sentinel: %v", src, err)
		}
		if p != nil {
			t.Fatalf("Parse(%q) mengembalikan program bersama galat", src)
		}
		return
	}
	got, err := p.Eval(env)
	if err != nil {
		if !isKnownEvalError(err) {
			t.Fatalf("Eval(%q) mengembalikan galat di luar sentinel: %v", src, err)
		}
		if got {
			t.Fatalf("Eval(%q) mengembalikan true bersama galat", src)
		}
	}
}

var fuzzSeeds = []string{
	`resource.status != "closed"`,
	`resource.owner_id == current_user.id`,
	`resource.amount <= 1000 && resource.status in ["open", "draft"]`,
	`!resource.closed || (scope.organization_id == null)`,
	`startsWith(lower(resource.status), "op") && len(resource.tags) >= 1`,
	`1 in resource.l`,
	`resource.amount > -5.25`,
	`"a\"b\\c" == "x"`,
	`((((((true))))))`,
	`!!!!!!true`,
	`[[[[1]]]] in [1]`,
	"",
	"\x00\xff",
	`resource.x === 1`,
	`'; DROP TABLE policies; --`,
}

func FuzzParseEval(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s)
	}
	env := baseEnv()
	f.Fuzz(func(t *testing.T, src string) {
		checkParseEval(t, src, env)
	})
}

// Kosakata tumpukan token untuk uji acak yang deterministik (berjalan di CI tanpa -fuzz).
var tokenSoup = []string{
	"resource", "current_user", "scope", ".", "status", "amount", "id", "l", "tags", "missing",
	"==", "!=", "<", "<=", ">", ">=", "in", "&&", "||", "!", "(", ")", "[", "]", ",",
	"true", "false", "null", `"open"`, `"a\"b"`, `"unterminated`, "1", "2.5", "-", "0",
	"lower", "len", "startsWith", "endsWith", "contains", "x", "=", "&", "|", "'", "\x00", "é",
}

func TestRandomTokenSoupNeverPanicsAndFailsClosed(t *testing.T) {
	rng := rand.New(rand.NewSource(20261102))
	env := baseEnv()
	for n := 0; n < 5000; n++ {
		var b strings.Builder
		for k := rng.Intn(30) + 1; k > 0; k-- {
			b.WriteString(tokenSoup[rng.Intn(len(tokenSoup))])
			if rng.Intn(3) > 0 {
				b.WriteByte(' ')
			}
		}
		checkParseEval(t, b.String(), env)
	}
}

func TestRandomBytesNeverPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	env := baseEnv()
	for n := 0; n < 2000; n++ {
		buf := make([]byte, rng.Intn(120))
		for i := range buf {
			buf[i] = byte(rng.Intn(256))
		}
		checkParseEval(t, string(buf), env)
	}
}
