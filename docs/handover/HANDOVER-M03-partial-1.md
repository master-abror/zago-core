# HANDOVER — M03 Authentication (PARSIAL 1, berhenti di ◆CP1)

**Tanggal:** 2026-10-22 · **Status:** IN PROGRESS · **Jenis:** PARSIAL (T1, T2, T3 selesai; T4, T5, T6 + smoke exit belum dimulai)
**Branch:** `milestone/m03-authentication` (basis: tag `m02-done`, `8e7a8b1`) · **Handover sebelumnya:** `HANDOVER-M02.md`
**Commit milestone ini (urutan):** hasher Argon2id · config password · repositori sqlc · `chore(deps)` x/crypto langsung · fix touch aktivitas ·
config timeout sesi · SessionService · Identity+CSRF · cookie/Origin/middleware · docs handover (commit ini). SHA lokal berbeda dari milik chat ini.

## 1. Ringkasan
Fondasi autentikasi backend sudah ada, belum ada endpoint HTTP-nya. Hasher Argon2id (PHC, rehash-on-login, dummy hash, batas 256 byte,
semaphore konkurensi), repositori sqlc untuk credentials/sessions/security_events, `SessionService` (token 32 byte acak, hanya SHA-256-nya
yang tersimpan; Redis untuk lookup cepat, PostgreSQL otoritatif; Redis mati/kosong tetap melayani; timeout idle + absolut; rotasi atomik;
pencabutan; event `session.revoked`), token CSRF terikat sesi, cookie `__Host-` dan middleware `Authenticate`/`CSRF`/`RequireAuth`.
Tidak ada migrasi baru. `X-Dev-Actor` di endpoint echo MASIH dipakai sampai T4.

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T1 hasher + repositori | DONE | `pkg/security`, `internal/auth/repository` (sqlc blok kedua) |
| T2 SessionService | DONE | Redis + PG; fallback; rotasi; revoke; tanpa token/hash di log |
| T3 ◆CP1 cookie + CSRF + middleware + Identity | DONE | belum dipasang ke `NewHandler` (T4) |
| T4 endpoint login/logout/refresh/me/csrf/switch-organization/security/* | TODO | lihat §10 |
| T5 kunci akun + rate limit login + security events | TODO | |
| T6 `make bootstrap-admin` | TODO | |
| Exit `scripts/smoke/m03.sh` + migrasi smoke M02 dari `X-Dev-Actor` ke sesi | TODO | |

## 3. Bukti verifikasi
- **Di mesin pengembang (2026-10-22, WSL2 + Docker Desktop, PG 18 / Redis 8 sungguhan):** setelah T1+T2:
  `go test -race -count=1 ./backend/pkg/... ./backend/internal/auth/...` hijau (`pkg/*`, `auth/repository` 35 s, `auth/service` 54 s);
  `make lint` 0 isu (golangci, svelte-check, prettier, lint-structure). `make generate-check` belum dilaporkan.
- **Hanya di sandbox chat (PostgreSQL 16.2 + Redis 6.2 sungguhan, `uuidv7()` ditiru; bukan PG18/Redis 8):** T3 (`auth/transport/http`,
  `service/csrf`), serta T1/T2 ulang; `go vet`, `gofmt`, `golangci-lint` v2.14.0 (0 isu). **Belum terbukti di mesin untuk T3**:
  jalankan perintah §7.
- **`make verify` penuh BELUM dijalankan sejak M02.** `scripts/smoke/m03.sh` belum ada.
- **Tes negatif yang ada:** token cookie sampah/berformat salah/tak pernah diterbitkan; sesi dicabut/idle/absolut; sesi user lain tak bisa
  dicabut/dirotasi; entri Redis basi dibatasi satu interval; rotasi serentak = satu pemenang; tulisan di transaksi rollback tidak
  meninggalkan jejak Redis; token tak muncul di Redis/PG/log; rehash basi tak menimpa password baru; CSRF hilang/salah/sesi lain/pasca-rotasi/
  Origin asing/`null`; galat infrastruktur = 503 (bukan logout). Mutasi sengaja dijalankan untuk menunjukkan tes menangkap regresinya.

## 4. Keputusan & penyimpangan
- **ADR-0020** (bagian 1) memuat 16 keputusan; yang menyimpang dari teks docs/05: hasher menerima `ctx` (§4.1); timeout dari config dulu, bukan
  `settings` (§12.1); aktivitas saat Redis mati dibatasi laju, bukan di-batch (§7.2); urutan middleware Authenticate→CSRF (§9);
  `refresh` = rotasi tanpa memperpanjang batas absolut (§17 vs §12.1); `last_activity_at` ikut di nilai Redis (§7).
- **Doc-sync TERTUNDA (lakukan di ritual akhir M03):** docs/05 §4.1 (antarmuka ber-`ctx`, `CompareDummy`, hash dummy acak), §7 (nilai Redis memuat
  last_activity_at; batas basi 1 interval), §7.2, §8 (nama cookie `__Host-` hanya bila Secure), §9 (urutan), §12.1 (sumber timeout), §17 (semantik refresh),
  §19 (`service/csrf.go`, paket `authhttp`); docs/08 §11 (kode baru dan statusnya: `csrf_failed` 403, dst. dari T4/T5); docs/10 §8 (env baru:
  `PASSWORD_*`, `SESSION_IDLE_TIMEOUT`, `SESSION_ABSOLUTE_TIMEOUT`); README.

## 5. Delta kontrak
- **DB:** tidak ada migrasi. Blok sqlc baru `backend/internal/auth/repository` (query: `users.sql`, `credentials.sql`, `sessions.sql`, `security_events.sql`).
- **API:** belum ada endpoint. Kode error baru: `csrf_failed` (403). Header `X-CSRF-Token`; cookie `[__Host-]platform_session`.
- **Event:** Redis Pub/Sub channel `session.revoked` `{session_id,user_id,reason}` diterbitkan `SessionService` (belum ada subscriber: Hub di M08).
- **Config/env baru:** `PASSWORD_ARGON2_MEMORY_KIB` (65536), `PASSWORD_ARGON2_TIME` (3), `PASSWORD_ARGON2_PARALLELISM` (2),
  `PASSWORD_HASH_CONCURRENCY` (0=CPU), `PASSWORD_MIN_LENGTH` (12), `SESSION_IDLE_TIMEOUT` (30m), `SESSION_ABSOLUTE_TIMEOUT` (12h); semuanya di `.env.example`.
- **Permission baru:** tidak ada (self-service auth hanya butuh autentikasi, docs/05 §17).

## 6. Peta kode
| Hal | Lokasi |
|---|---|
| Hasher Argon2id, `PasswordPolicy`, `Argon2ParamsFromInts` | `backend/pkg/security/password.go` |
| Query SQL auth + hasil sqlc | `backend/internal/auth/repository/queries/*.sql`, `*.sql.go`, `models.go` |
| `Store.Q(ctx)` (sadar transaksi) | `backend/internal/auth/repository/store.go` |
| `Identity`, `AuthLevel`, konteks, `ErrUnauthenticated`, `ErrSessionNotFound` | `backend/internal/auth/domain/` |
| `SessionService` (Create/Authenticate/Rotate/Revoke*/List) | `backend/internal/auth/service/session.go` |
| `CSRF` | `backend/internal/auth/service/csrf.go` |
| Cookie, `OriginPolicy`, `Middleware`, `CSRFFailed` | `backend/internal/auth/transport/http/` (paket `authhttp`) |
| Rakitan router (belum memasang auth) | `backend/internal/httpserver/router.go`, `kernel.go`, `echo.go` (`devActor`) |

