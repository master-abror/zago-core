# HANDOVER-UI-DESIGN-partial-2 (checkpoint kedua, fase desain UI/UX)

Dibuat di chat desain #2 atas penilaian konteks hampir habis (checkpoint DINI: sebelum F3b). Prompt awal: `docs/handover/PROMPT-UI-DESIGN-start.md`.
Handover sebelumnya: `HANDOVER-UI-DESIGN-partial-1.md`. Prompt lanjutan: `docs/handover/PROMPT-UI-DESIGN-partial-3.md`.

## 1. Ringkasan

Prototipe statis (HTML + CSS + Bootstrap 5.3.9, tanpa build, tanpa framework JS) di `design/`, branch `design/ui-prototype` (dari `m04-done` = `6a14f87` = `main`).
Selesai dan diserahkan sebagai patch: F1d (komponen + pola izin + dashboard), F2 (Security, 3 halaman), F3a (Roles: daftar, editor permission, buat peran).
**F3b BELUM dikerjakan** (detail peran; §9). Tidak ada berkas di `backend/**`, `apps/**`, `packages/**` yang disentuh. TIDAK ADA tangkapan layar atau catatan visual yang pernah diterima untuk F1a-F3a.

## 2. Keputusan desain dan status

| Keputusan | Status |
|---|---|
| Keputusan partial-1 (tiga tema, terang/gelap/otomatis, id-ID dan en-US, shell minibar/sidebar, Bootstrap lokal, Bootstrap Icons, login layar penuh, tanpa build) | tetap disetujui |
| Asumsi F1d: merek "Zago", kepadatan nyaman, login menulis "akses melalui undangan", MFA dan `csrf_failed` di login ditunda ke F5, izin efektif UI dan daftar organisasi = contract-gap, halaman tetap datar di `design/` | disetujui ("setuju") |
| F1d: semua kekurangan §7.1 partial-1 dalam satu patch; review visual digabung di akhir | disetujui |
| Security (F2), 4 keputusan: baris sesi saat ini tanpa tombol cabut (keluar lewat menu pengguna); `user_agent` ditampilkan "Browser · Sistem operasi"; ganti password dengan tombol tampil/sembunyi tanpa field konfirmasi; cabut satu/semua lain lewat `<dialog>` + toast | disetujui ("OK") |
| F3: dua patch (F3a lalu F3b); arsip lewat dialog konfirmasi biasa (tanpa mengetik slug) | disetujui ("OK") |
| Editor permission = DAFTAR CENTANG per modul > resource dengan deskripsi terlihat (bukan tabel matriks seperti usulan) karena aksi tiap resource tidak seragam (`read_own`, `manage_members`, `simulate`) | DEVIASI dari usulan; pemilik belum menanggapi |
| Pola izin: nonaktifkan = `.btn.disabled` + `aria-disabled="true"` + alasan umum terlihat via `aria-describedby`; sembunyikan = tidak dirender; 403 seragam tanpa penyebab dan tanpa janji solusi | diusulkan di F1d, belum direview visual |

## 3. Halaman dan state (semuanya via `?state=`; belum ada yang DIREVIEW visual)

| Berkas | State | Status |
|---|---|---|
| `components.html` | + dialog, toast, tooltip, skeleton, empty/error/403/scope, pola izin | diserahkan (F1d) |
| `dashboard.html` | normal (konteks + 5 pintasan, tanpa data palsu), loading, error, forbidden | diserahkan (F1d) |
| `security-sessions.html` | normal, loading, error, csrf, alone, success | diserahkan (F2) |
| `security-password.html` | normal, loading, validation, short, long, same, wrongpw, rate (`?retry=`), csrf, error, success | diserahkan (F2) |
| `security-events.html` | normal, loading, empty, error, more, moreerr, end | diserahkan (F2) |
| `roles.html` | normal, paged, archived, platform, viewer, loading, empty, error, forbidden | diserahkan (F3a) |
| `role-new.html` | normal, loading, validation, slugtaken, invscope, permnf, grant, scope, csrf, error, forbidden, success | diserahkan (F3a) |
| `login.html`, `shell.html`, `index.html` | tidak berubah selain tautan galeri; `shell.html` memuat `ui.js` | seperti partial-1 |

