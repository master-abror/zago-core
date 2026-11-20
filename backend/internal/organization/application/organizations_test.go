package application_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/organization/application"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

func payloadOf(t *testing.T, ev modulesdk.Event) map[string]any {
	t.Helper()
	var p map[string]any
	require.NoError(t, json.Unmarshal(ev.Payload, &p))
	return p
}

func TestArchiveTakesTheAdvisoryLockFirstAndInvalidatesBeforeAudit(t *testing.T) {
	e := newEnv(t, true)
	orgID := id.NewID()
	e.store.current = activeRecord(t, orgID)

	got, err := e.svc.Archive(bg, platformCaller(), orgID)
	require.NoError(t, err)
	require.Equal(t, domain.StatusArchived, got.Status)
	require.Equal(t, []string{
		"LockOrganization:" + orgID.String(),
		"ForUpdate",
		"SetStatus:archived",
		"Invalidate:" + orgID.String(),
		"Audit:organization.archive",
		"Event:organization.archived",
	}, e.trace.snapshot())
}

func TestArchiveWithoutCacheSkipsInvalidationButKeepsTheOrder(t *testing.T) {
	e := newEnv(t, false)
	orgID := id.NewID()
	e.store.current = activeRecord(t, orgID)

	_, err := e.svc.Archive(bg, platformCaller(), orgID)
	require.NoError(t, err)
	require.Equal(t, []string{
		"LockOrganization:" + orgID.String(),
		"ForUpdate",
		"SetStatus:archived",
		"Audit:organization.archive",
		"Event:organization.archived",
	}, e.trace.snapshot())
}

func TestArchiveByTheOwnTenantUsesTenantAccess(t *testing.T) {
	e := newEnv(t, true)
	orgID := id.NewID()
	e.store.current = activeRecord(t, orgID)

	_, err := e.svc.Archive(bg, tenantCaller(t, orgID), orgID)
	require.NoError(t, err)
	require.NotEmpty(t, e.store.access)
	for _, a := range e.store.access {
		tenant, ok := a.Tenant()
		require.True(t, ok, "akses tenant, bukan platform")
		require.Equal(t, orgID, tenant)
	}
}

func TestArchiveOfAnotherOrganizationIsRejectedBeforeAnyLockOrQuery(t *testing.T) {
	e := newEnv(t, true)
	orgA := id.NewID()
	orgB := id.NewID()

	_, err := e.svc.Archive(bg, tenantCaller(t, orgA), orgB)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound)
	require.Empty(t, e.trace.snapshot(), "tidak ada kunci, kueri, audit, atau event")
	require.Equal(t, 0, e.tx.entered(), "transaksi tidak dibuka")
}

func TestArchiveStopsWhenTheLockFails(t *testing.T) {
	e := newEnv(t, true)
	orgID := id.NewID()
	e.store.current = activeRecord(t, orgID)
	e.locker.err = errors.New("kunci gagal")

	_, err := e.svc.Archive(bg, platformCaller(), orgID)
	require.ErrorIs(t, err, e.locker.err)
	require.Equal(t, []string{"LockOrganization:" + orgID.String()}, e.trace.snapshot(), "tidak ada kueri setelah kunci gagal")
	require.Equal(t, 0, e.audit.count())
	require.Equal(t, 0, e.events.count())
}

func TestArchiveFailsClosedWhenInvalidationFails(t *testing.T) {
	e := newEnv(t, true)
	orgID := id.NewID()
	e.store.current = activeRecord(t, orgID)
	e.inv.err = errors.New("redis mati")

	_, err := e.svc.Archive(bg, platformCaller(), orgID)
	require.ErrorIs(t, err, e.inv.err)
	require.Equal(t, 0, e.audit.count(), "audit dicatat sesudah invalidasi berhasil")
	require.Equal(t, 0, e.events.count(), "event diterbitkan sesudah invalidasi berhasil")
}

func TestArchivePropagatesStoreErrorsWithoutAuditOrEvent(t *testing.T) {
	e := newEnv(t, true)
	orgID := id.NewID()
	e.store.forUpdateErr = domain.ErrOrganizationNotFound

	_, err := e.svc.Archive(bg, platformCaller(), orgID)
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound)
	require.Equal(t, []string{"LockOrganization:" + orgID.String(), "ForUpdate"}, e.trace.snapshot())
}

