package domain_test

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/permission"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

const codeCostRead = "invoice.field.internal_cost.read"

const codeCostWrite = "invoice.field.internal_cost.write"

const codeMarginRead = "invoice.field.margin.read"

func fieldRules(t *testing.T) domain.FieldRules {
	t.Helper()
	r, err := domain.NewFieldRules("invoice", []string{codeCostRead, codeCostWrite, codeMarginRead, "invoice.read"})
	require.NoError(t, err)
	return r
}

func fieldGrants(t *testing.T, level domain.DecisionLevel, codes ...string) domain.EffectivePermissions {
	t.Helper()
	m := make(map[string]domain.DecisionLevel, len(codes))
	for _, c := range codes {
		m[c] = level
	}
	e, err := domain.EffectiveFromGrants(m)
	require.NoError(t, err)
	return e
}

func fieldFilter(t *testing.T, eff domain.EffectivePermissions, org, grp *uuid.UUID) domain.FieldFilter {
	t.Helper()
	scope, err := domain.NewScope(org, grp)
	require.NoError(t, err)
	f, err := domain.NewFieldFilter(fieldRules(t), eff, scope)
	require.NoError(t, err)
	return f
}

func invoiceRef(org, grp *uuid.UUID) domain.ResourceRef {
	return domain.ResourceRef{Type: "invoice", ID: id.NewID(), OrganizationID: org, GroupID: grp}
}

func invoiceObject() map[string]any {
	return map[string]any{"id": "x", "number": "INV-001", "customer": "ABC", "amount": 1000000, "internal_cost": 250000, "margin": 0.2}
}

