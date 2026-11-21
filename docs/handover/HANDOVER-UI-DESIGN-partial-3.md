# HANDOVER-UI-DESIGN-partial-3 (checkpoint ketiga, fase desain UI/UX)

Dibuat di chat desain #3 pada ◆CP setelah F3b (F3 selesai). Prompt awal: `docs/handover/PROMPT-UI-DESIGN-start.md`.
Handover sebelumnya: `HANDOVER-UI-DESIGN-partial-1.md`, `HANDOVER-UI-DESIGN-partial-2.md`. Prompt lanjutan: `docs/handover/PROMPT-UI-DESIGN-partial-4.md`.

## 1. Ringkasan

Prototipe statis (HTML + CSS + Bootstrap 5.3.9, tanpa build, tanpa framework JS) di `design/`, branch `design/ui-prototype` (dari `m04-done` = `6a14f87` = `main`).
Selesai dan diserahkan sebagai patch: F1d, F2, F3a, dan di chat ini F3b (`role-detail.html`; pemilik menjawab "OK" setelah diberi tahu bahwa belum ada bukti visual). **F3 selesai.**
Belum dikerjakan: F4 (Role assignments, Policies, Simulator), F5 (halaman sistem + contract-first), F6 (`DESIGN-HANDOFF.md` + galeri final).
Tidak ada berkas di `backend/**`, `apps/**`, `packages/**` yang disentuh. TIDAK ADA tangkapan layar atau catatan visual yang pernah diterima untuk F1a-F3b.

## 2. Keputusan desain dan status

| Keputusan | Status |
|---|---|
| Keputusan partial-1 dan partial-2 (tiga tema, terang/gelap/otomatis, id-ID dan en-US, shell minibar/sidebar, Bootstrap lokal, Bootstrap Icons, login layar penuh, tanpa build, merek "Zago", kepadatan nyaman, login "akses melalui undangan", MFA dan `csrf_failed` login ke F5, izin efektif UI dan daftar organisasi = contract-gap, halaman datar di `design/`, 4 keputusan Security, F3 dua patch, arsip lewat dialog biasa) | tetap disetujui |
| Editor permission = DAFTAR CENTANG per modul > resource (bukan tabel matriks seperti usulan awal) | DEVIASI; pemilik tidak menjawab pertanyaan ini secara eksplisit; diteruskan dengan asumsi ("OK mulai" atas usulan yang memuat asumsi itu). F3b memakai ulang editor yang sama |
| F3b: bilah simpan lengket hanya untuk permission; detail (nama/deskripsi) punya tombol sendiri (PATCH dan PUT adalah dua permintaan terpisah) | disetujui bersama patch ("OK"), belum direview visual |
| F3b: state tambahan `viewer` (baca-saja tanpa izin ubah, pesan umum tanpa sebab) dan `validation` (nama kosong) di luar daftar handover partial-2 | disetujui bersama patch ("OK") |
| F3b: peran sistem dan terarsip tampil baca-saja lewat `fieldset disabled` + checkbox `disabled`, penjelasan di blok info (`role_immutable`, `role_archived`) | disetujui bersama patch ("OK") |
| Pola izin (nonaktifkan = `.btn.disabled` + `aria-disabled` + alasan umum; sembunyikan = tidak dirender; 403 seragam) | diusulkan di F1d, belum direview visual |

## 3. Halaman dan state (semuanya via `?state=`; belum ada yang DIREVIEW visual)

| Berkas | State | Status |
|---|---|---|
| `components.html`, `dashboard.html`, `login.html`, `shell.html` | seperti partial-2 | diserahkan |
| `security-sessions.html`, `security-password.html`, `security-events.html` | seperti partial-2 | diserahkan (F2) |
| `roles.html` | normal, paged, archived, platform, viewer, loading, empty, error, forbidden; tautan peran sistem kini `role-detail.html?state=system`, Kasir `?state=archived` | diserahkan (F3a, diubah di F3b) |
| `role-new.html` | normal, loading, validation, slugtaken, invscope, permnf, grant, scope, csrf, error, forbidden, success | diserahkan (F3a) |
| `role-detail.html` | normal, loading, notfound, system, archived, viewer, validation, grant, permnf, scope, csrf, saveerr, error, forbidden, success | diserahkan (F3b) |

