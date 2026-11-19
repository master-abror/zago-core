# HANDOVER — M04 Authorization Engine (PARSIAL 2)

**Tanggal:** 2026-10-31 · **Tag:** belum ada (`m04-done` dibuat di akhir milestone) · **Commit terakhir:** subjek `docs(m04): HANDOVER-M04-partial-2, ADR-0022 bagian 2, STATUS, CHANGELOG — checkpoint setelah T2` (SHA laptop berbeda dari sandbox; subjek dan urutan yang berlaku)
**Jenis:** PARSIAL 2 · **Branch:** `milestone/m04-authorization` (basis: tag `m03-done` = `20e7fc0`) · **Handover sebelumnya:** `HANDOVER-M04-partial-1.md` **TIDAK ADA di repo** (patch dokumen checkpoint T1 tidak pernah diterapkan di laptop; zip sesi ini sudah tanpa berkas itu). Isinya direkonstruksi di **Lampiran A** dari kode T1, ADR-0022 bagian 1, dan prompt sesi. Bila pengguna menemukan berkas aslinya, bandingkan §4 dan §10-nya dengan Lampiran A dan gabungkan selisihnya.

## 1. Ringkasan
T1 (katalog permission 41 kode, 4 role sistem, `Syncer`), T1b (Syncer terpasang di `RunAPI`, terbukti di stack nyata), dan T2 (`ScopeContext`, `ScopeResolver`, verifikasi grup aktif ke PostgreSQL, middleware `Scope`) selesai dan terverifikasi di laptop.
Stack berjalan kini punya role `super-admin` dengan 41 permission pada start pertama. Belum ada: effective permissions, cache versi, `Authorizer.Can`, middleware `Authorize` (T3 ◆CP1), policy + FieldGuard + migrasi (T4), authority ceiling, endpoint, simulate, smoke m04 (T5).
Middleware `Scope` belum dipasang di rute mana pun (belum ada rute yang membutuhkannya).

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T1 katalog permission, role sistem, Syncer | DONE | Lampiran A; ADR-0022 butir 5–9 |
| T1b pasang Syncer ke RunAPI + tes `app` | DONE | commit `feat(app)` + `fix(app)` (S1016); ADR-0022 butir 11; bukti stack nyata §3 |
| T2 ScopeContext | DONE | tiga commit `feat(authorization)`; ADR-0022 butir 12–15 |
| T3 effective permissions, cache versi, `Authorizer.Can`, `Authorize` (◆CP1) | TODO | §10 |
| T4 policy + FieldGuard + migrasi **034** (bukan 021) | TODO | ADR-0022 butir 10 |
| T5 authority ceiling, endpoint, simulate, smoke m04 | TODO | §10 |

## 3. Bukti verifikasi (apa adanya)
- **Baseline (laptop WSL2, port 25000–25005, Go 1.26.5 menurut prompt; keluaran `go version` TIDAK ditempel):** pada commit T1 (`feat(authorization): Syncer …`, `8744d46`) satu run
  `gofmt -l backend; make verify GO_TEST_PARALLEL=1` → `gofmt` kosong, **`exit=0`**; `grep` menunjukkan `bash ./scripts/verify-smoke.sh`, `M00 smoke OK`, `M02 smoke OK`, `M03 smoke OK`. Ini run `make verify` utuh hijau PERTAMA di laptop ini.
  Perintah baseline 3b (paket T1 sendirian) tidak ditempel; hijaunya disimpulkan dari `exit=0` run di atas.
- **T1b (commit `feat(app)` `ee7c201`, `fix(app)` `c132bbe`; sudah di-push, `8744d46..c132bbe`):**
  - Run pertama lint merah: `S1016` (staticcheck) di tes (`errRow{err: p.err}` → konversi `errRow(p)`); diperbaiki commit terpisah. Setelahnya: `0 issues.`, `ok …/internal/app 2.565s`.
  - **Stack nyata** (compose project terpisah `platform-t1b`, `docker compose --profile full up -d --build`, sudah dibongkar `down -v`): log API `katalog otorisasi disinkronkan` dengan `permissions_created=41`, `roles_created=4`, `role_permissions_added=103`
    (`group-admin` 20 + `member` 8 + `organization-admin` 34 + `super-admin` 41 = 103; query psql cocok). Setelah `docker compose restart api`: `katalog otorisasi sudah sinkron` dan `count(activities where action='authz.catalog_synced') = 1` (idempoten).
    `find . -path ./.git -prune -o -user root -print` kosong (tidak ada berkas root). Stack naik sekali dalam ±194 dtk (migrate 175 dtk).
