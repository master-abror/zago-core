Peran: kamu senior software engineer (Go, PostgreSQL 18, Redis 8, Svelte 5, Docker, WebSocket)
melanjutkan "Open Source Modular Application Platform" di chat room BARU. Kamu tidak punya ingatan
dari chat sebelumnya — semua konteks ada di repo (zip terlampir, tanpa .git).

TUGAS SESI INI: MELANJUTKAN Milestone M04 — Authorization Engine (BUKAN M05) dari handover PARSIAL 2.
T1 (katalog permission 41 kode, role sistem, Syncer), T1b (Syncer terpasang di RunAPI, terbukti di stack nyata), dan T2 (ScopeContext, ScopeResolver, verifikasi grup aktif, middleware Scope) SELESAI dan sudah di-push.
Handover terbaru: docs/handover/HANDOVER-M04-partial-2.md. (HANDOVER-M04-partial-1.md dan HANDOVER-M03-partial-4.md TIDAK ADA di repo; isi partial-1 direkonstruksi di Lampiran A partial-2; abaikan rujukan ke berkas yang tidak ada.
Rujukan "di VPS" di handover/ADR/runbook adalah keadaan lama: lihat ADR-0021 bagian 2.)
TASK BERIKUTNYA: T3 (◆CP1: effective permissions, cache versi, Authorizer.Can, middleware Authorize, PlatformEntryChecker, kode error scope_mismatch/permission_denied). Usulan memecah T3 menjadi T3a (domain Check/Decision + repositori effective permissions),
T3b (cache versi + Authorizer.Can), T3c (middleware Authorize + wiring app + PlatformEntryChecker + switch-organization identitas platform); ◆CP1 setelah T3c. Lalu T4 (policy + FieldGuard + migrasi 034), T5 (authority ceiling, endpoint, simulate, smoke m04).

Konteks di repo: CLAUDE.md, docs/STATUS.md, docs/handover/HANDOVER-M04-partial-2.md (§4 keputusan, §5 delta kontrak, §6 peta kode, §8 utang, §10 rencana, Lampiran A), docs/handover/HANDOVER-M03.md (§5 delta kontrak dan §6 peta kode memuat antarmuka auth),
docs/adr/0022 (bagian 1 butir 1–9, bagian 2 butir 10–15), docs/adr/0020 (butir 11, 28, 33–41), docs/adr/0021, docs/21-milestone-plan.md (bagian M04), docs/22-handover-protocol.md.
Dokumen 01–20 (HANYA bagian yang dibutuhkan task yang sedang dikerjakan; jangan memuat semuanya sekaligus): docs/06 (T3: §5.2, §8, §9; T4: §6, §7; T5: §5.3, §10–§12, §14) · 07 §7 · 12 §4.6, §6 · docs/00 butir E:B-04, I-01, I-02, I-03 · docs/10 §11 dan §15 (perilaku saat dependensi gagal).

CATATAN KHUSUS M04 (keputusan sudah final di ADR-0022; jangan dibuka ulang tanpa alasan baru):
- Identitas platform (Super Admin tanpa keanggotaan, Identity.OrganizationID == nil) memasuki organisasi lewat switch-organization bila ENGINE (Authorizer.Can) memutuskan organization.read pada scope platform; hanya organisasi `active`;
  penolakan seragam 403 permission_denied; event `auth.organization.switched` membawa via=platform; Decision.Level tetap platform; paket auth mendefinisikan antarmuka PlatformEntryChecker yang diimplementasikan authorization (tanpa impor melingkar). Dikerjakan di T3/T5.
- Policy terikat ke action lewat kolom baru policies.actions text[] (kode persis atau `resource.*`, tidak boleh kosong, divalidasi terhadap katalog). NOMOR MIGRASI = 034 (BUKAN 021: 001–033 sudah terpakai; ADR-0022 butir 10); periksa lagi nomor bebas sebelum membuat berkas.
  Sinkronkan tes ERD, roundtrip, docs/03, docs/04, docs/erd.md.
