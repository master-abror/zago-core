# HANDOVER-UI-DESIGN-partial-5 (checkpoint kelima, fase desain UI/UX)

Dibuat di chat desain #5 pada ◆CP setelah F4b (Policies) DISERAHKAN, BELUM DIREVIEW pemilik. Prompt awal: `docs/handover/PROMPT-UI-DESIGN-start.md`.
Handover sebelumnya: `HANDOVER-UI-DESIGN-partial-1.md` sampai `-4.md`. Prompt lanjutan: `docs/handover/PROMPT-UI-DESIGN-partial-6.md`.
Alasan checkpoint: banyak berkas besar dibaca penuh (swagger, ADR-0022, handover, HTML halaman) dan banyak siklus bangun-uji-ulang; keluaran bacaan awal sudah terpotong dari konteks. Menurut ketentuan itu tanda konteks menipis, jadi F4c TIDAK dimulai dan review F4b dipindah ke chat berikutnya.

## 1. Ringkasan

Prototipe statis (HTML + CSS + Bootstrap 5.3.9, tanpa build, tanpa framework JS) di `design/`, branch `design/ui-prototype` (dari `m04-done` = `main`).
Di chat ini: keadaan repo dicocokkan (sejajar origin di `a635ea9`, 623 berkas, memuat `b86fdd6`), kontrak Policies dibaca ulang dari swagger, ADR-0022 dan kode, lima keputusan terbuka partial-4 §8 dijawab pemilik, lalu **F4b (Policies) selesai dibangun dan diuji otomatis di browser (Chromium)** tetapi **belum ditinjau pemilik**.
Belum dikerjakan: **F4c (Simulator), F5 (halaman sistem + contract-first), F6 (`DESIGN-HANDOFF.md` + galeri final).**
Tidak ada berkas di `backend/**`, `apps/**`, `packages/**` yang disentuh. TIDAK ADA tangkapan layar atau catatan visual dari pemilik untuk F1a-F4b (jawaban pemilik: "Belum ada").

## 2. Keputusan desain dan status

| Keputusan | Status |
|---|---|
| Semua keputusan partial-1 sampai partial-4 | tetap disetujui |
| Editor permission = daftar centang per modul > resource (bukan tabel matriks) | DISETUJUI eksplisit di chat ini ("ya"); tidak lagi deviasi |
| Penulis commit | DIGANTI pemilik ke email `ruangedu404@gmail.com`; nama tidak disebut pemilik, dipakai `ruangedu404` (ubah dengan `git commit --amend --reset-author --no-edit` setelah mengatur `git config user.name`) |
| Tombol Cabut assignment hanya untuk `active`; editor policy = halaman penuh `policy-edit.html`; penghitung "X dari 50 aktif" TIDAK ditampilkan (hanya pesan saat `policy_limit_reached`, karena total daftar mencampur policy organisasi dan global yang batasnya terpisah) | DISETUJUI eksplisit ("ya, ya, ya") |
| F4b: label efek `deny` = "Tolak jika benar", `allow` = "Syarat wajib" (kode mentah `deny`/`allow` tampil kecil di editor); alasan: `allow` tidak pernah memberi izin (ADR-0022 butir 31) | usulan saya, BELUM direview |
| F4b: policy global = baca-saja untuk tenant (tombol "Lihat", state `globalro`); identitas Platform hanya melihat dan mengelola policy global dan melihat banner peringatan "berlaku untuk semua organisasi" saat buat/ubah | usulan saya, BELUM direview |
| F4b: aktifkan/nonaktifkan dari daftar lewat `<dialog>` biasa, galat DI DALAM dialog, baris hilang bila tidak lagi cocok dengan filter, toast, fokus ke judul (baris hilang) atau tombol yang sama | usulan saya (pola F4a), BELUM direview |
| F4b: urutan field editor = kode, nama, deskripsi, efek, ekspresi, aksi, status (ekspresi sebelum pemilih aksi agar tidak terdorong jauh oleh katalog); modul katalog tertutup kecuali berisi aksi terpilih | usulan saya, BELUM direview |
| F4b: pemilih aksi menyediakan baris `resource.*` per resource (kontrak mengizinkan) dan MENYEMBUNYIKAN resource `policy` dengan penjelasan (aksi `policy.*` tidak dapat dibatasi) | usulan saya, BELUM direview |
| F4b: ekspresi = textarea monospace, penghitung byte UTF-8 (batas 2000), pembaca layar diberi tahu hanya saat melewati 90% dan 100%, panduan statis dalam `<details>`; tidak ada validasi sebelum simpan karena tidak ada endpoint validasi | usulan saya, BELUM direview |
| F4b: state `scope` = pemanggil dengan grup aktif ditolak (403 `scope_mismatch`) saat buat DAN ubah; TANPA tombol CTA karena desain belum punya pengalih grup | usulan saya, BELUM direview |
| Nama state memakai awalan agar tidak bentrok dengan peta label `states.js` yang bersama: `pviewer`, `pnotfound`, `tg*` (ubah status) | keputusan teknis, lihat §10 |