- **T2 (tiga commit `feat(authorization)`; mbox diterapkan di laptop):** `REQUIRE_DOCKER=1 go test ./backend/internal/authorization/... ./backend/internal/testkit/... -race -count=1 -p 1`:
  `authorization/application` ok 30,2 dtk, `domain` ok 1,0 dtk, `repository` ok 18,9 dtk, `transport/http` ok 1,0 dtk. Paket `testkit` MERAH pada run gabungan itu dengan pesan kelas flake Docker
  (`create container: reaper: from container …: wait for reaper …: context deadline exceeded`; tes pertama gagal ±61 dtk, dua lainnya 0,00 dtk; Redis di run yang sama normal). Diulang sendirian: **`ok …/testkit 23.606s`**. Kode tidak diubah.
  `gofmt -l backend` kosong; `go vet` dan `go tool golangci-lint run` pada `./backend/internal/authorization/... ./backend/internal/testkit/...`: **`0 issues.`**
  **Push T2:** keluaran `git push` TIDAK ditempel di chat; konfirmasi lewat langkah `git status -sb` pada checkpoint ini.
- **`make verify` utuh SESUDAH T1b dan T2: BELUM dijalankan ulang.** Jangan menyebut hijau sebelum ada keluaran dari mesin pengguna.
- **Sandbox (bukan bukti utama):** Go dan gofmt tidak tersedia. Terbukti di sandbox: `bash scripts/lint-structure.sh` → `lint-structure OK`; `git am` berantai (zip → T1b → fix → T2 → patch dokumen ini) menghasilkan pohon identik.
  Format Go ditulis tangan; semua bukti kompilasi/gofmt/lint/tes berasal dari mesin pengguna di atas.
- **Tes negatif/keamanan baru (T1b/T2):** galat `Begin` Syncer dibungkus dan tak ada log sukses; `RunAPI` dengan pool yang selalu gagal = `ExitFailed` tanpa menyentuh listener dan semua dependensi tertutup; orphan = WARN;
  `ScopeContext` menolak UUID nol dan grup tanpa organisasi serta tidak berubah lewat pointer; 10 bentuk header cacat tidak pernah sampai ke database; header ganda; identitas platform mengabaikan header; galat DB = error (bukan penolakan);
  status keanggotaan grup `invited`/`suspended`/`removed`, grup `archived`/soft-deleted, keanggotaan organisasi `suspended`/`removed`, grup organisasi lain walau user anggota di sana, keanggotaan tidak berpindah antar user, ID tak dikenal;
  scope grup A tidak terbawa saat header = grup B; pencabutan keanggotaan berlaku pada request berikutnya di sesi yang sama; middleware tanpa Identity 401 dan galat resolusi 503 tanpa membocorkan detail infrastruktur.
- **Kendala mesin (laptop, Docker Desktop ±4,9 GB):** flake Docker kelas lama muncul sekali (di atas). Prosedur: baca pesan, jalankan paket sendirian, jangan ubah kode. `make up` TIDAK ada; target compose: `make docker-up` / `make docker-down`.
  Untuk bukti stack manual gunakan project terpisah: `COMPOSE_PROJECT_NAME=platform-t1b docker compose --profile full up -d --build && COMPOSE_PROJECT_NAME=platform-t1b bash ./scripts/wait-for-healthy.sh`, bongkar dengan `… down -v --remove-orphans`.

## 4. Keputusan & penyimpangan
- **ADR-0022 bagian 1 (butir 1–9)** dan **bagian 2 (butir 10–15)**:
  1 identitas platform memasuki organisasi lewat `switch-organization` bila engine memutuskan `organization.read` pada scope platform (hanya organisasi `active`, 403 `permission_denied` seragam, event `auth.organization.switched` membawa `via=platform`, `Decision.Level` tetap platform, antarmuka `PlatformEntryChecker` di paket `auth`);
  2 policy terikat ke action lewat `policies.actions text[]` (kode persis atau `resource.*`, tidak boleh kosong, divalidasi terhadap katalog) — **migrasi 034**; 3 invalidasi cache eksplisit di dalam transaksi pengubah (sebelum dan sesudah commit), versi kunci = pasangan (versi organisasi, versi platform), role sistem berubah = bump platform, `SyncResult.Changed()` memicu bump platform;
  4 pembuka kunci akun administratif ditunda ke M05 (`redis-cli DEL auth:lock:<user_id>`); 5 katalog 41 kode; 6 role sistem; 7 semantik Syncer; 8 struktur paket; 9 aturan assignment (T5);
  10 koreksi nomor migrasi (034); 11 Syncer di `RunAPI`; 12 header `X-Active-Group-ID` dibuang bila tidak sah; 13 verifikasi grup lebih ketat dari docs/06 §4.2; 14 kontrak middleware `Scope`; 15 struktur paket nyata.
