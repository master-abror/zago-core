# Changelog

Format mengikuti [Keep a Changelog](https://keepachangelog.com/); versi mengikuti milestone (docs/21).

## [Unreleased]

### M03 — Authentication (SEDANG BERJALAN; parsial 3, sampai ◆CP3)

#### Added
- `pkg/security`: hasher Argon2id (PHC, rehash-on-login, hash dummy, batas 256 byte, semaphore konkurensi berbasis context) dan `PasswordPolicy`.
- Config: `PASSWORD_ARGON2_MEMORY_KIB|TIME|PARALLELISM`, `PASSWORD_HASH_CONCURRENCY`, `PASSWORD_MIN_LENGTH`, `SESSION_IDLE_TIMEOUT`, `SESSION_ABSOLUTE_TIMEOUT`.
- `internal/auth`: repositori sqlc (credentials, sessions, security events, lookup user/keanggotaan minimal) dengan `Store` yang mengikuti transaksi;
  `SessionService` (Redis + PostgreSQL otoritatif, fallback saat Redis mati, rotasi, pencabutan, event `session.revoked`); `CSRF` terikat sesi;
  cookie sesi `__Host-`, `OriginPolicy`, middleware `Authenticate`/`CSRF`/`RequireAuth`; kode error `csrf_failed`.
- ADR-0020 (bagian 1–3).
- Endpoint autentikasi (T4): `POST /api/v1/auth/login` (anonim, rate limit 10/menit/IP, cek Origin), `POST /auth/logout`, `GET /auth/me`, `GET /auth/csrf`, `POST /auth/refresh`,
  `POST /auth/switch-organization`, `GET /api/v1/security/sessions`, `DELETE /security/sessions/{id}`, `POST /security/sessions/revoke-others`. Semua beranotasi swagger (`docs/api/swagger.json`).
  Login tanpa oracle enumerasi akun (verifikasi Argon2id selalu berjalan; status akun baru terlihat setelah password benar); sesi + event keamanan + activity ditulis dalam satu transaksi.
- `AuthService` (login, logout, profil, refresh, switch-organization, daftar/cabut sesi) di `internal/auth/service`; handler di paket `authhttp`; perakitan di `internal/app/auth.go`.
- `api bootstrap-admin` (T6) dan `make bootstrap-admin EMAIL=… [NAME=…] [PASSWORD_STDIN=1]`: membuat Super Admin pertama (role sistem `super-admin`, user, kredensial Argon2id, assignment platform) dalam satu transaksi,
  idempoten dan tidak pernah menimpa password; password dari stdin atau acak sekali-tampil; activity + security event `auth.bootstrap_admin` tanpa email/password. Logika di `internal/bootstrap`, wiring `app.BootstrapAdmin`.
- `scripts/smoke/auth-lib.sh`: bootstrap + login untuk smoke yang butuh sesi (password acak per `make smoke`, menolak BASE non-localhost).
- Kode error: `invalid_credentials` (401), `account_suspended` (403), `account_locked` (423).
- Event keamanan: `auth.login.success|failed`, `auth.logout`, `auth.session.refreshed`, `auth.organization.switched`, `auth.session.revoked`, `auth.sessions.revoked_others`.

#### Changed
- `httpserver.Options` punya `Auth` dan `AuthAPI`; grup rute API memakai `Authenticate → CSRF`, `/health*` di luarnya. `SessionService` menerima `redis.UniversalClient`.
- **Breaking (dev/test):** endpoint echo `POST` kini butuh sesi + `X-CSRF-Token`; header `X-Dev-Actor` dihapus.
- `scripts/smoke/m02.sh` memakai sesi nyata (login + cookie + `X-CSRF-Token`): langkah "X-Dev-Actor non-UUID" diganti "tanpa sesi → 401" dan "CSRF hilang → 403"; `make smoke` menyediakan direktori state sementara bersama.

#### Known issues
- Belum ada: ganti password + riwayat event keamanan (T4d-2), kunci akun + rate limit per akun (T5), smoke M03 (lihat docs/handover/HANDOVER-M03-partial-3.md §10).

### M02 — Kernel (2026-10-18; tag `m02-done`)

#### Added
- Kernel transaksional: pool per role dengan `statement_timeout`, `TxManager.WithinTx` (bersarang bergabung, rollback-only), outbox transaksional + relay `SKIP LOCKED`
  (satu transaksi per event, backoff, dead-letter) di worker, `EventBus` in-process, `AuditRecorder` (aktor, IP, UA, request id, trace id dari `ctx`), penulis `system_logs`
  lewat `logger.SystemLog` + gate, redaksi rahasia (`pkg/redact`). Tipe kontrak lintas batas di `packages/module-sdk`.
- Toolkit HTTP `internal/kernel/httpx`: envelope sukses/error (`Envelope[T]`, `ErrorEnvelope`), registry kode error, validasi per field, paginasi keyset dengan cursor bertanda tangan HMAC,
  middleware idempotency (Redis) dan rate limit (Redis + fallback memori), `Responder`; 404/405 kini envelope penuh (`method_not_allowed`).
- Composition root `internal/app` (`RunAPI`, `RunWorker`) dengan satu klien Redis, `KernelServices`, dan endpoint dev/test `/api/v1/_kernel/echo` (hanya `development`/`test`).
- Observabilitas: `pkg/telemetry` — `/metrics` Prometheus di port terpisah per proses (`METRICS_PORT`, `WORKER_METRICS_PORT`; metrik HTTP per pola rute, `db_pool_*`, gauge outbox, `worker_job_*`,
  `outbox_events_dead_total`) dan hook OpenTelemetry (no-op tanpa `OTEL_EXPORTER_OTLP_ENDPOINT`; OTLP/HTTP, propagator W3C).
- sqlc (`sqlc.yaml`; query pertama `ListActivitiesByRequestID` di `internal/audit/repository`), swag (`docs/api/swagger.json`, endpoint health), `make generate`, `make generate-check` (bagian dari `make verify`).
- Infra tes `internal/testkit` + `internal/testpg` (PostgreSQL 18 dan Redis 8 sungguhan, tanpa mock), `scripts/smoke/m02.sh`, ADR-0009 s.d. ADR-0019.

#### Changed
- `make test-backend` memakai `-p $(GO_TEST_PARALLEL)` (bawaan 3); `make verify` memuat `generate-check`.
- `OTEL_EXPORTER_OTLP_ENDPOINT` divalidasi bila diisi. Doc-sync: 08, 10, 13, 14, 16, 18, 19, README.

#### Fixed
- Klien Redis mengabaikan deadline context pada I/O soket (5,0 dtk vs 0,25 dtk terhadap Redis yang macet): `ContextTimeoutEnabled` diaktifkan (ADR-0019).
- Flake `make verify` pada Docker Desktop yang padat: batas tulis `system_logs` (`WithWriteTimeout`), kesiapan Redis `testkit` (tunggu log + `PING`), batas tunggu Ryuk (3 menit), dan batas Redis di tes rate limit (`WithRedisTimeout`); lihat ADR-0017, ADR-0019.

### M01 — Database Schema (2026-10-07; PR #2, tag `m01-done`)

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
