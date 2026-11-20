# design/ — prototipe UI/UX statis

Prototipe HTML + CSS + Bootstrap 5.3.9 (tanpa build, tanpa framework JS). Sumber desain untuk Svelte SPA (M10).
Branch kerja: `design/ui-prototype`. Status per fase: lihat `DESIGN-HANDOFF.md` (dibuat di F6).

## Membuka

```bash
python3 -m http.server -d design 8080
```

Buka `http://localhost:8080/`.

## Bootstrap (dipin lokal, offline)

Berkas Bootstrap 5.3.9 tidak ikut patch. Letakkan di `design/vendor/bootstrap-5.3.9/{css,js}` (BUKAN di folder `dist`:
`.gitignore` dan `scripts/repo-zip.sh` membuang `dist/`). Perintahnya diberikan di chat; verifikasi:

```bash
bash design/tools/check-theme-vars.sh
```

## Tema

`theme.css` DIHASILKAN dari `tools/gen-theme.py` (palet, status, kontras WCAG). Ubah nilai di sana, lalu:

```bash
python3 design/tools/gen-theme.py
```

Mode warna lewat `data-bs-theme` (terang/gelap), palet lewat `data-palette` (aurora, lagoon, ember), bahasa lewat `lang` (id-ID, en-US).
Pilihan pengguna disimpan di `localStorage` (hanya prototipe) dan mengikuti `prefers-color-scheme` bila mode "Otomatis".
