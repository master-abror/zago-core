# Handover Protocol
## Open Source Modular Application Platform v1.0

**Document:** 22-handover-protocol.md
**Status:** Baseline
**Previous:** 21-milestone-plan.md
**Next:** 23-prompt-library.md

---

# 1. Prinsip

```text
Repo adalah memori. Chat adalah pekerja sementara.
```

Konteks sebuah chat room terbatas dan makin penuh seiring log, file, dan diskusi. Karena itu:

1. Semua yang perlu diingat harus **tertulis di repo** (kode, ADR, STATUS, HANDOVER) — bukan di ingatan chat.
2. Satu milestone = satu chat room. Chat baru **tidak** mewarisi apa pun kecuali yang dibaca dari repo.
3. Chat baru **tidak membaca ulang seluruh 20 dokumen** — hanya bagian yang dicantumkan di plan milestone (21 §6).
4. Handover harus cukup kecil untuk dibaca cepat (HANDOVER ≤ ±200 baris) dan cukup tepat untuk melanjutkan tanpa bertanya ulang.
5. Kepercayaan pada hasil milestone sebelumnya harus **diverifikasi** (`make verify`), bukan diasumsikan.

---

# 2. Struktur File

```text
CLAUDE.md                          # aturan kerja & peta repo (≤ ±120 baris), dibaca SETIAP sesi
README.md
CHANGELOG.md
docs/
├── 00-errata-and-amendments.md    # catatan perubahan Rev 1.1 (alasan keputusan)
├── 01 … 20-*.md                   # spesifikasi Rev 1.1 (sumber aturan)
├── 21-milestone-plan.md
├── 22-handover-protocol.md        # dokumen ini
├── 23-prompt-library.md
├── STATUS.md                      # satu-satunya sumber "sekarang di mana"
├── erd.md                         # dibuat di M01
├── adr/
│   ├── 0001-stack.md
│   └── NNNN-<judul>.md            # satu keputusan = satu file pendek
└── handover/
    ├── HANDOVER-M00.md
    ├── HANDOVER-M03-partial-1.md  # handover parsial (bila milestone dipotong)
    └── …
scripts/
├── verify.sh                      # dipanggil `make verify`
├── repo-zip.sh                    # zip repo lengkap untuk chat berikutnya (Mode C)
├── handover-pack.sh               # `make handover-pack M=03` (Mode B)
└── smoke/m00.sh … mNN.sh
```

---

# 3. Aturan Satu Milestone, Satu Chat

**Mulai chat baru bila:**
- milestone selesai (DoD 21 §3 terpenuhi), atau
- mencapai ◆CP dan percakapan sudah panjang (banyak file dibuat/dibaca, log tes panjang, kamu mulai membaca ulang hal yang sama), atau
- asisten mulai melupakan keputusan sebelumnya / mengulang pertanyaan yang sudah dijawab / menghasilkan kode yang bertentangan dengan tahap awal chat.

**Jangan** memaksa menyelesaikan milestone di chat yang sudah penuh. Berhenti di ◆CP terdekat → handover parsial (prompt **P4**) → chat baru.

**Jangan** memulai milestone berikutnya di chat yang sama "karena sisa konteksnya masih ada".

---

# 4. Siklus Sesi

## 4.1 Ritual Awal (wajib, urut)

```text
1. Baca CLAUDE.md
2. Baca docs/STATUS.md
3. Baca HANDOVER terbaru (docs/handover/HANDOVER-Mxx.md)
4. Baca bagian milestone ini di docs/21-milestone-plan.md
5. (Opsional) baca bagian docs/00-errata-and-amendments.md hanya bila perlu alasan sebuah keputusan
6. Baca HANYA bagian dokumen 01–20 yang tercantum di "Baca:" milestone
7. Jalankan `make verify` → harus hijau.
     Merah? Perbaiki dulu sebagai task "M{NN-1}-fix"; JANGAN membangun di atas fondasi merah.
8. Tulis rencana singkat: daftar task T1..Tn + ◆CP + risiko. Lalu mulai.
```

## 4.2 Loop Kerja (per task)

