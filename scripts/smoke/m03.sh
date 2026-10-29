#!/usr/bin/env bash
# scripts/smoke/m03.sh — autentikasi end-to-end di stack asli (ADR-0020): login/me/csrf/logout,
# ganti password (rotasi sesi, sesi lama ditolak), riwayat event keamanan, dan penjaga login
# (kunci akun yang tak terlihat + pembuka darurat di Redis).
#
# Memakai scripts/smoke/auth-lib.sh (Super Admin smoke + sesi). Lingkup:
#   1. login → me → csrf → logout tanpa CSRF = 403 → logout → cookie yang sama = 401 (exit docs/21).
#   2. ganti password A→B: salah/terlalu pendek ditolak; sukses merotasi sesi (cookie lama 401,
#      cookie baru berlaku, csrf_token baru); login dengan A gagal, dengan B berhasil;
#      GET /security/events memuat auth.password.changed dan auth.password.change_failed;
#      lalu B→A SUPAYA password admin smoke kembali seperti semula (stack dev yang dipakai ulang
#      tidak rusak; bootstrap-admin tak pernah menimpa password).
#   3. akun kunci TERPISAH (smoke-lock@example.org; Super Admin smoke utama tidak pernah dikunci):
#      5 password salah → tiap kali 401 invalid_credentials; password BENAR saat kunci aktif tetap
#      401 invalid_credentials (bukan 423) dan tanpa Set-Cookie; kunci ada di Redis ber-TTL; setelah
#      pembuka darurat `DEL auth:lock:<user_id>` login berhasil; event auth.account.locked tercatat.
#
# Variabel:
#   BASE                  alamat API (bawaan http://localhost:${API_HOST_PORT:-8080}; harus localhost)
#   SMOKE_ADMIN_EMAIL / SMOKE_ADMIN_PASSWORD / SMOKE_STATE_DIR   lihat auth-lib.sh
#   SMOKE_LOCK_EMAIL      bawaan smoke-lock@example.org
#   SMOKE_LOCK_PASSWORD   bawaan: ACAK per eksekusi di SMOKE_STATE_DIR (0600). bootstrap-admin tak
#                         menimpa password, jadi di stack dev yang dipakai ulang isi variabel ini
#                         (atau pakai stack bersih).
#   SMOKE_REDIS_CLI       perintah redis-cli, bawaan `docker compose exec -T redis redis-cli` (dijalankan
#                         dari akar repo; COMPOSE_PROJECT_NAME diwarisi dari `make verify`)
#
# Batas terhadap penjaga login (ADR-0020 "Konsekuensi"): skrip memakai 8 login untuk akun kunci dan
# 5 untuk admin (LoginRule 10/menit/IP): sebelum tiap kelompok ia membaca X-RateLimit-Remaining dan
# menunggu pergantian jendela bila sisanya kurang. Penghitung IP penjaga TIDAK direset oleh sukses:
# tiap run menambah ±7 kegagalan, jadi >4 run dalam 15 menit pada stack yang SAMA menembus gerbang IP
# (30) dan login ditolak 429. `make verify` memakai stack baru per run, jadi tidak terpengaruh.
set -euo pipefail

