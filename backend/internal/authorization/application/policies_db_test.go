package application_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/master-abror/zago-core/backend/internal/audit"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	"github.com/master-abror/zago-core/backend/internal/kernel"
	"github.com/master-abror/zago-core/backend/internal/policy"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/internal/testpg"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

// Tes PolicyWriter memakai PostgreSQL 18 sungguhan dan role app_user. PolicyWriter tidak memeriksa
// permission pemanggil (itu pekerjaan middleware Authorize di endpoint), jadi pemanggil di sini
// tidak perlu memegang role apa pun.

var _ application.PolicyStore = (*repository.Policies)(nil)

type polEnv struct {
	db     *testpg.DB
	org    testkit.Organization
	admin  testkit.User
	grant  application.Grantor
	writer *application.PolicyWriter
}

func polWriter(t *testing.T, db *testpg.DB) *application.PolicyWriter {
	t.Helper()
	w, err := application.NewPolicyWriter(kernel.NewTxManager(db.App, nil), repository.NewPolicies(db.App),
		audit.New(db.App, nil), kernel.NewBus(db.App, nil))
	require.NoError(t, err)
	return w
}

func newPolEnv(t *testing.T) polEnv {
	t.Helper()
	var e polEnv
	e.db = testkit.NewDB(t)
	e.org = testkit.NewOrganization(t, e.db)
	e.admin = testkit.NewUser(t, e.db, testkit.InOrganization(e.org))
	scopeOrg := e.org.ID
	e.grant = application.Grantor{UserID: e.admin.ID, Scope: domain.ScopeContext{OrganizationID: &scopeOrg}}
	e.writer = polWriter(t, e.db)
	return e
}

// platform memberi pemanggil platform (tanpa organisasi) untuk database yang sama.
func (e polEnv) platform(t *testing.T) application.Grantor {
	t.Helper()
	return application.Grantor{UserID: testkit.NewUser(t, e.db).ID}
}

// otherOrg memberi pemanggil tenant dari organisasi lain.
func (e polEnv) otherOrg(t *testing.T) application.Grantor {
	t.Helper()
	org := testkit.NewOrganization(t, e.db)
	user := testkit.NewUser(t, e.db, testkit.InOrganization(org))
	scopeOrg := org.ID
	return application.Grantor{UserID: user.ID, Scope: domain.ScopeContext{OrganizationID: &scopeOrg}}
}

// grouped memberi pemanggil organisasi ini dengan grup aktif.
func (e polEnv) grouped(t *testing.T) application.Grantor {
	t.Helper()
	grp := testkit.NewGroup(t, e.db, e.org, e.admin)
	scopeOrg, groupID := e.org.ID, grp.ID
	return application.Grantor{UserID: e.admin.ID, Scope: domain.ScopeContext{OrganizationID: &scopeOrg, ActiveGroupID: &groupID}}
}

func (e polEnv) active(t *testing.T) int {
	t.Helper()
	org := e.org.ID
	return polActive(t, e.db, &org)
}

func polActive(t *testing.T, db *testpg.DB, org *uuid.UUID) int {
	t.Helper()
	return count(t, db, `SELECT count(*) FROM policies WHERE status = 'active' AND organization_id IS NOT DISTINCT FROM $1::uuid`, org)
}

func polTotal(t *testing.T, db *testpg.DB) int {
	t.Helper()
	return count(t, db, `SELECT count(*) FROM policies`)
}

func polInput(code string) application.CreatePolicyInput {
	return application.CreatePolicyInput{Code: code, Name: "Policy " + code, Effect: policy.EffectDeny, Expression: `resource.status == "paid"`, Actions: []string{"user.update"}}
}

// polSeedActive menyisipkan n policy aktif langsung ke tabel (org nil = global) untuk mencapai
// batas jumlah tanpa n kali melewati PolicyWriter.
func polSeedActive(t *testing.T, db *testpg.DB, org *uuid.UUID, n int) {
	t.Helper()
	awExec(t, db, `INSERT INTO policies (organization_id, name, code, effect, expression, status, actions)
		SELECT $1::uuid, 'seed ' || g, 'seed.' || g, 'deny', 'true', 'active', ARRAY['role.manage']
		  FROM generate_series(1, $2::int) AS g`, org, n)
}

