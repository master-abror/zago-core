# HANDOVER — M00 Foundation & Tooling

**Tanggal:** 2026-10-03 · **Tag:** (belum) · **Commit terakhir:** lihat `git log -1` di branch `milestone/m00-foundation`
**Jenis:** PARSIAL (setelah ◆CP1; sisa: T6..T7 + penutupan bukti `make verify`)

## 1. Ringkasan
Repo sudah punya skeleton monorepo, frontend Svelte 5 "hello" yang terverifikasi (lint/tes/build),
Makefile sesuai docs/16, docker-compose + Dockerfile + konfigurasi air, kernel Go
(`pkg/id`, `pkg/logger`, `pkg/config`, `internal/health`, `internal/httpserver`, `internal/app`,
`internal/infra`) dan tiga entrypoint (`api`, `worker`, `migrate`). **Belum:** `go.sum`, alat dev
terpatok, skrip pendukung Makefile, proxy/CI/smoke — jadi `make verify` BELUM bisa dijalankan.

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T1 skeleton repo + `apps/web` | DONE | commit T1 (hash berubah setelah penulisan ulang author) |
| T2 `go.mod` + Makefile | DONE (sebagian) | `go.mod` hanya `module platform` / `go 1.26.5`; **tanpa `go.sum` dan tanpa `tool` directives** |
| T3 compose + Dockerfile + air | DONE (belum dijalankan) | YAML valid; tak ada Docker di sandbox |
| T4 `pkg/id`, `pkg/config`, `pkg/logger` | DONE | tes lulus (-race) di sandbox |
| T5 server, health, shutdown, app, infra, cmd | DONE untuk `app`/`health`/`httpserver`/`migrate`; `infra` + `cmd/api` + `cmd/worker` **BELUM PERNAH DIKOMPILASI** | `golang.org/x/*` diblokir di sandbox, jadi pgx dan go-redis tak bisa di-resolve |
| T6 skrip, proxy Vite, CI, smoke | TODO | proxy Vite sudah ada di `vite.config.ts`; skrip/CI/smoke belum |
| T7 ADR, STATUS, README, CHANGELOG, handover akhir | TODO (ADR-0001..0003 + STATUS parsial sudah) | README/CHANGELOG belum |

## 3. Bukti verifikasi (apa adanya)
- Frontend (sandbox): `npm ci` OK · `svelte-check` 0 error 0 warning + Prettier bersih · Vitest 3/3 lulus · `vite build` OK.
- Backend (sandbox, Go 1.24 dengan `GOTOOLCHAIN=local`, versi dependensi diambil lewat `GOPROXY=direct`):
  `gofmt` bersih · `go vet` bersih · **65 tes top-level lulus dengan `-race`** di `pkg/{id,logger,config}`,
  `internal/{health,httpserver,app}`. Binary `cmd/migrate` dibangun dan dijalankan: tanpa env → exit 2;
  URL salah skema → exit 2; `down 0` → exit 2; `up` valid → exit 0.
- **TIDAK terverifikasi:** `go.mod` dengan `go 1.26.5` (sandbox hanya Go 1.24); `internal/infra`,
  `cmd/api`, `cmd/worker` (belum dikompilasi); tes integrasi testcontainers
  (`internal/infra/infra_integration_test.go`, tak pernah dijalankan); compose; Dockerfile; `make verify`.
- Tes negatif/keamanan yang ada: config tak membocorkan rahasia di error/log/`%v`/slog; XFF diabaikan dari peer tak tepercaya;
  `X-Request-ID` klien tak valid diganti; panic → 500 generik tanpa stack di respons; query string tak masuk access log;
  `/health/ready` tak membocorkan teks error dependensi; fail-fast config terjadi sebelum bind port (listener tetap utuh).

## 4. Keputusan & penyimpangan
- ADR-0001 (versi patok diisi) · ADR-0002 (Go 1.26.5 bukan 1.27; PG/Redis patch terbuka) · ADR-0003 (tabel penyimpangan kecil).
- Doc-sync tertunda: docs/15 §2.1 (volume `migrate`, `go tool air`, init SQL dev) dan docs/10 §8 (validasi per role) belum disinkronkan.

