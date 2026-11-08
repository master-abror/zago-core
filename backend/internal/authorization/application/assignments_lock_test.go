package application_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
	modulesdk "github.com/master-abror/zago-core/packages/module-sdk"
)

// Tes ini membuktikan kewajiban ADR-0022 butir 54 tanpa database: panggilan store PERTAMA di
// dalam transaksi adalah kunci advisory, atas organisasi PEMBERI (bukan organisasi target) untuk
// grantor tenant dan kunci platform untuk grantor platform, bahkan ketika permintaan berakhir
// ditolak. Store palsu hanya mengganti metode yang dijangkau; metode lain panik bila terpanggil,
// yang berarti ada pembacaan di luar yang diperkirakan.

type lockOrderStore struct {
	application.AssignmentStore
	calls []string
}

func (s *lockOrderStore) LockOrganization(_ context.Context, org uuid.UUID) error {
	s.calls = append(s.calls, "LockOrganization:"+org.String())
	return nil
}

func (s *lockOrderStore) LockPlatform(context.Context) error {
	s.calls = append(s.calls, "LockPlatform")
	return nil
}

func (s *lockOrderStore) Role(context.Context, uuid.UUID, *uuid.UUID) (domain.RoleRecord, error) {
	s.calls = append(s.calls, "Role")
	return domain.RoleRecord{}, domain.ErrRoleNotFound
}

func (s *lockOrderStore) ForUpdate(context.Context, uuid.UUID, *uuid.UUID) (domain.AssignmentRecord, error) {
	s.calls = append(s.calls, "ForUpdate")
	return domain.AssignmentRecord{}, domain.ErrAssignmentNotFound
}

type directTx struct{}

func (directTx) WithinTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type nopAudit struct{}

func (nopAudit) Record(context.Context, modulesdk.ActivityEntry) {}

type nopEvents struct{}

func (nopEvents) Publish(context.Context, modulesdk.Event) {}

func lockCallsOfCreate(t *testing.T, g application.Grantor, in application.CreateAssignmentInput) []string {
	t.Helper()
	store := &lockOrderStore{}
	w, err := application.NewAssignmentWriter(directTx{}, store, nopAudit{}, nopEvents{}, nil)
	require.NoError(t, err)
	_, err = w.Create(ctx, g, in)
	require.Error(t, err)
	return store.calls
}

func lockCallsOfRevoke(t *testing.T, g application.Grantor) []string {
	t.Helper()
	store := &lockOrderStore{}
	w, err := application.NewAssignmentWriter(directTx{}, store, nopAudit{}, nopEvents{}, nil)
	require.NoError(t, err)
	require.ErrorIs(t, w.Revoke(ctx, g, id.NewID()), domain.ErrAssignmentNotFound)
	return store.calls
}

func TestAssignmentWriterTakesTheAdvisoryLockBeforeAnyOtherStoreCall(t *testing.T) {
	org, otherOrg := id.NewID(), id.NewID()
	tenant := application.Grantor{UserID: id.NewID(), Scope: domain.ScopeContext{OrganizationID: &org}}
	platform := application.Grantor{UserID: id.NewID()}
	lockOrg := "LockOrganization:" + org.String()

	inOrg := application.CreateAssignmentInput{RoleID: id.NewID(), SubjectType: domain.SubjectUser, SubjectID: id.NewID(), Scope: domain.OrganizationTarget(org)}
	toOtherOrg := inOrg
	toOtherOrg.Scope = domain.OrganizationTarget(otherOrg)
	toPlatform := inOrg
	toPlatform.Scope = domain.PlatformTarget()

	require.Equal(t, []string{lockOrg, "Role"}, lockCallsOfCreate(t, tenant, inOrg),
		"grantor tenant: kunci organisasi dulu, baru baca role")
	require.Equal(t, []string{lockOrg}, lockCallsOfCreate(t, tenant, toOtherOrg),
		"target organisasi lain: tetap mengunci organisasi PEMBERI, tidak pernah organisasi target, dan tidak membaca apa pun")
	require.Equal(t, []string{lockOrg}, lockCallsOfCreate(t, tenant, toPlatform),
		"grantor tenant tidak pernah mengunci platform")

	require.Equal(t, []string{"LockPlatform", "Role"}, lockCallsOfCreate(t, platform, toPlatform),
		"grantor platform: kunci platform dulu")
	require.Equal(t, []string{"LockPlatform"}, lockCallsOfCreate(t, platform, inOrg),
		"grantor platform tidak mengunci organisasi dan tidak memberi di organisasi")

	require.Equal(t, []string{lockOrg, "ForUpdate"}, lockCallsOfRevoke(t, tenant))
	require.Equal(t, []string{"LockPlatform", "ForUpdate"}, lockCallsOfRevoke(t, platform))
}
