package bootstrap_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/bootstrap"
	"github.com/master-abror/zago-core/backend/internal/dbmigrate"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
	"github.com/master-abror/zago-core/backend/pkg/security"
)

// Tes Bootstrapper memakai PostgreSQL 18 sungguhan dan role app_user (hak runtime, BUKAN
// migrator): membuktikan bahwa bootstrap-admin tidak butuh hak DDL. Hasher memakai parameter
// Argon2id kecil agar cepat; perilakunya (PHC, verifikasi) tetap asli.

const goodPassword = "correct-horse-battery-staple"

var (
	ctx        = context.Background()
	testParams = security.Argon2Params{MemoryKiB: 64, Time: 1, Parallelism: 1}
)

func newHasher(t *testing.T) *security.Argon2idHasher {
	t.Helper()
	h, err := security.NewArgon2idHasher(testParams, 1)
	require.NoError(t, err)
	return h
}

func newBootstrapper(t *testing.T) *bootstrap.Bootstrapper {
	t.Helper()
	b, err := bootstrap.New(newHasher(t), security.PasswordPolicy{MinLength: 12})
	require.NoError(t, err)
	return b
}

// seededDB menghasilkan database termigrasi DENGAN baris platform (prasyarat bootstrap).
func seededDB(t *testing.T) *testpg.DB {
	t.Helper()
	db := testkit.NewDB(t)
	_, err := dbmigrate.Runner{}.Seed(ctx, db.MigratorURL())
	require.NoError(t, err)
	return db
}

func count(t *testing.T, db *testpg.DB, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.App.QueryRow(ctx, query, args...).Scan(&n))
	return n
}

func text(t *testing.T, db *testpg.DB, query string, args ...any) string {
	t.Helper()
	var s string
	require.NoError(t, db.App.QueryRow(ctx, query, args...).Scan(&s))
	return s
}

// totals menghitung baris di semua tabel yang disentuh bootstrap (untuk bukti "tanpa efek").
func totals(t *testing.T, db *testpg.DB) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range []string{"users", "credentials", "roles", "role_assignments", "activities", "security_events"} {
		out[table] = count(t, db, "SELECT count(*) FROM "+table)
	}
	return out
}

func TestRunCreatesSuperAdminWithGeneratedPassword(t *testing.T) {
	db := seededDB(t)
	b := newBootstrapper(t)

	res, err := b.Run(ctx, db.App, bootstrap.Params{Email: "  Admin@Example.ORG "})
	require.NoError(t, err)

	require.Equal(t, "admin@example.org", res.Email)
	require.True(t, res.UserCreated)
	require.True(t, res.RoleCreated)
	require.True(t, res.CredentialCreated)
	require.True(t, res.AssignmentCreated)
	require.True(t, res.Changed())
	require.GreaterOrEqual(t, len(res.GeneratedPassword), 24, "password acak minimal 24 karakter")

	require.Equal(t, "active", text(t, db, "SELECT status FROM users WHERE email = $1", "admin@example.org"))
	require.Equal(t, "admin", text(t, db, "SELECT display_name FROM users WHERE email = $1", "admin@example.org"))

	// Role Super Admin: sistem, tanpa organisasi/grup (ck_roles_boundary).
	require.Equal(t, 1, count(t, db,
		"SELECT count(*) FROM roles WHERE slug = 'super-admin' AND role_type = 'system' AND organization_id IS NULL AND group_id IS NULL"))

	// Assignment platform untuk user ini, tanpa organisasi/scope_id (ck_role_assignments_*).
	require.Equal(t, 1, count(t, db, `
		SELECT count(*) FROM role_assignments ra
		JOIN roles r ON r.id = ra.role_id
		WHERE r.slug = 'super-admin' AND ra.subject_type = 'user' AND ra.subject_id = $1
		  AND ra.scope_type = 'platform' AND ra.scope_id IS NULL AND ra.organization_id IS NULL
		  AND ra.revoked_at IS NULL`, res.UserID))

	// Hash Argon2id, bukan teks biasa, dan cocok dengan password yang dikembalikan.
	hash := text(t, db, "SELECT secret_hash FROM credentials WHERE user_id = $1 AND credential_type = 'password'", res.UserID)
	require.True(t, strings.HasPrefix(hash, "$argon2id$"), "hash PHC argon2id")
	require.NotContains(t, hash, res.GeneratedPassword)
	require.NoError(t, newHasher(t).Compare(ctx, hash, res.GeneratedPassword))
}

