# ADR-0022 — M04: keputusan desain Authorization Engine (bagian 1: keputusan awal)

**Status:** Diterima · **Tanggal:** 2026-10-30 · **Konteks:** awal M04 (docs/06, docs/00 B-04 dan I-01 sampai I-03). Bagian berikutnya
(bagian 2, 3, ...) ditambahkan per ◆CP/task; butir diberi nomor lanjut.

## Konteks
docs/06 menetapkan engine otorisasi (rantai, cache versi, policy, authority ceiling) tetapi menyisakan celah yang baru terlihat saat membaca
skema dan kode M03: identitas platform tidak punya jalur masuk organisasi (ADR-0020 butir 28), tabel `policies` tidak punya pengikat ke action,
`Bus` di `app/auth.go` masih instans sendiri (HANDOVER-M03 §8), dan penamaan role sistem selain `super-admin` belum ditetapkan.

## Keputusan
1. **Identitas platform memasuki organisasi lewat `switch-organization`.** Identitas tanpa keanggotaan boleh pindah ke organisasi berstatus `active`
   bila ENGINE (`Authorizer.Can`, bukan layanan auth) memutuskan `organization.read` pada scope `platform`. Tidak ada keanggotaan buatan.
   Penolakan tetap `403 permission_denied` seragam tanpa oracle keberadaan organisasi. Event keamanan `auth.organization.switched` membawa `via=platform`
   dan activity dicatat di organisasi tujuan. Di dalam sesi itu assignment `platform` tetap berlaku (docs/06 §5.2) sehingga `Decision.Level` = `platform`,
   tetapi setiap query tenant tetap memfilter `organization_id` sesi. Organisasi `suspended` tidak dimasuki; pengelolaannya lewat endpoint tingkat platform.
   Antarmuka: paket `auth` mendefinisikan `PlatformEntryChecker` yang diimplementasikan `authorization` (tidak ada impor melingkar). Dikerjakan di T3/T5.
2. **Policy terikat ke action lewat kolom baru `policies.actions text[]` (migrasi 021, dikerjakan di T4).** Isi: kode permission persis atau `resource.*`;
   tidak boleh kosong; divalidasi terhadap katalog saat ditulis. Policy `organization_id IS NULL` (global) berlaku untuk semua organisasi; policy organisasi
   hanya untuk organisasinya. Migrasi baru (bukan mengedit yang sudah di-tag); tes ERD, roundtrip, docs/03, docs/04, `docs/erd.md` ikut disinkronkan.
3. **Invalidasi cache dipanggil eksplisit di dalam transaksi pengubah (sebelum commit dan sesudah commit)**, bukan lewat event bus (menyimpang dari docs/06 §8.1
   yang menyebut handler in-process di bus): `Bus` di `app/auth.go` belum bersama, jadi kebenaran tidak boleh bergantung pada pengiriman event. Event outbox
   tetap boleh diterbitkan untuk konsumen lain. Versi kunci = pasangan (versi organisasi, versi platform) sehingga mengubah role sistem (global)
   menginvalidasi semua organisasi lewat penghitung `platform`. Dikerjakan di T3.
4. **Pembuka kunci akun administratif (HANDOVER-M03 §9 #9) ditunda ke M05** (bagian administrasi user). Sementara: `redis-cli DEL auth:lock:<user_id>`.
5. **Katalog permission core = docs/06 §5.4 persis (41 kode)**, dideklarasikan di `internal/permission/catalog.go`. `resource` = semua segmen sebelum titik terakhir,
   `action` = segmen terakhir (`organization.member.manage` → resource `organization.member`, action `manage`). Namespace core = segmen pertama kode katalog;
   modul tidak boleh mendeklarasikan kode di namespace itu (M07). docs/08 tidak memuat kode per rute, jadi tidak ada penambahan.
6. **Role sistem** (global, `role_type='system'`, organization/group NULL, ditetapkan oleh kode): `super-admin` ("Super Admin"), `organization-admin`
   ("Organization Admin"), `group-admin` ("Group Admin"), `member` ("Member"). Set default = docs/06 §5.5; `super-admin` = SEMUA permission core `is_system`
   (termasuk yang sudah tidak ada di katalog, supaya tidak kehilangan hak diam-diam).
7. **Semantik sinkron startup** (`authorization/application.Syncer`, satu transaksi, advisory lock `0x7a61676f03`, hanya hak `app_user`):
   upsert permission by `code` (tidak pernah menghapus; kode core yang dimiliki modul = `ErrCodeConflict` dan semuanya di-rollback; permission core yang
   hilang dari katalog dilaporkan sebagai orphan); upsert role sistem by slug (mengadopsi baris minimal bootstrap-admin; nama, deskripsi, dan status `active`
   dikembalikan bila menyimpang); set permission CORE tiap role sistem dikonvergensikan persis ke default (kelebihan dicabut, kekurangan ditambah);
   permission modul pada role sistem tidak disentuh. Satu activity `authz.catalog_synced` (`actor_type='system'`, scope platform, metadata hanya hitungan)
   hanya bila ada perubahan. Tanpa event outbox (belum ada konsumen; invalidasi cache dipasang di T3).
8. **Struktur paket:** `internal/permission` (katalog, kode, sinkron permission), `internal/role` (definisi dan sinkron role sistem; mengimpor `permission`),
   `internal/authorization/application` (Syncer; satu-satunya yang menggabungkan keduanya, sejalan dengan docs/06 §13). Repositori memakai pgx langsung
   (pola `bootstrap`); blok sqlc ditambahkan hanya bila query menjadi besar dan butuh `make generate`.
9. **Aturan assignment (ditegakkan di T5, tercatat di sini):** `super-admin` hanya boleh ditetapkan pada scope `platform` dan sebaliknya hanya `super-admin`
   pada scope `platform`; template lain hanya pada scope yang dinyatakan docs/06 §5.5; permission role sistem tidak dapat diubah lewat API (klon sebagai role custom);
   assignment platform `super-admin` terakhir yang aktif tidak boleh dicabut.

## Konsekuensi
- Wiring sinkron ke `RunAPI` dikerjakan sebagai commit terpisah setelah T1 terbukti (tes `app` memakai pool tiruan).
- **Doc-sync tertunda (kumulatif M04):** docs/06 (§4.1 jalur platform, §5.5 slug role, §6 pengikat policy, §8.1 invalidasi eksplisit dan versi pasangan),
  docs/03 + docs/04 + `docs/erd.md` (migrasi 021), docs/05 (switch-organization untuk identitas platform), docs/00 B-04 (katalog dan role sinkron sebagai dibangun),
  ADR-0020 butir 28 (catatan: dilanjutkan ADR-0022 butir 1).
