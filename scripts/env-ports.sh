#!/usr/bin/env bash
# Membuat .env dari .env.example dengan PORT HOST compose yang berbeda (ADR-0021), mis. di VPS
# yang port 5432/6379/8080 nya sudah dipakai. Semua nilai yang bergantung pada port ikut diubah
# supaya konsisten: URL database/Redis (proses native: make db-seed, bootstrap-admin, test),
# ALLOWED_ORIGINS, PUBLIC_BASE_URL, dan SMTP_PORT.
#
# Pakai:  bash scripts/env-ports.sh [--force] [POSTGRES=n] [REDIS=n] [API=n] [WEB=n] [SMTP=n] [MAILUI=n]
# Bawaan: POSTGRES=5432 REDIS=6379 API=8080 WEB=5173 SMTP=1025 MAILUI=8025
# Aturan: port 1024–65535 dan semuanya berbeda. Menolak menimpa .env yang sudah ada tanpa --force.
# Hanya mengatur port HOST: PORT (port di dalam container api) tetap 8080, dan port compose tetap
# terikat ke 127.0.0.1. Env ENV_OUT mengganti berkas keluaran (untuk tes).
set -euo pipefail
cd "$(dirname "$0")/.."

out=${ENV_OUT:-.env}
force=0
pg=5432 redis=6379 api=8080 web=5173 smtp=1025 mailui=8025

die() { echo "env-ports: $*" >&2; exit 2; }

for arg in "$@"; do
  case "$arg" in
    --force) force=1 ;;
    POSTGRES=*) pg=${arg#*=} ;;
    REDIS=*) redis=${arg#*=} ;;
    API=*) api=${arg#*=} ;;
    WEB=*) web=${arg#*=} ;;
    SMTP=*) smtp=${arg#*=} ;;
    MAILUI=*) mailui=${arg#*=} ;;
    *) die "argumen tidak dikenal: $arg (lihat komentar di kepala skrip)" ;;
  esac
done

names=(POSTGRES REDIS API WEB SMTP MAILUI)
values=("$pg" "$redis" "$api" "$web" "$smtp" "$mailui")
for i in "${!names[@]}"; do
  v=${values[$i]}
  [[ "$v" =~ ^[0-9]{1,5}$ ]] || die "${names[$i]} harus berupa angka 1–5 digit, dapat: '$v'"
  # 10# mencegah angka berawalan 0 dibaca sebagai oktal.
  if [ "$((10#$v))" -lt 1024 ] || [ "$((10#$v))" -gt 65535 ]; then
    die "${names[$i]}=$v di luar rentang 1024–65535"
  fi
  values[$i]=$((10#$v))
done
pg=${values[0]} redis=${values[1]} api=${values[2]} web=${values[3]} smtp=${values[4]} mailui=${values[5]}

dups=$(printf '%s\n' "${values[@]}" | sort | uniq -d | head -n1)
[ -z "$dups" ] || die "port $dups dipakai lebih dari satu layanan; setiap layanan butuh port host sendiri"

[ -f .env.example ] || die ".env.example tidak ditemukan di root repo"
if [ -e "$out" ] && [ "$force" != "1" ]; then
  die "$out sudah ada; tidak ditimpa (tambahkan --force bila memang ingin menggantinya)"
fi

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

while IFS= read -r line || [ -n "$line" ]; do
  case "$line" in
    DATABASE_URL=* | MAINTENANCE_DATABASE_URL=* | MIGRATION_DATABASE_URL=*) line=${line/@localhost:5432/@localhost:$pg} ;;
    REDIS_URL=*) line=${line/localhost:6379/localhost:$redis} ;;
    ALLOWED_ORIGINS=* | PUBLIC_BASE_URL=*) line=${line/localhost:5173/localhost:$web} ;;
    SMTP_PORT=*) line="SMTP_PORT=$smtp" ;;
  esac
  printf '%s\n' "$line"
done <.env.example >"$tmp"

# Bila .env.example berubah bentuk sehingga penggantian tak kena, gagal keras (bukan .env setengah benar).
[ "$(grep -c "@localhost:$pg/platform" "$tmp")" = "3" ] || die ".env.example: 3 URL database yang diharapkan tidak ditemukan"
grep -q "^REDIS_URL=redis://localhost:$redis\$" "$tmp" || die ".env.example: REDIS_URL tidak berbentuk yang diharapkan"
grep -q "^ALLOWED_ORIGINS=http://localhost:$web\$" "$tmp" || die ".env.example: ALLOWED_ORIGINS tidak berbentuk yang diharapkan"
grep -q "^PUBLIC_BASE_URL=http://localhost:$web\$" "$tmp" || die ".env.example: PUBLIC_BASE_URL tidak berbentuk yang diharapkan"
grep -q "^SMTP_PORT=$smtp\$" "$tmp" || die ".env.example: SMTP_PORT tidak ditemukan"

{
  echo
  echo "# --- Port HOST compose (dibuat scripts/env-ports.sh; ADR-0021) ---"
  echo "POSTGRES_HOST_PORT=$pg"
  echo "REDIS_HOST_PORT=$redis"
  echo "API_HOST_PORT=$api"
  echo "WEB_HOST_PORT=$web"
  echo "MAILPIT_SMTP_HOST_PORT=$smtp"
  echo "MAILPIT_UI_HOST_PORT=$mailui"
} >>"$tmp"

mv "$tmp" "$out"
chmod 600 "$out"
trap - EXIT
echo "env-ports: $out dibuat (postgres=$pg redis=$redis api=$api web=$web smtp=$smtp mailpit-ui=$mailui)"
