package service_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Manajemen sesi terhadap PostgreSQL+Redis sungguhan: daftar, cabut satu, cabut yang lain.

func (a *authHarness) extraSession(u testkit.User) (service.LoginResult, domain.Identity) {
	a.t.Helper()
	return a.loginIdentity(u)
}

func TestListSessionsReturnsOnlyTheCallersActiveSessionsAndMarksTheCurrentOne(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	_, first := a.loginIdentity(u)
	_, second := a.extraSession(u)
	_, third := a.extraSession(u)
	require.NoError(t, a.auth.Logout(a.ctx, third))
	other := a.newUserWithPassword(goodPassword)
	a.loginIdentity(other)

	list, err := a.auth.ListSessions(a.ctx, second)

	require.NoError(t, err)
	require.Len(t, list, 2, "sesi yang sudah dicabut dan sesi pengguna lain tidak ikut")
	ids := map[uuid.UUID]service.SessionInfo{}
	for _, s := range list {
		ids[s.ID] = s
	}
	require.Contains(t, ids, first.SessionID)
	require.Contains(t, ids, second.SessionID)
	require.True(t, ids[second.SessionID].Current)
	require.False(t, ids[first.SessionID].Current)
	require.Equal(t, string(domain.AuthLevelPassword), ids[first.SessionID].AuthLevel)
	require.True(t, list[0].CreatedAt.After(list[1].CreatedAt) || list[0].CreatedAt.Equal(list[1].CreatedAt), "terbaru dulu")
}

func TestRevokeSessionRevokesTheTargetAndRecordsEventAndActivity(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	res1, first := a.loginIdentity(u)
	res2, second := a.extraSession(u)

	require.NoError(t, a.auth.RevokeSession(a.ctx, first, second.SessionID))

	a.requireDead(res2.Issued.Token)
	_, err := a.svc.Authenticate(a.ctx, res1.Issued.Token)
	require.NoError(t, err, "sesi yang dipakai pemanggil tidak ikut tercabut")
	require.Equal(t, 1, a.count(
		`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2 AND session_id = $3 AND metadata->>'current' = 'false'`,
		u.ID, service.EventSessionRevoked, second.SessionID))
	last := len(a.audit.entries) - 1
	require.Equal(t, "auth.session.revoke", a.audit.entries[last].Action)
	require.Equal(t, second.SessionID, a.audit.entries[last].ResourceID)
}

func TestRevokeSessionAllowsRevokingTheCurrentSession(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	res, ident := a.loginIdentity(u)

	require.NoError(t, a.auth.RevokeSession(a.ctx, ident, ident.SessionID))

	a.requireDead(res.Issued.Token)
	require.Equal(t, 1, a.count(
		`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2 AND metadata->>'current' = 'true'`,
		u.ID, service.EventSessionRevoked))
}

func TestRevokeSessionRejectsForeignUnknownAndAlreadyRevokedSessionsWithoutSideEffects(t *testing.T) {
	a := newAuthHarness(t)
	victim := a.newUserWithPassword(goodPassword)
	attacker := a.newUserWithPassword(goodPassword)
	victimRes, victimIdent := a.loginIdentity(victim)
	_, attackerIdent := a.loginIdentity(attacker)
	_, goneIdent := a.extraSession(attacker)
	require.NoError(t, a.auth.Logout(a.ctx, goneIdent))
	before := a.audit.count()

	for name, target := range map[string]uuid.UUID{
		"sesi pengguna lain": victimIdent.SessionID,
		"sesi tak dikenal":   id.NewID(),
		"sesi sudah dicabut": goneIdent.SessionID,
	} {
		err := a.auth.RevokeSession(a.ctx, attackerIdent, target)
		require.ErrorIs(t, err, domain.ErrSessionNotFound, name)
	}

	_, err := a.svc.Authenticate(a.ctx, victimRes.Issued.Token)
	require.NoError(t, err, "sesi korban tidak boleh tersentuh")
	require.Zero(t, a.count(`SELECT count(*) FROM security_events WHERE event_type = $1`, service.EventSessionRevoked))
	require.Equal(t, before, a.audit.count(), "penolakan tidak menulis activity")
}

func TestRevokeOtherSessionsKeepsTheCurrentOneAndRecordsASingleEvent(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	res1, first := a.loginIdentity(u)
	res2, _ := a.extraSession(u)
	res3, _ := a.extraSession(u)
	other := a.newUserWithPassword(goodPassword)
	otherRes, _ := a.loginIdentity(other)

	n, err := a.auth.RevokeOtherSessions(a.ctx, first)

	require.NoError(t, err)
	require.Equal(t, 2, n)
	_, err = a.svc.Authenticate(a.ctx, res1.Issued.Token)
	require.NoError(t, err)
	a.requireDead(res2.Issued.Token)
	a.requireDead(res3.Issued.Token)
	_, err = a.svc.Authenticate(a.ctx, otherRes.Issued.Token)
	require.NoError(t, err, "sesi pengguna lain tidak boleh tersentuh")
	require.Equal(t, 1, a.count(
		`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2 AND (metadata->>'count')::int = 2`,
		u.ID, service.EventSessionsRevokedOthers))

	n, err = a.auth.RevokeOtherSessions(a.ctx, first)
	require.NoError(t, err)
	require.Zero(t, n)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2`,
		u.ID, service.EventSessionsRevokedOthers), "tidak ada yang dicabut = tidak ada event baru")
}

func TestSessionRevocationIsAtomicWhenTheActivityCannotBeWritten(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	_, first := a.loginIdentity(u)
	res2, second := a.extraSession(u)
	a.audit.failWith = errors.New("audit tidak bisa menulis")

	err := a.auth.RevokeSession(a.ctx, first, second.SessionID)
	require.ErrorIs(t, err, kernel.ErrTxRollbackOnly)
	_, err = a.auth.RevokeOtherSessions(a.ctx, first)
	require.ErrorIs(t, err, kernel.ErrTxRollbackOnly)

	_, err = a.svc.Authenticate(a.ctx, res2.Issued.Token)
	require.NoError(t, err, "pencabutan yang di-rollback tidak boleh mematikan sesi")
	require.Zero(t, a.count(`SELECT count(*) FROM security_events WHERE event_type IN ($1, $2)`,
		service.EventSessionRevoked, service.EventSessionsRevokedOthers))
}
