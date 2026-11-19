#!/usr/bin/env bash
# scripts/smoke/m04.sh — otorisasi end-to-end di stack asli (ADR-0022, bagian 20–22): mesin,
# sesi, PostgreSQL, dan Redis nyata. Exit docs/21: user tanpa permission → 403; diberi role → 200;
# dicabut → 403 langsung; simulator menjelaskan alasan.
#
# Memakai scripts/smoke/auth-lib.sh (Super Admin smoke + sesi). Tidak ada API untuk membuat
# organisasi/user/keanggotaan sampai M05, jadi fixture ditulis lewat `psql` sebagai superuser
# database sementara (seperti m03 memakai redis-cli). Setiap run memakai organisasi dan user BARU
# (akhiran acak RUN), jadi run ulang di stack yang sama tidak bertabrakan.
#
# Lingkup:
#   0. fixture: dua organisasi (A, B); user bootstrap "platform" (Super Admin platform kedua) yang
#      password-nya DISALIN ke user tenant (satu kali make bootstrap-admin, tanpa hash di skrip);
#      adminA, adminB (Organization Admin), Z (Organization Admin di A DAN B), X (anggota A dan B),
#      M (Member di A, tanpa hak role/policy/simulate).
#   1. tanpa sesi → 401 authentication_required pada empat rute baca.
#   2. user Member (M) → 403 permission_denied pada rute baca, simulate, dan POST /role-assignments.
#   3. Super Admin platform: GET boleh; POST /role-assignments dan simulate untuk organisasi → 403
#      scope_mismatch (platform tidak memberi/menyimulasikan di dalam organisasi).
#   4. idempotensi (ADR-0022 butir 73d): Z memanggil POST /role-assignments dengan Idempotency-Key
#      dan body SAMA di organisasi A lalu B (dua kali per organisasi). Di organisasi yang sama
#      jawaban diputar ulang (Idempotent-Replayed, id sama, satu baris di database); di organisasi
#      lain DIEKSEKUSI (id baru, baris baru): replay lintas organisasi tidak boleh terjadi.
#   5. batas organisasi: adminB tidak melihat dan tidak dapat mencabut assignment organisasi A (404).
#   6. alur exit docs/21 dengan M: 403 → diberi role organization-admin → 200 segera → policy deny
#      → 403 permission_denied SERAGAM (klien tidak melihat policy; ADR-0022 butir 32e) sedangkan
#      simulate menjelaskan reason policy_denied + policy_code kepada pemegang policy.read dan audit
#      authz.access_denied memuat policy_code → policy dinonaktifkan → 200 → role dicabut → 403
#      segera; pencabutan ulang 409 assignment_revoked.
#   7. pencabutan assignment platform mencabut SEMUA sesi user itu (ADR-0022 butir 76): sesi user
#      bootstrap "platform" 200 → assignment-nya dicabut Super Admin smoke → cookie yang sama 401.
#      Super Admin smoke utama TIDAK pernah dicabut atau dikunci.
#
# Variabel:
#   BASE                  alamat API (bawaan http://localhost:${API_HOST_PORT:-8080}; harus localhost)
#   SMOKE_ADMIN_EMAIL / SMOKE_ADMIN_PASSWORD / SMOKE_STATE_DIR   lihat auth-lib.sh
#   SMOKE_PSQL            perintah psql superuser, bawaan
#                         `docker compose exec -T postgres psql -U postgres -d platform` (dijalankan
#                         dari akar repo; COMPOSE_PROJECT_NAME diwarisi dari `make verify`)
#
# Catatan fixture: baris ditulis langsung ke database SEBELUM user terkait pernah dievaluasi, jadi
# cache permission (versi di Redis) belum pernah memuat himpunan lama. Setelah itu SEMUA perubahan
# hak lewat endpoint (yang menginvalidasi cache), bukan lewat SQL.
#
# Anggaran login (LoginRule 10/menit/IP): skrip memakai 6 login tanpa satu pun kegagalan: sebelum
# kelompok login ia membaca X-RateLimit-Remaining dan menunggu pergantian jendela bila kurang
# (pola m03.sh); respons 429 sesudah itu ditunggu sekali menurut Retry-After.
set -euo pipefail
set -o errtrace

