# ADR-0020 — M03: keputusan desain autentikasi (bagian 1: hasher, sesi, CSRF, cookie)

**Status:** Diterima (bagian 1, setelah ◆CP1 / T1–T3) · **Tanggal:** 2026-10-22 · **Konteks:** M03, docs/05
Bagian berikutnya (T4–T6: login, kunci akun, bootstrap) ditambahkan ke ADR ini di akhir milestone.

## Konteks
docs/05 menetapkan arsitektur autentikasi tetapi membiarkan beberapa hal terbuka atau saling bertabrakan. Daftar di bawah mencatat
setiap keputusan yang diambil saat membangun T1–T3 dan, bila menyimpang dari teks dokumen, alasannya.

## Keputusan
1. **Hasher menerima `context`.** `PasswordHasher.Hash/Compare/CompareDummy` memakai `ctx` (sketsa docs/05 §4.1 tidak). Antrean semaphore
   konkurensi harus bisa dibatalkan, bila tidak banjir login menumpuk goroutine tanpa batas. Password > 256 byte ditolak **sebelum** menunggu
   semaphore. `NeedsRehash` memperlakukan hash tak terbaca sebagai "perlu diganti"; `Compare` membedakan `ErrMismatch` dari `ErrInvalidHash`.
2. **Hash dummy dibuat acak per instans** saat konstruksi, dengan parameter yang sama dengan hash sungguhan (tidak ada nilai tetap di kode).
   Email tak dikenal menempuh satu derivasi Argon2id penuh di bagian kritis yang sama (I-04).
3. **Rehash-on-login bersifat compare-and-swap** (`RehashPasswordIfUnchanged`: `WHERE secret_hash = <hash lama>`): bila password diganti
   bersamaan, rehash dari password lama tidak boleh menimpanya.
4. **Kebijakan password:** minimum 12 karakter (rune) bawaan, dapat disetel `PASSWORD_MIN_LENGTH` 8–128; maksimum 256 byte tetap.
   Parameter Argon2 lewat `PASSWORD_ARGON2_*` (bawaan 64 MiB, t=3, p=2); divalidasi hanya untuk role API.
5. **Timeout sesi dari konfigurasi dulu.** docs/05 §12.1 meminta idle timeout dari `settings` (platform). M03 memakai `SESSION_IDLE_TIMEOUT`
   (30m) dan `SESSION_ABSOLUTE_TIMEOUT` (12h) lewat antarmuka `TimeoutSource`; sumber settings dapat dipasang nanti tanpa mengubah layanan sesi.
6. **Redis menyimpan `last_activity_at`** di nilai sesi (tambahan atas docs/05 §7) supaya timeout idle diperiksa tanpa PostgreSQL per request.
   Pembaruan aktivitas paling sering sekali per menit per sesi. Saat Redis mati, pembaruan **dibatasi laju per sesi, bukan di-batch**
   (docs/05 §7.2 menyebut batch): lebih sederhana dan cukup, karena jalur terdegradasi sudah membaca PostgreSQL per request.
7. **Entri Redis basi dibatasi satu interval aktivitas.** Efek ke Redis dan Pub/Sub dijalankan setelah commit (`kernel.AfterCommit`). Bila
   penghapusan Redis gagal setelah pencabutan, entri basi hidup paling lama satu interval (1 menit): pembaruan aktivitas berikutnya
   (`TouchSessionActivity`, `GREATEST`, 0 baris = dicabut) memeriksa PostgreSQL dan menolaknya. Terbukti oleh
   `TestStaleRedisEntryAfterRevocationIsBoundedByTheActivityInterval`.
8. **Redis kosong/hilang tidak mengeluarkan siapa pun:** miss di Redis selalu dilanjutkan ke PostgreSQL, lalu Redis diisi ulang.
   Token berformat salah ditolak sebelum menyentuh penyimpanan.
9. **`POST /auth/refresh` = rotasi token + idle clock mulai ulang; batas absolut TIDAK bertambah.** docs/05 §17 menulis "extends the
   session within its absolute limit", sedangkan §12.1 menyatakan sesi tidak bisa hidup melewati `expires_at`. Keduanya dipenuhi dengan
   tafsir ini (ditetapkan pemilik proyek 2026-10-21: refresh ikut M03). `Rotate` mempertahankan `expires_at`.
10. **Organisasi awal sesi = keanggotaan aktif tertua** pada organisasi aktif dan tidak dihapus (docs tidak menetapkan). Tanpa keanggotaan =
    sesi tingkat platform (`organization_id` NULL).
