package redact_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/redact"
)

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

func TestSensitiveKeysAreRedactedRegardlessOfCaseAndSeparators(t *testing.T) {
	r := redact.Default()
	for _, key := range []string{
		"password", "Password", "PASSWORD", "new_password", "passwordConfirmation", "user-password",
		"access_token", "refreshToken", "session_token", "reset_token", "invitation.token",
		"client_secret", "clientSecret", "session_secret", "mfa_secret", "totp",
		"Authorization", "authorization_header", "X-Api-Key", "api_key", "apiKey", "access-key",
		"private_key", "credentials", "cookie", "Set-Cookie", "card_number", "cvv", "pin", "otp",
		"session_id", "recovery_codes",
	} {
		require.True(t, r.IsSensitiveKey(key), key)
		out, err := r.Map(map[string]any{key: "CANARY-" + key})
		require.NoError(t, err)
		require.Equal(t, redact.Placeholder, out[key], key)
	}
}

func TestBenignKeysAndValuesSurviveUntouched(t *testing.T) {
	r := redact.Default()
	in := map[string]any{
		"action": "invoice.approved", "count": 3, "name": "Budi", "email": "budi@example.com",
		"changed_fields": []any{"status", "amount"}, "ok": true, "ratio": 1.5, "nothing": nil,
		"nested": map[string]any{"role": "admin", "ids": []any{"a", "b"}},
	}
	out, err := r.Map(in)
	require.NoError(t, err)
	require.JSONEq(t, marshal(t, in), marshal(t, out))
}

func TestNestedStructuresAndStructsAreWalked(t *testing.T) {
	type Credentials struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	in := map[string]any{
		"request": map[string]any{
			"headers": []any{map[string]any{"Authorization": "Bearer abc.def.ghi-CANARY1"}, map[string]any{"Accept": "json"}},
			"body":    Credentials{User: "budi", Password: "CANARY2"},
		},
		"list": []map[string]any{{"client_secret": "CANARY3", "keep": "KEEP1"}},
	}
	out, err := redact.Default().Map(in)
	require.NoError(t, err)
	s := marshal(t, out)
	for _, c := range []string{"CANARY1", "CANARY2", "CANARY3"} {
		require.NotContains(t, s, c)
	}
	require.Contains(t, s, "budi")
	require.Contains(t, s, "KEEP1")
	require.Contains(t, s, "json")
}

func TestSecretShapedValuesAreScrubbedUnderBenignKeys(t *testing.T) {
	r := redact.Default()
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r"
	for name, tc := range map[string]struct{ in, secret string }{
		"bearer":       {"header Authorization: Bearer abcdef123456CANARY ok", "abcdef123456CANARY"},
		"basic":        {"Basic dXNlcjpDQU5BUlk=", "dXNlcjpDQU5BUlk"},
		"jwt":          {"token yang bocor " + jwt, jwt},
		"url":          {"postgres://app_user:CANARYpw@localhost:5432/platform", "CANARYpw"},
		"inline eq":    {"gagal login password=CANARYeq untuk user", "CANARYeq"},
		"inline colon": {"api_key: CANARYkey", "CANARYkey"},
	} {
		out, err := r.Map(map[string]any{"note": tc.in})
		require.NoError(t, err, name)
		require.NotContains(t, marshal(t, out), tc.secret, name)
		require.Contains(t, marshal(t, out), redact.Placeholder, name)
	}
	require.Equal(t, "Kata pendek tetap utuh", r.String("Kata pendek tetap utuh"))
	require.Equal(t, "postgres://localhost/db", r.String("postgres://localhost/db"), "URL tanpa kredensial tak berubah")
}

func TestModuleDeclaredKeysAreRedacted(t *testing.T) {
	r := redact.New("national_id", "Salary")
	out, err := r.Map(map[string]any{"NationalID": "CANARY1", "salary": 9000, "name": "Budi"})
	require.NoError(t, err)
	require.Equal(t, redact.Placeholder, out["NationalID"])
	require.Equal(t, redact.Placeholder, out["salary"])
	require.Equal(t, "Budi", out["name"])
	require.False(t, redact.Default().IsSensitiveKey("national_id"), "daftar tambahan tidak bocor ke Default")
}

func TestMapNilAndUnserializableAndDeepNesting(t *testing.T) {
	r := redact.Default()
	out, err := r.Map(nil)
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Empty(t, out)

	_, err = r.Map(map[string]any{"c": make(chan int)})
	require.Error(t, err)

	deep := map[string]any{}
	cur := deep
	for i := 0; i < 40; i++ {
		next := map[string]any{}
		cur["n"] = next
		cur = next
	}
	cur["password"] = "CANARY-DEEP"
	cur["note"] = "CANARY-DEEP-NOTE"
	out, err = r.Map(deep)
	require.NoError(t, err)
	s := marshal(t, out)
	require.NotContains(t, s, "CANARY-DEEP", "terlalu dalam → dipotong, bukan dibiarkan lolos")
	require.Contains(t, s, "[TRUNCATED]")
}

