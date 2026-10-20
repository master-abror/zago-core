# HANDOVER — M02 Kernel

**Tanggal:** 2026-10-18 · **Tag:** `m02-done` · **Commit terakhir:** lihat `git log -1 m02-done` (handover ini ikut di commit itu)
**Jenis:** LENGKAP (menggantikan `HANDOVER-M02-partial-1/2/3.md`, yang tetap disimpan sebagai riwayat; rincian API publik per paket ada di sana)
**Branch:** `milestone/m02-kernel` (basis tag `m01-done`) · **Handover sebelumnya:** `HANDOVER-M01.md`

## 1. Ringkasan
Backend kini punya kernel yang terbukti ujung-ke-ujung terhadap PostgreSQL 18 dan Redis 8 sungguhan: pool per role dengan `statement_timeout`, `TxManager.WithinTx`
(bersarang bergabung, rollback-only), outbox transaksional + relay `SKIP LOCKED` di worker, `EventBus`, `AuditRecorder` (aktor/IP/UA/request id/trace id dari `ctx`),
penulis `system_logs` lewat gate logger, toolkit HTTP (`kernel/httpx`: envelope, registry kode error, validasi, paginasi keyset dengan cursor HMAC, idempotency,
rate limit), composition root (`app.RunAPI`/`RunWorker`) dengan satu klien Redis, dan endpoint dev/test `/api/v1/_kernel/echo` yang memperagakan semuanya.
Observabilitas: `/metrics` Prometheus di port terpisah per proses, hook OpenTelemetry (no-op tanpa endpoint). Toolchain: sqlc (query pertama di `audit/repository`),
swag (`docs/api/swagger.json`), `make generate-check` di `make verify`, dan `scripts/smoke/m02.sh`.

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T6 `internal/testkit` | DONE | membungkus `testpg`; ADR-0009 |
| T1 pool + `TxManager` | DONE | ADR-0009 |
| T2 outbox + relay + `EventBus` | DONE | ADR-0010 |
| T3 ◆CP1 audit + system log + redaksi | DONE | ADR-0011 |
| T4 HTTP toolkit (T4a–c) | DONE | ADR-0012, 0013, 0014 |
| T5a–c klien Redis, `KernelServices`, `logger.SystemLog` + gate, audit di API, echo | DONE | ADR-0015, 0016 |
| T5d `pkg/telemetry` (`/metrics`, hook OTel) | DONE | ADR-0017 |
| T5e sqlc contoh + swag + `make generate` + `generate-check` | DONE | ADR-0018 |
| T5f uji e2e `RunAPI`/`RunWorker` + `infra.Deps` | DONE | `infra/api_e2e_test.go` |
| `scripts/smoke/m02.sh` (syarat exit docs/21) | DONE | ditambahkan saat penutupan |
| Panggilan `worker_job_*` dari job worker | DEFERRED | belum ada job selain relay; M08 |
| Span OTel per lapisan + `trace_id` ke log/outbox | DEFERRED | datang bersama lapisan yang membuatnya (ADR-0017 poin 5) |
| Generator SDK TypeScript | DEFERRED | `packages/ts-sdk` masih stub; M10 (I-14) |

## 3. Bukti verifikasi (apa adanya)
Mesin pengembang (WSL2 + Docker Desktop, testcontainers PostgreSQL 18 + Redis 8, stack proyek lain dihentikan lebih dulu):
- **`make verify` penuh hijau, 2026-10-18** (`verify exit=0`): `lint-structure OK`, `generate-check OK`, 17 paket Go bertes `ok` dengan `-race`
  (`app`, `audit`, `audit/repository`, `health`, `httpserver`, `infra`, `kernel`, `kernel/httpx`, `systemlog`, `testkit`, `testpg`, `migrations`, `pkg/{config,id,logger,redact,telemetry}`),
  migrate-roundtrip, build, lalu smoke pada stack compose sementara: `M00 smoke OK` dan `M02 smoke OK` (terhadap API asli, bukan tiruan).
- Isi repo pada `cd84862` dibuktikan identik dengan state yang lolos `make verify` hijau sebelumnya lewat perbandingan hash subpohon (`backend`, `docs`, `scripts`, `packages`, `Makefile`, `sqlc.yaml`, `.env.example`).
- Uji ujung-ke-ujung dengan PostgreSQL + Redis nyata: `TestAPIEndToEndThroughEchoWithRealPostgresRedisAndMetrics`, `TestWorkerExposesPoolAndDeadLetterMetricsWithRealPostgres` (T5f); `httpserver/echo_audit_test.go` (activity + `request_id`).
- Terbukti di sandbox saja (tidak ada Docker/PostgreSQL): logika `scripts/smoke/m02.sh` dan `generate-check.sh` terhadap server/proyek tiruan; keluaran sqlc/swag identik byte-per-byte dengan mesin pengembang (hash dicocokkan).
- **Tes negatif/wajib docs/21:** cursor diubah → 400 `invalid_cursor` (tes `httpx` + smoke); idempotency tiga skenario (replay, badan beda → 422, paralel → 409; `httpx/*_test.go`); error tak dikenal tidak membocorkan teks asli (`httpx/respond_test.go`);
  `request_id` mengalir ke log dan activity (`echo_audit_test.go`, `api_e2e_test.go`); aturan batas di `httpx/ratelimit_test.go` dan `ratelimit_timeout_test.go`.
