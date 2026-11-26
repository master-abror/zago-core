# HANDOVER-UI-DESIGN-partial-6 (checkpoint keenam, fase desain UI/UX)

Dibuat di chat desain #6 pada ◆CP setelah F4c (Simulator) DISERAHKAN dan di-push pemilik. Prompt awal: `docs/handover/PROMPT-UI-DESIGN-start.md`.
Handover sebelumnya: `HANDOVER-UI-DESIGN-partial-1.md` sampai `-5.md`. Prompt lanjutan: `docs/handover/PROMPT-UI-DESIGN-partial-7.md`.
Alasan checkpoint: F4 (tiga patch) selesai, dan chat ini sudah memuat banyak bacaan besar (swagger, ADR-0022, handover, JS, HTML) serta siklus bangun-uji; menurut ketentuan itu tanda konteks menipis, jadi F5 TIDAK dimulai di chat ini. Pemilik menyuruh checkpoint setelah push F4c.

## 1. Ringkasan

Prototipe statis (HTML + CSS + Bootstrap 5.3.9, tanpa build, tanpa framework JS) di `design/`, branch `design/ui-prototype` (dari `m04-done` = `main`; tag terverifikasi pemilik: `6a14f87276ed64ba15af7673beaaebc4725ca79c`, ada di `origin/main`).
Di chat ini: keadaan repo dicocokkan (sejajar origin di `bab64a9`, 628 berkas = jumlah berkas zip), kontrak Simulator DIVERIFIKASI dari swagger, ADR-0022 dan kode, lalu **F4c (Simulator) dibangun, diuji otomatis di Chromium, diterapkan pemilik (`95d079e`) dan di-push pemilik**.
Review F4b: pemilik menjawab "Ok semua" pada rencana (tanpa tangkapan layar atau catatan visual): F4b TETAP belum direview secara visual. F4c juga TIDAK punya tangkapan layar atau catatan visual dari pemilik; pemilik hanya menerapkan dan mem-push tanpa melaporkan revisi.
Belum dikerjakan: **F5 (halaman sistem + contract-first + MFA/`csrf_failed` login) dan F6 (`DESIGN-HANDOFF.md`, galeri final, README, prompt M10).**
Tidak ada berkas di `backend/**`, `apps/**`, `packages/**` yang disentuh.

## 2. Keputusan desain dan status

| Keputusan | Status |
|---|---|
| Semua keputusan partial-1 sampai partial-5 | tetap seperti tercatat di sana |
| Penulis commit | DISETUJUI pemilik: `master-abror <ruangedu404@gmail.com>` (menggantikan `ruangedu404`; commit lama TIDAK diubah; terverifikasi pemilik lewat `git log -1 --format='%an <%ae>'` pada `95d079e`) |
| Halaman ubah policy mengambil record dari daftar (tidak ada `GET /policies/{id}`); dicatat sebagai usulan ke backend, bukan keputusan desain | DISETUJUI pemilik ("Ok semua") |
| Aksi Simulator dipilih dari katalog, tanpa ketik bebas | DISETUJUI pemilik ("Ok semua") |
| Referensi/merek, register publik vs undangan, cakupan MFA dan `csrf_failed` login | TETAP TERBUKA (default; register publik diperiksa di swagger saat F5) |
| Usulan F4b (label efek "Tolak jika benar"/"Syarat wajib", policy global baca-saja + banner, urutan field editor, pemilih aksi `resource.*`, state `scope` tanpa CTA) | masih BELUM direview secara visual |
| F4c: pemilih aksi = `<select>` biasa dengan `<optgroup>` per modul (bukan kombobox dengan pencarian); 43 kode non-wildcard diambil dari `policy-edit.html` | usulan saya, BELUM direview |
| F4c: hasil di region `role="status"`, fokus dipindah ke hasil setelah simulasi; catatan "gambaran keadaan saat ini, bukan jaminan" SELALU tampil (ADR-0022 butir 69g) | usulan saya, BELUM direview |
| F4c: bagian policy (`policy_code`, `deferred_policies`) absen utuh tanpa `policy.read`, dengan satu catatan kecil hanya saat penolakan policy tanpa kode (state `nopolread`) | usulan saya, BELUM direview |
| F4c: galat atribut resource ditempel pada KELOMPOK atribut (bukan baris), karena kontrak melaporkan `resource.attrs` tanpa indeks | usulan saya, BELUM direview |
| F4c: identitas Platform = form dinonaktifkan di muka + CTA "Pilih organisasi" (membuka pemilih organisasi di topbar); pemanggil grup: pesan khusus `scope_mismatch` tanpa CTA | usulan saya, BELUM direview |
| F4c: `user_id` tidak ada, bukan anggota aktif, atau milik organisasi lain = SATU pesan seragam pada field `user_id` | usulan saya (mengikuti kontrak), BELUM direview |
| F4c: teks alasan manusia untuk `no_permission` dan `scope_mismatch` adalah TAFSIRAN saya dari nama konstanta dan komentar kode, bukan dari dokumen semantik resmi | BUTUH konfirmasi (lihat §8) |

