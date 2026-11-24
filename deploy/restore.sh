#!/usr/bin/env bash
# Replaces the plsremind database with a dump from backup.sh; if the dump cannot be restored, nothing changes.
set -euo pipefail

cd "$(dirname "$0")"
yes=""
if [ "${1:-}" = "--yes" ]; then
  yes=1
  shift
fi
file="${1:-}"
if [ -z "$file" ] || [ ! -s "$file" ]; then
  echo "usage: ./restore.sh [--yes] backups/plsremind-<date>-<time>-<n>.dump" >&2
  exit 2
fi
if [ -z "$yes" ]; then
  read -r -p "This replaces everything in the database with $file. Type yes to go on: " answer
  [ "$answer" = "yes" ] || exit 1
fi

psql() {
  docker compose exec -T -e PGOPTIONS="-c client_min_messages=warning" db \
    psql --username plsremind --dbname postgres --quiet -v ON_ERROR_STOP=1 "$@"
}

# Restored next to the current database first, so that a dump that breaks off changes nothing.
psql -c "DROP DATABASE IF EXISTS plsremind_restore" -c "CREATE DATABASE plsremind_restore OWNER plsremind"
if ! docker compose exec -T db pg_restore --username plsremind --dbname plsremind_restore \
  --no-owner --exit-on-error --single-transaction < "$file"; then
  psql -c "DROP DATABASE IF EXISTS plsremind_restore"
  echo "restore.sh: $file could not be restored; the database is as it was" >&2
  exit 1
fi

# A hub that keeps crashing after an update is the usual reason to go back, so it counts too.
if ! running="$(docker compose ps --services --status running --status restarting)"; then
  psql -c "DROP DATABASE IF EXISTS plsremind_restore"
  echo "restore.sh: the running services could not be listed; the database is as it was" >&2
  exit 1
fi
writers=()
while IFS= read -r service; do
  case "$service" in
    "" | db | victoriametrics | vmalert | grafana) ;;
    *) writers+=("$service") ;;
  esac
done <<< "$running"
if [ "${#writers[@]}" -gt 0 ]; then
  docker compose stop "${writers[@]}"
fi

# One statement string is one transaction, so there is never a moment without a plsremind database.
psql -c "DROP DATABASE IF EXISTS plsremind_replaced" \
  -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'plsremind'" \
  -c "ALTER DATABASE plsremind RENAME TO plsremind_replaced; ALTER DATABASE plsremind_restore RENAME TO plsremind;" \
  -c "DROP DATABASE plsremind_replaced" >/dev/null

# up rather than start: after unpacking an older bundle, start would bring back the newer containers.
if [ "${#writers[@]}" -gt 0 ]; then
  docker compose up -d --no-deps "${writers[@]}"
fi
echo "restored $file"