## 3. Halaman dan state (semuanya via `?state=`; belum ada yang DIREVIEW pemilik)

| Berkas | State | Status |
|---|---|---|
| `components.html`, `dashboard.html`, `login.html`, `shell.html`, `security-*.html`, `roles.html`, `role-new.html`, `role-detail.html`, `role-assignments.html`, `role-assignment-new.html` | seperti partial-4 (menu "Policy" kini menuju `policies.html`) | diserahkan |
| `policies.html` | normal, paged, active, disabled, platform, pviewer, tglimit, tggone, tgcsrf, tgerr, tgscope, loading, empty, error, forbidden (15) | diserahkan (F4b) |
| `policy-edit.html` | normal (buat), edit, platform (buat global), platedit (ubah global oleh Platform), loading, validation, exprsyntax, exprcomplex, actprot, actunk, codetaken, limit, emptypatch, globalro, pviewer, scope, csrf, error, forbidden, pnotfound, success, saved (22) | diserahkan (F4b) |

Perilaku F4b (di `design/policies.js`, satu berkas untuk kedua halaman; JS vanilla; di SPA Svelte menggantikan seluruhnya):
- Daftar: filter status (`all`/`active`/`disabled`, nilai terverifikasi di kode) memindah ke `?state=` demo; badge `Global`; baris platform hanya policy global; tanpa `policy.manage` (`pviewer`) kolom tindakan disembunyikan dan tautan baris menuju tampilan baca-saja; ringkasan aksi = 2 kode pertama + "+N lainnya"; penghitung "Menampilkan a–b dari n" dihitung dari baris tampak (demo; di SPA dari `meta.pagination`). Teks bentukan skrip digambar ulang saat bahasa diganti.
- Ubah status: `po-toggle` mengisi dialog (judul/uraian per arah), spinner 700 ms, hasil menurut state: sukses, `tglimit` (409 hanya saat mengaktifkan), `tggone` (404, dengan tombol muat ulang), `tgcsrf` (403), `tgerr`, `tgscope` (403 `scope_mismatch`). Di SPA: `PATCH /policies/{id}` dengan `{status}`; menonaktifkan selalu diizinkan, mengaktifkan divalidasi penuh dan tunduk pada batas 50.
- Editor: kode hanya dapat diisi saat buat (`readonly` saat ubah); validasi sisi klien (kode, nama maks 150 karakter, deskripsi maks 1000, ekspresi wajib dan maks 2000 byte, aksi 1-50) dengan ringkasan alert + galat per field + fokus ke field pertama menurut urutan visual; galat server menempel pada field yang disebut kontrak (`expression` syntax/too_complex TANPA posisi, `actions` protected/unknown, `code` taken); tanpa perubahan saat ubah = "Tidak ada perubahan" (tidak dikirim); mode baca-saja (`globalro`, `pviewer`) menampilkan hanya aksi terpilih.
- Catatan implementasi SPA (komentar di JS): `POST /policies` (buat) dan `PATCH /policies/{id}` hanya dengan field yang berubah; nama dipangkas sebelum dikirim (server menolak spasi di tepi, tidak memangkasnya); TIDAK ada `GET /policies/{id}`, jadi halaman ubah mengambil record dari daftar (state router atau pencarian di `GET /policies`).
Berkas pendukung diubah: `states.js` (+20 label), `i18n.js` (+110 kunci `po.*`, `pf.*` per bahasa; total 495 per bahasa, dihitung dengan `Object.keys(ZI18N['id-ID']).length` di node; identik id-ID/en-US), `index.html` (tautan F4b), 10 halaman bernav + kartu dashboard (tautan "Policy" aktif; dihitung dari `grep`, bukan ingatan). `theme.css` dan `components.css` TIDAK berubah. `design/README.md` BELUM memuat daftar halaman (lihat §7).

