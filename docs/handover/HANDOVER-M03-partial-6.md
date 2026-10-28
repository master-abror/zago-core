# HANDOVER — M03 Authentication (PARSIAL 6, berhenti di ◆CP6)

**Tanggal:** 2026-10-28 · **Status:** IN PROGRESS · **Jenis:** PARSIAL (T1–T5, T6 + migrasi smoke M02 + task lingkungan VPS selesai; smoke M03 dan ritual akhir belum)
**Branch:** `milestone/m03-authentication` (basis: tag `m02-done`, `8e7a8b1`) · **Handover sebelumnya:** `HANDOVER-M03-partial-5.md` (dan `partial-3`, `partial-2`, `partial-1`). **`HANDOVER-M03-partial-4.md` TIDAK ADA di repo ini**
(lihat partial-5 §4/§8). Partial-6 adalah rujukan terbaru.
**Commit sejak `e39c73e` (urutan; SHA di sandbox chat berbeda dari VPS, urutan yang berlaku):** `docs(adr)` ADR-0020 bagian 5 (VPS: `b9f4771`) · `feat(auth)` penjaga login: kunci akun Redis, jeda progresif, fallback memori (T5a) ·
`docs(adr)` ADR-0020 butir 56 (kunci IP IPv6 per /64) · `feat(auth)` Login memakai penjaga (T5b) · `feat(auth)` penjaga login terpasang end-to-end: 429 + Retry-After, 5 env, wiring, laju refresh/switch (T5c) · docs handover (commit ini).
Tidak ada `chore(generate)`: anotasi swagger tidak diubah (`docs/api/swagger.json` sudah memuat 429; `generate-check` di `make verify` lolos).

## 1. Ringkasan
T5 selesai dan terbukti di VPS lewat tes Go (PostgreSQL + Redis sungguhan) dan `make verify`: kunci akun sementara di Redis (`auth:lock:{user_id}`) yang **tidak pernah terlihat oleh klien** (selama kunci aktif login selalu `invalid_credentials` dengan biaya Argon2 setara,
password benar pun tak menghasilkan sesi), penghitung kegagalan per akun dan per IP, jeda progresif (durasi kunci berlipat 15 → 30 → 60 menit), gerbang IP (429 + `Retry-After`, IPv6 per /64), event `auth.account.locked`, fallback memori bila Redis mati,
lima env baru, dan laju 30/menit per pengguna untuk `refresh` dan `switch-organization`. Keputusan #10 dan #11 ditetapkan di ADR-0020 bagian 5 (butir 47–56).
Sisa M03: **smoke M03** (`scripts/smoke/m03.sh`) dan **ritual akhir** (doc-sync kumulatif §4, README, CHANGELOG, `HANDOVER-M03.md`, STATUS, merge, tag `m03-done`, PROMPT P2 untuk M04).

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T1–T4d, T6, migrasi smoke M02, lingkungan VPS, ADR-0021 | DONE | partial-1..5 |
| ADR-0020 bagian 5 (butir 47–56) | DONE | keputusan #10, #11, Redis mati, ambang/TTL/jeda progresif, /64 |
| T5a penjaga login (`Guard`, Redis Lua + fallback memori) | DONE | `auth/service/guard.go`, `guard_memory.go`; 19 tes `Guard` hijau di VPS |
| T5b `Login` memakai penjaga | DONE | gerbang IP, kunci tak terlihat, `auth.account.locked`; tes integrasi `auth_guard_test.go` |
| T5c wiring end-to-end (handler 429, env, `newAuthStack`, laju refresh/switch) | DONE | `make verify` exit=0 |
| `scripts/smoke/m03.sh` | TODO | §10 (C) |
| Ritual akhir (docs/22 §4.3) + doc-sync (§4) | TODO | |

## 3. Bukti verifikasi (apa adanya; keluaran dari VPS pengguna, 2026-10-27/28)
- **T5a (commit penjaga, tanpa integrasi):** `REQUIRE_DOCKER=1 go test ./backend/internal/auth/service/ -run 'Guard' -race -count=1 -p 1 -v` → semua 19 tes tingkat atas `PASS` (+ 10 subtes kebijakan), `ok … auth/service 3.413s`.
  Keluaran `gofmt`/`go build`/`go vet` untuk T5a tidak ditempelkan (tes berhasil dikompilasi, jadi build lolos).
