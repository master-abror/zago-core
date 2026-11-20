# HANDOVER — M05 Organization, User, Group, Invitation, Email (PARSIAL 2)

**Tanggal:** 2026-11-20 · **Branch:** `milestone/m05-organization` (dari tag `m04-done` = `6a14f87`; ada di origin) · **Sesi:** partial-2 (T1a-1 dan T1a-2 selesai; checkpoint SEBELUM T1a-3)
**Prompt sesi ini:** `docs/handover/PROMPT-M05-partial-1.md`. **Prompt sesi berikutnya:** `docs/handover/PROMPT-M05-partial-2.md`.

## 1. Ringkasan
Sesi ini menulis kode M05 pertama: paket `internal/organization` (domain, repository, application) dalam tiga patch (T1a-1 domain+repository, perbaikan goimports, T1a-2 service). Semua gerbang di laptop HIJAU pada percobaan kedua untuk T1a-1 (satu temuan `goimports`, diperbaiki) dan pada percobaan pertama untuk T1a-2 (§3). Keputusan desain ditulis ke `docs/adr/0023-m05-organization-user-group-design.md` (butir 1–10; yang BELUM dikonfirmasi bertanda jelas). Baseline sebelum kode M05: `make verify` utuh pada kode M04 = `exit=0` (hijau, termasuk smoke m00–m04 pada alur dingin; hanya hasil saringan yang ditempel), berbeda dengan partial-1.

**Belum dikerjakan dari rencana T1a:** repository/service/handler ANGGOTA organisasi (`organization_memberships`) belum ada (rencana partial-1 memasukkannya ke T1a-1; sesi ini hanya membangun `Organizations`), `RequireTenant`, DTO, handler HTTP, `DomainError`, perakitan rute, dan `make generate`. `make verify` utuh pada kode M05 BELUM dijalankan. Checkpoint dipicu karena tanda-tanda konteks sudah ada (puluhan giliran, banyak keluaran ditempel, tiga patch kode, banyak berkas dibaca); tidak ada kode setengah jadi.

## 2. Status task
| Task | Isi | Status |
|---|---|---|
| T0 | Baseline + patch smoke (`3977aa6`, `4ff53dd`) | SELESAI (partial-1); baseline run utuh sesi ini `exit=0` |
| T1a-1 | domain `organization` + repository `Organizations` + 37 tes (18 domain, 19 repository ber-DB; lintas-organisasi DUA ARAH) | SELESAI, hijau di laptop (commit `246032c` + perbaikan `2b0f474`, sudah di origin) |
| T1a-2 | service `Create/Get/List/Update/Archive` + 26 tes (20 unit, 6 ber-DB: urutan kunci, penahanan kunci, serentak, rollback) | SELESAI, hijau di laptop (commit `7039796`; BELUM di-push saat checkpoint dibuat: lihat §3) |
| T1a-3 | repository/service/tes ANGGOTA organisasi (`organization_memberships`), lintas-organisasi dua arah | BELUM (usulan baru; §10) |
| T1a-4 | `RequireTenant`, DTO, handler `/organizations` (+ anggota), `DomainError`, perakit rute, `make generate` | BELUM (§10) |
| T1b | `identity`: users dan `profile`, kode `<tipe>.field.*`, pemakai pertama `FieldHelper` | BELUM |
| T1c | `group` dan `membership`: siklus/kedalaman 8, `activate`, pencabutan assignment ber-scope grup | BELUM |
| ◆CP | handover parsial | ◆CP ini = partial-2 |
| T2 | transaksi docs/03 §46 (create user, create group) dengan kunci advisory dan `InvalidateInTx` | BELUM |
| T3 | worker: konsumen event dan layanan email | BELUM |
| T4a/T4b | undangan; forgot/reset password; pembuka kunci akun administratif | BELUM |
| T5 | suspend/deactivate user dan organisasi, cabut sesi sesudah commit | BELUM |
| T6 | `scripts/smoke/m05.sh`, doc-sync, ritual akhir | BELUM |

