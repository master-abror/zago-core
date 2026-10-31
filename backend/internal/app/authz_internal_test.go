package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/jackc/pgx/v5"
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

	err := syncAuthorization(context.Background(), failBeginner{err: boom}, jsonLogger(&buf))

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
