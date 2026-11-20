# STATUS

**Terakhir diperbarui:** 2026-11-19 oleh sesi M05 partial-1 (checkpoint dini: baseline dan dua patch smoke, tanpa kode M05)
**Milestone aktif:** M05 — Organization · User · Group · Invite · Email — IN PROGRESS (partial-1: baseline dan dua patch smoke; T1a belum dimulai). **M04 — Authorization Engine — DONE** (tag `m04-done` = `6a14f87`, merge PR #7, ada di origin). M03 DONE (tag `m03-done` = `883487d`).
**Branch:** `milestone/m05-organization` (dari tag `m04-done`; ada di origin).
**Handover terbaru:** `docs/handover/HANDOVER-M05-partial-1.md` (juga `HANDOVER-M04.md` final dan riwayat `HANDOVER-M04-partial-2` sampai `-19`; `partial-1` M04 TIDAK ADA di repo). Prompt sesi berikutnya: `docs/handover/PROMPT-M05-partial-1.md`. Keputusan: `docs/adr/0022-m04-authorization-design.md` (bagian 1–23, butir 1–78); ADR-0023 belum ditulis.
**Kesehatan `make verify` (laptop WSL2, Go 1.26.5):** pada kode M04 (sesi M05 partial-1, 2026-11-19) run utuh `exit=2`, BUKAN hijau: lint, generate-check, test (30 `ok`, 0 FAIL, 6 paket tanpa tes), migrate-roundtrip, dan build lolos; `verify-smoke.sh` (alur dingin) gagal di `POST /auth/login` pada tiga run (penyebab TIDAK terbukti; I/O VM Docker lambat teramati). `make smoke` pada stack hangat dengan `SMOKE_CURL_MAX_TIME=90` `exit=0` (m00, m02, m03, m04 OK). Rincian: `HANDOVER-M05-partial-1.md` §3 dan §8. Hijau penuh sebelumnya: M04 partial-19 (`exit=0`).

**Ringkasan M04:** katalog permission core (41 kode) dan empat role sistem di kode; `ScopeContext`; hak efektif dengan cache versi di Redis; `Authorizer.Can`; middleware `Scope` dan `Authorize`; policy yang hanya membatasi; `FieldGuard` dan `authzhttp.FieldHelper`; authority ceiling; jalur tulis dan baca role, assignment, policy; `simulate`; 15 rute di `/api/v1`; pencabutan sesi saat assignment platform dicabut; migrasi 034 dan 035 (berikutnya 036); `scripts/smoke/m04.sh`. Utang dan batas: `HANDOVER-M04.md` §8 (terutama nama definisi OpenAPI panjang, scope `resource`/`own` belum dievaluasi, `FieldGuard` belum dipakai handler produksi). Kewajiban untuk M05: `HANDOVER-M04.md` §10.

| M | Nama | Status | Tag | Handover |
|---|---|---|---|---|
| M00 | Foundation & Tooling | DONE | m00-done | handover/HANDOVER-M00.md |
| M01 | Database Schema | DONE | m01-done | handover/HANDOVER-M01.md |
| M02 | Kernel (tx, outbox, audit, HTTP toolkit) | DONE | m02-done | handover/HANDOVER-M02.md |
| M03 | Authentication | DONE | m03-done | handover/HANDOVER-M03.md |
| M04 | Authorization | DONE | m04-done (dibuat setelah merge) | handover/HANDOVER-M04.md |
| M05 | Organization · User · Group · Invite · Email | IN PROGRESS (partial-1) | – | handover/HANDOVER-M05-partial-1.md |
| M06 | Audit & System Log | TODO | – | – |
| M07 | Module System (backend) | TODO | – | – |
| M08 | Realtime: WebSocket · Notification · Inbox | TODO | – | – |
| M09 | Chat | TODO | – | – |
| M10 | Frontend Foundation | TODO | – | – |
| M11 | Frontend Admin A | TODO | – | – |
| M12 | Frontend Admin B | TODO | – | – |
| M13 | Frontend Realtime | TODO | – | – |
| M14 | Reference Module: Announcements | TODO | – | – |
| M15 | Hardening & Release v1.0.0 | TODO | – | – |

## Versi yang dipatok
Go 1.26.5 · Node 24 (v24.21.0) · PostgreSQL 18.x (`postgres:18`, teramati 18.6) · Redis 8.x (`redis:8`, teramati 8.10.2); lihat adr/0001 dan adr/0002. Dependensi M02: `prometheus/client_golang`, OpenTelemetry (`otel`, `sdk`, `trace`, `otlptracehttp`), alat `sqlc` v1.31.1 dan `swag` v1.16.6 (`tool` di `go.mod`).

## Dokumen
Rev 1.1 berlaku. Penyimpangan: ADR-0002 s.d. ADR-0019 (M00–M02), ADR-0020 (M03, bagian 1–6, butir 1–61), ADR-0021 (port host dev; bagian 2: kembali ke laptop), ADR-0022 (M04, bagian 1–23, butir 1–78) · ADR terakhir 0022 (lanjutkan dari 0023 atau bagian baru ADR-0022; keputusan baru menambah ADR baru atau bagian baru ADR yang relevan) · **Doc-sync M04 diterapkan** sebagai blok "As built (M04, ADR-0022 …)" di docs/00 B-04, 02 §15–§16, 05 §7 dan §17, 06 §5.3–§14, 07 §7, 08 §4/§6/§9/§11, 10 §3/§11/§15, 12 §4.6, 13 §2.1, 16 §2.7, 18 §7, 19 §9 (docs/03 §16.1, docs/04 dan `docs/erd.md` sudah sejak T4a/035); TERTUNDA: contoh JSON `simulate` dan daftar kode error di docs/06 §10/§12 dan docs/08 §11 tidak diedit di tempat (hanya blok di bawahnya), docs/13 §4, docs/12 §6 dan docs/08 §10.2 tidak dibuka; rincian di `HANDOVER-M04.md` §4 · Doc-sync M03 SUDAH diterapkan (docs/05, 08, 10, 04 §16, 00 B-04, 15, 16, 19, ADR-0014, README; tidak ada yang tertunda; satu-satunya yang belum tergabung: `HANDOVER-M03-partial-4.md` §4 karena berkasnya tidak ada) · Doc-sync M02 sudah diterapkan: 08 §4/§6/§7/§9/§11, 10 §3/§4/§6.2/§7/§8, 13 §2.2, 14 §3/§5/§6, 16, 18 §8, 19 §3, README (tidak ada yang tertunda) · Doc-sync M01 sudah diterapkan: 04 §13.2/§15/§17, 16, 19.

## Blocker / catatan lintas milestone
- Terputuskan di M01: sumber PostgreSQL untuk roundtrip (ADR-0005), peringatan air (ADR-0007). Path modul Go: `github.com/master-abror/zago-core` (ADR-0004).
- Utang lintas milestone dari M01: modul harus mencabut DML `app_user` pada `schema_migrations_<kode>` (M07, ADR-0006); layanan group harus mencabut `role_assignments` ber-scope group saat group diarsipkan/dihapus (M05, ADR-0008); `platforms.version` belum disinkronkan (M15).
- Dari M02: urutan milestone tidak diubah (backend dulu, UI mulai M10; keputusan pemilik proyek 2026-10-18). Lisensi proyek belum ditetapkan. Keputusan terbuka: mekanisme checkpoint antar-chat (handover M02 §9).
- Lingkungan pengembangan: tes memakai Docker; bila `make verify` merah acak di tingkat container, simpan log lengkap di folder home (bukan `/tmp`) dan lihat handover M02 §8 sebelum mengubah apa pun.
- Dari M03: pengembangan di laptop WSL2 (VPS tidak dipakai lagi); `git push` setelah SETIAP patch. Utang M04+: pembuka kunci akun administratif, identitas platform vs `switch-organization`, CI `docker-smoke` belum menyiapkan Go (M15), retensi `security_events` (M06/M15). Lihat HANDOVER-M03 §8–§9.
- Dari M04: utang nama definisi OpenAPI `authzhttp` (butir 78e; kembalikan nama pendek sebelum `packages/ts-sdk` memakai kontrak); scope `resource`/`own` belum dievaluasi; pemakai produksi pertama `FieldGuard` di M05; rate limit `simulate`, outbox pencabutan sesi, dan pemisahan bind dari sinkron katalog saat start di M15; fixture smoke M04 lewat `psql` sampai ada API M05. Daftar lengkap: `HANDOVER-M04.md` §8–§10.
