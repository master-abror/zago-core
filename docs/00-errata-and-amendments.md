# Errata & Amendments — Revision 1.1 Change Record
## Open Source Modular Application Platform v1.0

**Document:** 00-errata-and-amendments.md
**Status:** **APPLIED IN REV 1.1** — semua item di bawah sudah diterapkan ke dokumen 01–20 (yang kini bertanda *Baseline (Rev 1.1)*). Dokumen ini dipertahankan sebagai **catatan perubahan dan alasan** (mengapa sesuatu diubah).
**Tanggal review:** 2026-09-30
**Dibaca oleh:** sesi yang butuh memahami *alasan* sebuah keputusan; bukan lagi sumber aturan utama

---

# 0. Cara memakai dokumen ini

Dokumen 01–20 sudah dituliskan ulang (Rev 1.1) dengan seluruh item di bawah diterapkan. Karena itu:

1. **Sumber aturan = dokumen 01–20 Rev 1.1.** Dokumen ini hanya menjelaskan *apa yang berubah dan kenapa* — berguna saat sebuah keputusan terlihat aneh dan Anda perlu tahu alasannya.
2. Urutan prioritas bila ada pertentangan: **ADR terbaru** (`docs/adr/`) > dokumen 01–20 Rev 1.1 > dokumen ini.
3. Kolom "Diterapkan di" pada tiap item menunjuk bagian dokumen yang kini memuatnya. Bila implementasi nanti harus menyimpang, tulis ADR dan sinkronkan dokumen yang bersangkutan (task **Doc-sync** di akhir milestone).
4. Severity: **B** = Blocker, **I** = Important, **D** = Dokumentasi/referensi.
5. Pemetaan item → milestone (E) dan pemetaan perubahan per dokumen (F) ada di akhir.

---

# A. Keputusan Stack (ADR-0001)

Diverifikasi per 2026-09-30: PostgreSQL 18 adalah rilis stabil terbaru (18.6); PostgreSQL 19 masih beta. Redis 8 adalah lini terbaru dan berlisensi AGPLv3 (opsi lain: RSALv2/SSPLv1); fork BSD-nya adalah Valkey. **Versi patch di-pin saat M00** (jalankan `docker pull` dan catat versi persisnya di `docs/adr/0001-stack.md`).

| Area | Keputusan | Catatan |
|---|---|---|
| Bahasa | Go stable terbaru; pin `go` + `toolchain` di `go.mod` | Dokumen menulis 1.23 → naikkan |
| HTTP router | **chi v5** (`github.com/go-chi/chi/v5`) | Sudah dipakai contoh di doc 17 |
| DB driver | **jackc/pgx v5** + `pgxpool` | Bukan `database/sql` (doc 10 §13 ambigu) |
| Query layer | **sqlc** (target pgx/v5) + SQL tulisan tangan; tanpa ORM | Sesuai prinsip "database source of truth". Modul boleh pakai pgx langsung |
| Migrasi | **golang-migrate sebagai library** di `cmd/migrate` (driver `pgx5://`) | CLI tidak perlu dipasang; CI & Docker memakai satu binary |
| PostgreSQL | **18.x** (`postgres:18`) | Punya `uuidv7()` native (lihat B-08) |
| Redis | **8.x** (`redis:8`) | Kode hanya memakai perintah dasar (string/hash/set/INCR/EXPIRE/Pub-Sub/Stream) sehingga kompatibel dengan Valkey; jelaskan pilihan lisensi di README |
| Redis client | `github.com/redis/go-redis/v9` | |
| WebSocket | `github.com/coder/websocket` | |
| Logging | `log/slog` (JSON) dibungkus `pkg/logger` | |
| Telemetry | OpenTelemetry (trace) + Prometheus (`/metrics`) | |
| Config | `github.com/caarlos0/env/v11` | Sesuai tag `env:"…,required"` di doc 10 §8 |
| Validasi | `go-playground/validator/v10` | |
| UUID | `google/uuid` (`NewV7`) lewat `pkg/id` | |
| Password | `golang.org/x/crypto/argon2` | Lihat I-04 |
| Email | SMTP (`go-mail`) ; dev: **Mailpit** di compose | Dibutuhkan invite + reset password |
| API docs | `swaggo/swag` → `swagger.json` → `packages/ts-sdk` | Lihat I-14 |
| Test | testify, testcontainers-go (postgres+redis), Vitest, Playwright | |
| Frontend | **Svelte 5 (runes) + Vite + TypeScript strict**, SPA murni | Router & UI kit diputuskan via ADR di M10 |
| Node | LTS aktif (24) | Node 20 (doc 15/19) sudah EOL |
| JS workspace | npm workspaces (`apps/web`, `packages/*`, `modules/*/frontend`) | |