## 7. Cara menjalankan & memverifikasi
Dari repo di `milestone/m03-authentication` setelah semua patch/commit diterapkan:
```bash
make generate-check
go test -race -count=1 ./backend/pkg/... ./backend/internal/auth/... 2>&1 | tail -40
make lint
```
Lalu (Nizza AI dihentikan lebih dulu bila memakai port 5432/6379/8080): `make verify`.

## 8. Masalah diketahui / utang teknis
- Bukti T3 hanya dari sandbox (PG16/Redis 6.2); belum dari `make verify`. `generate-check` belum dilaporkan untuk T1–T3.
- Entri Redis basi bisa hidup sampai 1 menit bila penghapusan Redis gagal (ADR-0020 butir 7).
- Parameter Argon2 bawaan belum disetel pada perangkat keras deploy (target 50–150 ms/hash); tes memakai parameter kecil.
- Tes waktu-tak-terbedakan (email tak dikenal vs password salah) hanya diuji pada jalur (satu derivasi, bagian kritis sama), belum statistik waktu;
  uji statistik rawan flake — jangan dipaksakan sebagai tes biasa (bila dibuat: toleran, di smoke).
- Hak `app_user` untuk `bootstrap-admin` (platforms, roles, role_assignments, users, credentials) belum diperiksa; kemungkinan butuh role migrator.
- Endpoint echo masih memakai `devActor`; smoke M02 menguji `X-Dev-Actor` non-UUID → 400 dan akan berubah di T4.
- Pembatas laju log "Redis terdegradasi" memakai jam dinding proses (bukan jam sesi): disengaja.

