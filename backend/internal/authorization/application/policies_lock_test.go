package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/policy"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes ini membuktikan kewajiban ADR-0022 butir 54 untuk penulis policy tanpa database: panggilan
// store PERTAMA di dalam transaksi adalah kunci advisory, organisasi untuk pemanggil tenant dan
// platform untuk pemanggil platform, juga ketika permintaan berakhir ditolak. Store palsu hanya
// mengganti metode yang dijangkau; metode lain panik bila terpanggil (antarmuka yang ditanam
// bernilai nil), yang berarti ada pembacaan di luar yang diperkirakan.

var errCatalogStop = errors.New("katalog sengaja gagal")

type policyLockStore struct {
	application.PolicyStore
	calls []string
}

func (s *policyLockStore) LockOrganization(_ context.Context, org uuid.UUID) error {
	s.calls = append(s.calls, "LockOrganization:"+org.String())
	return nil
}

func (s *policyLockStore) LockPlatform(context.Context) error {
	s.calls = append(s.calls, "LockPlatform")
	return nil
}

func (s *policyLockStore) Catalog(context.Context) (policy.ActionCatalog, error) {
	s.calls = append(s.calls, "Catalog")
	return nil, errCatalogStop
}

func (s *policyLockStore) ForUpdate(context.Context, uuid.UUID, *uuid.UUID) (domain.PolicyRecord, error) {
	s.calls = append(s.calls, "ForUpdate")
	return domain.PolicyRecord{}, domain.ErrPolicyNotFound
}

func polLockInput() application.CreatePolicyInput {
	return application.CreatePolicyInput{Code: "lock.order", Name: "Urutan kunci", Effect: policy.EffectDeny, Expression: "true", Actions: []string{"role.manage"}}
}

func polLockWriter(t *testing.T) (*application.PolicyWriter, *policyLockStore) {
	t.Helper()
	store := &policyLockStore{}
	w, err := application.NewPolicyWriter(directTx{}, store, nopAudit{}, nopEvents{})
	require.NoError(t, err)
	return w, store
}

func polGrantors() (tenant, platform, grouped application.Grantor, org uuid.UUID) {
	org = id.NewID()
	grp := id.NewID()
	tenant = application.Grantor{UserID: id.NewID(), Scope: domain.ScopeContext{OrganizationID: &org}}
	platform = application.Grantor{UserID: id.NewID()}
	grouped = application.Grantor{UserID: id.NewID(), Scope: domain.ScopeContext{OrganizationID: &org, ActiveGroupID: &grp}}
	return tenant, platform, grouped, org
}

func TestPolicyWriterCreateTakesTheAdvisoryLockBeforeAnyOtherStoreCall(t *testing.T) {
	tenant, platform, grouped, org := polGrantors()
	lockOrg := "LockOrganization:" + org.String()

	w, store := polLockWriter(t)
	_, err := w.Create(ctx, tenant, polLockInput())
	require.ErrorIs(t, err, errCatalogStop)
	require.Equal(t, []string{lockOrg, "Catalog"}, store.calls, "pemanggil tenant: kunci organisasi dulu, baru membaca apa pun")

	w, store = polLockWriter(t)
	_, err = w.Create(ctx, platform, polLockInput())
	require.ErrorIs(t, err, errCatalogStop)
	require.Equal(t, []string{"LockPlatform", "Catalog"}, store.calls, "pemanggil platform: kunci platform dulu, tidak pernah kunci organisasi")

	w, store = polLockWriter(t)
	_, err = w.Create(ctx, grouped, polLockInput())
	require.ErrorIs(t, err, domain.ErrTargetOutsideScope)
	require.Equal(t, []string{lockOrg}, store.calls, "grup aktif ditolak SESUDAH kunci diambil dan sebelum apa pun dibaca")
}

func TestPolicyWriterUpdateTakesTheAdvisoryLockBeforeAnyOtherStoreCall(t *testing.T) {
	tenant, platform, grouped, org := polGrantors()
	lockOrg := "LockOrganization:" + org.String()
	name := "baru"
	patch := application.UpdatePolicyInput{Name: &name}

	w, store := polLockWriter(t)
	_, err := w.Update(ctx, tenant, id.NewID(), patch)
	require.ErrorIs(t, err, domain.ErrPolicyNotFound)
	require.Equal(t, []string{lockOrg, "ForUpdate"}, store.calls)

	w, store = polLockWriter(t)
	_, err = w.Update(ctx, platform, id.NewID(), patch)
	require.ErrorIs(t, err, domain.ErrPolicyNotFound)
	require.Equal(t, []string{"LockPlatform", "ForUpdate"}, store.calls)

	w, store = polLockWriter(t)
	_, err = w.Update(ctx, grouped, id.NewID(), patch)
	require.ErrorIs(t, err, domain.ErrTargetOutsideScope)
	require.Equal(t, []string{lockOrg}, store.calls)
}

func TestPolicyWriterRejectsMalformedRequestsBeforeTouchingTheStore(t *testing.T) {
	tenant, _, _, _ := polGrantors()
	w, store := polLockWriter(t)

	bad := polLockInput()
	bad.Status = "weird"
	_, err := w.Create(ctx, tenant, bad)
	require.ErrorIs(t, err, domain.ErrInvalidPolicyStatus)

	_, err = w.Update(ctx, tenant, id.NewID(), application.UpdatePolicyInput{})
	require.ErrorIs(t, err, domain.ErrEmptyPolicyPatch)

	weird := "weird"
	_, err = w.Update(ctx, tenant, id.NewID(), application.UpdatePolicyInput{Status: &weird})
	require.ErrorIs(t, err, domain.ErrInvalidPolicyStatus)

	require.Empty(t, store.calls, "masukan cacat ditolak sebelum transaksi: tidak ada kunci, tidak ada pembacaan")
}

func TestNewPolicyWriterRequiresItsDependencies(t *testing.T) {
	store := &policyLockStore{}
	_, err := application.NewPolicyWriter(nil, store, nopAudit{}, nopEvents{})
	require.Error(t, err)
	_, err = application.NewPolicyWriter(directTx{}, nil, nopAudit{}, nopEvents{})
	require.Error(t, err)
	_, err = application.NewPolicyWriter(directTx{}, store, nil, nopEvents{})
	require.Error(t, err)
	_, err = application.NewPolicyWriter(directTx{}, store, nopAudit{}, nil)
	require.Error(t, err)
	w, err := application.NewPolicyWriter(directTx{}, store, nopAudit{}, nopEvents{})
	require.NoError(t, err)
	require.NotNil(t, w)
}
