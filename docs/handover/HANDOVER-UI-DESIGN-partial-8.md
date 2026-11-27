# HANDOVER-UI-DESIGN-partial-8 (checkpoint kedelapan, fase desain UI/UX)

Dibuat di chat desain #8 pada ◆CP setelah F5a v2 dan F5b (tiga halaman contract-first) DITERAPKAN dan DI-PUSH pemilik. Prompt awal: `docs/handover/PROMPT-UI-DESIGN-start.md`.
Handover sebelumnya: `HANDOVER-UI-DESIGN-partial-1.md` sampai `-7.md`. Prompt lanjutan: `docs/handover/PROMPT-UI-DESIGN-partial-9.md`.
Alasan checkpoint: F5b selesai (tiga halaman HTML baru, empat patch) dan chat ini memuat banyak bacaan (handover, docs/05, docs/08, swagger, JS, HTML) serta siklus bangun-uji-sapu berulang; menurut ketentuan itu tanda konteks menipis, jadi F5c TIDAK dimulai di chat ini. Checkpoint dibuat atas penilaian saya (pemilik belum menyuruh), sebelum review visual apa pun.

## 1. Ringkasan

Prototipe statis (HTML + CSS + Bootstrap 5.3.9, tanpa build, tanpa framework JS) di `design/`, branch `design/ui-prototype` (dari `m04-done` = `main`; tag terverifikasi pemilik di partial-6: `6a14f87276ed64ba15af7673beaaebc4725ca79c`).
Di chat ini: keadaan repo dicocokkan (zip 636 berkas = laptop `56bf396`, sejajar origin setelah `git push`), lalu dikerjakan dan di-push pemilik: **F5a v2** (cincin fokus `h1`), **F5b-1** (`forgot-password.html`), **F5b-2** (`reset-password.html`), **F5b-3** (`accept-invitation.html`). Kontrak F5b diverifikasi ulang dari `docs/05` §13 dan §6, `docs/08` §10/§11, dan `docs/api/swagger.json`.
Review visual: TIDAK ada. Pemilik hanya menjawab "ok" pada rencana dan menempelkan hasil `sha256sum`, `git am`, `git status`, `git push` (kecuali `sha256sum` dan `git am` untuk F5a v2); F4b, F4c, F5a, F5b (dan F1a-F4a) TETAP belum direview secara visual. Pertanyaan cincin fokus tombol (§8 butir 2) belum dijawab.
Belum dikerjakan: **F5c (login: state `csrf`, perbaikan temuan §7), F6 (`DESIGN-HANDOFF.md`, galeri final, README, prompt M10).**
Tidak ada berkas di `backend/**`, `apps/**`, `packages/**` yang disentuh.

## 2. Keputusan desain dan status

