# STATUS

**Terakhir diperbarui:** 2026-11-05 oleh sesi M04 (checkpoint partial-6, ◆CP3)
**Milestone aktif:** M04 — Authorization Engine — IN PROGRESS (T1–T3c dan T4a–T4c selesai, ◆CP3 tercapai; berikutnya T4d retry sinkron saat start, lalu T5). M03 DONE (tag `m03-done` = merge commit PR #6, `20e7fc0` di laptop). Rencana dan keputusan: `docs/adr/0022-m04-authorization-design.md` (bagian 1–6). Handover terbaru: `docs/handover/HANDOVER-M04-partial-6.md` (`partial-1` TIDAK ADA di repo; isinya direkonstruksi di lampiran A partial-2).
**Branch:** `milestone/m04-authorization` (basis: tag `m03-done`).
**Kesehatan `make verify` (laptop WSL2, Go 1.26.5, 2026-11-05):** semua patch T4c (domain field permission, `FieldGuard` + repositori, `field_permission_denied` + perakitan) lolos `gofmt`, `go vet`, `golangci-lint` (`0 issues.`) dan tes paket yang disentuh dengan `-race` pada percobaan pertama. Run utuh pada `HEAD` `e262cc0`: `exit=2` HANYA karena tahap smoke (login Super Admin melewati `curl --max-time 8` di bawah beban; 503 `context canceled` adalah akibat curl memutus koneksi); `lint`, `generate-check`, `test` (30 paket `ok`, 6 tanpa tes, 0 `FAIL`), `migrate-roundtrip`, dan `build` LOLOS pada run itu, dan smoke yang diulang sendirian `exit=0` (`M00/M02/M03 smoke OK`). Jadi setiap tahap hijau pada `HEAD`, tetapi TIDAK dalam satu run utuh: TIDAK ada satu run utuh hijau pada kode T4. Pada `7d046d5` run utuh juga `exit=2` (flake reaper di `internal/kernel`; 36 dari 36 paket hijau sendirian; smoke gagal dua kali karena unduhan modul, kelas lingkungan). Run utuh hijau terakhir: `ea16a9c` (T3b, `exit=0`, `M00/M02/M03 smoke OK`). Detail: HANDOVER-M04-partial-6 §3.

| M | Nama | Status | Tag | Handover |
|---|---|---|---|---|
| M00 | Foundation & Tooling | DONE | m00-done | handover/HANDOVER-M00.md |
| M01 | Database Schema | DONE | m01-done | handover/HANDOVER-M01.md |
| M02 | Kernel (tx, outbox, audit, HTTP toolkit) | DONE | m02-done | handover/HANDOVER-M02.md |
| M03 | Authentication | DONE | m03-done | handover/HANDOVER-M03.md |
| M04 | Authorization | IN PROGRESS | – | handover/HANDOVER-M04-partial-6.md (parsial) |
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
Rev 1.1 berlaku. Penyimpangan: ADR-0002 s.d. ADR-0019 (M00–M02), ADR-0020 (M03, bagian 1–6, butir 1–61), ADR-0021 (port host dev; bagian 2: kembali ke laptop), ADR-0022 (M04, bagian 1–6, butir 1–45) · ADR terakhir 0022 (lanjutkan dari 0023 atau bagian baru ADR-0022; keputusan baru menambah ADR baru atau bagian baru ADR yang relevan) · **Doc-sync M04 TERTUNDA (kumulatif, lihat HANDOVER-M04-partial-6 §4):** docs/06 (§4.1, §4.2, §5.2, §5.5, §6 semantik efek/penundaan/tata bahasa/Env, §7 FieldGuard (tanda tangan, sumber definisi, per aksi, pencocokan kunci), §8.1, §9, §12 termasuk `policy_evaluation_failed` dan `field_permission_denied`, §13, §14), docs/02 §16 + docs/03 §16 (arti `allow` sebagai syarat tambahan; kolom/constraint `actions` di docs/03, docs/04, `docs/erd.md` SUDAH diterapkan di T4a), docs/05 (switch-organization platform), docs/08 §11 (`scope_mismatch`, `policy_evaluation_failed`, status `field_permission_denied`), docs/07 (bentuk kode field permission dicadangkan), docs/00 B-04, docs/10 §3/§11/§15, docs/18 §7, ADR-0020 butir 28 · Doc-sync M03 SUDAH diterapkan (docs/05, 08, 10, 04 §16, 00 B-04, 15, 16, 19, ADR-0014, README; tidak ada yang tertunda; satu-satunya yang belum tergabung: `HANDOVER-M03-partial-4.md` §4 karena berkasnya tidak ada) · Doc-sync M02 sudah diterapkan: 08 §4/§6/§7/§9/§11, 10 §3/§4/§6.2/§7/§8, 13 §2.2, 14 §3/§5/§6, 16, 18 §8, 19 §3, README (tidak ada yang tertunda) · Doc-sync M01 sudah diterapkan: 04 §13.2/§15/§17, 16, 19.

## Blocker / catatan lintas milestone
- Terputuskan di M01: sumber PostgreSQL untuk roundtrip (ADR-0005), peringatan air (ADR-0007). Path modul Go: `github.com/master-abror/zago-core` (ADR-0004).
- Utang lintas milestone dari M01: modul harus mencabut DML `app_user` pada `schema_migrations_<kode>` (M07, ADR-0006); layanan group harus mencabut `role_assignments` ber-scope group saat group diarsipkan/dihapus (M05, ADR-0008); `platforms.version` belum disinkronkan (M15).
- Dari M02: urutan milestone tidak diubah (backend dulu, UI mulai M10; keputusan pemilik proyek 2026-10-18). Lisensi proyek belum ditetapkan. Keputusan terbuka: mekanisme checkpoint antar-chat (handover M02 §9).
- Lingkungan pengembangan: tes memakai Docker; bila `make verify` merah acak di tingkat container, simpan log lengkap di folder home (bukan `/tmp`) dan lihat handover M02 §8 sebelum mengubah apa pun.
- Dari M03: pengembangan di laptop WSL2 (VPS tidak dipakai lagi); `git push` setelah SETIAP patch. Utang M04+: pembuka kunci akun administratif, identitas platform vs `switch-organization`, CI `docker-smoke` belum menyiapkan Go (M15), retensi `security_events` (M06/M15). Lihat HANDOVER-M03 §8–§9.
