#!/usr/bin/env bash
# Menunggu SEMUA service compose (profile full) siap, dengan batas waktu (docs/15 §8, docs/19 §3).
#   siap = running+healthy, running tanpa healthcheck, atau exited dengan kode 0 (migrate).
#   gagal = unhealthy, exited non-nol, dead; service yang diharapkan tak kunjung muncul.
# Env: WAIT_TIMEOUT (detik, default 240), WAIT_INTERVAL (default 3),
#      EXPECTED_SERVICES (default "postgres redis mailpit migrate api worker web"),
#      COMPOSE_PROJECT_NAME (opsional; dipakai docker compose apa adanya).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

TIMEOUT=${WAIT_TIMEOUT:-240}
INTERVAL=${WAIT_INTERVAL:-3}
EXPECTED=${EXPECTED_SERVICES:-"postgres redis mailpit migrate api worker web"}
COMPOSE=(docker compose --profile full)

dump_logs() {
  for svc in "$@"; do
    echo "----- log $svc (50 baris terakhir) -----" >&2
    "${COMPOSE[@]}" logs --no-color --tail=50 "$svc" >&2 || true
  done
}

deadline=$((SECONDS + TIMEOUT))
while true; do
  ids=$("${COMPOSE[@]}" ps -a -q 2>/dev/null || true)

  declare -A seen=()
  pending=()
  failed=()

  for id in $ids; do
    svc=$(docker inspect -f '{{index .Config.Labels "com.docker.compose.service"}}' "$id")
    state=$(docker inspect -f '{{.State.Status}}' "$id")
    code=$(docker inspect -f '{{.State.ExitCode}}' "$id")
    health=$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$id")
    seen[$svc]=1
    case "$state" in
      running)
        case "$health" in
          healthy | none) ;;
          starting) pending+=("$svc") ;;
          *) failed+=("$svc ($health)") ;;
        esac
        ;;
      exited)
        [ "$code" = "0" ] || failed+=("$svc (exit $code)")
        ;;
      created | restarting) pending+=("$svc") ;;
      *) failed+=("$svc ($state)") ;;
    esac
  done

  for svc in $EXPECTED; do
    [ -n "${seen[$svc]:-}" ] || pending+=("$svc (belum ada)")
  done

  if [ ${#failed[@]} -gt 0 ]; then
    echo "✗ service gagal: ${failed[*]}" >&2
    dump_logs "${failed[@]%% *}"
    exit 1
  fi
  if [ ${#pending[@]} -eq 0 ]; then
    echo "✓ semua service siap"
    exit 0
  fi
  if [ "$SECONDS" -ge "$deadline" ]; then
    echo "✗ timeout ${TIMEOUT}s; belum siap: ${pending[*]}" >&2
    dump_logs "${pending[@]%% *}"
    exit 1
  fi
  echo "… menunggu: ${pending[*]}"
  sleep "$INTERVAL"
done
