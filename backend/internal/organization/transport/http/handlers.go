package orghttp

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authdomain "github.com/master-abror/zago-core/backend/internal/auth/domain"
	authzdomain "github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/organization/application"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
	"github.com/master-abror/zago-core/backend/internal/permission"
)

// OrganizationService adalah layanan organisasi yang dibutuhkan handler (dipenuhi
// *application.Service). Layanan menegakkan batas tenant lewat domain.Access; handler hanya
// menerjemahkan HTTP.
type OrganizationService interface {
	Get(ctx context.Context, c application.Caller, organizationID uuid.UUID) (domain.Record, error)
	List(ctx context.Context, c application.Caller, q domain.ListQuery) (domain.Page, error)
	Create(ctx context.Context, c application.Caller, in domain.CreateInput) (domain.Record, error)
	Update(ctx context.Context, c application.Caller, organizationID uuid.UUID, in domain.UpdateInput) (domain.Record, error)
	Archive(ctx context.Context, c application.Caller, organizationID uuid.UUID) (domain.Record, error)
}

// MemberService adalah layanan anggota organisasi yang dibutuhkan handler (dipenuhi
// *application.MemberService).
type MemberService interface {
	Get(ctx context.Context, c application.Caller, organizationID, userID uuid.UUID) (domain.Member, error)
	List(ctx context.Context, c application.Caller, organizationID uuid.UUID, q domain.MemberListQuery) (domain.MemberPage, error)
	Add(ctx context.Context, c application.Caller, organizationID, userID uuid.UUID) (domain.Member, error)
	Remove(ctx context.Context, c application.Caller, organizationID, userID uuid.UUID) (domain.Member, error)
}

var _ OrganizationService = (*application.Service)(nil)

var _ MemberService = (*application.MemberService)(nil)

// Handlers melayani endpoint organisasi dan anggota (docs/08 §10.6; ADR-0023 butir 19). Pemanggil
// (Caller) SELALU diturunkan dari Identity dan ScopeContext hasil middleware. Organisasi sasaran
// berasal dari path {id}, dan layanan memeriksanya melawan ScopeContext: pemanggil tenant yang
// menunjuk organisasi lain mendapat respons yang sama dengan organisasi yang tidak ada.
type Handlers struct {
	rs      *httpx.Responder
	orgs    OrganizationService
	members MemberService
}

// NewHandlers membuat Handlers; semua dependensi wajib ada.
func NewHandlers(rs *httpx.Responder, orgs OrganizationService, members MemberService) (*Handlers, error) {
	if rs == nil || orgs == nil || members == nil {
		return nil, errors.New("orghttp: Handlers membutuhkan Responder, OrganizationService, dan MemberService")
	}
	return &Handlers{rs: rs, orgs: orgs, members: members}, nil
}

// Mount memasang rute pada r. authorize adalah (*authzhttp.Middleware).Authorize. Setiap rute
// memasang aksinya SENDIRI; r harus sudah melewati Authenticate, CSRF, RequireAuth, dan Scope.
// Panik bila authorize nil (rute tanpa penjaga tidak boleh terpasang diam-diam).
//
// Aksi per rute (ADR-0023 butir 19): baca = organization.read; buat = organization.create;
// ubah = organization.update; arsip = organization.archive; semua rute anggota, termasuk baca,
// = organization.member.manage.
func (h *Handlers) Mount(r chi.Router, authorize func(action string) func(http.Handler) http.Handler) {
	if authorize == nil {
		panic("orghttp: Mount membutuhkan authorize")
	}
	r.With(authorize(permission.OrganizationRead)).Get("/organizations", h.ListOrganizations)
	r.With(authorize(permission.OrganizationCreate)).Post("/organizations", h.CreateOrganization)
	r.With(authorize(permission.OrganizationRead)).Get("/organizations/{id}", h.GetOrganization)
	r.With(authorize(permission.OrganizationUpdate)).Patch("/organizations/{id}", h.UpdateOrganization)
	r.With(authorize(permission.OrganizationArchive)).Delete("/organizations/{id}", h.ArchiveOrganization)
	r.With(authorize(permission.OrganizationMemberManage)).Get("/organizations/{id}/members", h.ListMembers)
	r.With(authorize(permission.OrganizationMemberManage)).Post("/organizations/{id}/members", h.AddMember)
	r.With(authorize(permission.OrganizationMemberManage)).Get("/organizations/{id}/members/{user_id}", h.GetMember)
	r.With(authorize(permission.OrganizationMemberManage)).Delete("/organizations/{id}/members/{user_id}", h.RemoveMember)
}