```text
tulis tes (terutama tes negatif untuk auth/authz/tenant) → implementasi → make lint test
→ commit kecil (Conventional Commits) → task berikutnya
```

Bila menemukan pertentangan antar dokumen atau keputusan baru: **catat sebagai ADR** (5–15 baris) sebelum melanjutkan. Jangan menyimpang diam-diam.

## 4.3 Ritual Akhir (wajib, urut)

```text
1. make verify hijau
2. Doc-sync: bila implementasi menyimpang dari dokumen 01–20, sinkronkan dokumen yang bersangkutan (dan pastikan penyimpangan itu punya ADR)
3. Perbarui README, CHANGELOG.md
4. Tulis docs/handover/HANDOVER-Mxx.md (template §7) — jalankan prompt P3
5. Perbarui docs/STATUS.md
6. Commit → merge branch milestone → tag mNN-done → push
7. Mode C: serahkan zip repo terbaru (`scripts/repo-zip.sh`) + perintah git; prompt berikutnya = P0 yang sama. Mode B: jalankan `make handover-pack M=NN` dan berikan prompt P2 terisi
```

---

# 5. Mode Transfer Konteks

**Mode C — Zip repo lengkap + prompt P0 (DIREKOMENDASIKAN untuk chat biasa).** Upload **satu zip** berisi seluruh repo (kode + `docs/`), tempel P0 (23). P0 membaca `STATUS.md` untuk menentukan milestone aktif, jadi prompt-nya identik di setiap chat. Di akhir milestone chat menyerahkan **zip repo terbaru** (`scripts/repo-zip.sh`) dan perintah git; kamu upload zip itu ke chat berikutnya. Jangan melampirkan dokumen `.md` satu per satu — file yang dilampirkan terpisah biasanya dimuat penuh ke konteks, sedangkan file di dalam zip tetap di disk dan dibaca selektif. Batasan: sandbox chat umumnya tanpa Docker/jaringan, sehingga `make verify` penuh dijalankan di mesinmu dan hasilnya ditempel ke chat.

**Mode A — Asisten punya akses repo/filesystem/git** (mis. lingkungan agent/Claude Code): cukup `git pull`, lalu tempel prompt. Tidak perlu upload apa pun.

**Mode B — Paket handover ringkas** (bila zip repo terlalu besar): jalankan `make handover-pack M=NN` lalu lampirkan ke chat baru bersama source yang relevan:

```text
handover-Mnn.zip berisi:
  CLAUDE.md, docs/STATUS.md, docs/handover/HANDOVER-Mnn.md,
  docs/21-milestone-plan.md, docs/22-handover-protocol.md,
  bagian dokumen 01–20 yang dibutuhkan milestone berikutnya (dipilih lewat daftar di script),
  tree.txt (git ls-files), git-log.txt (30 commit terakhir), diff-stat.txt
  + source-snapshot.zip (git archive HEAD, tanpa node_modules/.git) — hanya bila diminta
```

Skrip (`scripts/handover-pack.sh`), untuk dibuat di M00:

```bash
#!/usr/bin/env bash
set -euo pipefail
M="${1:?usage: handover-pack.sh <NN>}"
NEXT=$(printf "%02d" $((10#$M + 1)))
OUT="handover-M${M}"
rm -rf "$OUT" && mkdir -p "$OUT/docs/handover"

cp CLAUDE.md "$OUT/"
cp docs/STATUS.md docs/21-milestone-plan.md \
   docs/22-handover-protocol.md docs/23-prompt-library.md "$OUT/docs/"
cp "docs/handover/HANDOVER-M${M}.md" "$OUT/docs/handover/"

# Dokumen 01–20 yang dibutuhkan milestone berikutnya: isi/ubah daftar ini per milestone
# (lihat 21 §6). Contoh untuk M${NEXT}: salin file utuh; pembacaan per-§ dilakukan oleh sesi.
while read -r f; do [ -f "docs/$f" ] && cp "docs/$f" "$OUT/docs/"; done < "scripts/handover-docs-M${NEXT}.txt" || true

git ls-files > "$OUT/tree.txt"
git log --oneline -30 > "$OUT/git-log.txt"
git diff --stat "m$(printf "%02d" $((10#$M - 1)))-done"..HEAD > "$OUT/diff-stat.txt" 2>/dev/null || true

zip -qr "${OUT}.zip" "$OUT" && rm -rf "$OUT"
echo "created ${OUT}.zip"
```

