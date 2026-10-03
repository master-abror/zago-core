#!/usr/bin/env bash
# Tahap smoke dari `make verify` (docs/16 §2.7): naikkan stack SEMENTARA (infra, migrate, api,
# worker, web), jalankan `make smoke`, lalu bongkar. Memakai compose project terpisah supaya
# `down -v` TIDAK menyentuh volume data pengembangan.
set -euo pipefail
cd "$(dirname "$0")/.."

export COMPOSE_PROJECT_NAME=${VERIFY_PROJECT:-platform-verify}
COMPOSE=(docker compose --profile full)

# Port host yang dipublikasikan compose; bentrok dengan stack dev = gagal dengan pesan jelas.
for port in 5432 6379 8080 5173 1025 8025; do
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
  exit "$status"
}
trap cleanup EXIT

"${COMPOSE[@]}" up -d --build
bash ./scripts/wait-for-healthy.sh
make smoke
