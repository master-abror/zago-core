# CLAUDE.md — Open Source Modular Application Platform

## Cara kerja (WAJIB)
1. Baca urut: CLAUDE.md → docs/STATUS.md → HANDOVER terbaru (docs/handover/) → bagian milestone di docs/21-milestone-plan.md.
2. Baca HANYA bagian dokumen 01–20 yang tercantum di "Baca:" milestone. Jangan memuat semua dokumen.
3. `make verify` harus hijau sebelum mulai. Merah = perbaiki dulu (prompt P5 di docs/23).
4. Satu milestone per chat. Berhenti di ◆CP bila konteks sudah panjang → handover parsial (P4).
5. Prioritas bila bertentangan: ADR terbaru (docs/adr/) > dokumen 01–20 Rev 1.1 > docs/00 (catatan perubahan).
6. Tidak ada penyimpangan diam-diam: tulis ADR, lalu Doc-sync di akhir milestone.
7. Tes dulu untuk auth/authz/tenant isolation (tes negatif wajib). Jangan klaim selesai tanpa bukti.

## Stack (dipatok — jangan ganti tanpa ADR)
Go (chi v5, pgx v5/pgxpool, sqlc, golang-migrate sebagai library, go-redis v9, coder/websocket, slog)
PostgreSQL 18 · Redis 8 · Svelte 5 + Vite + TypeScript (SPA, same-origin dengan API) · Docker
Versi patch persis: docs/adr/0001-stack.md

## Peta repo
backend/{cmd/{api,worker,migrate},internal/*,pkg/*,migrations}
packages/{module-sdk,ts-sdk,ui} · modules/<code>/ · apps/web · deploy/ · scripts/ · docs/
Satu go.mod di ROOT. Build context Docker = root.

## Perintah
make setup | infra-up | dev | verify | test | lint | migrate-up | migrate-roundtrip |
generate | modules-sync | smoke | bootstrap-admin EMAIL=… | repo-zip | handover-pack M=NN

## Aturan kode ringkas
- Modul HANYA impor packages/module-sdk, tak pernah backend/internal/*.
- Domain/application tak mengenal HTTP; hanya transport yang memetakan error → kode API.
- Setiap query tabel tenant wajib filter organization_id; jalur platform = method berlabel eksplisit + Decision platform.
- Perubahan state + audit + event (outbox) dalam SATU transaksi (TxManager.WithinTx). Efek samping lambat setelah commit.
- Handler event idempotent (at-least-once). Handler HTTP tak pernah panik pada input buruk (uuid.Parse, bukan MustParse).
- Token (sesi/reset/undangan) hanya disimpan sebagai SHA-256 hash. UUID bukan bearer token.
- Frontend: tak pernah fetch langsung — selalu via core/api; URL relatif; izin di UI hanya UX.
- Rahasia tak pernah di-commit/log. Password/token tak pernah masuk audit metadata.
- Migrasi: nomor monoton, tak pernah mengedit migrasi yang sudah di-tag; setiap tabel ber-updated_at memanggil attach_updated_at.

## Akhir sesi
Serahkan zip repo terbaru (scripts/repo-zip.sh) + perintah git; chat berikutnya memakai prompt P0 yang sama (docs/23).

## Status
Lihat docs/STATUS.md (jangan menduplikasi status di sini).