Catatan: file `scripts/handover-docs-MNN.txt` (daftar nama file dokumen per milestone) dibuat di ritual akhir milestone sebelumnya.

---

# 6. Template `CLAUDE.md`

```markdown
# CLAUDE.md — Open Source Modular Application Platform

## Cara kerja (WAJIB)
1. Baca urut: CLAUDE.md → docs/STATUS.md → HANDOVER terbaru → bagian milestone di docs/21.
2. Baca HANYA bagian dokumen yang tercantum di "Baca:" milestone. Jangan memuat semua dokumen.
3. `make verify` harus hijau sebelum mulai. Merah = perbaiki dulu.
4. Satu milestone per chat. Berhenti di ◆CP bila konteks sudah panjang → handover parsial.
5. Prioritas bila bertentangan: ADR terbaru (docs/adr/) > dokumen 01–20 Rev 1.1 > docs/00 (catatan perubahan).
6. Tidak ada penyimpangan diam-diam: tulis ADR (docs/adr/).
7. Tes dulu untuk auth/authz/tenant isolation (tes negatif wajib).

## Stack (dipatok — jangan ganti tanpa ADR)
Go (chi v5, pgx v5/pgxpool, sqlc, golang-migrate lib, go-redis v9, coder/websocket, slog)
PostgreSQL 18 · Redis 8 · Svelte 5 + Vite + TS (SPA) · Docker

## Peta repo
backend/{cmd/{api,worker,migrate},internal/*,pkg/*,migrations}
packages/{module-sdk,ts-sdk,ui} · modules/<code>/ · apps/web · deploy/ · scripts/ · docs/

## Perintah
make setup | dev | verify | test | lint | migrate-up | migrate-roundtrip | generate | smoke | handover-pack M=NN

## Aturan kode ringkas
- Modul HANYA impor packages/module-sdk, tak pernah backend/internal/*.
- Domain/application tak mengenal HTTP; hanya transport yang memetakan error → kode API.
- Setiap query tabel tenant wajib filter organization_id; jalur platform hanya lewat method berlabel eksplisit.
- Perubahan state + audit + event (outbox) dalam SATU transaksi (TxManager).
- Frontend: tak pernah fetch langsung — selalu via core/api. Izin di UI hanya UX, bukan kontrol.
- Rahasia tak pernah di-commit/log. Password/token tak pernah masuk audit metadata.

## Status ringkas
Lihat docs/STATUS.md (jangan duplikasi di sini).
```

---

# 7. Template `docs/STATUS.md`

```markdown
# STATUS

**Terakhir diperbarui:** YYYY-MM-DD oleh sesi M{NN}
**Milestone aktif:** M{NN} — {nama}
**Branch:** milestone/m{NN}-{slug}
**Kesehatan `make verify`:** HIJAU | MERAH (alasan)

| M | Nama | Status | Tag | Handover |
|---|---|---|---|---|
| M00 | Foundation | DONE | m00-done | handover/HANDOVER-M00.md |
| M01 | Database Schema | IN PROGRESS (T4/T6, CP1 lewat) | – | handover/HANDOVER-M01-partial-1.md |
| M02 | Kernel | TODO | – | – |
| … | … | … | … | … |

## Versi yang dipatok
PostgreSQL 18.x.x · Redis 8.x.x · Go 1.x.x · Node 24.x.x  (isi persis di adr/0001-stack.md)

## Dokumen
Rev 1.1 berlaku. Penyimpangan dari dokumen yang sudah disinkronkan: (daftar ADR) · Doc-sync tertunda: (daftar)

## Blocker / catatan lintas milestone
- (kosongkan bila tidak ada)
```

---

# 8. Template `docs/handover/HANDOVER-Mxx.md`

