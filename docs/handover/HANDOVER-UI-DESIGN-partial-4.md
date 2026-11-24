# HANDOVER-UI-DESIGN-partial-4 (checkpoint keempat, fase desain UI/UX)

Dibuat di chat desain #4 pada ◆CP setelah F4a (Assignments). Prompt awal: `docs/handover/PROMPT-UI-DESIGN-start.md`.
Handover sebelumnya: `HANDOVER-UI-DESIGN-partial-1.md` sampai `-3.md`. Prompt lanjutan: `docs/handover/PROMPT-UI-DESIGN-partial-5.md`.
Alasan checkpoint: banyak keluaran bacaan awal sesi sudah terpotong dari konteks (swagger, handover), padahal F4b butuh detail itu; menurut ketentuan, itu tanda konteks menipis, jadi F4b TIDAK dimulai.

## 1. Ringkasan

Prototipe statis (HTML + CSS + Bootstrap 5.3.9, tanpa build, tanpa framework JS) di `design/`, branch `design/ui-prototype` (dari `m04-done` = `main`).
Di chat ini: prasyarat diperiksa (tag `m04-done` ada dan di `main`; branch sejajar origin di `bfebef9` dengan 618 berkas), rencana F4 disetujui ("OK"), dan **F4a (Assignments) selesai dan dinyatakan "OK" oleh pemilik** (tanpa bukti visual; lihat §5, §7).
Belum dikerjakan: **F4b (Policies), F4c (Simulator), F5 (halaman sistem + contract-first), F6 (`DESIGN-HANDOFF.md` + galeri final).**
Tidak ada berkas di `backend/**`, `apps/**`, `packages/**` yang disentuh. TIDAK ADA tangkapan layar atau catatan visual yang pernah diterima untuk F1a-F4a.

## 2. Keputusan desain dan status

| Keputusan | Status |
|---|---|
| Semua keputusan partial-1 sampai partial-3 (tiga tema, terang/gelap/otomatis, id-ID dan en-US, shell minibar/sidebar, Bootstrap lokal 5.3.9, Bootstrap Icons 1.13.2 sprite, login layar penuh, tanpa build, merek "Zago", kepadatan nyaman, halaman datar di `design/`, 4 keputusan Security, F3 dua patch, arsip lewat dialog biasa, F3b) | tetap disetujui |
| Pemecahan F4: F4a Assignments, F4b Policies, F4c Simulator, masing-masing satu patch | disetujui ("OK") |
| Editor permission = daftar centang per modul > resource (bukan tabel matriks) | DEVIASI belum dijawab eksplisit; pemilik menjawab "OK" atas asumsi yang disebut ulang di chat ini; diteruskan |
| Penulis commit `Claude <noreply@anthropic.com>`; review visual F1d-F3b belum ada | asumsi bawaan disebut ulang dan di-"OK" pemilik; bukan jawaban eksplisit |
| F4a: daftar assignment menampilkan `subject_id` (UUID, `<code>`) karena respons tidak memuat nama user; subjek dan grup berupa input UUID (daftar user/grup belum ada) | disetujui bersama patch ("OK"), contract-gap |
| F4a: cabut lewat `<dialog>` biasa; hasil konfirmasi ditentukan state (demo); GALAT TAMPIL DI DALAM dialog (dialog tetap terbuka); sukses menyembunyikan baris, toast, fokus ke judul halaman | disetujui bersama patch ("OK") |
| F4a: tombol Cabut hanya untuk status `active` (tidak untuk kedaluwarsa/dicabut; kontrak tidak jelas apakah assignment kedaluwarsa yang belum dicabut boleh dicabut); pemanggil tanpa izin ubah: kolom Aksi dan tombol Beri role tidak dirender (pola F1d) + pesan umum tanpa sebab | disetujui bersama patch ("OK") |
| F4a: 404 `resource_not_found` dan 409 `assignment_revoked` pada cabut memakai SATU pesan ("sudah tidak berlaku") | disetujui bersama patch ("OK") |
| F4a: form beri role menyesuaikan identitas: Platform hanya cakupan Platform dan tanpa kedaluwarsa (kontrak: `expires_at` tidak sah di cakupan platform); tenant memilih Organisasi atau Grup; subjek hanya tipe `user` (API menerima juga `group`/`organization`; tidak digambar sesuai tugas) | disetujui bersama patch ("OK") |
| Pola izin F1d, pola cursor "muat lagi" (F2) dipakai ulang | seperti sebelumnya |

