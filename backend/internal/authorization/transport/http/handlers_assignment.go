package authzhttp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/permission"
)

// AssignmentReads adalah jalur baca assignment yang dibutuhkan handler (dipenuhi
// repository.AssignmentReader; transport tidak mengimpor repository). organizationID berasal
// dari ScopeContext; nil berarti scope platform (hanya assignment platform).
type AssignmentReads interface {
	List(ctx context.Context, organizationID *uuid.UUID, q domain.AssignmentListQuery) ([]domain.AssignmentView, error)
}

// AssignmentManagement adalah jalur tulis assignment (dipenuhi application.AssignmentWriter).
// Penulis TIDAK memeriksa permission pemanggil; itu tugas Authorize di rute (ADR-0022 butir 49).
type AssignmentManagement interface {
	Create(ctx context.Context, g application.Grantor, in application.CreateAssignmentInput) (application.AssignmentResult, error)
	Revoke(ctx context.Context, g application.Grantor, assignmentID uuid.UUID) error
}

// AssignmentHandlers melayani /role-assignments (docs/06 §10-§11). Grantor SELALU diturunkan
// dari Identity dan ScopeContext hasil middleware; organisasi tidak pernah diterima dari body,
// path, atau query (ADR-0022 butir 49).
type AssignmentHandlers struct {
	rs      *httpx.Responder
	cursors *httpx.CursorCodec
	reads   AssignmentReads
	write   AssignmentManagement
}

// NewAssignmentHandlers membuat AssignmentHandlers; semua dependensi wajib ada.
func NewAssignmentHandlers(rs *httpx.Responder, cursors *httpx.CursorCodec, reads AssignmentReads, write AssignmentManagement) (*AssignmentHandlers, error) {
	if rs == nil || cursors == nil || reads == nil || write == nil {
		return nil, errors.New("authzhttp: AssignmentHandlers membutuhkan Responder, CursorCodec, AssignmentReads, dan AssignmentManagement")
	}
	return &AssignmentHandlers{rs: rs, cursors: cursors, reads: reads, write: write}, nil
}

// Mount memasang rute pada r. authorize adalah (*Middleware).Authorize dan idem adalah
// (*httpx.Idempotency).Require(). Setiap rute memasang aksinya SENDIRI karena penulis tidak
// memeriksa permission pemanggil; r harus sudah melewati Authenticate, CSRF, RequireAuth, dan
// Scope (ADR-0022 butir 25). Pada POST, idem dipasang SETELAH Authorize(role.assign): pemanggil
// yang haknya dicabut mendapat 403, bukan replay respons lama, dan pemanggil tanpa hak tidak
// pernah diminta Idempotency-Key. Panik bila authorize atau idem nil (rute tanpa penjaga tidak
// boleh terpasang diam-diam).
func (h *AssignmentHandlers) Mount(r chi.Router, authorize func(action string) func(http.Handler) http.Handler, idem func(http.Handler) http.Handler) {
	if authorize == nil || idem == nil {
		panic("authzhttp: Mount assignment membutuhkan authorize dan idem")
	}
	r.With(authorize(permission.RoleRead)).Get("/role-assignments", h.ListAssignments)
	r.With(authorize(permission.RoleAssign), idem).Post("/role-assignments", h.CreateAssignment)
	r.With(authorize(permission.RoleAssign)).Delete("/role-assignments/{id}", h.RevokeAssignment)
}

