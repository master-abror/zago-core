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

// Tes Refresh dan SwitchOrganization terhadap PostgreSQL+Redis sungguhan. Rotasi dasar (token
// lama mati, batas absolut dipertahankan, race) sudah diuji di session_test; yang diuji di sini
// adalah use case di atasnya: event keamanan, activity, keanggotaan, dan atomisitas.

func (a *authHarness) addMembership(u testkit.User, o testkit.Organization, status string) {
	a.t.Helper()
	_, err := a.db.Migrator.Exec(a.ctx,
		`INSERT INTO organization_memberships (id, organization_id, user_id, status, joined_at) VALUES ($1, $2, $3, $4, now())`,
		id.NewID(), o.ID, u.ID, status)
	require.NoError(a.t, err)
}

// loginIdentity login lalu mengembalikan hasil login dan Identity yang akan dilihat handler.
func (a *authHarness) loginIdentity(u testkit.User) (service.LoginResult, domain.Identity) {
	a.t.Helper()
	res, err := a.login(u.Email, goodPassword)
	require.NoError(a.t, err)
	ident, err := a.svc.Authenticate(a.ctx, res.Issued.Token)
	require.NoError(a.t, err)
	return res, *ident
}

func (a *authHarness) requireDead(token string) {
	a.t.Helper()
	_, err := a.svc.Authenticate(a.ctx, token)
	require.ErrorIs(a.t, err, domain.ErrUnauthenticated)
}

func TestRefreshRotatesTheTokenKeepsTheAbsoluteLimitAndTheOrganization(t *testing.T) {
	a := newAuthHarness(t)
	org := testkit.NewOrganization(t, a.db)
	u := a.newUserWithPassword(goodPassword, testkit.InOrganization(org))
	res, ident := a.loginIdentity(u)

	iss, err := a.auth.Refresh(a.ctx, ident)

	require.NoError(t, err)
	require.NotEqual(t, res.Issued.Token, iss.Token)
	require.NotEqual(t, res.Issued.Session.ID, iss.Session.ID)
	a.requireDead(res.Issued.Token)
	fresh, err := a.svc.Authenticate(a.ctx, iss.Token)
	require.NoError(t, err)
	require.Equal(t, u.ID, fresh.UserID)
	require.Equal(t, iss.Session.ID, fresh.SessionID)
	require.NotNil(t, fresh.OrganizationID)
	require.Equal(t, org.ID, *fresh.OrganizationID, "refresh tidak mengubah organisasi")
	require.True(t, iss.Session.ExpiresAt.Equal(res.Issued.Session.ExpiresAt), "refresh tidak memperpanjang batas absolut")

	require.Equal(t, 2, a.sessionCount(u))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, u.ID))
	require.Equal(t, 1, a.count(
		`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2 AND session_id = $3 AND metadata->>'previous_session_id' = $4`,
		u.ID, service.EventSessionRefreshed, iss.Session.ID, res.Issued.Session.ID.String()))
	require.Equal(t, 1, a.audit.count(), "refresh tidak menulis activity (hanya login tadi)")
}

func TestRefreshRejectsRevokedAndForeignSessionsWithoutSideEffects(t *testing.T) {
	a := newAuthHarness(t)
	victim := a.newUserWithPassword(goodPassword)
	attacker := testkit.NewUser(t, a.db)
	res, ident := a.loginIdentity(victim)

	forged := domain.Identity{UserID: attacker.ID, SessionID: ident.SessionID, AuthLevel: domain.AuthLevelPassword}
	_, err := a.auth.Refresh(a.ctx, forged)
	require.ErrorIs(t, err, domain.ErrUnauthenticated)
	_, err = a.svc.Authenticate(a.ctx, res.Issued.Token)
	require.NoError(t, err, "sesi korban tidak boleh tersentuh")

	require.NoError(t, a.auth.Logout(a.ctx, ident))
	_, err = a.auth.Refresh(a.ctx, ident)
	require.ErrorIs(t, err, domain.ErrUnauthenticated, "sesi yang sudah dicabut tidak bisa di-refresh")

	require.Equal(t, 1, a.sessionCount(victim), "tidak ada sesi baru yang lahir")
	require.Zero(t, a.count(`SELECT count(*) FROM security_events WHERE event_type = $1`, service.EventSessionRefreshed))
}

