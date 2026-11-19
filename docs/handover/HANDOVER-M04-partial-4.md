# HANDOVER — M04 Authorization Engine (PARSIAL 4)

**Tanggal:** 2026-11-02 · **Tag:** belum ada (`m04-done` dibuat di akhir milestone) · **Commit terakhir:** subjek `docs(m04): HANDOVER-M04-partial-4, ADR-0022 bagian 4, STATUS, CHANGELOG — checkpoint setelah T3c (◆CP1)` (SHA laptop berbeda dari sandbox; subjek dan urutan yang berlaku)
**Jenis:** PARSIAL 4 · **Branch:** `milestone/m04-authorization` (basis: tag `m03-done` = `20e7fc0`) · **Handover sebelumnya:** `HANDOVER-M04-partial-3.md` (partial-2 Lampiran A memuat rekonstruksi `partial-1`, yang berkas aslinya tidak ada di repo).

## 1. Ringkasan
T3c selesai (◆CP1): middleware `Authorize(action)`, kode error `scope_mismatch`, perakitan cache + `Authorizer` + `Syncer.WithInvalidator` di `RunAPI`, `PlatformEntryChecker` (auth) dengan implementasi `PlatformEntry` (authorization), dan jalur masuk organisasi lewat identitas platform di `SwitchOrganization`.
Enam commit kode, semuanya di-push (`ea16a9c..40ed3dc`, lalu `40ed3dc..67b2ac3`). Engine kini terpasang di satu jalur request nyata: `switch-organization` bagi identitas yang bukan anggota. Middleware `Authorize` sudah ada dan teruji, tetapi **belum dirakit di `internal/app` dan belum dipasang di rute mana pun** (rute pertama yang memakainya datang di T5).
Belum ada: policy + FieldGuard + migrasi 034 (T4); authority ceiling, endpoint, simulate, smoke m04, perakitan middleware `authzhttp` di app dan rute (T5).
◆CP1 dicatat jujur: satu run `make verify` utuh yang hijau pada commit terkini TIDAK terjadi di sesi ini (lihat §3); bukti penggantinya lengkap dan hijau.

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T1 katalog permission, role sistem, Syncer | DONE | partial-2 Lampiran A; ADR-0022 butir 5–9 |
| T1b Syncer di RunAPI | DONE | ADR-0022 butir 11 |
| T2 ScopeContext, ScopeResolver, middleware Scope | DONE | ADR-0022 butir 12–15 |
| T3a domain + repositori effective permissions | DONE | ADR-0022 butir 16–18 |
| T3b cache versi + `Authorizer.Can` + `Syncer.WithInvalidator` | DONE | ADR-0022 butir 19–22 |
| T3b2 evaluasi scope `resource`/`own` di `Can` | DITUNDA | keputusan sesi ini (§4); engine tetap gagal tertutup |
| T3c middleware `Authorize`, wiring app, `PlatformEntryChecker`, switch-organization platform, `scope_mismatch` (◆CP1) | DONE | enam commit; ADR-0022 butir 24–29 |
| T4 policy + FieldGuard + migrasi **034** | TODO | ADR-0022 butir 2 dan 10 |
| T5 authority ceiling, endpoint, simulate, smoke m04, perakitan middleware di app | TODO | §10 |