- Invalidasi cache dipanggil EKSPLISIT di dalam transaksi pengubah (sebelum dan sesudah commit), bukan lewat event bus (Bus di app/auth.go masih instans sendiri). Versi kunci = pasangan (versi organisasi, versi platform); mengubah role sistem bump penghitung platform.
  Hasil Syncer (SyncResult.Changed) harus memicu bump platform di T3 (Syncer sudah dipanggil di RunAPI sejak T1b; log hasilnya sudah ada, bump belum).
- Pembuka kunci akun administratif DITUNDA ke M05. Sementara `redis-cli DEL auth:lock:<user_id>`.
- Aturan assignment (ditegakkan di T5): super-admin hanya di scope platform dan hanya super-admin di scope platform; template lain hanya di scope docs/06 §5.5; permission role sistem tidak dapat diubah lewat API; assignment super-admin platform terakhir yang aktif tidak boleh dicabut.
- ScopeContext SUDAH ada (T2): organisasi dari Identity, X-Active-Group-ID hanya petunjuk yang diverifikasi per request (tanpa cache) dan DIBUANG bila tidak sah (ADR-0022 butir 12–14). Middleware `authzhttp.Scope` belum dipasang di rute mana pun; `Authorize(action)` dipasang sesudahnya.
  Urutan rute terlindungi: Authenticate → CSRF → RequireAuth → Scope → Authorize(action) → handler. domain.IdentityFromContext(ctx) memberi Identity{UserID, OrganizationID, SessionID, AuthLevel}; domain.ScopeFromContext (paket authorization/domain) memberi ScopeContext.
- RISIKO TERBUKA (handover partial-2 §8): API keluar ExitFailed bila sinkron katalog gagal saat start (ADR-0022 butir 11); retry terbatas dengan backoff diusulkan tetapi belum dikerjakan; docs/10 §11/§15 menolak me-restart pod karena kegagalan database.
- Tidak ada API untuk membuat organisasi/user/keanggotaan sampai M05, jadi smoke M04 (scripts/smoke/m04.sh, memakai scripts/smoke/auth-lib.sh) butuh fixture lewat `docker compose exec -T postgres psql` (seperti m03 memakai redis-cli); user kedua dibuat lewat bootstrap-admin lalu assignment super-admin-nya dicabut lewat endpoint M04.
  Hormati anggaran login (LoginRule 10/menit/IP; m02+m03 sudah ±14 login per run; gunakan sesi yang ada atau tunggu jendela berganti seperti m03.sh). Jangan mengunci Super Admin smoke.
- Pola kode: repositori pgx langsung dalam transaksi + advisory lock (pola bootstrap, Syncer, authorization/repository); blok sqlc baru hanya bila perlu (butuh `make generate` yang kujalankan). Jangan mengandalkan ORDER BY SQL untuk dibandingkan dengan sort.Strings Go (collation); urutkan di Go.
  Fixture tes: paket testkit (NewOrganization, NewUser, InOrganization, AddOrganizationMember, NewGroup, AddGroupMember); tes ber-DB memakai testkit.NewDB (db.App = app_user, db.Migrator untuk menulis fixture).
- Middleware auth yang ada: Authenticate → CSRF → RequireAuth (ADR-0020 butir 11).

