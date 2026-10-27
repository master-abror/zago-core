#!/usr/bin/env bash
# scripts/smoke/auth-lib.sh — pembantu smoke yang butuh sesi (M02, M03). DI-SOURCE, bukan dijalankan
# (tidak cocok dengan pola m*.sh, jadi `make smoke` tidak menjalankannya sendiri).
#
# Prasyarat pemanggil: `set -euo pipefail`, variabel BASE, dan fungsi step, fail, http (mengisi
# STATUS/HDR/BODY dari curl), serta want.
#
# smoke_admin_session membuat (idempoten) user Super Admin smoke lewat `make db-seed` dan
# `make bootstrap-admin` (password dari stdin), lalu login. Hasil: JAR (cookie jar curl), CSRF
# (token untuk header X-CSRF-Token), dan ADMIN_ID.
#   SMOKE_ADMIN_EMAIL     bawaan smoke-admin@example.org
#   SMOKE_ADMIN_PASSWORD  bawaan: ACAK per eksekusi `make smoke`, disimpan 0600 di SMOKE_STATE_DIR
#                         supaya dipakai ulang antar skrip lalu dihapus. Tidak ada password tetap di
#                         repo: nilai yang diketahui publik pada Super Admin adalah lubang. Bila
#                         diisi sendiri, jangan memuat tanda kutip atau backslash (dipakai di JSON).
#   SMOKE_STATE_DIR       bawaan: direktori sementara baru per proses; `make smoke` menyediakan satu
#                         untuk semua skrip.
# PERINGATAN: skrip ini MEMBUAT Super Admin. Hanya untuk stack sementara/lokal (verify, dev);
# menolak BASE selain localhost. Bila user itu sudah ada dengan password lain, login gagal dengan
# pesan jelas (isi SMOKE_ADMIN_PASSWORD atau pakai stack bersih).

SMOKE_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
SMOKE_ADMIN_EMAIL=${SMOKE_ADMIN_EMAIL:-smoke-admin@example.org}
JAR="" CSRF="" ADMIN_ID=""

smoke_require_local() {
  case "$BASE" in
    http://localhost | http://localhost:* | http://127.0.0.1 | http://127.0.0.1:*) ;;
    *) fail "smoke membuat user Super Admin ($SMOKE_ADMIN_EMAIL): hanya boleh terhadap localhost, bukan $BASE" ;;
  esac
}

smoke_state_dir() {
  if [ -z "${SMOKE_STATE_DIR:-}" ]; then
    SMOKE_STATE_DIR=$(mktemp -d)
    if [ -z "$(trap -p EXIT)" ]; then
      trap 'rm -rf "$SMOKE_STATE_DIR"' EXIT
    fi
  fi
}

smoke_password() {
  if [ -n "${SMOKE_ADMIN_PASSWORD:-}" ]; then
    printf '%s' "$SMOKE_ADMIN_PASSWORD"
    return
  fi
  local f="$SMOKE_STATE_DIR/admin-password"
  if [ ! -s "$f" ]; then
    (umask 077 && head -c 24 /dev/urandom | base64 | tr -d '/+=\n' >"$f")
  fi
  cat "$f"
}

smoke_admin_session() {
  smoke_require_local
  smoke_state_dir
  local pw out
  pw=$(smoke_password)
  JAR="$SMOKE_STATE_DIR/jar.$$"
  (umask 077 && : >"$JAR")

  step "Super Admin smoke ($SMOKE_ADMIN_EMAIL): make db-seed + make bootstrap-admin (idempoten)"
  out=$(cd "$SMOKE_ROOT" && make -s db-seed 2>&1) || fail "make db-seed gagal: $out"
  out=$(cd "$SMOKE_ROOT" && printf '%s\n' "$pw" |
    make -s bootstrap-admin EMAIL="$SMOKE_ADMIN_EMAIL" NAME="Smoke Admin" PASSWORD_STDIN=1 2>&1) ||
    fail "make bootstrap-admin gagal: $out"

  step "login Super Admin smoke → cookie sesi + csrf_token"
  http -c "$JAR" -X POST -H 'Content-Type: application/json' \
    --data "{\"email\":\"$SMOKE_ADMIN_EMAIL\",\"password\":\"$pw\"}" "$BASE/api/v1/auth/login"
  [ "$STATUS" = "200" ] ||
    fail "login smoke admin: diharapkan HTTP 200, dapat $STATUS: $BODY (user sudah ada dengan password lain? isi SMOKE_ADMIN_PASSWORD atau pakai stack bersih)"
  CSRF=$(sed -n 's/.*"csrf_token":"\([^"]*\)".*/\1/p' <<<"$BODY")
  [ -n "$CSRF" ] || fail "login tidak mengembalikan csrf_token: $BODY"
  ADMIN_ID=$(sed -n 's/.*"user":{"id":"\([^"]*\)".*/\1/p' <<<"$BODY")
  [ -n "$ADMIN_ID" ] || fail "login tidak mengembalikan user.id: $BODY"
}
