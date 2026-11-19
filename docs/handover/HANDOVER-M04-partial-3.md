# HANDOVER — M04 Authorization Engine (PARSIAL 3)

**Tanggal:** 2026-11-01 · **Tag:** belum ada (`m04-done` dibuat di akhir milestone) · **Commit terakhir:** subjek `docs(m04): HANDOVER-M04-partial-3, ADR-0022 bagian 3, STATUS, CHANGELOG — checkpoint setelah T3b` (SHA laptop berbeda dari sandbox; subjek dan urutan yang berlaku)
**Jenis:** PARSIAL 3 · **Branch:** `milestone/m04-authorization` (basis: tag `m03-done` = `20e7fc0`) · **Handover sebelumnya:** `HANDOVER-M04-partial-2.md` (Lampiran A-nya memuat rekonstruksi `partial-1`, yang berkas aslinya tidak ada di repo).

## 1. Ringkasan
T3a (domain `Check`/`Decision`/`DecisionLevel`/`EffectivePermissions` + repositori effective permissions) dan T3b (cache versi di Redis, `Authorizer.Can`, `Syncer.WithInvalidator`) selesai dan terverifikasi di laptop, di-push (`66e0a60..cca3f48`, lalu `cca3f48..5d01ed2`).
Mesin keputusan sudah ada dan teruji dengan PostgreSQL 18 dan Redis 8 asli, tetapi **belum terpasang di mana pun**: tidak ada middleware `Authorize`, tidak ada rute yang memakainya, `cache.Cache`/`Authorizer` belum dirakit di `internal/app`, dan `Syncer` di `RunAPI` masih tanpa invalidator.
Belum ada: middleware `Authorize`, wiring app, `PlatformEntryChecker`, `switch-organization` identitas platform, kode error `scope_mismatch`/`permission_denied` di `httpx` (T3c, lalu ◆CP1); policy + FieldGuard + migrasi 034 (T4); authority ceiling, endpoint, simulate, smoke m04 (T5).
Keputusan pemecahan T3 (T3a/T3b/T3c) dari partial-2 §9 dijalankan dan terbukti masuk akal. T3c adalah task terbesar (menyentuh `app/`, `auth`, rute), sehingga checkpoint ini sengaja dibuat SEBELUM T3c.

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T1 katalog permission, role sistem, Syncer | DONE | partial-2 Lampiran A; ADR-0022 butir 5–9 |
| T1b Syncer di RunAPI | DONE | ADR-0022 butir 11 |
| T2 ScopeContext, ScopeResolver, middleware Scope | DONE | ADR-0022 butir 12–15 |
| T3a domain Check/Decision/EffectivePermissions + repositori effective permissions | DONE | tiga commit; ADR-0022 butir 16–18 |
| T3b cache versi + `Authorizer.Can` + `Syncer.WithInvalidator` | DONE | tiga commit; ADR-0022 butir 19–22 |
| T3b2 evaluasi scope `resource` dan `own` di `Can` | TODO (opsional) | keputusan §9; sekarang ditolak (gagal tertutup) |
| T3c middleware `Authorize`, wiring app, `PlatformEntryChecker`, switch-organization platform, kode error (◆CP1) | TODO | §10; ADR-0022 butir 23 |
| T4 policy + FieldGuard + migrasi **034** | TODO | ADR-0022 butir 2 dan 10 |
| T5 authority ceiling, endpoint, simulate, smoke m04 | TODO | §10 |

## 3. Bukti verifikasi (apa adanya)
- **Baseline awal sesi (laptop WSL2):** `go version go1.26.5 linux/amd64`, `gofmt -l backend` kosong (`gofmt-exit=0`); paket M04 (`authorization/...`, `permission`, `role`, `app`, `testkit`) sendirian: semua `ok` (application 30,7 dtk; domain 1,0; repository 19,4; transport/http 1,0; permission 1,1; role 1,0; app 2,2; testkit 21,2). Keadaan repo cocok dengan harapan (branch sejajar origin, 400 berkas, fsck bersih, tag `m03-done` = `20e7fc0`).
- **T3a (tiga commit; mbox diterapkan di laptop; di-push `66e0a60..cca3f48`):** `gofmt-exit=0`; `go vet ./backend/internal/authorization/... ./backend/internal/testkit/...` kosong; `REQUIRE_DOCKER=1 go test … -race -count=1 -p 1`:
  `authorization/application` ok 40,4 dtk, `domain` ok 1,0, `repository` ok 39,6, `transport/http` ok 1,0, `testkit` ok 17,9; `go tool golangci-lint run` pada paket yang sama: **`0 issues.`** Lolos pada percobaan pertama.
