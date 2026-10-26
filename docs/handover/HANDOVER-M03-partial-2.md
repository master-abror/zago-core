# HANDOVER — M03 Authentication (PARSIAL 2, berhenti di ◆CP2)

**Tanggal:** 2026-10-26 · **Status:** IN PROGRESS · **Jenis:** PARSIAL (T1–T4c dan T4d-1 selesai; T4d-2, T5, T6, smoke M03 + migrasi smoke M02 belum)
**Branch:** `milestone/m03-authentication` (basis: tag `m02-done`, `8e7a8b1`) · **Handover sebelumnya:** `HANDOVER-M03-partial-1.md`
**Commit sejak CP1 (urutan):** wiring auth ke router dan composition root (T4a) · `AuthService` login/logout/profil (T4b-1) · endpoint login/logout/me/csrf (T4b-2) ·
refresh + switch-organization (T4c) · OpenAPI (T4b–T4c) · manajemen sesi (T4d-1) · OpenAPI (T4d-1) · `test(auth)`: UUID tes lewat `id.NewID` · docs handover (commit ini).
SHA lokal berbeda dari milik chat ini; urutan di atas yang berlaku.

## 1. Ringkasan
Autentikasi kini bekerja ujung ke ujung lewat HTTP terhadap PostgreSQL 18 + Redis 8 sungguhan: login (Argon2id, tanpa oracle enumerasi akun, rehash-on-login),
cookie sesi HttpOnly + token CSRF terikat sesi, `me`, `csrf`, logout, `refresh` (rotasi tanpa memperpanjang batas absolut), `switch-organization`, daftar/cabut sesi, dan
`revoke-others`. Rantai `Authenticate → CSRF` terpasang di grup rute API; `/health*` di luar grup. `X-Dev-Actor` sudah dihapus: endpoint echo memakai sesi.
Sisa M03: ganti password + riwayat event keamanan (T4d-2), kunci akun + rate limit per akun (T5), `bootstrap-admin` (T6), smoke M03 dan **migrasi smoke M02 (kini MERAH, lihat §3 dan §8)**.
Tidak ada migrasi DB baru di T4. Tidak ada env baru di T4.

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T1–T3 ◆CP1 | DONE | lihat HANDOVER-M03-partial-1 |
| T4a pasang `Options.Auth` + grup API + echo `RequireAuth` + composition root | DONE | fail closed tanpa Auth; `SessionService` menerima `redis.UniversalClient` |
| T4b-1 `AuthService` (Login, Logout, Profile) | DONE | satu transaksi: sesi + event + activity |
| T4b-2 `POST /auth/login`, `POST /auth/logout`, `GET /auth/me`, `GET /auth/csrf` | DONE | rate limit login 10/menit/IP |
| T4c `POST /auth/refresh`, `POST /auth/switch-organization` | DONE | |
| T4d-1 `GET/DELETE /security/sessions[/{id}]`, `POST /security/sessions/revoke-others` | DONE | |
| T4d-2 `POST /security/password`, `GET /security/events` (keyset) | TODO | §10 |
| T5 kunci akun + rate limit per akun/IP + jeda progresif | TODO | `account_locked` hanya setelah password benar |
| T6 `make bootstrap-admin` | TODO | |
| Migrasi `scripts/smoke/m02.sh` dari `X-Dev-Actor` ke sesi; `scripts/smoke/m03.sh` | TODO | **smoke M02 MERAH sekarang** |
| Ritual akhir (docs/22 §4.3) + doc-sync (§4) | TODO | |

## 3. Bukti verifikasi (apa adanya)
- **`make verify GO_TEST_PARALLEL=1` di mesin pengembang (2026-10-26, WSL2 + Docker Desktop, PG 18 / Redis 8) → exit 2.** Tahap yang LULUS: lint (golangci 0 isu, svelte-check,
  prettier, lint-structure), generate-check, test (21 paket `ok` termasuk `auth/repository`, `auth/service`, `auth/transport/http`, `httpserver`, `infra` dengan e2e auth),
  migrate-roundtrip, build, dan **smoke M00 OK**. Tahap yang MERAH: **smoke M02**, pada langkah "tanpa Idempotency-Key": diharapkan 400, dapat 401
  `authentication_required` — akibat langsung `X-Dev-Actor` dihapus (disengaja). Smoke M02 langkah sebelumnya (echo terpasang, rate limit, pagination, cursor) hijau.