## 3. Halaman dan state (semuanya via `?state=`; belum ada yang DIREVIEW visual)

| Berkas | State | Status |
|---|---|---|
| `components.html`, `dashboard.html`, `login.html`, `shell.html`, `security-*.html`, `roles.html`, `role-new.html`, `role-detail.html` | seperti partial-3 (menu: tautan "Assignment" kini menuju `role-assignments.html` di 8 halaman yang punya menu) | diserahkan |
| `role-assignments.html` | normal, more, moreerr, end, badcursor, badfilter, revoked, lapsed, all, platform, viewer, loading, empty, nofilter, error, forbidden, revlast, revgone, revgrant, revcsrf, reverr (21) | diserahkan (F4a) |
| `role-assignment-new.html` | normal, platform, loading, validation, subjnf, grpnf, expinv, invscope, exists, rolearch, rolenf, grant, scope, csrf, idem, idemconf, error, forbidden, success (19) | diserahkan (F4a) |

Perilaku F4a (di `design/assignments.js`, satu berkas untuk kedua halaman; JS vanilla; di SPA Svelte menggantikan seluruhnya):
- Daftar: filter status/role/subjek (UUID divalidasi; tidak valid = galat pada field dan fokus); "Terapkan" memindah ke `?state=` demo (status -> `normal`/`revoked`/`lapsed`/`all`; role atau subjek terisi -> `nofilter`). `badcursor` = 400 `invalid_cursor`/`cursor_mismatch`: peringatan + "Mulai dari awal". Penghitung "{n} assignment ditampilkan" dihitung dari baris yang tampak (cursor tidak punya total).
- Cabut: `as-revoke` mengisi dialog (`role`, `subject`; baris platform menambah kalimat sesi), konfirmasi dengan spinner 700 ms; hasil menurut state: sukses (baris hilang, toast `as.revoked.ok`, fokus ke `#as-h1`), `revlast` (super-admin terakhir), `revgone` (404/409), `revgrant` (403), `revcsrf` (403, dengan tombol muat ulang), `reverr`.
- Form: platform menghapus opsi role khusus-organisasi (`data-org`), tenant menghapus opsi khusus-platform (`data-plat`); radio cakupan per identitas; input ID grup muncul untuk Grup; kedaluwarsa = kotak centang "Berlaku sampai dicabut" (bawaan aktif) lalu `datetime-local`. Kirim: validasi sisi klien (ringkasan alert + galat per field + fokus ke field pertama), lalu spinner dan `?state=success`. State hasil-server mengisi ulang isian yang valid dan menempelkan galat pada field yang disebut kontrak (`subject_id`, `group_id`, `expires_at`, role).
- Catatan implementasi SPA (komentar di JS): `POST /role-assignments` wajib `Idempotency-Key` per percobaan (kunci SAMA saat mencoba ulang isian yang sama); 409 `idempotency_in_progress` -> periksa daftar dulu; 422 `idempotency_key_conflict` -> muat ulang dan kunci baru.
Berkas pendukung diubah: `states.js` (label state baru), `i18n.js` (+95 kunci `as.*`, `an.*`, `btn.revoking`; identik id-ID/en-US), `index.html` (tautan F4a). `theme.css` dan `components.css` TIDAK berubah. `design/README.md` BELUM memuat daftar halaman (lihat §7).

## 4. Patch dan commit (laptop `~/projects/zago-core-m03`, branch `design/ui-prototype`)

| Patch | sha256 (16 kar.) | Commit di laptop | Push |
|---|---|---|---|
| `design-f3b-v1.mbox`, `design-checkpoint3-v1.mbox` | seperti partial-3 | `6a9231e`, `bfebef9` | di-push (origin = `bfebef9` saat chat ini dimulai) |
| `design-f4a-v1.mbox` | `78c030933aa0b4bc` (cocok dengan hitungan sandbox) | `b86fdd6` (patch-id `b0120ff4b85190977fc05a4f28abaadbbc2bc548` SAMA dengan mbox) | BELUM di-push saat dokumen ini ditulis (`ahead 1`); di-push bersama patch dokumen ini |
| `design-checkpoint4-v1.mbox` (dokumen ini) | lihat pesan chat | lihat pesan chat | lihat pesan chat |