// create memberi operasi Create (dengan kode tertentu) untuk dijalankan serentak lewat polRace.
func (e polEnv) create(g application.Grantor, code string) func() error {
	return func() error {
		_, err := e.writer.Create(ctx, g, polInput(code))
		return err
	}
}

func polRace(fns ...func() error) []error {
	errs := make([]error, len(fns))
	var wg sync.WaitGroup
	for i, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = fn()
		}()
	}
	wg.Wait()
	return errs
}

// polOneWinner memastikan tepat satu operasi lolos dan yang lain ditolak batas jumlah.
func polOneWinner(t *testing.T, errs []error) {
	t.Helper()
	ok, limit := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, policy.ErrTooManyPolicies):
			limit++
		default:
			require.NoError(t, err, "hanya sukses atau batas jumlah yang diperkirakan")
		}
	}
	require.Equal(t, 1, ok, "tepat satu lolos")
	require.Equal(t, len(errs)-1, limit, "sisanya ditolak batas jumlah")
}

func TestPolicyWriterCreatesAnOrganizationPolicy(t *testing.T) {
	e := newPolEnv(t)
	in := polInput("freeze.paid")
	in.Name = "NamaRahasia"
	in.Expression = `resource.status == "ekspresirahasia"`
	in.Actions = []string{"user.update", "role.manage"}

	rec, err := e.writer.Create(ctx, e.grant, in)
	require.NoError(t, err)
	require.False(t, rec.Global())
	require.NotNil(t, rec.OrganizationID)
	require.Equal(t, e.org.ID, *rec.OrganizationID)
	require.Equal(t, domain.PolicyActive, rec.Status)
	require.Equal(t, []string{"role.manage", "user.update"}, rec.Actions, "actions tersimpan terurut")
	require.Equal(t, 1, e.active(t))
	require.Equal(t, 1, awActs(t, e.db, "policy.create", "success"))
	require.Equal(t, 1, awEvts(t, e.db, "authz.policy.changed"))

	meta := text(t, e.db, `SELECT metadata::text FROM activities WHERE action = 'policy.create'`)
	require.Contains(t, meta, "freeze.paid")
	require.NotContains(t, meta, "NamaRahasia", "nama ketikan operator tidak masuk audit")
	require.NotContains(t, meta, "ekspresirahasia", "ekspresi tidak masuk audit")
	payload := text(t, e.db, `SELECT payload::text FROM outbox_events WHERE event_name = 'authz.policy.changed'`)
	require.Contains(t, payload, "created")
	require.NotContains(t, payload, "ekspresirahasia")

	orgID := e.org.ID
	loaded, err := repository.NewPolicies(e.db.App).ForAction(ctx, &orgID, "user.update")
	require.NoError(t, err)
	require.Len(t, loaded, 1, "policy yang ditulis dimuat engine untuk aksinya")
	require.Equal(t, "freeze.paid", loaded[0].Code)
	require.Equal(t, policy.EffectDeny, loaded[0].Effect)
}

func TestPolicyWriterCreatesAGlobalPolicyForPlatformCallers(t *testing.T) {
	e := newPolEnv(t)
	rec, err := e.writer.Create(ctx, e.platform(t), polInput("global.freeze"))
	require.NoError(t, err)
	require.True(t, rec.Global())
	require.Nil(t, rec.OrganizationID)
	require.Equal(t, 1, count(t, e.db, `SELECT count(*) FROM policies WHERE organization_id IS NULL AND code = 'global.freeze'`))
	require.Equal(t, 0, e.active(t), "policy global tidak dihitung pada organisasi")

	orgID := e.org.ID
	loaded, err := repository.NewPolicies(e.db.App).ForAction(ctx, &orgID, "user.update")
	require.NoError(t, err)
	require.Len(t, loaded, 1, "policy global berlaku di organisasi mana pun")
	require.Equal(t, 1, awActs(t, e.db, "policy.create", "success"))
}

func TestPolicyWriterCreateCanStartDisabled(t *testing.T) {
	e := newPolEnv(t)
	in := polInput("start.off")
	in.Status = domain.PolicyDisabled
	rec, err := e.writer.Create(ctx, e.grant, in)
	require.NoError(t, err)
	require.Equal(t, domain.PolicyDisabled, rec.Status)
	require.Equal(t, 0, e.active(t))

	orgID := e.org.ID
	loaded, err := repository.NewPolicies(e.db.App).ForAction(ctx, &orgID, "user.update")
	require.NoError(t, err)
	require.Empty(t, loaded, "policy nonaktif tidak pernah dimuat engine")
}

