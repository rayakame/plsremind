#!/usr/bin/env bash
# Builds the image, starts a throwaway instance and walks through first use:
# setup, login cookie, creating an item. Exits non-zero on the first surprise.
set -euo pipefail

cd "$(dirname "$0")"
export COMPOSE_PROJECT_NAME=plsremind-smoke
export PLSREMIND_PORT="${PLSREMIND_PORT:-18080}"
export PLSREMIND_PUBLIC_URL="http://localhost:${PLSREMIND_PORT}"
export PLSREMIND_SECRET_KEY="$(openssl rand -base64 32)"
base="${PLSREMIND_PUBLIC_URL}/api/v1"
jar="$(mktemp)"

cleanup() {
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -f "$jar"
}
trap cleanup EXIT

fail() {
  echo "smoke: $1" >&2
  docker compose logs hub >&2 || true
  exit 1
}

expect() {
  local what="$1" got="$2" want="$3"
  [ "$got" = "$want" ] || fail "$what: expected $want, got $got"
  echo "ok  $what"
}

docker compose up -d --build --wait

for _ in $(seq 1 30); do
  curl -fsS "$base/info" >/dev/null 2>&1 && break
  sleep 1
done

expect "fresh instance needs setup" \
  "$(curl -fsS "$base/info" | grep -o '"setup_required":[a-z]*')" '"setup_required":true'

expect "setup creates the admin" \
  "$(curl -sS -o /dev/null -w '%{http_code}' -c "$jar" -H 'Content-Type: application/json' \
      -d '{"username":"admin","password":"smoke test password"}' "$base/setup")" 201

expect "the admin can create an item" \
  "$(curl -sS -o /dev/null -w '%{http_code}' -b "$jar" -H 'Content-Type: application/json' \
      -H "Origin: ${PLSREMIND_PUBLIC_URL}" -d '{"title":"first item","due_date":"2030-01-01"}' "$base/users/@me/items")" 201

expect "the item is listed" \
  "$(curl -fsS -b "$jar" "$base/users/@me/items" | grep -o '"title":"first item"')" '"title":"first item"'

expect "setup is closed afterwards" \
  "$(curl -fsS "$base/info" | grep -o '"setup_required":[a-z]*')" '"setup_required":false'

expect "the placeholder page is served" \
  "$(curl -sS -o /dev/null -w '%{http_code}' "${PLSREMIND_PUBLIC_URL}/")" 200

echo "smoke: all good"