- Setiap patch T4 (`go build ./...`, `go vet`, `go test -race -count=1` pada paket terkait, e2e `infra`) dijalankan di mesin dan hijau sebelum lanjut; `make generate`
  dijalankan dan `docs/api/swagger.json` di-commit dua kali (T4b–T4c, lalu T4d-1). `lint-structure` menangkap empat `uuid.New()` di tes; diperbaiki (`id.NewID`).
- **Dua run `make verify` sebelumnya gagal karena lingkungan, bukan kode** (lihat §8): timeout Docker API, lalu container Redis menolak koneksi di port publikasi.
  Setelah pemulihan Docker (Quit → `wsl --shutdown` → buka lagi → toggle WSL integration) dan `GO_TEST_PARALLEL=1`, run di atas bersih.
- **Sandbox chat tidak punya toolchain Go yang bisa memuat modul** (hanya `gofmt`); kode T4 ditulis tanpa kompilasi di sana. Satu-satunya bukti kompilasi dan tes adalah run di mesin.
- **Tes negatif yang ditambahkan di T4:** email tak dikenal = password salah (kode, pesan, dan verifikasi penuh via hasher pengintai); status akun tak terlihat sebelum password benar;
  password kosong/>256 byte = `invalid_credentials`; rehash tidak terjadi saat login gagal; login atomik (activity gagal = tanpa sesi, tanpa entri Redis); ctx dibatalkan bukan "password salah";
  Origin asing/`null` ditolak pada login anonim dan request terautentikasi; CSRF hilang/milik sesi lain; cookie basi dihapus; galat infrastruktur = 503 bukan anonim;
  `/health*` tidak menyentuh sesi; tanpa `Options.Auth` rute terlindungi 401 dan `/auth/*` 404; `X-Dev-Actor` diabaikan; token CSRF lama gugur setelah refresh;
  switch-organization ke bukan-anggota/invited/suspended/organisasi suspended/tak ada = 403 tanpa efek; sesi pengguna lain tak bisa dicabut/di-refresh/di-logout;
  refresh/switch/revoke atomik terhadap kegagalan activity; `DELETE /sessions/revoke-others` tidak terbaca sebagai `{id}` salah-rute.

## 4. Keputusan & penyimpangan
- **ADR-0020 Bagian 2** (butir 17–32) memuat keputusan T4. Ringkas yang menyimpang atau menetapkan hal yang docs belum tetapkan: `account_locked` = **423**; `deactivated` diperlakukan seperti `suspended`;
  `pending`/`deleted` setelah password benar tetap `invalid_credentials`; login gagal ditulis hanya untuk akun yang dikenal; `refresh` dan rotasi tidak menulis activity; switch-organization hanya ke
  keanggotaan aktif; mencabut sesi saat ini lewat DELETE = logout; nama event keamanan (`auth.login.success|failed`, `auth.logout`, `auth.session.refreshed`, `auth.organization.switched`,
  `auth.session.revoked`, `auth.sessions.revoked_others`); `result` mengikuti CHECK skema (`success|failure`, bukan `failed`).
- **Doc-sync TERTUNDA (lakukan di ritual akhir M03), tambahan atas daftar parsial 1 §4:** docs/05 §14 (katalog event: nama di atas), §17 (daftar endpoint final + `revoke-others` = POST,
  respons `csrf_token`), §18 (kode `invalid_credentials` 401, `account_suspended` 403, `account_locked` 423; `permission_denied` untuk switch-organization); docs/08 §11 (tabel kode error);
  docs/05 §11.1/§12.2 (event rotasi/pindah organisasi); docs/10 (peta paket `auth/*`, `app/auth.go`); CHANGELOG sudah diperbarui di commit ini.

