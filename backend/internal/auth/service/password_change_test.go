package service_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/security"
)

// Ganti password (docs/05 §13.1, ADR-0020 butir 42–45) terhadap PostgreSQL + Redis sungguhan.

const newPassword = "tapir-lompat-pagar-kuning-77"

func (a *authHarness) storedHash(u testkit.User) string {
	a.t.Helper()
	var h string
	require.NoError(a.t, a.db.Migrator.QueryRow(a.ctx,
		`SELECT secret_hash FROM credentials WHERE user_id = $1 AND credential_type = 'password' AND revoked_at IS NULL`, u.ID).Scan(&h))
	return h
}

func (a *authHarness) outboxCount(name string, u testkit.User) int {
	a.t.Helper()
	return a.count(`SELECT count(*) FROM outbox_events WHERE event_name = $1 AND aggregate_id = $2`, name, u.ID)
}

func (a *authHarness) requireNothingChanged(u testkit.User, hashBefore string, tokens ...string) {
	a.t.Helper()
	require.Equal(a.t, hashBefore, a.storedHash(u), "hash password tidak boleh berubah")
	for _, tok := range tokens {
		_, err := a.svc.Authenticate(a.ctx, tok)
		require.NoError(a.t, err, "sesi tidak boleh tercabut oleh perubahan yang gagal")
	}
	require.Zero(a.t, a.count(`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2`, u.ID, service.EventPasswordChanged))
	require.Zero(a.t, a.outboxCount(service.EventPasswordChanged, u), "tidak ada event outbox")
}

func TestChangePasswordRotatesTheSessionRevokesTheOthersAndRecordsEverythingTogether(t *testing.T) {
	a := newAuthHarness(t)
	org := testkit.NewOrganization(t, a.db)
	u := a.newUserWithPassword(goodPassword, testkit.InOrganization(org))
	res1, first := a.loginIdentity(u)
	res2, _ := a.extraSession(u)
	other := a.newUserWithPassword(goodPassword)
	otherRes, _ := a.loginIdentity(other)
	oldHash := a.storedHash(u)
	before := a.audit.count()

	out, err := a.auth.ChangePassword(a.ctx, first, service.ChangePasswordInput{Current: goodPassword, New: newPassword})

	require.NoError(t, err)
	require.Equal(t, before+1, a.audit.count(), "tepat satu activity untuk ganti password")
	require.NotEmpty(t, out.Issued.Token)
	require.Equal(t, 1, out.RevokedOthers, "hanya sesi lain yang dihitung; sesi pemanggil dirotasi, bukan 'dicabut lain'")
	a.requireDead(res1.Issued.Token) // token lama (fiksasi sesi): mati
	a.requireDead(res2.Issued.Token) // sesi lain: dicabut
	ident, err := a.svc.Authenticate(a.ctx, out.Issued.Token)
	require.NoError(t, err)
	require.NotEqual(t, first.SessionID, ident.SessionID, "ID sesi berotasi saat ganti password")
	require.Equal(t, first.OrganizationID, ident.OrganizationID, "organisasi sesi tidak berubah")
	_, err = a.svc.Authenticate(a.ctx, otherRes.Issued.Token)
	require.NoError(t, err, "sesi pengguna lain tidak boleh tersentuh")

	// Hash baru: Argon2id, berbeda, dan password lama tidak lagi berlaku.
	require.NotEqual(t, oldHash, a.storedHash(u))
	require.True(t, strings.HasPrefix(a.storedHash(u), "$argon2id$"))
	_, err = a.login(u.Email, goodPassword)
	require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	_, err = a.login(u.Email, newPassword)
	require.NoError(t, err)

	// Event keamanan + activity + outbox, semuanya untuk satu perubahan.
	var meta string
	require.NoError(t, a.db.Migrator.QueryRow(a.ctx,
		`SELECT metadata::text FROM security_events WHERE user_id = $1 AND event_type = $2 AND session_id = $3 AND result = 'success'`,
		u.ID, service.EventPasswordChanged, ident.SessionID).Scan(&meta))
	require.Contains(t, meta, first.SessionID.String(), "previous_session_id")
	require.Contains(t, meta, `"revoked_other_sessions": 1`)
	require.NotContains(t, meta, goodPassword)
	require.NotContains(t, meta, newPassword)
	var found bool
	for _, e := range a.audit.entries {
		if e.Action == "auth.password.change" {
			found = true
			require.Equal(t, "user", e.ResourceType)
			require.Equal(t, u.ID, e.ResourceID)
		}
	}
	require.True(t, found, "activity auth.password.change")

	require.Equal(t, 1, a.outboxCount(service.EventPasswordChanged, u))
	var payload string
	var orgID *string
	require.NoError(t, a.db.Migrator.QueryRow(a.ctx,
		`SELECT payload::text, organization_id::text FROM outbox_events WHERE event_name = $1 AND aggregate_id = $2`,
		service.EventPasswordChanged, u.ID).Scan(&payload, &orgID))
	require.Contains(t, payload, u.ID.String())
	require.Contains(t, payload, `"revoked_other_sessions": 1`)
	require.NotContains(t, payload, goodPassword)
	require.NotContains(t, payload, newPassword)
	require.NotContains(t, payload, "argon2")
	require.NotNil(t, orgID)
	require.Equal(t, org.ID.String(), *orgID)
}

