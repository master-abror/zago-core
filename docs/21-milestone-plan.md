# Milestone Plan
## Open Source Modular Application Platform v1.0

**Document:** 21-milestone-plan.md
**Status:** Baseline
**Previous:** 20-roadmap.md
**Builds on:** dokumen 01–20 **Rev 1.1** (sudah memuat seluruh perbaikan dari 00-errata-and-amendments.md), 18-testing-strategy.md, 19-ci-cd.md
**Dibaca bersama:** 22-handover-protocol.md, 23-prompt-library.md

---

# 1. Tujuan dokumen

Dokumen 01–20 (Rev 1.1) menjelaskan *apa* platform ini. Dokumen ini menjelaskan *urutan membangunnya* dalam potongan yang muat di **satu chat room per milestone**, dengan handover yang jelas ke chat berikutnya.

Prinsip pemotongan:

```text
1. Satu milestone = satu chat room = satu tema yang bisa didemokan lewat smoke test.
2. Setiap milestone hanya membaca BAGIAN dokumen yang tercantum (bukan seluruh 20 dokumen).
3. Milestone besar punya checkpoint (◆CP): titik aman untuk berhenti dan membuat handover parsial.
4. Repo adalah memori; chat hanya pekerja sementara (lihat 22-handover-protocol.md).
5. Tidak ada milestone yang dimulai sebelum `make verify` milestone sebelumnya hijau.
```

---

# 2. Peta Ketergantungan

```text
M00 Foundation
 └─ M01 Database Schema
     └─ M02 Kernel (tx, outbox, audit, HTTP toolkit)
         ├─ M03 Authentication
         │   └─ M04 Authorization
         │       ├─ M05 Org · User · Group · Invite · Email
         │       │   ├─ M06 Audit & System Log
         │       │   └─ M07 Module System (backend)
         │       │       ├─ M08 Realtime: WS · Notification · Inbox
         │       │       │   └─ M09 Chat
         │       │       └─ M14 Reference Module (Announcements)
         │       └─ M10 Frontend Foundation  (boleh dimajukan setelah M04 bila ingin UI lebih awal)
         │            ├─ M11 Frontend Admin A
         │            ├─ M12 Frontend Admin B
         │            └─ M13 Frontend Realtime  (butuh M08+M09)
         └─ M15 Hardening & Release  (butuh semuanya)
```

Urutan default: M00 → M01 → … → M15. Jalur alternatif "UI lebih cepat": kerjakan M10 tepat setelah M04, lalu selipkan M11 setelah M05, M12 setelah M07.

---

# 3. Definition of Done (berlaku di SEMUA milestone)

Sebuah milestone hanya boleh ditandai `DONE` di `docs/STATUS.md` bila **semua** hal ini benar:

```text
[ ] Semua task milestone selesai (atau yang ditunda dicatat eksplisit di HANDOVER sebagai "Deferred" + alasan)
[ ] Tes unit + integrasi ditulis; tes NEGATIF wajib (auth, authz, tenant isolation) sesuai 18 §5
[ ] `make verify` hijau (lint + test + migrate-roundtrip + build + smoke)
[ ] Smoke script `scripts/smoke/mNN.sh` ada, berjalan, dan ditambahkan ke `make smoke`
[ ] Handler baru punya anotasi swag dan `make generate` tidak menghasilkan diff (setelah M02)
[ ] Task "Doc-sync": bila implementasi menyimpang dari dokumen 01–20 (Rev 1.1), dokumen yang bersangkutan sudah disinkronkan dan penyimpangannya punya ADR
[ ] Keputusan baru dicatat sebagai ADR (docs/adr/NNNN-*.md); penyimpangan dari dokumen dicatat di HANDOVER
[ ] README, CHANGELOG.md, docs/STATUS.md diperbarui
[ ] docs/handover/HANDOVER-Mnn.md + prompt chat berikutnya ditulis
[ ] Commit per task (Conventional Commits), branch milestone di-merge, tag `mNN-done`, push
```

