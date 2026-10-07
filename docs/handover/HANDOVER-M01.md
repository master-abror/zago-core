# HANDOVER — M01 Database Schema

**Tanggal:** 2026-10-07 · **Status:** DONE · digabung ke `main` lewat PR #2 (merge commit `2fd19ce`, "Create a merge commit") · **Tag:** `m01-done`
**Branch:** `milestone/m01-database-schema` (basis: tag `m00-done`) · **Jenis:** LENGKAP · **Handover sebelumnya:** `HANDOVER-M00.md`

## 1. Ringkasan
Sistem kini punya skema inti lengkap: 31 tabel lewat migrasi 001–033 (dengan `.down.sql` untuk semuanya), disematkan ke binary `cmd/migrate`
(`up`, `down N`, `version`, `roundtrip`, `seed`). Baris platform bisa di-seed secara idempoten. Suite tes memakai PostgreSQL 18 sungguhan
(testcontainers) dan membuktikan constraint, FK komposit lintas-organisasi, trigger hierarki group, cascade/restrict, serta batas hak
`app_user`/`app_maintenance`. `docs/erd.md` dibangkitkan dari skema yang benar-benar dijalankan dan dijaga tes. `make verify` kini memakai
PostgreSQL sementara miliknya sendiri untuk roundtrip. Satu flaky test M00 ikut diperbaiki.

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T1 `cmd/migrate` lengkap | DONE | + subcommand `seed`; `down` di versi 0 = no-op (ADR-0006) |
| T2 role DB + default privileges | DONE (M00) | tidak diubah; dipakai ulang oleh `testpg` dan skrip roundtrip |
| T3 migrasi 001–016 | DONE | |
| T4 migrasi 017–032 (◆CP1 dilewati, konteks cukup) | DONE | |
| T5 migrasi 033 grants + seeder platform | DONE | 033 menambah pengaman `schema_migrations` (ADR-0006) |
| T6 `docs/erd.md` + doc-sync | DONE | ERD dibangkitkan tes; doc-sync 04 §13.2/§15/§17, 16, 19 |
| Roundtrip untuk `make verify` (diminta di awal M01) | DONE | ADR-0005 |
| Peringatan air `build.bin` (diminta di awal M01) | DONE | ADR-0007; lihat §3 untuk batas buktinya |
| Perbaikan flaky test M00 | DONE | `fix(app)`; akar masalah di klien HTTP tes |

## 3. Bukti verifikasi (apa adanya)
**Terbukti** di mesin pengembang (WSL2 Ubuntu, Docker Desktop, PostgreSQL 18 via testcontainers dan `postgres:18` sementara), 2026-10-07:
- `make lint`: golangci-lint **0 issues** · svelte-check 0 error/0 warning · Prettier bersih · `lint-structure OK`.
- `make test`: seluruh paket Go lulus dengan `-race` (`app`, `health`, `httpserver`, `infra` ±27 dtk, **`migrations` ±52 dtk**, `pkg/*`); Vitest 3/3.
  Run pertama menemukan satu kegagalan nyata di PG18: pelanggaran `ON DELETE RESTRICT` memakai SQLSTATE `23001`, bukan `23503` (PG16);
  diperbaiki dengan helper `requireRestricted` dan lulus pada run berikutnya.
- `make migrate-roundtrip`: `migrate roundtrip: up -> down(semua) -> up pada database sementara OK` (container sementara, port acak).
- `make build`: tiga binary dan bundle Vite.
- `make verify` penuh sampai akhir: seluruh image dibangun, stack naik, `smoke/m00.sh` → **`M00 smoke OK`** (8 langkah). `make` berhenti pada
  langkah gagal pertama, jadi lint/test/roundtrip/build pada run itu juga lulus (yang ditempel hanya bagian ekor log).
  Catatan: `api` baru healthy setelah ±560 dtk pada run itu (M00: 118–177 dtk), lihat §8.
- Tes negatif/pelindung yang ditambahkan: tes memakai SQLSTATE dan nama constraint dari database (bukan dari kode Go); pemindai katalog `updated_at`
  dengan kontrol negatif; `TestMigrationVersionTableIsReadOnlyForAppUser`; `TestERDIsInSyncWithMigrations`; roundtrip gagal bila ada objek
  tersisa; galat runner tidak membocorkan password; skrip roundtrip membongkar container saat gagal.

