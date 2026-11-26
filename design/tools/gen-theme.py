#!/usr/bin/env python3
"""Sumber token tema prototipe: menulis design/theme.css dan memeriksa kontras WCAG 2.2 AA.
Jalankan dari akar repo: python3 design/tools/gen-theme.py  (keluar 1 bila ada pasangan gagal)."""
import pathlib, sys

def rgb(h): h = h.lstrip('#'); return tuple(int(h[i:i+2], 16) for i in (0, 2, 4))
def trip(h): return ', '.join(map(str, rgb(h)))
def lum(h):
    f = lambda c: c/12.92 if c <= 0.03928 else ((c+0.055)/1.055)**2.4
    r, g, b = (f(c/255) for c in rgb(h)); return .2126*r + .7152*g + .0722*b
def cr(a, b):
    x, y = sorted((lum(a), lum(b)), reverse=True); return (x+.05)/(y+.05)
def mix(a, b, p): return '#' + ''.join(f'{round(x*p+y*(1-p)):02x}' for x, y in zip(rgb(a), rgb(b)))

K = 'bg surface text muted primary onp border strong b1 b2'.split()
def pal(*v): return dict(zip(K, v))
P = {  # palet: terang (L) dan gelap (D)
 'aurora': (pal('#f5f6fb','#ffffff','#171a2b','#5a6280','#4338ca','#ffffff','#e2e5f0','#8a91ad','#4338ca','#7c3aed'),
            pal('#0e1120','#161a2e','#e9ebf8','#a3abc9','#9a96ff','#0e1120','#262b47','#6a7195','#2a2780','#5b21b6')),
 'lagoon': (pal('#f2f8f7','#ffffff','#0f1f1e','#4f6866','#0f766e','#ffffff','#d9e6e4','#7e9996','#0f766e','#0e7490'),
            pal('#0a1514','#102220','#e2f1ef','#94b3af','#2dd4bf','#04201d','#1d3a36','#5f8581','#0b4f4a','#0c5568')),
 'ember':  (pal('#faf7f3','#ffffff','#211a14','#6d5f53','#b7410e','#ffffff','#eadfd3','#9a8a7b','#b7410e','#be123c'),
            pal('#16110d','#201813','#f4ebe2','#b9a898','#fb923c','#1c1005','#3a2d23','#86735f','#7c2d12','#881337')),
}
# status: (solid, on-solid, teks, latar-subtle, garis-subtle)
S = {
 'L': dict(success=('#15803d','#ffffff','#166534','#dcfce7','#bbf7d0'), warning=('#b45309','#ffffff','#854d0e','#fef9c3','#fde68a'),
           danger=('#b42318','#ffffff','#b42318','#fef3f2','#fecdca'), info=('#0369a1','#ffffff','#075985','#e0f2fe','#bae6fd')),
 'D': dict(success=('#4ade80','#0b0f14','#86efac','#14301f','#1f5a36'), warning=('#fbbf24','#0b0f14','#fcd34d','#33260a','#6b4e0f'),
           danger=('#fda29b','#0b0f14','#fda29b','#3a1411','#7a271a'), info=('#38bdf8','#0b0f14','#7dd3fc','#0c2a3a','#155e75')),
}
fails, rows = [], []
def check(label, fg, bg, need):
    v = cr(fg, bg); rows.append((label, v, need));
    if v < need: fails.append(f'{label}: {v:.2f} < {need}')

