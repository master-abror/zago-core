package bootstrap_test

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/app"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
)

// Tes lapisan CLI (app.BootstrapAdmin): argumen, stdin, kode keluar, dan kebocoran rahasia.
// Database sungguhan dipakai untuk jalur sukses; jalur galat memakai DATABASE_URL yang tak
// terjangkau untuk membuktikan bahwa input buruk berhenti SEBELUM menyentuh database.

const unreachableDB = "postgres://app_user:SECRET-DB-PW@127.0.0.1:1/platform"

var passwordLine = regexp.MustCompile(`(?m)^password: (\S+)$`)

func cliEnv(dbURL string) map[string]string {
	return map[string]string{
		"ENVIRONMENT":                 "test",
		"DATABASE_URL":                dbURL,
		"REDIS_URL":                   "redis://localhost:6379",
		"SESSION_SECRET":              "dev-only-change-me-dev-only-change-me",
		"COOKIE_SECURE":               "false",
		"ALLOWED_ORIGINS":             "http://localhost:5173",
		"PUBLIC_BASE_URL":             "http://localhost:5173",
		"PASSWORD_ARGON2_MEMORY_KIB":  "64",
		"PASSWORD_ARGON2_TIME":        "1",
		"PASSWORD_ARGON2_PARALLELISM": "1",
	}
}

type cliResult struct {
	code   int
	stdout string
	stderr string
}

func runCLI(t *testing.T, env map[string]string, stdin string, args ...string) cliResult {
	t.Helper()
	var out, errOut bytes.Buffer
	code := app.BootstrapAdmin(ctx, args, env, strings.NewReader(stdin), &out, &errOut)
	return cliResult{code: code, stdout: out.String(), stderr: errOut.String()}
}

func TestCLIGeneratesPasswordAndPrintsItOnce(t *testing.T) {
	db := seededDB(t)
	env := cliEnv(testkit.PostgresURL(db, testpg.AppRole))

	r := runCLI(t, env, "", "--email", "root@example.org")
	require.Equal(t, app.ExitOK, r.code, r.stderr)

	m := passwordLine.FindStringSubmatch(r.stdout)
	require.Len(t, m, 2, "password acak harus tampil sebagai baris `password: …`: %q", r.stdout)
	hash := text(t, db, "SELECT secret_hash FROM credentials WHERE credential_type = 'password'")
	require.NoError(t, newHasher(t).Compare(ctx, hash, m[1]))
	require.NotContains(t, r.stderr, m[1], "password tidak boleh juga masuk stderr")
	require.NotContains(t, r.stdout+r.stderr, "SECRET", "tidak ada rahasia konfigurasi di keluaran")

	// Run kedua: tidak ada perubahan dan password TIDAK ditampilkan lagi.
	again := runCLI(t, env, "", "--email", "root@example.org")
	require.Equal(t, app.ExitOK, again.code, again.stderr)
	require.Contains(t, again.stdout, "tidak ada perubahan")
	require.NotRegexp(t, passwordLine, again.stdout)
}

func TestCLIReadsPasswordFromStdinAndNeverEchoesIt(t *testing.T) {
	db := seededDB(t)
	env := cliEnv(testkit.PostgresURL(db, testpg.AppRole))

	r := runCLI(t, env, goodPassword+"\n", "--email", "root@example.org", "--name", "Root", "--password-stdin")
	require.Equal(t, app.ExitOK, r.code, r.stderr)
	require.NotContains(t, r.stdout, goodPassword)
	require.NotContains(t, r.stderr, goodPassword)
	require.NotRegexp(t, passwordLine, r.stdout)

	hash := text(t, db, "SELECT secret_hash FROM credentials WHERE credential_type = 'password'")
	require.NoError(t, newHasher(t).Compare(ctx, hash, goodPassword))
	require.Equal(t, "Root", text(t, db, "SELECT display_name FROM users WHERE email = 'root@example.org'"))
}

func TestCLIAcceptsCRLFFromStdin(t *testing.T) {
	db := seededDB(t)
	env := cliEnv(testkit.PostgresURL(db, testpg.AppRole))

	r := runCLI(t, env, goodPassword+"\r\n", "--email", "root@example.org", "--password-stdin")
	require.Equal(t, app.ExitOK, r.code, r.stderr)
	hash := text(t, db, "SELECT secret_hash FROM credentials WHERE credential_type = 'password'")
	require.NoError(t, newHasher(t).Compare(ctx, hash, goodPassword), "CR/LF akhir bukan bagian password")
}

func TestCLIInvalidInputExitsWithConfigCodeBeforeTouchingTheDatabase(t *testing.T) {
	env := cliEnv(unreachableDB)

	cases := map[string]struct {
		stdin string
		args  []string
	}{
		"tanpa --email":          {args: []string{}},
		"email kosong":           {args: []string{"--email", "  "}},
		"email tidak valid":      {args: []string{"--email", "bukan-email"}},
		"flag tidak dikenal":     {args: []string{"--email", "a@example.org", "--bogus"}},
		"argumen sisa":           {args: []string{"--email", "a@example.org", "sisa"}},
		"password stdin kosong":  {args: []string{"--email", "a@example.org", "--password-stdin"}},
		"password stdin lemah":   {stdin: "abc123\n", args: []string{"--email", "a@example.org", "--password-stdin"}},
		"password stdin panjang": {stdin: strings.Repeat("x", 400) + "\n", args: []string{"--email", "a@example.org", "--password-stdin"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := runCLI(t, env, c.stdin, c.args...)
			require.Equal(t, app.ExitConfig, r.code, r.stderr)
			require.NotEmpty(t, r.stderr)
			require.NotContains(t, r.stdout+r.stderr, "SECRET-DB-PW")
			require.NotContains(t, r.stdout+r.stderr, "abc123", "password tidak boleh dipantulkan ke pesan galat")
		})
	}
}

func TestCLIInvalidConfigurationExitsWithConfigCode(t *testing.T) {
	env := cliEnv(unreachableDB)
	delete(env, "SESSION_SECRET")

	r := runCLI(t, env, "", "--email", "a@example.org")
	require.Equal(t, app.ExitConfig, r.code)
	require.Contains(t, r.stderr, "SESSION_SECRET")
	require.NotContains(t, r.stdout+r.stderr, "SECRET-DB-PW")
}

func TestCLIUnreachableDatabaseFailsWithoutLeakingCredentials(t *testing.T) {
	r := runCLI(t, cliEnv(unreachableDB), "", "--email", "a@example.org")
	require.Equal(t, app.ExitFailed, r.code)
	require.NotEmpty(t, r.stderr)
	require.NotContains(t, r.stdout+r.stderr, "SECRET-DB-PW", "password database tidak boleh bocor di pesan galat")
	require.NotRegexp(t, passwordLine, r.stdout, "tanpa penulisan sukses, tidak ada password untuk ditampilkan")
}

func TestCLIMissingPlatformRowFailsWithoutPrintingAPassword(t *testing.T) {
	db := testkit.NewDB(t) // tanpa seed platform
	r := runCLI(t, cliEnv(testkit.PostgresURL(db, testpg.AppRole)), "", "--email", "a@example.org")
	require.Equal(t, app.ExitFailed, r.code)
	require.Contains(t, r.stderr, "platform")
	require.NotRegexp(t, passwordLine, r.stdout)
	require.Equal(t, 0, count(t, db, "SELECT count(*) FROM users"))
}
