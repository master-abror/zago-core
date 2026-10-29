# Open Source Modular Application Platform

Platform aplikasi modular: **inti kecil** (identitas, otorisasi, audit, event, sistem modul) dan **modul bisnis**
yang dipasang di atasnya. Stack: Go · PostgreSQL 18 · Redis 8 · Svelte 5 · Docker.

> Status per milestone ada di [`docs/STATUS.md`](docs/STATUS.md). Seluruh desain ada di [`docs/`](docs/)
> (mulai dari [`docs/00-errata-and-amendments.md`](docs/00-errata-and-amendments.md) dan [`CLAUDE.md`](CLAUDE.md)).

## Prasyarat

| Alat | Versi | Catatan |
|---|---|---|
| Go | 1.26.5 | lihat `go.mod`, ADR-0002 |
| Node.js | 24 | `.nvmrc` (`nvm install`) |
| Docker + Compose | terbaru | untuk datastore, tes integrasi, dan `make verify` |
| `make`, `bash`, `curl` | | |
| C compiler (`gcc`) | | hanya untuk `go test -race` (Ubuntu/WSL: `sudo apt install build-essential`) |

**Windows:** jalankan semuanya di dalam WSL2 dan **simpan repo di filesystem Linux** (mis. `~/projects/platform`),
bukan di `/mnt/c/...` atau folder OneDrive: I/O lintas-filesystem membuat `npm`, `go test`, Vitest, hot reload,
dan bind-mount Docker jauh lebih lambat dan bisa menyebabkan timeout. Pasang Go dan Node **di dalam WSL**
(bukan memakai `npm` Windows), dan aktifkan WSL Integration di Docker Desktop.

## Mulai cepat

```bash
git clone https://github.com/master-abror/zago-core.git && cd zago-core
make setup          # .env, dependensi, datastore (postgres, redis, mailpit), migrasi
make dev            # api + worker + frontend native, hot reload, satu terminal (Ctrl-C menghentikan semuanya)
```

Lalu: `curl localhost:8080/health/ready` → `200` dengan `postgres` dan `redis` `ok`; frontend di
<http://localhost:5173> (Vite meneruskan `/api` dan `/ws` ke `:8080`); email pengembangan di <http://localhost:8025> (Mailpit).

Semua stack di Docker: `make docker-up` / `make docker-down`.

Admin pertama (setelah stack jalan): `make db-seed && make bootstrap-admin EMAIL=kamu@example.org`, lalu `POST /api/v1/auth/login`
(cookie sesi `HttpOnly` + `csrf_token` di body; kirim `X-CSRF-Token` pada setiap request pengubah-state).

**Keamanan login (M03, ADR-0020):** Argon2id; token sesi 256-bit yang hanya disimpan sebagai hash (Redis + PostgreSQL, tetap berjalan bila Redis mati);
rotasi sesi saat login, ganti password, refresh, dan pindah organisasi; CSRF terikat sesi; 5 password salah/15 menit mengunci akun secara **tak terlihat**
(login tetap "email atau password salah", durasi berlipat 15→30→60 menit) dan 30 kegagalan/15 menit per IP ditolak 429 + `Retry-After`.
Pembuka kunci darurat: `redis-cli DEL auth:lock:<user_id>`. Batas yang diketahui (kunci akun bisa dipakai mengunci korban yang emailnya diketahui,
password spraying lintas IP, tanpa MFA/CAPTCHA) ada di `docs/handover/HANDOVER-M03.md`. Tidak ada jaminan keamanan absolut.

Port host bentrok dengan proyek lain? Jangan edit `.env` manual: `bash scripts/env-ports.sh --force POSTGRES=25000 REDIS=25001 API=25002 WEB=25003 SMTP=25004 MAILUI=25005`
(ADR-0021; port di dalam jaringan compose tidak berubah).

## Perintah utama (`make help` untuk daftar lengkap)