## 3. Bukti verifikasi (apa adanya)
- **Baseline awal sesi (laptop WSL2):** `go1.26.5`, `gofmt -l backend` kosong. Keadaan repo cocok (branch sejajar origin, 414 berkas, fsck bersih, tag `m03-done`, commit teratas `ea16a9c`). **Satu run `make verify GO_TEST_PARALLEL=1` utuh pada `ea16a9c`: `exit=0`, `M00/M02/M03 smoke OK`** (run utuh hijau pertama sejak T1, mencakup T1b–T3b).
- **T3c bagian 1 (tiga commit, di-push `ea16a9c..40ed3dc`):** `gofmt` kosong; `go vet ./backend/internal/authorization/...` bersih; `go tool golangci-lint run ./backend/internal/authorization/...`: `0 issues.`; `go test ./backend/internal/authorization/transport/... -race -count=1`: `ok` 1,1 dtk. Lolos pada percobaan pertama.
- **T3c bagian 2 (tiga commit, di-push `40ed3dc..67b2ac3`):** `gofmt` kosong; `go vet ./backend/...` bersih; `golangci-lint` pada `app`, `auth`, `authorization`: `0 issues.`; `REQUIRE_DOCKER=1 go test … -race -count=1 -p 1`: `auth/service` ok 79,2 dtk, `authorization/application` ok 36,1 dtk, `app` ok 2,2 dtk. Lolos pada percobaan pertama. Satu skrip penggantian teks di sandbox sempat gagal pada `assert` (komentar asli berbeda dari yang saya ketik); karena assert, tidak ada yang tertulis, dan diulang dengan jangkar yang benar (Pelajaran 3 berfungsi).
- **◆CP1 (pada kode `67b2ac3`):**
  - `make verify GO_TEST_PARALLEL=1`, run 1: `exit=2`. Sebelas tes di `internal/systemlog` gagal; pesan: `gagal menyalakan postgres:18 … reaper … wait for reaper … context deadline exceeded`. Satu tes gagal setelah 61,33 dtk lalu sepuluh gagal dalam 0,00 dtk. **Kelas flake Docker (bukan kode).** `systemlog` sendirian sesudah membersihkan container testcontainers: ok 26,4 dtk.
  - Run 2: `exit=2`. Satu tes (`TestMiddlewareWithRealSessionService`) di `auth/transport/http` gagal setelah 61,24 dtk dengan pesan reaper yang sama. **Kelas flake yang sama, paket berpindah.** Sendirian: ok 21,5 dtk. `authorization/application` pada run yang sama ok (40,1 dtk).
  - **Bukti pengganti:** setiap paket sendirian dengan `-race -count=1 -p 1`: **35 dari 35 paket `0`** (semua, termasuk `app`, `infra`, `auth/*`, `authorization/*`, `systemlog`, `testkit`).
  - **Tahap sisa:** `make lint` (golangci-lint `0 issues.`, svelte-check 0 error/0 warning, prettier bersih, `lint-structure OK`), `generate-check OK`, `migrate-roundtrip OK`, `make build` (tiga biner + web) lulus.
  - `verify-smoke.sh`, percobaan 1: `exit=1`. Container `migrate` (`go run` dengan volume modul baru) gagal mengunduh modul dari `proxy.golang.org` dengan `unexpected EOF` pada belasan modul berbeda; `api`, `worker`, `web` tidak pernah berjalan. **Kelas lingkungan baru (jaringan keluar Docker Desktop terputus), bukan kode**; tidak ada galat kompilasi atau SQL dari kode kita.
  - Percobaan 2 (hanya tahap smoke, tanpa perubahan kode): **`exit=0`, `M00 smoke OK`, `M02 smoke OK`, `M03 smoke OK`**. Ini bukti pertama bahwa `RunAPI` dengan `Syncer.WithInvalidator` dan cache Redis terpasang berjalan di stack nyata.
  - **Kesimpulan jujur:** TIDAK ada satu run `make verify` hijau pada `67b2ac3`. Yang ada: 35/35 paket hijau sendirian + semua tahap sisa hijau (percobaan smoke ke-2). Disarankan satu run utuh lagi bila Docker laptop sedang sehat (§9).