// caller menurunkan Caller dari Identity dan ScopeContext. Authorize sudah menjamin keduanya ada;
// bila hilang, rute dirakit tanpa middleware yang benar: Identity hilang 401, Scope hilang 500.
func (h *Handlers) caller(w http.ResponseWriter, r *http.Request) (application.Caller, bool) {
	ident, ok := authdomain.IdentityFromContext(r.Context())
	if !ok {
		h.rs.Error(w, r, httpx.AuthenticationRequired.New())
		return application.Caller{}, false
	}
	scope, ok := authzdomain.ScopeFromContext(r.Context())
	if !ok {
		h.rs.Error(w, r, httpx.InternalError.New())
		return application.Caller{}, false
	}
	return application.Caller{UserID: ident.UserID, Scope: scope}, true
}

func (h *Handlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.rs.Error(w, r, DomainError(err))
}

// orgID membaca {id}. ID cacat tidak dibedakan dari organisasi yang tidak ada.
func (h *Handlers) orgID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	v, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || v == uuid.Nil {
		h.rs.Error(w, r, OrganizationNotFound.New())
		return uuid.Nil, false
	}
	return v, true
}

// memberUserID membaca {user_id}. ID cacat tidak dibedakan dari anggota yang tidak ada.
func (h *Handlers) memberUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	v, err := uuid.Parse(chi.URLParam(r, "user_id"))
	if err != nil || v == uuid.Nil {
		h.rs.Error(w, r, MemberNotFound.New())
		return uuid.Nil, false
	}
	return v, true
}

// maxPageNumber membatasi page supaya offset (page-1)*per_page tidak dapat meluap.
const maxPageNumber = 10000

// parsePaging membaca page dan per_page dari query (daftar organisasi, offset: docs/08 §4).
// Nilai yang hilang memakai bawaan (page 1, per_page domain.DefaultPageSize); per_page di atas
// batas dipangkas ke domain.MaxPageSize; nilai bukan angka, nol, atau negatif menambah galat ke f.
func parsePaging(q url.Values, f *httpx.Fields) (page, perPage int) {
	page, perPage = 1, domain.DefaultPageSize

	if v := q.Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPageNumber {
			f.Add("page", "invalid", "page must be an integer between 1 and 10000")
		} else {
			page = n
		}
	}
	if v := q.Get("per_page"); v != "" {
		n, err := strconv.Atoi(v)
		switch {
		case err != nil || n < 1:
			f.Add("per_page", "invalid", "per_page must be a positive integer")
		case n > domain.MaxPageSize:
			perPage = domain.MaxPageSize
		default:
			perPage = n
		}
	}
	return page, perPage
}

// paginationMeta membentuk meta.pagination untuk daftar offset.
func paginationMeta(page, perPage, total int) httpx.Meta {
	return httpx.Meta{"pagination": map[string]int{"page": page, "per_page": perPage, "total": total}}
}

// cursorMeta membentuk meta.pagination untuk daftar cursor, bentuk yang sama dengan
// httpx.Paginate: next_cursor null pada halaman terakhir.
func cursorMeta(next string) httpx.Meta {
	var cursor any
	if next != "" {
		cursor = next
	}
	return httpx.Meta{"pagination": map[string]any{"next_cursor": cursor, "has_more": next != ""}}
}

