package domain

const (
	// DefaultPageSize adalah ukuran halaman bila pemanggil tidak menyebutnya (docs/08 §4).
	DefaultPageSize = 25

	// MaxPageSize adalah ukuran halaman terbesar; nilai lebih besar dipangkas, bukan ditolak.
	MaxPageSize = 100
)

// Window adalah jendela offset pada daftar kecil yang dikelola admin (docs/08 §4: role, permission,
// policy). Repositori selalu memangkasnya lewat ClampWindow, jadi pemanggil yang lalai tidak dapat
// meminta halaman tanpa batas.
type Window struct {
	// Offset adalah jumlah baris yang dilewati.
	Offset int

	// Limit adalah jumlah baris maksimum yang dikembalikan.
	Limit int
}

// ClampWindow memangkas Window ke batas yang sah: Offset tidak negatif, Limit antara 1 dan
// MaxPageSize (Limit nol atau negatif menjadi DefaultPageSize).
func ClampWindow(w Window) Window {
	if w.Offset < 0 {
		w.Offset = 0
	}
	if w.Limit < 1 {
		w.Limit = DefaultPageSize
	}
	if w.Limit > MaxPageSize {
		w.Limit = MaxPageSize
	}
	return w
}

// Nilai filter status pada daftar role. Role terarsip tidak dapat dikembalikan ke aktif, jadi
// daftar bawaan hanya memuat role aktif.
const (
	// RoleFilterActive hanya memuat role aktif (bawaan).
	RoleFilterActive = "active"

	// RoleFilterArchived hanya memuat role terarsip.
	RoleFilterArchived = "archived"

	// RoleFilterAll memuat role aktif dan terarsip.
	RoleFilterAll = "all"
)

// ValidRoleFilter melaporkan apakah s salah satu nilai filter status role.
func ValidRoleFilter(s string) bool {
	return s == RoleFilterActive || s == RoleFilterArchived || s == RoleFilterAll
}

// RoleListQuery adalah masukan daftar role. Status kosong berarti RoleFilterActive.
type RoleListQuery struct {
	// Status adalah salah satu RoleFilter*.
	Status string

	// Window adalah jendela halaman.
	Window Window
}

// RolePage adalah satu halaman role beserta jumlah seluruhnya.
type RolePage struct {
	// Items adalah role pada halaman ini (tidak pernah nil).
	Items []RoleRecord

	// Total adalah jumlah role yang cocok dengan filter, di semua halaman.
	Total int
}

// PermissionRecord adalah satu baris katalog permission.
type PermissionRecord struct {
	// Code adalah kode lengkap, mis. role.manage.
	Code string

	// Resource adalah bagian kode sebelum titik terakhir.
	Resource string

	// Action adalah bagian kode setelah titik terakhir.
	Action string

	// Description adalah uraian bebas (kosong bila tidak ada).
	Description string

	// Module adalah kode modul pemilik; kosong untuk permission core.
	Module string

	// System benar untuk permission yang dideklarasikan core.
	System bool
}

// PermissionPage adalah satu halaman katalog permission beserta jumlah seluruhnya.
type PermissionPage struct {
	// Items adalah permission pada halaman ini (tidak pernah nil).
	Items []PermissionRecord

	// Total adalah jumlah seluruh permission di katalog.
	Total int
}