---

# B. Blocker

## B-01 — EventBus in-process tidak bisa menjangkau worker (Doc 07 §8, 10 §5, 14 §6)
`cmd/api` dan `cmd/worker` adalah dua proses. `EventBus` in-memory tidak bisa mengirim event dari API ke worker, padahal doc 14 §6 mengandalkan `trace_id` mengalir ke job worker.

**Keputusan:** dua jalur.
1. Bus in-process sinkron: hanya untuk reaksi lokal murah (mis. invalidasi cache authz).
2. **Transactional Outbox:** tabel `outbox_events` ditulis dalam transaksi yang sama dengan perubahan state; relay di worker memakai `SELECT … FOR UPDATE SKIP LOCKED`, pengiriman **at-least-once**, handler wajib idempotent (dedupe berdasarkan `event_id`). `EventBus.Publish` di `module-sdk` menulis ke outbox bila ada transaksi di `ctx`.

Berlaku: M01 (tabel), M02 (implementasi).

## B-02 — Perubahan state, audit, dan event tidak atomik (Doc 10 §6 langkah d–f)
Kalau proses mati setelah `Save` tetapi sebelum `audit.Record`, jejak audit hilang.

**Keputusan:** `TxManager.WithinTx(ctx, fn)` di layer application. `Save` + `audit.Record` + `events.Publish` (outbox) berada dalam **satu transaksi**. Untuk aksi sensitif, kegagalan menulis audit **membatalkan** operasi (fail-closed). Activity berhasil `result=denied/failed` ditulis di transaksi terpisah (karena transaksi utama di-rollback). Berlaku: M02.

## B-03 — Model privilege database saling bertentangan (Doc 04 §13, 07 §3, 13 §6–7)
1. `install` modul menjalankan DDL saat runtime, tetapi `app_user` hanya DML (dan PostgreSQL 15+ tidak memberi `CREATE` di schema `public` ke semua user).
2. `user.anonymize` (13 §7) = `UPDATE activities`, retention (13 §6) = `DELETE` — keduanya di-`REVOKE` (04 §13).
3. `GRANT … ON ALL TABLES` hanya berlaku untuk tabel yang **sudah ada**; tabel modul yang dibuat kemudian tidak ter-grant.
4. `CREATE ROLE` di dalam migration butuh `CREATEROLE` dan bersifat cluster-level.

**Keputusan:** tiga role, dibuat oleh init infrastruktur (`deploy/db/init/`), **bukan** oleh migration:

| Role | Dipakai oleh | Hak |
|---|---|---|
| `app_migrator` | `cmd/migrate` dan lifecycle modul (install/uninstall) | Pemilik objek, DDL |
| `app_user` | API + worker (runtime) | DML; **tanpa** UPDATE/DELETE pada `activities`, `security_events`, `system_logs` |
| `app_maintenance` | hanya job retention & `user.anonymize` di worker | `DELETE` batch pada tabel log, `UPDATE (actor_user_id, metadata)` pada `activities` |

Tambahkan `ALTER DEFAULT PRIVILEGES FOR ROLE app_migrator IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO app_user;`. Env baru: `MIGRATION_DATABASE_URL`, `MAINTENANCE_DATABASE_URL`. Berlaku: M01 (grants), M06 (maintenance), M07 (module install).