Catatan: `git am` dijalankan dua kali oleh pemilik untuk F4a (kali pertama sudah menerapkan; kali kedua gagal "already exists in index"); dibuktikan dengan `patch-id` dan `git am --quit`. Penulis commit: `Claude <noreply@anthropic.com>` (ganti dengan `git commit --amend --reset-author --no-edit` sebelum push bila diinginkan).

## 5. Bukti terakhir (apa adanya)

Ditempel pemilik: `git status -sb && git log --oneline -10 --decorate && git ls-files | wc -l` awal (sejajar origin di `bfebef9`, 618 berkas); `git tag --list 'm04-done'` dan `git merge-base --is-ancestor m04-done main` ("m04-done ada di main"); `sha256sum` F4a (cocok); keluaran `git am` gagal (sudah terpasang) + `git status -sb` (`ahead 1`, `b86fdd6`); hasil perbandingan patch-id ("SAMA"); `git status -sb && git ls-files | wc -l` (621); jawaban "OK" (rencana F4, review F4a).
Di sandbox (bukan dari laptop): zip `platform-repo-20261123-1509.zip` memuat 618 berkas; `lint-structure OK` pada salinan bersih zip; `git am` F4a pada salinan bersih (baseline + `git am`), `diff -r -x .git` identik dengan pohon kerja, 621 berkas; `git diff --check` bersih; `node --check` semua JS; paritas i18n id/en identik (385 menurut regex `'[^']+': '`; baseline 290 dengan regex yang sama, jadi +95; handover partial-3 menulis 288 dengan penghitungan lain);
nol kunci i18n hilang, nol id ganda, nol rujukan aria/`for` putus, nol ikon asing, tag HTML seimbang, nol `style=`/`on*` di dua halaman baru; simulasi aturan `data-when` per state (baris: normal 4, revoked 2, lapsed 1, all 7, platform 2, revlast 1); `gen-theme.py`: 76 pasangan, 0 gagal, `theme.css` tidak berubah.
Bug yang ditemukan sendiri dan diperbaiki sebelum patch dibuat: asersi jumlah halaman bernav salah hitung (8, bukan 10); indeks `<main>` meleset karena `page()` mengganti judul/`<body>` sebelum menyambung `<main>` (sekarang menyambung dulu); wadah toast `#z-toasts` hilang di halaman daftar.
TIDAK ada: tangkapan layar atau catatan visual; eksekusi JS di browser (dialog cabut, filter, form, fokus); uji keyboard dan pembaca layar; tampilan 360/768/1280, mode gelap, tiga tema; `git push` F4a; `git fsck`; jumlah berkas zip setelah dokumen ini.

## 6. Temuan kontrak (dibaca di chat #4 dari `swagger.json`, ADR-0022, kode domain; VERIFIKASI ULANG sebelum menggambar)

