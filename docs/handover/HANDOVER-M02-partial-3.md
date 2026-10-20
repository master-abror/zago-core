# HANDOVER — M02 Kernel (PARSIAL 3, berhenti di ◆CP3)

**Tanggal:** 2026-10-10 · **Status:** IN PROGRESS · **Jenis:** PARSIAL (T6, T1, T2, T3, T4, T5a, T5b, T5c selesai; tersisa T5d, T5e, uji e2e `RunAPI`, lalu ritual akhir)
**Branch:** `milestone/m02-kernel` (basis: tag `m01-done`) · **Handover sebelumnya:** `HANDOVER-M02-partial-2.md` (commit `29aa47d`)
**Commit sesi ini (urut):** `ca77bd8` (T5b `logger.SystemLog` + gate, ADR-0015) · `01ef4ae` (T5a+T5c toolkit HTTP di composition root + endpoint dev `_kernel/echo`, ADR-0016) · commit handover ini.

## 1. Ringkasan
Toolkit HTTP kernel kini **terpasang dan terbukti ujung-ke-ujung** lewat `httpserver.NewHandler` (envelope, `request_id`, idempotency, rate limit, cursor, audit nyata ke
PostgreSQL). Composition root punya satu klien Redis (`app.Resource.Redis`), merakit `httpserver.KernelServices`, dan memasang `audit.Recorder` + sink `system_logs` (lewat gate logger).
Endpoint dev/test `/api/v1/_kernel/echo` hanya terdaftar bila `ENVIRONMENT` ∈ {development, test}.
**Belum ada:** `pkg/telemetry` (`/metrics`, OTel), sqlc contoh, swag, dan uji `RunAPI` nyata (infra.Deps + PG + Redis) lewat echo — itu sisa T5.

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T6 · T1 · T2 · T3 · T4 | DONE | lihat parsial 1 dan 2 |
| T5a klien Redis tunggal + `KernelServices` di composition root | DONE | ADR-0016; `httpserver/kernel.go`, `app/app.go`, `infra/redis.go` |
| T5b `logger.SystemLog` + gate, audit/system_logs terpasang ke API dan worker | DONE | ADR-0015 |
| T5c endpoint dev `/api/v1/_kernel/echo` + tes ujung-ke-ujung | DONE | ADR-0016; `httpserver/echo.go` |
| T5d `pkg/telemetry` (`/metrics` Prometheus, OTel no-op), `METRICS_PORT` | TODO | lihat §10 |
| T5e sqlc contoh + swag (`make generate`) | TODO | lihat §10 |
| T5f uji e2e `RunAPI` + `infra.Deps` (PG + Redis nyata) memakai echo | TODO | menutup celah di §3 "Belum terbukti" |

## 3. Bukti verifikasi (apa adanya)
**Terbukti** di mesin pengembang (WSL2, Docker Desktop, testcontainers PostgreSQL 18 + Redis 8), 2026-10-10, `-race`:
- `ca77bd8`: `gofmt`, `go vet ./backend/...`; `REQUIRE_DOCKER=1 go test` untuk `pkg/logger`, `internal/systemlog`, `internal/infra`, `internal/app` lulus; `go tool golangci-lint run ./...` → 0 issues; `lint-structure OK`.
- `01ef4ae`: `gofmt`, `go vet ./backend/...`; `REQUIRE_DOCKER=1 go test` untuk `internal/httpserver` (termasuk `echo_audit_test.go`: activity nyata di PostgreSQL dengan `request_id`/aktor/IP/UA dan log yang sama), `internal/app`, `internal/infra`, `pkg/config` lulus; lint 0 issues; `lint-structure OK`.
- `make verify` penuh (termasuk `verify-smoke`, "M00 smoke OK") hijau **setelah T4** menurut pengembang (2026-10-10).

