package domain

// PolicyFilterAll memuat policy aktif dan nonaktif. Itulah bawaan daftar policy: policy nonaktif
// tetap dikelola (dapat diaktifkan kembali lewat PATCH), jadi pengelola harus dapat melihatnya.
// Filter selain itu memakai nilai status (PolicyActive, PolicyDisabled).
const PolicyFilterAll = "all"

// ValidPolicyFilter melaporkan apakah s adalah PolicyFilterAll atau status policy yang sah.
func ValidPolicyFilter(s string) bool { return s == PolicyFilterAll || ValidPolicyStatus(s) }

// PolicyListQuery adalah masukan daftar policy. Status kosong berarti PolicyFilterAll.
type PolicyListQuery struct {
	// Status adalah PolicyFilterAll, PolicyActive, atau PolicyDisabled.
	Status string

	// Window adalah jendela halaman.
	Window Window
}

// PolicyPage adalah satu halaman policy beserta jumlah seluruhnya.
type PolicyPage struct {
	// Items adalah policy pada halaman ini (tidak pernah nil). PolicyRecord.Global() menandai
	// policy global.
	Items []PolicyRecord

	// Total adalah jumlah policy yang cocok dengan filter, di semua halaman.
	Total int
}
