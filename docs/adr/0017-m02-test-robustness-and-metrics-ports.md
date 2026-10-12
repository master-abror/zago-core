# ADR-0017 — M02: ketahanan tes integrasi di mesin kecil dan pembagian port `/metrics`

**Status:** Diterima · **Tanggal:** 2026-10-11 · **Keputusan oleh:** pemilik proyek (persetujuan atas usulan sesi M02, 2026-10-11)

## Konteks
1. **Tes acak-gagal di `make verify`.** Pada 2026-10-11, `make verify` penuh merah di satu tes:
   `TestLogScrubsSecretShapedMessage` (`internal/systemlog`) — baris `system_logs` tidak ada karena INSERT melewati batas
   `writeTimeout` 2 detik (`system log gagal ditulis … context deadline exceeded`). Semua paket lain hijau, dan paket yang sama
   lulus tiga kali berturut-turut bila dijalankan sendirian (`-race -count=3`). `go test ./... -race` menjalankan paket sebanyak
   CPU secara bersamaan dan tiap paket integrasi menyalakan container PostgreSQL 18 (+ Redis) sendiri; Docker Desktop di mesin
   pengembang memiliki RAM sekitar 3,5 GB, container PostgreSQL menunggu ±85 detik untuk siap dan paket selesai dalam 220–290
   detik. Penyebabnya diduga kuat kontensi sumber daya (bukan cacat logika); tes bergantung pada batas produksi yang sengaja ketat.
2. **`/metrics` untuk worker** (handover parsial 3 §9): apakah worker ikut membuka `/metrics`.

## Keputusan
1. **`systemlog.WithWriteTimeout(d)`**: opsi `Writer` untuk batas waktu satu INSERT. Bawaan **tetap 2 detik** (perilaku produksi
   tidak berubah); nilai ≤ 0 diabaikan. Tes integrasi `systemlog` memakai 30 detik lewat `newWriter`; perilaku batas itu sendiri
   (bawaan 2 detik, override, nilai tak valid, `Log` kembali setelah batas saat database macet) diuji terpisah di
   `timeout_test.go` dengan `DBTX` palsu yang hanya mengamati context (bukan pengganti database).
2. **Paralelisme paket tes dibatasi**: `make test-backend` memakai `go test ./... -race -count=1 -p $(GO_TEST_PARALLEL)` dengan
   `GO_TEST_PARALLEL ?= 3`. Dapat diganti per-eksekusi (`make test-backend GO_TEST_PARALLEL=6`), mis. di CI yang lebih besar.
   Konsekuensi: `make verify` bisa lebih lambat di mesin besar, tetapi tidak acak-gagal di mesin kecil.
3. **Tunggu Redis di `testkit` tidak bergantung pada Docker API.** Pada 2026-10-12, `make verify` merah di
   `TestRedisClientStartsClean`: container Redis sudah menulis "Ready to accept connections" dalam ±1 detik, tetapi
   `wait.ForListeningPort` (polling status container lewat Docker API) gagal selama 90 detik dengan `context deadline
   exceeded`. Paket yang sama lulus 3× berturut-turut bila dijalankan sendirian. Perbaikan: tunggu berbasis log (seperti
   container PostgreSQL), lalu `waitRedisReady` memverifikasi jalur host:port yang benar-benar dipakai tes lewat `PING` inline
   (batas 60 detik; galat akhir memuat alasan nyata percobaan terakhir). Penyebab di tingkat Docker Desktop tetap dugaan.
4. **`/metrics` dibagi per proses (diimplementasikan di T5d):**
   - **API** membuka `/metrics` di port `METRICS_PORT`: metrik HTTP per pola rute, statistik pool, dan gauge
     `outbox_events_pending` serta `outbox_oldest_pending_age_seconds` (dibaca dari database saat scrape). Gauge outbox sengaja di
     API: docs/14 §5 menyebutnya sinyal kesehatan worker, jadi harus tetap terbaca saat worker mati.
   - **Worker** membuka `/metrics` di port **terpisah** `WORKER_METRICS_PORT`: metrik proses worker (`worker_job_*`,
     `outbox_events_dead_total`, statistik pool miliknya).
   - Keduanya bawaan `0` (nonaktif). `.env.example` mengisi 9090 dan 9091. Dua variabel, bukan satu, karena `make dev` menjalankan
     API dan worker native di satu host dengan `.env` yang sama. Compose tidak mem-publish port ini.

## Konsekuensi
- Doc-sync (ritual akhir M02): docs/16 §2.3 (`test-backend` memakai `-p`), docs/14 §5 (pembagian metrik per proses, port), docs/10 §8
  (`METRICS_PORT`, `WORKER_METRICS_PORT`; `OUTBOX_*` sengaja tidak diekspos, ADR-0016).
- Tes lain yang bergantung pada batas waktu produksi dan database nyata harus mengikuti pola yang sama (batas bisa di-override).