| Keputusan | Status |
|---|---|
| Semua keputusan partial-1 sampai partial-7 | tetap seperti tercatat di sana |
| F5a v2: `h1[tabindex="-1"]:focus-visible { outline: 0 }` (sumber: `gen-theme.py`; elemen `tabindex=-1` lain seperti kotak alert dan hasil tetap bercincin) | DISETUJUI pemilik ("ok", default); di-push |
| Ikon 404/offline tetap `inbox`/`exclamation-triangle` (tanpa `search`/`wifi-off`); teks alasan Simulator `no_permission`/`scope_mismatch` tetap tafsiran ("butuh konfirmasi"); `request_id` contoh `contoh-7f3a9c2e` dan batas panjang `display_name` TIDAK dikarang (contract-gap) | DISETUJUI pemilik ("ok", default) |
| F5b: satu patch per halaman; layar penuh `.z-auth` dari pola login; spanduk `role="note"` "Contract-first · endpoint belum ada (direncanakan M05)" di atas form (penanda prototipe, DIBUANG di SPA); `recovery.js` bersama (`<body data-rec="forgot|reset|accept">`, id berawalan `fp-`/`rp-`/`ai-`) | DISETUJUI pemilik ("ok" atas usulan), visual BELUM direview |
| F5b: `forgot-password.html` jawaban `sent` generik identik untuk semua email, tanpa mengulang alamat, tanpa tombol kirim ulang; tautan "Gunakan email lain"; fokus ke `h1` baru; masa berlaku token TIDAK disebut dengan angka (default 30 menit/72 jam dapat dikonfigurasi) | DISETUJUI (rencana), visual BELUM direview |
| F5b: reset dan undangan memakai kotak centang "Tampilkan password" dengan label tetap (pola `security-password.html`), BUKAN tombol yang labelnya berganti | usulan saya, BELUM direview |
| F5b: state `token` = satu pesan untuk tak sah/terpakai/kedaluwarsa (`invalid_reset_token`, `invalid_invitation`); reset punya CTA "Minta tautan baru", undangan TIDAK (undangan diterbitkan administrator; pesan menyuruh menghubungi administrator) | usulan saya, BELUM direview |
| F5b: sukses TIDAK masuk otomatis (respons sukses belum terdokumentasi); sukses undangan tidak menampilkan organisasi/peran (tidak ada endpoint pratinjau); sukses reset menyebut semua sesi keluar | usulan saya, BELUM direview |
| F5b: `short`/`long` memakai `pw.e.short`/`pw.e.long` untuk reset dan undangan: kode detail `too_short`/`too_long` hanya terdokumentasi untuk ganti password, jadi ini ASUMSI | butuh konfirmasi saat kontrak M05 ada |
| Temuan cincin fokus tombol Bootstrap (§7 butir 1) | BUTUH keputusan pemilik; default: ditunda, dicatat di handoff F6 |
| Usulan F4b, F4c, F5a | masih BELUM direview secara visual |
| Referensi/merek, register publik vs undangan, teks alasan Simulator | TETAP TERBUKA (default) |

## 3. Halaman dan state (semuanya via `?state=`; belum ada yang DIREVIEW pemilik)

| Berkas | State | Status |
|---|---|---|
| Semua halaman partial-7 §3 (`components`, `dashboard`, `login`, `shell`, `security-*`, `roles`, `role-new`, `role-detail`, `role-assignments`, `role-assignment-new`, `policies`, `policy-edit`, `simulator`, `system`) | seperti partial-7 | diserahkan |
| `forgot-password.html` (F5b-1) | normal, loading, validation, sent, rate, error (6) | diserahkan, di-push, BELUM direview |
| `reset-password.html` (F5b-2) | normal, loading, validation, short, long, token, success, rate, error (9) | diserahkan, di-push, BELUM direview |
| `accept-invitation.html` (F5b-3) | normal, loading, validation, short, long, token, success, rate, error (9) | diserahkan, di-push, BELUM direview |

Perilaku F5b (`design/recovery.js`; JS vanilla; di SPA menjadi tiga rute dengan komponen form bersama):
- Umum: submit dicegah (tanpa request); alert ringkasan `role="alert"` (danger/warning) atau `role="status"` (info); galat per field memakai `is-invalid` + `aria-invalid` + `aria-describedby`; fokus ke field pertama yang galat; `rate` = hitung mundur dari `?retry=` (cadangan 30 detik, minimum 1), angka `aria-hidden`, selesai = alert info `role="status"` dan tombol aktif; `loading` = `fieldset` nonaktif + `aria-busy` + spinner; isian dipertahankan saat galat.
- `forgot-password`: `email` `autocomplete="username"`; `sent` mengganti form dengan panel (fokus `h1#fp-sent-h`).
- `reset-password`: `rp-pw` `autocomplete="new-password"`, bantuan `pw.help` (12 karakter, tanpa aturan komposisi); `token` dan `success` mengganti form dengan panel (fokus `h1`).
- `accept-invitation`: `ai-name` `autocomplete="name"` (hanya wajib diisi) dan `ai-pw`; `validation` menandai kedua field dan memfokuskan nama.
- Keamanan token di URL (untuk F6/SPA): token ada di query string, jadi SPA harus membacanya lalu menghapusnya dari URL (`history.replaceState`) dan memakai `Referrer-Policy: no-referrer`; prototipe tidak memperagakan ini.
Berkas pendukung diubah: `states.js` (+label `sent`, `token`), `i18n.js` (+35 kunci per bahasa: `cf.*`, `fp.*`, `rp.*`, `ai.*`, `al.fpdone.*`, `al.rpdone.*`, `al.aidone.*`; total 620 per bahasa menurut node, identik id-ID/en-US), `index.html` (tiga tautan), berkas baru `recovery.js`. `login.html` sudah menautkan `forgot-password.html`; tautan itu sekarang tidak rusak. `design/README.md` BELUM memuat daftar halaman (F6).

