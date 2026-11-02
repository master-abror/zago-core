package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	idgen "github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes jalur platform pada SwitchOrganization (ADR-0022 bagian 2): identitas tanpa keanggotaan
// masuk organisasi `active` bila PlatformEntryChecker mengizinkan. PlatformEntryChecker diganti
// tiruan di sini; sambungan ke engine sungguhan dibuktikan di paket authorization.

type fakePlatformEntry struct {
	allow bool
	err   error
	calls []uuid.UUID
}

func (f *fakePlatformEntry) CanEnterOrganizations(_ context.Context, userID uuid.UUID) (bool, error) {
	f.calls = append(f.calls, userID)
	return f.allow, f.err
}

// authWith membangun AuthService lain di atas penyimpanan dan perekam audit harness yang sama,
// dengan PlatformEntryChecker yang diberikan.
func (a *authHarness) authWith(chk service.PlatformEntryChecker) *service.AuthService {
	a.t.Helper()
	as, err := service.NewAuthService(service.AuthDeps{
		Store: a.store, Tx: a.tm, Sessions: a.svc, Hasher: a.spy, Audit: a.audit,
		Bus: kernel.NewBus(a.db.App, nil), PlatformEntry: chk,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	require.NoError(a.t, err)
	return as
}

func TestSwitchOrganizationLetsAPlatformIdentityInAndMarksViaPlatform(t *testing.T) {
	a := newAuthHarness(t)
	org := testkit.NewOrganization(t, a.db)
	u := a.newUserWithPassword(goodPassword)
	res, ident := a.loginIdentity(u)
	require.Nil(t, ident.OrganizationID, "prasyarat: identitas platform tidak punya organisasi")
	chk := &fakePlatformEntry{allow: true}

	iss, err := a.authWith(chk).SwitchOrganization(a.ctx, ident, org.ID)

	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{u.ID}, chk.calls)
	a.requireDead(res.Issued.Token)
	fresh, err := a.svc.Authenticate(a.ctx, iss.Token)
	require.NoError(t, err)
	require.NotNil(t, fresh.OrganizationID)
	require.Equal(t, org.ID, *fresh.OrganizationID)
	require.Equal(t, 1, a.count(
		`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2 AND organization_id = $3
		   AND session_id = $4 AND metadata->>'via' = 'platform' AND metadata->>'from_organization_id' IS NULL`,
		u.ID, service.EventOrganizationSwitched, org.ID, iss.Session.ID))
	require.Zero(t, a.count(`SELECT count(*) FROM organization_memberships WHERE user_id = $1`, u.ID),
		"masuk lewat platform tidak membuat keanggotaan")

	require.Equal(t, 2, a.audit.count())
	last := len(a.audit.entries) - 1
	require.Equal(t, "auth.switch_organization", a.audit.entries[last].Action)
	require.Equal(t, "platform", a.audit.entries[last].Metadata["via"])
	require.Equal(t, org.ID, a.audit.actors[last].OrganizationID, "activity dicatat di organisasi tujuan")
}

func TestSwitchOrganizationPlatformPathRejectsEverythingUniformlyWithoutSideEffects(t *testing.T) {
	a := newAuthHarness(t)
	u := a.newUserWithPassword(goodPassword)
	res, ident := a.loginIdentity(u)

	active := testkit.NewOrganization(t, a.db)
	suspended := testkit.NewOrganization(t, a.db)
	archived := testkit.NewOrganization(t, a.db)
	deleted := testkit.NewOrganization(t, a.db)
	for _, q := range []struct {
		sql string
		id  uuid.UUID
	}{
		{`UPDATE organizations SET status = 'suspended' WHERE id = $1`, suspended.ID},
		{`UPDATE organizations SET status = 'archived' WHERE id = $1`, archived.ID},
		{`UPDATE organizations SET deleted_at = now() WHERE id = $1`, deleted.ID},
	} {
		_, err := a.db.Migrator.Exec(a.ctx, q.sql, q.id)
		require.NoError(t, err)
	}

	// Izin platform ada, tetapi organisasi tidak layak dimasuki.
	allow := a.authWith(&fakePlatformEntry{allow: true})
	for _, tc := range []struct {
		name   string
		target uuid.UUID
	}{
		{"organisasi suspended", suspended.ID},
		{"organisasi archived", archived.ID},
		{"organisasi terhapus", deleted.ID},
		{"organisasi yang tidak ada", idgen.NewID()},
	} {
		_, err := allow.SwitchOrganization(a.ctx, ident, tc.target)
		require.ErrorIs(t, err, domain.ErrNotOrganizationMember, tc.name)
	}

	// Organisasi layak, tetapi engine tidak mengizinkan.
	_, err := a.authWith(&fakePlatformEntry{allow: false}).SwitchOrganization(a.ctx, ident, active.ID)
	require.ErrorIs(t, err, domain.ErrNotOrganizationMember, "tanpa izin platform")

	// Tanpa PlatformEntryChecker sama sekali: fail-closed.
	_, err = a.auth.SwitchOrganization(a.ctx, ident, active.ID)
	require.ErrorIs(t, err, domain.ErrNotOrganizationMember, "tanpa checker")

	_, err = a.svc.Authenticate(a.ctx, res.Issued.Token)
	require.NoError(t, err, "penolakan tidak boleh mencabut sesi")
	require.Equal(t, 1, a.sessionCount(u))
	require.Zero(t, a.count(`SELECT count(*) FROM security_events WHERE event_type = $1`, service.EventOrganizationSwitched))
	require.Equal(t, 1, a.audit.count(), "hanya activity login")
}

func TestSwitchOrganizationPlatformCheckerFailureIsAnErrorNotAnAllowNorADenial(t *testing.T) {
	a := newAuthHarness(t)
	org := testkit.NewOrganization(t, a.db)
	u := a.newUserWithPassword(goodPassword)
	res, ident := a.loginIdentity(u)
	boom := errors.New("engine tidak dapat memutuskan")

	_, err := a.authWith(&fakePlatformEntry{allow: true, err: boom}).SwitchOrganization(a.ctx, ident, org.ID)

	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, domain.ErrNotOrganizationMember, "galat bukan penolakan seragam")
	_, err = a.svc.Authenticate(a.ctx, res.Issued.Token)
	require.NoError(t, err, "sesi lama tetap hidup")
	require.Equal(t, 1, a.sessionCount(u))
	require.Zero(t, a.count(`SELECT count(*) FROM security_events WHERE event_type = $1`, service.EventOrganizationSwitched))
}

