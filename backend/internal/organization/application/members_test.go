package application_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/organization/application"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func TestMemberServiceRemoveRunsEveryStepInOrderWithTheLockFirst(t *testing.T) {
	e := newMemberEnv(t, true)

	got, err := e.svc.Remove(bg, platformCaller(), e.orgID, e.userID)
	require.NoError(t, err)
	require.Equal(t, domain.MemberRemoved, got.Status)
	require.Equal(t, []string{
		e.lockStep(),
		"Org.Get",
		"Member.Remove",
		"Member.RemoveFromGroups",
		"Revoke",
		e.invalidateStep(),
		"Audit:organization.member.remove",
		"Event:organization.member.removed",
	}, e.trace.snapshot())
	require.Equal(t, 1, e.tx.entered(), "satu transaksi untuk seluruh akibat penghapusan")
}

func TestMemberServiceRemoveWithoutCacheSkipsInvalidationButKeepsTheOrder(t *testing.T) {
	e := newMemberEnv(t, false)

	_, err := e.svc.Remove(bg, platformCaller(), e.orgID, e.userID)
	require.NoError(t, err)
	require.Equal(t, []string{
		e.lockStep(),
		"Org.Get",
		"Member.Remove",
		"Member.RemoveFromGroups",
		"Revoke",
		"Audit:organization.member.remove",
		"Event:organization.member.removed",
	}, e.trace.snapshot())
}

func TestMemberServiceAddRunsEveryStepInOrderWithTheLockFirst(t *testing.T) {
	e := newMemberEnv(t, true)

	got, err := e.svc.Add(bg, platformCaller(), e.orgID, e.userID)
	require.NoError(t, err)
	require.Equal(t, domain.MemberActive, got.Status)
	require.Equal(t, []string{
		e.lockStep(),
		"Org.Get",
		"Member.Add",
		e.invalidateStep(),
		"Audit:organization.member.add",
		"Event:organization.member.added",
	}, e.trace.snapshot())
	require.Equal(t, 1, e.tx.entered())
}

func TestMemberServiceAddIsRejectedForTenantCallersBeforeAnyTransaction(t *testing.T) {
	e := newMemberEnv(t, true)

	_, err := e.svc.Add(bg, tenantCaller(t, e.orgID), e.orgID, e.userID)
	require.ErrorIs(t, err, domain.ErrPlatformRequired)
	require.Empty(t, e.trace.snapshot(), "tidak ada kunci, kueri, audit, atau event")
	require.Equal(t, 0, e.tx.entered())
}

func TestMemberServiceRemoveOfAnotherOrganizationIsRejectedBeforeAnyLockOrQuery(t *testing.T) {
	e := newMemberEnv(t, true)
	other := id.NewID()

	_, err := e.svc.Remove(bg, tenantCaller(t, other), e.orgID, e.userID)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound)
	require.Empty(t, e.trace.snapshot(), "tenant lain tidak meraba kunci organisasi ini")
	require.Equal(t, 0, e.tx.entered())
}

func TestMemberServiceRemoveOfSelfIsRejectedForTenantCallers(t *testing.T) {
	e := newMemberEnv(t, true)
	caller := tenantCaller(t, e.orgID)

	_, err := e.svc.Remove(bg, caller, e.orgID, caller.UserID)
	require.ErrorIs(t, err, domain.ErrCannotRemoveSelf)
	require.Empty(t, e.trace.snapshot())
	require.Equal(t, 0, e.tx.entered())
}

func TestMemberServiceTenantMayRemoveAnotherMemberOfItsOwnOrganization(t *testing.T) {
	e := newMemberEnv(t, true)

	_, err := e.svc.Remove(bg, tenantCaller(t, e.orgID), e.orgID, e.userID)
	require.NoError(t, err)
	require.NotEmpty(t, e.store.access)
	for _, a := range e.store.access {
		tenant, ok := a.Tenant()
		require.True(t, ok, "akses tenant, bukan platform")
		require.Equal(t, e.orgID, tenant)
	}
}

func TestMemberServicePlatformCallerIsNotBlockedFromRemovingItself(t *testing.T) {
	e := newMemberEnv(t, true)
	caller := platformCaller()

	_, err := e.svc.Remove(bg, caller, e.orgID, caller.UserID)
	require.NoError(t, err, "pembatasan hapus-diri hanya untuk pemanggil tenant")
}