func TestRunWithProvidedPasswordDoesNotGenerateOne(t *testing.T) {
	db := seededDB(t)
	b := newBootstrapper(t)

	res, err := b.Run(ctx, db.App, bootstrap.Params{Email: "root@example.org", Password: goodPassword, DisplayName: "Root Admin"})
	require.NoError(t, err)
	require.Empty(t, res.GeneratedPassword, "password dari operator tidak boleh dikembalikan/ditampilkan lagi")
	require.Equal(t, "Root Admin", text(t, db, "SELECT display_name FROM users WHERE id = $1", res.UserID))

	hash := text(t, db, "SELECT secret_hash FROM credentials WHERE user_id = $1", res.UserID)
	require.NoError(t, newHasher(t).Compare(ctx, hash, goodPassword))
	require.ErrorIs(t, newHasher(t).Compare(ctx, hash, goodPassword+"x"), security.ErrMismatch)
}

func TestRunWritesActivityAndSecurityEventWithoutSecrets(t *testing.T) {
	db := seededDB(t)
	res, err := newBootstrapper(t).Run(ctx, db.App, bootstrap.Params{Email: "audit@example.org"})
	require.NoError(t, err)

	require.Equal(t, 1, count(t, db,
		"SELECT count(*) FROM activities WHERE action = 'auth.bootstrap_admin' AND actor_type = 'system' AND resource_type = 'user' AND resource_id = $1 AND result = 'success'", res.UserID))
	require.Equal(t, 1, count(t, db,
		"SELECT count(*) FROM security_events WHERE event_type = 'auth.bootstrap_admin' AND result = 'success' AND user_id = $1", res.UserID))

	for _, q := range []string{
		"SELECT metadata::text FROM activities WHERE action = 'auth.bootstrap_admin'",
		"SELECT metadata::text FROM security_events WHERE event_type = 'auth.bootstrap_admin'",
	} {
		meta := text(t, db, q)
		require.NotContains(t, meta, res.GeneratedPassword, "password tidak boleh masuk jejak audit")
		require.NotContains(t, meta, "audit@example.org", "email tidak boleh masuk metadata audit")
	}
}

func TestRunIsIdempotent(t *testing.T) {
	db := seededDB(t)
	b := newBootstrapper(t)

	first, err := b.Run(ctx, db.App, bootstrap.Params{Email: "admin@example.org"})
	require.NoError(t, err)
	require.True(t, first.Changed())
	before := totals(t, db)
	hashBefore := text(t, db, "SELECT secret_hash FROM credentials WHERE user_id = $1", first.UserID)

	// Run kedua, bahkan dengan password berbeda: tidak ada yang berubah dan tidak ada password baru.
	second, err := b.Run(ctx, db.App, bootstrap.Params{Email: "ADMIN@example.org", Password: goodPassword})
	require.NoError(t, err)
	require.False(t, second.Changed())
	require.Equal(t, first.UserID, second.UserID)
	require.Empty(t, second.GeneratedPassword)

	require.Equal(t, before, totals(t, db), "run kedua tidak boleh menambah baris apa pun (termasuk audit)")
	require.Equal(t, hashBefore, text(t, db, "SELECT secret_hash FROM credentials WHERE user_id = $1", first.UserID),
		"password yang ada tidak boleh ditimpa")
}

func TestRunRestoresRevokedAssignmentWithoutTouchingPassword(t *testing.T) {
	db := seededDB(t)
	b := newBootstrapper(t)

	first, err := b.Run(ctx, db.App, bootstrap.Params{Email: "admin@example.org"})
	require.NoError(t, err)
	hashBefore := text(t, db, "SELECT secret_hash FROM credentials WHERE user_id = $1", first.UserID)

	_, err = db.App.Exec(ctx, "UPDATE role_assignments SET revoked_at = now() WHERE subject_id = $1", first.UserID)
	require.NoError(t, err)

	again, err := b.Run(ctx, db.App, bootstrap.Params{Email: "admin@example.org"})
	require.NoError(t, err)
	require.True(t, again.AssignmentCreated)
	require.False(t, again.UserCreated)
	require.False(t, again.CredentialCreated)
	require.False(t, again.RoleCreated)
	require.Empty(t, again.GeneratedPassword, "kredensial sudah ada: tidak ada password baru untuk ditampilkan")

	require.Equal(t, 1, count(t, db, "SELECT count(*) FROM role_assignments WHERE subject_id = $1 AND revoked_at IS NULL", first.UserID))
	require.Equal(t, 2, count(t, db, "SELECT count(*) FROM role_assignments WHERE subject_id = $1", first.UserID))
	require.Equal(t, hashBefore, text(t, db, "SELECT secret_hash FROM credentials WHERE user_id = $1", first.UserID))
}

func TestRunRejectsWeakPasswordWithoutSideEffects(t *testing.T) {
	db := seededDB(t)
	before := totals(t, db)

	_, err := newBootstrapper(t).Run(ctx, db.App, bootstrap.Params{Email: "admin@example.org", Password: "short"})
	require.ErrorIs(t, err, bootstrap.ErrInvalidInput)
	require.ErrorIs(t, err, security.ErrPasswordTooShort)

	_, err = newBootstrapper(t).Run(ctx, db.App, bootstrap.Params{Email: "admin@example.org", Password: strings.Repeat("a", security.MaxPasswordBytes+1)})
	require.ErrorIs(t, err, bootstrap.ErrInvalidInput)
	require.ErrorIs(t, err, security.ErrPasswordTooLong)

	require.Equal(t, before, totals(t, db), "password ditolak: tidak ada baris yang boleh dibuat")
}