func hasKey(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

func TestFieldCodeBuildsValidCodesAndRejectsMalformed(t *testing.T) {
	code, err := domain.FieldCode("invoice", "internal_cost", domain.FieldRead)
	require.NoError(t, err)
	require.Equal(t, codeCostRead, code)

	nested, err := domain.FieldCode("organization.member", "title", domain.FieldWrite)
	require.NoError(t, err)
	require.Equal(t, "organization.member.field.title.write", nested)
	_, _, err = permission.Split(nested)
	require.NoError(t, err, "kode field harus sah menurut aturan katalog")

	bad := []struct{ typ, name, act string }{
		{"invoice", "internal_cost", "approve"},
		{"invoice", "internal_cost", ""},
		{"Invoice", "cost", "read"},
		{"", "cost", "read"},
		{"invoice", "", "read"},
		{"invoice", "a.b", "read"},
		{"invoice", "1cost", "read"},
		{"invoice", "internal-cost", "read"},
		{"invoice", "x.field.y", "read"},
		{"invoice", "cost ", "read"},
	}
	for _, c := range bad {
		_, err := domain.FieldCode(c.typ, c.name, domain.FieldAction(c.act))
		require.ErrorIs(t, err, domain.ErrInvalidFieldCode, "%q %q %q", c.typ, c.name, c.act)
	}
}

func TestNewFieldRulesRejectsMalformedInput(t *testing.T) {
	for _, typ := range []string{"", "Invoice", "a b", "invoice."} {
		_, err := domain.NewFieldRules(typ, nil)
		require.ErrorIs(t, err, domain.ErrInvalidFieldRules, "tipe %q", typ)
	}

	_, err := domain.NewFieldRules("invoice", []string{"Invoice.Field.X.Read"})
	require.ErrorIs(t, err, domain.ErrInvalidFieldCode)

	_, err = domain.NewFieldRules("invoice", []string{"invoice.field.cost.approve"})
	require.ErrorIs(t, err, domain.ErrInvalidFieldCode, "aksi selain read/write pada tipe ini adalah galat, bukan diabaikan")

	_, err = domain.NewFieldRules("invoice", []string{"invoice.read", codeCostRead, "not a code"})
	require.ErrorIs(t, err, domain.ErrInvalidFieldCode, "satu kode rusak menggagalkan seluruh definisi")
}

func TestNewFieldRulesIgnoresCodesThatAreNotItsOwnFieldCodes(t *testing.T) {
	rules, err := domain.NewFieldRules("invoice", []string{
		"invoice.read",
		"invoice.field.read",
		"other.field.cost.read",
		"invoice.field.x.field.y.read",
		"other.field.cost.approve",
	})
	require.NoError(t, err)
	for _, key := range []string{"cost", "x", "y", "read", "field"} {
		require.False(t, rules.Restricts(key, domain.FieldRead), key)
		require.False(t, rules.Restricts(key, domain.FieldWrite), key)
	}
}

func TestFieldCodeRoundTripsThroughRulesAndTypesDoNotClaimEachOthersFields(t *testing.T) {
	for _, typ := range []string{"invoice", "organization.member", "a.field.b"} {
		for _, action := range []domain.FieldAction{domain.FieldRead, domain.FieldWrite} {
			code, err := domain.FieldCode(typ, "cost", action)
			require.NoError(t, err)
			rules, err := domain.NewFieldRules(typ, []string{code})
			require.NoError(t, err)
			require.True(t, rules.Restricts("cost", action), code)
			require.Equal(t, typ, rules.ResourceType())
		}
	}

	code, err := domain.FieldCode("a.field.b", "c", domain.FieldRead)
	require.NoError(t, err)
	rules, err := domain.NewFieldRules("a", []string{code})
	require.NoError(t, err)
	require.False(t, rules.Restricts("c", domain.FieldRead), "tipe a tidak boleh mengklaim field milik tipe a.field.b")
}

func TestRulesRestrictPerActionOnlyWhereACodeIsDeclared(t *testing.T) {
	rules := fieldRules(t)
	require.Equal(t, "invoice", rules.ResourceType())
	require.True(t, rules.Restricts("internal_cost", domain.FieldRead))
	require.True(t, rules.Restricts("internal_cost", domain.FieldWrite))
	require.True(t, rules.Restricts("margin", domain.FieldRead))
	require.False(t, rules.Restricts("margin", domain.FieldWrite), "hanya read yang dideklarasikan untuk margin")
	require.False(t, rules.Restricts("number", domain.FieldRead))
	require.False(t, rules.Restricts("number", domain.FieldWrite))
	require.True(t, rules.Restricts("internalCost", domain.FieldRead))
}

func TestApplyDropsRestrictedFieldsEntirelyNotNullOrMasked(t *testing.T) {
	org := id.NewID()
	f := fieldFilter(t, domain.EffectivePermissions{}, ptr(org), nil)

	out, err := f.Apply(invoiceRef(ptr(org), nil), invoiceObject())
	require.NoError(t, err)
	require.False(t, hasKey(out, "internal_cost"), "field terbatas harus absen, bukan null")
	require.False(t, hasKey(out, "margin"))
	require.Equal(t, map[string]any{"id": "x", "number": "INV-001", "customer": "ABC", "amount": 1000000}, out)
}

func TestApplyKeepsOnlyTheRestrictedFieldsTheCallerMayRead(t *testing.T) {
	org := id.NewID()
	ref := invoiceRef(ptr(org), nil)

	onlyCost := fieldFilter(t, fieldGrants(t, domain.LevelOrganization, codeCostRead), ptr(org), nil)
	out, err := onlyCost.Apply(ref, invoiceObject())
	require.NoError(t, err)
	require.True(t, hasKey(out, "internal_cost"))
	require.False(t, hasKey(out, "margin"))

	both := fieldFilter(t, fieldGrants(t, domain.LevelOrganization, codeCostRead, codeMarginRead), ptr(org), nil)
	out, err = both.Apply(ref, invoiceObject())
	require.NoError(t, err)
	require.Equal(t, invoiceObject(), out)

	platform := fieldFilter(t, fieldGrants(t, domain.LevelPlatform, codeCostRead, codeMarginRead), ptr(org), nil)
	out, err = platform.Apply(ref, invoiceObject())
	require.NoError(t, err)
	require.Equal(t, invoiceObject(), out)
}

func TestApplyDoesNotMutateInputAndReturnsAFreshMap(t *testing.T) {
	org := id.NewID()
	f := fieldFilter(t, domain.EffectivePermissions{}, ptr(org), nil)
	ref := invoiceRef(ptr(org), nil)

	in := invoiceObject()
	out, err := f.Apply(ref, in)
	require.NoError(t, err)
	require.Len(t, in, 6, "masukan tidak boleh diubah")
	out["injected"] = 1
	require.False(t, hasKey(in, "injected"), "hasil harus peta baru")

	empty, err := f.Apply(ref, nil)
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty)
}

