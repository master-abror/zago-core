# HANDOVER — M05 Organization, User, Group, Invitation, Email (PARSIAL 3)

**Tanggal:** 2026-11-21 · **Branch:** `milestone/m05-organization` (dari tag `m04-done` = `6a14f87`; ada di origin) · **Sesi:** partial-3 (T1a-3a selesai; checkpoint SEBELUM T1a-3b)
**Prompt sesi ini:** `docs/handover/PROMPT-M05-partial-2.md`. **Prompt sesi berikutnya:** `docs/handover/PROMPT-M05-partial-3.md`.

## 1. Ringkasan
Sesi ini (1) memulihkan baseline pada kode M05, (2) membangun T1a-3a: domain dan repository ANGGOTA organisasi (`organization_memberships`) dengan 33 tes baru, lalu (3) checkpoint sebelum T1a-3b karena tanda-tanda konteks (puluhan giliran, banyak log ditempel, dua patch kode, banyak berkas dibaca). Gerbang kode T1a-3a HIJAU di laptop (§3) setelah satu temuan `go vet` (nama tes ganda, kesalahan saya) yang diperbaiki sebelum `git push`. Keputusan desain ditulis di `docs/adr/0023-m05-organization-user-group-design.md` butir 11-14 (BELUM dikonfirmasi; dipakai sebagai default karena pemilik tidak membantah).

**Keadaan `make verify`: TIDAK hijau.** Run utuh pada kode M05 (`78f232a`, sebelum T1a-3a) = `exit=2`: hanya paket `authorization/cache` merah (pesan kelas reaper/Docker; hijau sendirian dua kali; sebab TIDAK terbukti). Smoke alur dingin `verify-smoke.sh` gagal DUA kali dengan sebab berbeda (unduhan `npm ci` `ECONNRESET`; unduhan modul Go `unexpected EOF`); tes API tidak pernah jalan pada run itu. Smoke di stack manual = `exit=0` (m00, m02, m03, m04 OK). Detail dan keluaran di §3.

**Belum dikerjakan dari rencana T1a:** service anggota organisasi (T1a-3b), `RequireTenant`, DTO, handler HTTP, `DomainError`, perakitan rute, `make generate`. Tidak ada kode setengah jadi.

## 2. Status task
| Task | Isi | Status |
|---|---|---|
| T0 | Baseline + patch smoke (`3977aa6`, `4ff53dd`) | SELESAI (partial-1); baseline run utuh sesi ini `exit=0` |
| T1a-1 | domain `organization` + repository `Organizations` + 37 tes (18 domain, 19 repository ber-DB; lintas-organisasi DUA ARAH) | SELESAI, hijau di laptop (commit `246032c` + perbaikan `2b0f474`, sudah di origin) |
| T1a-2 | service `Create/Get/List/Update/Archive` + 26 tes (20 unit, 6 ber-DB: urutan kunci, penahanan kunci, serentak, rollback) | SELESAI, hijau di laptop (commit `7039796`; BELUM di-push saat checkpoint dibuat: lihat §3) |
| T1a-3a | domain anggota + repository `OrganizationMembers` (`Get`, `List` cursor, `Add` platform-saja, `Remove` logis, `RemoveFromGroups`) + 33 tes (10 domain, 23 repository ber-DB; lintas-organisasi DUA ARAH, keunikan dengan dua organisasi) | SELESAI, hijau di laptop (commit `df8af41`, sudah di origin) |
| T1a-3b | service anggota (Add platform-saja, Remove + akibatnya, kunci advisory pertama, `InvalidateInTx`, audit, event) + tes tiga lapis | BELUM (§10) |
| T1a-4 | `RequireTenant`, DTO, handler `/organizations` (+ anggota), `DomainError`, perakit rute, `make generate` | BELUM (§10) |
| T1b | `identity`: users dan `profile`, kode `<tipe>.field.*`, pemakai pertama `FieldHelper` | BELUM |
| T1c | `group` dan `membership`: siklus/kedalaman 8, `activate`, pencabutan assignment ber-scope grup | BELUM |
| ◆CP | handover parsial | ◆CP ini = partial-3 |
| T2 | transaksi docs/03 §46 (create user, create group) dengan kunci advisory dan `InvalidateInTx` | BELUM |
| T3 | worker: konsumen event dan layanan email | BELUM |
| T4a/T4b | undangan; forgot/reset password; pembuka kunci akun administratif | BELUM |
| T5 | suspend/deactivate user dan organisasi, cabut sesi sesudah commit | BELUM |
| T6 | `scripts/smoke/m05.sh`, doc-sync, ritual akhir | BELUM |

