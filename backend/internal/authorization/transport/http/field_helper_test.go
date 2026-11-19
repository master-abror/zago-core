package authzhttp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	authzhttp "github.com/master-abror/zago-core/backend/internal/authorization/transport/http"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/pkg/id"
)

const fhCostRead = "invoice.field.internal_cost.read"

const fhCostWrite = "invoice.field.internal_cost.write"

const fhMarginRead = "invoice.field.margin.read"

// fhInvoice adalah masukan bertipe yang didekode setelah pemeriksaan kunci mentah.
type fhInvoice struct {
	Number string `json:"number"`

	InternalCost *int `json:"internal_cost"`

	Margin *float64 `json:"margin"`
}

// fhSource adalah FieldFilterSource palsu: ia menyerahkan filter nyata (domain) yang dibangun
// dari kode yang dipegang, dan mencatat pemanggilannya.
type fhSource struct {
	filter domain.FieldFilter

	err error

	calls int

	gotType string

	gotIdentity authdomain.Identity

	gotScope domain.ScopeContext
}

func (s *fhSource) For(_ context.Context, ident authdomain.Identity, scope domain.ScopeContext, resourceType string) (domain.FieldFilter, error) {
	s.calls++
	s.gotType = resourceType
	s.gotIdentity = ident
	s.gotScope = scope
	if s.err != nil {
		return domain.FieldFilter{}, s.err
	}
	return s.filter, nil
}

type fhRig struct {
	src *fhSource

	helper *authzhttp.FieldHelper

	rs *httpx.Responder

	logs *bytes.Buffer

	org uuid.UUID

	ident authdomain.Identity

	scope domain.ScopeContext

	max int64

	contentType string
}

// newFhRig membuat rig dengan filter untuk tipe invoice (internal_cost read+write dan margin
// read terbatas) dan pemanggil yang memegang kode held pada level organisasi.
func newFhRig(t *testing.T, held ...string) *fhRig {
	t.Helper()
	org := id.NewID()
	rules, err := domain.NewFieldRules("invoice", []string{fhCostRead, fhCostWrite, fhMarginRead, "invoice.read"})
	require.NoError(t, err)
	grants := make(map[string]domain.DecisionLevel, len(held))
	for _, code := range held {
		grants[code] = domain.LevelOrganization
	}
	eff, err := domain.EffectiveFromGrants(grants)
	require.NoError(t, err)
	scope, err := domain.NewScope(&org, nil)
	require.NoError(t, err)
	filter, err := domain.NewFieldFilter(rules, eff, scope)
	require.NoError(t, err)

	logs := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(logs, nil))
	src := &fhSource{filter: filter}
	helper, err := authzhttp.NewFieldHelper(src, log)
	require.NoError(t, err)
	return &fhRig{
		src:    src,
		helper: helper,
		rs:     httpx.NewResponder(slog.New(slog.NewTextHandler(io.Discard, nil))),
		logs:   logs,
		org:    org,
		ident:  authdomain.Identity{UserID: id.NewID(), OrganizationID: &org, SessionID: id.NewID(), AuthLevel: authdomain.AuthLevelPassword},
		scope:  scope,
	}
}

func (g *fhRig) ref() domain.ResourceRef {
	org := g.org
	return domain.ResourceRef{Type: "invoice", ID: id.NewID(), OrganizationID: &org}
}

func (g *fhRig) callCtx(withIdentity, withScope bool) context.Context {
	ctx := context.Background()
	if withIdentity {
		ctx = authdomain.WithIdentity(ctx, g.ident)
	}
	if withScope {
		ctx = domain.WithScope(ctx, g.scope)
	}
	return ctx
}

// postWith menjalankan handler kecil yang memakai DecodeWrite lalu menjawab 204 bila lolos.
func (g *fhRig) postWith(body string, ref domain.ResourceRef, withIdentity, withScope bool) (*httptest.ResponseRecorder, *fhInvoice) {
	dst := &fhInvoice{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		if err := g.helper.DecodeWrite(w, r, ref, dst, g.max); err != nil {
			g.rs.Error(w, r, err)
			return
		}
		g.rs.NoContent(w)
	}
	req := httptest.NewRequest(http.MethodPost, "/invoices", strings.NewReader(body))
	contentType := "application/json"
	if g.contentType != "" {
		contentType = g.contentType
	}
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()
	handler(w, req.WithContext(g.callCtx(withIdentity, withScope)))
	return w, dst
}

