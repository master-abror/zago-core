# ADR-0002 — Versi yang dipatok (M00)

**Status:** Diterima · **Tanggal:** 2026-10-03

## Konteks
ADR-0001 meminta versi persis dipatok saat M00. Kebutuhan: build yang bisa diulang, tanpa menahan upgrade keamanan.

## Keputusan
- **Go 1.26.5**, bukan 1.27: 1.27.0 baru rilis Agustus 2026 dan toolchain pihak ketiga (air, sqlc, golangci-lint)
  belum diverifikasi di atasnya. Terverifikasi di mesin pengembang (`go1.26.5 linux/amd64`) dan di image
  `golang:1.26.5-alpine`. Naik ke 1.27.x = ubah `go.mod` + `GO_VERSION` di backend.Dockerfile, tanpa ADR baru.
- **Node 24** (`.nvmrc`, `engines`, `NODE_VERSION=24`); terverifikasi v24.21.0. Paket frontend dipatok persis
  di `apps/web/package.json` (Svelte 5.57.1, Vite 8.3.2, TypeScript 6.0.3, Vitest 5.0.3, svelte-check 4.7.6).
- **Alat dev dipatok lewat `tool` directive di `go.mod`** (air, golangci-lint v2, sqlc, swag, goimports),
  checksum di `go.sum`; dipasang dengan `make tools-pin`, dijalankan sebagai `go tool <nama>`.
- **PostgreSQL dan Redis: tag mayor mengambang** (`postgres:18`, `redis:8`), patch yang TERAMATI pada 2026-10-03:
  **PostgreSQL 18.6**, **Redis 8.10.2**. Ini sengaja menyimpang dari "patch persis" di ADR-0001: patch kedua
  image menerima perbaikan keamanan, dan kode hanya memakai fitur dasar. Tes integrasi (testcontainers)
  dan compose memakai tag yang sama sehingga perilakunya konsisten. Pematokan ke patch persis (atau digest)
  ditinjau ulang di M15 (hardening/rilis). Setiap handover mencatat patch yang teramati.

## Konsekuensi
Dua dari lima versi inti dapat berubah antar-hari tanpa perubahan repo; regresi yang muncul dari update patch
terdeteksi oleh `make verify` dan CI, bukan oleh pematokan.
