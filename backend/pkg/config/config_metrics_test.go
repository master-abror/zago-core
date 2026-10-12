package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/config"
)

func TestMetricsPortsDefaultToDisabled(t *testing.T) {
	for _, role := range []config.Role{config.RoleAPI, config.RoleWorker} {
		c, err := config.LoadFrom(role, devEnv())
		require.NoError(t, err, "role %s", role)
		require.Zero(t, c.MetricsPort)
		require.Zero(t, c.WorkerMetricsPort)
		require.Zero(t, c.MetricsPortFor(role), "tanpa env, /metrics nonaktif untuk %s", role)
	}
}

func TestMetricsPortForPicksPortByRole(t *testing.T) {
	e := with(devEnv(), func(m map[string]string) {
		m["METRICS_PORT"] = "9090"
		m["WORKER_METRICS_PORT"] = "9091"
	})

	api, err := config.LoadFrom(config.RoleAPI, e)
	require.NoError(t, err)
	require.Equal(t, 9090, api.MetricsPortFor(config.RoleAPI))
	require.Equal(t, 9091, api.MetricsPortFor(config.RoleWorker))

	wk, err := config.LoadFrom(config.RoleWorker, e)
	require.NoError(t, err)
	require.Equal(t, 9091, wk.MetricsPortFor(config.RoleWorker))
}

func TestMetricsPortsRejectOutOfRange(t *testing.T) {
	for _, key := range []string{"METRICS_PORT", "WORKER_METRICS_PORT"} {
		for _, bad := range []string{"-1", "65536"} {
			e := with(devEnv(), func(m map[string]string) { m[key] = bad })
			_, err := config.LoadFrom(config.RoleAPI, e)
			require.Error(t, err, "%s=%s", key, bad)
			require.Contains(t, problems(t, err), key+" harus 0–65535 (0 = nonaktif)")
		}
	}
}

func TestMetricsPortMustNotCollideWithMainPortForAPI(t *testing.T) {
	e := with(devEnv(), func(m map[string]string) {
		m["PORT"] = "9090"
		m["METRICS_PORT"] = "9090"
	})
	_, err := config.LoadFrom(config.RoleAPI, e)
	require.Error(t, err)
	require.Contains(t, problems(t, err), "METRICS_PORT tidak boleh sama dengan PORT")
}

func TestMetricsPortEqualToMainPortIsHarmlessWhenDisabled(t *testing.T) {
	// 0 = nonaktif: tidak ada bind, jadi tidak ada tabrakan walau PORT berapa pun.
	_, err := config.LoadFrom(config.RoleAPI, devEnv())
	require.NoError(t, err)
}