func (g *fhRig) post(body string) (*httptest.ResponseRecorder, *fhInvoice) {
	return g.postWith(body, g.ref(), true, true)
}

func (g *fhRig) read(obj map[string]any, ref domain.ResourceRef, withIdentity bool) (map[string]any, error) {
	req := httptest.NewRequest(http.MethodGet, "/invoices/x", nil)
	return g.helper.FilterRead(req.WithContext(g.callCtx(withIdentity, true)), ref, obj)
}

func (g *fhRig) logRecords(t *testing.T) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(g.logs.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), line)
		out = append(out, rec)
	}
	return out
}

func fhRequireDenied(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Equal(t, authzhttp.FieldPermissionDenied.Name, errCode(t, w))
}

func TestNewFieldHelperRequiresItsDependencies(t *testing.T) {
	log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	_, err := authzhttp.NewFieldHelper(nil, log)
	require.Error(t, err)

	_, err = authzhttp.NewFieldHelper(&fhSource{}, nil)
	require.Error(t, err)

	h, err := authzhttp.NewFieldHelper(&fhSource{}, log)
	require.NoError(t, err)
	require.NotNil(t, h)
}

func TestFieldHelperDecodesUnrestrictedWritesAndAsksForTheCallersFilter(t *testing.T) {
	g := newFhRig(t)

	w, dst := g.post(`{"number":"INV-1","margin":0.5}`)

	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	require.Equal(t, "INV-1", dst.Number)
	require.NotNil(t, dst.Margin)
	require.InDelta(t, 0.5, *dst.Margin, 0.0001)
	require.Equal(t, 1, g.src.calls)
	require.Equal(t, "invoice", g.src.gotType)
	require.Equal(t, g.ident, g.src.gotIdentity, "Identity hanya dari context")
	require.Equal(t, g.scope, g.src.gotScope, "Scope hanya dari context")
	require.Empty(t, g.logRecords(t), "penulisan yang lolos tidak dicatat sebagai penolakan")
}

func TestFieldHelperChecksTheRawKeysBeforeDecodingIntoTheStruct(t *testing.T) {
	// Setiap bentuk kunci berikut dipetakan ke field internal_cost oleh dekoder atau pencocokan
	// ringkas; pemeriksaan kunci mentah harus menolaknya SEBELUM decode (yang akan menjawab 400
	// unknown_field untuk bentuk yang tidak dikenal struct dan karenanya menyembunyikan penolakan).
	keys := []string{
		`"internal_cost"`,
		`"Internal_Cost"`,
		`"INTERNAL-COST"`,
		`"internalCost"`,
		`"internal cost"`,
		`"internal\u005fcost"`,
		`"internal_co\u017ft"`,
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			g := newFhRig(t)

			w, dst := g.post(`{"number":"INV-1",` + key + `:1}`)

			fhRequireDenied(t, w)
			require.NotEmpty(t, w.Body.String(), "tes tidak boleh lulus karena body kosong")
			require.NotContains(t, w.Body.String(), "internal", "nama field tidak digaungkan")
			require.Empty(t, dst.Number, "decode ke struct tidak boleh terjadi sebelum penolakan")
		})
	}
}

func TestFieldHelperTreatsANullValueOnARestrictedKeyAsAWrite(t *testing.T) {
	g := newFhRig(t)

	w, dst := g.post(`{"number":"INV-1","internal_cost":null}`)

	fhRequireDenied(t, w)
	require.Empty(t, dst.Number)
}

func TestFieldHelperAllowsRestrictedWritesWhenTheWriteCodeIsHeld(t *testing.T) {
	g := newFhRig(t, fhCostWrite)

	w, dst := g.post(`{"number":"INV-1","internal_cost":250000}`)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	require.NotNil(t, dst.InternalCost)
	require.Equal(t, 250000, *dst.InternalCost)

	// Variasi huruf dipetakan dekoder ke field yang sama dan sama-sama sah bila hak dipegang.
	w, dst = g.post(`{"Internal_Cost":7}`)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	require.NotNil(t, dst.InternalCost)
	require.Equal(t, 7, *dst.InternalCost)
}