BASE=${BASE:-http://localhost:${API_HOST_PORT:-8080}}
API="$BASE/api/v1"

step() { echo "▶ $*"; }

# Pemulihan password admin bila skrip gagal di antara dua penggantian password (best-effort).
PW_CHANGED=0 CUR_PW="" ORIG_PW=""
JAR="" CSRF=""
recover_password() {
  [ "$PW_CHANGED" = 1 ] || return 0
  PW_CHANGED=0
  local code
  code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 8 -b "$JAR" -X POST \
    -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" \
    --data "{\"current_password\":\"$CUR_PW\",\"new_password\":\"$ORIG_PW\"}" "$API/security/password" || true)
  if [ "$code" = "200" ]; then
    echo "• password admin smoke dipulihkan seperti semula" >&2
  else
    echo "✗ pemulihan password admin smoke gagal (HTTP ${code:-?}): akun itu mungkin memakai password sementara; pakai stack bersih" >&2
  fi
}
fail() {
  echo "✗ $*" >&2
  recover_password
  exit 1
}

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
hasnt() { # hasnt <pola-ERE> <konteks>
  if grep -Eq "$1" <<<"$BODY"; then fail "$2: badan memuat /$1/: $BODY"; fi
}
no_cookie() { # no_cookie <konteks>: respons tidak boleh mengeluarkan Set-Cookie
  if grep -qi '^set-cookie:' <<<"$HDR"; then fail "$1: respons mengeluarkan Set-Cookie"; fi
}
json_field() { sed -n "s/.*\"$1\":\"\\([^\"]*\\)\".*/\\1/p" <<<"$BODY" | head -n1; }
count_events() { { grep -o "\"event_type\":\"$1\"" <<<"$BODY" || true; } | wc -l | tr -d ' '; }

# ---------- jatah login (LoginRule) ----------
RL_REM="" RL_RESET="" RL_LIM=""
rl_note() {
  RL_REM=$(header X-RateLimit-Remaining)
  RL_RESET=$(header X-RateLimit-Reset)
  RL_LIM=$(header X-RateLimit-Limit)
}
rl_wait() { # rl_wait <n>: pastikan sisa jatah login >= n (tunggu pergantian jendela bila kurang)
  if ! [[ "$RL_REM" =~ ^[0-9]+$ && "$RL_RESET" =~ ^[0-9]+$ ]]; then
    return 0
  fi
  if [ "$RL_REM" -ge "$1" ]; then
    return 0
  fi
  local w=$((RL_RESET - $(date +%s) + 1))
  [ "$w" -ge 1 ] || w=1
  [ "$w" -le 65 ] || fail "menunggu jendela rate limit terlalu lama: ${w}s"
  echo "• sisa jatah login $RL_REM < $1; menunggu ${w}s"
  sleep "$w"
  RL_REM=${RL_LIM:-10}
}

# shellcheck source=scripts/smoke/auth-lib.sh
source "$(dirname "${BASH_SOURCE[0]}")/auth-lib.sh"

new_jar() { mktemp "$SMOKE_STATE_DIR/jar.XXXXXX"; }
random_password() { head -c 24 /dev/urandom | base64 | tr -d '/+=\n'; }

login_post() { # login_post <email> <password> [jar]: STATUS/HDR/BODY dari POST /auth/login
  local args=(-X POST -H 'Content-Type: application/json'
    --data "{\"email\":\"$1\",\"password\":\"$2\"}")
  [ -z "${3:-}" ] || args+=(-c "$3")
  http "${args[@]}" "$API/auth/login"
  rl_note
}

change_password() { # change_password <current> <new>: memakai JAR/CSRF; cookie baru ditulis ke JAR
  http -b "$JAR" -c "$JAR" -X POST -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" \
    --data "{\"current_password\":\"$1\",\"new_password\":\"$2\"}" "$API/security/password"
}

# ======================================================================================
# 1. login → me → csrf → logout → me = 401
# ======================================================================================
smoke_admin_session # step + login pertama (JAR, CSRF, ADMIN_ID); HDR = respons login
rl_note
ORIG_PW=$(smoke_password)
CUR_PW=$ORIG_PW

step "me: pengguna dan sesi saat ini"
http -b "$JAR" "$API/auth/me"
want 200 "GET /auth/me"
has "\"email\":\"$SMOKE_ADMIN_EMAIL\"" "me: email"
has "\"id\":\"$ADMIN_ID\"" "me: id pengguna"
has '"session":\{"id":"[^"]+","auth_level":"' "me: ringkasan sesi"

step "csrf: token terikat sesi (sama dengan yang dikembalikan login)"
http -b "$JAR" "$API/auth/csrf"
want 200 "GET /auth/csrf"
has "\"csrf_token\":\"$CSRF\"" "csrf sama dengan token login"

step "logout tanpa X-CSRF-Token → 403 csrf_failed (sesi tetap hidup)"
http -b "$JAR" -X POST "$API/auth/logout"
want 403 "logout tanpa CSRF"
has '"csrf_failed"' "logout tanpa CSRF"
http -b "$JAR" "$API/auth/me"
want 200 "me setelah logout ditolak CSRF"

step "logout → 204; cookie yang SAMA lalu ditolak 401 (sesi dicabut di server)"
http -b "$JAR" -X POST -H "X-CSRF-Token: $CSRF" "$API/auth/logout"
want 204 "logout"
http -b "$JAR" "$API/auth/me"
want 401 "me dengan cookie sesi yang sudah dicabut"
has '"authentication_required"' "me setelah logout"

# ======================================================================================
# 2. ganti password A→B (lalu B→A)
# ======================================================================================
rl_wait 5 # admin: login A, login A (salah), login B, login A (pulih) = 4
JAR=$(new_jar)
step "login ulang admin smoke (password A)"
login_post "$SMOKE_ADMIN_EMAIL" "$ORIG_PW" "$JAR"
want 200 "login ulang"
CSRF=$(json_field csrf_token)
[ -n "$CSRF" ] || fail "login ulang tanpa csrf_token: $BODY"

NEW_PW=$(random_password)

step "ganti password: password saat ini salah → 401 invalid_credentials, sesi tetap hidup"
change_password "salah-$ORIG_PW" "$NEW_PW"
want 401 "ganti password dengan password saat ini salah"
has '"invalid_credentials"' "password saat ini salah"
http -b "$JAR" "$API/auth/me"
want 200 "me setelah ganti password gagal"

step "ganti password: password baru terlalu pendek → 400 validation_failed (new_password)"
change_password "$ORIG_PW" "pendek"
want 400 "password baru terlalu pendek"
has '"validation_failed"' "password baru terlalu pendek"
has '"new_password"' "password baru terlalu pendek: field"

step "ganti password A→B → 200 + cookie dan csrf_token baru; sesi lama dicabut"
OLDJAR=$(new_jar)
cp "$JAR" "$OLDJAR"
OLDCSRF=$CSRF
change_password "$ORIG_PW" "$NEW_PW"
want 200 "ganti password"
# Token CSRF baru diambil SEBELUM pemeriksaan lain: bila ada yang gagal, pemulihan memakai sesi baru.
CSRF=$(json_field csrf_token)
CUR_PW=$NEW_PW
PW_CHANGED=1
[ -n "$CSRF" ] && [ "$CSRF" != "$OLDCSRF" ] || fail "csrf_token tidak berganti setelah rotasi sesi: $BODY"
has '"revoked_other_sessions":[0-9]+' "ganti password: revoked_other_sessions"
grep -qi '^set-cookie:' <<<"$HDR" || fail "ganti password tidak mengeluarkan cookie sesi baru"

step "cookie sesi LAMA → 401; cookie baru → 200 (rotasi sesi)"
http -b "$OLDJAR" "$API/auth/me"
want 401 "cookie lama setelah ganti password"
http -b "$JAR" "$API/auth/me"
want 200 "cookie baru setelah ganti password"

step "login dengan password A → 401 invalid_credentials; dengan password B → 200"
login_post "$SMOKE_ADMIN_EMAIL" "$ORIG_PW"
want 401 "login dengan password lama"
has '"invalid_credentials"' "login dengan password lama"
no_cookie "login dengan password lama"
J3=$(new_jar)
login_post "$SMOKE_ADMIN_EMAIL" "$NEW_PW" "$J3"
want 200 "login dengan password baru"

step "riwayat event keamanan: auth.password.changed dan auth.password.change_failed"
http -b "$JAR" "$API/security/events?limit=100"
want 200 "GET /security/events"
has '"event_type":"auth.password.changed"' "events: password.changed"
has '"event_type":"auth.password.change_failed"' "events: password.change_failed"
has '"event_type":"auth.login.failed"' "events: login.failed"
hasnt "$NEW_PW|$ORIG_PW" "events tidak boleh memuat password"

step "ganti password B→A (pulihkan password admin smoke) lalu login dengan A → 200"
change_password "$NEW_PW" "$ORIG_PW"
want 200 "ganti password kembali"
PW_CHANGED=0
CUR_PW=$ORIG_PW
CSRF=$(json_field csrf_token)
J4=$(new_jar)
login_post "$SMOKE_ADMIN_EMAIL" "$ORIG_PW" "$J4"
want 200 "login dengan password A setelah dipulihkan"

# ======================================================================================
# 3. penjaga login: akun kunci terpisah
# ======================================================================================
LOCK_EMAIL=${SMOKE_LOCK_EMAIL:-smoke-lock@example.org}
lock_password() {
  if [ -n "${SMOKE_LOCK_PASSWORD:-}" ]; then
    printf '%s' "$SMOKE_LOCK_PASSWORD"
    return
  fi
  local f="$SMOKE_STATE_DIR/lock-password"
  if [ ! -s "$f" ]; then
    (umask 077 && random_password >"$f")
  fi
  cat "$f"
}
redis_cli() { # redis_cli <argumen...>: keluaran tanpa CR; dijalankan dari akar repo
  local -a cmd
  if [ -n "${SMOKE_REDIS_CLI:-}" ]; then
    read -r -a cmd <<<"$SMOKE_REDIS_CLI"
  else
    cmd=(docker compose exec -T redis redis-cli)
  fi
  (cd "$SMOKE_ROOT" && "${cmd[@]}" "$@") | tr -d '\r'
}

LOCK_PW=$(lock_password)
step "akun kunci ($LOCK_EMAIL): make bootstrap-admin (idempoten; HANYA stack lokal)"
out=$(cd "$SMOKE_ROOT" && printf '%s\n' "$LOCK_PW" |
  make -s bootstrap-admin EMAIL="$LOCK_EMAIL" NAME="Smoke Lock" PASSWORD_STDIN=1 2>&1) ||
  fail "make bootstrap-admin ($LOCK_EMAIL) gagal: $out"

rl_wait 8 # login akun kunci: 1 (ambil id) + 5 salah + 1 benar saat terkunci + 1 setelah dibuka
step "login akun kunci (password benar) → 200; ambil user_id"
login_post "$LOCK_EMAIL" "$LOCK_PW" "$(new_jar)"
[ "$STATUS" = "200" ] ||
  fail "login akun kunci: diharapkan HTTP 200, dapat $STATUS: $BODY (user sudah ada dengan password lain? isi SMOKE_LOCK_PASSWORD atau pakai stack bersih)"
LOCK_ID=$(sed -n 's/.*"user":{"id":"\([^"]*\)".*/\1/p' <<<"$BODY")
[ -n "$LOCK_ID" ] || fail "login akun kunci tanpa user.id: $BODY"

step "5 password salah → tiap kali 401 invalid_credentials (bukan 423, tanpa cookie)"
for i in 1 2 3 4 5; do
  login_post "$LOCK_EMAIL" "salah-$i-$LOCK_PW"
  want 401 "password salah #$i"
  has '"invalid_credentials"' "password salah #$i"
  hasnt 'account_locked|locked' "password salah #$i tidak boleh membocorkan kunci"
  no_cookie "password salah #$i"
done

step "password BENAR saat kunci aktif → tetap 401 invalid_credentials, tanpa cookie (kunci tak terlihat)"
login_post "$LOCK_EMAIL" "$LOCK_PW"
want 401 "password benar saat terkunci"
has '"invalid_credentials"' "password benar saat terkunci"
hasnt 'account_locked|locked' "password benar saat terkunci tidak boleh membocorkan kunci"
no_cookie "password benar saat terkunci"

step "kunci ada di Redis, ber-TTL (auth:lock:<user_id>)"
exists=$(redis_cli EXISTS "auth:lock:$LOCK_ID") ||
  fail "redis-cli gagal (docker compose exec redis tersedia? atau isi SMOKE_REDIS_CLI)"
[ "$exists" = "1" ] || fail "auth:lock:$LOCK_ID tidak ada di Redis setelah 5 kegagalan (EXISTS=$exists)"
ttl=$(redis_cli TTL "auth:lock:$LOCK_ID") || fail "redis-cli TTL gagal"
[[ "$ttl" =~ ^[0-9]+$ ]] && [ "$ttl" -ge 1 ] && [ "$ttl" -le 86460 ] ||
  fail "auth:lock:$LOCK_ID harus ber-TTL (1..86460 dtk), dapat '$ttl'"

step "pembuka darurat: DEL auth:lock:<user_id> → login dengan password benar berhasil"
deleted=$(redis_cli DEL "auth:lock:$LOCK_ID") || fail "redis-cli DEL gagal"
[ "$deleted" = "1" ] || fail "DEL auth:lock:$LOCK_ID mengembalikan '$deleted' (diharapkan 1)"
LJAR=$(new_jar)
login_post "$LOCK_EMAIL" "$LOCK_PW" "$LJAR"
want 200 "login setelah kunci dibuka"
LOCK_CSRF=$(json_field csrf_token)
[ -n "$LOCK_CSRF" ] || fail "login setelah kunci dibuka tanpa csrf_token: $BODY"

step "riwayat event akun kunci: tepat satu auth.account.locked dan ≥ 5 auth.login.failed"
http -b "$LJAR" "$API/security/events?limit=100"
want 200 "GET /security/events (akun kunci)"
n=$(count_events auth.account.locked)
[ "$n" = "1" ] || fail "auth.account.locked tercatat $n kali (diharapkan 1): $BODY"
n=$(count_events auth.login.failed)
[ "$n" -ge 5 ] || fail "auth.login.failed tercatat $n kali (diharapkan ≥ 5): $BODY"
hasnt "$LOCK_PW" "events tidak boleh memuat password"

echo "M03 smoke OK"