- **T3b (tiga commit; di-push `cca3f48..5d01ed2`):** percobaan PERTAMA tidak terkompilasi: `sync.go:10 "log/slog" imported and not used` dan `s.bump undefined`. Penyebab: **kesalahan saya**, penggantian teks lewat skrip di sandbox untuk definisi `Invalidator`/`Syncer`/`bump` diam-diam tidak cocok
  (teks komentar asli berbeda dari yang saya tebak) dan tidak diverifikasi; hanya impor dan dua pemanggil yang terpasang. Diperbaiki dengan meng-amend commit ketiga (`assert` bahwa teks asli ditemukan tepat sekali), mbox diregenerasi, semua penggantian skrip lain diverifikasi dengan grep.
  Di laptop sempat membingungkan dua kali: (1) `git am` versi baru belum dijalankan sehingga `vet` mengulang galat yang sama; (2) `git am` menolak karena sisa `.git/rebase-apply` (`previous rebase directory … still exists`), diselesaikan dengan `git am --quit` (HEAD tidak berubah).
  Hasil akhir sesudah `git am` yang benar (`grep -c 'func (s \*Syncer) bump'` = 1): `gofmt-exit=0`; `go vet ./backend/internal/authorization/...` kosong; `REQUIRE_DOCKER=1 go test ./backend/internal/authorization/... -race -count=1 -p 1`:
  `application` ok 30,8 dtk, **`cache` ok 15,1**, `domain` ok 1,0, `repository` ok 31,6, `transport/http` ok 1,0; `golangci-lint`: **`0 issues.`**; `git push` berhasil (`cca3f48..5d01ed2`).
- **TIDAK dijalankan sejak T1:** `make verify` utuh (T1b, T2, T3a, T3b semuanya belum). Paket `permission`, `role`, `app` tidak dijalankan ulang sesudah T3b (perubahan T3b pada `Syncer` hanya penambahan; `app` memanggil `NewDefaultSyncer`/`Run` yang tanda tangannya tidak berubah, tetapi itu belum terbukti oleh keluaran mesin). `testkit` dijalankan di T3a (ok), tidak sesudah T3b.
  Jangan menyebut hijau sebelum ada keluaran dari mesin pengguna. Dianjurkan satu run `make verify GO_TEST_PARALLEL=1` di awal sesi berikutnya.
