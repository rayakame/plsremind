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

# Two restores at once would share the staging database.
lock=".restore.lock"
if ! mkdir "$lock" 2>/dev/null; then
  echo "restore.sh: another restore is running; if none is, remove the folder $lock" >&2
  exit 1
fi
stopped=()
finish() {
  if [ "${#stopped[@]}" -gt 0 ]; then
    # up rather than start: after unpacking an older bundle, start would bring back the newer containers.
    docker compose up -d --no-deps "${stopped[@]}" \
      || echo "restore.sh: start ${stopped[*]} again with docker compose up -d" >&2
  fi
  rmdir "$lock"
}
trap finish EXIT

psql() {
  docker compose exec -T -e PGOPTIONS="-c client_min_messages=warning" db \
    psql --username plsremind --dbname postgres --quiet -v ON_ERROR_STOP=1 "$@"
}

give_up() {
  psql -c "DROP DATABASE IF EXISTS plsremind_restore" || true
  echo "restore.sh: $1; the database is as it was" >&2
  exit 1
}

# Restored next to the current database first, so that a dump that breaks off changes nothing.
psql -c "DROP DATABASE IF EXISTS plsremind_restore" -c "CREATE DATABASE plsremind_restore OWNER plsremind"
docker compose exec -T db pg_restore --username plsremind --dbname plsremind_restore \
  --no-owner --exit-on-error --single-transaction < "$file" || give_up "$file could not be restored"

# A hub that keeps crashing after an update is the usual reason to go back, so it counts too.
running="$(docker compose ps --status running --status restarting --format '{{.ID}} {{.Service}}')" \
  || give_up "the running services could not be listed"
profiles=()
while IFS= read -r profile; do
  [ -n "$profile" ] && profiles+=(--profile "$profile")
done <<< "$(docker compose config --profiles)"
known="$(docker compose ${profiles[@]+"${profiles[@]}"} config --services)" \
  || give_up "the services of compose.yaml could not be listed"
containers=()
orphans=()
while read -r id service; do
  case "$service" in
    "" | db | victoriametrics | vmalert | grafana) continue ;;
  esac
  containers+=("$id")
  if grep -qxF "$service" <<< "$known"; then
    stopped+=("$service")
  else
    orphans+=("$service")
  fi
done <<< "$running"
if [ "${#containers[@]}" -gt 0 ]; then
  docker stop "${containers[@]}" >/dev/null
fi
if [ "${#orphans[@]}" -gt 0 ]; then
  echo "restore.sh: stopped ${orphans[*]}, which compose.yaml no longer has;" \
    "docker compose up -d --remove-orphans removes them" >&2
fi

# One statement string is one transaction, so there is never a moment without a plsremind database.
psql -c "DROP DATABASE IF EXISTS plsremind_replaced" \
  -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = 'plsremind'" \
  -c "ALTER DATABASE plsremind RENAME TO plsremind_replaced; ALTER DATABASE plsremind_restore RENAME TO plsremind;" \
  >/dev/null
echo "restored $file"
psql -c "DROP DATABASE plsremind_replaced" >/dev/null \
  || echo "restore.sh: the database it replaced is still there as plsremind_replaced; drop it once you no longer need it" >&2
