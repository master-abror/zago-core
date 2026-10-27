package authhttp

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// SessionView adalah satu sesi aktif pada GET /security/sessions. Tidak ada token maupun hash token.
type SessionView struct {
	ID             uuid.UUID  `json:"id"`
	CreatedAt      time.Time  `json:"created_at"`
	LastActivityAt time.Time  `json:"last_activity_at"`
	ExpiresAt      time.Time  `json:"expires_at"`
	IPAddress      string     `json:"ip_address"`
	UserAgent      *string    `json:"user_agent"`
	OrganizationID *uuid.UUID `json:"organization_id"`
	AuthLevel      string     `json:"auth_level"`
	Current        bool       `json:"current"`
}

// SessionsResponse adalah data respons GET /security/sessions.
type SessionsResponse struct {
	Sessions []SessionView `json:"sessions"`
}

// ListSessions godoc
//
//	@Summary	Sesi aktif milik pengguna
//	@Tags		security
//	@Produce	json
//	@Success	200	{object}	httpx.Envelope[authhttp.SessionsResponse]
//	@Failure	401	{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Router		/api/v1/security/sessions [get]
func (h *Handlers) ListSessions(w http.ResponseWriter, r *http.Request) {
	id, ok := domain.IdentityFromContext(r.Context())
	if !ok {
		h.rs.Error(w, r, httpx.AuthenticationRequired.New())
		return
	}
	list, err := h.svc.ListSessions(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := SessionsResponse{Sessions: make([]SessionView, 0, len(list))}
	for _, s := range list {
		out.Sessions = append(out.Sessions, SessionView{
			ID: s.ID, CreatedAt: s.CreatedAt, LastActivityAt: s.LastActivityAt, ExpiresAt: s.ExpiresAt,
			IPAddress: s.IPAddress, UserAgent: s.UserAgent, OrganizationID: s.OrganizationID,
			AuthLevel: s.AuthLevel, Current: s.Current,
		})
	}
	noStore(w)
	h.rs.OK(w, r, out, nil)
}

// RevokeSession godoc
//
//	@Summary		Cabut satu sesi milik pengguna
//	@Description	Boleh mencabut sesi saat ini (setara logout; cookie ikut dihapus). Sesi yang tidak ada, milik orang lain, atau sudah dicabut = 404.
//	@Tags			security
//	@Param			id				path	string	true	"ID sesi"
//	@Param			X-CSRF-Token	header	string	true	"Token CSRF sesi"
//	@Success		204
//	@Failure		401	{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403	{object}	httpx.ErrorEnvelope	"csrf_failed"
//	@Failure		404	{object}	httpx.ErrorEnvelope	"resource_not_found"
//	@Router			/api/v1/security/sessions/{id} [delete]
func (h *Handlers) RevokeSession(w http.ResponseWriter, r *http.Request) {
	id, ok := domain.IdentityFromContext(r.Context())
	if !ok {
		h.rs.Error(w, r, httpx.AuthenticationRequired.New())
		return
	}
	target, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.rs.Error(w, r, httpx.ResourceNotFound.New()) // ID cacat tidak dibedakan dari ID tak dikenal
		return
	}
	if err := h.svc.RevokeSession(r.Context(), id, target); err != nil {
		h.fail(w, r, err)
		return
	}
	if target == id.SessionID {
		h.ck.Clear(w)
	}
	h.rs.NoContent(w)
}

// RevokeOthersResponse adalah data respons POST /security/sessions/revoke-others.
type RevokeOthersResponse struct {
	Revoked int `json:"revoked"`
}

// RevokeOtherSessions godoc
//
//	@Summary	Cabut semua sesi lain milik pengguna
//	@Tags		security
//	@Produce	json
//	@Param		X-CSRF-Token	header		string	true	"Token CSRF sesi"
//	@Success	200				{object}	httpx.Envelope[authhttp.RevokeOthersResponse]
//	@Failure	401				{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure	403				{object}	httpx.ErrorEnvelope	"csrf_failed"
//	@Router		/api/v1/security/sessions/revoke-others [post]
func (h *Handlers) RevokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	id, ok := domain.IdentityFromContext(r.Context())
	if !ok {
		h.rs.Error(w, r, httpx.AuthenticationRequired.New())
		return
	}
	n, err := h.svc.RevokeOtherSessions(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.OK(w, r, RevokeOthersResponse{Revoked: n}, nil)
}
