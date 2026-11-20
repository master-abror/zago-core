# HANDOVER-UI-DESIGN-partial-1 (checkpoint pertama, fase desain UI/UX)

Dibuat di chat desain #1 atas perintah pemilik ("checkpoint"). Prompt awal: `docs/handover/PROMPT-UI-DESIGN-start.md`.
Prompt lanjutan: `docs/handover/PROMPT-UI-DESIGN-partial-2.md`.

## 1. Ringkasan

Prototipe statis (HTML + CSS + Bootstrap 5.3.9, tanpa build, tanpa framework JS) di `design/` pada branch `design/ui-prototype`
(dari `m04-done` = `6a14f87` = `main`). Selesai: F0 (style tile), F1a (token tema, komponen dasar, i18n), F1b (shell), F1c (Login, 10 state; diserahkan).
**F1 BELUM lengkap** (daftar kekurangan di §7). Tidak ada berkas di `backend/**`, `apps/**`, `packages/**` yang disentuh.

## 2. Keputusan desain dan status

| Keputusan | Status |
|---|---|
| Keluaran di `design/` (luar workspace npm); halaman datar di `design/` (`login.html`, `shell.html`, `components.html`), BUKAN `design/pages/` | lokasi disetujui; letak datar adalah deviasi dari usulan awal, belum dibahas |
| Bootstrap 5.3.9 lokal di `design/vendor/bootstrap-5.3.9/{css,js}` + `LICENSE` (bukan folder `dist`: `.gitignore` dan `scripts/repo-zip.sh` membuang `dist/`) | disetujui |
| Bootstrap Icons 1.13.2 (MIT, "Copyright (c) 2019-2024 The Bootstrap Authors"), sprite `design/assets/icons.svg` berisi 12 ikon, lisensi di `design/assets/LICENSE-bootstrap-icons` | disetujui, ter-commit di laptop (`721f3de`) |
| Tiga tema pilihan pengguna, SEMUA disertakan: Aurora (indigo, bawaan), Lagoon (teal), Ember (terakota); pemilih tema di menu pengguna (avatar AB) | disetujui |
| Mode: terang, gelap, otomatis (`prefers-color-scheme`); tombol terang/gelap di navbar kanan atas, tepat di samping avatar AB | disetujui |
| Bahasa: id-ID dan en-US dahulu; katalog `design/i18n.js` (110 kunci, id-ID = en-US) | disetujui |
| Shell: minibar (ikon saja; hover/fokus membuka popup di samping berisi nama menu + submenu) dan sidebar, dapat ditukar pengguna; drawer di bawah 992px | disetujui (F1b di-OK pemilik) |
| Login/register/reset layar penuh: banner kiri, form kanan | login dibuat (belum direview); register/reset belum (F5) |
| Kepadatan nyaman, tumpukan font sistem Bootstrap, nama produk sementara "Zago" | ASUMSI; pertanyaan F0 (referensi visual, merek, kepadatan) tidak dijawab pemilik |
| `theme.css` DIHASILKAN oleh `design/tools/gen-theme.py` (sumber palet/status, jembatan `--bs-*` dengan padanan Sass di komentar, 76 pasangan kontras WCAG AA) | diterima sebagai cara kerja; belum ditinjau visual |
| Tanpa `style=`, tanpa handler `on*` inline, tanpa warna mentah di HTML; perilaku lewat atribut `data-*` | aturan prompt, dipatuhi |

## 3. Halaman dan state

| Berkas | Isi | Status |
|---|---|---|
| `design/components.html` | tipografi, tombol, formulir, badge, alert, tabel, paginasi | dimuat di browser pemilik (log server GET 200); catatan visual TIDAK ada |
| `design/shell.html` | shell: minibar/sidebar, navbar, pemilih organisasi (DATA CONTOH), menu pengguna (tema, bahasa, keluar) | di-OK pemilik; tangkapan layar tidak ditempel |
| `design/login.html` | 10 state via `?state=`: normal, loading, validation, invalid, suspended, locked, rate (hitung mundur, `?retry=`), expired, error, success | diserahkan (`design-f1c-v1.mbox`); BELUM dikonfirmasi diterapkan atau direview |
| `design/index.html` | galeri tautan | ada, tautan ke halaman di atas |

