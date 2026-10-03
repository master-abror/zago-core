package config_test

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"platform/backend/pkg/config"
)

// .env.example adalah titik awal `make setup`. Tes ini menjaganya tetap valid untuk SEMUA role,
// supaya `make dev` / `make docker-up` tidak gagal start di clone bersih.
func readEnvExample(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open("../../../.env.example")
	require.NoError(t, err, ".env.example harus ada di root repo")
	defer f.Close()

	env := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		require.True(t, ok, "baris .env.example tidak berformat KEY=VALUE: %q", line)
		env[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	require.NoError(t, sc.Err())
	return env
}

func TestEnvExampleIsValidForEveryRole(t *testing.T) {
	env := readEnvExample(t)
	for _, role := range []config.Role{config.RoleAPI, config.RoleWorker, config.RoleMigrate} {
		_, err := config.LoadFrom(role, env)
		require.NoError(t, err, "role %s", role)
	}
}

func TestEnvExampleContainsNoRealisticSecrets(t *testing.T) {
	env := readEnvExample(t)
	// Nilai contoh pengembangan harus tetap ditolak di production (SESSION_SECRET diawali dev-only).
	require.True(t, strings.HasPrefix(env["SESSION_SECRET"], "dev-only"))
	require.Equal(t, "development", env["ENVIRONMENT"])
}