func TestEveryAsciiVariantOfARestrictedKeyIsDroppedAndRejected(t *testing.T) {
	org := id.NewID()
	ref := invoiceRef(ptr(org), nil)
	rules := fieldRules(t)
	denied := fieldFilter(t, domain.EffectivePermissions{}, ptr(org), nil)
	granted := fieldFilter(t, fieldGrants(t, domain.LevelOrganization, codeCostRead, codeCostWrite), ptr(org), nil)

	seps := []string{"", "_", "-", " ", ".", "\t", "/"}
	forms := []func(string) string{
		strings.ToLower,
		strings.ToUpper,
		func(s string) string { return strings.ToUpper(s[:1]) + s[1:] },
	}
	for _, sep := range seps {
		for _, form := range forms {
			key := form("internal") + sep + form("cost")
			require.True(t, rules.Restricts(key, domain.FieldRead), "varian %q harus dikenali", key)
			obj := map[string]any{key: 1, "number": 2}

			out, err := denied.Apply(ref, obj)
			require.NoError(t, err)
			require.False(t, hasKey(out, key), "varian %q harus dijatuhkan", key)
			require.True(t, hasKey(out, "number"))
			require.Error(t, denied.CheckWrite(ref, obj), "varian %q harus ditolak saat ditulis", key)

			out, err = granted.Apply(ref, obj)
			require.NoError(t, err)
			require.True(t, hasKey(out, key), "varian %q harus tampil bila izin dipegang", key)
			require.NoError(t, granted.CheckWrite(ref, obj))
		}
	}
}

func TestUnrestrictedKeysAreNeverDropped(t *testing.T) {
	org := id.NewID()
	f := fieldFilter(t, domain.EffectivePermissions{}, ptr(org), nil)
	obj := map[string]any{"number": 1, "customer": 2, "amount": 3, "a_b": 4, "internal": 5, "cost": 6, "internal_costs": 7, "": 8}

	out, err := f.Apply(invoiceRef(ptr(org), nil), obj)
	require.NoError(t, err)
	require.Equal(t, obj, out)
	require.NoError(t, f.CheckWrite(invoiceRef(ptr(org), nil), map[string]any{"number": 1, "margin": 2}))
}

func TestNonAsciiKeysNeedEveryRestrictedCodeNotJustTheirLookalike(t *testing.T) {
	org := id.NewID()
	ref := invoiceRef(ptr(org), nil)
	none := fieldFilter(t, domain.EffectivePermissions{}, ptr(org), nil)
	onlyCost := fieldFilter(t, fieldGrants(t, domain.LevelOrganization, codeCostRead), ptr(org), nil)
	everything := fieldFilter(t, fieldGrants(t, domain.LevelOrganization, codeCostRead, codeMarginRead), ptr(org), nil)
	writer := fieldFilter(t, fieldGrants(t, domain.LevelOrganization, codeCostWrite), ptr(org), nil)

	for _, key := range []string{"internal_co\u017ft", "\u540d\u524d"} {
		obj := map[string]any{key: 1, "number": 2}

		out, err := none.Apply(ref, obj)
		require.NoError(t, err)
		require.False(t, hasKey(out, key), "tanpa izin: kunci non-ASCII dijatuhkan")
		require.True(t, hasKey(out, "number"))

		out, err = onlyCost.Apply(ref, obj)
		require.NoError(t, err)
		require.False(t, hasKey(out, key), "izin untuk sebagian field terbatas tidak cukup")

		out, err = everything.Apply(ref, obj)
		require.NoError(t, err)
		require.True(t, hasKey(out, key))

		require.Error(t, none.CheckWrite(ref, obj))
		require.NoError(t, writer.CheckWrite(ref, obj), "semua kode tulis dipegang")
	}
}

