# HANDOVER — M03 Authentication (PARSIAL 3, berhenti di ◆CP3)

**Tanggal:** 2026-10-27 · **Status:** IN PROGRESS · **Jenis:** PARSIAL (T1–T4c, T4d-1, T6 + migrasi smoke M02 selesai; T4d-2, T5, smoke M03 belum)
**Branch:** `milestone/m03-authentication` (basis: tag `m02-done`, `8e7a8b1`) · **Handover sebelumnya:** `HANDOVER-M03-partial-2.md` (dan `partial-1`)
**Commit sejak CP2 (urutan):** `feat(bootstrap)` Super Admin pertama (T6a) · `feat(auth)` `api bootstrap-admin` + `make bootstrap-admin` (T6b) · `test(smoke)` migrasi smoke M02 ke sesi nyata (T6c) · docs handover (commit ini).
SHA di mesin pengembang: `a159b1e`, `dae7427`, `8b622b2`; SHA di chat lain berbeda, urutan di atas yang berlaku.

## 1. Ringkasan
`make bootstrap-admin EMAIL=…` kini nyata: membuat Super Admin pertama (role sistem `super-admin`, user, kredensial Argon2id, assignment platform) dalam satu transaksi, idempoten, tidak pernah menimpa password,
dan hanya butuh hak `app_user`. Smoke M02 kembali hijau dengan sesi nyata (bootstrap → login → cookie + `X-CSRF-Token`), sehingga **`make verify` HIJAU lagi** (utang smoke M02 dari parsial 2 lunas).
Sisa M03: ganti password + riwayat event keamanan (T4d-2), kunci akun + rate limit per akun (T5), smoke M03, ritual akhir. Tidak ada migrasi DB dan tidak ada env baru di T6.

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T1–T3 ◆CP1, T4a–T4c, T4d-1 ◆CP2 | DONE | lihat parsial 1 dan 2 |
| T6 `api bootstrap-admin` + `make bootstrap-admin` | DONE | ADR-0020 butir 33–41; `internal/bootstrap`, `internal/app/bootstrap.go` |
| Migrasi `scripts/smoke/m02.sh` ke sesi nyata | DONE | `scripts/smoke/auth-lib.sh`; `make smoke` menyediakan `SMOKE_STATE_DIR` |
| T4d-2 `POST /security/password`, `GET /security/events` (keyset) | TODO | §10 (B) |
| T5 kunci akun + rate limit per akun/IP + jeda progresif | TODO | §10 (C) |
| `scripts/smoke/m03.sh` | TODO | §10 (D) |
| Ritual akhir (docs/22 §4.3) + doc-sync (§4) | TODO | |

## 3. Bukti verifikasi (apa adanya)
- **`make verify GO_TEST_PARALLEL=1` di mesin pengembang (2026-10-27, WSL2 + Docker Desktop, PG 18 / Redis 8) → exit=0.** Semua paket test `ok` (termasuk `internal/bootstrap`, `httpserver`, `systemlog`, `auth/*`, `infra`), smoke M00 OK,
  **smoke M02 OK** dengan langkah baru: "Super Admin smoke: make db-seed + make bootstrap-admin", "login → cookie sesi + csrf_token", "Idempotency-Key wajib → 400", "tanpa sesi → 401", "sesi tanpa X-CSRF-Token → 403",
  POST echo 201 (termasuk `actor_id` = pengguna yang login), replay, konflik kunci. Itu satu-satunya bukti untuk bootstrap-admin dan smoke di stack asli.
- **Tidak terbukti oleh keluaran yang dilampirkan:** lint/generate-check/migrate-roundtrip/build tidak muncul di `grep` verify; hijaunya disimpulkan dari `exit=0`.
- **Terbukti di sandbox chat (bukan bukti utama):** `gofmt` bersih, `lint-structure` OK, `bash -n` untuk skrip smoke, dan lapisan bash `auth-lib.sh` terhadap server HTTP tiruan (cookie jar, `csrf_token`, password via stdin, reuse
  password antar skrip, penolakan BASE non-localhost). Sandbox kini punya `gofmt` (dan `go`, tetapi modul `golang.org` tidak bisa diunduh), jadi kompilasi dan tes Go tetap hanya dibuktikan di mesin.
