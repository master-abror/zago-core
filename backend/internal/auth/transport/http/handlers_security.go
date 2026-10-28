package authhttp

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// Dua password hingga 256 byte masing-masing bisa menjadi ~6 byte per karakter setelah escape JSON.
const passwordMaxBodyBytes = 8 << 10

// ChangePasswordRequest adalah body POST /security/password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePasswordResponse adalah data respons POST /security/password. Sesi dirotasi: cookie baru
// diset dan token CSRF lama gugur, jadi klien wajib memakai csrf_token ini.
type ChangePasswordResponse struct {
	CSRFToken            string    `json:"csrf_token"`
	ExpiresAt            time.Time `json:"expires_at"`
	RevokedOtherSessions int       `json:"revoked_other_sessions"`
}

// ChangePassword godoc
//
//	@Summary		Ganti password (pemanggil sendiri)
//	@Description	Memverifikasi password saat ini, menerapkan kebijakan password, mengganti hash, merotasi sesi saat ini (cookie dan csrf_token baru), dan mencabut semua sesi lain. Password saat ini yang salah = 401 invalid_credentials dan tidak ada yang berubah (sesi tetap berlaku). Tanpa Idempotency-Key (ADR-0020 butir 42).
//	@Tags			security
//	@Accept			json
//	@Produce		json
//	@Param			X-CSRF-Token	header		string							true	"Token CSRF sesi"
//	@Param			body			body		authhttp.ChangePasswordRequest	true	"Password saat ini dan password baru"
//	@Success		200				{object}	httpx.Envelope[authhttp.ChangePasswordResponse]
//	@Failure		400				{object}	httpx.ErrorEnvelope	"validation_failed (new_password: too_short | too_long | same_as_current)"
//	@Failure		401				{object}	httpx.ErrorEnvelope	"authentication_required | invalid_credentials (password saat ini salah)"
//	@Failure		403				{object}	httpx.ErrorEnvelope	"csrf_failed"
//	@Failure		429				{object}	httpx.ErrorEnvelope	"rate_limit_exceeded"
//	@Router			/api/v1/security/password [post]
func (h *Handlers) ChangePassword(w http.ResponseWriter, r *http.Request) {
	id, ok := domain.IdentityFromContext(r.Context())
	if !ok {
		h.rs.Error(w, r, httpx.AuthenticationRequired.New())
		return
	}
	var req ChangePasswordRequest
	if err := httpx.DecodeJSON(w, r, &req, passwordMaxBodyBytes); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	var f httpx.Fields
	if req.CurrentPassword == "" {
		f.Add("current_password", "required", "Current password is required.")
	}
	if req.NewPassword == "" {
		f.Add("new_password", "required", "New password is required.")
	}
	if err := f.Err(); err != nil {
		h.rs.Error(w, r, err)
		return
	}

	res, err := h.svc.ChangePassword(r.Context(), id, service.ChangePasswordInput{Current: req.CurrentPassword, New: req.NewPassword})
	if err != nil {
		h.fail(w, r, err) // tanpa cookie baru: sesi lama tetap berlaku
		return
	}
	h.ck.Set(w, res.Issued.Token, res.Issued.Session.ExpiresAt, h.now())
	noStore(w)
	h.rs.OK(w, r, ChangePasswordResponse{
		CSRFToken: h.csrf.Token(res.Issued.Session.ID), ExpiresAt: res.Issued.Session.ExpiresAt, RevokedOtherSessions: res.RevokedOthers,
	}, nil)
}

// SecurityEventView adalah satu baris riwayat pada GET /security/events. metadata dan request_id
// sengaja tidak dipublikasikan (ADR-0020 butir 46).
type SecurityEventView struct {
	ID             uuid.UUID  `json:"id"`
	EventType      string     `json:"event_type"`
	Result         string     `json:"result"`
	CreatedAt      time.Time  `json:"created_at"`
	IPAddress      string     `json:"ip_address"`
	UserAgent      *string    `json:"user_agent"`
	OrganizationID *uuid.UUID `json:"organization_id"`
	SessionID      *uuid.UUID `json:"session_id"`
}

// ListSecurityEvents godoc
//
//	@Summary		Riwayat event keamanan milik pengguna
//	@Description	Hanya event milik pemanggil, terbaru dulu, paginasi cursor (meta.pagination). Tidak ada parameter pengguna.
//	@Tags			security
//	@Produce		json
//	@Param			limit	query		int		false	"Jumlah baris (bawaan 50, maksimum 100)"
//	@Param			cursor	query		string	false	"Cursor dari meta.pagination.next_cursor"
//	@Success		200		{object}	httpx.Envelope[[]authhttp.SecurityEventView]
//	@Failure		400		{object}	httpx.ErrorEnvelope	"validation_failed (termasuk cursor rusak)"
//	@Failure		401		{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Router			/api/v1/security/events [get]
func (h *Handlers) ListSecurityEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := domain.IdentityFromContext(r.Context())
	if !ok {
		h.rs.Error(w, r, httpx.AuthenticationRequired.New())
		return
	}
	pr, err := httpx.ParsePage(r, httpx.PageOptions{})
	if err != nil {
		h.rs.Error(w, r, err)
		return
	}
	hash := httpx.FilterHash(nil) // tanpa filter di v1.0
	var before *service.EventPosition
	if pr.Cursor != "" {
		pos, err := h.cursors.Decode(pr.Cursor, httpx.Desc, hash)
		if err != nil {
			h.rs.Error(w, r, err)
			return
		}
		before = &service.EventPosition{CreatedAt: pos.CreatedAt, ID: pos.ID}
	}

	rows, err := h.svc.ListSecurityEvents(r.Context(), id, pr.FetchLimit(), before)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	views := make([]SecurityEventView, 0, len(rows))
	for _, e := range rows {
		views = append(views, SecurityEventView{
			ID: e.ID, EventType: e.Type, Result: e.Result, CreatedAt: e.CreatedAt, IPAddress: e.IPAddress,
			UserAgent: e.UserAgent, OrganizationID: e.OrganizationID, SessionID: e.SessionID,
		})
	}
	page, meta := httpx.Paginate(views, pr.Limit, h.cursors, httpx.Desc, hash, func(v SecurityEventView) httpx.Position {
		return httpx.Position{CreatedAt: v.CreatedAt, ID: v.ID}
	})
	noStore(w)
	h.rs.OK(w, r, page, meta)
}