func TestMemberServiceWritesAreRejectedForAnArchivedOrganization(t *testing.T) {
	e := newMemberEnv(t, true)
	e.orgs.rec.Status = domain.StatusArchived

	_, err := e.svc.Add(bg, platformCaller(), e.orgID, e.userID)
	require.ErrorIs(t, err, domain.ErrOrganizationArchived)
	_, err = e.svc.Remove(bg, platformCaller(), e.orgID, e.userID)
	require.ErrorIs(t, err, domain.ErrOrganizationArchived)

	require.Equal(t, []string{e.lockStep(), "Org.Get", e.lockStep(), "Org.Get"}, e.trace.snapshot(), "berhenti sebelum menulis apa pun")
	require.Equal(t, 0, e.audit.count())
	require.Equal(t, 0, e.events.count())
}

func TestMemberServiceTreatsADeletedAtStampAsArchivedToo(t *testing.T) {
	e := newMemberEnv(t, true)
	stamp := e.orgs.rec.CreatedAt
	e.orgs.rec.ArchivedAt = &stamp

	_, err := e.svc.Remove(bg, platformCaller(), e.orgID, e.userID)
	require.ErrorIs(t, err, domain.ErrOrganizationArchived)
	require.Equal(t, []string{e.lockStep(), "Org.Get"}, e.trace.snapshot())
}

func TestMemberServiceAddToASuspendedOrganizationIsAllowed(t *testing.T) {
	e := newMemberEnv(t, true)
	e.orgs.rec.Status = domain.StatusSuspended

	_, err := e.svc.Add(bg, platformCaller(), e.orgID, e.userID)
	require.NoError(t, err)
	require.Equal(t, 1, e.events.count())
}

