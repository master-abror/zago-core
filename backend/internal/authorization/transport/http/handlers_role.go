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

// RoleReads adalah jalur baca role yang dibutuhkan handler (dipenuhi repository.RoleReader;
// transport tidak mengimpor repository). organizationID berasal dari ScopeContext; nil berarti
// scope platform.
type RoleReads interface {
	List(ctx context.Context, organizationID *uuid.UUID, q domain.RoleListQuery) (domain.RolePage, error)
	Get(ctx context.Context, organizationID *uuid.UUID, roleID uuid.UUID) (domain.RoleRecord, []string, error)
}

// RoleManagement adalah jalur tulis role (dipenuhi application.RoleWriter). Penulis TIDAK memeriksa
// permission pemanggil; itu tugas Authorize di rute (ADR-0022 butir 49).
type RoleManagement interface {
	CreateRole(ctx context.Context, g application.Grantor, in application.CreateRoleInput) (application.RoleResult, error)
	UpdateRole(ctx context.Context, g application.Grantor, roleID uuid.UUID, name, description string) (application.RoleResult, error)
	SetPermissions(ctx context.Context, g application.Grantor, roleID uuid.UUID, requested []string) (application.RoleResult, error)
	ArchiveRole(ctx context.Context, g application.Grantor, roleID uuid.UUID) error
}

// PermissionReads adalah jalur baca katalog permission (dipenuhi repository.PermissionReader).
type PermissionReads interface {
	List(ctx context.Context, window domain.Window) (domain.PermissionPage, error)
}

// RoleHandlers melayani endpoint role dan katalog permission (docs/06 §11). Grantor SELALU
// diturunkan dari Identity dan ScopeContext hasil middleware; organisasi tidak pernah diterima dari
// body atau path (ADR-0022 butir 49).
type RoleHandlers struct {
	rs    *httpx.Responder
	reads RoleReads
	write RoleManagement
	perms PermissionReads
}

// NewRoleHandlers membuat RoleHandlers; semua dependensi wajib ada.
func NewRoleHandlers(rs *httpx.Responder, reads RoleReads, write RoleManagement, perms PermissionReads) (*RoleHandlers, error) {
	if rs == nil || reads == nil || write == nil || perms == nil {
		return nil, errors.New("authzhttp: RoleHandlers membutuhkan Responder, RoleReads, RoleManagement, dan PermissionReads")
	}
	return &RoleHandlers{rs: rs, reads: reads, write: write, perms: perms}, nil
}

// Mount memasang rute pada r. authorize adalah (*Middleware).Authorize. Setiap rute memasang
// aksinya SENDIRI karena penulis tidak memeriksa permission pemanggil; r harus sudah melewati
// Authenticate, CSRF, RequireAuth, dan Scope (ADR-0022 butir 25). Panik bila authorize nil
// (rute tanpa penjaga tidak boleh terpasang diam-diam).
func (h *RoleHandlers) Mount(r chi.Router, authorize func(action string) func(http.Handler) http.Handler) {
	if authorize == nil {
		panic("authzhttp: Mount membutuhkan authorize")
	}
	r.With(authorize(permission.RoleRead)).Get("/roles", h.ListRoles)
	r.With(authorize(permission.RoleManage)).Post("/roles", h.CreateRole)
	r.With(authorize(permission.RoleRead)).Get("/roles/{id}", h.GetRole)
	r.With(authorize(permission.RoleManage)).Patch("/roles/{id}", h.UpdateRole)
	r.With(authorize(permission.RoleManage)).Delete("/roles/{id}", h.ArchiveRole)
	r.With(authorize(permission.RoleRead)).Get("/roles/{id}/permissions", h.GetRolePermissions)
	r.With(authorize(permission.PermissionAssign)).Put("/roles/{id}/permissions", h.SetRolePermissions)
	r.With(authorize(permission.PermissionRead)).Get("/permissions", h.ListPermissions)
}

