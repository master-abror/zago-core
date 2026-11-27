# HANDOVER-UI-DESIGN-partial-7 (checkpoint ketujuh, fase desain UI/UX)

Dibuat di chat desain #7 pada ◆CP setelah Patch 0 (`F4 v2`) dan F5a (halaman sistem) DITERAPKAN pemilik di laptop. Prompt awal: `docs/handover/PROMPT-UI-DESIGN-start.md`.
Handover sebelumnya: `HANDOVER-UI-DESIGN-partial-1.md` sampai `-6.md`. Prompt lanjutan: `docs/handover/PROMPT-UI-DESIGN-partial-8.md`.
Alasan checkpoint: F5a selesai diserahkan dan chat ini sudah memuat banyak bacaan (handover, JS, HTML, swagger, docs/05) serta siklus bangun-uji dua patch; F5b berisi tiga halaman HTML baru sekaligus, jadi menurut ketentuan itu tanda konteks menipis dan F5b TIDAK dimulai di chat ini. Checkpoint dibuat atas penilaian saya (pemilik belum menyuruh), sebelum review F5a.

## 1. Ringkasan

Prototipe statis (HTML + CSS + Bootstrap 5.3.9, tanpa build, tanpa framework JS) di `design/`, branch `design/ui-prototype` (dari `m04-done` = `main`; tag terverifikasi pemilik di partial-6: `6a14f87276ed64ba15af7673beaaebc4725ca79c`).
Di chat ini: keadaan repo dicocokkan (zip 632 berkas = laptop `ce16e5b`, sejajar origin), kontrak F5 diperiksa, rencana F5 DISETUJUI pemilik ("ok" atas empat default), lalu dikerjakan: **Patch 0 (`F4 v2`)** dan **F5a (`system.html`)**, keduanya diterapkan pemilik lewat `git am`.
Review visual: TIDAK ada. Pemilik hanya menjawab "OK" pada rencana dan menempelkan hasil `git am`; F4b, F4c, F5a (dan F1a-F4a) TETAP belum direview secara visual.
Belum dikerjakan: **F5b (lupa password, atur ulang password, terima undangan; contract-first), F5c (login: state `csrf` dan tautan "Lupa password?" hidup), F6 (`DESIGN-HANDOFF.md`, galeri final, README, prompt M10).**
Tidak ada berkas di `backend/**`, `apps/**`, `packages/**` yang disentuh.

## 2. Keputusan desain dan status

| Keputusan | Status |
|---|---|
| Semua keputusan partial-1 sampai partial-6 | tetap seperti tercatat di sana |
| Patch 0 `F4 v2` dikerjakan lebih dulu | DISETUJUI pemilik ("ok", default) |
| 401 TIDAK punya halaman sendiri: alur memakai ulang `login.html?state=expired` (satu pesan jujur; `session_expired`/`session_revoked` dicadangkan di kontrak tapi tidak dikirim) | DISETUJUI pemilik ("ok", default) |
| MFA ditunda, tidak didesain (docs/05 §6 berlabel Phase 2; tanpa path di swagger); dicatat di handoff sebagai pekerjaan mendatang | DISETUJUI pemilik ("ok", default) |
| Kode rujukan (`request_id`) ditampilkan di halaman galat 5xx dengan tombol Salin | DISETUJUI pemilik ("ok", default) |
| F5a: satu halaman `system.html` (8 state), `offline` dan `boundary` MANDIRI tanpa shell (kelas `.z-solo`), sisanya di dalam shell; 403 memakai ulang teks `sx.forbidden.*` | usulan saya, BELUM direview |
| F5a: banner offline (`offlinebar`) `role="status"` di atas halaman yang tetap tampil, terpisah dari halaman offline penuh | usulan saya, BELUM direview |
| F5a: 503 = hitung mundur dari `Retry-After` (minimum 1 detik, bawaan 30), hitungan visual `aria-hidden`, akhirnya diumumkan sekali lewat live region; tombol Coba lagi nonaktif selama hitung mundur | usulan saya, BELUM direview |
| F5a: ikon 404 = `inbox`, offline = `exclamation-triangle` (ikon `search`/`wifi-off` lebih tepat; butuh pemilik menjalankan `build-icons.py`) | usulan saya, BELUM direview |
| F5a: `h1` state aktif menerima fokus saat dimuat (meniru perpindahan rute SPA); cincin fokus tampak di halaman galat | BUTUH keputusan pemilik: biarkan atau sembunyikan untuk `h1[tabindex="-1"]` |
| Token warna `<code>` (`--z-code` -> jembatan `--bs-code-color`, Sass `$code-color`) ditambah di `gen-theme.py` | usulan saya (Patch 0), BELUM direview; kontras terendah terukur 7,82:1 (sebelumnya 4,501:1) |
| Usulan F4b dan F4c (partial-6 §2) | masih BELUM direview secara visual |
| Referensi/merek, register publik vs undangan, teks alasan Simulator `no_permission`/`scope_mismatch` | TETAP TERBUKA (default) |

