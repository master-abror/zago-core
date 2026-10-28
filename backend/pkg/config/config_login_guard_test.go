package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/config"
)

func TestLoginGuardDefaults(t *testing.T) {
	cfg, err := config.LoadFrom(config.RoleAPI, devEnv())
	require.NoError(t, err)
	require.Equal(t, 5, cfg.LoginMaxFailures)
	require.Equal(t, 15*time.Minute, cfg.LoginFailureWindow)
	require.Equal(t, 15*time.Minute, cfg.LoginLockDuration)
	require.Equal(t, time.Hour, cfg.LoginLockMax)
	require.Equal(t, 30, cfg.LoginIPMaxFailures)

	ex, err := config.LoadFrom(config.RoleAPI, readEnvExample(t))
	require.NoError(t, err)
	require.Equal(t, cfg.LoginMaxFailures, ex.LoginMaxFailures)
	require.Equal(t, cfg.LoginFailureWindow, ex.LoginFailureWindow)
	require.Equal(t, cfg.LoginLockDuration, ex.LoginLockDuration)
	require.Equal(t, cfg.LoginLockMax, ex.LoginLockMax)
	require.Equal(t, cfg.LoginIPMaxFailures, ex.LoginIPMaxFailures)
}

func TestLoginGuardOverride(t *testing.T) {
	cfg, err := config.LoadFrom(config.RoleAPI, with(devEnv(), func(m map[string]string) {
		m["LOGIN_MAX_FAILURES"] = "10"
		m["LOGIN_FAILURE_WINDOW"] = "30m"
		m["LOGIN_LOCK_DURATION"] = "5m"
		m["LOGIN_LOCK_MAX"] = "2h"
		m["LOGIN_IP_MAX_FAILURES"] = "100"
	}))
	require.NoError(t, err)
	require.Equal(t, 10, cfg.LoginMaxFailures)
	require.Equal(t, 30*time.Minute, cfg.LoginFailureWindow)
	require.Equal(t, 5*time.Minute, cfg.LoginLockDuration)
	require.Equal(t, 2*time.Hour, cfg.LoginLockMax)
	require.Equal(t, 100, cfg.LoginIPMaxFailures)
}

func TestLoginGuardRejectsUnsafeValues(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]string)
		want   string
	}{
		{"gagal terlalu sedikit", func(m map[string]string) { m["LOGIN_MAX_FAILURES"] = "2" }, "LOGIN_MAX_FAILURES"},
		{"gagal terlalu banyak", func(m map[string]string) { m["LOGIN_MAX_FAILURES"] = "101" }, "LOGIN_MAX_FAILURES"},
		{"ip nol", func(m map[string]string) { m["LOGIN_IP_MAX_FAILURES"] = "0" }, "LOGIN_IP_MAX_FAILURES"},
		{"ip terlalu banyak", func(m map[string]string) { m["LOGIN_IP_MAX_FAILURES"] = "10001" }, "LOGIN_IP_MAX_FAILURES"},
		{"jendela terlalu pendek", func(m map[string]string) { m["LOGIN_FAILURE_WINDOW"] = "30s" }, "LOGIN_FAILURE_WINDOW"},
		{"jendela terlalu panjang", func(m map[string]string) { m["LOGIN_FAILURE_WINDOW"] = "25h" }, "LOGIN_FAILURE_WINDOW"},
		{"durasi kunci terlalu pendek", func(m map[string]string) { m["LOGIN_LOCK_DURATION"] = "30s" }, "LOGIN_LOCK_DURATION"},
		{"durasi kunci terlalu panjang", func(m map[string]string) { m["LOGIN_LOCK_DURATION"] = "25h" }, "LOGIN_LOCK_DURATION"},
		{"batas kunci terlalu panjang", func(m map[string]string) { m["LOGIN_LOCK_MAX"] = "25h" }, "LOGIN_LOCK_MAX tidak boleh melebihi"},
		{"batas kunci di bawah durasi", func(m map[string]string) {
			m["LOGIN_LOCK_DURATION"] = "30m"
			m["LOGIN_LOCK_MAX"] = "15m"
		}, "LOGIN_LOCK_MAX tidak boleh kurang"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := config.LoadFrom(config.RoleAPI, with(devEnv(), tc.mutate))
			require.Error(t, err, "nilai tak sah ditolak saat start, bukan dikoreksi diam-diam")
			require.Contains(t, strings.Join(problems(t, err), "|"), tc.want)
		})
	}
}

func TestLoginGuardIsValidatedOnlyForAPI(t *testing.T) {
	bad := with(devEnv(), func(m map[string]string) { m["LOGIN_MAX_FAILURES"] = "1" })
	_, err := config.LoadFrom(config.RoleWorker, bad)
	require.NoError(t, err)
	_, err = config.LoadFrom(config.RoleAPI, bad)
	require.Error(t, err)
}

func TestLoginGuardUnparseableValuesFailLoad(t *testing.T) {
	for _, kv := range [][2]string{
		{"LOGIN_MAX_FAILURES", "banyak"},
		{"LOGIN_LOCK_DURATION", "sebentar"},
	} {
		_, err := config.LoadFrom(config.RoleAPI, with(devEnv(), func(m map[string]string) { m[kv[0]] = kv[1] }))
		require.Error(t, err, kv[0])
	}
}
