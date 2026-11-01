package application_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/cache"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
)

// Tes integrasi: Authorizer + repository.Effective (PostgreSQL 18, app_user) + cache.Cache
// (Redis 8). Membuktikan sambungan ketiganya, terutama "pencabutan berlaku pada request
// berikutnya" dan "Redis mati tetap jawaban yang benar dari PostgreSQL" (docs/06 §14, Cache).

const permProbe = "thing.read"

type stack struct {
	db    *testpg.DB
	org   testkit.Organization
	alice testkit.User
	role  testkit.Role
	asg   testkit.Assignment
	cache *cache.Cache
	authz *application.Authorizer
	tm    *kernel.TxManager
}

func newStack(t *testing.T, rdb redis.UniversalClient) stack {
	t.Helper()
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	alice := testkit.NewUser(t, db, testkit.InOrganization(org))
	role := testkit.RoleWith(t, db, []string{permProbe})
	oid := org.ID
	asg := testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: role, SubjectType: "user", SubjectID: alice.ID,
		ScopeType: "organization", OrganizationID: &oid,
	})
	c := cache.New(rdb, cache.Options{Timeout: 200 * time.Millisecond})
	a, err := application.NewAuthorizer(repository.NewEffective(db.App), c)
	require.NoError(t, err)
	return stack{db: db, org: org, alice: alice, role: role, asg: asg, cache: c, authz: a,
		tm: kernel.NewTxManager(db.App, nil)}
}

func (s stack) can(t *testing.T) domain.Decision {
	t.Helper()
	oid := s.org.ID
	d, err := s.authz.Can(ctx, domain.Check{
		Identity: authdomain.Identity{UserID: s.alice.ID, OrganizationID: &oid},
		Scope:    domain.ScopeContext{OrganizationID: &oid},
		Action:   permProbe,
	})
	require.NoError(t, err)
	return d
}

// revoke mencabut assignment di dalam transaksi pengubah dan menaikkan versi organisasi
// (sebelum dan sesudah commit), persis seperti yang akan dilakukan endpoint T5.
func (s stack) revoke(t *testing.T) {
	t.Helper()
	err := s.tm.WithinTx(ctx, func(txctx context.Context) error {
		db := kernel.DBFrom(txctx, s.db.App)
		if _, err := db.Exec(txctx, `UPDATE role_assignments SET revoked_at = now() WHERE id = $1`, s.asg.ID); err != nil {
			return err
		}
		return s.cache.InvalidateInTx(txctx, cache.Targets{Organizations: []uuid.UUID{s.org.ID}})
	})
	require.NoError(t, err)
}

func TestRevocationTakesEffectOnTheVeryNextRequestEvenWithAWarmCache(t *testing.T) {
	s := newStack(t, testkit.Redis(t).Client(t))

	require.True(t, s.can(t).Allowed, "kontrol: izin berlaku")
	require.True(t, s.can(t).Allowed, "kontrol: pembacaan kedua dari cache")

	s.revoke(t)

	d := s.can(t)
	require.False(t, d.Allowed, "pencabutan harus langsung berlaku, bukan menunggu TTL")
	require.Equal(t, domain.ReasonNoPermission, d.Reason)
}

func TestWithoutInvalidationAWarmCacheStillAnswersFromTheCache(t *testing.T) {
	// kontrol negatif: membuktikan cache sungguh dipakai, sehingga tes di atas bermakna
	s := newStack(t, testkit.Redis(t).Client(t))
	require.True(t, s.can(t).Allowed)

	_, err := s.db.Migrator.Exec(ctx, `UPDATE role_assignments SET revoked_at = now() WHERE id = $1`, s.asg.ID.String())
	require.NoError(t, err)

	require.True(t, s.can(t).Allowed, "tanpa bump, entri cache masih dilayani (dibatasi TTL)")

	require.NoError(t, s.cache.BumpOrganization(ctx, s.org.ID))
	require.False(t, s.can(t).Allowed, "setelah bump, jawaban dihitung ulang dari PostgreSQL")
}

func TestRolePermissionChangeTakesEffectAfterAPlatformBump(t *testing.T) {
	s := newStack(t, testkit.Redis(t).Client(t))
	require.True(t, s.can(t).Allowed)

	_, err := s.db.Migrator.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, s.role.ID.String())
	require.NoError(t, err)
	require.NoError(t, s.cache.BumpPlatform(ctx)) // perubahan role sistem = penghitung platform

	require.False(t, s.can(t).Allowed)
}

func TestRolledBackChangeKeepsTheAnswerAndCostsOnlyARecompute(t *testing.T) {
	s := newStack(t, testkit.Redis(t).Client(t))
	require.True(t, s.can(t).Allowed)

	err := s.tm.WithinTx(ctx, func(txctx context.Context) error {
		db := kernel.DBFrom(txctx, s.db.App)
		_, _ = db.Exec(txctx, `UPDATE role_assignments SET revoked_at = now() WHERE id = $1`, s.asg.ID)
		if err := s.cache.InvalidateInTx(txctx, cache.Targets{Organizations: []uuid.UUID{s.org.ID}}); err != nil {
			return err
		}
		return context.Canceled // bisnis gagal setelah bump sebelum commit
	})
	require.Error(t, err)

	require.True(t, s.can(t).Allowed, "rollback: data tidak berubah, izin tetap berlaku")
}

func TestRedisDownStillAnswersCorrectlyFromPostgresAndNeverAllowsByDefault(t *testing.T) {
	dead := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:1", DialTimeout: 50 * time.Millisecond, MaxRetries: -1,
	})
	t.Cleanup(func() { _ = dead.Close() })
	s := newStack(t, dead)

	require.True(t, s.can(t).Allowed, "Redis mati: jawaban benar dari PostgreSQL")

	_, err := s.db.Migrator.Exec(ctx, `UPDATE role_assignments SET revoked_at = now() WHERE id = $1`, s.asg.ID.String())
	require.NoError(t, err)

	require.False(t, s.can(t).Allowed, "tanpa cache pencabutan langsung terlihat; tidak ada jawaban basi")
}

func TestUserWithNoGrantIsDeniedThroughTheFullStack(t *testing.T) {
	s := newStack(t, testkit.Redis(t).Client(t))
	bob := testkit.NewUser(t, s.db, testkit.InOrganization(s.org))
	oid := s.org.ID

	d, err := s.authz.Can(ctx, domain.Check{
		Identity: authdomain.Identity{UserID: bob.ID, OrganizationID: &oid},
		Scope:    domain.ScopeContext{OrganizationID: &oid},
		Action:   permProbe,
	})

	require.NoError(t, err)
	require.False(t, d.Allowed)
	require.Equal(t, domain.ReasonNoPermission, d.Reason)
}

func TestDatabaseFailureThroughTheFullStackIsAnErrorNotADecision(t *testing.T) {
	s := newStack(t, testkit.Redis(t).Client(t))
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	oid := s.org.ID

	d, err := s.authz.Can(canceled, domain.Check{
		Identity: authdomain.Identity{UserID: s.alice.ID, OrganizationID: &oid},
		Scope:    domain.ScopeContext{OrganizationID: &oid},
		Action:   permProbe,
	})

	require.Error(t, err)
	require.False(t, d.Allowed)
}
