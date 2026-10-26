# STATUS

**Terakhir diperbarui:** 2026-10-26 oleh sesi M03 (parsial 2)
**Milestone aktif:** M03 — Authentication — IN PROGRESS (parsial 2, ◆CP2 lewat: T1–T4c dan T4d-1 selesai; T4d-2, T5, T6, smoke M03 + migrasi smoke M02 belum). Lanjutkan dari `docs/handover/HANDOVER-M03-partial-2.md`. M02 DONE (tag `m02-done`).
**Branch:** `milestone/m03-authentication` (basis: tag `m02-done`, `8e7a8b1`); belum di-merge ke `main`
**Kesehatan `make verify`:** MERAH hanya di smoke M02, SENGAJA (2026-10-26, `GO_TEST_PARALLEL=1`, PG 18 / Redis 8): lint 0 isu, generate-check, test (21 paket ok), migrate-roundtrip, build, dan smoke M00 lulus; smoke M02 gagal di "tanpa Idempotency-Key" (401, bukan 400) karena `X-Dev-Actor` dihapus di T4a — dipulihkan oleh T6 + migrasi skrip (HANDOVER-M03-partial-2 §8, §10). Catatan lingkungan: `make verify` paralel rapuh di Docker Desktop mesin ini (HANDOVER-M03-partial-2 §8); pakai `GO_TEST_PARALLEL=1` bila perlu.

| M | Nama | Status | Tag | Handover |
|---|---|---|---|---|
| M00 | Foundation & Tooling | DONE | m00-done | handover/HANDOVER-M00.md |
| M01 | Database Schema | DONE | m01-done | handover/HANDOVER-M01.md |
| M02 | Kernel (tx, outbox, audit, HTTP toolkit) | DONE | m02-done | handover/HANDOVER-M02.md |
| M03 | Authentication | IN PROGRESS (parsial 2: T1–T4c, T4d-1) | – | handover/HANDOVER-M03-partial-2.md |
| M04 | Authorization | TODO | – | – |
| M05 | Organization · User · Group · Invite · Email | TODO | – | – |
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
Rev 1.1 berlaku. Penyimpangan: ADR-0002, ADR-0003, ADR-0004, ADR-0005, ADR-0006, ADR-0007, ADR-0008, ADR-0009, ADR-0010, ADR-0011, ADR-0012, ADR-0013, ADR-0014, ADR-0015, ADR-0016, ADR-0017, ADR-0018, ADR-0019, ADR-0020 (M03, bagian 1–2) · ADR terakhir 0020 (lanjutkan dari 0021; keputusan T4d-2–T6 menambah ke ADR-0020 (bagian 3) atau ADR baru) · Doc-sync M03 TERTUNDA sampai akhir milestone (daftar: HANDOVER-M03-partial-1 §4 dan HANDOVER-M03-partial-2 §4) · Doc-sync M02 sudah diterapkan: 08 §4/§6/§7/§9/§11, 10 §3/§4/§6.2/§7/§8, 13 §2.2, 14 §3/§5/§6, 16, 18 §8, 19 §3, README (tidak ada yang tertunda) · Doc-sync M01 sudah diterapkan: 04 §13.2/§15/§17, 16, 19.

## Blocker / catatan lintas milestone
- Terputuskan di M01: sumber PostgreSQL untuk roundtrip (ADR-0005), peringatan air (ADR-0007). Path modul Go: `github.com/master-abror/zago-core` (ADR-0004).
- Utang lintas milestone dari M01: modul harus mencabut DML `app_user` pada `schema_migrations_<kode>` (M07, ADR-0006); layanan group harus mencabut `role_assignments` ber-scope group saat group diarsipkan/dihapus (M05, ADR-0008); `platforms.version` belum disinkronkan (M15).
- Dari M02: urutan milestone tidak diubah (backend dulu, UI mulai M10; keputusan pemilik proyek 2026-10-18). Lisensi proyek belum ditetapkan. Keputusan terbuka: mekanisme checkpoint antar-chat (handover M02 §9).
- Lingkungan pengembangan: tes memakai Docker; bila `make verify` merah acak di tingkat container, simpan log lengkap di folder home (bukan `/tmp`) dan lihat handover M02 §8 sebelum mengubah apa pun.
