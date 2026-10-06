// Package migrations menyematkan migrasi SQL inti (docs/04 §14–§15) ke dalam binary sehingga
// cmd/migrate yang sama berjalan lokal, di CI, dan di image produksi tanpa menyalin folder.
// Berkas: NNN_nama.up.sql / NNN_nama.down.sql; nomor monoton dan tak pernah dipakai ulang.
package migrations

import "embed"

// FS berisi seluruh berkas *.sql di folder ini (akar FS = folder ini).
//
//go:embed *.sql
var FS embed.FS
