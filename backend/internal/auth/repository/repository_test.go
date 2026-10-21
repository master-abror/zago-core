package repository_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/repository"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Semua kueri dijalankan lewat role runtime (app_user) terhadap PostgreSQL 18 sungguhan; baris
// fixture ditulis lewat role pemilik skema (testkit). Tanpa mock DB (docs/22 §10.1).

type env struct {
	ctx context.Context
	db  *testpg.DB
	q   *repository.Queries
}

func newEnv(t *testing.T) env {
	t.Helper()
	db := testkit.NewDB(t)
	return env{ctx: context.Background(), db: db, q: repository.New(db.App)}
}

func hashOf(label string) []byte {
	h := sha256.Sum256([]byte(label))
	return h[:]
}

func (e env) session(t *testing.T, user uuid.UUID, org *uuid.UUID, label string, expiresIn time.Duration) repository.Session {
	t.Helper()
	ua := "test-agent/1.0"
	ip := netip.MustParseAddr("203.0.113.7")
	s, err := e.q.InsertSession(e.ctx, repository.InsertSessionParams{
		TokenHash:      hashOf(label),
		UserID:         user,
		OrganizationID: org,
		ExpiresAt:      time.Now().Add(expiresIn),
		IpAddress:      &ip,
		UserAgent:      &ua,
		AuthLevel:      "password",
	})
	require.NoError(t, err)
	return s
}