func TestChangePasswordWithTheWrongCurrentPasswordChangesNothingAndRecordsAFailure(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	res1, first := a.loginIdentity(u)
	res2, _ := a.extraSession(u)
	hashBefore := a.storedHash(u)
	auditBefore := a.audit.count()
	comparesBefore := a.spy.compares.Load()

	for name, current := range map[string]string{
		"password salah":      "bukan-password-yang-benar",
		"kosong":              "",
		"lebih dari 256 byte": strings.Repeat("x", security.MaxPasswordBytes+1),
	} {
		_, err := a.auth.ChangePassword(a.ctx, first, service.ChangePasswordInput{Current: current, New: newPassword})
		require.ErrorIs(t, err, domain.ErrInvalidCredentials, name)
	}

	a.requireNothingChanged(u, hashBefore, res1.Issued.Token, res2.Issued.Token)
	require.Equal(t, auditBefore, a.audit.count(), "kegagalan tidak menulis activity")
	require.Equal(t, 3, a.count(
		`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2 AND result = 'failure' AND metadata->>'reason' = 'bad_current_password' AND session_id = $3`,
		u.ID, service.EventPasswordChangeFailed, first.SessionID))
	require.Greater(t, a.spy.compares.Load(), comparesBefore, "password saat ini tetap diverifikasi")
	_, err := a.login(u.Email, goodPassword)
	require.NoError(t, err, "password lama tetap berlaku")
}

func TestChangePasswordChecksTheCurrentPasswordBeforeThePolicy(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	_, first := a.loginIdentity(u)

	_, err := a.auth.ChangePassword(a.ctx, first, service.ChangePasswordInput{Current: "salah-sekali-ini", New: "pendek"})

	require.ErrorIs(t, err, domain.ErrInvalidCredentials, "password lama salah mendahului penilaian kebijakan")
	require.NotErrorIs(t, err, security.ErrPasswordTooShort)
}

func TestChangePasswordRejectsWeakLongAndUnchangedPasswordsWithoutSideEffects(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	res1, first := a.loginIdentity(u)
	res2, _ := a.extraSession(u)
	hashBefore := a.storedHash(u)

	_, err := a.auth.ChangePassword(a.ctx, first, service.ChangePasswordInput{Current: goodPassword, New: "pendek"})
	require.ErrorIs(t, err, security.ErrPasswordTooShort)
	_, err = a.auth.ChangePassword(a.ctx, first, service.ChangePasswordInput{Current: goodPassword, New: strings.Repeat("y", security.MaxPasswordBytes+1)})
	require.ErrorIs(t, err, security.ErrPasswordTooLong)
	_, err = a.auth.ChangePassword(a.ctx, first, service.ChangePasswordInput{Current: goodPassword, New: goodPassword})
	require.ErrorIs(t, err, domain.ErrSamePassword)

	a.requireNothingChanged(u, hashBefore, res1.Issued.Token, res2.Issued.Token)
	require.Zero(t, a.count(`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2`, u.ID, service.EventPasswordChangeFailed),
		"penolakan kebijakan bukan tebakan password: tidak dicatat sebagai kegagalan")
}

