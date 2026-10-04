# STATUS

**Terakhir diperbarui:** 2026-10-04 oleh sesi M00
**Milestone aktif:** M01 — Database Schema (belum dimulai; M00 selesai)
**Branch:** main (setelah merge PR #1); M01 memakai `milestone/m01-database-schema`
**Kesehatan `make verify`:** hijau (exit 0) pada klon bersih Linux, 2026-10-04; `make dev` native terbukti (`/health/ready` 200, Ctrl-C tanpa proses yatim); CI GitHub hijau pada PR #1 (run awal `37163858143`; run pada head akhir adalah syarat merge).

| M | Nama | Status | Tag | Handover |
|---|---|---|---|---|
| M00 | Foundation & Tooling | DONE | m00-done | handover/HANDOVER-M00.md |
| M01 | Database Schema | TODO | – | – |
| M02 | Kernel (tx, outbox, audit, HTTP toolkit) | TODO | – | – |
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
Rev 1.1 berlaku. Penyimpangan: ADR-0002, ADR-0003, ADR-0004 · Doc-sync tertunda: (tidak ada)

## Blocker / catatan lintas milestone
- Putuskan di awal M01: sumber PostgreSQL untuk `migrate-roundtrip` di `make verify` (handover M00 §8). Path modul Go sudah diputuskan: `github.com/master-abror/zago-core` (ADR-0004).
