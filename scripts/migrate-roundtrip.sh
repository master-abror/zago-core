#!/usr/bin/env bash
# `make migrate-roundtrip` (docs/16 §2.4, ADR-0005): nyalakan PostgreSQL SEMENTARA di Docker, jalankan
# `cmd/migrate roundtrip` (up -> down(semua) -> up pada database sementara di server itu), lalu bongkar.
#
# Kenapa container sendiri, bukan DB dev (:5432): `make verify` tidak boleh bergantung pada stack dev
# yang kebetulan menyala, dan `verify-smoke.sh` justru menolak jalan bila port 5432 terpakai. Port host
# di sini dipilih acak oleh Docker, sehingga tidak pernah bentrok. Role & hak memakai berkas yang sama
# dengan compose dev (deploy/db/init/*.sql), jadi yang diuji adalah role yang sama.
#
# Variabel opsional: PG_IMAGE (default postgres:18, ADR-0002).
set -euo pipefail
cd "$(dirname "$0")/.."

PG_IMAGE=${PG_IMAGE:-postgres:18}
NAME="zago-roundtrip-$$"

if ! command -v docker >/dev/null 2>&1 || ! docker info >/dev/null 2>&1; then
  echo "✗ migrate-roundtrip butuh Docker yang berjalan (PostgreSQL sementara). Di WSL: jalankan Docker Desktop dan aktifkan integrasi WSL." >&2
  exit 1
fi

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "----- migrate-roundtrip gagal (kode $status); log PostgreSQL -----" >&2
    docker logs --tail 30 "$NAME" >&2 2>&1 || true
  fi
  docker rm -f -v "$NAME" >/dev/null 2>&1 || true
  exit "$status"
}
trap cleanup EXIT

docker run -d --name "$NAME" \
  -e POSTGRES_DB=platform -e POSTGRES_USER=postgres -e POSTGRES_PASSWORD=postgres \
  -v "$PWD/deploy/db/init:/docker-entrypoint-initdb.d:ro" \
  -p 127.0.0.1::5432 \
  "$PG_IMAGE" >/dev/null

# Server sementara milik init script hanya mendengarkan di socket unix; pemeriksaan lewat TCP
# (-h 127.0.0.1) baru berhasil setelah server FINAL menyala, jadi init script pasti sudah selesai.
ready=0
for _ in $(seq 1 90); do
  if docker exec "$NAME" pg_isready -h 127.0.0.1 -U postgres -d platform >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
if [ "$ready" -ne 1 ]; then
  echo "✗ PostgreSQL sementara tidak siap dalam 90 detik" >&2
  exit 1
fi

port=$(docker port "$NAME" 5432/tcp | head -n1 | sed 's/.*://')
if [ -z "$port" ]; then
  echo "✗ gagal membaca port host container" >&2
  exit 1
fi

# Menimpa MIGRATION_DATABASE_URL dari .env (Makefile mengekspornya): target roundtrip adalah container ini.
MIGRATION_DATABASE_URL="pgx5://app_migrator:migrator_dev_pw@127.0.0.1:${port}/platform" \
  go run ./backend/cmd/migrate roundtrip