- **T5b (patch dua commit diterapkan):** `git am` OK (dua commit); `gofmt -l backend` tanpa keluaran; `go build ./... && go vet ./backend/internal/auth/...` → `BUILD-VET-OK`;
  `REQUIRE_DOCKER=1 go test ./backend/internal/auth/... -race -count=1 -p 1` → `ok` (`auth/repository` 5,230 dtk, `auth/service` 20,976 dtk, `auth/transport/http` 4,424 dtk).
- **T5c:** pengguna menjalankan `make verify GO_TEST_PARALLEL=1 > ~/verify.log 2>&1; echo "exit=$?"` → **`exit=0`**. Ekor `grep` (40 baris terakhir) menunjukkan `ok` untuk `infra`, `kernel`, `kernel/httpx`, `systemlog`, `testkit`, `testpg`, `migrations`, `pkg/config` (1,347 dtk), `pkg/id`,
  `pkg/logger`, `pkg/redact`, `pkg/security`, `pkg/telemetry`; smoke M00 OK (8 langkah) dan smoke M02 OK (13 langkah, login Super Admin smoke dengan penjaga terpasang).
- **TIDAK terbukti oleh keluaran yang dilampirkan (periksa di langkah (A') chat berikutnya):** (1) SHA commit teratas di VPS saat verify dijalankan dan keluaran `git am`/`gofmt -l`/`go vet ./...` untuk patch T5c tidak ditempelkan (diasumsikan T5c sudah diterapkan karena
  verify dijalankan setelah patch dikirim; `git log` harus menunjukkan `feat(auth): penjaga login terpasang end-to-end …`); (2) baris `ok` untuk `auth/service`, `httpserver`, `app` ada di luar ekor 40 baris; (3) lint, generate-check, migrate-roundtrip, build tidak muncul di `grep` (hijau disimpulkan dari `exit=0` dan dari
  fakta smoke adalah langkah terakhir `verify`); (4) kunci akun **belum dibuktikan di stack asli** (belum ada smoke M03; hanya tes Go dengan PostgreSQL + Redis sungguhan dan handler/rute dengan layanan palsu); (5) `.env` di VPS dibuat sebelum T5 dan tidak memuat `LOGIN_*`: nilai bawaan yang dipakai.
- **Sandbox chat (bukan bukti utama):** `lint-structure` OK, `git am` pada salinan bersih zip + patch menghasilkan pohon identik di setiap langkah. **`gofmt` TIDAK tersedia di sandbox sesi ini** (catatan prompt tentang `/usr/local/go/bin/gofmt` keliru); format ditulis manual dan dibuktikan oleh `gofmt -l` di VPS.
- **Tes yang ditambahkan di T5 (negatif/keamanan di depan):**
  - `guard_test.go` (Redis sungguhan, jam palsu): kunci tepat di ambang ke-5 dan berakhir lewat jam; durasi 15/30/60/60 menit; strike gugur setelah 24 jam; jendela kegagalan mereset; sukses mereset akun dan strike; percobaan saat kunci aktif tidak dihitung dan tidak memperpanjang;
    50 kegagalan bersamaan = tepat satu pemasangan kunci; akun independen; gerbang IP 30/jendela, tidak direset oleh login sukses, IP lain bebas, IP kosong tak dilacak; semua kunci Redis ber-TTL dan bernama sesuai ADR, tak ada email di kunci/nilai, pembuka `DEL auth:lock:<uuid>` berfungsi;
    nilai kunci/penghitung rusak = gagal tertutup; ctx dibatalkan tidak menandai Redis mati; Redis mati = fallback memori dengan semantik sama (kunci, progresif, reset, gerbang IP); tabel memori penuh menolak identitas baru dan tidak mengusir entri aktif; validasi kebijakan.
  - `auth_guard_test.go` (PostgreSQL + Redis, `Login`): 5 salah → kunci, event `auth.account.locked` sekali (`failures=5`, `lock_seconds=900`), password BENAR saat kunci = `invalid_credentials` (bukan 423), tanpa sesi, tanpa catatan, tanpa verifikasi hash asli (hanya dummy), sesi yang ada tetap sah, kunci berakhir lewat jam;
    biaya Argon2 setara (spy hasher: salah = 1 compare, tak dikenal = 1 dummy, terkunci = 1 dummy); status suspended/locked administratif dan email tak dikenal tidak membuat kunci akun; sukses mereset (4 + sukses + 4 ≠ 8); gerbang IP menolak SEBELUM Argon2 dan sama untuk akun dikenal/tak dikenal; IPv6 per /64;
    kegagalan ganti password tidak menyentuh penghitung (butir 45); Redis penjaga mati: login tetap jalan dan tetap mengunci; 20 percobaan bersamaan = satu event kunci.
  - `httpserver/auth_routes_test.go`: `ThrottledError` → 429 `rate_limit_exceeded` + `Retry-After` (90 → "90", 200 ms → "1"), tanpa cookie; laju refresh dan switch-organization per pengguna (pengguna lain tak terkunci; permintaan terbatas tak sampai ke layanan).
  - `pkg/config/config_login_guard_test.go`: bawaan = `.env.example`, override, nilai tak sah ditolak (nama env di pesan), tak terparse gagal, hanya divalidasi untuk role API. `app/auth_internal_test.go`: kebijakan penjaga tak sah/tak diisi menggagalkan perakitan (`penjaga login`).

## 4. Keputusan & penyimpangan
- **ADR-0020 Bagian 5** (butir 47–56): (47) kunci Redis tak terlihat: `invalid_credentials` selalu selama kunci aktif; 423 `account_locked` hanya untuk `users.status='locked'` — **menyimpang dari kata-kata docs/05 §18** (423 untuk kunci sementara), sejalan dengan docs/05 §20;
  (48) event `auth.account.locked` sekali per kunci, `unlocked` tidak ditulis; (49) 5 kegagalan/15 menit → kunci 15 menit, berlipat sampai 60 menit (strike ber-TTL 24 jam), bukan tidur sisi server; (50) gerbang IP 30/15 menit sebelum pencarian user, tidak direset oleh sukses;
  (51) Redis mati = fallback memori per proses (10.000 identitas, gagal tertutup bila penuh oleh jendela aktif, 250 ms per operasi, mundur 1 detik); (52) waktu dari jam aplikasi (Lua menerima `now`), kunci `auth:lock|fail|strike:{user_id}`, `auth:ipfail:{ip}`; (53) urutan `Login`;
  (54) laju refresh/switch 30/menit per pengguna; (55) lima env, nilai tak sah = konfigurasi ditolak; (56) kunci IP IPv6 per /64.
- **Pengetatan di config (di luar ADR, dijelaskan di `.env.example`):** `LOGIN_MAX_FAILURES` 3–100, `LOGIN_FAILURE_WINDOW` dan `LOGIN_LOCK_DURATION` 1 menit–24 jam, `LOGIN_LOCK_MAX` ≤ 24 jam dan ≥ durasi dasar, `LOGIN_IP_MAX_FAILURES` 1–10000 (`GuardPolicy.Validate` lebih longgar: 1–100 dan ≥ 1 detik, dipakai tes).
  Bila ADR-0020 butir 55 perlu menyebut rentang ini, tambahkan di ritual akhir.
- **Doc-sync TERTUNDA KUMULATIF (lakukan di ritual akhir M03):**
  - **partial-1 §4:** docs/05 §4.1 (antarmuka ber-`ctx`, `CompareDummy`, hash dummy acak), §7 (nilai Redis memuat last_activity_at; batas basi 1 interval), §7.2, §8 (nama cookie `__Host-` hanya bila Secure), §9 (urutan Authenticate→CSRF), §12.1 (sumber timeout),
    §17 (semantik refresh), §19 (`service/csrf.go`, paket `authhttp`); docs/08 §11 (kode baru dan statusnya); docs/10 §8 (env baru `PASSWORD_*`, `SESSION_IDLE_TIMEOUT`, `SESSION_ABSOLUTE_TIMEOUT`); README.
  - **partial-2 §4:** docs/05 §14 (katalog event: `auth.login.success|failed`, `auth.logout`, `auth.session.refreshed`, `auth.organization.switched`, `auth.session.revoked`, `auth.sessions.revoked_others`), §17 (daftar endpoint final + `revoke-others` = POST,
    respons `csrf_token`), §18 (kode `invalid_credentials` 401, `account_suspended` 403, `account_locked` 423; `permission_denied` untuk switch-organization); docs/08 §11; docs/05 §11.1/§12.2; docs/10 (peta paket `auth/*`, `app/auth.go`).
  - **partial-3 §4:** docs/16 (bentuk `api bootstrap-admin --email/--name/--password-stdin`, variabel `EMAIL`/`NAME`/`PASSWORD_STDIN`, `make smoke` dengan `SMOKE_STATE_DIR`); docs/04 §16 langkah 4; docs/00 E:B-04 (role Super Admin dibuat bootstrap, M04 mengadopsi by slug);
    docs/15 (urutan `make db-seed` sebelum `make bootstrap-admin`; compose `migrate` hanya `up`); docs/05 §14 (`auth.bootstrap_admin`); docs/19 (smoke M02 butuh Go dan DB terjangkau dari host); README (alur setup dari nol).
  - **ADR-0021 "Konsekuensi":** docs/15 §2 (port host dapat disetel; `env-ports.sh`), docs/16 §2.7 (`verify-smoke` membaca port), docs/19 (CI tidak terpengaruh), README (alur pengembangan di VPS).
  - **partial-5 (ADR-0020 bagian 4):** docs/08 §6 (hapus `POST /security/password` dari daftar Idempotency-Key; catatan di ADR-0014), docs/05 §13.1 (event outbox terwujud + urutan butir 44), docs/05 §14 (`auth.password.changed`, `auth.password.change_failed`),
    docs/08 §10.1 (respons password/events; `limit` bawaan 50 maks 100), docs/10 (peta paket: `service/password.go`, `service/security_events.go`, `transport/http/handlers_security.go`), docs/08 §11 (tak ada kode error baru).
  - **Baru (partial-6, ADR-0020 bagian 5):** docs/05 §12.3 (kunci tak terlihat, durasi kunci berlipat = jeda progresif, gerbang IP per /64, fallback memori; menggantikan "jeda progresif" yang tak terdefinisi) dan §18 (423 hanya administratif; kunci sementara = `invalid_credentials`) dan §14 (`auth.account.locked`) dan §7.2 (Redis mati: penjaga login fallback memori);
    docs/08 §7 (aturan rate limit: `auth.login` 10/menit/IP, gerbang IP penjaga 429 + `Retry-After`, `auth.refresh` dan `auth.switch_organization` 30/menit/pengguna), docs/08 §11 (`rate_limit_exceeded` kini juga dari penjaga login); docs/10 §8 (lima env `LOGIN_*`) dan peta paket (`auth/service/guard.go`, `guard_memory.go`);
    docs/16 atau runbook (pembuka kunci darurat `redis-cli DEL auth:lock:<user_id>`); README (ringkasan keamanan login).
  - **BELUM TERGABUNG: partial-4 §4.** Berkas `HANDOVER-M03-partial-4.md` tidak ada di repo dan pengguna tidak menanggapi pertanyaan di sesi ini; bila ia memilikinya (`sed -n '/^## 4/,/^## 5/p'`), gabungkan daftar itu di sini sebelum ritual akhir.

## 5. Delta kontrak (sejak ◆CP5)
- **API:** tidak ada rute baru. `POST /api/v1/auth/login`: akun yang sedang dikunci Redis → **401 `invalid_credentials`** (identik dengan password salah/email tak dikenal; bukan 423); IP yang melewati ambang kegagalan → **429 `rate_limit_exceeded` + `Retry-After`** (detik, minimal 1) bahkan dengan password benar. 423 tetap hanya untuk `users.status='locked'`.
  `POST /auth/refresh` dan `POST /auth/switch-organization` kini bisa 429 (30/menit/pengguna).
- **Event keamanan baru:** `auth.account.locked` (`result=failure`, metadata `{failures, lock_seconds}`, hanya akun dikenal, sekali per kunci). Tidak ada event untuk percobaan saat kunci aktif.
- **Redis:** `auth:lock:{user_id}` (ms epoch berakhir), `auth:fail:{user_id}` dan `auth:strike:{user_id}` (`jumlah:berakhir_ms`), `auth:ipfail:{ip}` (IPv6 sebagai `…/64`). Semua ber-TTL (sisa + 1 menit; strike 24 jam + 1 menit). Pembuka darurat: `DEL auth:lock:<user_id>`.
- **Env baru:** `LOGIN_MAX_FAILURES` (5), `LOGIN_FAILURE_WINDOW` (15m), `LOGIN_LOCK_DURATION` (15m), `LOGIN_LOCK_MAX` (60m), `LOGIN_IP_MAX_FAILURES` (30); di `.env.example` dan divalidasi untuk role API.
- **Go API:** `service.LoginGuard` (antarmuka), `Guard`/`NewGuard`/`GuardPolicy`/`DefaultGuardPolicy`/`FailureResult`/`GuardOption` (`WithGuardClock`, `WithGuardRedisTimeout`, `WithGuardMemoryLimit`); `AuthDeps.Guard` (nil hanya untuk tes; `newAuthStack` selalu memasangnya dan gagal bila kebijakan tak sah);
  `domain.ThrottledError`; `service.EventAccountLocked`; `httpserver.RefreshRule`, `httpserver.SwitchOrganizationRule`; `config.Config.Login*`.
- **Tidak ada** migrasi DB, query sqlc baru, atau perubahan `docs/api/swagger.json`.

## 6. Peta kode (tambahan)
- `backend/internal/auth/service/guard.go` — `GuardPolicy` (+`Validate`), `LoginGuard`, `Guard` (skrip Lua `accountFailScript`/`ipFailScript`, `viaRedis` dengan batas 250 ms dan mundur 1 detik memakai jam NYATA, fallback), `lockDuration`. `guard_memory.go` — `memGuard`.
- `backend/internal/auth/service/auth.go` — `Login` (gerbang IP → cari user → kunci aktif → verifikasi → hitung/kunci → reset), helper `guardIP` (IPv6 /64), `guardCtx` (tulis penjaga tak ikut dibatalkan klien, 3 dtk), `dummyCompare`, `guardFailure`, `passwordFailed`, `recordLocked`; `EventAccountLocked`.
- `backend/internal/auth/domain/errors.go` — `ThrottledError`. `backend/internal/auth/transport/http/handlers.go` — `fail` memetakan `ThrottledError` ke `RateLimitExceeded` + `Retry-After`.
- `backend/internal/httpserver/auth_routes.go` — `RefreshRule`, `SwitchOrganizationRule`. `backend/internal/app/auth.go` — `service.NewGuard(rdb, GuardPolicy{…}, log)`. `backend/pkg/config/config.go` — field `Login*`, `validateAuth`. `.env.example`.
- Tes: `service/guard_test.go`, `service/auth_guard_test.go` (harness `newGuardedAuthHarness`, `loginFrom`, `eventCount`, `authKeys`), tambahan `httpserver/auth_routes_test.go`, `pkg/config/config_login_guard_test.go`, `app/auth_internal_test.go`.

## 7. Cara menjalankan & memverifikasi
Dari `/home/projects/zago-core` di `milestone/m03-authentication` (VPS, user root):
```bash
REQUIRE_DOCKER=1 go test ./backend/internal/auth/... ./backend/internal/httpserver/... ./backend/internal/app/... ./backend/pkg/config/... -race -count=1 -p 1
make verify GO_TEST_PARALLEL=1 > ~/verify.log 2>&1; echo "exit=$?"; grep -nE '^(--- FAIL|FAIL|ok |\?)|✗|▶|smoke' ~/verify.log | tail -70
```
**Keadaan yang diharapkan saat chat berikutnya mulai: `make verify` HIJAU (exit=0)** (terakhir: exit=0 di VPS setelah T5c; commit sesudahnya hanya dokumen). Merah = baca pesan galat dulu. `✗ port … sudah dipakai` = bentrok port (atur ulang `.env` lewat `scripts/env-ports.sh --force`), bukan kode.
Flake Docker (partial-3 §8) belum teramati di VPS; bila muncul, jalankan paket itu sendirian, pulihkan Docker, ulangi; jangan ubah kode tanpa bukti.

## 8. Masalah diketahui / utang teknis
- **Berkas `HANDOVER-M03-partial-4.md` hilang** dan **checksum zip ≠ checksum VPS tanpa sebab terbukti** (partial-5 §8; jangan jadikan checksum satu-satunya alasan berhenti bila commit teratas dan jumlah berkas cocok; laporkan selisihnya).
- **Kunci akun bisa dipakai sebagai DoS terarah:** penyerang yang tahu email korban bisa mengunci korban dengan 5 request per periode kunci (maks 60 menit per kunci). Dibatasi oleh batas atas 60 menit dan gerbang IP; tidak ada pembuka kunci administratif (sementara `redis-cli DEL auth:lock:<user_id>`; usulan endpoint admin di M04/M05).
  Pemilik tak diberi tahu di respons; ia melihat `auth.account.locked` di `GET /security/events` setelah masuk (baru bisa setelah kunci berakhir). Notifikasi ke pemilik (email/inbox) = M05/M08.
- **Belum tertangani:** password spraying lintas banyak IP, CAPTCHA/MFA (M15/Fase 2). `LoginRule` (limiter 10/menit) masih per alamat IPv6, bukan /64 (ADR-0020 butir 56; samakan di M15). Saat Redis mati ambang efektif ×jumlah instance dan status memori tak disalin kembali ke Redis (butir 51).
- `AuthService` menerima `Guard == nil` (hanya untuk tes lama); pencegahnya adalah `newAuthStack`. Pertimbangkan membuatnya wajib saat tes lama sudah dipindah ke harness berpenjaga.
- Verifikasi dummy saat kunci aktif tetap memakan CPU Argon2 (dibatasi semaphore `PASSWORD_HASH_CONCURRENCY` dan gerbang IP); penyerang yang membanjiri akun terkunci menghabiskan jatah hash sampai terkena gerbang IP/limiter.
- Dari partial-5 yang masih berlaku: **`Bus` di `app/auth.go` instans sendiri** (M07/M08 harus membuat satu `Bus` bersama); pesan 401 `invalid_credentials` pada ganti password; `ListSecurityEvents` tanpa filter/retensi (`security_events` tumbuh tanpa batas: M06/M15); `ts-sdk` placeholder;
  flake Docker Desktop di laptop; smoke M00 pernah gagal sekali `curl gagal … /health`; smoke membuat Super Admin `smoke-admin@example.org`; `make setup`/`make dev` tidak menjalankan `db-seed`; login tidak mencabut sesi lama pemanggil dan `Set-Cookie` ganda saat cookie basi (ADR-0020 butir 25);
  parameter Argon2 bawaan belum disetel pada perangkat keras deploy; tes waktu-tak-terbedakan hanya menguji kesetaraan jalur; entri Redis basi bisa hidup sampai 1 menit bila penghapusan Redis gagal.
- **Terselesaikan sesi ini:** `refresh`/`switch-organization` tak dibatasi laju (kini 30/menit/pengguna).
- Smoke M03 belum ada: alur ganti password, riwayat event, dan kunci akun belum dibuktikan di stack asli (hanya tes Go).

## 9. Pertanyaan terbuka
1. `account_locked` = 423 (ADR-0020 butir 20; kini HANYA untuk `users.status='locked'`). 2. Batas jumlah sesi aktif per user. 3. Perlukah kegagalan CSRF menulis `security_events` (kini hanya log debug). 4. Login saat pemanggil sudah punya sesi sah: cabut sesi lama otomatis?
5. Identitas platform (Super Admin tanpa keanggotaan) belum bisa `switch-organization`; menunggu M04. 6. Perlukah `GET /auth/csrf` mengembalikan `expires_at`? 7. Haruskah `make setup`/`make dev` (dan compose `migrate`) menjalankan `db-seed`?
8. Prompt password interaktif tanpa echo (dependensi `golang.org/x/term`)? 9. Bolehkah `bootstrap-admin` berjalan di `ENVIRONMENT=production` (kini boleh)?
10. **Baru:** perlukah akun smoke kedua (mis. `smoke-lock@example.org`, dibuat `make bootstrap-admin`, hanya dev/test) untuk membuktikan di stack asli "5 salah → `invalid_credentials` walau password benar → pembuka `redis-cli DEL auth:lock:<uuid>` → login lagi"?
    Usulan: ya, karena kunci tak terlihat tak bisa dibuktikan dengan tes handler saja; jangan memakai Super Admin smoke utama (jangan mengunci akun yang dipakai smoke lain). Alternatif tanpa akun kedua: smoke hanya membuktikan reset (1 password salah lalu sukses) dan kunci cukup dibuktikan oleh tes Go.
11. **Baru:** perlukah endpoint administratif pembuka kunci (M04/M05), dan notifikasi ke pemilik saat kunci dipasang (M05/M08)?
- Terjawab di sesi ini: partial-5 §9 #10 (kunci Redis tak terlihat, ADR-0020 butir 47) dan #11 (`auth.account.locked` ditulis sekali, `unlocked` tidak, butir 48).

## 10. Langkah berikutnya (lanjutan M03) — urutan yang disarankan
**Urutan:** (A') pemeriksaan keadaan repo, (C) smoke M03 + ritual akhir. `make verify GO_TEST_PARALLEL=1` di tiap ◆CP; jangan menyatakan hijau tanpa keluaran dari mesin.
- **(A') Awal chat:** `git status -sb && git log --oneline -8 --decorate && git tag && git fsck --no-dangling 2>&1 | head -3` di VPS; pastikan `git log` memuat `feat(auth): penjaga login terpasang end-to-end …` (§3 butir 1); konfirmasi `make verify` hijau (§7) dan `gofmt -l backend` kosong. Tanyakan apakah partial-4 §4 tersedia.
- **(C) `scripts/smoke/m03.sh`** (pakai `scripts/smoke/auth-lib.sh`: bootstrap → csrf → login → me → logout → me = 401; lalu ganti password: cookie lama = 401 setelah ganti, cookie baru berlaku, login ulang dengan password baru; `GET /security/events` memuat `auth.password.changed`;
  `auth-lib.sh` menyimpan password acak di direktori state 0600: skrip ganti-password harus memperbarui berkas itu atau memakai akun smoke terpisah). **Batas smoke terhadap penjaga (ADR-0020 "Konsekuensi"):** paling banyak SATU password salah per run lalu login sukses (membuktikan reset akun);
  penghitung IP tidak direset oleh sukses, jadi >30 run dengan kegagalan dalam 15 menit dari satu IP menembus gerbang IP; jumlah login m02 + m03 harus < 10/menit (`LoginRule`; verifikasi dengan menghitung `login` di kedua skrip). Jangan mengunci Super Admin smoke. Putuskan pertanyaan #10 dulu.
  Daftarkan skrip di `scripts/verify-smoke.sh` dan `make smoke`; jalankan `make verify`.
- **Ritual akhir** (docs/22 §4.3): doc-sync kumulatif (§4), README, CHANGELOG, `HANDOVER-M03.md`, STATUS, merge ke `main`, tag `m03-done`, PROMPT P2 terisi untuk M04 (termasuk "M04 mengadopsi role `super-admin` by slug" dan seluruh ketentuan checkpoint konteks).

## 11. Prompt untuk chat berikutnya
Upload zip repo terbaru (`bash scripts/repo-zip.sh /root/platform-repo-$(date +%Y%m%d-%H%M).zip` di VPS) dan tempel **PROMPT P5 (lanjutan M03, parsial 6)** yang diberikan bersama handover ini. Pemeriksaan keadaan repo wajib sebelum membangun apa pun:
`git status -sb && git log --oneline -8 --decorate && git tag && git fsck --no-dangling 2>&1 | head -3` — branch `milestone/m03-authentication`, working tree bersih, commit teratas `docs(m03): handover parsial 6, STATUS, CHANGELOG`, tag `m02-done` ada, fsck bersih.
