# ADR-0019 — M02: klien Redis harus menghormati deadline context

**Status:** Diterima · **Tanggal:** 2026-10-15 · **Konteks:** M02, gerbang `make verify` setelah T5f

## Konteks
`make verify` merah pada `TestRateLimitConcurrentRequestsNeverExceedLimit` (`kernel/httpx`; 60 request paralel, batas 10, Redis nyata),
tetapi lulus 10/10 bila dijalankan sendirian dan 3/3 sebagai paket. Log kegagalan (angka `actual`) hilang sebelum sempat dibaca, jadi
**penyebab flake itu sendiri tidak terbukti**. Dugaan kuat yang konsisten dengan kodenya: aturan di tes itu non-sensitif, dan satu panggilan
Redis yang melewati `rlRedisTimeout` (250 ms) membuat limiter fail open dan melewatkan semua request berikutnya selama 1 detik tanpa
menghitungnya (ADR-0014), sehingga jumlah yang lolos melebihi 10. Di run yang gagal tes itu memakan 0,53 detik, sedangkan 0,09–0,17 detik di
isolasi.

Saat menguji batas 250 ms itu terhadap server yang menerima koneksi tetapi tak pernah menjawab, ditemukan **cacat yang lebih mendasar**:
go-redis v9 secara bawaan **mengabaikan deadline context pada I/O soket** (`ContextTimeoutEnabled = false`); hanya `ReadTimeout`/`WriteTimeout`
klien yang berlaku. Pengukuran, context 250 ms terhadap server yang diam:

| Konfigurasi klien | Waktu tunggu |
|---|---|
| bawaan (klien `infra.Redis` sebelumnya) | **5,001 detik**, context diabaikan |
| `ContextTimeoutEnabled = true` | **251 ms** |

Akibatnya semua asumsi "context ber-batas-waktu" di kernel tidak berlaku untuk Redis yang hidup-tapi-macet (partisi jaringan, failover tersendat):
batas 250 ms di rate limiter, timeout pemeriksaan readiness, dan penyelesaian idempotency. Redis yang mati total (koneksi ditolak) tidak terpengaruh,
sehingga tes "Redis mati" yang ada tidak pernah menangkapnya.

## Keputusan
1. `infra.Redis` menyetel `ContextTimeoutEnabled = true` pada klien bersama. Terbukti oleh `TestRedisResourceHonorsContextDeadlineWhenServerStopsAnswering`
   (0,50 detik untuk dua panggilan dengan context 250 ms; 5,0 detik sebelum perbaikan).
2. `httpx.NewRateLimiter` menerima opsi: `WithRedisTimeout(d)` mengganti batas 250 ms (nilai ≤ 0 diabaikan; bawaan produksi tidak berubah).
   `newLimiter` di tes integrasi memakai 5 detik, agar tes tidak bergantung pada batas ketat itu saat Docker kewalahan (pola yang sama dengan ADR-0017).
3. Perilaku batas itu sendiri diuji tanpa Redis nyata (`ratelimit_timeout_test.go`) dengan server TCP "lubang hitam" yang meniru konfigurasi produksi;
   tes memakai batas bawah dan batas atas longgar (3 detik, di bawah 5 detik bawaan go-redis) dan terbukti merah bila `ContextTimeoutEnabled` dihapus.

## Konsekuensi
- Pembatalan context request (klien memutus koneksi) kini juga memutus panggilan Redis yang sedang berjalan. Jalur yang harus tuntas tetap memakai
  `context.WithoutCancel` (penyelesaian idempotency sudah begitu); kode baru yang menulis ke Redis harus mengikuti pola itu.
- Tes lain yang bergantung pada batas ketat produksi dengan Redis nyata (mis. tes echo di `httpserver`, yang memakai limiter dengan batas bawaan) rentan
  terhadap kelas flake yang sama; belum diubah karena belum ada bukti kegagalan. Dicatat di handover.
- Doc-sync (ritual akhir M02): docs/08 §7 (limiter menghormati deadline 250 ms hanya dengan klien ber-`ContextTimeoutEnabled`), docs/10 §8 (catatan klien Redis).