```markdown
# HANDOVER — M{NN} {Nama Milestone}

**Tanggal:** YYYY-MM-DD · **Tag:** m{NN}-done · **Commit terakhir:** <sha>
**Jenis:** LENGKAP | PARSIAL (setelah ◆CP{k}; sisa: T{a}..T{b})

## 1. Ringkasan (≤ 8 baris)
Apa yang sekarang bisa dilakukan sistem yang sebelumnya tidak bisa.

## 2. Status task
| Task | Status | Catatan |
|---|---|---|
| T1 … | DONE | |
| T4 … | DEFERRED | alasan + ke milestone mana |

## 3. Bukti verifikasi
- `make verify`: hijau (tanggal, ringkasan jumlah tes)
- `scripts/smoke/m{NN}.sh`: keluaran ringkas
- Tes negatif yang ditambahkan: daftar

## 4. Keputusan & penyimpangan
- ADR: 0002-…, 0003-… (satu baris per ADR)
- Penyimpangan dari dokumen 01–20: <dokumen §> → <apa yang dilakukan & kenapa> (+ ADR)
- Dokumen yang disinkronkan (Doc-sync): <daftar>

## 5. Delta kontrak
- **DB:** migrasi baru (nomor+nama), perubahan tabel
- **API:** endpoint baru/berubah (method path), kode error baru
- **Event:** nama event baru, publisher/subscriber
- **Config/env baru:** …
- **Permission baru:** …

## 6. Peta kode (tempat menemukan sesuatu)
| Hal | Lokasi |
|---|---|
| … | backend/internal/... |

## 7. Cara menjalankan & memverifikasi
Perintah persis dari clone bersih sampai demo berhasil.

## 8. Masalah diketahui / utang teknis
Daftar jujur: apa yang rapuh, apa yang belum dites, apa yang sementara.

## 9. Pertanyaan terbuka
Keputusan yang butuh jawaban manusia.

## 10. Milestone berikutnya: M{NN+1}
- Baca: (salin dari 21 §5)
- Langkah pertama yang disarankan: T1…
- Risiko/perhatian khusus:

## 11. Prompt untuk chat berikutnya
(salin prompt P2 yang sudah terisi — lihat docs/23-prompt-library.md)
```

**Aturan isi HANDOVER:** faktual dan ringkas; tautkan ke file/ADR, jangan menyalin kode; tidak ada klaim "selesai" tanpa bukti di bagian 3.

---

# 9. Panduan Git (langkah demi langkah)

## 9.1 Model cabang

```text
main                        selalu hijau, hanya berisi milestone yang sudah DONE
milestone/m03-authentication   satu cabang per milestone
```

Hanya merge ke `main` saat DoD terpenuhi. Tidak ada push langsung ke `main`; gunakan PR (walau kerja sendirian, PR memicu CI dan meninggalkan riwayat).

## 9.2 Alur harian

```bash
# awal milestone
git checkout main && git pull
git checkout -b milestone/m03-authentication

# tiap task selesai (tes lulus)
git status                         # periksa apa yang berubah
git add -p                         # pilih perubahan secara sadar (hindari `git add .` buta)
git commit -m "feat(auth): add argon2id hasher with rehash-on-login"
git push -u origin milestone/m03-authentication

# akhir milestone
make verify
git push
# buka PR → tunggu CI hijau → merge (squash TIDAK dianjurkan: pertahankan commit per task)
git checkout main && git pull
git tag -a m03-done -m "M03 authentication done"
git push origin m03-done
```

## 9.3 Format commit (Conventional Commits)

```text
<type>(<scope>): <ringkasan imperatif ≤ 72 karakter>

type : feat | fix | test | docs | refactor | chore | build | ci | perf
scope: auth | authz | db | kernel | module | ws | chat | web | ops | docs …

contoh:
feat(db): add composite FK for group_memberships
test(authz): cover grant_exceeds_authority for group admin
docs(db): sync 04 §13 with grants migration (M01)
fix(auth): equalize timing for unknown email login
```