## 4. Patch dan commit (laptop `~/projects/zago-core-m03`, branch `design/ui-prototype`)

| Patch | sha256 (16 kar.) | Commit di laptop | Push |
|---|---|---|---|
| `design-f4a-v1.mbox`, `design-checkpoint4-v1.mbox` | seperti partial-4 | `b86fdd6`, `a635ea9` | di-push (origin = `a635ea9` saat chat ini dimulai) |
| `design-f4b-v1.mbox` | `6b99189fb5bcd696` (patch-id sandbox `06285f721f2220a47a84cd3aef1c30b1170f73c6`) | lihat pesan chat | lihat pesan chat |
| `design-checkpoint5-v1.mbox` (dokumen ini) | lihat pesan chat | lihat pesan chat | lihat pesan chat |

Penulis commit di sandbox: `ruangedu404 <ruangedu404@gmail.com>`.

## 5. Bukti terakhir (apa adanya)

Ditempel pemilik: `git status -sb && git log --oneline -10 --decorate && git ls-files | wc -l` awal (sejajar origin di `a635ea9`, 623 berkas, memuat `b86fdd6`); jawaban: tangkapan layar "Belum ada"; editor permission "ya"; penulis commit diganti ke `ruangedu404@gmail.com`; tiga keputusan lain "ya".
Di sandbox (bukan dari laptop): zip `platform-repo-20261124-1038.zip` memuat 623 berkas (cocok dengan laptop); `lint-structure OK` pada salinan bersih zip dan pada pohon akhir; `gen-theme.py`: 76 pasangan, 0 gagal, `theme.css` tidak berubah; `check-theme-vars OK (105 variabel)`; `node --check` semua JS; `git diff --check` bersih; tag HTML seimbang di dua halaman baru; paritas i18n 495/495.
BARU: sandbox kini punya Playwright dengan Chromium (python `playwright.sync_api`; lihat §10 cara memakainya). Dengan itu dijalankan: (a) sapuan 296 kombinasi (2 halaman x semua state x id-ID/en-US x terang/gelap x lebar 360/1280) dengan 0 masalah pada pemeriksaan: kunci i18n mentah yang tampak, tepat satu `<h1>`, id ganda, kontrol tanpa label, overflow horizontal dokumen, galat konsol, elemen `[hidden]` yang masih tampil, tombol tanpa nama, `body[data-state]` sesuai; (b) 90 pemeriksaan perilaku interaksi (dialog ubah status per hasil, fokus, filter, baris hilang/penghitung, validasi buat, `aria-describedby` dipulihkan, pemilih aksi: pencarian/pilih semua modul/penghitung/tanpa `policy.*`, penghitung byte dan pengumuman ambang, submit sukses buat/ubah, tanpa perubahan, baca-saja, ganti bahasa lewat kontrol UI) semuanya lolos; (c) rasio kontras teks baru diukur dari DOM di 3 palet x terang/gelap: terendah 5,57:1 (batas 4,5:1).
Tangkapan layar yang saya lihat SENDIRI (bukan review pemilik): daftar desktop dan 360, editor desktop (galat ekspresi), editor 360 mode gelap (validasi).
Bug yang ditemukan sendiri dan diperbaiki sebelum patch: penggantian `sed` global merusak dua kunci i18n (`po.viewer`, `pf.ro.viewer`); overflow dokumen di 360 px karena `.visually-hidden` di dalam `.table-responsive` lolos dari pemotongan; tiga kasus `hidden` dikalahkan utilitas `d-flex`/`d-block` (baris pencarian baca-saja, tombol Simpan/Batal pada sukses dan baca-saja, pesan galat aksi); nama state `viewer`/`notfound` bentrok dengan label peran di `states.js`; katalog terbuka penuh mendorong field Ekspresi ±1900 px ke bawah; `text-danger` diganti `text-danger-emphasis` agar konsisten dengan galat field; demo `exprcomplex` memakai 90 suku yang belum tentu melewati 400 token, diganti 130 suku rapat (1948 byte, minimal 520 token).
TIDAK ada: review visual pemilik; uji keyboard dan pembaca layar manual (hanya pemeriksaan fokus otomatis); lebar 768; Firefox/Safari; `git push` F4b; `git fsck`; contoh ekspresi tidak dijalankan lewat parser Go asli (Go tidak ada di sandbox; tata bahasa dibaca dari `backend/internal/policy/policy.go` dan ketiga contoh panduan cocok dengannya).