## 4. Patch dan commit (laptop `~/projects/zago-core-m03`, branch `design/ui-prototype`)

| Patch | sha256 (16 kar.) | Commit di laptop | Push |
|---|---|---|---|
| `design-f4v2-v1.mbox`, `design-f5a-v1.mbox`, `design-checkpoint7-v1.mbox` | seperti partial-7 | `af6126e`, `6d8585f`, `56bf396` | di-push (terbukti: keluaran `git push` menunjukkan `56bf396..462800b`, awal chat ini `git push` = "Everything up-to-date") |
| `design-f5av2-v1.mbox` | `289f2088e64785d6` (hasil `sha256sum` pemilik: TIDAK ditempel) | `462800b` (ditempel lewat `git log`) | `56bf396..462800b` (ditempel) |
| `design-f5b1-v1.mbox` | `d16aba32e6dfb568` (cocok, ditempel) | `c1a9a63` (ditempel) | `462800b..c1a9a63` (ditempel) |
| `design-f5b2-v1.mbox` | `4d24b13004e6ebe1` (cocok, ditempel) | `0e6dba5` (ditempel) | `c1a9a63..0e6dba5` (ditempel) |
| `design-f5b3-v1.mbox` | `7930e305a581019a` (cocok, ditempel) | `70223a1` (ditempel) | `0e6dba5..70223a1` (ditempel) |
| `design-checkpoint8-v1.mbox` (dokumen ini) | lihat pesan chat | lihat pesan chat | lihat pesan chat |

Penulis commit di sandbox: `master-abror <ruangedu404@gmail.com>`.

## 5. Bukti terakhir (apa adanya)

Ditempel pemilik: `git status -sb && git log --oneline -10 --decorate && git ls-files | wc -l` awal (sejajar origin di `56bf396`, 636 berkas); `git push` = "Everything up-to-date"; untuk tiap mbox F5b-1, F5b-2, F5b-3: `sha256sum` (cocok), `git am` ("Applying: ..."), `git status -sb && git log --oneline -3 --decorate && git ls-files | wc -l` (`ahead 1`; jumlah berkas 638, 639, 640), dan keluaran `git push` lengkap; untuk F5a v2: `git status`/`git log` (`ahead 1`, `462800b`, 636 berkas) dan `git push` lengkap. Tidak ditempel: `sha256sum` dan `git am` untuk F5a v2, dan `git fsck`.
Di sandbox (bukan dari laptop): zip `platform-repo-20261127-1119.zip` memuat 636 berkas; `lint-structure OK` pada salinan zip dan pada salinan bersih sesudah setiap mbox dipasang berurutan (pohon identik dengan pohon kerja via `diff -r`; 640 berkas sesudah F5b-3); `git diff --check` bersih; `node --check` semua JS; paritas i18n 620/620 (597 sesudah F5b-1, 608 sesudah F5b-2); `gen-theme.py` idempoten (94 pasangan kontras, 0 gagal) dan `check-theme-vars OK (106 variabel --bs-*)` setelah F5a v2; pemeriksa struktur sendiri (tag seimbang, id ganda, rujukan `for`/`aria-*`, ikon, kunci i18n, `style=`/`on*`, `data-when` + `d-*`) 0 kegagalan di semua halaman `design/*.html` (20) sesudah tiap patch.
Chromium (Playwright, lewat HTTP lokal): F5a v2 (cincin `h1` nol di s404/s500/boundary dan `policies.html`, kotak alert `tabindex=-1` tetap `solid`); F5b: forgot 6 state x 360/768/1280 x terang/gelap x id-ID/en-US = 72 kombinasi, reset dan undangan 9 state masing-masing = 108 kombinasi, 0 masalah pada: tepat satu `h1` terlihat, `[hidden]` bocor, overflow horizontal, galat konsol, field galat tanpa aria, target di bawah 24 px (kecuali label kotak centang, §7 butir 4); perilaku diuji: hitung mundur (`?retry=2` -> tombol aktif, alert info), fokus per state, urutan Tab, kotak centang tampilkan password, `autocomplete`; semua 20 halaman dimuat tanpa galat konsol. Saya melihat sendiri tangkapan layar 360 px forgot `rate`, forgot `sent`, reset `short` (hanya memeriksa kerusakan tata letak: BUKAN penilaian visual).
Bug dan temuan sendiri: skrip i18n pertama gagal karena entri terakhir tiap blok di `i18n.js` tidak berakhir koma (asersi menangkap; teknik sisip diperbaiki, tidak ada berkas yang tertulis); `d-block` + `hidden` pada hitung mundur dan tautan teks 19 px tertangkap sapuan pada halaman baru dan diperbaiki sebelum mbox; server HTTP sandbox mati di antara panggilan bash (lihat §10).
TIDAK ada: review visual pemilik (F1a-F5b); tangkapan layar yang dinilai pemilik; uji keyboard dan pembaca layar manual; perangkat sentuh nyata; galat server asli (endpoint belum ada); Firefox/Safari; `git fsck`.

