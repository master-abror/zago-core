# HANDOVER — M03 Authentication (PARSIAL 5, berhenti di ◆CP5)

**Tanggal:** 2026-10-27 · **Status:** IN PROGRESS · **Jenis:** PARSIAL (T1–T4d, T6 + migrasi smoke M02 + task (A) lingkungan VPS selesai; T5 dan smoke M03 belum)
**Branch:** `milestone/m03-authentication` (basis: tag `m02-done`, `8e7a8b1`) · **Handover sebelumnya:** `HANDOVER-M03-partial-3.md` (dan `partial-2`, `partial-1`). **`HANDOVER-M03-partial-4.md` TIDAK ADA di repo ini**
(patch-nya dibuat di chat sebelumnya tetapi tidak pernah diterapkan di VPS; lihat §8). Partial-5 adalah rujukan terbaru.
**Commit sejak a3aef49 (urutan):** `feat(dev)` port host compose (ADR-0021) · `docs(dev)` ADR-0021 + runbook VPS (`0810369`) · `docs(adr)` ADR-0020 bagian 4 · `feat(auth)` ChangePassword + ListSecurityEvents di layanan (T4d-2a) ·
`feat(auth)` `POST /security/password` + `GET /security/events` (T4d-2b) · `chore(generate)` OpenAPI untuk T4d-2 (`36dbde0`, dibuat di VPS) · docs handover (commit ini). SHA lain di VPS berbeda dari sandbox chat; urutan di atas yang berlaku.

## 1. Ringkasan
T4d-2 selesai dan terbukti di VPS: `POST /api/v1/security/password` (verifikasi password saat ini → kebijakan → hash di luar tx → satu tx: CAS hash, rotasi sesi, cabut sesi lain, event keamanan, activity, event outbox
`auth.password.changed`) dan `GET /api/v1/security/events` (hanya milik sesi pemanggil, keyset, tanpa metadata). Keputusan #11 (Idempotency-Key) dan #12 (event outbox) ditetapkan di ADR-0020 bagian 4.
Task (A) lingkungan VPS selesai (Go 1.26.5 sesuai pin ADR-0002; `make verify` hijau). Sisa M03: **T5** (kunci akun + rate limit per akun + jeda progresif), **smoke M03**, ritual akhir.

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T1–T3 ◆CP1, T4a–T4c, T4d-1 ◆CP2, T6 + migrasi smoke M02 ◆CP3 | DONE | partial-1..3 |
| (A) lingkungan VPS (Go 1.26.5, `.env` via `env-ports.sh`, baseline `make verify`) | DONE | §3 |
| ADR-0021 port host dev + VPS | DONE | `docs/adr/0021-dev-host-ports-and-vps.md`, `deploy/vps/README.md` |
| T4d-2 `POST /security/password`, `GET /security/events` | DONE | ADR-0020 butir 42–46; commit `feat(auth)` ×2 + `chore(generate)` |
| T5 kunci akun Redis + rate limit per akun/IP + jeda progresif | TODO | §10 (B) |
| `scripts/smoke/m03.sh` | TODO | §10 (C) |
| Ritual akhir (docs/22 §4.3) + doc-sync (§4) | TODO | |

## 3. Bukti verifikasi (apa adanya; keluaran dari VPS pengguna, 2026-10-27)
- **Lingkungan:** `go version` = go1.26.5 setelah `go env -w GOTOOLCHAIN=go1.26.5` (pin ADR-0002 dipertahankan). Percobaan pertama `make verify` exit=2 karena `✗ port 15432 sudah dipakai` (container lain memegang 15432 dan 18080):
  pemeriksaan port di `verify-smoke.sh` bekerja sesuai desain. `.env` dibuat `env-ports.sh --force` dengan port bebas rentang 25000–25005 (WEB 25003).
  Baseline di commit `0810369` (sebelum T4d-2): `make verify GO_TEST_PARALLEL=1` exit=0, smoke M00 (8 langkah) dan M02 (13 langkah) OK; tes `auth/service` 17,1 dtk di VPS vs 53,1 dtk di laptop.