func TestMemberServiceRemoveStopsAtTheFirstFailingStep(t *testing.T) {
	type failure struct {
		step string
		want func(e *memberEnv) []string
	}
	cases := []failure{
		{"lock", func(e *memberEnv) []string { return []string{e.lockStep()} }},
		{"org", func(e *memberEnv) []string { return []string{e.lockStep(), "Org.Get"} }},
		{"remove", func(e *memberEnv) []string { return []string{e.lockStep(), "Org.Get", "Member.Remove"} }},
		{"groups", func(e *memberEnv) []string {
			return []string{e.lockStep(), "Org.Get", "Member.Remove", "Member.RemoveFromGroups"}
		}},
		{"revoke", func(e *memberEnv) []string {
			return []string{e.lockStep(), "Org.Get", "Member.Remove", "Member.RemoveFromGroups", "Revoke"}
		}},
		{"invalidate", func(e *memberEnv) []string {
			return []string{e.lockStep(), "Org.Get", "Member.Remove", "Member.RemoveFromGroups", "Revoke", e.invalidateStep()}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.step, func(t *testing.T) {
			e := newMemberEnv(t, true)
			boom := errors.New("boom " + tc.step)
			switch tc.step {
			case "lock":
				e.locker.err = boom
			case "org":
				e.orgs.err = boom
			case "remove":
				e.store.removeErr = boom
			case "groups":
				e.store.groupsErr = boom
			case "revoke":
				e.revoker.err = boom
			case "invalidate":
				e.inv.err = boom
			}

			_, err := e.svc.Remove(bg, platformCaller(), e.orgID, e.userID)
			require.ErrorIs(t, err, boom)
			require.Equal(t, tc.want(e), e.trace.snapshot())
			require.Equal(t, 0, e.audit.count(), "tidak ada audit untuk operasi yang gagal")
			require.Equal(t, 0, e.events.count(), "tidak ada event untuk operasi yang gagal")
		})
	}
}

func TestMemberServiceAddStopsAtTheFirstFailingStep(t *testing.T) {
	type failure struct {
		step string
		want func(e *memberEnv) []string
	}
	cases := []failure{
		{"lock", func(e *memberEnv) []string { return []string{e.lockStep()} }},
		{"org", func(e *memberEnv) []string { return []string{e.lockStep(), "Org.Get"} }},
		{"add", func(e *memberEnv) []string { return []string{e.lockStep(), "Org.Get", "Member.Add"} }},
		{"invalidate", func(e *memberEnv) []string {
			return []string{e.lockStep(), "Org.Get", "Member.Add", e.invalidateStep()}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.step, func(t *testing.T) {
			e := newMemberEnv(t, true)
			boom := errors.New("boom " + tc.step)
			switch tc.step {
			case "lock":
				e.locker.err = boom
			case "org":
				e.orgs.err = boom
			case "add":
				e.store.addErr = boom
			case "invalidate":
				e.inv.err = boom
			}

			_, err := e.svc.Add(bg, platformCaller(), e.orgID, e.userID)
			require.ErrorIs(t, err, boom)
			require.Equal(t, tc.want(e), e.trace.snapshot())
			require.Equal(t, 0, e.audit.count())
			require.Equal(t, 0, e.events.count())
		})
	}
}

func TestMemberServiceDomainErrorsFromTheStoreReachTheCallerUnchanged(t *testing.T) {
	e := newMemberEnv(t, true)
	e.store.removeErr = domain.ErrMemberNotFound
	_, err := e.svc.Remove(bg, platformCaller(), e.orgID, e.userID)
	require.ErrorIs(t, err, domain.ErrMemberNotFound)

	e.store.addErr = domain.ErrMemberExists
	_, err = e.svc.Add(bg, platformCaller(), e.orgID, e.userID)
	require.ErrorIs(t, err, domain.ErrMemberExists)

	e.store.addErr = domain.ErrUserNotEligible
	_, err = e.svc.Add(bg, platformCaller(), e.orgID, e.userID)
	require.ErrorIs(t, err, domain.ErrUserNotEligible)

	require.Equal(t, 0, e.audit.count())
	require.Equal(t, 0, e.events.count())
}

func TestMemberServiceRemoveAuditAndEventCarryIdsAndCountsButNoPersonalData(t *testing.T) {
	e := newMemberEnv(t, true)
	e.store.groups = 2
	e.revoker.n = 3
	caller := platformCaller()

	_, err := e.svc.Remove(bg, caller, e.orgID, e.userID)
	require.NoError(t, err)

	require.Len(t, e.audit.entries, 1)
	entry := e.audit.entries[0]
	require.Equal(t, "organization.member.remove", entry.Action)
	require.Equal(t, "organization_membership", entry.ResourceType)
	require.Equal(t, e.store.member.ID, entry.ResourceID)
	require.Equal(t, map[string]any{"user_id": e.userID.String(), "groups_removed": 2, "assignments_revoked": 3}, entry.Metadata)
	require.Equal(t, e.orgID, e.audit.actors[0].OrganizationID, "audit tercatat di organisasi sasaran")
	require.Equal(t, caller.UserID, e.audit.actors[0].UserID)

	require.Len(t, e.events.events, 1)
	ev := e.events.events[0]
	require.Equal(t, "organization.member.removed", ev.Name)
	require.Equal(t, e.orgID, ev.OrganizationID)
	require.Equal(t, "organization_membership", ev.AggregateType)
	require.Equal(t, e.store.member.ID, ev.AggregateID)
	p := payloadOf(t, ev)
	require.Equal(t, e.orgID.String(), p["organization_id"])
	require.Equal(t, caller.UserID.String(), p["actor_user_id"])
	require.Equal(t, e.userID.String(), p["user_id"])
	require.EqualValues(t, 2, p["groups_removed"])
	require.EqualValues(t, 3, p["assignments_revoked"])
	require.NotContains(t, p, "email")
}

func TestMemberServiceAddAuditAndEventCarryTheNewStatus(t *testing.T) {
	e := newMemberEnv(t, true)
	caller := platformCaller()

	_, err := e.svc.Add(bg, caller, e.orgID, e.userID)
	require.NoError(t, err)

	require.Len(t, e.audit.entries, 1)
	require.Equal(t, "organization.member.add", e.audit.entries[0].Action)
	require.Equal(t, map[string]any{"user_id": e.userID.String(), "status": domain.MemberActive}, e.audit.entries[0].Metadata)
	require.Equal(t, e.orgID, e.audit.actors[0].OrganizationID)

	require.Len(t, e.events.events, 1)
	require.Equal(t, "organization.member.added", e.events.events[0].Name)
	p := payloadOf(t, e.events.events[0])
	require.Equal(t, e.userID.String(), p["user_id"])
	require.Equal(t, domain.MemberActive, p["status"])
	require.Equal(t, caller.UserID.String(), p["actor_user_id"])
}

func TestMemberServiceRejectsCallersWithoutAUserAndNilIdsBeforeAnyTransaction(t *testing.T) {
	e := newMemberEnv(t, true)
	noUser := application.Caller{}

	_, err := e.svc.Get(bg, noUser, e.orgID, e.userID)
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	_, err = e.svc.List(bg, noUser, e.orgID, domain.MemberListQuery{})
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	_, err = e.svc.Add(bg, noUser, e.orgID, e.userID)
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	_, err = e.svc.Remove(bg, noUser, e.orgID, e.userID)
	require.ErrorIs(t, err, domain.ErrInvalidAccess)

	_, err = e.svc.Add(bg, platformCaller(), uuid.Nil, e.userID)
	require.ErrorIs(t, err, domain.ErrInvalidID)
	_, err = e.svc.Add(bg, platformCaller(), e.orgID, uuid.Nil)
	require.ErrorIs(t, err, domain.ErrInvalidID)
	_, err = e.svc.Remove(bg, platformCaller(), uuid.Nil, e.userID)
	require.ErrorIs(t, err, domain.ErrInvalidID)
	_, err = e.svc.Remove(bg, platformCaller(), e.orgID, uuid.Nil)
	require.ErrorIs(t, err, domain.ErrInvalidID)

	require.Empty(t, e.trace.snapshot())
	require.Equal(t, 0, e.tx.entered())
}

func TestMemberServiceReadsUseTheCallerAccessAndPropagateStoreErrors(t *testing.T) {
	e := newMemberEnv(t, true)

	_, err := e.svc.Get(bg, platformCaller(), e.orgID, e.userID)
	require.NoError(t, err)
	_, err = e.svc.List(bg, tenantCaller(t, e.orgID), e.orgID, domain.MemberListQuery{})
	require.NoError(t, err)
	require.Len(t, e.store.access, 2)
	require.True(t, e.store.access[0].IsPlatform())
	tenant, ok := e.store.access[1].Tenant()
	require.True(t, ok)
	require.Equal(t, e.orgID, tenant)
	require.Equal(t, 0, e.tx.entered(), "pembacaan tidak membuka transaksi")

	e.store.getErr = domain.ErrMemberNotFound
	_, err = e.svc.Get(bg, platformCaller(), e.orgID, e.userID)
	require.ErrorIs(t, err, domain.ErrMemberNotFound)
}

func TestNewMemberServiceRequiresItsDependencies(t *testing.T) {
	e := newMemberEnv(t, true)

	_, err := application.NewMemberService(nil, e.store, e.orgs, e.revoker, e.locker, e.audit, e.events, nil)
	require.Error(t, err)
	_, err = application.NewMemberService(e.tx, nil, e.orgs, e.revoker, e.locker, e.audit, e.events, nil)
	require.Error(t, err)
	_, err = application.NewMemberService(e.tx, e.store, nil, e.revoker, e.locker, e.audit, e.events, nil)
	require.Error(t, err)
	_, err = application.NewMemberService(e.tx, e.store, e.orgs, nil, e.locker, e.audit, e.events, nil)
	require.Error(t, err)
	_, err = application.NewMemberService(e.tx, e.store, e.orgs, e.revoker, nil, e.audit, e.events, nil)
	require.Error(t, err)
	_, err = application.NewMemberService(e.tx, e.store, e.orgs, e.revoker, e.locker, nil, e.events, nil)
	require.Error(t, err)
	_, err = application.NewMemberService(e.tx, e.store, e.orgs, e.revoker, e.locker, e.audit, nil, nil)
	require.Error(t, err)

	svc, err := application.NewMemberService(e.tx, e.store, e.orgs, e.revoker, e.locker, e.audit, e.events, nil)
	require.NoError(t, err, "invalidator boleh nil (tanpa Redis)")
	require.NotNil(t, svc)
}