## 6. Temuan kontrak (F5b DIVERIFIKASI di chat ini)

- `docs/05` §13.2: `POST /auth/forgot-password` selalu menjawab sama (pesan generik) apa pun emailnya, email dikirim worker lewat outbox; `POST /auth/reset-password {token, new_password}`: token sekali pakai, "default 30 minutes" (dapat dikonfigurasi), sukses mencabut SEMUA sesi dan menulis `auth.password.reset`. §13.3: `POST /auth/accept-invitation {token, display_name, password}` tanpa autentikasi, token "default 72 hours", jawaban untuk token tak sah/terpakai/kedaluwarsa IDENTIK, peran preset dicek ulang terhadap wewenang pengundang saat penerimaan; user dibuat atau diaktifkan. §6: MFA berlabel Phase 2.
- `docs/08` §10.1 (As built M03): `forgot-password`, `reset-password`, `accept-invitation` dan MFA BELUM dibangun (reset/undangan: M05; MFA: Phase 2). Registry §11: `invalid_reset_token` dan `invalid_invitation` ada di namespace Auth, TETAPI status HTTP-nya tidak tertulis (contract-gap). §7: endpoint sensitif (login, forgot/reset, accept-invitation, MFA verify) punya batas laju tersendiri, angkanya TIDAK tertulis; aturan yang tertulis hanya `auth.login` 10/menit/IP dan `security.password` 5/menit/user.
- Swagger: path auth/security hanya `csrf`, `login`, `logout`, `me`, `refresh`, `switch-organization`, `/security/events`, `/security/password`, `/security/sessions`, `/security/sessions/revoke-others`, `/security/sessions/{id}`; TIDAK ada path lupa/atur ulang password, undangan, register, atau MFA.
- Contract-gap baru dari F5b: status HTTP `invalid_reset_token`/`invalid_invitation`; rincian `validation_failed` untuk reset dan undangan (kode `too_short`/`too_long` diasumsikan sama seperti ganti password); batas panjang `display_name`; bentuk respons sukses `accept-invitation` (apakah sesi dibuat); apakah `csrf_failed` berlaku pada tiga endpoint publik ini (state `csrf` TIDAK didesain untuk F5b); batas laju endpoint publik ini (state `rate` mengikuti pola login 429 + `Retry-After`). Contract-gap lama: seperti partial-6/7 §6.

## 7. Kekurangan dan revisi tertunda