- **Insiden lingkungan (bukan kode), terjadi berturut-turut pada 2026-10-26/27:**
  1. Repo lokal sempat korup: `git am` gagal (`object file … is empty`), HEAD menunjuk objek 0 byte, dan file yang disentuh patch menjadi 0 byte. Disk tidak penuh (946 GB kosong); Docker daemon saat itu mati. Penyebab pasti tidak terbukti
     (dugaan: crash/restart Docker Desktop atau WSL di tengah penulisan). Dipulihkan oleh pengguna; setelahnya `./tmp` dibuat Docker milik root (volume anonim `/app/tmp` di compose) dan diperbaiki dengan `chown`.
  2. Empat run `make verify` berturut-turut merah di test dengan penyebab berbeda tetapi satu kelas (lihat §8): timeout start Ryuk ×2 (`wait for reaper … deadline exceeded`), `connection refused` di port publikasi PostgreSQL ×1,
     dan sekali `curl gagal … /health` pada smoke M00 padahal log api mencatat request itu dijawab 200. Tiap paket yang merah lulus saat dijalankan sendirian, dan run berikutnya (setelah pemulihan Docker) hijau.
- **Tes negatif yang ditambahkan di T6:** password lemah/terlalu panjang ditolak tanpa baris apa pun; email tak sah (kosong, tanpa `@`, dua `@`, `Nama <a@b>`, `<a@b>`, terlalu panjang) dan nama tampilan > 200 ditolak; platform row hilang = rollback
  penuh (role pun tidak tertinggal); user `pending|suspended|locked|deactivated|deleted` dan soft-deleted tidak diubah; run kedua tidak menambah baris apa pun (termasuk audit) dan tidak menimpa hash; assignment yang dicabut dibuat ulang tanpa
  menyentuh password; 6 pemanggil bersamaan menghasilkan satu admin; metadata audit tanpa email/password; CLI: input buruk = `ExitConfig` tanpa menyentuh DB (DB tak terjangkau), galat koneksi tidak membocorkan password DB, password stdin
  tidak pernah dipantulkan ke stdout/stderr, password acak hanya tercetak sekali.

## 4. Keputusan & penyimpangan
- **ADR-0020 Bagian 3** (butir 33–41): subcommand `api bootstrap-admin`; hak `app_user` cukup; satu transaksi + advisory lock; role Super Admin minimal (M04 mengadopsi lewat upsert by slug `super-admin`); idempoten tanpa menimpa password;
  password stdin/acak sekali-tampil tanpa dependensi baru; audit `auth.bootstrap_admin`; smoke dengan password acak per `make smoke`; login nyata dibuktikan di smoke.
- **Doc-sync TERTUNDA (lakukan di ritual akhir M03), tambahan atas daftar parsial 1 §4 dan parsial 2 §4:** docs/16 (bentuk perintah `api bootstrap-admin --email/--name/--password-stdin`, variabel `EMAIL`/`NAME`/`PASSWORD_STDIN` pada make, `make smoke`
  dengan `SMOKE_STATE_DIR`); docs/04 §16 langkah 4 (rincian: platform row prasyarat, role dibuat minimal); docs/00 E:B-04 (role Super Admin dibuat oleh bootstrap, M04 mengadopsi by slug); docs/15 (urutan `make db-seed` sebelum
  `make bootstrap-admin`; compose `migrate` hanya `up`); docs/05 §14 (event `auth.bootstrap_admin` di katalog); docs/19 (smoke M02 butuh Go dan DB terjangkau dari host); README (alur setup dari nol). CHANGELOG sudah diperbarui di commit ini.

## 5. Delta kontrak (sejak CP2)
- **CLI:** `go run ./backend/cmd/api bootstrap-admin --email <email> [--name <nama>] [--password-stdin]`; `make bootstrap-admin EMAIL=… [NAME=…] [PASSWORD_STDIN=1]`. Kode keluar: 0 sukses (juga "tidak ada perubahan"), 2 argumen/konfigurasi/input tidak valid,
  1 kegagalan runtime (DB tak terjangkau, platform row belum ada, user tak aktif). Keluaran password acak: baris `password: …` di stdout. Butuh environment API lengkap (role API) dan `make db-seed` lebih dulu.
- **Audit baru:** activity dan security event `auth.bootstrap_admin` (`actor_type='system'`, scope platform).
- **Smoke:** `scripts/smoke/auth-lib.sh` (di-source; `smoke_admin_session` mengisi `JAR`, `CSRF`, `ADMIN_ID`); env `SMOKE_ADMIN_EMAIL`, `SMOKE_ADMIN_PASSWORD`, `SMOKE_STATE_DIR`. `m02.sh`: `X-Dev-Actor`/`SMOKE_ACTOR` dihapus, `NEEDED=17`.
- Tidak ada perubahan HTTP API, skema DB, atau env aplikasi.

