# HANDOVER — M02 Kernel (PARSIAL 1, berhenti di ◆CP1)

**Tanggal:** 2026-10-08 · **Status:** IN PROGRESS · **Jenis:** PARSIAL (T6, T1, T2, T3 selesai; T4, T5 belum dimulai)
**Branch:** `milestone/m02-kernel` (basis: tag `m01-done`, commit `ab3a441`) · **Handover sebelumnya:** `HANDOVER-M01.md`
**Commit milestone ini:** `f3d2486` (T6+T1) · `4e85930` (T2) · commit T3 (pesan: `feat(m02): AuditRecorder, penulis system_logs, pkg/redact, hook dead-letter, ADR-0011`) · commit handover ini.

## 1. Ringkasan
Fondasi kernel backend sudah ada dan terbukti di mesin pengembang: pool PostgreSQL per role dengan `statement_timeout` server-side,
`TxManager.WithinTx` (transaksi di `ctx`, bersarang bergabung, rollback-only bila ditandai), outbox transaksional + relay `SKIP LOCKED`
(retry/backoff/dead-letter) yang kini berjalan di worker, `EventBus` final, `AuditRecorder` (aktor/IP/UA/request id/trace id dari `ctx`;
`denied`/`failed` di transaksi terpisah), penulis `system_logs` (best-effort, dibatasi laju), dan daftar redaksi (`pkg/redact`) dengan tes
properti. `internal/testkit` (PostgreSQL 18 + Redis 8 testcontainers + fixture) sudah dipakai semua tes baru.
**Belum ada:** HTTP toolkit (envelope, AppError, mapper, cursor, idempotency, rate limit), wrapper Redis, telemetri, sqlc, swag, endpoint echo.

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T6 `internal/testkit` | DONE | membungkus `testpg` (ADR-0009); dikerjakan lebih dulu karena tes lain bergantung padanya |
| T1 pool + `TxManager` | DONE | pemasangan pool ke `app`/`infra`: lewat `PostgresSpec` (ADR-0010 poin 6) |
| T2 outbox + relay + EventBus | DONE | worker menjalankan relay; ADR-0010 |
| T3 ◆CP1 audit + system log + redaksi | DONE | ADR-0011 |
| T4 HTTP toolkit | TODO | lihat §10 |
| T5 Redis wrapper, `pkg/telemetry`, sqlc contoh, swag, endpoint echo | TODO | + opsi `logger.SystemLog` yang ditunda (ADR-0011 poin 7) |

## 3. Bukti verifikasi (apa adanya)
**Terbukti** di mesin pengembang (WSL2, Docker Desktop, testcontainers PostgreSQL 18 dan Redis 8), 2026-10-08, dengan `-race`:
- Baseline `m01-done`: lint, test, roundtrip, build, `M00 smoke OK` (stack Nizza AI dihentikan lebih dulu agar port bebas).
- Setelah patch T6+T1: `gofmt` bersih, `go vet`, tes `testkit`/`kernel`/`config` lulus, `golangci-lint` 0 issues, `lint-structure OK`.
- Setelah patch T2: `go vet`, tes `kernel` (±70 dtk), `app`, `infra` (±64 dtk, termasuk worker end-to-end dengan relay), `pkg/*` lulus; lint 0 issues.
- Setelah patch T3: `go vet`; `golangci-lint` 0 issues; `lint-structure OK`; tes lengkap `pkg/redact` (seed contoh 1791421673688897491), `internal/systemlog`,
  `internal/audit` lulus; tes `kernel` (yang disaring dengan `-run`) lulus dan `app` tidak punya tes yang cocok dengan filter itu.

**Belum terbukti:**
- `make verify` penuh SETELAH perubahan M02 (khususnya `verify-smoke`: worker di Docker kini menjalankan relay dan membuka dua pool dengan konfigurasi baru).
- `go test ./...` seluruh repo dalam satu run setelah patch T3 (paket yang tidak disentuh tidak berubah; tes frontend tidak berubah).
- Sandbox pengembangan tidak punya Go/Docker: semua kode ditulis tanpa kompilasi di sandbox; bukti kompilasi hanya dari mesin pengembang di atas.

## 4. Keputusan & penyimpangan
- **ADR-0009** — `testkit` membungkus `testpg`; `WithinTx` bersarang bergabung dan rollback-only bila fungsi dalam gagal; rollback tahan pembatalan ctx; konfigurasi pool (`DB_MAX_CONNS`, `DB_MAINTENANCE_MAX_CONNS`, `DB_STATEMENT_TIMEOUT`).
- **ADR-0010** — relay: satu transaksi per event (bukan per batch seperti docs/10 §6.2), kegagalan dicatat di transaksi kedua, event tanpa handler → `processed`;
  dua registri handler (`Bus` in-process di API, `Relay` di worker); tipe event/`TxManager` dipindah ke `module-sdk` sejak M02; `PostgresSpec` + `Resource.Pool`; `trace_id` di outbox.
