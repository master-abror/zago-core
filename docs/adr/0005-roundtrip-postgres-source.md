# ADR-0005 — Sumber PostgreSQL untuk `migrate-roundtrip` dan tes skema

**Status:** Diterima · **Tanggal:** 2026-10-05 · **Menjawab:** HANDOVER-M00 §8 (butir 2)

## Konteks
`make verify` menjalankan `migrate-roundtrip` sebelum `verify-smoke`, saat belum ada PostgreSQL menyala; `verify-smoke.sh`
justru menolak jalan bila port 5432 terpakai. Roundtrip tak boleh bergantung pada stack dev yang kebetulan hidup.

## Keputusan
1. `make migrate-roundtrip` menjalankan `scripts/migrate-roundtrip.sh`: menyalakan container `postgres:18` **sementara**
   (port host dipilih acak oleh Docker, init dari `deploy/db/init/*.sql` = role yang sama dengan compose dev), memanggil
   `cmd/migrate roundtrip` terhadapnya, lalu menghapus container (juga saat gagal).
2. CI memakai skrip yang sama; service container `postgres` dan langkah `psql` di job `migrations` dihapus (satu jalur, bukan dua).
3. Tes skema/seeder memakai **testcontainers** lewat `backend/internal/testpg` (satu container per paket tes, satu database per
   tes, klon dari template yang dimigrasi sekali). Tanpa mock database.
4. `REQUIRE_DOCKER=1` diset oleh `make test-backend` dan `make db-test`: tanpa Docker, tes `testpg` **gagal** (bukan skip) agar
   tak ada hijau palsu di `verify`/CI. Tes M00 yang sudah ada tetap memakai `SkipIfProviderIsNotHealthy` (tidak berubah).

## Alternatif yang ditolak
Memakai DB dev `:5432` (bergantung pada stack dev, bentrok dengan penjaga port); profile compose khusus (bentrok port);
testcontainers di dalam `cmd/migrate` (binary produksi tak boleh bergantung pada testcontainers).

## Konsekuensi
`make verify` tetap butuh Docker (sudah begitu sejak M00). `make test` kini gagal tanpa Docker. Image `postgres:18` mengambang (ADR-0002).
