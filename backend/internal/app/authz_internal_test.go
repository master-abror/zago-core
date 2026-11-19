package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	authzapp "github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/internal/role"
)

type failBeginner struct{ err error }

func (b failBeginner) Begin(context.Context) (pgx.Tx, error) { return nil, b.err }

func jsonLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

func TestSyncAuthorizationReturnsWrappedErrorWhenDatabaseFails(t *testing.T) {
	boom := errors.New("koneksi putus")
	var buf bytes.Buffer

	err := syncAuthorization(context.Background(), failBeginner{err: boom}, nil, jsonLogger(&buf))

	require.Error(t, err)
	require.ErrorIs(t, err, boom, "galat asli harus tetap bisa ditelusuri")
	require.Contains(t, err.Error(), "sinkron katalog otorisasi")
	require.Empty(t, buf.String(), "tidak ada log keberhasilan bila sinkron gagal")
}

func TestLogSyncWarnsAboutOrphansWithoutDeletingAnything(t *testing.T) {
	var buf bytes.Buffer
	res := authzapp.SyncResult{Permissions: permission.SyncResult{Orphans: []string{"legacy.thing.read"}}}

	logSync(jsonLogger(&buf), res)

	out := buf.String()
	require.Contains(t, out, `"level":"WARN"`)
	require.Contains(t, out, "legacy.thing.read")
	require.Contains(t, out, "orphan")
	require.Contains(t, out, "sudah sinkron", "orphan saja bukan perubahan")
}

func TestLogSyncReportsCountsOnlyWhenSomethingChanged(t *testing.T) {
	var buf bytes.Buffer
	res := authzapp.SyncResult{
		Permissions: permission.SyncResult{Created: []string{"a.b.read", "a.b.write"}},
		Roles:       role.SyncResult{RolesCreated: []string{"member"}, PermissionsAdded: 5},
	}

	logSync(jsonLogger(&buf), res)

	out := buf.String()
	require.Contains(t, out, "disinkronkan")
	require.Contains(t, out, `"permissions_created":2`)
	require.Contains(t, out, `"roles_created":1`)
	require.Contains(t, out, `"role_permissions_added":5`)
	require.NotContains(t, out, `"level":"WARN"`)
}

func TestLogSyncIsQuietWhenNothingChanged(t *testing.T) {
	var buf bytes.Buffer

	logSync(jsonLogger(&buf), authzapp.SyncResult{})

	out := buf.String()
	require.Contains(t, out, "sudah sinkron")
	require.NotContains(t, out, `"level":"WARN"`)
}

type countingInvalidator struct{ n int }

func (c *countingInvalidator) BumpPlatform(context.Context) error {
	c.n++
	return nil
}

func TestSyncAuthorizationDoesNotBumpWhenTheSyncFails(t *testing.T) {
	inv := &countingInvalidator{}
	var buf bytes.Buffer

	err := syncAuthorization(context.Background(), failBeginner{err: errors.New("koneksi putus")}, inv, jsonLogger(&buf))

	require.Error(t, err)
	require.Zero(t, inv.n, "tanpa perubahan katalog tidak ada versi yang dinaikkan")
}

// newAuthzStack hanya merakit objek (tanpa I/O): db dan klien Redis di sini tidak pernah dipakai.
func TestNewAuthzStackWithoutRedisHasNoInvalidatorButStillAssemblesPlatformEntry(t *testing.T) {
	var buf bytes.Buffer

	s, err := newAuthzStack(nil, nil, jsonLogger(&buf))

	require.NoError(t, err)
	require.True(t, s.invalidator == nil, "tanpa Redis: interface nil, bukan pointer nil terbungkus")
	require.NotNil(t, s.platformEntry)
	require.NotNil(t, s.entryChecker())
}

func TestNewAuthzStackWithRedisWiresTheCacheAsInvalidator(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
	t.Cleanup(func() { _ = rdb.Close() })
	var buf bytes.Buffer

	s, err := newAuthzStack(nil, rdb, jsonLogger(&buf))

	require.NoError(t, err)
	require.NotNil(t, s.invalidator)
	require.NotNil(t, s.platformEntry)
}

func TestZeroAuthzStackYieldsANilEntryCheckerInterface(t *testing.T) {
	require.True(t, authzStack{}.entryChecker() == nil,
		"tanpa platform entry: interface nil supaya AuthService menolak (fail-closed), bukan pointer nil terbungkus")
}