def tokens(m, c):
    sub = mix(c['primary'], c['surface'], .12)
    t = {'bg': c['bg'], 'surface': c['surface'], 'surface-2': mix(c['surface'], c['text'], .96), 'text': c['text'], 'muted': c['muted'],
         'primary': c['primary'], 'on-primary': c['onp'], 'primary-subtle': sub, 'primary-border': mix(c['primary'], c['surface'], .35),
         'link': c['primary'], 'link-hover': mix(c['primary'], c['text'], .8), 'border': c['border'], 'border-strong': c['strong'],
         'banner-1': c['b1'], 'banner-2': c['b2'],
         'code': mix(c['primary'], c['text'], .7)}
    out = [f'  --z-{k}: {v};' for k, v in t.items()]
    for k in ('bg', 'surface', 'text', 'muted', 'primary', 'link', 'link-hover'): out.append(f'  --z-{k}-rgb: {trip(t[k])};')
    for n, (sol, on, tx, sb, bd) in S[m].items():
        out += [f'  --z-{n}: {sol};', f'  --z-{n}-rgb: {trip(sol)};', f'  --z-on-{n}: {on};', f'  --z-{n}-text: {tx};', f'  --z-{n}-subtle: {sb};', f'  --z-{n}-border: {bd};']
    return out, t

blocks = []
for name, (L, D) in P.items():
    for m, c in (('L', L), ('D', D)):
        body, t = tokens(m, c); tag = f'{name}/{"terang" if m == "L" else "gelap"}'
        check(f'{tag} teks/latar', c['text'], c['bg'], 4.5); check(f'{tag} redup/latar', c['muted'], c['bg'], 4.5)
        check(f'{tag} redup/permukaan', c['muted'], c['surface'], 4.5); check(f'{tag} tautan/latar', c['primary'], c['bg'], 4.5)
        check(f'{tag} tautan/permukaan', c['primary'], c['surface'], 4.5); check(f'{tag} on-primer/primer', c['onp'], c['primary'], 4.5)
        check(f'{tag} primer/subtle', c['primary'], t['primary-subtle'], 4.5); check(f'{tag} tepi input/permukaan', c['strong'], c['surface'], 3)
        for sf, sv in (('latar', c['bg']), ('permukaan', c['surface']), ('permukaan-2', t['surface-2'])): check(f'{tag} kode/{sf}', t['code'], sv, 4.5)
        check(f'{tag} putih/banner-1', '#ffffff', c['b1'], 4.5); check(f'{tag} putih/banner-2', '#ffffff', c['b2'], 4.5)
        mode = 'light' if m == 'L' else 'dark'
        sel = f':root[data-palette="{name}"][data-bs-theme="{mode}"]'
        if name == 'aurora': sel += ',\n' + (':root:not([data-palette]):not([data-bs-theme="dark"])' if m == 'L' else ':root[data-bs-theme="dark"]:not([data-palette])')
        blocks.append(f'/* {name} / {mode} */\n{sel} {{\n  color-scheme: {mode};\n' + '\n'.join(body) + '\n}')
for m in 'LD':
    for n, (sol, on, tx, sb, bd) in S[m].items():
        check(f'status {n}/{m} on/solid', on, sol, 4.5); check(f'status {n}/{m} teks/subtle', tx, sb, 4.5)

