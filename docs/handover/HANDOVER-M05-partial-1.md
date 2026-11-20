# HANDOVER — M05 Organization, User, Group, Invitation, Email (PARSIAL 1)

**Tanggal:** 2026-11-19 · **Branch:** `milestone/m05-organization` (dibuat dari tag `m04-done` = `6a14f87`, sudah di origin) · **Sesi:** partial-1 (checkpoint dini, sebelum satu baris kode M05 ditulis)
**Prompt sesi ini:** `docs/handover/PROMPT-M05.md`. **Prompt sesi berikutnya:** `docs/handover/PROMPT-M05-partial-1.md`.

## 1. Ringkasan
Sesi ini TIDAK menulis kode M05. Isinya: (1) pemeriksaan keadaan repo (cocok dengan zip: 580 berkas, `m04-done` ada di lokal dan origin, fsck bersih); (2) baseline sebelum M05; (3) dua patch kecil pada skrip smoke (di luar lingkup M05, sudah di-push); (4) pembacaan dokumen dan kode untuk T1a; (5) rencana task dan keputusan terbuka. Checkpoint dipicu dini karena tanda-tanda dalam KETENTUAN CHECKPOINT sudah ada (percakapan panjang, banyak log tertempel, banyak berkas dibaca) dan T1a-1 membutuhkan pembacaan kode tambahan yang besar tanpa kompiler di sandbox. Tidak ada kode setengah jadi.

Temuan terpenting: `make verify` utuh pada kode M04 yang sama dengan tag **TIDAK hijau di laptop pada sesi ini** (`exit=2`, berhenti di tahap smoke karena login lambat). Tahap tes dan semua tahap sebelum smoke lolos. Penyebab login lambat TIDAK terbukti (lihat §3 dan §8).

## 2. Status task
| Task | Isi | Status |
|---|---|---|
| T0 | Baseline + patch smoke (`3977aa6`, `4ff53dd`) | SELESAI dengan catatan (smoke utuh di alur dingin belum terbukti hijau; §3) |
| T1a | `organization`: domain, repository, service, handler `/organizations` dan `members`, `RequireTenant` | BELUM DIMULAI (desain dan konvensi dibaca; §6, §10) |
| T1b | `identity`: users dan `profile`, kode `<tipe>.field.*`, pemakai pertama `FieldHelper` | BELUM |
| T1c | `group` dan `membership`: siklus/kedalaman 8, `activate`, pencabutan assignment ber-scope grup | BELUM |
| ◆CP1 | handover parsial | (checkpoint ini adalah ◆CP0) |
| T2 | transaksi docs/03 §46 (create user, create group) dengan kunci advisory dan `InvalidateInTx` | BELUM |
| T3 | worker: konsumen event dan layanan email | BELUM |
| T4a/T4b | undangan; forgot/reset password; pembuka kunci akun administratif | BELUM |
| T5 | suspend/deactivate user dan organisasi, cabut sesi sesudah commit | BELUM |
| T6 | `scripts/smoke/m05.sh`, doc-sync, ritual akhir | BELUM |