func TestFieldHelperDoesNotLetTheReadCodeStandInForTheWriteCode(t *testing.T) {
	g := newFhRig(t, fhCostRead)

	w, _ := g.post(`{"internal_cost":1}`)

	fhRequireDenied(t, w)
}

func TestFieldHelperRejectsUnknownFieldsButOnlyAfterTheRestrictedCheck(t *testing.T) {
	g := newFhRig(t)

	w, _ := g.post(`{"nope":1}`)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	require.Equal(t, httpx.ValidationFailed.Name, errCode(t, w))
	require.Contains(t, w.Body.String(), "unknown_field")

	w, _ = g.post(`{"nope":1,"internal_cost":1}`)
	fhRequireDenied(t, w)
}

func TestFieldHelperRejectsBodiesThatAreNotASingleJSONObjectWithoutAskingForAFilter(t *testing.T) {
	bodies := []string{``, `[]`, `"x"`, `5`, `null`, `{`, `{"number":"a"} {"number":"b"}`}
	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			g := newFhRig(t)

			w, dst := g.post(body)

			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Equal(t, httpx.ValidationFailed.Name, errCode(t, w))
			require.Equal(t, 0, g.src.calls, "body cacat ditolak sebelum menyentuh penyimpanan")
			require.Empty(t, dst.Number)
		})
	}
}

func TestFieldHelperKeepsTheTransportRulesOfDecodeJSON(t *testing.T) {
	g := newFhRig(t)
	g.contentType = "text/plain"
	w, _ := g.post(`{"number":"a"}`)
	require.Equal(t, http.StatusUnsupportedMediaType, w.Code, w.Body.String())
	require.Equal(t, httpx.UnsupportedMediaType.Name, errCode(t, w))

	g = newFhRig(t)
	g.max = 16
	w, _ = g.post(`{"number":"` + strings.Repeat("a", 100) + `"}`)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code, w.Body.String())
	require.Equal(t, httpx.PayloadTooLarge.Name, errCode(t, w))
	require.Equal(t, 0, g.src.calls)
}

func TestFieldHelperFailsClosedWhenTheFilterCannotBeBuilt(t *testing.T) {
	g := newFhRig(t)
	g.src.err = errors.New("pg down at 10.0.0.5")
	w, dst := g.post(`{"number":"INV-1"}`)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), "pg down", "penyebab tidak sampai ke klien")
	require.Empty(t, dst.Number, "tanpa filter tidak ada decode: tidak pernah menjadi izin")

	g = newFhRig(t)
	g.src.err = domain.ErrFieldScopeMismatch
	w, _ = g.post(`{"number":"INV-1"}`)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Equal(t, authzhttp.ScopeMismatch.Name, errCode(t, w))

	g = newFhRig(t)
	g.src.err = domain.ErrInvalidFieldRules
	w, _ = g.post(`{"number":"INV-1"}`)
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	require.Equal(t, httpx.InternalError.Name, errCode(t, w))
}

