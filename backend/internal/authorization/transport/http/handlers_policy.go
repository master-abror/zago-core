package authzhttp

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/permission"
)

// PolicyReads adalah jalur baca policy yang dibutuhkan handler (dipenuhi repository.PolicyReader).
// organizationID berasal dari ScopeContext; nil berarti scope platform (hanya policy global).
type PolicyReads interface {
	List(ctx context.Context, organizationID *uuid.UUID, q domain.PolicyListQuery) (domain.PolicyPage, error)
}

// PolicyManagement adalah jalur tulis policy (dipenuhi application.PolicyWriter). Penulis TIDAK
// memeriksa permission pemanggil; itu tugas Authorize di rute (ADR-0022 butir 59).
type PolicyManagement interface {
	Create(ctx context.Context, g application.Grantor, in application.CreatePolicyInput) (domain.PolicyRecord, error)
	Update(ctx context.Context, g application.Grantor, policyID uuid.UUID, in application.UpdatePolicyInput) (domain.PolicyRecord, error)
}

// PolicyHandlers melayani /policies (docs/06 §11). Grantor SELALU diturunkan dari Identity dan
// ScopeContext; cakupan policy (organisasi atau global) tidak pernah diterima dari body, path,
// atau query.
type PolicyHandlers struct {
	rs    *httpx.Responder
	reads PolicyReads
	write PolicyManagement
}

// NewPolicyHandlers membuat PolicyHandlers; semua dependensi wajib ada.
func NewPolicyHandlers(rs *httpx.Responder, reads PolicyReads, write PolicyManagement) (*PolicyHandlers, error) {
	if rs == nil || reads == nil || write == nil {
		return nil, errors.New("authzhttp: PolicyHandlers membutuhkan Responder, PolicyReads, dan PolicyManagement")
	}
	return &PolicyHandlers{rs: rs, reads: reads, write: write}, nil
}

// Mount memasang rute pada r. authorize adalah (*Middleware).Authorize. Setiap rute memasang
// aksinya SENDIRI: policy.read untuk GET, policy.manage untuk POST dan PATCH. r harus sudah
// melewati Authenticate, CSRF, RequireAuth, dan Scope (ADR-0022 butir 25). Panik bila authorize
// nil (rute tanpa penjaga tidak boleh terpasang diam-diam). Rute policy tidak memakai
// Idempotency-Key (docs/06 §11 hanya mewajibkannya untuk POST /role-assignments; kunci unik kode
// melindungi POST /policies).
func (h *PolicyHandlers) Mount(r chi.Router, authorize func(action string) func(http.Handler) http.Handler) {
	if authorize == nil {
		panic("authzhttp: Mount policy membutuhkan authorize")
	}
	r.With(authorize(permission.PolicyRead)).Get("/policies", h.ListPolicies)
	r.With(authorize(permission.PolicyManage)).Post("/policies", h.CreatePolicy)
	r.With(authorize(permission.PolicyManage)).Patch("/policies/{id}", h.UpdatePolicy)
}

// caller menurunkan Grantor dari Identity dan ScopeContext. Authorize sudah menjamin keduanya
// ada; bila hilang, rute dirakit tanpa middleware yang benar: Identity hilang 401, Scope hilang 500.
func (h *PolicyHandlers) caller(w http.ResponseWriter, r *http.Request) (application.Grantor, bool) {
	ident, ok := authdomain.IdentityFromContext(r.Context())
	if !ok {
		h.rs.Error(w, r, httpx.AuthenticationRequired.New())
		return application.Grantor{}, false
	}
	scope, ok := domain.ScopeFromContext(r.Context())
	if !ok {
		h.rs.Error(w, r, httpx.InternalError.New())
		return application.Grantor{}, false
	}
	return application.Grantor{UserID: ident.UserID, Scope: scope}, true
}

func (h *PolicyHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.rs.Error(w, r, DomainError(err))
}

