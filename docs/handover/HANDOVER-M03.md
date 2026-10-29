# HANDOVER — M03 Authentication

**Tanggal:** 2026-10-29 · **Tag:** `m03-done` (dibuat setelah merge, lihat §7) · **Commit terakhir:** subjek `docs(m03): HANDOVER-M03 lengkap, STATUS, CHANGELOG — M03 selesai` (SHA laptop berbeda dari sandbox; subjek dan urutan yang berlaku)
**Jenis:** LENGKAP · **Branch:** `milestone/m03-authentication` (basis: tag `m02-done`) · **Handover sebelumnya:** `HANDOVER-M03-partial-1,2,3,5,6.md` (`partial-4` TIDAK ADA di repo)

## 1. Ringkasan
Platform kini punya autentikasi lengkap: login Argon2id tanpa oracle enumerasi akun, sesi 256-bit (hanya hash yang disimpan; Redis + PostgreSQL, tetap berjalan bila Redis mati), cookie `HttpOnly` + CSRF terikat sesi, rotasi sesi
(login, refresh, ganti password, pindah organisasi), manajemen sesi, ganti password, riwayat event keamanan, penjaga login (kunci akun sementara yang tak terlihat klien, jeda progresif = durasi kunci berlipat, gerbang IP 429, fallback memori saat Redis mati),
dan `make bootstrap-admin` untuk Super Admin pertama. `scripts/smoke/m03.sh` membuktikan alurnya di stack asli. Belum ada: otorisasi (M04), reset password/undangan (M05), MFA (Fase 2).

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T1 hasher Argon2id, repositori credentials/sessions/security_events | DONE | partial-1 |
| T2 layanan sesi (Redis + PostgreSQL, rotasi, idle/absolut) | DONE | partial-1 |
| T3 cookie + CSRF + middleware + `Identity` | DONE | partial-1 (◆CP1) |
| T4 endpoint login/logout/me/csrf/refresh/switch-organization/sesi/password/events | DONE | partial-2, partial-5 |
| T5 penjaga login (kunci akun, per IP, jeda progresif, fallback memori, event) | DONE | partial-6; ADR-0020 bagian 5 |
| T6 `bootstrap-admin` | DONE | partial-3 |
| Migrasi smoke M02 ke sesi nyata | DONE | partial-3 |
| `scripts/smoke/m03.sh` | DONE | sesi ini; ADR-0020 butir 58–60 |
| Doc-sync kumulatif | DONE | sesi ini; §4 |
| Ritual akhir (README, CHANGELOG, STATUS, handover) | DONE | sesi ini; merge + tag oleh pengguna (§7) |

## 3. Bukti verifikasi (apa adanya)
- **`make verify GO_TEST_PARALLEL=1` di laptop (WSL2), port 25000–25005, Go 1.26.5, 2026-10-29:**
  1. Pada commit `docs(m03): handover parsial 6, STATUS, CHANGELOG`: `gofmt -l backend` kosong, `exit=0`; `grep` menunjukkan `bash ./scripts/verify-smoke.sh`, `M00 smoke OK`, `M02 smoke OK`.
  2. Setelah commit `test(smoke): smoke M03 …`: `gofmt -l backend` kosong, **`exit=0`**; `grep` menunjukkan `== scripts/smoke/m03.sh`, langkah-langkahnya, `• sisa jatah login 4 < 8; menunggu 53s` (penantian jatah `LoginRule`, perilaku yang dirancang) dan **`M03 smoke OK`**.
  3. **Commit dokumen sesudahnya (doc-sync, handover, STATUS, CHANGELOG; hanya berkas teks):** `make verify GO_TEST_PARALLEL=1` penuh GAGAL (`exit=2`) pada tiga run berturut-turut karena flake Docker laptop, dengan paket yang berpindah-pindah
     (`auth/repository`, `infra`, `audit/repository` + `migrations`). Pesan galat dibaca dan semuanya kelas flake (`wait for reaper … context deadline exceeded`, `start container: wait until ready: context deadline exceeded`), tanpa galat aserasi atau kode.
     Bukti pengganti (BUKAN satu run `make verify` utuh pada commit ini): (a) setiap paket Go dijalankan sendirian (`REQUIRE_DOCKER=1 go test <paket> -race -count=1 -p 1`, per paket dari `go list ./backend/...`): semua `ok`
     (`auth/service` 77,0 dtk, `migrations` 45,0 dtk, `infra` 40,9 dtk, `kernel` 39,6 dtk, `bootstrap` 28,1 dtk, `audit` 28,5 dtk, dst.; tak ada `FAIL`); (b) tahap sisa dijalankan terpisah:
     `make lint` (`golangci-lint` 0 issues, `svelte-check` 0 error/0 warning, `prettier` bersih, `lint-structure OK`) `&& make generate-check` (OK) `&& make migrate-roundtrip` (OK) `&& make build` (Go + Vite OK) `&& bash ./scripts/verify-smoke.sh` → **`exit=0`**,
     `M00 smoke OK`, `M02 smoke OK`, `M03 smoke OK` (penantian jatah login 51 dtk). Patch dokumen tidak menyentuh kode; yang belum ada hanya satu run `make verify` utuh yang hijau pada commit dokumen.
     Rekomendasi: naikkan memori WSL (`memory=6GB` di `.wslconfig`) dan ulangi `make verify` utuh di awal M04 sebagai konfirmasi baseline.
  - Yang TIDAK terlihat di keluaran yang ditempel (hijau disimpulkan dari `exit=0`): baris `ok` per paket tes, lint, generate-check, migrate-roundtrip, build.