Berkas pendukung baru: `ui.js` (dialog, toast `ZUI.toast(tone,key,params)`, tooltip, Esc untuk tooltip dan popup minibar), `security.js`, `roles.js` (editor permission + form buat peran),
`states.js` (sekarang juga menampilkan/menyembunyikan blok `data-when="state1 state2"` lewat atribut `hidden`), `components.css` (F1d, F3a), `i18n.js` (258 kunci identik id-ID/en-US), sprite 18 ikon.
`design/README.md` dan `design/index.html`: galeri memuat tautan F1d-F3a; README belum memuat daftar halaman baru.

## 4. Patch dan commit (laptop `~/projects/zago-core-m03`, branch `design/ui-prototype`)

| Patch | sha256 (16 kar.) | Commit di laptop | Push |
|---|---|---|---|
| `design-f1d-v1.mbox` | `0bf4b299833f13ca` | `78b9429` | di-push (`b6cbca0..f92555f`) |
| sprite 18 ikon (dibuat pemilik: `npm pack bootstrap-icons@1.13.2 --pack-destination /tmp`, lalu `python3 design/tools/build-icons.py /tmp/bootstrap-icons-1.13.2.tgz`) | n/a | `f92555f` | di-push |
| `design-f2-v1.mbox` | `349182a3c4f38cc5` | `808ffd7` | di-push (origin/design/ui-prototype = `808ffd7`) |
| `design-f3a-v1.mbox` | `f1ca6ef7787ae844` | `381a08c` | BELUM di-push (`ahead 1` saat checkpoint dibuat) |
| `design-checkpoint2-v1.mbox` (dokumen ini) | lihat pesan chat | lihat pesan chat | lihat pesan chat |

## 5. Bukti terakhir (apa adanya)

Ditempel pemilik: keluaran `git push` (`b6cbca0..f92555f`), `build-icons.py` (18 ikon), `sha256sum` + `git am` + `git status/log` untuk F2 dan F3a (hash cocok; F3a `ahead 1`); jawaban "OK" untuk rencana F2 dan F3, "ya"/"setuju" untuk F1d. Nama branch: `design/ui-prototype`.
Di sandbox (bukan dari laptop): `git am` berurutan zip -> F1d -> F2 -> F3a, pohon identik (`diff -r -x .git`), `lint-structure OK`; `node --check` semua JS; paritas kunci i18n 258/258; tag HTML seimbang; tanpa id ganda; semua `aria-describedby`/`for` merujuk id ada;
tanpa `style=` dan handler `on*` inline; `git diff --cached --check` bersih; `theme.css` tidak berubah (76 pasangan kontras lolos); 45 checkbox permission (41 kode inti + 4 contoh modul) dengan kode unik.
TIDAK ada: tangkapan layar atau catatan visual (termasuk mode gelap, tiga tema, lebar 360/768/1280); uji keyboard; uji pembaca layar; `git push` untuk F3a; `git fsck`; jumlah berkas zip setelah F1d.

## 6. Temuan kontrak baru yang mengubah UX (swagger.json dan docs, dibaca di chat ini)

- Sesi (`GET /security/sessions`): `id`, `current`, `auth_level`, `created_at`, `last_activity_at`, `expires_at`, `ip_address`, `user_agent`, `organization_id` (UUID atau kosong = Platform; TANPA nama organisasi, TANPA nama perangkat). `auth_level` tidak ditampilkan (nilai tak terdokumentasi).
  `DELETE /sessions/{id}` 204; 404 seragam (salah id, milik orang lain, sudah dicabut); mencabut sesi saat ini = logout. `POST /sessions/revoke-others` -> `{revoked: n}`.
