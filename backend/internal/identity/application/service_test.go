package application_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/identity/application"
	"github.com/master-abror/zago-core/backend/internal/identity/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	orgdomain "github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

func TestNewServiceRequiresEveryDependency(t *testing.T) {
	e := newEnv(t)
	_, err := application.NewService(nil, e.store, e.audit, e.events)
	require.Error(t, err)
	_, err = application.NewService(e.tx, nil, e.audit, e.events)
	require.Error(t, err)
	_, err = application.NewService(e.tx, e.store, nil, e.events)
	require.Error(t, err)
	_, err = application.NewService(e.tx, e.store, e.audit, nil)
	require.Error(t, err)
}

func TestGetProfileReadsOnlyTheCallersOwnRow(t *testing.T) {
	e := newEnv(t)
	caller := tenantCaller(t, id.NewID())
	e.store.current = storedRecord(caller.UserID)

	rec, err := e.svc.GetProfile(bg, caller)
	require.NoError(t, err)
	require.Equal(t, caller.UserID, rec.ID)
	require.Equal(t, []string{"GetSelf"}, e.trace.snapshot())
	require.Equal(t, caller.UserID, e.store.userIDs[0], "id berasal dari pemanggil")
	require.Empty(t, e.store.access, "profil milik sendiri tidak memakai Access")
}

func TestGetUserAndListUsersPassTheCallersAccess(t *testing.T) {
	e := newEnv(t)
	org := id.NewID()
	tenant := tenantCaller(t, org)
	plat := platformCaller()
	target := id.NewID()

	_, err := e.svc.GetUser(bg, tenant, target)
	require.NoError(t, err)
	_, err = e.svc.GetUser(bg, plat, target)
	require.NoError(t, err)
	_, err = e.svc.ListUsers(bg, tenant, domain.ListQuery{Filter: domain.FilterAll})
	require.NoError(t, err)
	require.Equal(t, domain.FilterAll, e.store.lastQuery.Filter, "kueri diteruskan apa adanya sampai repository")
	_, err = e.svc.ListUsers(bg, plat, domain.ListQuery{})
	require.NoError(t, err)

	require.Equal(t, []string{"Get", "Get", "List", "List"}, e.trace.snapshot())
	require.Len(t, e.store.access, 4)
	gotOrg, ok := e.store.access[0].Tenant()
	require.True(t, ok)
	require.Equal(t, org, gotOrg, "organisasi berasal dari scope pemanggil")
	require.False(t, e.store.access[0].IsPlatform())
	require.True(t, e.store.access[1].IsPlatform())
	_, ok = e.store.access[2].Tenant()
	require.True(t, ok)
	require.True(t, e.store.access[3].IsPlatform())
	require.Equal(t, target, e.store.userIDs[0])
}

func TestUpdateProfileWritesChangesWithAuditAndEventInOrder(t *testing.T) {
	e := newEnv(t)
	org := id.NewID()
	caller := tenantCaller(t, org)
	e.store.current = storedRecord(caller.UserID)
	updated := storedRecord(caller.UserID)
	updated.DisplayName = "Budi Baru"
	e.store.updated = updated

	in := domain.UpdateInput{DisplayName: strPtr("Budi Baru"), Phone: strPtr("+62 812 3456 7890")}
	out, err := e.svc.UpdateProfile(bg, caller, in)
	require.NoError(t, err)
	require.Equal(t, "Budi Baru", out.DisplayName, "yang dikembalikan adalah baris sesudah ubah")

	require.Equal(t, []string{"ForUpdateSelf", "UpdateSelf", "Audit:profile.update", "Event:user.updated"}, e.trace.snapshot())
	require.Equal(t, 1, e.tx.entered(), "satu transaksi")
	require.NotNil(t, e.store.lastUpdate.DisplayName)
	require.Equal(t, "Budi Baru", *e.store.lastUpdate.DisplayName)

	require.Equal(t, 1, e.audit.count())
	entry := e.audit.entries[0]
	require.Equal(t, "user", entry.ResourceType)
	require.Equal(t, caller.UserID, entry.ResourceID)
	require.Equal(t, []string{"display_name", "phone"}, entry.Metadata["changed_fields"])
	actor := e.audit.actors[0]
	require.Equal(t, caller.UserID, actor.UserID)
	require.Equal(t, kernel.ActorUser, actor.Type)
	require.Equal(t, org, actor.OrganizationID, "organisasi aktor = scope pemanggil")

	require.Equal(t, 1, e.events.count())
	ev := e.events.events[0]
	require.Equal(t, org, ev.OrganizationID)
	require.Equal(t, "user", ev.AggregateType)
	require.Equal(t, caller.UserID, ev.AggregateID)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(ev.Payload, &payload))
	require.Equal(t, caller.UserID.String(), payload["user_id"])
	require.Equal(t, caller.UserID.String(), payload["actor_user_id"])
	require.Equal(t, []any{"display_name", "phone"}, payload["changed_fields"])
}