## B-04 — Tidak ada bootstrap Super Admin; katalog permission tidak lengkap (Doc 04 §16)
Seed membuat role Super Admin tapi tidak ada user pemegangnya; kriteria sukses 01 §5 tidak bisa dimulai. Seed hanya 10 permission, sedangkan endpoint di 08 butuh jauh lebih banyak (`organization.*`, `group.*`, `profile.update`, `conversation.*`, `policy.*`, `permission.simulate`, `settings.*`, `activity.read_group`, dst.).

**Keputusan:**
- Katalog permission core **dideklarasikan di kode** (`internal/permission/catalog.go`) dan disinkron idempotent saat startup (upsert by `code`, tidak pernah menghapus otomatis) — pola yang sama dengan modul.
- Role sistem (Super Admin, Org Admin, Group Admin, Member) di-seed dari kode beserta set permission default.
- Perintah `make bootstrap-admin EMAIL=…` membuat user + `role_assignment` scope platform. Password diinput interaktif/stdin atau dibuat acak sekali-tampil — **tidak pernah** dari file yang di-commit.

Berlaku: M03 (bootstrap), M04 (katalog + role sistem).

## B-05 — Bug Docker/Makefile/CI (Doc 15, 16, 19)
1. Compose `build.context: ./backend` tidak bisa melihat `go.mod` di root, `packages/module-sdk`, dan `modules/`. → **context = root repo**, Dockerfile di `deploy/docker/`.
2. `go test ./... ./../modules/.../backend/...` bukan pola valid. Karena `go.mod` di root → cukup `go test ./...` dari root.
3. `make setup` mengklaim hanya butuh Docker/Go/Node, padahal butuh `migrate`, `swag`, `golangci-lint`, `goimports`. → pin lewat `tool` directive di `go.mod` (`go tool …`) + `make tools`; migrasi lewat `cmd/migrate`.
4. File yang dirujuk tapi tidak ada: `docker-compose.prod.yml`, `nginx.conf`, `scripts/wait-for-healthy.sh`, `scripts/create-module.sh`. → dibuat di M00/M14/M15.
5. `make backend` menjalankan dua proses dengan `&` → proses yatim saat Ctrl-C. → target `run-api`, `run-worker`; `make dev` memakai `scripts/dev.sh` dengan `trap 'kill 0' EXIT` dan **air** untuk hot reload.
6. `docker-up` berarti "hanya postgres+redis" (16) sekaligus "semua service" (15). → `make infra-up` (postgres, redis, mailpit) dan `make docker-up` (profile `full`).
7. Image `postgres:18` mengubah lokasi `PGDATA`; mount volume di `/var/lib/postgresql` (bukan `/var/lib/postgresql/data`).
8. Publish port datastore hanya ke `127.0.0.1`.
9. CI: `postgres:17`→`18`, `redis:7`→`8`, Node 20→LTS aktif, Go→stable terbaru; job `migration-rollback` menjadi `make migrate-roundtrip` (up → down → up).
10. Target baru **`make verify`** = lint + test + migrate-roundtrip + build + smoke. Satu pintu untuk CI **dan** untuk memverifikasi hasil milestone sebelumnya di awal chat baru.

Berlaku: M00.

## B-06 — Invariant tenant tidak ditegakkan database (Doc 02 §34 #1, #2, #5; Doc 04 §6)
- `group_memberships` tidak menjamin user punya `organization_membership` di organisasi grup (inv. 2).
- `conversation_members` tidak menjamin organisasi yang sama (inv. 5).
- Trigger parent group tidak mencegah **siklus**, dan `groups.organization_id` masih bisa diubah sehingga anak menjadi lintas organisasi (inv. 1).

**Keputusan:**
- Tambah `organization_id` pada `group_memberships` dan `conversation_members`.
- `UNIQUE (id, organization_id)` pada `groups` dan `conversations`.
- FK komposit: `(group_id, organization_id) → groups(id, organization_id)` dan `(organization_id, user_id) → organization_memberships(organization_id, user_id)`.
- Trigger: `organization_id` pada `groups` **immutable**; cegah siklus parent (recursive CTE) dengan batas kedalaman yang bisa dikonfigurasi (default 8).
- Status `active` pada membership tetap dicek di aplikasi.

Berlaku: M01.

