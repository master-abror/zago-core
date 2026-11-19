package authzhttp_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/repository"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/testkit"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

const fhdbMarginWrite = "invoice.field.margin.write"

var _ authzhttp.FieldFilterSource = (*application.FieldGuard)(nil)

// fhdbRig memasang FieldHelper di atas FieldGuard SUNGGUHAN (Authorizer dan definisi field dari
// PostgreSQL). Katalog core belum punya kode field, jadi definisi berasal dari kode fixture di
// tabel permissions (invoice.field.*), seperti tes FieldGuard di paket application.
type fhdbRig struct {
	helper *authzhttp.FieldHelper

	rs *httpx.Responder

	scope domain.ScopeContext

	declare func(code string)

	alice authdomain.Identity

	bob authdomain.Identity

	carol authdomain.Identity
}

// newFhdbRig membuat dua organisasi. alice (organisasi utama) memegang internal_cost read dan
// write pada level organisasi; bob anggota organisasi yang sama tanpa hak apa pun; carol
// anggota organisasi lain. margin.read dideklarasikan (terbatas) dan tidak dipegang siapa pun.
func newFhdbRig(t *testing.T) *fhdbRig {
	t.Helper()
	db := testkit.NewDB(t)
	org := testkit.NewOrganization(t, db)
	other := testkit.NewOrganization(t, db)
	alice := testkit.NewUser(t, db, testkit.InOrganization(org))
	bob := testkit.NewUser(t, db, testkit.InOrganization(org))
	carol := testkit.NewUser(t, db, testkit.InOrganization(other))
	oid, otherID := org.ID, other.ID

	costRole := testkit.RoleWith(t, db, []string{fhCostRead, fhCostWrite})
	testkit.AssignRole(t, db, testkit.AssignmentSpec{
		Role: costRole, SubjectType: "user", SubjectID: alice.ID,
		ScopeType: "organization", OrganizationID: &oid,
	})
	testkit.EnsurePermission(t, db, fhMarginRead)

	authz, err := application.NewAuthorizer(repository.NewEffective(db.App), nil)
	require.NoError(t, err)
	guard, err := application.NewFieldGuard(authz, repository.NewFields(db.App))
	require.NoError(t, err)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	helper, err := authzhttp.NewFieldHelper(guard, log)
	require.NoError(t, err)

	return &fhdbRig{
		helper:  helper,
		rs:      httpx.NewResponder(log),
		scope:   domain.ScopeContext{OrganizationID: &oid},
		declare: func(code string) { testkit.EnsurePermission(t, db, code) },
		alice:   authdomain.Identity{UserID: alice.ID, OrganizationID: &oid},
		bob:     authdomain.Identity{UserID: bob.ID, OrganizationID: &oid},
		carol:   authdomain.Identity{UserID: carol.ID, OrganizationID: &otherID},
	}
}

func (g *fhdbRig) ref(resourceType string) domain.ResourceRef {
	return domain.ResourceRef{Type: resourceType, ID: id.NewID(), OrganizationID: g.scope.OrganizationID}
}

func (g *fhdbRig) request(method string, body string, ident authdomain.Identity) *http.Request {
	req := httptest.NewRequest(method, "/x", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx := domain.WithScope(authdomain.WithIdentity(context.Background(), ident), g.scope)
	return req.WithContext(ctx)
}

func (g *fhdbRig) post(ident authdomain.Identity, resourceType, body string) (*httptest.ResponseRecorder, *fhInvoice) {
	dst := &fhInvoice{}
	req := g.request(http.MethodPost, body, ident)
	w := httptest.NewRecorder()
	if err := g.helper.DecodeWrite(w, req, g.ref(resourceType), dst, 0); err != nil {
		g.rs.Error(w, req, err)
		return w, dst
	}
	g.rs.NoContent(w)
	return w, dst
}

func (g *fhdbRig) read(ident authdomain.Identity, obj map[string]any) (map[string]any, error) {
	return g.helper.FilterRead(g.request(http.MethodGet, "", ident), g.ref("invoice"), obj)
}

func TestFieldHelperWithPostgresEnforcesTheWriteCodeOfTheCaller(t *testing.T) {
	g := newFhdbRig(t)

	w, dst := g.post(g.bob, "invoice", `{"number":"INV-1"}`)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	require.Equal(t, "INV-1", dst.Number, "field tidak terbatas boleh ditulis siapa pun")

	for _, body := range []string{`{"number":"X","internal_cost":1}`, `{"number":"X","Internal-Cost":1}`} {
		w, dst = g.post(g.bob, "invoice", body)
		fhRequireDenied(t, w)
		require.NotContains(t, w.Body.String(), "internal", "nama field tidak digaungkan")
		require.Empty(t, dst.Number, "tanpa hak tulis tidak ada decode ke struct")
	}

	w, dst = g.post(g.alice, "invoice", `{"number":"INV-2","internal_cost":5}`)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	require.NotNil(t, dst.InternalCost)
	require.Equal(t, 5, *dst.InternalCost)

	// Tipe tanpa field terbatas tidak dibatasi sama sekali, juga untuk pemanggil tanpa hak.
	w, dst = g.post(g.bob, "bill", `{"number":"B-1"}`)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	require.Equal(t, "B-1", dst.Number)
}

func TestFieldHelperWithPostgresAppliesNewDeclarationsImmediately(t *testing.T) {
	g := newFhdbRig(t)

	// margin baru dibatasi untuk BACA: menulisnya belum terbatas.
	w, _ := g.post(g.bob, "invoice", `{"margin":0.5}`)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	g.declare(fhdbMarginWrite)

	for name, ident := range map[string]authdomain.Identity{"bob": g.bob, "alice": g.alice} {
		w, dst := g.post(ident, "invoice", `{"number":"X","margin":0.5}`)
		fhRequireDenied(t, w)
		require.Empty(t, dst.Number, "%s tidak memegang hak tulis margin yang baru dideklarasikan", name)
	}
}

func TestFieldHelperWithPostgresFilterReadHidesWhatTheCallerCannotRead(t *testing.T) {
	g := newFhdbRig(t)
	obj := map[string]any{"number": "INV-1", "internal_cost": 250000, "margin": 0.2}

	out, err := g.read(g.alice, obj)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"number": "INV-1", "internal_cost": 250000}, out, "margin.read terbatas dan tidak dipegang")

	out, err = g.read(g.bob, obj)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"number": "INV-1"}, out)
	require.Contains(t, obj, "margin", "masukan tidak diubah")
}

func TestFieldHelperWithPostgresRejectsAnIdentityOfAnotherOrganization(t *testing.T) {
	g := newFhdbRig(t)

	w, dst := g.post(g.carol, "invoice", `{"number":"X"}`)

	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Equal(t, authzhttp.ScopeMismatch.Name, errCode(t, w))
	require.Empty(t, dst.Number)

	out, err := g.read(g.carol, map[string]any{"number": "INV-1"})
	require.Nil(t, out)
	require.True(t, httpx.HasCode(err, authzhttp.ScopeMismatch))
}