1. Kontras cincin fokus TOMBOL: `.btn:focus-visible` memakai `box-shadow` Bootstrap 4 px alfa 0,5; terukur dari DOM pada `.btn-primary` (tautan "Kembali ke Dashboard" di `system.html?state=s404`, palet Aurora): cincin efektif terhadap latar halaman 2,45:1 (terang) dan 2,72:1 (gelap), di bawah 3:1 (WCAG 1.4.11). Palet Lagoon/Ember dan varian tombol lain TIDAK diukur. Cincin tautan (`outline` 2 px `--z-primary`) tidak terkena. Usul perbaikan (belum dikerjakan, butuh persetujuan karena mengubah semua tombol): `.btn:focus-visible { outline: 2px solid var(--z-primary); outline-offset: 2px; box-shadow: none; }` di `gen-theme.py`.
2. `login.html`: hitung mundur `#la-c` berkelas `d-block` bersama `hidden` (utilitas `!important` mengalahkan `hidden`): blok kosong bermargin 4 px selalu tampil di dalam alert. Perbaikan: ganti `<span ... d-block ...>` menjadi `<div ...>` tanpa `d-block` (sudah diterapkan di tiga halaman F5b; sapuan `[hidden]` di halaman login belum dijalankan untuk memastikan).
3. `login.html`: tautan "Lupa password?" kelas `small` memakai pola yang sama dengan tautan 19 px yang tertangkap di F5b; tinggi sebenarnya BELUM diukur (diperkirakan); perbaiki dengan `d-inline-block py-2` bila terukur di bawah 24 px.
4. `login.html`: tombol Tampilkan/Sembunyikan mengubah label DAN `aria-pressed` sekaligus (anti-pola: label berganti + status toggle). Pilihan di F5c: kotak centang berlabel tetap (seperti F5b dan Security) atau satu tombol dengan label tetap + `aria-pressed`. Label kotak centang "Tampilkan password" (`form-check-label`) setinggi 23 px di `security-password.html` dan tiga halaman F5b: di bawah 24 px sebesar 1 px, memenuhi pengecualian jarak WCAG 2.5.8; tidak diubah karena CSS global akan menyentuh daftar centang editor permission tanpa review.
5. SEMUA review visual (F1a-F5b) oleh pemilik; belum diuji keyboard dan pembaca layar manual (F5a live region 503, F5b fokus `h1` setelah `sent`/`token`/`success`, toast/skeleton); `offlinebar` hanya statis.
6. Dari partial-5/6/7 yang masih terbuka: kolom "Diubah" di tabel policy membungkus; contoh ekspresi ilustratif (tidak dijalankan lewat parser Go asli); butir partial-3 §7 (label bilah state tanpa i18n, badge primer Lagoon/Ember dekat 4,5, `DESIGN-HANDOFF.md`, README tanpa daftar halaman); pemilih aksi policy, editor permission peran, dan `<select>` aksi Simulator harus menjadi satu komponen bersama di SPA.

## 8. Keputusan terbuka (perlu jawaban pemilik)

1. Review visual F1a-F5b (belum ada sama sekali).
2. Cincin fokus tombol 2,45:1/2,72:1: perbaiki dengan `outline` (usul §7 butir 1) atau tunda ke handoff?
3. Login (F5c): pola tampilkan password (kotak centang atau tombol berlabel tetap + `aria-pressed`)?
4. Konfirmasi teks alasan Simulator `no_permission`/`scope_mismatch`.
5. Referensi/merek; register publik vs undangan (default: undangan saja); sumber izin efektif UI dan endpoint daftar organisasi (contract-gap); parsing `user_agent`, makna `auth_level`, nilai `module` katalog inti.
6. Hal yang menunggu kontrak M05 (bukan keputusan pemilik): status HTTP `invalid_reset_token`/`invalid_invitation`, rincian `validation_failed`, batas `display_name`, respons `accept-invitation`, batas laju, `request_id`.

## 9. Rencana fase berikut