- **Setelah T4d-2 (patch `0001` dan `0002` diterapkan, sebelum commit generate):** `gofmt -l backend && go build ./... && go vet ./backend/internal/auth/... ./backend/internal/httpserver/... ./backend/internal/app/...` tanpa keluaran (bersih);
  `REQUIRE_DOCKER=1 go test ./backend/internal/auth/... ./backend/internal/httpserver/... ./backend/internal/app/... -race -count=1 -p 1` → semua `ok`
  (`auth/repository` 7,1 dtk, `auth/service` 20,8 dtk, `auth/transport/http` 5,2 dtk, `httpserver` 6,5 dtk, `app` 1,9 dtk).
- `make generate` mengubah hanya `docs/api/swagger.json` (+190 baris: skema `ChangePasswordRequest/Response`, `SecurityEventView`, dua rute baru); di-commit sebagai `36dbde0`. `ts-sdk` masih placeholder ("belum ada kontrak OpenAPI").
- **`make verify GO_TEST_PARALLEL=1` setelah commit generate → exit=0.** Semua paket `ok` (app, audit, audit/repository, auth/repository, auth/service, auth/transport/http, bootstrap, health, httpserver, infra, kernel, kernel/httpx,
  systemlog, testkit, testpg, migrations, pkg/*), smoke M00 OK, smoke M02 OK (login Super Admin smoke, Idempotency-Key wajib, tanpa sesi 401, tanpa CSRF 403, echo 201, replay, konflik 422).
- **Tidak terbukti oleh keluaran yang dilampirkan:** lint, generate-check, migrate-roundtrip, build tidak muncul di `grep` verify; hijaunya disimpulkan dari `exit=0`. Alur ganti password dan riwayat event **belum dibuktikan di stack asli**
  (belum ada smoke; hanya tes Go dengan PostgreSQL + Redis sungguhan dan handler/rute dengan layanan palsu).
- **Terbukti di sandbox chat (bukan bukti utama):** `gofmt` bersih, `lint-structure` OK, `git am` pada salinan bersih zip menghasilkan pohon identik. Sandbox tidak bisa memuat modul Go: kompilasi dan tes selalu menunggu keluaran VPS.
- **Tes negatif yang ditambahkan di T4d-2:** password saat ini salah/kosong/>256 byte = `invalid_credentials`, hash dan sesi tak berubah, tak ada event/outbox sukses, event `auth.password.change_failed` dicatat; verifikasi password lama mendahului kebijakan;
  password baru pendek/terlalu panjang/sama dengan yang lama ditolak tanpa efek samping dan tanpa event kegagalan; identitas dengan sesi yang sudah dicabut tidak bisa mengganti password (rollback penuh); user tanpa credential password ditolak
  tanpa membuat credential; atomik bila activity gagal ditulis (hash, sesi, event, outbox tak berubah); dua perubahan bersamaan dari sesi yang sama: tepat satu menang, yang kalah `invalid_credentials`/`ErrUnauthenticated`, satu event, satu outbox;
  sesi pengguna lain tak tersentuh; payload outbox dan metadata tak memuat password/hash/token. Handler: body kosong/rusak/field asing/terlalu besar ditolak sebelum layanan; kegagalan tak pernah menyentuh cookie; password tak pernah dipantulkan;
  kesalahan infrastruktur menjadi 500 tanpa bocor. Rute: tanpa sesi 401, tanpa/salah CSRF atau Origin asing 403 (layanan tak dipanggil), tanpa `Idempotency-Key` tetap 200, rate limit 5/menit per PENGGUNA (pengguna lain satu IP tak terkunci).
  Riwayat event: hanya milik pemanggil (pengguna lain tak terlihat), terbaru dulu, keyset tanpa celah/duplikat, limit < 1 dipotong, kursor palsu/limit tak sah = 400 tanpa memanggil layanan, `?user_id=` diabaikan, bidang terpublikasi persis delapan (tanpa `metadata`/`request_id`).

## 4. Keputusan & penyimpangan
- **ADR-0020 Bagian 4** (butir 42–46): `POST /security/password` TIDAK memakai `Idempotency-Key` (menyimpang dari docs/08 §6: respons merotasi sesi, `Set-Cookie` tak pernah disimpan ADR-0014 #7, request ulang dengan cookie lama ditolak 401 sebelum idempotency;
  pelindung = CAS hash + rotasi dalam satu tx); event outbox `auth.password.changed` via `kernel.Bus` dalam tx yang sama (payload `{user_id, revoked_other_sessions}`; relay menandai event tanpa handler `processed`); urutan ganti password; event
  `auth.password.change_failed` untuk password saat ini yang salah (TIDAK menaikkan kunci akun); aturan `security.password` 5/menit per pengguna (Sensitive); `GET /security/events` hanya milik sendiri, bidang whitelist, tanpa `metadata`/`request_id`.
- **ADR-0021** (port host dev + VPS): lihat dokumennya; "Konsekuensi" memuat doc-sync tertunda sendiri.
- **Doc-sync TERTUNDA KUMULATIF (lakukan di ritual akhir M03):**
  - **partial-1 §4:** docs/05 §4.1 (antarmuka ber-`ctx`, `CompareDummy`, hash dummy acak), §7 (nilai Redis memuat last_activity_at; batas basi 1 interval), §7.2, §8 (nama cookie `__Host-` hanya bila Secure), §9 (urutan Authenticate→CSRF), §12.1 (sumber timeout),
    §17 (semantik refresh), §19 (`service/csrf.go`, paket `authhttp`); docs/08 §11 (kode baru dan statusnya); docs/10 §8 (env baru `PASSWORD_*`, `SESSION_IDLE_TIMEOUT`, `SESSION_ABSOLUTE_TIMEOUT`); README.
  - **partial-2 §4:** docs/05 §14 (katalog event: `auth.login.success|failed`, `auth.logout`, `auth.session.refreshed`, `auth.organization.switched`, `auth.session.revoked`, `auth.sessions.revoked_others`), §17 (daftar endpoint final + `revoke-others` = POST,
    respons `csrf_token`), §18 (kode `invalid_credentials` 401, `account_suspended` 403, `account_locked` 423; `permission_denied` untuk switch-organization); docs/08 §11; docs/05 §11.1/§12.2; docs/10 (peta paket `auth/*`, `app/auth.go`).
  - **partial-3 §4:** docs/16 (bentuk `api bootstrap-admin --email/--name/--password-stdin`, variabel `EMAIL`/`NAME`/`PASSWORD_STDIN`, `make smoke` dengan `SMOKE_STATE_DIR`); docs/04 §16 langkah 4; docs/00 E:B-04 (role Super Admin dibuat bootstrap, M04 mengadopsi by slug);
    docs/15 (urutan `make db-seed` sebelum `make bootstrap-admin`; compose `migrate` hanya `up`); docs/05 §14 (`auth.bootstrap_admin`); docs/19 (smoke M02 butuh Go dan DB terjangkau dari host); README (alur setup dari nol).
  - **ADR-0021 "Konsekuensi":** docs/15 §2 (port host dapat disetel; `env-ports.sh`), docs/16 §2.7 (`verify-smoke` membaca port), docs/19 (CI tidak terpengaruh), README (alur pengembangan di VPS).
  - **Baru (partial-5, ADR-0020 bagian 4):** docs/08 §6 (hapus `POST /security/password` dari daftar Idempotency-Key; catatan di ADR-0014), docs/05 §13.1 (event outbox terwujud + urutan butir 44), docs/05 §14 (`auth.password.changed`, `auth.password.change_failed`),
    docs/08 §10.1 (respons password/events; `limit` bawaan 50 maks 100), docs/10 (peta paket: `service/password.go`, `service/security_events.go`, `transport/http/handlers_security.go`), docs/08 §11 (tak ada kode error baru).
  - **BELUM TERGABUNG: partial-4 §4.** Berkas `HANDOVER-M03-partial-4.md` tidak ada di repo. Bila pengguna memilikinya (unduhan chat sebelumnya), minta isinya (`sed -n '/^## 4/,/^## 5/p'`) dan gabungkan daftar itu di sini sebelum ritual akhir.

## 5. Delta kontrak (sejak CP3)
- **API baru (OpenAPI `docs/api/swagger.json`, tag `security`):** `POST /api/v1/security/password` body `{current_password, new_password}` → 200 `{csrf_token, expires_at, revoked_other_sessions}` + cookie sesi baru, `Cache-Control: no-store`;
  galat: 400 `validation_failed` (field `current_password|new_password`; kode field `required|too_short|too_long|same_as_current`), 401 `authentication_required` atau `invalid_credentials` (password saat ini salah; sesi TETAP berlaku, cookie tak diubah),
  403 `csrf_failed`, 429 `rate_limit_exceeded`. `GET /api/v1/security/events?limit&cursor` → `data: [{id,event_type,result,created_at,ip_address,user_agent,organization_id,session_id}]`, `meta.pagination{next_cursor,has_more}`.
- **Event keamanan baru:** `auth.password.changed` (success; metadata `previous_session_id`, `revoked_other_sessions`), `auth.password.change_failed` (failure; metadata `reason=bad_current_password`). Activity baru: `auth.password.change` (resource `user`).
- **Event outbox baru:** `auth.password.changed` (`aggregate_type=user`, `aggregate_id`=user, `organization_id`=organisasi sesi atau NULL). Belum ada konsumen (M05/M08).
- **Kode/aturan:** rate limit `security.password` 5/menit per pengguna (`httpserver.PasswordRule`). Tidak ada kode error baru.
- **Go API:** `AuthDeps` + `Policy`, `Bus`; `authhttp.HandlerDeps` + `Cursors` (WAJIB); `authhttp.Service` + `ChangePassword`, `ListSecurityEvents`; `domain.ErrSamePassword`; `service.ChangePasswordInput/Result`, `SecurityEventInfo`, `EventPosition`.
- **Tidak ada** migrasi DB, query sqlc baru, atau env baru.

## 6. Peta kode (tambahan)
- `backend/internal/auth/service/password.go` — `ChangePassword`, `publishPasswordChanged`, `recordPasswordChangeFailed`, konstanta `EventPasswordChanged|EventPasswordChangeFailed`.
- `backend/internal/auth/service/security_events.go` — `ListSecurityEvents` (`SecurityEventInfo`, `EventPosition`, batas halaman 1000 di layanan).
- `backend/internal/auth/service/auth.go` — `AuthDeps.Policy/Bus`, `recordResource` (record activity per jenis resource).
- `backend/internal/auth/transport/http/handlers_security.go` — `ChangePassword`, `ListSecurityEvents` (+ anotasi swagger); `handlers.go` — `fail` memetakan `ErrPasswordTooShort|TooLong|ErrSamePassword` ke `validation_failed` field `new_password`.
- `backend/internal/httpserver/auth_routes.go` — `PasswordRule`, `userRateKey`, rute baru (RequireAuth sebelum limiter). `backend/internal/app/auth.go` — `kernel.NewBus(pool, log)`, `cfg.PasswordPolicy()`, `httpx.NewCursorCodec(cfg.SessionSecret)`.
- Tes: `service/{password_change,security_events}_test.go`, `transport/http/handlers_security_test.go`, tambahan di `httpserver/auth_routes_test.go`; harness `newAuthHarness` kini memasang `kernel.NewBus`.

## 7. Cara menjalankan & memverifikasi
Dari `/home/projects/zago-core` di `milestone/m03-authentication` (VPS, user root):
```bash
REQUIRE_DOCKER=1 go test ./backend/internal/auth/... ./backend/internal/httpserver/... ./backend/internal/app/... -race -count=1 -p 1
make verify GO_TEST_PARALLEL=1 > ~/verify.log 2>&1; echo "exit=$?"; grep -nE '^(--- FAIL|FAIL|ok |\?)|✗|▶|smoke' ~/verify.log | tail -70
```
**Keadaan yang diharapkan saat chat berikutnya mulai: `make verify` HIJAU (exit=0)** (terakhir terbukti di VPS pada commit `36dbde0`; commit sesudahnya hanya dokumen). Merah = baca pesan galat dulu. `✗ port … sudah dipakai` = bentrok port
(atur ulang `.env` lewat `scripts/env-ports.sh --force`), bukan kode. Flake Docker (partial-3 §8) belum teramati di VPS; bila muncul, jalankan paket itu sendirian, pulihkan Docker, ulangi; jangan ubah kode tanpa bukti.

## 8. Masalah diketahui / utang teknis
- **Berkas `HANDOVER-M03-partial-4.md` hilang dari repo** (§ atas), dan **checksum zip ≠ checksum VPS tanpa sebab terbukti**: zip `platform-repo-20261027-1228.zip` dan zip dari VPS identik isi (`diff -rq` kosong), tetapi
  `LC_ALL=C git ls-files -z | sort -z | xargs -0 sha256sum | sha256sum` di VPS (`743a7990…`) berbeda dari hitungan atas pohon zip (`af02b89e…`), padahal jumlah berkas sama (358). Dugaan: himpunan berkas yang dihitung `git ls-files` berbeda dari isi zip
  (mis. berkas terlacak yang dikecualikan pola `-x` di `repo-zip.sh`, atau berkas tak terlacak). Diagnostik bila perlu: `git ls-files | sort > /root/ls.txt; unzip -Z1 <zip> | grep -vc '/$'; unzip -Z1 <zip> | grep -v '/$' | sort | diff /root/ls.txt -`.
- **`Bus` di `app/auth.go` adalah instans sendiri** (`kernel.NewBus(pool, log)`): belum ada instans bersama di composition root. M07/M08 harus membuat satu `Bus` dan menyuntikkannya ke auth, atau handler in-process tak akan terhubung. Event outbox tetap sampai ke relay (durable).
- Pesan 401 `invalid_credentials` pada ganti password adalah "Invalid email or password." (kode registri yang sama dengan login). Klien harus mengandalkan `code`, bukan menganggap 401 dari endpoint ini sebagai sesi habis (sesi tetap berlaku).
- `ListSecurityEvents` tak punya filter dan tak membatasi usia; `security_events` tumbuh tanpa batas (kebijakan retensi = M06/M15).
- Smoke M03 belum ada: alur ganti password dan riwayat event belum dibuktikan di stack asli (hanya tes Go).
- Dari partial-3 yang masih berlaku: flake Docker Desktop di laptop (kelas ke-5; belum teramati di VPS); smoke M00 pernah gagal sekali `curl gagal … /health` (bila berulang: `curl -sS` di helper `http()`); smoke membuat Super Admin `smoke-admin@example.org` di stack tempat ia
  dijalankan (password acak dibuang di akhir `make smoke`); `make setup`/`make dev` tidak menjalankan `db-seed`; login tidak mencabut sesi lama pemanggil dan `Set-Cookie` ganda saat cookie basi (ADR-0020 butir 25); `refresh`/`switch-organization` tak dibatasi laju
  sendiri (pertimbangkan di T5); parameter Argon2 bawaan belum disetel pada perangkat keras deploy; tes waktu-tak-terbedakan hanya menguji kesetaraan jalur; entri Redis basi bisa hidup sampai 1 menit bila penghapusan Redis gagal.
- `ts-sdk` masih placeholder (kontrak OpenAPI mulai dikonsumsi nanti).

## 9. Pertanyaan terbuka
1. `account_locked` = 423 (usulan, ADR-0020 butir 20). 2. Batas jumlah sesi aktif per user. 3. Perlukah kegagalan CSRF menulis `security_events` (kini hanya log debug). 4. Login saat pemanggil sudah punya sesi sah: cabut sesi lama otomatis?
5. Identitas platform (Super Admin tanpa keanggotaan) belum bisa `switch-organization`; menunggu M04. 6. Perlukah `GET /auth/csrf` mengembalikan `expires_at`? 7. Haruskah `make setup`/`make dev` (dan compose `migrate`) menjalankan `db-seed`?
8. Prompt password interaktif tanpa echo (dependensi `golang.org/x/term`)? 9. Bolehkah `bootstrap-admin` berjalan di `ENVIRONMENT=production` (kini boleh)?
10. **Baru:** haruskah login yang berhasil dengan kunci akun aktif (T5) tetap ditolak `account_locked` hanya setelah password benar (usulan: ya, sesuai docs/05 §18) atau selalu `invalid_credentials` selama kunci Redis aktif (lebih aman terhadap enumerasi, tetapi pemilik tak tahu sebabnya)?
11. **Baru:** perlukah event `auth.account.locked`/`auth.account.unlocked` (katalog docs/05 §14) ditulis saat kunci Redis dipasang/habis? (usulan: tulis `locked` saat dipasang, `unlocked` tidak ditulis karena TTL tidak punya pemicu).
- Terjawab di sesi ini: partial-4 §9 #10 (Go 1.26.5 terpasang, pin dipertahankan), #11 (tanpa Idempotency-Key, ADR-0020 butir 42), #12 (event outbox ditulis, butir 43).

## 10. Langkah berikutnya (lanjutan M03) — urutan yang disarankan
**Urutan:** (A') pemeriksaan keadaan repo, (B) T5, (C) smoke M03 + ritual akhir. `make verify GO_TEST_PARALLEL=1` di tiap ◆CP; jangan menyatakan hijau tanpa keluaran dari mesin.
- **(A') Awal chat:** `git status -sb && git log --oneline -8 --decorate && git tag && git fsck --no-dangling 2>&1 | head -3` di VPS; konfirmasi `make verify` hijau (§7). Tanyakan apakah partial-4 §4 tersedia (§4 baris "BELUM TERGABUNG").
- **(B) T5 (tes dulu; tes NEGATIF wajib):** kunci akun sementara di Redis `auth:lock:{user_id}` (TTL, mis. 5 kegagalan berturut dalam jendela), penghitung kegagalan per akun dan per IP, jeda progresif, event keamanan `auth.account.locked` (pertanyaan #11),
  `account_locked` (423) hanya SETELAH password benar (pertanyaan #10; docs/05 §12.3 dan §18). Bukan `users.status='locked'` (itu kunci administratif manual tanpa TTL). **Aturan dari ADR-0020 butir 45:** kegagalan ganti password TIDAK menaikkan penghitung kunci
  (pencuri sesi tak boleh bisa mengunci pemilik); login sukses mereset penghitung akun. Titik sentuh: `service/auth.go` (`Login`, `recordFailure`; kini hanya memetakan `status='locked'`), `httpserver/auth_routes.go` (`LoginRule` 10/menit/IP saja),
  config/env baru bila ada (+ `.env.example` + tes `env_example_test.go`). Pertimbangkan laju untuk `refresh`/`switch-organization`. Redis mati: pembatasan akun harus tetap berlaku (fallback memori, bukan fail open) atau gagal tertutup untuk login; putuskan di ADR-0020 bagian 5.
  **Awas:** smoke M02/M03 login berulang dari satu IP/akun; jangan sampai T5 mengunci Super Admin smoke (`scripts/smoke/auth-lib.sh`). Tes lock harus memakai waktu/TTL yang bisa dikendalikan (hindari tidur nyata).
- **(C) Exit:** `scripts/smoke/m03.sh` (pakai `auth-lib.sh`: bootstrap → csrf → login → me → logout → me = 401; lalu ganti password: cookie lama = 401 setelah ganti, cookie baru berlaku, login ulang dengan password baru; `GET /security/events` memuat `auth.password.changed`;
  perhatikan `auth-lib.sh` menyimpan password acak di direktori state 0600: skrip ganti-password harus memperbarui berkas itu atau memakai akun smoke terpisah). Lalu ritual akhir docs/22 §4.3: doc-sync kumulatif (§4), README, CHANGELOG, `HANDOVER-M03.md`, STATUS,
  merge ke `main`, tag `m03-done`, PROMPT P2 terisi untuk M04 (termasuk "M04 mengadopsi role `super-admin` by slug" dan seluruh ketentuan checkpoint konteks).

## 11. Prompt untuk chat berikutnya
Upload zip repo terbaru (`bash scripts/repo-zip.sh /root/platform-repo-$(date +%Y%m%d-%H%M).zip` di VPS) dan tempel **PROMPT P4 (lanjutan M03, parsial 5)** yang diberikan bersama handover ini. Pemeriksaan keadaan repo wajib sebelum membangun apa pun:
`git status -sb && git log --oneline -8 --decorate && git tag && git fsck --no-dangling 2>&1 | head -3` — branch `milestone/m03-authentication`, working tree bersih, commit teratas `docs(m03): handover parsial 5, STATUS, CHANGELOG`, tag `m02-done` ada, fsck bersih.