def bridge():  # (variabel Bootstrap, nilai, padanan Sass)
    r = [('body-bg','var(--z-bg)','$body-bg'),('body-bg-rgb','var(--z-bg-rgb)','(turunan $body-bg)'),('body-color','var(--z-text)','$body-color'),
         ('body-color-rgb','var(--z-text-rgb)','(turunan $body-color)'),('emphasis-color','var(--z-text)','$body-emphasis-color'),
         ('emphasis-color-rgb','var(--z-text-rgb)','(turunan)'),('secondary-color','var(--z-muted)','$body-secondary-color'),
         ('secondary-color-rgb','var(--z-muted-rgb)','(turunan)'),('secondary-bg','var(--z-surface-2)','$body-secondary-bg'),
         ('tertiary-bg','var(--z-bg)','$body-tertiary-bg'),('border-color','var(--z-border)','$border-color'),
         ('code-color','var(--z-code)','$code-color'),
         ('link-color','var(--z-link)','$link-color'),('link-color-rgb','var(--z-link-rgb)','(turunan)'),
         ('link-hover-color','var(--z-link-hover)','$link-hover-color'),('link-hover-color-rgb','var(--z-link-hover-rgb)','(turunan)'),
         ('focus-ring-color','rgba(var(--z-primary-rgb), .35)','$focus-ring-color'),
         ('border-radius','.5rem','$border-radius'),('border-radius-sm','.375rem','$border-radius-sm'),('border-radius-lg','.75rem','$border-radius-lg'),
         ('body-font-size','.9375rem','$font-size-base'),('body-line-height','1.55','$line-height-base'),
         ('form-valid-color','var(--z-success-text)','$form-feedback-valid-color'),('form-valid-border-color','var(--z-success)','$form-feedback-valid-color'),
         ('form-invalid-color','var(--z-danger-text)','$form-feedback-invalid-color'),('form-invalid-border-color','var(--z-danger)','$form-feedback-invalid-color')]
    for k, sass in (('primary','$primary'),('success','$success'),('warning','$warning'),('danger','$danger'),('info','$info')):
        z = k if k != 'primary' else 'primary'
        r += [(k, f'var(--z-{z})', sass), (f'{k}-rgb', f'var(--z-{z}-rgb)', '(turunan)'),
              (f'{k}-text-emphasis', f'var(--z-{z}-text)' if k != 'primary' else 'var(--z-link)', f'${k}-text-emphasis (gelap: -dark)'),
              (f'{k}-bg-subtle', f'var(--z-{z}-subtle)', f'${k}-bg-subtle (gelap: -dark)'),
              (f'{k}-border-subtle', f'var(--z-{z}-border)' if k != 'primary' else 'var(--z-primary-border)', f'${k}-border-subtle (gelap: -dark)')]
    return r
bridge_css = ':root {\n' + '\n'.join(f'  --bs-{k}: {v}; /* {s} */' for k, v, s in bridge()) + '\n}'