- **Sandbox (bukan bukti utama):** Go dan gofmt tidak tersedia. Terbukti di sandbox: `bash scripts/lint-structure.sh` → `lint-structure OK`; `git am` berantai (zip → t3a → t3b → patch dokumen ini) menghasilkan pohon identik. Semua bukti kompilasi/gofmt/vet/lint/tes berasal dari laptop.
- **Tes negatif/keamanan baru:**
  - *Effective permissions (T3a, ber-DB lewat `app_user`):* tanpa assignment; user tak dikenal; assignment tidak berpindah antar user; dicabut; kedaluwarsa (dan belum kedaluwarsa berlaku); role `archived`; assignment organisasi B tidak berlaku di A walau user anggota keduanya; `scope_id` organisasi lain; role milik organisasi lain;
    scope grup hanya untuk grup aktifnya sendiri, grup `archived`/soft-deleted, `scope_id` grup organisasi lain (scope dipalsukan); platform berlaku tanpa organisasi/grup, identitas platform tanpa keanggotaan hanya mendapat grant platform; assignment organisasi tidak berlaku di scope platform;
    **platform hanya untuk subjek `user`** (grup/organisasi/service diabaikan); subjek grup hanya diwarisi anggota `active` (invited/suspended/removed tidak), tidak dari grup `archived`, non-anggota, atau grup organisasi lain; subjek organisasi hanya untuk anggota aktif;
    keanggotaan organisasi non-aktif kehilangan grant tenant tetapi bukan grant platform; organisasi `suspended`/`archived`/soft-deleted tanpa grant tenant; level terluas menang; scope `resource`/`own` bukan bagian himpunan; subjek `service` tidak pernah berlaku untuk user;
    perubahan role_permissions terlihat pada pembacaan berikutnya; scope tidak sah (UUID nol, grup tanpa organisasi) = `ErrInvalidScope`; user nol = galat; database gagal (ctx dibatalkan) = galat, bukan himpunan kosong.
  - *Cache (T3b, Redis asli + hook go-redis untuk menyuntik galat):* isolasi per user/grup/organisasi/scope platform; bump organisasi hanya menginvalidasi organisasinya; bump platform menginvalidasi semuanya; hasil yang dihitung SEBELUM bump tidak pernah dibaca sesudahnya; versi dimulai dari jam dan naik 1;
    kunci versi yang hilang tidak menghidupkan entri lama; bump pada kunci hilang dimulai di atas jam; 9 bentuk nilai rusak/asing = miss yang ditimpa; 4 bentuk versi rusak = lewati cache tanpa menulis; Redis gagal = tidak ada jawaban dan tidak ada tulisan; backoff lalu pulih; bump gagal TIDAK ikut backoff;
    `InvalidateInTx`: bump sebelum dan sesudah commit, rollback hanya menyisakan bump sebelum commit, bump sebelum commit gagal = transaksi dibatalkan, dedup/UUID nol, tanpa target = no-op.
  - *Authorizer (unit + integrasi):* tanpa/izin salah = `no_permission` dengan level nol; himpunan kosong menolak semuanya; izin level group hanya untuk sumber daya di grup aktif (grup lain, tanpa grup = ditolak), level organisasi mencakup grup mana pun;
    sumber daya organisasi lain = `scope_mismatch` TANPA menyentuh cache/database (juga untuk level platform); sumber daya tanpa organisasi di scope tenant, sumber daya tenant di scope platform, Identity-vs-scope tidak cocok (3 bentuk) = `scope_mismatch`;
    5 bentuk `Check` salah bentuk = `ErrInvalidCheck`; scope tidak sah = `ErrInvalidScope`; galat loader = galat dan tidak di-cache; hit dilayani dari cache; cache tak tersedia = hitung dari database dan tidak menulis; tanpa cache berfungsi.
    **Integrasi (PostgreSQL + Redis nyata):** pencabutan di transaksi dengan `InvalidateInTx` berlaku pada request berikutnya walau cache hangat; tanpa bump cache masih dilayani (kontrol negatif); perubahan role + bump platform; rollback tidak mengubah jawaban; **Redis mati (alamat yang menolak koneksi) tetap benar dari PostgreSQL**; ctx dibatalkan = galat.
  - *Syncer:* bump platform dua kali hanya bila `Changed()`, tidak sama sekali pada rollback, sinkron tetap sukses bila invalidator gagal, tanpa invalidator tetap berfungsi.
- **Kendala mesin (laptop, Docker Desktop ±4,9 GB):** TIDAK ada flake Docker pada run T3a dan T3b (semua `ok` pada percobaan pertama setelah kode benar). Prosedur lama tetap berlaku: baca pesan, jalankan paket sendirian, jangan ubah kode. `make up` TIDAK ada (pakai `make docker-up`).

## 4. Keputusan & penyimpangan
- **ADR-0022 bagian 3 (butir 16–23)**, ringkas: 16 platform hanya subjek `user`; 17 cabang tenant lebih ketat dari docs/06 §5.2 (organisasi aktif, anggota aktif, role global atau se-organisasi, grup scope eksak dan aktif, subjek terbatas); 18 himpunan hanya platform/organization/group dengan level terluas, `resource`/`own` belum dievaluasi (ditolak);
  19 desain cache (kunci, versi dari jam, skrip bump Lua, TTL 2 menit, timeout 250 ms, backoff 1 dtk, nilai divalidasi); 20 `InvalidateInTx` (bump sebelum commit gagal = batal, sesudah commit gagal = log, dibatasi TTL); 21 `Syncer.WithInvalidator` (kegagalan hanya di-log; belum terpasang di RunAPI);
  22 `Can(ctx, Check)` mengikuti docs/06 §9 (bukan tiga parameter di prompt), urutan pemeriksaan, `ErrInvalidCheck`/`ErrInvalidScope` adalah galat, `scope_mismatch` sebelum menyentuh penyimpanan; 23 pekerjaan T3c yang diwariskan.
