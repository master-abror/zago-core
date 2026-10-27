#!/usr/bin/env bash
# scripts/smoke/m02.sh — kernel: envelope, rate limit, pagination keyset + cursor bertanda tangan,
# idempotency, validasi, dan request id lewat endpoint dev/test /api/v1/_kernel/echo (ADR-0016).
# POST echo butuh sesi: skrip membuat Super Admin smoke lewat bootstrap-admin, login, lalu memakai
# cookie + X-CSRF-Token (scripts/smoke/auth-lib.sh; M03 menghapus X-Dev-Actor).
# Idempoten (kunci idempotensi unik per eksekusi) dan gagal keras bila ada langkah gagal.
#   BASE   alamat API (default http://localhost:${API_HOST_PORT:-8080}; harus localhost karena skrip membuat Super Admin)
# Catatan: echo dibatasi 30 permintaan/menit per IP; skrip memakai 12-13. Bila sisa jatah di awal
# kurang dari kebutuhan (atau sudah 429), skrip menunggu jendela berganti (maks 65 dtk) lalu lanjut,
# jadi aman dijalankan berulang.
set -euo pipefail

BASE=${BASE:-http://localhost:${API_HOST_PORT:-8080}}
ECHO="$BASE/api/v1/_kernel/echo"
RUN="$(date +%s)-$$"

step() { echo "▶ $*"; }
fail() { echo "✗ $*" >&2; exit 1; }

STATUS="" HDR="" BODY=""
http() { # http <argumen curl...>: mengisi STATUS, HDR (header, tanpa CR), BODY
  local dir
  dir=$(mktemp -d)
  STATUS=$(curl -s -D "$dir/h" -o "$dir/b" -w '%{http_code}' --max-time 8 "$@") || { rm -rf "$dir"; fail "curl gagal: $*"; }
  HDR=$(tr -d '\r' <"$dir/h")
  BODY=$(cat "$dir/b")
  rm -rf "$dir"
}
header() { grep -i "^$1:" <<<"$HDR" | head -n1 | sed 's/^[^:]*: *//' || true; }
want() { # want <status> <konteks>
  [ "$STATUS" = "$1" ] || fail "$2: diharapkan HTTP $1, dapat $STATUS: $BODY"
}
has() { # has <pola-ERE> <konteks>
  grep -Eq "$1" <<<"$BODY" || fail "$2: badan tidak memuat /$1/: $BODY"
}
post() { # post <idempotency-key|-> <body> [header tambahan...]
  local key=$1 body=$2
  shift 2
  local args=(-X POST -H 'Content-Type: application/json' -b "$JAR" -H "X-CSRF-Token: $CSRF" --data "$body")
  [ "$key" = "-" ] || args+=(-H "Idempotency-Key: $key")
  http "${args[@]}" "$@" "$ECHO"
}
first_id() { grep -o '"id":"[^"]*"' <<<"$BODY" | head -n1 | cut -d'"' -f4; }
next_cursor() { sed -n 's/.*"next_cursor":"\([^"]*\)".*/\1/p' <<<"$BODY"; }

NEEDED=17 # kebutuhan permintaan skrip + sisa aman

# shellcheck source=scripts/smoke/auth-lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/auth-lib.sh"

step "echo terpasang (development/test) dan membalas dengan envelope {data, meta}"
http "$ECHO/items?limit=2"
remaining=$(header X-RateLimit-Remaining)
if [ "$STATUS" = "429" ] || { [ "$STATUS" = "200" ] && [[ "$remaining" =~ ^[0-9]+$ ]] && [ "$remaining" -lt "$NEEDED" ]; }; then
  wait_s=$(header Retry-After)
  if [ -z "$wait_s" ]; then
    reset=$(header X-RateLimit-Reset)
    [[ "$reset" =~ ^[0-9]+$ ]] || fail "tak bisa menentukan kapan jendela rate limit berganti"
    wait_s=$((reset - $(date +%s) + 1))
  fi
  [ "$wait_s" -ge 1 ] || wait_s=1
  [ "$wait_s" -le 65 ] || fail "menunggu terlalu lama: ${wait_s}s"
  echo "• sisa jatah rate limit kurang (status $STATUS, sisa '${remaining:-?}'); menunggu ${wait_s}s"
  sleep "$wait_s"
  http "$ECHO/items?limit=2"
fi
[ "$STATUS" != "404" ] || fail "echo tidak terpasang (ENVIRONMENT harus development/test dan Redis tersedia)"
want 200 "daftar item"
has '^\{"data":\[' "envelope sukses"
# encoding/json mengurutkan kunci map secara alfabetis: has_more mendahului next_cursor.
has '"pagination":\{"has_more":true,"next_cursor":"[^"]+"\}' "meta pagination halaman pertama"

step "rate limit: header X-RateLimit-* ada dan sisa berkurang"
limit=$(header X-RateLimit-Limit)
[ "$limit" = "30" ] || fail "X-RateLimit-Limit diharapkan 30, dapat '$limit'"
rem1=$(header X-RateLimit-Remaining)
[[ "$rem1" =~ ^[0-9]+$ ]] || fail "X-RateLimit-Remaining bukan angka: '$rem1'"
[ -n "$(header X-RateLimit-Reset)" ] || fail "X-RateLimit-Reset hilang"
decreased=0
for _ in 1 2; do # ulang sekali bila jendela kebetulan berganti di antara dua panggilan
  http "$ECHO/items?limit=2"
  want 200 "daftar item (ulang)"
  rem2=$(header X-RateLimit-Remaining)
  if [ "$rem2" -lt "$rem1" ]; then decreased=1; break; fi
  rem1=$rem2
done
[ "$decreased" = 1 ] || fail "X-RateLimit-Remaining tidak berkurang antar permintaan"

step "pagination keyset: cursor membawa ke halaman berikutnya yang berbeda"
http "$ECHO/items?limit=2"
want 200 "halaman 1"
id1=$(first_id)
cursor=$(next_cursor)
[ -n "$id1" ] && [ -n "$cursor" ] || fail "halaman 1 tanpa id/next_cursor: $BODY"
http "$ECHO/items?limit=2&cursor=$cursor"
want 200 "halaman 2"
id2=$(first_id)
[ -n "$id2" ] && [ "$id1" != "$id2" ] || fail "halaman 2 sama dengan halaman 1 ($id1)"

step "cursor diubah → 400 validation_failed (invalid_cursor)"
last=${cursor: -1}
swap=A
[ "$last" = "A" ] && swap=B
http "$ECHO/items?limit=2&cursor=${cursor%?}$swap"
want 400 "cursor dirusak"
has '"validation_failed"' "cursor dirusak"
has '"invalid_cursor"' "cursor dirusak"

step "cursor sah dipakai dengan filter lain → 400 (cursor_mismatch)"
http "$ECHO/items?limit=2&q=zzz&cursor=$cursor"
want 400 "cursor beda filter"
has '"cursor_mismatch"' "cursor beda filter"

smoke_admin_session

step "Idempotency-Key wajib → 400 validation_failed"
post - '{"message":"smoke m02"}'
want 400 "tanpa Idempotency-Key"
has '"validation_failed"' "tanpa Idempotency-Key"
has 'Idempotency-Key' "tanpa Idempotency-Key"

step "tanpa sesi → 401 authentication_required"
http -X POST -H 'Content-Type: application/json' -H "Idempotency-Key: smoke-m02-anon-$RUN" \
  --data '{"message":"x"}' "$ECHO"
want 401 "POST tanpa sesi"
has '"authentication_required"' "POST tanpa sesi"

step "sesi tanpa X-CSRF-Token → 403 csrf_failed"
http -b "$JAR" -X POST -H 'Content-Type: application/json' -H "Idempotency-Key: smoke-m02-nocsrf-$RUN" \
  --data '{"message":"x"}' "$ECHO"
want 403 "POST tanpa CSRF"
has '"csrf_failed"' "POST tanpa CSRF"

step "POST echo → 201, request_id mengalir dari header ke badan"
KEY="smoke-m02-$RUN"
REQID="smoke-m02-req-$RUN"
post "$KEY" '{"message":"smoke m02"}' -H "X-Request-ID: $REQID"
want 201 "POST echo"
has '"data":\{' "envelope sukses POST"
has '"message":"smoke m02"' "POST echo"
has "\"request_id\":\"$REQID\"" "request_id di badan"
has "\"actor_id\":\"$ADMIN_ID\"" "actor_id = pengguna yang login"
[ "$(header X-Request-ID)" = "$REQID" ] || fail "X-Request-ID respons != $REQID"
FIRST_BODY=$BODY

step "kunci yang sama + badan sama → respons diputar ulang (Idempotent-Replayed: true)"
post "$KEY" '{"message":"smoke m02"}' -H "X-Request-ID: $REQID"
want 201 "replay"
[ "$(header Idempotent-Replayed)" = "true" ] || fail "header Idempotent-Replayed hilang pada replay"
[ "$BODY" = "$FIRST_BODY" ] || fail "badan replay berbeda: $BODY"

step "kunci yang sama + badan berbeda → 422 idempotency_key_conflict"
post "$KEY" '{"message":"badan lain"}' -H "X-Request-ID: $REQID"
want 422 "konflik kunci"
has '"idempotency_key_conflict"' "konflik kunci"

echo "M02 smoke OK"