func TestPolicyWriterValidatesActionsAgainstCoreAndModuleCodes(t *testing.T) {
	e := newPolEnv(t)
	insertModulePermission(t, e.db, "billing", "billing.invoice", "export")

	exact := polInput("module.exact")
	exact.Actions = []string{"billing.invoice.export"}
	_, err := e.writer.Create(ctx, e.grant, exact)
	require.NoError(t, err, "kode modul dibaca dari tabel permissions")

	wild := polInput("module.wild")
	wild.Actions = []string{"billing.invoice.*", "role.*"}
	_, err = e.writer.Create(ctx, e.grant, wild)
	require.NoError(t, err)

	for i, actions := range [][]string{{"billing.invoice.ghost"}, {"ghost.thing.*"}, {"invoice.read"}} {
		bad := polInput("module.bad" + string(rune('a'+i)))
		bad.Actions = actions
		_, err = e.writer.Create(ctx, e.grant, bad)
		require.ErrorIs(t, err, policy.ErrUnknownAction, "%v", actions)
	}
	require.Equal(t, 2, polTotal(t, e.db), "yang ditolak tidak ditulis")
}

func TestPolicyWriterRejectsInvalidDrafts(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(in *application.CreatePolicyInput)
		want   error
	}{
		{"kode", func(in *application.CreatePolicyInput) { in.Code = "Bad Code" }, policy.ErrInvalidPolicyCode},
		{"nama", func(in *application.CreatePolicyInput) { in.Name = "" }, policy.ErrInvalidName},
		{"efek", func(in *application.CreatePolicyInput) { in.Effect = policy.Effect("maybe") }, policy.ErrInvalidEffect},
		{"ekspresi", func(in *application.CreatePolicyInput) { in.Expression = "(((" }, policy.ErrSyntax},
		{"actions kosong", func(in *application.CreatePolicyInput) { in.Actions = nil }, policy.ErrInvalidActions},
		{"actions ganda", func(in *application.CreatePolicyInput) { in.Actions = []string{"user.update", "user.update"} }, policy.ErrInvalidActions},
		{"actions tak dikenal", func(in *application.CreatePolicyInput) { in.Actions = []string{"invoice.read"} }, policy.ErrUnknownAction},
	}
	e := newPolEnv(t)
	for _, c := range cases {
		in := polInput("invalid.case")
		c.mutate(&in)
		_, err := e.writer.Create(ctx, e.grant, in)
		require.ErrorIs(t, err, c.want, c.name)
	}
	require.Equal(t, 0, polTotal(t, e.db))
	require.Equal(t, 0, awActs(t, e.db, "policy.create", "success"))
	require.Equal(t, 0, awEvts(t, e.db, "authz.policy.changed"))
}

func TestPolicyWriterProtectsPolicyManagementFromPolicies(t *testing.T) {
	e := newPolEnv(t)
	for _, actions := range [][]string{{"policy.manage"}, {"policy.*"}, {"role.manage", "policy.read"}} {
		in := polInput("lockout.try")
		in.Actions = actions
		_, err := e.writer.Create(ctx, e.grant, in)
		require.ErrorIs(t, err, domain.ErrProtectedPolicyAction, "%v", actions)
	}
	require.Equal(t, 3, awActs(t, e.db, "policy.create", "denied"), "penolakan kebijakan diaudit")
	require.Equal(t, 0, polTotal(t, e.db))

	ok, err := e.writer.Create(ctx, e.grant, polInput("lockout.fine"))
	require.NoError(t, err)
	protected := []string{"role.manage", "policy.manage"}
	_, err = e.writer.Update(ctx, e.grant, ok.ID, application.UpdatePolicyInput{Actions: protected})
	require.ErrorIs(t, err, domain.ErrProtectedPolicyAction, "juga lewat perubahan")
	require.Equal(t, 1, awActs(t, e.db, "policy.update", "denied"))
	require.Equal(t, 0, awActs(t, e.db, "policy.update", "success"))
}