## 9. Pertanyaan terbuka
1. Status HTTP `account_locked`: usulan 423 (docs/08 §11 tidak menetapkan). 2. Batas jumlah sesi aktif per user (belum ada). 3. Apakah kegagalan CSRF perlu
   `security_events` (kini hanya log debug; menulis event per request bisa menjadi vektor banjir).

## 10. Langkah berikutnya: T4 (lanjutan M03)
- **Baca:** docs/05 §8–§9, §11–§13, §17–§18; docs/08 §10.1; errata B-04, B-07, I-04, I-05; docs/12 §4.1/§4.4. Tidak perlu membaca ulang yang lain.
- **T4 (tes dulu):** pasang `authhttp.Middleware` ke `NewHandler` lewat `Options.Auth` (rantai: RequestID … SecurityHeaders → Authenticate → CSRF → rute);
  `KernelServices`: `Idempotency.Subject` dari `Identity`. Handler: `POST /auth/login` (rate limit aturan `Sensitive` → cari user → email tak dikenal/tanpa
  credential → `CompareDummy`; password benar BARU memeriksa `status` (locked/suspended) dan kunci akun; `RehashPasswordIfUnchanged`; `Create` sesi + security event +
  activity dalam SATU transaksi; `Cookie.Set`; respons memuat `csrf_token`), `POST /auth/logout`, `POST /auth/refresh`, `GET /auth/me`, `GET /auth/csrf`,
  `POST /auth/switch-organization` (`HasActiveMembership`, rotasi), `GET/DELETE /security/sessions[/{id}]`, `POST /security/sessions/revoke-others`,
  `POST /security/password` (rotasi + revoke others), `GET /security/events` (keyset). Daftarkan kode error yang dipakai (`invalid_credentials` 401, `account_suspended` 403, ...).
  Ganti `devActor` di echo dengan `RequireAuth`; perbarui tes echo, tes e2e infra, dan `scripts/smoke/m02.sh`.
- **T5:** kunci akun Redis `auth:lock:{user_id}` (TTL, 5 gagal), penghitung per akun/IP + jeda progresif; `account_locked` hanya setelah password benar.
- **T6:** `make bootstrap-admin EMAIL=…` idempoten (platform row + role Super Admin + user + assignment platform); password dari stdin atau acak sekali-tampil;
  periksa hak role DB lebih dulu. **Exit:** `scripts/smoke/m03.sh` (bootstrap → csrf → login → me → logout → me = 401); lalu ritual akhir docs/22 §4.3.
- **Risiko:** menyentuh tes M02 (echo, e2e, smoke) bersamaan dengan fitur baru; kerjakan bertahap dan jalankan `make verify` di tiap ◆CP.

## 11. Prompt untuk chat berikutnya
Upload zip repo terbaru dan tempel **prompt P0 yang sama** (docs/23-prompt-library.md): P0 membaca `docs/STATUS.md`, yang kini menyatakan M03 IN PROGRESS dan
menunjuk handover ini. Pemeriksaan keadaan repo wajib: `git status -sb && git log --oneline -5 --decorate && git tag`.