- **ADR-0011** — audit: `success` fail-closed lewat `kernel.FailTx`; `denied`/`failed` satu statement ke pool (tak menggagalkan transaksi utama); aktor/scope/klien lewat accessor `ctx`;
  redaksi di `pkg/redact` (bias menyamarkan berlebihan); `systemlog` sinkron dengan batas laju; `logger.SystemLog` ditunda ke T5.
- **Doc-sync yang tertunda (lakukan di ritual akhir M02):** docs/10 §4 (tipe `Event`/`TxManager`/`ActivityEntry` kini ada di `module-sdk` sejak M02), docs/10 §6.2 (transaksi per event, event tanpa handler → processed),
  docs/13 §2.2 (kegagalan menulis `denied`/`failed` dilaporkan, tidak membatalkan transaksi).

## 5. Delta kontrak
- **DB:** tidak ada migrasi baru.
- **Config/env (semua punya default; `.env.example` tidak berubah):** `DB_MAX_CONNS` (10, 1–200), `DB_MAINTENANCE_MAX_CONNS` (2, 1–50), `DB_STATEMENT_TIMEOUT` (15s, 100ms–10m).
- **Event:** nama event harus `^[a-z][a-z0-9_.]*` dengan ≥2 segmen titik, ≤200 karakter; payload objek JSON. `system_logs.event_code` baru: `outbox.event_dead`, `audit.write_failed`.
- **Go (publik antar-paket):** `kernel.{NewPool,PoolConfig,TxManager,DBFrom,InTx,TxFromContext,DetachTx,FailTx,AfterCommit,ErrTxRollbackOnly,Pool,DBTX,Bus,NewBus,Relay,NewRelay,RelayConfig,ReadOutboxStats,NullUUID,NullString,
  With/FromContext(Actor|Scope|Client|RequestID|TraceID)}`; `app.PostgresSpec`; `app.Dependencies.Postgres(ctx, spec)` (berubah dari `(ctx, name, url)`); `app.Resource.Pool`.
- **`module-sdk` baru:** `Event`, `NewEvent`, `EventHandler`, `EventBus`, `TxManager`, `ActivityEntry`, `AuditRecorder`, konstanta `Result*`.
- **Dependensi Go baru:** tidak ada (`go.mod`/`go.sum` tidak berubah).

## 6. Peta kode
| Hal | Lokasi |
|---|---|
| Pool, `WithinTx`, `DBFrom`, `FailTx`, `AfterCommit` | `backend/internal/kernel/{pool,tx}.go` |
| Accessor context (request/trace id, aktor, scope, klien) | `backend/internal/kernel/context.go` (request/trace id disimpan oleh `pkg/logger`) |
| Outbox `Bus` (Publish/Subscribe in-process) | `backend/internal/kernel/outbox.go` |
| Relay + statistik antrian | `backend/internal/kernel/relay.go` |
| `AuditRecorder` | `backend/internal/audit/audit.go` |
| Penulis `system_logs` + hook dead-letter | `backend/internal/systemlog/systemlog.go` |
| Redaksi | `backend/pkg/redact/redact.go` |
| Tipe kontrak modul | `packages/module-sdk/{event,audit}.go` |
| Worker menjalankan relay | `backend/internal/app/app.go` (`RunWorker`) |
| Infra tes | `backend/internal/testkit/{testkit,redis,fixtures}.go` (+ `testpg` untuk PostgreSQL) |

## 7. Cara menjalankan & memverifikasi
```bash
cd ~/projects/zago-core && git switch milestone/m02-kernel
# hentikan stack proyek lain (port 5432 6379 8080 5173 1025 8025 harus bebas)
make verify                                   # lint → test → migrate-roundtrip → build → smoke
REQUIRE_DOCKER=1 go test ./backend/... -race -count=1 2>&1 | tail -n 60   # semua tes Go
go tool golangci-lint run ./... && bash scripts/lint-structure.sh
```

## 8. Masalah diketahui / utang teknis
- Tiap paket tes yang memakai PostgreSQL memulai container sendiri (±30–60 dtk per paket). Bila suite membesar, pertimbangkan berbagi lewat satu container per run.
- `audit`: penulisan `denied`/`failed` butuh satu koneksi pool tambahan selagi transaksi utama terbuka; pool berukuran 1 akan menunggu sampai batas 5 dtk. Default pool 10.
- `systemlog.Log` sinkron: satu tulis bisa menahan ≤2 dtk saat database bermasalah, sebanyak batas laju (5/menit/kode). Ganti ke antrean asinkron hanya bila terukur bermasalah.
- `statement_timeout` 15 dtk berlaku juga untuk pool `app_maintenance`; job retensi (batch 5.000 baris) harus muat, atau beri pool maintenance nilai sendiri saat M06.
- `redact` menyamarkan berlebihan secara sengaja (mis. kunci `token_count`).
- Relay: polling 1 dtk, parameter belum bisa diubah lewat env (default di `RelayConfig`). Gauge metrik dari `ReadOutboxStats` belum diekspor (T5).
- Relay tidak punya pemicu Redis (pub/sub) — hanya polling.
- `audit.Recorder` dan `systemlog.Writer` belum dipasang ke API (menunggu middleware autentikasi M03 dan toolkit T4).
- `testkit.Redis` memakai `FLUSHALL` per klien: tes yang memakainya tidak boleh `t.Parallel`.
- Utang lintas milestone dari M01 tetap berlaku (modul mencabut DML `schema_migrations_<kode>` — M07; group diarsipkan mencabut `role_assignments` — M05; `platforms.version` — M15).