func TestCollidingFieldNamesRequireEveryCodeOfTheCollision(t *testing.T) {
	org := id.NewID()
	rules, err := domain.NewFieldRules("doc", []string{"doc.field.ab.read", "doc.field.a_b.read"})
	require.NoError(t, err)
	scope, err := domain.NewScope(ptr(org), nil)
	require.NoError(t, err)
	ref := domain.ResourceRef{Type: "doc", ID: id.NewID(), OrganizationID: ptr(org)}
	obj := map[string]any{"ab": 1, "a_b": 2, "title": 3}

	oneCode, err := domain.EffectiveFromGrants(map[string]domain.DecisionLevel{"doc.field.ab.read": domain.LevelOrganization})
	require.NoError(t, err)
	f, err := domain.NewFieldFilter(rules, oneCode, scope)
	require.NoError(t, err)
	out, err := f.Apply(ref, obj)
	require.NoError(t, err)
	require.False(t, hasKey(out, "ab"), "kunci berbentuk ringkas sama wajib memegang kedua kode")
	require.False(t, hasKey(out, "a_b"))
	require.True(t, hasKey(out, "title"))

	twoCodes, err := domain.EffectiveFromGrants(map[string]domain.DecisionLevel{"doc.field.ab.read": domain.LevelOrganization, "doc.field.a_b.read": domain.LevelOrganization})
	require.NoError(t, err)
	f, err = domain.NewFieldFilter(rules, twoCodes, scope)
	require.NoError(t, err)
	out, err = f.Apply(ref, obj)
	require.NoError(t, err)
	require.Equal(t, obj, out)
}

func TestGroupLevelGrantAppliesOnlyToResourcesOfTheActiveGroup(t *testing.T) {
	org, group, other := id.NewID(), id.NewID(), id.NewID()
	eff := fieldGrants(t, domain.LevelGroup, codeCostRead)
	f := fieldFilter(t, eff, ptr(org), ptr(group))

	out, err := f.Apply(invoiceRef(ptr(org), ptr(group)), invoiceObject())
	require.NoError(t, err)
	require.True(t, hasKey(out, "internal_cost"))

	out, err = f.Apply(invoiceRef(ptr(org), ptr(other)), invoiceObject())
	require.NoError(t, err)
	require.False(t, hasKey(out, "internal_cost"), "sumber daya grup lain")

	out, err = f.Apply(invoiceRef(ptr(org), nil), invoiceObject())
	require.NoError(t, err)
	require.False(t, hasKey(out, "internal_cost"), "sumber daya tanpa grup")

	noActiveGroup := fieldFilter(t, eff, ptr(org), nil)
	out, err = noActiveGroup.Apply(invoiceRef(ptr(org), ptr(group)), invoiceObject())
	require.NoError(t, err)
	require.False(t, hasKey(out, "internal_cost"), "tanpa grup aktif izin level group tidak berlaku")

	orgLevel := fieldFilter(t, fieldGrants(t, domain.LevelOrganization, codeCostRead), ptr(org), nil)
	out, err = orgLevel.Apply(invoiceRef(ptr(org), nil), invoiceObject())
	require.NoError(t, err)
	require.True(t, hasKey(out, "internal_cost"), "level organisasi tidak mensyaratkan grup")
}