// caller menurunkan Grantor dari Identity dan ScopeContext. Authorize sudah menjamin keduanya ada;
// bila hilang, rute dirakit tanpa middleware yang benar: Identity hilang 401, Scope hilang 500.
func (h *RoleHandlers) caller(w http.ResponseWriter, r *http.Request) (application.Grantor, bool) {
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

func (h *RoleHandlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.rs.Error(w, r, DomainError(err))
}

// roleID membaca {id}. ID cacat tidak dibedakan dari role yang tidak ada.
func (h *RoleHandlers) roleID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	v, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || v == uuid.Nil {
		h.rs.Error(w, r, RoleNotFound.New())
		return uuid.Nil, false
	}
	return v, true
}

// ListRoles godoc
//
//	@Summary		Daftar role yang terlihat bagi pemanggil
//	@Description	Role sistem ditambah role organisasi pemanggil; scope platform hanya melihat role sistem. Bawaan hanya role aktif.
//	@Tags			authorization
//	@Produce		json
//	@Param			status		query		string	false	"active (bawaan), archived, atau all"
//	@Param			page		query		int		false	"Nomor halaman (mulai 1)"
//	@Param			per_page	query		int		false	"Ukuran halaman (bawaan 25, maksimum 100)"
//	@Success		200			{object}	httpx.Envelope[[]authzhttp.RoleResponse]
//	@Failure		400			{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401			{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403			{object}	httpx.ErrorEnvelope	"permission_denied"
//	@Router			/api/v1/roles [get]
func (h *RoleHandlers) ListRoles(w http.ResponseWriter, r *http.Request) {
	g, ok := h.caller(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var bad httpx.Fields
	status := q.Get("status")
	if status != "" && !domain.ValidRoleFilter(status) {
		bad.Add("status", "invalid", "status must be active, archived, or all")
	}
	page, perPage := parsePaging(q, &bad)
	if bad.Len() > 0 {
		h.rs.Error(w, r, bad.Err())
		return
	}

	result, err := h.reads.List(r.Context(), g.Scope.OrganizationID, domain.RoleListQuery{Status: status, Window: windowFor(page, perPage)})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items := make([]RoleResponse, 0, len(result.Items))
	for _, row := range result.Items {
		items = append(items, NewRoleResponse(row))
	}
	h.rs.OK(w, r, items, paginationMeta(page, perPage, result.Total))
}

// GetRole godoc
//
//	@Summary		Detail satu role beserta permission-nya
//	@Description	Role organisasi lain dan role yang tidak ada sama-sama 404 role_not_found.
//	@Tags			authorization
//	@Produce		json
//	@Param			id	path		string	true	"ID role"
//	@Success		200	{object}	httpx.Envelope[authzhttp.RoleDetailResponse]
//	@Failure		401	{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403	{object}	httpx.ErrorEnvelope	"permission_denied"
//	@Failure		404	{object}	httpx.ErrorEnvelope	"role_not_found"
//	@Router			/api/v1/roles/{id} [get]
func (h *RoleHandlers) GetRole(w http.ResponseWriter, r *http.Request) {
	g, ok := h.caller(w, r)
	if !ok {
		return
	}
	id, ok := h.roleID(w, r)
	if !ok {
		return
	}
	row, codes, err := h.reads.Get(r.Context(), g.Scope.OrganizationID, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.OK(w, r, NewRoleDetailResponse(application.RoleResult{Role: row, Permissions: codes}), nil)
}

// CreateRole godoc
//
//	@Summary		Buat role di organisasi pemanggil
//	@Description	Organisasi berasal dari sesi, bukan dari body. Permission awal tunduk pada authority ceiling pemanggil.
//	@Tags			authorization
//	@Accept			json
//	@Produce		json
//	@Param			X-CSRF-Token	header		string						true	"Token CSRF sesi"
//	@Param			body			body		authzhttp.CreateRoleRequest	true	"Role baru"
//	@Success		201				{object}	httpx.Envelope[authzhttp.RoleDetailResponse]
//	@Failure		400				{object}	httpx.ErrorEnvelope	"validation_failed, invalid_scope"
//	@Failure		401				{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403				{object}	httpx.ErrorEnvelope	"permission_denied, scope_mismatch, grant_exceeds_authority, csrf_failed"
//	@Failure		409				{object}	httpx.ErrorEnvelope	"role_slug_taken"
//	@Failure		422				{object}	httpx.ErrorEnvelope	"permission_not_found"
//	@Router			/api/v1/roles [post]
func (h *RoleHandlers) CreateRole(w http.ResponseWriter, r *http.Request) {
	g, ok := h.caller(w, r)
	if !ok {
		return
	}
	var body CreateRoleRequest
	if err := httpx.DecodeJSON(w, r, &body, 0); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	res, err := h.write.CreateRole(r.Context(), g, body.Input())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.Created(w, r, NewRoleDetailResponse(res), nil)
}

// UpdateRole godoc
//
//	@Summary		Ubah nama dan deskripsi role
//	@Description	Hanya field yang dikirim yang berubah; slug dan permission tidak dapat diubah di sini. Role sistem tidak dapat diubah.
//	@Tags			authorization
//	@Accept			json
//	@Produce		json
//	@Param			id				path		string						true	"ID role"
//	@Param			X-CSRF-Token	header		string						true	"Token CSRF sesi"
//	@Param			body			body		authzhttp.UpdateRoleRequest	true	"Perubahan"
//	@Success		200				{object}	httpx.Envelope[authzhttp.RoleDetailResponse]
//	@Failure		400				{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401				{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403				{object}	httpx.ErrorEnvelope	"permission_denied, scope_mismatch, grant_exceeds_authority, csrf_failed"
//	@Failure		404				{object}	httpx.ErrorEnvelope	"role_not_found"
//	@Failure		409				{object}	httpx.ErrorEnvelope	"role_immutable, role_archived"
//	@Router			/api/v1/roles/{id} [patch]
func (h *RoleHandlers) UpdateRole(w http.ResponseWriter, r *http.Request) {
	g, ok := h.caller(w, r)
	if !ok {
		return
	}
	id, ok := h.roleID(w, r)
	if !ok {
		return
	}
	var body UpdateRoleRequest
	if err := httpx.DecodeJSON(w, r, &body, 0); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	if err := body.Validate(); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	// Penulis menerima nama dan deskripsi sekaligus, jadi nilai yang sedang berlaku dibaca lebih
	// dulu (dengan filter tenant yang sama). Dua PATCH serentak pada field berbeda dapat saling
	// menimpa (kasus sempit yang dicatat di ADR-0022 butir 64).
	cur, _, err := h.reads.Get(r.Context(), g.Scope.OrganizationID, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	name, description := body.Merge(cur.Name, cur.Description)
	res, err := h.write.UpdateRole(r.Context(), g, id, name, description)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.OK(w, r, NewRoleDetailResponse(res), nil)
}

// ArchiveRole godoc
//
//	@Summary		Arsipkan role
//	@Description	Role tidak dihapus; role terarsip tidak dapat dikembalikan dan assignment-nya berhenti berlaku. Role sistem tidak dapat diarsipkan.
//	@Tags			authorization
//	@Param			id				path	string	true	"ID role"
//	@Param			X-CSRF-Token	header	string	true	"Token CSRF sesi"
//	@Success		204
//	@Failure		401	{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403	{object}	httpx.ErrorEnvelope	"permission_denied, scope_mismatch, grant_exceeds_authority, csrf_failed"
//	@Failure		404	{object}	httpx.ErrorEnvelope	"role_not_found"
//	@Failure		409	{object}	httpx.ErrorEnvelope	"role_immutable, role_archived"
//	@Router			/api/v1/roles/{id} [delete]
func (h *RoleHandlers) ArchiveRole(w http.ResponseWriter, r *http.Request) {
	g, ok := h.caller(w, r)
	if !ok {
		return
	}
	id, ok := h.roleID(w, r)
	if !ok {
		return
	}
	if err := h.write.ArchiveRole(r.Context(), g, id); err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.NoContent(w)
}

// GetRolePermissions godoc
//
//	@Summary	Permission yang dipegang satu role
//	@Tags		authorization
//	@Produce	json
//	@Param		id	path		string	true	"ID role"
//	@Success	200	{object}	httpx.Envelope[authzhttp.RolePermissionsResponse]
//	@Failure	401	{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure	403	{object}	httpx.ErrorEnvelope	"permission_denied"
//	@Failure	404	{object}	httpx.ErrorEnvelope	"role_not_found"
//	@Router		/api/v1/roles/{id}/permissions [get]
func (h *RoleHandlers) GetRolePermissions(w http.ResponseWriter, r *http.Request) {
	g, ok := h.caller(w, r)
	if !ok {
		return
	}
	id, ok := h.roleID(w, r)
	if !ok {
		return
	}
	_, codes, err := h.reads.Get(r.Context(), g.Scope.OrganizationID, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.OK(w, r, NewRolePermissionsResponse(codes), nil)
}

// SetRolePermissions godoc
//
//	@Summary		Ganti himpunan permission role
//	@Description	Himpunan PENUH yang baru; field permissions wajib ada (irisan kosong mencabut semuanya). Authority ceiling berlaku atas gabungan himpunan lama dan baru. Permission role sistem tidak dapat diubah.
//	@Tags			authorization
//	@Accept			json
//	@Produce		json
//	@Param			id				path		string								true	"ID role"
//	@Param			X-CSRF-Token	header		string								true	"Token CSRF sesi"
//	@Param			body			body		authzhttp.SetPermissionsRequest	true	"Himpunan permission baru"
//	@Success		200				{object}	httpx.Envelope[authzhttp.RolePermissionsResponse]
//	@Failure		400				{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401				{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403				{object}	httpx.ErrorEnvelope	"permission_denied, scope_mismatch, grant_exceeds_authority, csrf_failed"
//	@Failure		404				{object}	httpx.ErrorEnvelope	"role_not_found"
//	@Failure		409				{object}	httpx.ErrorEnvelope	"role_immutable, role_archived"
//	@Failure		422				{object}	httpx.ErrorEnvelope	"permission_not_found"
//	@Router			/api/v1/roles/{id}/permissions [put]
func (h *RoleHandlers) SetRolePermissions(w http.ResponseWriter, r *http.Request) {
	g, ok := h.caller(w, r)
	if !ok {
		return
	}
	id, ok := h.roleID(w, r)
	if !ok {
		return
	}
	var body SetPermissionsRequest
	if err := httpx.DecodeJSON(w, r, &body, 0); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	if err := body.Validate(); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	res, err := h.write.SetPermissions(r.Context(), g, id, body.Permissions)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.OK(w, r, NewRolePermissionsResponse(res.Permissions), nil)
}

// ListPermissions godoc
//
//	@Summary		Katalog permission
//	@Description	Katalog bersifat global (core dan modul), terurut menurut kode.
//	@Tags			authorization
//	@Produce		json
//	@Param			page		query		int	false	"Nomor halaman (mulai 1)"
//	@Param			per_page	query		int	false	"Ukuran halaman (bawaan 25, maksimum 100)"
//	@Success		200			{object}	httpx.Envelope[[]authzhttp.PermissionResponse]
//	@Failure		400			{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401			{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403			{object}	httpx.ErrorEnvelope	"permission_denied"
//	@Router			/api/v1/permissions [get]
func (h *RoleHandlers) ListPermissions(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.caller(w, r); !ok {
		return
	}
	var bad httpx.Fields
	page, perPage := parsePaging(r.URL.Query(), &bad)
	if bad.Len() > 0 {
		h.rs.Error(w, r, bad.Err())
		return
	}
	result, err := h.perms.List(r.Context(), windowFor(page, perPage))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	items := make([]PermissionResponse, 0, len(result.Items))
	for _, row := range result.Items {
		items = append(items, NewPermissionResponse(row))
	}
	h.rs.OK(w, r, items, paginationMeta(page, perPage, result.Total))
}