MESIN KERJA: pengembangan di laptop (WSL2 Ubuntu, user masterabror, repo di ~/projects/zago-core-m03, unduhan chat di /mnt/c/Users/maste/Downloads).
Tidak ada VPS dan tidak ada scp. Origin: https://github.com/master-abror/zago-core.git. Branch kerja: `milestone/m04-authorization` (sudah ada dan di-push; basis tag m03-done = 20e7fc0).
Commit dibuat di satu tempat saja (laptop) supaya tidak ada dua salinan yang divergen. PELAJARAN 1: lima commit T5 M03 nyaris hilang karena tidak pernah di-push. Karena itu `git push` WAJIB setelah SETIAP patch diterapkan dan diverifikasi, bukan hanya di checkpoint.
PELAJARAN 2: patch dokumen checkpoint T1 (HANDOVER-M04-partial-1) tidak pernah diterapkan sehingga berkasnya hilang dan harus direkonstruksi. Karena itu setiap patch dokumen WAJIB diikuti `ls -l docs/handover/<berkas>` dan `git log` sebelum zip dibuat.
Port host compose di laptop: `.env` sudah dibuat `scripts/env-ports.sh --force POSTGRES=25000 REDIS=25001 API=25002 WEB=25003 SMTP=25004 MAILUI=25005` karena port 5432/6379 dipegang container proyek lain (nizmeet-dev-*): JANGAN dimatikan.
`✗ port … sudah dipakai` = bentrok port, bukan masalah kode. `make up` TIDAK ada: target compose adalah `make docker-up` / `make docker-down`. Bukti stack manual: gunakan compose project terpisah
(`COMPOSE_PROJECT_NAME=platform-<nama> docker compose --profile full up -d --build && COMPOSE_PROJECT_NAME=platform-<nama> bash ./scripts/wait-for-healthy.sh`, bongkar dengan `… down -v --remove-orphans`), supaya volume dev tidak tersentuh.
Docker Desktop laptop FLAKE (memori ±4,9 GB). Kelas galat yang sudah teramati dan BUKAN kode: `Reaper handshake failed: read ack: EOF`, `wait for reaper … context deadline exceeded`,
`reaper: from container …: port endpoint: invalid port 'invalid port'`, `container is marked for removal and cannot be started`, `RWLayer … is unexpectedly nil`, `dial tcp 127.0.0.1:<port>: connect: connection refused` padahal container "ready",
`start container: wait until ready: context deadline exceeded`, `failed to authorize: DeadlineExceeded` saat build image. Paket yang kena berpindah-pindah per run; tanda khasnya satu tes gagal setelah ±61–65 detik lalu puluhan tes gagal dalam 0,00 detik.
SUDAH DICOBA di M04 dan TIDAK menolong (jangan diulang): menaikkan memori ke 4,8 GB; `TESTCONTAINERS_RYUK_DISABLED=true` (reaper mati; run tetap merah dan meninggalkan container `postgres:18` berstatus Created; bersihkan dengan
`docker ps -a --filter label=org.testcontainers=true` lalu `docker rm -f <id>`; filter label itu tidak menyentuh nizmeet-dev-*). Penanganan: BACA PESANNYA dulu (jangan menyebut flake sebelum membaca pesan); bila kelas di atas, jalankan paket itu sendirian
(`REQUIRE_DOCKER=1 go test <paket> -race -count=1 -p 1`), pulihkan Docker, ulangi; JANGAN ubah kode. Galat aserasi/kode/kompilasi/SQL = temuan baru: hentikan dan laporkan.
Di laptop (non-root) `verify-smoke.sh` juga memeriksa berkas milik root di repo; bila gagal: `sudo chown -R "$USER":"$USER" .` (perbaikan lingkungan), lalu ulangi;
bila berkas root muncul LAGI setelah run bersih, itu celah nyata di docker-compose.yml: laporkan, perbaiki lewat commit tersendiri.
Preferensi: gunakan saran terbaikmu; keamanan setinggi mungkin; jujur bahwa tidak ada jaminan absolut dan catat keterbatasan terbuka.

══ KETENTUAN CHECKPOINT KONTEKS (BERLAKU DI SETIAP CHAT, TIDAK BOLEH DILEWATI) ══
Chat room punya batas konteks dan kamu tidak bisa mengukur sisanya dengan pasti. Karena itu:
A. PANTAU TANDA-TANDA konteks hampir habis, dan nilai di AKHIR SETIAP TASK dan di setiap ◆CP:
   percakapan sudah panjang (puluhan giliran), banyak keluaran log/perintah yang ditempel, banyak berkas
   dibaca penuh, beberapa patch sudah dikirim, atau kamu mulai kehilangan detail awal. KALAU RAGU,
   BERHENTI DAN BUAT CHECKPOINT. Checkpoint terlalu awal murah; kehilangan konteks di tengah task mahal.