## 9. Pertanyaan terbuka
- **Sumber rahasia HMAC cursor (I-06):** turunkan dari `SESSION_SECRET` (mis. HMAC dengan label tetap, tanpa env baru) atau env baru `CURSOR_SECRET`? Putuskan di T4 dan tulis ADR.
- Apakah parameter relay (`OUTBOX_*`) perlu diekspos lewat env sekarang atau menunggu kebutuhan nyata.
- Lisensi proyek (belum ditetapkan, sejak M00).

## 10. Langkah berikutnya (chat baru): T4 lalu T5
**Baca:** 08 §2–§7, §11 · 10 §9, §10, §13 · 14 §2–§6 (ringkas) · `E:I-06, I-07, I-08` · lalu docs/21 bagian M02 (T4, T5, tes wajib, exit).
- **T4 (HTTP toolkit)** di `backend/internal/kernel` (atau subpaket `kernel/httpx` bila terlalu besar): envelope `{data,meta}`/`{error}`; `AppError` + registry kode (08 §11 + I-08);
  mapper error (tak dikenal → `internal_error` tanpa teks asli); validasi → `validation_failed`; paginasi keyset + cursor HMAC (cursor diubah → 400); middleware Idempotency (Redis; key sama+body sama → respons sama,
  body beda → `idempotency_key_conflict`, paralel → `idempotency_in_progress`); rate limiter Redis + header `X-RateLimit-*`; accessor `ctx`.
- **Catatan integrasi T4:** `internal/httpserver` sudah punya `RequestID` (memakai `logger.WithRequestID`, sama dengan `kernel.WithRequestID`), `RealIP`, `AccessLog`, `Recoverer`, `SecurityHeaders`, dan `writeError` sederhana — ganti `writeError`
  dengan toolkit dan **isi `kernel.WithClient`** (IP dari RealIP yang tepercaya + User-Agent) di middleware. Periksa `scripts/smoke/m00.sh` dan tes `httpserver` sebelum mengubah bentuk body 404/405 agar tidak merusak smoke M00.
- **T5:** wrapper Redis (klien go-redis tunggal dipakai idempotency + rate limit; `testkit.Redis` untuk tes); `pkg/telemetry` (`/metrics` Prometheus: gauge outbox dari `ReadOutboxStats`, pool DB; hook OTel no-op tanpa endpoint);
  contoh sqlc + swag terpasang (`make generate`); opsi `logger.SystemLog("kode")` + pemasangan `systemlog.Writer` ke logger (butuh urutan: logger dibuat sebelum pool — gunakan "gate" yang di-attach setelah pool siap);
  pasang `audit.Recorder`/`systemlog.Writer` ke komposisi API; endpoint dev/test `/api/v1/_kernel/echo` (envelope, idempotency, rate limit, cursor).
  Dependensi baru (Prometheus/OTel/sqlc/swag) mengubah `go.mod`/`go.sum`: berikan perintah `go get`/`go mod tidy` untuk dijalankan di WSL, bukan patch `go.sum`.
- **Tes wajib sisa (docs/21 M02):** cursor diubah → 400; idempotency (3 skenario); error tak dikenal tidak membocorkan teks asli; `request_id` mengalir ke log dan activity (bagian activity sudah terbukti di `audit`).
- **Ritual akhir milestone:** `make verify` hijau, doc-sync (§4), README/CHANGELOG, `HANDOVER-M02.md`, `STATUS.md`, merge, tag `m02-done`, zip repo terbaru (`make repo-zip`) + perintah git.

## 11. Prompt untuk chat berikutnya
Gunakan prompt P0 yang sama (docs/23) dengan zip repo terbaru (`make repo-zip` setelah semua commit), dan ganti baris konteks tambahan menjadi: "LANJUTAN M02 — Kernel (parsial 1 selesai: T6, T1, T2, T3; berikutnya T4 lalu T5).
Repo: https://github.com/master-abror/zago-core, modul Go `github.com/master-abror/zago-core`. Branch kerja `milestone/m02-kernel` (basis tag `m01-done`), lingkungan WSL2 Ubuntu + Docker Desktop, repo di `~/projects/zago-core`.
Terima perubahan sebagai patch `git apply` terhadap commit terakhir yang kusebut atau berkas lengkap; jangan beri overlay zip yang menimpa go.mod/go.sum. Tandai mana yang terbukti di sandbox dan mana yang hanya lewat `make verify`/CI;
tes yang butuh PostgreSQL/Redis memakai testcontainers, tanpa mock database. Stack Docker proyek lain memegang port 5432/6379/8080; aku hentikan dulu sebelum `make verify`."