- Ganti password (`POST /security/password`): 401 `invalid_credentials` = password saat ini salah (bukan sesi habis; bedakan dari `authentication_required`); galat `new_password`: `too_short`, `too_long`, `same_as_current`; 429 `rate_limit_exceeded` (5/menit);
  `Retry-After` hanya tertulis di docs/05 (baris ~539, rate rules), TIDAK di swagger endpoint ini: UI punya cadangan 30 detik. Sukses: sesi dirotasi, sesi lain dicabut (`revoked_other_sessions`). Tidak ada field konfirmasi password.
- Peristiwa (`GET /security/events`): hanya milik pemanggil, terbaru dulu, cursor; `meta.pagination = {next_cursor, has_more}` (`next_cursor` null di halaman terakhir; default 50, maks 100). Field: `event_type`, `result` (`success|failure`), `created_at`, `ip_address`, `user_agent`, `session_id`, `organization_id`.
  10 tipe M03: `auth.login.success|failed`, `auth.logout`, `auth.session.refreshed`, `auth.organization.switched`, `auth.session.revoked`, `auth.sessions.revoked_others`, `auth.password.changed|change_failed`, `auth.account.locked`; tipe lain -> label cadangan.
- Endpoint Security hanya data milik sendiri: 403 `permission_denied` tidak berlaku; yang mungkin hanya `csrf_failed`.
- Roles: tipe di DB `system|organization|group|custom` (hanya organisasi dan grup ditawarkan di form buat); slug `^[a-z0-9]+(-[a-z0-9]+)*$` maks 100 karakter, tidak dapat diubah, empat slug sistem dicadangkan: `super-admin`, `organization-admin`, `group-admin`, `member`.
  `GET /roles` offset (`page`, `per_page` bawaan 25 maks 100, `status` = `active` bawaan | `archived` | `all`), `meta.pagination = {page, per_page, total}`; semua parameter tak valid dilaporkan sekaligus (400 `validation_failed`). Identitas platform hanya melihat role sistem.
  `PATCH` hanya nama/deskripsi; `PUT /roles/{id}/permissions` mengganti himpunan PENUH (kosong = cabut semua); role sistem: 409 `role_immutable`; terarsip: 409 `role_archived` (tidak dapat dipulihkan, assignment berhenti berlaku);
  409 `role_slug_taken`; 422 `permission_not_found`; 403 `grant_exceeds_authority`, `scope_mismatch`; 400 `invalid_scope`; role organisasi lain = 404 `role_not_found` (tak dapat dibedakan dari tidak ada).
- Katalog permission core: 41 kode di `backend/internal/permission/catalog.go` (`<resource>.<action>`, resource dapat bertitik mis. `organization.member`); `GET /permissions` memberi `module`, `resource`, `action`, `code`, `description`, `is_system`.
  Nilai `module` untuk kode inti BELUM diverifikasi (diasumsikan `core`); deskripsi di prototipe adalah CONTOH buatan (server yang mengirim).

## 7. Kekurangan dan revisi tertunda

1. F3b seluruhnya (detail peran; §9). Tautan nama peran di `roles.html` menuju `role-detail.html` yang belum ada.
2. SEMUA review visual (F1a-F3a): mode gelap, tiga tema, 360/768/1280, centang/radio/switch gelap, tata letak editor permission.
3. Fokus setelah mencabut satu sesi: baris hilang, fokus native kembali ke pemicu yang sudah tidak ada; di SPA pindahkan fokus ke judul daftar (tidak diperagakan).
4. `Retry-After` ganti password tidak ada di swagger (hanya docs/05).
5. Penghitung editor permission "N dari M" tidak ikut berganti bahasa sampai interaksi berikutnya; tanggal di Security berformat netral `YYYY-MM-DD HH:MM` (SPA: `Intl` + zona waktu profil); parsing `user_agent` di prototipe hanya teks contoh; konteks organisasi `Org 7f3a…c91d` = contract-gap.
6. Toast `role="alert"` yang disisipkan lewat JS dan skeleton belum diuji dengan pembaca layar; dialog `<dialog>` tidak menutup popup Bootstrap yang sedang terbuka.
7. Dari partial-1 yang masih terbuka: `forgot-password.html` (F5); login belum menangani `csrf_failed` dan MFA; label bilah state (`states.js`) berbahasa Indonesia tanpa i18n; badge primer terang Lagoon (4,64) dan Ember (4,65) dekat batas 4,5; `DESIGN-HANDOFF.md` belum ada (F6).
8. `design/README.md` belum memuat daftar halaman F1d-F3a.