Data contoh F3b: normal = "Keuangan" (`keuangan`, jenis Organisasi, aktif, 3 permission), system = "Organization Admin" (semua kode inti tercentang, contoh modul tidak), archived = "Kasir". Nama/slug/badge/permission awal diisi `roles.js` dari tabel kecil per state (bukan dari markup).
Perilaku F3b (di `roles.js`, blok "Detail peran"): bilah simpan muncul bila himpunan permission berbeda dari baseline; wilayah `role="status"` tersembunyi mengumumkan "Perubahan belum disimpan"; Simpan memakai spinner 700 ms lalu toast sukses dan fokus ke judul "Permission"; himpunan kosong membuka `dlg-empty`; Batalkan mengembalikan baseline;
Arsipkan membuka `dlg-archive`, konfirmasi memindah ke `?state=archived` (demo; toast tidak ditampilkan); state `grant`/`permnf`/`csrf`/`saveerr` memuat percobaan simpan yang ditolak (perubahan masih ada, bilah tampil, alert di atas dan difokuskan).
Berkas pendukung diubah: `components.css` (`.z-savebar`, `scroll-padding-bottom` via `html:has(...)`), `states.js` (label `notfound`, `system`, `saveerr`), `i18n.js` (+32 kunci `rd.*` dan `al.rdok.*`, identik id-ID/en-US), `index.html` (tautan F3b). `design/README.md` BELUM memuat daftar halaman (lihat §7).

## 4. Patch dan commit (laptop `~/projects/zago-core-m03`, branch `design/ui-prototype`)

| Patch | sha256 (16 kar.) | Commit di laptop | Push |
|---|---|---|---|
| `design-f1d-v1.mbox`, sprite 18 ikon, `design-f2-v1.mbox`, `design-f3a-v1.mbox`, `design-checkpoint2-v1.mbox` | seperti partial-2 | `78b9429`, `f92555f`, `808ffd7`, `381a08c`, `a6e862b` | di-push (origin = `a6e862b` saat checkpoint 3 dimulai) |
| `design-f3b-v1.mbox` | `05099e2a64402f1d` (cocok dengan hitungan sandbox) | `6a9231e` | BELUM di-push saat dokumen ini ditulis (`ahead 1`); di-push bersama patch dokumen ini |
| `design-checkpoint3-v1.mbox` (dokumen ini) | lihat pesan chat | lihat pesan chat | lihat pesan chat |

Penulis commit di mbox F3b dan checkpoint 3: `Claude <noreply@anthropic.com>` (identitas pemilik tidak diketahui sandbox); ganti dengan `git commit --amend --reset-author --no-edit` bila diinginkan SEBELUM push.

## 5. Bukti terakhir (apa adanya)

Ditempel pemilik: `git status -sb && git log --oneline -10 --decorate && git tag --list 'm04-done' && git ls-files | wc -l` (branch sejajar origin di `a6e862b`, tag `m04-done` ada, 615 berkas); `sha256sum` F3b (cocok), `git am` F3b bersih, `git status -sb` (`ahead 1`); jawaban "OK mulai" (rencana) dan "OK" (F3b).
Di sandbox (bukan dari laptop): zip memuat 615 berkas; `lint-structure OK`; `git am` F3b pada salinan bersih zip, `diff -r -x .git` identik dengan pohon kerja; `node --check` semua JS; paritas i18n id/en identik (288 menurut regex `'kunci': '`, dari 256 sebelum F3b; angka 258 di partial-2 dihitung dengan cara lain);
nol kunci i18n hilang, nol id ganda, nol rujukan aria/`for` putus, nol ikon tak dikenal (terhadap `NAMES`), tag HTML seimbang, nol `style=`/handler `on*` di `role-detail.html`, `roles.html`, `index.html`, `role-new.html`; `git diff --check` bersih; `gen-theme.py`: 76 pasangan, 0 gagal, `theme.css` tidak berubah.
Satu bug ditemukan sendiri dan diperbaiki sebelum patch dibuat (penghitung editor tidak ikut preset awal).
TIDAK ada: tangkapan layar atau catatan visual (termasuk mode gelap, tiga tema, 360/768/1280); eksekusi JS di browser (bilah simpan, dialog, fokus); uji keyboard; uji pembaca layar; `git push` F3b; `git fsck`; jumlah berkas zip setelah F3b.