## B-07 — Desain token sesi (Doc 05 §7.1)
Doc 05 membolehkan UUIDv7 sebagai session ID. UUIDv7 memuat timestamp dan hanya ±74 bit acak — **jangan dipakai sebagai bearer token**.

**Keputusan:**
- Token cookie = 32 byte `crypto/rand`, base64url.
- Redis key = `session:{sha256(token)}` (dump Redis tidak membocorkan token yang valid).
- `sessions.id` (UUIDv7) adalah ID internal; tambah kolom `sessions.token_hash bytea UNIQUE`.
- Redis set `user_sessions:{user_id}` agar revoke-all O(1).
- Sesi dicabut / user disuspend → publish `session.revoked` → Hub WebSocket menutup koneksi (I-09).
- Redis down → fallback lookup ke `sessions` di PostgreSQL berdasarkan `token_hash` (mode degraded + log warn), bukan logout massal.
- Produksi: nama cookie berprefiks `__Host-` (Secure, Path=/, tanpa Domain); nama dan flag `Secure` dikontrol config (`COOKIE_NAME`, `COOKIE_SECURE`) untuk dev http.

Berlaku: M01 (kolom), M03.

## B-08 — Celah skema (Doc 04)
1. **`roles` tanpa `group_id`** padahal 01 §3 dan 06 §5.3 menyebut role per-group yang dibuat Group Admin. Tambah `group_id` (FK komposit dengan org), `CHECK`: `role_type='system' ⇔ organization_id IS NULL`, `role_type='group' ⇒ group_id NOT NULL`; uniqueness slug per `(organization_id, group_id)` dengan partial index untuk kombinasi NULL.
2. **`role_assignments`**: tidak ada `CHECK` scope dan tidak ada uniqueness. Tambah `CHECK` (`platform ⇒ scope_id IS NULL`; `group|resource ⇒ scope_id NOT NULL`) dan unique index aktif `(role_id, subject_type, subject_id, scope_type, COALESCE(scope_id, '00000000-0000-0000-0000-000000000000')) WHERE revoked_at IS NULL`.
3. **Email**: tambah `CHECK (email = lower(email))` agar normalisasi tidak hanya bergantung pada disiplin aplikasi.
4. **Tabel baru:** `outbox_events` (B-01), `invitations` (org, email, invited_by, preset role/group, `token_hash`, `expires_at`, `accepted_at`, `revoked_at`), `password_reset_tokens` (user, `token_hash`, `expires_at`, `used_at`). Idempotency key disimpan di Redis (I-07), bukan tabel.
5. **`conversations.direct_key`** (mis. `least(uid_a,uid_b)||':'||greatest(...)`) + `UNIQUE (organization_id, direct_key) WHERE type='direct'` — tanpa ini "reuse direct conversation" (09 §8.1) balapan dan menghasilkan duplikat.
6. **`notifications.title/body` NOT NULL** bertentangan dengan 08 §8 (teks dirender klien dari `type`). Jadikan nullable sebagai fallback Inggris; parameter i18n di `data`.
7. **PostgreSQL 18 punya `uuidv7()` native.** Revisi 04 §2.1: aplikasi tetap membuat ID (`pkg/id`, karena ID dibutuhkan sebelum insert untuk audit/outbox dalam satu transaksi), tetapi semua kolom `id` diberi `DEFAULT uuidv7()` sebagai jaring pengaman dan seed memakai `uuidv7()`. Hapus `pgcrypto`.
8. **Trigger `updated_at`**: 04 §4 berkata "assume exists" → rawan terlewat. Buat helper `attach_updated_at(regclass)` yang dipanggil eksplisit di tiap migration + test yang memindai catalog agar tidak ada tabel berkolom `updated_at` tanpa trigger.
9. **Penomoran migrasi**: golang-migrate hanya menerapkan versi > versi saat ini, jadi nomor harus monoton. Urutan di 04 §14 tetap jadi acuan dependensi; tabel tambahan (B-01/B-08) diberi nomor berikutnya dan 04 di-patch.
10. Tabel append-only sebaiknya tanpa FK beraksi (`SET NULL`) karena aksi RI memutasi baris yang seharusnya immutable → gunakan `NO ACTION`.

