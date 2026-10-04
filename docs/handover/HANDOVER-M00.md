# HANDOVER — M00 Foundation & Tooling

**Tanggal:** 2026-10-04 · **Status:** DONE (V1–V3 tercatat; merge ke `main` hanya setelah CI hijau pada head akhir) · **Tag:** `m00-done` (diberikan setelah merge)
**Branch:** `milestone/m00-foundation` · **Handover sebelumnya:** `archive/HANDOVER-M00-partial-1.md`

## 1. Ringkasan
Fondasi selesai: monorepo (Go + Svelte 5), `Makefile` sesuai docs/16, compose dev + Dockerfile berkonteks root,
kernel Go (`pkg/{id,logger,config}`, `internal/{health,httpserver,app,infra}`), tiga entrypoint
(`cmd/{api,worker,migrate}`; migrate masih kerangka), proxy dev Vite, CI GitHub Actions (belum pernah dijalankan),
skrip (`dev`, `wait-for-healthy`, `verify-smoke`, `lint-structure`, `smoke/m00`), ADR-0001..0003, README, CHANGELOG.
Tidak ada fitur bisnis, tidak ada migrasi, tidak ada endpoint selain `/health*` (sesuai batas M00).

## 2. Status task
| Task | Status |
|---|---|
| T1 skeleton + `apps/web` | DONE |
| T2 `go.mod` + Makefile | DONE (`tool` directives dan `go.sum` ada, 5 alat terpatok) |
| T3 compose + Dockerfile + air | DONE (image backend 386 MB; stack naik penuh di Docker) |
| T4 `pkg/id`, `pkg/config`, `pkg/logger` | DONE |
| T5 server, health, shutdown, app, infra, cmd | DONE (semua paket terkompilasi dan teruji di Go 1.26.5 asli) |
| T6 skrip, proxy Vite, CI, smoke | DONE (CI belum pernah berjalan di GitHub) |
| T7 ADR, STATUS, README, CHANGELOG, handover | DONE |

## 3. Bukti verifikasi (apa adanya)
**Terbukti** (log dari mesin pengembang, 2026-10-03; WSL2 Ubuntu, Docker Desktop). *Butir di bawah (sampai "Di sandbox") diperoleh SEBELUM path modul diganti (ADR-0004); dikonfirmasi ulang oleh V1 dan V3 sesudahnya.*
- `make lint`: golangci-lint 0 issues · svelte-check 0 error 0 warning · Prettier bersih · `lint-structure OK`.
- `make test`: backend lulus dengan `-race` (`app`, `health`, `httpserver`, `infra` ±20 dtk, `pkg/*`); frontend Vitest 3/3.
  `internal/infra` memakai PostgreSQL 18.6 dan Redis 8.10.2 sungguhan: `/health/ready` 200 → 503 saat container dihentikan.
- `make build`: tiga binary (`CGO_ENABLED=0`) dan bundle Vite.
- `make migrate-roundtrip` (kerangka), `make setup` (install, infra-up, migrate-up), dan `make verify` sampai tahap smoke:
  stack Docker penuh naik, `api` healthy ±118 dtk dengan cache dingin, `smoke/m00.sh` → `M00 smoke OK` (8 langkah,
  termasuk proxy `/api` Vite → API).
- `make dev` (native): worker, api (air), dan Vite (`ready`) berjalan bersamaan dalam satu terminal.
- Di sandbox (tanpa Docker): `dev.sh` (crash, Ctrl-C, tanpa proses yatim), `wait-for-healthy.sh` (6 skenario),
  `verify-smoke.sh` (guard port, `.env`, `down -v` selalu jalan), `lint-structure.sh` (5 pelanggaran terdeteksi).

**Terbukti SESUDAH penggantian path modul (ADR-0004), 2026-10-04:**
- **V1** — `make verify` pada klon bersih di filesystem Linux (`~/projects/zago-core`, tanpa `make setup` lebih dulu): **`exit=0`**.
  `.env` dan `node_modules` dibuat otomatis; golangci-lint 0 issues; `internal/infra` 23 dtk di PostgreSQL/Redis sungguhan;
  roundtrip (kerangka), build, lalu stack Docker penuh naik (`api` healthy 177 dtk dengan cache dingin) dan `M00 smoke OK`.
- **V3** — CI GitHub Actions hijau pada PR #1: 6 job (`lint`, `test`, `migrations`, `build`, `docker-smoke`, `contract-check`);
  run `actions/runs/37163858143`. Ini juga membuktikan `internal/infra`, `cmd/api`, `cmd/worker` terkompilasi di runner bersih.

- **V2** — jalur native (2026-10-04, WSL2, repo di filesystem Linux): `make setup && make dev` → worker, api (air), dan Vite berjalan
  dalam satu terminal; `curl -i localhost:8080/health/ready` → **`HTTP/1.1 200 OK`**, badan `{"status":"ok","checks":{"postgres":"ok","redis":"ok"}}`
  (header keamanan dan `X-Request-Id` UUIDv7 hadir); `curl localhost:5173/api/x` → `{"error":{"code":"not_found"}}` (proxy Vite → API);
  `tmp/` milik pengguna, bukan root. **Ctrl-C** → log `shutdown dimulai` (worker dan api), `api stopped`, dependensi ditutup terbalik dari urutan dibuka,
  `make dev` keluar dengan kode 130, dan `ps aux | grep -E 'tmp/(api|worker)|vite'` **kosong** (tanpa proses yatim).
  Bug yang ditemukan lewat V2 dan diperbaiki: container menulis berkas milik root ke working tree (`EACCES` pada `npm ci`); perbaikan = volume
  anonim `/app/apps/web/node_modules`, `/app/tmp`, dan `verify-smoke.sh` gagal bila tersisa berkas milik root. `make verify` sesudah perbaikan: `exit=0`,
  `find . -user root` kosong. Penjaga `node_modules` di `verify` juga diperkuat (`.package-lock.json`).