func TestPolicyWriterRejectsDuplicateCodesAndRollsBack(t *testing.T) {
	e := newPolEnv(t)
	_, err := e.writer.Create(ctx, e.grant, polInput("dup.code"))
	require.NoError(t, err)
	_, err = e.writer.Create(ctx, e.grant, polInput("dup.code"))
	require.ErrorIs(t, err, domain.ErrPolicyCodeTaken)
	require.Equal(t, 1, polTotal(t, e.db))
	require.Equal(t, 1, awActs(t, e.db, "policy.create", "success"), "yang gagal tidak meninggalkan audit sukses")
	require.Equal(t, 1, awEvts(t, e.db, "authz.policy.changed"), "dan tidak meninggalkan event")

	_, err = e.writer.Create(ctx, e.platform(t), polInput("dup.code"))
	require.NoError(t, err, "kode yang sama pada policy global sah")
	_, err = e.writer.Create(ctx, e.platform(t), polInput("dup.code"))
	require.ErrorIs(t, err, domain.ErrPolicyCodeTaken, "kode ganda pada cakupan global")
	_, err = e.writer.Create(ctx, e.otherOrg(t), polInput("dup.code"))
	require.NoError(t, err, "kode yang sama di organisasi lain sah")
}

func TestPolicyWriterEnforcesTheOrganizationLimit(t *testing.T) {
	e := newPolEnv(t)
	org := e.org.ID
	polSeedActive(t, e.db, &org, policy.MaxPoliciesPerOrganization-1)

	_, err := e.writer.Create(ctx, e.grant, polInput("limit.fiftieth"))
	require.NoError(t, err, "policy aktif ke-50 masih sah")
	_, err = e.writer.Create(ctx, e.grant, polInput("limit.fifty_first"))
	require.ErrorIs(t, err, policy.ErrTooManyPolicies)
	require.Equal(t, policy.MaxPoliciesPerOrganization, e.active(t))

	off := polInput("limit.disabled")
	off.Status = domain.PolicyDisabled
	_, err = e.writer.Create(ctx, e.grant, off)
	require.NoError(t, err, "policy nonaktif tidak dihitung")

	_, err = e.writer.Create(ctx, e.platform(t), polInput("limit.global"))
	require.NoError(t, err, "batas organisasi tidak menghalangi policy global")
	_, err = e.writer.Create(ctx, e.otherOrg(t), polInput("limit.other"))
	require.NoError(t, err, "organisasi lain punya batas sendiri")
}

func TestPolicyWriterEnforcesTheGlobalLimit(t *testing.T) {
	e := newPolEnv(t)
	polSeedActive(t, e.db, nil, policy.MaxGlobalPolicies-1)
	platform := e.platform(t)

	_, err := e.writer.Create(ctx, platform, polInput("limit.fiftieth"))
	require.NoError(t, err)
	_, err = e.writer.Create(ctx, platform, polInput("limit.fifty_first"))
	require.ErrorIs(t, err, policy.ErrTooManyPolicies)
	require.Equal(t, policy.MaxGlobalPolicies, polActive(t, e.db, nil))
	require.Equal(t, 0, e.active(t))
}

func TestPolicyWriterUpdatesAPolicyAndAuditsTheChange(t *testing.T) {
	e := newPolEnv(t)
	rec, err := e.writer.Create(ctx, e.grant, polInput("upd.base"))
	require.NoError(t, err)

	name, desc, effect, expr := "Nama baru", "", policy.EffectAllow, `resource.status == "open"`
	upd, err := e.writer.Update(ctx, e.grant, rec.ID, application.UpdatePolicyInput{Name: &name, Description: &desc, Effect: &effect, Expression: &expr, Actions: []string{"role.read", "role.manage"}})
	require.NoError(t, err)
	require.Equal(t, rec.ID, upd.ID)
	require.Equal(t, "upd.base", upd.Code, "kode tidak berubah")
	require.Equal(t, "Nama baru", upd.Name)
	require.Equal(t, policy.EffectAllow, upd.Effect)
	require.Equal(t, `resource.status == "open"`, upd.Expression)
	require.Equal(t, []string{"role.manage", "role.read"}, upd.Actions)
	require.Equal(t, domain.PolicyActive, upd.Status)

	require.Equal(t, "Nama baru", text(t, e.db, `SELECT name FROM policies WHERE id = $1`, rec.ID))
	require.Equal(t, 1, awActs(t, e.db, "policy.update", "success"))
	require.Equal(t, 2, awEvts(t, e.db, "authz.policy.changed"), "created dan updated")
	meta := text(t, e.db, `SELECT metadata::text FROM activities WHERE action = 'policy.update'`)
	require.Contains(t, meta, "changed")
	require.Contains(t, meta, "expression")
	require.NotContains(t, meta, "Nama baru")
	require.NotContains(t, meta, "resource.status")
}

