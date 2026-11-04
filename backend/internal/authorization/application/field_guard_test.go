package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/cache"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes FieldGuard murni (tanpa DB/Redis): definisi dan himpunan izin adalah fake yang mencatat
// panggilan, sehingga yang dibuktikan adalah urutan pemeriksaan dan sifat gagal-tertutupnya.

const fieldCostRead = "invoice.field.internal_cost.read"

const fieldCostWrite = "invoice.field.internal_cost.write"

const fieldMarginRead = "invoice.field.margin.read"

type fakeFieldDefs struct {
	// codes dikembalikan apa adanya oleh FieldCodes.
	codes []string

	// err, bila terisi, dikembalikan oleh FieldCodes.
	err error

	// calls menghitung panggilan FieldCodes.
	calls int

	// types mencatat tipe yang diminta.
	types []string
}

func (f *fakeFieldDefs) FieldCodes(_ context.Context, resourceType string) ([]string, error) {
	f.calls++
	f.types = append(f.types, resourceType)
	return f.codes, f.err
}

func restrictedDefs() *fakeFieldDefs {
	return &fakeFieldDefs{codes: []string{fieldCostRead, fieldCostWrite, fieldMarginRead}}
}

func fieldIdentity(f fixture) authdomain.Identity {
	return authdomain.Identity{UserID: f.user, OrganizationID: ptrID(f.org)}
}

func fieldScope(f fixture) domain.ScopeContext {
	return domain.ScopeContext{OrganizationID: ptrID(f.org)}
}

func fieldRef(f fixture) domain.ResourceRef {
	return domain.ResourceRef{Type: "invoice", ID: id.NewID(), OrganizationID: ptrID(f.org)}
}

func fieldObject() map[string]any {
	return map[string]any{"number": "INV-1", "internal_cost": 250000, "margin": 0.2}
}

func newGuard(t *testing.T, f fixture, defs *fakeFieldDefs) *application.FieldGuard {
	t.Helper()
	g, err := application.NewFieldGuard(f.authz, defs)
	require.NoError(t, err)
	return g
}

func TestFieldGuardKeepsOnlyFieldsTheUserMayRead(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{fieldCostRead: domain.LevelOrganization})
	defs := restrictedDefs()
	g := newGuard(t, f, defs)

	out, err := g.Filter(ctx, fieldIdentity(f), fieldScope(f), fieldRef(f), fieldObject())

	require.NoError(t, err)
	require.Equal(t, map[string]any{"number": "INV-1", "internal_cost": 250000}, out, "margin terbatas dan tidak dipegang")
	require.Equal(t, []string{"invoice"}, defs.types)
	require.Equal(t, 1, f.loader.calls)
}

func TestFieldGuardWithoutRestrictionsDoesNotLoadPermissions(t *testing.T) {
	f := newFixture(t, nil)
	defs := &fakeFieldDefs{codes: []string{"invoice.read"}}
	g := newGuard(t, f, defs)

	out, err := g.Filter(ctx, fieldIdentity(f), fieldScope(f), fieldRef(f), fieldObject())

	require.NoError(t, err)
	require.Equal(t, fieldObject(), out)
	require.Zero(t, f.loader.calls, "tanpa field terbatas himpunan izin tidak dimuat")
	require.Zero(t, f.cache.lookups)
}

func TestFieldGuardReadsPermissionsThroughTheSameCacheAsCan(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{fieldCostRead: domain.LevelOrganization})
	g := newGuard(t, f, restrictedDefs())

	_, err := g.Filter(ctx, fieldIdentity(f), fieldScope(f), fieldRef(f), fieldObject())
	require.NoError(t, err)
	require.Equal(t, 1, f.cache.lookups)
	require.Equal(t, 1, f.loader.calls, "miss: dimuat dari sumber kebenaran lalu disimpan")
	require.Len(t, f.cache.stored, 1)

	f.cache.lookup = cache.Lookup{Hit: true, Key: "k", Effective: grants(t, map[string]domain.DecisionLevel{fieldMarginRead: domain.LevelOrganization})}
	out, err := g.Filter(ctx, fieldIdentity(f), fieldScope(f), fieldRef(f), fieldObject())
	require.NoError(t, err)
	require.Equal(t, 1, f.loader.calls, "hit: loader tidak dipanggil lagi")
	require.Equal(t, map[string]any{"number": "INV-1", "margin": 0.2}, out, "hasil mengikuti himpunan izin dari cache")
}

func TestFieldGuardDefinitionLoadFailureIsAnErrorNeverAnUnrestrictedFilter(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{fieldCostRead: domain.LevelOrganization})
	boom := errors.New("db down")
	g := newGuard(t, f, &fakeFieldDefs{err: boom})

	out, err := g.Filter(ctx, fieldIdentity(f), fieldScope(f), fieldRef(f), fieldObject())
	require.ErrorIs(t, err, boom)
	require.Nil(t, out, "tidak boleh ada keluaran yang lolos tanpa penapisan")
	require.Zero(t, f.loader.calls)

	flt, err := g.For(ctx, fieldIdentity(f), fieldScope(f), "invoice")
	require.ErrorIs(t, err, boom)
	_, applyErr := flt.Apply(fieldRef(f), fieldObject())
	require.ErrorIs(t, applyErr, domain.ErrInvalidFieldRules, "pemanggil yang mengabaikan galat tetap gagal-tertutup")
}