## 3. Halaman dan state (semuanya via `?state=`; belum ada yang DIREVIEW pemilik)

| Berkas | State | Status |
|---|---|---|
| `components.html`, `dashboard.html`, `login.html`, `shell.html`, `security-*.html`, `roles.html`, `role-new.html`, `role-detail.html`, `role-assignments.html`, `role-assignment-new.html`, `policies.html`, `policy-edit.html`, `simulator.html` | seperti partial-6 | diserahkan |
| `policies.html` (Patch 0) | + tabel `z-tbl-tight` (padding sel kecil di bawah 576 px) dan blok `#po-none` "Tidak ada policy yang cocok" bila filter menghabiskan baris | diserahkan |
| `system.html` (F5a) | s403, s404, s500, s503, retry, offlinebar, offline, boundary (8) | diserahkan, diterapkan pemilik, BELUM direview |

Perilaku F5a (`design/system.js`; JS vanilla; di SPA menjadi satu komponen halaman galat dengan varian):
- `s403`: ikon shield-lock, teks 403 seragam, CTA "Kembali ke Dashboard" (tanpa alasan dan tanpa janji solusi). `s404`: "Halaman tidak ditemukan" (404 untuk RUTE; 404 sumber daya punya state sendiri di tiap halaman).
- `s500`/`retry`: `role="alert"`; kode rujukan contoh `contoh-7f3a9c2e` (format `request_id` TIDAK ditetapkan swagger) + tombol Salin (`aria-label` "Salin kode rujukan", teks terlihat "Salin"; `navigator.clipboard`, toast sukses atau galat). `retry`: tombol nonaktif + spinner, wilayah menjadi `role="status"` + `aria-busy="true"`, label "Mencoba lagi…".
- `s503`: `?retry=detik`; `tick` tiap detik memakai kunci `al.rate.c`; saat habis tombol aktif, teks menjadi "Anda dapat mencoba lagi sekarang." dan `#sy-live` (visually-hidden, `role="status"`) mengumumkannya sekali.
- `offlinebar`: banner `alert-warning` + halaman contoh; di SPA banner disisipkan dinamis (diumumkan) saat `offline`, hilang saat `online`, tindakan simpan nonaktif (perilaku belum diperagakan di prototipe).
- `offline` dan `boundary`: tanpa shell; pemilih bahasa dan mode gelap tetap ada; `boundary` `role="alert"`, CTA Muat ulang + Dashboard. Berlaku juga di mode minibar (nama merek tetap tampak).
- Judul dokumen mengikuti `h1` aktif (`<title data-i18n>` diubah lewat `dataset.i18n` lalu `ZApp.apply()`); fokus ke `h1`.
Berkas pendukung diubah: `states.js` (+8 label berawalan `s403`, `s404`, `s500`, `s503`, `retry`, `offlinebar`, `offline`, `boundary`; nama `notfound` sudah dipakai halaman peran), `i18n.js` (+2 kunci `po.nomatch.*` dan +19 kunci `sy.*` per bahasa; total 585 per bahasa menurut node, identik id-ID/en-US), `components.css` (+`.z-solo*`, +`.z-tbl-tight`), `theme.css` (dihasilkan; +7 baris token `code`), `design/tools/gen-theme.py`, `index.html` (tautan F5a). `design/README.md` BELUM memuat daftar halaman (F6).

## 4. Patch dan commit (laptop `~/projects/zago-core-m03`, branch `design/ui-prototype`)

| Patch | sha256 (16 kar.) | Commit di laptop | Push |
|---|---|---|---|
| `design-f4c-v1.mbox`, `design-checkpoint6-v1.mbox` | seperti partial-6 | `95d079e`, `ce16e5b` | di-push (terbukti: `origin/design/ui-prototype` = `ce16e5b` di awal chat ini) |
| `design-f4v2-v1.mbox` (Patch 0) | `7b2730dfe2a234f8` (hasil `sha256sum` pemilik: TIDAK ditempel) | `af6126e` (ditempel pemilik, `ahead 1`) | BELUM; keluaran push tidak ada |
| `design-f5a-v1.mbox` | `fa92d8113ca6ef65` (hasil `sha256sum` pemilik: ditempel, cocok) | sha TIDAK ditempel (hanya "Applying: feat(design): F5a ..." tanpa `git status`) | BELUM; keluaran push tidak ada |
| `design-checkpoint7-v1.mbox` (dokumen ini) | lihat pesan chat | lihat pesan chat | lihat pesan chat |