- **Sandbox (bukan bukti utama):** Go dan gofmt tidak tersedia. Terbukti di sandbox: `bash scripts/lint-structure.sh` → `lint-structure OK`; `git am` berantai (zip → part1 → part2 → patch dokumen ini) menghasilkan pohon identik. Semua bukti kompilasi/gofmt/vet/lint/tes berasal dari laptop.
- **Tes negatif/keamanan baru (T3c):**
  - *`scope_mismatch`:* terdaftar di registry `httpx` dengan status 403 dan kode berbeda dari `permission_denied`; dirender lewat `Responder`.
  - *`Authorize`:* izin → handler tercapai dengan `Check` persis (Identity, Scope, Action, `Resource` nil); identitas platform diteruskan tanpa diubah; ditolak (`no_permission`, `scope_mismatch`, Decision nol, alasan tak dikenal, `granted` tetapi tidak allowed) = 403 seragam dan handler TIDAK tercapai, aksi dan alasan internal tidak bocor ke body;
    penolakan direkam `authz.access_denied` (`result=denied`) dengan `required_action`, `reason` (daftar putih; selain itu `unspecified`), `method`, dan scope request (grup > organisasi > platform); tetap menolak tanpa perekam audit;
    tanpa Identity = 401 dan engine TIDAK dipanggil; Identity tanpa Scope = 500 dan engine tidak dipanggil (salah urutan tidak membuka rute); **galat engine tidak pernah menjadi ALLOW, juga bila `Decision{Allowed:true}` ikut dikembalikan bersama galat**: infrastruktur = 503, `ErrInvalidCheck`/`ErrInvalidScope` = 500, detail infrastruktur tidak bocor, bukan direkam sebagai denied;
    tiap rute memakai aksinya sendiri; `Authorize` panik saat dipasang tanpa `Authorizer` atau dengan aksi tak sah (enam bentuk).
  - *Middleware `Scope`:* `ErrInvalidScope` = 500 (bukan 503).
  - *`SwitchOrganization` jalur platform (ber-DB):* identitas tanpa keanggotaan masuk organisasi `active` bila checker mengizinkan (sesi lama mati, sesi baru di organisasi tujuan, event dan activity membawa `via=platform`, tidak ada keanggotaan dibuat, activity dicatat di organisasi tujuan);
    organisasi `suspended`/`archived`/terhapus/tidak ada, tanpa izin platform, dan tanpa checker = `ErrNotOrganizationMember` yang sama, tanpa efek samping (sesi utuh, tanpa event, tanpa activity baru); galat checker = galat (bukan penolakan seragam, bukan izin), sesi utuh; jalur anggota TIDAK memanggil checker dan metadata-nya tidak berubah;
    non-anggota tanpa hak platform: engine ditanya sama banyak untuk organisasi yang ada dan yang tidak ada; identitas platform yang sudah berada di dalam organisasi dapat masuk ke organisasi lain (checker hanya menerima user id).
  - *`PlatformEntry` (unit + integrasi PostgreSQL asli):* memanggil `Can` dengan Identity tanpa organisasi, scope platform, `organization.read`; hanya `Allowed` dengan level platform yang dihitung (organisasi/grup/tanpa level = false); galat engine = galat walau Decision ALLOW; grant platform = ya, grant platform dicabut = tidak, **grant tingkat organisasi bukan tiket masuk ke organisasi lain**, anggota tanpa grant = tidak.
  - *Perakitan app:* `newAuthzStack` tanpa Redis = invalidator interface nil (bukan pointer nil terbungkus) dan `PlatformEntry` tetap dirakit; dengan Redis = cache terpasang sebagai invalidator; `authzStack{}` nol memberi `entryChecker()` interface nil (fail-closed); sinkron gagal tidak menaikkan versi.
- **Kendala mesin (laptop, Docker Desktop ±4,9 GB):** tiga flake teramati di ◆CP1 (dua reaper di paket berbeda, satu unduhan modul `unexpected EOF` di container `migrate`); semuanya hilang dengan menjalankan sendirian/mengulang tahap tanpa mengubah kode. Prosedur lama tetap berlaku: baca pesan dulu, bersihkan container testcontainers (`docker ps -aq --filter label=org.testcontainers=true | xargs -r docker rm -f`), jalankan paket sendirian. `make up` TIDAK ada (pakai `make docker-up`).

## 4. Keputusan & penyimpangan
- **ADR-0022 bagian 4 (butir 24–29)**, ringkas: 24 `scope_mismatch` didaftarkan di `authzhttp` (paket pemilik; registry `httpx` yang sama), status 403; 25 perilaku `Authorize` (di atas) dan audit penolakan; 26 `ErrInvalidScope` dari resolver = 500;
  27 `PlatformEntryChecker` di `auth/service`, implementasi `authorization/application.PlatformEntry`, engine ditanya LEBIH DULU lalu organisasi diperiksa `active`, penolakan seragam, `via=platform`; 28 perakitan `authzStack` di `internal/app` (cache hanya bila ada Redis, `WithInvalidator` terpasang, middleware `authzhttp` belum dirakit); 29 jawaban pertanyaan terbuka partial-3 §9.
- **Penyimpangan dari prompt/handover sebelumnya (disengaja, dicatat):** (a) `Authorize` tanpa Scope = 500, bukan 401 (klien yang sudah login tidak dapat memperbaikinya dengan login ulang; tetap gagal tertutup); (b) galat engine pada `switch-organization` naik lewat jalur handler yang sama dengan galat database lain di endpoint itu = 500, bukan 503 (503 khusus butuh sentinel baru; tidak dikerjakan);
  (c) `scope_mismatch` di `authzhttp`, bukan di berkas `httpx` (konvensi `errors.go`).
