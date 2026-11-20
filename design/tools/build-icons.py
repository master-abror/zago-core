#!/usr/bin/env python3
"""Membuat design/assets/icons.svg (sprite) dari paket bootstrap-icons hasil `npm pack`.
Pakai: python3 design/tools/build-icons.py /tmp/bootstrap-icons-1.13.2.tgz"""
import pathlib, re, sys, tarfile

NAMES = 'house-door key shield-lock sun moon-stars box-arrow-right list chevron-down building layout-sidebar-inset check-lg plus-lg'.split()
out = pathlib.Path(__file__).resolve().parent.parent / 'assets'
out.mkdir(exist_ok=True)
tf = tarfile.open(sys.argv[1])
syms, missing = [], []
for n in NAMES:
    try:
        svg = tf.extractfile(f'package/icons/{n}.svg').read().decode()
    except KeyError:
        missing.append(n)
        continue
    vb = re.search(r'viewBox="([^"]+)"', svg).group(1)
    inner = svg[svg.index('>', svg.index('<svg')) + 1:svg.rindex('</svg>')].strip()
    syms.append(f'<symbol id="{n}" viewBox="{vb}">{inner}</symbol>')
(out / 'icons.svg').write_text('<svg xmlns="http://www.w3.org/2000/svg">\n' + '\n'.join(syms) + '\n</svg>\n', encoding='utf-8')
(out / 'LICENSE-bootstrap-icons').write_bytes(tf.extractfile('package/LICENSE').read())
print(f'{len(syms)} ikon -> assets/icons.svg; lisensi disalin ke assets/LICENSE-bootstrap-icons')
if missing:
    print('TIDAK ADA:', ', '.join(missing), file=sys.stderr)
    sys.exit(1)
