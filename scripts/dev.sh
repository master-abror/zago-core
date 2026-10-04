#!/usr/bin/env bash
# Menjalankan api + worker + frontend native dalam SATU terminal (docs/16 §2.2).
# Ctrl-C (atau salah satu proses berhenti) menghentikan SEMUA anak: tidak ada proses yatim.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1

cleanup() {
  trap - EXIT INT TERM
  # Skrip ini mengabaikan TERM sendiri supaya `kill 0` (ke seluruh process group) tidak
  # membunuhnya sebelum kode keluar asli diteruskan. Anak sudah berjalan, jadi tetap menerima TERM.
  trap '' TERM
  kill 0 2>/dev/null || true
  wait 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 130' INT TERM

make run-api &
make run-worker &
make frontend &

# Tunggu proses pertama yang berhenti, lalu teruskan kode keluarnya; trap EXIT mematikan sisanya.
wait -n
status=$?
echo "dev: salah satu proses berhenti (kode $status); menghentikan semua." >&2
exit "$status"