- **Jawaban pertanyaan terbuka partial-3 §9:** T3b2 DITUNDA ke milestone pertama yang punya sumber daya ber-pemilik; env cache TIDAK ditambah (konstanta sampai M15); retry sinkron saat start BELUM dikerjakan (tetap terbuka, §9); `Authorize` 503/500 sesuai butir 25.
- **Doc-sync M04 TERTUNDA (kumulatif; dasar: partial-3 §4 + ADR-0022 bagian 4):**
  docs/06 (§4.1 jalur platform, §4.2 header dibuang/bentuk kanonik/pengetatan, §5.2 platform hanya subjek user + pengetatan + level terluas + resource/own belum, §5.5 slug role, §6 pengikat policy, §8.1 invalidasi eksplisit, versi pasangan, versi dari jam, TTL, skrip bump, perilaku bump gagal, §9 galat vs keputusan, urutan `Can`, perilaku `Authorize` (butir 25) dan activity `authz.access_denied`, §12 lokasi `scope_mismatch`, §13 struktur paket nyata, §14 tes yang sudah/belum terpenuhi);
  docs/03 + docs/04 + `docs/erd.md` (kolom `policies.actions`, migrasi **034**); docs/05 (switch-organization untuk identitas platform, `via=platform`, penolakan seragam, urutan engine-lalu-organisasi); docs/08 §11 (`scope_mismatch` 403); docs/00 B-04 (katalog dan role sinkron sebagai dibangun);
  docs/10 §3 (urutan start API: dependensi → `authzStack` → sinkron katalog dengan invalidator → audit → toolkit → auth) dan §11/§15 (invalidasi gagal tidak mematikan start); docs/18 §7 (fixture grup; fixture role/permission/assignment di `testkit`); ADR-0020 butir 28 (dilanjutkan ADR-0022 butir 1).
- **Belum tergabung (berkas tidak ada):** `HANDOVER-M03-partial-4.md` §4 dan `HANDOVER-M04-partial-1.md` §4 asli.

## 5. Delta kontrak (sejauh ini di M04)
- **DB:** tidak ada migrasi baru. `034_policies_actions` (T4) akan menambah `policies.actions text[]`; periksa lagi nomor bebas sebelum membuat berkas (001–033 terpakai).
- **API:** endpoint `switch-organization` yang sudah ada kini menerima identitas platform (bukan anggota) bila engine memutuskan `organization.read` pada scope platform dan organisasi `active`; respons penolakan tidak berubah (403 `permission_denied`). Header `X-Active-Group-ID` masih belum terpasang di rute mana pun.
- **Kode error:** `scope_mismatch` (403) terdaftar; `permission_denied` (403) sudah ada di `httpx`.
- **Activity:** `authz.access_denied` (`result=denied`; metadata `required_action`, `reason`, `method`) didefinisikan dan teruji, tetapi belum dipancarkan oleh rute mana pun. `auth.switch_organization` bermetadata `{via: platform}` pada jalur platform (jalur anggota tanpa metadata, tidak berubah).
- **Event keamanan:** `auth.organization.switched` bermetadata `via=platform` pada jalur platform (tanpa `from_organization_id` bila sesi asal tanpa organisasi).
- **Redis (kini aktif di runtime):** `authz:ver:<organization_id>`, `authz:ver:platform`, `authz:eff:…` (lihat partial-3 §5). `Syncer` di `RunAPI` menaikkan `authz:ver:platform` bila katalog berubah. Belum ada pemakai lain sampai T5.
- **Startup API:** dependensi dibuka → `newAuthzStack` (hanya bila pool ada) → `syncAuthorization` (dengan invalidator bila ada Redis) → audit → toolkit HTTP kernel → `newAuthStack` (dengan `PlatformEntryChecker`) → router. Galat merakit atau menyinkronkan = `ExitFailed`.
- **Config/env:** tidak ada baru.
- **Antarmuka Go (baru):** `auth/service` (`PlatformEntryChecker`, `AuthDeps.PlatformEntry`); `auth/repository` (`Store.OrganizationIsActive`); `authorization/application` (`Decider`, `PlatformEntry`, `NewPlatformEntry`); `authorization/transport/http` (`Authorizer`, `Deps.Authorizer`/`Deps.Audit`, `Middleware.Authorize`, `ActivityAccessDenied`, `ScopeMismatch`); `app` (`authzStack`, `newAuthzStack`, `entryChecker`; `syncAuthorization` dan `newAuthStack` bertanda tangan baru).