- **Penyimpangan dari dokumen 01–20:** docs/06 §5.2 diketatkan (butir 16–17); `Can` memakai satu parameter `Check` (sesuai docs/06 §9; prompt dan partial-2 §10 menulis tiga parameter, ini koreksi terhadap prompt, bukan terhadap docs); TTL cache 2 menit (docs/06 §8.1: "beberapa menit"); versi penghitung dari jam (docs/06 §8.1 tidak menyebut).
- **Doc-sync M04 TERTUNDA (kumulatif; dasar: partial-2 §4 + ADR-0022 bagian 3):**
  docs/06 (§4.1 jalur platform, §4.2 header dibuang/bentuk kanonik/pengetatan, §5.2 platform hanya subjek user + pengetatan + level terluas + resource/own belum, §5.5 slug role, §6 pengikat policy, §8.1 invalidasi eksplisit, versi pasangan, versi dari jam, TTL, skrip bump, perilaku bump gagal, §9 galat vs keputusan dan urutan `Can`, §13 struktur paket nyata, §14 tes yang sudah/belum terpenuhi);
  docs/03 + docs/04 + `docs/erd.md` (kolom `policies.actions`, migrasi **034**); docs/05 (switch-organization untuk identitas platform); docs/00 B-04 (katalog dan role sinkron sebagai dibangun);
  docs/10 §3 (urutan start API: sinkron katalog sebelum melayani request; gagal start tanpa DB) dan §11/§15 (invalidasi gagal tidak mematikan start); docs/18 §7 (fixture grup; fixture role/permission/assignment di `testkit`); ADR-0020 butir 28 (dilanjutkan ADR-0022 butir 1).
- **Belum tergabung (berkas tidak ada):** `HANDOVER-M03-partial-4.md` §4 dan `HANDOVER-M04-partial-1.md` §4 asli.

## 5. Delta kontrak (sejauh ini di M04)
- **DB:** tidak ada migrasi baru. Migrasi `034_policies_actions` (T4) akan menambah `policies.actions text[]`; periksa lagi nomor bebas sebelum membuat berkas (001–033 terpakai).
- **API:** belum ada endpoint baru. Header `X-Active-Group-ID` (T2) belum terpasang di rute mana pun.
- **Redis (baru, T3b; belum ada pemakai di runtime):** `authz:ver:<organization_id>` dan `authz:ver:platform` (bilangan bulat, tanpa TTL); `authz:eff:<org|->:<verOrg>:<verPlatform>:<user>:<grup|->` (JSON `{"v":1,"g":{kode:level}}`, TTL 2 menit).
- **Startup API:** tidak berubah sejak partial-2 (Syncer di `RunAPI` TANPA invalidator).
- **Activity/event/outbox:** tidak ada yang baru. **Kode error:** `scope_mismatch` dan `permission_denied` BELUM didaftarkan di `httpx` (T3c).
- **Config/env:** tidak ada baru (TTL, timeout, backoff adalah konstanta/`cache.Options`; belum dapat diatur lewat env; lihat §9).
- **Antarmuka Go:** `authorization/domain` (`Check`, `ResourceRef`, `Decision`, `DecisionLevel`, `Reason*`, `EffectivePermissions`, `EffectiveBuilder`, `EffectiveFromGrants`, `ErrInvalidGrant`); `authorization/application` (`Authorizer`, `NewAuthorizer(loader, cache)`, `Can`, `ErrInvalidCheck`, `EffectiveLoader`, `EffectiveCache`, `Invalidator`, `Syncer.WithInvalidator`);
  `authorization/cache` (`Cache`, `Options`, `Lookup`, `Targets`, `New`, `Lookup`/`Store`/`BumpOrganization`/`BumpPlatform`/`InvalidateInTx`); `authorization/repository` (`Effective.Load`, `NewEffective`).