func TestUpdateProfileAuditAndEventCarryFieldNamesNeverValues(t *testing.T) {
	e := newEnv(t)
	caller := tenantCaller(t, id.NewID())
	e.store.current = storedRecord(caller.UserID)
	e.store.updated = storedRecord(caller.UserID)

	in := domain.UpdateInput{DisplayName: strPtr("Nama Rahasia Baru"), Phone: strPtr("+62 812 7777 1111")}
	_, err := e.svc.UpdateProfile(bg, caller, in)
	require.NoError(t, err)

	meta, err := json.Marshal(e.audit.entries[0].Metadata)
	require.NoError(t, err)
	require.NotContains(t, string(meta), "812 7777", "nilai phone tidak masuk audit")
	require.NotContains(t, string(meta), "Rahasia", "nilai display_name tidak masuk audit")
	require.NotContains(t, string(e.events.events[0].Payload), "812 7777", "nilai phone tidak masuk event")
	require.NotContains(t, string(e.events.events[0].Payload), "Rahasia", "nilai display_name tidak masuk event")
}

func TestUpdateProfileForPlatformScopeUsesNoOrganization(t *testing.T) {
	e := newEnv(t)
	caller := platformCaller()
	e.store.current = storedRecord(caller.UserID)
	e.store.updated = storedRecord(caller.UserID)

	_, err := e.svc.UpdateProfile(bg, caller, domain.UpdateInput{Locale: strPtr("en")})
	require.NoError(t, err)
	require.Equal(t, caller.UserID, e.audit.actors[0].UserID)
	require.Equal(t, uuid.Nil, e.audit.actors[0].OrganizationID, "scope platform: organisasi aktor kosong")
	require.Equal(t, uuid.Nil, e.events.events[0].OrganizationID, "scope platform: organisasi event kosong")
}

func TestUpdateProfileWithoutChangeWritesNothing(t *testing.T) {
	e := newEnv(t)
	caller := tenantCaller(t, id.NewID())
	e.store.current = storedRecord(caller.UserID)

	in := domain.UpdateInput{DisplayName: strPtr(" Budi "), LastName: strPtr(""), Locale: strPtr("id-ID")}
	out, err := e.svc.UpdateProfile(bg, caller, in)
	require.NoError(t, err)
	require.Equal(t, "Budi", out.DisplayName, "keadaan sekarang dikembalikan")
	require.Equal(t, []string{"ForUpdateSelf"}, e.trace.snapshot(), "tidak menulis")
	require.Equal(t, 0, e.audit.count())
	require.Equal(t, 0, e.events.count())
}

func TestUpdateProfileRejectsInvalidInputBeforeAnyTransaction(t *testing.T) {
	e := newEnv(t)
	caller := tenantCaller(t, id.NewID())

	_, err := e.svc.UpdateProfile(bg, caller, domain.UpdateInput{})
	require.ErrorIs(t, err, domain.ErrNothingToUpdate)
	_, err = e.svc.UpdateProfile(bg, caller, domain.UpdateInput{Phone: strPtr("abc")})
	require.ErrorIs(t, err, domain.ErrInvalidPhone)
	_, err = e.svc.UpdateProfile(bg, caller, domain.UpdateInput{AvatarURL: strPtr("http://cdn.example.org/a.png")})
	require.ErrorIs(t, err, domain.ErrInvalidAvatarURL)

	require.Equal(t, 0, e.tx.entered())
	require.Empty(t, e.trace.snapshot())
}

func TestUpdateProfilePropagatesStoreErrorsWithoutAuditOrEvent(t *testing.T) {
	e := newEnv(t)
	caller := tenantCaller(t, id.NewID())
	e.store.current = storedRecord(caller.UserID)
	in := domain.UpdateInput{DisplayName: strPtr("Baru")}

	e.store.forUpdateErr = domain.ErrUserNotFound
	_, err := e.svc.UpdateProfile(bg, caller, in)
	require.ErrorIs(t, err, domain.ErrUserNotFound)
	require.Equal(t, []string{"ForUpdateSelf"}, e.trace.snapshot())

	e.store.forUpdateErr = nil
	e.store.updateErr = errors.New("boom")
	_, err = e.svc.UpdateProfile(bg, caller, in)
	require.EqualError(t, err, "boom")
	require.Equal(t, []string{"ForUpdateSelf", "ForUpdateSelf", "UpdateSelf"}, e.trace.snapshot())
	require.Equal(t, 0, e.audit.count())
	require.Equal(t, 0, e.events.count())
}

