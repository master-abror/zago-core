package app

import (
	"bytes"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// Policy hanya berlaku bila perakitan produksi memasangnya. Tes ini menjaga agar perakitan tidak
// diam-diam kehilangan WithPolicies (semua policy akan terabaikan tanpa ada yang gagal).

func TestAuthzStackWiresPolicyEnforcement(t *testing.T) {
	var buf bytes.Buffer
	s, err := newAuthzStack(nil, nil, jsonLogger(&buf))
	require.NoError(t, err)

	require.NotNil(t, s.authorizer)
	require.True(t, s.authorizer.PoliciesEnabled(), "newAuthzStack harus memasang repository.Policies")
}

func TestAuthzStackWiresPolicyEnforcementWithRedisToo(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = rdb.Close() })
	var buf bytes.Buffer
	s, err := newAuthzStack(nil, rdb, jsonLogger(&buf))
	require.NoError(t, err)

	require.True(t, s.authorizer.PoliciesEnabled())
}