// ListPolicies godoc
//
//	@Summary		Daftar policy yang terlihat bagi pemanggil
//	@Description	Pemanggil tenant melihat policy organisasinya dan policy global (global=true, hanya-baca); scope platform hanya policy global. Bawaan memuat policy aktif dan nonaktif.
//	@Tags			authorization
//	@Produce		json
//	@Param			status		query		string	false	"all (bawaan), active, atau disabled"
//	@Param			page		query		int		false	"Nomor halaman (bawaan 1)"
//	@Param			per_page	query		int		false	"Ukuran halaman (bawaan 25, maksimum 100)"
//	@Success		200			{object}	httpx.Envelope[[]authzhttp.PolicyResponse]
//	@Failure		400			{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401			{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403			{object}	httpx.ErrorEnvelope	"permission_denied"
//	@Router			/api/v1/policies [get]
func (h *PolicyHandlers) ListPolicies(w http.ResponseWriter, r *http.Request) {
	g, ok := h.caller(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var bad httpx.Fields
	status := q.Get("status")
	if status == "" {
		status = domain.PolicyFilterAll
	} else if !domain.ValidPolicyFilter(status) {
		bad.Add("status", "invalid", "status must be all, active, or disabled")
	}
	page, perPage := parsePaging(q, &bad)
	if bad.Len() > 0 {
		h.rs.Error(w, r, bad.Err())
		return
	}

	result, err := h.reads.List(r.Context(), g.Scope.OrganizationID, domain.PolicyListQuery{Status: status, Window: windowFor(page, perPage)})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items := make([]PolicyResponse, 0, len(result.Items))
	for _, rec := range result.Items {
		items = append(items, NewPolicyResponse(rec))
	}
	h.rs.OK(w, r, items, paginationMeta(page, perPage, result.Total))
}

// CreatePolicy godoc
//
//	@Summary		Buat policy
//	@Description	Pemanggil tenant membuat policy organisasinya; pemanggil platform membuat policy global; pemanggil dengan grup aktif ditolak. Policy hanya membatasi (deny menolak, allow menjadi syarat tambahan); aksi policy.* tidak dapat dibatasi. Batas 50 policy aktif per cakupan.
//	@Tags			authorization
//	@Accept			json
//	@Produce		json
//	@Param			X-CSRF-Token	header		string							true	"Token CSRF sesi"
//	@Param			body			body		authzhttp.CreatePolicyRequest	true	"Policy baru"
//	@Success		201				{object}	httpx.Envelope[authzhttp.PolicyResponse]
//	@Failure		400				{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401				{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403				{object}	httpx.ErrorEnvelope	"permission_denied, scope_mismatch, csrf_failed"
//	@Failure		409				{object}	httpx.ErrorEnvelope	"policy_code_taken, policy_limit_reached"
//	@Router			/api/v1/policies [post]
func (h *PolicyHandlers) CreatePolicy(w http.ResponseWriter, r *http.Request) {
	g, ok := h.caller(w, r)
	if !ok {
		return
	}
	var body CreatePolicyRequest
	if err := httpx.DecodeJSON(w, r, &body, 0); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	if err := body.Validate(); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	rec, err := h.write.Create(r.Context(), g, body.Input())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.Created(w, r, NewPolicyResponse(rec), nil)
}

// UpdatePolicy godoc
//
//	@Summary		Ubah sebagian policy
//	@Description	Field yang tidak dikirim tidak diubah; kode dan cakupan tidak dapat diubah. Menonaktifkan selalu boleh; mengaktifkan kembali tunduk pada batas 50. Policy global dari pemanggil tenant sama dengan tidak ada (404).
//	@Tags			authorization
//	@Accept			json
//	@Produce		json
//	@Param			id				path		string							true	"ID policy"
//	@Param			X-CSRF-Token	header		string							true	"Token CSRF sesi"
//	@Param			body			body		authzhttp.UpdatePolicyRequest	true	"Perubahan"
//	@Success		200				{object}	httpx.Envelope[authzhttp.PolicyResponse]
//	@Failure		400				{object}	httpx.ErrorEnvelope	"validation_failed (termasuk empty_patch)"
//	@Failure		401				{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403				{object}	httpx.ErrorEnvelope	"permission_denied, scope_mismatch, csrf_failed"
//	@Failure		404				{object}	httpx.ErrorEnvelope	"resource_not_found"
//	@Failure		409				{object}	httpx.ErrorEnvelope	"policy_limit_reached"
//	@Router			/api/v1/policies/{id} [patch]
func (h *PolicyHandlers) UpdatePolicy(w http.ResponseWriter, r *http.Request) {
	g, ok := h.caller(w, r)
	if !ok {
		return
	}
	pid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || pid == uuid.Nil {
		h.fail(w, r, domain.ErrPolicyNotFound)
		return
	}
	var body UpdatePolicyRequest
	if err := httpx.DecodeJSON(w, r, &body, 0); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	if err := body.Validate(); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	rec, err := h.write.Update(r.Context(), g, pid, body.Input())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.OK(w, r, NewPolicyResponse(rec), nil)
}