11. **Urutan middleware: Authenticate → CSRF → RequireAuth**, bukan CSRF → Authentication seperti diagram docs/05 §9: token CSRF terikat ke
    id sesi, jadi sesi harus dikenali lebih dulu. Jaminan inti tetap: tidak ada request pengubah-state yang mencapai handler tanpa lolos CSRF.
    Cookie tidak sah tidak ditolak di middleware ini (request lanjut anonim, cookie dihapus); 401 adalah tugas `RequireAuth`.
    Kegagalan infrastruktur menjadi 503, bukan 401.
12. **CSRF:** `HMAC-SHA256(kunci turunan SESSION_SECRET, id sesi)` dengan label domain `platform/auth/csrf/v1`; tanpa penyimpanan; ikut
    gugur pada setiap rotasi. Origin, bila dikirim, harus persis salah satu `ALLOWED_ORIGINS` (juga untuk request terautentikasi, sebagai
    pertahanan berlapis). Request anonim tanpa Origin (klien non-peramban) lolos tahap Origin.
13. **Cookie:** `HttpOnly; SameSite=Lax; Path=/`, tanpa `Domain`; nama efektif `__Host-` + `COOKIE_NAME` bila `COOKIE_SECURE=true`, tanpa
    prefiks di pengembangan (peramban menolak `__Host-` tanpa Secure). `Max-Age` = sisa umur absolut sesi.
14. **Kode error:** semua kegagalan sesi (tak dikenal, dicabut, kedaluwarsa, idle) menjadi satu `authentication_required` (401) — tak ada
    oracle; `session_expired`/`session_revoked` tetap dicadangkan di docs/05 §18 tetapi tidak dipancarkan. `csrf_failed` = 403.
15. **`session.revoked`** diterbitkan ke channel Redis Pub/Sub `session.revoked` (`{session_id, user_id, reason}`, tanpa token/hash) setelah
    commit. Jalur outbox untuk event yang sama belum dibuat; Hub WebSocket (M08) yang memutuskan konsumsinya. Rotasi menerbitkan `reason=rotated`
    agar Hub dapat memilih mempertahankan koneksi.
16. **`go.mod`:** `golang.org/x/crypto` menjadi dependensi langsung (hanya perpindahan baris; `go.sum` tidak berubah).

## Konsekuensi
- Tidak ada migrasi baru di T1–T3; blok sqlc kedua (`internal/auth/repository`) ditambahkan di `sqlc.yaml`.
- Doc-sync yang tertunda sampai akhir M03 dicatat di HANDOVER-M03-partial-1 §4.
- Risiko diterima: sampai satu menit entri Redis basi bila penghapusan Redis gagal (butir 7); dapat dipersempit dengan mengecilkan interval.

## Bagian 2 — keputusan T4 (◆CP2, 2026-10-26)
17. **Rantai `Authenticate → CSRF` dipasang di grup rute API** (`httpserver.Options.Auth`); `/health*` berada di luar grup (probe tidak menyentuh sesi dan tidak terblokir saat sesi terganggu).
    Tanpa `Auth`, rute yang butuh identitas menolak SEMUA request dengan 401 (fail closed), dan `/auth/*` tidak dipasang (404) kecuali Kernel, Auth, dan AuthAPI semuanya ada.