func TestArchiveRecordsThePreviousStatusInAuditAndEvent(t *testing.T) {
	e := newEnv(t, true)
	orgID := id.NewID()
	cur := activeRecord(t, orgID)
	cur.Status = domain.StatusSuspended
	e.store.current = cur
	caller := platformCaller()

	_, err := e.svc.Archive(bg, caller, orgID)
	require.NoError(t, err)
	require.Len(t, e.audit.entries, 1)
	require.Equal(t, "organization", e.audit.entries[0].ResourceType)
	require.Equal(t, orgID, e.audit.entries[0].ResourceID)
	require.Equal(t, domain.StatusSuspended, e.audit.entries[0].Metadata["previous_status"])
	require.Equal(t, caller.UserID, e.audit.actors[0].UserID)
	require.Equal(t, orgID, e.audit.actors[0].OrganizationID, "organisasi aktor = organisasi sasaran")

	require.Len(t, e.events.events, 1)
	ev := e.events.events[0]
	require.Equal(t, "organization.archived", ev.Name)
	require.Equal(t, orgID, ev.OrganizationID)
	require.Equal(t, "organization", ev.AggregateType)
	require.Equal(t, orgID, ev.AggregateID)
	p := payloadOf(t, ev)
	require.Equal(t, orgID.String(), p["organization_id"])
	require.Equal(t, caller.UserID.String(), p["actor_user_id"])
	require.Equal(t, domain.StatusSuspended, p["previous_status"])
}

func TestCreateIsRejectedForTenantCallers(t *testing.T) {
	e := newEnv(t, true)

	_, err := e.svc.Create(bg, tenantCaller(t, id.NewID()), domain.CreateInput{Name: "Acme", Slug: "acme"})
	require.ErrorIs(t, err, domain.ErrPlatformRequired)
	require.Empty(t, e.trace.snapshot())
	require.Equal(t, 0, e.tx.entered())
}

func TestCreateValidatesBeforeOpeningATransaction(t *testing.T) {
	e := newEnv(t, true)

	_, err := e.svc.Create(bg, platformCaller(), domain.CreateInput{Name: "Acme", Slug: "Bad Slug"})
	require.ErrorIs(t, err, domain.ErrInvalidSlug)
	require.Empty(t, e.trace.snapshot())
	require.Equal(t, 0, e.tx.entered())
}

func TestCreateWritesAuditAndEventWithoutLockOrInvalidation(t *testing.T) {
	e := newEnv(t, true)
	caller := platformCaller()

	got, err := e.svc.Create(bg, caller, domain.CreateInput{Name: "Acme", Slug: "acme"})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, got.ID)
	require.Equal(t, []string{"Insert", "Audit:organization.create", "Event:organization.created"}, e.trace.snapshot())

	entry := e.audit.entries[0]
	require.Equal(t, "organization", entry.ResourceType)
	require.Equal(t, got.ID, entry.ResourceID)
	require.Equal(t, "acme", entry.Metadata["slug"])
	require.Equal(t, caller.UserID, e.audit.actors[0].UserID)
	require.Equal(t, got.ID, e.audit.actors[0].OrganizationID)

	ev := e.events.events[0]
	require.Equal(t, got.ID, ev.OrganizationID)
	require.Equal(t, "organization", ev.AggregateType)
	require.Equal(t, got.ID, ev.AggregateID)
}

func TestUpdateWithUnchangedValuesWritesNothing(t *testing.T) {
	e := newEnv(t, true)
	orgID := id.NewID()
	cur := activeRecord(t, orgID)
	e.store.current = cur

	got, err := e.svc.Update(bg, platformCaller(), orgID, domain.UpdateInput{
		Name:          strPtr("Acme"),
		LegalName:     strPtr(""),
		DefaultLocale: strPtr(domain.DefaultLocale),
	})
	require.NoError(t, err)
	require.Equal(t, cur.Name, got.Name)
	require.Equal(t, []string{"ForUpdate"}, e.trace.snapshot(), "tanpa Update, audit, atau event")
	require.Equal(t, 0, e.audit.count())
	require.Equal(t, 0, e.events.count())
}

func TestUpdateReportsOnlyTheFieldsThatChanged(t *testing.T) {
	e := newEnv(t, true)
	orgID := id.NewID()
	cur := activeRecord(t, orgID)
	e.store.current = cur
	e.store.updated = cur

	_, err := e.svc.Update(bg, platformCaller(), orgID, domain.UpdateInput{
		Name:            strPtr("Acme"),
		DefaultLocale:   strPtr("id-ID"),
		DefaultTimezone: strPtr("UTC"),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"ForUpdate", "Update", "Audit:organization.update", "Event:organization.updated"}, e.trace.snapshot())
	require.Equal(t, []string{"default_locale"}, e.audit.entries[0].Metadata["changed_fields"])
	require.Equal(t, []any{"default_locale"}, payloadOf(t, e.events.events[0])["changed_fields"])
	require.NotNil(t, e.store.lastUpdate.DefaultLocale)
	require.Equal(t, "id-ID", *e.store.lastUpdate.DefaultLocale)
}