## 5. Delta kontrak (sejak CP1)
- **DB:** tidak ada migrasi.
- **API (semua di OpenAPI `docs/api/swagger.json`, tag `auth` dan `security`):** `POST /api/v1/auth/login` (anonim, rate limit, cek Origin), `POST /api/v1/auth/logout`, `GET /api/v1/auth/me`, `GET /api/v1/auth/csrf`,
  `POST /api/v1/auth/refresh`, `POST /api/v1/auth/switch-organization`, `GET /api/v1/security/sessions`, `DELETE /api/v1/security/sessions/{id}`, `POST /api/v1/security/sessions/revoke-others`.
  Semua `POST/DELETE` terautentikasi menuntut `X-CSRF-Token`. Echo `POST` kini butuh sesi + CSRF (`X-Dev-Actor` dihapus).
- **Kode error baru:** `invalid_credentials` (401), `account_suspended` (403), `account_locked` (423). Dipakai ulang: `authentication_required`, `csrf_failed`, `permission_denied`, `resource_not_found`, `validation_failed`, `rate_limit_exceeded`.
- **Event keamanan (`security_events`):** `auth.login.success`, `auth.login.failed`, `auth.logout`, `auth.session.refreshed`, `auth.organization.switched`, `auth.session.revoked`, `auth.sessions.revoked_others`.
  **Activity:** `auth.login`, `auth.logout`, `auth.switch_organization`, `auth.session.revoke`, `auth.sessions.revoke_others` (refresh tidak menulis activity).
- **Rate limit:** aturan `auth.login` 10/menit per IP (`Sensitive`: fallback memori bila Redis mati).
- **Config/env:** tidak ada yang baru. **Permission baru:** tidak ada.
- **Kode:** `SessionService` menerima `redis.UniversalClient`; `httpserver.Options` punya `Auth` dan `AuthAPI`; `authhttp` punya `Handlers`, `Service`, DTO respons.

## 6. Peta kode (tambahan atas parsial 1 §6)
| Hal | Lokasi |
|---|---|
| Use case login/logout/profil/refresh/switch-organization | `backend/internal/auth/service/auth.go` |
| Use case daftar/cabut sesi | `backend/internal/auth/service/sessions_mgmt.go` |
| Handler auth dan DTO | `backend/internal/auth/transport/http/handlers.go` |
| Handler manajemen sesi | `backend/internal/auth/transport/http/handlers_sessions.go` |
| Kode error auth (`invalid_credentials`, dst.) | `backend/internal/auth/transport/http/errors.go` |
| Pemasangan rute `/auth/*`, `/security/*`, `LoginRule`, `ipRateKey` | `backend/internal/httpserver/auth_routes.go` |
| Grup API (Authenticate → CSRF) dan `Options.Auth/AuthAPI` | `backend/internal/httpserver/router.go` |
| Perakitan hasher + sesi + AuthService + handler dari config | `backend/internal/app/auth.go` |
| Tes: sesi palsu + CSRF nyata (handler) | `httpserver/echo_test.go` (`fakeSessions`, `buildEcho`), `httpserver/auth_routes_test.go` (`fakeAuthSvc`) |
| Tes: service di PG+Redis nyata | `auth/service/auth_test.go`, `auth_rotate_test.go`, `sessions_mgmt_test.go` |
| Tes e2e stack nyata | `backend/internal/infra/auth_e2e_test.go`, `api_e2e_test.go` (`newE2ESession`) |

