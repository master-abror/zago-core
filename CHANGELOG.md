# Changelog

Format mengikuti [Keep a Changelog](https://keepachangelog.com/); versi mengikuti milestone (docs/21).

## [Unreleased]

### M01 — Database Schema (dalam peninjauan: menunggu `make verify`/CI hijau pada hasil akhir)

#### Added
- Migrasi inti 001–033 (`backend/migrations`, disematkan ke binary): fungsi umum + `attach_updated_at`, platform, organisasi,
  user, keanggotaan, group (trigger hierarki), modul, permission, role, assignment, policy, setting, credential, session,
  security event, undangan, reset password, notifikasi, inbox, chat, lampiran, activity, system log, outbox, grants.
  Setiap migrasi punya `.down.sql`.
- `cmd/migrate`: `up`, `down N`, `version`, `roundtrip`, `seed` (golang-migrate sebagai library, driver `pgx5://`);
  `internal/dbmigrate` (runner, roundtrip pada database sementara, seeder platform idempoten).
- Suite skema `backend/migrations/*_test.go` (PostgreSQL 18 via testcontainers, `internal/testpg`): constraint, integritas
  tenant (FK komposit), trigger, cascade/restrict, grant `app_user`/`app_maintenance`, seeder, roundtrip.
- `docs/erd.md` dibangkitkan dari skema nyata dan dijaga tes (`UPDATE_ERD=1 make db-test`).
- `make migrate-version`, `make db-seed`, `scripts/migrate-roundtrip.sh`.
- ADR-0005 (sumber PostgreSQL untuk roundtrip), ADR-0006 (penyimpangan kecil M01), ADR-0007 (air `entrypoint`), ADR-0008 (semantik hapus).

#### Changed
- `make migrate-roundtrip` menyalakan PostgreSQL sementara sendiri (Docker); job CI `migrations` memakai skrip yang sama.
- `make test`/`make db-test` memakai `REQUIRE_DOCKER=1`: tanpa Docker tes skema gagal, bukan skip.
- `.air.*.toml`: `build.bin` → `build.entrypoint` (peringatan deprecation hilang).
- Doc-sync: 04 §13.2/§15/§17, 16 §2.4, 19 §3 dan §5.

#### Fixed
- Tes M00 `TestAPIServesHealthAndShutsDownClosingResourcesInReverseOrder` flaky (gagal 9/30 di bawah `-race`): klien HTTP tes tanpa keep-alive.

## [M00] - 2026-10-04 — Foundation & Tooling

#### Added
- Monorepo: `backend/` (Go), `apps/web` (Svelte 5 runes + Vite + Vitest), npm workspaces, `packages/*`, `modules/`.
- Kernel Go: `pkg/id` (UUIDv7), `pkg/logger` (slog JSON + request id), `pkg/config` (fail-fast, validasi per role,
  rahasia tak pernah masuk error/log), `internal/health` (`/health`, `/health/live`, `/health/ready`),
  `internal/httpserver` (RequestID, RealIP anti-spoof, AccessLog, Recoverer, SecurityHeaders, Timeout, graceful shutdown),
  `internal/app`, `internal/infra` (pgx, go-redis), entrypoint `cmd/{api,worker,migrate}` (migrate masih kerangka).
- `Makefile` sesuai docs/16; `docker-compose.yml` (PostgreSQL 18, Redis 8, Mailpit, profile `full`), Dockerfile backend/web
  berkonteks root, konfigurasi air, proxy dev Vite (`/api`, `/ws`).
- Skrip: `dev.sh`, `wait-for-healthy.sh`, `verify-smoke.sh`, `lint-structure.sh` (kerangka), `smoke/m00.sh`.
- CI GitHub Actions (lint, test, migrations, build, docker-smoke, contract-check).
- ADR-0001 (stack), ADR-0002 (versi), ADR-0003 (penyimpangan kecil dari dokumen), ADR-0004 (path modul).

#### Fixed
- Container `web`/`api`/`worker` tak lagi menulis berkas milik root ke working tree (volume anonim `/app/apps/web/node_modules`, `/app/tmp`); `verify-smoke.sh` mendeteksi kebocoran.

#### Changed
- Path modul Go `platform` → `github.com/master-abror/zago-core` (ADR-0004).
- Dockerfile backend tanpa `go mod download` (image 2,13 GB → 386 MB; build 18 menit → hitungan menit).
- Vitest memakai environment `node` secara default; jsdom dipilih per berkas.
- `make verify` membuat `.env` dari `.env.example` bila belum ada; `make db-test`, `make generate`,
  `make modules-sync` bertahap sampai komponennya ada.
