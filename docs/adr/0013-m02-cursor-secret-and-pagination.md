# ADR-0013 — M02/T4b: rahasia cursor, format token, dan aturan limit

**Status:** Diterima · **Tanggal:** 2026-10-10 · **Keputusan oleh:** pemilik proyek (sumber rahasia, dipilih dari usulan sesi M02); sisanya usulan sesi M02

## Konteks
docs/08 §4 mewajibkan cursor keyset yang ditandatangani HMAC, memuat `(created_at, id)`, arah urut, dan hash filter aktif;
cursor rusak atau dipakai dengan filter lain harus `400 validation_failed`. Dokumen tidak menyebut dari mana kunci HMAC berasal
(handover M02 §9 menandainya sebagai keputusan terbuka), format token, dan perilaku `limit` di atas maksimum.

## Keputusan
1. **Kunci cursor diturunkan dari `SESSION_SECRET`, tanpa env baru** (keputusan pemilik): `kunci = HMAC-SHA256(SESSION_SECRET,
   "zago/cursor/v1")`. Label tetap memisahkan kunci ini dari kunci turunan lain (mis. CSRF). Konstruktor menolak rahasia < 32 byte
   (selaras validasi `pkg/config`). Konsekuensi yang diterima: memutar `SESSION_SECRET` membuat semua cursor lama ditolak (400) dan
   klien memulai paginasi dari awal; cursor berumur pendek dan bukan kredensial. Bila kelak butuh rotasi terpisah, `CURSOR_SECRET`
   opsional dapat menimpa turunan ini tanpa mengubah format token.
2. **Format token**: `base64url(payload).base64url(HMAC-SHA256(payload))`, tanpa padding, didekode dengan mode ketat (bit sisa
   non-nol ditolak) sehingga tidak ada token "tetangga" yang sah. Payload JSON: `{v, t, i, d, f}` = versi (1), `created_at`
   (UnixNano UTC), id, arah (`desc`|`asc`), hash filter. Panjang token ≤ 512 karakter. Verifikasi MAC memakai `hmac.Equal`
   (waktu konstan) dan dilakukan SEBELUM payload diurai.
3. **Dua kode detail**, keduanya `400 validation_failed` pada field `cursor`: `invalid_cursor` (rusak, terpotong, tanda tangan salah,
   versi/arah/id tak dikenal) dan `cursor_mismatch` (tanda tangan sah tetapi arah atau filter berbeda dari request saat ini).
   `cursor_mismatch` hanya muncul setelah MAC lolos, jadi tidak membuka oracle bagi penyerang.
4. **Hash filter**: `FilterHash` = 16 byte pertama SHA-256 atas pasangan kunci/nilai terurut dengan prefiks panjang; hanya
   filter aktif yang dimasukkan, nilai sudah dinormalisasi oleh pemanggil.
5. **`limit`**: bawaan 50, maksimum 100, bisa diatur per endpoint (`PageOptions`). Nilai bukan bilangan bulat ≥ 1 → `validation_failed`
   (field `limit`, `invalid_limit`). Nilai di atas maksimum **dipotong ke maksimum**, tidak ditolak (docs/08 hanya menyebut
   "server-side maximum"). Endpoint mengambil `Limit+1` baris; `Paginate` menentukan `has_more` dari baris ekstra dan memberi
   `next_cursor: null` pada halaman terakhir.

## Konsekuensi
- Pemanggil mengurutkan `(created_at, id)` dengan arah yang sama pada query dan pada cursor; predikat halaman berikut
  `(created_at, id) < ($c, $id)` untuk `desc` dan `>` untuk `asc`.
- Pemasangan `CursorCodec` ke wiring aplikasi (dari `cfg.SessionSecret`) dilakukan bersama endpoint echo di T5.