## 7. Cara menjalankan & memverifikasi
Dari repo di `milestone/m03-authentication` (Nizza AI dihentikan lebih dulu bila memakai port 5432/6379/8080):
```bash
make generate-check
REQUIRE_DOCKER=1 go test ./backend/internal/auth/... ./backend/internal/httpserver/... ./backend/internal/infra/... -race -count=1
make lint
make verify GO_TEST_PARALLEL=1 > ~/verify.log 2>&1; echo "exit=$?"; grep -nE "^(--- FAIL|FAIL|ok |\?)|✗|▶|smoke" ~/verify.log | tail -70
```
**Keadaan yang diharapkan saat chat berikutnya mulai: `make verify` MERAH hanya di smoke M02 ("tanpa Idempotency-Key": 401, bukan 400).** Itu utang yang diketahui, bukan regresi; jangan dibuka sebagai "M03-fix".
Merah di tempat lain = temuan baru.

## 8. Masalah diketahui / utang teknis
- **Smoke M02 merah (disengaja, T4a).** `scripts/smoke/m02.sh` memakai `X-Dev-Actor` untuk POST echo (idempotensi, request id, header klien). Perbaikan butuh user nyata di database → T6 (`bootstrap-admin`) lebih dulu,
  lalu smoke login (cookie + `X-CSRF-Token`) sebelum langkah echo. Bagian GET (rate limit, pagination, cursor) tidak berubah.
- **Flake lingkungan kelas ke-5 (bukan kode):** `make verify` biasa (paralel) gagal dua kali di mesin ini pada 2026-10-25/26. Run 1: `TestReadyFlipsTo503WhenRedisStops` timeout Docker API (`docker.sock ... context deadline exceeded`).
  Run 2 (`GO_TEST_PARALLEL=2`): container Redis melaporkan "Ready" tetapi port publikasi menolak koneksi 60 dtk (`connection refused`), dan karena `testkit.Redis` memakai `sync.Once` galat itu men-cache dan
  menjatuhkan SEMUA tes ber-Redis di `httpserver` dan `infra` seketika. Pemulihan di atas + `GO_TEST_PARALLEL=1` → bersih. Memori Docker Desktop hanya ~3,7 GB (WSL ~3,5 GB) dan dipakai bersama build `-race`, PG 18, Redis 8 berbagai modul;
  kemungkinan kontributor, **tidak terbukti**. Opsi (jangan dikerjakan tanpa bukti baru): `.wslconfig` dengan memori lebih besar; menjadikan `GO_TEST_PARALLEL=1` bawaan lokal; membuat galat start `testkit.Redis` tidak
  menjatuhkan seluruh paket (mis. coba ulang start sekali). Tiga e2e auth baru menambah beban pada paket `infra`.
- Login tidak mencabut sesi lama pemanggil yang masih sah; `Set-Cookie` ganda (hapus basi + baru) saat login dengan cookie basi (ADR-0020 butir 25).
- `refresh` dan `switch-organization` tidak dibatasi laju sendiri (dilindungi sesi + CSRF); pertimbangkan di T5.
- Parameter Argon2 bawaan belum disetel pada perangkat keras deploy (target 50–150 ms/hash); tes memakai parameter kecil.
- Tes waktu-tak-terbedakan hanya menguji kesetaraan jalur (verifikasi penuh), bukan statistik waktu: jangan dipaksakan sebagai tes biasa.
- Hak `app_user` untuk `bootstrap-admin` (platforms, roles, role_assignments, users, credentials) belum diperiksa; kemungkinan butuh role migrator.
- Entri Redis basi bisa hidup sampai 1 menit bila penghapusan Redis gagal (ADR-0020 butir 7). Pembatas laju log "Redis terdegradasi" memakai jam dinding proses (disengaja).

## 9. Pertanyaan terbuka
1. **Dijawab dengan usulan (ADR-0020 butir 20), ubah bila pemilik proyek berbeda:** `account_locked` = 423.
2. Batas jumlah sesi aktif per user (belum ada). 3. Apakah kegagalan CSRF perlu `security_events` (kini hanya log debug; menulis event per request bisa menjadi vektor banjir).
4. Login saat pemanggil sudah punya sesi sah: cabut sesi lama otomatis? (kini tidak). 5. Identitas tingkat platform (super admin tanpa keanggotaan) belum bisa `switch-organization` ke organisasi mana pun; aturannya menunggu M04 (RBAC).
6. Perlukah `GET /auth/csrf` juga mengembalikan `expires_at`? (kini hanya token.)