- **Smoke M03 di stack asli (2026-10-29). Skrip gagal keras pada langkah mana pun yang gagal (`set -e` + `fail`), jadi `M03 smoke OK` + `exit=0` membuktikan seluruh urutan berikut; hanya sebagian barisnya terlihat di keluaran yang ditempel:** login admin smoke → `me` → `csrf` → logout tanpa CSRF 403 → logout 204 → cookie sama 401 → login ulang → ganti password (salah/terlalu pendek ditolak, A→B sukses, cookie lama 401, cookie baru 200, login A 401, login B 200, `security/events` memuat `auth.password.changed`/`change_failed`/`login.failed`) → B→A → login A 200 → akun kunci `smoke-lock@example.org`: 5 salah (401 `invalid_credentials`), benar saat terkunci (401, tanpa cookie), `auth:lock:<id>` ada dan ber-TTL di Redis, `DEL` membuka, login 200, tepat satu `auth.account.locked`. Baris yang terlihat di keluaran pengguna: 333–335, 340, 347–349 dan 356 dari `verify.log`; isi tiap respons tidak ditempel.
- **Sandbox (bukan bukti utama):** `m03.sh` diuji terhadap API tiruan (server Python + stub `make`/`docker`): jalur sukses, server yang tak pernah mengunci (skrip gagal di langkah yang benar), dan kegagalan yang disuntikkan setelah password berubah (pemulihan B→A berjalan). Itu menemukan satu bug urutan di skrip yang diperbaiki sebelum dikirim. `bash scripts/lint-structure.sh` OK; `git am` pada salinan bersih zip = pohon identik.
- **Tes negatif/keamanan M03 (ringkas; rincian partial-1,2,5,6 §3):** hasher (batas panjang, dummy, CAS rehash), sesi (cabut, kedaluwarsa, idle, Redis kosong/mati, entri basi terbatas), CSRF/Origin, enumerasi akun, rotasi sesi, switch-organization (tanpa oracle), ganti password (salah, kebijakan, CAS bersamaan), penjaga login (50 kegagalan bersamaan = satu kunci, kunci tak terlihat, biaya Argon2 setara, gerbang IP sebelum Argon2, /64, fallback Redis mati), laju `refresh`/`switch`, bootstrap (idempoten, tak menimpa password, rollback).

## 4. Keputusan & penyimpangan
- **ADR-0020 bagian 1–6** (butir 1–61): hasher, sesi, CSRF, cookie (1–16); T4 (17–32); T6 (33–41); ganti password/events (42–46); penjaga login (47–56); penutupan (57–61: rentang config, desain smoke, akun kunci terpisah, A→B→A, anggaran login, koreksi `revoke-others`).
- **ADR-0021** (port host compose dapat disetel; bagian 2: kembali ke laptop, VPS tak lagi dipakai). Catatan lain: ADR-0014 diberi catatan (butir 42).
- **Penyimpangan utama dari dokumen 01–20:** hasher ber-`ctx`; timeout sesi dari config dulu; urutan Authenticate→CSRF→RequireAuth; `refresh` tidak memperpanjang batas absolut; `423 account_locked` hanya untuk `users.status='locked'` (kunci sementara = `invalid_credentials`, menyimpang dari kata-kata docs/05 §18); `POST /security/password` tanpa `Idempotency-Key`; jeda progresif = durasi kunci berlipat; paket `auth` tanpa lapisan `application/`.
- **Doc-sync M03 DITERAPKAN (commit `docs(sync)` ×2):** docs/05 (§4.1, §7, §7.2, §8, §9, §12.1, §12.2, §12.3, §13.1, §14, §17, §18, §19), docs/08 (§6, §7, §10.1, §11), docs/10 (§3, §8), docs/04 §16, docs/00 B-04, docs/15 (§2, §4), docs/16 (§2.4, §2.7), docs/19, ADR-0014, ADR-0021, README, `deploy/vps/README.md` (catatan historis). Tidak ada doc-sync M03 yang tertunda.
- **Satu-satunya yang belum tergabung:** `HANDOVER-M03-partial-4.md` §4 (berkas tidak ada di repo). Bila pengguna menemukannya (`sed -n '/^## 4/,/^## 5/p'`), gabungkan sebagai doc-sync tambahan di M04.
- **Koreksi:** partial-2 §4 menulis respons `revoke-others` memuat `csrf_token`; kode dan OpenAPI: `{revoked: <jumlah>}` (ADR-0020 butir 61).