func TestPolicyWriterUpdateWithoutEffectWritesNothing(t *testing.T) {
	e := newPolEnv(t)
	rec, err := e.writer.Create(ctx, e.grant, polInput("noop.base"))
	require.NoError(t, err)

	same := rec.Name
	got, err := e.writer.Update(ctx, e.grant, rec.ID, application.UpdatePolicyInput{Name: &same, Actions: []string{"user.update"}})
	require.NoError(t, err)
	require.True(t, got.UpdatedAt.Equal(rec.UpdatedAt), "tidak ada tulis")
	require.Equal(t, 0, awActs(t, e.db, "policy.update", "success"))
	require.Equal(t, 1, awEvts(t, e.db, "authz.policy.changed"), "hanya event created")
}

func TestPolicyWriterAlwaysAllowsDisablingButRevalidatesOnEnableAndEdit(t *testing.T) {
	e := newPolEnv(t)
	stale := id.NewID()
	awExec(t, e.db, `INSERT INTO policies (id, organization_id, name, code, effect, expression, status, actions)
		VALUES ($1, $2, 'stale', 'stale.one', 'deny', 'true', 'active', ARRAY['ghost.gone'])`, stale, e.org.ID)

	off, on := domain.PolicyDisabled, domain.PolicyActive
	_, err := e.writer.Update(ctx, e.grant, stale, application.UpdatePolicyInput{Status: &off})
	require.NoError(t, err, "policy yang actions-nya sudah hilang dari katalog tetap dapat dinonaktifkan")
	require.Equal(t, 0, e.active(t))

	_, err = e.writer.Update(ctx, e.grant, stale, application.UpdatePolicyInput{Status: &on})
	require.ErrorIs(t, err, policy.ErrUnknownAction, "mengaktifkan kembali divalidasi penuh")
	name := "baru"
	_, err = e.writer.Update(ctx, e.grant, stale, application.UpdatePolicyInput{Name: &name})
	require.ErrorIs(t, err, policy.ErrUnknownAction, "mengubah isi divalidasi penuh")
	require.Equal(t, domain.PolicyDisabled, text(t, e.db, `SELECT status FROM policies WHERE id = $1`, stale))
	require.Equal(t, "stale", text(t, e.db, `SELECT name FROM policies WHERE id = $1`, stale))

	fix := []string{"role.manage"}
	_, err = e.writer.Update(ctx, e.grant, stale, application.UpdatePolicyInput{Actions: fix, Status: &on})
	require.NoError(t, err, "memperbaiki actions sekaligus mengaktifkan lolos validasi")
	require.Equal(t, 1, e.active(t))
}

func TestPolicyWriterCountsReactivationAgainstTheLimit(t *testing.T) {
	e := newPolEnv(t)
	org := e.org.ID
	polSeedActive(t, e.db, &org, policy.MaxPoliciesPerOrganization-1)
	last, err := e.writer.Create(ctx, e.grant, polInput("react.last"))
	require.NoError(t, err)
	off := polInput("react.off")
	off.Status = domain.PolicyDisabled
	parked, err := e.writer.Create(ctx, e.grant, off)
	require.NoError(t, err)

	on, disabled := domain.PolicyActive, domain.PolicyDisabled
	_, err = e.writer.Update(ctx, e.grant, parked.ID, application.UpdatePolicyInput{Status: &on})
	require.ErrorIs(t, err, policy.ErrTooManyPolicies, "pada batas, mengaktifkan kembali ditolak")
	require.Equal(t, policy.MaxPoliciesPerOrganization, e.active(t))

	expr := `resource.status == "open"`
	_, err = e.writer.Update(ctx, e.grant, last.ID, application.UpdatePolicyInput{Expression: &expr})
	require.NoError(t, err, "mengubah policy aktif yang sudah dihitung tidak menambah hitungan")

	_, err = e.writer.Update(ctx, e.grant, last.ID, application.UpdatePolicyInput{Status: &disabled})
	require.NoError(t, err)
	_, err = e.writer.Update(ctx, e.grant, parked.ID, application.UpdatePolicyInput{Status: &on})
	require.NoError(t, err, "setelah ada ruang, aktivasi lolos")
	require.Equal(t, policy.MaxPoliciesPerOrganization, e.active(t))
}