## 10. Langkah berikutnya (lanjutan M03) — urutan yang disarankan
**Urutan:** (A) T6 + migrasi smoke M02 supaya `make verify` HIJAU lagi sedini mungkin, (B) T4d-2, (C) T5, (D) smoke M03 + ritual akhir. Alasan: gerbang merah lebih lama = bukti makin lemah.
- **(A) T6 `make bootstrap-admin EMAIL=…`:** idempoten (platform row + role Super Admin + user + assignment platform); password dari stdin atau acak sekali-tampil (hasher + `PasswordPolicy`);
  periksa hak role DB lebih dulu (§8). **Baca:** docs/05 (bagian bootstrap), docs/04/06 untuk skema `platforms/roles/role_assignments`, `backend/cmd/` (bentuk binary baru), `Makefile`.
  **Lalu** ubah `scripts/smoke/m02.sh`: bootstrap → `POST /auth/login` (cookie jar curl) → ambil `csrf_token`; semua POST echo membawa cookie + `X-CSRF-Token`; langkah "X-Dev-Actor non-UUID → 400" diganti
  "tanpa sesi → 401" dan "CSRF hilang → 403". Tes yang ada: e2e Go `TestAPIEndToEnd...` dan `TestAuthFlowEndToEnd...` sudah membuktikan alurnya; yang belum ada hanya skrip smoke dan bootstrap.
- **(B) T4d-2 (tes dulu):** `POST /security/password` (docs/05 §13.1: verifikasi password saat ini → `PasswordPolicy` → hash baru → tx: ganti hash + security event + activity + `RevokeOthers` → rotasi sesi
  saat ini → cookie + `csrf_token` baru; password salah = `invalid_credentials` dan TIDAK mengubah apa pun; periksa apakah §13.1 meminta event outbox). Query `SetPasswordHash`/`GetActivePasswordCredential` sudah ada.
  `GET /security/events` keyset (kursor ber-HMAC dari kernel httpx; `ListSecurityEventsByUser` sudah ada; hanya event milik pemanggil — tes negatif: tidak bisa membaca event pengguna lain).
  Pola: tambahkan metode di `service/auth.go` (atau berkas baru), handler di `authhttp`, rute di `httpserver/auth_routes.go`, perluas `fakeSvc` (handlers_test) dan `fakeAuthSvc` (auth_routes_test).
- **(C) T5:** kunci akun Redis `auth:lock:{user_id}` (TTL, 5 gagal), penghitung per akun/IP + jeda progresif; `account_locked` hanya setelah password benar (kini `AuthService.Login` hanya memetakan `status='locked'`).
  Titik sentuh: `service/auth.go` (`Login`, `recordFailure`), `httpserver/auth_routes.go` (`LoginRule`).
- **(D) Exit:** `scripts/smoke/m03.sh` (bootstrap → csrf → login → me → logout → me = 401); lalu ritual akhir docs/22 §4.3 (doc-sync §4 di atas, README, HANDOVER-M03.md, merge, tag `m03-done`).
- **Risiko:** menyentuh smoke M02 dan Makefile bersamaan dengan fitur baru; jalankan `make verify GO_TEST_PARALLEL=1` di tiap ◆CP. Jangan menyatakan hijau tanpa keluaran dari mesin.

## 11. Prompt untuk chat berikutnya
Upload zip repo terbaru (`scripts/repo-zip.sh`) dan tempel **PROMPT P2 (lanjutan M03)** yang diberikan bersama handover ini. Pemeriksaan keadaan repo wajib sebelum membangun apa pun:
`git status -sb && git log --oneline -6 --decorate && git tag` — branch `milestone/m03-authentication`, working tree bersih, commit teratas `docs(m03): handover parsial 2, ADR-0020 bagian 2, STATUS, CHANGELOG`, tag `m02-done` ada.