**Belum terbukti:**
- **`make verify` penuh SETELAH T5a/T5b/T5c**, khususnya `verify-smoke`: `RunAPI` kini merakit toolkit dan (di development) memasang rute echo; smoke belum diulang.
- `go test ./...` seluruh repo dalam satu run setelah T5.
- `RunAPI` nyata (infra.Deps + PG + Redis) belum diuji lewat echo; wiring dibuktikan terpisah: `AttachSystemLog` (infra test), `KernelServices` + audit nyata (httpserver test), gerbang `ENVIRONMENT` (app test dengan fake). → T5f.

**Catatan sandbox (sesi ini; Claude, bukan mesin pengembang):** Go 1.24 (`apt-get update && apt-get install golang-1.24-go redis-server`; jalur `/usr/lib/go-1.24/bin`) dan redis 7.0 tersedia; Go 1.26.5, Docker, Redis 8, PostgreSQL 18 TIDAK ada.
Modul scratch (`go 1.24`, `GOFLAGS=-mod=mod GOPROXY=off`) dengan klon GitHub + `replace` ke direktori lokal (turunkan `go` directive dan kosongkan `require` di tiap klon) untuk: chi v5.3.2, uuid v1.6.0, pgx v5.11.0 (ganti satu `wg.Go` di `pgconn/pgconn.go` dengan `Add/Done`), go-redis v9.22.0, xxhash, go-rendezvous, pgpassfile, pgservicefile, puddle v2.2.2,
golang.org/x/{crypto,sync,text,sys} (dari github.com/golang), go.uber.org/atomic (github.com/uber-go/atomic), caarlos0/env v11, testify v1.10.0 (+ spew, difflib, objx, yaml). Dengan itu `pkg/{logger,config}`, `internal/{kernel/httpx,httpserver,app,audit,systemlog}` dikompilasi dan tesnya (yang tak butuh PG) dijalankan.
`testkit` diganti overlay (Redis dari redis-server lokal) dan `testpg` diganti stub (hanya untuk typecheck). `internal/infra` tidak bisa dikompilasi di sandbox (golang-migrate/testcontainers). Semua yang butuh PostgreSQL atau Redis 8 hanya terbukti di mesin pengembang.

## 4. Keputusan & penyimpangan
- **ADR-0015** — `logger.SystemLog("kode")` berbentuk atribut slog (bukan fungsi `logger.Error(ctx,…)` seperti contoh docs/14 §3); `logger.Gate` di-attach setelah pool siap dan di-detach sebelum pool ditutup; `systemlog.Writer.Sink()`; pemetaan level; atribut `With()` tidak ikut; sink panik dipulihkan.
- **ADR-0016** — satu klien Redis di `app.Resource`; `httpserver.KernelServices`; `audit.Recorder` dipasang di API; gerbang echo = `Config.DevEndpointsEnabled()` (`ENVIRONMENT` development/test; handover lama menulis `APP_ENV`, variabel nyata `ENVIRONMENT`); aktor dev lewat header `X-Dev-Actor`;
  `OUTBOX_*` **tidak** diekspos lewat env; `/metrics` di port terpisah lewat `METRICS_PORT` (bawaan `0` = nonaktif; `.env.example` mengisi 9090 untuk dev) — keputusan sudah diambil, implementasinya T5d.
- **Doc-sync yang tertunda (lakukan di ritual akhir M02):** dari parsial 1 — docs/10 §4, docs/10 §6.2, docs/13 §2.2. Dari parsial 2 — docs/08 §11, §4, §6, §7 dan docs/10 §3 (rinciannya di `HANDOVER-M02-partial-2.md` §4). Baru — docs/14 §3 (bentuk `logger.SystemLog` = atribut slog; gate), docs/14 §5 (`METRICS_PORT`, setelah T5d), docs/10 §8 (catatan `OUTBOX_*` sengaja tidak diekspos; `METRICS_PORT` baru).

