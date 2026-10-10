# STATUS

**Terakhir diperbarui:** 2026-10-10 oleh sesi M02
**Milestone aktif:** M02 — Kernel — IN PROGRESS (T6, T1, T2, T3, T4, T5a, T5b, T5c terbukti hijau di mesin pengembang: tes Go + `make lint`; tersisa T5d telemetry, T5e sqlc + swag, T5f uji e2e `RunAPI`, lalu ritual akhir; `make verify` penuh (khususnya `verify-smoke`) belum diulang setelah T5)
**Branch:** `main` (tag `m01-done`); M02 memakai `milestone/m02-kernel`
**Kesehatan `make verify`:** hijau penuh (termasuk `verify-smoke`, `M00 smoke OK`) pada baseline M02 `f1f7ae0` dan, menurut pengembang, setelah T4 (2026-10-10; WSL2 + Docker Desktop; stack Nizza AI dihentikan lebih dulu). Setelah T5a/b/c (`ca77bd8`, `01ef4ae`): tes Go paket tersentuh (`-race`, testcontainers) + `make lint` hijau, `make verify` penuh belum diulang. M01: hijau 2026-10-07; CI PR #2 hijau.

| M | Nama | Status | Tag | Handover |
|---|---|---|---|---|
| M00 | Foundation & Tooling | DONE | m00-done | handover/HANDOVER-M00.md |
| M01 | Database Schema | DONE | m01-done | handover/HANDOVER-M01.md |
| M02 | Kernel (tx, outbox, audit, HTTP toolkit) | IN PROGRESS (T6, T1–T4, T5a–c selesai; berikutnya T5d, T5e, T5f) | – | handover/HANDOVER-M02-partial-3.md |
| M03 | Authentication | TODO | – | – |
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
Go 1.26.5 · Node 24 (v24.21.0) · PostgreSQL 18.x (`postgres:18`, teramati 18.6) · Redis 8.x (`redis:8`, teramati 8.10.2); lihat adr/0001 dan adr/0002.

## Dokumen
Rev 1.1 berlaku. Penyimpangan: ADR-0002, ADR-0003, ADR-0004, ADR-0005, ADR-0006, ADR-0007, ADR-0008, ADR-0009, ADR-0010, ADR-0011, ADR-0012, ADR-0013, ADR-0014, ADR-0015, ADR-0016 · Doc-sync M02 tertunda sampai ritual akhir (daftar di handover M02 §4) · Doc-sync M01 sudah diterapkan: 04 §13.2/§15/§17, 16, 19 (tidak ada yang tertunda)

## Blocker / catatan lintas milestone
- Terputuskan di M01: sumber PostgreSQL untuk roundtrip (ADR-0005), peringatan air (ADR-0007). Path modul Go: `github.com/master-abror/zago-core` (ADR-0004).
- Utang lintas milestone dari M01: modul harus mencabut DML `app_user` pada `schema_migrations_<kode>` (M07, ADR-0006); layanan group harus mencabut `role_assignments` ber-scope group saat group diarsipkan/dihapus (M05, ADR-0008).
