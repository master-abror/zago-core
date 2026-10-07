# ADR-0007 — `air`: `build.bin` diganti `build.entrypoint`

**Status:** Diterima · **Tanggal:** 2026-10-05 · **Menjawab:** HANDOVER-M00 §8 (butir 1)

## Konteks
`air` v1.67.4 mencetak `build.bin is deprecated; set build.entrypoint instead` untuk `.air.api.toml` dan `.air.worker.toml`.

## Keputusan
Ganti `bin = "./tmp/api"` menjadi `entrypoint = ["./tmp/api"]` (idem worker). Dari kode sumber air v1.67.4
(`runner/config.go`): peringatan hanya muncul bila `bin` diisi **tanpa** `entrypoint`, dan `setEntrypointFromBin` memetakan
`bin` ke `entrypoint{bin}` — jadi perilaku identik, hanya peringatannya hilang.

## Bukti & batas
Terbukti dari pembacaan kode sumber, **belum** dari menjalankan `make dev` (sandbox tak punya Docker/air). Verifikasi: `make run-api`
tidak lagi mencetak peringatan dan API tetap start.