Detail git dan testing: 22-handover-protocol.md §9–§10.

---

# 4. Ukuran & Aturan Checkpoint

Perkiraan beban konteks: **S** (kecil), **M** (sedang), **L** (besar — punya checkpoint).

| Milestone | Tema | Beban |
|---|---|---|
| M00 | Foundation & tooling | M |
| M01 | Database schema + tes | L |
| M02 | Kernel: tx, outbox, audit, HTTP toolkit | L |
| M03 | Authentication | L |
| M04 | Authorization | L |
| M05 | Org/User/Group/Invite/Email | L |
| M06 | Audit & System Log | M |
| M07 | Module system backend | L |
| M08 | Realtime + Notification + Inbox | L |
| M09 | Chat | M |
| M10 | Frontend foundation | L |
| M11 | Frontend admin A | M |
| M12 | Frontend admin B | L |
| M13 | Frontend realtime | M |
| M14 | Reference module Announcements | M |
| M15 | Hardening & release v1.0.0 | M |

**Aturan checkpoint:** bila percakapan sudah sangat panjang (banyak file dibaca/dibuat, log panjang, pengulangan pembacaan yang sama), berhenti di ◆CP terdekat, jalankan prompt P4 (handover parsial), dan lanjutkan di chat baru. **Jangan memaksa** menyelesaikan milestone dalam satu chat yang sudah penuh — kualitas turun sebelum batasnya terasa.

---

# 5. Milestone

Format tiap milestone: **Baca** (bagian dokumen yang boleh dibaca) · **Bangun** · **Tes wajib** · **Exit criteria (demo)**.
Notasi: `04 §6` = dokumen 04 bagian 6; `E:B-03` = item di 00-errata-and-amendments.md (opsional dibaca — hanya untuk memahami *alasan* sebuah keputusan; isinya sudah ada di dokumen Rev 1.1).

---

## M00 — Foundation & Tooling

**Baca:** 10 §1.1, §2, §5, §7, §8, §11, §12 · 15 · 16 · 19 §3

**Bangun**
- T1. Skeleton repo sesuai 10 §2 (`go.mod` di root, `backend/`, `packages/module-sdk/` kosong, `modules/`, `apps/web/` Vite+Svelte 5 "hello", `deploy/`, `scripts/`, `docs/`). Salin dokumen 00–23 ke `docs/`.
- T2. `go.mod` dengan `tool` directives; `Makefile` versi perbaikan (`tools`, `setup`, `infra-up`, `docker-up`, `dev`, `run-api`, `run-worker`, `lint`, `test`, `migrate-*`, `migrate-roundtrip`, `generate`, `build`, `smoke`, `verify`, `handover-pack`).
- T3. `docker-compose.yml` sesuai 15 §2: `postgres:18` (volume `/var/lib/postgresql`, init `deploy/db/init/00-roles.sql` — sudah ada di berkas awal), `redis:8`, `mailpit`; service `migrate/api/worker/web` di profile `full`; port datastore ke `127.0.0.1`; Dockerfile berkonteks root (`deploy/docker/`), stage `dev/build/production`, hot reload memakai air.
- T4. `pkg/config` (fail-fast), `pkg/logger` (slog JSON + `request_id`), `pkg/id` (UUIDv7).
- T5. ◆CP1 — Server chi: middleware RequestID, RealIP (trusted proxies), Recoverer, access log, security headers, timeout; `/health`, `/health/live`, `/health/ready` (ping pgxpool + Redis); graceful shutdown untuk `cmd/api` dan `cmd/worker`; `cmd/migrate` (kerangka).
- T6. Proxy dev Vite (`/api`,`/ws` → `:8080`); CI GitHub Actions sesuai 19 §3 (lint, test, migrations, build, docker-smoke, contract-check) memakai `make`; skrip `scripts/dev.sh`, `scripts/wait-for-healthy.sh`, `scripts/verify-smoke.sh`, `scripts/lint-structure.sh` (kerangka awal), `scripts/smoke/m00.sh`.
- T7. `CLAUDE.md`, `docs/STATUS.md`, `docs/adr/0001-stack.md` (versi patch yang di-pin), `scripts/handover-pack.sh`, `scripts/dev.sh`, README.