- **Penyimpangan dari dokumen 01–20:** header grup yang gagal verifikasi dibuang (bukan 400/403) — sesuai docs/06 §4.2 — tetapi hanya bentuk UUID kanonik diterima; keanggotaan organisasi user harus `active` pada verifikasi grup (lebih ketat dari docs/06 §4.2);
  API gagal start bila database tak terjangkau saat startup (komentar `kernel/pool.go` dan `app.Dependencies`: database belum siap hanya dilaporkan `/health/ready`; docs/10 §11 menegaskan alasannya: kegagalan database tidak boleh me-restart semua pod sekaligus, dan §15: permintaan yang butuh PostgreSQL gagal 503) — keputusan pemilik proyek "galat sinkron = ExitFailed", RISIKO TERBUKA di §8; nomor migrasi 034, bukan 021 (ADR-0022 butir 2 semula).
- **Doc-sync M04 TERTUNDA (kumulatif; dasar: ADR-0022 bagian 1 "Konsekuensi", prompt sesi, dan butir baru; `HANDOVER-M04-partial-1.md` §4 asli tidak ada):**
  docs/06 (§4.1 jalur platform, §4.2 header dibuang/bentuk kanonik/pengetatan, §5.5 slug role, §6 pengikat policy, §8.1 invalidasi eksplisit dan versi pasangan, §13 struktur paket nyata);
  docs/03 + docs/04 + `docs/erd.md` (kolom `policies.actions`, migrasi **034**); docs/05 (switch-organization untuk identitas platform); docs/00 B-04 (katalog dan role sinkron sebagai dibangun);
  docs/10 §3 (urutan start API: sinkron katalog sebelum melayani request; gagal start tanpa DB); docs/18 §7 (fixture grup); ADR-0020 butir 28 (catatan: dilanjutkan ADR-0022 butir 1).
- **Belum tergabung (berkas tidak ada):** `HANDOVER-M03-partial-4.md` §4 dan `HANDOVER-M04-partial-1.md` §4 asli.

## 5. Delta kontrak (sejauh ini di M04)
- **DB:** tidak ada migrasi baru. Migrasi `034_policies_actions` (T4) akan menambah `policies.actions text[]`. Nomor bebas harus diperiksa lagi sebelum berkas dibuat (001–033 terpakai).
- **API:** belum ada endpoint baru. Header request baru: `X-Active-Group-ID` (petunjuk, diverifikasi per request oleh middleware `Scope`; belum terpasang di rute).
- **Startup API:** sinkron katalog permission + role sistem sebelum `AttachSystemLog`/audit/auth; galat = `ExitFailed`; log `katalog otorisasi disinkronkan` (hitungan) / `sudah sinkron` / WARN orphan.
- **Activity:** `authz.catalog_synced` (hanya bila ada perubahan). **Event keamanan/outbox/Redis:** tidak ada yang baru.
- **Kode error:** `scope_mismatch` BELUM didaftarkan (T3). 401 `authentication_required` dan 503 `service_unavailable` memakai kode yang sudah ada.
- **Config/env:** tidak ada baru.
- **Permission:** 41 kode core (docs/06 §5.4). **Role sistem:** `super-admin` (semua permission core is_system, 41), `organization-admin` (34), `group-admin` (20), `member` (8) — angka dari stack nyata.

## 6. Peta kode (tambahan di M04)
- `backend/internal/permission/` — `catalog.go` (41 kode), validasi kode, `sync.go` (sinkron tanpa hapus, `SyncResult`) + tes.
- `backend/internal/role/` — `system.go` (`SystemRoles()`, `SlugSuperAdmin|OrgAdmin|GroupAdmin|Member`), `sync.go` (konvergensi set core, `SyncResult`) + tes.
- `backend/internal/authorization/application/` — `sync.go` (`Syncer`, `NewDefaultSyncer`, `Run(ctx, Beginner)`, `SyncResult.Changed()`), `scope.go` (`ScopeResolver`, `GroupMembershipChecker`), `main_test.go`, `sync_test.go`, `scope_test.go` (unit), `scope_db_test.go` (PostgreSQL).
- `backend/internal/authorization/domain/` — `scope.go` (`ScopeContext`, `NewScope`, `WithScope`, `ScopeFromContext`, `ErrInvalidScope`) + tes.
- `backend/internal/authorization/repository/` — `groups.go` (`Groups.IsActiveGroupMember`, satu kueri `EXISTS`), `main_test.go`, `groups_test.go`.
- `backend/internal/authorization/transport/http/` — `middleware.go` (paket `authzhttp`; `HeaderActiveGroup`, `Middleware.Scope`), `middleware_test.go`.
- `backend/internal/app/authz.go` — `syncAuthorization`, `logSync`; pemasangan di `app.go` (`RunAPI`); tes `authz_internal_test.go`, `authz_api_test.go` (`poolDeps` membungkus `fakeDeps`).
- `backend/internal/testkit/fixtures.go` — `AddOrganizationMember`, `NewGroup`, `AddGroupMember`.