Berlaku: M01.

## B-09 — Celah kontrak modul (Doc 07 §5, Doc 17)
1. `Migrate(ctx, *sql.DB)` tidak cocok dengan pgx → `Migrate(ctx, MigrationRunner)` (interface di SDK; implementasi core memakai pool `app_migrator` dan **tabel versi per modul** `schema_migrations_<code>`). Ini juga menyelesaikan konflik "nomor migrasi dicadangkan" (04 §14) vs "001" (17 §5): tiap modul mulai dari 001.
2. `Module` belum punya `Health(ctx) error` padahal 10 §11 dan 14 §7 mengandalkannya; tambah juga `RegisterJobs`. `RegisterEventHandlers` dijalankan di **worker** (via outbox); `RegisterRoutes` di **API**.
3. Route perlu deklarasi permission: `r.POST(path, handler, modulesdk.Require("announcement.publish"))`. Tanpa ini klaim di 17 §9 ("middleware sudah memeriksa") tidak punya dasar.
4. Belum ada mekanisme daftar modul saat compile. → generator `make modules-sync` membaca `modules/*/module.yaml` dan menulis `backend/cmd/{api,worker}/modules_gen.go` dan `apps/web/src/modules_gen.ts`. **"Install" = aktivasi di database untuk modul yang sudah ter-compile**, bukan memasang kode. Dokumentasikan di 07 §3.
5. `module-sdk.Router` = lapisan tipis di atas chi (`Get/Post/Put/Patch/Delete/Group`) plus `URLParam(r, "id")` sehingga modul tidak perlu mengimpor chi.
6. Doc 17 §9 memakai `uuid.MustParse(chi.URLParam(...))` → panic/500 untuk input buruk. Ganti `uuid.Parse` + error `validation_failed` (400).
7. Notification Template (07 §8) dinaikkan dari "future" ke v1.0: manifest punya bagian `notifications:` (event → `type` + pemetaan parameter).

Berlaku: M07, M14.

## B-10 — Origin, cookie, CORS, dan WebSocket (Doc 05 §8, 09 §2, 15)
SPA (`:5173`) dan API (`:8080`) beda origin → CORS + credentials rapuh, dan upgrade WebSocket adalah `GET` sehingga **tidak tercakup token CSRF** (Cross-Site WebSocket Hijacking).

**Keputusan:**
- **Same-origin**: dev lewat proxy Vite (`/api`, `/ws` → `:8080`); produksi lewat reverse proxy (Caddy/nginx) satu host. Tidak ada CORS.
- Handshake WebSocket wajib memverifikasi header `Origin` terhadap `ALLOWED_ORIGINS`.
- `X-Forwarded-*` hanya dipercaya dari `TRUSTED_PROXIES`.
- Security headers (CSP `default-src 'self'`, dst.) dipasang di proxy dan API.

Berlaku: M00 (proxy dev), M08 (Origin check), M15 (proxy prod).

---

# C. Important