**Tes wajib:** config wajib hilang → proses keluar sebelum bind port; `/health/ready` 503 saat PostgreSQL mati (testcontainers); graceful shutdown menyelesaikan request in-flight; `pkg/id` menghasilkan ID urut waktu.

**Exit:** di mesin bersih `git clone && make setup && make dev` jalan; `curl localhost:8080/health/ready` → 200; CI hijau; `make verify` hijau.

---

## M01 — Database Schema

**Baca:** 04 (seluruhnya) · 03 §41–51 · 02 §34 (invariant)

**Bangun**
- T1. `cmd/migrate` lengkap (golang-migrate library, driver `pgx5://`, folder `backend/migrations`, subcommand `up/down/version/roundtrip`).
- T2. `deploy/db/init/00-roles.sql`: role `app_migrator`, `app_user`, `app_maintenance` + `ALTER DEFAULT PRIVILEGES` (E:B-03); dipasang otomatis di compose.
- T3. Implementasikan migrasi 001–016 (04 §3–§9) persis sesuai 04 §14: fungsi umum + `attach_updated_at`, composite FK, trigger hierarki group, `roles.group_id` + CHECK boundary, CHECK/unique `role_assignments`, `CHECK email lowercase`, `modules.code` format.
- T4. ◆CP1 — Migrasi 017–032 (04 §10–§12): credentials, sessions (`token_hash`), security_events, invitations, password_reset_tokens, komunikasi (`direct_key`, composite FK), activities/system_logs (soft reference), `outbox_events`.
- T5. Migrasi 033 grants (04 §13.2) + seeder Go untuk **platform row** (idempoten). Katalog permission & role sistem → M04; bootstrap admin → M03.
- T6. Diagram ERD final (`docs/erd.md`, Mermaid); pastikan 03/04 tetap sesuai SQL yang benar-benar dijalankan (Doc-sync).

**Tes wajib:** seluruh daftar 04 §17 (sudah mencakup FK komposit lintas-org, siklus/kedalaman group, `organization_id` immutable, CHECK scope role_assignment, duplikat assignment aktif, email uppercase, `direct_key` unik, grants `app_user`/`app_maintenance`, default privileges untuk tabel baru, **scan trigger `updated_at`**) + `make migrate-roundtrip` hijau.

**Exit:** `make migrate-up && make migrate-down && make migrate-up` bersih; `make db-test` hijau; ERD terbit.

---

## M02 — Kernel: Transaksi, Outbox, Audit, HTTP Toolkit

**Baca:** 10 §6, §9, §10, §13 · 13 §2.2–2.4 · 08 §2–§7, §11 · 14 §2–§6 (ringkas) · `E:B-01, B-02, I-06, I-07, I-08`

**Bangun**
- T1. Factory `pgxpool` untuk 3 role (statement timeout, ukuran dari config). `TxManager.WithinTx` (tx di `ctx`; repository memakai tx bila ada).
- T2. Outbox: penulisan dalam tx + relay (`SKIP LOCKED`, retry + backoff, dead-letter setelah N gagal) + bus in-process sinkron. Interface `EventBus` final.
- T3. ◆CP1 — `AuditRecorder` (mengisi actor/IP/UA/request_id/trace_id dari `ctx`, satu tx dengan aksi; `denied` di tx terpisah) dan penulis `system_logs`; daftar redaksi metadata (13 §2.3).
- T4. HTTP toolkit: envelope `{data,meta}`/`{error}`, `AppError` + registry kode error (08 §11 + I-08), mapper error (default → `internal_error`, tanpa bocor detail), validasi → `validation_failed`, pagination keyset + cursor HMAC, middleware Idempotency (Redis), rate limiter (Redis) + header `X-RateLimit-*`, accessor context bertipe (10 §10).
- T5. Wrapper Redis; `pkg/telemetry` (`/metrics` Prometheus + hook OTel); setup sqlc contoh; swag terpasang (`make generate`).
- T6. Paket `internal/testkit`: testcontainers Postgres+Redis, fixture builder (18 §7).