## 4. Patch dan commit (urutan di laptop `~/projects/zago-core-m03`)

`m04-done 6a14f87` -> `git am design-f1a-v1.mbox` (sha256 16 kar: `134ed3aef378fa1d`) -> `ea7c0b1` vendor Bootstrap -> `git am design-f1b-v1.mbox`
(`e300ac238991c504`) -> `721f3de` sprite ikon -> `design-f1c-v1.mbox` (`793aff3692878b52`; belum ada konfirmasi diterapkan) -> patch dokumen checkpoint ini.
Hash commit F1a/F1b di laptop tidak ditempel. `git push`: keluaran TIDAK ditempel untuk satu pun patch; anggap belum di-push sampai dikonfirmasi.
Penulis commit pada mbox: `Claude <noreply@anthropic.com>`.

## 5. Bukti terakhir (apa adanya)

Ditempel pemilik: `git status/log/tag/ls-files` (580 berkas, tag `m04-done`), `node v24.21.0`, `npm view bootstrap` (5.3.9; dist-tags: latest 5.3.9, next 6.0.0-alpha.1),
`npm view bootstrap-icons version` (1.13.2), `check-theme-vars OK (105 variabel --bs-*)`, `npm view bootstrap@5.3.9 dist.integrity`
(`sha512-20cU0rcfw8WdxtVbmfU+xT2EjvVTw9Ror/h1Qvd7DXCujRg8OO9mdBCsmTHdJFf3BuadmBNJtgIuUXsBqSkk/w==`), sha256 `bootstrap.min.css`
`9caac977a0af0c2229abd397eee3345899c4020da2a1a0b711c1cc543fe3719d`, `bootstrap.bundle.min.js` `4ad2ad30a7df1fa0a615c4cfeb1aeffbe92019a8dff0262c99464acbd56dca5c`,
`build-icons.py` (12 ikon, lisensi MIT), commit vendor dan sprite, server `python3 -m http.server -d design 8787` memuat `index.html` dan `components.html`.
Di sandbox (bukan dari laptop): `git am` berurutan zip -> F1a -> F1b -> F1c, pohon identik dengan pohon kerja, `lint-structure OK`, `node --check`, paritas kunci i18n, tag HTML seimbang,
76 pasangan kontras lolos.
TIDAK ada: tangkapan layar atau catatan visual apa pun; pemeriksaan lebar 360/768/1280; pengujian keyboard; hasil membuka `login.html`; `git push`; `git fsck`.

## 6. Temuan kontrak yang mengubah UX (swagger.json dan docs, dibaca di chat ini)

- `login` dan `auth/me` TIDAK mengembalikan daftar izin efektif (docs/11 §6 menyebutnya) dan hanya memuat `organization_id`, tanpa nama organisasi atau keanggotaan: sumber data `can()` dan pemilih organisasi belum ada (endpoint M05).
- Login: 400 `validation_failed`, 401 `invalid_credentials`, 403 `account_suspended` atau `csrf_failed`, 423 `account_locked` (hanya `users.status='locked'`), 429 `rate_limit_exceeded` + `Retry-After`.
  Kunci sementara akun adalah 401 `invalid_credentials` (bukan 423/429); `account_suspended`/`account_locked` hanya setelah password benar. 429 dapat terjadi bahkan bila password benar.
