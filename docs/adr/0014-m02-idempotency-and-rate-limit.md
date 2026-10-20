# ADR-0014 — M02/T4c: middleware idempotency dan rate limiter (Redis)

**Status:** Diterima · **Tanggal:** 2026-10-10 · **Keputusan oleh:** sesi M02 (usulan; pemilik proyek dapat membatalkan)

## Konteks
docs/08 §6–§7, errata I-07, dan docs/10 §15 menetapkan perilaku inti (Redis 24 jam, kunci `idem:{user_id}:{method}:{route}:{key}`,
409/422, fail closed; trio header `X-RateLimit-*`, fallback memori untuk endpoint sensitif) tetapi tidak merinci: respons mana yang
disimpan, header mana yang diputar ulang, nasib kunci bila handler gagal/panic, urutan 422 vs 409, semantik `X-RateLimit-Reset`,
algoritma jendela, dan ukuran "konservatif" fallback.

## Keputusan — idempotency
1. **Lokasi**: `internal/kernel/httpx` (docs/10 §3). `Idempotency.Require()` adalah middleware untuk route `Idempotent()` (07 §5.1);
   identitas (`Subject`) dan pola rute (`Route`) disuntikkan pemanggil sehingga paket tidak bergantung pada auth atau chi. Tanpa
   subject → 401 `authentication_required` (kunci tak boleh dibuat tanpa scope user).
2. **Format `Idempotency-Key`**: 1–128 karakter dari `A-Za-z0-9._~:-`; kosong/tak valid → 400 `validation_failed` (field `Idempotency-Key`).
3. **Hash request** = SHA-256(method, route, query string mentah, body). Body dibaca penuh (batas 1 MiB, lebih → 413) lalu dikembalikan
   ke handler.
4. **Urutan keputusan** saat kunci sudah ada: hash beda → 422 `idempotency_key_conflict` (diperiksa lebih dulu, termasuk saat yang
   pertama masih berjalan); hash sama + masih berjalan → 409 `idempotency_in_progress`; hash sama + selesai → putar ulang.
5. **Penguncian**: `SET NX` nilai "sedang berjalan" dengan TTL 60 dtk (umur kunci bila proses mati) memuat token pemilik acak;
   penyelesaian dan pelepasan lewat skrip Lua yang hanya berlaku bila token pemilik masih cocok, sehingga request lambat yang
   kuncinya sudah kedaluwarsa tidak menimpa hasil request lain. Hasil disimpan 24 jam.
6. **Yang disimpan hanya respons 2xx.** Respons non-2xx (termasuk 5xx dan panic) melepas kunci supaya klien dapat mencoba ulang dengan
   key yang sama; menyimpan kegagalan akan mengunci klien selama 24 jam (mis. 401 karena sesi habis). Respons > 1 MiB tidak disimpan
   dan kunci dilepas (pemutaran ulang setengah-setengah lebih buruk daripada eksekusi ulang); endpoint idempotent diharapkan
   mengembalikan body kecil. Klien selalu menerima respons asli utuh.
7. **Header yang diputar ulang**: hanya `Content-Type` dan `Location`, ditambah `Cache-Control: no-store` dan
   `Idempotent-Replayed: true`. `Set-Cookie` dan header sesi/CSRF tidak pernah disimpan.
8. **Redis tak tersedia / rekaman rusak → 503 `service_unavailable`** dan handler tidak dieksekusi (fail closed, docs/10 §15).

## Keputusan — rate limiter
9. **Fixed window** lewat satu skrip Lua atomik (`INCR` + `PEXPIRE` pada hit pertama, TTL diperbaiki bila hilang). Kekurangan yang
   diterima: hingga 2× Limit dapat lolos di batas dua jendela; untuk login/reset password pembatas per-akun terpisah (M03) menutupnya.
   Sliding window dapat menggantikannya tanpa mengubah antarmuka.
10. **Header**: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset` (= **detik epoch Unix** akhir jendela, dibulatkan ke
    atas; docs/08 tidak menetapkan epoch vs selisih). 429 membawa `Retry-After` (detik, minimal 1) dan envelope `rate_limit_exceeded`.
    Bila beberapa aturan berlaku pada satu route, trio header dari aturan dengan sisa paling sedikit yang ditampilkan.
11. **Redis tak tersedia**: aturan `Sensitive` memakai fallback memori per-instance dengan batas **Limit/2 (minimal 1)**, dibatasi
    10.000 identitas aktif — penuh → identitas baru ditolak (fail closed); aturan biasa **fail open** tanpa header dan dicatat. Setelah
    satu galat Redis, limiter langsung memakai fallback selama 1 dtk (tanpa menunggu timeout 250 ms per request) lalu mencoba lagi.
12. **Identitas** dipilih pemanggil lewat `KeyFunc` (mis. `ip:<RealIP>` atau `user:<uuid>`); string kosong = tidak dibatasi.

## Konsekuensi
- Pemasangan ke composition root (klien Redis, `Subject` dari `kernel.ActorFromContext`, `Route` dari pola chi, aturan login M03)
  dilakukan di T5 (`/api/v1/_kernel/echo`) dan milestone pemilik endpoint.
- Tes memakai Redis 8 asli (`testkit.Redis`); skenario Redis mati memakai klien ke port tertutup.
