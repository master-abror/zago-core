package authhttp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/auth/service"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

const (
	loginMaxBodyBytes  = 4 << 10
	switchMaxBodyBytes = 1 << 10
	maxEmailLen        = 254 // batas praktis RFC 5321
)

// Service adalah use case yang dipanggil handler (diimplementasikan service.AuthService).
type Service interface {
	Login(ctx context.Context, in service.LoginInput) (service.LoginResult, error)
	Logout(ctx context.Context, id domain.Identity) error
	Profile(ctx context.Context, userID uuid.UUID) (service.Profile, error)
	Refresh(ctx context.Context, id domain.Identity) (service.Issued, error)
	SwitchOrganization(ctx context.Context, id domain.Identity, orgID uuid.UUID) (service.Issued, error)
	ListSessions(ctx context.Context, id domain.Identity) ([]service.SessionInfo, error)
	RevokeSession(ctx context.Context, id domain.Identity, target uuid.UUID) error
	RevokeOtherSessions(ctx context.Context, id domain.Identity) (int, error)
}

// HandlerDeps adalah dependensi Handlers. Now opsional (bawaan time.Now).
type HandlerDeps struct {
	Service   Service
	Cookie    Cookie
	CSRF      *service.CSRF
	Responder *httpx.Responder
	Now       func() time.Time
}

// Handlers adalah endpoint /auth/login, /auth/logout, /auth/me, dan /auth/csrf. Handler tidak
// pernah membaca cookie: identitas datang dari Middleware (domain.IdentityFromContext), dan satu-
// satunya yang menulis cookie sesi adalah Login/Logout.
type Handlers struct {
	svc  Service
	ck   Cookie
	csrf *service.CSRF
	rs   *httpx.Responder
	now  func() time.Time
}

// NewHandlers membuat Handlers; semua dependensi kecuali Now wajib.
func NewHandlers(d HandlerDeps) (*Handlers, error) {
	switch {
	case d.Service == nil:
		return nil, errors.New("authhttp: HandlerDeps.Service wajib")
	case d.CSRF == nil:
		return nil, errors.New("authhttp: HandlerDeps.CSRF wajib")
	case d.Responder == nil:
		return nil, errors.New("authhttp: HandlerDeps.Responder wajib")
	case d.Cookie.Name() == "":
		return nil, errors.New("authhttp: HandlerDeps.Cookie wajib (pakai NewCookie)")
	}
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Handlers{svc: d.Service, ck: d.Cookie, csrf: d.CSRF, rs: d.Responder, now: now}, nil
}