- **Tidak terbukti:** bahwa flake yang dijelaskan di §8 sudah hilang. Satu run hijau bukan bukti ketahanan.

## 4. Keputusan & penyimpangan
- ADR-0009 `testkit` membungkus `testpg`, transaksi bersarang, konfigurasi pool · ADR-0010 relay outbox (satu transaksi per event) · ADR-0011 audit, system log, redaksi · ADR-0012 registry error `httpx` · ADR-0013 cursor + pagination (kunci turunan `SESSION_SECRET`)
  · ADR-0014 idempotency + rate limit · ADR-0015 `logger.SystemLog` + gate · ADR-0016 composition root, `KernelServices`, echo, `/metrics` terpisah · ADR-0017 ketahanan tes + port `/metrics` + hook tracing + Ryuk
  · ADR-0018 sqlc, swag, `generate-check` · ADR-0019 klien Redis menghormati deadline context. **ADR terakhir 0019; lanjutkan dari 0020.**
- Penyimpangan dari dokumen: docs/10 §6.2 (transaksi per event, bukan per batch), docs/14 §3 (`logger.SystemLog` = atribut slog), docs/08 §9 (`docs/api/swagger.json`, bukan `docs/swagger.json`), docs/16 (`test-backend -p`, `generate-check`).
- **Doc-sync diterapkan (tidak ada yang tertunda):** 08 §4/§6/§7/§9/§11 · 10 §3/§4/§6.2/§7/§8 · 13 §2.2 · 14 §3/§5/§6 · 16 · 18 §8 · 19 §3 · README.

## 5. Delta kontrak
- **DB:** tidak ada migrasi baru.
- **API:** hanya dev/test (`ENVIRONMENT` ∈ {development, test}, ada Redis): `POST /api/v1/_kernel/echo` (butuh `Idempotency-Key` dan `X-Dev-Actor`; merekam activity `kernel.echo`), `GET /api/v1/_kernel/echo/items` (cursor; `limit` bawaan 10, maks 20; `?q=`);
  rate limit 30/menit per IP. Dokumen publik `docs/api/swagger.json` memuat endpoint health (echo dikecualikan, tag `kernel-dev`). Body 404/405 kini envelope penuh (`resource_not_found`, `method_not_allowed` 405).
  Kode error kernel: `validation_failed`, `resource_not_found`, `method_not_allowed`, `permission_denied`, `authentication_required`, `version_conflict`, `idempotency_in_progress`, `idempotency_key_conflict`, `payload_too_large`, `unsupported_media_type`, `rate_limit_exceeded`, `internal_error`, `service_unavailable`.
- **Event:** nama `^[a-z][a-z0-9_.]*`, ≥2 segmen titik, ≤200 karakter, payload objek JSON. Belum ada publisher/subscriber bisnis. `system_logs.event_code` baru: `outbox.event_dead`, `audit.write_failed`.
- **Config/env:** `DB_MAX_CONNS` (10), `DB_MAINTENANCE_MAX_CONNS` (2), `DB_STATEMENT_TIMEOUT` (15s); `METRICS_PORT` (API) dan `WORKER_METRICS_PORT` (worker), bawaan 0 = nonaktif, `.env.example` 9090/9091, tidak di-publish Compose;
  `OTEL_EXPORTER_OTLP_ENDPOINT` kini divalidasi (http/https tanpa kredensial/query/fragment). Variabel Make `GO_TEST_PARALLEL` (3). Tes: `TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT` disetel `3m` oleh `testpg.ConfigureReaper` bila belum ada.
- **Redis key:** `idem:{subject}:{method}:{route}:{key}` (24 jam; `subject` = aktor, di M02 dari `X-Dev-Actor`), `rl:{rule}:{identity}` (satu jendela).
- **Dependensi Go baru:** `prometheus/client_golang`, OpenTelemetry (`otel`, `sdk`, `trace`, `otlptracehttp`); versi persis di `go.mod`/`go.sum`.
- **Make/skrip:** `make generate-check` (di `verify`), `scripts/generate-check.sh`, `scripts/smoke/m02.sh`; `test-backend` memakai `-p $(GO_TEST_PARALLEL)`.

