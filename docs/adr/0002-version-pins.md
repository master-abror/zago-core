# ADR-0002 — Versi yang dipatok (M00)

**Status:** Diterima (sebagian TERBUKA, lihat bawah)
**Tanggal:** 2026-10-03

## Konteks
ADR-0001 meminta versi patch persis dipatok saat M00. Sandbox sesi M00 tidak punya Docker,
sehingga tag image tidak bisa diverifikasi dengan `docker pull`.

## Keputusan
- **Go 1.26.5**, bukan 1.27: ADR-0001 berkata "stable terbaru", tetapi 1.27.0 baru rilis
  Agustus 2026 dan kompatibilitas toolchain pihak ketiga (air, sqlc, golangci-lint) belum
  diverifikasi. Naik ke 1.27.x nanti = ubah `go.mod` + `GO_VERSION` di backend.Dockerfile, tanpa ADR baru.
- **Node 24** (`.nvmrc`, `engines`, `NODE_VERSION=24`). Paket frontend dipatok persis di
  `apps/web/package.json` (Svelte 5.57.1, Vite 8.3.2, TypeScript 6.0.3, Vitest 5.0.3,
  svelte-check 4.7.6) — semua diverifikasi lewat `npm ci`, lint, tes, dan build di sandbox.
- **PostgreSQL / Redis:** compose memakai `postgres:18` dan `redis:8` (major dipatok, patch mengambang).
  Ini menyimpang dari "patch persis" dan HARUS ditutup: operator menjalankan
  `docker compose exec postgres postgres --version` dan `docker compose exec redis redis-server --version`,
  lalu patch persis dicatat di sini dan di compose.
- Alat dev (air, sqlc, swag, golangci-lint, goimports) dipatok lewat `tool` directive di go.mod
  memakai `make tools-pin` (butuh jaringan penuh; belum dijalankan, lihat HANDOVER).

## Konsekuensi
`go.sum` dan `tool` directives belum ada di repo sampai `make tools-pin` dijalankan di mesin dengan akses jaringan penuh.
