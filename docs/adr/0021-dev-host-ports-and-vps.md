# ADR-0021 — Port host compose dapat disetel lewat .env; pindah pengembangan ke VPS

**Status:** Diterima · **Tanggal:** 2026-10-27 · **Konteks:** di tengah M03 (setelah ◆CP3), di luar scope fitur M03.
Pengembangan dipindahkan dari laptop (WSL2 + Docker Desktop) ke VPS. Di VPS port 5432/6379/8080 sudah dipakai layanan lain,
sedangkan `docker-compose.yml` mematok port host itu.

## Keputusan
1. **Setiap port host di `docker-compose.yml` memakai `${NAMA_HOST_PORT:-bawaan}`**, bawaan = port lama:
   `POSTGRES_HOST_PORT` 5432, `REDIS_HOST_PORT` 6379, `API_HOST_PORT` 8080, `WEB_HOST_PORT` 5173,
   `MAILPIT_SMTP_HOST_PORT` 1025, `MAILPIT_UI_HOST_PORT` 8025. Tanpa `.env` khusus, perilaku di laptop tidak berubah.
   Pemetaan tetap terikat `127.0.0.1` (tidak pernah dipublikasikan ke jaringan).
2. **Port di dalam jaringan compose tidak berubah** (`postgres:5432`, `redis:6379`, `mailpit:1025`, `api:8080`).
   `PORT` di `.env` adalah port DI DALAM container api (`env_file`), jadi tidak boleh diubah untuk menghindari bentrok.
3. **`api` dan `worker` meng-override `SMTP_PORT=1025`.** `SMTP_PORT` di `.env` adalah port HOST mailpit (dipakai proses native);
   karena `env_file: .env` ikut masuk ke container, tanpa override container akan menghubungi `mailpit:<port host>` yang salah.
4. **`scripts/env-ports.sh` membuat `.env` yang konsisten** dari `.env.example`: tiga URL database, `REDIS_URL`, `ALLOWED_ORIGINS`,
   `PUBLIC_BASE_URL`, `SMTP_PORT`, ditambah variabel `*_HOST_PORT`. Port 1024–65535 dan semuanya berbeda; input salah = gagal
   tanpa menulis apa pun; `.env` yang ada tidak ditimpa tanpa `--force`; bila bentuk `.env.example` berubah sehingga penggantian
   tak kena, skrip gagal keras. Nilai `.env.example` tidak diubah (hanya komentar), sehingga tes `env_example_test.go` tetap valid.
5. **`verify-smoke.sh` dan smoke m00/m02 membaca port dari environment, lalu `.env`, lalu bawaan.** `make` mengekspor `.env`,
   jadi `make verify`/`make smoke` mengikuti port VPS tanpa argumen tambahan. `BASE`/`WEB` tetap bisa di-override manual.
   Aturan smoke "hanya localhost" (`auth-lib.sh`) tidak berubah: smoke dijalankan di VPS itu sendiri.
6. **Akses dari laptop lewat tunnel SSH dengan port lokal SAMA dengan port host VPS** (`ssh -L 15173:127.0.0.1:15173 …`):
   `ALLOWED_ORIGINS` memeriksa `Origin` persis (ADR-0020 butir 12), jadi peramban harus memakai `http://localhost:<WEB_HOST_PORT>`.
7. **Alur kerja satu arah:** commit dibuat di satu tempat saja (VPS), `git push` dari sana; zip untuk chat dibuat di VPS
   (`scripts/repo-zip.sh`). Runbook: `deploy/vps/README.md`.

## Di luar cakupan
- `make dev` (api/worker/frontend NATIVE) tetap memakai `PORT`, 5173, 9090, 9091 di host. Di VPS dengan port bentrok pakai
  `make docker-up`; mengubah `PORT` merusak container api (butir 2).
- Tes Go memakai testcontainers dengan port acak dan `migrate-roundtrip.sh` memakai `-p 127.0.0.1::5432`; keduanya tak terpengaruh.
- Deployment produksi (bukan pengembangan) tidak diubah.

## Konsekuensi
- Tes baru `backend/pkg/config/env_ports_test.go`: skrip (nilai, bawaan, negatif, tanpa timpa) dan penjaga compose
  (setiap port host harus berbentuk variabel dengan bawaan = port lama).
- **Doc-sync TERTUNDA (akhir M03):** docs/15 §2 (port host dapat disetel; `env-ports.sh`), docs/16 §2.7 (`verify-smoke` membaca port),
  docs/19 (CI tidak terpengaruh; catat bila CI memakai port non-bawaan), README (alur pengembangan di VPS).

## Bagian 2 — pengembangan kembali ke laptop (2026-10-29)
Akses ke VPS hilang; pengembangan M03 dilanjutkan di laptop (WSL2 Ubuntu, `~/projects/zago-core-m03`). Butir 1–5 (port host dapat disetel lewat `.env`) tetap berlaku dan dipakai di laptop:
`bash scripts/env-ports.sh --force POSTGRES=25000 REDIS=25001 API=25002 WEB=25003 SMTP=25004 MAILUI=25005`, karena port 5432/6379 dipegang container proyek lain yang tidak boleh dimatikan.
- **Butir 6 (tunnel SSH) dan butir 7 (commit hanya di VPS) tidak berlaku lagi.** Commit dibuat di satu tempat saja (laptop) dan `git push` WAJIB setelah setiap patch diterapkan dan diverifikasi: lima commit T5 nyaris hilang karena belum di-push saat akses VPS putus.
- `deploy/vps/README.md` adalah catatan historis. Semua rujukan "di VPS" di handover parsial 3–6 menggambarkan keadaan sebelumnya.
- Docker Desktop laptop (≈3,5 GB memori) kadang flake (`Reaper handshake failed`, `wait for reaper … context deadline exceeded`, `RWLayer … is unexpectedly nil`, `connection refused` padahal container ready, `failed to authorize: DeadlineExceeded` saat build). Itu masalah lingkungan, bukan kode: jalankan paket itu sendirian (`REQUIRE_DOCKER=1 go test <paket> -race -count=1 -p 1`), pulihkan Docker, ulangi. Di laptop non-root, `verify-smoke.sh` juga memeriksa berkas milik root di repo (`sudo chown -R "$USER":"$USER" .` bila perlu; muncul lagi setelah run bersih = celah di `docker-compose.yml`, perbaiki lewat commit tersendiri).
