#!/usr/bin/env bash
# Replaces everything in the plsremind database with a dump from backup.sh.
set -euo pipefail

cd "$(dirname "$0")"
yes=""
if [ "${1:-}" = "--yes" ]; then
  yes=1
  shift
fi
file="${1:-}"
if [ -z "$file" ] || [ ! -s "$file" ]; then
  echo "usage: ./restore.sh [--yes] backups/plsremind-YYYYMMDD-HHMMSS.dump" >&2
  exit 2
fi
if [ -z "$yes" ]; then
  read -r -p "This replaces everything in the database with $file. Type yes to go on: " answer
  [ "$answer" = "yes" ] || exit 1
fi

# Whatever writes to the database stops first, every bot included; monitoring may go on.
writers=()
while IFS= read -r service; do
  writers+=("$service")
done < <(docker compose ps --services --status running | grep -v -x -e db -e victoriametrics -e vmalert -e grafana || true)
if [ "${#writers[@]}" -gt 0 ]; then
  docker compose stop "${writers[@]}"
fi
docker compose exec -T db dropdb --username plsremind --if-exists --force plsremind
docker compose exec -T db createdb --username plsremind plsremind
docker compose exec -T db pg_restore --username plsremind --dbname plsremind --no-owner < "$file"
if [ "${#writers[@]}" -gt 0 ]; then
  docker compose start "${writers[@]}"
fi
echo "restored $file"