func TestSwitchOrganizationRotatesTheTokenAndRecordsEventAndActivity(t *testing.T) {
	a := newAuthHarness(t)
	orgA := testkit.NewOrganization(t, a.db)
	orgB := testkit.NewOrganization(t, a.db)
	u := a.newUserWithPassword(goodPassword, testkit.InOrganization(orgA))
	a.addMembership(u, orgB, "active")
	res, ident := a.loginIdentity(u)
	require.NotNil(t, ident.OrganizationID)
	require.Equal(t, orgA.ID, *ident.OrganizationID, "prasyarat: organisasi awal adalah keanggotaan tertua")

	iss, err := a.auth.SwitchOrganization(a.ctx, ident, orgB.ID)

	require.NoError(t, err)
	a.requireDead(res.Issued.Token)
	fresh, err := a.svc.Authenticate(a.ctx, iss.Token)
	require.NoError(t, err)
	require.NotNil(t, fresh.OrganizationID)
	require.Equal(t, orgB.ID, *fresh.OrganizationID)
	require.True(t, iss.Session.ExpiresAt.Equal(res.Issued.Session.ExpiresAt), "pindah organisasi tidak memperpanjang batas absolut")
	require.Equal(t, 1, a.count(
		`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2 AND organization_id = $3 AND session_id = $4
		   AND metadata->>'from_organization_id' = $5`,
		u.ID, service.EventOrganizationSwitched, orgB.ID, iss.Session.ID, orgA.ID.String()))

	require.Equal(t, 2, a.audit.count())
	last := len(a.audit.entries) - 1
	require.Equal(t, "auth.switch_organization", a.audit.entries[last].Action)
	require.Equal(t, iss.Session.ID, a.audit.entries[last].ResourceID)
	require.Equal(t, u.ID, a.audit.actors[last].UserID)
	require.Equal(t, orgB.ID, a.audit.actors[last].OrganizationID, "activity dicatat di organisasi tujuan")
}

func TestSwitchOrganizationRejectsNonMembersWithoutAnySideEffect(t *testing.T) {
	a := newAuthHarness(t)
	home := testkit.NewOrganization(t, a.db)
	u := a.newUserWithPassword(goodPassword, testkit.InOrganization(home))
	res, ident := a.loginIdentity(u)

	strangerOrg := testkit.NewOrganization(t, a.db)
	invited := testkit.NewOrganization(t, a.db)
	a.addMembership(u, invited, "invited")
	suspendedMember := testkit.NewOrganization(t, a.db)
	a.addMembership(u, suspendedMember, "suspended")
	suspendedOrg := testkit.NewOrganization(t, a.db)
	a.addMembership(u, suspendedOrg, "active")
	_, err := a.db.Migrator.Exec(a.ctx, `UPDATE organizations SET status = 'suspended' WHERE id = $1`, suspendedOrg.ID)
	require.NoError(t, err)

	targets := map[string]uuid.UUID{
		"organisasi tanpa keanggotaan": strangerOrg.ID,
		"organisasi yang tidak ada":    uuid.New(),
		"keanggotaan masih invited":    invited.ID,
		"keanggotaan suspended":        suspendedMember.ID,
		"organisasi suspended":         suspendedOrg.ID,
	}
	for name, target := range targets {
		_, err := a.auth.SwitchOrganization(a.ctx, ident, target)
		require.ErrorIs(t, err, domain.ErrNotOrganizationMember, name)
	}

	_, err = a.svc.Authenticate(a.ctx, res.Issued.Token)
	require.NoError(t, err, "penolakan tidak boleh mencabut sesi")
	require.Equal(t, 1, a.sessionCount(u))
	require.Zero(t, a.count(`SELECT count(*) FROM security_events WHERE event_type = $1`, service.EventOrganizationSwitched))
	require.Equal(t, 1, a.audit.count(), "hanya activity login")
}

func TestSwitchOrganizationToTheCurrentOrganizationStillRotates(t *testing.T) {
	a := newAuthHarness(t)
	org := testkit.NewOrganization(t, a.db)
	u := a.newUserWithPassword(goodPassword, testkit.InOrganization(org))
	res, ident := a.loginIdentity(u)

	iss, err := a.auth.SwitchOrganization(a.ctx, ident, org.ID)

	require.NoError(t, err)
	a.requireDead(res.Issued.Token)
	fresh, err := a.svc.Authenticate(a.ctx, iss.Token)
	require.NoError(t, err)
	require.Equal(t, org.ID, *fresh.OrganizationID)
}

func TestSwitchOrganizationIsAtomicWhenTheActivityCannotBeWritten(t *testing.T) {
	a := newAuthHarness(t)
	orgA := testkit.NewOrganization(t, a.db)
	orgB := testkit.NewOrganization(t, a.db)
	u := a.newUserWithPassword(goodPassword, testkit.InOrganization(orgA))
	a.addMembership(u, orgB, "active")
	res, ident := a.loginIdentity(u)
	a.audit.failWith = errors.New("audit tidak bisa menulis")

	_, err := a.auth.SwitchOrganization(a.ctx, ident, orgB.ID)

	require.ErrorIs(t, err, kernel.ErrTxRollbackOnly)
	cur, err := a.svc.Authenticate(a.ctx, res.Issued.Token)
	require.NoError(t, err, "sesi lama tetap hidup")
	require.Equal(t, orgA.ID, *cur.OrganizationID, "dan tetap di organisasi lama")
	require.Equal(t, 1, a.sessionCount(u), "tidak ada sesi baru")
	require.Zero(t, a.count(`SELECT count(*) FROM security_events WHERE event_type = $1`, service.EventOrganizationSwitched))
}
