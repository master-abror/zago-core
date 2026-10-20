package testkit_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
)

func TestMain(m *testing.M) { os.Exit(testkit.Run(m)) }

var ctx = context.Background()

func TestNewDBGivesMigratedDatabaseWithRuntimeRole(t *testing.T) {
	db := testkit.NewDB(t)

	var user string
	require.NoError(t, db.App.QueryRow(ctx, `SELECT current_user`).Scan(&user))
	require.Equal(t, testpg.AppRole, user)

	var n int
	require.NoError(t, db.App.QueryRow(ctx, `SELECT count(*) FROM outbox_events`).Scan(&n))
	require.Zero(t, n)
}

func TestPostgresURLUsesRuntimeScheme(t *testing.T) {
	db := testkit.NewDB(t)

	pool, err := pgxpool.New(ctx, testkit.PostgresURL(db, testpg.AppRole))
	require.NoError(t, err)
	defer pool.Close()

	var user string
	require.NoError(t, pool.QueryRow(ctx, `SELECT current_user`).Scan(&user))
	require.Equal(t, testpg.AppRole, user)
}

func TestFixturesProduceValidLinkedRows(t *testing.T) {
	db := testkit.NewDB(t)

	org := testkit.NewOrganization(t, db)
	other := testkit.NewOrganization(t, db, testkit.WithSlug("kustom-slug"))
	require.NotEqual(t, org.ID, other.ID)
	require.Equal(t, "kustom-slug", other.Slug)

	member := testkit.NewUser(t, db, testkit.InOrganization(org))
	loner := testkit.NewUser(t, db)
	require.NotEqual(t, member.Email, loner.Email)
	require.Equal(t, org.ID, member.OrganizationID)

	var memberships int
	require.NoError(t, db.App.QueryRow(ctx,
		`SELECT count(*) FROM organization_memberships WHERE organization_id = $1 AND user_id = $2 AND status = 'active'`,
		org.ID.String(), member.ID.String()).Scan(&memberships))
	require.Equal(t, 1, memberships)

	require.NoError(t, db.App.QueryRow(ctx,
		`SELECT count(*) FROM organization_memberships WHERE user_id = $1`, loner.ID.String()).Scan(&memberships))
	require.Zero(t, memberships, "pengguna tanpa InOrganization tidak punya keanggotaan")
}

func TestRedisClientStartsClean(t *testing.T) {
	rs := testkit.Redis(t)

	first := rs.Client(t)
	require.NoError(t, first.Set(ctx, "kunci", "nilai", 0).Err())

	second := rs.Client(t)
	_, err := second.Get(ctx, "kunci").Result()
	require.ErrorIs(t, err, redis.Nil, "klien baru harus mulai dari Redis kosong")

	require.Equal(t, "redis://"+rs.Addr(), rs.URL())
}