**Tes wajib:** rollback tx → tidak ada baris outbox/audit; relay mengirim ulang & handler idempotent menyaring duplikat; cursor diubah → 400; idempotency (key sama+body sama → respons sama; body beda → `idempotency_key_conflict`; paralel → `idempotency_in_progress`); error tak dikenal tidak membocorkan teks asli; `request_id` mengalir ke log dan activity.

**Exit:** endpoint contoh `/api/v1/_kernel/echo` (khusus dev/test) memperagakan envelope, idempotency, rate limit, cursor; `make verify` hijau.

---

## M03 — Authentication (Phase 1)

**Baca:** 05 (seluruhnya) · 12 §4.1, §4.4 · 08 §10.1 · `E:B-04 (bootstrap), B-07, I-04, I-05`

**Bangun**
- T1. Repo credentials/sessions/security_events; hasher Argon2id (PHC, rehash-on-login, batas panjang, dummy hash, semaphore).
- T2. Session service: token 256-bit, Redis `session:{sha256}` + `user_sessions:{uid}`, baris `sessions` PostgreSQL (`token_hash`), idle + absolute timeout, rotasi, fallback ke PG bila Redis down.
- T3. ◆CP1 — Cookie + CSRF (HMAC terikat sesi), middleware chain 05 §9, `Identity` di context.
- T4. Endpoint: `login`, `logout`, `me`, `csrf`, `switch-organization`, `security/sessions` (list/delete/revoke-others), `security/password`, `security/events`.
- T5. Lock sementara (Redis, per akun + per IP, progressive delay), security events + activities via recorder.
- T6. `make bootstrap-admin EMAIL=…` (idempoten: platform row + role Super Admin + user + assignment platform).

**Tes wajib:** seluruh 05 §20 kecuali MFA/reset; email tak dikenal ≈ waktu respons password salah; sesi hidup setelah restart API; sesi dicabut ditolak; idle timeout; Redis mati → login/lookup tetap jalan (degraded); ID sesi berotasi saat login/ganti password; CSRF hilang/salah ditolak.

**Exit:** `scripts/smoke/m03.sh`: bootstrap → csrf → login → me → logout → me = 401.

---

## M04 — Authorization Engine

**Baca:** 06 (seluruhnya) · 07 §7 · 12 §4.6, §6 · `E:B-04 (katalog), I-01, I-02, I-03`

**Bangun**
- T1. Paket `role`, `permission`, `policy`, `authorization`. Katalog permission core **di kode** + sinkron startup; role sistem (Super Admin, Org Admin, Group Admin, Member) + set default.
- T2. Resolusi `ScopeContext` (verifikasi `X-Active-Group-ID` ke DB; `OrganizationID` nullable untuk platform).
- T3. ◆CP1 — Effective permissions + cache versi-per-org (I-01); `Authorizer.Can`; middleware `Authorize(action)` untuk route; fail-safe saat Redis down.
- T4. Parser/evaluator policy (tokenizer + parser sendiri, allowlist fungsi; **tanpa eval**), deny menang; `FieldGuard` di serialisasi.
- T5. Authority ceiling (`grant.go`), endpoint roles, role permissions, role-assignments, policies, permissions, `simulate` (jalur kode yang sama dengan produksi).

**Tes wajib:** seluruh 06 §14; fuzz test parser policy; perubahan `role_permissions`/assignment berlaku **seketika** (bukan setelah TTL); `grant_exceeds_authority`; scope group A tidak berlaku saat header = group B; jalur platform Super Admin.

**Exit:** `scripts/smoke/m04.sh`: user tanpa permission → 403; diberi role → 200; dicabut → 403 langsung; simulator menjelaskan alasan.