## 6. Peta kode (tambahan sejak partial-2)
- `backend/internal/authorization/domain/` — `check.go` (`Check`, `ResourceRef`, `Decision`, `DecisionLevel`, `Reason*`), `effective.go` (`EffectivePermissions`, `EffectiveBuilder`, `EffectiveFromGrants`), `effective_test.go`.
- `backend/internal/authorization/repository/` — `effective.go` (`Effective.Load`, `effectiveSQL` satu kueri), `effective_test.go` (ber-DB; helper `world.load/loadOrg/assignOrg`, `ptr`).
- `backend/internal/authorization/cache/` — `cache.go`, `cache_test.go` (rig Redis + `faultHook`, jam palsu), `main_test.go` (`testkit.Run`).
- `backend/internal/authorization/application/` — `authorizer.go`, `authorizer_test.go` (fake loader/cache), `authorizer_integration_test.go` (PostgreSQL + Redis); `sync.go` (+ `Invalidator`, `WithInvalidator`, `bump`), `sync_test.go` (+ `recordingInvalidator`).
- `backend/internal/testkit/authz_fixtures.go` — `NewRole`, `RoleInOrganization`, `RoleStatus`, `EnsurePermission`, `GrantPermission`, `RoleWith`, `AssignRole`, `AssignmentSpec`.

## 7. Cara menjalankan & memverifikasi
- Paket M04: `REQUIRE_DOCKER=1 go test ./backend/internal/authorization/... ./backend/internal/permission ./backend/internal/role ./backend/internal/app/... ./backend/internal/testkit/... -race -count=1 -p 1`.
- Lint: `gofmt -l backend; go vet <paket> && go tool golangci-lint run <paket>`; `bash scripts/lint-structure.sh`.
- Penuh: `gofmt -l backend; make verify GO_TEST_PARALLEL=1 > ~/verify.log 2>&1; echo "exit=$?"`. Flake Docker: lihat §3 dan prompt.
- Sesudah `git am`: verifikasi penanda (`git log --oneline -N`, grep fungsi kunci) SEBELUM menjalankan tes; bila `git am` menolak karena `.git/rebase-apply`, pakai `git am --quit`.

## 8. Masalah diketahui / utang teknis
- **`make verify` utuh belum dijalankan sejak T1** (§3); `app`, `permission`, `role`, `testkit` belum dijalankan ulang sesudah T3b.
- **Engine belum terpasang:** `cache.Cache`, `Authorizer`, `Syncer.WithInvalidator` belum dirakit di `internal/app`; `InvalidateInTx` belum punya pemanggil (endpoint pengubah di T5). Sampai T3c, tidak ada jalur request yang memakai engine.
- **Scope `resource` dan `own` belum dievaluasi** (ditolak, gagal tertutup; ADR-0022 butir 18). **Filter "permission modul nonaktif"** (docs/06 §14) belum ada (butuh enablement modul, M07).
- **Keterbatasan cache (ADR-0022 butir 19–20), tidak ada jaminan absolut:** bila Redis gagal tepat antara bump sebelum dan sesudah commit, pencabutan bisa tertunda sampai TTL (2 menit); jam mundur lebih dari 1 ms dapat menabrakkan versi; tidak ada singleflight (banyak request bersamaan pada miss yang sama sama-sama menghitung dari PostgreSQL);
  TTL/timeout/backoff belum dapat diatur lewat konfigurasi.
- **Risiko operasional ADR-0022 butir 11:** API keluar `ExitFailed` bila sinkron katalog gagal saat start; retry terbatas dengan backoff diusulkan, BELUM dikerjakan (docs/10 §11/§15). Perlu keputusan sebelum M15.
- `domain.ErrInvalidScope` dari resolver saat ini menjadi 503 lewat middleware `Scope`; usulan: 500 untuk `ErrInvalidCheck`/`ErrInvalidScope` (kesalahan pemrograman), 503 untuk galat infrastruktur; diputuskan di T3c.
- `Bus` di `app/auth.go` masih instans sendiri (itu sebabnya invalidasi cache eksplisit). Pembuka kunci akun administratif ditunda ke M05 (sementara `redis-cli DEL auth:lock:<user_id>`); smoke M04 tidak boleh mengunci Super Admin smoke.
- Anggaran login smoke (`LoginRule` 10/menit/IP; m02+m03 ±14 login per run) sempit untuk `m04.sh`: gunakan sesi yang ada atau tunggu jendela berganti (pola `m03.sh`).
- Flake Docker laptop (kelas lama); `make up` tidak ada (pakai `make docker-up`).
- Utang lintas milestone yang masih berlaku: modul harus mencabut DML `app_user` pada `schema_migrations_<kode>` (M07); layanan group harus mencabut `role_assignments` ber-scope group saat group diarsipkan/dihapus (M05); `platforms.version` belum disinkronkan (M15); retensi `security_events` (M06/M15); CI `docker-smoke` belum menyiapkan Go (M15).

