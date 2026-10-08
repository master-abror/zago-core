# ADR-0011 — M02: perekam audit, system log, dan redaksi

**Status:** Diterima · **Tanggal:** 2026-10-08 · **Keputusan oleh:** sesi M02 (usulan; pemilik proyek dapat membatalkan)

## Konteks
docs/13 §2.2–2.4 dan docs/10 §4/§6 menetapkan `AuditRecorder.Record(ctx, entry)` tanpa nilai kembali, atomik dengan aksi,
`denied`/`failed` di transaksi terpisah, dan daftar redaksi metadata. Dokumen tidak merinci bagaimana kegagalan tulis
dilaporkan bila tidak ada error untuk dikembalikan, siapa yang mengisi aktor/IP/UA di `ctx`, dan di mana daftar redaksi tinggal.

## Keputusan
1. **Tipe audit di `module-sdk` sejak M02** (`ActivityEntry`, `AuditRecorder`, konstanta `Result*`), alasan sama dengan
   ADR-0010 poin 4: implementasi core harus memenuhi kontrak modul secara struktural.
2. **Aktor, scope, dan asal request dibawa `ctx`** lewat accessor bertipe di `internal/kernel` (`WithActor`, `WithScope`,
   `WithClient` dan pasangannya; docs/10 §10). M02 hanya mendefinisikan dan membacanya; pengisinya adalah middleware
   autentikasi (M03) dan middleware asal-request (T4). Tanpa aktor di `ctx`, activity bertipe `system` dengan aktor NULL.
3. **`success` memakai transaksi pada ctx; kegagalan menandai transaksi rollback-only** (`kernel.FailTx`) — operasi ikut gagal
   (fail-closed, errata B-02). Entri tak valid (action kosong/terlalu panjang, result tak dikenal, tipe aktor tak dikenal,
   metadata tak ter-encode) diperlakukan sama: bug pemanggil tidak boleh meloloskan aksi tanpa jejak. Di luar transaksi,
   kegagalan hanya dicatat keras (log error + system log `audit.write_failed`) karena tak ada yang bisa dibatalkan.
4. **`denied`/`failed` ditulis dengan satu statement langsung ke pool** (transaksi implisit sendiri), memakai ctx tanpa
   transaksi dan tanpa pembatalan (batas 5 dtk). Kegagalannya dilaporkan tetapi TIDAK menandai transaksi utama gagal: aksinya
   sudah ditolak/gagal. Konsekuensi: butuh satu koneksi pool tambahan selagi transaksi utama terbuka (ukuran pool ≥ 2).
5. **Redaksi di `backend/pkg/redact`** (dipakai audit, system log, dan nanti logger): dua lapis — kunci (nama dinormalisasi
   tanpa pemisah/huruf besar, dicocokkan sebagai potongan: password, token, secret, authorization, apikey, …; singkatan pendek
   seperti `otp`/`pin` dicocokkan utuh) dan nilai teks berbentuk rahasia (Bearer/Basic, JWT, kredensial di URL, `password=…`).
   Bias sengaja ke arah menyamarkan berlebihan (mis. `token_count` ikut tersamar). Kunci sensitif tambahan modul lewat
   `redact.New(keys...)`. Dibuktikan oleh tes properti (1500 pohon acak per run, seed dicetak).
6. **`internal/systemlog` sinkron, best-effort, dibatasi laju per `event_code`** (default 5 baris/menit; jumlah yang ditahan
   dilaporkan di `metadata.suppressed` pada jendela berikutnya; maksimum 1000 kode dilacak, sisanya berbagi satu ember).
   `Log` tidak mengembalikan error; kegagalan hanya ke log stdout. Batas atas blokir: satu tulis ≤ 2 dtk, sebanyak
   batas laju — diterima karena dipakai di jalur kegagalan, bukan jalur normal. Pilihan antrean asinkron ditolak
   (butuh siklus hidup tambahan dan titik sinkronisasi di tes) kecuali pengukuran membuktikan perlunya.
7. **Opsi `logger.SystemLog("kode")` pada panggilan log (docs/14 §3) DITUNDA ke T5**, bersama pemasangan `systemlog.Writer`
   ke logger aplikasi (perlu urutan pembuatan: logger lebih dulu dari pool). Sementara itu pemanggil memakai
   `systemlog.Writer.Log` langsung; hook dead-letter relay sudah terpasang di worker.

## Konsekuensi
- `audit.Recorder` dan `systemlog.Writer` belum dipasang ke API (belum ada request autentikasi); dipasang saat M03/T4.
- Nama kolom `activities.actor_type` dibatasi tiga nilai oleh constraint; tipe lain ditolak sebelum menyentuh database.
- `kernel.NullUUID`/`kernel.NullString` diekspor sebagai pembantu argumen kolom nullable.
