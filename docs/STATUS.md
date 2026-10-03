# STATUS

**Terakhir diperbarui:** 2026-10-03 oleh sesi M00
**Milestone aktif:** M00 — Foundation & Tooling
**Branch:** milestone/m00-foundation
**Kesehatan `make verify`:** hijau sampai `M00 smoke OK` (log pengembang 2026-10-03); kode keluar penuh pada clone bersih menunggu V1

| M | Nama | Status | Tag | Handover |
|---|---|---|---|---|
| M00 | Foundation & Tooling | IN REVIEW (kode selesai; menunggu V1–V3) | – | handover/HANDOVER-M00.md |
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