## 8. Keputusan terbuka (perlu jawaban pemilik)

1. Editor permission sebagai daftar centang (dibuat) atau tabel matriks (usulan awal).
2. Referensi visual dan merek (diasumsikan); register publik vs undangan (periksa swagger di F5); cakupan MFA dan `csrf_failed` di login.
3. Sumber izin efektif untuk UI dan endpoint daftar organisasi (M05): ditandai "contract-gap" di handoff.
4. Parsing `user_agent`: di klien (heuristik) atau server; makna `auth_level`; nilai `module` katalog inti.
5. Hasil review visual F1a-F3a (belum ada sama sekali).

## 9. Rencana fase berikut

F3b `role-detail.html`: header (nama, slug, badge jenis dan status), baca-saja untuk role sistem (penjelasan `role_immutable`) dan terarsip (`role_archived`) dengan editor `disabled`; ubah nama/deskripsi (PATCH); arsipkan (dialog: tidak dapat dipulihkan, assignment berhenti berlaku);
editor permission dengan bilah aksi lengket "Perubahan belum disimpan" (Simpan/Batal; himpunan kosong butuh dialog konfirmasi); state: normal, loading, 404 `role_not_found`, `role_immutable`, `role_archived`, `grant_exceeds_authority`, `permission_not_found`, `scope_mismatch`, `csrf_failed`, galat umum, 403 seragam, sukses.
Lalu ◆CP (F3 selesai; checkpoint berikutnya `HANDOVER-UI-DESIGN-partial-3.md` dan `PROMPT-UI-DESIGN-partial-4.md`), F4 (Assignments, Policies, Simulator), F5 (halaman sistem + contract-first), F6 (`DESIGN-HANDOFF.md` + galeri final). Setiap fase: usulan singkat -> patch kecil -> tinjau -> revisi -> push.

## 10. Catatan proses (agar tidak terulang)

- Bukti patch di sandbox: salinan bersih zip + `git init` + commit baseline + `git am` SEMUA mbox berurutan, `diff -r -x .git` terhadap pohon kerja, `git diff --cached --check`, `bash scripts/lint-structure.sh`. Sprite ikon buatan pemilik tidak ada di sandbox: patch tidak boleh menyentuhnya.
- Halaman baru dibangun dari `shell.html` lewat skrip Python ber-assert (jangkar muncul tepat sekali); string baru lewat blok kunci id-ID dan en-US sekaligus, lalu cek paritas, kunci terpakai, ikon dikenal (terhadap `NAMES` di `build-icons.py`), id ganda, rujukan aria.
- Blok `data-when` memakai atribut `hidden`; JANGAN taruh utilitas `d-*` (`!important`) pada elemen yang sama. Perubahan komposisi (mis. `d-flex`) lewat elemen dalam.
- Ikon baru: ubah `NAMES`, pemilik menjalankan `npm pack` + `build-icons.py` dan meng-commit sprite terpisah (`chore(design): sprite ... N ikon`) SEBELUM meninjau; patch yang memakai ikon baru dikirim lebih dulu.
- Nama mbox `design-<fase>-vN.mbox`; hash 16 karakter dari `sha256sum`; berkas lewat present_files; satu perintah per blok kode.
- `.gitignore` membuang `dist/`, `data/`, `*.zip`; `scripts/repo-zip.sh` membuang `dist/*`, `*/dist/*`, `data/*`, `*.zip`: jangan memakai nama folder itu di `design/`.
- Sandbox tanpa jaringan dan tanpa browser; penilaian render ada di sisi pemilik.