func TestNumbersKeepPrecision(t *testing.T) {
	out, err := redact.Default().Map(map[string]any{"big": int64(9007199254740993)})
	require.NoError(t, err)
	require.Contains(t, marshal(t, out), "9007199254740993")
}

// ---- Properti: tidak ada nilai dari daftar redaksi yang lolos (docs/13 §2.3, §11) ----

var (
	sensitiveBases = []string{"password", "token", "secret", "authorization", "api_key", "access_token",
		"client_secret", "refresh_token", "session_secret", "credential", "private_key", "cookie"}
	benignBases = []string{"note", "name", "action", "count", "email", "status", "role", "title"}
	separators  = []string{"", "_", "-", ".", " "}
)

func mutateKey(rng *rand.Rand, base string, idx int) string {
	parts := strings.FieldsFunc(base, func(r rune) bool { return r == '_' })
	sep := separators[rng.Intn(len(separators))]
	k := strings.Join(parts, sep)
	switch rng.Intn(4) {
	case 0:
		k = strings.ToUpper(k)
	case 1:
		k = strings.ToUpper(k[:1]) + k[1:]
	}
	if rng.Intn(2) == 0 {
		return fmt.Sprintf("%s%d", k, idx)
	}
	return fmt.Sprintf("x%d%s", idx, k)
}

func alnum(rng *rand.Rand, n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rng.Intn(len(chars))]
	}
	return string(b)
}

type gen struct {
	rng      *rand.Rand
	canaries []string // tidak boleh muncul di keluaran
	keeps    []string // harus tetap ada di keluaran
	n        int
}

func (g *gen) canary() string {
	c := "CANARY" + alnum(g.rng, 10)
	g.canaries = append(g.canaries, c)
	return c
}

func (g *gen) keep() string {
	k := "KEEP" + alnum(g.rng, 10)
	g.keeps = append(g.keeps, k)
	return k
}

// secretValue membuat nilai di bawah kunci sensitif: skalar, objek bersarang, atau array.
func (g *gen) secretValue() any {
	switch g.rng.Intn(4) {
	case 0:
		return g.canary()
	case 1:
		return map[string]any{"inner": g.canary(), "list": []any{g.canary(), g.canary()}}
	case 2:
		return []any{g.canary(), map[string]any{"x": g.canary()}}
	default:
		return g.canary()
	}
}

// shapedSecret membuat teks berbentuk rahasia di bawah kunci yang tak mencurigakan.
func (g *gen) shapedSecret() any {
	c := g.canary()
	switch g.rng.Intn(5) {
	case 0:
		return "header Authorization: Bearer " + c + " dikirim"
	case 1:
		return "login password=" + c + " gagal"
	case 2:
		return "postgres://user:" + c + "@db:5432/x"
	case 3:
		return "api_key: " + c
	default:
		jwt := "eyJ" + alnum(g.rng, 12) + ".eyJ" + alnum(g.rng, 12) + "." + alnum(g.rng, 12)
		g.canaries[len(g.canaries)-1] = jwt // yang harus hilang: seluruh JWT
		return "token bocor " + jwt
	}
}

func (g *gen) tree(depth int) map[string]any {
	m := map[string]any{}
	for i, size := 0, 1+g.rng.Intn(5); i < size; i++ {
		g.n++
		switch g.rng.Intn(6) {
		case 0, 1:
			m[mutateKey(g.rng, sensitiveBases[g.rng.Intn(len(sensitiveBases))], g.n)] = g.secretValue()
		case 2:
			m[mutateKey(g.rng, benignBases[g.rng.Intn(len(benignBases))], g.n)] = g.shapedSecret()
		case 3:
			if depth < 4 {
				m[mutateKey(g.rng, benignBases[g.rng.Intn(len(benignBases))], g.n)] = g.tree(depth + 1)
			}
		case 4:
			if depth < 4 {
				m[mutateKey(g.rng, benignBases[g.rng.Intn(len(benignBases))], g.n)] = []any{g.tree(depth + 1), g.keep()}
			}
		default:
			m[mutateKey(g.rng, benignBases[g.rng.Intn(len(benignBases))], g.n)] = g.keep()
		}
	}
	return m
}

func TestPropertyNoSensitiveValueEverSurvives(t *testing.T) {
	seed := time.Now().UnixNano()
	t.Logf("seed=%d (jalankan ulang dengan seed ini bila gagal)", seed)
	rng := rand.New(rand.NewSource(seed))
	r := redact.Default()

	for i := 0; i < 1500; i++ {
		g := &gen{rng: rng}
		in := g.tree(0)
		out, err := r.Map(in)
		require.NoError(t, err)
		s := marshal(t, out)
		for _, c := range g.canaries {
			require.NotContains(t, s, c, "iterasi %d: nilai rahasia lolos", i)
		}
		for _, k := range g.keeps {
			require.Contains(t, s, k, "iterasi %d: nilai biasa ikut tersamar", i)
		}
	}
}
