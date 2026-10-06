# ADR-0006 — Penyimpangan kecil dari dokumen di M01

**Status:** Diterima · **Tanggal:** 2026-10-05

| Dokumen | Penyimpangan | Alasan |
|---|---|---|
| 04 §13.2 (`033_grants.up.sql`) | Menambah blok `DO $$ … REVOKE INSERT, UPDATE, DELETE ON schema_migrations FROM app_user … $$` | `GRANT … ON ALL TABLES` ikut mencakup tabel versi golang-migrate (dibuat sebelum migrasi 001). Tanpa pencabutan, role runtime bisa menulis versi/`dirty` skema, padahal API/worker tak boleh punya hak DDL. Dibuktikan oleh tes `TestMigrationVersionTableIsReadOnlyForAppUser` (gagal bila blok dihapus). Tabel versi modul (`schema_migrations_<kode>`, 04 §15.2) dibuat setelah 033 dan mendapat default privileges; **modul wajib mencabutnya sendiri** (utang untuk M07). |
| 04 §15 / 21 M01 T1 (subcommand) | `cmd/migrate` punya `up`, `down N`, `version`, `roundtrip`, dan **`seed`**; Makefile menambah `migrate-version` dan `db-seed` | Seeder platform (04 §16 butir 1, T5) perlu pintu masuk; ia hanya butuh DML. `up`/`down` mencetak versi akhir. |
| 04 §15 (`down` di versi 0) | `Runner.Down` di versi 0 = sukses tanpa perubahan | golang-migrate mengembalikan `os.ErrNotExist` untuk `Steps(-n)` di versi nol; perilaku idempoten lebih aman untuk operator. |
| 04 §15 (folder `backend/migrations`) | Berkas SQL disematkan ke binary lewat `backend/migrations/embed.go` (`go:embed`) | Satu binary yang sama jalan lokal, di CI, dan di image (kalimat 04 §15); tak ada jalur berkas yang bisa salah. `COPY backend/migrations /migrations` di Dockerfile tidak diubah. |
| 04 §14 (berkas `.down.sql`) | 04 hanya menuliskan `001` down; `002`–`033` down ditulis di M01 mengikuti 04 §15.1 (`DROP TABLE`, `026` melepas FK `last_read` dulu, `006` juga `DROP FUNCTION check_group_hierarchy`, `033` mencabut yang diberikan) | Syarat `migrate-roundtrip`. Roundtrip juga gagal bila ada tabel/fungsi tersisa di `public` setelah `down` semua. |
| 04 §16 (seeder) | `platforms.version` diisi `dbmigrate.PlatformVersion` (`0.1.0-dev`, bisa diganti `-ldflags` saat rilis M15); seeder dilindungi `pg_advisory_xact_lock` | Menyerialkan seeder bersamaan; `ON CONFLICT (installation_key)` saja tak mencegah dua baris ber-key berbeda. |
| 16 §2.4 (`db-test`) | Tak lagi dilewati; menjalankan `go test ./backend/migrations/... -race` dengan `REQUIRE_DOCKER=1` | Sudah ada tes skema (ADR-0003 menjadwalkan ini di M01). |
| 21 M01 T6 (`docs/erd.md`) | ERD **dibangkitkan dari katalog PostgreSQL oleh tes** dan dibandingkan dengan `docs/erd.md`; perbarui dengan `UPDATE_ERD=1 make db-test` | ERD tak bisa menyimpang dari SQL tanpa disadari. |
| 22 §11.6 (bug milestone lama) | Tes M00 `TestAPIServes…` flaky diperbaiki: klien HTTP tes tanpa keep-alive | Akar masalah terbukti (dump goroutine): koneksi cadangan `http.Transport` menahan `Server.Shutdown` ±5 dtk. Kode produksi tak berubah. Sebelum: gagal 9/30 di bawah `-race`; sesudah: 0/60. |
