package config_test

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/config"
)

// ADR-0021: port host compose dapat disetel lewat .env; scripts/env-ports.sh membuat .env yang
// konsisten (URL, origin, port SMTP) dari .env.example. Tes ini menjaga skrip dan compose.

const envPortsScript = "../../../scripts/env-ports.sh"

// runEnvPorts menjalankan skrip dengan keluaran ke out. Mengembalikan keluaran gabungan dan galat
// (nil bila kode keluar 0).
func runEnvPorts(t *testing.T, out string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{envPortsScript}, args...)...)
	cmd.Env = append(os.Environ(), "ENV_OUT="+out)
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func parseEnvFile(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	env := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		require.True(t, ok, "baris tidak berformat KEY=VALUE: %q", line)
		env[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	require.NoError(t, sc.Err())
	return env
}

func TestEnvPortsRewritesEveryDependentValue(t *testing.T) {
	out := filepath.Join(t.TempDir(), ".env")
	msg, err := runEnvPorts(t, out,
		"POSTGRES=15432", "REDIS=16379", "API=18080", "WEB=15173", "SMTP=11025", "MAILUI=18025")
	require.NoError(t, err, msg)

	env := parseEnvFile(t, out)
	require.Equal(t, "15432", env["POSTGRES_HOST_PORT"])
	require.Equal(t, "16379", env["REDIS_HOST_PORT"])
	require.Equal(t, "18080", env["API_HOST_PORT"])
	require.Equal(t, "15173", env["WEB_HOST_PORT"])
	require.Equal(t, "11025", env["MAILPIT_SMTP_HOST_PORT"])
	require.Equal(t, "18025", env["MAILPIT_UI_HOST_PORT"])

	for _, k := range []string{"DATABASE_URL", "MAINTENANCE_DATABASE_URL", "MIGRATION_DATABASE_URL"} {
		require.Contains(t, env[k], "@localhost:15432/platform", k)
	}
	require.Equal(t, "redis://localhost:16379", env["REDIS_URL"])
	require.Equal(t, "http://localhost:15173", env["ALLOWED_ORIGINS"])
	require.Equal(t, "http://localhost:15173", env["PUBLIC_BASE_URL"])
	require.Equal(t, "11025", env["SMTP_PORT"])
	// PORT adalah port DI DALAM container api (compose memetakan host:8080); tidak boleh ikut berubah.
	require.Equal(t, "8080", env["PORT"])

	for _, role := range []config.Role{config.RoleAPI, config.RoleWorker, config.RoleMigrate} {
		_, err := config.LoadFrom(role, env)
		require.NoError(t, err, "role %s", role)
	}
}

func TestEnvPortsDefaultsKeepEnvExampleValues(t *testing.T) {
	out := filepath.Join(t.TempDir(), ".env")
	msg, err := runEnvPorts(t, out)
	require.NoError(t, err, msg)

	got := parseEnvFile(t, out)
	want := readEnvExample(t)
	for k, v := range want {
		require.Equal(t, v, got[k], "nilai bawaan %s harus sama dengan .env.example", k)
	}
	require.Equal(t, "5432", got["POSTGRES_HOST_PORT"])
	require.Equal(t, "8080", got["API_HOST_PORT"])
}

func TestEnvPortsRejectsBadInputWithoutWritingAnything(t *testing.T) {
	cases := map[string][]string{
		"port ganda":            {"POSTGRES=15432", "REDIS=15432"},
		"bukan angka":           {"POSTGRES=abc"},
		"di bawah 1024":         {"WEB=80"},
		"di atas 65535":         {"API=70000"},
		"kosong":                {"REDIS="},
		"kunci tak dikenal":     {"MONGO=27017"},
		"argumen tak dikenal":   {"--paksa"},
		"bentrok dengan bawaan": {"POSTGRES=6379"}, // bawaan REDIS 6379 belum diganti
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), ".env")
			msg, err := runEnvPorts(t, out, args...)
			var ee *exec.ExitError
			require.True(t, errors.As(err, &ee), "harus gagal dengan kode keluar non-nol: %s", msg)
			require.NotZero(t, ee.ExitCode())
			_, statErr := os.Stat(out)
			require.True(t, os.IsNotExist(statErr), "tidak boleh menulis berkas saat input salah: %s", msg)
		})
	}
}

func TestEnvPortsNeverOverwritesWithoutForce(t *testing.T) {
	out := filepath.Join(t.TempDir(), ".env")
	require.NoError(t, os.WriteFile(out, []byte("SESSION_SECRET=milik-saya\n"), 0o600))

	msg, err := runEnvPorts(t, out, "POSTGRES=15432")
	require.Error(t, err, msg)
	b, readErr := os.ReadFile(out)
	require.NoError(t, readErr)
	require.Equal(t, "SESSION_SECRET=milik-saya\n", string(b), ".env yang ada tidak boleh berubah")

	msg, err = runEnvPorts(t, out, "--force", "POSTGRES=15432")
	require.NoError(t, err, msg)
	require.Equal(t, "15432", parseEnvFile(t, out)["POSTGRES_HOST_PORT"])
}

// Setiap port host di compose harus bisa disetel lewat ${NAMA_HOST_PORT:-bawaan}, dan bawaan itu
// harus sama dengan port lama (laptop tanpa .env khusus tidak boleh berubah perilakunya).
func TestComposePublishedPortsAreConfigurable(t *testing.T) {
	f, err := os.Open("../../../docker-compose.yml")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()

	published := regexp.MustCompile(`^\s*-\s*"127\.0\.0\.1:`)
	configurable := regexp.MustCompile(`^\s*-\s*"127\.0\.0\.1:\$\{([A-Z_]+_HOST_PORT):-(\d+)\}:(\d+)"`)
	defaults := map[string]string{
		"POSTGRES_HOST_PORT":     "5432",
		"REDIS_HOST_PORT":        "6379",
		"MAILPIT_SMTP_HOST_PORT": "1025",
		"MAILPIT_UI_HOST_PORT":   "8025",
		"API_HOST_PORT":          "8080",
		"WEB_HOST_PORT":          "5173",
	}

	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !published.MatchString(line) {
			continue
		}
		m := configurable.FindStringSubmatch(line)
		require.NotNil(t, m, "port host dipatok tanpa variabel: %q", line)
		want, known := defaults[m[1]]
		require.True(t, known, "variabel port tak dikenal: %s", m[1])
		require.Equal(t, want, m[2], "bawaan %s harus tetap %s", m[1], want)
		require.Equal(t, want, m[3], "port container %s harus sama dengan bawaan", m[1])
		seen[m[1]] = true
	}
	require.NoError(t, sc.Err())
	require.Len(t, seen, len(defaults), "semua port yang dipublikasikan harus muncul: %v", seen)
}