func TestUpdateReportsChangedFieldNamesSortedAndNeverTheirValues(t *testing.T) {
	e := newEnv(t, true)
	orgID := id.NewID()
	cur := activeRecord(t, orgID)
	e.store.current = cur
	e.store.updated = cur

	_, err := e.svc.Update(bg, platformCaller(), orgID, domain.UpdateInput{
		Name:            strPtr("Rahasia Baru"),
		LegalName:       strPtr("PT Rahasia"),
		DefaultTimezone: strPtr("Asia/Jakarta"),
	})
	require.NoError(t, err)
	require.Equal(t, []string{"default_timezone", "legal_name", "name"}, e.audit.entries[0].Metadata["changed_fields"])
	raw, err := json.Marshal(e.audit.entries[0].Metadata)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "Rahasia", "nilai tidak masuk metadata audit")
	require.NotContains(t, string(e.events.events[0].Payload), "Rahasia", "nilai tidak masuk payload event")
}

func TestUpdateOfAnArchivedOrganizationIsRejected(t *testing.T) {
	e := newEnv(t, true)
	orgID := id.NewID()
	cur := activeRecord(t, orgID)
	cur.Status = domain.StatusArchived
	at := time.Now()
	cur.ArchivedAt = &at
	e.store.current = cur

	_, err := e.svc.Update(bg, platformCaller(), orgID, domain.UpdateInput{Name: strPtr("Baru")})
	require.ErrorIs(t, err, domain.ErrOrganizationArchived)
	require.Equal(t, []string{"ForUpdate"}, e.trace.snapshot())
}

func TestUpdatePropagatesStoreErrorsWithoutAuditOrEvent(t *testing.T) {
	e := newEnv(t, true)
	e.store.forUpdateErr = domain.ErrOrganizationNotFound

	_, err := e.svc.Update(bg, platformCaller(), id.NewID(), domain.UpdateInput{Name: strPtr("Baru")})
	require.ErrorIs(t, err, domain.ErrOrganizationNotFound)
	require.Equal(t, 0, e.audit.count())
	require.Equal(t, 0, e.events.count())
}

func TestUpdateValidatesBeforeOpeningATransaction(t *testing.T) {
	e := newEnv(t, true)

	_, err := e.svc.Update(bg, platformCaller(), id.NewID(), domain.UpdateInput{})
	require.ErrorIs(t, err, domain.ErrNothingToUpdate)
	_, err = e.svc.Update(bg, platformCaller(), id.NewID(), domain.UpdateInput{DefaultTimezone: strPtr("Mars/Base")})
	require.ErrorIs(t, err, domain.ErrInvalidTimezone)
	require.Equal(t, 0, e.tx.entered())
	require.Empty(t, e.trace.snapshot())
}

func TestCallerScopeSelectsTheStoreAccess(t *testing.T) {
	e := newEnv(t, false)
	orgID := id.NewID()
	e.store.current = activeRecord(t, orgID)

	_, err := e.svc.Get(bg, tenantCaller(t, orgID), orgID)
	require.NoError(t, err)
	_, err = e.svc.Get(bg, platformCaller(), orgID)
	require.NoError(t, err)
	_, err = e.svc.List(bg, tenantCaller(t, orgID), domain.ListQuery{})
	require.NoError(t, err)

	require.Len(t, e.store.access, 3)
	tenant, ok := e.store.access[0].Tenant()
	require.True(t, ok)
	require.Equal(t, orgID, tenant)
	require.True(t, e.store.access[1].IsPlatform())
	tenant, ok = e.store.access[2].Tenant()
	require.True(t, ok)
	require.Equal(t, orgID, tenant)
}

func TestCallerWithoutUserIsRejectedByEveryOperation(t *testing.T) {
	e := newEnv(t, true)
	var none application.Caller
	name := "Baru"

	_, err := e.svc.Get(bg, none, id.NewID())
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	_, err = e.svc.List(bg, none, domain.ListQuery{})
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	_, err = e.svc.Create(bg, none, domain.CreateInput{Name: "Acme", Slug: "acme"})
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	_, err = e.svc.Update(bg, none, id.NewID(), domain.UpdateInput{Name: &name})
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	_, err = e.svc.Archive(bg, none, id.NewID())
	require.ErrorIs(t, err, domain.ErrInvalidAccess)
	require.Empty(t, e.trace.snapshot())
	require.Equal(t, 0, e.tx.entered())
}

func TestNewServiceRequiresItsDependencies(t *testing.T) {
	tr := &trace{}
	tx := &countingTx{}
	st := &fakeStore{trace: tr}
	lk := &fakeLocker{trace: tr}
	au := &fakeAudit{trace: tr}
	ev := &fakeEvents{trace: tr}

	_, err := application.NewService(nil, st, lk, au, ev, nil)
	require.Error(t, err)
	_, err = application.NewService(tx, nil, lk, au, ev, nil)
	require.Error(t, err)
	_, err = application.NewService(tx, st, nil, au, ev, nil)
	require.Error(t, err)
	_, err = application.NewService(tx, st, lk, nil, ev, nil)
	require.Error(t, err)
	_, err = application.NewService(tx, st, lk, au, nil, nil)
	require.Error(t, err)

	svc, err := application.NewService(tx, st, lk, au, ev, nil)
	require.NoError(t, err, "invalidator boleh nil")
	require.NotNil(t, svc)
}