TICK = "url(\"data:image/svg+xml,%3csvg xmlns='http://www.w3.org/2000/svg' viewBox='@VB@'%3e@BODY@%3c/svg%3e\")"
tick_cb = TICK.replace('@VB@', '0 0 20 20').replace('@BODY@', "%3cpath fill='none' stroke='%230b0f14' stroke-linecap='round' stroke-linejoin='round' stroke-width='3' d='m6 10 3 3 6-6'/%3e")
tick_rd = TICK.replace('@VB@', '-4 -4 8 8').replace('@BODY@', "%3ccircle r='2' fill='%230b0f14'/%3e")
def mixp(p): return f'color-mix(in srgb, var(--z-primary) {p}%, var(--z-text))'
COMP = f"""
/* Komponen Bootstrap yang warnanya konstanta hasil Sass (tidak mengikuti --bs-primary): ditimpa per komponen. */
.btn-primary {{ --bs-btn-color: var(--z-on-primary); --bs-btn-bg: var(--z-primary); --bs-btn-border-color: var(--z-primary);
  --bs-btn-hover-color: var(--z-on-primary); --bs-btn-hover-bg: {mixp(88)}; --bs-btn-hover-border-color: {mixp(88)};
  --bs-btn-active-color: var(--z-on-primary); --bs-btn-active-bg: {mixp(80)}; --bs-btn-active-border-color: {mixp(80)};
  --bs-btn-disabled-color: var(--z-on-primary); --bs-btn-disabled-bg: var(--z-primary); --bs-btn-disabled-border-color: var(--z-primary);
  --bs-btn-focus-shadow-rgb: var(--z-primary-rgb); }}
.btn-danger {{ --bs-btn-color: var(--z-on-danger); --bs-btn-bg: var(--z-danger); --bs-btn-border-color: var(--z-danger);
  --bs-btn-hover-color: var(--z-on-danger); --bs-btn-hover-bg: color-mix(in srgb, var(--z-danger) 88%, var(--z-text)); --bs-btn-hover-border-color: color-mix(in srgb, var(--z-danger) 88%, var(--z-text));
  --bs-btn-active-color: var(--z-on-danger); --bs-btn-active-bg: color-mix(in srgb, var(--z-danger) 80%, var(--z-text)); --bs-btn-active-border-color: color-mix(in srgb, var(--z-danger) 80%, var(--z-text));
  --bs-btn-disabled-color: var(--z-on-danger); --bs-btn-disabled-bg: var(--z-danger); --bs-btn-disabled-border-color: var(--z-danger);
  --bs-btn-focus-shadow-rgb: var(--z-danger-rgb); }}
.btn-outline-primary {{ --bs-btn-color: var(--z-link); --bs-btn-border-color: var(--z-primary); --bs-btn-hover-color: var(--z-on-primary); --bs-btn-hover-bg: var(--z-primary);
  --bs-btn-hover-border-color: var(--z-primary); --bs-btn-active-color: var(--z-on-primary); --bs-btn-active-bg: var(--z-primary); --bs-btn-active-border-color: var(--z-primary);
  --bs-btn-disabled-color: var(--z-link); --bs-btn-disabled-border-color: var(--z-primary); --bs-btn-focus-shadow-rgb: var(--z-primary-rgb); }}
.btn-outline-danger {{ --bs-btn-color: var(--z-danger-text); --bs-btn-border-color: var(--z-danger); --bs-btn-hover-color: var(--z-on-danger); --bs-btn-hover-bg: var(--z-danger);
  --bs-btn-hover-border-color: var(--z-danger); --bs-btn-active-color: var(--z-on-danger); --bs-btn-active-bg: var(--z-danger); --bs-btn-active-border-color: var(--z-danger);
  --bs-btn-disabled-color: var(--z-danger-text); --bs-btn-disabled-border-color: var(--z-danger); --bs-btn-focus-shadow-rgb: var(--z-danger-rgb); }}
.btn-outline-secondary {{ --bs-btn-color: var(--z-text); --bs-btn-border-color: var(--z-border-strong); --bs-btn-hover-color: var(--z-text); --bs-btn-hover-bg: var(--z-surface-2);
  --bs-btn-hover-border-color: var(--z-border-strong); --bs-btn-active-color: var(--z-text); --bs-btn-active-bg: var(--z-surface-2); --bs-btn-active-border-color: var(--z-border-strong);
  --bs-btn-disabled-color: var(--z-muted); --bs-btn-disabled-border-color: var(--z-border); --bs-btn-focus-shadow-rgb: var(--z-text-rgb); }}
.btn-link {{ --bs-btn-color: var(--z-link); --bs-btn-hover-color: var(--z-link-hover); --bs-btn-active-color: var(--z-link-hover); --bs-btn-disabled-color: var(--z-muted); }}
.form-control, .form-select {{ background-color: var(--z-surface); border-color: var(--z-border-strong); }}
.form-control:focus, .form-select:focus {{ border-color: var(--z-primary); box-shadow: 0 0 0 .25rem rgba(var(--z-primary-rgb), .25); }}
.form-control:disabled, .form-select:disabled {{ background-color: var(--z-surface-2); }}
.form-check-input {{ background-color: var(--z-surface); border-color: var(--z-border-strong); }}
.form-check-input:focus {{ border-color: var(--z-primary); box-shadow: 0 0 0 .25rem rgba(var(--z-primary-rgb), .25); }}
.form-check-input:checked, .form-check-input[type=checkbox]:indeterminate {{ background-color: var(--z-primary); border-color: var(--z-primary); }}
:root[data-bs-theme="dark"] .form-check-input:checked[type=checkbox] {{ --bs-form-check-bg-image: {tick_cb}; }}
:root[data-bs-theme="dark"] .form-check-input:checked[type=radio] {{ --bs-form-check-bg-image: {tick_rd}; }}
.card {{ --bs-card-bg: var(--z-surface); --bs-card-border-color: var(--z-border); --bs-card-cap-bg: transparent; box-shadow: var(--z-shadow-sm); }}
.dropdown-menu {{ --bs-dropdown-bg: var(--z-surface); --bs-dropdown-border-color: var(--z-border); --bs-dropdown-link-hover-bg: var(--z-surface-2);
  --bs-dropdown-link-active-bg: var(--z-primary); --bs-dropdown-link-active-color: var(--z-on-primary); --bs-dropdown-box-shadow: var(--z-shadow-md); }}
.modal {{ --bs-modal-bg: var(--z-surface); --bs-modal-border-color: var(--z-border); }}
.toast {{ --bs-toast-bg: var(--z-surface); --bs-toast-border-color: var(--z-border); }}
.offcanvas {{ --bs-offcanvas-bg: var(--z-surface); --bs-offcanvas-border-color: var(--z-border); }}
.popover {{ --bs-popover-bg: var(--z-surface); --bs-popover-border-color: var(--z-border); }}
.table {{ --bs-table-bg: transparent; --bs-table-border-color: var(--z-border); }}
.pagination {{ --bs-pagination-bg: var(--z-surface); --bs-pagination-border-color: var(--z-border); --bs-pagination-color: var(--z-link);
  --bs-pagination-hover-bg: var(--z-surface-2); --bs-pagination-hover-color: var(--z-link-hover); --bs-pagination-hover-border-color: var(--z-border);
  --bs-pagination-focus-bg: var(--z-surface-2); --bs-pagination-focus-color: var(--z-link-hover); --bs-pagination-active-bg: var(--z-primary);
  --bs-pagination-active-border-color: var(--z-primary); --bs-pagination-active-color: var(--z-on-primary);
  --bs-pagination-disabled-bg: var(--z-surface-2); --bs-pagination-disabled-color: var(--z-muted); --bs-pagination-disabled-border-color: var(--z-border); }}
.nav-pills {{ --bs-nav-pills-link-active-bg: var(--z-primary); --bs-nav-pills-link-active-color: var(--z-on-primary); }}
.progress {{ --bs-progress-bar-bg: var(--z-primary); }}
.list-group {{ --bs-list-group-bg: var(--z-surface); --bs-list-group-border-color: var(--z-border); --bs-list-group-active-bg: var(--z-primary); --bs-list-group-active-border-color: var(--z-primary); --bs-list-group-active-color: var(--z-on-primary); }}
a:focus-visible, summary:focus-visible, [tabindex]:focus-visible {{ outline: 2px solid var(--z-primary); outline-offset: 2px; }}
@media (prefers-reduced-motion: reduce) {{ *, *::before, *::after {{ animation-duration: .01ms !important; animation-iteration-count: 1 !important; transition-duration: .01ms !important; scroll-behavior: auto !important; }} }}
"""
DOTS = '\n'.join(f'.z-dot[data-p="{n}"] {{ background: linear-gradient(140deg, {L["b1"]}, {L["b2"]}); }}' for n, (L, D) in P.items()) + '\n'
HEAD = """/* theme.css: DIHASILKAN oleh design/tools/gen-theme.py (ubah nilai di sana, lalu jalankan ulang).
 * Dimuat SESUDAH CSS Bootstrap 5.3.9. Mode warna: data-bs-theme=light|dark; palet: data-palette=aurora|lagoon|ember.
 * Token --z-* adalah sumber; --bs-* hanya jembatan (komentar = padanan variabel Sass untuk SPA). */
:root {
  --z-radius-sm: .375rem; --z-radius: .5rem; --z-radius-lg: .75rem;
  --z-shadow-sm: 0 1px 2px rgb(16 24 40 / .06); --z-shadow-md: 0 8px 24px rgb(16 24 40 / .14);
  --z-on-banner: #ffffff;
  --z-dur: 150ms; --z-ease: cubic-bezier(.2, 0, 0, 1);
}
"""
pathlib.Path(__file__).resolve().parent.parent.joinpath('theme.css').write_text(HEAD + '\n' + '\n\n'.join(blocks) + '\n\n' + bridge_css + '\n' + COMP + DOTS, encoding='utf-8')
w = max(len(r[0]) for r in rows)
for l, v, n in rows: print(f'{l:<{w}}  {v:6.2f}  (min {n})  {"OK" if v >= n else "GAGAL"}')
print(f'{len(rows)} pasangan, {len(fails)} gagal'); sys.exit(1 if fails else 0)