func TestSwitchOrganizationMemberPathNeverConsultsThePlatformChecker(t *testing.T) {
	a := newAuthHarness(t)
	orgA := testkit.NewOrganization(t, a.db)
	orgB := testkit.NewOrganization(t, a.db)
	u := a.newUserWithPassword(goodPassword, testkit.InOrganization(orgA))
	a.addMembership(u, orgB, "active")
	_, ident := a.loginIdentity(u)
	chk := &fakePlatformEntry{allow: true}

	_, err := a.authWith(chk).SwitchOrganization(a.ctx, ident, orgB.ID)

	require.NoError(t, err)
	require.Empty(t, chk.calls, "anggota tidak melewati jalur platform")
	require.Zero(t, a.count(`SELECT count(*) FROM security_events WHERE user_id = $1 AND metadata->>'via' IS NOT NULL`, u.ID))
	last := len(a.audit.entries) - 1
	require.Nil(t, a.audit.entries[last].Metadata, "jalur anggota tidak berubah")
}

func TestSwitchOrganizationNonMemberWithoutPlatformRightGetsTheSameWorkForAnyTarget(t *testing.T) {
	a := newAuthHarness(t)
	home := testkit.NewOrganization(t, a.db)
	stranger := testkit.NewOrganization(t, a.db)
	u := a.newUserWithPassword(goodPassword, testkit.InOrganization(home))
	_, ident := a.loginIdentity(u)
	chk := &fakePlatformEntry{allow: false}
	svc := a.authWith(chk)

	_, err := svc.SwitchOrganization(a.ctx, ident, stranger.ID)
	require.ErrorIs(t, err, domain.ErrNotOrganizationMember)
	_, err = svc.SwitchOrganization(a.ctx, ident, idgen.NewID())
	require.ErrorIs(t, err, domain.ErrNotOrganizationMember)

	require.Equal(t, []uuid.UUID{u.ID, u.ID}, chk.calls,
		"organisasi yang ada dan yang tidak ada diperlakukan sama: engine ditanya untuk keduanya")
}

func TestSwitchOrganizationPlatformIdentityAlreadyInsideAnOrganizationCanEnterAnother(t *testing.T) {
	a := newAuthHarness(t)
	org1 := testkit.NewOrganization(t, a.db)
	org2 := testkit.NewOrganization(t, a.db)
	u := a.newUserWithPassword(goodPassword)
	_, ident := a.loginIdentity(u)
	chk := &fakePlatformEntry{allow: true}
	svc := a.authWith(chk)

	iss1, err := svc.SwitchOrganization(a.ctx, ident, org1.ID)
	require.NoError(t, err)
	inside, err := a.svc.Authenticate(a.ctx, iss1.Token)
	require.NoError(t, err)
	require.NotNil(t, inside.OrganizationID, "prasyarat: sesi sekarang berada di organisasi")

	iss2, err := svc.SwitchOrganization(a.ctx, *inside, org2.ID)

	require.NoError(t, err)
	require.Equal(t, []uuid.UUID{u.ID, u.ID}, chk.calls, "checker hanya menerima user id, bukan organisasi sesi")
	require.Equal(t, 1, a.count(
		`SELECT count(*) FROM security_events WHERE user_id = $1 AND event_type = $2 AND organization_id = $3
		   AND session_id = $4 AND metadata->>'via' = 'platform' AND metadata->>'from_organization_id' = $5`,
		u.ID, service.EventOrganizationSwitched, org2.ID, iss2.Session.ID, org1.ID.String()))
}