- **CI GitHub pada PR #2: hijau pada semua job** (dilaporkan pengembang, 2026-10-07), termasuk job `migrations` yang kini menjalankan
  `make migrate-roundtrip` + `make db-test` tanpa service container. Nomor run tidak dicatat.
- **Exit criterion docs/21 M01 pada database dev asli** (2026-10-07, compose `postgres:18`): `make migrate-up` → `migrate-down` → `migrate-up` bersih
  (`versi 33 (dirty=false)` → `versi 32` → `versi 33`); `make migrate-version` → `versi 33 dirty=false`; `make db-seed` → `baris platform sudah ada (id 01a113b2-…), tidak ada perubahan`
  (seed kedua pada DB asli idempoten).

**Terbukti di sandbox pengembangan saya (PostgreSQL 16 lokal, bukan 18):** mutasi sengaja pada migrasi (9 kasus) terdeteksi oleh tes yang tepat;
semua subcommand CLI pada binary sungguhan; alur `migrate-roundtrip.sh` dengan `docker` palsu. Ini pelengkap, bukan pengganti bukti di atas.

**Belum terbukti:**
1. `make dev`/`make run-api` tanpa peringatan `build.bin is deprecated` (ADR-0007 terbukti dari kode sumber air, bukan dari menjalankannya).

## 4. Keputusan & penyimpangan
- ADR-0005 — sumber PostgreSQL untuk roundtrip: skrip dengan container sementara; CI memakai skrip yang sama; `REQUIRE_DOCKER=1` untuk `test`/`db-test`.
- ADR-0006 — penyimpangan kecil M01 (tabel; termasuk pengaman `schema_migrations`, subcommand `seed`, embed, file `.down.sql`, seeder, ERD, perbaikan flaky).
- ADR-0007 — air: `build.bin` → `build.entrypoint`.
- ADR-0008 — semantik hapus: DDL menang atas teks 04 §17 (organisasi/grup `RESTRICT`; `role_assignments` ber-scope group tidak diblokir DB).
- Doc-sync: 04 §13.2, §15, §17; 16 §2.4; 19 §3 dan §5; README; `docs/erd.md` (baru).

## 5. Delta kontrak
- **DB:** migrasi `001_common_functions` … `033_grants` (31 tabel; urutan resmi 04 §14). Tabel versi golang-migrate: `schema_migrations` (read-only untuk `app_user`).
- **API:** tidak ada. **Event:** tidak ada. **Permission:** tidak ada.
- **Config/env:** tidak ada variabel produk baru. Tombol dev/tes: `REQUIRE_DOCKER=1`, `UPDATE_ERD=1`, `PG_IMAGE` (skrip roundtrip, default `postgres:18`).
- **Perintah:** `cmd/migrate seed`; `make migrate-version`, `make db-seed`; `make migrate-roundtrip` kini butuh Docker.
- **Dependensi Go baru:** `github.com/golang-migrate/migrate/v4` v4.20.1 (go.mod/go.sum berubah lewat `go get`).

## 6. Peta kode
| Hal | Lokasi |
|---|---|
| Migrasi SQL + embed | `backend/migrations/*.sql`, `backend/migrations/embed.go` |
| Runner (up/down/version/roundtrip) | `backend/internal/dbmigrate/dbmigrate.go` |
| Seeder platform | `backend/internal/dbmigrate/seed.go` |
| Alur perintah `migrate` (dapat dites tanpa DB) | `backend/internal/app/migrate.go`, wiring `backend/cmd/migrate/main.go` |
| PostgreSQL 18 untuk tes (testcontainers, template DB) | `backend/internal/testpg/` |
| Suite skema | `backend/migrations/{files,schema,grants,seed,roundtrip,erd}_test.go`, `helpers_test.go` |
| ERD (dibangkitkan) | `docs/erd.md` (`UPDATE_ERD=1 make db-test`) |
| Roundtrip dengan container sementara | `scripts/migrate-roundtrip.sh` |