## 5. Delta kontrak
- **DB:** tidak ada migrasi baru. **Config/env:** belum ada yang baru (`METRICS_PORT` datang di T5d). **Dependensi Go:** `go.mod`/`go.sum` tidak berubah di sesi ini (T5d/T5e akan mengubahnya).
- **HTTP (hanya development/test):** `POST /api/v1/_kernel/echo` (idempotent, `Idempotency-Key` + `X-Dev-Actor` wajib, 201 `{message, request_id, actor_id}`, merekam activity `kernel.echo`); `GET /api/v1/_kernel/echo/items` (cursor; `limit` bawaan 10, maks 20; `?q=` prefiks). Rate limit 30/menit per IP.
- **Redis key:** `idem:{user_id}:{METHOD}:{route-pattern}:{key}` kini nyata dipakai; `rl:kernel.echo:ip:{ip}`.
- **Go (publik antar-paket):** `pkg/logger`: `SystemLog`, `SystemEntry`, `SystemSink`, `Gate`, `NewGate`, `Option`, `WithSystemGate`, dan `New(w, level, opts ...Option)`. `internal/systemlog`: `Writer.Sink`. `internal/app`: `AttachSystemLog`, `Resource.Redis`.
  `pkg/config`: `Config.DevEndpointsEnabled`. `internal/httpserver`: `KernelDeps`, `KernelServices`, `NewKernelServices`, `Options.Kernel`, `Options.DevEndpoints`, `EchoPath`, `EchoRule`.

## 6. Peta kode (tambahan sejak parsial 2)
| Hal | Lokasi |
|---|---|
| `logger.SystemLog`, `Gate`, `SystemSink`, penanda di handler | `backend/pkg/logger/{systemlog,logger}.go` |
| Adaptor sink `systemlog.Writer` | `backend/internal/systemlog/systemlog.go` (`Sink`, `levelFromSlog`) |
| `AttachSystemLog`, wiring `RunAPI`/`RunWorker`, `Resource.Redis` | `backend/internal/app/app.go` · `backend/internal/infra/redis.go` |
| `KernelServices` | `backend/internal/httpserver/kernel.go` |
| Endpoint echo + `devActor` | `backend/internal/httpserver/echo.go` · rute dipasang di `router.go` (`Options.Kernel`/`DevEndpoints`) |
| Tes echo (Redis nyata; `fakeAudit`) | `backend/internal/httpserver/echo_test.go` · `TestMain` di `main_test.go` |
| Tes echo + audit nyata (PostgreSQL) | `backend/internal/httpserver/echo_audit_test.go` |
| Tes wiring system log (PostgreSQL) · klien Redis `infra` | `backend/internal/infra/systemlog_wiring_test.go` · `redis_client_test.go` |
| ADR | `docs/adr/0015…0016` |

## 7. Cara menjalankan & memverifikasi
```bash
cd ~/projects/zago-core && git switch milestone/m02-kernel
# hentikan stack proyek lain: docker compose -p nizza-ai stop   (port 5432 6379 8080 5173 1025 8025 harus bebas)
make verify                                   # WAJIB dijalankan dulu di sesi berikutnya (lihat §3 "Belum terbukti")
REQUIRE_DOCKER=1 go test ./backend/... -race -count=1 2>&1 | tail -n 60
go tool golangci-lint run ./... && bash scripts/lint-structure.sh
```

## 8. Masalah diketahui / utang teknis
- Semua utang di parsial 1 §8 dan parsial 2 §8 tetap berlaku.
- Belum ada call site produksi yang memakai `logger.SystemLog` (call site pertama menyusul di M03+/M06); `audit.Recorder` memakai `systemlog.Writer` langsung, bukan gate.
- Echo memakai dataset tetap dan aktor dari header: murni dev/test, boleh dihapus setelah ada endpoint nyata yang memakai toolkit yang sama (M03+).
- Dengan Redis mati, request ber-idempotency mendapat 503 (fail closed, ADR-0014), sedangkan rate limit non-sensitif fail open; perilaku ini kini terbukti lewat rute echo.
- `RunAPI` mencatat peringatan bila tidak ada pool atau klien Redis; itu hanya terjadi dengan fake di tes, tidak dengan `infra.Deps`.

## 9. Pertanyaan terbuka
- Lisensi proyek (belum ditetapkan, sejak M00).
- (T5d) apakah worker juga membuka `/metrics` pada `METRICS_PORT` yang sama (proses terpisah, jadi port berbeda) atau cukup API — baca docs/14 §5 dulu, lalu usulkan.

