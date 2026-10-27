# Pengembangan di VPS (ADR-0021)

Panduan memindahkan pengembangan dari laptop ke VPS. Semua perintah dijalankan di VPS kecuali disebut "di laptop".
Asumsi: Ubuntu/Debian, akses SSH berkunci. Hal yang belum terbukti di repo ini ditandai **(belum terbukti)**.

## 1. Prasyarat di VPS
`git`, `make`, `zip`, `curl`; Docker Engine + plugin Compose v2 (user masuk grup `docker`); Go 1.26.5; Node 24 (`.nvmrc`).
Perkiraan sumber daya (belum diukur): RAM ≥ 4 GB (tes menyalakan container PostgreSQL per paket), disk ≥ 20 GB.
Cek cepat: `docker compose version && go version && node --version`.

## 2. Kunci GitHub untuk VPS
Buat kunci khusus, lalu daftarkan sebagai *Deploy key* dengan **Allow write access** di Settings repo GitHub (repo harus private):
`ssh-keygen -t ed25519 -C "vps-zago" -f ~/.ssh/id_ed25519_zago` → tempel isi `~/.ssh/id_ed25519_zago.pub` ke GitHub.
Pakai kunci itu: `GIT_SSH_COMMAND='ssh -i ~/.ssh/id_ed25519_zago -o IdentitiesOnly=yes' git clone <URL-origin> ~/projects/zago-core`
(URL origin: di laptop, `git remote get-url origin`). Agar permanen: `git config core.sshCommand 'ssh -i ~/.ssh/id_ed25519_zago -o IdentitiesOnly=yes'`.

## 3. Pindah (urutan)
1. Di laptop: pastikan semua commit sudah ada di origin: `git status -sb` bersih dan `git push`. Hentikan stack lokal: `make docker-down`.
2. Di VPS: clone (bagian 2), lalu `cd ~/projects/zago-core && git switch milestone/m03-authentication`.
3. Pilih port yang bebas: `ss -ltnH | awk '{print $4}' | sort`.
4. Buat `.env` (contoh; ganti angkanya dengan port bebas):
   `bash scripts/env-ports.sh POSTGRES=15432 REDIS=16379 API=18080 WEB=15173 SMTP=11025 MAILUI=18025`
5. Verifikasi pertama di VPS (baseline; harus hijau sebelum menyentuh kode):
   `make verify GO_TEST_PARALLEL=1 > ~/verify.log 2>&1; echo "exit=$?"; grep -nE '^(--- FAIL|FAIL|ok |\?)|✗|▶|smoke' ~/verify.log | tail -70`

`.env` tidak ada di git (jangan di-commit); data PostgreSQL di volume Docker laptop tidak ikut pindah (data pengembangan, dibuat ulang oleh migrasi + `make db-seed` + `make bootstrap-admin`).

## 4. Mengakses dari laptop
Port hanya terikat `127.0.0.1` di VPS; jangan dibuka di firewall. Pakai tunnel SSH dengan port lokal **sama** dengan port VPS
(origin diperiksa persis, ADR-0021 butir 6):
`ssh -L 15173:127.0.0.1:15173 -L 18025:127.0.0.1:18025 <user>@<vps>` lalu buka `http://localhost:15173` (UI) dan `http://localhost:18025` (Mailpit).

## 5. Alur kerja harian
- Patch dari chat: unduh di laptop, kirim ke VPS (`scp patch.patch <user>@<vps>:~/`), lalu di VPS `git am < ~/patch.patch`, cek, `git push`.
- Zip untuk chat berikutnya dibuat di VPS: `bash scripts/repo-zip.sh ~/platform-repo-$(date +%Y%m%d-%H%M).zip`, lalu di laptop
  `scp <user>@<vps>:~/platform-repo-YYYYMMDD-HHMM.zip ~/Downloads/` (nama persis, bukan wildcard).
- Commit dibuat di satu tempat saja (VPS) supaya tidak ada dua salinan yang divergen.

## 6. Batasan
`make dev` (native) tidak ikut mengubah port; di VPS pakai `make docker-up` (ADR-0021, "Di luar cakupan").
Password database di compose hanya untuk pengembangan; jangan pernah membuka port datastore ke publik.