## 3. Halaman dan state (semuanya via `?state=`; belum ada yang DIREVIEW pemilik)

| Berkas | State | Status |
|---|---|---|
| `components.html`, `dashboard.html`, `login.html`, `shell.html`, `security-*.html`, `roles.html`, `role-new.html`, `role-detail.html`, `role-assignments.html`, `role-assignment-new.html`, `policies.html`, `policy-edit.html` | seperti partial-5 (menu "Simulator" kini menuju `simulator.html` di 12 halaman bernav: dashboard, policies, policy-edit, role-assignment-new, role-assignments, role-detail, role-new, roles, security-events, security-password, security-sessions, shell) | diserahkan |
| `simulator.html` | normal, loading, allow, noperm, scopemis, policy, polfail, nopolread, deferred, usernf, attrres, attrbad, platform, grpscope, validation, forbidden, csrf, error (18) | diserahkan (F4c), di-push |

Perilaku F4c (di `design/simulator.js`; JS vanilla; di SPA Svelte menggantikan seluruhnya; komentar di kepala berkas memuat bentuk request/response):
- Form: `user_id` (UUID; input teks karena belum ada daftar pengguna = contract-gap), aksi (select katalog), grup aktif opsional (UUID), `<details>` resource hipotetis (jenis, ID, pemilik, grup pemilik, atribut dinamis). Resource dianggap "terisi" bila salah satu field-nya atau satu baris atribut ada; bila terisi, `type` dan `id` wajib. Atribut: nama + tipe (teks/angka/benar/salah/daftar) + nilai (nonaktif untuk benar/salah); maks 32 (tombol tambah dinonaktifkan di 32); nama harus berbentuk pengenal, tidak boleh dicadangkan (`type`, `id`, `organization_id`, `owner_id`, `group_id`) atau ganda; angka divalidasi.
- Validasi sisi klien menurut urutan visual, ringkasan alert + galat per field (`invalid-feedback` + `aria-invalid` + `aria-describedby` dipulihkan saat galat dibersihkan), fokus ke field pertama yang salah; details resource dibuka bila galatnya di dalamnya.
- Kirim: spinner 700 ms (`aria-busy`), lalu hasil menurut state (state non-hasil/non-galat menghasilkan `allow`). Hasil dibangun dari kunci i18n; kode policy dan daftar tertunda diisi lewat `textContent`.
- Hasil: lencana Diizinkan/Ditolak + kode `ALLOW`/`DENY` (tidak hanya warna), alasan manusia + kode `reason`, level (hanya bila ada) manusia + kode, policy penolak (hanya bila ada), policy tertunda dengan penjelasan, catatan tanpa `policy.read`.
- Di SPA: `POST /permissions/simulate` dengan `X-CSRF-Token`, tanpa `Idempotency-Key`; field kosong DIHILANGKAN dari body (bukan string kosong); atribut: teks -> string, angka -> number, benar/salah -> bool, daftar -> array skalar. Server melaporkan SEMUA field salah sekaligus (`validation_failed` dengan rincian). 403 punya tiga arti: `permission_denied` (halaman terlarang seragam), `scope_mismatch` (Platform atau grup lain), `csrf_failed`.
Berkas pendukung diubah: `states.js` (+11 label), `i18n.js` (+69 kunci `sm.*` dan `page.sim.title` per bahasa; total 564 per bahasa, dihitung di node dengan stub `window`; identik id-ID/en-US), `index.html` (tautan F4c). `theme.css` dan `components.css` TIDAK berubah. `design/README.md` BELUM memuat daftar halaman (F6).

