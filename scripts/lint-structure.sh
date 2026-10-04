#!/usr/bin/env bash
# Pemeriksaan struktural yang tak bisa dilakukan golangci-lint (docs/16 §2.3). KERANGKA M00:
# hanya aturan yang sudah bermakna sekarang. Aturan tabel-modul (07 §6), attach_updated_at
# (04 §4), Require/Public pada route modul (07 §5.1), dan "file hasil generate sudah di-commit"
# ditambahkan bersama komponennya (M01, M02, M07).
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0
violation() {
  echo "✗ $1" >&2
  [ -z "${2:-}" ] || echo "$2" | sed 's/^/    /' >&2
  fail=1
}

GREP_EXCLUDES=(--exclude-dir=node_modules --exclude-dir=.git --exclude-dir=dist --exclude-dir=bin --exclude-dir=tmp)

# 1. Modul HANYA mengimpor packages/module-sdk, tak pernah backend/internal/* (CLAUDE.md, docs/10 §4).
if [ -d modules ]; then
  hits=$(grep -rnE '"github.com/master-abror/zago-core/backend/(internal|cmd)' modules --include='*.go' "${GREP_EXCLUDES[@]}" || true)
  [ -z "$hits" ] || violation "modules/ mengimpor backend/internal atau backend/cmd" "$hits"
fi

# 2. UUID hanya dibangkitkan lewat backend/pkg/id (docs/10 §7).
hits=$(grep -rnE 'uuid\.(New|NewV7|NewString|NewRandom|Must)\b' backend packages modules --include='*.go' "${GREP_EXCLUDES[@]}" 2>/dev/null |
  grep -v '^backend/pkg/id/' || true)
[ -z "$hits" ] || violation "UUID dibangkitkan di luar backend/pkg/id (pakai id.NewID)" "$hits"

# 3. Frontend tak pernah memanggil fetch() langsung; satu-satunya pintu adalah core/api (CLAUDE.md).
hits=$(grep -rnE '(^|[^A-Za-z0-9_.])fetch[[:space:]]*\(' apps modules packages \
  --include='*.ts' --include='*.js' --include='*.svelte' "${GREP_EXCLUDES[@]}" 2>/dev/null |
  grep -v '^apps/web/src/core/api/' || true)
[ -z "$hits" ] || violation "fetch() dipanggil di luar apps/web/src/core/api" "$hits"

# 4. Rahasia tak pernah di-commit: .env tidak boleh terlacak git; tak ada private key / access key AWS.
if command -v git >/dev/null 2>&1 && git rev-parse --git-dir >/dev/null 2>&1; then
  if git ls-files --error-unmatch .env >/dev/null 2>&1; then
    violation ".env terlacak oleh git (hapus dengan: git rm --cached .env)"
  fi
fi
hits=$(grep -rnE 'BEGIN [A-Z ]*PRIVATE KEY|AKIA[0-9A-Z]{16}' . "${GREP_EXCLUDES[@]}" --exclude='lint-structure.sh' 2>/dev/null || true)
[ -z "$hits" ] || violation "kandidat rahasia ditemukan di repo" "$hits"

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "lint-structure OK"