Penulis commit di sandbox: `master-abror <ruangedu404@gmail.com>`. Pemilik dapat men-push Patch 0, F5a, dan dokumen ini sekaligus (satu `git push`).

## 5. Bukti terakhir (apa adanya)

Ditempel pemilik: `git status -sb && git log --oneline -10 --decorate && git ls-files | wc -l` awal (sejajar origin di `ce16e5b`, 632 berkas); keluaran perintah Python atas swagger `origin/milestone/m05-organization` (27 path; path auth/security sama dengan `main`: `csrf`, `login`, `logout`, `me`, `refresh`, `switch-organization`, `/security/password`; TIDAK ada path lupa password, atur ulang password, undangan, register, MFA); setelah `git am` Patch 0: `[ahead 1]`, `af6126e` di atas `ce16e5b`, `lint-structure OK`, 632 berkas; `sha256sum` mbox F5a = `fa92d8113ca6ef65`; `git am` F5a: "Applying: feat(design): F5a ...". Tidak ditempel: `sha256sum` mbox Patch 0, `git status`/`git log` sesudah F5a, jumlah berkas sesudah F5a, `git push` apa pun, `git fsck`.
Di sandbox (bukan dari laptop): zip `platform-repo-20261126-1737.zip` memuat 632 berkas; `lint-structure OK` pada salinan zip dan pada salinan bersih sesudah kedua mbox dipasang berurutan (634 berkas, pohon identik dengan pohon kerja via `diff -r`); `git diff --check` bersih; `node --check` semua JS yang diubah; paritas i18n 585/585; `gen-theme.py`: 94 pasangan kontras, 0 gagal, `check-theme-vars OK (106 variabel --bs-*)`, `theme.css` tidak berubah setelah dijalankan ulang; tidak ada id ganda, rujukan aria/jangkar hilang, ikon hilang, kunci i18n hilang, `style=`/`on*`, tag tak seimbang, atau `hidden` + `d-*` di `system.html` dan `policies.html`.
Chromium (Playwright, lewat HTTP lokal): (a) Patch 0: overflow tabel policy di 360 px 0 px di semua state; state `disabled` + Aktifkan -> `#po-none` tampil, tabel disembunyikan, fokus ke `po-h1` di 360 dan 1280; kontras `<code>` terukur dari DOM di 3 palet x terang/gelap, terendah 7,82:1; (b) F5a: 8 state x lebar 360/768/1280 x terang/gelap x id-ID/en-US = 96 kombinasi, 0 masalah pada: tepat satu `h1` dan satu `main` terlihat, kunci i18n mentah, overflow horizontal, galat konsol, `document.title` = teks `h1`; hitung mundur `?retry=3` (3 detik -> teks siap, tombol aktif, live region terisi), `?retry=0` -> 1 detik, state `retry` (`role=status`, `aria-busy`, tombol nonaktif, spinner), salin (isi clipboard dan toast sukses), minibar (nama merek terlihat di halaman mandiri), tombol drawer terlihat di 360; (c) sapuan seluruh halaman `design/*.html` (selain galeri) x 360/1280 x terang/gelap: 64 kombinasi, 0 masalah pada overflow, galat konsol, kunci i18n mentah.
Bug dan temuan sendiri: `?retry=0` jatuh ke 30 detik (`0 || 30`), diperbaiki sebelum mbox (minimum 1 sesuai kontrak); server HTTP di sandbox mati di antara panggilan bash (lihat §10).
TIDAK ada: review visual pemilik (F1a-F5a); tangkapan layar yang dinilai pemilik (saya melihat sendiri dua tangkapan F5a dan satu F4 v2: bukan pengganti review); uji keyboard dan pembaca layar manual; uji jaringan offline sungguhan; Firefox/Safari; `git push` terbukti dengan keluaran.

## 6. Temuan kontrak (F5 DIVERIFIKASI sebagian di chat ini)

