#!/usr/bin/env bash
# Membuat zip repo lengkap untuk di-upload ke chat berikutnya.
# Usage: scripts/repo-zip.sh [nama-zip]    (default: platform-repo-<tanggal>.zip)
set -euo pipefail
OUT="${1:-platform-repo-$(date +%Y%m%d-%H%M).zip}"
cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
rm -f "$OUT"
zip -qr "$OUT" . \
  -x "node_modules/*" "*/node_modules/*" "bin/*" "dist/*" "*/dist/*" "tmp/*" "data/*" \
     "coverage/*" ".env" ".git/*" "*.zip"
echo "created $OUT ($(du -h "$OUT" | cut -f1))"
echo "Upload zip ini ke chat baru dan tempel prompt P0 (docs/23-prompt-library.md)."