## 6. Peta kode (tambahan sejak partial-3)
- `backend/internal/authorization/transport/http/` — `errors.go` (`ScopeMismatch`), `authorize.go` (`Authorizer`, `Authorize`, `engineFailure`, `deny`, `auditContext`), `middleware.go` (+ `Deps.Authorizer`/`Deps.Audit`; `ErrInvalidScope` = 500), tes: `errors_test.go`, `authorize_test.go`, `middleware_test.go`.
- `backend/internal/authorization/application/` — `platform_entry.go` (`Decider`, `PlatformEntry`), `platform_entry_test.go` (unit + integrasi PostgreSQL).
- `backend/internal/auth/service/auth.go` — `PlatformEntryChecker`, `AuthDeps.PlatformEntry`, `platformMayEnter`, `switchViaPlatform`, `recordResourceMeta`; `auth_platform_entry_test.go` (ber-DB; `fakePlatformEntry`, `authHarness.authWith`).
- `backend/internal/auth/repository/platform_entry.go` — `Store.OrganizationIsActive` (SQL tulis tangan, bukan sqlc).
- `backend/internal/app/` — `authz.go` (`authzStack`, `newAuthzStack`, `entryChecker`, `syncAuthorization` + invalidator), `app.go` (urutan start), `auth.go` (`newAuthStack` + parameter `platform`), tes: `authz_internal_test.go`, `auth_internal_test.go`.

## 7. Cara menjalankan & memverifikasi
- Paket M04: `REQUIRE_DOCKER=1 go test ./backend/internal/authorization/... ./backend/internal/auth/... ./backend/internal/permission ./backend/internal/role ./backend/internal/app/... ./backend/internal/testkit/... -race -count=1 -p 1`.
- Lint: `gofmt -l backend; go vet <paket> && go tool golangci-lint run <paket>`; `bash scripts/lint-structure.sh`.
- Penuh: `gofmt -l backend; make verify GO_TEST_PARALLEL=1 > ~/verify.log 2>&1; echo "exit=$?"`. Flake Docker: lihat §3 dan prompt; bukti pengganti = tiap paket sendirian + tahap sisa (`make lint && make generate-check && make migrate-roundtrip && make build && bash ./scripts/verify-smoke.sh`).
- Sesudah `git am`: verifikasi penanda (`git log --oneline -N`, grep fungsi kunci) SEBELUM menjalankan tes; bila `git am` menolak karena `.git/rebase-apply`, pakai `git am --quit`.

## 8. Masalah diketahui / utang teknis
- **Satu run `make verify` utuh hijau pada kode T3c belum ada** (§3): ada bukti pengganti lengkap; satu run utuh lagi dianjurkan di awal sesi berikutnya bila Docker sehat.
- **Middleware `authzhttp` belum dirakit di `internal/app`** dan tidak ada rute yang memakai `Scope`/`Authorize`; `authzStack` sengaja belum menyimpan `Authorizer` (field yang tak dibaca ditolak linter). Perakitan di T5 bersama rute pertama (tambahkan `Authorizer` ke `authzStack`, adaptor `ScopeResolver`, `Options.Authz` di `httpserver`).
- **Sesi hasil masuk-lewat-platform tetap hidup sampai habis walau assignment platform dicabut:** `Authenticate` tidak memeriksa ulang keanggotaan/hak per request (keterbatasan sejak M03; pencabutan sesi saat suspend datang di M05 T5). Setiap request di sesi itu tetap melewati `Authorize` (engine memutuskan ulang), tetapi `switch-organization` baru dan tindakan yang tidak diguard lolos sampai sesi berakhir. Pertimbangkan pencabutan sesi saat assignment super-admin dicabut (T5).
- **Audit penolakan menulis satu baris `activity` per request ditolak;** belum ada rate limit per pengguna. Penyerang terautentikasi dapat memicu tulis berulang pada rute yang diguard (mitigasi: rate limit toolkit HTTP; belum dipasang di rute M04).
- **`kernel.Scope` belum dipasang di middleware `Scope`:** activity sukses milik handler tidak akan memuat `scope_type`/`scope_id` (hanya activity penolakan `Authorize` yang memuatnya). Putuskan di T5/M05.
- **`RequireTenant`** (tolak scope platform pada handler tenant dengan `scope_mismatch`, docs/06 §2) ditunda ke handler tenant pertama (M05).
- **Scope `resource` dan `own` belum dievaluasi** (ditolak; ADR-0022 butir 18). Filter "permission modul nonaktif" (docs/06 §14) menunggu M07.
- **Keterbatasan cache (ADR-0022 butir 19–20), tidak ada jaminan absolut:** bila Redis gagal tepat antara bump sebelum dan sesudah commit, pencabutan bisa tertunda sampai TTL (2 menit); jam mundur lebih dari 1 ms dapat menabrakkan versi; tidak ada singleflight; TTL/timeout/backoff belum dapat diatur lewat konfigurasi.
- **Risiko operasional ADR-0022 butir 11:** API keluar `ExitFailed` bila sinkron katalog gagal saat start; retry terbatas dengan backoff BELUM dikerjakan (docs/10 §11/§15 menolak me-restart pod karena kegagalan database).
- `Bus` di `app/auth.go` masih instans sendiri (itu sebabnya invalidasi cache eksplisit). Pembuka kunci akun administratif ditunda ke M05 (sementara `redis-cli DEL auth:lock:<user_id>`); smoke M04 tidak boleh mengunci Super Admin smoke.
- Anggaran login smoke (`LoginRule` 10/menit/IP; m02+m03 ±14 login per run) sempit untuk `m04.sh`: gunakan sesi yang ada atau tunggu jendela berganti (pola `m03.sh`).
- **Flake Docker/jaringan laptop (kelas teramati, bukan kode):** reaper (`wait for reaper … context deadline exceeded`, dua run beruntun di paket berbeda); unduhan modul di container `migrate` (`unexpected EOF` dari `proxy.golang.org`); kelas lama pada prompt. `make up` tidak ada (pakai `make docker-up`).
- Utang lintas milestone yang masih berlaku: modul harus mencabut DML `app_user` pada `schema_migrations_<kode>` (M07); layanan group harus mencabut `role_assignments` ber-scope group saat group diarsipkan/dihapus (M05); `platforms.version` belum disinkronkan (M15); retensi `security_events` (M06/M15); CI `docker-smoke` belum menyiapkan Go (M15).