func TestFieldHelperRejectsAResourceOutsideTheScopeOrOfAnotherType(t *testing.T) {
	g := newFhRig(t)
	other := id.NewID()
	foreign := domain.ResourceRef{Type: "invoice", ID: id.NewID(), OrganizationID: &other}
	w, dst := g.postWith(`{"number":"INV-1"}`, foreign, true, true)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Equal(t, authzhttp.ScopeMismatch.Name, errCode(t, w), "organisasi sumber daya bukan organisasi scope")
	require.Empty(t, dst.Number)

	// Tipe sumber daya yang tidak sama dengan tipe aturan adalah kesalahan pemrograman (500).
	g = newFhRig(t)
	org := g.org
	wrongType := domain.ResourceRef{Type: "bill", ID: id.NewID(), OrganizationID: &org}
	w, _ = g.postWith(`{"number":"INV-1"}`, wrongType, true, true)
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

func TestFieldHelperRequiresIdentityAndScopeFromTheContext(t *testing.T) {
	g := newFhRig(t)
	w, _ := g.postWith(`{"number":"INV-1"}`, g.ref(), false, true)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.Equal(t, httpx.AuthenticationRequired.Name, errCode(t, w))
	require.Equal(t, 0, g.src.calls)

	g = newFhRig(t)
	w, _ = g.postWith(`{"number":"INV-1"}`, g.ref(), true, false)
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	require.Equal(t, httpx.InternalError.Name, errCode(t, w), "tanpa middleware Scope")
	require.Equal(t, 0, g.src.calls)
}

func TestFieldHelperLogsDeniedNamesSanitizedAndTruncated(t *testing.T) {
	g := newFhRig(t)
	long := strings.Repeat("é", 100)
	body, err := json.Marshal(map[string]any{"x\u0000é": 1, long: 2})
	require.NoError(t, err)

	w, _ := g.post(string(body))

	fhRequireDenied(t, w)
	require.NotContains(t, w.Body.String(), "é", "nama kunci klien tidak masuk respons")
	records := g.logRecords(t)
	require.Len(t, records, 1)
	rec := records[0]
	require.Equal(t, "invoice", rec["resource_type"])
	require.Equal(t, g.ident.UserID.String(), rec["user_id"])
	require.EqualValues(t, 2, rec["denied_total"])
	require.Equal(t, []any{"x?é", strings.Repeat("é", 64) + "..."}, rec["denied_fields"])
}

func TestFieldHelperLogsAtMostTenDeniedNamesButReportsTheTotal(t *testing.T) {
	g := newFhRig(t)
	obj := make(map[string]any, 12)
	for i := 1; i <= 12; i++ {
		obj[fmt.Sprintf("é%02d", i)] = i
	}
	body, err := json.Marshal(obj)
	require.NoError(t, err)

	w, _ := g.post(string(body))

	fhRequireDenied(t, w)
	records := g.logRecords(t)
	require.Len(t, records, 1)
	require.EqualValues(t, 12, records[0]["denied_total"])
	names, ok := records[0]["denied_fields"].([]any)
	require.True(t, ok)
	require.Len(t, names, 10)
}

func TestFieldHelperFilterReadDropsRestrictedFieldsWithoutTheirReadCodes(t *testing.T) {
	obj := map[string]any{"number": "INV-1", "internal_cost": 250000, "margin": 0.2}

	g := newFhRig(t)
	out, err := g.read(obj, g.ref(), true)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"number": "INV-1"}, out)
	require.Contains(t, obj, "margin", "masukan tidak diubah")
	require.Contains(t, obj, "internal_cost")

	g = newFhRig(t, fhCostRead)
	out, err = g.read(obj, g.ref(), true)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"number": "INV-1", "internal_cost": 250000}, out)
	require.Equal(t, 1, g.src.calls)
	require.Equal(t, "invoice", g.src.gotType)
}

func TestFieldHelperFilterReadFailsClosed(t *testing.T) {
	obj := map[string]any{"number": "INV-1", "internal_cost": 250000}

	g := newFhRig(t)
	out, err := g.read(obj, g.ref(), false)
	require.Nil(t, out)
	require.True(t, httpx.HasCode(err, httpx.AuthenticationRequired))

	g = newFhRig(t)
	g.src.err = errors.New("redis down")
	out, err = g.read(obj, g.ref(), true)
	require.Nil(t, out, "galat tidak pernah mengembalikan objek penuh")
	require.True(t, httpx.HasCode(err, httpx.ServiceUnavailable))

	g = newFhRig(t)
	org := g.org
	wrongType := domain.ResourceRef{Type: "bill", ID: id.NewID(), OrganizationID: &org}
	out, err = g.read(obj, wrongType, true)
	require.Nil(t, out)
	require.True(t, httpx.HasCode(err, httpx.InternalError))

	g = newFhRig(t)
	other := id.NewID()
	foreign := domain.ResourceRef{Type: "invoice", ID: id.NewID(), OrganizationID: &other}
	out, err = g.read(obj, foreign, true)
	require.Nil(t, out)
	require.True(t, httpx.HasCode(err, authzhttp.ScopeMismatch))
}