- `docs/05` §13 (rancangan, BELUM ada di swagger): `POST /auth/forgot-password` selalu menjawab sama apa pun emailnya; `POST /auth/reset-password {token, new_password}` (token sekali pakai, berlaku 30 menit; token tidak sah, terpakai, dan kedaluwarsa memberi jawaban IDENTIK; sukses mencabut semua sesi); `POST /auth/accept-invitation {token, display_name, password}` (berlaku 72 jam, aturan token yang sama; TIDAK ada endpoint untuk melihat isi undangan, jadi halaman tidak dapat menampilkan organisasi/peran undangan); MFA di §6 berlabel Phase 2. Angka dan nama bidang ini dari bacaan `docs/05` di chat ini; verifikasi ulang sebelum menulis F5b bila ragu.
- Swagger `main` dan cabang m05: tidak ada path di atas. Login: respons 400/401/403/423/429; 403 dapat `account_suspended` ATAU `csrf_failed`; kunci terkunci sementara memakai `invalid_credentials`, `account_locked` 423 hanya untuk `users.status = 'locked'`; `Retry-After` dalam detik, minimum 1. Kode `session_expired`/`session_revoked` dicadangkan tetapi tidak dikirim: semua kegagalan sesi = `authentication_required` 401. Kebijakan password: minimal 12 karakter, maksimal 256 byte, tanpa aturan komposisi.
- Bentuk galat `{error:{code,message,details,request_id}}`; 503 membawa `Retry-After`; kode `internal_error` ada di `docs/08` tetapi tidak di swagger (swagger hanya mencantumkan status 503).
Contract-gap yang masih berlaku: seperti partial-6 §6, ditambah batas panjang `display_name` (tidak diketahui; jangan dikarang) dan format `request_id`.

## 7. Kekurangan dan revisi tertunda

1. SEMUA review visual (F1a-F5a) oleh pemilik, terutama: F5a (halaman galat di dalam shell vs mandiri, cincin fokus `h1`, ikon 404/offline, banner offline, hitung mundur 503), F4c (kolom form/hasil di 360 px, `<details>` resource tertutup, lencana mode gelap), F4b (label efek, banner global).
2. Belum diuji keyboard dan pembaca layar manual: F5a (live region 503, `role=alert` vs `status` pada `retry`, fokus `h1`), F4c, toast/skeleton.
3. `offlinebar` hanya statis: perilaku `online`/`offline` event dan penonaktifan tombol simpan belum diperagakan (hanya didokumentasikan).
4. Pemilih aksi policy, editor permission peran, dan `<select>` aksi Simulator harus menjadi satu komponen bersama di SPA.
5. Dari partial-5/6 yang masih terbuka: kolom "Diubah" di tabel policy membungkus; contoh ekspresi ilustratif (tidak dijalankan lewat parser Go asli); butir partial-3 §7 (toast/skeleton pembaca layar, label bilah state tanpa i18n, badge primer Lagoon/Ember dekat 4,5, `DESIGN-HANDOFF.md`, README tanpa daftar halaman). `login.html` menautkan `forgot-password.html` yang BELUM ada (satu-satunya tautan rusak di `design/*.html`; ditutup di F5b).

## 8. Keputusan terbuka (perlu jawaban pemilik)

1. Review visual F1a-F5a (belum ada sama sekali).
2. Cincin fokus `h1` pada halaman galat: biarkan atau sembunyikan untuk `h1[tabindex="-1"]`?
3. Ikon 404 dan offline: tambah `search` dan `wifi-off` ke `NAMES` di `design/tools/build-icons.py` (pemilik menjalankan dan meng-commit sprite terpisah) atau pertahankan `inbox`/`exclamation-triangle`?
4. Konfirmasi teks alasan Simulator untuk `no_permission` dan `scope_mismatch` (tafsiran dari nama konstanta).
5. Referensi/merek; register publik vs undangan (swagger `main` dan m05 tidak punya register: default = undangan saja, sesuai teks login); sumber izin efektif UI dan endpoint daftar organisasi (contract-gap); parsing `user_agent`, makna `auth_level`, nilai `module` katalog inti.
6. Format `request_id` (swagger tidak menetapkan) dan batas panjang `display_name` undangan (belum ada di kontrak).

## 9. Rencana fase berikut

