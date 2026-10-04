# Changelog

Format mengikuti [Keep a Changelog](https://keepachangelog.com/); versi mengikuti milestone (docs/21).

## [Unreleased]

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
