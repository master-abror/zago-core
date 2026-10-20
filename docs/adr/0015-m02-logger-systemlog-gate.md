# ADR-0015 — M02/T5b: `logger.SystemLog` dan gate system_logs

**Status:** Diterima · **Tanggal:** 2026-10-10 · **Keputusan oleh:** sesi M02 (usulan; pemilik proyek dapat membatalkan)

## Konteks
docs/14 §3 menetapkan bahwa satu panggilan log dapat ditandai agar juga ditulis ke `system_logs`
(`logger.Error(ctx, msg, …, logger.SystemLog("event.code"))`), dengan penulisan best-effort, dibatasi laju per
`event_code`, lewat pool runtime, dan tidak pernah menggagalkan request. Kenyataannya `pkg/logger` adalah pembungkus
`log/slog` (tidak ada fungsi `logger.Error(ctx, …)`), dan logger dibuat paling awal di composition root — sebelum pool
PostgreSQL ada.

## Keputusan
1. **Bentuk API mengikuti slog**, bukan fungsi baru: `log.ErrorContext(ctx, msg, "k", v, logger.SystemLog("event.code"))`.
   `logger.SystemLog(code)` mengembalikan `slog.Attr` penanda. Penanda **selalu dibuang** dari record sebelum sampai
   ke handler JSON, sehingga tidak pernah muncul di stdout (juga bila kodenya kosong atau tidak ada sink).
2. **Gate** (`logger.Gate`, `logger.WithSystemGate`): logger dibuat dengan gate kosong; sink di-attach SETELAH pool siap
   (`app.AttachSystemLog`) dan di-detach SEBELUM pool ditutup (urutan `defer`). Selama tidak ada sink, log bertanda hanya
   ke stdout. Gate aman dipakai serentak (`atomic.Pointer`) dan tetap berlaku pada logger turunan (`With`, `WithGroup`).
3. **Sink** (`logger.SystemSink`) diimplementasikan `systemlog.Writer.Sink()`. Pemetaan level: ≥ Error → `error`,
   Warn → `warn`, Info → `info`, di bawahnya `debug`. Validasi `event_code`, redaksi metadata, batas laju per kode, dan
   timeout tulis tetap milik `systemlog.Writer` (ADR-0011).
4. **Isi entri**: `Fields` = atribut milik panggilan itu (grup diratakan `a.b`, `error` menjadi teks). Atribut dari
   `With()`/`WithGroup()` tidak ikut. `request_id`/`trace_id` dibawa lewat `ctx`. Pemanggil bertanggung jawab tidak
   menyertakan detail besar (mis. stack trace) pada log bertanda; Writer membatasi metadata 16 KiB.
5. **Level minimum tetap berlaku**: log di bawah level logger tidak diproses, jadi tidak masuk `system_logs`. System log
   ditujukan untuk warn/error.
6. **Sink yang panik tidak menjatuhkan pemanggil**: panic dipulihkan dan dilaporkan sebagai satu peringatan ke stdout.
7. **API dan worker** keduanya memasang gate ke `systemlog.Writer` pada pool runtime (`app_user`). Worker memakai Writer
   yang sama untuk hook dead-letter relay (perilaku tidak berubah).

## Konsekuensi
- Doc-sync tertunda: docs/14 §3 (contoh kode memakai bentuk slog `log.ErrorContext(ctx, …, logger.SystemLog("…"))`).
- Belum ada call site produksi yang menandai log; call site pertama datang bersama kebutuhan nyata (M03+, M06).
  `audit.Recorder` memakai `Writer` langsung (ADR-0011), bukan lewat gate.
- Penyimpangan dari dokumen: hanya bentuk sintaks; perilaku sama dengan docs/14 §3.