| ID | Temuan → Keputusan | Milestone |
|---|---|---|
| **I-01** | Kunci cache authz per-user (06 §8) tak bisa di-invalidate saat `role_permissions` berubah tanpa scan. → **versi per organisasi**: `authz:ver:{org}` di-`INCR`; kunci `authz:eff:{org}:{ver}:{user}:{group\|-}`; TTL pendek hanya jaring pengaman. Redis down → hitung dari PostgreSQL (fail-safe, bukan fail-open). | M04 |
| **I-02** | `ScopeContext.OrganizationID` non-nil (06 §3) vs `Identity.OrganizationID` nil untuk Super Admin. → jadikan `*uuid.UUID`. Permintaan platform-level memakai method repository berlabel eksplisit (`…Platform…`) yang hanya boleh dipanggil bila keputusan authz bersifat platform. Ini jalur resmi untuk "Super Admin melihat semua Activities" tanpa melanggar aturan "query wajib `organization_id`" (12 §6). | M04, M06 |
| **I-03** | `identity.active_groups` (06 §5.2) tak terdefinisi. → grup dengan `group_membership` `active` milik user di organisasi tsb. Assignment `subject_type=group` diwarisi semua anggota aktif; cakupannya tetap ditentukan `scope_type/scope_id`. | M04 |
| **I-04** | Argon2id: parameter awal m=64 MiB, t=3, p=2 (target 50–150 ms di hardware deploy; tuning via config), format PHC string, **rehash saat login** bila parameter berubah, batas panjang password (maks 256 byte), **dummy hash** untuk email tak dikenal (samakan timing → cegah enumerasi via waktu respons), semaphore concurrency hashing agar login flood tidak menghabiskan memori. | M03 |
| **I-05** | `users.status='locked'` tak punya kedaluwarsa, sedangkan 05 §12.3 bicara "temporary lock". → lock sementara di Redis (`auth:lock:{user_id}` + TTL + progressive delay); status `locked` hanya untuk kunci manual admin. | M03 |
| **I-06** | Cursor pagination: keyset `(created_at, id)`, opaque, **HMAC-signed**, memuat arah + hash filter. UUIDv7 dari beberapa instance tidak monotonik ketat → jangan `ORDER BY id` saja. | M02 |
| **I-07** | Idempotency: Redis 24 jam, kunci `idem:{user_id}:{method}:{route}:{key}` → `{request_hash,status,body}`; duplikat konkuren → 409 `idempotency_in_progress`; tak ada replay lintas user. | M02 |
| **I-08** | Registry error (08 §11) ditambah: `internal_error` (dipakai 10 §9 tapi belum terdaftar), `version_conflict` (409, optimistic locking 03 §48), `payload_too_large`, `unsupported_media_type`, `idempotency_in_progress`. | M02 |
| **I-09** | Operasi WebSocket (09): ping/pong ±30 s, read limit (mis. 64 KiB), send buffer terbatas & putus slow consumer, batas koneksi per user, tutup koneksi saat sesi dicabut / user atau org disuspend. Reconnect replay hanya menjamin event `*.created`; `updated/deleted/typing` tidak durable → server membalas `resync_required` bila ada celah, klien refetch via REST. | M08 |
| **I-10** | **Pull-forward**: forgot/reset password + invitation masuk v1.0 (M05). Kriteria 01 §5 #1 ("invite a Group Admin") butuh mekanisme token + email yang sama. Update 20-roadmap §3. TOTP MFA tetap Phase 2. | M05 |
| **I-11** | Attachment: interface `storage` (driver disk lokal di v1; S3-compatible → roadmap), deteksi content-type di server, batas ukuran default 10 MiB (bisa dikonfigurasi). Menutup open item 12 §4.5 dengan angka awal. | M09 |
| **I-12** | Config tambahan (10 §8): `ALLOWED_ORIGINS`, `TRUSTED_PROXIES`, `COOKIE_NAME`, `COOKIE_SECURE`, `MIGRATION_DATABASE_URL`, `MAINTENANCE_DATABASE_URL`, `SMTP_*`, `PUBLIC_BASE_URL`, `STORAGE_PATH`, `LOG_LEVEL`, `OTEL_EXPORTER_OTLP_ENDPOINT`. Semua tervalidasi saat startup. | M00–M05 |
| **I-13** | Frontend: `can()` hanya mencerminkan permission yang tak bergantung resource/own-scope (06 §5.2). Untuk aksi per-resource, respons detail boleh membawa `meta.allowed_actions` yang dihitung server. Field permission tetap lewat "field absen". | M10 |
| **I-14** | swag v1 menghasilkan OpenAPI 2.0. Evaluasi swag v2 (OpenAPI 3.x) + `openapi-typescript`; putuskan lewat ADR di M10. | M10 |
| **I-15** | `/health/ready`: timeout 1–2 s per dependency, tanpa query berat; modul unhealthy tidak menggugurkan readiness kecuali mode `strict`. | M07 |
| **I-16** | Retention/partisi: v1 tanpa partisi; retention = batch delete oleh `app_maintenance`. Catat di roadmap: partisi bulanan `activities`/`system_logs`/`messages` mengharuskan PK `(id, created_at)` — ditunda secara sadar. | M06 |