## 6. Temuan kontrak (swagger.json dan docs)

Dari partial-2 (§6) tetap berlaku. Baru di F3b:
- `GET /roles/{id}` = `{id, slug, name, description, type, status, is_system, group_id (null kecuali tipe group), permissions: []string}`; `permissions` selalu ada (irisan kosong). `GET /roles/{id}/permissions` (`{permissions}`) tidak dipakai prototipe karena detail sudah memuatnya.
- `PATCH /roles/{id}`: hanya field yang dikirim berubah; deskripsi kosong menghapus; kode: 400 `validation_failed`, 403 (`permission_denied`, `scope_mismatch`, `grant_exceeds_authority`, `csrf_failed`), 404 `role_not_found`, 409 (`role_immutable`, `role_archived`).
- `PUT /roles/{id}/permissions`: body `{permissions: [...]}` wajib ada (kosong sah = cabut semua; hilang/null ditolak 400); 422 `permission_not_found`; **authority ceiling dihitung atas gabungan himpunan LAMA dan BARU**, jadi `grant_exceeds_authority` mungkin muncul walau perubahan hanya mengurangi (pesan `rd.grant.m` menjelaskan ini).
- `DELETE /roles/{id}` = arsip, 204; 409 `role_immutable`/`role_archived`. Tidak ada endpoint pemulihan.
Penunjuk untuk F4 (dari daftar path swagger; bidang dan kode galat BELUM dibaca, verifikasi di F4): `GET/POST /role-assignments` (query `status`, `role_id`, `subject_id`, `limit`, `cursor`), `DELETE /role-assignments/{id}` (204; 404, 409);
`GET/POST /policies` (query `status`, `page`, `per_page`; POST 409), `PATCH /policies/{id}` (404, 409); TIDAK ada `GET /policies/{id}` dan TIDAK ada `DELETE /policies/...` di swagger: detail policy berasal dari daftar, "hapus" = ubah status (periksa). `POST /permissions/simulate` (200, 400, 401, 403). `GET /permissions` (katalog; `page`, `per_page`).
Kode 409 dari docs/08 (M04): `role_slug_taken`, `role_immutable`, `role_archived`, `assignment_exists`, `assignment_revoked`, `last_super_admin`, `policy_code_taken`, `policy_limit_reached`; lain: `grant_exceeds_authority`, `invalid_scope`, `scope_mismatch`, `policy_evaluation_failed`, `field_permission_denied`, `permission_not_found` 422, `role_not_found` 404.

## 7. Kekurangan dan revisi tertunda

1. SEMUA review visual (F1a-F3b): mode gelap, tiga tema, 360/768/1280, tata letak editor permission (daftar centang), tata letak detail peran.
2. F3b belum dijalankan di browser: bilah simpan lengket (jarak dari bilah state demo 3,5rem; `html:has()` untuk `scroll-padding-bottom` agar fokus keyboard tidak tertutup bilah, WCAG 2.4.11), dialog, fokus ke judul "Permission" setelah simpan atau batal, state `validation`.
3. F3b: tombol "Simpan detail" selalu aktif (tidak menunggu perubahan); kegagalan sebagian (PATCH berhasil, PUT gagal) dan konflik edit bersamaan tidak digambar (konkurensi/ETag tidak diperiksa di kontrak); konfirmasi arsip di demo hanya memindah ke `?state=archived` tanpa toast; tidak ada tautan kembali ke daftar selain breadcrumb.
4. Identitas Platform membuka peran organisasi = 404 `role_not_found` (klaim handover partial-2 §6, tidak diverifikasi ulang): digambar sebagai `notfound`; `scope` (pilih organisasi dulu) dipakai untuk `scope_mismatch`.
5. Deskripsi permission di editor adalah CONTOH buatan; nilai `module` kode inti belum diverifikasi (diasumsikan `core`); penghitung editor "N dari M" tidak ikut berganti bahasa sampai interaksi berikutnya; tanggal Security format netral; parsing `user_agent` hanya contoh.
6. Fokus setelah mencabut satu sesi (F2) belum diperagakan; toast `role="alert"` dan skeleton belum diuji pembaca layar; `<dialog>` tidak menutup popup Bootstrap yang sedang terbuka.
7. Dari partial-1/2 yang masih terbuka: `forgot-password.html` (F5); login belum menangani `csrf_failed` dan MFA; label bilah state (`states.js`) berbahasa Indonesia tanpa i18n; badge primer terang Lagoon (4,64) dan Ember (4,65) dekat batas 4,5; `Retry-After` ganti password tidak ada di swagger; `DESIGN-HANDOFF.md` belum ada (F6).
8. `design/README.md` belum memuat daftar halaman F1d-F3b (perbarui di F6 atau patch kecil terpisah).

