package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/pkg/config"
)

func TestDBPoolSettingsDefaults(t *testing.T) {
	cfg, err := config.LoadFrom(config.RoleWorker, readEnvExample(t))
	require.NoError(t, err)
	require.Equal(t, 10, cfg.DBMaxConns)
	require.Equal(t, 2, cfg.DBMaintenanceMaxConns)
	require.Equal(t, 15*time.Second, cfg.DBStatementTimeout)
}

func TestDBPoolSettingsOverrideAndValidation(t *testing.T) {
	base := readEnvExample(t)
	with := func(k, v string) map[string]string {
		m := make(map[string]string, len(base)+1)
		for bk, bv := range base {
			m[bk] = bv
		}
		m[k] = v
		return m
	}

	cfg, err := config.LoadFrom(config.RoleAPI, with("DB_STATEMENT_TIMEOUT", "2500ms"))
	require.NoError(t, err)
	require.Equal(t, 2500*time.Millisecond, cfg.DBStatementTimeout)

	for _, tc := range []struct{ key, val, problem string }{
		{"DB_MAX_CONNS", "0", "DB_MAX_CONNS"},
		{"DB_MAX_CONNS", "500", "DB_MAX_CONNS"},
		{"DB_MAINTENANCE_MAX_CONNS", "0", "DB_MAINTENANCE_MAX_CONNS"},
		{"DB_STATEMENT_TIMEOUT", "10ms", "DB_STATEMENT_TIMEOUT"},
		{"DB_STATEMENT_TIMEOUT", "1h", "DB_STATEMENT_TIMEOUT"},
	} {
		_, err := config.LoadFrom(config.RoleAPI, with(tc.key, tc.val))
		require.Error(t, err, "%s=%s", tc.key, tc.val)
		require.Contains(t, err.Error(), tc.problem)
	}
}
