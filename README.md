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

## Perintah utama (`make help` untuk daftar lengkap)

| Perintah | Fungsi |
|---|---|
| `make verify` | **Gerbang tunggal**: lint + tes + migrate-roundtrip + build + smoke pada stack Docker sementara. Harus hijau sebelum merge. |
| `make lint` / `make test` | lint Go + frontend + pemeriksaan struktural / tes backend (`-race`, testcontainers) + frontend |
| `make generate` | regenerasi OpenAPI, SDK TS, kode sqlc, registri modul (bertahap per milestone) |
| `make infra-up` | hanya datastore, untuk pengembangan native |
| `make clean` | hapus artefak **dan volume data** (destruktif) |

`make verify` memakai compose project terpisah (`platform-verify`) sehingga tidak menghapus data pengembanganmu, tetapi
port 5432/6379/8080/5173/1025/8025 harus bebas (hentikan `make dev`/`make docker-up` dulu).
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