**Syarat merge (dicek manual di PR #1):** V3 di atas diperoleh pada commit awal. Sesudahnya ada tiga commit perbaikan yang menyentuh compose
(dipakai job `docker-smoke`), jadi **run CI pada head akhir harus hijau (6 job) sebelum PR digabung**. Merge dengan "Create a merge commit"
(bukan Squash) agar commit kecil tetap tercatat; beri tag `m00-done` pada `main` setelahnya.

## 4. Keputusan & penyimpangan
ADR-0001 (stack, versi) · ADR-0004 (path modul `github.com/master-abror/zago-core`) · ADR-0002 (Go 1.26.5; PG/Redis tag mayor mengambang, teramati 18.6 / 8.10.2) ·
ADR-0003 (tabel penyimpangan kecil: volume `migrate`, CREATEDB dev, config per role, `db-test`/`generate` bertahap,
`contract-check` dengan `git status`, Dockerfile tanpa `go mod download`, Vitest env `node`, `verify` membuat `.env`).
Doc-sync selesai untuk docs/10 §8 dan docs/15 (compose + Dockerfile).

## 5. Delta kontrak
- **DB:** tidak ada migrasi. Init dev: `deploy/db/init/01-dev-privileges.sql` (`app_migrator` CREATEDB; dev/CI saja).
- **API:** `GET /health`, `GET /health/live` (200); `GET /health/ready` (200/503, badan `{status, checks{nama: ok|unavailable}}`);
  error JSON `{"error":{"code":...}}` untuk 404/405/500 (bentuk final di M02).
- **Event / permission:** tidak ada.
- **Config/env:** semua variabel di `.env.example`, divalidasi per role; `TRUSTED_PROXIES` sebagai CIDR/IP; `COOKIE_SECURE` default `true`.

## 6. Peta kode
| Hal | Lokasi |
|---|---|
| Start/stop api, worker, migrate | `backend/internal/app` (`cmd/*` hanya wiring) |
| Adapter pgx / go-redis | `backend/internal/infra` |
| Middleware, router, graceful shutdown | `backend/internal/httpserver` |
| Liveness / readiness | `backend/internal/health` |
| Config / logger / UUIDv7 | `backend/pkg/{config,logger,id}` |
| Klien API frontend (satu-satunya pintu fetch) | `apps/web/src/core/api/client.ts` |
| Gerbang verifikasi | `Makefile` (`verify`), `scripts/verify-smoke.sh`, `scripts/smoke/m00.sh` |
| CI | `.github/workflows/ci.yml` |

## 7. Cara menjalankan & memverifikasi
```bash
git clone https://github.com/master-abror/zago-core.git && cd zago-core     # taruh di filesystem Linux/WSL, BUKAN /mnt/c atau OneDrive
make verify                                  # mandiri: membuat .env + npm ci bila perlu; port 5432/6379/8080/5173/1025/8025 harus bebas; build pertama beberapa menit
make setup && make dev                       # (terpisah, SETELAH verify selesai) api + worker + web native; curl localhost:8080/health/ready → 200
```
Prasyarat: Go 1.26.5, Node 24 (`nvm install`), Docker, `make`, dan `gcc` (hanya untuk `go test -race`). Lihat README.

## 8. Masalah diketahui / utang teknis
- `air` v1.67.4 memperingatkan `build.bin is deprecated; set build.entrypoint instead` di `.air.api.toml`/`.air.worker.toml`
  (tidak fatal; perbaikan belum diuji: ganti `bin = "./tmp/api"` menjadi `entrypoint = ["./tmp/api"]`, lalu `make dev`).
- `migrate-roundtrip` masih kerangka. **M01 harus memutuskan** dari mana PostgreSQL datang saat `make verify`
  (urutan sekarang: roundtrip dulu, `verify-smoke` terakhir; tak ada Postgres menyala di tahap roundtrip dan
  `verify-smoke` menolak jalan bila port 5432 terpakai) — mis. roundtrip lewat testcontainers.
- Worker tak punya health endpoint sendiri; `docker-compose` hanya memantau prosesnya berjalan.
- Setiap perubahan compose yang menambah jalur tulis ke bind-mount `.:/app` harus di-mask volume (lihat V2); `verify-smoke.sh` mendeteksi kebocoran berkas root.
- Start pertama dengan cache modul dingin lambat (compose `start_period` 120 dtk); di `/mnt/c` Vite butuh ±36 dtk untuk siap.
- Tag image PG/Redis mengambang (ADR-0002); Valkey belum diuji di CI (docs/20).
- Lisensi proyek belum ditetapkan.

## 9. Pertanyaan terbuka
- ~~Path modul Go~~ → diputuskan: `github.com/master-abror/zago-core` (ADR-0004).
- Lisensi proyek (belum ditetapkan).

## 10. Langkah pertama berikutnya
1. Pastikan PR #1 sudah digabung (merge commit) dengan CI hijau, lalu `git tag m00-done` pada `main`.
2. M01 — Database Schema: buka `docs/21` bagian M01 dan hanya dokumen yang tercantum di "Baca:"-nya (docs/04 dan terkait). Cabang: `milestone/m01-database-schema`.
3. Putuskan item `migrate-roundtrip` (§8) sebagai ADR di awal M01.