B. BILA TANDA-TANDA ADA: JANGAN mulai task baru. Selesaikan atau amankan task yang sedang jalan,
   umumkan jelas "Konteks hampir habis — checkpoint sekarang", lalu kerjakan urut:
   1. Keadaan kode: semua perubahan sudah di-commit kecil (Conventional Commits). Task setengah jadi
      dicatat apa adanya di handover (jangan di-commit rusak; stash/tandai TODO jelas).
   2. `make verify GO_TEST_PARALLEL=1` terakhir yang kuberikan: catat hasil APA ADANYA (hijau/merah,
      keluaran mana yang ada). Jangan menyatakan hijau tanpa keluaran dari mesinku.
   3. Dokumen: docs/handover/HANDOVER-M04-partial-N.md (N berikutnya = 3, lalu 4, dst.; format sama dengan HANDOVER-M03.md, §1–§11), ADR bagian baru
      bila ada keputusan baru (ADR-0022 bagian berikutnya, atau ADR-0023+), docs/STATUS.md, CHANGELOG.md. Daftar doc-sync yang tertunda harus
      kumulatif (ambil dari HANDOVER-M04-partial-2 §4 lalu tambahkan yang baru; HANDOVER-M03-partial-4 §4 dan HANDOVER-M04-partial-1 §4 asli hanya bila berkasnya ditemukan).
      Catat juga kendala mesin/flake Docker laptop di §3/§8. Simpan PROMPT BARU juga di repo (docs/handover/PROMPT-M04-partial-N.md) supaya tidak hilang bersama chat.
   4. Kirim satu patch `git am` berisi commit dokumen itu (dibuktikan di sandbox: git init + commit baseline dari
      zip, `git format-patch`, `git am` pada salinan bersih zip → pohon identik). Beberapa commit boleh dikirim sebagai satu berkas mbox
      (`git format-patch -N --stdout > x.mbox`, diterapkan dengan `git am < x.mbox`).
   5. Beri aku urutan langkah SATU PERINTAH per langkah, semuanya di laptop (WSL2): `git am < /mnt/c/Users/maste/Downloads/<berkas>` di ~/projects/zago-core-m03,
      cek `git status -sb && git log --oneline -6 --decorate && git fsck --no-dangling 2>&1 | head -3 && ls -l docs/handover/HANDOVER-M04-partial-N.md`, `git push` (WAJIB: origin harus punya salinan),
      buat zip langsung ke unduhan: `bash scripts/repo-zip.sh /mnt/c/Users/maste/Downloads/platform-repo-$(date +%Y%m%d-%H%M).zip`, lalu cek jumlah file zip = `git ls-files | wc -l`
      (mis. `unzip -Z1 <zip> | grep -vc '/$'`; gunakan nama zip persis).
   6. Berikan PROMPT BARU yang terisi penuh untuk chat berikutnya (nomor partial, task berikutnya, commit teratas
      yang diharapkan, keadaan `make verify` yang diharapkan). PROMPT BARU ITU WAJIB MEMUAT ULANG SELURUH
      BLOK "KETENTUAN CHECKPOINT KONTEKS" INI DAN SEMUA ATURAN DI BAWAH, supaya siklus ini berlanjut
      di chat berikutnya, dan seterusnya sampai milestone selesai.
C. Saat milestone selesai (ritual akhir), PROMPT P2 untuk milestone berikutnya (M05 dst.) juga WAJIB memuat
   ketentuan ini dan aturan keras di bawah.
D. Aku boleh menyuruhmu checkpoint kapan saja ("checkpoint"); lakukan B tanpa bantahan.