## 3. Bukti verifikasi (apa adanya)
Semua keluaran di bawah ditempel pemilik pada sesi ini, kecuali yang ditandai "tidak ditempel". Bukti partial-1 ada di `HANDOVER-M05-partial-1.md` §3.
- **Keadaan repo awal:** `milestone/m05-organization...origin/milestone/m05-organization`, `02aa134`, tag `m00-done`..`m04-done`, `git rev-parse --short m04-done` = `6a14f87`, `git ls-remote --tags origin m04-done` = `6a14f87276ed64ba15af7673beaaebc4725ca79c`, `git fsck` tanpa keluaran, `git ls-files | wc -l` = 582, `HANDOVER-M05-partial-1.md` 18116 byte, `PROMPT-M05-partial-1.md` 51963 byte.
- **Insiden branch:** setelah patch T1a-1 dikirim, pemilik sempat berada di `design/ui-prototype` (= `6a14f87`, sama dengan `main` dan `m04-done`; working tree bersih). Dipulihkan dengan `git switch milestone/m05-organization` sebelum `git am` (keluaran `git switch` dan `git status` setelahnya tidak ditempel; `git log` sesudah `git am` menunjukkan commit di atas `02aa134` pada branch itu). Pelajaran: minta `git branch --show-current` sebelum `git am`.
- **Baseline (a)(a2)(a3):** tidak ada container `platform*`/`zago*` (`grep-exit=1`), `down` platform-m05 tanpa keluaran; `go1.26.5`, `gofmt-exit=0`; `docker image ls postgres` memuat `postgres:18`; `go vet` (app, authorization) `vet-exit=0`; `golangci-lint` `0 issues.`.
- **Run utuh sebelum kode M05** (`gofmt -l backend; make verify GO_TEST_PARALLEL=1 > ~/verify.log`): **`exit=0`**. Yang ditempel hanya saringan `grep`: baris smoke `M00 smoke OK`, `M02 smoke OK`, `M03 smoke OK`, `M04 smoke OK`; tidak ada baris `FAIL`/`✗`. Log lengkap tidak ditempel.
- **T1a-1:** `git am` mbox pertama: commit `246032c` (`sha256sum` mbox: tidak ditempel); `git ls-files | wc -l` = 587; `grep -c 'func NewOrganizations'` = 1. Gerbang pertama: `gofmt -l backend` kosong, `vet-exit=0`, **`golangci-lint`: 1 temuan `goimports` (organization.go:10, blok impor dengan komentar di sekitar impor kosong `time/tzdata`)**. Perbaikan (commit kedua `2b0f474`, mbox `m05-t1a1-fix-goimports.mbox`; `sha256sum`: tidak ditempel; `ls tzdata.go` ada; 588 berkas): `gofmt` kosong, `vet-exit=0`, `0 issues.`, `REQUIRE_DOCKER=1 go test ./backend/internal/organization/... -race -count=1 -p 1`: `ok .../domain 1.819s`, `ok .../repository 99.411s`. Origin = `2b0f474` (terlihat di dekorasi `git log`; keluaran `git push` T1a-1 tidak ditempel; `git push` berikutnya: `Everything up-to-date`).
- **T1a-2:** `sha256sum` mbox `0f14998890bd700c` (cocok dengan sandbox); `git am` terpasang (`7039796`); `git ls-files | wc -l` = 593; `grep -c 'func (s \*Service) Archive'` = 1. Gerbang: `gofmt -l backend` kosong, `vet-exit=0`, `0 issues.`, tes: `ok .../application 64.301s`, `ok .../domain 1.082s`, `ok .../repository 70.768s`. **`git push` T1a-2: BELUM dijalankan saat checkpoint ini dibuat** (pemilik mem-push sesudah patch dokumen diterapkan).
- **Hitungan tes (dari berkas, bukan ingatan):** domain 18; repository 19; application 20 unit + 6 ber-DB. Klaim awal sesi (19 dan 5 untuk application) salah dan dikoreksi sebelum dikirim.
- **Dibuktikan di sandbox (bukan di mesin pemilik):** `git am` tumpukan semua mbox berurutan pada salinan bersih zip memberi pohon identik; `scripts/lint-structure.sh` OK; pemeriksa mekanis penjajaran literal ber-kunci 0 salah. Go/gofmt tidak tersedia di sandbox: kompilasi, gofmt, vet, lint, dan tes HANYA terbukti lewat keluaran laptop di atas.
- **Tidak dijalankan/tidak ditempel:** `make verify` utuh pada kode M05; `make generate`/`generate-check` (belum ada anotasi swag baru); smoke terhadap stack hidup pada kode M05; keluaran `git push` T1a-1 dan `sha256sum` dua mbox pertama.
- **Keadaan laptop saat checkpoint:** tidak ada stack compose `platform-*` yang diketahui hidup (partial-1 mencatat `platform-m05` hidup; sesi ini `down -v` dijalankan tanpa keluaran dan `docker ps | grep` kosong).

## 4. Keputusan & penyimpangan (KUMULATIF; isi partial-1 di bawah, keputusan baru partial-2 di akhir)
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

## 5. Delta kontrak (sesi ini)
Tidak ada perubahan API publik, swagger, atau skema/migrasi (migrasi terakhir tetap 035; tes ber-DB di laptop membuktikan `organizations` cukup untuk T1a-1/T1a-2 tanpa migrasi 036). Kode baru: paket `internal/organization/{domain,repository,application}` (14 berkas; tidak ada yang lama diubah). `time/tzdata` ditanam di biner lewat `domain/tzdata.go` (±450 KB). Katalog core tetap 41 kode. Tidak ada rute baru (handler belum ada).

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