## 7. Cara menjalankan & memverifikasi
- Paket M04: `REQUIRE_DOCKER=1 go test ./backend/internal/authorization/... ./backend/internal/permission ./backend/internal/role ./backend/internal/app/... ./backend/internal/testkit/... -race -count=1 -p 1`.
- Lint: `gofmt -l backend; go vet <paket> && go tool golangci-lint run <paket>`; `bash scripts/lint-structure.sh`.
- Penuh: `gofmt -l backend; make verify GO_TEST_PARALLEL=1 > ~/verify.log 2>&1; echo "exit=$?"`. Flake Docker: lihat §3 dan prompt.
- Stack manual: lihat §3 (project terpisah `platform-t1b`).

## 8. Masalah diketahui / utang teknis
- `make verify` utuh belum dijalankan setelah T1b/T2 (§3).
- **Risiko operasional ADR-0022 butir 11:** API keluar `ExitFailed` bila sinkron gagal saat start. Pada failover database yang lambat, pod yang baru start akan crash-loop (bertentangan dengan semangat docs/10 §11 dan §15). Aman di compose (API menunggu `migrate`), tetapi belum dinilai untuk deployment lain.
  Mitigasi yang diusulkan, BELUM dikerjakan: retry terbatas dengan backoff (mis. sampai 30–60 dtk, mengikuti ctx) sebelum `ExitFailed`, dengan log tiap percobaan. Perlu keputusan sebelum M15; catat di docs/10 §3 bersama doc-sync.
- `domain.ErrInvalidScope` dari resolver (Identity dengan organisasi UUID nol, mestinya mustahil) saat ini menjadi 503 lewat middleware; bila ingin dibedakan (500), putuskan di T3.
- Middleware `Scope` belum dipasang dan `scope_mismatch` belum didaftarkan (T3).
- `Bus` di `app/auth.go` masih instans sendiri (itu sebabnya invalidasi cache eksplisit, ADR-0022 butir 3).
- Pembuka kunci akun administratif ditunda ke M05 (sementara `redis-cli DEL auth:lock:<user_id>`). Smoke M04 tidak boleh mengunci Super Admin smoke.
- Anggaran login smoke (`LoginRule` 10/menit/IP; m02+m03 ±14 login per run) sempit untuk `m04.sh`: gunakan sesi yang ada atau tunggu jendela berganti (pola `m03.sh`).
- Flake Docker laptop (kelas lama); `make up` tidak ada (pakai `make docker-up`).
- Utang lintas milestone yang masih berlaku: modul harus mencabut DML `app_user` pada `schema_migrations_<kode>` (M07); layanan group harus mencabut `role_assignments` ber-scope group saat group diarsipkan/dihapus (M05); `platforms.version` belum disinkronkan (M15); retensi `security_events` (M06/M15); CI `docker-smoke` belum menyiapkan Go (M15).

## 9. Pertanyaan terbuka
- Apakah handover asli `partial-1` bisa ditemukan (riwayat chat sebelumnya)? Bila ya, bandingkan dengan Lampiran A.
- T3 besar: pecah menjadi T3a (domain `Check`/`Decision` + repositori effective permissions), T3b (cache versi + `Authorizer.Can`), T3c (middleware `Authorize` + wiring app + `PlatformEntryChecker` + `scope_mismatch`)? Usulan: ya, dengan ◆CP1 setelah T3c.
- Retry terbatas untuk sinkron saat start (§8, risiko crash-loop) — kerjakan sebagai commit kecil sebelum akhir M04, atau tunda ke M15?
- Perilaku `Authorize` saat Redis mati (fail-safe docs/06 §8.2) dan kebijakan `Decision` untuk identitas platform: ikuti docs/06 §8–§9 dan ADR-0022 butir 1; bila ada celah, jadikan butir ADR-0022 bagian 3.

