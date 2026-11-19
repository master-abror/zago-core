package domain

import (
	"time"

	"github.com/google/uuid"
)

// Nilai filter status pada daftar assignment. Nilai yang sama dipakai AssignmentRecord.State
// sebagai status satu assignment. Daftar bawaan hanya memuat assignment yang sedang berlaku.
const (
	// AssignmentFilterActive memuat assignment yang belum dicabut dan belum kedaluwarsa (bawaan).
	AssignmentFilterActive = "active"

	// AssignmentFilterRevoked memuat assignment yang sudah dicabut, kedaluwarsa atau tidak.
	AssignmentFilterRevoked = "revoked"

	// AssignmentFilterExpired memuat assignment yang belum dicabut tetapi masa berlakunya lewat.
	AssignmentFilterExpired = "expired"

	// AssignmentFilterAll memuat semuanya.
	AssignmentFilterAll = "all"
)

// ValidAssignmentFilter melaporkan apakah s salah satu nilai filter status assignment. Pencocokan
// persis (huruf besar dan spasi ditolak): nilai klien tidak dinormalkan diam-diam.
func ValidAssignmentFilter(s string) bool {
	switch s {
	case AssignmentFilterActive, AssignmentFilterRevoked, AssignmentFilterExpired, AssignmentFilterAll:
		return true
	}
	return false
}

// State mengembalikan status assignment pada waktu now: revoked bila sudah dicabut (mengalahkan
// kedaluwarsa), expired bila masa berlakunya sudah lewat atau tepat sekarang, selain itu active.
// State sama dengan active tepat ketika Active(now) benar.
func (r AssignmentRecord) State(now time.Time) string {
	switch {
	case r.RevokedAt != nil:
		return AssignmentFilterRevoked
	case r.ExpiresAt != nil && !r.ExpiresAt.After(now):
		return AssignmentFilterExpired
	default:
		return AssignmentFilterActive
	}
}

// AssignmentPosition adalah baris terakhir pada satu halaman: kunci keyset (created_at, id).
// Domain tidak mengimpor lapisan HTTP, jadi handler menerjemahkannya dari/ke cursor bertanda
// tangan.
type AssignmentPosition struct {
	CreatedAt time.Time
	ID        uuid.UUID
}

// AssignmentListQuery adalah masukan daftar assignment. Urutannya tetap: terbaru dulu
// (created_at DESC, id DESC). Organisasi tidak ada di sini: ia selalu datang dari ScopeContext
// sebagai parameter repositori terpisah, tidak pernah dari filter klien.
type AssignmentListQuery struct {
	// Status adalah salah satu AssignmentFilter*; kosong berarti AssignmentFilterActive.
	Status string

	// RoleID menyaring satu role; nil berarti semua role.
	RoleID *uuid.UUID

	// SubjectID menyaring satu subjek; nil berarti semua subjek.
	SubjectID *uuid.UUID

	// After adalah posisi terakhir halaman sebelumnya; nil berarti halaman pertama. Posisi hanya
	// membandingkan urutan dan tidak pernah melebarkan scope.
	After *AssignmentPosition

	// Limit adalah jumlah baris yang diminta (pemanggil meminta satu baris lebih banyak dari ukuran
	// halaman untuk mengetahui has_more). Repositori memangkasnya lewat ClampAssignmentLimit.
	Limit int

	// Now adalah waktu acuan untuk status active/expired; nol berarti waktu sekarang.
	Now time.Time
}

// ClampAssignmentLimit memangkas Limit ke batas yang sah: nol atau negatif menjadi
// DefaultPageSize+1, dan paling banyak MaxPageSize+1 (satu halaman penuh ditambah baris pengintai
// has_more). Pemanggil yang lalai tidak dapat meminta halaman tanpa batas.
func ClampAssignmentLimit(n int) int {
	if n < 1 {
		return DefaultPageSize + 1
	}
	if n > MaxPageSize+1 {
		return MaxPageSize + 1
	}
	return n
}

// AssignmentView adalah satu baris daftar assignment: record ditambah ringkasan role yang
// dirujuknya (nama, slug, status). Ringkasan role kosong bila role tidak dapat dibaca dari
// organisasi assignment itu (keadaan yang tidak dibuat jalur tulis).
type AssignmentView struct {
	AssignmentRecord

	RoleName string

	RoleSlug string

	RoleStatus string
}