## 6. Peta kode
| Hal | Lokasi |
|---|---|
| Pool, `WithinTx`, `FailTx`, `AfterCommit`, `StatsOf` | `backend/internal/kernel/{pool,tx}.go` |
| Outbox `Bus`, relay, `ReadOutboxStats` | `backend/internal/kernel/{outbox,relay}.go` |
| `AuditRecorder` · penulis `system_logs` · redaksi | `backend/internal/audit/audit.go` · `backend/internal/systemlog/systemlog.go` · `backend/pkg/redact/` |
| Tipe kontrak modul (`Event`, `TxManager`, `ActivityEntry`, `AuditRecorder`) | `packages/module-sdk/{event,audit}.go` |
| HTTP toolkit (error, envelope, cursor, idempotency, rate limit) | `backend/internal/kernel/httpx/` |
| Router, middleware (`Metrics`, `ClientContext`), `KernelServices`, echo | `backend/internal/httpserver/{router,middleware,kernel,echo}.go` |
| Composition root, metrik, tracing, hook dead-letter | `backend/internal/app/{app,metrics,tracing}.go` |
| Pool + klien Redis nyata | `backend/internal/infra/{postgres,redis}.go` |
| Metrik Prometheus, tracing OTel | `backend/pkg/telemetry/` |
| `logger.SystemLog`, `Gate` | `backend/pkg/logger/` |
| sqlc (config, query, kode) | `sqlc.yaml` · `backend/internal/audit/repository/` |
| OpenAPI hasil swag | `docs/api/swagger.json` |
| Infra tes (PostgreSQL, Redis, Ryuk, fixture) | `backend/internal/{testkit,testpg}/` |
| Tes e2e komposisi · smoke | `backend/internal/infra/api_e2e_test.go` · `scripts/smoke/m02.sh` |

## 7. Cara menjalankan & memverifikasi
```bash
git clone https://github.com/master-abror/zago-core.git ~/projects/zago-core && cd ~/projects/zago-core
make setup && make dev            # api + worker + frontend; /metrics di :9090 (API) dan :9091 (worker)
curl -s -X POST localhost:8080/api/v1/_kernel/echo -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-1' -H 'X-Dev-Actor: 0198f3a0-0000-7000-8000-0000000000a2' -d '{"message":"halo"}'
curl -s 'localhost:8080/api/v1/_kernel/echo/items?limit=2'
curl -s localhost:9090/metrics | grep -E '^(http_requests_total|db_pool|outbox_)'
# gerbang penuh (hentikan stack proyek lain dulu: docker compose -p nizza-ai stop)
make verify > ~/verify.log 2>&1; echo $?     # simpan di folder home, bukan /tmp
```

## 8. Masalah diketahui / utang teknis
- **Flake lingkungan (tidak ada yang terbukti hilang):** empat kegagalan acak di `make verify` pada Docker Desktop yang padat — batas tulis `system_logs` 2 dtk (ADR-0017 poin 1), kesiapan Redis lewat Docker API (poin 3),
  batas tunggu Ryuk (poin 6), dan tes rate limit konkuren (ADR-0019; **angka kegagalannya hilang bersama log, jadi penyebabnya tidak terbukti**, hanya konsisten dengan fail-open 1 dtk setelah timeout 250 ms).
  Perbaikan selalu lewat opsi eksplisit yang bawaan produksinya tidak berubah (docs/18 §8). Bila muncul lagi: simpan log lengkap di folder home; opsi berikutnya `GO_TEST_PARALLEL=2`.
- Masih rentan kelas yang sama: helper `start()` di `infra/infra_integration_test.go` memakai `wait.ForListeningPort`; tes echo di `httpserver` memakai limiter dengan batas bawaan 250 ms.
- **Insiden lingkungan:** pada sesi ini berkas cache modul Go berukuran nol (12 Okt) dan indeks modul di build cache basi (`expected 'package', found 'EOF'` padahal `go mod verify` bersih); `go clean -cache` memperbaikinya. `Bus error`/`Input/output error` dari Docker saat `verify-smoke` (12 Okt) hilang setelah Docker dipulihkan.
  Penyebab di lapisan WSL/Windows tidak terbukti. Pemulihan: `go mod verify`; hapus hanya berkas cache berukuran nol; `go clean -cache`; jangan pernah melewati verifikasi `go.sum`.