18. **`X-Dev-Actor` dihapus.** POST echo memakai `RequireAuth`; subject idempotensi tetap dari `kernel.Actor` yang kini diisi `Authenticate` (tidak ada kode baru untuk subject).
19. **`SessionService` menerima `redis.UniversalClient`** (satu klien composition root; `*redis.Client` tetap memenuhi).
20. **Login: satu kegagalan untuk semua sebab sebelum password terverifikasi.** `invalid_credentials` (401) untuk email tak dikenal, akun tanpa credential password, password salah, kosong atau >256 byte;
    verifikasi Argon2id SELALU berjalan (`CompareDummy` bila tak ada hash). Setelah password benar: `suspended` dan `deactivated` → `account_suspended` (403); `locked` → `account_locked` (**423**,
    jawaban atas pertanyaan terbuka parsial 1 #1; docs/08 §11 tidak menetapkan); `pending` dan `deleted` tetap `invalid_credentials`.
21. **Login sukses = SATU transaksi:** organisasi awal, rehash atau sentuh credential, `last_login_at`, sesi, event keamanan, dan activity `auth.login`. Kegagalan menulis activity = rollback total
    (tidak ada sesi dan tidak ada entri Redis; entri Redis ditulis setelah commit). Rehash dihitung sebelum transaksi (CPU berat di luar tx) dan gagalnya rehash tidak menggagalkan login.
22. **Login gagal dicatat (`auth.login.failed`, result `failure`, metadata `reason` = `bad_password` | `status_<status>`) HANYA untuk akun yang dikenal.** Email tak dikenal atau tanpa credential tidak menulis baris
    (tak ada pemilik yang berhak melihatnya; banjir tebakan tidak boleh jadi banjir tulis). Best-effort (dilog bila gagal), tanpa activity, memakai konteks tanpa pembatalan.
23. **Nama event keamanan:** `auth.login.success`, `auth.login.failed`, `auth.logout`, `auth.session.refreshed`, `auth.organization.switched`, `auth.session.revoked`, `auth.sessions.revoked_others`.
    `result` mengikuti CHECK skema (`success` | `failure`; bukan `failed`). Metadata tidak pernah memuat email, password, atau token. Activity: `auth.login`, `auth.logout`, `auth.switch_organization`,
    `auth.session.revoke`, `auth.sessions.revoke_others`; refresh tidak menulis activity (frekuensi tinggi; event keamanan cukup).
24. **Rate limit login:** aturan `auth.login` 10/menit per IP, `Sensitive` (fallback memori bila Redis mati, bukan fail open). Kunci per akun, jeda progresif, dan kunci akun menyusul di T5.
25. **Bentuk respons:** token sesi hanya di cookie; `csrf_token` di body; `Cache-Control: no-store` pada login/me/csrf/refresh/switch/daftar sesi. Login anonim tetap melewati cek Origin. Login dengan cookie basi
    menghasilkan dua `Set-Cookie` (hapus lalu baru; peramban memakai yang terakhir). **Login tidak mencabut sesi lama pemanggil** (belum diputuskan pemilik proyek; pertanyaan terbuka parsial 2 #4).
26. **Logout** idempoten (sesi yang sudah tak ada bukan kesalahan) dan butuh sesi (anonim = 401); bila pencabutan gagal, cookie dibiarkan agar klien bisa mengulang.
27. **Refresh** = rotasi (ADR-0020 butir 9): token dan CSRF baru, organisasi dan batas absolut sama, token CSRF lama gugur; event membawa `previous_session_id`.
28. **Switch-organization:** keanggotaan aktif pada organisasi aktif diperiksa dan token dirotasi dalam SATU transaksi. Semua penolakan (organisasi tak ada, bukan anggota, keanggotaan `invited`/`suspended`,
    organisasi `suspended`) = `ErrNotOrganizationMember` → 403 `permission_denied` tanpa efek samping dan tanpa oracle keberadaan organisasi. Event dan activity dicatat di organisasi TUJUAN.
    Pindah ke organisasi yang sama tetap merotasi. Identitas tingkat platform belum bisa memasuki organisasi tanpa keanggotaan (menunggu M04).
29. **Manajemen sesi:** daftar hanya sesi aktif milik pemanggil, tanpa token atau hash, dengan penanda `current`; `DELETE /security/sessions/{id}` memberi 404 seragam (ID cacat, tak dikenal, milik orang lain,
    sudah dicabut); mencabut sesi saat ini = logout (cookie dihapus); `revoke-others` tanpa event bila yang dicabut nol; pencabutan atomik bersama event dan activity.
30. **Kode error baru:** `invalid_credentials` 401, `account_suspended` 403, `account_locked` 423. Dipakai ulang: `authentication_required`, `csrf_failed`, `permission_denied`, `resource_not_found`.
31. **OpenAPI:** endpoint `auth` dan `security` beranotasi swagger dan masuk `docs/api/swagger.json`; echo tetap dikecualikan (`kernel-dev`).
32. **Tes:** handler memakai sesi palsu (`fakeSessions`) dengan CSRF dan cookie nyata; service dan e2e memakai PostgreSQL + Redis sungguhan. UUID di tes dibangkitkan lewat `id.NewID` (lint-structure).

## Konsekuensi (bagian 2)
- Tidak ada migrasi dan tidak ada env baru di T4. Smoke M02 merah sampai `bootstrap-admin` (T6) memungkinkan login di skrip (HANDOVER-M03-partial-2 §8, §10).
- Doc-sync tambahan yang tertunda sampai akhir M03: HANDOVER-M03-partial-2 §4.

## Bagian 3 — keputusan T6 (◆CP3, 2026-10-27)
33. **`bootstrap-admin` adalah subcommand binary `api`** (`go run ./backend/cmd/api bootstrap-admin --email …`, docs/16 §2.4), bukan binary baru. Logika di `internal/bootstrap`; wiring tipis `app.BootstrapAdmin`
    (pola `app.Migrate`: argumen/konfigurasi/input buruk = `ExitConfig` SEBELUM menyentuh database; kegagalan runtime = `ExitFailed`). Konfigurasi dimuat dengan role API (environment yang sama dengan `api`); yang dipakai hanya
    `DATABASE_URL`, parameter Argon2id, dan `PASSWORD_MIN_LENGTH`.
34. **Hak DB: cukup `app_user`** (`DATABASE_URL`); tidak ada DDL dan tidak butuh role migrator. Menutup utang parsial 2 §8. Dibuktikan oleh tes integrasi yang memakai role `app_user` dan oleh smoke di stack asli.
35. **Satu transaksi, diserialkan advisory lock** `0x7a61676f02` (berbeda dari kunci seeder platform `…01`). Urutan: platform row harus sudah ada (`ErrNoPlatform` bila belum → jalankan `make db-seed`; bootstrap TIDAK membuat
    baris platform) → role Super Admin → user → kredensial password → assignment platform → activity + security event. Gagal di langkah mana pun = rollback total (tak ada role/user setengah jadi).
36. **Role Super Admin dibuat minimal:** `role_type='system'`, slug `super-admin`, `organization_id`/`group_id` NULL, tanpa permission. **M04 wajib mengadopsi baris ini lewat upsert by slug `super-admin`** saat menyinkronkan
    role sistem dari kode (E:B-04), bukan membuat baris kedua (`uq_roles_global_slug` akan menolaknya).
37. **Idempoten dan tidak pernah menimpa password.** Run berulang tidak menambah baris apa pun (termasuk audit). User aktif yang belum punya kredensial password mendapat satu; yang sudah punya tidak disentuh. Assignment yang
    dicabut dibuat ulang (baris baru). User dengan email itu yang tidak `active` atau soft-deleted → `ErrUserUnavailable`, tidak diubah apa pun (pemulihan akun bukan tugas bootstrap). Email dinormalisasi ke huruf kecil
    dan hanya alamat polos yang diterima (bukan `Nama <a@b>`).
38. **Password:** satu baris dari stdin (`--password-stdin`; CR/LF akhir dibuang; ditolak `PasswordPolicy` bila < minimum atau > 256 byte) atau acak ≥ 24 karakter (`crypto/rand`, sampling seragam, tanpa karakter yang mudah tertukar).
    Password acak dicetak SEKALI sebagai baris `password: …` di stdout (peringatan "simpan sekarang" di stderr) dan hanya bila transaksi commit dan kredensial benar-benar dibuat. Password tak pernah lewat argv, environment, atau log;
    galat koneksi disamarkan (password dan URL). **Tidak ada prompt interaktif tanpa-echo** (butuh `golang.org/x/term`, dependensi baru; ditunda — lihat handover parsial 3 §9).
39. **Audit:** activity `auth.bootstrap_admin` (`actor_type='system'`, `resource_type='user'`, `scope_type='platform'`) dan security event `auth.bootstrap_admin` (result `success`) pada transaksi yang sama dengan perubahan state.
    Metadata hanya penanda boolean (`user_created`, `role_created`, `credential_created`, `assignment_created`, `password_generated`): tanpa email, tanpa password. Run tanpa perubahan tidak menulis audit. Tidak ada event outbox (belum ada konsumen).
40. **Smoke M02 memakai sesi nyata** (`scripts/smoke/auth-lib.sh`, di-source): `make db-seed` + `make bootstrap-admin` (password dari stdin) lalu `POST /auth/login` dengan cookie jar curl dan `X-CSRF-Token`. Password Super Admin smoke ACAK per
    eksekusi `make smoke` (disimpan 0600 di direktori sementara yang dihapus di akhir; tidak ada password tetap di repo). Skrip menolak `BASE` selain localhost karena MEMBUAT Super Admin. Langkah "X-Dev-Actor non-UUID" diganti
    "tanpa sesi → 401" dan "CSRF hilang → 403"; POST echo juga menegaskan `actor_id` = pengguna yang login.
41. **Login nyata dengan kredensial hasil bootstrap dibuktikan di smoke (stack asli), bukan di tes Go:** harness Redis + `SessionService` hidup di paket tes lain. Tes Go membuktikan hash Argon2id yang tersimpan cocok dengan
    password (acak atau stdin) lewat hasher yang sama dengan login.

## Konsekuensi (bagian 3)
- Tidak ada migrasi dan tidak ada env baru di T6. `make bootstrap-admin` kini nyata (`EMAIL=…`, opsional `NAME=…`, `PASSWORD_STDIN=1`); `make smoke` menyediakan satu direktori state sementara untuk semua skrip.
- Alur dev dari nol: `make setup` dan `make dev` menjalankan `migrate-up` tetapi TIDAK `db-seed`; `make db-seed` harus dijalankan sebelum `make bootstrap-admin` (compose `migrate` juga hanya `up`). Dicatat di handover parsial 3 §9.
- Doc-sync tambahan yang tertunda sampai akhir M03: HANDOVER-M03-partial-3 §4.