func TestPolicyWriterUpdateRespectsTheScopeOfTheCaller(t *testing.T) {
	e := newPolEnv(t)
	inOrg, err := e.writer.Create(ctx, e.grant, polInput("scope.org"))
	require.NoError(t, err)
	global, err := e.writer.Create(ctx, e.platform(t), polInput("scope.global"))
	require.NoError(t, err)
	hijack := "dibajak"
	patch := application.UpdatePolicyInput{Name: &hijack}

	_, err = e.writer.Update(ctx, e.platform(t), inOrg.ID, patch)
	require.ErrorIs(t, err, domain.ErrPolicyNotFound, "pemanggil platform tidak mengubah policy organisasi")
	_, err = e.writer.Update(ctx, e.grant, global.ID, patch)
	require.ErrorIs(t, err, domain.ErrPolicyNotFound, "pemanggil tenant tidak mengubah policy global")
	_, err = e.writer.Update(ctx, e.otherOrg(t), inOrg.ID, patch)
	require.ErrorIs(t, err, domain.ErrPolicyNotFound, "organisasi lain tidak mengubah policy ini")
	_, err = e.writer.Update(ctx, e.grant, id.NewID(), patch)
	require.ErrorIs(t, err, domain.ErrPolicyNotFound)

	require.Equal(t, 0, count(t, e.db, `SELECT count(*) FROM policies WHERE name = 'dibajak'`))
	require.Equal(t, 0, awActs(t, e.db, "policy.update", "success"))
}

func TestPolicyWriterUpdateRejectsInvalidChangesWithoutWriting(t *testing.T) {
	e := newPolEnv(t)
	rec, err := e.writer.Create(ctx, e.grant, polInput("bad.update"))
	require.NoError(t, err)

	bad := "((("
	_, err = e.writer.Update(ctx, e.grant, rec.ID, application.UpdatePolicyInput{Expression: &bad})
	require.ErrorIs(t, err, policy.ErrSyntax)
	empty := ""
	_, err = e.writer.Update(ctx, e.grant, rec.ID, application.UpdatePolicyInput{Name: &empty})
	require.ErrorIs(t, err, policy.ErrInvalidName)
	_, err = e.writer.Update(ctx, e.grant, rec.ID, application.UpdatePolicyInput{Actions: []string{}})
	require.ErrorIs(t, err, policy.ErrInvalidActions, "irisan kosong bukan 'tidak diubah'")

	require.Equal(t, rec.Expression, text(t, e.db, `SELECT expression FROM policies WHERE id = $1`, rec.ID))
	require.Equal(t, 0, awActs(t, e.db, "policy.update", "success"))
}

func TestPolicyWriterRefusesCallersWithAnActiveGroup(t *testing.T) {
	e := newPolEnv(t)
	rec, err := e.writer.Create(ctx, e.grant, polInput("grp.base"))
	require.NoError(t, err)
	grouped := e.grouped(t)

	_, err = e.writer.Create(ctx, grouped, polInput("grp.try"))
	require.ErrorIs(t, err, domain.ErrTargetOutsideScope, "policy berlaku di seluruh organisasi, bukan hanya grup")
	name := "baru"
	_, err = e.writer.Update(ctx, grouped, rec.ID, application.UpdatePolicyInput{Name: &name})
	require.ErrorIs(t, err, domain.ErrTargetOutsideScope)

	require.Equal(t, 1, polTotal(t, e.db))
	require.Equal(t, 1, awActs(t, e.db, "policy.create", "denied"))
	require.Equal(t, 1, awActs(t, e.db, "policy.update", "denied"))
}

func TestPolicyWriterCreateWaitsForTheOrganizationLock(t *testing.T) {
	e := newPolEnv(t)
	release := awHold(t, e.db, func(c context.Context) error { return repository.LockOrganization(c, e.org.ID) })
	done := make(chan error, 1)
	go func() {
		_, err := e.writer.Create(ctx, e.grant, polInput("wait.org"))
		done <- err
	}()

	awStaysBlocked(t, done, "Create")
	require.Equal(t, 0, polTotal(t, e.db), "belum ada yang ditulis selama kunci ditahan")
	release()
	require.NoError(t, <-done)
	require.Equal(t, 1, polTotal(t, e.db))
}

