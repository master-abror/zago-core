package authzhttp

import (
	"time"

	"github.com/google/uuid"

	"github.com/master-abror/zago-core/backend/internal/authorization/application"
	"github.com/master-abror/zago-core/backend/internal/authorization/domain"
	"github.com/master-abror/zago-core/backend/internal/kernel/httpx"
)

// DTO endpoint role-assignments (docs/06 §10-§11). Body didekode lewat httpx.DecodeJSON yang
// menolak field tak dikenal: DTO sengaja TIDAK punya organization_id, karena organisasi hanya
// boleh berasal dari ScopeContext (ADR-0022 butir 49). Body yang membawanya ditolak sebagai
// unknown_field. Nilai subject_type dinilai penulis (domain.ErrAssignmentSubject); DTO hanya
// memeriksa bentuk.

// CreateAssignmentRequest adalah body POST /role-assignments.
type CreateAssignmentRequest struct {
	// RoleID adalah role yang diberikan.
	RoleID uuid.UUID `json:"role_id"`

	// SubjectType adalah jenis subjek (nilai sah dinilai penulis).
	SubjectType string `json:"subject_type"`

	// SubjectID adalah subjek penerima.
	SubjectID uuid.UUID `json:"subject_id"`

	// ScopeType adalah platform, organization, atau group. Organisasi scope diambil dari sesi.
	ScopeType string `json:"scope_type"`

	// GroupID wajib untuk scope group dan hanya untuk itu.
	GroupID *uuid.UUID `json:"group_id"`

	// ExpiresAt adalah masa berlaku (RFC 3339); kosong berarti tanpa masa berlaku.
	ExpiresAt *time.Time `json:"expires_at"`
}

// Validate memeriksa bentuk body dan melaporkan SEMUA field yang salah sekaligus.
func (q CreateAssignmentRequest) Validate() error {
	var bad httpx.Fields
	if q.RoleID == uuid.Nil {
		bad.Add("role_id", "required", "role_id is required")
	}
	if q.SubjectType == "" {
		bad.Add("subject_type", "required", "subject_type is required")
	}
	if q.SubjectID == uuid.Nil {
		bad.Add("subject_id", "required", "subject_id is required")
	}
	switch domain.DecisionLevel(q.ScopeType) {
	case domain.LevelPlatform, domain.LevelOrganization:
		if q.GroupID != nil {
			bad.Add("group_id", "invalid", "group_id is only allowed for group scope")
		}
	case domain.LevelGroup:
		if q.GroupID == nil || *q.GroupID == uuid.Nil {
			bad.Add("group_id", "required", "group_id is required for group scope")
		}
	default:
		if q.ScopeType == "" {
			bad.Add("scope_type", "required", "scope_type is required")
		} else {
			bad.Add("scope_type", "invalid", "scope_type must be platform, organization, or group")
		}
	}
	return bad.Err()
}

// Target membentuk target pemberian dari body dan scope pemanggil. Organisasi target SELALU
// organisasi scope pemanggil. Pemanggil platform tidak dapat menargetkan organisasi atau grup
// dari scope platform (ADR-0022 butir 48; masuk dulu lewat switch-organization): galatnya
// domain.ErrTargetOutsideScope dan penulis tidak dipanggil. Scope platform dari pemanggil
// tenant diteruskan apa adanya; penulis yang menolaknya (dan mengauditnya).
//
// Validate wajib dipanggil lebih dulu. Group tanpa group_id atau scope_type tak dikenal di sini
// adalah kesalahan pemrograman (domain.ErrInvalidGrantTarget, 500), bukan kesalahan klien.
func (q CreateAssignmentRequest) Target(scope domain.ScopeContext) (domain.GrantTarget, error) {
	org, tenant := scope.Tenant()
	switch domain.DecisionLevel(q.ScopeType) {
	case domain.LevelPlatform:
		return domain.PlatformTarget(), nil
	case domain.LevelOrganization:
		if !tenant {
			return domain.GrantTarget{}, domain.ErrTargetOutsideScope
		}
		return domain.OrganizationTarget(org), nil
	case domain.LevelGroup:
		if q.GroupID == nil || *q.GroupID == uuid.Nil {
			return domain.GrantTarget{}, domain.ErrInvalidGrantTarget
		}
		if !tenant {
			return domain.GrantTarget{}, domain.ErrTargetOutsideScope
		}
		return domain.GroupTarget(org, *q.GroupID), nil
	default:
		return domain.GrantTarget{}, domain.ErrInvalidGrantTarget
	}
}

