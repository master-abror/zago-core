package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Tipe role (kolom roles.role_type, docs/02 §12).
const (
	// RoleTypeSystem: role global dari kode (organization_id dan group_id NULL).
	RoleTypeSystem = "system"

	// RoleTypeOrganization: role milik satu organisasi.
	RoleTypeOrganization = "organization"

	// RoleTypeGroup: role milik satu grup.
	RoleTypeGroup = "group"

	// RoleTypeCustom: role buatan administrator (organisasi wajib, grup opsional).
	RoleTypeCustom = "custom"
)

// Slug role sistem. Nilainya HARUS sama dengan internal/role (Slug*); domain tidak mengimpor
// paket itu supaya tetap murni, dan tes (TestSystemTemplateTableMatchesTheRoleCatalog) menjaga
// kesamaannya serta kelengkapan tabel template.
const (
	RoleSlugSuperAdmin = "super-admin"

	RoleSlugOrgAdmin = "organization-admin"

	RoleSlugGroupAdmin = "group-admin"

	RoleSlugMember = "member"
)

// Tipe subjek assignment (kolom role_assignments.subject_type, docs/02 §15).
const (
	SubjectUser = "user"

	SubjectGroup = "group"

	SubjectOrganization = "organization"

	// SubjectService belum dievaluasi engine (Effective mengabaikannya), sehingga assignment untuk
	// subjek ini ditolak: assignment yang tidak pernah berlaku menyesatkan administrator.
	SubjectService = "service"
)

var (
	// ErrInvalidRole menandai RoleRef yang bentuknya tidak sah (slug kosong, tipe tak dikenal,
	// atau kolom boundary yang tidak cocok dengan tipenya; docs/02 §12 aturan 3).
	ErrInvalidRole = errors.New("authorization: role tidak sah")

	// ErrRoleTypeForbidden menandai upaya membuat role bertipe system lewat API: role sistem hanya
	// berasal dari kode (ADR-0022 butir 9).
	ErrRoleTypeForbidden = errors.New("authorization: tipe role tidak boleh dibuat lewat API")

	// ErrSystemRoleImmutable menandai upaya mengubah role sistem (permission, status, hapus) lewat
	// API; klon sebagai role custom (ADR-0022 butir 9).
	ErrSystemRoleImmutable = errors.New("authorization: role sistem tidak dapat diubah lewat API")

	// ErrRoleArchived menandai assignment pada role yang diarsipkan.
	ErrRoleArchived = errors.New("authorization: role diarsipkan")

	// ErrAssignmentScope menandai role yang tidak boleh ditetapkan pada scope itu (ADR-0022 butir
	// 9): super-admin hanya di platform dan hanya super-admin di platform; template sistem lain
	// hanya di scope docs/06 §5.5; role non-sistem hanya di dalam boundary-nya.
	ErrAssignmentScope = errors.New("authorization: role tidak boleh ditetapkan pada scope itu")

	// ErrAssignmentSubject menandai subjek yang tidak sah untuk assignment itu.
	ErrAssignmentSubject = errors.New("authorization: subjek assignment tidak sah")

	// ErrAssignmentExpiry menandai masa berlaku yang tidak sah: sudah lewat, atau diberikan pada
	// assignment platform (super-admin tidak boleh kedaluwarsa diam-diam, ADR-0022 butir 47).
	ErrAssignmentExpiry = errors.New("authorization: masa berlaku assignment tidak sah")

	// ErrInvalidAssignment menandai masukan pemeriksaan pencabutan yang tidak masuk akal.
	ErrInvalidAssignment = errors.New("authorization: assignment tidak sah")

	// ErrLastSuperAdmin menandai pencabutan assignment super-admin platform terakhir yang aktif
	// (ADR-0022 butir 9): tidak boleh, karena platform tidak lagi punya administrator.
	ErrLastSuperAdmin = errors.New("authorization: assignment super-admin platform terakhir tidak boleh dicabut")
)

// RoleRef adalah gambaran role yang dibutuhkan aturan domain; diisi repositori dari tabel roles.
type RoleRef struct {
	Slug           string
	Type           string
	OrganizationID *uuid.UUID
	GroupID        *uuid.UUID
	Archived       bool
}

