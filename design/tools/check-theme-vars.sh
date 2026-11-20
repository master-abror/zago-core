#!/usr/bin/env bash
# Memastikan setiap variabel --bs-* yang ditimpa theme.css memang ada di CSS Bootstrap terpasang.
set -euo pipefail
cd "$(dirname "$0")/.."
css=vendor/bootstrap-5.3.9/css/bootstrap.min.css
[ -f "$css" ] || { echo "✗ $css tidak ada (lihat design/README.md, langkah Bootstrap)" >&2; exit 1; }
missing=0; n=0
for v in $(grep -o -- '--bs-[a-z0-9-]*:' theme.css | tr -d ':' | sort -u); do
  n=$((n + 1))
  grep -q -- "$v:" "$css" || { echo "✗ $v tidak didefinisikan di Bootstrap" >&2; missing=1; }
done
[ "$missing" -eq 0 ] && echo "check-theme-vars OK ($n variabel --bs-*)"
exit "$missing"
