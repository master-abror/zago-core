package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/config"
)

func TestSessionTimeoutDefaults(t *testing.T) {
	cfg, err := config.LoadFrom(config.RoleAPI, devEnv())
	require.NoError(t, err)
	require.Equal(t, 30*time.Minute, cfg.SessionIdleTimeout)
	require.Equal(t, 12*time.Hour, cfg.SessionAbsoluteTimeout)

	ex, err := config.LoadFrom(config.RoleAPI, readEnvExample(t))
	require.NoError(t, err)
	require.Equal(t, cfg.SessionIdleTimeout, ex.SessionIdleTimeout)
	require.Equal(t, cfg.SessionAbsoluteTimeout, ex.SessionAbsoluteTimeout)
}

func TestSessionTimeoutOverride(t *testing.T) {
	cfg, err := config.LoadFrom(config.RoleAPI, with(devEnv(), func(m map[string]string) {
		m["SESSION_IDLE_TIMEOUT"] = "10m"
		m["SESSION_ABSOLUTE_TIMEOUT"] = "8h"
	}))
	require.NoError(t, err)
	require.Equal(t, 10*time.Minute, cfg.SessionIdleTimeout)
	require.Equal(t, 8*time.Hour, cfg.SessionAbsoluteTimeout)
}

func TestSessionTimeoutRejectsUnsafeValues(t *testing.T) {
	cases := map[string]struct {
		mutate func(map[string]string)
		want   string
	}{
		"idle nol":                {func(m map[string]string) { m["SESSION_IDLE_TIMEOUT"] = "0s" }, "SESSION_IDLE_TIMEOUT"},
		"idle terlalu pendek":     {func(m map[string]string) { m["SESSION_IDLE_TIMEOUT"] = "30s" }, "SESSION_IDLE_TIMEOUT"},
		"idle terlalu panjang":    {func(m map[string]string) { m["SESSION_IDLE_TIMEOUT"] = "25h" }, "SESSION_IDLE_TIMEOUT"},
		"absolut terlalu pendek":  {func(m map[string]string) { m["SESSION_ABSOLUTE_TIMEOUT"] = "1m" }, "SESSION_ABSOLUTE_TIMEOUT"},
		"absolut terlalu panjang": {func(m map[string]string) { m["SESSION_ABSOLUTE_TIMEOUT"] = "721h" }, "SESSION_ABSOLUTE_TIMEOUT"},
		"idle melebihi absolut": {func(m map[string]string) {
			m["SESSION_IDLE_TIMEOUT"] = "2h"
			m["SESSION_ABSOLUTE_TIMEOUT"] = "1h"
		}, "tidak boleh melebihi"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := config.LoadFrom(config.RoleAPI, with(devEnv(), tc.mutate))
			require.Error(t, err)
			require.Contains(t, strings.Join(problems(t, err), "|"), tc.want)
		})
	}
}

func TestSessionTimeoutIsValidatedOnlyForAPI(t *testing.T) {
	bad := with(devEnv(), func(m map[string]string) { m["SESSION_IDLE_TIMEOUT"] = "1s" })
	_, err := config.LoadFrom(config.RoleWorker, bad)
	require.NoError(t, err)
	_, err = config.LoadFrom(config.RoleAPI, bad)
	require.Error(t, err)
}
