# ADR-0010 — M02: semantik relay outbox dan jalur event

**Status:** Diterima · **Tanggal:** 2026-10-08 · **Keputusan oleh:** sesi M02 (usulan; pemilik proyek dapat membatalkan)

## Konteks
docs/10 §6.2 menggambarkan relay sebagai satu transaksi per batch: SELECT … FOR UPDATE SKIP LOCKED, jalankan
handler, tandai processed/retry, COMMIT. Dengan satu transaksi per batch, efek samping DB dari handler yang gagal
ikut ter-commit bersama penandaan retry, dan satu event buruk menahan kunci seluruh batch. Doc 07 §8 dan errata B-01
menetapkan dua jalur (outbox durable dan handler in-process) tetapi tidak merinci siapa menjalankan apa.

## Keputusan
1. **Satu transaksi per event.** Handler berjalan di dalam transaksi klaim; `Publish` dari handler ikut atomik.
   Bila handler gagal/panik/timeout (atau menandai transaksi rollback-only), transaksi di-ROLLBACK, lalu kegagalan
   dicatat di transaksi kedua: `attempts+1`, `available_at = now() + backoff`, `last_error`. Pembaruan itu dijaga
   `status='pending' AND attempts=<nilai terbaca>` agar tidak menimpa relay lain. Setelah `MaxAttempts` (default 8)
   event menjadi `dead`; hook `OnDead` + log `outbox.event_dead` (sambungan ke system log di T3). Urutan klaim:
   `available_at, created_at, id` (tie-breaker `id` UUIDv7 menjaga urutan publish dalam satu transaksi).
2. **Event tanpa handler terdaftar di proses relay ditandai `processed`.** Tak ada konsumen berarti tak ada yang
   menunggu; membiarkannya `pending` hanya menumpuk antrian.
3. **Dua registri handler, satu kontrak `modulesdk.EventBus`.** `kernel.Bus` (API): `Subscribe` = handler in-process
   sinkron setelah commit (khusus core, ringan; docs/07 §8 jalur 2). `kernel.Relay` (worker): `Subscribe` = handler
   pengiriman outbox; `Relay.EventBus(pub)` menghasilkan `modulesdk.EventBus` untuk `RegisterEventHandlers` modul (M07).
   Handler in-process memakai ctx tanpa transaksi (`DetachTx`): transaksi asal sudah selesai.
4. **Tipe event milik `module-sdk` sejak M02** (`Event`, `EventHandler`, `EventBus`, `TxManager`), bukan M07: paket
   `internal/*` boleh mengimpor `module-sdk` tetapi tidak sebaliknya, jadi tipe yang dipakai bersama harus di sana
   agar implementasi core memenuhi kontrak modul secara struktural. Isi kontrak lain (Authorizer, CoreDeps, …) tetap M07.
5. **Validasi event di `Publish`**: nama `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$` (≤200), payload objek JSON valid
   (kosong → `{}`). Event tak valid atau gagal ditulis → `FailTx` (transaksi rollback-only, fail-closed) + log error.
6. **`app.Dependencies.Postgres` menerima `PostgresSpec`** (nama, URL, `MaxConns`, `StatementTimeout`) dan
   `app.Resource.Pool` mengembalikan `kernel.Pool` agar worker dapat menjalankan relay; fake di tes tetap boleh
   membiarkannya nil (relay tidak jalan, dicatat sebagai warning). Worker menunggu relay berhenti sebelum menutup pool.
7. `trace_id` ikut disimpan di outbox dan dipulihkan ke ctx handler (docs/14 §6); `pkg/logger` kini membawa
   `trace_id` bila ada pada context.

## Konsekuensi
- Handler HARUS idempoten (dedupe pada `event.ID`), sesuai docs/07 §8.
- Handler yang berjalan lama menahan satu koneksi dan satu kunci baris; `HandlerTimeout` (30 dtk) membatasinya.
- Doc-sync (ringkas, saat M02 selesai): docs/10 §6.2 — transaksi per event dan penanganan event tanpa handler.