// Input mengubah body dan target menjadi masukan penulis.
func (q CreateAssignmentRequest) Input(target domain.GrantTarget) application.CreateAssignmentInput {
	return application.CreateAssignmentInput{
		RoleID:      q.RoleID,
		SubjectType: q.SubjectType,
		SubjectID:   q.SubjectID,
		Scope:       target,
		ExpiresAt:   q.ExpiresAt,
	}
}

// AssignmentResponse adalah satu assignment pada respons. organization_id sengaja tidak
// ditampilkan: organisasi selalu organisasi pemanggil (atau platform).
type AssignmentResponse struct {
	// ID adalah id assignment.
	ID uuid.UUID `json:"id"`

	// RoleID adalah role yang diberikan.
	RoleID uuid.UUID `json:"role_id"`

	// RoleName adalah nama role (kosong bila role tidak terbaca).
	RoleName string `json:"role_name"`

	// RoleSlug adalah slug role.
	RoleSlug string `json:"role_slug"`

	// RoleStatus adalah active atau archived.
	RoleStatus string `json:"role_status"`

	// SubjectType adalah jenis subjek.
	SubjectType string `json:"subject_type"`

	// SubjectID adalah subjek penerima.
	SubjectID uuid.UUID `json:"subject_id"`

	// ScopeType adalah platform, organization, atau group.
	ScopeType string `json:"scope_type"`

	// GroupID terisi untuk scope group, selain itu null.
	GroupID *uuid.UUID `json:"group_id"`

	// AssignedBy adalah pemberi; null bila tidak tercatat.
	AssignedBy *uuid.UUID `json:"assigned_by"`

	// ExpiresAt adalah masa berlaku; null berarti tidak kedaluwarsa.
	ExpiresAt *time.Time `json:"expires_at"`

	// RevokedAt terisi bila assignment sudah dicabut.
	RevokedAt *time.Time `json:"revoked_at"`

	// CreatedAt adalah waktu pembuatan.
	CreatedAt time.Time `json:"created_at"`

	// Status adalah active, expired, atau revoked pada saat respons dibuat.
	Status string `json:"status"`
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func assignmentBase(rec domain.AssignmentRecord, now time.Time) AssignmentResponse {
	var resp AssignmentResponse
	resp.ID = rec.ID
	resp.RoleID = rec.RoleID
	resp.SubjectType = rec.SubjectType
	resp.SubjectID = rec.SubjectID
	resp.ScopeType = string(rec.Scope.Level)
	resp.GroupID = clonePtr(rec.Scope.GroupID)
	resp.AssignedBy = clonePtr(rec.AssignedBy)
	resp.ExpiresAt = clonePtr(rec.ExpiresAt)
	resp.RevokedAt = clonePtr(rec.RevokedAt)
	resp.CreatedAt = rec.CreatedAt
	resp.Status = rec.State(now)
	return resp
}

// NewAssignmentResponse membentuk respons dari satu baris daftar.
func NewAssignmentResponse(v domain.AssignmentView, now time.Time) AssignmentResponse {
	resp := assignmentBase(v.AssignmentRecord, now)
	resp.RoleName = v.RoleName
	resp.RoleSlug = v.RoleSlug
	resp.RoleStatus = v.RoleStatus
	return resp
}

// NewAssignmentResultResponse membentuk respons dari hasil penulis.
func NewAssignmentResultResponse(res application.AssignmentResult, now time.Time) AssignmentResponse {
	resp := assignmentBase(res.Assignment, now)
	resp.RoleName = res.Role.Name
	resp.RoleSlug = res.Role.Slug
	resp.RoleStatus = res.Role.Status
	return resp
}