## 6. Peta kode (tambahan)
- `backend/internal/bootstrap/bootstrap.go` — `Bootstrapper` (`New`, `Validate`, `Run`), `NormalizeEmail`, error `ErrInvalidInput|ErrNoPlatform|ErrUserUnavailable`, konstanta `SuperAdminSlug`, `EventBootstrapAdmin`. SQL langsung via pgx (pola `dbmigrate/seed.go`, bukan sqlc).
- `backend/internal/bootstrap/{main_test,bootstrap_test,cli_test}.go` — tes `Bootstrapper` (PG sungguhan, role `app_user`) dan tes CLI (`app.BootstrapAdmin`).
- `backend/internal/app/bootstrap.go` — `BootstrapAdmin`, `readPasswordLine`, `redactSecrets`. `backend/cmd/api/main.go` — dispatch `bootstrap-admin` sebelum `RunAPI`.
- `Makefile` — target `bootstrap-admin` dan `smoke` (direktori state sementara). `scripts/smoke/auth-lib.sh`, `scripts/smoke/m02.sh`.

## 7. Cara menjalankan & memverifikasi
Dari repo di `milestone/m03-authentication` (Nizza AI dihentikan lebih dulu bila memakai port 5432/6379/8080):
```bash
REQUIRE_DOCKER=1 go test ./backend/internal/bootstrap/... ./backend/internal/app/... -race -count=1 -p 1
make verify GO_TEST_PARALLEL=1 > ~/verify.log 2>&1; echo "exit=$?"; grep -nE "^(--- FAIL|FAIL|ok |\?)|✗|▶|smoke" ~/verify.log | tail -70
# uji manual (stack dev berjalan): make db-seed && make bootstrap-admin EMAIL=you@example.org
```
**Keadaan yang diharapkan saat chat berikutnya mulai: `make verify` HIJAU (exit=0).** Merah di mana pun = flake lingkungan (§8) sampai terbukti sebaliknya: baca pesan galatnya, jalankan paket itu sendirian, pulihkan Docker bila perlu; JANGAN ubah kode
tanpa bukti. Gejala flake: `wait for reaper … context deadline exceeded`, `connection refused` di port publikasi, tes pertama habis ~60–66 dtk lalu sisanya gagal 0,00 dtk.

## 8. Masalah diketahui / utang teknis
- **Flake lingkungan Docker Desktop (kelas ke-5, makin sering; tidak terbukti penyebabnya).** Pemulihan: Quit Docker Desktop → `wsl --shutdown` → buka lagi → toggle WSL integration, lalu cek port publikasi:
  `docker run -d --rm --name porttest -p 127.0.0.1:0:80 nginx:alpine` + `curl` ke port yang dipetakan harus HTTP 200. Memori Docker Desktop ~3,5 GB. Opsi (jangan dikerjakan tanpa bukti baru): `.wslconfig` dengan memori lebih besar; `GO_TEST_PARALLEL=1` bawaan lokal;
  `testkit` mencoba ulang start container sekali. `TESTCONTAINERS_RYUK_DISABLED=true` menyentuh ADR-0017 (jangan sebagai default).
- **Smoke M00 pernah gagal sekali (`curl gagal … /health`)** padahal log api mencatat `GET /health` dari gateway Docker dijawab 200 dan tidak ada rebuild air; run berikutnya hijau. Penyebab tak terbukti (dugaan: respons hilang di jalur Docker Desktop ↔ WSL).
  Bila berulang, ubah `curl -s` jadi `curl -sS` di helper `http()` (m00/m02) supaya galat curl tampil di log.
- **Smoke membuat Super Admin `smoke-admin@example.org`** di stack tempat ia dijalankan (password acak dibuang di akhir `make smoke`, jadi akun itu tak bisa dipakai login lagi). Aman untuk stack verify (di-`down -v`); di stack dev akun itu tertinggal.
  Skrip menolak BASE non-localhost. Pertimbangkan membersihkannya di akhir bila dibutuhkan.
