package migrations_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/dbmigrate"
	"github.com/master-abror/zago-core/backend/internal/testpg"
)

func TestRunnerRoundtripOnThrowawayDatabase(t *testing.T) {
	srv := server(t)
	require.NoError(t, dbmigrate.Runner{}.Roundtrip(ctx, srv.RoleURL(testpg.MigratorRole)))

	// database sementara harus sudah dihapus
	probe := srv.EmptyDB(t)
	left := scalar[int](t, probe.Migrator, `SELECT count(*) FROM pg_database WHERE datname LIKE 'roundtrip\_%'`)
	require.Zero(t, left, "database roundtrip tidak dibersihkan")
}

func TestRoundtripNeedsCreateDBAndDoesNotLeakPassword(t *testing.T) {
	srv := server(t)
	err := dbmigrate.Runner{}.Roundtrip(ctx, srv.RoleURL(testpg.AppRole)) // app_user tak punya CREATEDB
	require.Error(t, err)
	require.Contains(t, err.Error(), "CREATEDB")
	require.NotContains(t, err.Error(), "app_dev_pw")
}

func TestUpDownUp(t *testing.T) {
	db := server(t).EmptyDB(t)
	r := dbmigrate.Runner{}
	latest, err := dbmigrate.LatestVersion()
	require.NoError(t, err)
	require.Equal(t, uint(len(wantOrder)), latest)

	version := func() uint {
		v, dirty, verr := r.Version(ctx, db.MigratorURL())
		require.NoError(t, verr)
		require.False(t, dirty)
		return v
	}
	require.Zero(t, version(), "database kosong = versi 0")

	require.NoError(t, r.Up(ctx, db.MigratorURL()))
	require.Equal(t, latest, version())
	require.NoError(t, r.Up(ctx, db.MigratorURL()), "up berulang = tanpa perubahan, bukan error")

	require.NoError(t, r.Down(ctx, db.MigratorURL(), 1)) // make migrate-down
	require.Equal(t, latest-1, version())
	require.NoError(t, r.Up(ctx, db.MigratorURL()))
	require.Equal(t, latest, version())

	require.NoError(t, r.Down(ctx, db.MigratorURL(), int(latest)))
	require.Zero(t, version())
	require.NoError(t, r.Down(ctx, db.MigratorURL(), 1), "down di versi 0 = tanpa perubahan")
}

func TestRunnerErrorsDoNotLeakCredentials(t *testing.T) {
	srv := server(t)
	bad := strings.Replace(srv.RoleURL(testpg.MigratorRole), "migrator_dev_pw", "SECRET-WRONG-PW", 1)
	for name, fn := range map[string]func() error{
		"up":      func() error { return dbmigrate.Runner{}.Up(ctx, bad) },
		"down":    func() error { return dbmigrate.Runner{}.Down(ctx, bad, 1) },
		"version": func() error { _, _, err := dbmigrate.Runner{}.Version(ctx, bad); return err },
		"seed":    func() error { _, err := dbmigrate.Runner{}.Seed(ctx, bad); return err },
	} {
		err := fn()
		require.Error(t, err, name)
		require.NotContains(t, err.Error(), "SECRET-WRONG-PW", name)
	}
}

func TestDownLeavesNothingBehindExceptVersionTable(t *testing.T) {
	db := server(t).EmptyDB(t)
	r := dbmigrate.Runner{}
	require.NoError(t, r.Up(ctx, db.MigratorURL()))
	require.NoError(t, r.Down(ctx, db.MigratorURL(), len(wantOrder)))
	tables := scalar[int](t, db.Migrator, `SELECT count(*) FROM pg_tables WHERE schemaname='public' AND tablename <> 'schema_migrations'`)
	funcs := scalar[int](t, db.Migrator, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public'`)
	require.Zero(t, tables)
	require.Zero(t, funcs)
}
