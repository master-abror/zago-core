#!/usr/bin/env bash
# scripts/smoke/m00.sh — fondasi: server hidup, readiness mencerminkan dependensi, header
# keamanan, request id, proxy dev Vite. Hanya-baca dan idempoten. Gagal keras bila ada langkah gagal.
#   BASE     alamat API  (default http://localhost:8080)
#   WEB      alamat Vite (default http://localhost:5173)
#   SKIP_WEB=1 melewati langkah frontend/proxy
set -euo pipefail

BASE=${BASE:-http://localhost:8080}
WEB=${WEB:-http://localhost:5173}
SKIP_WEB=${SKIP_WEB:-0}

step() { echo "▶ $*"; }
fail() { echo "✗ $*" >&2; exit 1; }
code() { curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$@"; }

expect_code() { # expect_code <kode> <curl args...>
  local want=$1 got
  shift
  got=$(code "$@") || fail "curl gagal: $*"
  [ "$got" = "$want" ] || fail "$*: diharapkan HTTP $want, dapat $got"
}

step "liveness: /health dan /health/live → 200"
expect_code 200 "$BASE/health"
expect_code 200 "$BASE/health/live"

step "readiness: /health/ready → 200 dengan postgres & redis ok"
ready=$(curl -s --max-time 5 "$BASE/health/ready") || fail "curl /health/ready gagal"
expect_code 200 "$BASE/health/ready"
grep -q '"postgres":"ok"' <<<"$ready" || fail "postgres tidak ok: $ready"
grep -q '"redis":"ok"' <<<"$ready" || fail "redis tidak ok: $ready"

step "readiness tidak membocorkan detail dependensi"
if grep -qiE 'password|postgres://|redis://|dial tcp|sqlstate' <<<"$ready"; then
  fail "badan /health/ready membocorkan detail: $ready"
fi

step "header keamanan dan X-Request-ID pada respons"
headers=$(curl -s -D - -o /dev/null --max-time 5 "$BASE/health") || fail "curl header gagal"
grep -qi '^x-content-type-options: *nosniff' <<<"$headers" || fail "X-Content-Type-Options hilang"
grep -qi '^x-frame-options: *DENY' <<<"$headers" || fail "X-Frame-Options hilang"
grep -qi '^content-security-policy:' <<<"$headers" || fail "Content-Security-Policy hilang"
grep -qi '^x-request-id: *[A-Za-z0-9._-]\{8,\}' <<<"$headers" || fail "X-Request-ID hilang"

step "X-Request-ID valid dari klien dipantulkan; yang tak valid diganti"
echoed=$(curl -s -D - -o /dev/null --max-time 5 -H 'X-Request-ID: smoke-m00-trace-0001' "$BASE/health")
grep -qi '^x-request-id: *smoke-m00-trace-0001' <<<"$echoed" || fail "request id klien tidak dipantulkan"
replaced=$(curl -s -D - -o /dev/null --max-time 5 -H 'X-Request-ID: bad id with spaces' "$BASE/health")
! grep -qi 'bad id with spaces' <<<"$replaced" || fail "request id tak valid ikut dipantulkan"

step "rute tak dikenal → 404 JSON; metode salah → 405"
notfound=$(curl -s --max-time 5 "$BASE/api/v1/__smoke_probe")
grep -q '"not_found"' <<<"$notfound" || fail "404 bukan JSON not_found: $notfound"
expect_code 404 "$BASE/api/v1/__smoke_probe"
expect_code 405 -X POST "$BASE/health"

if [ "$SKIP_WEB" = "1" ]; then
  echo "• langkah frontend dilewati (SKIP_WEB=1)"
else
  step "frontend: $WEB/ menyajikan SPA"
  expect_code 200 "$WEB/"
  grep -q 'id="app"' < <(curl -s --max-time 5 "$WEB/") || fail "index.html tidak berisi #app"

  step "proxy dev Vite: $WEB/api/... diteruskan ke API (404 JSON dari API, bukan fallback SPA)"
  proxied=$(curl -s --max-time 5 "$WEB/api/v1/__smoke_probe")
  grep -q '"not_found"' <<<"$proxied" || fail "proxy /api tidak sampai ke API: ${proxied:0:120}"
  expect_code 404 "$WEB/api/v1/__smoke_probe"
fi

echo "M00 smoke OK"