## 7. Cara menjalankan & memverifikasi
```bash
# dari klon bersih (filesystem Linux/WSL), Docker berjalan, port 5432 6379 8080 5173 1025 8025 bebas
make verify                      # lint → test → migrate-roundtrip → build → smoke
make db-test                     # hanya suite skema (butuh Docker)
make infra-up && make migrate-up && make db-seed && make migrate-version
UPDATE_ERD=1 make db-test        # perbarui docs/erd.md setelah mengubah migrasi
```

## 8. Masalah diketahui / utang teknis
- **`api` healthy ±560 dtk** pada run `verify` terakhir (M00: 118–177 dtk). Tidak diselidiki. Run itu berbarengan dengan Docker Desktop yang sempat
  time-out (`accept4 failed 110`) dan cache modul Go yang dingin; jangan dianggap regresi M01 tanpa pengukuran ulang dengan cache hangat.
- **`credsStore: desktop.exe` di WSL** kadang gagal (`error getting credentials`) saat `docker build`; solusi sementara di sisi pengguna (menghapus
  `credsStore` dari `~/.docker/config.json`, cadangan `.bak`). Bukan bagian repo.
- **Penjaga port `verify-smoke`** menolak jalan bila stack proyek lain (mis. Nizza AI) memegang 5432/6379/8080/5173/1025/8025; ini disengaja (M00).
- **Perbedaan versi PG:** SQLSTATE `RESTRICT` berbeda antara PG≤17 dan 18; tes menerima keduanya, proyek memakai 18.
- **Utang lintas milestone:** modul wajib mencabut DML `app_user` pada `schema_migrations_<kode>` (M07, ADR-0006); layanan group wajib mencabut
  `role_assignments` ber-scope group saat group diarsipkan/dihapus (M05, ADR-0008); `platforms.version` diisi `0.1.0-dev` dan belum disinkronkan
  saat upgrade (M15 lewat `-ldflags`).
- `testpg` memulai satu container per paket tes. M02 T6 membuat `internal/testkit` (Postgres+Redis); sebaiknya `testkit` membungkus/menggantikan `testpg`
  agar tidak ada dua pembangun fixture.
- `backend/internal/dbmigrate` tampil "no test files": runner dites dari paket `backend/migrations` (lintas paket), tanpa angka cakupan per paket.
- Image `postgres:18` mengambang (ADR-0002); skrip roundtrip memakainya.

## 9. Pertanyaan terbuka
- Lisensi proyek (belum ditetapkan, sejak M00).
- Apakah `testkit` (M02) akan menggantikan `testpg` atau membungkusnya (lihat §8).

## 10. Milestone berikutnya: M02 — Kernel: Transaksi, Outbox, Audit, HTTP Toolkit
- **Baca:** 10 §6, §9, §10, §13 · 13 §2.2–2.4 · 08 §2–§7, §11 · 14 §2–§6 (ringkas) · `E:B-01, B-02, I-06, I-07, I-08` (docs/21 bagian M02).
- **Langkah pertama:** T1 factory `pgxpool` untuk tiga role + `TxManager.WithinTx`. Gunakan role dan URL yang sama dengan `deploy/db/init` dan `testpg`.
- **Perhatian:** `app_user` boleh `UPDATE` `outbox_events` tetapi tidak `UPDATE/DELETE` `activities`/`security_events`/`system_logs`; retensi memakai `app_maintenance`
  (hak kolom terbatas). Relay outbox dan penghapusan dead-letter harus menghormati batas ini; tes M02 sebaiknya memakai role sungguhan, bukan superuser.

## 11. Prompt untuk chat berikutnya
Gunakan prompt P0 yang sama (docs/23) dengan zip repo terbaru, dan ganti baris konteks tambahan menjadi: "LANJUTAN setelah M01 (tag `m01-done`, sudah di `main`). Milestone berikutnya: M02 — Kernel.
Repo: https://github.com/master-abror/zago-core, modul Go `github.com/master-abror/zago-core`. Lingkungan: WSL2 Ubuntu + Docker Desktop, repo di `~/projects/zago-core`, branch `milestone/m02-kernel`.
Terima perubahan sebagai patch `git apply` terhadap tag `m01-done` (atau commit terakhir yang kusebut) atau berkas lengkap; jangan beri overlay zip yang menimpa go.mod/go.sum.
Tandai mana yang terbukti di sandbox dan mana yang hanya lewat `make verify`/CI; tes yang butuh PostgreSQL/Redis memakai testcontainers, tanpa mock database."