## 5. Delta kontrak
- **DB:** tidak ada migrasi. Init dev baru: `deploy/db/init/01-dev-privileges.sql` (`app_migrator` CREATEDB).
- **API:** `GET /health`, `GET /health/live` (200), `GET /health/ready` (200/503; badan `{status, checks{nama: ok|unavailable}}`); error JSON `{"error":{"code":...}}` untuk 404/405/500 (bentuk final di M02).
- **Event:** tidak ada.
- **Config/env:** semua variabel di `.env.example`; `TRUSTED_PROXIES` divalidasi sebagai CIDR/IP; `COOKIE_SECURE` default `true`.
- **Permission:** tidak ada.

## 6. Peta kode
| Hal | Lokasi |
|---|---|
| Start/stop api, worker, migrate | `backend/internal/app` (`cmd/*` hanya wiring) |
| Adapter pgx / go-redis | `backend/internal/infra` |
| Middleware, router, graceful shutdown | `backend/internal/httpserver` |
| Liveness/readiness | `backend/internal/health` |
| Config / logger / UUIDv7 | `backend/pkg/{config,logger,id}` |
| Klien API frontend (satu-satunya pintu fetch) | `apps/web/src/core/api/client.ts` |
| Penyimpangan & versi | `docs/adr/0001..0003` |

## 7. Cara menjalankan & memverifikasi (di mesin dengan Docker dan jaringan penuh)
```bash
git checkout milestone/m00-foundation
go mod tidy                 # membuat go.sum (resolusi pgx, go-redis, chi, uuid, testify, testcontainers, env)
make tools-pin              # memasang tool directives (air, sqlc, swag, golangci-lint, goimports)
make test-backend           # tes unit + integrasi (butuh Docker)
```
Tempel keluaran perintah-perintah itu (terutama error kompilasi di `internal/infra`) ke chat lanjutan.
`make lint` dan `make verify` baru bisa jalan setelah T6 (`scripts/lint-structure.sh`, `dev.sh`, `verify-smoke.sh`, `smoke/m00.sh` dibuat).

## 8. Masalah diketahui / utang teknis
- `internal/infra/*` ditulis tanpa kompilasi: kemungkinan perlu perbaikan kecil API (pgxpool/go-redis/testcontainers `TerminateContainer`, `SkipIfProviderIsNotHealthy`).
- `go 1.26.5` belum dicoba pada toolchain asli.
- Tag `postgres:18` / `redis:8` mengambang (ADR-0002).
- Worker M00 hanya membuka dependensi lalu menunggu sinyal; tanpa health endpoint sendiri.
- `cmd/migrate` belum menyentuh database (M01).
- `make setup` memanggil `migrate-up` yang berhasil tanpa menguji koneksi DB; bukti koneksi datang dari `/health/ready`.

## 9. Pertanyaan terbuka
- Patch persis PostgreSQL 18.x dan Redis 8.x (lihat ADR-0002) setelah kamu menjalankan stack.
- Apakah memakai `go 1.26.5` sebagai nilai `go` tanpa `toolchain` directive dapat diterima? (Saya tidak menambah `toolchain` karena tidak bisa memverifikasi di sandbox.)

## 10. Langkah pertama berikutnya (lanjutan M00)
1. Jalankan perintah di §7, tempel hasilnya; perbaiki error kompilasi/tes di `internal/infra` dan `go.mod` dulu.
2. T6: `scripts/{dev.sh,lint-structure.sh,verify-smoke.sh}`, `scripts/smoke/m00.sh`, `.github/workflows/ci.yml` (docs/19 §3), `Makefile` target `smoke`.
3. T7: README (termasuk catatan lisensi Redis/Valkey), CHANGELOG, isi patch PG/Redis di ADR-0002, handover LENGKAP, STATUS → DONE.
4. Buka: `docs/16`, `docs/19 §3`, `docs/15 §5–§6`, `docs/21` bagian M00.