// ListOrganizations godoc
//
//	@Summary		Daftar organisasi yang terlihat bagi pemanggil
//	@Description	Scope platform melihat semua organisasi; pemanggil tenant hanya melihat organisasinya sendiri. Bawaan: organisasi aktif dan ditangguhkan (open).
//	@Tags			organization
//	@Produce		json
//	@Param			status		query		string	false	"open (bawaan), all, active, suspended, atau archived"
//	@Param			page		query		int		false	"Nomor halaman (mulai 1)"
//	@Param			per_page	query		int		false	"Ukuran halaman (bawaan 50, maksimum 100)"
//	@Success		200			{object}	httpx.Envelope[[]orghttp.OrganizationResponse]
//	@Failure		400			{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401			{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403			{object}	httpx.ErrorEnvelope	"permission_denied"
//	@Router			/api/v1/organizations [get]
func (h *Handlers) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	var bad httpx.Fields
	page, perPage := parsePaging(q, &bad)
	if bad.Len() > 0 {
		h.rs.Error(w, r, bad.Err())
		return
	}
	res, err := h.orgs.List(r.Context(), c, domain.ListQuery{
		Window: domain.Window{Offset: (page - 1) * perPage, Limit: perPage},
		Filter: q.Get("status"),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.OK(w, r, NewOrganizationResponses(res.Items), paginationMeta(page, perPage, res.Total))
}

// CreateOrganization godoc
//
//	@Summary		Buat organisasi (hanya platform)
//	@Description	Hanya pemanggil scope platform. Pemanggil tenant selalu mendapat 403 permission_denied. Slug unik dan tidak dapat diubah.
//	@Tags			organization
//	@Accept			json
//	@Produce		json
//	@Param			X-CSRF-Token	header		string								true	"Token CSRF sesi"
//	@Param			body			body		orghttp.CreateOrganizationRequest	true	"Organisasi baru"
//	@Success		201				{object}	httpx.Envelope[orghttp.OrganizationResponse]
//	@Failure		400				{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401				{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403				{object}	httpx.ErrorEnvelope	"permission_denied, csrf_failed"
//	@Failure		409				{object}	httpx.ErrorEnvelope	"organization_slug_taken"
//	@Router			/api/v1/organizations [post]
func (h *Handlers) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	var body CreateOrganizationRequest
	if err := httpx.DecodeJSON(w, r, &body, 0); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	rec, err := h.orgs.Create(r.Context(), c, body.Input())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.Created(w, r, NewOrganizationResponse(rec), nil)
}