---

# D. Dokumentasi & Referensi

| ID | Temuan → Perbaikan |
|---|---|
| **D-01** | "Master architecture §N" dirujuk ±30 kali tetapi file-nya tidak ada di set dokumen. → Bila ada, simpan sebagai `docs/00-master-architecture.md`; bila tidak, anggap dokumen 01–20 + file ini sudah mandiri (default). |
| **D-02** | Header 13-audit-logging.md masih berkata dokumen 08/10/11/12 belum ditulis → hapus catatan. |
| **D-03** | Referensi silang usang: 06 §5.3 "05 §37" → 05 §22; 12 §4.6 "05 §4.7" → 05 §15; 13 §3 "05 §26" → 05 §14; 10 §11 "07 §13 module health" → `Health()` di 07 §5 (B-09). |
| **D-04** | Ganti nama `03-database-erd_2_.md` → `03-database-erd.md`. |
| **D-05** | 05 §2 menulis cookie `SameSite` tanpa nilai, §8 `Lax` → tetapkan `Lax`. |
| **D-06** | Seed 04 §16 memakai `gen_random_uuid()` → `uuidv7()` (lihat B-08.7). |
| **D-07** | 01 §5 dan 20 §2 harus diperbarui setelah I-10 (invite + reset password masuk v1.0). |

---

# E. Ringkasan Dampak per Milestone

```text
M00  B-05, B-10(dev), I-12
M01  B-03, B-06, B-07(kolom), B-08, D-04, D-06
M02  B-01, B-02, I-06, I-07, I-08
M03  B-04(bootstrap), B-07, I-04, I-05
M04  B-04(katalog), I-01, I-02, I-03
M05  I-10
M06  B-03(maintenance), I-16
M07  B-09, I-15
M08  B-10(Origin), I-09
M09  I-11
M10  I-13, I-14
M14  B-09(scaffold)
M15  B-10(proxy prod), D-01, D-07
```

---

# F. Diterapkan di (peta perubahan per dokumen)

