# STATUS

**Terakhir diperbarui:** 2026-10-08 oleh sesi M02
**Milestone aktif:** M02 — Kernel — IN PROGRESS (T6 testkit + T1 pool/TxManager terbukti hijau; T2 outbox/relay/EventBus terbukti hijau; T3 audit/system log/redaksi terbukti hijau; berikutnya T4 HTTP toolkit, lalu T5; `make verify` penuh belum dijalankan setelah perubahan M02)
**Branch:** `main` (tag `m01-done`); M02 memakai `milestone/m02-kernel`
**Kesehatan `make verify`:** hijau pada hasil akhir M01, 2026-10-07 (WSL2 + Docker Desktop, PostgreSQL 18 via testcontainers; `M00 smoke OK`); exit criterion M01 pada DB dev lulus. CI GitHub PR #2 hijau pada semua job.

| M | Nama | Status | Tag | Handover |
|---|---|---|---|---|
| M00 | Foundation & Tooling | DONE | m00-done | handover/HANDOVER-M00.md |
| M01 | Database Schema | DONE | m01-done | handover/HANDOVER-M01.md |
| M02 | Kernel (tx, outbox, audit, HTTP toolkit) | IN PROGRESS (T6, T1, T2, T3 selesai; berikutnya T4) | – | handover/HANDOVER-M02-partial-1.md |
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
Rev 1.1 berlaku. Penyimpangan: ADR-0002, ADR-0003, ADR-0004, ADR-0005, ADR-0006, ADR-0007, ADR-0008, ADR-0009, ADR-0010, ADR-0011 · Doc-sync M01 sudah diterapkan: 04 §13.2/§15/§17, 16, 19 (tidak ada yang tertunda)

## Blocker / catatan lintas milestone
- Terputuskan di M01: sumber PostgreSQL untuk roundtrip (ADR-0005), peringatan air (ADR-0007). Path modul Go: `github.com/master-abror/zago-core` (ADR-0004).
- Utang lintas milestone dari M01: modul harus mencabut DML `app_user` pada `schema_migrations_<kode>` (M07, ADR-0006); layanan group harus mencabut `role_assignments` ber-scope group saat group diarsipkan/dihapus (M05, ADR-0008).
