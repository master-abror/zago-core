# HANDOVER — M02 Kernel (PARSIAL 2, berhenti di ◆CP2)

**Tanggal:** 2026-10-10 · **Status:** IN PROGRESS · **Jenis:** PARSIAL (T6, T1, T2, T3, T4 selesai; hanya T5 tersisa)
**Branch:** `milestone/m02-kernel` (basis: tag `m01-done`, commit `ab3a441`) · **Handover sebelumnya:** `HANDOVER-M02-partial-1.md`
**Commit sesi ini (urut):** `ae6500e` (T4a httpx + ClientContext + ADR-0012) · `85f0294` (T4b cursor + ADR-0013) · `c2b4716` (T4c idempotency + rate limit + ADR-0014) · commit handover ini.

## 1. Ringkasan
HTTP toolkit kernel lengkap dan terbukti di mesin pengembang: registry kode error + `AppError`, envelope `{data,meta}`/`{error}`, mapper error
(tak dikenal → `internal_error` tanpa teks asli), `DecodeJSON`, validasi per field, paginasi keyset dengan cursor HMAC, middleware idempotency
(Redis), rate limiter (Redis, fallback memori), dan `httpserver.ClientContext` (isi `kernel.WithClient`). `Recoverer` serta 404/405 kini memakai toolkit.
**Belum ada:** wrapper Redis tunggal di composition root, `pkg/telemetry`, sqlc contoh, swag, opsi `logger.SystemLog`, pemasangan audit/systemlog ke API,
endpoint `/api/v1/_kernel/echo` — semuanya T5. Toolkit belum dipasang ke rute mana pun selain `Recoverer`/404/405.

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T6 testkit · T1 pool/TxManager · T2 outbox/relay/EventBus · T3 audit/system log/redaksi | DONE | lihat parsial 1 |
| T4 HTTP toolkit | DONE | `backend/internal/kernel/httpx` (+ `httpserver.ClientContext`); ADR-0012, 0013, 0014 |
| T5 wrapper Redis, `pkg/telemetry`, sqlc, swag, `logger.SystemLog`, pasang audit/systemlog, echo | TODO | lihat §10 |

## 3. Bukti verifikasi (apa adanya)
**Terbukti** di mesin pengembang (WSL2, Docker Desktop, testcontainers PostgreSQL 18 + Redis 8), 2026-10-10, dengan `-race` bila tes Go:
- Baseline `f1f7ae0` (sebelum T4): `make verify` penuh — lint, test, migrate-roundtrip, build — hijau; `verify-smoke` hijau setelah stack Nizza AI dihentikan (`M00 smoke OK`; api, worker dengan relay, web sehat).
- T4a: `gofmt`, `go vet`, tes `kernel/httpx` + `httpserver` lulus; T4b: tes `kernel/httpx` lulus; T4c: `REQUIRE_DOCKER=1 go test -race ./backend/internal/kernel/httpx/...` lulus (±10 dtk, Redis 8 asli).
- `make lint` hijau (golangci-lint 0 issues, svelte-check, prettier, `lint-structure OK`) pada tiga commit di atas, setelah dua perbaikan lint kecil (UUID di tes internal; ST1023).

**Belum terbukti:**
- **`make verify` penuh SETELAH T4**, khususnya `verify-smoke`: `scripts/smoke/m00.sh` diubah (404 kini `resource_not_found` + `request_id`) dan `ClientContext`/`Recoverer`/404/405 berubah, tetapi smoke belum dijalankan lagi.
- `go test ./...` seluruh repo dalam satu run setelah T4.
- Idempotency/rate limit belum diuji melalui rute HTTP sungguhan (hanya lewat handler tes); itu tugas endpoint echo di T5.
- Catatan sandbox sesi ini: Go 1.24 (`apt-get update && apt-get install golang-1.24-go`, jalur `/usr/lib/go-1.24/bin`) dan `redis-server` 7.0 dari apt tersedia; modul memerlukan 1.26.5 sehingga kode diuji di modul scratch
  (`GOPROXY=direct`, host vanity diblokir → klon GitHub + `replace` untuk `go.uber.org/atomic`, `golang.org/x/sys`, go-redis; pgx butuh Go ≥ 1.25 jadi `kernel` asli tidak dikompilasi di sandbox).
  Bukti kompilasi dengan toolchain proyek tetap hanya dari mesin pengembang.