## 3. Bukti verifikasi (apa adanya)
Semua keluaran di bawah ditempel pemilik pada sesi ini, kecuali yang ditandai "tidak ditempel".
- **Keadaan repo:** `main...origin/main`, `6a14f87 (tag: m04-done, origin/main)`, `git ls-remote --tags origin m04-done` = `6a14f87276ed64ba15af7673beaaebc4725ca79c`, `git fsck` tanpa keluaran, `git ls-files | wc -l` = 580, `HANDOVER-M04.md` 26436 byte, `PROMPT-M05.md` 47821 byte. Branch dibuat dan di-push (`* [new branch]`).
- **Baseline (a)(a2)(a3):** `go1.26.5`, `gofmt -l backend` kosong, `postgres:18` ada di `docker image ls`, `go vet` (app, authorization) `vet-exit=0`, `golangci-lint` `0 issues.`.
- **Run utuh #1** (`make verify GO_TEST_PARALLEL=1 > ~/verify.log`): **`exit=2`, TIDAK hijau.** Dari log: `grep -cE '^ok'` = 30, `grep -cE '^(FAIL|--- FAIL|panic:)'` = 0, `grep -cE '^\?'` = 6 (paket tanpa tes), `go list ./... | wc -l` = 36. Perintah make yang tercatat berurutan `lint`, `generate-check`, `test` (backend, frontend), `migrate-roundtrip`, `build`, lalu `verify-smoke.sh` (baris 111); satu-satunya `make: ***` sebelum smoke tidak ada, galat pertama di baris 356 (`Makefile:158: smoke`) dan 583 (`verify`). Smoke: `M00 smoke OK`, `M02 smoke OK`, lalu m03 gagal pada `POST /auth/login`.
- **Pesan galat API run #1:** `service_unavailable: auth: login dibatalkan: context canceled` (503, `duration_ms` 14688). `m03.sh:66` memakai `curl --max-time 8`. Login m02 beberapa detik sebelumnya: 200 dalam 729 ms. Kode keluar curl tidak tercatat (pesan lama tidak memuatnya).
- **Run smoke #2** (hanya `verify-smoke.sh`, kode M04 tanpa patch): `exit=2`, `M00 OK`, gagal di m02 pada login yang sama. Baris login API tidak ada di tempelan.
- **Run smoke #3** (setelah patch `3977aa6`, batas 20 dtk): `exit=2`, gagal di langkah yang sama. Tidak ada kode keluar curl di pesan.
- **Stack manual `platform-measure` (tenang):** login 1 = 3,55 dtk (pertama setelah bootstrap), login 2–5 = 0,22–0,37 dtk, semua 200. `docker stats`: container proyek lain nyaris idle; `free -m`: 4918 MB total, 316 MB bebas, swap terpakai 218 MB. `migrate` selesai 532 dtk (run ini) lalu 108 dtk (run `platform-m05`): unduhan modul jaringan sangat bervariasi.
- **Log postgres pada ketiga run gagal** (`~/verify.log`, `~/smoke2.log`, `~/smoke3.log`): checkpoint sangat lambat untuk beban kecil: 07:44:04–07:44:48 (`write=38.9 s`, 280 buffer), 08:17:08–08:18:06 (`write=32.5 s`, `sync=19.7 s`, 274 buffer), 11:02:20–11:03:09 (`write=28.3 s`, 241 buffer). Run #3 juga memuat crash-restart 10:56:17 ("untracked child process (PID 818) exited with exit code 2" → `terminating any other active server processes` → recovery ±55 dtk dengan `syncing data directory (fsync)` sampai 30 dtk). Pencocokan jam: login gagal run #1 dimulai ±0,1 dtk setelah checkpoint selesai; run #2 dan #3: tidak ada checkpoint berjalan pada saat login gagal (cadence 5 menit). **Hipotesis "I/O disk VM Docker menahan commit login" TIDAK terbukti untuk detik kegagalan.**
- **Patch smoke** (hanya `scripts/smoke/m02.sh`, `m03.sh`, `m04.sh`): `3977aa6` `fix(smoke): batas waktu curl dapat diatur (SMOKE_CURL_MAX_TIME, bawaan 20)` dan `4ff53dd` `chore(smoke): pesan curl gagal memuat kode keluar dan lama tunggu`. Dibuktikan di sandbox: penggantian dengan assert tepat-satu, `bash -n`, `lint-structure OK`, `git am` berurutan pada salinan bersih zip memberi pohon identik (580 berkas), uji curl tiruan keluar 28 menghasilkan `curl gagal (exit=28, 1 dtk)`. Di laptop: `git log` menunjukkan kedua commit di atas `6a14f87`; `grep -c` = 1/1/1 (m02/m03/m04) untuk `curl gagal (exit=`; `sha256sum` mbox sebelum `git am`: tidak ditempel.
- **Smoke terhadap stack hidup `platform-m05`** (`COMPOSE_PROJECT_NAME=platform-m05 SMOKE_CURL_MAX_TIME=90 make smoke`): **`exit=0`; M00, M02, M03, M04 smoke OK** (`~/smoke4.log`). Ini membuktikan smoke lolos pada stack hangat dengan batas 90 dtk; TIDAK membuktikan alur dingin `verify-smoke.sh` dengan batas bawaan 20 dtk.
- **Build image pertama `platform-m05`:** `TLS handshake timeout` ke `auth.docker.io` (jaringan; bukan kode); percobaan kedua berhasil, `curl https://auth.docker.io/token` = `http=200 t=0.90s`.
- **`git push`:** `6a14f87..4ff53dd  milestone/m05-organization -> milestone/m05-organization` (ditempel).
- **Tidak dijalankan/tidak ditempel:** `make verify` utuh yang hijau pada kode M05 (belum ada kode M05); setiap paket sendirian (langkah 3c; tidak perlu karena tahap tes run #1 hijau); `gofmt/vet/lint` setelah patch smoke (hanya skrip shell berubah).
- **Keadaan laptop saat checkpoint:** stack compose `platform-m05` MASIH HIDUP (port 25000–25005). Bongkar sebelum `make verify`/`verify-smoke.sh`: `COMPOSE_PROJECT_NAME=platform-m05 docker compose --profile full down -v --remove-orphans`.

## 4. Keputusan & penyimpangan
**Seluruh isi §4 HANDOVER-M04 tetap berlaku** (keputusan yang sudah dikonfirmasi; keputusan yang dipakai tetapi BELUM dikonfirmasi: migrasi 035 opsi A, opsi `Scope` idempotensi, semantik `simulate`, status bawaan `all` pada `GET /policies`, gagal-start tanpa policy atau pencabut sesi, pencabutan sesi satu percobaan 10 dtk tanpa outbox, semantik `FieldHelper`, fixture smoke lewat superuser; (a) nama definisi OpenAPI panjang diterima sementara, (b) blok "As built" di bawah judul bagian, (c) PROMPT P2 sebagai berkas; koreksi tag `m03-done` = `883487d`).

**Doc-sync tertunda (kumulatif dari HANDOVER-M04 §4; belum ada yang tergabung di sesi ini):** contoh JSON respons `simulate` di docs/06 §10 dan daftar kode di docs/06 §12 dan docs/08 §11 tidak diedit di tempat (hanya blok di bawahnya); docs/13 §4 (aturan visibilitas aksi audit baru) dan docs/12 §6 tidak dibuka; docs/08 §10.2 tidak diubah; nomor bagian tepat di docs/10 dan docs/19 dipilih menurut judul tanpa membaca isi penuh; `HANDOVER-M03-partial-4.md` §4 dan `HANDOVER-M04-partial-1.md` §4 asli tidak ada. **Baru (sesi ini):** docs/08 §10.6–§10.9 harus mendapat blok "As built (M05, ADR-0023)" setelah T1–T5 (path `{id}` dan scope, kode error baru, paginasi); docs/00 butir E:I-10 belum dibaca (tidak diperlukan sampai T3/T4).

**Keputusan baru sesi ini:**
1. Batas curl smoke m02/m03/m04 dibuat dapat diatur lewat `SMOKE_CURL_MAX_TIME`, bawaan 20 dtk (m02 dan m03 sebelumnya 8 dtk, m04 sudah 20). Sebagian menjawab pertanyaan lama §9(f) (naikkan batas login); volume cache modul `verify-smoke.sh` (`down -v`) TIDAK diubah.
2. Pesan `curl gagal` kini memuat `exit=<rc>` dan lama tunggu (detik). Perilaku lain tidak berubah.

**Usulan desain M05 (DIPAKAI sebagai default, BELUM dikonfirmasi pemilik; bantah bila perlu; ditulis ke ADR-0023 saat kode pertama ditulis):**
1. **Path `{id}` melawan "organisasi hanya dari ScopeContext":** pemanggil tenant: `{id}` harus sama dengan organisasi di scope, kalau tidak 404 seragam (tidak membedakan "bukan milikmu" dari "tidak ada"); hanya pemanggil platform boleh menarget organisasi lain, lewat rute/method berlabel eksplisit dengan keputusan platform.
2. **Accept-invitation untuk email yang sudah punya akun:** token undangan TIDAK boleh mengatur password akun yang sudah ada; akun `active` yang sudah ada hanya mendapat membership dan assignment dan wajib login lebih dulu; hanya akun baru yang mengatur password.
3. **TTL token:** undangan 72 jam, reset 30 menit (docs/05 §13); token hanya disimpan sebagai hash SHA-256, sekali pakai; respons invalid/terpakai/kedaluwarsa identik.
4. **Paginasi:** offset (`page`/`per_page`, bawaan 50, maksimum 100) untuk organisasi, user, grup; cursor untuk anggota dan undangan.
5. **`DELETE /organizations/{id}`** = `status = archived` + `deleted_at`, hak efektif kosong; cakupan pencabutan sesi (semua anggota atau hanya yang aktif di organisasi itu) diputuskan di T5 SETELAH skema sesi dibaca (belum diperiksa).
6. **Admin membuat user dengan email yang sudah ada = 409** membuka oracle keberadaan email bagi admin; dicatat sebagai keterbatasan, tidak ditutup.

## 5. Delta kontrak (sesi ini)
Tidak ada perubahan API, skema, migrasi, atau kode Go. Hanya `scripts/smoke/m02.sh`, `m03.sh`, `m04.sh`: variabel lingkungan baru `SMOKE_CURL_MAX_TIME` (bawaan 20) dan format pesan `curl gagal (exit=<rc>, <n> dtk): <argumen>`. Migrasi terakhir tetap 035 (berikutnya 036; kemungkinan M05 tidak membutuhkan 036, harus dibuktikan di T1). Katalog core tetap 41 kode.

## 6. Peta kode (yang dibaca untuk T1a; BUKAN kode baru)
- **Belum ada** paket `organization`, `identity`, `group`, `membership`; `RequireTenant` tidak ada di kode; worker (`cmd/worker` → `app.RunWorker`) membuka pool app dan maintenance serta Redis, relay outbox ada di `internal/kernel/relay.go` (apakah `RunWorker` sudah memasangnya: BELUM diperiksa).
- **Tabel:** `organizations` (status `active|suspended|archived`, `uq_organizations_slug`, `deleted_at`), `organization_memberships` (status `invited|active|suspended|removed`, `uq_org_memberships (organization_id, user_id)`), `users.email` unik global (`uq_users_email`), `uq_invitations_pending` sudah memuat `organization_id`.
- **Konvensi repository** (pola `authorization/repository`): pgx langsung di atas `kernel.DBTX`, struktur `type X struct{ db kernel.DBTX }` + `NewX(db)`; konstanta SQL; galat dibungkus `fmt.Errorf("<paket>: ...: %w", err)`; kunci advisory via `LockOrganization(ctx, orgID)` dan `LockPlatform(ctx)` (di dalam transaksi; `ErrNoTransaction` di luar).
- **Pola penulis** (`authorization/application/assignments.go`): `tx TxRunner` + `store` + `audit modulesdk.AuditRecorder` + `events EventPublisher` + `inv TxInvalidator`; per operasi satu `tx.WithinTx`: kunci → aturan → authority ceiling dari PostgreSQL → tulis → invalidasi cache → audit → event; penolakan kebijakan diaudit `denied` di transaksi terpisah; pencabutan sesi sesudah commit (`SessionRevoker`).
- **Perakitan rute:** `httpserver.AuthzMounter`, `MountAuthzRoutes` (prefiks `/api/v1` ditambahkan sekali; hanya `With`, `Get/Post/Put/Patch/Delete`), urutan `Authenticate → CSRF → RequireAuth → Scope → Authorize(action) → [TenantIdempotency.Require()]`.
- **Fixture testkit:** `NewOrganization`, `NewUser(InOrganization(o))`, `AddOrganizationMember`, `NewGroup`, `AddGroupMember(g, u, status)`; baris ditulis lewat `db.Migrator`, kode aplikasi diuji lewat `db.App`.
- **Katalog core terkait:** `organization.read|create|update|archive|member.manage`, `user.read|create|update|delete`, `group.read|create|update|delete|manage_members`.
- **ADR-0022 yang dibaca untuk T1a:** butir 12–15, 25, 27 (tiket masuk organisasi: `organization.read` level platform; engine ditanya sebelum `OrganizationIsActive`), 28, 54, 62, 67, 73, 76, 77. Butir ADR-0020/0021 tidak dibaca sesi ini.

## 7. Cara menjalankan & memverifikasi
- Gerbang kode (laptop, `~/projects/zago-core-m03`): `gofmt -l backend` (kosong), `go vet <paket>`, `go tool golangci-lint run <paket>` (`0 issues.`), `REQUIRE_DOCKER=1 go test <paket> -race -count=1 -p 1`.
- **Smoke tanpa membangun ulang tiap kali** (build ±10 menit di laptop ini): naikkan SATU stack manual, jalankan `make smoke` berulang, bongkar sebelum `make verify`:
  `COMPOSE_PROJECT_NAME=platform-m05 docker compose --profile full up -d --build && COMPOSE_PROJECT_NAME=platform-m05 bash ./scripts/wait-for-healthy.sh`, lalu `COMPOSE_PROJECT_NAME=platform-m05 SMOKE_CURL_MAX_TIME=90 make smoke > ~/smoke.log 2>&1; echo "exit=$?"`, bongkar: `COMPOSE_PROJECT_NAME=platform-m05 docker compose --profile full down -v --remove-orphans`. Bila `migrate` gagal `unexpected EOF`: ulangi `up` tanpa `--build` dan tanpa `down -v`. Bila `build` gagal `TLS handshake timeout`: ulangi, lalu `curl -sS -m 15 -o /dev/null -w 'http=%{http_code} t=%{time_total}s\n' https://auth.docker.io/token`.

## 8. Masalah diketahui / utang teknis
**Seluruh §8 HANDOVER-M04 tetap berlaku** (OpenAPI nama panjang, smoke fixture psql, scope `resource`/`own`, FieldGuard tanpa pemakai produksi, sesi tanpa outbox, retry start, `simulate`, policy, penulis, cache, audit indeks unik pola 035 BELUM dilakukan, utang lintas milestone M05/M07/M15, lingkungan laptop).
**Baru:**
- **Login > 8–20 dtk pada alur dingin `verify-smoke.sh` (tiga run berturut-turut, dua skrip berbeda): PENYEBAB TIDAK TERBUKTI.** Yang terukur: login tenang 0,22–0,37 dtk (login pertama 3,55 dtk); I/O VM Docker lambat (checkpoint 240–280 buffer butuh 44–58 dtk; `fsync` direktori data sampai 30 dtk); satu crash-restart postgres terpicu "untracked child process exited with exit code 2" (hipotesis: proses luar, mis. `pg_isready` healthcheck yang keluar 2 lalu yatim ke PID 1, ATAU proses `psql` dari skrip; baris `exit code 0` hanya tiga kali per menit sehingga TIDAK cocok dengan healthcheck 5 dtk, jadi tidak diklaim); Redis dan Argon2 (64 MiB, t=3, p=2) belum disingkirkan. Langkah berikutnya bila terulang: baca `exit=` dan durasi dari pesan baru, lalu ukur login selama pemuatan I/O.
- **Cacat compose yang MUNGKIN:** bila crash-restart postgres terbukti dipicu proses luar yang keluar tidak nol, itu cacat `docker-compose.yml` (laporkan; perbaiki di commit tersendiri). Belum terbukti.
- Log API hanya mencatat request SETELAH selesai; request yang masih menggantung saat log diambil tidak terlihat (observabilitas, M15).
- `verify-smoke.sh` masih `down -v` volume cache modul tiap run (§9(f) separuh terjawab).
- Pesan `curl gagal: …` mencetak argumen curl, termasuk password acak smoke (sandi smoke hanya untuk stack lokal; pertimbangkan penyamaran).
- Dugaan proses yang SALAH di sesi ini (dicatat supaya tidak diulang): "batas 8 dtk saja yang sempit" (20 dtk tetap gagal), "api/worker restart" (ternyata `migrate` lama menunda mulai).

## 9. Pertanyaan terbuka
(a)–(g) di HANDOVER-M04 §9 tetap berlaku ((f) dijawab sebagian oleh keputusan 1 di §4).
- (h) Enam usulan desain M05 di §4 (terutama butir 1 path `{id}`, butir 2 accept-invitation akun yang sudah ada, butir 5 cakupan pencabutan sesi organisasi): bantah sebelum T1a membangun di atasnya.
- (i) Perlukah menyamarkan password di pesan `curl gagal`?
- (j) Apakah smoke `verify-smoke.sh` harus tetap memakai alur dingin penuh (build + `down -v`) atau cukup stack yang dipertahankan?

## 10. Milestone berikutnya: lanjutan M05 (T1a)
Mulai dari T1a, dipecah menjadi patch kecil (tes dulu):
- **T1a-1** domain `organization` (entitas, status, sentinel galat) + repository (`Organizations`, `OrganizationMembers`; semua query memfilter organisasi; baca/ubah/daftar/anggota) + tes ber-DB: lintas organisasi DUA ARAH untuk SETIAP fungsi repository; keunikan memakai DUA organisasi; pesan galat identik antara "milik organisasi lain" dan "tidak ada".
- **T1a-2** service (transaksi: kunci → aturan → tulis → invalidasi → audit → event; ceiling dari PostgreSQL di dalam transaksi) + tes unit (urutan panggilan) + tes penahanan kunci.
- **T1a-3** `RequireTenant`, DTO, handler `/organizations` (+ `members`), `DomainError`/`codeRules` untuk sentinel baru, `MountAuthzRoutes`-style perakit, `make generate` + `chore(generate)`, `bash ./scripts/generate-check.sh`.
Sebelum menulis: baca `kernel.DBTX`/`TxRunner`/`EventPublisher`/`modulesdk.AuditRecorder`, satu tes repository M04 sebagai konvensi, pola `DomainError` dan DTO role, dan `AuthDeps`/`RunWorker` untuk T3. Alokasikan nomor butir ADR (ADR-0023 bagian 1) SEBELUM menulis komentar kode yang merujuknya.

## 11. Prompt untuk chat berikutnya
`docs/handover/PROMPT-M05-partial-1.md` (disimpan di repo; dirakit dari `PROMPT-M05.md` dengan skrip beranchor). Commit teratas yang diharapkan: commit `docs(m05): HANDOVER-M05-partial-1 …` di atas `4ff53dd`. Jumlah berkas yang diharapkan: 582 (580 + `HANDOVER-M05-partial-1.md` + `PROMPT-M05-partial-1.md`). Keadaan `make verify` yang diharapkan: seperti §3 (utuh `exit=2` pada kode M04 karena smoke alur dingin; tahap tes hijau; smoke hijau pada stack hangat).