## 9. Pertanyaan terbuka
- **T3b2 (scope `resource`/`own` di `Can`):** kerjakan sekarang sebagai task kecil sebelum ◆CP1, atau tunda sampai ada sumber daya ber-pemilik (usulan penulis: tunda ke milestone pertama yang membutuhkannya, dan catat; engine sudah gagal tertutup)?
- Retry terbatas untuk sinkron saat start: commit kecil sebelum akhir M04 atau tunda ke M15?
- `Authorize` pada galat engine: usulan 503 untuk galat infrastruktur (PostgreSQL), 500 untuk `ErrInvalidCheck`/`ErrInvalidScope`; setuju?
- Apakah TTL/timeout/backoff cache perlu env var (`AUTHZ_CACHE_TTL` dst.) di T3c, atau cukup konstanta sampai M15?
- Apakah handover asli `partial-1` bisa ditemukan? Bila ya, bandingkan dengan Lampiran A partial-2.

## 10. Milestone berikutnya (lanjutan M04): T3c → T5
- **T3c (◆CP1):** baca docs/06 §9 (audit DENY dalam transaksinya sendiri), §12 (kode error), docs/05 (switch-organization), dan kode `auth` untuk switch-organization (service, handler, tes). Bangun, per commit kecil:
  (1) daftarkan `permission_denied` (403) dan `scope_mismatch` di `httpx` bila belum ada; (2) middleware `authzhttp.Authorize(action)` sesudah `Scope`: `Decision` ditolak = 403 `permission_denied` seragam (`scope_mismatch` = kode sendiri), galat `Can` = 503/500 sesuai keputusan §9, tanpa Identity/Scope = 401/fail-closed, audit DENY;
  (3) rakit `cache.New(redis)`, `Authorizer`, dan `Syncer.WithInvalidator` di `internal/app` (Redis nil/tidak ada = tanpa cache; `Authorizer` tetap bekerja); (4) antarmuka `PlatformEntryChecker` di paket `auth` (tanpa impor `authorization`) + implementasi di `authorization` (`Can` dengan `Check{Identity platform, Scope kosong, Action organization.read}` dan organisasi tujuan `active`),
  lalu `switch-organization` untuk identitas platform (ADR-0022 butir 1: 403 `permission_denied` seragam, event `auth.organization.switched` dengan `via=platform`, `Decision.Level` tetap platform). Tes negatif wajib: identitas platform tanpa izin, organisasi `suspended`/`archived`/tidak ada = respons yang sama, identitas non-platform tidak terpengaruh, galat engine tidak pernah melonggarkan.
  **Akhiri dengan ◆CP1:** satu run `make verify GO_TEST_PARALLEL=1` utuh (atau bukti pengganti) dan handover partial-4.
- **T4:** policy (`policies.actions`, migrasi **034**), `FieldGuard`; sinkronkan tes ERD, roundtrip, docs/03, docs/04, `docs/erd.md`. **T5:** authority ceiling (docs/06 §5.3), endpoint (docs/06 §10–§12, §14), simulate, `scripts/smoke/m04.sh` dengan fixture lewat `docker compose exec -T postgres psql`,
  aturan assignment (ADR-0022 butir 9; `InvalidateInTx` dipakai di sini).
- Tulis `HANDOVER-M04-partial-4.md` pada ◆CP berikutnya (atau lebih awal bila konteks hampir habis).

## 11. Prompt untuk chat berikutnya
Prompt lengkap (memuat ulang seluruh KETENTUAN CHECKPOINT KONTEKS dan ATURAN KERAS) ada di `docs/handover/PROMPT-M04-partial-4.md`; salinan yang sama diberikan pada akhir sesi ini sebagai berkas unduhan.