## 9. Pertanyaan terbuka
- Retry terbatas untuk sinkron saat start: commit kecil sebelum akhir M04 atau tunda ke M15 (usulan penulis: kerjakan setelah T4 bila konteks cukup, kalau tidak catat sebagai utang M15)?
- Perlukah pencabutan sesi (atau pemeriksaan ulang hak) saat assignment platform dicabut, sebelum M05? Usulan: di T5, saat endpoint pencabutan assignment dibuat, ikut mencabut sesi subjek bila yang dicabut adalah assignment platform.
- Rate limit penolakan `Authorize` per pengguna: sekarang atau M15?
- Pasang `kernel.Scope` di middleware `Scope` (T5) atau biarkan tiap handler?
- Apakah handover asli `partial-1` bisa ditemukan? Bila ya, bandingkan dengan Lampiran A partial-2.

## 10. Milestone berikutnya (lanjutan M04): T4 → T5
- **T4:** policy dan FieldGuard. Baca docs/06 §6 (policy), §7 (FieldGuard), dan ADR-0022 butir 2 dan 10. Migrasi **034** menambah `policies.actions text[]` (kode persis atau `resource.*`, tidak boleh kosong, divalidasi terhadap katalog); periksa nomor bebas sebelum membuat berkas. Sinkronkan tes ERD, roundtrip, docs/03, docs/04, `docs/erd.md`. Tes negatif wajib untuk evaluasi policy dan FieldGuard.
- **T5:** authority ceiling (docs/06 §5.3), endpoint (docs/06 §10–§12, §14), simulate, aturan assignment (ADR-0022 butir 9; `InvalidateInTx` dipakai di sini), `scripts/smoke/m04.sh` dengan fixture lewat `docker compose exec -T postgres psql`. Pada T5 juga: rakit middleware `authzhttp` di `internal/app` (tambah `Authorizer` ke `authzStack`, adaptor `ScopeResolver`, `Options.Authz` di `httpserver`), pasang `Scope` lalu `Authorize(action)` di rute pertama, putuskan §9 (sesi, `kernel.Scope`, rate limit).
- Tulis `HANDOVER-M04-partial-5.md` pada ◆CP berikutnya (atau lebih awal bila konteks hampir habis).

## 11. Prompt untuk chat berikutnya
Prompt lengkap (memuat ulang seluruh KETENTUAN CHECKPOINT KONTEKS dan ATURAN KERAS) ada di `docs/handover/PROMPT-M04-partial-5.md`; salinan yang sama diberikan pada akhir sesi ini sebagai berkas unduhan.
