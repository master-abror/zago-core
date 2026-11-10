package authzhttp

import (
	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// DTO endpoint role (docs/06 §11). Body didekode lewat httpx.DecodeJSON yang menolak field tak
// dikenal: DTO sengaja TIDAK punya organization_id atau scope, karena organisasi hanya boleh
// berasal dari ScopeContext (ADR-0022 butir 49). Body yang membawa organization_id ditolak
// sebagai unknown_field. Validasi isi (panjang, pola, tipe) ada di domain dan penulis; DTO hanya
// memeriksa bentuk yang tidak dapat dibedakan di sana.

// CreateRoleRequest adalah body POST /roles. Urutan field berbeda dari
// application.CreateRoleInput dengan sengaja, supaya konversi struct tidak tampak mungkin.
type CreateRoleRequest struct {
	// Name adalah nama tampilan role.
	Name string `json:"name"`

	// Slug adalah pengenal role, unik per organisasi.
	Slug string `json:"slug"`

	// Type adalah organization, group, atau custom (system ditolak penulis).
	Type string `json:"type"`

	// Description adalah deskripsi bebas.
	Description string `json:"description"`

	// GroupID wajib untuk role bertipe group dan hanya untuk itu.
	GroupID *uuid.UUID `json:"group_id"`

	// Permissions adalah kode permission awal role.
	Permissions []string `json:"permissions"`
}

// Input mengubah body menjadi masukan penulis.
func (q CreateRoleRequest) Input() application.CreateRoleInput {
	return application.CreateRoleInput{
		Name:        q.Name,
		Slug:        q.Slug,
		Description: q.Description,
		Type:        q.Type,
		GroupID:     q.GroupID,
		Permissions: q.Permissions,
	}
}

// UpdateRoleRequest adalah body PATCH /roles/{id}: field yang tidak dikirim (atau null) tidak
// diubah. Slug tidak dapat diubah.
type UpdateRoleRequest struct {
	// Name adalah nama baru.
	Name *string `json:"name"`

	// Description adalah deskripsi baru; string kosong menghapus deskripsi.
	Description *string `json:"description"`
}

// Validate menolak perubahan kosong.
func (q UpdateRoleRequest) Validate() error {
	if q.Name == nil && q.Description == nil {
		return httpx.Validation(httpx.FieldError{Code: "empty_patch", Message: "the request changes nothing"})
	}
	return nil
}

// Merge menerapkan perubahan pada nama dan deskripsi yang sedang berlaku; penulis menerima
// keduanya sekaligus.
func (q UpdateRoleRequest) Merge(currentName, currentDescription string) (string, string) {
	name, description := currentName, currentDescription
	if q.Name != nil {
		name = *q.Name
	}
	if q.Description != nil {
		description = *q.Description
	}
	return name, description
}

// SetPermissionsRequest adalah body PUT /roles/{id}/permissions: himpunan PENUH yang baru.
type SetPermissionsRequest struct {
	// Permissions adalah himpunan lengkap kode permission; wajib ada (irisan kosong sah dan
	// berarti mencabut semuanya, tetapi field yang hilang atau null ditolak supaya body yang
	// salah ketik tidak menghapus seluruh permission role).
	Permissions []string `json:"permissions"`
}

// Validate menolak body tanpa field permissions.
func (q SetPermissionsRequest) Validate() error {
	if q.Permissions == nil {
		return httpx.Validation(httpx.FieldError{Field: "permissions", Code: "required", Message: "permissions is required"})
	}
	return nil
}

// RoleResponse adalah satu role pada respons. organization_id sengaja tidak ditampilkan.
type RoleResponse struct {
	// ID adalah id role.
	ID uuid.UUID `json:"id"`

	// Name adalah nama tampilan.
	Name string `json:"name"`

	// Slug adalah pengenal role.
	Slug string `json:"slug"`

	// Description adalah deskripsi role.
	Description string `json:"description"`

	// Type adalah system, organization, group, atau custom.
	Type string `json:"type"`

	// Status adalah active atau archived.
	Status string `json:"status"`

	// GroupID terisi untuk role bertipe group, selain itu null.
	GroupID *uuid.UUID `json:"group_id"`

	// IsSystem benar untuk role sistem (tidak dapat diubah lewat API).
	IsSystem bool `json:"is_system"`
}

// NewRoleResponse membentuk respons dari baris role.
func NewRoleResponse(r domain.RoleRecord) RoleResponse {
	return RoleResponse{
		ID:          r.ID,
		Name:        r.Name,
		Slug:        r.Slug,
		Description: r.Description,
		Type:        r.Type,
		Status:      r.Status,
		GroupID:     r.GroupID,
		IsSystem:    r.Type == domain.RoleTypeSystem,
	}
}

// RoleDetailResponse adalah role beserta kode permission-nya.
type RoleDetailResponse struct {
	RoleResponse

	// Permissions selalu ada pada respons (irisan kosong, bukan null).
	Permissions []string `json:"permissions"`
}

// NewRoleDetailResponse membentuk respons detail dari hasil penulis.
func NewRoleDetailResponse(res application.RoleResult) RoleDetailResponse {
	perms := res.Permissions
	if perms == nil {
		perms = []string{}
	}
	return RoleDetailResponse{RoleResponse: NewRoleResponse(res.Role), Permissions: perms}
}
