package authzhttp

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/permission"
)

// PermissionSimulation adalah jalur simulasi yang dibutuhkan handler (dipenuhi
// application.Simulator). Simulator TIDAK memeriksa permission.simulate pemanggil; itu tugas
// Authorize di rute (ADR-0022 butir 69).
type PermissionSimulation interface {
	Simulate(ctx context.Context, g application.Grantor, in application.SimulateInput) (application.SimulationResult, error)
}

// SimulateHandlers melayani POST /permissions/simulate (docs/06 §10-§12). Pemanggil SELALU
// diturunkan dari Identity dan ScopeContext; organisasi simulasi tidak pernah diterima dari body,
// path, atau query.
type SimulateHandlers struct {
	rs  *httpx.Responder
	sim PermissionSimulation
}

// NewSimulateHandlers membuat SimulateHandlers; semua dependensi wajib ada.
func NewSimulateHandlers(rs *httpx.Responder, sim PermissionSimulation) (*SimulateHandlers, error) {
	if rs == nil || sim == nil {
		return nil, errors.New("authzhttp: SimulateHandlers membutuhkan Responder dan PermissionSimulation")
	}
	return &SimulateHandlers{rs: rs, sim: sim}, nil
}

// Mount memasang rute pada r. authorize adalah (*Middleware).Authorize; rute memasang
// permission.simulate SENDIRI. r harus sudah melewati Authenticate, CSRF, RequireAuth, dan Scope
// (ADR-0022 butir 25). Panik bila authorize nil (rute tanpa penjaga tidak boleh terpasang diam-diam).
// Rute ini tidak memakai Idempotency-Key: simulasi tidak mengubah state (docs/06 §11 hanya
// mewajibkannya untuk POST /role-assignments).
func (h *SimulateHandlers) Mount(r chi.Router, authorize func(action string) func(http.Handler) http.Handler) {
	if authorize == nil {
		panic("authzhttp: Mount simulate membutuhkan authorize")
	}
	r.With(authorize(permission.PermissionSimulate)).Post("/permissions/simulate", h.Simulate)
}

// Simulate godoc
//
//	@Summary		Simulasikan keputusan otorisasi untuk satu user
//	@Description	Menjalankan Authorizer.Can sungguhan untuk user target di organisasi pemanggil. Target harus anggota aktif organisasi pemanggil; selain itu galat seragam 400 (field user_id, not_found). Pemanggil level group hanya menyimulasikan anggota grup aktifnya. Pemanggil di scope platform ditolak (403 scope_mismatch). policy_code dan deferred_policies hanya terisi bila pemanggil juga memegang policy.read.
//	@Tags			authorization
//	@Accept			json
//	@Produce		json
//	@Param			X-CSRF-Token	header		string						true	"Token CSRF sesi"
//	@Param			body			body		authzhttp.SimulateRequest	true	"Pertanyaan simulasi"
//	@Success		200				{object}	httpx.Envelope[authzhttp.SimulateResponse]
//	@Failure		400				{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401				{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403				{object}	httpx.ErrorEnvelope	"permission_denied, scope_mismatch, csrf_failed"
//	@Router			/api/v1/permissions/simulate [post]
func (h *SimulateHandlers) Simulate(w http.ResponseWriter, r *http.Request) {
	g, ok := h.caller(w, r)
	if !ok {
		return
	}
	var body SimulateRequest
	if err := httpx.DecodeJSON(w, r, &body, 0); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	if err := body.Validate(); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	res, err := h.sim.Simulate(r.Context(), g, body.Input())
	if err != nil {
		h.rs.Error(w, r, DomainError(err))
		return
	}
	h.rs.OK(w, r, NewSimulateResponse(res), nil)
}

// caller menurunkan Grantor dari Identity dan ScopeContext. Authorize sudah menjamin keduanya
// ada; bila hilang, rute dirakit tanpa middleware yang benar: Identity hilang 401, Scope hilang 500.
func (h *SimulateHandlers) caller(w http.ResponseWriter, r *http.Request) (application.Grantor, bool) {
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