// systemTemplateLevels memuat scope tunggal tempat tiap role sistem boleh ditetapkan (docs/06
// §5.5). Role sistem yang tidak ada di sini tidak dapat ditetapkan (gagal-tertutup); tes
// memastikan setiap role di internal/role punya baris.
var systemTemplateLevels = []struct {
	slug  string
	level DecisionLevel
}{
	{slug: RoleSlugSuperAdmin, level: LevelPlatform},
	{slug: RoleSlugOrgAdmin, level: LevelOrganization},
	{slug: RoleSlugGroupAdmin, level: LevelGroup},
	{slug: RoleSlugMember, level: LevelOrganization},
}

func systemTemplateLevel(slug string) (DecisionLevel, bool) {
	for _, e := range systemTemplateLevels {
		if e.slug == slug {
			return e.level, true
		}
	}
	return "", false
}

// validBoundary memeriksa kolom boundary terhadap tipe role (docs/02 §12 aturan 3).
func validBoundary(roleType string, org, group *uuid.UUID) bool {
	switch roleType {
	case RoleTypeSystem:
		return org == nil && group == nil
	case RoleTypeOrganization:
		return validID(org) && group == nil
	case RoleTypeGroup:
		return validID(org) && validID(group)
	case RoleTypeCustom:
		return validID(org) && (group == nil || validID(group))
	default:
		return false
	}
}

func (r RoleRef) validate() error {
	if r.Slug == "" || !validBoundary(r.Type, r.OrganizationID, r.GroupID) {
		return ErrInvalidRole
	}
	return nil
}

// boundaryTarget memberi target scope sebuah role non-sistem yang boundary-nya sudah divalidasi:
// grup bila group_id terisi, selain itu organisasi.
func boundaryTarget(org, group *uuid.UUID) GrantTarget {
	if group != nil {
		return GroupTarget(*org, *group)
	}
	return OrganizationTarget(*org)
}

// ValidateRoleBoundary memeriksa boundary role yang akan dibuat lewat API dan mengembalikan target
// scope-nya, yang menjadi target CheckCeiling untuk permission role itu (grup bila group_id
// terisi, selain itu organisasi). Tipe system ditolak (ErrRoleTypeForbidden); tipe tak dikenal
// atau kolom yang tidak cocok ditolak (ErrInvalidRole).
func ValidateRoleBoundary(roleType string, org, group *uuid.UUID) (GrantTarget, error) {
	if roleType == RoleTypeSystem {
		return GrantTarget{}, ErrRoleTypeForbidden
	}
	if !validBoundary(roleType, org, group) {
		return GrantTarget{}, ErrInvalidRole
	}
	return boundaryTarget(org, group), nil
}

// CheckRoleMutable menolak setiap perubahan role sistem lewat API: set permission, status, atau
// hapus (ADR-0022 butir 9). Role non-sistem boleh diubah (tunduk pada authority ceiling dan izin
// role.manage di tempat lain).
func CheckRoleMutable(r RoleRef) error {
	if err := r.validate(); err != nil {
		return err
	}
	if r.Type == RoleTypeSystem {
		return ErrSystemRoleImmutable
	}
	return nil
}

// AssignmentDraft adalah assignment yang akan dibuat. ExpiresAt nil = tanpa masa berlaku.
type AssignmentDraft struct {
	Role        RoleRef
	SubjectType string
	SubjectID   uuid.UUID
	Scope       GrantTarget
	ExpiresAt   *time.Time
}