func TestFieldGuardPermissionLoadFailureIsAnError(t *testing.T) {
	f := newFixture(t, nil)
	f.loader.err = errors.New("pg down")
	g := newGuard(t, f, restrictedDefs())

	out, err := g.Filter(ctx, fieldIdentity(f), fieldScope(f), fieldRef(f), fieldObject())

	require.ErrorIs(t, err, f.loader.err)
	require.Nil(t, out)
}

func TestFieldGuardRejectsBadInputBeforeTouchingAnyStorage(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{fieldCostRead: domain.LevelOrganization})
	defs := restrictedDefs()
	g := newGuard(t, f, defs)
	otherOrg := id.NewID()

	reject := func(identity authdomain.Identity, scope domain.ScopeContext, typ string, want error) {
		t.Helper()
		_, err := g.For(ctx, identity, scope, typ)
		require.ErrorIs(t, err, want)
		require.Zero(t, defs.calls, "tidak boleh menyentuh definisi")
		require.Zero(t, f.loader.calls, "tidak boleh menyentuh himpunan izin")
	}

	reject(authdomain.Identity{OrganizationID: ptrID(f.org)}, fieldScope(f), "invoice", application.ErrInvalidCheck)
	reject(authdomain.Identity{UserID: f.user, OrganizationID: ptrID(otherOrg)}, fieldScope(f), "invoice", domain.ErrFieldScopeMismatch)
	reject(authdomain.Identity{UserID: f.user}, fieldScope(f), "invoice", domain.ErrFieldScopeMismatch)
	reject(fieldIdentity(f), domain.ScopeContext{}, "invoice", domain.ErrFieldScopeMismatch)
	reject(fieldIdentity(f), domain.ScopeContext{OrganizationID: ptrID(f.org), ActiveGroupID: ptrID(id.NewID())}, "Invoice", domain.ErrInvalidFieldRules)
	reject(authdomain.Identity{UserID: f.user}, domain.ScopeContext{ActiveGroupID: ptrID(f.group)}, "invoice", domain.ErrInvalidScope)
}

func TestFieldGuardBrokenDefinitionFailsClosedWithoutLoadingPermissions(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{fieldCostRead: domain.LevelOrganization})
	g := newGuard(t, f, &fakeFieldDefs{codes: []string{fieldCostRead, "invoice.field.cost.approve"}})

	out, err := g.Filter(ctx, fieldIdentity(f), fieldScope(f), fieldRef(f), fieldObject())

	require.ErrorIs(t, err, domain.ErrInvalidFieldCode)
	require.Nil(t, out)
	require.Zero(t, f.loader.calls)
}

func TestFieldGuardCheckWriteDeniesFieldsWithoutTheirWriteCode(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{fieldCostRead: domain.LevelOrganization})
	g := newGuard(t, f, restrictedDefs())

	err := g.CheckWrite(ctx, fieldIdentity(f), fieldScope(f), fieldRef(f), map[string]any{"number": "X", "internal_cost": 1})
	require.ErrorIs(t, err, domain.ErrFieldPermissionDenied)
	var denied *domain.FieldDeniedError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, []string{"internal_cost"}, denied.Fields)

	require.NoError(t, g.CheckWrite(ctx, fieldIdentity(f), fieldScope(f), fieldRef(f), map[string]any{"number": "X"}))

	writer := newFixture(t, map[string]domain.DecisionLevel{fieldCostWrite: domain.LevelOrganization})
	wg := newGuard(t, writer, restrictedDefs())
	require.NoError(t, wg.CheckWrite(ctx, fieldIdentity(writer), fieldScope(writer), fieldRef(writer), map[string]any{"internal_cost": 1}))
}

func TestFieldGuardWorksForPlatformScope(t *testing.T) {
	f := newFixture(t, map[string]domain.DecisionLevel{fieldMarginRead: domain.LevelPlatform})
	g := newGuard(t, f, restrictedDefs())
	identity := authdomain.Identity{UserID: f.user}
	ref := domain.ResourceRef{Type: "invoice", ID: id.NewID()}

	out, err := g.Filter(ctx, identity, domain.ScopeContext{}, ref, fieldObject())

	require.NoError(t, err)
	require.Equal(t, map[string]any{"number": "INV-1", "margin": 0.2}, out)
	require.Nil(t, f.loader.scope.OrganizationID, "himpunan izin dimuat untuk scope platform")
}

func TestNewFieldGuardRequiresItsCollaborators(t *testing.T) {
	f := newFixture(t, nil)

	_, err := application.NewFieldGuard(nil, restrictedDefs())
	require.Error(t, err)

	_, err = application.NewFieldGuard(f.authz, nil)
	require.Error(t, err)
}