func TestChangePasswordIsRefusedForARevokedSessionAndLeavesThePasswordAlone(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	_, stale := a.loginIdentity(u)
	a.extraSession(u)
	require.NoError(t, a.auth.Logout(a.ctx, stale)) // identitas basi: sesinya sudah dicabut
	hashBefore := a.storedHash(u)

	_, err := a.auth.ChangePassword(a.ctx, stale, service.ChangePasswordInput{Current: goodPassword, New: newPassword})

	require.ErrorIs(t, err, domain.ErrUnauthenticated)
	require.Equal(t, hashBefore, a.storedHash(u), "sesi tercabut tidak boleh mengganti password (rollback penuh)")
	require.Zero(t, a.outboxCount(service.EventPasswordChanged, u))
	require.Equal(t, 1, a.sessionActive(u), "sesi lain tetap hidup")
}

func (a *authHarness) sessionActive(u testkit.User) int {
	a.t.Helper()
	return a.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, u.ID)
}

func TestChangePasswordForAUserWithoutAPasswordCredentialIsRefused(t *testing.T) {
	a := newAuthHarness(t)
	u := testkit.NewUser(t, a.db) // tanpa credential password
	iss, err := a.svc.Create(a.ctx, service.CreateParams{UserID: u.ID})
	require.NoError(t, err)
	ident, err := a.svc.Authenticate(a.ctx, iss.Token)
	require.NoError(t, err)

	_, err = a.auth.ChangePassword(a.ctx, *ident, service.ChangePasswordInput{Current: goodPassword, New: newPassword})

	require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	require.Zero(t, a.count(`SELECT count(*) FROM credentials WHERE user_id = $1`, u.ID), "tidak ada credential yang dibuat")
}

func TestChangePasswordIsAtomicWhenTheActivityCannotBeWritten(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	res1, first := a.loginIdentity(u)
	res2, _ := a.extraSession(u)
	hashBefore := a.storedHash(u)
	a.audit.failWith = errors.New("audit tidak bisa menulis")

	_, err := a.auth.ChangePassword(a.ctx, first, service.ChangePasswordInput{Current: goodPassword, New: newPassword})

	require.ErrorIs(t, err, kernel.ErrTxRollbackOnly)
	a.requireNothingChanged(u, hashBefore, res1.Issued.Token, res2.Issued.Token)
	a.audit.failWith = nil
	_, err = a.login(u.Email, goodPassword)
	require.NoError(t, err, "password lama tetap berlaku setelah rollback")
}

func TestConcurrentPasswordChangesFromTheSameSessionLetExactlyOneWin(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	_, first := a.loginIdentity(u)
	candidates := []string{"kandidat-satu-aman-001", "kandidat-dua-aman-002"}

	var wg sync.WaitGroup
	errs := make([]error, len(candidates))
	for i, pw := range candidates {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = a.auth.ChangePassword(a.ctx, first, service.ChangePasswordInput{Current: goodPassword, New: pw})
		}()
	}
	wg.Wait()

	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
			continue
		}
		require.True(t, errors.Is(err, domain.ErrInvalidCredentials) || errors.Is(err, domain.ErrUnauthenticated),
			"yang kalah berlomba ditolak tanpa efek samping, bukan 500: %v", err)
	}
	require.Equal(t, 1, wins, "tepat satu perubahan yang commit")

	working := 0
	for _, pw := range candidates {
		if _, err := a.login(u.Email, pw); err == nil {
			working++
		}
	}
	require.Equal(t, 1, working, "tepat satu password baru yang berlaku")
	_, err := a.login(u.Email, goodPassword)
	require.ErrorIs(t, err, domain.ErrInvalidCredentials)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2`, u.ID, service.EventPasswordChanged))
	require.Equal(t, 1, a.outboxCount(service.EventPasswordChanged, u))
}