func TestScopeMismatchIsAnErrorNotASilentFilter(t *testing.T) {
	org, otherOrg := id.NewID(), id.NewID()
	eff := fieldGrants(t, domain.LevelOrganization, codeCostRead, codeCostWrite)
	tenant := fieldFilter(t, eff, ptr(org), nil)

	for _, ref := range []domain.ResourceRef{invoiceRef(ptr(otherOrg), nil), invoiceRef(nil, nil)} {
		out, err := tenant.Apply(ref, invoiceObject())
		require.ErrorIs(t, err, domain.ErrFieldScopeMismatch)
		require.Nil(t, out)
		require.ErrorIs(t, tenant.CheckWrite(ref, invoiceObject()), domain.ErrFieldScopeMismatch)
	}

	platform := fieldFilter(t, eff, nil, nil)
	out, err := platform.Apply(invoiceRef(nil, nil), invoiceObject())
	require.NoError(t, err)
	require.True(t, hasKey(out, "internal_cost"))

	_, err = platform.Apply(invoiceRef(ptr(org), nil), invoiceObject())
	require.ErrorIs(t, err, domain.ErrFieldScopeMismatch, "scope platform tidak boleh membaca sumber daya organisasi lewat filter ini")
}

func TestResourceTypeMustMatchTheRules(t *testing.T) {
	org := id.NewID()
	f := fieldFilter(t, fieldGrants(t, domain.LevelOrganization, codeCostRead, codeCostWrite), ptr(org), nil)
	ref := domain.ResourceRef{Type: "bill", ID: id.NewID(), OrganizationID: ptr(org)}

	out, err := f.Apply(ref, invoiceObject())
	require.ErrorIs(t, err, domain.ErrFieldTypeMismatch)
	require.Nil(t, out)
	require.ErrorIs(t, f.CheckWrite(ref, invoiceObject()), domain.ErrFieldTypeMismatch)
}

func TestZeroValuesAndInvalidInputsFailClosed(t *testing.T) {
	org := id.NewID()
	ref := invoiceRef(ptr(org), nil)

	var zero domain.FieldFilter
	out, err := zero.Apply(ref, invoiceObject())
	require.ErrorIs(t, err, domain.ErrInvalidFieldRules)
	require.Nil(t, out)
	require.ErrorIs(t, zero.CheckWrite(ref, invoiceObject()), domain.ErrInvalidFieldRules)

	scope, err := domain.NewScope(ptr(org), nil)
	require.NoError(t, err)
	_, err = domain.NewFieldFilter(domain.FieldRules{}, domain.EffectivePermissions{}, scope)
	require.ErrorIs(t, err, domain.ErrInvalidFieldRules)

	_, err = domain.NewFieldFilter(fieldRules(t), domain.EffectivePermissions{}, domain.ScopeContext{ActiveGroupID: ptr(id.NewID())})
	require.ErrorIs(t, err, domain.ErrInvalidScope, "grup aktif tanpa organisasi")
	_, err = domain.NewFieldFilter(fieldRules(t), domain.EffectivePermissions{}, domain.ScopeContext{OrganizationID: ptr(uuid.Nil)})
	require.ErrorIs(t, err, domain.ErrInvalidScope, "organisasi UUID nol")
}

func TestCheckWriteRejectsOverPostedFieldsIndependentlyOfRead(t *testing.T) {
	org := id.NewID()
	ref := invoiceRef(ptr(org), nil)

	readOnly := fieldFilter(t, fieldGrants(t, domain.LevelOrganization, codeCostRead, codeMarginRead), ptr(org), nil)
	err := readOnly.CheckWrite(ref, invoiceObject())
	require.ErrorIs(t, err, domain.ErrFieldPermissionDenied)
	var denied *domain.FieldDeniedError
	require.ErrorAs(t, err, &denied)
	require.Equal(t, []string{"internal_cost"}, denied.Fields, "margin hanya dibatasi baca, jadi menulisnya tidak ditolak")
	require.Equal(t, 1, denied.Total)
	require.NotContains(t, err.Error(), "internal_cost", "pesan galat tidak memuat nama kunci dari klien")

	require.NoError(t, readOnly.CheckWrite(ref, map[string]any{"number": "X", "customer": "Y"}))
	require.NoError(t, readOnly.CheckWrite(ref, nil))

	writeOnly := fieldFilter(t, fieldGrants(t, domain.LevelOrganization, codeCostWrite), ptr(org), nil)
	require.NoError(t, writeOnly.CheckWrite(ref, invoiceObject()), "hak tulis dipegang")
	out, err := writeOnly.Apply(ref, invoiceObject())
	require.NoError(t, err)
	require.False(t, hasKey(out, "internal_cost"), "hak tulis tidak membuka hak baca")

	variants := map[string]any{"internalCost": 1, "INTERNAL-COST": 2, "number": 3}
	err = readOnly.CheckWrite(ref, variants)
	require.ErrorAs(t, err, &denied)
	require.Equal(t, []string{"INTERNAL-COST", "internalCost"}, denied.Fields)
	require.False(t, errors.Is(err, domain.ErrFieldScopeMismatch))
}

