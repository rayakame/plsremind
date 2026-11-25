#!/usr/bin/env bash
# Updates the previous release, full of data, to a candidate hub image and checks that it all survived.
# PREVIOUS_COMPOSE and PREVIOUS_HUB_IMAGE stand in for a published release, to try this before one exists.
set -euo pipefail

if [ -n "${PREVIOUS_COMPOSE:-}" ]; then
  PREVIOUS_COMPOSE="$(cd "$(dirname "$PREVIOUS_COMPOSE")" && pwd)/$(basename "$PREVIOUS_COMPOSE")"
fi
cd "$(dirname "$0")"
previous="${1:?usage: ./upgrade-test.sh <previous version|none> <candidate hub image>}"
candidate="${2:?usage: ./upgrade-test.sh <previous version|none> <candidate hub image>}"
work="$(mktemp -d)"
server="$work/server"
port=18090
base="http://localhost:${port}"
export COMPOSE_PROJECT_NAME=plsremind-upgrade
unset COMPOSE_FILE COMPOSE_PROFILES

cleanup() {
  (cd "$server" 2>/dev/null && docker compose down -v --remove-orphans >/dev/null 2>&1) || true
  rm -rf "$work"
}
trap cleanup EXIT

fail() {
  echo "upgrade: $1" >&2
  (cd "$server" && docker compose logs hub >&2) || true
  exit 1
}

sql() {
  (cd "$server" && docker compose exec -T db psql --username plsremind --dbname plsremind -v ON_ERROR_STOP=1 -tA "$@")
}

# What the API cannot write: accounts linked through a provider, which need a real sign-in.
seed() {
  python3 upgrade-check.py seed "$base" "$work/state.json"
  sql -c "INSERT INTO identities (id, user_id, provider, external_id, display_name, username, created_at)
    SELECT gen_random_uuid(), id, p.provider, p.external_id, 'Ada', 'ada', now()
    FROM users, (VALUES ('github', '1815'), ('discord', '1843')) AS p (provider, external_id)" >/dev/null
  python3 upgrade-check.py snapshot "$base" "$work/state.json"
}

# Every table, also one no read covers yet, must hold rows and keep them through the update.
count_rows() {
  sql -c "SELECT table_name || ' ' || (xpath('/row/n/text()', query_to_xml(
      format('SELECT count(*) AS n FROM %I', table_name), false, true, '')))[1]::text
    FROM information_schema.tables
    WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
      AND table_name NOT IN ('goose_db_version', 'oauth_states')
    ORDER BY 1" > "$1"
}

# Its own port and network, and the hub pinned to an exact image rather than to the series.
use_hub() {
  cat > "$server/compose.override.yaml" <<YAML
services:
  hub:
    image: $1
    pull_policy: $2
    ports: !override
      - "${port}:8080"
networks:
  default:
    name: plsremind-upgrade
YAML
}

start() {
  (cd "$server" && docker compose up -d --wait db hub) || fail "the hub did not become ready"
}

mkdir -p "$server"
{
  echo "PLSREMIND_SECRET_KEY=$(openssl rand -base64 32)"
  echo "PLSREMIND_PUBLIC_URL=${base}"
} > "$server/.env"

if [ "$previous" != "none" ]; then
  if [ -n "${PREVIOUS_COMPOSE:-}" ]; then
    cp "$PREVIOUS_COMPOSE" "$server/compose.yaml"
  else
    gh release download "v${previous}" --pattern compose.yaml --dir "$server"
  fi
  if [ -n "${PREVIOUS_HUB_IMAGE:-}" ]; then
    use_hub "$PREVIOUS_HUB_IMAGE" never
  else
    use_hub "ghcr.io/rayakame/plsremind:${previous}" missing
  fi
  start
  seed
  count_rows "$work/before.txt"
  echo "ok  seeded release ${previous}"
fi

cp compose.yaml "$server/compose.yaml"
use_hub "$candidate" never
start
if [ "$previous" = "none" ]; then
  seed
  count_rows "$work/before.txt"
  echo "ok  seeded a fresh install"
fi
python3 upgrade-check.py verify "$base" "$work/state.json" || fail "data changed or went missing"
count_rows "$work/after.txt"
problems="$(awk 'NR == FNR { after[$1] = $2; next }
  $2 == 0 { print $1 ": the seed wrote no rows, so the update is not checked for it" }
  !($1 in after) { print $1 ": gone" ; next }
  after[$1] < $2 { print $1 ": had " $2 " rows, now " after[$1] }' "$work/after.txt" "$work/before.txt")"
[ -z "$problems" ] || fail "$problems"
echo "ok  $(wc -l < "$work/before.txt" | tr -d ' ') tables kept their rows"
echo "upgrade: all good"
