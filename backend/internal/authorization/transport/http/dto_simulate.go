package authzhttp

import (
	"slices"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/permission"
)

// DTO endpoint POST /permissions/simulate (docs/06 §10-§12; ADR-0022 butir 69). Body didekode
// lewat httpx.DecodeJSON yang menolak field tak dikenal: DTO sengaja TIDAK punya organization_id
// (organisasi simulasi selalu organisasi pemanggil). Atribut sumber daya dinilai oleh Simulator
// (domain.NormalizeSimulationAttrs) dan dipetakan DomainError.

// maxSimulateResourceTypeLen membatasi panjang resource.type.
const maxSimulateResourceTypeLen = 100

// SimulateRequest adalah body POST /permissions/simulate.
type SimulateRequest struct {
	// UserID adalah user yang disimulasikan; harus anggota aktif organisasi pemanggil.
	UserID uuid.UUID `json:"user_id"`

	// Action adalah kode permission yang ditanyakan.
	Action string `json:"action"`

	// ActiveGroupID adalah grup aktif simulasi (opsional). Pemanggil level group hanya boleh
	// mengosongkannya atau mengisinya dengan grup aktifnya sendiri.
	ActiveGroupID *uuid.UUID `json:"active_group_id"`

	// Resource adalah sumber daya hipotetis (opsional; kosong untuk aksi list/create).
	Resource *SimulateResourceRequest `json:"resource"`
}

// SimulateResourceRequest adalah sumber daya hipotetis pada simulasi. Organisasinya tidak ada
// di sini: selalu organisasi pemanggil.
type SimulateResourceRequest struct {
	// Type adalah jenis sumber daya (huruf kecil, angka, garis bawah; diawali huruf).
	Type string `json:"type"`

	// ID adalah id sumber daya.
	ID uuid.UUID `json:"id"`

	// OwnerID adalah pemilik sumber daya (opsional).
	OwnerID *uuid.UUID `json:"owner_id"`

	// GroupID adalah grup pemilik sumber daya (opsional).
	GroupID *uuid.UUID `json:"group_id"`

	// Attrs adalah atribut tambahan yang dapat dirujuk ekspresi policy: nilai skalar atau daftar
	// skalar, paling banyak 32 atribut; nama type, id, organization_id, owner_id, dan group_id
	// dicadangkan.
	Attrs map[string]any `json:"attrs"`
}

// Validate memeriksa bentuk body dan melaporkan SEMUA field yang salah sekaligus. Atribut
// sumber daya dinilai Simulator, bukan di sini.
func (q SimulateRequest) Validate() error {
	var bad httpx.Fields
	if q.UserID == uuid.Nil {
		bad.Add("user_id", "required", "user_id is required")
	}
	if q.Action == "" {
		bad.Add("action", "required", "action is required")
	} else if _, _, err := permission.Split(q.Action); err != nil {
		bad.Add("action", "invalid", "action must be a valid permission code")
	}
	if q.ActiveGroupID != nil && *q.ActiveGroupID == uuid.Nil {
		bad.Add("active_group_id", "invalid", "active_group_id must not be empty")
	}
	if r := q.Resource; r != nil {
		if r.Type == "" {
			bad.Add("resource.type", "required", "resource.type is required")
		} else if !validSimulateResourceType(r.Type) {
			bad.Add("resource.type", "invalid", "resource.type must be lowercase letters, digits, and underscores, starting with a letter")
		}
		if r.ID == uuid.Nil {
			bad.Add("resource.id", "required", "resource.id is required")
		}
		if r.OwnerID != nil && *r.OwnerID == uuid.Nil {
			bad.Add("resource.owner_id", "invalid", "resource.owner_id must not be empty")
		}
		if r.GroupID != nil && *r.GroupID == uuid.Nil {
			bad.Add("resource.group_id", "invalid", "resource.group_id must not be empty")
		}
	}
	return bad.Err()
}

// Input mengubah body menjadi masukan Simulator. Validate wajib dipanggil lebih dulu.
func (q SimulateRequest) Input() application.SimulateInput {
	in := application.SimulateInput{
		UserID:        q.UserID,
		Action:        q.Action,
		ActiveGroupID: clonePtr(q.ActiveGroupID),
	}
	if r := q.Resource; r != nil {
		in.Resource = &application.SimulateResource{
			Type:    r.Type,
			ID:      r.ID,
			OwnerID: clonePtr(r.OwnerID),
			GroupID: clonePtr(r.GroupID),
			Attrs:   r.Attrs,
		}
	}
	return in
}

// validSimulateResourceType memeriksa pola jenis sumber daya: huruf kecil ASCII di awal, lalu huruf
// kecil, angka, atau garis bawah; paling panjang maxSimulateResourceTypeLen byte.
func validSimulateResourceType(s string) bool {
	if s == "" || len(s) > maxSimulateResourceTypeLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		lower := c >= 'a' && c <= 'z'
		digit := c >= '0' && c <= '9'
		if !lower && (i == 0 || (!digit && c != '_')) {
			return false
		}
	}
	return true
}

// Hasil simulasi pada respons.
const (
	// SimulateAllow adalah nilai result bila engine mengizinkan.
	SimulateAllow = "ALLOW"

	// SimulateDeny adalah nilai result bila engine menolak.
	SimulateDeny = "DENY"
)

// SimulateResponse adalah jawaban ringkas simulasi (tanpa rincian evaluasi).
type SimulateResponse struct {
	// Result adalah ALLOW atau DENY.
	Result string `json:"result"`

	// Reason adalah kode alasan stabil: granted, no_permission, scope_mismatch, policy_denied,
	// atau policy_evaluation_failed.
	Reason string `json:"reason"`

	// Level adalah scope yang memberi izin; null bila ditolak.
	Level *string `json:"level"`

	// PolicyCode adalah kode policy yang menolak; null bila tidak ada atau bila pemanggil tidak
	// berhak membaca policy (policy.read).
	PolicyCode *string `json:"policy_code"`

	// DeferredPolicies adalah kode policy yang butuh sumber daya tetapi simulasi tanpa resource;
	// tidak pernah null, dikosongkan bila pemanggil tidak berhak membaca policy.
	DeferredPolicies []string `json:"deferred_policies"`
}

// NewSimulateResponse membentuk respons dari hasil simulasi. Daftar disalin dan tidak pernah nil.
func NewSimulateResponse(res application.SimulationResult) SimulateResponse {
	resp := SimulateResponse{Result: SimulateDeny, Reason: res.Reason}
	if res.Allowed {
		resp.Result = SimulateAllow
	}
	if res.Level != "" {
		level := string(res.Level)
		resp.Level = &level
	}
	if res.PolicyCode != "" {
		code := res.PolicyCode
		resp.PolicyCode = &code
	}
	resp.DeferredPolicies = slices.Clone(res.DeferredPolicies)
	if resp.DeferredPolicies == nil {
		resp.DeferredPolicies = []string{}
	}
	return resp
}