## 7. Cara menjalankan & memverifikasi
- Gerbang kode (laptop, `~/projects/zago-core-m03`): `gofmt -l backend` (kosong), `go vet <paket>`, `go tool golangci-lint run <paket>` (`0 issues.`), `REQUIRE_DOCKER=1 go test <paket> -race -count=1 -p 1`.
- **Smoke tanpa membangun ulang tiap kali** (build ±10 menit di laptop ini): naikkan SATU stack manual, jalankan `make smoke` berulang, bongkar sebelum `make verify`:
  `COMPOSE_PROJECT_NAME=platform-m05 docker compose --profile full up -d --build && COMPOSE_PROJECT_NAME=platform-m05 bash ./scripts/wait-for-healthy.sh`, lalu `COMPOSE_PROJECT_NAME=platform-m05 SMOKE_CURL_MAX_TIME=90 make smoke > ~/smoke.log 2>&1; echo "exit=$?"`, bongkar: `COMPOSE_PROJECT_NAME=platform-m05 docker compose --profile full down -v --remove-orphans`. Bila `migrate` gagal `unexpected EOF`: ulangi `up` tanpa `--build` dan tanpa `down -v`. Bila `build` gagal `TLS handshake timeout`: ulangi, lalu `curl -sS -m 15 -o /dev/null -w 'http=%{http_code} t=%{time_total}s\n' https://auth.docker.io/token`.
- **Gerbang paket organisasi (partial-2):** `gofmt -l backend; go vet ./backend/internal/organization/... ; echo "vet-exit=$?"`, `go tool golangci-lint run ./backend/internal/organization/...`, `REQUIRE_DOCKER=1 go test ./backend/internal/organization/... -race -count=1 -p 1 2>&1 | tail -30` (repository ±70–100 dtk dan application ±64 dtk wajar: satu database per tes). `gofmt -l` kosong TIDAK membuktikan `goimports`; hanya `golangci-lint` (goimports) yang menangkapnya.

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

## 10. Milestone berikutnya: lanjutan M05 (T1a-3, T1a-4)
Usulan pemecahan (BELUM dikonfirmasi): rencana partial-1 memasukkan anggota organisasi ke T1a-1, tetapi sesi ini hanya membangun `Organizations`; karena itu:
- **T1a-3 anggota organisasi:** domain (`Member`, status `invited|active|suspended|removed`, `uq_org_memberships (organization_id, user_id)`), repository `OrganizationMembers` (baca daftar dengan cursor, tambah, ubah status, hapus logis; SEMUA kueri memfilter `organization_id`) + tes ber-DB lintas-organisasi DUA ARAH untuk SETIAP fungsi, keunikan memakai DUA organisasi, pesan galat identik antara "milik organisasi lain" dan "tidak ada"; service (kunci advisory pertama, `InvalidateInTx`, audit, event; pembatalan sesi sesudah commit hanya di T5) + tes urutan panggilan dan penahanan kunci. Baca dulu docs/02 §8–§11, docs/08 §10.6–§10.9, skema migrasi 005 dan 007, dan SQL `organizationMemberSQL` di authorization (definisi "anggota aktif" tidak boleh bercabang).
- **T1a-4:** `RequireTenant` (ditunda dari M04 ke handler tenant pertama), DTO, handler `/organizations` (+ anggota) dengan pola handler M03/M04 (`h.rs.Error`, `DecodeJSON`, `chi.URLParam`, anotasi swag; jalur RELATIF yang diberi prefiks `/api/v1` oleh perakit), `DomainError`/`codeRules` untuk SEMUA sentinel ADR-0023 butir 10, perakit rute di `internal/app` (konstruktor `application.NewService` dengan `authorization/repository.LockOrganization` lewat `LockFunc`, `kernel.TxManager`, `kernel.Bus`, `cache` sebagai `TxInvalidator` atau nil tanpa Redis), `KernelServices.TenantIdempotency` untuk rute ber-`Idempotency-Key`, tes handler dengan Authorize SUNGGUHAN, `make generate` dan commit `chore(generate)`, `bash ./scripts/generate-check.sh`.
- Setelah T1a-4: T1b, T1c, ◆CP, lalu T2–T6 (lihat §2).
Sebelum menulis: baca `authz_routes.go` dan perakit `authzStack.httpAPI` (kontrak jalur relatif), satu handler M04 dan tesnya, `domain.ScopeContext`, dan `grep -n 'func' internal/authorization/application/assignments.go` untuk pola penulis. Alokasikan nomor butir ADR (ADR-0023 butir 11 dst.) SEBELUM menulis komentar kode yang merujuknya.

## 11. Prompt untuk chat berikutnya
`docs/handover/PROMPT-M05-partial-2.md` (disimpan di repo; dirakit dari `PROMPT-M05-partial-1.md` dengan skrip beranchor). Commit teratas yang diharapkan: commit `docs(m05): HANDOVER-M05-partial-2 …` di atas `7039796` (T1a-2). Jumlah berkas yang diharapkan: 596 (593 + `HANDOVER-M05-partial-2.md` + `PROMPT-M05-partial-2.md` + `docs/adr/0023-m05-organization-user-group-design.md`; bila berbeda, tanyakan sebabnya). Keadaan `make verify`: run utuh pada kode M04 = `exit=0` (partial-2); pada kode M05 belum dijalankan; tes tiga paket `internal/organization/*` hijau di laptop.