## 10. Milestone berikutnya (lanjutan M04): T3 → T5
- **T3 (◆CP1):** baca docs/06 §5.2, §8, §9 dan docs/07 §7, 12 §4.6/§6. Bangun effective permissions (role assignment pada scope platform/organisasi/grup; gabungan), cache versi (kunci memuat pasangan versi organisasi + versi platform; invalidasi eksplisit di dalam transaksi pengubah, sebelum dan sesudah commit; `SyncResult.Changed()` → bump platform),
  `Authorizer.Can(ctx, Identity, ScopeContext, Check) (Decision, error)`, middleware `Authorize(action)` (sesudah `Scope`), antarmuka `PlatformEntryChecker` di paket `auth` (diimplementasikan `authorization`; tanpa impor melingkar) dan perubahan `switch-organization` untuk identitas platform (ADR-0022 butir 1), daftarkan `scope_mismatch`/`permission_denied`.
  Tes negatif wajib: tanpa assignment = ditolak; assignment organisasi lain; grup tidak terverifikasi; role sistem berubah = bump platform; cache basi setelah pencabutan; Redis mati; identitas platform vs organisasi tidak `active`.
- **T4:** policy (`policies.actions`, migrasi **034**), `FieldGuard`; sinkronkan tes ERD, roundtrip, docs/03, docs/04, `docs/erd.md`. **T5:** authority ceiling (docs/06 §5.3), endpoint (docs/06 §10–§12, §14), simulate, `scripts/smoke/m04.sh` dengan fixture lewat `docker compose exec -T postgres psql` (user kedua via `bootstrap-admin`, assignment super-admin-nya dicabut lewat endpoint M04), aturan assignment (ADR-0022 butir 9).
- Tulis `HANDOVER-M04-partial-3.md` pada ◆CP berikutnya (atau lebih awal bila konteks hampir habis).

## 11. Prompt untuk chat berikutnya
Prompt lengkap (memuat ulang seluruh KETENTUAN CHECKPOINT KONTEKS dan ATURAN KERAS) ada di `docs/handover/PROMPT-M04-partial-3.md`; salinan yang sama diberikan pada akhir sesi ini sebagai berkas unduhan.

---

## Lampiran A — Rekonstruksi HANDOVER-M04-partial-1 (T1)
*Direkonstruksi pada sesi partial-2 karena berkas aslinya hilang; bersumber dari kode T1 di repo, ADR-0022 bagian 1, dan prompt sesi. Bukan salinan asli.*
- **Isi T1 (tiga commit `feat` + satu commit `docs(m04): STATUS … ADR-0022 bagian 1`):** `internal/permission` (katalog 41 kode = docs/06 §5.4, validasi kode, sinkron tanpa hapus), `internal/role` (empat role sistem global `role_type='system'`, organization/group NULL; `super-admin` memegang semua permission core `is_system`; konvergensi set core, permission modul dibiarkan),
  `internal/authorization/application.Syncer` (satu transaksi, advisory lock `0x7a61676f03`, hanya hak `app_user`, adopsi role `super-admin` minimal dari bootstrap menurut slug, penolakan kode core yang dimiliki modul, audit `authz.catalog_synced` hanya bila ada perubahan, rollback total bila audit gagal).
- **Bukti T1:** `lint`, `go vet`, `gofmt` lolos pada percobaan pertama; paket T1 hijau; `make verify GO_TEST_PARALLEL=1` hijau (`exit=0`, smoke M00/M02/M03 OK) pada commit `8744d46` (§3). Empat run `make verify` merah sebelumnya di awal M04 adalah flake Docker (§8).
- **Tes T1 (nama dari `sync_test.go`):** katalog dan role pada database baru; idempoten tanpa audit bila tak ada perubahan; adopsi `super-admin` minimal dari bootstrap menurut slug; bootstrap sesudah sync memakai ulang role; sinkron tak pernah menghapus permission dan melaporkan orphan; menolak kode core milik modul tanpa meninggalkan apa pun;
  konvergensi role sistem tanpa menyentuh permission modul; perbaikan baris permission yang melenceng; rollback total bila audit tak bisa ditulis; sinkron bersamaan menghasilkan satu katalog; definisi tak konsisten ditolak sebelum menyentuh database.
- **Utang/pertanyaan terbuka yang diwarisi (§8/§9 asli tidak tersedia; dari prompt sesi dan HANDOVER-M03 §8–§9):** pembuka kunci akun administratif (M05), identitas platform vs `switch-organization` (diputuskan ADR-0022 butir 1; dikerjakan T3/T5), `Bus` instans sendiri, anggaran login smoke.
