#!/usr/bin/env bash
# Tahap smoke dari `make verify` (docs/16 §2.7): naikkan stack SEMENTARA (infra, migrate, api,
# worker, web), jalankan `make smoke`, lalu bongkar. Memakai compose project terpisah supaya
# `down -v` TIDAK menyentuh volume data pengembangan.
set -euo pipefail
cd "$(dirname "$0")/.."

export COMPOSE_PROJECT_NAME=${VERIFY_PROJECT:-platform-verify}
COMPOSE=(docker compose --profile full)

# Port host yang dipublikasikan compose (bawaan atau dari .env, ADR-0021); bentrok dengan stack
# dev/proses lain = gagal dengan pesan jelas. Nilai: environment, lalu .env, lalu bawaan.
env_port() { # env_port NAMA BAWAAN
  local v=${!1:-}
  if [ -z "$v" ] && [ -f .env ]; then
    v=$(sed -n "s/^$1=//p" .env | tail -n1 | tr -d '\r"')
  fi
  echo "${v:-$2}"
}
for port in "$(env_port POSTGRES_HOST_PORT 5432)" "$(env_port REDIS_HOST_PORT 6379)" \
  "$(env_port API_HOST_PORT 8080)" "$(env_port WEB_HOST_PORT 5173)" \
  "$(env_port MAILPIT_SMTP_HOST_PORT 1025)" "$(env_port MAILPIT_UI_HOST_PORT 8025)"; do
  if (exec 3<>"/dev/tcp/127.0.0.1/$port") 2>/dev/null; then
    echo "✗ port $port sudah dipakai. Hentikan stack/proses dev dulu (make docker-down, atau hentikan make dev), lalu ulangi make verify." >&2
    exit 1
  fi
done

created_env=0
if [ ! -f .env ]; then
  cp .env.example .env
  created_env=1
fi

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "----- verify-smoke gagal (kode $status); status & log service -----" >&2
    "${COMPOSE[@]}" ps -a >&2 || true
    "${COMPOSE[@]}" logs --no-color --tail=40 >&2 || true
  fi
  "${COMPOSE[@]}" down -v --remove-orphans >/dev/null 2>&1 || true
  [ "$created_env" = "1" ] && rm -f .env

  # Container berjalan sebagai root dengan bind-mount .:/app; bila ada jalur tulis yang tak di-mask
  # volume, berkas milik root muncul di working tree dan merusak `npm ci`/`make dev` berikutnya.
  if [ "$(id -u)" -ne 0 ]; then
    leaked=$(find . -path ./.git -prune -o -user root -print 2>/dev/null | head -5)
    if [ -n "$leaked" ]; then
      echo "✗ container meninggalkan berkas milik root di repo (jalur tulis belum di-mask volume di docker-compose.yml):" >&2
      echo "$leaked" | sed 's/^/    /' >&2
      echo "  Perbaiki kepemilikan: sudo chown -R \"\$USER\":\"\$USER\" ." >&2
      [ "$status" -ne 0 ] || status=1
    fi
  fi
  exit "$status"
}
trap cleanup EXIT

"${COMPOSE[@]}" up -d --build
bash ./scripts/wait-for-healthy.sh
make smoke
