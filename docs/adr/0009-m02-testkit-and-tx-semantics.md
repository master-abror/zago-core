# ADR-0009 — M02: testkit membungkus testpg, semantik transaksi bersarang, konfigurasi pool

**Status:** Diterima · **Tanggal:** 2026-10-08 · **Keputusan oleh:** sesi M02 (usulan; pemilik proyek dapat membatalkan)

## Konteks
docs/18 §7 meminta `backend/internal/testkit` berisi fixture builder; docs/10 §6 meminta `WithinTx` yang
"bergabung" bila sudah ada transaksi. Handover M01 §9 menyisakan pertanyaan terbuka: apakah `testkit` menggantikan
`testpg`? Dokumen tidak menetapkan perilaku `WithinTx` bersarang saat fungsi dalam gagal dan pemanggil menelan
error-nya, dan tidak menetapkan nama variabel pool database.

## Keputusan
1. **`testkit` membungkus `testpg`, tidak menggantikannya.** `testkit.NewDB` memanggil `testpg.Shared(t).NewDB(t)`;
   tes skema M01 (`backend/migrations`) tetap memakai `testpg` apa adanya. `testkit` menambah Redis 8 testcontainers
   (satu container per paket, `FLUSHALL` per klien baru; tidak untuk `t.Parallel`) dan fixture builder
   (`NewOrganization`, `NewUser`, opsi `InOrganization`). Aturan Docker sama: tanpa Docker skip, `REQUIRE_DOCKER=1` gagal.
   Fixture menulis lewat role pemilik skema (`db.Migrator`); kode yang diuji memakai `db.App` (`app_user`).
2. **`WithinTx` bersarang bergabung tanpa savepoint dan bersifat rollback-only saat gagal.** Bila fungsi bersarang
   mengembalikan error, transaksi ditandai gagal; jika fungsi terluar tetap mengembalikan `nil`, `WithinTx` terluar
   mengembalikan `ErrTxRollbackOnly` (membungkus penyebab asli) dan melakukan ROLLBACK. Alasan: mencegah commit separuh
   ketika error ditelan. `kernel.FailTx(ctx, err)` memakai mekanisme yang sama untuk penulis audit/outbox yang
   kontrak `module-sdk`-nya tidak mengembalikan error (docs/10 §4: `Record(ctx, entry)`, `Publish(ctx, event)`).
3. **Rollback tetap berjalan walau ctx dibatalkan** (`context.WithoutCancel` + batas 5 dtk). Hook `AfterCommit`
   hanya jalan setelah COMMIT; panic di hook dicatat dan tidak membatalkan commit.
4. **Konfigurasi pool** (baru, semua punya default sehingga `.env.example` tidak berubah): `DB_MAX_CONNS` (10),
   `DB_MAINTENANCE_MAX_CONNS` (2), `DB_STATEMENT_TIMEOUT` (15s, dipasang server-side per koneksi lewat
   `RuntimeParams["statement_timeout"]`). Divalidasi untuk role api dan worker.
5. **Pemasangan pool ke `internal/app`/`internal/infra` ditunda** sampai ada pemakai nyata (relay di worker: T2;
   endpoint echo di API: T5). `kernel.NewPool` sudah tersedia dan dites; antarmuka `app.Dependencies` diubah pada
   task yang membutuhkannya, bersama tes fake-nya.

## Konsekuensi
- Tes baru memakai `testkit.Run(m)` di `TestMain` (menghentikan container PostgreSQL dan Redis bersama).
- Tidak ada doc-sync: perilaku (2) melengkapi docs/10 §6, tidak bertentangan. Ringkas di docs/10 §6 saat M02 selesai
  bila pemilik proyek menyetujui.