## 6. Temuan kontrak (F4b terverifikasi di chat #5; F4c BELUM, verifikasi ulang)

Policies (terverifikasi di swagger, ADR-0022 dan kode): `GET /policies` offset, filter `status` = `all` (bawaan) / `active` / `disabled`; izin baca `policy.read`, tulis `policy.manage`; tanpa `Idempotency-Key`; kode ganda = 409 `policy_code_taken`; batas 50 aktif per organisasi dan 50 global terpisah (`policy_limit_reached` 409 saat buat aktif dan saat mengaktifkan lagi); tenant melihat policy organisasi + global, PATCH policy global dari tenant = 404; identitas Platform hanya melihat dan membuat/mengubah policy global; pemanggil dengan grup aktif ditolak 403 `scope_mismatch` di `Create` (`policies.go:120`) dan `Update` (`policies.go:171`); menonaktifkan selalu diizinkan tanpa validasi ulang, mengaktifkan divalidasi penuh (policy yang aksinya sudah hilang dari katalog hanya bisa diaktifkan bersama perbaikan aksinya); PATCH tanpa efek tidak menulis; kode `^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$` maks 150, nama maks 150 karakter tanpa spasi di tepi, deskripsi maks 1000 karakter, `actions` 1-50 unik dan boleh `resource.*`, resource `policy` dilindungi (`policy.*` ditolak). Tata bahasa ekspresi: `ekspr := atau`, `atau := dan (|| dan)*`, `dan := banding (&& banding)*`, `banding := unary ((== != < <= > >= in) unary)?` tidak berantai, `unary := ! unary | primer`; literal teks kutip ganda (escape `\" \\ \n \t`), angka, `true`/`false`/`null`, daftar `[a, b]`; akar `resource`/`current_user`/`scope`; fungsi `lower len startsWith endsWith contains`; galat evaluasi = tolak (gagal-tertutup).
Contract-gap F4b: tidak ada `GET /policies/{id}` (halaman ubah butuh record dari daftar) dan tidak ada DELETE; tidak ada endpoint validasi ekspresi; nama atribut `resource.*` yang sah bergantung modul dan belum ada daftarnya (contoh di panduan dan data contoh bersifat ilustrasi).
Simulator (BELUM digambar): `SimulateRequest` = `{user_id, action, active_group_id?, resource?{type, id, owner_id, group_id, attrs}}`; `SimulateResponse` = `{result, reason, level, policy_code, deferred_policies[]}`; izin `permission.simulate` (bukan `policy.read`); target harus anggota aktif organisasi pemanggil (selain itu satu galat seragam `user_id: not_found`); pemanggil Platform = 403 `scope_mismatch`; pemanggil level grup hanya dapat menyimulasikan anggota grup aktifnya; `policy_code` dan `deferred_policies` hanya terisi bila pemanggil punya `policy.read` (tanpa itu absen; tampilan harus utuh); atribut resource maks 32 dan nama `type`/`id`/`organization_id`/`owner_id`/`group_id` dicadangkan (`resource.attrs: reserved`); alasan: `granted`, `no_permission`, `scope_mismatch`, `policy_denied`, `policy_evaluation_failed`. Verifikasi di `docs/api/swagger.json` dan ADR-0022 (cari "simulat"; butir 69 diduga relevan, belum dibaca penuh).
Katalog `GET /permissions`: `PermissionResponse` = `{code, resource, action, module, description, is_system}`; offset (`page`, `per_page`). Pemilih aksi F4b memakai katalog contoh yang sama dengan `role-new.html` (diambil dari HTML itu, 45 kode, modul inti + "Pengumuman (contoh modul)").

