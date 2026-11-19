package authzhttp

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/policy"
)

// DTO endpoint policies (docs/06 §11). Body didekode lewat httpx.DecodeJSON yang menolak field
// tak dikenal: DTO sengaja TIDAK punya organization_id (cakupan dari ScopeContext lewat penulis)
// dan PATCH tidak punya code (kode tidak dapat diubah). DTO hanya memeriksa kehadiran dan bentuk
// kasar; pola kode, panjang nama, ekspresi, dan actions terhadap katalog dinilai policy.Validate
// di penulis (ADR-0022 butir 50 dan 59).

// CreatePolicyRequest adalah body POST /policies.
type CreatePolicyRequest struct {
	// Code adalah kode unik pada cakupannya (huruf kecil, angka, titik, garis bawah).
	Code string `json:"code"`

	// Name adalah nama tampilan.
	Name string `json:"name"`

	// Description adalah deskripsi bebas (boleh kosong).
	Description string `json:"description"`

	// Effect adalah allow atau deny.
	Effect string `json:"effect"`

	// Expression adalah ekspresi policy (bahasa ekspresi internal/policy).
	Expression string `json:"expression"`

	// Actions adalah kode permission yang dibatasi policy ini (juga "resource.*"); wajib, tidak kosong.
	Actions []string `json:"actions"`

	// Status adalah active (bawaan bila kosong) atau disabled.
	Status string `json:"status"`
}

// Validate melaporkan SEMUA field wajib yang hilang dan status yang tidak sah sekaligus.
func (q CreatePolicyRequest) Validate() error {
	var bad httpx.Fields
	if q.Code == "" {
		bad.Add("code", "required", "code is required")
	}
	if q.Name == "" {
		bad.Add("name", "required", "name is required")
	}
	if q.Effect == "" {
		bad.Add("effect", "required", "effect is required")
	}
	if q.Expression == "" {
		bad.Add("expression", "required", "expression is required")
	}
	if len(q.Actions) == 0 {
		bad.Add("actions", "required", "actions is required and must not be empty")
	}
	if q.Status != "" && !domain.ValidPolicyStatus(q.Status) {
		bad.Add("status", "invalid", "status must be active or disabled")
	}
	return bad.Err()
}

// Input mengubah body menjadi masukan penulis. Actions disalin.
func (q CreatePolicyRequest) Input() application.CreatePolicyInput {
	return application.CreatePolicyInput{
		Code:        q.Code,
		Name:        q.Name,
		Description: q.Description,
		Effect:      policy.Effect(q.Effect),
		Expression:  q.Expression,
		Actions:     slices.Clone(q.Actions),
		Status:      q.Status,
	}
}

// UpdatePolicyRequest adalah body PATCH /policies/{id}: field yang tidak dikirim (atau null)
// tidak diubah. Kode dan cakupan tidak dapat diubah.
type UpdatePolicyRequest struct {
	// Name adalah nama baru.
	Name *string `json:"name"`

	// Description adalah deskripsi baru; string kosong menghapus deskripsi.
	Description *string `json:"description"`

	// Effect adalah allow atau deny.
	Effect *string `json:"effect"`

	// Expression adalah ekspresi baru.
	Expression *string `json:"expression"`

	// Actions adalah himpunan PENUH yang baru; irisan kosong ditolak penulis.
	Actions []string `json:"actions"`

	// Status adalah active atau disabled.
	Status *string `json:"status"`
}

// Validate menolak perubahan kosong dan status yang tidak sah.
func (q UpdatePolicyRequest) Validate() error {
	if q.Name == nil && q.Description == nil && q.Effect == nil && q.Expression == nil && q.Actions == nil && q.Status == nil {
		return httpx.Validation(httpx.FieldError{Code: "empty_patch", Message: "the request changes nothing"})
	}
	var bad httpx.Fields
	if q.Status != nil && !domain.ValidPolicyStatus(*q.Status) {
		bad.Add("status", "invalid", "status must be active or disabled")
	}
	return bad.Err()
}

// Input mengubah body menjadi masukan penulis. Field yang tidak dikirim tetap nil dan Actions
// disalin.
func (q UpdatePolicyRequest) Input() application.UpdatePolicyInput {
	in := application.UpdatePolicyInput{
		Name:        clonePtr(q.Name),
		Description: clonePtr(q.Description),
		Expression:  clonePtr(q.Expression),
		Status:      clonePtr(q.Status),
	}
	if q.Effect != nil {
		effect := policy.Effect(*q.Effect)
		in.Effect = &effect
	}
	if q.Actions != nil {
		in.Actions = slices.Clone(q.Actions)
	}
	return in
}

// PolicyResponse adalah satu policy pada respons. organization_id sengaja tidak ditampilkan;
// global menandai policy yang berlaku di semua organisasi (hanya-baca bagi pemanggil tenant).
type PolicyResponse struct {
	// ID adalah id policy.
	ID uuid.UUID `json:"id"`

	// Code adalah kode policy.
	Code string `json:"code"`

	// Name adalah nama tampilan.
	Name string `json:"name"`

	// Description adalah deskripsi (kosong bila tidak ada).
	Description string `json:"description"`

	// Effect adalah allow atau deny.
	Effect string `json:"effect"`

	// Expression adalah ekspresi policy.
	Expression string `json:"expression"`

	// Actions adalah kode permission yang dibatasi (tidak pernah null).
	Actions []string `json:"actions"`

	// Status adalah active atau disabled.
	Status string `json:"status"`

	// Global benar untuk policy global.
	Global bool `json:"global"`

	// CreatedAt adalah waktu pembuatan.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt adalah waktu perubahan terakhir.
	UpdatedAt time.Time `json:"updated_at"`
}

// NewPolicyResponse membentuk respons dari satu record. Actions disalin dan tidak pernah nil.
func NewPolicyResponse(rec domain.PolicyRecord) PolicyResponse {
	var resp PolicyResponse
	resp.ID = rec.ID
	resp.Code = rec.Code
	resp.Name = rec.Name
	resp.Description = rec.Description
	resp.Effect = string(rec.Effect)
	resp.Expression = rec.Expression
	resp.Actions = slices.Clone(rec.Actions)
	if resp.Actions == nil {
		resp.Actions = []string{}
	}
	resp.Status = rec.Status
	resp.Global = rec.Global()
	resp.CreatedAt = rec.CreatedAt
	resp.UpdatedAt = rec.UpdatedAt
	return resp
}