## 5. Delta kontrak (seluruh M03)
- **DB:** tidak ada migrasi baru di M03 (tabel dari M01 dipakai); blok sqlc kedua `internal/auth/repository`.
- **API (`docs/api/swagger.json`):** `POST /api/v1/auth/login|logout|refresh|switch-organization`, `GET /auth/me|csrf`, `POST /security/password`, `GET /security/sessions|events`, `DELETE /security/sessions/{id}`, `POST /security/sessions/revoke-others`.
  Kode error baru: `invalid_credentials` 401, `account_suspended` 403, `account_locked` 423 (hanya administratif), `csrf_failed` 403; `rate_limit_exceeded` 429 + `Retry-After` kini juga dari penjaga login. `X-Dev-Actor` dihapus (echo POST butuh sesi + `X-CSRF-Token`).
- **Event keamanan:** `auth.login.success|failed`, `auth.logout`, `auth.session.refreshed`, `auth.organization.switched`, `auth.session.revoked`, `auth.sessions.revoked_others`, `auth.password.changed`, `auth.password.change_failed`, `auth.account.locked`, `auth.bootstrap_admin`.
  **Event outbox:** `auth.password.changed` (payload `{user_id, revoked_other_sessions}`; belum ada konsumen: M05/M08). **Pub/Sub Redis:** `session.revoked` `{session_id, user_id, reason}`.
- **Redis:** `session:{sha256}`, `user_sessions:{uid}`, `auth:lock|fail|strike:{user_id}`, `auth:ipfail:{ip}` (IPv6 per /64); pembuka darurat `DEL auth:lock:<user_id>`.
- **Config/env:** `PASSWORD_ARGON2_MEMORY_KIB|TIME|PARALLELISM`, `PASSWORD_HASH_CONCURRENCY`, `PASSWORD_MIN_LENGTH`, `SESSION_IDLE_TIMEOUT`, `SESSION_ABSOLUTE_TIMEOUT`, `LOGIN_MAX_FAILURES|FAILURE_WINDOW|LOCK_DURATION|LOCK_MAX|IP_MAX_FAILURES`, port host compose `*_HOST_PORT` (ADR-0021).
- **CLI/skrip:** `api bootstrap-admin`, `make bootstrap-admin EMAIL=… [NAME=…] [PASSWORD_STDIN=1]`, `scripts/env-ports.sh`, `scripts/smoke/{auth-lib,m03}.sh`, `make smoke` dengan `SMOKE_STATE_DIR`.
- **Permission:** tidak ada (M04). **Role:** `super-admin` dibuat minimal oleh bootstrap tanpa permission (M04 mengadopsi by slug).

## 6. Peta kode
| Hal | Lokasi |
|---|---|
| Hasher Argon2id, kebijakan password | `backend/pkg/security/password*.go` |
| Login/logout/refresh/switch/profil | `backend/internal/auth/service/auth.go` |
| Sesi (Redis + PG), manajemen sesi | `auth/service/session.go`, `sessions_mgmt.go` |
| Ganti password, riwayat event | `auth/service/password.go`, `security_events.go` |
| CSRF | `auth/service/csrf.go` |
| Penjaga login | `auth/service/guard.go` (Lua + Redis), `guard_memory.go` (fallback) |
| Handler HTTP, middleware, cookie, Origin | `auth/transport/http/*` (paket `authhttp`) |
| Identity di context | `auth/domain/identity.go` (`IdentityFromContext`) |
| Rute dan aturan rate limit | `backend/internal/httpserver/auth_routes.go` |
| Perakitan | `backend/internal/app/auth.go` (`newAuthStack`), `app/bootstrap.go` |
| Bootstrap admin | `backend/internal/bootstrap/bootstrap.go` |
| Config auth | `backend/pkg/config/config.go` (`validateAuth`), `.env.example` |
| Smoke | `scripts/smoke/auth-lib.sh`, `m02.sh`, `m03.sh` |