func requireNoRows(t *testing.T, err error) {
	t.Helper()
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func requirePgCode(t *testing.T, err error, code string) {
	t.Helper()
	var pgErr *pgconn.PgError
	require.True(t, errors.As(err, &pgErr), "bukan *pgconn.PgError: %v", err)
	require.Equal(t, code, pgErr.Code)
}

// ---- credentials ----

func TestPasswordCredential(t *testing.T) {
	e := newEnv(t)
	u := testkit.NewUser(t, e.db)

	t.Run("tanpa kredensial: tidak ada baris", func(t *testing.T) {
		_, err := e.q.GetActivePasswordCredential(e.ctx, u.ID)
		requireNoRows(t, err)
	})

	credID, err := e.q.InsertPasswordCredential(e.ctx, repository.InsertPasswordCredentialParams{UserID: u.ID, SecretHash: "$argon2id$h1"})
	require.NoError(t, err)

	t.Run("dibaca kembali", func(t *testing.T) {
		got, err := e.q.GetActivePasswordCredential(e.ctx, u.ID)
		require.NoError(t, err)
		require.Equal(t, credID, got.ID)
		require.Equal(t, "$argon2id$h1", got.SecretHash)
		require.Nil(t, got.LastUsedAt)
	})

	t.Run("satu password aktif per user (23505)", func(t *testing.T) {
		_, err := e.q.InsertPasswordCredential(e.ctx, repository.InsertPasswordCredentialParams{UserID: u.ID, SecretHash: "$argon2id$h2"})
		requirePgCode(t, err, "23505")
	})

	t.Run("kredensial dicabut tidak terbaca", func(t *testing.T) {
		u2 := testkit.NewUser(t, e.db)
		cid, err := e.q.InsertPasswordCredential(e.ctx, repository.InsertPasswordCredentialParams{UserID: u2.ID, SecretHash: "$argon2id$x"})
		require.NoError(t, err)
		_, err = e.db.Migrator.Exec(e.ctx, `UPDATE credentials SET revoked_at = now() WHERE id = $1`, cid)
		require.NoError(t, err)
		_, err = e.q.GetActivePasswordCredential(e.ctx, u2.ID)
		requireNoRows(t, err)
	})

	t.Run("secret_hash NULL dikembalikan kosong (gagal-tutup di hasher)", func(t *testing.T) {
		u3 := testkit.NewUser(t, e.db)
		_, err := e.db.Migrator.Exec(e.ctx,
			`INSERT INTO credentials (user_id, credential_type, secret_hash) VALUES ($1, 'password', NULL)`, u3.ID)
		require.NoError(t, err)
		got, err := e.q.GetActivePasswordCredential(e.ctx, u3.ID)
		require.NoError(t, err)
		require.Empty(t, got.SecretHash)
	})

	t.Run("hanya kredensial milik user itu", func(t *testing.T) {
		other := testkit.NewUser(t, e.db)
		_, err := e.q.GetActivePasswordCredential(e.ctx, other.ID)
		requireNoRows(t, err)
	})
}

func TestRehashPasswordIfUnchanged(t *testing.T) {
	e := newEnv(t)
	u := testkit.NewUser(t, e.db)
	cid, err := e.q.InsertPasswordCredential(e.ctx, repository.InsertPasswordCredentialParams{UserID: u.ID, SecretHash: "old"})
	require.NoError(t, err)

	t.Run("hash masih sama: rehash tertulis dan last_used_at terisi", func(t *testing.T) {
		n, err := e.q.RehashPasswordIfUnchanged(e.ctx, repository.RehashPasswordIfUnchangedParams{
			CredentialID: cid, OldHash: "old", NewHash: "rehashed"})
		require.NoError(t, err)
		require.EqualValues(t, 1, n)
		got, err := e.q.GetActivePasswordCredential(e.ctx, u.ID)
		require.NoError(t, err)
		require.Equal(t, "rehashed", got.SecretHash)
		require.NotNil(t, got.LastUsedAt)
	})

	t.Run("password diganti bersamaan: rehash dari hash lama TIDAK menimpa", func(t *testing.T) {
		n, err := e.q.SetPasswordHash(e.ctx, repository.SetPasswordHashParams{CredentialID: cid, SecretHash: "changed-by-user"})
		require.NoError(t, err)
		require.EqualValues(t, 1, n)

		// login yang berjalan serentak masih memegang "rehashed" sebagai hash lama
		n, err = e.q.RehashPasswordIfUnchanged(e.ctx, repository.RehashPasswordIfUnchangedParams{
			CredentialID: cid, OldHash: "rehashed", NewHash: "stale-rehash"})
		require.NoError(t, err)
		require.Zero(t, n, "rehash basi harus ditolak")

		got, err := e.q.GetActivePasswordCredential(e.ctx, u.ID)
		require.NoError(t, err)
		require.Equal(t, "changed-by-user", got.SecretHash)
	})

	t.Run("SetPasswordHash pada kredensial tak dikenal: 0 baris", func(t *testing.T) {
		n, err := e.q.SetPasswordHash(e.ctx, repository.SetPasswordHashParams{CredentialID: id.NewID(), SecretHash: "x"})
		require.NoError(t, err)
		require.Zero(t, n)
	})
}

// ---- sessions ----

func TestSessionLookupAndUniqueness(t *testing.T) {
	e := newEnv(t)
	org := testkit.NewOrganization(t, e.db)
	u := testkit.NewUser(t, e.db, testkit.InOrganization(org))

	s := e.session(t, u.ID, &org.ID, "tok-a", time.Hour)
	require.Equal(t, hashOf("tok-a"), s.TokenHash)
	require.Nil(t, s.RevokedAt)
	require.NotNil(t, s.OrganizationID)
	require.Equal(t, org.ID, *s.OrganizationID)

	t.Run("ditemukan lewat hash token", func(t *testing.T) {
		got, err := e.q.GetSessionByTokenHash(e.ctx, hashOf("tok-a"))
		require.NoError(t, err)
		require.Equal(t, s.ID, got.ID)
		require.Equal(t, u.ID, got.UserID)
		require.Equal(t, "203.0.113.7", got.IpAddress.String())
	})

	t.Run("hash tak dikenal: tidak ada baris", func(t *testing.T) {
		_, err := e.q.GetSessionByTokenHash(e.ctx, hashOf("tok-tak-ada"))
		requireNoRows(t, err)
	})

	t.Run("token mentah bukan kunci: mencari dengan nilai token (bukan hash) tidak menemukan apa-apa", func(t *testing.T) {
		_, err := e.q.GetSessionByTokenHash(e.ctx, []byte("tok-a"))
		requireNoRows(t, err)
	})

	t.Run("token_hash unik (23505)", func(t *testing.T) {
		_, err := e.q.InsertSession(e.ctx, repository.InsertSessionParams{
			TokenHash: hashOf("tok-a"), UserID: u.ID, ExpiresAt: time.Now().Add(time.Hour), AuthLevel: "password"})
		requirePgCode(t, err, "23505")
	})

	t.Run("sesi tanpa organisasi (identitas platform)", func(t *testing.T) {
		p := e.session(t, u.ID, nil, "tok-platform", time.Hour)
		require.Nil(t, p.OrganizationID)
	})

	t.Run("GetSessionByIDForUser: milik user lain tidak terlihat", func(t *testing.T) {
		got, err := e.q.GetSessionByIDForUser(e.ctx, repository.GetSessionByIDForUserParams{SessionID: s.ID, UserID: u.ID})
		require.NoError(t, err)
		require.Equal(t, s.ID, got.ID)

		stranger := testkit.NewUser(t, e.db)
		_, err = e.q.GetSessionByIDForUser(e.ctx, repository.GetSessionByIDForUserParams{SessionID: s.ID, UserID: stranger.ID})
		requireNoRows(t, err)
	})
}

func TestListActiveSessionsExcludesRevokedExpiredAndOthers(t *testing.T) {
	e := newEnv(t)
	u := testkit.NewUser(t, e.db)
	stranger := testkit.NewUser(t, e.db)

	live1 := e.session(t, u.ID, nil, "live1", time.Hour)
	live2 := e.session(t, u.ID, nil, "live2", time.Hour)
	revoked := e.session(t, u.ID, nil, "revoked", time.Hour)
	_ = e.session(t, u.ID, nil, "expired", -time.Minute)
	_ = e.session(t, stranger.ID, nil, "stranger", time.Hour)
	_, err := e.q.RevokeSession(e.ctx, repository.RevokeSessionParams{SessionID: revoked.ID, UserID: u.ID})
	require.NoError(t, err)

	got, err := e.q.ListActiveSessionsByUser(e.ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, []uuid.UUID{live2.ID, live1.ID}, []uuid.UUID{got[0].ID, got[1].ID}, "terbaru lebih dulu")
	for _, s := range got {
		require.Equal(t, u.ID, s.UserID)
	}
}

func TestRevokeSession(t *testing.T) {
	e := newEnv(t)
	owner := testkit.NewUser(t, e.db)
	stranger := testkit.NewUser(t, e.db)
	s := e.session(t, owner.ID, nil, "to-revoke", time.Hour)

	t.Run("user lain tidak bisa mencabut sesi orang lain", func(t *testing.T) {
		_, err := e.q.RevokeSession(e.ctx, repository.RevokeSessionParams{SessionID: s.ID, UserID: stranger.ID})
		requireNoRows(t, err)
		got, err := e.q.GetSessionByTokenHash(e.ctx, hashOf("to-revoke"))
		require.NoError(t, err)
		require.Nil(t, got.RevokedAt, "sesi harus tetap aktif")
	})

	t.Run("pemilik mencabut; hash dikembalikan untuk pembersihan Redis", func(t *testing.T) {
		row, err := e.q.RevokeSession(e.ctx, repository.RevokeSessionParams{SessionID: s.ID, UserID: owner.ID})
		require.NoError(t, err)
		require.Equal(t, s.ID, row.ID)
		require.Equal(t, hashOf("to-revoke"), row.TokenHash)

		got, err := e.q.GetSessionByTokenHash(e.ctx, hashOf("to-revoke"))
		require.NoError(t, err)
		require.NotNil(t, got.RevokedAt)
	})

	t.Run("mencabut dua kali: tak ada baris kedua kalinya", func(t *testing.T) {
		_, err := e.q.RevokeSession(e.ctx, repository.RevokeSessionParams{SessionID: s.ID, UserID: owner.ID})
		requireNoRows(t, err)
	})
}

func TestRevokeOtherAndAllSessions(t *testing.T) {
	e := newEnv(t)
	u := testkit.NewUser(t, e.db)
	stranger := testkit.NewUser(t, e.db)
	keep := e.session(t, u.ID, nil, "keep", time.Hour)
	o1 := e.session(t, u.ID, nil, "o1", time.Hour)
	o2 := e.session(t, u.ID, nil, "o2", time.Hour)
	foreign := e.session(t, stranger.ID, nil, "foreign", time.Hour)

	rows, err := e.q.RevokeOtherSessions(e.ctx, repository.RevokeOtherSessionsParams{UserID: u.ID, KeepSessionID: keep.ID})
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{o1.ID, o2.ID}, []uuid.UUID{rows[0].ID, rows[1].ID})
	require.ElementsMatch(t, [][]byte{hashOf("o1"), hashOf("o2")}, [][]byte{rows[0].TokenHash, rows[1].TokenHash})

	active, err := e.q.ListActiveSessionsByUser(e.ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, keep.ID, active[0].ID)

	fs, err := e.q.GetSessionByTokenHash(e.ctx, hashOf("foreign"))
	require.NoError(t, err)
	require.Equal(t, foreign.ID, fs.ID)
	require.Nil(t, fs.RevokedAt, "sesi user lain tak tersentuh")

	all, err := e.q.RevokeAllSessions(e.ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, all, 1, "hanya yang belum dicabut")
	require.Equal(t, keep.ID, all[0].ID)

	none, err := e.q.RevokeAllSessions(e.ctx, u.ID)
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestTouchSessionActivity(t *testing.T) {
	e := newEnv(t)
	u := testkit.NewUser(t, e.db)
	s := e.session(t, u.ID, nil, "touch", time.Hour)
	lastActivity := func() time.Time {
		got, err := e.q.GetSessionByTokenHash(e.ctx, hashOf("touch"))
		require.NoError(t, err)
		return got.LastActivityAt
	}

	later := s.LastActivityAt.Add(time.Minute)
	n, err := e.q.TouchSessionActivity(e.ctx, repository.TouchSessionActivityParams{SessionID: s.ID, At: later})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.WithinDuration(t, later, lastActivity(), time.Millisecond)

	// mundur: tetap 1 baris (sesi hidup) tetapi nilainya TIDAK mundur
	n, err = e.q.TouchSessionActivity(e.ctx, repository.TouchSessionActivityParams{SessionID: s.ID, At: s.LastActivityAt})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	require.WithinDuration(t, later, lastActivity(), time.Millisecond, "tidak pernah mundur")

	_, err = e.q.RevokeSession(e.ctx, repository.RevokeSessionParams{SessionID: s.ID, UserID: u.ID})
	require.NoError(t, err)
	n, err = e.q.TouchSessionActivity(e.ctx, repository.TouchSessionActivityParams{SessionID: s.ID, At: later.Add(time.Hour)})
	require.NoError(t, err)
	require.Zero(t, n, "sesi dicabut: 0 baris, tidak dihidupkan lewat aktivitas")
	require.WithinDuration(t, later, lastActivity(), time.Millisecond)

	n, err = e.q.TouchSessionActivity(e.ctx, repository.TouchSessionActivityParams{SessionID: id.NewID(), At: later})
	require.NoError(t, err)
	require.Zero(t, n, "sesi tak dikenal: 0 baris")
}

// ---- security events ----

func TestSecurityEvents(t *testing.T) {
	e := newEnv(t)
	u := testkit.NewUser(t, e.db)
	stranger := testkit.NewUser(t, e.db)

	insert := func(user uuid.UUID, typ, result string) {
		t.Helper()
		ip := netip.MustParseAddr("198.51.100.20")
		rid := "req-1"
		err := e.q.InsertSecurityEvent(e.ctx, repository.InsertSecurityEventParams{
			UserID: &user, EventType: typ, Result: result, IpAddress: &ip, RequestID: &rid, Metadata: []byte(`{}`)})
		require.NoError(t, err)
	}
	for i := range 5 {
		insert(u.ID, fmt.Sprintf("auth.e%d", i), "success")
	}
	insert(stranger.ID, "auth.stranger", "failure")

	t.Run("halaman pertama: terbaru dulu, hanya milik user", func(t *testing.T) {
		p1, err := e.q.ListSecurityEventsByUser(e.ctx, repository.ListSecurityEventsByUserParams{UserID: u.ID, PageSize: 2})
		require.NoError(t, err)
		require.Equal(t, []string{"auth.e4", "auth.e3"}, []string{p1[0].EventType, p1[1].EventType})
	})

	t.Run("keyset menelusuri semua halaman tanpa duplikat dan tanpa milik orang lain", func(t *testing.T) {
		var seen []string
		var cur *repository.SecurityEvent
		for range 10 { // pengaman agar tes tak berputar selamanya
			params := repository.ListSecurityEventsByUserParams{UserID: u.ID, PageSize: 2}
			if cur != nil {
				c, id := cur.CreatedAt, cur.ID
				params.BeforeCreatedAt, params.BeforeID = &c, &id
			}
			page, err := e.q.ListSecurityEventsByUser(e.ctx, params)
			require.NoError(t, err)
			if len(page) == 0 {
				break
			}
			for _, ev := range page {
				require.NotNil(t, ev.UserID)
				require.Equal(t, u.ID, *ev.UserID)
				seen = append(seen, ev.EventType)
			}
			cur = &page[len(page)-1]
		}
		require.Equal(t, []string{"auth.e4", "auth.e3", "auth.e2", "auth.e1", "auth.e0"}, seen)
	})

	t.Run("app_user tidak bisa mengubah atau menghapus (append-only)", func(t *testing.T) {
		_, err := e.db.App.Exec(e.ctx, `UPDATE security_events SET result = 'success' WHERE user_id = $1`, stranger.ID)
		requirePgCode(t, err, "42501")
		_, err = e.db.App.Exec(e.ctx, `DELETE FROM security_events WHERE user_id = $1`, u.ID)
		requirePgCode(t, err, "42501")
	})

	t.Run("hasil di luar success/failure ditolak skema", func(t *testing.T) {
		err := e.q.InsertSecurityEvent(e.ctx, repository.InsertSecurityEventParams{
			EventType: "auth.x", Result: "maybe", Metadata: []byte(`{}`)})
		requirePgCode(t, err, "23514")
	})
}

// ---- users & keanggotaan ----

func TestGetUserForLoginByEmail(t *testing.T) {
	e := newEnv(t)
	u := testkit.NewUser(t, e.db, testkit.WithEmail("Budi@Example.Test"))

	got, err := e.q.GetUserForLoginByEmail(e.ctx, "budi@example.test")
	require.NoError(t, err)
	require.Equal(t, u.ID, got.ID)
	require.Equal(t, "active", got.Status)

	_, err = e.q.GetUserForLoginByEmail(e.ctx, "nobody@example.test")
	requireNoRows(t, err)

	t.Run("user yang dihapus lunak tidak bisa login", func(t *testing.T) {
		_, err := e.db.Migrator.Exec(e.ctx, `UPDATE users SET deleted_at = now() WHERE id = $1`, u.ID)
		require.NoError(t, err)
		_, err = e.q.GetUserForLoginByEmail(e.ctx, "budi@example.test")
		requireNoRows(t, err)
		_, err = e.q.GetUserByID(e.ctx, u.ID)
		requireNoRows(t, err)
	})
}

func TestMembershipLookups(t *testing.T) {
	e := newEnv(t)
	orgA := testkit.NewOrganization(t, e.db)
	orgB := testkit.NewOrganization(t, e.db)
	u := testkit.NewUser(t, e.db, testkit.InOrganization(orgA))

	member := func(org uuid.UUID) bool {
		t.Helper()
		ok, err := e.q.HasActiveMembership(e.ctx, repository.HasActiveMembershipParams{UserID: u.ID, OrganizationID: org})
		require.NoError(t, err)
		return ok
	}

	require.True(t, member(orgA.ID))
	require.False(t, member(orgB.ID), "organisasi lain: bukan anggota (isolasi tenant)")
	require.False(t, member(id.NewID()), "organisasi tak dikenal")

	t.Run("keanggotaan ditangguhkan tidak dihitung", func(t *testing.T) {
		_, err := e.db.Migrator.Exec(e.ctx, `UPDATE organization_memberships SET status = 'suspended' WHERE user_id = $1`, u.ID)
		require.NoError(t, err)
		require.False(t, member(orgA.ID))
		_, err = e.q.GetDefaultActiveOrganizationID(e.ctx, u.ID)
		requireNoRows(t, err)
		_, err = e.db.Migrator.Exec(e.ctx, `UPDATE organization_memberships SET status = 'active' WHERE user_id = $1`, u.ID)
		require.NoError(t, err)
	})

	t.Run("organisasi ditangguhkan atau dihapus tidak dihitung", func(t *testing.T) {
		_, err := e.db.Migrator.Exec(e.ctx, `UPDATE organizations SET status = 'suspended' WHERE id = $1`, orgA.ID)
		require.NoError(t, err)
		require.False(t, member(orgA.ID))
		_, err = e.db.Migrator.Exec(e.ctx, `UPDATE organizations SET status = 'active', deleted_at = now() WHERE id = $1`, orgA.ID)
		require.NoError(t, err)
		require.False(t, member(orgA.ID))
		_, err = e.db.Migrator.Exec(e.ctx, `UPDATE organizations SET deleted_at = NULL WHERE id = $1`, orgA.ID)
		require.NoError(t, err)
		require.True(t, member(orgA.ID))
	})

	t.Run("organisasi awal = keanggotaan aktif tertua", func(t *testing.T) {
		_, err := e.db.Migrator.Exec(e.ctx,
			`INSERT INTO organization_memberships (organization_id, user_id, status, created_at)
			 VALUES ($1, $2, 'active', now() + interval '1 hour')`, orgB.ID, u.ID)
		require.NoError(t, err)
		got, err := e.q.GetDefaultActiveOrganizationID(e.ctx, u.ID)
		require.NoError(t, err)
		require.Equal(t, orgA.ID, got)
		require.True(t, member(orgB.ID))
	})

	t.Run("tanpa keanggotaan: tidak ada organisasi awal", func(t *testing.T) {
		loner := testkit.NewUser(t, e.db)
		_, err := e.q.GetDefaultActiveOrganizationID(e.ctx, loner.ID)
		requireNoRows(t, err)
	})
}

func TestSetUserLastLogin(t *testing.T) {
	e := newEnv(t)
	u := testkit.NewUser(t, e.db)
	before, err := e.q.GetUserByID(e.ctx, u.ID)
	require.NoError(t, err)
	require.Nil(t, before.LastLoginAt)

	require.NoError(t, e.q.SetUserLastLogin(e.ctx, u.ID))
	after, err := e.q.GetUserByID(e.ctx, u.ID)
	require.NoError(t, err)
	require.NotNil(t, after.LastLoginAt)
}

// ---- Store dan transaksi ----

func TestStoreFollowsTransactionOnContext(t *testing.T) {
	e := newEnv(t)
	st := repository.NewStore(e.db.App)
	tm := kernel.NewTxManager(e.db.App, nil)
	u := testkit.NewUser(t, e.db)

	t.Run("rollback: tulisan lewat Store ikut dibatalkan", func(t *testing.T) {
		boom := errors.New("boom")
		err := tm.WithinTx(e.ctx, func(ctx context.Context) error {
			_, err := st.Q(ctx).InsertPasswordCredential(ctx, repository.InsertPasswordCredentialParams{UserID: u.ID, SecretHash: "in-tx"})
			require.NoError(t, err)
			got, err := st.Q(ctx).GetActivePasswordCredential(ctx, u.ID)
			require.NoError(t, err, "terlihat di dalam transaksi yang sama")
			require.Equal(t, "in-tx", got.SecretHash)

			// koneksi lain (di luar transaksi) tidak melihatnya sebelum commit
			_, err = e.q.GetActivePasswordCredential(e.ctx, u.ID)
			requireNoRows(t, err)
			return boom
		})
		require.ErrorIs(t, err, boom)
		_, err = st.Q(e.ctx).GetActivePasswordCredential(e.ctx, u.ID)
		requireNoRows(t, err)
	})

	t.Run("commit: tulisan bertahan", func(t *testing.T) {
		err := tm.WithinTx(e.ctx, func(ctx context.Context) error {
			_, err := st.Q(ctx).InsertPasswordCredential(ctx, repository.InsertPasswordCredentialParams{UserID: u.ID, SecretHash: "committed"})
			return err
		})
		require.NoError(t, err)
		got, err := st.Q(e.ctx).GetActivePasswordCredential(e.ctx, u.ID)
		require.NoError(t, err)
		require.Equal(t, "committed", got.SecretHash)
	})
}