## 8. Keputusan terbuka (perlu jawaban pemilik)

1. Editor permission: daftar centang (dibuat, diteruskan dengan asumsi) atau tabel matriks.
2. Referensi visual dan merek (diasumsikan); register publik vs undangan (periksa swagger di F5); cakupan MFA dan `csrf_failed` di login.
3. Sumber izin efektif untuk UI dan endpoint daftar organisasi (M05): "contract-gap" di handoff.
4. Parsing `user_agent`: klien atau server; makna `auth_level`; nilai `module` katalog inti.
5. Hasil review visual F1a-F3b (belum ada sama sekali).
6. Identitas penulis commit untuk patch dari sandbox.

## 9. Rencana fase berikut

F4, kemungkinan lebih dari satu patch (usulkan pemecahan di awal, mis. F4a Assignments, F4b Policies, F4c Simulator): baca HANYA kontrak yang relevan (swagger path di §6, docs/05, 06, 08) sebelum menggambar; jangan menebak bidang.
- Assignments: daftar dengan cursor "muat lagi" (pola seperti `security-events.html`), filter status active/revoked/expired/all, role, subjek; buat (role, subjek tipe user sebagai input UUID karena daftar user belum ada, scope platform/organisasi/grup, kedaluwarsa opsional); cabut dengan dialog; galat `assignment_exists`, `assignment_revoked`, `last_super_admin`, `grant_exceeds_authority`, `invalid_scope`.
- Policies: daftar offset, buat/ubah (kode, nama, efek deny/allow, aksi dari katalog, ekspresi, status), editor ekspresi dengan petunjuk bahasa (akar `resource`/`current_user`/`scope`; fungsi `lower`/`len`/`startsWith`/`endsWith`/`contains`), `policy_limit_reached`, galat ekspresi per field, aksi `policy.*` tidak dapat dibatasi.
- Simulator: masukan `user_id`, aksi, grup aktif opsional, resource opsional beserta atribut; hasil ALLOW/DENY + `reason` + level + `policy_code` + `deferred_policies`; SATU-SATUNYA tempat alasan penolakan boleh tampil.
Lalu F5 (halaman sistem 401/403/404/5xx/offline/error boundary; contract-first: lupa password, atur ulang password, terima undangan; MFA dan `csrf_failed` login), F6 (`DESIGN-HANDOFF.md`, galeri final, README, prompt M10). Checkpoint berikutnya: `HANDOVER-UI-DESIGN-partial-4.md` dan `PROMPT-UI-DESIGN-partial-5.md`. Setiap fase: usulan singkat -> patch kecil -> tinjau -> revisi -> push.

## 10. Catatan proses (agar tidak terulang)

- Semua catatan §10 partial-2 tetap berlaku (bukti patch di sandbox pada salinan bersih zip + `git init` + baseline + `git am`; halaman dari `shell.html` lewat skrip Python ber-assert; `data-when` memakai atribut `hidden`, tanpa `d-*` pada elemen yang sama; ikon baru lewat `NAMES` + pemilik membangun sprite; nama mbox `design-<fase>-vN.mbox`; `.gitignore` membuang `dist/`, `data/`, `*.zip`).
- Teknik hemat untuk halaman baru: ambil blok yang sama dari halaman lama lewat indeks string (mis. editor permission dari `role-new.html`) alih-alih menulis ulang; tambahkan satu blok `if ($('id-form'))` di `roles.js`/`security.js` sebelum `return` dini milik halaman lain.
- Pemeriksa statis (paritas i18n, kunci terpakai, id ganda, rujukan aria, ikon, tag seimbang, `style=`/`on*`) ditulis ulang di sandbox tiap chat; kunci dinamis (`rl.st.`+nilai, `rl.type.`+nilai) muncul sebagai positif palsu.
- Sandbox tanpa jaringan dan tanpa browser; penilaian render ada di sisi pemilik.