## 4. Patch dan commit (laptop `~/projects/zago-core-m03`, branch `design/ui-prototype`)

| Patch | sha256 (16 kar.) | Commit di laptop | Push |
|---|---|---|---|
| `design-f4b-v1.mbox`, `design-checkpoint5-v1.mbox` | seperti partial-5 | `9463523`, `bab64a9` | di-push (origin = `bab64a9` saat chat ini dimulai) |
| `design-f4c-v1.mbox` | `0c906413631ed9f4` (hasil `sha256sum` pemilik: tidak ditempel) | `95d079e` (ditempel pemilik, penulis `master-abror`) | pemilik menyatakan sudah push; keluaran push tidak ditempel |
| `design-checkpoint6-v1.mbox` (dokumen ini) | lihat pesan chat | lihat pesan chat | lihat pesan chat |

Penulis commit di sandbox untuk patch F4c dan dokumen ini: `master-abror <ruangedu404@gmail.com>`.

## 5. Bukti terakhir (apa adanya)

Ditempel pemilik: `git status -sb && git log --oneline -10 --decorate && git ls-files | wc -l` awal (sejajar origin di `bab64a9`, 628 berkas); `git rev-parse m04-done && git branch -r --contains m04-done` (tag di `origin/main`, juga di `origin/design/ui-prototype` dan `origin/milestone/m05-organization`); setelah `git am` F4c: `## design/ui-prototype...origin/design/ui-prototype [ahead 1]`, `95d079e` di atas `bab64a9`, 630 berkas; penulis `master-abror <ruangedu404@gmail.com>`. Tidak ditempel: `sha256sum` mbox F4c, keluaran `git push` F4c, `git fsck`.
Di sandbox (bukan dari laptop): zip `platform-repo-20261126-0737.zip` memuat 628 berkas (cocok dengan laptop); `lint-structure OK` pada salinan zip dan pada salinan bersih setelah `git am` (630 berkas, pohon identik dengan pohon kerja via `diff -r`); `git diff --check` bersih; `node --check` semua JS; paritas i18n 564/564; tidak ada id ganda, rujukan aria hilang, `style=`/`on*`, tag tak seimbang di `simulator.html`; tidak ada kunci i18n terpakai yang hilang.
Chromium (Playwright, lewat HTTP lokal): (a) sapuan 144 kombinasi (18 state x id-ID/en-US x terang/gelap x lebar 360/1280) dengan 0 masalah pada: kunci i18n mentah yang tampak, tepat satu `<h1>`, id ganda, overflow horizontal dokumen, galat konsol, elemen `[hidden]` yang masih tampil, tombol tanpa nama, kontrol tanpa label; (b) 20 pemeriksaan interaksi lolos (validasi dan fokus, `aria-describedby`, UUID, submit sukses, resource dan atribut, tipe benar/salah menonaktifkan nilai, hapus baris memulihkan fokus, batas 32, state policy/nopolread/deferred/platform/usernf/attrres/csrf, CTA membuka pemilih organisasi, ganti bahasa mengganti hasil); (c) kontras teks diukur dari DOM di 3 palet x terang/gelap x 5 state: TERENDAH 4,501:1 (batas 4,5): `<code>` bawaan Bootstrap (#d63384) di atas putih pada Aurora terang; ini berlaku untuk SEMUA `<code>` di prototipe (tidak diukur di halaman lain), sangat tipis.
Bug dan temuan sendiri: pemeriksaan "tombol tanpa nama" memakai `innerText` memberi 96 positif palsu (tombol di dalam `<details>` tertutup); diganti `textContent`. Daftar "kunci `sm.*` tak terpakai" dari regex juga positif palsu (kunci dipakai di string templat JS).
TIDAK ada: review visual pemilik (F1a-F4c); tangkapan layar yang kulihat sendiri (tidak diambil di chat ini); uji keyboard dan pembaca layar manual; lebar 768; Firefox/Safari; `git push` terbukti dengan keluaran; halaman F4a dan F1-F3 belum disapu browser; contoh ekspresi tidak dijalankan lewat parser Go asli.

## 6. Temuan kontrak (Simulator DIVERIFIKASI di chat ini; F5 BELUM)

Simulator (swagger `docs/api/swagger.json`, ADR-0022 butir 69 dan 75, `backend/internal/authorization/transport/http/dto_simulate.go`, `domain/check.go`): `POST /api/v1/permissions/simulate`; izin `permission.simulate`; wajib `X-CSRF-Token`; tanpa `Idempotency-Key`; respons galat 400 `validation_failed`, 401, 403 (`permission_denied`, `scope_mismatch`, `csrf_failed`). Body `{user_id, action, active_group_id?, resource?{type, id, owner_id?, group_id?, attrs?}}`; field lain (termasuk `organization_id`) ditolak `unknown_field`. Validasi: `user_id` wajib UUID; `action` wajib berbentuk kode permission; bila `resource` ada, `type` (`[a-z][a-z0-9_]*`, maks 100 byte) dan `id` wajib; `owner_id`, `group_id`, `active_group_id` tidak boleh UUID kosong; atribut maks 32, nama pengenal maks 64 byte, nilai null/bool/teks (maks 1024 byte)/angka/daftar skalar; nama `type`, `id`, `organization_id`, `owner_id`, `group_id` dicadangkan (`resource.attrs: reserved`). Target yang tidak ada, bukan anggota aktif, atau milik organisasi lain = satu galat 400 `user_id: not_found`. Pemanggil Platform = 403 `scope_mismatch`; pemanggil level grup terkunci ke grupnya dan `active_group_id` lain = 403 `scope_mismatch`; untuk pemanggil level organisasi, grup yang bukan keanggotaan aktif target dibuang diam-diam. Respons `{result: ALLOW|DENY, reason, level, policy_code, deferred_policies}`: `reason` = `granted`, `no_permission`, `scope_mismatch`, `policy_denied`, `policy_evaluation_failed` (kode stabil, bukan kalimat); `level` (null bila DENY) = `platform`/`organization`/`group`/`resource`/`own`; `policy_code` dan `deferred_policies` hanya terisi bila pemanggil punya `policy.read`; `deferred_policies` tidak pernah null; kode aksi berbentuk sah tapi tidak ada di katalog = DENY `no_permission` (bukan galat). Hasil adalah gambaran keadaan saat ini, bukan jaminan (ADR butir 69g). `docs/06` §10 (contoh `reason` kalimat bebas dan objek `evaluated`) sudah digantikan ADR dan swagger.
Catatan hitungan: `policy-edit.html` memuat 43 kode non-wildcard di pemilih aksi (dihitung dengan regex `for="pa-..."`, tanpa baris `resource.*`); angka "45 kode" di partial-5 §6 tidak cocok dan tidak diselidiki lebih jauh.
Swagger di `main` (`m04-done`) memuat 23 path; path `/api/v1/auth/*` hanya enam (diperiksa lewat skrip): `csrf`, `login`, `logout`, `me`, `refresh`, `switch-organization`: TIDAK ada path lupa password, atur ulang password, undangan, register, atau MFA. Itu menguatkan "contract-first" untuk F5. Cabang `origin/milestone/m05-organization` ADA di origin dan memuat `m04-done`, tapi isinya TIDAK diperiksa (zip tanpa `.git`): di F5, minta pemilik menempelkan daftar path swagger cabang itu bila berbeda.
Contract-gap yang masih berlaku: tidak ada `GET /policies/{id}` dan tidak ada DELETE policy; tidak ada endpoint validasi ekspresi; nama atribut `resource.*` yang sah belum ada daftarnya; daftar pengguna/grup/organisasi belum ada (input UUID); izin efektif UI belum ada endpoint; Retry-After ganti password tidak ada di swagger.

## 7. Kekurangan dan revisi tertunda

1. SEMUA review visual (F1a-F4c) oleh pemilik. Kekhawatiran F4c: kolom form dan hasil bertumpuk di bawah `lg` (hasil jauh di bawah form di 360 px; fokus pindah ke hasil, tetapi tidak ada tautan lompat); `<details>` resource tertutup sehingga atribut tidak terlihat sampai dibuka; di mode gelap lencana hasil memakai kelas subtle Bootstrap, belum dilihat mata.
2. F4c belum diuji keyboard dan pembaca layar secara manual: pengumuman region `role="status"` saat hasil muncul, urutan fokus setelah hapus atribut, `aria-busy` pada form, `<select>` optgroup.
3. F4c disederhanakan: pemilih aksi tanpa pencarian (katalog 43 kode); tanpa state `badfilter`/`unknownaction` (kode tak dikenal = DENY `no_permission`, tidak diperagakan sebagai state sendiri); tanpa penyimpanan riwayat simulasi.
4. Kontras `<code>`: sebaiknya token warna kode ditambahkan di `design/tools/gen-theme.py` (menimpa `--bs-code-color`) agar tidak bergantung pada 4,501:1; perubahan `theme.css` dihasilkan oleh skrip, jalankan ulang `gen-theme.py` dan `check-theme-vars`.
5. Pemilih aksi policy dan editor permission peran menduplikasi logika; di SPA harus satu komponen bersama (pemilih aksi Simulator adalah `<select>` ketiga dengan data yang sama).
6. Dari partial-5 yang masih terbuka: tabel daftar policy di 360 px 2 px lebih lebar dari kartu; kolom "Diubah" membungkus; daftar tidak berganti ke state kosong setelah baris terakhir hilang; contoh ekspresi ilustratif; butir partial-3 §7 (toast/skeleton pembaca layar, `forgot-password.html` di F5, MFA dan `csrf_failed` login, label bilah state tanpa i18n, badge primer Lagoon/Ember dekat 4,5, `DESIGN-HANDOFF.md`, README tanpa daftar halaman).

## 8. Keputusan terbuka (perlu jawaban pemilik)

1. Review visual F1a-F4c (belum ada sama sekali), termasuk label efek F4b, banner policy global, dan seluruh tampilan Simulator (susunan dua kolom, lencana hasil, catatan "bukan jaminan").
2. Konfirmasi teks alasan manusia Simulator untuk `no_permission` dan `scope_mismatch` (tafsiran dari nama konstanta; mungkin perlu dikonfirmasi ke pemilik kode otorisasi).
3. Dari partial-3: referensi/merek, register publik vs undangan (periksa swagger di F5), cakupan MFA dan `csrf_failed` login; sumber izin efektif UI dan endpoint daftar organisasi (contract-gap); parsing `user_agent`, makna `auth_level`, nilai `module` katalog inti.
4. Apakah `<code>` perlu token warna sendiri (usulan §7.4) sekarang atau di F6?
5. Isi `origin/milestone/m05-organization`: apakah kontrak lupa/atur ulang password dan undangan sudah ada di sana (memengaruhi apakah F5 tetap "contract-first")?

## 9. Rencana fase berikut

- **Awal chat berikutnya: tinjauan F4b dan F4c** (jawaban pemilik atas §8, tangkapan layar), revisi bila ada (patch `v2`), lalu `git push`.
- **F5**: halaman sistem (401 sesi habis/dicabut, 403 seragam, 404, 5xx, offline, error boundary), halaman contract-first (lupa password, atur ulang password, terima undangan; tandai "contract-first" di dokumen serah-terima), MFA dan `csrf_failed` di login. Baca HANYA: `login.html`/`login.js` (pola 10 state), `security-password.html` (kebijakan password 12 karakter, `autocomplete="new-password"`), swagger bagian auth, daftar kode galat, `docs/05`. Setiap halaman: pengalih state (normal, memuat, kosong, galat umum, terlarang, sesi habis, validasi, sukses) sesuai prompt awal.
- **F6**: `design/DESIGN-HANDOFF.md` (prinsip UX, token dan padanan Sass, inventaris komponen dengan pemetaan Svelte dan perilaku JS, inventaris halaman dengan rute usulan dan endpoint, kode galat -> perilaku UI, daftar string, keputusan terbuka, keterbatasan), galeri `design/index.html` final, `design/README.md` dengan daftar halaman, ringkasan keputusan untuk ADR frontend (rentang ADR-0030–0039), dan PROMPT fase Svelte SPA (M10).
- Checkpoint berikutnya: `HANDOVER-UI-DESIGN-partial-7.md` dan `PROMPT-UI-DESIGN-partial-8.md`. Setiap fase: usulan singkat -> patch kecil -> tinjau -> revisi -> push.

## 10. Catatan proses (agar tidak terulang)

- Semua catatan §10 partial-2 sampai partial-5 tetap berlaku (Playwright + Chromium ada di sandbox; uji lewat HTTP bukan `file://`; jebakan `d-*` vs `hidden`; `.visually-hidden` di `.table-responsive`; `ZApp.apply()` menimpa `textContent`; nama state berawalan; jangan `sed` global untuk kunci state).
- Zip tidak membawa `.git`: untuk membuat mbox di sandbox, `unzip` ke direktori baru, `git init -b design/ui-prototype`, atur `user.name`/`user.email`, `git add -A && git commit` sebagai baseline, salin berkas hasil kerja, `git add -A`, `git diff --cached --check`, commit, `git format-patch -1 --stdout > ...mbox`. Bukti: `unzip` ke direktori bersih lain, baseline commit, `git am`, `diff -r` melawan pohon kerja (tanpa `.git`), `lint-structure`.
- Alat bash sandbox memakai `/bin/sh`: substitusi proses `<(...)` memberi "Syntax error"; bungkus perintah dengan `bash -c '...'`.
- Membaca `ZI18N` di node: berkas `i18n.js` memasang ke `window`; pakai `global.window = {}; new Function('window', src)(window); window.ZI18N` (bukan `require` atau `return ZI18N`).
- Pemeriksaan teks tampak di browser: pakai `textContent` (bukan `innerText`) untuk nama tombol di dalam `<details>` tertutup; `innerText` kosong untuk elemen tak dirender.
- Halaman F4c dihasilkan skrip sandbox yang memakai rangka `policies.html` (kepala + menu + topbar + ekor); skrip tidak ada di repo. Untuk revisi, ubah HTML langsung dengan penggantian berjangkar ber-assert dan jalankan ulang sapuan.
- Jawaban ambigu pemilik ("sudah saya terapkan", "sudah saya push") BUKAN review visual dan bukan bukti keluaran; selalu minta tempelan keluaran dan catat yang tidak ditempel.
