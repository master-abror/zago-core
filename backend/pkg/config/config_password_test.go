package config_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/config"
	"github.com/master-abror/zago-core/backend/pkg/security"
)

func TestPasswordSettingsDefaultsMatchDoc05(t *testing.T) {
	cfg, err := config.LoadFrom(config.RoleAPI, devEnv()) // tanpa variabel PASSWORD_*: bawaan
	require.NoError(t, err)
	require.Equal(t, security.DefaultArgon2Params(), cfg.Argon2Params())
	require.Equal(t, 0, cfg.PasswordHashConcurrency, "0 = jumlah CPU")
	require.Equal(t, security.PasswordPolicy{MinLength: 12}, cfg.PasswordPolicy())
}

func TestPasswordSettingsFromEnvExampleAreValid(t *testing.T) {
	env := readEnvExample(t)
	for _, k := range []string{
		"PASSWORD_ARGON2_MEMORY_KIB", "PASSWORD_ARGON2_TIME", "PASSWORD_ARGON2_PARALLELISM",
		"PASSWORD_HASH_CONCURRENCY", "PASSWORD_MIN_LENGTH",
	} {
		require.Contains(t, env, k, ".env.example harus mendokumentasikan %s", k)
	}
	cfg, err := config.LoadFrom(config.RoleAPI, env)
	require.NoError(t, err)
	_, err = security.NewArgon2idHasher(cfg.Argon2Params(), cfg.PasswordHashConcurrency)
	require.NoError(t, err, "parameter contoh harus diterima hasher")
}

func TestPasswordSettingsOverride(t *testing.T) {
	cfg, err := config.LoadFrom(config.RoleAPI, with(devEnv(), func(m map[string]string) {
		m["PASSWORD_ARGON2_MEMORY_KIB"] = "19456"
		m["PASSWORD_ARGON2_TIME"] = "2"
		m["PASSWORD_ARGON2_PARALLELISM"] = "1"
		m["PASSWORD_HASH_CONCURRENCY"] = "4"
		m["PASSWORD_MIN_LENGTH"] = "16"
	}))
	require.NoError(t, err)
	require.Equal(t, security.Argon2Params{MemoryKiB: 19456, Time: 2, Parallelism: 1}, cfg.Argon2Params())
	require.Equal(t, 4, cfg.PasswordHashConcurrency)
	require.Equal(t, 16, cfg.PasswordPolicy().MinLength)
}

func TestPasswordSettingsRejectUnsafeValues(t *testing.T) {
	cases := map[string]struct {
		mutate func(map[string]string)
		want   string
	}{
		"memori nol":                {func(m map[string]string) { m["PASSWORD_ARGON2_MEMORY_KIB"] = "0" }, "PASSWORD_ARGON2_MEMORY_KIB"},
		"memori terlalu kecil":      {func(m map[string]string) { m["PASSWORD_ARGON2_MEMORY_KIB"] = "4" }, "PASSWORD_ARGON2_MEMORY_KIB"},
		"memori kurang dari 8*p":    {func(m map[string]string) { m["PASSWORD_ARGON2_MEMORY_KIB"] = "15" }, "PASSWORD_ARGON2_MEMORY_KIB"},
		"memori melewati batas":     {func(m map[string]string) { m["PASSWORD_ARGON2_MEMORY_KIB"] = "99999999" }, "PASSWORD_ARGON2_MEMORY_KIB"},
		"memori negatif":            {func(m map[string]string) { m["PASSWORD_ARGON2_MEMORY_KIB"] = "-1" }, "PASSWORD_ARGON2_MEMORY_KIB"},
		"waktu nol":                 {func(m map[string]string) { m["PASSWORD_ARGON2_TIME"] = "0" }, "PASSWORD_ARGON2_TIME"},
		"waktu terlalu besar":       {func(m map[string]string) { m["PASSWORD_ARGON2_TIME"] = "17" }, "PASSWORD_ARGON2_TIME"},
		"paralelisme nol":           {func(m map[string]string) { m["PASSWORD_ARGON2_PARALLELISM"] = "0" }, "PASSWORD_ARGON2_PARALLELISM"},
		"paralelisme melewati 8bit": {func(m map[string]string) { m["PASSWORD_ARGON2_PARALLELISM"] = "256" }, "PASSWORD_ARGON2_PARALLELISM"},
		"konkurensi negatif":        {func(m map[string]string) { m["PASSWORD_HASH_CONCURRENCY"] = "-1" }, "PASSWORD_HASH_CONCURRENCY"},
		"konkurensi terlalu besar":  {func(m map[string]string) { m["PASSWORD_HASH_CONCURRENCY"] = "257" }, "PASSWORD_HASH_CONCURRENCY"},
		"panjang minimum lemah":     {func(m map[string]string) { m["PASSWORD_MIN_LENGTH"] = "7" }, "PASSWORD_MIN_LENGTH"},
		"panjang minimum absurd":    {func(m map[string]string) { m["PASSWORD_MIN_LENGTH"] = "129" }, "PASSWORD_MIN_LENGTH"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := config.LoadFrom(config.RoleAPI, with(devEnv(), tc.mutate))
			require.Error(t, err)
			require.Contains(t, strings.Join(problems(t, err), "|"), tc.want)
		})
	}
}

// Hanya API yang menghash password; worker dan migrate tidak boleh gagal start karenanya.
func TestPasswordSettingsAreValidatedOnlyForAPI(t *testing.T) {
	bad := with(devEnv(), func(m map[string]string) { m["PASSWORD_MIN_LENGTH"] = "1" })
	_, err := config.LoadFrom(config.RoleWorker, bad)
	require.NoError(t, err)
	_, err = config.LoadFrom(config.RoleMigrate, bad)
	require.NoError(t, err)
	_, err = config.LoadFrom(config.RoleAPI, bad)
	require.Error(t, err)
}
