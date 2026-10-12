package kernel_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/kernel"
)

func TestStatsOfReadsRealPoolWithoutConnecting(t *testing.T) {
	// Alamat tak terjangkau: pool lazy, statistik tetap bisa dibaca (nol terpakai, batas = MaxConns).
	pool, err := kernel.NewPool(context.Background(), kernel.PoolConfig{
		Name: "app", URL: "postgres://u:pw@127.0.0.1:1/db",
		MaxConns: 7, StatementTimeout: time.Second,
	})
	require.NoError(t, err)
	defer pool.Close()

	s, ok := kernel.StatsOf(pool)
	require.True(t, ok)
	require.EqualValues(t, 7, s.Max)
	require.EqualValues(t, 0, s.Acquired)
	require.EqualValues(t, 0, s.Idle)
}

// Fake pool di tes composition root tidak punya Stat(); pemanggil tak boleh panik.
func TestStatsOfReportsUnavailableForPoolWithoutStat(t *testing.T) {
	_, ok := kernel.StatsOf(struct{ kernel.Pool }{})
	require.False(t, ok)
	_, ok = kernel.StatsOf(nil)
	require.False(t, ok)
}