- Semua kegagalan sesi adalah 401 `authentication_required` (`session_expired`/`session_revoked` dicadangkan, tidak dikirim): satu alur global dengan satu pesan.
- Kebijakan password: minimal 12 karakter tanpa aturan komposisi (docs/05 §4.1); nilai bawaan di `config.go` dan batas panjang maksimum TIDAK diperiksa; galat server `too_short | too_long | same_as_current` pada `new_password`.
- Daftar assignment hanya memuat `subject_id` (UUID); tidak ada `GET /policies/{id}` dan tidak ada hapus policy (nonaktifkan saja); `POST /role-assignments` memerlukan `Idempotency-Key`.
- Simulator mengembalikan kode stabil: `reason` terlihat setidaknya `granted`, `no_permission`, `scope_mismatch`, `policy_denied`, `policy_evaluation_failed` (daftar lengkap diverifikasi di F4);
  `level`: `platform | organization | group | resource | own`. Identitas platform memanggil simulate -> 403 `scope_mismatch`.
- Registri galat auth juga memuat `mfa_required`, `invalid_mfa_code`, `invalid_reset_token`, `invalid_invitation`; belum didesain.

## 7. Kekurangan dan revisi tertunda

1. Komponen F1 yang BELUM dibuat: dialog (pakai `<dialog>` native), toast, tooltip, skeleton, empty/error state; dashboard awal (halaman 3); pola "sembunyikan vs nonaktifkan dengan alasan umum"; pesan 403 seragam;
   pesan dan CTA `scope_mismatch` ("masuk ke organisasi dulu"); indikator konteks "Platform" hanya chip statis.
2. Popup minibar belum bisa ditutup dengan Esc selama kursor di atasnya (WCAG 1.4.13).
3. Centang, radio, switch di mode gelap belum pernah dilihat (tanda centang gelap di atas primer pastel; switch tidak ditimpa).
4. `forgot-password.html` belum ada; tautan "Lupa password?" di login menuju berkas itu (F5).
5. Login belum menangani `csrf_failed` (403) dan MFA.
6. Bilah pengalih state (`states.js`) berlabel Indonesia, tidak lewat i18n (chrome demo saja).
7. Badge primer terang Lagoon (4,64) dan Ember (4,65) dekat batas kontras 4,5.
8. `DESIGN-HANDOFF.md` belum ada (F6): padanan Sass ada di komentar `theme.css`, perilaku komponen berperilaku belum didokumentasikan.

## 8. Keputusan terbuka (perlu jawaban pemilik)

1. Referensi visual, merek, kepadatan (nyaman vs padat): diasumsikan, belum dikonfirmasi.
2. Register publik atau undangan saja: swagger tampaknya tidak punya endpoint registrasi (periksa ulang di F5); sementara login menulis "akses melalui undangan".
3. Cakupan MFA; perilaku `csrf_failed` di login.
4. Sumber izin efektif untuk UI (tambah ke `auth/me`/endpoint baru, atau hanya mengandalkan 403) dan endpoint daftar organisasi (M05): ditandai "contract-gap" di handoff.
5. Pindah halaman ke `design/pages/` atau tetap datar.

## 9. Rencana fase berikut

F1d (kekurangan §7.1 + perbaikan hasil review F1a/F1c) -> F2 Security (sesi aktif, ganti password, riwayat peristiwa login) -> F3 Roles + editor permission (◆CP) -> F4 Assignments, Policies, Simulator -> F5 halaman sistem + contract-first -> F6 `DESIGN-HANDOFF.md` dan galeri final.
Setiap fase: usulan singkat -> patch kecil -> tinjau -> revisi -> push.

## 10. Catatan proses (agar tidak terulang)

- Bukti patch di sandbox: salinan bersih zip + `git init` + `git am` semua mbox berurutan, `diff -r -x .git` terhadap pohon kerja, `git diff --cached --check` (whitespace; `.editorconfig` melarang spasi akhir selain `.md`), `bash scripts/lint-structure.sh`.
- Nama mbox: `design-<fase>-vN.mbox`; hash 16 karakter dari `sha256sum`; berkas diserahkan lewat present_files; satu perintah per blok kode.
- `.gitignore` membuang `dist/`, `data/`, `*.zip`; `scripts/repo-zip.sh` membuang `dist/*`, `*/dist/*`, `data/*`, `*.zip`: jangan memakai nama folder itu di `design/`.
- Sandbox tanpa jaringan dan tanpa browser: berkas vendor, ikon, dan render ada di sisi pemilik.