## 4. Keputusan & penyimpangan
- **ADR-0012** — `kernel/httpx` stdlib-only (+`pkg/logger`); rute tak dikenal → `resource_not_found`; `method_not_allowed` ditambahkan ke registry; registry terdesentralisasi (`Register`); `DecodeJSON` (415/413/unknown field); `ClientContext` (UA ≤ 512 byte).
- **ADR-0013** — kunci cursor = `HMAC-SHA256(SESSION_SECRET, "zago/cursor/v1")` (keputusan pemilik, tanpa env baru); token `base64url(payload).base64url(mac)` mode ketat; `invalid_cursor` vs `cursor_mismatch`; `limit` bawaan 50, maks 100, **dipotong** bila lebih.
- **ADR-0014** — idempotency: hanya 2xx disimpan, kunci dilepas pada non-2xx/panic/respons > 1 MiB, token pemilik + Lua CAS, 422 diperiksa sebelum 409, header replay hanya `Content-Type`/`Location`, fail closed 503;
  rate limit: fixed window Lua, `X-RateLimit-Reset` = detik epoch (dibulatkan ke atas), fallback memori Limit/2 (maks 10.000 identitas, penuh → tolak identitas baru) untuk aturan `Sensitive`, aturan biasa fail open.
- **Doc-sync yang tertunda (lakukan di ritual akhir M02):** dari parsial 1 — docs/10 §4, docs/10 §6.2, docs/13 §2.2. Baru — docs/08 §11 (tambah `method_not_allowed` 405; rute tak dikenal = `resource_not_found`), docs/08 §4 (`limit` dipotong ke maksimum; kode detail `invalid_cursor`/`cursor_mismatch`; kunci cursor turunan `SESSION_SECRET`),
  docs/08 §6 (hanya 2xx disimpan; kunci dilepas pada kegagalan; batas respons 1 MiB; header yang diputar ulang), docs/08 §7 (semantik `X-RateLimit-Reset`, fallback Limit/2), docs/10 §3 (lokasi `kernel/httpx`).

## 5. Delta kontrak
- **DB:** tidak ada migrasi baru. **Config/env:** tidak ada yang baru (`.env.example` tidak berubah). **Dependensi Go baru:** tidak ada (go-redis sudah ada; `go.mod`/`go.sum` tidak berubah).
- **Perilaku HTTP yang berubah:** body 404 dari `{"error":{"code":"not_found"}}` menjadi envelope penuh `resource_not_found` dengan `message` dan `request_id`; 405 `method_not_allowed` kini juga membawa `message`/`request_id`.
- **Go (publik antar-paket), paket `internal/kernel/httpx`:** `Code`, `Register`, `Lookup`, `Codes`, `Error` (+`WithMessage/WithDetails/WithRetryAfter`, `HasCode`), kode kernel (`ValidationFailed`, `ResourceNotFound`, `MethodNotAllowed`, `PermissionDenied`, `AuthenticationRequired`, `VersionConflict`,
  `IdempotencyInProgress`, `IdempotencyKeyConflict`, `PayloadTooLarge`, `UnsupportedMediaType`, `RateLimitExceeded`, `InternalError`, `ServiceUnavailable`), `Responder` (`NewResponder`, `OK/Created/JSON/NoContent/Error`), `Meta`, `Items`,
  `Fields`/`FieldError`/`Validation`, `DecodeJSON`, `Direction` (`Desc`/`Asc`), `Position`, `CursorCodec` (`NewCursorCodec`, `Encode`, `Decode`), `FilterHash`, `PageOptions`/`PageRequest`/`ParsePage`/`Paginate`,
  `Idempotency` (`NewIdempotency`, `IdempotencyOptions`, `Require`), `RateLimiter` (`NewRateLimiter`, `Check`, `Middleware`), `Rule`, `KeyFunc`, `RateResult`. Paket `internal/httpserver`: `ClientContext`.
- **Redis key baru:** `idem:{subject}:{METHOD}:{route}:{key}` (24 j), `rl:{rule}:{identity}` (satu jendela).

## 6. Peta kode (tambahan sejak parsial 1)
| Hal | Lokasi |
|---|---|
| Registry kode, `Error`, `HasCode` | `backend/internal/kernel/httpx/errors.go` |
| Responder + mapper error + envelope | `.../httpx/respond.go` · validasi `.../validation.go` · `DecodeJSON` `.../decode.go` |
| Cursor, `FilterHash`, `ParsePage`, `Paginate` | `.../httpx/cursor.go` |
| Idempotency | `.../httpx/idempotency.go` |
| Rate limiter + fallback memori | `.../httpx/ratelimit.go` |
| Tes (Redis lewat `testkit`; `TestMain` di `main_test.go`) | `.../httpx/*_test.go` |
| `ClientContext`, `Recoverer`/404/405 via toolkit | `backend/internal/httpserver/{middleware,router}.go` |
| ADR | `docs/adr/0012…0014` |

## 7. Cara menjalankan & memverifikasi
```bash
cd ~/projects/zago-core && git switch milestone/m02-kernel
# hentikan stack proyek lain: docker compose -p nizza-ai stop   (port 5432 6379 8080 5173 1025 8025 harus bebas)
make verify                                   # WAJIB dijalankan dulu di sesi berikutnya (lihat §3 "Belum terbukti")
REQUIRE_DOCKER=1 go test ./backend/... -race -count=1 2>&1 | tail -n 60
go tool golangci-lint run ./... && bash scripts/lint-structure.sh
```

