# Open Source Modular Application Platform — Dokumentasi (Rev 1.1)

Fondasi aplikasi B2B/admin (autentikasi, organisasi, otorisasi, audit, modul, realtime) yang dibangun
sekali dengan benar, sehingga aplikasi baru cukup menulis sebuah **modul**.

## Isi
- `docs/01–20` — spesifikasi (Rev 1.1, sudah memuat seluruh perbaikan hasil review)
- `docs/00-errata-and-amendments.md` — catatan perubahan Rev 1.1 dan alasannya
- `docs/21-milestone-plan.md` — rencana pembangunan M00–M15
- `docs/22-handover-protocol.md` — aturan sesi, handover, git, testing
- `docs/23-prompt-library.md` — prompt P1–P6 untuk setiap chat room
- `CLAUDE.md`, `docs/STATUS.md`, `docs/adr/`, `docs/handover/` — memori repo untuk sesi
- `deploy/db/init/00-roles.sql`, `.env.example`, `scripts/handover-pack.sh` — berkas awal

## Mulai (cara yang direkomendasikan)
1. Zip folder ini (atau gunakan zip yang diterima) — **satu file zip**, jangan lampirkan dokumen satu per satu.
2. Buka chat baru, upload zip, tempel **P0** dari `docs/23-prompt-library.md` (prompt universal; milestone aktif
   dibaca dari `docs/STATUS.md`).
3. Chat bekerja sampai milestone selesai (atau berhenti di ◆CP), menulis HANDOVER + memperbarui STATUS, lalu
   menyerahkan **zip repo terbaru**. Jalankan `make verify` di mesinmu dan simpan ke git.
4. Buka chat baru, upload zip terbaru, tempel P0 yang sama. Ulangi sampai M15.

Alternatif: bila chat punya akses repo/git langsung, cukup `git pull` lalu tempel P0.

## Stack
Go (chi, pgx, sqlc) · PostgreSQL 18 · Redis 8 · Svelte 5 + Vite + TypeScript (SPA) · Docker