func TestRunRejectsInvalidInputWithoutSideEffects(t *testing.T) {
	db := seededDB(t)
	before := totals(t, db)
	b := newBootstrapper(t)

	for name, p := range map[string]bootstrap.Params{
		"email kosong":          {Email: ""},
		"email spasi saja":      {Email: "   "},
		"tanpa @":               {Email: "bukan-email"},
		"dua @":                 {Email: "a@b@c.org"},
		"dengan nama tampilan":  {Email: "Admin <admin@example.org>"},
		"dalam kurung sudut":    {Email: "<admin@example.org>"},
		"terlalu panjang":       {Email: strings.Repeat("a", 330) + "@example.org"},
		"nama tampilan panjang": {Email: "admin@example.org", DisplayName: strings.Repeat("n", 201)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := b.Run(ctx, db.App, p)
			require.ErrorIs(t, err, bootstrap.ErrInvalidInput)
		})
	}
	require.Equal(t, before, totals(t, db))
}

func TestRunFailsWithoutPlatformRowAndLeavesNothingBehind(t *testing.T) {
	db := testkit.NewDB(t) // termigrasi tetapi TANPA seed platform
	before := totals(t, db)

	_, err := newBootstrapper(t).Run(ctx, db.App, bootstrap.Params{Email: "admin@example.org"})
	require.ErrorIs(t, err, bootstrap.ErrNoPlatform)
	require.Equal(t, before, totals(t, db), "rollback penuh: bahkan role Super Admin tidak boleh tertinggal")
}

func TestRunRefusesExistingUserThatIsNotActive(t *testing.T) {
	for _, status := range []string{"pending", "suspended", "locked", "deactivated", "deleted"} {
		t.Run(status, func(t *testing.T) {
			db := seededDB(t)
			_, err := db.App.Exec(ctx,
				"INSERT INTO users (id, display_name, email, status) VALUES ($1, 'Lama', 'admin@example.org', $2)", id.NewID(), status)
			require.NoError(t, err)
			before := totals(t, db)

			_, err = newBootstrapper(t).Run(ctx, db.App, bootstrap.Params{Email: "admin@example.org"})
			require.ErrorIs(t, err, bootstrap.ErrUserUnavailable)

			require.Equal(t, before, totals(t, db), "tidak ada role/kredensial/assignment untuk akun yang tak aktif")
			require.Equal(t, status, text(t, db, "SELECT status FROM users WHERE email = 'admin@example.org'"), "status tidak boleh diubah")
		})
	}
}

func TestRunRefusesSoftDeletedUser(t *testing.T) {
	db := seededDB(t)
	_, err := db.App.Exec(ctx,
		"INSERT INTO users (id, display_name, email, status, deleted_at) VALUES ($1, 'Hapus', 'admin@example.org', 'active', now())", id.NewID())
	require.NoError(t, err)

	_, err = newBootstrapper(t).Run(ctx, db.App, bootstrap.Params{Email: "admin@example.org"})
	require.ErrorIs(t, err, bootstrap.ErrUserUnavailable)
	require.Equal(t, 0, count(t, db, "SELECT count(*) FROM role_assignments"))
}

func TestRunConcurrentCallsProduceOneAdmin(t *testing.T) {
	db := seededDB(t)
	b := newBootstrapper(t)

	const n = 6
	results := make([]bootstrap.Result, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = b.Run(ctx, db.App, bootstrap.Params{Email: "admin@example.org", Password: goodPassword})
		}()
	}
	wg.Wait()

	changed := 0
	for i := range n {
		require.NoError(t, errs[i])
		if results[i].Changed() {
			changed++
		}
	}
	require.Equal(t, 1, changed, "tepat satu pemanggil yang membuat; sisanya no-op")
	require.Equal(t, 1, count(t, db, "SELECT count(*) FROM users"))
	require.Equal(t, 1, count(t, db, "SELECT count(*) FROM credentials"))
	require.Equal(t, 1, count(t, db, "SELECT count(*) FROM roles WHERE slug = 'super-admin'"))
	require.Equal(t, 1, count(t, db, "SELECT count(*) FROM role_assignments"))
	require.Equal(t, 1, count(t, db, "SELECT count(*) FROM activities WHERE action = 'auth.bootstrap_admin'"))
}

func TestNewRejectsNilHasher(t *testing.T) {
	_, err := bootstrap.New(nil, security.PasswordPolicy{})
	require.Error(t, err)
	require.False(t, errors.Is(err, bootstrap.ErrInvalidInput))
}
