#!/usr/bin/env bash
# Builds the image, starts a throwaway instance and walks through first use:
# setup, creating an item, and a reminder that travels through the gateway to the
# example adapter and back as confirmed. Exits non-zero on the first surprise.
set -euo pipefail

cd "$(dirname "$0")"
export COMPOSE_PROJECT_NAME=plsremind-smoke
export PLSREMIND_PORT="${PLSREMIND_PORT:-18080}"
export PLSREMIND_PUBLIC_URL="http://localhost:${PLSREMIND_PORT}"
export PLSREMIND_SECRET_KEY="$(openssl rand -base64 32)"
base="${PLSREMIND_PUBLIC_URL}/api/v1"
jar="$(mktemp)"

adapter_pid=""
adapter_log="$(mktemp)"

cleanup() {
  [ -n "$adapter_pid" ] && kill "$adapter_pid" >/dev/null 2>&1 || true
  docker compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -f "$jar" "$adapter_log"
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

# A reminder, end to end: the example adapter waits on the gateway, an item comes due,
# the adapter confirms the delivery, and the log shows it as delivered.
token="$(curl -fsS -b "$jar" -H 'Content-Type: application/json' -H "Origin: ${PLSREMIND_PUBLIC_URL}" \
  -d '{"name":"smoke adapter","remind":true}' "$base/users/@me/tokens" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')"
[ -n "$token" ] || fail "could not create a personal token"

(cd ../hub && PLSREMIND_URL="$PLSREMIND_PUBLIC_URL" PLSREMIND_TOKEN="$token" go run ./examples/remind -once) >"$adapter_log" 2>&1 &
adapter_pid=$!
for _ in $(seq 1 60); do
  grep -q "waiting for reminders" "$adapter_log" && break
  sleep 1
done
grep -q "waiting for reminders" "$adapter_log" || { cat "$adapter_log" >&2; fail "the example adapter did not connect"; }

# The reminder is set relative to the hub's own clock, so a clock that differs
# between this machine and the container cannot make it "already past".
item="$(curl -fsS -b "$jar" -H 'Content-Type: application/json' -H "Origin: ${PLSREMIND_PUBLIC_URL}" \
  -d '{"title":"smoke reminder"}' "$base/users/@me/items" | sed -n 's/^{"id":"\([^"]*\)".*/\1/p')"
[ -n "$item" ] || fail "could not create the item to be reminded about"
expect "a reminder in one minute" \
  "$(curl -sS -o /dev/null -w '%{http_code}' -b "$jar" -H 'Content-Type: application/json' \
      -H "Origin: ${PLSREMIND_PUBLIC_URL}" -d '{"in":"1m"}' "$base/users/@me/items/$item/reminders")" 201

for _ in $(seq 1 100); do
  kill -0 "$adapter_pid" >/dev/null 2>&1 || break
  sleep 1
done
if kill -0 "$adapter_pid" >/dev/null 2>&1; then
  cat "$adapter_log" >&2
  curl -sS -b "$jar" "$base/users/@me/reminder-log" >&2 || true
  fail "the adapter never received the reminder"
fi
adapter_pid=""
expect "the adapter was reminded" "$(grep -c 'smoke reminder' "$adapter_log")" 1
expect "the hub recorded the confirmation" \
  "$(curl -fsS -b "$jar" "$base/users/@me/reminder-log" | grep -o '"status":"delivered"')" '"status":"delivered"'

echo "smoke: all good"