BASE=${BASE:-http://localhost:${API_HOST_PORT:-8080}}
API="$BASE/api/v1"

step() { echo "▶ $*"; }

# Pembersihan best-effort bila skrip berhenti di tengah: user bootstrap "platform" adalah Super
# Admin kedua dan TIDAK boleh tertinggal berhak; policy deny dinonaktifkan. Idempoten.
CLEANED=0
PLAT_ID="" POLICY_ID=""
cleanup_m04() {
  [ "$CLEANED" = 0 ] || return 0
  CLEANED=1
  if [ -n "$PLAT_ID" ]; then
    psql_run "UPDATE role_assignments SET revoked_at = now() WHERE subject_type = 'user' AND subject_id = '$PLAT_ID' AND scope_type = 'platform' AND revoked_at IS NULL" >/dev/null 2>&1 || true
  fi
  if [ -n "$POLICY_ID" ]; then
    psql_run "UPDATE policies SET status = 'disabled' WHERE id = '$POLICY_ID'" >/dev/null 2>&1 || true
  fi
}
trap cleanup_m04 ERR

fail() {
  echo "✗ $*" >&2
  cleanup_m04
  exit 1
}

STATUS="" HDR="" BODY=""
http() { # http <argumen curl...>: mengisi STATUS, HDR (header, tanpa CR), BODY
  local dir
  dir=$(mktemp -d)
  STATUS=$(curl -s -D "$dir/h" -o "$dir/b" -w '%{http_code}' --max-time "${SMOKE_CURL_MAX_TIME:-20}" "$@") || { rm -rf "$dir"; fail "curl gagal: $*"; }
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
json_field() { sed -n "s/.*\"$1\":\"\\([^\"]*\\)\".*/\\1/p" <<<"$BODY" | head -n1; }

# ---------- jatah login (LoginRule), pola m03.sh ----------
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

# ---------- psql (fixture dan pembuktian keadaan database) ----------
psql_run() { # psql_run <sql>: keluaran tanpa CR; SQL lewat stdin; dijalankan dari akar repo
  local -a cmd
  if [ -n "${SMOKE_PSQL:-}" ]; then
    read -r -a cmd <<<"$SMOKE_PSQL"
  else
    cmd=(docker compose exec -T postgres psql -U postgres -d platform)
  fi
  printf '%s\n' "$1" | (cd "$SMOKE_ROOT" && "${cmd[@]}" -v ON_ERROR_STOP=1 -qAt) | tr -d '\r'
}
sql1() { # sql1 <sql>: baris pertama keluaran (untuk SELECT dan INSERT ... RETURNING); gagal = fail
  local out
  out=$(psql_run "$1") || fail "psql gagal (docker compose exec postgres tersedia? atau isi SMOKE_PSQL): $1"
  head -n1 <<<"$out"
}

# ---------- panggilan API ----------
login_post() { # login_post <email> <password> <jar>: STATUS/HDR/BODY dari POST /auth/login
  http -c "$3" -X POST -H 'Content-Type: application/json' \
    --data "{\"email\":\"$1\",\"password\":\"$2\"}" "$API/auth/login"
  rl_note
}
L_JAR="" L_CSRF=""
login_user() { # login_user <email> <password>: mengisi L_JAR dan L_CSRF; menunggu 429 sekali
  L_JAR=$(new_jar)
  login_post "$1" "$2" "$L_JAR"
  if [ "$STATUS" = "429" ]; then
    local w
    w=$(header Retry-After)
    [[ "$w" =~ ^[0-9]+$ ]] || w=5
    [ "$w" -le 65 ] || fail "login $1: Retry-After terlalu lama: ${w}s"
    echo "• login $1 kena 429; menunggu ${w}s"
    sleep "$((w + 1))"
    login_post "$1" "$2" "$L_JAR"
  fi
  want 200 "login $1"
  L_CSRF=$(json_field csrf_token)
  [ -n "$L_CSRF" ] || fail "login $1 tanpa csrf_token: $BODY"
}

call() { # call <jar> <csrf> <metode> <jalur> [json]; opsional CALL_IDEM=<kunci> di depan
  local jar=$1 csrf=$2 method=$3 path=$4 data=${5-}
  local -a args=(-b "$jar" -X "$method")
  [ "$method" = GET ] || args+=(-H "X-CSRF-Token: $csrf")
  [ -z "${CALL_IDEM:-}" ] || args+=(-H "Idempotency-Key: $CALL_IDEM")
  if [ -n "$data" ]; then
    args+=(-H 'Content-Type: application/json' --data "$data")
  fi
  http "${args[@]}" "$API$path"
}

SW_CSRF=""
switch_org() { # switch_org <jar> <csrf> <organisasi>: token CSRF baru di SW_CSRF; cookie baru ke jar
  http -b "$1" -c "$1" -X POST -H 'Content-Type: application/json' -H "X-CSRF-Token: $2" \
    --data "{\"organization_id\":\"$3\"}" "$API/auth/switch-organization"
  want 200 "switch-organization ke $3"
  SW_CSRF=$(json_field csrf_token)
  [ -n "$SW_CSRF" ] || fail "switch-organization tanpa csrf_token: $BODY"
}

policy_denied_audits() { # policy_denied_audits <user> <kode-policy>: penolakan authz berkode policy itu di audit
  sql1 "SELECT count(*) FROM activities WHERE action = 'authz.access_denied' AND result = 'denied' AND actor_user_id = '$1' AND metadata->>'policy_code' = '$2'"
}

count_assign() { # count_assign <organisasi> <subjek> <role>: assignment aktif
  sql1 "SELECT count(*) FROM role_assignments WHERE organization_id = '$1' AND subject_type = 'user' AND subject_id = '$2' AND role_id = '$3' AND scope_type = 'organization' AND revoked_at IS NULL"
}

# ======================================================================================
# 0. fixture
# ======================================================================================
smoke_admin_session # step + login Super Admin smoke (JAR, CSRF, ADMIN_ID); HDR = respons login
rl_note
ADMIN_JAR=$JAR ADMIN_CSRF=$CSRF

RUN=$(head -c 5 /dev/urandom | od -An -tx1 | tr -d ' \n')
FIX_PW=$(random_password)
PLAT_EMAIL="smoke-m04-$RUN-plat@example.org"

step "user bootstrap 'platform' ($PLAT_EMAIL): make bootstrap-admin (Super Admin platform kedua; HANYA stack lokal)"
out=$(cd "$SMOKE_ROOT" && printf '%s\n' "$FIX_PW" |
  make -s bootstrap-admin EMAIL="$PLAT_EMAIL" NAME="Smoke M04 Platform" PASSWORD_STDIN=1 2>&1) ||
  fail "make bootstrap-admin ($PLAT_EMAIL) gagal: $out"
PLAT_ID=$(sql1 "SELECT id FROM users WHERE email = '$PLAT_EMAIL'")
[ -n "$PLAT_ID" ] || fail "user $PLAT_EMAIL tidak ditemukan setelah bootstrap-admin"

step "fixture psql: organisasi A dan B, user tenant (password disalin dari user platform), keanggotaan, assignment"
ROLE_MEMBER=$(sql1 "SELECT id FROM roles WHERE organization_id IS NULL AND slug = 'member'")
ROLE_ORGADMIN=$(sql1 "SELECT id FROM roles WHERE organization_id IS NULL AND slug = 'organization-admin'")
[ -n "$ROLE_MEMBER" ] && [ -n "$ROLE_ORGADMIN" ] ||
  fail "role sistem member/organization-admin belum ada (sinkron katalog saat start API gagal?)"

ORG_A=$(sql1 "INSERT INTO organizations (name, slug) VALUES ('Smoke M04 A $RUN', 'smoke-m04-$RUN-a') RETURNING id")
ORG_B=$(sql1 "INSERT INTO organizations (name, slug) VALUES ('Smoke M04 B $RUN', 'smoke-m04-$RUN-b') RETURNING id")

mk_user() { # mk_user <sufiks>: id user aktif
  sql1 "INSERT INTO users (display_name, email, status) VALUES ('Smoke M04 $1', 'smoke-m04-$RUN-$1@example.org', 'active') RETURNING id"
}
UA=$(mk_user a)
UB=$(mk_user b)
UZ=$(mk_user z)
UX=$(mk_user x)
UM=$(mk_user m)
psql_run "INSERT INTO credentials (user_id, credential_type, secret_hash)
  SELECT u.id, 'password', c.secret_hash
  FROM users u CROSS JOIN credentials c
  WHERE u.email LIKE 'smoke-m04-$RUN-%' AND u.id <> '$PLAT_ID'
    AND c.user_id = '$PLAT_ID' AND c.credential_type = 'password' AND c.revoked_at IS NULL" >/dev/null ||
  fail "salin kredensial ke user tenant gagal"
[ "$(sql1 "SELECT count(*) FROM credentials WHERE user_id IN ('$UA','$UB','$UZ','$UX','$UM') AND revoked_at IS NULL")" = "5" ] ||
  fail "kredensial user tenant tidak lengkap (diharapkan 5)"

mk_member() { # mk_member <organisasi> <user> <menit-lalu>: keanggotaan aktif (urutan menentukan organisasi awal login)
  psql_run "INSERT INTO organization_memberships (organization_id, user_id, status, joined_at, created_at) VALUES ('$1', '$2', 'active', now(), now() - interval '$3 minutes')" >/dev/null ||
    fail "fixture keanggotaan gagal"
}
mk_assign() { # mk_assign <organisasi> <role> <user>: assignment scope organisasi kanonik (scope_id NULL)
  psql_run "INSERT INTO role_assignments (organization_id, role_id, subject_type, subject_id, scope_type) VALUES ('$1', '$2', 'user', '$3', 'organization')" >/dev/null ||
    fail "fixture assignment gagal"
}
mk_member "$ORG_A" "$UA" 1
mk_member "$ORG_B" "$UB" 1
mk_member "$ORG_A" "$UZ" 2 # A lebih tua: organisasi awal login Z
mk_member "$ORG_B" "$UZ" 1
mk_member "$ORG_A" "$UX" 1
mk_member "$ORG_B" "$UX" 1
mk_member "$ORG_A" "$UM" 1
mk_assign "$ORG_A" "$ROLE_ORGADMIN" "$UA"
mk_assign "$ORG_B" "$ROLE_ORGADMIN" "$UB"
mk_assign "$ORG_A" "$ROLE_ORGADMIN" "$UZ"
mk_assign "$ORG_B" "$ROLE_ORGADMIN" "$UZ"
mk_assign "$ORG_A" "$ROLE_MEMBER" "$UM"

rl_wait 5 # login: platform, adminA, adminB, Z, M
step "login user platform, adminA, adminB, Z, dan M"
login_user "$PLAT_EMAIL" "$FIX_PW"
PLAT_JAR=$L_JAR PLAT_CSRF=$L_CSRF
login_user "smoke-m04-$RUN-a@example.org" "$FIX_PW"
A_JAR=$L_JAR A_CSRF=$L_CSRF
login_user "smoke-m04-$RUN-b@example.org" "$FIX_PW"
B_JAR=$L_JAR B_CSRF=$L_CSRF
login_user "smoke-m04-$RUN-z@example.org" "$FIX_PW"
Z_JAR=$L_JAR Z_CSRF=$L_CSRF
login_user "smoke-m04-$RUN-m@example.org" "$FIX_PW"
M_JAR=$L_JAR M_CSRF=$L_CSRF

# ======================================================================================
# 1. tanpa sesi → 401
# ======================================================================================
step "tanpa sesi → 401 authentication_required pada GET /roles, /permissions, /role-assignments, /policies"
for p in /roles /permissions /role-assignments /policies; do
  http "$API$p"
  want 401 "GET $p tanpa sesi"
  has '"authentication_required"' "GET $p tanpa sesi"
done

# ======================================================================================
# 2. tanpa hak → 403 permission_denied (mesin nyata)
# ======================================================================================
step "user Member (M) → 403 permission_denied pada rute baca, simulate, dan POST /role-assignments"
for p in /roles /permissions /role-assignments /policies; do
  call "$M_JAR" "$M_CSRF" GET "$p"
  want 403 "M GET $p"
  has '"permission_denied"' "M GET $p"
done
call "$M_JAR" "$M_CSRF" POST /permissions/simulate "{\"user_id\":\"$UM\",\"action\":\"role.read\"}"
want 403 "M POST /permissions/simulate"
has '"permission_denied"' "M POST /permissions/simulate"
ASSIGN_MEMBER_X="{\"role_id\":\"$ROLE_MEMBER\",\"subject_type\":\"user\",\"subject_id\":\"$UX\",\"scope_type\":\"organization\"}"
CALL_IDEM="smoke-m04-$RUN-denied" call "$M_JAR" "$M_CSRF" POST /role-assignments "$ASSIGN_MEMBER_X"
want 403 "M POST /role-assignments"
has '"permission_denied"' "M POST /role-assignments"
[ "$(count_assign "$ORG_A" "$UX" "$ROLE_MEMBER")" = "0" ] || fail "POST yang ditolak tetap membuat assignment"

# ======================================================================================
# 3. Super Admin platform: baca boleh, menulis/menyimulasikan di organisasi tidak
# ======================================================================================
step "Super Admin platform: GET /roles dan GET /policies 200; POST /role-assignments (scope organization) dan simulate → 403 scope_mismatch"
call "$ADMIN_JAR" "$ADMIN_CSRF" GET /roles
want 200 "Super Admin GET /roles"
has '"slug":"super-admin"' "Super Admin GET /roles"
call "$ADMIN_JAR" "$ADMIN_CSRF" GET /policies
want 200 "Super Admin GET /policies"
CALL_IDEM="smoke-m04-$RUN-platform" call "$ADMIN_JAR" "$ADMIN_CSRF" POST /role-assignments "$ASSIGN_MEMBER_X"
want 403 "Super Admin POST /role-assignments scope organization"
has '"scope_mismatch"' "Super Admin POST /role-assignments scope organization"
call "$ADMIN_JAR" "$ADMIN_CSRF" POST /permissions/simulate "{\"user_id\":\"$UM\",\"action\":\"role.read\"}"
want 403 "Super Admin POST /permissions/simulate"
has '"scope_mismatch"' "Super Admin POST /permissions/simulate"

# ======================================================================================
# 4. idempotensi: kunci dan body SAMA di dua organisasi (ADR-0022 butir 73d)
# ======================================================================================
IDEM_KEY="smoke-m04-$RUN-idem"
post_x() { # post_x <jar> <csrf>: POST /role-assignments dengan IDEM_KEY dan body tetap
  CALL_IDEM=$IDEM_KEY call "$1" "$2" POST /role-assignments "$ASSIGN_MEMBER_X"
}

step "Z di organisasi A: POST /role-assignments → 201 dieksekusi (bukan replay)"
switch_org "$Z_JAR" "$Z_CSRF" "$ORG_A"
Z_CSRF=$SW_CSRF
post_x "$Z_JAR" "$Z_CSRF"
want 201 "POST pertama di A"
[ -z "$(header Idempotent-Replayed)" ] || fail "POST pertama di A ditandai replay"
ID_A=$(json_field id)
[ -n "$ID_A" ] || fail "POST pertama di A tanpa id: $BODY"
BODY_A=$BODY
[ "$(count_assign "$ORG_A" "$UX" "$ROLE_MEMBER")" = "1" ] || fail "assignment di A tidak tepat satu setelah POST pertama"

step "Z di organisasi A: kunci dan body sama → 201 DIPUTAR ULANG (Idempotent-Replayed, id dan badan sama, tidak ada baris baru)"
post_x "$Z_JAR" "$Z_CSRF"
want 201 "POST kedua di A (replay)"
[ "$(header Idempotent-Replayed)" = "true" ] || fail "POST kedua di A tidak ditandai Idempotent-Replayed (dieksekusi ulang?): $HDR"
[ "$BODY" = "$BODY_A" ] || fail "replay di A tidak identik dengan jawaban pertama: $BODY"
[ "$(count_assign "$ORG_A" "$UX" "$ROLE_MEMBER")" = "1" ] || fail "replay di A mengubah jumlah assignment"

step "Z pindah ke organisasi B: kunci dan body SAMA → 201 DIEKSEKUSI (id baru, tanpa tanda replay)"
switch_org "$Z_JAR" "$Z_CSRF" "$ORG_B"
Z_CSRF=$SW_CSRF
post_x "$Z_JAR" "$Z_CSRF"
want 201 "POST pertama di B (kunci sama dengan A)"
[ -z "$(header Idempotent-Replayed)" ] ||
  fail "POST di B memutar ulang jawaban organisasi A (replay lintas organisasi!): $HDR"
ID_B=$(json_field id)
[ -n "$ID_B" ] && [ "$ID_B" != "$ID_A" ] || fail "POST di B tidak membuat assignment baru (id B='$ID_B', id A='$ID_A'): $BODY"
BODY_B=$BODY
[ "$(count_assign "$ORG_B" "$UX" "$ROLE_MEMBER")" = "1" ] || fail "assignment di B tidak tepat satu setelah POST pertama"
[ "$(sql1 "SELECT organization_id FROM role_assignments WHERE id = '$ID_B'")" = "$ORG_B" ] ||
  fail "assignment hasil POST di B tidak tersimpan di organisasi B"

step "Z di organisasi B: kunci dan body sama lagi → diputar ulang dengan id B"
post_x "$Z_JAR" "$Z_CSRF"
want 201 "POST kedua di B (replay)"
[ "$(header Idempotent-Replayed)" = "true" ] || fail "POST kedua di B tidak ditandai replay: $HDR"
[ "$BODY" = "$BODY_B" ] || fail "replay di B tidak identik dengan jawaban B: $BODY"
[ "$(count_assign "$ORG_B" "$UX" "$ROLE_MEMBER")" = "1" ] || fail "replay di B mengubah jumlah assignment"

step "Z kembali ke organisasi A: kunci sama → tetap diputar ulang dengan id A (catatan per scope bertahan)"
switch_org "$Z_JAR" "$Z_CSRF" "$ORG_A"
Z_CSRF=$SW_CSRF
post_x "$Z_JAR" "$Z_CSRF"
want 201 "POST ketiga di A (replay)"
[ "$(header Idempotent-Replayed)" = "true" ] || fail "POST ketiga di A tidak ditandai replay: $HDR"
[ "$BODY" = "$BODY_A" ] || fail "replay ketiga di A bukan jawaban A: $BODY"

# ======================================================================================
# 5. batas organisasi
# ======================================================================================
step "adminB tidak melihat assignment organisasi A di daftar dan tidak dapat mencabutnya (404)"
call "$B_JAR" "$B_CSRF" GET "/role-assignments?subject_id=$UX"
want 200 "adminB GET /role-assignments"
has "\"id\":\"$ID_B\"" "adminB melihat assignment organisasinya"
hasnt "\"id\":\"$ID_A\"" "adminB tidak boleh melihat assignment organisasi A"
call "$B_JAR" "$B_CSRF" DELETE "/role-assignments/$ID_A"
want 404 "adminB DELETE assignment organisasi A"
has '"resource_not_found"' "adminB DELETE assignment organisasi A"
[ "$(count_assign "$ORG_A" "$UX" "$ROLE_MEMBER")" = "1" ] || fail "pencabutan lintas organisasi mengubah assignment A"

# ======================================================================================
# 6. alur exit docs/21 dengan M di organisasi A
# ======================================================================================
step "M (Member) → 403; simulator menjelaskan no_permission"
call "$M_JAR" "$M_CSRF" GET /roles
want 403 "M GET /roles sebelum diberi role"
call "$A_JAR" "$A_CSRF" POST /permissions/simulate "{\"user_id\":\"$UM\",\"action\":\"role.read\"}"
want 200 "simulate M role.read (sebelum)"
has '"result":"DENY"' "simulate sebelum"
has '"reason":"no_permission"' "simulate sebelum"

step "adminA memberi M role organization-admin → M GET /roles 200 SEGERA (cache diinvalidasi)"
CALL_IDEM="smoke-m04-$RUN-grant-m" call "$A_JAR" "$A_CSRF" POST /role-assignments \
  "{\"role_id\":\"$ROLE_ORGADMIN\",\"subject_type\":\"user\",\"subject_id\":\"$UM\",\"scope_type\":\"organization\"}"
want 201 "adminA memberi M role organization-admin"
GM=$(json_field id)
[ -n "$GM" ] || fail "pemberian role ke M tanpa id: $BODY"
call "$M_JAR" "$M_CSRF" GET /roles
want 200 "M GET /roles setelah diberi role"
call "$A_JAR" "$A_CSRF" POST /permissions/simulate "{\"user_id\":\"$UM\",\"action\":\"role.read\"}"
want 200 "simulate M role.read (sesudah)"
has '"result":"ALLOW"' "simulate sesudah"
has '"reason":"granted"' "simulate sesudah"
has '"level":"organization"' "simulate sesudah"

step "policy deny untuk M pada role.read → M 403 permission_denied seragam; simulate menyebut policy_denied + policy_code; audit memuat policy_code; GET /policies memuatnya"
POLICY_CODE="smoke_m04_$RUN"
call "$A_JAR" "$A_CSRF" POST /policies \
  "{\"code\":\"$POLICY_CODE\",\"name\":\"Smoke M04 deny\",\"description\":\"smoke\",\"effect\":\"deny\",\"expression\":\"current_user.id == \\\"$UM\\\"\",\"actions\":[\"role.read\"],\"status\":\"active\"}"
want 201 "POST /policies"
POLICY_ID=$(json_field id)
[ -n "$POLICY_ID" ] || fail "POST /policies tanpa id: $BODY"
call "$M_JAR" "$M_CSRF" GET /roles
want 403 "M GET /roles dengan policy deny"
has '"permission_denied"' "M GET /roles dengan policy deny"
hasnt "$POLICY_CODE|policy_denied|policy" "M GET /roles dengan policy deny (klien tidak boleh melihat policy)"
for _ in 1 2 3 4 5 6 7 8 9 10; do
  [ "$(policy_denied_audits "$UM" "$POLICY_CODE")" -ge 1 ] && break
  sleep 0.5
done
[ "$(policy_denied_audits "$UM" "$POLICY_CODE")" -ge 1 ] ||
  fail "audit authz.access_denied tanpa policy_code=$POLICY_CODE untuk M setelah penolakan policy"
call "$A_JAR" "$A_CSRF" POST /permissions/simulate "{\"user_id\":\"$UM\",\"action\":\"role.read\"}"
want 200 "simulate M role.read (policy)"
has '"result":"DENY"' "simulate policy"
has '"reason":"policy_denied"' "simulate policy"
has "\"policy_code\":\"$POLICY_CODE\"" "simulate policy (adminA punya policy.read)"
call "$A_JAR" "$A_CSRF" GET /policies
want 200 "adminA GET /policies"
has "\"code\":\"$POLICY_CODE\"" "GET /policies memuat policy baru"

step "policy dinonaktifkan (PATCH) → M GET /roles 200 lagi (policy tidak di-cache)"
call "$A_JAR" "$A_CSRF" PATCH "/policies/$POLICY_ID" '{"status":"disabled"}'
want 200 "PATCH /policies status disabled"
call "$M_JAR" "$M_CSRF" GET /roles
want 200 "M GET /roles setelah policy dinonaktifkan"
POLICY_ID=""

step "adminA mencabut role M → M GET /roles 403 SEGERA; pencabutan ulang 409 assignment_revoked"
call "$A_JAR" "$A_CSRF" DELETE "/role-assignments/$GM"
want 204 "adminA DELETE role M"
call "$M_JAR" "$M_CSRF" GET /roles
want 403 "M GET /roles setelah role dicabut"
has '"permission_denied"' "M GET /roles setelah role dicabut"
call "$A_JAR" "$A_CSRF" POST /permissions/simulate "{\"user_id\":\"$UM\",\"action\":\"role.read\"}"
want 200 "simulate M role.read (dicabut)"
has '"reason":"no_permission"' "simulate setelah dicabut"
call "$A_JAR" "$A_CSRF" DELETE "/role-assignments/$GM"
want 409 "DELETE ulang"
has '"assignment_revoked"' "DELETE ulang"

# ======================================================================================
# 7. pencabutan assignment platform mencabut sesi user itu
# ======================================================================================
step "user 'platform' punya sesi hidup (GET /auth/me 200) dan satu assignment platform aktif"
call "$PLAT_JAR" "$PLAT_CSRF" GET /auth/me
want 200 "me user platform sebelum dicabut"
call "$ADMIN_JAR" "$ADMIN_CSRF" GET "/role-assignments?subject_id=$PLAT_ID"
want 200 "Super Admin GET /role-assignments?subject_id=platform"
PLAT_ASSIGN=$(json_field id)
[ -n "$PLAT_ASSIGN" ] || fail "assignment platform user tidak ditemukan di daftar: $BODY"
[ "$(sql1 "SELECT id FROM role_assignments WHERE subject_id = '$PLAT_ID' AND scope_type = 'platform' AND revoked_at IS NULL")" = "$PLAT_ASSIGN" ] ||
  fail "id assignment di daftar berbeda dari database"
[ "$(sql1 "SELECT count(*) FROM sessions WHERE user_id = '$PLAT_ID' AND revoked_at IS NULL")" -ge 1 ] ||
  fail "user platform tidak punya sesi aktif di database sebelum pencabutan"

step "Super Admin smoke mencabut assignment platform user itu → 204; cookie yang SAMA lalu 401; tidak ada sesi aktif tersisa"
call "$ADMIN_JAR" "$ADMIN_CSRF" DELETE "/role-assignments/$PLAT_ASSIGN"
want 204 "DELETE assignment platform"
call "$PLAT_JAR" "$PLAT_CSRF" GET /auth/me
want 401 "me user platform setelah assignment platform dicabut"
has '"authentication_required"' "me user platform setelah dicabut"
[ "$(sql1 "SELECT count(*) FROM sessions WHERE user_id = '$PLAT_ID' AND revoked_at IS NULL")" = "0" ] ||
  fail "masih ada sesi aktif user platform setelah pencabutan assignment platform"
[ "$(sql1 "SELECT count(*) FROM role_assignments WHERE subject_id = '$PLAT_ID' AND scope_type = 'platform' AND revoked_at IS NULL")" = "0" ] ||
  fail "assignment platform user masih aktif"

step "Super Admin smoke tetap berhak (tidak ikut dicabut): GET /roles 200"
call "$ADMIN_JAR" "$ADMIN_CSRF" GET /roles
want 200 "Super Admin smoke setelah pencabutan user platform"

cleanup_m04
echo "M04 smoke OK"