func TestDeniedFieldsAreCappedAndSorted(t *testing.T) {
	org := id.NewID()
	f := fieldFilter(t, domain.EffectivePermissions{}, ptr(org), nil)
	obj := make(map[string]any, 200)
	for i := 0; i < 200; i++ {
		obj["\u00e9"+strconv.Itoa(i)] = i
	}

	err := f.CheckWrite(invoiceRef(ptr(org), nil), obj)
	var denied *domain.FieldDeniedError
	require.ErrorAs(t, err, &denied)
	require.Len(t, denied.Fields, domain.MaxDeniedFields)
	require.Equal(t, 200, denied.Total)
	require.True(t, slices.IsSorted(denied.Fields))
}

func TestNestedValuesAreNotFilteredAndCopyIsShallow(t *testing.T) {
	org := id.NewID()
	f := fieldFilter(t, domain.EffectivePermissions{}, ptr(org), nil)
	inner := map[string]any{"internal_cost": 1}
	obj := map[string]any{"number": 1, "details": inner, "lines": []any{map[string]any{"margin": 2}}}

	out, err := f.Apply(invoiceRef(ptr(org), nil), obj)
	require.NoError(t, err)
	require.True(t, hasKey(out, "details"))
	nested, ok := out["details"].(map[string]any)
	require.True(t, ok)
	require.True(t, hasKey(nested, "internal_cost"), "keterbatasan ADR-0022 butir 39: hanya kunci tingkat atas yang ditapis")
	nested["touched"] = true
	require.True(t, hasKey(inner, "touched"), "salinan dangkal: nilai bersarang berbagi referensi")
}

func TestFieldFilterIsSafeForConcurrentUse(t *testing.T) {
	org := id.NewID()
	f := fieldFilter(t, fieldGrants(t, domain.LevelOrganization, codeMarginRead), ptr(org), nil)
	ref := invoiceRef(ptr(org), nil)

	const workers = 8
	errs := make([]error, workers)
	leaked := make([]bool, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				out, err := f.Apply(ref, invoiceObject())
				if err != nil {
					errs[w] = err
					return
				}
				if hasKey(out, "internal_cost") || !hasKey(out, "margin") {
					leaked[w] = true
				}
				if werr := f.CheckWrite(ref, map[string]any{"internal_cost": 1}); werr == nil {
					leaked[w] = true
				}
			}
		}()
	}
	wg.Wait()
	for w := 0; w < workers; w++ {
		require.NoError(t, errs[w])
		require.False(t, leaked[w])
	}
}

func TestHasRestrictionsReflectsDeclaredCodesOnly(t *testing.T) {
	require.True(t, fieldRules(t).HasRestrictions())

	none, err := domain.NewFieldRules("invoice", []string{"invoice.read", "other.field.cost.read"})
	require.NoError(t, err)
	require.False(t, none.HasRestrictions(), "kode biasa dan field tipe lain tidak membatasi tipe ini")

	var zero domain.FieldRules
	require.False(t, zero.HasRestrictions())
}
