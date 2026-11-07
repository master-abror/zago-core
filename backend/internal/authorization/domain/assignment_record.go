package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrAssignmentNotFound menandai assignment yang tidak ada ATAU bukan milik organisasi
	// pemanggil. Kedua keadaan sengaja tidak dibedakan (tidak membocorkan keberadaan lintas
	// tenant), seperti ErrRoleNotFound.
	ErrAssignmentNotFound = errors.New("authorization: assignment tidak ditemukan")

	// ErrAssignmentRevoked menandai pencabutan assignment yang sudah dicabut sebelumnya.
	ErrAssignmentRevoked = errors.New("authorization: assignment sudah dicabut")

	// ErrAssignmentExists menandai assignment aktif yang sama (role, subjek, dan scope) yang sudah
	// ada. Untuk scope organisasi, dua bentuk kolom scope_id (NULL dan id organisasi) dianggap
	// sama; indeks unik database tidak menyamakannya, jadi repositori memeriksanya sendiri di
	// bawah kunci advisory.
	ErrAssignmentExists = errors.New("authorization: assignment aktif yang sama sudah ada")

	// ErrSubjectNotFound menandai subjek assignment yang tidak ada, tidak aktif, bukan anggota
	// organisasi scope-nya, atau bukan milik organisasi pemanggil. Keadaan-keadaan itu tidak
	// dibedakan (tidak membocorkan keberadaan lintas tenant).
	ErrSubjectNotFound = errors.New("authorization: subjek assignment tidak ditemukan")
)

// AssignmentRecord adalah satu baris role_assignments seperti dibaca jalur tulis assignment.
// Scope memakai bentuk target (platform, organisasi, atau grup); kolom scope_type resource dan
// own tidak pernah dibuat lewat API dan ditolak TargetFromColumns.
type AssignmentRecord struct {
	ID          uuid.UUID
	RoleID      uuid.UUID
	SubjectType string
	SubjectID   uuid.UUID
	Scope       GrantTarget
	AssignedBy  *uuid.UUID
	ExpiresAt   *time.Time
	RevokedAt   *time.Time
	CreatedAt   time.Time
}

// Active melaporkan apakah assignment berlaku pada waktu now: belum dicabut dan belum
// kedaluwarsa. Assignment tanpa expires_at tidak pernah kedaluwarsa.
func (r AssignmentRecord) Active(now time.Time) bool {
	if r.RevokedAt != nil {
		return false
	}
	return r.ExpiresAt == nil || r.ExpiresAt.After(now)
}

// Revocation mengubah record menjadi AssignmentRef untuk CheckRevocation; role adalah role yang
// dirujuk assignment itu, dibaca pemanggil dari tabel roles.
func (r AssignmentRecord) Revocation(role RoleRef, now time.Time) AssignmentRef {
	return AssignmentRef{Role: role, Scope: r.Scope, Active: r.Active(now)}
}

// ScopeColumns memetakan target ke kolom role_assignments (scope_type, organization_id,
// scope_id) dalam bentuk kanonik: platform tanpa organisasi dan scope_id; organisasi dengan
// organization_id dan scope_id NULL (docs/02 §16 aturan 6, "organisasi milik assignment itu
// sendiri"); grup dengan keduanya. Target yang tidak sah memberi ErrInvalidGrantTarget. Pointer
// yang dikembalikan adalah salinan.
func ScopeColumns(t GrantTarget) (string, *uuid.UUID, *uuid.UUID, error) {
	if err := t.Validate(); err != nil {
		return "", nil, nil, err
	}
	switch t.Level {
	case LevelPlatform:
		return string(LevelPlatform), nil, nil, nil
	case LevelOrganization:
		return string(LevelOrganization), cloneID(t.OrganizationID), nil, nil
	default:
		return string(LevelGroup), cloneID(t.OrganizationID), cloneID(t.GroupID), nil
	}
}

// TargetFromColumns adalah kebalikan ScopeColumns untuk baris yang dibaca dari database. Scope
// organisasi menerima kedua bentuk yang sah di database (scope_id NULL, atau sama dengan
// organization_id) dan mengembalikan target yang sama. Kombinasi lain, termasuk scope_type
// resource, own, atau yang tidak dikenal, memberi ErrInvalidGrantTarget: baris yang tidak dapat
// dipahami tidak pernah dianggap sah.
func TargetFromColumns(scopeType string, organizationID, scopeID *uuid.UUID) (GrantTarget, error) {
	var t GrantTarget
	switch DecisionLevel(scopeType) {
	case LevelPlatform:
		if organizationID != nil || scopeID != nil {
			return GrantTarget{}, ErrInvalidGrantTarget
		}
		t = PlatformTarget()
	case LevelOrganization:
		if !validID(organizationID) || (scopeID != nil && *scopeID != *organizationID) {
			return GrantTarget{}, ErrInvalidGrantTarget
		}
		t = OrganizationTarget(*organizationID)
	case LevelGroup:
		if !validID(organizationID) || !validID(scopeID) {
			return GrantTarget{}, ErrInvalidGrantTarget
		}
		t = GroupTarget(*organizationID, *scopeID)
	default:
		return GrantTarget{}, ErrInvalidGrantTarget
	}
	return t, nil
}