| Perintah | Fungsi |
|---|---|
| `make verify` | **Gerbang tunggal**: lint + `generate-check` + tes + migrate-roundtrip + build + smoke pada stack Docker sementara. Harus hijau sebelum merge. |
| `make lint` / `make test` | lint Go + frontend + pemeriksaan struktural / tes backend (`-race`, testcontainers) + frontend |
| `make generate` / `make generate-check` | regenerasi OpenAPI (`docs/api/swagger.json`), kode sqlc, SDK TS dan registri modul (bertahap per milestone) / gagal bila keluaran generator di disk basi |
| `make smoke` | semua `scripts/smoke/m*.sh` terhadap stack yang berjalan (`M00`: fondasi; `M02`: kernel lewat endpoint echo; `M03`: login/logout, ganti password, kunci akun). Membuat user Super Admin smoke, jadi hanya terhadap localhost; butuh Go, DB terjangkau dari host, dan `docker compose exec redis` |
| `make migrate-up` / `migrate-down` / `migrate-version` | terapkan / mundurkan satu / tampilkan versi migrasi inti (`MIGRATION_DATABASE_URL`) |
| `make db-seed` | pastikan baris platform ada (idempoten); WAJIB sebelum `make bootstrap-admin` (`make setup`/`make dev` hanya menjalankan migrasi) |
| `make bootstrap-admin EMAIL=…` | buat Super Admin pertama (idempoten, tidak pernah menimpa password): `NAME=…`, `PASSWORD_STDIN=1` (baca password dari stdin; tanpa itu password acak dicetak SEKALI) |
| `make migrate-roundtrip` / `make db-test` | up→down→up pada PostgreSQL sementara / tes constraint, trigger, cascade, grant skema (butuh Docker). Skema: [docs/erd.md](docs/erd.md) |
| `make infra-up` | hanya datastore, untuk pengembangan native |
| `make clean` | hapus artefak **dan volume data** (destruktif) |

Endpoint khusus pengembangan (hanya saat `ENVIRONMENT` = `development`/`test`; tidak pernah ada di staging/production): `POST` dan
`GET /api/v1/_kernel/echo` (+ `/items`) memperagakan envelope, idempotency, rate limit, dan pagination cursor; lihat `scripts/smoke/m02.sh`.
Metrik Prometheus disajikan di **port terpisah** (bukan port API) bila `METRICS_PORT` (API) / `WORKER_METRICS_PORT` (worker) diisi;
`.env.example` memakai 9090/9091 dan Compose tidak mem-publish-nya. Kecepatan `make test`: `make test-backend GO_TEST_PARALLEL=N`
membatasi jumlah paket tes yang menyalakan container bersamaan (bawaan 3).

`make verify` memakai compose project terpisah (`platform-verify`) sehingga tidak menghapus data pengembanganmu, tetapi
port host yang dipublikasikan compose (bawaan 5432/6379/8080/5173/1025/8025, atau yang kamu setel di `.env`) harus bebas (hentikan `make dev`/`make docker-up` dulu).
Di Docker Desktop bermemori kecil, tes integrasi bisa flake (`Reaper handshake failed`, `connection refused`): jalankan paket itu sendirian dan ulangi, jangan ubah kode.
Build pertama dengan cache dingin butuh beberapa menit; jangan hentikan saat tampak diam di `menunggu: api`.

## Struktur

```text
backend/{cmd,internal,pkg,migrations}   Go: api, worker, migrate; kernel
apps/web                                 Svelte 5 SPA (runes)
packages/{module-sdk,ts-sdk,ui}          kontrak modul, SDK TypeScript, UI kit
modules/                                 modul bisnis (hanya impor packages/module-sdk)
deploy/{docker,db,nginx}                 Dockerfile, init SQL, konfigurasi web
scripts/                                 dev, smoke, lint-structure, handover, repo-zip
docs/                                    desain 00–23, ADR, STATUS, handover
```

## Lisensi komponen: Redis atau Valkey

Redis 8 dirilis dengan opsi lisensi **RSALv2 / SSPLv1 / AGPLv3**; **Valkey** adalah fork berlisensi **BSD**.
Pilihan proyek ini: kode hanya memakai perintah Redis dasar, sehingga operator bebas memakai Redis atau Valkey
tanpa mengubah kode. `docker-compose.yml` memakai `redis:8` hanya untuk kenyamanan pengembangan lokal.
Kompatibilitas Valkey **belum diuji di CI** (dijadwalkan di `docs/20-roadmap.md`); sampai itu ada, anggap Valkey
sebagai kompatibel menurut desain, bukan menurut bukti. Alasan pemilihan: `docs/adr/0001-stack.md`.

## Kontribusi

Baca [`CLAUDE.md`](CLAUDE.md) (aturan keras) dan [`docs/22-handover-protocol.md`](docs/22-handover-protocol.md).
Perubahan yang menyimpang dari dokumen wajib punya ADR di `docs/adr/`.
Lisensi proyek ini sendiri belum ditetapkan di dokumen; tentukan sebelum rilis publik.