- `make setup` dan `make dev` tidak menjalankan `db-seed`; compose `migrate` hanya `up` (§9 #7).
- Dari parsial 2 yang masih berlaku: login tidak mencabut sesi lama pemanggil dan `Set-Cookie` ganda saat cookie basi (ADR-0020 butir 25); `refresh`/`switch-organization` tidak dibatasi laju sendiri (pertimbangkan di T5);
  parameter Argon2 bawaan belum disetel pada perangkat keras deploy; tes waktu-tak-terbedakan hanya menguji kesetaraan jalur; entri Redis basi bisa hidup sampai 1 menit bila penghapusan Redis gagal.
- Lunas di parsial 3: smoke M02 merah; hak `app_user` untuk `bootstrap-admin` (cukup, ADR-0020 butir 34).

## 9. Pertanyaan terbuka
1. `account_locked` = 423 (usulan, ADR-0020 butir 20). 2. Batas jumlah sesi aktif per user. 3. Perlukah kegagalan CSRF menulis `security_events` (kini hanya log debug). 4. Login saat pemanggil sudah punya sesi sah: cabut sesi lama otomatis?
5. Identitas platform (Super Admin tanpa keanggotaan) belum bisa `switch-organization`; aturannya menunggu M04. 6. Perlukah `GET /auth/csrf` mengembalikan `expires_at`?
7. **Baru:** haruskah `make setup`/`make dev` (dan compose `migrate`) menjalankan `db-seed` supaya alur dari nol tidak gagal dengan `ErrNoPlatform`? (kini pengguna harus `make db-seed` sendiri; pesan galat menunjukkannya.)
8. **Baru:** perlukah prompt password interaktif tanpa echo (butuh dependensi `golang.org/x/term`)? Kini hanya stdin atau acak (ADR-0020 butir 38).
9. **Baru:** bolehkah `bootstrap-admin` berjalan di `ENVIRONMENT=production`? Kini boleh (itu alur normal instalasi pertama); beri tahu bila ingin penjaga tambahan (mis. menolak bila sudah ada Super Admin lain).

## 10. Langkah berikutnya (lanjutan M03) — urutan yang disarankan
**Urutan:** (B) T4d-2, (C) T5, (D) smoke M03 + ritual akhir. `make verify GO_TEST_PARALLEL=1` di tiap ◆CP; jangan menyatakan hijau tanpa keluaran dari mesin.
- **(B) T4d-2 (tes dulu):** `POST /security/password` (docs/05 §13.1: verifikasi password saat ini → `PasswordPolicy` → hash baru → tx: ganti hash + security event + activity + `RevokeOthers` → rotasi sesi saat ini → cookie + `csrf_token` baru;
  password salah = `invalid_credentials` dan TIDAK mengubah apa pun; periksa apakah §13.1 meminta event outbox). Query `SetPasswordHash`/`GetActivePasswordCredential` sudah ada. `GET /security/events` keyset (kursor ber-HMAC dari kernel httpx;
  `ListSecurityEventsByUser` sudah ada; hanya event milik pemanggil — tes negatif: tidak bisa membaca event pengguna lain). Pola: metode di `service/auth.go` (atau berkas baru), handler di `authhttp`, rute di `httpserver/auth_routes.go`,
  perluas `fakeSvc` (handlers_test) dan `fakeAuthSvc` (auth_routes_test). Anotasi swagger lalu `make generate`.
- **(C) T5:** kunci akun Redis `auth:lock:{user_id}` (TTL, 5 gagal), penghitung per akun/IP + jeda progresif; `account_locked` hanya setelah password benar (kini `AuthService.Login` hanya memetakan `status='locked'`).
  Titik sentuh: `service/auth.go` (`Login`, `recordFailure`), `httpserver/auth_routes.go` (`LoginRule`). Pertimbangkan laju untuk `refresh`/`switch-organization`. **Awas:** smoke M02/M03 login berulang dari satu IP; jangan sampai T5 mengunci Super Admin smoke.
- **(D) Exit:** `scripts/smoke/m03.sh` (pakai `auth-lib.sh`: bootstrap → csrf → login → me → logout → me = 401; bila T4d-2 selesai, ganti password lalu login ulang); lalu ritual akhir docs/22 §4.3 (doc-sync §4 di tiga handover, README,
  HANDOVER-M03.md, merge, tag `m03-done`, PROMPT P2 terisi untuk M04 — termasuk catatan "M04 mengadopsi role `super-admin` by slug").

## 11. Prompt untuk chat berikutnya
Upload zip repo terbaru (`scripts/repo-zip.sh`) dan tempel **PROMPT P4 (lanjutan M03, parsial 3)** yang diberikan bersama handover ini. Pemeriksaan keadaan repo wajib sebelum membangun apa pun:
`git status -sb && git log --oneline -6 --decorate && git tag` — branch `milestone/m03-authentication`, working tree bersih, commit teratas `docs(m03): handover parsial 3, ADR-0020 bagian 3, STATUS, CHANGELOG`, tag `m02-done` ada.
