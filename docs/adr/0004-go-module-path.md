# ADR-0004 — Path modul Go

**Status:** Diterima · **Tanggal:** 2026-10-03 · **Keputusan oleh:** pemilik proyek (Abror)

## Konteks
`go.mod` memakai `module platform` (placeholder). Repositori akan dipublikasikan di GitHub sebagai
`github.com/master-abror/zago-core`. Mengganti path modul menyentuh setiap impor Go, jadi paling murah dilakukan
sebelum M01 menambah kode.

## Keputusan
Path modul: **`github.com/master-abror/zago-core`** (sama dengan URL repositori, sehingga `go get` dan `pkg.go.dev` bekerja).

## Konsekuensi
- Semua impor internal berbentuk `github.com/master-abror/zago-core/backend/...`; modul bisnis hanya boleh
  mengimpor `github.com/master-abror/zago-core/packages/module-sdk` (docs/10 §4).
- `scripts/lint-structure.sh` memakai path baru untuk aturan "modul tak boleh mengimpor `backend/internal|cmd`";
  aturan itu diuji dengan pelanggaran sengaja (gagal) dan impor `module-sdk` (lulus).
- `go.sum` tidak terpengaruh (hanya memuat dependensi, bukan modul utama).
- Bila repositori dipindah atau diganti nama, path modul harus diganti lagi (mekanis: `go.mod`, impor, `lint-structure.sh`).
- Dokumen desain 00–23 tidak menyebut path modul, jadi tidak ada doc-sync.