## 7. Kekurangan dan revisi tertunda

1. SEMUA review visual (F1a-F4b) oleh pemilik. Kekhawatiran F4b: tabel daftar di 360 px masih 2 px lebih lebar dari kartu (gulir internal 312 vs 310 px di state normal; bukan overflow dokumen); kolom "Diubah" membungkus tanggal dan jam di lebar sedang; daftar katalog panjang bila modul inti terbuka (sama dengan editor peran); jarak bilah filter.
2. F4b belum diuji keyboard dan pembaca layar secara manual: dialog (fokus awal pada Batal, fokus kembali), `role="status"` ringkasan rentang yang ikut membacakan perubahan saat baris hilang, pengumuman ambang penghitung byte, `aria-describedby` yang berganti saat galat.
3. F4b: setelah baris terakhir hilang karena filter, daftar tidak berganti ke state kosong (sama dengan F4a); status baris diubah di klien tanpa memuat ulang; tidak ada state `badfilter` (pilihan status berasal dari `<select>` sehingga nilai tak sah hanya dari URL).
4. F4b: contoh ekspresi dan atribut (`resource.owner_id`, `resource.status`, `resource.is_admin`) adalah ilustrasi; `scope.*` tidak dicontohkan karena nama atributnya belum diverifikasi.
5. Pemilih aksi menduplikasi logika editor permission peran (`roles.js`); di SPA harus satu komponen bersama.
6. Dari partial-4 yang masih terbuka: review visual F1a-F4a, F4a belum dijalankan manual (kini sebagian tercakup uji otomatis F4b hanya untuk halaman policy; halaman F4a BELUM disapu dengan browser), butir 1-8 §7 partial-3 (toast/skeleton pembaca layar, Retry-After ganti password tidak ada di swagger, `forgot-password.html` di F5, MFA dan `csrf_failed` login, label bilah state tanpa i18n, badge primer Lagoon/Ember dekat 4,5, `DESIGN-HANDOFF.md`, README belum memuat daftar halaman).
7. Menu: tautan "Simulator" masih `href="#"` di semua halaman bernav; ubah saat F4c (hitung jumlah halaman dengan skrip).

## 8. Keputusan terbuka (perlu jawaban pemilik)

1. Review visual F1a-F4b (belum ada sama sekali), termasuk label efek ("Tolak jika benar" / "Syarat wajib") dan banner policy global.
2. Nama penulis commit (email sudah `ruangedu404@gmail.com`).
3. Dari partial-3: referensi/merek, register publik vs undangan (periksa swagger di F5), cakupan MFA dan `csrf_failed` login; sumber izin efektif UI dan endpoint daftar organisasi (contract-gap); parsing `user_agent`, makna `auth_level`, nilai `module` katalog inti.
4. Baru: apakah halaman ubah policy boleh bergantung pada daftar (karena tidak ada `GET /policies/{id}`), atau backend perlu menambah endpoint itu (usulan ke backend, bukan keputusan desain)?

## 9. Rencana fase berikut