---

## M05 — Organization, User, Group, Invitation, Email

**Baca:** 02 §6–§11 · 08 §10.6–§10.9 · 05 §13 · 10 §5 · 03 §46 · `E:I-10`

**Bangun**
- T1. Paket `organization`, `identity`, `group`, `membership` (domain/application/repository/transport) + endpoint 08 §10.6–§10.9 (termasuk `members/{id}/activate`, `profile` dengan field permission).
- T2. Transaksi 03 §46 (create user, create group, dst.) via `TxManager`.
- T3. ◆CP1 — Worker: job runner + relay outbox; layanan email (SMTP → Mailpit, template per locale).
- T4. Invitation (token hash, kedaluwarsa, accept → set password) dan forgot/reset password (`password_reset_tokens`); respons generik anti-enumerasi.
- T5. Suspend/deactivate user atau organisasi → cabut sesi (event) dan tutup akses.

**Tes wajib:** tes lintas-org negatif untuk **setiap** repository; siklus grup ditolak; Group Admin tak bisa keluar scope; user suspended tak bisa login; token invite/reset sekali pakai & kedaluwarsa; create user atomik (rollback penuh).

**Exit:** `scripts/smoke/m05.sh` (kriteria sukses 01 §5 #1–#2): Super Admin buat org → undang Group Admin (email masuk Mailpit) → accept → Group Admin buat group, tambah + aktifkan user, buat role custom dalam scope-nya.

---

## M06 — Audit & System Log

**Baca:** 13 (seluruhnya) · 04 §13 · 12 §4.3 · `E:B-03 (maintenance), I-02, I-16`

**Bangun**
- T1. Query `GET /activities` (scoped per peran) dan `GET /activities/me` sebagai **dua jalur query berbeda**; `GET /system-logs` (Super Admin).
- T2. ◆CP1 — Export CSV via worker (streaming, scope sama dengan tampilan).
- T3. Job retention (batch delete) dan `user.anonymize` memakai pool `app_maintenance`.
- T4. Sink `logger` → `system_logs` untuk subset penting (`.SystemLog()`); property test daftar redaksi.

**Tes wajib:** seluruh 13 §11; Group Admin tak melihat grup lain; aksi Group Admin muncul di "All" **dan** "My"; `app_user` gagal UPDATE/DELETE log; anonymize menjaga baris + timestamp + action.

**Exit:** `scripts/smoke/m06.sh` (kriteria 01 §5 #4–#5).

---

## M07 — Module System (Backend)

**Baca:** 07 (seluruhnya) · 10 §4 · 17 §3–§9 (sekilas) · `E:B-09, I-15`

**Bangun**
- T1. `packages/module-sdk` final: `Module{Manifest, Migrate, RegisterRoutes, RegisterPermissions, RegisterEventHandlers, RegisterJobs, Health}`, `CoreDeps`, `Router` (tipis di atas chi), `Require()`, `URLParam`, `MigrationRunner`.
- T2. Parser + validator `module.yaml` (JSON Schema); generator `make modules-sync` (Go + TS).
- T3. ◆CP1 — `internal/module`: registry compile-time, lifecycle install/enable/disable/uninstall, resolver dependensi semver, migrasi per-modul (pool `app_migrator`, tabel versi per modul).
- T4. Sinkron permission modul, gating route per organisasi (404/403 saat disabled), settings modul (`settings_schema`, `is_secret` write-only).
- T5. Health modul → `/health/ready` + metrik; handler event modul dijalankan di worker.
- T6. Endpoint 07 §14; modul uji palsu di `testdata/`.

**Tes wajib:** seluruh 07 §17; install gagal → `failed`, tidak setengah aktif; disable menjaga data; uninstall butuh konfirmasi + laporan role terdampak; modul tak bisa mengimpor `backend/internal/*` (tes compile); enable di Org A tak berpengaruh ke Org B.

**Exit:** `scripts/smoke/m07.sh`: install → enable org A → route hidup; disable → 404; org B tak terpengaruh.

---

## M08 — Realtime: WebSocket, Notification, Inbox

**Baca:** 09 §1–§7, §9–§10 · 08 §10.4 · 05 §8 · `E:B-10 (Origin), I-09`

**Bangun**
- T1. Hub `coder/websocket`: autentikasi handshake dengan sesi yang sama, **cek Origin**, ping/pong, read limit, send buffer + putus slow consumer, batas koneksi per user.
- T2. Fan-out Redis Pub/Sub `ws:user:{id}`; presence (TTL Redis); tutup koneksi saat `session.revoked`.
- T3. ◆CP1 — Notification service + registry template (core + manifest modul); Inbox service; `inbox.completed` via event; REST 09 §6–§7.
- T4. Protokol reconnect + `resync_required`; `FieldGuard` pada payload; rate limit WS; fallback email untuk prioritas tinggi.

**Tes wajib:** 09 §12 (non-chat); handshake tanpa sesi / Origin salah ditolak; event dari instance A sampai ke klien di instance B; user offline melihat data via REST; sesi dicabut → koneksi ditutup.

**Exit:** `scripts/smoke/m08.sh`: notifikasi dari event masuk ke klien WS < 1 detik dan tetap terlihat via REST (kriteria 01 §5 #6).

---

## M09 — Chat

**Baca:** 09 §8 · 02 §23–§27 · `E:B-06, B-08.5, I-11`

**Bangun**
- T1. Conversations + members (reuse direct via `direct_key`, invariant org via FK komposit), permission `conversation.*`.
- T2. Messages (tombstone delete, edit policy, ukuran dibatasi), reactions, read state `last_read_message_id`.
- T3. ◆CP1 — Attachments: interface `storage` + driver disk lokal, deteksi content-type server-side, batas ukuran, akses lewat authz.
- T4. Event WS chat + rate limit `ws:message`/`ws:typing`; REST 09 §8.5.

**Tes wajib:** 09 §12 bagian chat; non-member tak bisa kirim; direct chat tidak duplikat walau dibuat paralel; tombstone; file palsu (ekstensi ≠ isi) ditolak.

**Exit:** `scripts/smoke/m09.sh`: dua user saling chat via WS, satu offline lalu membaca riwayat via REST.

---

## M10 — Frontend Foundation

**Baca:** 11 §1–§10, §12 · 08 §2–§3, §11 · 05 §8 · `E:I-13, I-14, B-10`

**Bangun**
- T1. Vite + Svelte 5 + TS strict, npm workspaces; ADR router (kriteria: dukung Svelte 5, history mode, guard, lazy route) dan UI kit; Vitest + Playwright.
- T2. `make generate` → `packages/ts-sdk`; `core/api` (CSRF, `X-Request-ID`, `X-Active-Group-ID`, envelope error → i18n).
- T3. ◆CP1 — `core/auth` (me, switch org/group), `core/permission` (`can`, `PermissionGate`), `core/i18n` (en-US, id-ID), `core/module` (registry dari `modules_gen.ts`).
- T4. Layouts (public/authenticated/admin), route guard, halaman login/forgot/reset/accept-invite, shell dashboard, error boundary global.

**Tes wajib:** Vitest untuk `core/api` (header & envelope), `can()`, merge registry modul; Playwright: login → dashboard → logout; field absen tidak membuat komponen crash.

**Exit:** login lewat browser berhasil pada stack lokal same-origin.

---

## M11 — Frontend Admin A

**Baca:** 11 §5–§9 · 07 §11 · 08 §10.6–§10.9

**Bangun:** halaman Users, Groups (+members, activate), Organizations (Super Admin), Profile, Security (sessions, login history, ganti password), pemilih organisasi & group — mengikuti **six-file pattern**.

**Tes wajib:** Playwright: Group Admin menambah + mengaktifkan user; tombol tersembunyi **dan** aksi langsung tetap 403; i18n dua bahasa.

**Exit:** Skenario kriteria 01 §5 #2 dapat dijalankan penuh lewat UI.

---

## M12 — Frontend Admin B

**Baca:** 06 §5, §10 · 13 §4 · 07 §14

**Bangun:** editor Roles/Permissions (matriks + field permission), role assignments, policies, simulator, Activities (All / My / export), System Log, Modules (manage), Settings.

**Tes wajib:** UI tak menawarkan grant di atas authority (dan server tetap menolak); viewer tanpa hak tak melihat menu System Log; ekspor mengikuti scope.

**Exit:** Super Admin bisa mengelola role + modul dari UI; Group Admin hanya melihat scope-nya.

---

## M13 — Frontend Realtime

**Baca:** 11 §8 · 09 §2.1, §5 · `E:I-09`

**Bangun:** klien WS (reconnect eksponensial + `resync_required`), pusat notifikasi + toast, Inbox, Chat (percakapan, pesan, reaksi, lampiran, presence/typing).

**Tes wajib:** event duplikat = no-op (kunci `id`); reconnect memulihkan pesan yang terlewat; e2e dua browser saling chat.

**Exit:** notifikasi < 1 detik dan chat dua arah pada stack lokal.

---

## M14 — Reference Module: Announcements

**Baca:** 17 (seluruhnya) · 07 §11–§12 · 16 §2.8 · `E:B-09`

**Bangun**
- T1. Scaffolder Go `backend/cmd/modulegen` + `make module-create` / `make modules-sync` (16 §2.9).
- T2. Modul **Announcements** lengkap (CRUD + publish, migrasi, permission, event → notifikasi, frontend six-file pattern) memakai **hanya** `module-sdk`.
- T3. ◆CP1 — Lint isolasi tabel (`<code>_*`), CI check modul, panduan 17 diverifikasi dengan menjalankannya dari nol; perbaiki setiap langkah yang keliru.

**Tes wajib:** 17 §13 seluruhnya; modul tidak menyentuh `backend/internal/*`; modul baru dari scaffold otomatis ikut `make test`.

**Exit:** kriteria 01 §5 #7 terbukti oleh modul ini.

---

## M15 — Hardening & Release v1.0.0

**Baca:** 12 · 14 §5–§9 · 18 §5 · 19 §7–§8 · 01 §5 · `E:B-10 (proxy prod), D-01, D-07`

**Bangun**
- T1. Dashboard Grafana + alert (14 §8–§9); propagasi trace API → outbox → worker terbukti.
- T2. Review keamanan: tiap baris 12 §12 ↔ bukti tes; catat gap.
- T3. ◆CP1 — Produksi: `docker-compose.prod.yml`, reverse proxy same-origin, image distroless, runbook backup/restore.
- T4. Uji beban ringan (k6, tanpa klaim SLA), checklist kriteria sukses 01 §5 dijalankan penuh.
- T5. Patch akhir dokumen 01–20 agar sesuai realita; update 20-roadmap; changelog; tag `v1.0.0`.

**Exit:** delapan kriteria 01 §5 tercentang dengan bukti; `v1.0.0` dirilis.

---

# 6. Peta Konteks Hemat (ringkasan "baca apa")

```text
M00  10, 15, 16, 19§3            M08  09, 08§10.4
M01  04, 03§41-51                M09  09§8, 02§23-27
M02  10§6-13, 13§2, 08, 14       M10  11, 08§2-3
M03  05, 12§4                    M11  11§5-9, 07§11
M04  06, 07§7                    M12  06§5,10 · 13§4
M05  02§6-11, 08§10.6-9, 05§13   M13  11§8, 09§2.1
M06  13, 04§13                   M14  17, 07§11-12
M07  07, 10§4, 17§3-9            M15  12, 14, 18§5, 19
```

Selalu ditambah: `CLAUDE.md`, `docs/STATUS.md`, HANDOVER terakhir, dan bagian milestone ini di 21. (`docs/00-errata-and-amendments.md` hanya bila perlu alasan sebuah keputusan.)