## 8. Masalah diketahui / utang teknis
- Semua utang di parsial 1 §8 tetap berlaku.
- Rate limiter memakai fixed window: hingga 2× limit dapat lolos di batas dua jendela; pembatas per-akun untuk login (M03) menutupnya. Sliding window dapat menggantikan tanpa mengubah antarmuka.
- Idempotency tidak menyimpan respons > 1 MiB (kunci dilepas → eksekusi ulang bila diulang): endpoint `Idempotent()` harus mengembalikan body kecil.
- Cursor tidak punya kedaluwarsa; memutar `SESSION_SECRET` membatalkan semua cursor (diterima, ADR-0013).
- Tes Redis memakai `FLUSHALL` per klien: tidak boleh `t.Parallel`. Satu container Redis per paket tes.
- `httpx` tidak memeriksa panjang `Subject`/`Route` sebelum membentuk kunci Redis; pemanggil menjamin keduanya terbatas (UUID dan pola rute).

## 9. Pertanyaan terbuka
- Gerbang endpoint `/api/v1/_kernel/echo` ("khusus dev/test"): usulan — hanya terdaftar bila `APP_ENV != production`; konfirmasi atau pilih env eksplisit.
- Apakah parameter relay (`OUTBOX_*`) diekspos lewat env sekarang atau menunggu kebutuhan nyata.
- Lisensi proyek (belum ditetapkan, sejak M00).

## 10. Langkah berikutnya (chat baru): T5, lalu ritual akhir
**Baca:** 14 §2–§6 (ringkas) · 10 §6, §8, §10, §13 · 08 §2.2, §6–§7 (rujukan toolkit) · lalu docs/21 bagian M02 (T5, tes wajib, exit). Lalu **jalankan `make verify` penuh dan tempel hasilnya sebelum membangun apa pun**.
- **T5 (urutan usulan):** (a) composition root: satu klien go-redis (lewat `app.Dependencies`/`infra`), `Responder` tunggal, `CursorCodec` dari `cfg.SessionSecret`, `Idempotency` dan `RateLimiter`;
  (b) endpoint dev/test `/api/v1/_kernel/echo` yang memperagakan envelope, idempotency (`Subject` dari `kernel.ActorFromContext`; di M02 belum ada autentikasi → gunakan penyuntikan aktor khusus dev/test),
  rate limit (`KeyFunc` = `"ip:"+ClientIP`), cursor (data sintetis) — dengan tes HTTP end-to-end melalui `httpserver.NewHandler`; (c) `pkg/telemetry` (`/metrics` Prometheus: gauge outbox dari `ReadOutboxStats`, pool DB; hook OTel no-op tanpa endpoint);
  (d) sqlc contoh + swag terpasang (`make generate`); (e) opsi `logger.SystemLog("kode")` + pasang `systemlog.Writer`/`audit.Recorder` ke komposisi API (urutan: logger dibuat sebelum pool → "gate" yang di-attach setelah pool siap).
  Dependensi baru (Prometheus/OTel/sqlc/swag) mengubah `go.mod`/`go.sum`: berikan perintah `go get`/`go mod tidy` untuk dijalankan di WSL, bukan patch `go.sum`.
- **Tes wajib yang tersisa untuk exit M02:** `request_id` mengalir dari request HTTP ke log **dan** activity (bagian activity terbukti di `audit`; ujung-ke-ujung lewat echo di T5); idempotency/rate limit/cursor terbukti lewat rute HTTP.
- **Ritual akhir milestone:** `make verify` hijau, doc-sync (§4), README/CHANGELOG, `HANDOVER-M02.md`, `STATUS.md`, merge, tag `m02-done`, zip repo terbaru (`make repo-zip`) + perintah git.

## 11. Prompt untuk chat berikutnya
Gunakan prompt P0 yang sama (docs/23) dengan zip repo terbaru (`make repo-zip` setelah semua commit), dan ganti baris konteks tambahan menjadi: "LANJUTAN M02 — Kernel (parsial 2 selesai: T6, T1, T2, T3, T4; tersisa T5 lalu ritual akhir).
Repo: https://github.com/master-abror/zago-core, modul Go `github.com/master-abror/zago-core`. Branch kerja `milestone/m02-kernel` (basis tag `m01-done`), lingkungan WSL2 Ubuntu + Docker Desktop, repo di `~/projects/zago-core`.
Terima perubahan sebagai patch `git apply` terhadap commit terakhir yang kusebut atau berkas lengkap; jangan beri overlay zip yang menimpa go.mod/go.sum. Tandai mana yang terbukti di sandbox dan mana yang hanya lewat `make verify`/CI;
tes yang butuh PostgreSQL/Redis memakai testcontainers, tanpa mock database. Stack Docker proyek lain (nizza-ai) memegang port 5432/6379/8080; aku hentikan dulu (`docker compose -p nizza-ai stop`) sebelum `make verify`. ADR terakhir 0014, lanjutkan dari 0015.\"
