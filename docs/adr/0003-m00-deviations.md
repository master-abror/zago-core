# ADR-0003 — Penyimpangan kecil dari dokumen di M00

**Status:** Diterima · **Tanggal:** 2026-10-03

| Dokumen | Penyimpangan | Alasan |
|---|---|---|
| 15 §2.1 (service `migrate`) | Menambah `volumes: .:/app` | Stage `dev` hanya berisi go.mod/go.sum; tanpa source, `go run ./backend/cmd/migrate` gagal. |
| 15 (Dockerfile dev) | `go tool air`, bukan `air@latest` | Alat dipatok lewat `tool` directive (ADR-0001), bukan di-install tak terpatok. |
| 15 §2.1 (init SQL) | Menambah `deploy/db/init/01-dev-privileges.sql`: `ALTER ROLE app_migrator CREATEDB` | `migrate-roundtrip` memakai database sementara agar `down` penuh tak menyentuh data dev. Hanya dev/CI; produksi tanpa hak ini. |
| 16 (`generate`, `modules-sync`, `bootstrap-admin`, `module-create`) | Target ada tetapi bertahap: melewati langkah yang masukannya belum ada, dengan pesan eksplisit; `bootstrap-admin` dan `module-create` gagal tegas | Binary/kontraknya baru lahir di M02/M03/M14; target tidak boleh diam-diam "hijau" palsu. |
| 10 §8 (Config) | Satu struct `Config`, kewajiban variabel divalidasi per `Role` (api/worker/migrate); `cmd/migrate` hanya butuh `MIGRATION_DATABASE_URL` | Dokumen tak menyebut role; migrate tak boleh butuh Redis/SESSION_SECRET. |
| 10 §2 (struktur) | Logika start/stop di `backend/internal/app`, adapter pgx/redis di `backend/internal/infra`; `cmd/*` hanya wiring | Alur fail-fast dan graceful shutdown bisa dites dengan fake tanpa Docker. |
| 21 M00 (`cmd/migrate`) | Hanya kerangka: validasi config + urai perintah; belum menyentuh database | golang-migrate dihubungkan di M01 bersama migrasi pertama. |
