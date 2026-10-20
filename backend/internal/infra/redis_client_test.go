package infra_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/infra"
	"github.com/master-abror/zago-core/backend/internal/testkit"
)

// Resource Redis membawa SATU klien go-redis yang dipakai bersama composition root (ADR-0016),
// dan klien itu benar-benar bisa bicara ke Redis 8.
func TestRedisResourceExposesTheSharedClient(t *testing.T) {
	srv := testkit.Redis(t)
	res, err := infra.Deps{}.Redis(context.Background(), "redis", srv.URL())
	require.NoError(t, err)
	t.Cleanup(res.Close)

	require.NotNil(t, res.Redis)
	require.NoError(t, res.Redis.Ping(context.Background()).Err())
	require.NoError(t, res.Check(context.Background()))
}
