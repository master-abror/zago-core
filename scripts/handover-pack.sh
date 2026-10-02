#!/usr/bin/env bash
# Usage: scripts/handover-pack.sh <NN>   (NN = milestone yang BARU SELESAI, mis. 03)
set -euo pipefail
M="${1:?usage: handover-pack.sh <NN>}"
NEXT=$(printf "%02d" $((10#$M + 1)))
PREV=$(printf "%02d" $((10#$M - 1)))
OUT="handover-M${M}"
rm -rf "$OUT" && mkdir -p "$OUT/docs/handover"

cp CLAUDE.md "$OUT/"
cp docs/STATUS.md docs/21-milestone-plan.md docs/22-handover-protocol.md docs/23-prompt-library.md "$OUT/docs/"
cp docs/00-errata-and-amendments.md "$OUT/docs/" 2>/dev/null || true
cp docs/handover/HANDOVER-M${M}*.md "$OUT/docs/handover/"
cp -r docs/adr "$OUT/docs/" 2>/dev/null || true

# Dokumen 01–20 yang dibutuhkan milestone berikutnya: daftar nama file di scripts/handover-docs-M${NEXT}.txt
if [ -f "scripts/handover-docs-M${NEXT}.txt" ]; then
  while read -r f; do [ -n "$f" ] && [ -f "docs/$f" ] && cp "docs/$f" "$OUT/docs/"; done < "scripts/handover-docs-M${NEXT}.txt"
fi

git ls-files > "$OUT/tree.txt"
git log --oneline -30 > "$OUT/git-log.txt"
git diff --stat "m${PREV}-done"..HEAD > "$OUT/diff-stat.txt" 2>/dev/null || true

zip -qr "${OUT}.zip" "$OUT" && rm -rf "$OUT"
echo "created ${OUT}.zip"
echo "Lampirkan zip ini + (bila perlu) source: git archive --format=zip HEAD -o source-snapshot.zip"
