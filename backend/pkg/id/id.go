// Package id adalah SATU-SATUNYA tempat identifier dibangkitkan (docs/10 §7, docs/04 §2.1).
// Semua primary key memakai UUIDv7: urut menurut waktu sehingga indeks B-tree tetap lokal.
package id

import "github.com/google/uuid"

// NewID mengembalikan UUIDv7 baru. Dalam satu proses hasilnya menaik secara ketat.
func NewID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// Parse mengurai UUID dari input yang tidak tepercaya. Handler HTTP wajib memakai ini
// (atau uuid.Parse), tidak pernah uuid.MustParse: input buruk tidak boleh membuat panik.
func Parse(s string) (uuid.UUID, error) {
	return uuid.Parse(s)
}