## 10. Langkah berikutnya (chat baru): T5d, T5e, T5f, lalu ritual akhir
**Baca:** 14 §2–§6 (khusus `/metrics`, metrik wajib, OTel) · 10 §8 (config) · docs/21 bagian M02 (T5, tes wajib, exit) · lalu **jalankan `make verify` penuh dan tempel hasilnya sebelum membangun apa pun**.
- **T5d `pkg/telemetry`:** `/metrics` Prometheus di port `METRICS_PORT` (0 = nonaktif; tambahkan ke `pkg/config`, `.env.example`, tes `TestEnvExample…`), metrik HTTP per **pola rute** (bukan path mentah) dan status, gauge outbox dari `kernel.ReadOutboxStats`, statistik pool pgx; hook OTel no-op bila `OTEL_EXPORTER_OTLP_ENDPOINT` kosong.
  Dependensi baru mengubah `go.mod`/`go.sum`: beri perintah untuk dijalankan di WSL, mis. `go get github.com/prometheus/client_golang@latest go.opentelemetry.io/otel@latest go.opentelemetry.io/otel/sdk@latest go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@latest && go mod tidy` — jangan beri patch `go.sum`.
- **T5e sqlc + swag:** `sqlc.yaml` + satu query contoh terhadap skema nyata, anotasi swag (mulai dari handler echo), `make generate`, dan pemeriksaan "tanpa diff" di `verify`/CI. `sqlc`/`swag` sudah ada sebagai `tool` di `go.mod`.
- **T5f uji e2e `RunAPI`:** di paket `infra_test` (PostgreSQL + Redis nyata via testkit): jalankan `app.RunAPI` dengan `infra.Deps{}` di development, `POST` echo dengan `X-Request-ID`, lalu pastikan satu baris `activities` dengan `request_id` yang sama dan baris log akses berisi `request_id` itu. Menutup celah §3.
- **Tes wajib exit M02 yang tersisa:** `request_id` HTTP → log **dan** activity lewat `RunAPI` nyata (T5f); metrik outbox di `/metrics` (T5d).
- **Ritual akhir milestone:** `make verify` hijau, doc-sync (§4), README/CHANGELOG, `HANDOVER-M02.md`, `STATUS.md`, merge, tag `m02-done`, zip repo terbaru (`make repo-zip`) + perintah git. ADR berikutnya mulai dari 0017.

## 11. Prompt untuk chat berikutnya
Gunakan prompt P0 yang sama (docs/23) dengan zip repo terbaru (`make repo-zip` setelah semua commit, termasuk commit handover ini) dan ganti baris konteks tambahan menjadi: "LANJUTAN M02 — Kernel (parsial 3 selesai: T6, T1, T2, T3, T4, T5a, T5b, T5c; tersisa T5d, T5e, T5f lalu ritual akhir).
Repo: https://github.com/master-abror/zago-core, modul Go `github.com/master-abror/zago-core`. Branch kerja `milestone/m02-kernel` (basis tag `m01-done`), lingkungan WSL2 Ubuntu + Docker Desktop, repo di `~/projects/zago-core`. Handover terbaru: `docs/handover/HANDOVER-M02-partial-3.md`.
Terima perubahan sebagai patch `git apply` terhadap commit terakhir yang kusebut atau berkas lengkap; jangan beri overlay zip yang menimpa go.mod/go.sum. Tandai mana yang terbukti di sandbox dan mana yang hanya lewat `make verify`/CI; tes yang butuh PostgreSQL/Redis memakai testcontainers, tanpa mock database.
Stack Docker proyek lain (nizza-ai) memegang port 5432/6379/8080; aku hentikan dulu (`docker compose -p nizza-ai stop`) sebelum `make verify`. ADR terakhir 0016, lanjutkan dari 0017. Keputusan T5 sudah disetujui (ADR-0016): jangan tanya ulang gerbang echo, `X-Dev-Actor`, `OUTBOX_*`, atau `METRICS_PORT`."
