package orghttp

import (
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
	"github.com/master-abror/zago-core/backend/internal/organization/domain"
)

// DTO endpoint anggota organisasi (docs/08 §10.6; ADR-0023 butir 12 dan 18). Organisasi
// berasal dari path dan diperiksa melawan ScopeContext oleh handler, bukan dari body: body tidak
// punya organization_id. Alamat email sengaja tidak ada pada respons (dibaca lewat jalur
// pengguna yang dilindungi field permission).

// AddMemberRequest adalah body POST /organizations/{id}/members.
type AddMemberRequest struct {
	// UserID adalah pengguna yang akan ditambahkan; wajib.
	UserID uuid.UUID `json:"user_id"`
}

// Validate menolak body tanpa user_id (field hilang, null, atau UUID nol).
func (q AddMemberRequest) Validate() error {
	if q.UserID == uuid.Nil {
		return httpx.Validation(httpx.FieldError{Field: "user_id", Code: "required", Message: "user_id is required"})
	}
	return nil
}

// MemberResponse adalah satu anggota organisasi pada respons. organization_id dan email sengaja
// tidak ditampilkan.
type MemberResponse struct {
	// ID adalah id baris keanggotaan.
	ID uuid.UUID `json:"id"`

	// UserID adalah pengguna yang menjadi anggota.
	UserID uuid.UUID `json:"user_id"`

	// DisplayName adalah nama tampilan pengguna.
	DisplayName string `json:"display_name"`

	// UserStatus adalah status akun pengguna, terpisah dari status keanggotaan.
	UserStatus string `json:"user_status"`

	// Status adalah invited, active, suspended, atau removed.
	Status string `json:"status"`

	// JoinedAt adalah waktu bergabung (UTC); null selama belum pernah aktif.
	JoinedAt *time.Time `json:"joined_at"`

	// InvitedBy adalah pengguna yang menambahkan atau mengundang; null bila tidak diketahui.
	InvitedBy *uuid.UUID `json:"invited_by"`

	// CreatedAt adalah waktu pembuatan baris (UTC).
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt adalah waktu perubahan terakhir (UTC).
	UpdatedAt time.Time `json:"updated_at"`
}

// NewMemberResponse membentuk respons dari baris anggota.
func NewMemberResponse(m domain.Member) MemberResponse {
	out := MemberResponse{
		ID:          m.ID,
		UserID:      m.UserID,
		DisplayName: m.DisplayName,
		UserStatus:  m.UserStatus,
		Status:      m.Status,
		InvitedBy:   m.InvitedBy,
		CreatedAt:   m.CreatedAt.UTC(),
		UpdatedAt:   m.UpdatedAt.UTC(),
	}
	if m.JoinedAt != nil {
		at := m.JoinedAt.UTC()
		out.JoinedAt = &at
	}
	return out
}

// NewMemberResponses membentuk larik respons; hasilnya tidak pernah nil.
func NewMemberResponses(rows []domain.Member) []MemberResponse {
	out := make([]MemberResponse, 0, len(rows))
	for _, m := range rows {
		out = append(out, NewMemberResponse(m))
	}
	return out
}