// ValidateAssignment menegakkan aturan assignment yang murni (tanpa database): bentuk scope dan
// role; role tidak diarsipkan; kecocokan role dengan scope (ADR-0022 butir 9); subjek yang
// didukung engine; dan masa berlaku. Authority ceiling (CheckCeiling) dan keberadaan subjek,
// grup, serta organisasi diperiksa pemanggil di dalam transaksi yang sama.
//
// Aturan scope: super-admin hanya pada scope platform dan hanya super-admin pada scope platform
// (role non-sistem tidak pernah di platform). Template sistem lain hanya pada scope docs/06 §5.5
// (organization-admin dan member pada organization, group-admin pada group); role sistem yang
// tidak dikenal ditolak. Role non-sistem hanya di dalam boundary-nya: role organisasi pada
// organisasi itu atau grup di dalamnya; role yang dimiliki grup hanya pada grup itu.
//
// Subjek platform hanya user (Effective hanya mengevaluasi subjek user pada scope platform);
// subjek organization harus organisasi scope itu sendiri; subjek service ditolak. Assignment
// platform tidak boleh punya masa berlaku, dan masa berlaku harus di masa depan.
func ValidateAssignment(d AssignmentDraft, now time.Time) error {
	if err := d.Scope.Validate(); err != nil {
		return err
	}
	if err := d.Role.validate(); err != nil {
		return err
	}
	if d.Role.Archived {
		return ErrRoleArchived
	}
	if err := checkRoleScope(d.Role, d.Scope); err != nil {
		return err
	}
	if err := checkSubject(d); err != nil {
		return err
	}
	return checkExpiry(d, now)
}

func checkRoleScope(r RoleRef, s GrantTarget) error {
	if r.Type == RoleTypeSystem {
		level, ok := systemTemplateLevel(r.Slug)
		if !ok || level != s.Level {
			return ErrAssignmentScope
		}
		return nil
	}
	if s.Level == LevelPlatform || *r.OrganizationID != *s.OrganizationID {
		return ErrAssignmentScope
	}
	if r.GroupID != nil && (s.Level != LevelGroup || *s.GroupID != *r.GroupID) {
		return ErrAssignmentScope
	}
	return nil
}

func checkSubject(d AssignmentDraft) error {
	if d.SubjectID == uuid.Nil {
		return ErrAssignmentSubject
	}
	switch d.SubjectType {
	case SubjectUser:
		return nil
	case SubjectGroup:
		if d.Scope.Level == LevelPlatform {
			return ErrAssignmentSubject
		}
		return nil
	case SubjectOrganization:
		if d.Scope.Level == LevelPlatform || d.SubjectID != *d.Scope.OrganizationID {
			return ErrAssignmentSubject
		}
		return nil
	default:
		return ErrAssignmentSubject
	}
}

func checkExpiry(d AssignmentDraft, now time.Time) error {
	if d.ExpiresAt == nil {
		return nil
	}
	if d.Scope.Level == LevelPlatform || !d.ExpiresAt.After(now) {
		return ErrAssignmentExpiry
	}
	return nil
}

// AssignmentRef adalah gambaran assignment yang akan dicabut. Active berarti belum dicabut dan
// belum kedaluwarsa.
type AssignmentRef struct {
	Role   RoleRef
	Scope  GrantTarget
	Active bool
}

// CheckRevocation menolak pencabutan assignment super-admin platform terakhir yang aktif
// (ADR-0022 butir 9). activeSuperAdmins adalah jumlah assignment super-admin platform yang aktif
// DAN dapat dipakai (belum dicabut, belum kedaluwarsa, user aktif), dihitung pemanggil di dalam
// transaksi pencabutan di bawah advisory lock supaya dua pencabutan serentak tidak sama-sama
// melihat "masih ada satu lagi"; hitungan itu mencakup assignment yang akan dicabut bila aktif.
// Assignment yang sudah tidak aktif boleh dicabut (tidak mengurangi yang aktif). Hitungan negatif
// adalah masukan tidak masuk akal dan ditolak.
func CheckRevocation(a AssignmentRef, activeSuperAdmins int) error {
	if activeSuperAdmins < 0 {
		return ErrInvalidAssignment
	}
	if err := a.Role.validate(); err != nil {
		return err
	}
	isPlatformSuperAdmin := a.Role.Type == RoleTypeSystem && a.Role.Slug == RoleSlugSuperAdmin && a.Scope.Level == LevelPlatform
	if isPlatformSuperAdmin && a.Active && activeSuperAdmins <= 1 {
		return ErrLastSuperAdmin
	}
	return nil
}