## 3. Bukti verifikasi (apa adanya)
Semua keluaran di bawah ditempel pemilik pada sesi ini, kecuali yang ditandai "tidak ditempel". Bukti partial-2 ada di `HANDOVER-M05-partial-2.md` §3, partial-1 di `HANDOVER-M05-partial-1.md` §3.
- **Keadaan repo awal:** `milestone/m05-organization...origin/milestone/m05-organization`, HEAD `78f232a`, tag `m00-done`..`m04-done`, `m04-done` = `6a14f87` (lokal dan `git ls-remote`), `git fsck` tanpa keluaran, `git ls-files | wc -l` = 596, `ADR-0023` 7387 byte, `HANDOVER-M05-partial-2.md` 25452 byte, `PROMPT-M05-partial-2.md` 56597 byte; `git branch --show-current` = `milestone/m05-organization`.
- **Baseline (a)(a2)(a3):** `go1.26.5`, `gofmt-exit=0`; `docker image ls postgres` memuat `postgres:18` (juga `16-alpine`, `18-alpine`, `12.0`); `vet-exit=0`; `golangci-lint` `0 issues.` (app, authorization, organization); tidak ada container `platform*`/`zago*` (`grep-exit=1`).
- **Run utuh pada kode M05 `78f232a` (`gofmt -l backend; make verify GO_TEST_PARALLEL=1 > ~/verify.log`): `exit=2`.** Baris 358 `make: *** [Makefile:166: verify] Error 2` (didahului `test-backend` Error 1). Urutan target `verify` (dari Makefile): lint, generate-check, test, migrate-roundtrip, build, smoke; run berhenti di tahap test, jadi tiga tahap sesudahnya TIDAK berjalan pada run itu. Satu-satunya paket merah: `backend/internal/authorization/cache` (64,301 dtk): `TestMissStoreHitRoundTrip` gagal 63,98 dtk, lalu 20 tes gagal 0,00 dtk. Satu ragam pesan (32 kemunculan, terpotong 200 karakter): `gagal menyalakan redis:8 (Docker berjalan?): create container: reaper: new reaper: run container: started hook: wait until ready: external check: check target: retries: 441 address: loca...`. Paket lain pada 25 baris terakhir (kernel, httpx, organization/{application,domain,repository}, permission, policy, role, systemlog, testkit, testpg, migrations, pkg/*) `ok`; log lengkap tidak ditempel.
- **`cache` sendirian** (`REQUIRE_DOCKER=1 go test ./backend/internal/authorization/cache -race -count=1 -p 1`): `exit=0` `ok 37.318s`, lalu `exit=0` `ok 26.232s`; tanpa baris `Error:`. SEBAB kegagalan di run utuh TIDAK terbukti (tidak diberi label).
- **Tahap sisa:** `make migrate-roundtrip` `exit=0`; `make build` `exit=0`; container testcontainers sisa dibersihkan (tanpa keluaran).
- **`verify-smoke.sh` alur dingin #1: `exit=1`.** Build image `web` berhenti di `RUN npm ci` (`web.Dockerfile:9`): `npm error code ECONNRESET`, `network aborted` pada detik 105,6; tabel status container kosong. `curl https://registry.npmjs.org/svelte` dari WSL: `http=200 total=4.52s` (pengukuran saat itu; tidak menjelaskan putusnya unduhan).
- **`verify-smoke.sh` alur dingin #2: `exit=1`.** Build lolos; `platform-verify-migrate-1` `Exited (1)`; log migrate: `go: downloading ...` lalu `Get "https://proxy.golang.org/.../@v/...zip": unexpected EOF` untuk chi v5.3.2, pgx v5.11.0, uuid v1.6.0, x/crypto v0.57.0, go-redis v9.22.0, env v11.4.1; `api`, `web`, `worker` berstatus `Created`; tes API tidak berjalan. Dari skrip (bukan dari log): `verify-smoke.sh` menjalankan `down -v --remove-orphans` di `trap cleanup EXIT`, sehingga volume `go_mod_cache` ikut terhapus tiap run dan alur dingin selalu mengunduh ulang semua modul.
- **Stack manual `platform-m05`** (kode working tree = `78f232a`): `up -d --build` `exit=0`, `up -d` `exit=0`, `wait-for-healthy.sh` `exit=0`, `SMOKE_CURL_MAX_TIME=90 make smoke` `exit=0` (`M00 smoke OK`, `M02 smoke OK`, `M03 smoke OK`, `M04 smoke OK`); `down -v --remove-orphans` menghapus semua (juga volume `go_mod_cache`). `find . -path ./.git -prune -o -user root -print` keluaran kosong (kode keluar 0 itu milik `head`; buktinya keluaran kosong); `git status -sb` bersih dan sejajar origin.
- **T1a-3a, mbox pertama** (`m05-t1a-3a-members-repo.mbox`, `sha256sum` sandbox `b97ac1835af412ae`; `sha256sum` dan `git am` di laptop TIDAK ditempel). Gerbang pada dua commit: `gofmt-exit=0`; **`vet-exit=1`: `organizations_test.go:63:6: TestGetIsScopedToTheCallerOrganizationInBothDirections redeclared in this block`** (satu-satunya galat; kesalahan saya: nama tes bentrok, hanya fungsi non-tes yang di-grep); `golangci-lint` typecheck 1 temuan yang sama.
- **Perbaikan** (`m05-t1a-3a-fix-testname.mbox`, `sha256sum` `c84949ece4c4b002` cocok): `git am` `Applying`; `git log`: `751979f fix(...)` di atas `fc7ecb3 feat(...)` di atas `78f232a`; `git ls-files | wc -l` = 600; `grep -c 'func TestMemberGetIsScoped'` = 1. Gerbang: `gofmt-exit=0`, `vet-exit=0`, `golangci-lint` `0 issues.`, `go test .../domain -race -count=1` `ok 1.110s`, `REQUIRE_DOCKER=1 go test .../repository -race -count=1 -p 1` `exit=0` `ok 71.673s` tanpa baris `Error:`/`SQLSTATE`.
- **Pelipatan commit:** `git reset --soft HEAD~1 && git commit --amend --no-edit` (belum di-push) menghasilkan `df8af41` di atas `78f232a`; hash pohon sesudahnya `e2aa56ef977d99b812b19069ad1f1e45d5b5ed6d` = hash pohon sandbox (tumpukan zip + dua mbox, terbukti identik dengan `diff -r`). Hash pohon SEBELUM melipat tidak ditempel; kesetaraan dibuktikan lewat pohon sandbox. `git push`: `78f232a..df8af41 milestone/m05-organization -> milestone/m05-organization` (ditempel).
- **Hitungan tes (dari berkas):** `domain/member_test.go` 10; `repository/members_test.go` 23.
- **Dibuktikan di sandbox (bukan di mesin pemilik):** `git am` tumpukan mbox pada salinan bersih zip memberi pohon identik; `scripts/lint-structure.sh` OK; tidak ada indentasi non-tab di luar string SQL. Go/gofmt tidak tersedia di sandbox: kompilasi, gofmt, vet, lint, dan tes HANYA terbukti lewat keluaran laptop di atas.
- **Tidak dijalankan/tidak ditempel:** `make verify` utuh pada kode T1a-3a; `make generate`/`generate-check` (belum ada anotasi swag baru); smoke terhadap stack hidup pada kode T1a-3a (kode anggota belum terpasang di API); keluaran `sha256sum`/`git am` mbox pertama; hash pohon sebelum pelipatan.

## 4. Keputusan & penyimpangan (KUMULATIF; isi partial-1 dan partial-2 di bawah, keputusan baru partial-3 di akhir)
**Seluruh isi §4 HANDOVER-M04 tetap berlaku** (keputusan yang sudah dikonfirmasi; keputusan yang dipakai tetapi BELUM dikonfirmasi: migrasi 035 opsi A, opsi `Scope` idempotensi, semantik `simulate`, status bawaan `all` pada `GET /policies`, gagal-start tanpa policy atau pencabut sesi, pencabutan sesi satu percobaan 10 dtk tanpa outbox, semantik `FieldHelper`, fixture smoke lewat superuser; (a) nama definisi OpenAPI panjang diterima sementara, (b) blok "As built" di bawah judul bagian, (c) PROMPT P2 sebagai berkas; koreksi tag `m03-done` = `883487d`).

**Doc-sync tertunda (kumulatif dari HANDOVER-M04 §4; belum ada yang tergabung (partial-1 dan partial-2)):** contoh JSON respons `simulate` di docs/06 §10 dan daftar kode di docs/06 §12 dan docs/08 §11 tidak diedit di tempat (hanya blok di bawahnya); docs/13 §4 (aturan visibilitas aksi audit baru) dan docs/12 §6 tidak dibuka; docs/08 §10.2 tidak diubah; nomor bagian tepat di docs/10 dan docs/19 dipilih menurut judul tanpa membaca isi penuh; `HANDOVER-M03-partial-4.md` §4 dan `HANDOVER-M04-partial-1.md` §4 asli tidak ada. **Baru (partial-1):** docs/08 §10.6–§10.9 harus mendapat blok "As built (M05, ADR-0023)" setelah T1–T5 (path `{id}` dan scope, kode error baru, paginasi); docs/00 butir E:I-10 belum dibaca (tidak diperlukan sampai T3/T4).

**Keputusan partial-1 (smoke):**
1. Batas curl smoke m02/m03/m04 dibuat dapat diatur lewat `SMOKE_CURL_MAX_TIME`, bawaan 20 dtk (m02 dan m03 sebelumnya 8 dtk, m04 sudah 20). Sebagian menjawab pertanyaan lama §9(f) (naikkan batas login); volume cache modul `verify-smoke.sh` (`down -v`) TIDAK diubah.
2. Pesan `curl gagal` kini memuat `exit=<rc>` dan lama tunggu (detik). Perilaku lain tidak berubah.

**Usulan desain M05 (DIPAKAI sebagai default, BELUM dikonfirmasi pemilik; bantah bila perlu; DITULIS ke ADR-0023 butir 9 di partial-2 dengan status tetap BELUM dikonfirmasi):**
1. **Path `{id}` melawan "organisasi hanya dari ScopeContext":** pemanggil tenant: `{id}` harus sama dengan organisasi di scope, kalau tidak 404 seragam (tidak membedakan "bukan milikmu" dari "tidak ada"); hanya pemanggil platform boleh menarget organisasi lain, lewat rute/method berlabel eksplisit dengan keputusan platform.
2. **Accept-invitation untuk email yang sudah punya akun:** token undangan TIDAK boleh mengatur password akun yang sudah ada; akun `active` yang sudah ada hanya mendapat membership dan assignment dan wajib login lebih dulu; hanya akun baru yang mengatur password.
3. **TTL token:** undangan 72 jam, reset 30 menit (docs/05 §13); token hanya disimpan sebagai hash SHA-256, sekali pakai; respons invalid/terpakai/kedaluwarsa identik.
4. **Paginasi:** offset (`page`/`per_page`, bawaan 50, maksimum 100) untuk organisasi, user, grup; cursor untuk anggota dan undangan.
5. **`DELETE /organizations/{id}`** = `status = archived` + `deleted_at`, hak efektif kosong; cakupan pencabutan sesi (semua anggota atau hanya yang aktif di organisasi itu) diputuskan di T5 SETELAH skema sesi dibaca (belum diperiksa).
6. **Admin membuat user dengan email yang sudah ada = 409** membuka oracle keberadaan email bagi admin; dicatat sebagai keterbatasan, tidak ditutup.

**Keputusan baru sesi partial-2 (ditulis di `docs/adr/0023-m05-organization-user-group-design.md`, butir 1–10; yang bertanda BELUM dikonfirmasi dipakai sebagai default):**
1. Batas tenant `organizations` = `domain.Access` (nilai nol ditolak; tenant ≠ id = 404 seragam; Insert hanya platform).
2. Status: active↔suspended, keduanya → archived, **archived terminal** (BELUM dikonfirmasi); arsip mengisi `status` dan `deleted_at`.
3. Bentuk masukan (slug ASCII 2–100 tak dapat diubah, name 1–200 rune, legal_name ≤255, locale, timezone IANA dengan `time/tzdata` tertanam, tanpa slug cadangan) (BELUM dikonfirmasi).
4. Daftar: filter `open`/`all`/status, 50/100, tenant melihat satu organisasi (BELUM dikonfirmasi).
5. `Archive`: kunci advisory pertama, invalidasi di dalam transaksi sebelum audit/event, tolak tenant lain sebelum transaksi; sesi dicabut di T5.
6. `Update` no-op tanpa tulis/audit/event; audit dan event hanya nama bidang.
7. Nama audit/event `organization.create|update|archive` dan `organization.created|updated|archived`; `Actor.OrganizationID` = organisasi sasaran (BELUM dikonfirmasi).
8. `Caller`: `UserID` wajib, `ScopeContext` nol = platform.
9. Enam usulan desain partial-1 tetap default BELUM dikonfirmasi.
10. Daftar sentinel baru yang WAJIB didaftarkan di pemetaan galat HTTP (T1a-4).

**Doc-sync tertunda baru (partial-2):** docs/08 §10.6 harus mendapat blok "As built (M05, ADR-0023)" (path `{id}` melawan scope, status terminal, filter daftar, kode galat baru, tenant melihat satu organisasi); docs/02 §6 (siklus status terminal, aturan slug); docs/00 butir E:I-10 belum dibaca. Nomor ADR yang dirujuk kode: tidak ada kode yang merujuk ADR-0023 (komentar kode merujuk hanya butir 54 ADR-0022).

**Keputusan baru partial-3 (ADR-0023 butir 11-14; semuanya BELUM dikonfirmasi; dipakai sebagai default karena pemilik tidak membantah):**
11. `Add` anggota HANYA platform dan hanya untuk user aktif (status awal `active`); tenant memakai undangan (T4). `Remove` = logis (`removed`; baris dan user tetap); reaktivasi memakai baris yang sama; `ErrMemberExists` untuk anggota terbuka; `ErrUserNotEligible` satu galat untuk user tidak ada/tidak aktif/dihapus; tenant tidak boleh menghapus dirinya sendiri (service T1a-3b); pelindung admin terakhir BELUM ada.
12. Daftar anggota: cursor buram `(created_at, id)`, 50/100, filter `open|all|status`, tanpa email, `ErrOrganizationNotFound` untuk tenant yang menunjuk organisasi lain, `Get` mengembalikan juga baris `removed`.
13. Penghapusan anggota (T1a-3b): keanggotaan grup `removed` (sudah ada di repository), role assignment ber-scope organisasi/grup dicabut (metode baru di `authorization/repository` DIBUTUHKAN), `InvalidateInTx`; sesi TIDAK dicabut sampai T5 (keanggotaan hanya diperiksa saat login dan pindah organisasi, `auth/service/auth.go:316-337`); akses sudah tertutup lewat `Effective` (`effective.go`).
14. Audit `organization.member.add|remove` (imperatif, mengikuti M04 as-built; bentuk lampau contoh docs/13 TIDAK dipakai), event `organization.member.added|removed`; sentinel baru `ErrMemberNotFound`, `ErrMemberExists`, `ErrUserNotEligible`, `ErrInvalidCursor` WAJIB didaftarkan di pemetaan galat HTTP (T1a-4).

**Doc-sync tertunda baru (partial-3):** docs/08 §10.6 blok "As built (M05, ADR-0023)" harus memuat juga anggota (POST platform-saja, DELETE logis, daftar cursor tanpa email, kode galat); docs/02 §9 (status `removed` dan reaktivasi baris yang sama); docs/13 (aksi audit as-built imperatif vs contoh bentuk lampau); nomor butir ADR yang dirujuk kode T1a-3a: ADR-0023 butir 11-14 (ditulis di patch dokumen yang sama).

**Temuan fakta (diperiksa dari kode, partial-3):** `Effective` mensyaratkan `organization_memberships.status = 'active'` untuk scope organisasi dan grup; keanggotaan hanya diperiksa saat login/pindah organisasi; tidak ada RLS pada skema (hak `app_user` = grant biasa, `033_grants.up.sql`); aksi audit M04 imperatif (`role.create`, `role_assignment.revoke`).

## 5. Delta kontrak (sesi ini)
Tidak ada perubahan API publik, swagger, atau skema/migrasi (migrasi terakhir tetap 035). Kode baru: `internal/organization/domain/{member.go,member_test.go}` dan `internal/organization/repository/{members.go,members_test.go}` (4 berkas; tidak ada yang lama diubah). Katalog core tetap 41 kode. Tidak ada rute baru.

## 6. Peta kode (konvensi dari partial-1 + kode M05 baru di akhir)
- **Ada kini (partial-2):** paket `organization` (domain, repository, application); belum ada `identity`, `group`, `membership`, repository/service anggota organisasi, `RequireTenant`, handler HTTP. Worker (`cmd/worker` → `app.RunWorker`): relay outbox ada di `internal/kernel/relay.go` (apakah `RunWorker` sudah memasangnya: BELUM diperiksa).
- **Tabel:** `organizations` (status `active|suspended|archived`, `uq_organizations_slug`, `deleted_at`), `organization_memberships` (status `invited|active|suspended|removed`, `uq_org_memberships (organization_id, user_id)`), `users.email` unik global (`uq_users_email`), `uq_invitations_pending` sudah memuat `organization_id`.
- **Konvensi repository** (pola `authorization/repository`): pgx langsung di atas `kernel.DBTX`, struktur `type X struct{ db kernel.DBTX }` + `NewX(db)`; konstanta SQL; galat dibungkus `fmt.Errorf("<paket>: ...: %w", err)`; kunci advisory via `LockOrganization(ctx, orgID)` dan `LockPlatform(ctx)` (di dalam transaksi; `ErrNoTransaction` di luar).
- **Pola penulis** (`authorization/application/assignments.go`): `tx TxRunner` + `store` + `audit modulesdk.AuditRecorder` + `events EventPublisher` + `inv TxInvalidator`; per operasi satu `tx.WithinTx`: kunci → aturan → authority ceiling dari PostgreSQL → tulis → invalidasi cache → audit → event; penolakan kebijakan diaudit `denied` di transaksi terpisah; pencabutan sesi sesudah commit (`SessionRevoker`).
- **Perakitan rute:** `httpserver.AuthzMounter`, `MountAuthzRoutes` (prefiks `/api/v1` ditambahkan sekali; hanya `With`, `Get/Post/Put/Patch/Delete`), urutan `Authenticate → CSRF → RequireAuth → Scope → Authorize(action) → [TenantIdempotency.Require()]`.
- **Fixture testkit:** `NewOrganization`, `NewUser(InOrganization(o))`, `AddOrganizationMember`, `NewGroup`, `AddGroupMember(g, u, status)`; baris ditulis lewat `db.Migrator`, kode aplikasi diuji lewat `db.App`.
- **Katalog core terkait:** `organization.read|create|update|archive|member.manage`, `user.read|create|update|delete`, `group.read|create|update|delete|manage_members`.
- **ADR-0022 yang dibaca untuk T1a:** butir 12–15, 25, 27 (tiket masuk organisasi: `organization.read` level platform; engine ditanya sebelum `OrganizationIsActive`), 28, 54, 62, 67, 73, 76, 77. Butir ADR-0020/0021 tidak dibaca sesi ini.

**Kode M05 baru (partial-2), `backend/internal/organization/`:**
- `domain/organization.go`: konstanta status dan batas; sentinel (ADR-0023 butir 10); `Access` (`PlatformAccess()`, `TenantAccess(id)`, `Valid`, `IsPlatform`, `Tenant`, `Filter() (*uuid.UUID, error)`; nilai nol = tidak sah); `Record` (ID, Name, Slug, LegalName *string, Status, DefaultLocale, DefaultTimezone, CreatedAt, UpdatedAt, ArchivedAt = `deleted_at`); `CreateInput`, `UpdateInput` (pointer; `Normalize()` mengembalikan salinan); `NewRecord(id, CreateInput)`, `Record.Validate()`; `ListQuery`, `Page`, `Window`, `ClampWindow` (50/100); `StatusesForFilter`, `ValidStatus`, `PredecessorStatuses`; `NormalizeName`, `NormalizeSlug`. `domain/tzdata.go`: impor kosong `time/tzdata` di berkas sendiri (goimports).
- `repository/organizations.go`: `Organizations` (`NewOrganizations(db kernel.DBTX)`): `Get`, `ForUpdate` (wajib transaksi; `ErrNoTransaction`), `List`, `Insert` (platform saja), `Update`, `SetStatus`; semua menerima `domain.Access`; pemetaan 23505 pada `uq_organizations_slug` → `ErrSlugTaken`; `diagnose` membedakan NotFound / archived / transisi.
- `application/organizations.go`: antarmuka `TxRunner`, `EventPublisher`, `TxInvalidator` (nil = tanpa Redis), `Locker` (+ adaptor `LockFunc`, dipakai dengan `authorization/repository.LockOrganization`), `Store`; `Caller{UserID, Scope authzdomain.ScopeContext}`; `NewService(tx, store, locker, audit, events, inv)`; `Service.Get/List/Create/Update/Archive`. Belum dirakit di `internal/app` (T1a-4).
- Tes: `domain/organization_test.go` (18), `repository/organizations_test.go` (19, ber-DB) + `main_test.go`, `application/organizations_test.go` (20 unit dengan fake yang berbagi satu `trace` urutan panggilan), `application/organizations_db_test.go` (6 ber-DB), `application/fakes_test.go`, `application/main_test.go`.
- Pola uji yang dipakai ulang: tes tiga lapis untuk kewajiban konkurensi (urutan panggilan, penahanan kunci ber-DB dengan jendela 300 ms, dua operasi serentak tepat satu lolos, rollback bila invalidasi gagal); `require.FailNow` tidak dipanggil dari goroutine (kanal dan `select` dengan `time.After`).

**Kode M05 baru (partial-3, T1a-3a), `backend/internal/organization/`:**
- `domain/member.go`: konstanta `MemberInvited|MemberActive|MemberSuspended|MemberRemoved`, `MaxCursorLength`; sentinel `ErrMemberNotFound`, `ErrMemberExists`, `ErrUserNotEligible`, `ErrInvalidCursor`; `Member` (ID, OrganizationID, UserID, Status, JoinedAt, InvitedBy, CreatedAt, UpdatedAt, DisplayName, UserStatus; `NextCursor()`); `MemberListQuery`, `MemberPage`; `ValidMemberStatus`, `MemberStatusesForFilter`, `ClampMemberLimit`; `MemberCursor` (`Encode`) dan `DecodeMemberCursor`. Memakai ulang `FilterOpen`, `FilterAll`, `DefaultPageSize`, `MaxPageSize`, `ErrInvalidFilter`, `ErrInvalidID`, `ErrInvalidAccess`, `ErrPlatformRequired`, `ErrOrganizationNotFound` dari `organization.go`.
- `repository/members.go`: `OrganizationMembers` (`NewOrganizationMembers(db kernel.DBTX)`; memakai `kernel.DBFrom(ctx, r.db)` sehingga ikut transaksi di context): `Get(ctx, a, orgID, userID)`, `List(ctx, a, orgID, q)`, `Add(ctx, a, orgID, userID, addedBy *uuid.UUID)` (platform saja; CTE `INSERT ... ON CONFLICT ON CONSTRAINT uq_org_memberships DO UPDATE ... WHERE m.status = 'removed'`; `diagnoseAdd` membedakan `ErrMemberExists` dan `ErrUserNotEligible`; FK `organization_memberships_organization_id_fkey` -> `ErrOrganizationNotFound`), `Remove(ctx, a, orgID, userID)`, `RemoveFromGroups(ctx, a, orgID, userID) (int, error)`; pembantu `scopeFor` (validasi Access dan id; tenant yang menunjuk organisasi lain ditolak di Go lalu dipagari lagi di SQL).
- Tes: `domain/member_test.go` (10), `repository/members_test.go` (23, ber-DB; helper `memberRepo`, `platformAdd`, `setMembershipStatus`, `setUserStatus`, `softDeleteUser`, `membershipRows`, `membershipStatus`, `userRowStatus`, `groupStatus`, `isAuthzMember`, `memberUserIDs`). Semua nama tes anggota memakai awalan entitas bila bisa bentrok (`TestMemberGet...`).
- Belum dirakit di `internal/app` (T1a-4).

## 7. Cara menjalankan & memverifikasi
- Gerbang kode (laptop, `~/projects/zago-core-m03`): `gofmt -l backend` (kosong), `go vet <paket>`, `go tool golangci-lint run <paket>` (`0 issues.`), `REQUIRE_DOCKER=1 go test <paket> -race -count=1 -p 1`.
- **Smoke tanpa membangun ulang tiap kali** (build ±10 menit di laptop ini): naikkan SATU stack manual, jalankan `make smoke` berulang, bongkar sebelum `make verify`:
  `COMPOSE_PROJECT_NAME=platform-m05 docker compose --profile full up -d --build && COMPOSE_PROJECT_NAME=platform-m05 bash ./scripts/wait-for-healthy.sh`, lalu `COMPOSE_PROJECT_NAME=platform-m05 SMOKE_CURL_MAX_TIME=90 make smoke > ~/smoke.log 2>&1; echo "exit=$?"`, bongkar: `COMPOSE_PROJECT_NAME=platform-m05 docker compose --profile full down -v --remove-orphans`. Bila `migrate` gagal `unexpected EOF`: ulangi `up` tanpa `--build` dan tanpa `down -v`. Bila `build` gagal `TLS handshake timeout`: ulangi, lalu `curl -sS -m 15 -o /dev/null -w 'http=%{http_code} t=%{time_total}s\n' https://auth.docker.io/token`.
- **Gerbang paket organisasi (partial-2):** `gofmt -l backend; go vet ./backend/internal/organization/... ; echo "vet-exit=$?"`, `go tool golangci-lint run ./backend/internal/organization/...`, `REQUIRE_DOCKER=1 go test ./backend/internal/organization/... -race -count=1 -p 1 2>&1 | tail -30` (repository ±70–100 dtk dan application ±64 dtk wajar: satu database per tes). `gofmt -l` kosong TIDAK membuktikan `goimports`; hanya `golangci-lint` (goimports) yang menangkapnya.

- **Partial-3:** jalur smoke yang terbukti pada sesi ini = stack manual: `COMPOSE_PROJECT_NAME=platform-m05 docker compose --profile full up -d --build`, lalu (bila `migrate` gagal unduh) `up -d` tanpa `--build` dan tanpa `down -v`, `wait-for-healthy.sh`, `SMOKE_CURL_MAX_TIME=90 make smoke`, bongkar `down -v --remove-orphans`. Pelipatan commit sebelum push: catat `git rev-parse HEAD^{tree}` SEBELUM `git reset --soft HEAD~1 && git commit --amend --no-edit` dan bandingkan sesudahnya (sesi ini langkah "sebelum" tidak ditempel).

## 8. Masalah diketahui / utang teknis
**Seluruh §8 HANDOVER-M04 tetap berlaku** (OpenAPI nama panjang, smoke fixture psql, scope `resource`/`own`, FieldGuard tanpa pemakai produksi, sesi tanpa outbox, retry start, `simulate`, policy, penulis, cache, audit indeks unik pola 035 BELUM dilakukan, utang lintas milestone M05/M07/M15, lingkungan laptop).
**Baru:**
- **Login > 8–20 dtk pada alur dingin `verify-smoke.sh` (tiga run berturut-turut, dua skrip berbeda): PENYEBAB TIDAK TERBUKTI.** Yang terukur: login tenang 0,22–0,37 dtk (login pertama 3,55 dtk); I/O VM Docker lambat (checkpoint 240–280 buffer butuh 44–58 dtk; `fsync` direktori data sampai 30 dtk); satu crash-restart postgres terpicu "untracked child process exited with exit code 2" (hipotesis: proses luar, mis. `pg_isready` healthcheck yang keluar 2 lalu yatim ke PID 1, ATAU proses `psql` dari skrip; baris `exit code 0` hanya tiga kali per menit sehingga TIDAK cocok dengan healthcheck 5 dtk, jadi tidak diklaim); Redis dan Argon2 (64 MiB, t=3, p=2) belum disingkirkan. Langkah berikutnya bila terulang: baca `exit=` dan durasi dari pesan baru, lalu ukur login selama pemuatan I/O.
- **Cacat compose yang MUNGKIN:** bila crash-restart postgres terbukti dipicu proses luar yang keluar tidak nol, itu cacat `docker-compose.yml` (laporkan; perbaiki di commit tersendiri). Belum terbukti.
- Log API hanya mencatat request SETELAH selesai; request yang masih menggantung saat log diambil tidak terlihat (observabilitas, M15).
- `verify-smoke.sh` masih `down -v` volume cache modul tiap run (§9(f) separuh terjawab).
- Pesan `curl gagal: …` mencetak argumen curl, termasuk password acak smoke (sandi smoke hanya untuk stack lokal; pertimbangkan penyamaran).
- Dugaan proses yang SALAH di sesi ini (dicatat supaya tidak diulang): "batas 8 dtk saja yang sempit" (20 dtk tetap gagal), "api/worker restart" (ternyata `migrate` lama menunda mulai).
**Baru (partial-2):**
- `make verify` utuh pada kode M05 BELUM dijalankan (hanya tes tiga paket organisasi di laptop).
- Anggota organisasi (`organization_memberships`: repository, service, tes lintas-organisasi dua arah, aturan status `invited|active|suspended|removed`) belum ada; ini yang paling mendesak sebelum handler.
- `Archive` tidak mencabut sesi (cakupan diputuskan di T5 setelah skema sesi dibaca); sementara akses organisasi terarsip tertutup lewat `Effective` (status dan `deleted_at`) dan invalidasi cache di dalam transaksi.
- `Window`/`ClampWindow` terduplikasi (organisasi 50/100; authorization 25): gabungkan saat T1b/T1c.
- `time/tzdata` menambah ±450 KB pada biner; alternatif bila dianggap berat: validasi pola saja.
- `Caller` dengan `ScopeContext` nol dianggap platform; keselamatannya bergantung pada middleware `Scope` (semantik M04).
- Tanpa slug cadangan (`admin`, `api`, `platform`, ...): putuskan bila slug dipakai di URL/subdomain.
- Audit indeks unik lain terhadap pola bug 035 masih BELUM dilakukan (tes keunikan organisasi memakai dua organisasi dan lolos).

**Baru (partial-3):**
- `make verify` utuh pada kode M05 = `exit=2` (paket `authorization/cache` merah: pesan kelas reaper/Docker, hijau sendirian dua kali; SEBAB TIDAK terbukti). Tiga tahap sesudah test (migrate-roundtrip, build, smoke) hanya terbukti terpisah (`exit=0`, `exit=0`, dan smoke di stack manual `exit=0`), bukan dalam satu run `make verify` hijau.
- Smoke alur dingin gagal DUA kali dengan sebab berbeda, keduanya unduhan (npm `ECONNRESET`; modul Go `unexpected EOF`). `verify-smoke.sh` menghapus volume `go_mod_cache` tiap run (`down -v` di trap), sehingga alur dingin rawan; belum diubah (usulan: pertahankan volume cache modul atau beri retry unduhan; keputusan pemilik, §9).
- `Remove` anggota belum mencabut role assignment (butuh metode baru di `authorization/repository`; T1a-3b) dan tidak mencabut sesi (T5); akses sudah tertutup lewat `Effective`.
- Pelindung "admin terakhir organisasi" dan larangan hapus-diri di tenant belum ada (T1a-3b untuk yang kedua).
- `RemoveFromGroups` menulis `group_memberships` dari paket organisasi (paket group belum ada); pindahkan saat T1c.
- Audit indeks unik lain terhadap pola bug 035 masih BELUM dilakukan (tes keunikan anggota memakai dua organisasi dan lolos).
- Nama tes ganda di satu paket menggagalkan kompilasi paket tes (`redeclared in this block`): terjadi sekali sesi ini (§3).

## 9. Pertanyaan terbuka
(a)–(g) di HANDOVER-M04 §9 tetap berlaku ((f) dijawab sebagian oleh keputusan 1 di §4).
- (h) Enam usulan desain M05 di §4 (terutama butir 1 path `{id}`, butir 2 accept-invitation akun yang sudah ada, butir 5 cakupan pencabutan sesi organisasi): bantah sebelum T1a membangun di atasnya.
- (i) Perlukah menyamarkan password di pesan `curl gagal`?
- (j) Apakah smoke `verify-smoke.sh` harus tetap memakai alur dingin penuh (build + `down -v`) atau cukup stack yang dipertahankan?
- (k) Archived terminal (tidak ada pemulihan): setuju? (ADR-0023 butir 2)
- (l) `GET /organizations` oleh pemanggil tenant hanya mengembalikan organisasi di scope-nya (satu item); daftar keanggotaan lintas organisasi di `/me`? (butir 4)
- (m) Nama audit/event `organization.*` dan `Actor.OrganizationID` = organisasi sasaran? (butir 7)
- (n) `Archive` mengambil kunci advisory walau tidak mengubah role/assignment/policy (diserialkan dengan penulis lain); setuju? (butir 5)
- (o) Slug cadangan dan keabadian slug (tak dapat diubah lewat PATCH)? (butir 3)
- (p) Pisahkan anggota organisasi menjadi T1a-3 dan handler menjadi T1a-4 (usulan; §10)?

- (q) ADR-0023 butir 11a: `Add` anggota hanya oleh platform (tenant memakai undangan)? Alternatif: tenant boleh menambah user yang sudah ia kenal dengan persetujuan user (undangan).
- (r) Butir 11d: tenant tidak boleh menghapus keanggotaannya sendiri; perlu pelindung admin terakhir organisasi (cara menghitungnya: pemegang `organization.member.manage`?).
- (s) Butir 13: pencabutan sesi saat anggota dihapus cukup per (user, organisasi) di T5, atau sekarang?
- (t) `verify-smoke.sh`: pertahankan volume cache modul antar run (mengurangi kegagalan unduhan) atau tetap `down -v` penuh?

## 10. Milestone berikutnya: lanjutan M05 (T1a-3b, T1a-4)
- **T1a-3b service anggota** (`application/members.go`, paket yang sama dengan `Service` organisasi atau `MemberService` terpisah; putuskan setelah membaca `organizations.go`): `Add` (platform saja; kunci advisory `LockOrganization` PERTAMA; organisasi aktif; `Members.Add`; `InvalidateInTx`; audit `organization.member.add`; event `organization.member.added`), `Remove` (tolak tenant lain SEBELUM transaksi; kunci pertama; tolak hapus-diri untuk tenant (`ErrCannotRemoveSelf`, alokasikan di ADR butir 14); `Members.Remove`; `RemoveFromGroups`; pencabutan assignment ber-scope organisasi/grup user itu lewat metode BARU di `authorization/repository` (baca `assignments.go`, SQL pencabutan, dan aturan butir 54; mencabut tidak butuh ceiling); `InvalidateInTx`; audit `organization.member.remove`; event `organization.member.removed`), `List`/`Get` (baca). Tes: unit dengan fake berbagi satu `trace` (urutan: kunci dulu), penahanan kunci ber-DB (jendela 300 ms), dua operasi serentak tepat satu lolos, rollback bila langkah akhir gagal; tenant lain ditolak sebelum transaksi (kunci tidak diambil); organisasi `archived` ditolak. Pelajaran: baca `application/organizations.go` dan `fakes_test.go` lebih dulu; periksa dengan `grep -h '^func Test' | uniq -d` SEBELUM mengirim.
- **T1a-4 (usulan tiga patch):** (a) `RequireTenant` (ditunda dari M04), DTO, `DomainError`/`codeRules` untuk SEMUA sentinel ADR-0023 butir 10 dan 14; (b) handler `/organizations` dan `/organizations/{id}/members` dengan pola handler M03/M04 (`h.rs.Error`, `DecodeJSON`, `chi.URLParam`, anotasi swag; jalur RELATIF; `KernelServices.TenantIdempotency` untuk rute ber-`Idempotency-Key`; path `{id}` melawan ScopeContext), tes handler dengan Authorize SUNGGUHAN; (c) perakit rute di `internal/app` (`application.NewService` dengan `authorization/repository.LockOrganization` lewat `LockFunc`, `kernel.TxManager`, `kernel.Bus`, `cache` sebagai `TxInvalidator` atau nil), `make generate`, commit `chore(generate)`, `bash ./scripts/generate-check.sh`.
- Sesudahnya: T1b, T1c, ◆CP, lalu T2-T6 (lihat §2).
Sebelum menulis: baca `authz_routes.go` dan `authzStack.httpAPI`, satu handler M04 dan tesnya, `domain.ScopeContext`. Alokasikan nomor butir ADR (butir 15 dst.) SEBELUM menulis komentar kode yang merujuknya.

## 11. Prompt untuk chat berikutnya
`docs/handover/PROMPT-M05-partial-3.md` (disimpan di repo; dirakit dari `PROMPT-M05-partial-2.md` dengan skrip beranchor). Commit teratas yang diharapkan: commit `docs(m05): HANDOVER-M05-partial-3 ...` di atas `df8af41` (T1a-3a). Jumlah berkas yang diharapkan: 602 (600 + `HANDOVER-M05-partial-3.md` + `PROMPT-M05-partial-3.md`; bila berbeda, tanyakan sebabnya). Keadaan `make verify`: run utuh pada kode M05 = `exit=2` (paket `authorization/cache` merah kelas Docker, hijau sendirian dua kali; smoke alur dingin gagal dua kali karena unduhan; smoke stack manual `exit=0`); gerbang kode T1a-3a hijau di laptop.