func TestPolicyWriterUpdateWaitsForTheOrganizationLock(t *testing.T) {
	e := newPolEnv(t)
	rec, err := e.writer.Create(ctx, e.grant, polInput("wait.update"))
	require.NoError(t, err)
	release := awHold(t, e.db, func(c context.Context) error { return repository.LockOrganization(c, e.org.ID) })
	off := domain.PolicyDisabled
	done := make(chan error, 1)
	go func() {
		_, err := e.writer.Update(ctx, e.grant, rec.ID, application.UpdatePolicyInput{Status: &off})
		done <- err
	}()

	awStaysBlocked(t, done, "Update")
	require.Equal(t, 1, e.active(t), "belum berubah selama kunci ditahan")
	release()
	require.NoError(t, <-done)
	require.Equal(t, 0, e.active(t))
}

func TestPolicyWriterGlobalCreateWaitsForThePlatformLock(t *testing.T) {
	e := newPolEnv(t)
	platform := e.platform(t)
	release := awHold(t, e.db, repository.LockPlatform)
	done := make(chan error, 1)
	go func() {
		_, err := e.writer.Create(ctx, platform, polInput("wait.global"))
		done <- err
	}()

	awStaysBlocked(t, done, "Create global")
	require.Equal(t, 0, polTotal(t, e.db))
	release()
	require.NoError(t, <-done)
	require.Equal(t, 1, polActive(t, e.db, nil))
}

func TestPolicyWriterTenantCreateIsNotBlockedByThePlatformLock(t *testing.T) {
	e := newPolEnv(t)
	release := awHold(t, e.db, repository.LockPlatform)
	defer release()
	done := make(chan error, 1)
	go func() {
		_, err := e.writer.Create(ctx, e.grant, polInput("free.org"))
		done <- err
	}()
	select {
	case err := <-done:
		require.NoError(t, err, "pemanggil tenant tidak mengunci platform")
	case <-time.After(10 * time.Second):
		require.FailNow(t, "Create tenant tertahan kunci platform")
	}
}

func TestPolicyWriterSerializesConcurrentCreatesAtTheOrganizationLimit(t *testing.T) {
	e := newPolEnv(t)
	org := e.org.ID
	polSeedActive(t, e.db, &org, policy.MaxPoliciesPerOrganization-1)

	errs := polRace(e.create(e.grant, "race.a"), e.create(e.grant, "race.b"))
	polOneWinner(t, errs)
	require.Equal(t, policy.MaxPoliciesPerOrganization, e.active(t), "tidak pernah melampaui 50")
}

func TestPolicyWriterSerializesConcurrentActivationsAtTheOrganizationLimit(t *testing.T) {
	e := newPolEnv(t)
	org := e.org.ID
	polSeedActive(t, e.db, &org, policy.MaxPoliciesPerOrganization-1)
	var parked [2]domain.PolicyRecord
	for i, code := range []string{"act.a", "act.b"} {
		in := polInput(code)
		in.Status = domain.PolicyDisabled
		rec, err := e.writer.Create(ctx, e.grant, in)
		require.NoError(t, err)
		parked[i] = rec
	}

	on := domain.PolicyActive
	activate := func(rec domain.PolicyRecord) func() error {
		return func() error {
			_, err := e.writer.Update(ctx, e.grant, rec.ID, application.UpdatePolicyInput{Status: &on})
			return err
		}
	}
	polOneWinner(t, polRace(activate(parked[0]), activate(parked[1])))
	require.Equal(t, policy.MaxPoliciesPerOrganization, e.active(t), "tidak pernah melampaui 50")
}

func TestPolicyWriterSerializesConcurrentCreatesAtTheGlobalLimit(t *testing.T) {
	e := newPolEnv(t)
	polSeedActive(t, e.db, nil, policy.MaxGlobalPolicies-1)
	platform := e.platform(t)

	errs := polRace(e.create(platform, "race.a"), e.create(platform, "race.b"))
	polOneWinner(t, errs)
	require.Equal(t, policy.MaxGlobalPolicies, polActive(t, e.db, nil), "tidak pernah melampaui 50")
}
