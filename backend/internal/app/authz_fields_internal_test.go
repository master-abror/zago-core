package app

import (
	"bytes"
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	authzdomain "github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// FieldGuard hanya berguna bila perakitan produksi memasangnya (ADR-0022 butir 42). Tes ini
// menjaga agar newAuthzStack tidak diam-diam kehilangannya: tanpa penjaga ini handler yang kelak
// memakainya menemukan nil saat request pertama, bukan saat start.

func requireWiredFieldGuard(t *testing.T, s authzStack) {
	t.Helper()
	require.NotNil(t, s.fieldGuard, "newAuthzStack harus merakit FieldGuard")

	// Tipe tidak sah ditolak sebelum menyentuh penyimpanan apa pun (db nil di tes ini): bukti
	// bahwa yang terpasang adalah FieldGuard sungguhan dengan validasi urutan butir 42.
	_, err := s.fieldGuard.For(context.Background(), authdomain.Identity{UserID: id.NewID()}, authzdomain.ScopeContext{}, "Invalid")
	require.ErrorIs(t, err, authzdomain.ErrInvalidFieldRules)
}

func TestAuthzStackWiresFieldGuard(t *testing.T) {
	var buf bytes.Buffer
	s, err := newAuthzStack(nil, nil, jsonLogger(&buf))
	require.NoError(t, err)

	requireWiredFieldGuard(t, s)
}

func TestAuthzStackWiresFieldGuardWithRedisToo(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = rdb.Close() })
	var buf bytes.Buffer
	s, err := newAuthzStack(nil, rdb, jsonLogger(&buf))
	require.NoError(t, err)

	requireWiredFieldGuard(t, s)
}