Satu task ≈ satu-beberapa commit. Jangan mencampur perubahan tak terkait dalam satu commit.

## 9.4 Yang tidak boleh masuk repo

```text
.env (hanya .env.example)      password/token/kunci asli      dump database dengan data nyata
node_modules/   bin/   dist/   file build     *.zip hasil handover-pack
```

Periksa `.gitignore` sudah memuat semua itu di M00. Sebelum commit: `git diff --cached | grep -iE "password|secret|token|key"` untuk pemeriksaan cepat.

## 9.5 Pemulihan

```bash
git restore <file>                 # buang perubahan yang belum di-stage
git restore --staged <file>        # batalkan stage
git revert <sha>                   # batalkan commit yang sudah di-push (aman)
git stash / git stash pop          # simpan sementara pekerjaan setengah jadi
```

Jangan `git push --force` ke `main`. Jangan `git reset --hard` tanpa memastikan tidak ada pekerjaan yang hilang.

## 9.6 Tag

`mNN-done` menandai milestone selesai; `vX.Y.Z` (semver) hanya untuk rilis (M15). Tag `mNN-done` dipakai `handover-pack` untuk menghitung diff.

---

# 10. Panduan Testing

## 10.1 Lapisan (selaras 18 §2)

| Lapisan | Alat | Kapan |
|---|---|---|
| Unit domain/application | `testing` + testify, dependency di-mock | selalu; domain hampir 100% |
| Repository/integrasi | testcontainers (PostgreSQL 18 + Redis 8) — **tanpa mock DB** | setiap query/constraint baru |
| Transport/HTTP | table-driven lewat middleware chain penuh | setiap endpoint |
| Frontend | Vitest (logika), Playwright (alur kritis) | sejak M10 |
| Smoke | `scripts/smoke/mNN.sh` (curl/psql end-to-end) | setiap milestone |

## 10.2 Tes negatif adalah syarat, bukan bonus

Setiap fitur yang menyentuh auth, authz, atau data tenant wajib punya tes yang membuktikan **penolakan**: org lain, group lain, sesi dicabut, izin dicabut, field terlarang, input jahat. Rujukan katalog: 18 §5.

## 10.3 Format smoke script

```bash
#!/usr/bin/env bash
# scripts/smoke/m03.sh — exit non-zero bila ada langkah gagal
set -euo pipefail
BASE=${BASE:-http://localhost:8080}
step() { echo "▶ $*"; }

step "login sukses"
# … curl -sf … | jq -e '.data.user.id'
step "me tanpa cookie → 401"
test "$(curl -s -o /dev/null -w '%{http_code}' $BASE/api/v1/auth/me)" = "401"
echo "M03 smoke OK"
```

Aturan: idempoten (bisa dijalankan berulang), mencetak langkah, gagal keras (`set -e`), tidak bergantung urutan smoke lain kecuali dideklarasikan.

## 10.4 Bukti di HANDOVER

Cantumkan: perintah yang dijalankan, jumlah tes lulus, keluaran ringkas smoke, dan daftar tes negatif baru. Tanpa bukti → status task tidak boleh `DONE`.

## 10.5 Tes flaky

Flaky = bug (18 §8). Karantina hari itu juga (skip + issue tertaut), jangan menambah sleep/retry untuk menutupi race.

---

# 11. Anti-Drift

```text
1. ADR terbaru menang atas dokumen 01–20 Rev 1.1; dokumen disinkronkan di akhir milestone (Doc-sync).
2. Jangan mengganti nomor/urutan migrasi yang sudah di-tag; tambahkan migrasi baru.
3. Jangan mengubah kontrak publik (API, event, module-sdk) tanpa ADR + catatan di HANDOVER §5.
4. Setiap sesi hanya menyentuh area milestone-nya; perubahan lintas milestone dicatat sebagai "Deferred/Fix" di HANDOVER.
5. STATUS.md adalah sumber tunggal status. Jangan menyalin status ke CLAUDE.md atau README.
6. Bila menemukan bug di milestone lama: perbaiki dengan commit `fix(...)` + tes regresi, catat di HANDOVER §4.
```