- **Awal chat berikutnya: tinjauan F4b, F4c, F5a, F5b** (jawaban pemilik atas §8, tangkapan layar atau catatan; untuk F5b: tiga halaman di 360 dan 1280, `sent`/`token`/`success`, hitung mundur, mode gelap, kotak centang tampilkan password, spanduk contract-first), revisi bila ada (patch `v2`), lalu `git push`.
- **F5c: login**: tambah state `csrf` (403 `csrf_failed` -> "Muat ulang halaman"; kunci `al.csrf.*` dan `sx.reload` sudah ada; `login.html` sudah memiliki 10 state, periksa dulu apakah `csrf` sudah ada di peta `TONE`/`data-states` sebelum menambah); perbaiki temuan §7 butir 2-4 (hitung mundur `d-block`, tautan "Lupa password?", pola tampilkan password sesuai jawaban §8 butir 3); jalankan sapuan `[hidden]` untuk `login.html`. MFA TIDAK didesain.
- **F6**: `design/DESIGN-HANDOFF.md` (prinsip UX, token dan padanan Sass termasuk `$code-color`, inventaris komponen dengan pemetaan Svelte dan perilaku JS, inventaris halaman dengan rute usulan dan endpoint, kode galat -> perilaku UI, termasuk 401 = alur ke login dan keamanan token di URL, daftar string, keputusan terbuka dan contract-gap §6, keterbatasan, MFA = pekerjaan mendatang), galeri `design/index.html` final, `design/README.md` dengan daftar halaman, ringkasan keputusan untuk ADR frontend (rentang ADR-0030–0039), dan PROMPT fase Svelte SPA (M10).
- Checkpoint berikutnya: `HANDOVER-UI-DESIGN-partial-9.md` dan `PROMPT-UI-DESIGN-partial-10.md`. Setiap fase: usulan singkat -> patch kecil -> tinjau -> revisi -> push.

## 10. Catatan proses (agar tidak terulang)

- Semua catatan §10 partial-2 sampai partial-7 tetap berlaku (Playwright + Chromium ada di sandbox; uji lewat HTTP bukan `file://`; jebakan `d-*` vs `hidden`; `.visually-hidden` di `.table-responsive`; `ZApp.apply()` menimpa `textContent`; nama state berawalan unik; jangan `sed` global untuk kunci state; pembuatan mbox dari zip tanpa `.git`; `bash -c`; cara membaca `ZI18N` di node; `textContent` bukan `innerText`; server HTTP mati di antara panggilan bash; jangan `cut -c` pada teks non-ASCII).
- Skrip pembantu chat ini (TIDAK ada di repo; tulis ulang bila perlu): pembangun halaman ber-assert dari `forgot-password.html` (potong `<main>` lewat indeks string, ganti `<title>`/`<body data-rec data-states>`); penyisip kunci i18n; `check_page.py` (parser HTML: tag seimbang, id ganda, `for`/`aria-*`, `style=`/`on*`, `data-when` + `d-*`, ikon, kunci i18n, id yang dirujuk skrip); `sweep.py` (Playwright: state x lebar x tema x bahasa, gulir horizontal, `[hidden]` bocor, jumlah `h1`, target < 24 px, aria field galat, galat konsol; keluaran dikompres per jenis temuan).
- Sisip kunci i18n: entri TERAKHIR blok id-ID dan en-US di `i18n.js` TIDAK berakhir koma; tambahkan koma pada entri lama dan biarkan blok baru tanpa koma penutup; jangan menambah kunci yang sudah ada (objek literal diam-diam menimpa duplikat, `Object.keys` tidak menunjukkannya).
- Elemen yang disembunyikan lewat `hidden` jangan diberi `d-block`/`d-flex`/`hstack`: untuk hitung mundur pakai `<div>` (blok bawaan), bukan `<span class="d-block">`. Tautan teks mandiri butuh `d-inline-block py-2` agar target >= 24 px; `label.form-check-label` mendarat 23 px (dikecualikan dari sapuan, lihat §7 butir 4).
- Tangkapan layar `full_page` menaruh bilah state demo (fixed) di tinggi viewport sehingga tampak menutupi isi bawah: artefak tangkapan, bukan cacat; halaman memiliki `padding-bottom: 3rem` untuk itu.
- Jaga keluaran alat tetap kecil: jangan `grep -o` pola lebar pada banyak berkas HTML (keluaran sangat besar); cetak ringkasan dari Python.
- Pembuktian patch: salin bersih zip -> `git init` + commit dasar -> terapkan mbox berurutan dengan `git am` -> `diff -r -x .git` terhadap pohon kerja -> `lint-structure` -> hitung berkas.
- Jawaban ambigu pemilik ("ok", "sudah saya terapkan") BUKAN review visual dan bukan bukti keluaran; selalu minta tempelan keluaran dan catat yang tidak ditempel. Pemilik menempelkan `sha256sum`, `git am`, `git status`, `git push` untuk F5b-1 sampai F5b-3 dan `git status` + `git push` untuk F5a v2.