Assignments (dipakai F4a): `AssignmentResponse` = `{id, role_id, role_name, role_slug, role_status, subject_type, subject_id, scope_type, group_id, status, expires_at, revoked_at, assigned_by, created_at}`; tanpa nama user. `GET /role-assignments` (query `status`, `role_id`, `subject_id`, `limit`, `cursor`); respons `{data, meta}` dengan cursor. `POST` body `{role_id, subject_type, subject_id, scope_type, group_id?, expires_at?}` + header `Idempotency-Key` (409 `idempotency_in_progress`, 422 `idempotency_key_conflict`). Pemanggil platform hanya cakupan platform dan subjek `user`; `expires_at` tidak sah di cakupan platform dan harus di masa depan. Subjek tidak ada = `validation_failed` pada `subject_id` (`not_found`, seragam). `DELETE /role-assignments/{id}` 204; 403 `grant_exceeds_authority`, 404, 409 `assignment_revoked`/`last_super_admin`.
Policies (BELUM digambar): `PolicyResponse` = `{id, code, name, description, effect, actions[], expression, status, global, created_at, updated_at}`; `CreatePolicyRequest` = `{code, name, description, effect, actions, expression, status}`; `UpdatePolicyRequest` = `{name, description, effect, actions, expression, status}` (PATCH; field nil = tidak diubah; `actions` kosong ditolak; `description` kosong menghapus; kode dan cakupan TIDAK dapat diubah). `GET /policies` offset (`status`, `page`, `per_page`); TIDAK ada `GET /policies/{id}` dan TIDAK ada DELETE (mematikan policy = `status: disabled`; nilai status: `active`/`disabled`; nilai filter status daftar belum diverifikasi, lihat `ValidPolicyFilter`). Pemanggil tenant melihat policy organisasinya DAN policy global (`global: true`, baca-saja; respons ubah-global dari tenant belum diverifikasi).
Aturan tulis policy (ADR-0022 butir 33, 50, 55, 60-61, 68): kode `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$` maks 150; nama tanpa spasi di ujung; `actions` 1-50 elemen, unik, tiap elemen maks 255 dan berbentuk kode permission, harus ada di katalog (`GET /permissions`), aksi `policy.*` tidak dapat dibatasi (galat `actions`); batas keras ekspresi: 2000 byte, 400 token, kedalaman 24, 1000 langkah evaluasi, string literal 256 byte, pengenal 64 byte, daftar 64 elemen, string atribut 4096 byte (pelanggaran = `too_complex`); galat ekspresi pada field `expression` dengan kode `syntax`/`too_complex` TANPA posisi (respons tidak memuat nilai klien atau indeks); TIDAK ada endpoint validasi ekspresi, jadi editor hanya bisa memberi petunjuk bahasa statis (akar `resource`/`current_user`/`scope`; fungsi `lower`/`len`/`startsWith`/`endsWith`/`contains`; baca ADR-0022 butir 33 untuk tata bahasa lengkap, jangan menebak).
Batas jumlah: 50 policy AKTIF per organisasi + 50 global; memindah dari `disabled` ke `active` juga menambah hitungan (`policy_limit_reached` 409 di buat dan di aktifkan ulang); `policy_code_taken` 409; `empty_patch`; pemanggil tingkat grup ditolak saat buat (verifikasi kodenya).
Simulator (BELUM digambar): `SimulateRequest` = `{user_id, action, active_group_id?, resource?{type, id, owner_id, group_id, attrs}}`; `SimulateResponse` = `{result, reason, level, policy_code, deferred_policies[]}`; target harus anggota aktif organisasi pemanggil, selain itu satu galat seragam `user_id: not_found`; pemanggil cakupan platform = 403 `scope_mismatch`; `policy_code` dan `deferred_policies` hanya terisi bila pemanggil punya `policy.read` (tanpa itu kosong/absen, tampilan harus utuh); atribut resource maks 32 dan nama `type`/`id`/`organization_id`/`owner_id`/`group_id` dicadangkan (`resource.attrs: reserved`); alasan: `granted`, `no_permission`, `scope_mismatch`, `policy_denied`, `policy_evaluation_failed`. Verifikasi daftar ini di `docs/adr/0022-m04-authorization-design.md` (butir simulasi) dan swagger sebelum menggambar.
Katalog `GET /permissions`: `PermissionResponse` = `{code, resource, action, module, description, is_system}`; offset (`page`, `per_page`).

## 7. Kekurangan dan revisi tertunda

1. SEMUA review visual (F1a-F4a): mode gelap, tiga tema, 360/768/1280. Kekhawatiran F4a: kolom UUID 36 karakter di 360 px (`text-break`, tabel 4 kolom + gulir horizontal), tabel 6 kolom di 768, jarak bilah filter.
2. F4a belum dijalankan di browser: dialog cabut (fokus kembali ke judul karena baris hilang), validasi filter/form, fokus ke field pertama, `aria-live` penghitung, wadah toast.
3. F4a: setelah baris terakhir dicabut tidak berganti ke state kosong; penghitung "{n} ditampilkan" tidak ikut berganti bahasa sampai interaksi berikutnya; kalimat dialog "sesi pengguna dapat ikut diakhiri" untuk assignment platform berasal dari catatan `docs/STATUS.md` ("pencabutan sesi saat assignment platform dicabut"), perilaku persisnya TIDAK diverifikasi di kode; pencabutan assignment kedaluwarsa yang belum dicabut tidak digambar.
4. Dari partial-3 yang masih terbuka: butir 1-8 §7 partial-3 (toast/skeleton belum diuji pembaca layar, Retry-After ganti password tidak ada di swagger, `forgot-password.html` di F5, MFA dan `csrf_failed` login, label bilah state tanpa i18n, badge primer Lagoon/Ember dekat batas 4,5, `DESIGN-HANDOFF.md` belum ada, README belum memuat daftar halaman).
5. Menu: tautan "Policy" dan "Simulator" masih `href="#"` di semua halaman bernav; ubah saat F4b/F4c (hitung jumlah halaman dengan skrip, jangan diasumsikan).

