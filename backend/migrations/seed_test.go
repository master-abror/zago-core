package migrations_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/dbmigrate"
	"github.com/master-abror/zago-core/backend/internal/testpg"
)

func TestSeedPlatformRowIsIdempotent(t *testing.T) {
	db := server(t).NewDB(t)
	r := dbmigrate.Runner{}

	first, err := r.Seed(ctx, db.MigratorURL())
	require.NoError(t, err)
	require.True(t, first.Created)
	require.NotEmpty(t, first.ID)

	second, err := r.Seed(ctx, db.MigratorURL())
	require.NoError(t, err)
	require.False(t, second.Created, "seed kedua tidak boleh membuat baris")
	require.Equal(t, first.ID, second.ID)

	require.Equal(t, 1, scalar[int](t, db.Migrator, `SELECT count(*) FROM platforms`))
	require.Equal(t, "active", scalar[string](t, db.Migrator, `SELECT status FROM platforms`))
	require.Equal(t, dbmigrate.PlatformVersion, scalar[string](t, db.Migrator, `SELECT version FROM platforms`))
	require.Regexp(t, `^inst_[0-9a-f]{32}$`, scalar[string](t, db.Migrator, `SELECT installation_key FROM platforms`))
}

func TestSeedConcurrentRunsProduceOneRow(t *testing.T) {
	db := server(t).NewDB(t)
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = dbmigrate.Runner{}.Seed(ctx, db.MigratorURL())
		}()
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, scalar[int](t, db.Migrator, `SELECT count(*) FROM platforms`), "advisory lock harus menserialkan seeder")
}

func TestSeedWorksWithRuntimeRoleToo(t *testing.T) {
	// Seeder hanya butuh DML; bootstrap-admin (M03) akan memanggilnya dengan role runtime.
	db := server(t).NewDB(t)
	res, err := dbmigrate.Runner{}.Seed(ctx, db.RoleURL(testpg.AppRole))
	require.NoError(t, err)
	require.True(t, res.Created)
	requireDB(t, tryExec(db.App, `INSERT INTO platforms (name, installation_key, version) VALUES ('P2', (SELECT installation_key FROM platforms), '1')`),
		uniqueViolation, "uq_platforms_installation_key")
}