// GetOrganization godoc
//
//	@Summary		Detail satu organisasi
//	@Description	Organisasi milik tenant lain dan organisasi yang tidak ada sama-sama 404 organization_not_found.
//	@Tags			organization
//	@Produce		json
//	@Param			id	path		string	true	"ID organisasi"
//	@Success		200	{object}	httpx.Envelope[orghttp.OrganizationResponse]
//	@Failure		401	{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403	{object}	httpx.ErrorEnvelope	"permission_denied"
//	@Failure		404	{object}	httpx.ErrorEnvelope	"organization_not_found"
//	@Router			/api/v1/organizations/{id} [get]
func (h *Handlers) GetOrganization(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	id, ok := h.orgID(w, r)
	if !ok {
		return
	}
	rec, err := h.orgs.Get(r.Context(), c, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.OK(w, r, NewOrganizationResponse(rec), nil)
}

// UpdateOrganization godoc
//
//	@Summary		Ubah organisasi
//	@Description	Hanya field yang dikirim yang berubah; slug dan status tidak dapat diubah di sini (field itu ditolak). Organisasi terarsip tidak dapat diubah.
//	@Tags			organization
//	@Accept			json
//	@Produce		json
//	@Param			id				path		string								true	"ID organisasi"
//	@Param			X-CSRF-Token	header		string								true	"Token CSRF sesi"
//	@Param			body			body		orghttp.UpdateOrganizationRequest	true	"Perubahan"
//	@Success		200				{object}	httpx.Envelope[orghttp.OrganizationResponse]
//	@Failure		400				{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401				{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403				{object}	httpx.ErrorEnvelope	"permission_denied, csrf_failed"
//	@Failure		404				{object}	httpx.ErrorEnvelope	"organization_not_found"
//	@Failure		409				{object}	httpx.ErrorEnvelope	"organization_archived"
//	@Router			/api/v1/organizations/{id} [patch]
func (h *Handlers) UpdateOrganization(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	id, ok := h.orgID(w, r)
	if !ok {
		return
	}
	var body UpdateOrganizationRequest
	if err := httpx.DecodeJSON(w, r, &body, 0); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	if err := body.Validate(); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	rec, err := h.orgs.Update(r.Context(), c, id, body.Input())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.OK(w, r, NewOrganizationResponse(rec), nil)
}

// ArchiveOrganization godoc
//
//	@Summary		Arsipkan organisasi
//	@Description	Soft delete: status menjadi archived dan bersifat terminal. Sesi anggota dicabut pada task T5.
//	@Tags			organization
//	@Param			id				path	string	true	"ID organisasi"
//	@Param			X-CSRF-Token	header	string	true	"Token CSRF sesi"
//	@Success		204
//	@Failure		401	{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403	{object}	httpx.ErrorEnvelope	"permission_denied, csrf_failed"
//	@Failure		404	{object}	httpx.ErrorEnvelope	"organization_not_found"
//	@Failure		409	{object}	httpx.ErrorEnvelope	"organization_archived, invalid_status_transition"
//	@Router			/api/v1/organizations/{id} [delete]
func (h *Handlers) ArchiveOrganization(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	id, ok := h.orgID(w, r)
	if !ok {
		return
	}
	if _, err := h.orgs.Archive(r.Context(), c, id); err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.NoContent(w)
}

// ListMembers godoc
//
//	@Summary		Daftar anggota satu organisasi
//	@Description	Paginasi cursor (urutan created_at, id). Bawaan: anggota yang belum dihapus (open). Alamat email tidak ditampilkan.
//	@Tags			organization
//	@Produce		json
//	@Param			id		path		string	true	"ID organisasi"
//	@Param			status	query		string	false	"open (bawaan), all, invited, active, suspended, atau removed"
//	@Param			limit	query		int		false	"Ukuran halaman (bawaan 50, maksimum 100)"
//	@Param			cursor	query		string	false	"Cursor halaman berikutnya dari meta.pagination.next_cursor"
//	@Success		200		{object}	httpx.Envelope[[]orghttp.MemberResponse]
//	@Failure		400		{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401		{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403		{object}	httpx.ErrorEnvelope	"permission_denied"
//	@Failure		404		{object}	httpx.ErrorEnvelope	"organization_not_found"
//	@Router			/api/v1/organizations/{id}/members [get]
func (h *Handlers) ListMembers(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	id, ok := h.orgID(w, r)
	if !ok {
		return
	}
	page, perr := httpx.ParsePage(r, httpx.PageOptions{DefaultLimit: domain.DefaultPageSize, MaxLimit: domain.MaxPageSize})
	if perr != nil {
		h.rs.Error(w, r, perr)
		return
	}
	res, err := h.members.List(r.Context(), c, id, domain.MemberListQuery{
		Limit:  page.Limit,
		Cursor: page.Cursor,
		Filter: r.URL.Query().Get("status"),
	})
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.OK(w, r, NewMemberResponses(res.Items), cursorMeta(res.NextCursor))
}

// AddMember godoc
//
//	@Summary		Tambah anggota ke organisasi (hanya platform)
//	@Description	Hanya pemanggil scope platform; pengguna harus aktif. Tenant memakai undangan. Pengguna yang pernah dihapus diaktifkan kembali pada baris yang sama.
//	@Tags			organization
//	@Accept			json
//	@Produce		json
//	@Param			id				path		string					true	"ID organisasi"
//	@Param			X-CSRF-Token	header		string					true	"Token CSRF sesi"
//	@Param			body			body		orghttp.AddMemberRequest	true	"Pengguna yang ditambahkan"
//	@Success		201				{object}	httpx.Envelope[orghttp.MemberResponse]
//	@Failure		400				{object}	httpx.ErrorEnvelope	"validation_failed"
//	@Failure		401				{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403				{object}	httpx.ErrorEnvelope	"permission_denied, csrf_failed"
//	@Failure		404				{object}	httpx.ErrorEnvelope	"organization_not_found"
//	@Failure		409				{object}	httpx.ErrorEnvelope	"member_exists, organization_archived"
//	@Failure		422				{object}	httpx.ErrorEnvelope	"user_not_eligible"
//	@Router			/api/v1/organizations/{id}/members [post]
func (h *Handlers) AddMember(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	id, ok := h.orgID(w, r)
	if !ok {
		return
	}
	var body AddMemberRequest
	if err := httpx.DecodeJSON(w, r, &body, 0); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	if err := body.Validate(); err != nil {
		h.rs.Error(w, r, err)
		return
	}
	m, err := h.members.Add(r.Context(), c, id, body.UserID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.Created(w, r, NewMemberResponse(m), nil)
}

// GetMember godoc
//
//	@Summary		Detail satu anggota
//	@Description	Anggota organisasi lain, anggota yang tidak ada, dan anggota yang sudah dihapus sama-sama 404 member_not_found.
//	@Tags			organization
//	@Produce		json
//	@Param			id		path		string	true	"ID organisasi"
//	@Param			user_id	path		string	true	"ID pengguna"
//	@Success		200		{object}	httpx.Envelope[orghttp.MemberResponse]
//	@Failure		401		{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403		{object}	httpx.ErrorEnvelope	"permission_denied"
//	@Failure		404		{object}	httpx.ErrorEnvelope	"organization_not_found, member_not_found"
//	@Router			/api/v1/organizations/{id}/members/{user_id} [get]
func (h *Handlers) GetMember(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	id, ok := h.orgID(w, r)
	if !ok {
		return
	}
	uid, ok := h.memberUserID(w, r)
	if !ok {
		return
	}
	m, err := h.members.Get(r.Context(), c, id, uid)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.OK(w, r, NewMemberResponse(m), nil)
}

// RemoveMember godoc
//
//	@Summary		Hapus anggota dari organisasi
//	@Description	Penghapusan logis: keanggotaan menjadi removed, keanggotaan grup dan role assignment ber-scope organisasi dicabut, cache hak diinvalidasi. Pemanggil tenant tidak dapat menghapus dirinya sendiri. Sesi tidak dicabut sampai task T5.
//	@Tags			organization
//	@Param			id				path	string	true	"ID organisasi"
//	@Param			user_id			path	string	true	"ID pengguna"
//	@Param			X-CSRF-Token	header	string	true	"Token CSRF sesi"
//	@Success		204
//	@Failure		401	{object}	httpx.ErrorEnvelope	"authentication_required"
//	@Failure		403	{object}	httpx.ErrorEnvelope	"permission_denied, csrf_failed"
//	@Failure		404	{object}	httpx.ErrorEnvelope	"organization_not_found, member_not_found"
//	@Failure		409	{object}	httpx.ErrorEnvelope	"cannot_remove_self"
//	@Router			/api/v1/organizations/{id}/members/{user_id} [delete]
func (h *Handlers) RemoveMember(w http.ResponseWriter, r *http.Request) {
	c, ok := h.caller(w, r)
	if !ok {
		return
	}
	id, ok := h.orgID(w, r)
	if !ok {
		return
	}
	uid, ok := h.memberUserID(w, r)
	if !ok {
		return
	}
	if _, err := h.members.Remove(r.Context(), c, id, uid); err != nil {
		h.fail(w, r, err)
		return
	}
	h.rs.NoContent(w)
}