## 8. Keputusan terbuka (perlu jawaban pemilik)

1. Editor permission: daftar centang (diteruskan dengan asumsi) atau tabel matriks.
2. Hasil review visual F1a-F4a (belum ada sama sekali); identitas penulis commit.
3. Dari partial-3: referensi/merek, register publik vs undangan (periksa swagger di F5), cakupan MFA dan `csrf_failed` login; sumber izin efektif UI dan endpoint daftar organisasi (contract-gap); parsing `user_agent`, makna `auth_level`, nilai `module` katalog inti.
4. Baru: apakah assignment kedaluwarsa yang belum dicabut boleh dicabut (tombol Cabut hanya untuk `active` saat ini)? Editor policy sebagai halaman penuh (`policy-edit.html`, usulan) atau panel/dialog? Penghitung batas policy "X dari 50 aktif" hanya dapat dihitung dari daftar (tidak ada endpoint hitungan): tampilkan atau cukup pesan saat `policy_limit_reached`?

## 9. Rencana fase berikut

- **F4b Policies** (patch tersendiri; usulkan di awal): `policies.html` (daftar offset, filter status, badge `global` baca-saja, aksi per baris) dan satu halaman editor buat/ubah (`policy-edit.html`: kode tidak dapat diubah saat ubah, nama, deskripsi, efek deny/allow, aksi dari katalog tanpa `policy.*` dengan penjelasan, editor ekspresi dengan petunjuk bahasa statis dan batas 2000 byte, status aktif/nonaktif). State minimal: normal, loading, empty, error, forbidden, viewer, validation (galat per field termasuk `expression` syntax/too_complex tanpa posisi, `actions` protected/unknown), codetaken, limit, globalro, emptypatch, scope, csrf, success. Baca dulu kontrak §6 dan ADR-0022 butir 33, 50, 55, 60-61, 68; jangan menebak. Ubah tautan menu "Policy" di semua halaman bernav.
- **F4c Simulator**: `simulator.html` (masukan `user_id` UUID, aksi dari katalog, grup aktif opsional, resource opsional + atribut dinamis maks 32 dengan nama dicadangkan ditolak; hasil ALLOW/DENY + `reason` + level + `policy_code` + `deferred_policies`; SATU-SATUNYA tempat alasan penolakan boleh tampil); state: normal, loading, allow, deny (tiap `reason`), tanpa `policy.read` (field absen), `user_id` tidak ada, atribut tak sah, scope (platform), forbidden, error, csrf, validation.
- **F5** (halaman sistem 401/403/404/5xx/offline/error boundary; contract-first: lupa password, atur ulang password, terima undangan; MFA dan `csrf_failed` di login) dan **F6** (`DESIGN-HANDOFF.md`, galeri final, README, prompt M10). Checkpoint berikutnya: `HANDOVER-UI-DESIGN-partial-5.md` dan `PROMPT-UI-DESIGN-partial-6.md`. Setiap fase: usulan singkat -> patch kecil -> tinjau -> revisi -> push.

## 10. Catatan proses (agar tidak terulang)

- Semua catatan §10 partial-2 dan partial-3 tetap berlaku.
- `git am` yang gagal dengan "already exists in index" dan cabang sudah `ahead` SEBELUM `git am` berarti patch sudah terpasang: bandingkan `git show HEAD | git patch-id --stable` dengan `git patch-id --stable < file.mbox`, lalu `git am --quit`; jangan terapkan ulang.
- Builder halaman: sambung `<main>` ke `shell.html` SEBELUM mengganti judul/`<body>` (indeks bergeser); jangan membaca `shell.html` setelah loop nav yang mengubahnya tanpa memperhitungkannya; halaman yang memakai `ZUI.toast` WAJIB memuat `<div class="z-toasts" id="z-toasts" ...>`; asersi jumlah halaman dihitung dari `grep`, bukan dari ingatan.
- Halaman baru bisa memakai satu berkas JS bersama untuk daftar+form (`assignments.js`) dengan dua blok `if` yang berdiri sendiri; pola data tabel per state ada di `roles.js`.
- Pemeriksa statis dan simulasi `data-when` per state ditulis ulang di sandbox tiap chat (tidak ada di repo); sandbox tidak punya jsdom/browser, jadi JS hanya diperiksa dengan `node --check`.
