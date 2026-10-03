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
| 19 §3 (`contract-check`) | `git status --porcelain -- <path>` menggantikan `git diff --exit-code <path>` | `git diff` error untuk path yang belum ada di M00 dan tak mendeteksi berkas hasil generate yang belum terlacak. |
| 19 §3 (workflow) | Menambah `permissions: contents: read`, `concurrency` (batalkan run lama), dump log service saat `docker-smoke` gagal; job `migrations` juga menjalankan `01-dev-privileges.sql` | Hak minimum token CI; diagnosis kegagalan; `roundtrip` butuh CREATEDB (baris di atas). |
| 16 §2.4 (`db-test`) | Dilewati dengan pesan eksplisit selama belum ada `*_test.go` di `backend/migrations` | `go test` pada direktori tanpa paket Go keluar dengan kode 1 ("no packages to test") dan akan memerahkan CI di M00. Aktif otomatis saat tes skema pertama ada (M01). |
| 16 §2.7 (`verify-smoke.sh`) | Compose project terpisah `platform-verify`, penjaga port 5432/6379/8080/5173/1025/8025, `.env` sementara hanya bila belum ada | `down -v` tak boleh menghapus volume data pengembangan; bentrok port harus gagal dengan pesan jelas. |
| 16 (pemanggilan skrip) | Makefile memanggil skrip lewat `bash ./scripts/x.sh` | Bit eksekusi tidak selalu bertahan di `/mnt/c` (WSL) atau saat zip diekstrak. |
| 10 §2 (repo) | `backend/migrations/.gitkeep` | Git tak melacak direktori kosong; tanpanya `COPY backend/migrations` di Dockerfile produksi gagal di clone bersih. |
| 16 §2.7 (`verify`) | Langkah pertama membuat `.env` dari `.env.example` dan menjalankan `npm ci` bila `node_modules` belum ada | Zip/clone bersih tak punya `.env` (gitignored) dan prompt serah-terima mewajibkan `make verify` segera; tanpanya `migrate-roundtrip` gagal "MIGRATION_DATABASE_URL wajib diisi". `.env` yang sudah ada tak pernah ditimpa. |
| 15 §3 (Dockerfile backend) | Tanpa `go mod download`; cache modul/build lewat BuildKit cache mount; `air` dibangun ke image; compose memberi volume `go_mod_cache` + `go_build_cache` dan memanggil `air` langsung (bukan `go tool air`) | `go mod download` menarik seluruh dependensi alat dev ke layer image: build pertama 1114 dtk, export+unpack ±1000 dtk ×3 image, dan Docker Desktop (±3,5 GB RAM) mati. Layer image kini hanya golang:alpine + biner air. |
| 15 §2.1 (healthcheck api) | `start_period` 120 dtk dan `retries` 40 (semula 30 dtk / 30); `wait-for-healthy.sh` default 600 dtk | Start pertama dengan cache modul dingin mengunduh dan mengompilasi di dalam container. |
| 11 §1.1 / 18 (Vitest) | Environment default `node`; jsdom dipilih per berkas (`// @vitest-environment jsdom`) | Dokumen hanya menyebut "Vitest". Memuat jsdom dari disk lambat (`/mnt/c`, WSL) memakan 94% durasi tes (36–60 dtk untuk 3 tes) hingga worker timeout. Tes logika tak butuh DOM. |

