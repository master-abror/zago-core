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
