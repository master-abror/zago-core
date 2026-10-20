package config_test

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/config"
)

// .env.example adalah titik awal `make setup`. Tes ini menjaganya tetap valid untuk SEMUA role,
// supaya `make dev` / `make docker-up` tidak gagal start di clone bersih.
func readEnvExample(t *testing.T) map[string]string {
	t.Helper()
	f, err := os.Open("../../../.env.example")
	require.NoError(t, err, ".env.example harus ada di root repo")
	defer func() { _ = f.Close() }()

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

// Port /metrics untuk pengembangan native (ADR-0016/0017): API 9090, worker 9091. Keduanya berbeda
// karena `make dev` menjalankan API dan worker di satu host dengan .env yang sama.
func TestEnvExampleEnablesDistinctMetricsPorts(t *testing.T) {
	env := readEnvExample(t)
	require.Equal(t, "9090", env["METRICS_PORT"])
	require.Equal(t, "9091", env["WORKER_METRICS_PORT"])

	api, err := config.LoadFrom(config.RoleAPI, env)
	require.NoError(t, err)
	wk, err := config.LoadFrom(config.RoleWorker, env)
	require.NoError(t, err)
	require.NotEqual(t, api.MetricsPortFor(config.RoleAPI), wk.MetricsPortFor(config.RoleWorker))
}
