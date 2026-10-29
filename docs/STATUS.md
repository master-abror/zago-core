# STATUS

**Terakhir diperbarui:** 2026-10-29 oleh sesi M03 (ritual akhir)
**Milestone aktif:** M03 — Authentication — DONE (menunggu merge ke `main` dan tag `m03-done` oleh pengguna). Berikutnya: M04 — Authorization Engine (mulai dari `docs/handover/HANDOVER-M03.md` §10–§11; branch `milestone/m04-authorization` dari tag `m03-done`).
**Branch:** `milestone/m03-authentication` (basis: tag `m02-done`); belum di-merge ke `main` saat berkas ini ditulis (catatan: setelah merge dan tag, perbarui baris ini bila perlu)
**Kesehatan `make verify`:** HIJAU di laptop (WSL2, 2026-10-29, `GO_TEST_PARALLEL=1`, PG 18 / Redis 8, Go 1.26.5, port 25000–25005, exit=0) setelah commit `test(smoke): smoke M03 …`: `gofmt -l backend` kosong, smoke M00, M02, dan **M03 OK** di stack asli (termasuk kunci akun, ganti password, riwayat event). Commit dokumen sesudahnya (doc-sync, handover, STATUS, CHANGELOG) hanya mengubah teks: `make verify` utuh gagal tiga kali karena flake Docker laptop (paket berpindah-pindah; kelas `wait for reaper …`), tetapi setiap paket Go hijau saat dijalankan sendirian dan tahap sisa (lint, generate-check, migrate-roundtrip, build, `verify-smoke.sh` dengan smoke M00/M02/M03) hijau, `exit=0` (HANDOVER-M03 §3 butir 3); satu run `make verify` utuh yang hijau pada commit dokumen belum ada. Hijau disimpulkan dari `exit=0`; baris lint/generate-check/migrate-roundtrip/build tidak ditempel. Flake Docker laptop = kelas galat di ADR-0021 bagian 2, bukan kode; `✗ port … sudah dipakai` = bentrok port.

| M | Nama | Status | Tag | Handover |
|---|---|---|---|---|
| M00 | Foundation & Tooling | DONE | m00-done | handover/HANDOVER-M00.md |
| M01 | Database Schema | DONE | m01-done | handover/HANDOVER-M01.md |
| M02 | Kernel (tx, outbox, audit, HTTP toolkit) | DONE | m02-done | handover/HANDOVER-M02.md |
| M03 | Authentication | DONE | m03-done (dibuat setelah merge) | handover/HANDOVER-M03.md |
| M04 | Authorization | TODO (berikutnya) | – | – |
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
Rev 1.1 berlaku. Penyimpangan: ADR-0002 s.d. ADR-0019 (M00–M02), ADR-0020 (M03, bagian 1–6, butir 1–61), ADR-0021 (port host dev; bagian 2: kembali ke laptop) · ADR terakhir 0021 (lanjutkan dari 0022; keputusan baru menambah ADR baru atau bagian baru ADR yang relevan) · Doc-sync M03 SUDAH diterapkan (docs/05, 08, 10, 04 §16, 00 B-04, 15, 16, 19, ADR-0014, README; tidak ada yang tertunda; satu-satunya yang belum tergabung: `HANDOVER-M03-partial-4.md` §4 karena berkasnya tidak ada) · Doc-sync M02 sudah diterapkan: 08 §4/§6/§7/§9/§11, 10 §3/§4/§6.2/§7/§8, 13 §2.2, 14 §3/§5/§6, 16, 18 §8, 19 §3, README (tidak ada yang tertunda) · Doc-sync M01 sudah diterapkan: 04 §13.2/§15/§17, 16, 19.

## Blocker / catatan lintas milestone
- Terputuskan di M01: sumber PostgreSQL untuk roundtrip (ADR-0005), peringatan air (ADR-0007). Path modul Go: `github.com/master-abror/zago-core` (ADR-0004).
- Utang lintas milestone dari M01: modul harus mencabut DML `app_user` pada `schema_migrations_<kode>` (M07, ADR-0006); layanan group harus mencabut `role_assignments` ber-scope group saat group diarsipkan/dihapus (M05, ADR-0008); `platforms.version` belum disinkronkan (M15).
- Dari M02: urutan milestone tidak diubah (backend dulu, UI mulai M10; keputusan pemilik proyek 2026-10-18). Lisensi proyek belum ditetapkan. Keputusan terbuka: mekanisme checkpoint antar-chat (handover M02 §9).
- Lingkungan pengembangan: tes memakai Docker; bila `make verify` merah acak di tingkat container, simpan log lengkap di folder home (bukan `/tmp`) dan lihat handover M02 §8 sebelum mengubah apa pun.
- Dari M03: pengembangan di laptop WSL2 (VPS tidak dipakai lagi); `git push` setelah SETIAP patch. Utang M04+: pembuka kunci akun administratif, identitas platform vs `switch-organization`, CI `docker-smoke` belum menyiapkan Go (M15), retensi `security_events` (M06/M15). Lihat HANDOVER-M03 §8–§9.