func TestUpdateUserIsRejectedForTenantBeforeTransactionAndStore(t *testing.T) {
	e := newEnv(t)
	caller := tenantCaller(t, id.NewID())
	target := id.NewID()

	_, err := e.svc.UpdateUser(bg, caller, target, domain.UpdateInput{DisplayName: strPtr("Baru")})
	require.ErrorIs(t, err, orgdomain.ErrPlatformRequired)
	_, err = e.svc.UpdateUser(bg, caller, target, domain.UpdateInput{})
	require.ErrorIs(t, err, orgdomain.ErrPlatformRequired, "otorisasi lebih dulu daripada validasi masukan")

	require.Equal(t, 0, e.tx.entered())
	require.Empty(t, e.trace.snapshot(), "store tidak disentuh sama sekali")
}

func TestUpdateUserByPlatformWritesChangesWithAuditAndEventInOrder(t *testing.T) {
	e := newEnv(t)
	caller := platformCaller()
	target := id.NewID()
	e.store.current = storedRecord(target)
	updated := storedRecord(target)
	updated.Timezone = "UTC"
	e.store.updated = updated

	out, err := e.svc.UpdateUser(bg, caller, target, domain.UpdateInput{Timezone: strPtr("UTC")})
	require.NoError(t, err)
	require.Equal(t, "UTC", out.Timezone)

	require.Equal(t, []string{"ForUpdate", "Update", "Audit:user.update", "Event:user.updated"}, e.trace.snapshot())
	require.Equal(t, 1, e.tx.entered())
	require.Len(t, e.store.access, 2)
	require.True(t, e.store.access[0].IsPlatform())
	require.True(t, e.store.access[1].IsPlatform())
	require.Equal(t, []uuid.UUID{target, target}, e.store.userIDs)

	entry := e.audit.entries[0]
	require.Equal(t, "user", entry.ResourceType)
	require.Equal(t, target, entry.ResourceID, "sumber daya = user sasaran, bukan pemanggil")
	require.Equal(t, []string{"timezone"}, entry.Metadata["changed_fields"])
	require.Equal(t, caller.UserID, e.audit.actors[0].UserID)

	ev := e.events.events[0]
	require.Equal(t, target, ev.AggregateID)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(ev.Payload, &payload))
	require.Equal(t, target.String(), payload["user_id"])
	require.Equal(t, caller.UserID.String(), payload["actor_user_id"])
}

func TestUpdateUserWithoutChangeWritesNothing(t *testing.T) {
	e := newEnv(t)
	caller := platformCaller()
	target := id.NewID()
	e.store.current = storedRecord(target)

	out, err := e.svc.UpdateUser(bg, caller, target, domain.UpdateInput{Timezone: strPtr("Asia/Jakarta"), FirstName: strPtr("Budi")})
	require.NoError(t, err)
	require.Equal(t, target, out.ID)
	require.Equal(t, []string{"ForUpdate"}, e.trace.snapshot())
	require.Equal(t, 0, e.audit.count())
	require.Equal(t, 0, e.events.count())
}

func TestUpdateUserPropagatesNotFoundWithoutAuditOrEvent(t *testing.T) {
	e := newEnv(t)
	e.store.forUpdateErr = domain.ErrUserNotFound

	_, err := e.svc.UpdateUser(bg, platformCaller(), id.NewID(), domain.UpdateInput{DisplayName: strPtr("Baru")})
	require.ErrorIs(t, err, domain.ErrUserNotFound)
	require.Equal(t, []string{"ForUpdate"}, e.trace.snapshot())
	require.Equal(t, 0, e.audit.count())
	require.Equal(t, 0, e.events.count())
}

func TestZeroCallerIsRejectedEverywhere(t *testing.T) {
	e := newEnv(t)
	var zero application.Caller
	target := id.NewID()
	in := domain.UpdateInput{DisplayName: strPtr("Baru")}

	_, err := e.svc.GetProfile(bg, zero)
	require.ErrorIs(t, err, orgdomain.ErrInvalidAccess)
	_, err = e.svc.UpdateProfile(bg, zero, in)
	require.ErrorIs(t, err, orgdomain.ErrInvalidAccess)
	_, err = e.svc.GetUser(bg, zero, target)
	require.ErrorIs(t, err, orgdomain.ErrInvalidAccess)
	_, err = e.svc.ListUsers(bg, zero, domain.ListQuery{})
	require.ErrorIs(t, err, orgdomain.ErrInvalidAccess)
	_, err = e.svc.UpdateUser(bg, zero, target, in)
	require.ErrorIs(t, err, orgdomain.ErrInvalidAccess)

	require.Equal(t, 0, e.tx.entered())
	require.Empty(t, e.trace.snapshot())
}