- **Awal chat berikutnya: tinjauan F4b** (jawaban pemilik atas §8, tangkapan layar), revisi bila ada, lalu `git push` F4b.
- **F4c Simulator**: `simulator.html` (masukan `user_id` UUID, aksi dari katalog, grup aktif opsional, resource opsional + atribut dinamis maks 32 dengan nama dicadangkan ditolak; hasil ALLOW/DENY + `reason` + level + `policy_code` + `deferred_policies`; SATU-SATUNYA tempat alasan penolakan boleh tampil); state: normal, loading, allow, deny (tiap `reason`), tanpa `policy.read` (field absen), `user_id` tidak ada, atribut tak sah, scope (platform), forbidden, error, csrf, validation. Ubah tautan menu "Simulator" di semua halaman bernav.
- **F5** (halaman sistem 401/403/404/5xx/offline/error boundary; contract-first: lupa password, atur ulang password, terima undangan; MFA dan `csrf_failed` di login) dan **F6** (`DESIGN-HANDOFF.md`, galeri final, README, prompt M10). Checkpoint berikutnya: `HANDOVER-UI-DESIGN-partial-6.md` dan `PROMPT-UI-DESIGN-partial-7.md`. Setiap fase: usulan singkat -> patch kecil -> tinjau -> revisi -> push.

## 10. Catatan proses (agar tidak terulang)

- Semua catatan §10 partial-2 sampai partial-4 tetap berlaku, KECUALI "sandbox tidak punya browser": di chat #5 sandbox punya Playwright (python) dengan Chromium. Periksa dengan `python3 -c "from playwright.sync_api import sync_playwright as p; ..."` dan `launch()`; Firefox tidak ada.
- Uji lewat HTTP, bukan `file://`: `file://` memblokir `<use href="assets/icons.svg#...">` dan membanjiri konsol dengan galat palsu. Jalankan `cd design && nohup setsid python3 -m http.server 8765 >/tmp/http.log 2>&1 &` (proses latar biasa mati antar pemanggilan alat; `setsid` menahannya) lalu `curl` untuk memastikan 200.
- Awalan skrip uji yang berguna (ditulis ulang tiap chat, tidak ada di repo): sapuan per state x bahasa x tema x lebar dengan pemeriksaan di atas, dan uji interaksi per halaman. Tambahkan pemeriksaan "elemen `[hidden]` yang `getComputedStyle(e).display !== 'none'`".
- JEBAKAN: utilitas Bootstrap `d-flex`, `d-block`, `hstack` bersifat `!important` dan MENGALAHKAN atribut `hidden`. Jangan beri `hidden` pada elemen yang membawa utilitas itu; bungkus dengan `<div>` polos dan sembunyikan pembungkusnya (aturan ini sudah tertulis di prompt, dan tetap terlanggar tiga kali).
- JEBAKAN: `.visually-hidden` (position:absolute) di dalam `.table-responsive` yang statis tidak ikut terpotong dan melebarkan dokumen saat tabel bergulir; beri `position-relative` pada `.table-responsive`.
- JEBAKAN: `ZApp.apply()` menimpa `textContent` semua `[data-i18n]` (tanpa markup anak) dan tidak menyentuh teks bentukan skrip; teks dinamis (penghitung, rentang, "+N") digambar ulang saat tombol bahasa (`data-set="lang:..."`) ditekan, lihat `dyn` di `policies.js`.
- JEBAKAN: peta label `states.js` bersama untuk semua halaman; jangan memakai kunci state yang sudah berarti lain (`viewer` = "Tanpa role.manage", `notfound` = "404 peran tidak ada"); beri awalan.
- JEBAKAN: jangan `sed` global untuk mengganti nama state: ikut mengubah nama kunci i18n. Pakai penggantian berjangkar dengan assert. Dan `git checkout -- design/*.html` dengan berkas tak terlacak di glob membatalkan SELURUH perintah; pakai `git checkout -- design`.
- Halaman F4b dihasilkan oleh skrip sandbox (tidak di repo); untuk revisi, ubah HTML langsung dengan penggantian berjangkar ber-assert dan jalankan ulang sapuan.
