# ADR-0012 — M02/T4a: registry kode error, envelope, dan decode body di `kernel/httpx`

**Status:** Diterima · **Tanggal:** 2026-10-10 · **Keputusan oleh:** sesi M02 (usulan; pemilik proyek dapat membatalkan)

## Konteks
docs/08 §3 dan §11 menetapkan envelope `{data,meta}`/`{error}` dan registry kode error datar; errata I-08 menambah
`internal_error`, `version_conflict`, `payload_too_large`, `unsupported_media_type`, `idempotency_in_progress`.
Dua hal tidak tercakup: (a) kode untuk rute tak dikenal dan metode salah (M00 memakai `not_found` dan `method_not_allowed`;
`not_found` tidak ada di registry, `method_not_allowed` juga tidak), (b) siapa yang mendaftarkan kode domain.

## Keputusan
1. **Toolkit tinggal di subpaket `internal/kernel/httpx`** (docs/10 §3 menyebut toolkit bagian kernel; handover §10 mengizinkan
   subpaket). `httpx` hanya bergantung pada stdlib dan `pkg/logger`, jadi tidak menarik pgx ke paket transport.
2. **Rute tak dikenal memakai `resource_not_found` (404)**, sesuai registry, menggantikan `not_found` buatan M00.
   `scripts/smoke/m00.sh` diperbarui di commit yang sama. `internal_error` tetap 500 tanpa teks asli.
3. **`method_not_allowed` (405) ditambahkan ke registry** sebagai kode umum baru (docs/08 §11 tidak memilikinya). Alasan:
   405 adalah respons HTTP yang pasti terjadi dan klien perlu kode stabil untuknya. Doc-sync: tambahkan ke 08 §11 dan
   pemetaan HTTP-nya saat doc-sync akhir M02.
4. **Registry terdesentralisasi**: `httpx.Register(Code{Name, Status, Message})` dipanggil saat init oleh paket pemilik kode.
   Kernel hanya mendaftarkan kode umum + `authentication_required`, `permission_denied`, `rate_limit_exceeded` (pemetaan HTTP
   eksplisit di 08 §11). Kode auth/otorisasi/modul/realtime/audit lain didaftarkan milestone pemiliknya (M03, M04, M07, M08, M06),
   karena docs/08 §11 tidak menetapkan status HTTP-nya. `unsupported_api_version` ditunda ke M15 (status tak ditetapkan).
   Mendaftar ulang kode identik diizinkan; nama sama dengan definisi berbeda, nama tak valid (`snake_case` atau
   `modul.snake_case`), atau status non-4xx/5xx → panic saat init (bug pemrograman).
5. **Mapper error**: `*Error` (termasuk yang dibungkus `%w`) dirender apa adanya; `*http.MaxBytesError` → `payload_too_large`;
   selain itu `internal_error` dengan pesan bawaan. Teks error asli tidak pernah masuk respons; `message`/`details` pada
   `internal_error` diabaikan. Log hanya untuk error tak dikenal dan 5xx yang membawa cause (supaya `Recoverer`, yang sudah
   mencatat panic dengan stack, tidak menulis dua baris).
6. **`DecodeJSON`** menerapkan docs/08 §2.3: `Content-Type` harus `application/json` (415), batas bawaan 1 MiB (413),
   field tak dikenal, body kosong/rusak/salah tipe/lebih dari satu nilai → `validation_failed` dengan
   `details.fields[]` (`field`, `code`, `message`).
7. **`httpserver.ClientContext`** mengisi `kernel.WithClient` (IP dari `RealIP` yang tepercaya + User-Agent, dibatasi 512 byte)
   sesuai ADR-0011 poin 2.

## Konsekuensi
- Body 404 berubah dari `not_found` ke `resource_not_found` dan kini memuat `message` + `request_id`.
- Cursor (T4b), idempotency dan rate limit (T4c) memakai `httpx.Error` yang sama; rahasia HMAC cursor diputuskan di ADR-0013.