// LoginRequest adalah body POST /auth/login.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// UserSummary adalah ringkasan pengguna pada respons login.
type UserSummary struct {
	ID          uuid.UUID `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
}

// LoginResponse adalah data respons login. Token sesi TIDAK ada di sini: hanya di cookie HttpOnly.
type LoginResponse struct {
	User           UserSummary `json:"user"`
	OrganizationID *uuid.UUID  `json:"organization_id"`
	CSRFToken      string      `json:"csrf_token"`
	ExpiresAt      time.Time   `json:"expires_at"`
}

// Login godoc
//
//	@Summary		Login dengan email dan password
//	@Description	Sukses: cookie sesi HttpOnly diset dan respons memuat csrf_token untuk request pengubah-state berikutnya. Kegagalan kredensial selalu invalid_credentials; account_suspended/account_locked hanya setelah password benar.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		authhttp.LoginRequest	true	"Kredensial"
//	@Success		200		{object}	httpx.Envelope[authhttp.LoginResponse]
//	@Failure		400		{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401		{object}	httpx.ErrorEnvelope	"invalid_credentials"
//	@Failure		403		{object}	httpx.ErrorEnvelope	"account_suspended | csrf_failed"
//	@Failure		423		{object}	httpx.ErrorEnvelope	"account_locked"
//	@Failure		429		{object}	httpx.ErrorEnvelope	"rate_limit_exceeded"
//	@Router			/api/v1/auth/login [post]
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := httpx.DecodeJSON(w, r, &req, loginMaxBodyBytes); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	var f httpx.Fields
	email := strings.TrimSpace(req.Email)
	switch {
	case email == "":
		f.Add("email", "required", "Email is required.")
	case len(email) > maxEmailLen || !strings.Contains(email, "@"):
		f.Add("email", "invalid", "Email is invalid.")
	}
	if req.Password == "" {
		f.Add("password", "required", "Password is required.")
	}
	if err := f.Err(); err != nil {
		h.rs.Error(w, r, err)
		return
	}

	res, err := h.svc.Login(r.Context(), service.LoginInput{Email: email, Password: req.Password})
	if err != nil {
		h.fail(w, r, err)
		return
	}

	h.ck.Set(w, res.Issued.Token, res.Issued.Session.ExpiresAt, h.now())
	noStore(w)
	h.rs.OK(w, r, LoginResponse{
		User:           UserSummary{ID: res.User.ID, Email: res.User.Email, DisplayName: res.User.DisplayName},
		OrganizationID: res.OrganizationID,
		CSRFToken:      h.csrf.Token(res.Issued.Session.ID),
		ExpiresAt:      res.Issued.Session.ExpiresAt,
	}, nil)
}

// Logout godoc
//
//	@Summary		Logout (mencabut sesi saat ini)
//	@Tags			auth
//	@Param			X-CSRF-Token	header	string	true	"Token CSRF sesi"
//	@Success		204
//	@Failure		401	{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403	{object}	httpx.ErrorEnvelope	"csrf_failed"
//	@Router			/api/v1/auth/logout [post]
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	id, ok := domain.IdentityFromContext(r.Context())
	if !ok {
		h.rs.Error(w, r, httpx.AuthenticationRequired.New())
		return
	}
	if err := h.svc.Logout(r.Context(), id); err != nil {
		h.fail(w, r, err) // cookie tetap: klien boleh mengulang logout
		return
	}
	h.ck.Clear(w)
	h.rs.NoContent(w)
}

// SessionSummary adalah ringkasan sesi saat ini pada GET /auth/me.
type SessionSummary struct {
	ID        uuid.UUID `json:"id"`
	AuthLevel string    `json:"auth_level"`
}

// Profile adalah data diri pengguna pada GET /auth/me.
type Profile struct {
	ID          uuid.UUID  `json:"id"`
	Email       string     `json:"email"`
	DisplayName string     `json:"display_name"`
	Username    *string    `json:"username"`
	Locale      string     `json:"locale"`
	Timezone    string     `json:"timezone"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

// MeResponse adalah data respons GET /auth/me.
type MeResponse struct {
	User           Profile        `json:"user"`
	OrganizationID *uuid.UUID     `json:"organization_id"`
	Session        SessionSummary `json:"session"`
}

// Me godoc
//
//	@Summary	Pengguna dan sesi saat ini
//	@Tags		auth
//	@Produce	json
//	@Success	200	{object}	httpx.Envelope[authhttp.MeResponse]
//	@Failure	401	{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Router		/api/v1/auth/me [get]
func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	id, ok := domain.IdentityFromContext(r.Context())
	if !ok {
		h.rs.Error(w, r, httpx.AuthenticationRequired.New())
		return
	}
	p, err := h.svc.Profile(r.Context(), id.UserID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	noStore(w)
	h.rs.OK(w, r, MeResponse{
		User: Profile{
			ID: p.ID, Email: p.Email, DisplayName: p.DisplayName, Username: p.Username,
			Locale: p.Locale, Timezone: p.Timezone, LastLoginAt: p.LastLoginAt,
		},
		OrganizationID: id.OrganizationID,
		Session:        SessionSummary{ID: id.SessionID, AuthLevel: string(id.AuthLevel)},
	}, nil)
}

// CSRFResponse adalah data respons GET /auth/csrf.
type CSRFResponse struct {
	CSRFToken string `json:"csrf_token"`
}

// CSRF godoc
//
//	@Summary		Token CSRF untuk sesi saat ini
//	@Description	Token terikat ke sesi dan berubah setiap sesi dirotasi; kirim di header X-CSRF-Token pada request pengubah-state.
//	@Tags			auth
//	@Produce		json
//	@Success		200	{object}	httpx.Envelope[authhttp.CSRFResponse]
//	@Failure		401	{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Router			/api/v1/auth/csrf [get]
func (h *Handlers) CSRF(w http.ResponseWriter, r *http.Request) {
	id, ok := domain.IdentityFromContext(r.Context())
	if !ok {
		h.rs.Error(w, r, httpx.AuthenticationRequired.New())
		return
	}
	noStore(w)
	h.rs.OK(w, r, CSRFResponse{CSRFToken: h.csrf.Token(id.SessionID)}, nil)
}

// RefreshResponse adalah data respons POST /auth/refresh. Token CSRF lama tidak berlaku lagi
// (terikat ke sesi lama): klien wajib memakai yang baru.
type RefreshResponse struct {
	CSRFToken string    `json:"csrf_token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Refresh godoc
//
//	@Summary		Ganti token sesi (rotasi)
//	@Description	Mengganti cookie sesi dan token CSRF; idle clock mulai ulang, batas absolut sesi TIDAK bertambah (ADR-0020).
//	@Tags			auth
//	@Produce		json
//	@Param			X-CSRF-Token	header		string	true	"Token CSRF sesi"
//	@Success		200				{object}	httpx.Envelope[authhttp.RefreshResponse]
//	@Failure		401				{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403				{object}	httpx.ErrorEnvelope	"csrf_failed"
//	@Router			/api/v1/auth/refresh [post]
func (h *Handlers) Refresh(w http.ResponseWriter, r *http.Request) {
	id, ok := domain.IdentityFromContext(r.Context())
	if !ok {
		h.rs.Error(w, r, httpx.AuthenticationRequired.New())
		return
	}
	iss, err := h.svc.Refresh(r.Context(), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.ck.Set(w, iss.Token, iss.Session.ExpiresAt, h.now())
	noStore(w)
	h.rs.OK(w, r, RefreshResponse{CSRFToken: h.csrf.Token(iss.Session.ID), ExpiresAt: iss.Session.ExpiresAt}, nil)
}

// SwitchOrganizationRequest adalah body POST /auth/switch-organization.
type SwitchOrganizationRequest struct {
	OrganizationID string `json:"organization_id"`
}

// SwitchOrganizationResponse adalah data respons POST /auth/switch-organization.
type SwitchOrganizationResponse struct {
	OrganizationID uuid.UUID `json:"organization_id"`
	CSRFToken      string    `json:"csrf_token"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// SwitchOrganization godoc
//
//	@Summary		Pindah organisasi aktif sesi
//	@Description	Hanya ke organisasi tempat pengguna menjadi anggota aktif. Token sesi dan token CSRF diganti.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			X-CSRF-Token	header		string							true	"Token CSRF sesi"
//	@Param			body			body		authhttp.SwitchOrganizationRequest	true	"Organisasi tujuan"
//	@Success		200				{object}	httpx.Envelope[authhttp.SwitchOrganizationResponse]
//	@Failure		400				{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401				{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403				{object}	httpx.ErrorEnvelope	"permission_denied (bukan anggota aktif) | csrf_failed"
//	@Router			/api/v1/auth/switch-organization [post]
func (h *Handlers) SwitchOrganization(w http.ResponseWriter, r *http.Request) {
	id, ok := domain.IdentityFromContext(r.Context())
	if !ok {
		h.rs.Error(w, r, httpx.AuthenticationRequired.New())
		return
	}
	var req SwitchOrganizationRequest
	if err := httpx.DecodeJSON(w, r, &req, switchMaxBodyBytes); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	orgID, err := uuid.Parse(strings.TrimSpace(req.OrganizationID))
	if err != nil || orgID == uuid.Nil {
		var f httpx.Fields
		if strings.TrimSpace(req.OrganizationID) == "" {
			f.Add("organization_id", "required", "Organization is required.")
		} else {
			f.Add("organization_id", "invalid", "Organization ID is invalid.")
		}
		h.rs.Error(w, r, f.Err())
		return
	}

	iss, err := h.svc.SwitchOrganization(r.Context(), id, orgID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.ck.Set(w, iss.Token, iss.Session.ExpiresAt, h.now())
	noStore(w)
	h.rs.OK(w, r, SwitchOrganizationResponse{
		OrganizationID: orgID, CSRFToken: h.csrf.Token(iss.Session.ID), ExpiresAt: iss.Session.ExpiresAt,
	}, nil)
}

// fail memetakan kesalahan layanan ke kode error API. Kesalahan tak dikenal diteruskan apa
// adanya: Responder menjadikannya 500 internal_error tanpa membocorkan teksnya.
func (h *Handlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidCredentials):
		err = InvalidCredentials.New()
	case errors.Is(err, domain.ErrAccountSuspended):
		err = AccountSuspended.New()
	case errors.Is(err, domain.ErrAccountLocked):
		err = AccountLocked.New()
	case errors.Is(err, domain.ErrSessionNotFound):
		err = httpx.ResourceNotFound.New()
	case errors.Is(err, domain.ErrNotOrganizationMember):
		err = httpx.PermissionDenied.New()
	case errors.Is(err, domain.ErrUnauthenticated):
		err = httpx.AuthenticationRequired.New()
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		err = httpx.ServiceUnavailable.Wrap(err)
	}
	h.rs.Error(w, r, err)
}

// noStore mencegah respons yang memuat token/identitas disimpan cache perantara.
func noStore(w http.ResponseWriter) { w.Header().Set("Cache-Control", "no-store") }