- `ContextTimeoutEnabled` (ADR-0019): pembatalan context request kini memutus panggilan Redis yang berjalan; penulisan yang wajib tuntas harus memakai `context.WithoutCancel`.
- Observabilitas belum lengkap: `worker_job_*` tanpa pemanggil; belum ada span; `auth_*`, `authz_*`, `db_query_duration_seconds`, `redis_command_duration_seconds`, `websocket_*` menunggu milestone masing-masing. `/metrics` tanpa autentikasi: jangan pernah dipublish.
- Gauge outbox menjalankan satu query hitung di `outbox_events` pada setiap scrape; periksa biaya/indeks saat tabel besar.
- swag menamai definisi dari jalur impor (`backend_internal_health.Report`, generik jauh lebih panjang); putuskan penyajiannya saat generator SDK dipilih (M10). Skema sqlc hanya `backend/migrations`; tambahkan migrasi modul saat M07.
- `ListActivitiesByRequestID` platform-scoped (`organization_id` nullable); pemanggil bertenant WAJIB mengisinya (M06). `generate-check` tidak mendeteksi keluaran yang lupa di-commit (CI yang menangkapnya, docs/19 §3).
- Semua utang di `HANDOVER-M02-partial-1/2/3.md` §8 tetap berlaku (mis. fixed window rate limit, idempotency tidak menyimpan respons > 1 MiB, cursor tanpa kedaluwarsa, `FLUSHALL` per klien tes Redis → tanpa `t.Parallel`, `statement_timeout` juga untuk pool maintenance, `redact` sengaja berlebihan).
- Utang lintas milestone dari M01 tetap: modul mencabut DML `schema_migrations_<kode>` (M07); group diarsipkan mencabut `role_assignments` (M05); `platforms.version` (M15).
- Echo (dataset tetap, aktor dari header) murni dev/test; hapus setelah ada endpoint nyata yang memakai toolkit yang sama.

## 9. Pertanyaan terbuka
- Lisensi proyek (belum ditetapkan, sejak M00).
- Mekanisme checkpoint antar-chat (berkas `CHECKPOINT.md` yang selalu terbarui + `make checkpoint`; menyentuh docs/22 dan 23) sudah diusulkan tetapi **belum diputuskan**; bila disetujui, tulis ADR-0020.

## 10. Milestone berikutnya: M03 Authentication
- **Baca:** 05 (seluruhnya) · 12 §4.1, §4.4 · 08 §10.1 · `E:B-04 (bootstrap), B-07, I-04, I-05` · lalu docs/21 bagian M03.
- **Langkah pertama yang disarankan:** `make verify` hijau dulu; lalu T1 (repo credentials/sessions/security_events + hasher Argon2id). Tes NEGATIF wajib sejak T1 (auth/authz/tenant).
- **Risiko/perhatian khusus:** ganti `X-Dev-Actor` dengan `Identity` dari sesi lewat middleware (aktor echo, `Idempotency.Subject`, `AuditRecorder`); `session:{sha256}` memakai klien Redis yang sama (kini menghormati deadline context); `RateLimiter` aturan `Sensitive` untuk login; 
  M03 pertama yang punya call site produksi `logger.SystemLog`; `go.mod` berubah (Argon2) → beri perintah `go get`, bukan patch `go.sum`.

## 11. Prompt untuk chat berikutnya
Gunakan P2 (docs/23) dengan zip repo terbaru (`make repo-zip` setelah merge dan tag), dan isi:
```text
TUGAS SESI INI: Milestone M03 — Authentication saja.
Bagian yang dibaca: 05 (seluruhnya); 12 §4.1 §4.4; 08 §10.1; E:B-04, B-07, I-04, I-05.
Konteks tambahan:
- Repo: https://github.com/master-abror/zago-core, modul Go github.com/master-abror/zago-core. Branch kerja milestone/m03-authentication (basis tag m02-done).
- Lingkunganku: WSL2 Ubuntu + Docker Desktop, repo di ~/projects/zago-core. Perubahan sebagai patch `git apply` terhadap commit terakhir yang kusebut, atau berkas lengkap;
  jangan overlay zip yang menimpa go.mod/go.sum; dependensi baru → beri perintah `go get`/`go mod tidy` untuk WSL.
- Tandai mana yang terbukti di sandbox dan mana yang hanya lewat `make verify`/tes di mesinku. Tes DB/Redis memakai testkit (testcontainers), tanpa mock.
- Stack nizza-ai memegang port 5432/6379/8080: `docker compose -p nizza-ai stop` sebelum `make verify`. Simpan log verify di folder home, bukan /tmp.
- ADR terakhir 0019; lanjutkan dari 0020. Baca HANDOVER-M02 §8 (flake, insiden cache) dan §9 sebelum menyusun rencana.
- Aku komunikasi singkat dalam bahasa Indonesia; beri berkas lengkap dan perintah yang bisa langsung disalin; verifikasi dan apply dipisah per perintah.
```