// caller menurunkan Grantor dari Identity dan ScopeContext. Authorize sudah menjamin keduanya
// ada; bila hilang, rute dirakit tanpa middleware yang benar: Identity hilang 401, Scope hilang 500.
func (h *AssignmentHandlers) caller(w http.ResponseWriter, r *http.Request) (application.Grantor, bool) {
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

func (h *AssignmentHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.rs.Error(w, r, DomainError(err))
}

// parseOptionalID membaca parameter query berisi UUID; kosong = nil, cacat atau nil UUID dicatat
// pada bad.
func parseOptionalID(q url.Values, field string, bad *httpx.Fields) *uuid.UUID {
	raw := q.Get(field)
	if raw == "" {
		return nil
	}
	v, err := uuid.Parse(raw)
	if err != nil || v == uuid.Nil {
		bad.Add(field, "invalid", field+" must be a UUID")
		return nil
	}
	return &v
}

// foldFieldErrors memindahkan galat field dari err ke bad supaya semua parameter yang salah
// dilaporkan sekaligus; bila err tidak membawa daftar field, satu entri fallbackField dicatat.
func foldFieldErrors(bad *httpx.Fields, err error, fallbackField string) {
	var ae *httpx.Error
	if errors.As(err, &ae) {
		if fs, ok := ae.Details["fields"].([]httpx.FieldError); ok && len(fs) > 0 {
			for _, f := range fs {
				bad.Add(f.Field, f.Code, f.Message)
			}
			return
		}
	}
	bad.Add(fallbackField, "invalid", fallbackField+" is invalid")
}

// assignmentScopeTag adalah penanda organisasi untuk sidik jari cursor: cursor yang diterbitkan
// di satu organisasi (atau platform) ditolak di tempat lain (cursor_mismatch).
func assignmentScopeTag(s domain.ScopeContext) string {
	if org, ok := s.Tenant(); ok {
		return "org:" + org.String()
	}
	return "platform"
}

func assignmentFilterHash(s domain.ScopeContext, status string, roleID, subjectID *uuid.UUID) string {
	f := map[string]string{"status": status, "scope": assignmentScopeTag(s)}
	if roleID != nil {
		f["role_id"] = roleID.String()
	}
	if subjectID != nil {
		f["subject_id"] = subjectID.String()
	}
	return httpx.FilterHash(f)
}

// ListAssignments godoc
//
//	@Summary		Daftar assignment organisasi pemanggil
//	@Description	Tenant hanya melihat assignment organisasinya; scope platform hanya assignment platform. Bawaan hanya yang berlaku (active). Terbaru dulu, paginasi cursor.
//	@Tags			authorization
//	@Produce		json
//	@Param			status		query		string	false	"active (bawaan), revoked, expired, atau all"
//	@Param			role_id		query		string	false	"Saring satu role (UUID)"
//	@Param			subject_id	query		string	false	"Saring satu subjek (UUID)"
//	@Param			limit		query		int		false	"Ukuran halaman (bawaan 25, maksimum 100)"
//	@Param			cursor		query		string	false	"Cursor dari meta.pagination.next_cursor"
//	@Success		200			{object}	httpx.Envelope[[]authzhttp.AssignmentResponse]
//	@Failure		400			{object}	httpx.ErrorEnvelope	"validation_failed (parameter salah, invalid_cursor, cursor_mismatch)"
//	@Failure		401			{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403			{object}	httpx.ErrorEnvelope	"permission_denied"
//	@Router			/api/v1/role-assignments [get]
func (h *AssignmentHandlers) ListAssignments(w http.ResponseWriter, r *http.Request) {
	g, ok := h.caller(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var bad httpx.Fields
	status := q.Get("status")
	if status == "" {
		status = domain.AssignmentFilterActive
	} else if !domain.ValidAssignmentFilter(status) {
		bad.Add("status", "invalid", "status must be active, revoked, expired, or all")
	}
	roleID := parseOptionalID(q, "role_id", &bad)
	subjectID := parseOptionalID(q, "subject_id", &bad)
	page, perr := httpx.ParsePage(r, httpx.PageOptions{DefaultLimit: domain.DefaultPageSize, MaxLimit: domain.MaxPageSize})
	if perr != nil {
		foldFieldErrors(&bad, perr, "limit")
	}
	if bad.Len() > 0 {
		h.rs.Error(w, r, bad.Err())
		return
	}

	hash := assignmentFilterHash(g.Scope, status, roleID, subjectID)
	var after *domain.AssignmentPosition
	if page.Cursor != "" {
		pos, err := h.cursors.Decode(page.Cursor, httpx.Desc, hash)
		if err != nil {
			h.rs.Error(w, r, err)
			return
		}
		after = &domain.AssignmentPosition{CreatedAt: pos.CreatedAt, ID: pos.ID}
	}

	now := time.Now()
	query := domain.AssignmentListQuery{Status: status, RoleID: roleID, SubjectID: subjectID, After: after, Limit: page.FetchLimit(), Now: now}
	views, err := h.reads.List(r.Context(), g.Scope.OrganizationID, query)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	views, meta := httpx.Paginate(views, page.Limit, h.cursors, httpx.Desc, hash, func(v domain.AssignmentView) httpx.Position {
		return httpx.Position{CreatedAt: v.CreatedAt, ID: v.ID}
	})
	items := make([]AssignmentResponse, 0, len(views))
	for _, v := range views {
		items = append(items, NewAssignmentResponse(v, now))
	}
	h.rs.OK(w, r, items, meta)
}

// CreateAssignment godoc
//
//	@Summary		Beri role kepada subjek di scope pemanggil
//	@Description	Organisasi berasal dari sesi, bukan dari body. Pemanggil platform hanya memberi di scope platform; masuk dulu ke organisasi (switch-organization) untuk memberi di sana. Authority ceiling berlaku. Idempotency-Key wajib; kunci yang sama di scope berbeda tidak diputar ulang.
//	@Tags			authorization
//	@Accept			json
//	@Produce		json
//	@Param			X-CSRF-Token	header		string								true	"Token CSRF sesi"
//	@Param			Idempotency-Key	header		string								true	"Kunci idempotensi (1-128 karakter)"
//	@Param			body			body		authzhttp.CreateAssignmentRequest	true	"Assignment baru"
//	@Success		201				{object}	httpx.Envelope[authzhttp.AssignmentResponse]
//	@Failure		400				{object}	httpx.ErrorEnvelope	"validation_failed, invalid_scope"
//	@Failure		401				{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403				{object}	httpx.ErrorEnvelope	"permission_denied, scope_mismatch, grant_exceeds_authority, csrf_failed"
//	@Failure		404				{object}	httpx.ErrorEnvelope	"role_not_found"
//	@Failure		409				{object}	httpx.ErrorEnvelope	"assignment_exists, role_archived, idempotency_in_progress"
//	@Failure		422				{object}	httpx.ErrorEnvelope	"idempotency_key_conflict"
//	@Router			/api/v1/role-assignments [post]
func (h *AssignmentHandlers) CreateAssignment(w http.ResponseWriter, r *http.Request) {
	g, ok := h.caller(w, r)
	if !ok {
		return
	}
	var body CreateAssignmentRequest
	if err := httpx.DecodeJSON(w, r, &body, 0); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	if err := body.Validate(); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	target, err := body.Target(g.Scope)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	res, err := h.write.Create(r.Context(), g, body.Input(target))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.Created(w, r, NewAssignmentResultResponse(res, time.Now()), nil)
}

// RevokeAssignment godoc
//
//	@Summary		Cabut assignment di scope pemanggil
//	@Description	Tenant hanya dapat mencabut assignment organisasinya, platform hanya assignment platform; yang lain sama dengan tidak ada (404). Assignment super-admin platform terakhir tidak dapat dicabut.
//	@Tags			authorization
//	@Param			id				path	string	true	"ID assignment"
//	@Param			X-CSRF-Token	header	string	true	"Token CSRF sesi"
//	@Success		204
//	@Failure		401	{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403	{object}	httpx.ErrorEnvelope	"permission_denied, scope_mismatch, grant_exceeds_authority, csrf_failed"
//	@Failure		404	{object}	httpx.ErrorEnvelope	"resource_not_found"
//	@Failure		409	{object}	httpx.ErrorEnvelope	"assignment_revoked, last_super_admin"
//	@Router			/api/v1/role-assignments/{id} [delete]
func (h *AssignmentHandlers) RevokeAssignment(w http.ResponseWriter, r *http.Request) {
	g, ok := h.caller(w, r)
	if !ok {
		return
	}
	aid, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || aid == uuid.Nil {
		h.fail(w, r, domain.ErrAssignmentNotFound)
		return
	}
	if err := h.write.Revoke(r.Context(), g, aid); err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.NoContent(w)
}