| Dokumen | Perubahan Rev 1.1 |
|---|---|
| 01 Product Requirements | invitation + reset password masuk v1.0; kriteria sukses diperbarui (bootstrap admin, undangan email, `make verify`); technology baseline; glosarium (Outbox, Invitation, Milestone); peta dokumen 00–23 |
| 02 Domain Model | invariant 16–18 + tabel penegakan (§34.1); group membership/conversation member membawa `organization_id`; `roles.group_id`; aturan scope role assignment; entitas Invitation, Password Reset Token, Outbox Event; aturan token sesi; `direct_key`; notifikasi title/body opsional |
| 03 Database ERD | PostgreSQL 18, `uuidv7()`; composite FK; tabel baru; soft reference pada tabel append-only; urutan migrasi; kunci cache versi; transaksi dengan outbox |
| 04 Database Schema | ditulis ulang penuh: 33 migrasi, `attach_updated_at`, trigger hierarki group, composite FK, `roles` boundary CHECK, `role_assignments` CHECK/unique, `sessions.token_hash`, `invitations`, `password_reset_tokens`, `outbox_events`, tiga role database + grants + default privileges, bootstrap & katalog dari kode, tes skema |
| 05 Authentication | token sesi 256-bit + hash, fallback PostgreSQL, WebSocket revoke, `__Host-` cookie, CSRF terikat sesi, Argon2id (PHC, rehash, dummy verify, limiter), lock di Redis, reset password + undangan (Phase 1), error `invalid_invitation` |
| 06 Authorization | `ScopeContext` nullable, `Decision.Level`, definisi group subject, katalog permission core (§5.4), role sistem default (§5.5), endpoint self-service (§5.6), cache versi per organisasi (§8), fail-safe |
| 07 Module System | arti "install", kontrak `Module` (Migrate runner, Health, RegisterJobs), Router tipis + `Require`, registry compile-time (`make modules-sync`), migrasi per modul + tabel versi, module gate, event: outbox vs in-process, `notifications:` & `jobs:` di manifest, aturan namespace permission |
| 08 API | same-origin, batas body, optimistic concurrency, `meta.allowed_actions`, cursor HMAC keyset, idempotency Redis terscope user, error registry tambahan + pemetaan HTTP, endpoint invitation & attachment |
| 09 Realtime | Origin check, kebersihan koneksi (ping, limit, backpressure), resync, format frame, `direct_key`, atribut attachment aman, penanganan revoke |
| 10 Backend | baseline teknologi (chi, pgx, sqlc, dst.), tiga binary, `internal/kernel`, `TxManager` + outbox, config lengkap, tiga pool database, perilaku saat dependensi mati, aturan kritis baru |
| 11 Frontend | baseline, same-origin + proxy Vite, kriteria ADR router/i18n, `meta.allowed_actions`, resync WebSocket, aturan render konten tak tepercaya |
| 12 Threat Model | aktor #8 (halaman web berbahaya), CSWSH, token hash, timing, batas privilege DB, outbox/audit atomik, supply chain (distroless, pin tool), tabel tes baru |
| 13 Audit | atomik dengan state, `denied` terpisah, role maintenance untuk anonymize/retention, kebijakan visibilitas per peran, ekspor via worker + anti formula injection |
| 14 Observability | slog, metrik outbox, `/metrics` tidak publik, label route pattern, trace melintasi outbox |
| 15 Docker | PostgreSQL 18 (mount `/var/lib/postgresql`), Redis 8, Mailpit, service `migrate`, profile `full`, context root, distroless nonroot, same-origin serving, `.env.example` |
| 16 Makefile | seluruh target diperbaiki (`tools`, `infra-up`, `docker-up`, `run-*`, `migrate-roundtrip`, `bootstrap-admin`, `verify`, `smoke`, `modules-sync`, `handover-pack`) |
| 17 Module Guide | `uuid.Parse`, `Require()`/`Idempotent()`, transaksi tunggal, migrasi per modul, `attach_updated_at`, notifikasi lewat manifest, tes tenant, pitfalls baru |
| 18 Testing | katalog negatif terkonsolidasi (DB, kernel, API, realtime), cek khusus modul, fixture, polling bukan sleep |
| 19 CI/CD | versi dari ADR/`go.mod`, job `migrations`, `docker-smoke` + `make smoke`, `contract-check` mencakup registry, scan image, rilis dengan urutan migrate |
| 20 Roadmap | pull-forward invitation/reset, item baru (Valkey CI, S3, partisi, RLS) |

---

# G. Temuan tambahan saat penulisan ulang

Ditemukan dan diperbaiki ketika dokumen ditulis ulang (tidak ada di daftar B/I/D sebelumnya):

```text
G-01  Tabel append-only memakai FK beraksi (SET NULL) → mutasi baris "immutable"; diganti soft reference (04 §2.5).
G-02  `modules.code` dipakai sebagai prefix tabel & nama tabel versi migrasi tetapi tak divalidasi → CHECK format (04 §7).
G-03  `credentials` boleh punya >1 password aktif → unique index parsial (04 §10).
G-04  `module_installations` & `settings` tanpa kolom versi untuk optimistic locking padahal 03 §48 mewajibkan → `row_version`.
G-05  Visibilitas "All Activities" untuk Organization Admin tak terdefinisi → ditambahkan (13 §4).
G-06  Pengecekan Origin WebSocket + format frame tak ada → ditambahkan (09 §2).
G-07  `/metrics` tak boleh publik; label metrik berkardinalitas tinggi → aturan di 14 §5.
G-08  Ekspor CSV rawan formula injection → dinetralkan (13 §8).
G-09  Image produksi berjalan sebagai root & berisi `go install air@latest` tak ter-pin → distroless nonroot; tool dipin lewat `go.mod`.
G-10  `uuid.MustParse` pada input pengguna di contoh modul → `uuid.Parse` (17 §9).
G-11  Idempotency tanpa penanganan Redis down → fail-closed untuk endpoint yang mewajibkan key (08 §6).
G-12  `attachments` tak terhubung ke pesan → kolom `message_id` + indeks (04 §11).
```