- **Awal chat berikutnya: tinjauan F4b, F4c, F5a** (jawaban pemilik atas §8, tangkapan layar atau catatan), revisi bila ada (patch `v2`), lalu `git push` (termasuk Patch 0, F5a, dan dokumen ini bila belum).
- **F5b: tiga halaman contract-first**, masing-masing dengan spanduk kecil "Contract-first: endpoint belum ada", dibangun dari `shell.html`/pola layar penuh `login.html` (`.z-auth`), kunci i18n id-ID/en-US:
  - `forgot-password.html`: email -> `sent` (pesan generik identik untuk semua email, tanpa petunjuk akun ada). State: normal, loading, validation, sent, rate (429 + hitung mundur seperti login), error. Tanpa tombol "kirim ulang".
  - `reset-password.html?token=`: satu field password baru dengan tombol tampilkan (tanpa field konfirmasi; API tidak memerlukannya), `autocomplete="new-password"`, aturan 12 karakter (galat `short`, `long`), state `token` (satu pesan untuk tidak sah/terpakai/kedaluwarsa), success (semua sesi dicabut, CTA "Masuk"), rate, error. Pola password dari `security-password.html`.
  - `accept-invitation.html?token=`: field `display_name` dan password; tanpa ringkasan undangan (contract-gap); batas panjang `display_name` TIDAK dikarang; state seperti halaman atur ulang.
- **F5c: login**: tambah state `csrf` (403 `csrf_failed` -> "Muat ulang halaman"; kunci `al.csrf.*` dan `sx.reload` sudah ada) dan tautan "Lupa password?" hidup. MFA TIDAK didesain.
- **F6**: `design/DESIGN-HANDOFF.md` (prinsip UX, token dan padanan Sass termasuk `$code-color`, inventaris komponen dengan pemetaan Svelte dan perilaku JS, inventaris halaman dengan rute usulan dan endpoint, kode galat -> perilaku UI, termasuk 401 = alur ke login, daftar string, keputusan terbuka, keterbatasan, MFA = pekerjaan mendatang), galeri `design/index.html` final, `design/README.md` dengan daftar halaman, ringkasan keputusan untuk ADR frontend (rentang ADR-0030–0039), dan PROMPT fase Svelte SPA (M10).
- Checkpoint berikutnya: `HANDOVER-UI-DESIGN-partial-8.md` dan `PROMPT-UI-DESIGN-partial-9.md`. Setiap fase: usulan singkat -> patch kecil -> tinjau -> revisi -> push.

## 10. Catatan proses (agar tidak terulang)

- Semua catatan §10 partial-2 sampai partial-6 tetap berlaku (Playwright + Chromium ada di sandbox; uji lewat HTTP bukan `file://`; jebakan `d-*` vs `hidden`; `.visually-hidden` di `.table-responsive`; `ZApp.apply()` menimpa `textContent`; nama state berawalan; jangan `sed` global untuk kunci state; pembuatan mbox dari zip tanpa `.git`; `bash -c`; cara membaca `ZI18N` di node; `textContent` bukan `innerText`).
- Server HTTP latar belakang di sandbox MATI di antara panggilan bash: jalankan `python3 -m http.server -d design 8765 >/tmp/http.log 2>&1 &` di panggilan yang SAMA dengan skrip uji, lalu `kill %1` di akhir (status keluar 1 dari `kill` tidak berarti uji gagal).
- Keluaran `cut -c` pada teks non-ASCII merusak alat bash (keluaran tidak valid UTF-8, tidak ada hasil): cetak lewat Python (`print(open(...).read())`) atau hindari `cut -c`.
- Preferensi tampilan di `localStorage`: `z-mode` (light/dark/auto), `z-palette`, `z-lang`, `z-nav` (side/min); untuk uji, set lewat halaman yang sama-origin lalu muat ulang.
- `states.js` memiliki satu peta label `L` bersama SEMUA halaman: nama state baru wajib unik (`notfound` sudah milik halaman peran; F5a memakai `s403`/`s404`/`s500`/`s503`). `<body data-states="a,b,c">` dipisahkan KOMA; `data-when` dipisahkan SPASI.
- `system.html` dibangun dari `shell.html` lewat skrip ber-assert (sambung `<main>` dulu; hapus `is-active`/`aria-current` pada menu; pembungkus `.z-app` diberi `data-when`; wadah mandiri `.z-solo` dengan `<main id="main-solo">`, `id` unik; `#z-toasts` wajib karena ada `ZUI.toast`). Skrip tidak ada di repo.
- Pemeriksaan wajib sebelum mbox (dijalankan di chat ini): paritas i18n, kunci terpakai, ikon, id ganda, rujukan aria dan jangkar, tag seimbang, `style=`/`on*`, `hidden` + `d-*`, `lint-structure`, `git diff --check`, sapuan browser seluruh halaman, bukti `git am` berurutan pada salinan bersih zip.
- Jawaban ambigu pemilik ("ok", "sudah saya terapkan") BUKAN review visual dan bukan bukti keluaran; selalu minta tempelan keluaran dan catat yang tidak ditempel. Patch yang diterapkan tetapi belum di-push berisiko tertinggal: ingatkan `git push` dan minta keluarannya.