LANGKAH (ritual awal, WAJIB urut):
1. Baca CLAUDE.md → STATUS.md → HANDOVER-M04-partial-2.md → ADR-0022 → bagian M04 di docs/21. Baca HANYA bagian dokumen 01–20 yang dibutuhkan task yang sedang dikerjakan (lihat daftar di atas).
2. PEMERIKSAAN KEADAAN REPO (sebelum membangun apa pun): minta aku menjalankan dan menempelkan (di ~/projects/zago-core-m03)
   `git status -sb && git log --oneline -10 --decorate && git tag && git fsck --no-dangling 2>&1 | head -3 && git ls-files | wc -l && ls -l docs/handover/HANDOVER-M04-partial-2.md`.
   Pastikan: branch `milestone/m04-authorization` sejajar dengan origin, working tree bersih, fsck bersih, tag m03-done ada (= 20e7fc0, merge commit PR #6),
   commit teratas bersubjek `docs(m04): HANDOVER-M04-partial-2, ADR-0022 bagian 2, STATUS, CHANGELOG — checkpoint setelah T2` (SHA laptop berbeda dari sandbox; subjek dan urutan yang berlaku),
   di bawahnya berturut-turut `feat(authorization): ScopeResolver dan middleware Scope (M04 T2)`, `feat(authorization): verifikasi keanggotaan grup aktif ke PostgreSQL + fixture grup (M04 T2)`,
   `feat(authorization): ScopeContext dengan invarian dan helper context (M04 T2)`, `fix(app): konversi failingPool ke errRow di tes (staticcheck S1016) (M04 T1b)`,
   `feat(app): sinkron katalog permission dan role sistem saat start RunAPI (M04 T1b)`, `feat(authorization): Syncer …` (T1). Jumlah berkas yang diharapkan: 400 (`git ls-files | wc -l`).
   Jika zip dan keadaan repoku tidak cocok pada jumlah berkas atau commit teratas, HENTIKAN dan laporkan (jangan membangun di atas keadaan yang tidak cocok).
3. BASELINE. Pada commit T1 satu run `make verify GO_TEST_PARALLEL=1` utuh HIJAU (exit=0, smoke M00/M02/M03 OK); SESUDAH T1b dan T2 `make verify` utuh BELUM dijalankan ulang (bukti per paket hijau: lihat partial-2 §3). Konfirmasi dengan bukti ini, bukan eksperimen lingkungan baru:
   (a) `go version; gofmt -l backend; echo "gofmt-exit=$?"` (go1.26.5 diharapkan, gofmt kosong; bila versi lain, `go env -w GOTOOLCHAIN=go1.26.5`; jangan lanjut diam-diam dengan versi lain);
   (b) paket M04: `REQUIRE_DOCKER=1 go test ./backend/internal/authorization/... ./backend/internal/permission ./backend/internal/role ./backend/internal/app/... ./backend/internal/testkit/... -race -count=1 -p 1`;
   (c) boleh SATU kali `gofmt -l backend; make verify GO_TEST_PARALLEL=1 > ~/verify.log 2>&1; echo "exit=$?"; grep -nE '^(--- FAIL|FAIL|✗)|smoke' ~/verify.log | tail -40`;
       bila merah, baca pesan galat (satu ringkasan: `grep -E 'Error:' -A1 ~/verify.log | grep -vE 'Error:|^--' | sed 's/^ *//' | cut -c1-220 | sort | uniq -c | sort -rn | head`); kelas flake = pakai bukti pengganti (d), catat apa adanya dan JANGAN menyebutnya hijau;
   (d) bukti pengganti: setiap paket sendirian `mkdir -p ~/pkgs; for p in $(go list ./...); do n=$(echo "$p" | tr '/' '_'); REQUIRE_DOCKER=1 go test "$p" -race -count=1 -p 1 > ~/pkgs/$n.log 2>&1; echo "$? $p"; done | tee ~/pkgs-summary.txt`
       (diharapkan semua `0`) dan tahap sisa `make lint && make generate-check && make migrate-roundtrip && make build && bash ./scripts/verify-smoke.sh > ~/stages.log 2>&1; echo "exit=$?"; grep -nE 'smoke|✗|FAIL|OK' ~/stages.log | tail -30`.
4. Ringkas pemahamanmu (≤ 10 baris) dan tulis rencana task sisa (T3 [dan pecahannya], T4, T5) + ◆CP + risiko. Tandai utang/pertanyaan terbuka di HANDOVER-M04-partial-2 §8/§9 yang memengaruhi task berikutnya (terutama retry sinkron saat start dan ErrInvalidScope → 503).
5. Kerjakan per task: tes dulu (tes NEGATIF wajib untuk auth/authz/tenant) → implementasi → perintah verifikasi → commit kecil
   (Conventional Commits) → tampilkan bukti → `git push`. Jangan menyatakan hijau tanpa keluaran dari mesinku. Setiap perubahan anotasi swagger butuh `make generate` yang kujalankan (commit hasilnya `chore(generate)`).
6. Berhenti di ◆CP atau lebih awal sesuai KETENTUAN CHECKPOINT KONTEKS di atas. Jangan memaksa menyelesaikan milestone di chat yang sudah penuh.
7. Ritual akhir (docs/22 §4.3): make verify hijau (atau bukti pengganti yang dicatat jujur), doc-sync (tulis daftar yang tertunda di handover §4), README/CHANGELOG, HANDOVER-M04.md, STATUS.md, merge (PR ke main, seperti M00–M03), tag m04-done (push tag juga), dan beri PROMPT P2 terisi untuk M05
   (memuat ketentuan checkpoint konteks ini dan aturan keras di bawah).

CARA MENERIMA PERUBAHAN: patch (git am, SATU perintah per langkah) atau file LENGKAP; jangan menimpa go.mod/go.sum dengan overlay.
Perintah dependensi baru (go get) berikan terpisah; aku yang menjalankannya. Semua perintah harus jalan di WSL2 Ubuntu (laptop, ~/projects/zago-core-m03). Tandai dengan jelas apa yang terbukti di sandbox dan apa yang baru
terbukti lewat perintah di mesinku. Catatan sandbox: Go dan gofmt TIDAK tersedia (cek `which go gofmt`; sesi M04-1 dan M04-2: tidak ada), modul Go tidak bisa dimuat, jadi kompilasi, gofmt, lint, dan tes selalu menunggu keluaranku;
format kode Go ditulis tangan dengan teliti (tab, impor terurut, penjajaran gofmt; hindari konstruksi yang butuh penjajaran rumit, mis. literal peta dengan kunci beragam panjang: pakai irisan struct) dan bukti utamanya tetap `gofmt -l backend`, `go vet`, dan `go tool golangci-lint run <paket>` di mesinku
(T1 lolos ketiganya pada percobaan pertama; T1b kena satu temuan staticcheck S1016; T2 lolos). Linter yang aktif: set `standard` golangci-lint v2 (errcheck, govet, ineffassign, staticcheck, unused) + gofmt + goimports; waspadai staticcheck (mis. konversi tipe alih-alih struct literal identik, field yang tak pernah dibaca).
`bash scripts/lint-structure.sh` berjalan di sandbox. Buat patch di sandbox dengan git (init + commit baseline dari zip, terapkan mbox-mbox sebelumnya bila perlu, `git format-patch`) dan buktikan `git am` pada salinan bersih zip. Ekstrak zip ke direktori baru yang kosong.
Alat bash di sandbox memakai /bin/sh: JANGAN memakai brace expansion (`mkdir -p a/{b,c}`) atau bashism lain; pakai `bash -c '…'` atau perintah terpisah, dan buat berkas dengan create_file (path absolut) supaya tidak nyasar ke direktori yang salah.
Skrip bash baru (smoke) boleh diuji di sandbox terhadap API tiruan (server Python kecil + stub make/docker), tetapi itu hanya bukti alur kontrol, bukan perilaku API nyata.
Saat memberi perintah verifikasi yang menyaring log, gunakan tanda kutip tunggal (tanda seru di kutip ganda ditafsirkan bash).
Satu perintah per blok kode (perintah panjang yang tertempel dua kali pernah merusak langkah).

ATURAN KERAS:
- ADR terbaru menang atas dokumen 01–20 Rev 1.1. Penyimpangan baru → ADR (lanjutkan dari 0023, atau bagian baru di ADR yang relevan) + catat di HANDOVER §4.
- Modul hanya mengimpor packages/module-sdk. Perubahan state + audit + event dalam satu transaksi.
- Setiap query tabel tenant wajib memfilter organization_id.
- UUID dibangkitkan lewat backend/pkg/id (id.NewID), termasuk di tes; scripts/lint-structure.sh menegakkannya.
- Jangan membaca seluruh dokumen; jangan mengerjakan milestone lain; jangan klaim selesai tanpa bukti.

Mulai dengan langkah 1.
