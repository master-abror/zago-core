// Package telemetry menyediakan metrik Prometheus (docs/14 §5): registry per proses, handler
// /metrics, metrik HTTP per POLA rute, metrik worker, statistik pool database, dan gauge outbox.
//
// Paket ini tidak mengenal chi, pgx, maupun kernel: pemanggil (composition root / middleware)
// yang menyuntikkan data. Tidak ada registry global, jadi tiap proses dan tiap tes punya
// registry sendiri. Label SELALU berkardinalitas rendah (docs/14 §5): tak pernah id pengguna,
// id organisasi, atau path mentah.
package telemetry
