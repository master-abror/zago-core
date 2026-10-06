# STATUS

**Terakhir diperbarui:** 2026-10-05 oleh sesi M01
**Milestone aktif:** M01 — Database Schema (IN PROGRESS: kode, tes, ADR, doc-sync, ERD selesai; menunggu `make verify` dan CI hijau pada hasil akhir)
**Branch:** `milestone/m01-database-schema` (basis: tag `m00-done`)
**Kesehatan `make verify`:** M00: hijau (2026-10-04). M01: BELUM dijalankan pada hasil akhir; di sandbox terbukti hanya tes skema/seeder/CLI pada PostgreSQL 16 lokal (bukan PG18/testcontainers) — lihat handover M01 setelah verify hijau.

| M | Nama | Status | Tag | Handover |
|---|---|---|---|---|
| M00 | Foundation & Tooling | DONE | m00-done | handover/HANDOVER-M00.md |
| M01 | Database Schema | IN PROGRESS | – | – |
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
Rev 1.1 berlaku. Penyimpangan: ADR-0002, ADR-0003, ADR-0004, ADR-0005, ADR-0006, ADR-0007, ADR-0008 · Doc-sync M01 sudah diterapkan: 04 §13.2/§15/§17, 16, 19 (tidak ada yang tertunda)

## Blocker / catatan lintas milestone
- Terputuskan di M01: sumber PostgreSQL untuk roundtrip (ADR-0005), peringatan air (ADR-0007). Path modul Go: `github.com/master-abror/zago-core` (ADR-0004).
- Utang lintas milestone dari M01: modul harus mencabut DML `app_user` pada `schema_migrations_<kode>` (M07, ADR-0006); layanan group harus mencabut `role_assignments` ber-scope group saat group diarsipkan/dihapus (M05, ADR-0008).