## 7. Cara menjalankan & memverifikasi
Dari `~/projects/zago-core-m03` (WSL2):
```bash
gofmt -l backend; make verify GO_TEST_PARALLEL=1 > ~/verify.log 2>&1; echo "exit=$?"; grep -nE '^(--- FAIL|FAIL|✗)|smoke|•' ~/verify.log | tail -50
```
Demo manual setelah `make docker-up`: `make db-seed && make bootstrap-admin EMAIL=kamu@example.org`, lalu `POST /api/v1/auth/login` (cookie jar + `X-CSRF-Token`).
Penutupan milestone (pengguna): `git push`, lalu buka PR `milestone/m03-authentication` → `main` di GitHub (PR #4 sudah me-merge M03 secara parsial; PR akhir hanya membawa commit sesudahnya) dan merge dengan merge commit seperti M00/M02; lalu `git switch main && git pull`, `git tag m03-done <sha merge commit>` dan `git push origin m03-done`.
Flake Docker laptop (kelas galat di ADR-0021 bagian 2) bukan kode: jalankan paket sendirian, pulihkan Docker, ulangi.

## 8. Masalah diketahui / utang teknis
- **Mesin:** pengembangan pindah dari VPS ke laptop (WSL2, port 25000–25005; ADR-0021 bagian 2); lima commit T5 nyaris hilang karena tidak di-push (aturan: `git push` setelah setiap patch). Docker Desktop laptop flake (kelas galat di ADR-0021 bagian 2); belum ada ukuran sumber daya stabil.
- **`HANDOVER-M03-partial-4.md` hilang** (§4 belum tergabung); checksum zip ≠ checksum VPS tanpa sebab terbukti (tidak lagi relevan setelah pindah mesin).
- **Kunci akun bisa dipakai sebagai DoS terarah** (korban yang emailnya diketahui; ≤ 60 menit per kunci, dibatasi gerbang IP); tidak ada pembuka administratif (sementara `redis-cli DEL auth:lock:<user_id>`); pemilik tak diberi tahu (M05/M08).
- **Belum tertangani:** password spraying lintas IP, CAPTCHA/MFA (M15/Fase 2); `LoginRule` per alamat IPv6, bukan /64 (M15); saat Redis mati ambang efektif ×jumlah instance dan status memori tidak disalin ke Redis.
- **Smoke:** membuat DUA Super Admin (`smoke-admin@`, `smoke-lock@example.org`), hanya localhost; di stack dev yang dipakai ulang `bootstrap-admin` tak menimpa password sehingga `SMOKE_ADMIN_PASSWORD`/`SMOKE_LOCK_PASSWORD` harus diisi atau pakai stack bersih; butuh `docker compose exec redis` dari host; ±14 login dan ±7 kegagalan IP per run (gerbang IP 30/15 menit tertembus setelah >±4 run pada stack yang sama; `make verify` memakai stack baru). Bila m03 gagal di antara dua penggantian password, pemulihan hanya best-effort.
- **CI:** job `docker-smoke` tidak menyiapkan Go secara eksplisit dan smoke M02/M03 butuh `go run` + `docker compose exec`; belum terbukti di CI (utang M15; docs/19).
- Verifikasi dummy saat kunci aktif tetap memakan CPU Argon2 (dibatasi semaphore dan gerbang IP). `AuthService` menerima `Guard == nil` hanya untuk tes lama (`newAuthStack` selalu memasangnya).
- Dari milestone sebelumnya yang masih berlaku: `Bus` di `app/auth.go` instans sendiri (M07/M08 harus membuat satu `Bus` bersama); `security_events` tumbuh tanpa batas (retensi: M06/M15); `ts-sdk` placeholder; login tidak mencabut sesi lama pemanggil dan `Set-Cookie` ganda saat cookie basi (butir 25);
  parameter Argon2 bawaan belum disetel pada perangkat keras deploy; tes waktu-tak-terbedakan hanya menguji kesetaraan jalur; entri Redis basi bisa hidup sampai 1 menit bila penghapusan Redis gagal; `make setup`/`make dev` tidak menjalankan `db-seed`; flake smoke M00 pernah sekali (`curl gagal … /health`).
- Tidak ada jaminan keamanan absolut: desain ini mempersulit, bukan mustahilkan, penebakan password, pencurian sesi, dan CSRF; belum ada audit keamanan pihak ketiga.

## 9. Pertanyaan terbuka
1. Batas jumlah sesi aktif per pengguna. 2. Perlukah kegagalan CSRF menulis `security_events` (kini hanya log debug). 3. Login saat pemanggil sudah punya sesi sah: cabut sesi lama otomatis? 4. Identitas platform (Super Admin tanpa keanggotaan) belum bisa `switch-organization`: **putuskan di M04**.
5. Perlukah `GET /auth/csrf` mengembalikan `expires_at`? 6. Haruskah `make setup`/`make dev` (dan compose `migrate`) menjalankan `db-seed`? 7. Prompt password interaktif tanpa echo (`golang.org/x/term`)? 8. Bolehkah `bootstrap-admin` berjalan di `ENVIRONMENT=production` (kini boleh)?
9. Endpoint administratif pembuka kunci akun (M04/M05) dan notifikasi ke pemilik saat kunci dipasang (M05/M08). 10. Lisensi proyek belum ditetapkan.
- Terjawab di M03: `account_locked` 423 hanya administratif (ADR-0020 butir 47); kunci tak terlihat; `auth.account.locked` sekali, `unlocked` tidak ditulis (48); akun smoke kedua (58).

## 10. Milestone berikutnya: M04 — Authorization Engine
- **Baca:** 06 (seluruhnya) · 07 §7 · 12 §4.6, §6 · `E:B-04 (katalog), I-01, I-02, I-03`.
- **Langkah pertama yang disarankan:** T1 paket `role`, `permission`, `policy`, `authorization` (katalog permission core di kode + sinkron startup; role sistem + set default), lalu T2 `ScopeContext`, T3 ◆CP1 (effective permissions + cache versi-per-org, `Authorizer.Can`, middleware `Authorize(action)`).
- **Perhatian khusus:** adopsi role `super-admin` lewat upsert by slug (bukan INSERT kedua); identitas platform vs `switch-organization` (§9 #4); `Authorize` dipasang setelah `RequireAuth`; tes negatif tenant/scope wajib; perubahan `role_permissions`/assignment berlaku seketika; fail-safe saat Redis down;
  smoke M04 harus menghormati anggaran login (§8) dan tidak mengunci Super Admin smoke.

## 11. Prompt untuk chat berikutnya
Upload zip repo terbaru (`bash scripts/repo-zip.sh /mnt/c/Users/maste/Downloads/platform-repo-$(date +%Y%m%d-%H%M).zip` di laptop; jumlah berkas harus sama dengan `git ls-files | wc -l`, diharapkan 373) dan tempel **PROMPT P2 untuk M04** di bawah:

```text
Peran: kamu senior software engineer (Go, PostgreSQL 18, Redis 8, Svelte 5, Docker, WebSocket)
melanjutkan "Open Source Modular Application Platform" di chat room BARU. Kamu tidak punya ingatan
dari chat sebelumnya — semua konteks ada di repo (zip terlampir, tanpa .git).

TUGAS SESI INI: MEMULAI Milestone M04 — Authorization Engine (BUKAN M05). M03 (Authentication) SELESAI dan sudah di-merge (tag m03-done).
Handover terbaru: docs/handover/HANDOVER-M03.md. (HANDOVER-M03-partial-4.md TIDAK ADA di repo; abaikan rujukan ke berkas itu.
Rujukan "di VPS" di handover/ADR/runbook adalah keadaan lama: lihat ADR-0021 bagian 2.)

Konteks di repo: CLAUDE.md, docs/STATUS.md, docs/handover/HANDOVER-M03.md (§5 delta kontrak dan §6 peta kode memuat antarmuka auth yang dipakai M04),
docs/adr/0020 (bagian 1–6), docs/adr/0021, docs/21-milestone-plan.md (bagian M04), docs/22-handover-protocol.md.
Dokumen 01–20 yang dibaca untuk M04 (HANYA bagian ini): 06 (seluruhnya) · 07 §7 · 12 §4.6, §6 · docs/00 butir E:B-04 (katalog permission), I-01, I-02, I-03.

CATATAN KHUSUS M04 (dari M03):
- **M04 mengadopsi role `super-admin` lewat upsert by slug.** `bootstrap-admin` (M03) membuat role itu minimal (`role_type='system'`, slug `super-admin`, organization_id/group_id NULL,
  TANPA permission). Saat menyinkronkan role sistem dari kode, upsert baris itu berdasarkan slug `super-admin` (jangan INSERT baris kedua: `uq_roles_global_slug` akan menolaknya) lalu beri ia seluruh permission `is_system`.
- Identitas platform (Super Admin tanpa keanggotaan organisasi, `Identity.OrganizationID == nil`) belum bisa `switch-organization` (HANDOVER-M03 §9 #5): putuskan di M04 (ADR) bagaimana jalur platform memasuki organisasi.
- Middleware auth yang ada: Authenticate → CSRF → RequireAuth (ADR-0020 butir 11). `Authorize(action)` dipasang SESUDAH RequireAuth. `domain.IdentityFromContext(ctx)` memberi `Identity{UserID, OrganizationID, SessionID, AuthLevel}`.
- Pembuka kunci akun administratif (HANDOVER-M03 §9 #11) adalah kandidat permission/endpoint M04–M05; sementara `redis-cli DEL auth:lock:<user_id>`.
- Smoke M04 (`scripts/smoke/m04.sh`) memakai `scripts/smoke/auth-lib.sh`; batasi login smoke terhadap penjaga login (HANDOVER-M03 §3/§8: LoginRule 10/menit/IP dan skrip m02+m03 sudah ±14 login per run; gunakan sesi yang sudah ada atau tunggu jendela berganti seperti `m03.sh`). Jangan mengunci Super Admin smoke.

MESIN KERJA: pengembangan di laptop (WSL2 Ubuntu, user masterabror, repo di ~/projects/zago-core-m03, unduhan chat di /mnt/c/Users/maste/Downloads).
Tidak ada VPS dan tidak ada scp. Origin: https://github.com/master-abror/zago-core.git. Branch kerja M04: `milestone/m04-authorization`, dibuat dari tag `m03-done` (minta aku membuatnya di langkah 2 bila belum ada).
Commit dibuat di satu tempat saja (laptop) supaya tidak ada dua salinan yang divergen. PELAJARAN: lima commit T5 M03 nyaris hilang karena tidak pernah di-push sebelum akses VPS putus. Karena itu `git push` WAJIB setelah SETIAP patch diterapkan dan diverifikasi, bukan hanya di checkpoint.
Port host compose di laptop: `.env` sudah dibuat `scripts/env-ports.sh --force POSTGRES=25000 REDIS=25001 API=25002 WEB=25003 SMTP=25004 MAILUI=25005` karena port 5432/6379 dipegang container proyek lain (nizmeet-dev-*): JANGAN dimatikan.
`✗ port … sudah dipakai` = bentrok port, bukan masalah kode.
Docker Desktop laptop (≈3,5 GB memori, bisa dinaikkan lewat .wslconfig) FLAKE: kelas galat yang sudah teramati dan BUKAN kode: `Reaper handshake failed`, `wait for reaper … context deadline exceeded`, `RWLayer … is unexpectedly nil`, `dial tcp 127.0.0.1:<port>: connection refused`
padahal container "ready", `failed to authorize: DeadlineExceeded` saat build image (Docker Hub). Paket yang kena berpindah-pindah per run; tanda khasnya satu tes gagal setelah ±65 detik lalu puluhan tes gagal dalam 0,00 detik. Penanganan: baca pesannya dulu; bila kelas di atas, jalankan paket itu sendirian
(`REQUIRE_DOCKER=1 go test <paket> -race -count=1 -p 1`), pulihkan Docker, ulangi; JANGAN ubah kode. Galat aserasi/kode = temuan baru: hentikan dan laporkan. Di laptop (non-root) `verify-smoke.sh` juga memeriksa berkas milik root di repo; bila gagal: `sudo chown -R "$USER":"$USER" .` (perbaikan lingkungan), lalu ulangi;
bila berkas root muncul LAGI setelah run bersih, itu celah nyata di docker-compose.yml: laporkan, perbaiki lewat commit tersendiri.
Preferensi: gunakan saran terbaikmu; keamanan setinggi mungkin; jujur bahwa tidak ada jaminan absolut dan catat keterbatasan terbuka.

══ KETENTUAN CHECKPOINT KONTEKS (BERLAKU DI SETIAP CHAT, TIDAK BOLEH DILEWATI) ══
Chat room punya batas konteks dan kamu tidak bisa mengukur sisanya dengan pasti. Karena itu:
A. PANTAU TANDA-TANDA konteks hampir habis, dan nilai di AKHIR SETIAP TASK dan di setiap ◆CP:
   percakapan sudah panjang (puluhan giliran), banyak keluaran log/perintah yang ditempel, banyak berkas
   dibaca penuh, beberapa patch sudah dikirim, atau kamu mulai kehilangan detail awal. KALAU RAGU,
   BERHENTI DAN BUAT CHECKPOINT. Checkpoint terlalu awal murah; kehilangan konteks di tengah task mahal.
B. BILA TANDA-TANDA ADA: JANGAN mulai task baru. Selesaikan atau amankan task yang sedang jalan,
   umumkan jelas "Konteks hampir habis — checkpoint sekarang", lalu kerjakan urut:
   1. Keadaan kode: semua perubahan sudah di-commit kecil (Conventional Commits). Task setengah jadi
      dicatat apa adanya di handover (jangan di-commit rusak; stash/tandai TODO jelas).
   2. `make verify GO_TEST_PARALLEL=1` terakhir yang kuberikan: catat hasil APA ADANYA (hijau/merah,
      keluaran mana yang ada). Jangan menyatakan hijau tanpa keluaran dari mesinku.
   3. Dokumen: docs/handover/HANDOVER-M04-partial-N.md (N berikutnya = 1, lalu 2, dst.; format sama dengan HANDOVER-M03.md, §1–§11), ADR bagian baru
      bila ada keputusan baru (ADR-0022+), docs/STATUS.md, CHANGELOG.md. Daftar doc-sync yang tertunda harus
      kumulatif (tidak ada yang tertunda dari M03 kecuali HANDOVER-M03-partial-4 §4 bila berkasnya ditemukan; tambahkan yang baru dari M04).
      Catat juga kendala mesin/flake Docker laptop di §3/§8.
   4. Kirim satu patch `git am` berisi commit dokumen itu (dibuktikan di sandbox: git init + commit baseline dari
      zip, `git format-patch`, `git am` pada salinan bersih zip → pohon identik). Beberapa commit boleh dikirim sebagai satu berkas mbox
      (`git format-patch -N --stdout > x.mbox`, diterapkan dengan `git am < x.mbox`).
   5. Beri aku urutan langkah SATU PERINTAH per langkah, semuanya di laptop (WSL2): `git am < /mnt/c/Users/maste/Downloads/<berkas>` di ~/projects/zago-core-m03,
      cek `git status -sb && git log --oneline -6 --decorate && git fsck --no-dangling 2>&1 | head -3`, `git push` (WAJIB: origin harus punya salinan),
      buat zip langsung ke unduhan: `bash scripts/repo-zip.sh /mnt/c/Users/maste/Downloads/platform-repo-$(date +%Y%m%d-%H%M).zip`, lalu cek jumlah file zip = `git ls-files | wc -l`
      (mis. `unzip -Z1 <zip> | grep -vc '/$'`; gunakan nama zip persis).
   6. Berikan PROMPT BARU yang terisi penuh untuk chat berikutnya (nomor partial, task berikutnya, commit teratas
      yang diharapkan, keadaan `make verify` yang diharapkan). PROMPT BARU ITU WAJIB MEMUAT ULANG SELURUH
      BLOK "KETENTUAN CHECKPOINT KONTEKS" INI DAN SEMUA ATURAN DI BAWAH, supaya siklus ini berlanjut
      di chat berikutnya, dan seterusnya sampai milestone selesai.
C. Saat milestone selesai (ritual akhir), PROMPT P2 untuk milestone berikutnya (M05 dst.) juga WAJIB memuat
   ketentuan ini dan aturan keras di bawah.
D. Aku boleh menyuruhmu checkpoint kapan saja ("checkpoint"); lakukan B tanpa bantahan.

LANGKAH (ritual awal, WAJIB urut):
1. Baca CLAUDE.md → STATUS.md → HANDOVER-M03.md → bagian M04 di docs/21. Baca HANYA bagian dokumen 01–20 yang tercantum di atas.
2. PEMERIKSAAN KEADAAN REPO (sebelum membangun apa pun): minta aku menjalankan dan menempelkan (di ~/projects/zago-core-m03)
   `git status -sb && git log --oneline -8 --decorate && git tag && git fsck --no-dangling 2>&1 | head -3`.
   Pastikan: tag m03-done ada dan menunjuk commit hasil merge M03 (atau ujung branch M03 bila merge fast-forward), working tree bersih, fsck bersih, STATUS.md menyatakan M03 DONE.
   Bila branch `milestone/m04-authorization` belum ada, minta aku membuatnya: `git switch -c milestone/m04-authorization m03-done` lalu `git push -u origin milestone/m04-authorization`.
   Jumlah berkas yang diharapkan: 373 (`git ls-files | wc -l`). Jika zip dan keadaan repoku tidak cocok pada jumlah berkas atau commit teratas, HENTIKAN dan laporkan.
   SHA commit di laptop berbeda dari yang tertulis di dokumen (commit dibuat ulang lewat `git am`); urutan dan subjek yang berlaku.
3. `make verify` DIHARAPKAN HIJAU di laptop (exit=0; terakhir terbukti exit=0 dengan smoke M00, M02, dan M03 di dalamnya; port 25000–25005). Konfirmasi sekali di awal:
   gofmt -l backend; make verify GO_TEST_PARALLEL=1 > ~/verify.log 2>&1; echo "exit=$?"; grep -nE '^(--- FAIL|FAIL|✗)|smoke' ~/verify.log | tail -40
   Harapan: `gofmt -l backend` kosong dan exit=0. Merah = BACA PESAN GALATNYA dulu (lihat blok flake Docker). Go harus go1.26.5 (`go version`; bila tidak, `go env -w GOTOOLCHAIN=go1.26.5`); jangan lanjut diam-diam dengan versi lain.
4. Ringkas pemahamanmu (≤ 10 baris) dan tulis rencana task T1..T5 M04 + ◆CP + risiko. Tandai utang/pertanyaan terbuka di HANDOVER-M03 §8/§9 yang memengaruhi M04 (terutama §9 #5 identitas platform dan #11 pembuka kunci akun).
5. Kerjakan per task: tes dulu (tes NEGATIF wajib untuk auth/authz/tenant) → implementasi → perintah verifikasi → commit kecil
   (Conventional Commits) → tampilkan bukti → `git push`. Jangan menyatakan hijau tanpa keluaran dari mesinku. Setiap perubahan anotasi swagger butuh `make generate` yang kujalankan (commit hasilnya `chore(generate)`).
6. Berhenti di ◆CP atau lebih awal sesuai KETENTUAN CHECKPOINT KONTEKS di atas. Jangan memaksa menyelesaikan milestone di chat yang sudah penuh.
7. Ritual akhir (docs/22 §4.3): make verify hijau, doc-sync (tulis daftar yang tertunda di handover §4), README/CHANGELOG, HANDOVER-M04.md, STATUS.md, merge, tag m04-done (push tag juga), dan beri PROMPT P2 terisi untuk M05
   (memuat ketentuan checkpoint konteks ini dan aturan keras di bawah).

CARA MENERIMA PERUBAHAN: patch (git am, SATU perintah per langkah) atau file LENGKAP; jangan menimpa go.mod/go.sum dengan overlay.
Perintah dependensi baru (go get) berikan terpisah; aku yang menjalankannya. Semua perintah harus jalan di WSL2 Ubuntu (laptop, ~/projects/zago-core-m03). Tandai dengan jelas apa yang terbukti di sandbox dan apa yang baru
terbukti lewat perintah di mesinku. Catatan sandbox: modul Go TIDAK bisa dimuat (golang.org dan proxy.golang.org terblokir, toolchain 1.26.5 tak bisa diunduh), jadi kompilasi dan tes selalu menunggu
keluaranku. `gofmt` mungkin tersedia di sandbox (cek dulu dengan `which gofmt`; di sesi terakhir ada dan `gofmt -l backend` kosong), tetapi bukti utamanya tetap `gofmt -l backend` di mesinku;
`bash scripts/lint-structure.sh` berjalan di sandbox. Buat patch di sandbox dengan git (init + commit baseline dari zip, `git format-patch`) dan buktikan `git am` pada salinan bersih zip. Ekstrak zip ke direktori baru yang kosong.
Skrip bash baru (smoke) boleh diuji di sandbox terhadap API tiruan (server Python kecil + stub make/docker), tetapi itu hanya bukti alur kontrol, bukan perilaku API nyata.
Saat memberi perintah verifikasi yang menyaring log, gunakan tanda kutip tunggal (tanda seru di kutip ganda ditafsirkan bash).
Satu perintah per blok kode (perintah panjang yang tertempel dua kali pernah merusak langkah).

ATURAN KERAS:
- ADR terbaru menang atas dokumen 01–20 Rev 1.1. Penyimpangan baru → ADR (lanjutkan dari 0022, atau bagian baru di ADR yang relevan) + catat di HANDOVER §4.
- Modul hanya mengimpor packages/module-sdk. Perubahan state + audit + event dalam satu transaksi.
- Setiap query tabel tenant wajib memfilter organization_id.
- UUID dibangkitkan lewat backend/pkg/id (id.NewID), termasuk di tes; scripts/lint-structure.sh menegakkannya.
- Jangan membaca seluruh dokumen; jangan mengerjakan milestone lain; jangan klaim selesai tanpa bukti.

Mulai dengan langkah 1.
```
